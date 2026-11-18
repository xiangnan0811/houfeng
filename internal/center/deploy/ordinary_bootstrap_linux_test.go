//go:build linux

package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func ordinaryBootstrapFixture(t *testing.T) string {
	t.Helper()
	base := os.Getenv("HOUFENG_ORDINARY_GUARD_FIXTURE_ROOT")
	if base == "" || os.Geteuid() != 0 {
		t.Skip("requires root and HOUFENG_ORDINARY_GUARD_FIXTURE_ROOT")
	}
	work, err := os.MkdirTemp(base, "ordinary-bootstrap-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(work) })
	if err := os.Chmod(work, 0o755); err != nil {
		t.Fatal(err)
	}
	return work
}

func TestOrdinaryLegacyBootstrapAnchorsAndRefusesLostMarker(t *testing.T) {
	work := ordinaryBootstrapFixture(t)
	run := func() error {
		return ordinaryWithControllerLock(context.Background(), work, func() error { return ordinaryBootstrapGuard(work) })
	}
	if err := run(); err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}
	lock := filepath.Join(work, ordinaryControllerLockName)
	guard := filepath.Join(work, "maintenance", "ordinary", ordinaryGuardFile)
	lockBefore, err := os.Stat(lock)
	if err != nil {
		t.Fatal(err)
	}
	guardBefore, err := os.Stat(guard)
	if err != nil {
		t.Fatal(err)
	}
	if err := run(); err != nil {
		t.Fatalf("exact replay: %v", err)
	}
	lockAfter, e1 := os.Stat(lock)
	guardAfter, e2 := os.Stat(guard)
	if e1 != nil || e2 != nil || !os.SameFile(lockBefore, lockAfter) || !os.SameFile(guardBefore, guardAfter) {
		t.Fatal("permanent lock identity changed")
	}
	if err := os.Remove(filepath.Join(work, "maintenance", OrdinaryComposeInitializationMarker)); err != nil {
		t.Fatal(err)
	}
	if err := run(); err == nil {
		t.Fatal("lost established root marker was recreated")
	}
}

func TestOrdinaryBootstrapRejectsControllerLockReplacement(t *testing.T) {
	work := ordinaryBootstrapFixture(t)
	run := func() error {
		return ordinaryWithControllerLock(context.Background(), work, func() error { return ordinaryBootstrapGuard(work) })
	}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(work, ordinaryControllerLockName)); err != nil {
		t.Fatal(err)
	}
	if err := run(); err == nil {
		t.Fatal("replaced controller lock accepted")
	}
}

func TestOrdinaryBootstrapRecoversOnlyRecordedEmptyIntermediate(t *testing.T) {
	work := ordinaryBootstrapFixture(t)
	fd, err := unix.Open(work, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	var parent unix.Stat_t
	if unix.Fstat(fd, &parent) != nil {
		t.Fatal("stat fixture parent")
	}
	intent := ordinaryBootstrapDirectoryIntent{Version: 1, ParentDevice: uint64(parent.Dev), ParentInode: parent.Ino, Name: "ordinary", UID: composeLaunchPublisherUID, GID: composeLaunchRuntimeGID, Nonce: strings.Repeat("a", 32)}
	if ordinaryImageRecord(fd, ".ordinary-bootstrap-ordinary.intent.json", intent) != nil {
		t.Fatal("write durable fixture intent")
	}
	if err := unix.Mkdirat(fd, "ordinary", 0o700); err != nil {
		t.Fatal(err)
	}
	owned, err := ordinaryBootstrapDirectory(fd, "ordinary", composeLaunchPublisherUID, composeLaunchRuntimeGID)
	if err != nil || !owned {
		t.Fatalf("recover recorded empty intermediate: %v", err)
	}
	var st unix.Stat_t
	if unix.Fstatat(fd, "ordinary", &st, unix.AT_SYMLINK_NOFOLLOW) != nil || st.Uid != composeLaunchPublisherUID || st.Gid != composeLaunchRuntimeGID || st.Mode&0o7777 != 0o750 {
		t.Fatal("wrong recovered directory identity")
	}
	if err := os.Remove(filepath.Join(work, "ordinary")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(work, "ordinary"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ordinaryBootstrapDirectory(fd, "ordinary", composeLaunchPublisherUID, composeLaunchRuntimeGID); err == nil {
		t.Fatal("replaced committed directory accepted")
	}
	maintenanceIntent := ordinaryBootstrapDirectoryIntent{Version: 1, ParentDevice: uint64(parent.Dev), ParentInode: parent.Ino, Name: "maintenance", UID: 0, GID: composeLaunchRuntimeGID, Nonce: strings.Repeat("b", 32)}
	if ordinaryImageRecord(fd, ".ordinary-bootstrap-maintenance.intent.json", maintenanceIntent) != nil {
		t.Fatal("write maintenance fixture intent")
	}
	if err := os.Mkdir(filepath.Join(work, "maintenance"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "maintenance", "foreign"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ordinaryBootstrapDirectory(fd, "maintenance", 0, composeLaunchRuntimeGID); err == nil {
		t.Fatal("nonempty intermediate accepted")
	}
}

func TestOrdinaryPrivateSecretStoreExactReplay(t *testing.T) {
	work := ordinaryBootstrapFixture(t)
	fd, err := unix.Open(work, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	secret := []byte("nonproduction fixture secret")
	if err := ordinaryStageOneSecret(fd, "fixture", secret); err != nil {
		t.Fatal(err)
	}
	if err := ordinaryStageOneSecret(fd, "fixture", secret); err != nil {
		t.Fatalf("exact replay: %v", err)
	}
	var st unix.Stat_t
	if unix.Fstatat(fd, "fixture", &st, unix.AT_SYMLINK_NOFOLLOW) != nil || st.Uid != 0 || st.Gid != 0 || st.Mode&0o7777 != 0o444 || st.Nlink != 1 {
		t.Fatal("staged secret metadata mismatch")
	}
	if err := os.WriteFile(filepath.Join(work, "fixture"), []byte("foreign bytes"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := ordinaryStageOneSecret(fd, "fixture", secret); err == nil {
		t.Fatal("changed secret accepted")
	}
}

func TestOrdinaryPrivateInputRejectsUnprivilegedParent(t *testing.T) {
	work := ordinaryBootstrapFixture(t)
	directory := filepath.Join(work, "untrusted")
	if err := os.Mkdir(directory, 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "input")
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(directory, composeLaunchPublisherUID, composeLaunchRuntimeGID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readOrdinaryPrivateIdentity(path, 64); err == nil {
		t.Fatal("private input accepted an unprivileged renameable parent")
	}
}

func TestOrdinaryPrivateInputsAcceptRoot0700AndRejectUnsafeAncestors(t *testing.T) {
	work := ordinaryBootstrapFixture(t)
	private := filepath.Join(work, "private")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(private, "bootstrap")
	if err := os.WriteFile(secret, []byte("fixture-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := ordinaryUpgradeRequest{Version: 1, OperationID: "op_1", Project: "fixture", WorkingDirectory: work, DatabaseContainerID: strings.Repeat("a", 64), TargetImageID: "sha256:" + strings.Repeat("b", 64), Secrets: ordinaryUpgradeSecrets{Bootstrap: secret, Runtime: filepath.Join(private, "runtime"), PlatformAdmin: filepath.Join(private, "admin"), DeletionWorker: filepath.Join(private, "worker"), Migrator: filepath.Join(private, "migrator"), InitialPassword: filepath.Join(private, "initial"), SessionHMACKey: filepath.Join(private, "hmac")}}
	wire, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	requestFile := filepath.Join(private, "request.json")
	if err := os.WriteFile(requestFile, append(wire, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := ordinaryReadRequestFile(requestFile); err != nil || got != request {
		t.Fatalf("root-private request rejected: %v", err)
	}
	if body, _, err := readOrdinaryPrivateIdentity(secret, 64); err != nil || string(body) != "fixture-secret" {
		t.Fatalf("root-private secret rejected: %v", err)
	}
	staging, err := ordinaryOpenPrivateDirectories(private, "ordinary-secrets", "op_1", true)
	if err != nil {
		t.Fatalf("private staging under root-owned 0700 parent rejected: %v", err)
	}
	if err := ordinaryStageOneSecret(staging.operationFD, "fixture", []byte("fixture-secret")); err != nil || !staging.unchanged() {
		staging.Close()
		t.Fatalf("private staging lost its pinned ancestors: %v", err)
	}
	staging.Close()
	if err := os.Chmod(private, 0o720); err != nil {
		t.Fatal(err)
	}
	if _, err := ordinaryReadRequestFile(requestFile); err == nil {
		t.Fatal("group-writable request ancestor accepted")
	}
	if _, _, err := readOrdinaryPrivateIdentity(secret, 64); err == nil {
		t.Fatal("group-writable secret ancestor accepted")
	}
	if err := os.Chmod(private, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(private, composeLaunchPublisherUID, composeLaunchRuntimeGID); err != nil {
		t.Fatal(err)
	}
	if _, err := ordinaryReadRequestFile(requestFile); err == nil {
		t.Fatal("foreign-owned request ancestor accepted")
	}
	if _, _, err := readOrdinaryPrivateIdentity(secret, 64); err == nil {
		t.Fatal("foreign-owned secret ancestor accepted")
	}
}

func TestOrdinaryFenceControlIsPrivateAndOutsideProjectMounts(t *testing.T) {
	work := ordinaryBootstrapFixture(t)
	if _, err := ordinaryFenceControlPath(work, "missing", false); err == nil {
		t.Fatal("recovery created an absent fence directory")
	}
	path, err := ordinaryFenceControlPath(work, "op_1", true)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(work, ".ordinary-fences", "op_1", "request.json") || fenceOverlap(filepath.Dir(path), filepath.Join(work, "maintenance")) || fenceOverlap(filepath.Dir(path), filepath.Join(work, "data")) {
		t.Fatalf("unsafe fence path %q", path)
	}
	for _, dir := range []string{filepath.Join(work, ".ordinary-fences"), filepath.Dir(path)} {
		var stat unix.Stat_t
		if unix.Lstat(dir, &stat) != nil || !ordinaryPrivateDirectoryMatches(stat) {
			t.Fatalf("unsafe fence directory %q", dir)
		}
	}
	if again, err := ordinaryFenceControlPath(work, "op_1", false); err != nil || again != path {
		t.Fatal("exact fence directory replay failed")
	}
	if err := os.Symlink(filepath.Join(work, ".ordinary-fences", "op_1"), filepath.Join(work, ".ordinary-fences", "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := ordinaryFenceControlPath(work, "link", false); err == nil {
		t.Fatal("fence directory symlink accepted")
	}
}

func TestOrdinaryPrivateDirectoryRequiresRootAndMode0700(t *testing.T) {
	work := ordinaryBootstrapFixture(t)
	dirs, err := ordinaryOpenPrivateDirectories(work, ".ordinary-fences", "op_1", true)
	if err != nil {
		t.Fatalf("root-owned 0700 directory rejected: %v", err)
	}
	dirs.Close()
	root := filepath.Join(work, ".ordinary-fences")
	operation := filepath.Join(root, "op_1")
	if err := os.Chmod(operation, 0o750); err != nil {
		t.Fatal(err)
	}
	if dirs, err := ordinaryOpenPrivateDirectories(work, ".ordinary-fences", "op_1", false); err == nil {
		dirs.Close()
		t.Fatal("group-traversable operation directory accepted")
	}
	if err := os.Chmod(operation, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o705); err != nil {
		t.Fatal(err)
	}
	if dirs, err := ordinaryOpenPrivateDirectories(work, ".ordinary-fences", "op_1", false); err == nil {
		dirs.Close()
		t.Fatal("world-traversable root directory accepted")
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(operation, composeLaunchPublisherUID, composeLaunchRuntimeGID); err != nil {
		t.Fatal(err)
	}
	if dirs, err := ordinaryOpenPrivateDirectories(work, ".ordinary-fences", "op_1", false); err == nil {
		dirs.Close()
		t.Fatal("foreign-owned operation directory accepted")
	}
}

func TestOrdinaryControllerRootsPreserveHeldGuard(t *testing.T) {
	work := ordinaryBootstrapFixture(t)
	request := ordinaryUpgradeRequest{Project: "fixture", OperationID: "op_1", WorkingDirectory: work}
	err := ordinaryWithControllerLock(context.Background(), work, func() error {
		if err := ordinaryBootstrapGuard(work); err != nil {
			return err
		}
		if err := ordinaryPrepareControllerRoots(request, true); err != nil {
			return err
		}
		dirs, err := ordinaryOpenOperationDirectories(work, request.OperationID, true)
		if err != nil {
			return err
		}
		defer dirs.Close()
		guard := filepath.Join(work, "maintenance", "ordinary")
		return withOrdinaryComposeGuardFD(context.Background(), guard, true, func(guardFD int) error {
			guardDirs, err := openComposeLaunchDirectories(guard)
			if err != nil {
				return fmt.Errorf("open guard ancestors: %w", err)
			}
			defer func() {
				for _, dir := range guardDirs {
					_ = unix.Close(dir.fd)
				}
			}()
			anchor, err := ordinaryGuardIdentity(guardFD)
			if err != nil {
				return fmt.Errorf("read guard anchor: %w", err)
			}
			checkGuard := func(stage string) error {
				for i, dir := range guardDirs {
					var current unix.Stat_t
					if err := unix.Fstat(dir.fd, &current); err != nil {
						return fmt.Errorf("%s stat guard ancestor %q: %w", stage, dir.name, err)
					}
					if !sameComposeLaunchDirectory(dir.before, current) {
						return fmt.Errorf("%s guard ancestor %q changed: dev %d/%d inode %d/%d mode %#o/%#o uid %d/%d gid %d/%d nlink %d/%d", stage, dir.name, dir.before.Dev, current.Dev, dir.before.Ino, current.Ino, dir.before.Mode, current.Mode, dir.before.Uid, current.Uid, dir.before.Gid, current.Gid, dir.before.Nlink, current.Nlink)
					}
					if i > 0 {
						var named unix.Stat_t
						if err := unix.Fstatat(guardDirs[i-1].fd, dir.name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil || !sameComposeLaunchDirectory(current, named) {
							return fmt.Errorf("%s named guard ancestor %q changed: %v", stage, dir.name, err)
						}
					}
				}
				actual, err := ordinaryGuardIdentity(guardFD)
				if err != nil {
					return fmt.Errorf("%s read guard anchor: %w", stage, err)
				}
				if actual.stat.Dev != anchor.stat.Dev || actual.stat.Ino != anchor.stat.Ino || actual.stat.Mode != anchor.stat.Mode || actual.stat.Uid != anchor.stat.Uid || actual.stat.Gid != anchor.stat.Gid || actual.stat.Nlink != anchor.stat.Nlink || actual.nonce != anchor.nonce || !ordinaryGuardMatches(guardDirs, guardFD, anchor) {
					return fmt.Errorf("%s guard anchor changed: dev %d/%d inode %d/%d mode %#o/%#o uid %d/%d gid %d/%d nlink %d/%d", stage, anchor.stat.Dev, actual.stat.Dev, anchor.stat.Ino, actual.stat.Ino, anchor.stat.Mode, actual.stat.Mode, anchor.stat.Uid, actual.stat.Uid, anchor.stat.Gid, actual.stat.Gid, anchor.stat.Nlink, actual.stat.Nlink)
				}
				return nil
			}
			if err := checkGuard("before"); err != nil {
				return err
			}
			targets, err := ordinaryImageDirectories(filepath.Join(work, "maintenance", "ordinary-targets"))
			if err != nil {
				return fmt.Errorf("open target root: %w", err)
			}
			defer func() {
				for _, dir := range targets {
					_ = unix.Close(dir.fd)
				}
			}()
			targetFD := targets[len(targets)-1].fd
			var targetBefore, targetAfter unix.Stat_t
			if err := unix.Fstat(targetFD, &targetBefore); err != nil {
				return fmt.Errorf("stat target root: %w", err)
			}
			if err := ordinaryImageMkdirJournalled(targetFD, strings.Repeat("a", 64), request.Project, request.OperationID); err != nil {
				return fmt.Errorf("create target child: %w", err)
			}
			if err := unix.Fstat(targetFD, &targetAfter); err != nil {
				return fmt.Errorf("stat target root after mkdir: %w", err)
			}
			if targetAfter.Dev != targetBefore.Dev || targetAfter.Ino != targetBefore.Ino || targetAfter.Mode != targetBefore.Mode || targetAfter.Uid != targetBefore.Uid || targetAfter.Gid != targetBefore.Gid || targetAfter.Nlink != targetBefore.Nlink+1 {
				return fmt.Errorf("target root identity or nlink changed unexpectedly: before=%+v after=%+v", targetBefore, targetAfter)
			}
			if err := checkGuard("after target child"); err != nil {
				return err
			}
			secrets, err := ordinaryOpenRootDirectories(filepath.Join(work, "maintenance", "ordinary-secrets"))
			if err != nil {
				return fmt.Errorf("open private secret root: %w", err)
			}
			defer func() {
				for _, dir := range secrets {
					_ = unix.Close(dir.fd)
				}
			}()
			secretFD := secrets[len(secrets)-1].fd
			var secretBefore, secretAfter unix.Stat_t
			if err := unix.Fstat(secretFD, &secretBefore); err != nil {
				return fmt.Errorf("stat secret root: %w", err)
			}
			private, err := ordinaryOpenPrivateDirectories(filepath.Join(work, "maintenance"), "ordinary-secrets", request.OperationID, true)
			if err != nil {
				return fmt.Errorf("create private secret child: %w", err)
			}
			private.Close()
			if err := unix.Fstat(secretFD, &secretAfter); err != nil {
				return fmt.Errorf("stat secret root after mkdir: %w", err)
			}
			if secretAfter.Dev != secretBefore.Dev || secretAfter.Ino != secretBefore.Ino || secretAfter.Mode != secretBefore.Mode || secretAfter.Uid != secretBefore.Uid || secretAfter.Gid != secretBefore.Gid || secretAfter.Nlink != secretBefore.Nlink+1 {
				return fmt.Errorf("secret root identity or nlink changed unexpectedly: before=%+v after=%+v", secretBefore, secretAfter)
			}
			return checkGuard("after secret child")
		})
	})
	if err != nil {
		t.Fatalf("operation-specific directories invalidated guard: %v", err)
	}
	if err := os.Remove(filepath.Join(work, "maintenance", "ordinary-secrets", "op_1")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(work, "maintenance", "ordinary-secrets")); err != nil {
		t.Fatal(err)
	}
	if err := ordinaryPrepareControllerRoots(request, false); err == nil {
		t.Fatal("ACTIVE recovery recreated a missing private root")
	}
}

func TestOrdinaryComposeOverridePinsImagesAndFiles(t *testing.T) {
	request := ordinaryUpgradeRequest{OperationID: "test_1", WorkingDirectory: "/srv/project$fixture", TargetImageID: "sha256:" + strings.Repeat("a", 64)}
	body, err := ordinaryComposeOverride(request)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if strings.Count(text, "!override") != len(ordinarySecretNames) || strings.Count(text, request.TargetImageID) != 7 || !strings.Contains(text, "/srv/project$$fixture/") || strings.Contains(text, "environment:") {
		t.Fatalf("unexpected fixed override: %s", text)
	}
	for _, version := range []string{"2.24.4", "v2.25.0", "2.30.0-desktop.1", "3.0.0"} {
		if !ordinaryComposeVersionSupported(version) {
			t.Errorf("supported version rejected: %s", version)
		}
	}
	for _, version := range []string{"2.24.3", "2.24.4-rc.1", "2.23.9", "1.99.99", "junk"} {
		if ordinaryComposeVersionSupported(version) {
			t.Errorf("unsupported version accepted: %s", version)
		}
	}
}
