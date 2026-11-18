//go:build linux

package deploy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"houfeng/internal/center/recordauthority"
	"houfeng/internal/center/recordbirth"
)

// The immutable operation intent is synthetic; the guard, executable digest,
// root marker, file ownership and cross-UID admission are real.
func TestOrdinaryReadyAdmitsOnlySelectedExecutable(t *testing.T) {
	base := os.Getenv("HOUFENG_ORDINARY_GUARD_FIXTURE_ROOT")
	if base == "" || os.Geteuid() != 0 {
		t.Skip("requires root and HOUFENG_ORDINARY_GUARD_FIXTURE_ROOT")
	}
	work, err := os.MkdirTemp(base, "ordinary-ready-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(work) })
	if err := os.Chmod(work, 0o755); err != nil {
		t.Fatal(err)
	}
	maintenance := filepath.Join(work, "maintenance")
	if err := os.Mkdir(maintenance, 0o755); err != nil {
		t.Fatal(err)
	}
	guard := filepath.Join(maintenance, "ordinary")
	if err := os.Mkdir(guard, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(guard, 10002, 10001); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(guard, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := ProvisionOrdinaryComposeGuard(guard); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(work, "ready-fixture-test")
	out, err := os.OpenFile(copyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		source.Close()
		t.Fatal(err)
	}
	_, copyErr := io.Copy(out, source)
	closeErr := out.Close()
	_ = source.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatalf("copy test executable: %v %v", copyErr, closeErr)
	}
	if err := os.Chmod(copyPath, 0o755); err != nil {
		t.Fatal(err)
	}
	selfDigest, err := recordbirth.DigestExecutable("/proc/self/exe")
	if err != nil {
		t.Fatal(err)
	}
	authorityRoot := filepath.Join(work, "authority")
	if _, err := recordauthority.CreateComposeState(authorityRoot); err != nil {
		t.Fatal(err)
	}
	state, err := recordauthority.PrepareComposeMembershipV2(authorityRoot)
	if err != nil || state.MembershipRotation == nil {
		t.Fatalf("prepare synthetic authority: %v", err)
	}
	activation, err := state.ActivationCommand.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	rotation, err := state.MembershipRotation.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	centerHash, workerHash := sha256.Sum256([]byte("center")), sha256.Sum256([]byte("worker"))
	center := recordbirth.LaunchBinding{ProtocolVersion: 1, ProcessKind: "center", DeploymentID: string(state.DeploymentID), ActivationBundleDigest: state.ActivationCommand.ActivationBundleDigest, BinaryDigest: centerHash}
	worker := center
	worker.ProcessKind, worker.BinaryDigest = "content-processor", workerHash
	pair, err := encodeComposeBirthLaunchPair(center, worker)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := ordinaryCurrentTargetContract()
	if err != nil {
		t.Fatal(err)
	}
	contractWire, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	contractWire = append(contractWire, '\n')
	dummy := strings.Repeat("a", 64)
	request := ordinaryUpgradeRequest{Version: 1, OperationID: "selected", Project: "fixture", WorkingDirectory: work, DatabaseContainerID: strings.Repeat("b", 64), TargetImageID: "sha256:" + strings.Repeat("c", 64), Secrets: ordinaryUpgradeSecrets{Bootstrap: "/root/a", Runtime: "/root/b", PlatformAdmin: "/root/c", DeletionWorker: "/root/d", Migrator: "/root/e", InitialPassword: "/root/f", SessionHMACKey: "/root/g"}}
	intent := ordinaryUpgradeIntent{
		Version: 3, Request: request,
		OldCenterContainerID: strings.Repeat("e", 64), SecretCommitmentSHA256: dummy,
		Recipe:      ordinaryComposeRecipe{Compose: ordinaryRecipeFile{Inode: 1, SHA256: dummy}, Environment: ordinaryEnvironmentIdentity{Inode: 2}, ProxyName: "compose.proxy-host.yaml", Proxy: ordinaryRecipeFile{Inode: 3, SHA256: dummy}},
		Physical:    ordinaryPhysicalDatabase{SystemIdentifier: "1", DatabaseOID: 1, PostmasterStarted: time.Now().UTC()},
		Predecessor: ordinaryPredecessorIntent{CatalogClass: ordinaryPredecessorC81, DeploymentID: string(state.DeploymentID), GenesisLedgerSHA256: dummy, AuthorityPublicKeySHA256: dummy, ActivationReceiptSHA256: dummy},
		Target:      ordinaryTargetIntent{AdminSHA256: hex.EncodeToString(selfDigest[:]), CenterSHA256: hex.EncodeToString(centerHash[:]), ProcessorSHA256: hex.EncodeToString(workerHash[:]), ContractSHA256: ordinaryDigestHex(contractWire), ContractWire: contractWire, C81Identity: contract.SourceIdentitySHA256, ActivationCommand: activation, RotationCommand: rotation, CenterLaunch: pair.centerBytes, ProcessorLaunch: pair.processorBytes},
	}
	paths := []string{request.Secrets.Bootstrap, request.Secrets.Runtime, request.Secrets.PlatformAdmin, request.Secrets.DeletionWorker, request.Secrets.Migrator, request.Secrets.InitialPassword, request.Secrets.SessionHMACKey}
	for i, path := range paths {
		intent.Secrets = append(intent.Secrets, ordinarySecretIdentity{Path: path, Inode: uint64(i + 1), Size: 1, SHA256: dummy})
	}
	publish := func(operation, adminHash string) (string, [32]byte) {
		t.Helper()
		intent.Request.OperationID = operation
		intent.Target.AdminSHA256 = adminHash
		encoded, err := json.Marshal(intent.Request)
		if err != nil {
			t.Fatal(err)
		}
		intent.RequestSHA256 = ordinaryDigestHex(append(encoded, '\n'))
		override, err := ordinaryComposeOverride(intent.Request)
		if err != nil {
			t.Fatal(err)
		}
		intent.Recipe.OverrideSHA256 = ordinaryDigestHex(override)
		dirs, err := ordinaryOpenOperationDirectories(work, operation, true)
		if err != nil {
			t.Fatal(err)
		}
		defer dirs.Close()
		intent.Fence = fenceJournal{Version: 1, Request: ComposeOfflineFenceRequest{RecoveryID: operation, Project: request.Project, WorkingDirectory: work, DatabaseContainerID: request.DatabaseContainerID, ControlConfigPath: dirs.fenceConfigPath}, Containers: []fenceContainer{{ID: request.DatabaseContainerID, Project: request.Project, WorkingDirectory: work, Service: "db", Networks: []fenceNetworkAnchor{{ID: strings.Repeat("d", 64)}}}, {ID: strings.Repeat("e", 64), Project: request.Project, WorkingDirectory: work, Service: "houfeng"}}}
		digest, err := ordinaryPublishIntent(dirs.dirs[len(dirs.dirs)-1].fd, intent)
		if err != nil {
			t.Fatalf("publish intent %s: %v", operation, err)
		}
		journalBytes, err := json.Marshal(intent.Fence)
		if err != nil {
			t.Fatal(err)
		}
		fd := dirs.dirs[len(dirs.dirs)-1].fd
		fenced := ComposeOfflineFence{RecoveryID: operation, DatabaseContainerID: request.DatabaseContainerID, MaintenanceNetworkID: strings.Repeat("f", 64), InventoryDigest: sha256.Sum256(journalBytes)}
		intentHex := hex.EncodeToString(digest[:])
		if err := ordinaryRecordRestoration(fd, intent, intentHex, fenced); err != nil {
			t.Fatal(err)
		}
		return operation, digest
	}
	selectReady := func(operation string, digest [32]byte) {
		t.Helper()
		operationDirs, err := ordinaryOpenOperationDirectories(work, operation, false)
		if err != nil {
			t.Fatal(err)
		}
		defer operationDirs.Close()
		if err := withOrdinaryComposeGuardFD(context.Background(), guard, true, func(guardFD int) error {
			dirs, err := openComposeLaunchDirectories(guard)
			if err != nil {
				return err
			}
			defer func() {
				for _, dir := range dirs {
					_ = unix.Close(dir.fd)
				}
			}()
			state, present, err := ordinaryReadDisposition(dirs[len(dirs)-1].fd)
			if err != nil || !present {
				return errOrdinaryUpgrade
			}
			state.State, state.OperationID, state.IntentSHA256 = "ACTIVE", operation, hex.EncodeToString(digest[:])
			if err := ordinaryWriteDisposition(dirs, state, false); err != nil {
				return err
			}
			return ordinaryCommitReady(dirs, guardFD, operationDirs.dirs[len(operationDirs.dirs)-1].fd, intent, hex.EncodeToString(digest[:]))
		}); err != nil {
			t.Fatal(err)
		}
	}
	child := func(env string) {
		t.Helper()
		cmd := exec.Command(copyPath, "-test.run=^TestOrdinaryGuardFixtureChild$")
		cmd.Env = append(os.Environ(), "HOUFENG_ORDINARY_GUARD_CHILD_ROOT="+guard, env+"=1")
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 10001, Gid: 10001, Groups: []uint32{10001}}}
		if body, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("shared admission fixture: %v %s", err, body)
		}
	}
	operation, digest := publish("selected", hex.EncodeToString(selfDigest[:]))
	selectReady(operation, digest)
	if err := WithOrdinaryComposeGuard(context.Background(), guard, false, func() error { return nil }); err != nil {
		t.Fatalf("selected target denied: %v", err)
	}
	child("HOUFENG_ORDINARY_GUARD_CHILD_SHARED")
	operation, digest = publish("other", strings.Repeat("e", 64))
	selectReady(operation, digest)
	if err := WithOrdinaryComposeGuard(context.Background(), guard, false, func() error { t.Error("old executable admitted"); return nil }); err == nil {
		t.Fatal("old executable was admitted")
	}
	child("HOUFENG_ORDINARY_GUARD_CHILD_REJECT")
}
