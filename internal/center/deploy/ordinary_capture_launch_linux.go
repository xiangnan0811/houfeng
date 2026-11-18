//go:build linux

package deploy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5"
	"golang.org/x/sys/unix"

	"houfeng/internal/center/recordauthority"
	"houfeng/internal/center/recordbirth"
)

// Capture an existing registration and both corresponding protected files as
// one predecessor. A pre-C81 database may have no registration or files.
func ordinaryCaptureLaunchPredecessor(ctx context.Context, observer *pgx.Conn, work string, predecessor *ordinaryPredecessorIntent) error {
	if ctx == nil || observer == nil || predecessor == nil {
		return errOrdinaryUpgrade
	}
	directory := filepath.Join(work, "data", "center-config")
	dirs, err := openComposeLaunchDirectories(directory)
	if err != nil {
		return errOrdinaryUpgrade
	}
	defer func() {
		for _, dir := range dirs {
			_ = unix.Close(dir.fd)
		}
	}()
	var tableExists bool
	if err := observer.QueryRow(ctx, `select to_regclass('record_platform_internal.record_birth_launch_state') is not null`).Scan(&tableExists); err != nil {
		return errOrdinaryUpgrade
	}
	if !tableExists && predecessor.CatalogClass != ordinaryPredecessorC80 {
		return errOrdinaryUpgrade
	}
	if tableExists && predecessor.CatalogClass == ordinaryPredecessorC80 {
		return errOrdinaryUpgrade
	}
	var registry composeBirthLaunchRegistry
	var manifest, currentManifest []byte
	var readErr error
	if tableExists {
		readErr = observer.QueryRow(ctx, `select l.revision,l.opened,l.runtime_principal::text,l.worker_principal::text,l.center_binding,l.processor_binding,l.manifest_digest,h.manifest_digest from record_platform_internal.record_birth_launch_state l join public.app_acl_manifest_head h on h.singleton where l.singleton`).Scan(&registry.revision, &registry.opened, &registry.runtimeRole, &registry.workerRole, &registry.centerBytes, &registry.processorBytes, &manifest, &currentManifest)
	} else {
		readErr = pgx.ErrNoRows
	}
	if errors.Is(readErr, pgx.ErrNoRows) {
		for _, name := range []string{composeCenterBirthLaunchFile, composeProcessorBirthLaunchFile} {
			if _, err := os.Lstat(filepath.Join(directory, name)); !errors.Is(err, os.ErrNotExist) {
				return errOrdinaryUpgrade
			}
		}
		if !composeLaunchDirectoriesUnchanged(dirs) {
			return errOrdinaryUpgrade
		}
		return nil
	}
	if readErr != nil || registry.revision <= 0 || registry.runtimeRole != composeRuntimeRole || registry.workerRole != composeDeletionWorkerRole || len(manifest) != 32 || !bytes.Equal(manifest, currentManifest) {
		return errOrdinaryUpgrade
	}
	for _, item := range []struct {
		name, kind string
		body       []byte
	}{{composeCenterBirthLaunchFile, "center", registry.centerBytes}, {composeProcessorBirthLaunchFile, "content-processor", registry.processorBytes}} {
		expected, err := recordbirth.DecodeLaunchBinding(item.body)
		observed, digest, readErr := recordbirth.ReadPublishedLaunchBinding(filepath.Join(directory, item.name), item.kind)
		if err != nil || readErr != nil || observed != expected || digest != sha256.Sum256(item.body) {
			return errOrdinaryUpgrade
		}
	}
	if !composeLaunchDirectoriesUnchanged(dirs) {
		return errOrdinaryUpgrade
	}
	predecessor.RegistryRevision = registry.revision
	predecessor.RegistryOpened = registry.opened
	predecessor.RegistryManifestSHA256 = hex.EncodeToString(manifest)
	predecessor.RegistryRuntimeRole = registry.runtimeRole
	predecessor.RegistryWorkerRole = registry.workerRole
	predecessor.CenterLaunch = bytes.Clone(registry.centerBytes)
	predecessor.ProcessorLaunch = bytes.Clone(registry.processorBytes)
	return nil
}

func ordinaryBuildTargetLaunch(state recordauthority.VerifiedComposeState, image ordinaryImageTarget) (composeBirthLaunchPair, error) {
	center := recordbirth.LaunchBinding{ProtocolVersion: 1, ProcessKind: "center", DeploymentID: string(state.DeploymentID), ActivationBundleDigest: state.ActivationCommand.ActivationBundleDigest, BinaryDigest: image.CenterSHA256}
	processor := center
	processor.ProcessKind = "content-processor"
	processor.BinaryDigest = image.ProcessorSHA256
	return encodeComposeBirthLaunchPair(center, processor)
}
