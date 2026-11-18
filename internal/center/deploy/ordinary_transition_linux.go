//go:build linux

package deploy

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recordauthority"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/store/migrate"
)

func ordinaryParticipantTransition(ctx context.Context, intent ordinaryUpgradeIntent, passwords ComposeInitPasswords, address, backendDirectory, targetDirectory string) (result error) {
	phase := ordinaryNativePreview
	defer func() {
		if result != nil {
			result = &ordinaryParticipantPhaseError{status: phase, cause: result}
		}
	}()
	if ctx == nil {
		return errOrdinaryUpgrade
	}
	root := filepath.Join(intent.Request.WorkingDirectory, "data", "records-authority")
	launchDirectory := filepath.Join(intent.Request.WorkingDirectory, "data", "center-config")
	state, predecessor, err := ordinaryCapturePredecessorAuthority(root)
	p := intent.Predecessor
	if err != nil || predecessor.DeploymentID != p.DeploymentID || predecessor.GenesisLedgerSHA256 != p.GenesisLedgerSHA256 || predecessor.AuthorityPublicKeySHA256 != p.AuthorityPublicKeySHA256 || predecessor.ActivationReceiptSHA256 != p.ActivationReceiptSHA256 {
		return errOrdinaryUpgrade
	}
	activation, err1 := state.ActivationCommand.MarshalBinary()
	rotation, err2 := state.MembershipRotation.MarshalBinary()
	if err1 != nil || err2 != nil || !bytes.Equal(activation, intent.Target.ActivationCommand) || !bytes.Equal(rotation, intent.Target.RotationCommand) {
		return errOrdinaryUpgrade
	}
	phase = ordinaryNativeConnect
	endpoint := composePostgresEndpoint{Host: address, Port: composeDatabasePort, Database: composeDatabaseName, Role: composeMigratorRole, Password: passwords.Migrator}
	migrator, err := ordinaryOpenTrackedPool(ctx, endpoint, intent.Physical, intent.Request.OperationID, backendDirectory)
	if err != nil {
		return errOrdinaryUpgrade
	}
	defer migrator.Close()
	connection, err := migrator.pool.Acquire(ctx)
	if err != nil {
		return errOrdinaryUpgrade
	}
	err = ordinaryObserverRequireIntentSourceAbsent(ctx, connection.Conn(), intent)
	connection.Release()
	if err != nil {
		return errOrdinaryUpgrade
	}
	phase = ordinaryNativeRegistry
	// Check the old or exact already-staged target before changing the catalog.
	registry, manifest, err := ordinaryParticipantRegistry(ctx, migrator.pool, intent.Predecessor.CatalogClass == ordinaryPredecessorC80)
	if err != nil || !ordinaryParticipantRegistryExpected(registry, manifest, intent) {
		return errOrdinaryUpgrade
	}
	phase = ordinaryNativeBootstrap
	config, err := NewComposeInitConfig(passwords)
	if err != nil || !distinctComposePasswords(passwords.Bootstrap, passwords.Runtime, passwords.PlatformAdmin, passwords.DeletionWorker, passwords.Migrator, state.DatabasePassword()) {
		return errOrdinaryUpgrade
	}
	bootstrapEndpoint := composePostgresEndpoint{Host: address, Port: composeDatabasePort, Database: config.DatabaseName, Role: config.BootstrapRole, Password: passwords.Bootstrap}
	bootstrap, err := ordinaryOpenTrackedPool(ctx, bootstrapEndpoint, intent.Physical, intent.Request.OperationID, backendDirectory)
	if err != nil {
		return errOrdinaryUpgrade
	}
	bootstrapConfig := platformmigrate.ComposeBootstrapConfig{
		DatabaseName: config.DatabaseName, BootstrapRole: config.BootstrapRole, AuthorityRole: config.AuthorityRole, Roles: config.Roles,
		Passwords: platformmigrate.ComposeRolePasswords{Runtime: passwords.Runtime, PlatformAdmin: passwords.PlatformAdmin, DeletionWorker: passwords.DeletionWorker, Migrator: passwords.Migrator, Authority: state.DatabasePassword()},
	}
	err = platformmigrate.ProvisionComposeBootstrap(ctx, bootstrap.pool, bootstrapConfig)
	bootstrap.Close()
	if err != nil {
		return err
	}
	phase = ordinaryNativeConverge
	if _, err := migrate.ConvergeAppACLCurrent(ctx, migrator.pool, composeRuntimeRole, composeAdminRole, composeDeletionWorkerRole); err != nil {
		return err
	}
	connection, err = migrator.pool.Acquire(ctx)
	if err != nil {
		return errOrdinaryUpgrade
	}
	err = ordinaryObserverRequireCurrentSourceAbsent(ctx, connection.Conn())
	connection.Release()
	if err != nil {
		return errOrdinaryUpgrade
	}
	phase = ordinaryNativeRegistry
	registry, manifest, err = ordinaryParticipantRegistry(ctx, migrator.pool, false)
	if err != nil || !ordinaryParticipantRegistryExpected(registry, manifest, intent) {
		return errOrdinaryUpgrade
	}
	phase = ordinaryNativeLaunchClose
	if registry != nil && registry.opened {
		if _, err := migrator.pool.Exec(ctx, `select record_platform_internal.record_birth_launch_close($1::bigint)`, registry.revision); err != nil {
			return errOrdinaryUpgrade
		}
		registry, manifest, err = ordinaryParticipantRegistry(ctx, migrator.pool, false)
		if err != nil || registry == nil || registry.opened || !ordinaryParticipantRegistryExpected(registry, manifest, intent) {
			return errOrdinaryUpgrade
		}
	}
	phase = ordinaryNativePrepare
	prepared, err := recordauthority.PrepareComposeMembershipV2(root)
	if err != nil || prepared.MembershipRotation == nil {
		return errOrdinaryUpgrade
	}
	actualActivation, err1 := prepared.ActivationCommand.MarshalBinary()
	actualRotation, err2 := prepared.MembershipRotation.MarshalBinary()
	if err1 != nil || err2 != nil || !bytes.Equal(actualActivation, intent.Target.ActivationCommand) || !bytes.Equal(actualRotation, intent.Target.RotationCommand) {
		return errOrdinaryUpgrade
	}
	roles := platformmigrate.AppRoleSetV2{CenterRuntime: composeRuntimeRole, PlatformAdmin: composeAdminRole, DeletionWorker: composeDeletionWorkerRole, Migrator: composeMigratorRole}
	phase = ordinaryNativeProject
	receipt, err := migrate.ProjectComposeMembershipV2(ctx, migrator.pool, roles, actualActivation, actualRotation)
	if err != nil {
		return errOrdinaryUpgrade
	}
	phase = ordinaryNativeReceipt
	if recordauthority.PersistMembershipRotationReceipt(root, prepared, receipt) != nil || recordauthority.VerifyMembershipRotationReceipt(root, prepared) != nil || recordauthority.VerifyActivationReceipt(root, prepared) != nil {
		return errOrdinaryUpgrade
	}
	phase = ordinaryNativeLaunchStage
	paths := ordinaryTargetPaths(targetDirectory)
	target, err := expectedComposeBirthLaunch(ctx, migrator.pool, prepared, nil, paths)
	if err != nil || !bytes.Equal(target.centerBytes, intent.Target.CenterLaunch) || !bytes.Equal(target.processorBytes, intent.Target.ProcessorLaunch) {
		return errOrdinaryUpgrade
	}
	var revision int64
	if err := migrator.pool.QueryRow(ctx, `select record_platform_internal.record_birth_launch_stage($1::bytea,$2::bytea,$3::name,$4::name,$5::bigint)`, target.centerBytes, target.processorBytes, composeRuntimeRole, composeDeletionWorkerRole, p.RegistryRevision).Scan(&revision); err != nil || (revision != p.RegistryRevision && revision != p.RegistryRevision+1) || revision <= 0 {
		return errOrdinaryUpgrade
	}
	registry, _, err = ordinaryParticipantRegistry(ctx, migrator.pool, false)
	if err != nil || registry == nil || registry.revision != revision || registry.opened || !bytes.Equal(registry.centerBytes, target.centerBytes) || !bytes.Equal(registry.processorBytes, target.processorBytes) {
		return errOrdinaryUpgrade
	}
	phase = ordinaryNativeLaunchPublish
	if p.RegistryRevision == 0 {
		if err := publishComposeBirthLaunchPair(launchDirectory, target); err != nil {
			return errOrdinaryUpgrade
		}
	} else {
		oldCenter, err1 := recordbirth.DecodeLaunchBinding(p.CenterLaunch)
		oldProcessor, err2 := recordbirth.DecodeLaunchBinding(p.ProcessorLaunch)
		old, err3 := encodeComposeBirthLaunchPair(oldCenter, oldProcessor)
		if err1 != nil || err2 != nil || err3 != nil || !bytes.Equal(old.centerBytes, p.CenterLaunch) || !bytes.Equal(old.processorBytes, p.ProcessorLaunch) || replaceOrdinaryComposeBirthLaunchPair(launchDirectory, old, target) != nil {
			return errOrdinaryUpgrade
		}
	}
	phase = ordinaryNativeLaunchVerify
	observed, err := inspectPublishedComposeBirthLaunch(ctx, migrator.pool, prepared, nil, launchDirectory, false, paths)
	if err != nil || observed.revision != revision || !bytes.Equal(observed.centerBytes, target.centerBytes) || !bytes.Equal(observed.processorBytes, target.processorBytes) {
		return errOrdinaryUpgrade
	}
	phase = ordinaryNativeLaunchOpen
	if _, err := migrator.pool.Exec(ctx, `select record_platform_internal.record_birth_launch_open($1::bigint)`, revision); err != nil {
		return errOrdinaryUpgrade
	}
	phase = ordinaryNativeLaunchVerify
	observed, err = inspectPublishedComposeBirthLaunch(ctx, migrator.pool, prepared, nil, launchDirectory, true, paths)
	if err != nil || observed.revision != revision || !bytes.Equal(observed.centerBytes, target.centerBytes) || !bytes.Equal(observed.processorBytes, target.processorBytes) {
		return errOrdinaryUpgrade
	}
	phase = ordinaryNativeHeartbeat
	authorityEndpoint := composePostgresEndpoint{Host: address, Port: composeDatabasePort, Database: composeDatabaseName, Role: composeAuthorityRole, Password: prepared.DatabasePassword()}
	authority, err := ordinaryOpenTrackedPool(ctx, authorityEndpoint, intent.Physical, intent.Request.OperationID, backendDirectory)
	if err != nil {
		return errOrdinaryUpgrade
	}
	defer authority.Close()
	if _, err := heartbeatComposeAuthorityAt(ctx, authority.pool, prepared, time.Now().UTC()); err != nil {
		return errOrdinaryUpgrade
	}
	phase = ordinaryNativeAdmit
	runtimeEndpoint := composePostgresEndpoint{Host: address, Port: composeDatabasePort, Database: composeDatabaseName, Role: composeRuntimeRole, Password: passwords.Runtime, LaunchDigest: target.centerDigest}
	runtimePool, err := ordinaryOpenTrackedPool(ctx, runtimeEndpoint, intent.Physical, intent.Request.OperationID, backendDirectory)
	if err != nil {
		return errOrdinaryUpgrade
	}
	defer runtimePool.Close()
	phase = ordinaryNativeProbe
	if err := probeComposeRuntimeLaunch(ctx, runtimePool.pool, target.center, nil); err != nil {
		return errOrdinaryUpgrade
	}
	return nil
}

func ordinaryParticipantRegistry(ctx context.Context, pool *pgxpool.Pool, allowMissing bool) (*composeBirthLaunchRegistry, string, error) {
	var exists bool
	if err := pool.QueryRow(ctx, `select to_regclass('record_platform_internal.record_birth_launch_state') is not null`).Scan(&exists); err != nil {
		return nil, "", errOrdinaryUpgrade
	}
	if !exists {
		if !allowMissing {
			return nil, "", errOrdinaryUpgrade
		}
		return nil, "", nil
	}
	var registry composeBirthLaunchRegistry
	var manifest []byte
	err := pool.QueryRow(ctx, `select revision,opened,runtime_principal::text,worker_principal::text,center_binding,processor_binding,manifest_digest from record_platform_internal.record_birth_launch_state where singleton`).Scan(&registry.revision, &registry.opened, &registry.runtimeRole, &registry.workerRole, &registry.centerBytes, &registry.processorBytes, &manifest)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil || len(manifest) != 32 || registry.revision <= 0 {
		return nil, "", errOrdinaryUpgrade
	}
	return &registry, hex.EncodeToString(manifest), nil
}

func ordinaryParticipantRegistryExpected(registry *composeBirthLaunchRegistry, manifest string, intent ordinaryUpgradeIntent) bool {
	p := intent.Predecessor
	if registry == nil {
		return p.RegistryRevision == 0
	}
	if registry.runtimeRole != composeRuntimeRole || registry.workerRole != composeDeletionWorkerRole {
		return false
	}
	old := registry.revision == p.RegistryRevision && manifest == p.RegistryManifestSHA256 && bytes.Equal(registry.centerBytes, p.CenterLaunch) && bytes.Equal(registry.processorBytes, p.ProcessorLaunch)
	target := (registry.revision == p.RegistryRevision || registry.revision == p.RegistryRevision+1) && bytes.Equal(registry.centerBytes, intent.Target.CenterLaunch) && bytes.Equal(registry.processorBytes, intent.Target.ProcessorLaunch)
	return old || target
}
