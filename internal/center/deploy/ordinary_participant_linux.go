//go:build linux

package deploy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

type ordinaryParticipantPasswords struct {
	Bootstrap      string `json:"bootstrap"`
	Runtime        string `json:"runtime"`
	PlatformAdmin  string `json:"platform_admin"`
	DeletionWorker string `json:"deletion_worker"`
	Migrator       string `json:"migrator"`
}

type ordinaryParticipantInput struct {
	Version            int                          `json:"version"`
	OperationDirectory string                       `json:"operation_directory"`
	DatabaseAddress    string                       `json:"database_address"`
	Passwords          ordinaryParticipantPasswords `json:"passwords"`
}

// RunOrdinaryNativeParticipant is reachable only through the fixed private
// admin mode. Receive validates identity and returns nothing until EXECUTE ACK.
func RunOrdinaryNativeParticipant(ctx context.Context) error {
	binding, payload, err := ordinaryReceiveNativeExecute()
	if err != nil {
		return errOrdinaryUpgrade
	}
	defer clear(payload)
	status := ordinaryNativeSucceeded
	if err := ordinaryExecuteParticipant(ctx, binding, payload); err != nil {
		status = ordinaryNativeFailed
		var phase *ordinaryParticipantPhaseError
		if errors.As(err, &phase) {
			if _, err := ordinaryEncodeNativeStatus(phase.status); err == nil && phase.status != ordinaryNativeSucceeded {
				status = phase.status
			}
		}
	}
	return ordinaryWriteNativeStatus(status)
}

func ordinaryExecuteParticipant(ctx context.Context, binding ordinaryNativeBinding, raw []byte) error {
	var input ordinaryParticipantInput
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil {
		return errOrdinaryUpgrade
	}
	canonical, err := json.Marshal(input)
	if err != nil || !bytes.Equal(canonical, raw) || input.Version != 1 || net.ParseIP(input.DatabaseAddress) == nil || !filepath.IsAbs(input.OperationDirectory) || filepath.Clean(input.OperationDirectory) != input.OperationDirectory {
		return errOrdinaryUpgrade
	}
	defer clear(canonical)
	dirs, err := ordinaryImageDirectories(input.OperationDirectory)
	if err != nil {
		return errOrdinaryUpgrade
	}
	defer func() {
		for _, dir := range dirs {
			_ = unix.Close(dir.fd)
		}
	}()
	operationDir := dirs[len(dirs)-1]
	if operationDir.before.Uid != 0 || operationDir.before.Gid != composeLaunchRuntimeGID || operationDir.before.Mode&0o7777 != 0o750 {
		return errOrdinaryUpgrade
	}
	body, exists, err := ordinaryReadOperationFile(operationDir.fd, "intent.json")
	var intent ordinaryUpgradeIntent
	if err != nil || !exists || sha256.Sum256(body) != binding.IntentSHA256 || fenceDecode(body, &intent) != nil || ordinaryValidateIntent(intent) != nil || intent.Request.OperationID != binding.OperationID || input.OperationDirectory != filepath.Join(intent.Request.WorkingDirectory, "maintenance", "ordinary-operations", binding.OperationID) || hex.EncodeToString(binding.AdminSHA256[:]) != intent.Target.AdminSHA256 || !composeLaunchDirectoriesUnchanged(dirs) {
		return errOrdinaryUpgrade
	}
	values := []string{input.Passwords.Bootstrap, input.Passwords.Runtime, input.Passwords.PlatformAdmin, input.Passwords.DeletionWorker, input.Passwords.Migrator}
	for _, value := range values {
		if !validComposePassword(value) {
			return errOrdinaryUpgrade
		}
	}
	passwords := ComposeInitPasswords{Bootstrap: values[0], Runtime: values[1], PlatformAdmin: values[2], DeletionWorker: values[3], Migrator: values[4]}
	if _, err := NewComposeInitConfig(passwords); err != nil {
		return errOrdinaryUpgrade
	}
	guardRoot := filepath.Join(intent.Request.WorkingDirectory, "maintenance", "ordinary")
	guardDirs, err := openComposeLaunchDirectories(guardRoot)
	if err != nil {
		return errOrdinaryUpgrade
	}
	defer func() {
		for _, dir := range guardDirs {
			_ = unix.Close(dir.fd)
		}
	}()
	anchor, err := ordinaryGuardIdentity(binding.GuardFD)
	if err != nil || !ordinaryGuardMatches(guardDirs, binding.GuardFD, anchor) || len(guardDirs) < 2 {
		return errOrdinaryUpgrade
	}
	marker, exists, err := ordinaryReadRootMarker(guardDirs[len(guardDirs)-2].fd)
	if err != nil || !exists || marker.Phase != "COMMITTED" || !ordinaryDispositionMatches(ordinaryMarkerDisposition(marker), guardDirs, anchor) {
		return errOrdinaryUpgrade
	}
	state, exists, err := ordinaryReadDisposition(guardDirs[len(guardDirs)-1].fd)
	if err != nil || !exists || state.State != "ACTIVE" || state.OperationID != binding.OperationID || state.IntentSHA256 != hex.EncodeToString(binding.IntentSHA256[:]) || state.BootstrapName != marker.BootstrapName || !ordinaryDispositionMatches(state, guardDirs, anchor) {
		return errOrdinaryUpgrade
	}
	targetDirectory := filepath.Join(intent.Request.WorkingDirectory, "maintenance", "ordinary-targets", strings.TrimPrefix(intent.Request.TargetImageID, "sha256:"))
	if binding.AdminPath != filepath.Join(targetDirectory, "houfeng-record-platform-admin") {
		return errOrdinaryUpgrade
	}
	targetDirs, err := ordinaryImageDirectories(targetDirectory)
	if err != nil {
		return errOrdinaryUpgrade
	}
	defer func() {
		for _, dir := range targetDirs {
			_ = unix.Close(dir.fd)
		}
	}()
	for _, item := range []struct{ name, digest string }{{"houfeng-record-platform-admin", intent.Target.AdminSHA256}, {"houfeng-center", intent.Target.CenterSHA256}, {"houfeng-content-processor", intent.Target.ProcessorSHA256}} {
		digest, present, err := ordinaryImageReadExecutable(targetDirs[len(targetDirs)-1].fd, item.name)
		if err != nil || !present || hex.EncodeToString(digest[:]) != item.digest {
			return errOrdinaryUpgrade
		}
	}
	contract, err := ordinaryDecodeTargetContract(intent.Target.ContractWire)
	if err != nil || contract.SourceIdentitySHA256 != intent.Target.C81Identity {
		return errOrdinaryUpgrade
	}
	current, err := ordinaryCurrentTargetContract()
	if err != nil || current.Version != contract.Version || current.NativeProtocolVersion != contract.NativeProtocolVersion || current.SchemaVersion != contract.SchemaVersion || current.SourceIdentitySHA256 != contract.SourceIdentitySHA256 || !bytes.Equal(current.CanonicalMigrationSet, contract.CanonicalMigrationSet) || !bytes.Equal(current.CanonicalPrivilegeSet, contract.CanonicalPrivilegeSet) {
		return errOrdinaryUpgrade
	}
	return ordinaryParticipantTransition(ctx, intent, passwords, input.DatabaseAddress, filepath.Join(input.OperationDirectory, "backends"), targetDirectory)
}
