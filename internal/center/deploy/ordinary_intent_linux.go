//go:build linux

package deploy

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/unix"

	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordplatform"
)

type ordinarySecretIdentity struct {
	Path   string `json:"path"`
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	Size   int64  `json:"-"`
	SHA256 string `json:"-"`
}

type ordinaryPredecessorIntent struct {
	CatalogClass             string `json:"catalog_class"`
	DeploymentID             string `json:"deployment_id"`
	GenesisLedgerSHA256      string `json:"genesis_ledger_sha256"`
	AuthorityPublicKeySHA256 string `json:"authority_public_key_sha256"`
	ActivationReceiptSHA256  string `json:"activation_receipt_sha256"`
	RegistryRevision         int64  `json:"registry_revision"`
	RegistryOpened           bool   `json:"registry_opened"`
	RegistryManifestSHA256   string `json:"registry_manifest_sha256"`
	RegistryRuntimeRole      string `json:"registry_runtime_role"`
	RegistryWorkerRole       string `json:"registry_worker_role"`
	CenterLaunch             []byte `json:"center_launch"`
	ProcessorLaunch          []byte `json:"processor_launch"`
}

type ordinaryTargetIntent struct {
	AdminSHA256       string `json:"admin_sha256"`
	CenterSHA256      string `json:"center_sha256"`
	ProcessorSHA256   string `json:"processor_sha256"`
	ContractSHA256    string `json:"contract_sha256"`
	ContractWire      []byte `json:"contract_wire"`
	C81Identity       string `json:"c81_identity"`
	ActivationCommand []byte `json:"activation_command"`
	RotationCommand   []byte `json:"rotation_command"`
	CenterLaunch      []byte `json:"center_launch"`
	ProcessorLaunch   []byte `json:"processor_launch"`
}

type ordinaryIntentLink struct {
	OperationID  string `json:"operation_id"`
	IntentSHA256 string `json:"intent_sha256"`
}

type ordinaryUpgradeIntent struct {
	Version                int                       `json:"version"`
	Request                ordinaryUpgradeRequest    `json:"request"`
	RequestSHA256          string                    `json:"request_sha256"`
	Secrets                []ordinarySecretIdentity  `json:"secrets"`
	OldCenterContainerID   string                    `json:"old_center_container_id"`
	SecretCommitmentSHA256 string                    `json:"secret_commitment_sha256"`
	Recipe                 ordinaryComposeRecipe     `json:"recipe"`
	Namespace              ordinaryNamespaceBaseline `json:"namespace"`
	Physical               ordinaryPhysicalDatabase  `json:"physical"`
	Predecessor            ordinaryPredecessorIntent `json:"predecessor"`
	Target                 ordinaryTargetIntent      `json:"target"`
	Fence                  fenceJournal              `json:"fence"`
	Previous               *ordinaryIntentLink       `json:"previous,omitempty"`
}

func ordinaryDigestHex(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

// Validate immutable local bindings. Authenticity of the signed predecessor,
// physical database, target image provenance and observed Docker inventory is
// established by their respective real readers before constructing the intent.
func ordinaryValidateIntent(intent ordinaryUpgradeIntent) error {
	requestBytes, err := json.Marshal(intent.Request)
	if err != nil {
		return errOrdinaryUpgrade
	}
	requestBytes = append(requestBytes, '\n')
	if ordinaryDigestHex(requestBytes) != intent.RequestSHA256 {
		return errOrdinaryUpgrade
	}
	switch intent.Version {
	case 3:
		if intent.Previous != nil {
			return errOrdinaryUpgrade
		}
	case 4:
		link := intent.Previous
		if link == nil || !fenceRID.MatchString(link.OperationID) || link.OperationID == intent.Request.OperationID || !fenceID.MatchString(link.IntentSHA256) ||
			intent.Predecessor.CatalogClass != ordinaryPredecessorC81 || !intent.Predecessor.RegistryOpened || intent.Predecessor.RegistryRevision <= 0 {
			return errOrdinaryUpgrade
		}
	default:
		return errOrdinaryUpgrade
	}
	if _, err := decodeOrdinaryUpgradeRequest(requestBytes); err != nil || len(intent.Secrets) != 7 {
		return errOrdinaryUpgrade
	}
	paths := []string{intent.Request.Secrets.Bootstrap, intent.Request.Secrets.Runtime, intent.Request.Secrets.PlatformAdmin, intent.Request.Secrets.DeletionWorker, intent.Request.Secrets.Migrator, intent.Request.Secrets.InitialPassword, intent.Request.Secrets.SessionHMACKey}
	for i, secret := range intent.Secrets {
		if secret.Path != paths[i] || secret.Inode == 0 {
			return errOrdinaryUpgrade
		}
	}
	if intent.Recipe.Compose.Inode == 0 || intent.Recipe.Environment.Inode == 0 || intent.Recipe.Proxy.Inode == 0 || (intent.Recipe.ProxyName != "compose.proxy-network.yaml" && intent.Recipe.ProxyName != "compose.proxy-host.yaml") || !fenceID.MatchString(intent.Recipe.Compose.SHA256) || !fenceID.MatchString(intent.Recipe.Proxy.SHA256) || !fenceID.MatchString(intent.Recipe.OverrideSHA256) {
		return errOrdinaryUpgrade
	}
	if !ordinaryNamespaceBaselineMatches(intent.Namespace) {
		return errOrdinaryUpgrade
	}
	override, err := ordinaryComposeOverride(intent.Request)
	if err != nil || ordinaryDigestHex(override) != intent.Recipe.OverrideSHA256 {
		return errOrdinaryUpgrade
	}
	p, target := intent.Predecessor, intent.Target
	if p.CatalogClass != ordinaryPredecessorC80 && p.CatalogClass != ordinaryPredecessorC81 {
		return errOrdinaryUpgrade
	}
	if p.CatalogClass == ordinaryPredecessorC80 && p.RegistryRevision != 0 {
		return errOrdinaryUpgrade
	}
	for _, digest := range []string{p.GenesisLedgerSHA256, p.AuthorityPublicKeySHA256, p.ActivationReceiptSHA256, target.AdminSHA256, target.CenterSHA256, target.ProcessorSHA256, target.ContractSHA256, target.C81Identity} {
		if !fenceID.MatchString(digest) {
			return errOrdinaryUpgrade
		}
	}
	if len(target.ContractWire) == 0 || ordinaryDigestHex(target.ContractWire) != target.ContractSHA256 || intent.Physical.SystemIdentifier == "" || intent.Physical.DatabaseOID == 0 || intent.Physical.PostmasterStarted.IsZero() {
		return errOrdinaryUpgrade
	}
	contract, err := ordinaryDecodeTargetContract(target.ContractWire)
	if err != nil || contract.SourceIdentitySHA256 != target.C81Identity {
		return errOrdinaryUpgrade
	}
	systemID, err := strconv.ParseUint(intent.Physical.SystemIdentifier, 10, 64)
	if err != nil || systemID == 0 || strconv.FormatUint(systemID, 10) != intent.Physical.SystemIdentifier {
		return errOrdinaryUpgrade
	}
	activation, err := recordplatform.ParseContractActivationProjectionCommandV1(target.ActivationCommand)
	if err != nil {
		return errOrdinaryUpgrade
	}
	rotation, err := recordplatform.ParseDomainRotationProjectionCommandV1(target.RotationCommand)
	if err != nil || activation.DeploymentID != p.DeploymentID || rotation.DeploymentID != activation.DeploymentID || rotation.ActiveProfile != activation.ActiveProfile || rotation.ExpectedWitnessedLedgerSequence != activation.WitnessedLedgerSequence || rotation.ExpectedWitnessedLedgerHash != activation.WitnessedLedgerHash || rotation.ExpectedIdentitySetEpoch != activation.IdentitySetEpoch || rotation.ExpectedIdentitySetDigest != activation.IdentitySetDigest || rotation.ExpectedAdapterPolicyGeneration != activation.AdapterPolicyGeneration || rotation.ExpectedAdapterPolicyDigest != activation.AdapterPolicyDigest || rotation.ExpectedMinimumFenceContractVersion != activation.MinimumFenceContractVersion || rotation.ExpectedTrustRevision != activation.TrustRevision || rotation.ExpectedTrustHeadHash != activation.TrustHeadHash || rotation.NextWitnessedLedgerSequence != 2 || rotation.NextIdentitySetEpoch != 2 || rotation.NextMinimumFenceContractVersion != 1 {
		return errOrdinaryUpgrade
	}
	checkLaunch := func(center, processor []byte) bool {
		c, e1 := recordbirth.DecodeLaunchBinding(center)
		p, e2 := recordbirth.DecodeLaunchBinding(processor)
		return e1 == nil && e2 == nil && c.ProcessKind == "center" && p.ProcessKind == "content-processor" && c.DeploymentID == activation.DeploymentID && p.DeploymentID == activation.DeploymentID && c.ActivationBundleDigest == activation.ActivationBundleDigest && p.ActivationBundleDigest == activation.ActivationBundleDigest && !c.BirthInstalled && !p.BirthInstalled && c.SourceGeneration == 0 && p.SourceGeneration == 0
	}
	if !checkLaunch(target.CenterLaunch, target.ProcessorLaunch) {
		return errOrdinaryUpgrade
	}
	center, _ := recordbirth.DecodeLaunchBinding(target.CenterLaunch)
	processor, _ := recordbirth.DecodeLaunchBinding(target.ProcessorLaunch)
	if hex.EncodeToString(center.BinaryDigest[:]) != target.CenterSHA256 || hex.EncodeToString(processor.BinaryDigest[:]) != target.ProcessorSHA256 {
		return errOrdinaryUpgrade
	}
	if p.RegistryRevision < 0 || (p.RegistryRevision == 0 && (p.RegistryOpened || p.RegistryManifestSHA256 != "" || p.RegistryRuntimeRole != "" || p.RegistryWorkerRole != "" || len(p.CenterLaunch) != 0 || len(p.ProcessorLaunch) != 0)) || (p.RegistryRevision > 0 && (!fenceID.MatchString(p.RegistryManifestSHA256) || p.RegistryRuntimeRole != composeRuntimeRole || p.RegistryWorkerRole != composeDeletionWorkerRole || !checkLaunch(p.CenterLaunch, p.ProcessorLaunch))) {
		return errOrdinaryUpgrade
	}
	fence := intent.Fence
	if fence.Version != 1 || fence.Request.RecoveryID != intent.Request.OperationID || fence.Request.Project != intent.Request.Project || fence.Request.WorkingDirectory != intent.Request.WorkingDirectory || fence.Request.DatabaseContainerID != intent.Request.DatabaseContainerID || fence.Request.ControlConfigPath != filepath.Join(intent.Request.WorkingDirectory, ".ordinary-fences", intent.Request.OperationID, "request.json") {
		return errOrdinaryUpgrade
	}
	if !fenceID.MatchString(intent.OldCenterContainerID) || !fenceID.MatchString(intent.SecretCommitmentSHA256) {
		return errOrdinaryUpgrade
	}
	foundDB, foundCenter := 0, 0
	for _, container := range fence.Containers {
		if !fenceID.MatchString(container.ID) || container.Project != intent.Request.Project || container.WorkingDirectory != intent.Request.WorkingDirectory || container.Service == "" {
			return errOrdinaryUpgrade
		}
		if container.Service == "db" {
			foundDB++
			if container.ID != intent.Request.DatabaseContainerID || len(container.Networks) == 0 {
				return errOrdinaryUpgrade
			}
		}
		if container.Service == "houfeng" {
			foundCenter++
			if container.ID != intent.OldCenterContainerID {
				return errOrdinaryUpgrade
			}
		}
	}
	if foundDB != 1 || foundCenter != 1 {
		return errOrdinaryUpgrade
	}
	return nil
}

// Publish one immutable, exact-replay intent into a root-owned operation
// directory before any fenced effects. Caller holds controller lock and guard EX.
func ordinaryPublishIntent(dir int, intent ordinaryUpgradeIntent) ([32]byte, error) {
	var digest [32]byte
	var stat unix.Stat_t
	if ordinaryValidateIntent(intent) != nil || unix.Fstat(dir, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != 0 || stat.Gid != composeLaunchRuntimeGID || stat.Mode&0o7777 != 0o750 || stat.Nlink == 0 {
		return digest, errOrdinaryUpgrade
	}
	body, err := json.Marshal(intent)
	if err != nil || len(body) > fenceOutputLimit {
		return digest, errOrdinaryUpgrade
	}
	existing, present, err := ordinaryReadOperationFile(dir, "intent.json")
	if err != nil {
		return digest, errOrdinaryUpgrade
	}
	if !present {
		if ordinaryPublishOperationFile(dir, "intent.json", body) != nil {
			return digest, errOrdinaryUpgrade
		}
		existing, present, err = ordinaryReadOperationFile(dir, "intent.json")
	}
	if err != nil || !present || !bytes.Equal(existing, body) || unix.Fsync(dir) != nil {
		return digest, errOrdinaryUpgrade
	}
	return sha256.Sum256(body), nil
}

func ordinaryReadOperationFile(dir int, name string) ([]byte, bool, error) {
	fd, err := unix.Openat2(dir, name, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK, Resolve: maintenanceGuardResolve})
	if errors.Is(err, unix.ENOENT) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, errOrdinaryUpgrade
	}
	file := os.NewFile(uintptr(fd), "ordinary operation file")
	defer file.Close()
	var before, after, named unix.Stat_t
	if unix.Fstat(fd, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Mode&0o7777 != 0o440 || before.Uid != 0 || before.Gid != composeLaunchRuntimeGID || before.Nlink != 1 || before.Size <= 0 || before.Size > fenceOutputLimit {
		return nil, false, errOrdinaryUpgrade
	}
	body, err := io.ReadAll(io.LimitReader(file, fenceOutputLimit+1))
	if err != nil || len(body) != int(before.Size) || unix.Fstat(fd, &after) != nil || unix.Fstatat(dir, name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameSourceFile(before, after) || !sameSourceFile(before, named) {
		return nil, false, errOrdinaryUpgrade
	}
	return body, true, nil
}

func ordinaryPublishOperationFile(dir int, name string, body []byte) error {
	temp, err := ordinaryLaunchTempName()
	if err != nil {
		return errOrdinaryUpgrade
	}
	fd, err := unix.Openat2(dir, temp, &unix.OpenHow{Flags: unix.O_WRONLY | unix.O_CREAT | unix.O_EXCL | unix.O_CLOEXEC | unix.O_NOFOLLOW, Mode: 0o600, Resolve: maintenanceGuardResolve})
	if err != nil {
		return errOrdinaryUpgrade
	}
	file := os.NewFile(uintptr(fd), "ordinary operation temporary")
	defer func() { _ = file.Close(); _ = unix.Unlinkat(dir, temp, 0) }()
	if unix.Fchown(fd, 0, composeLaunchRuntimeGID) != nil || unix.Fchmod(fd, 0o440) != nil {
		return errOrdinaryUpgrade
	}
	if n, err := file.Write(body); err != nil || n != len(body) || file.Sync() != nil || unix.Renameat2(dir, temp, dir, name, unix.RENAME_NOREPLACE) != nil || unix.Fsync(dir) != nil {
		return errOrdinaryUpgrade
	}
	return nil
}
