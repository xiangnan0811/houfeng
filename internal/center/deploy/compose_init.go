package deploy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recordauthority"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/store"
	"houfeng/internal/center/store/migrate"
)

const (
	composeDatabaseHost           = "db"
	composeDatabasePort           = uint16(5432)
	composeDatabaseName           = "houfeng"
	composeBootstrapRole          = "postgres"
	composeRuntimeRole            = "houfeng_runtime"
	composeAdminRole              = "houfeng_platform_admin"
	composeDeletionWorkerRole     = "houfeng_record_deletion_worker"
	composeMigratorRole           = "houfeng_migrator"
	composeAuthorityRole          = "houfeng_records_authority"
	composeAuthorityStateRoot     = "/var/lib/houfeng/records-authority"
	composeCenterDeploymentIDPath = "/var/lib/houfeng/center-config/deployment-id"
)

var (
	ErrInvalidComposeInitConfig      = errors.New("Compose deployment initialization configuration is invalid")
	ErrComposeInitOpenBootstrap      = errors.New("open Compose bootstrap PostgreSQL connection failed")
	ErrComposeInitPrepareAuthority   = errors.New("prepare Compose Records authority state failed")
	ErrComposeInitProvisionBootstrap = errors.New("provision Compose database bootstrap contract failed")
	ErrComposeInitOpenMigrator       = errors.New("open Compose migrator PostgreSQL connection failed")
	ErrComposeInitConvergeCurrent    = errors.New("converge current Compose application database failed")
	ErrComposeInitActivateAuthority  = errors.New("activate Compose Records authority contract failed")
	ErrComposeInitPublishAuthority   = errors.New("publish Compose Center deployment identity failed")
	ErrComposeInitBirthLaunch        = errors.New("prepare Compose birth launch registration failed")
	ErrComposeInitOpenAuthority      = errors.New("open Compose Records authority PostgreSQL connection failed")
	ErrComposeInitHeartbeatAuthority = errors.New("establish Compose Records authority membership failed")
	ErrComposeInitOpenRuntime        = errors.New("open Compose runtime PostgreSQL connection failed")
	ErrComposeInitAdmitRuntime       = errors.New("admit current Compose runtime database failed")
	ErrComposeInitMaintenance        = errors.New("Compose source maintenance check failed")
	ErrComposeColdUpgradeRequired    = errors.New("active ordinary Compose V1 requires cold upgrade")
)

type ComposeInitPasswords struct {
	Bootstrap      string
	Runtime        string
	PlatformAdmin  string
	DeletionWorker string
	Migrator       string
}

type ComposeInitConfig struct {
	DatabaseHost           string
	DatabasePort           uint16
	DatabaseName           string
	BootstrapRole          string
	AuthorityRole          string
	AuthorityStateRoot     string
	CenterDeploymentIDPath string
	Roles                  platformmigrate.AppRoleSetV2
	Passwords              ComposeInitPasswords
}

func NewComposeInitConfig(passwords ComposeInitPasswords) (ComposeInitConfig, error) {
	config := ComposeInitConfig{
		DatabaseHost:           composeDatabaseHost,
		DatabasePort:           composeDatabasePort,
		DatabaseName:           composeDatabaseName,
		BootstrapRole:          composeBootstrapRole,
		AuthorityRole:          composeAuthorityRole,
		AuthorityStateRoot:     composeAuthorityStateRoot,
		CenterDeploymentIDPath: composeCenterDeploymentIDPath,
		Roles: platformmigrate.AppRoleSetV2{
			CenterRuntime:  composeRuntimeRole,
			PlatformAdmin:  composeAdminRole,
			DeletionWorker: composeDeletionWorkerRole,
			Migrator:       composeMigratorRole,
		},
		Passwords: passwords,
	}
	if err := config.Validate(); err != nil {
		return ComposeInitConfig{}, err
	}
	return config, nil
}

func (config ComposeInitConfig) Validate() error {
	if config.DatabaseHost != composeDatabaseHost || config.DatabasePort != composeDatabasePort ||
		config.BootstrapRole != composeBootstrapRole || config.AuthorityRole != composeAuthorityRole ||
		!validComposeAuthorityStateRoot(config.AuthorityStateRoot) ||
		!validComposeCenterDeploymentIDPath(config.CenterDeploymentIDPath) || config.Roles.Validate() != nil {
		return ErrInvalidComposeInitConfig
	}
	for _, password := range []string{
		config.Passwords.Bootstrap,
		config.Passwords.Runtime,
		config.Passwords.PlatformAdmin,
		config.Passwords.DeletionWorker,
		config.Passwords.Migrator,
	} {
		if !validComposePassword(password) {
			return ErrInvalidComposeInitConfig
		}
	}
	if !distinctComposePasswords(
		config.Passwords.Bootstrap,
		config.Passwords.Runtime,
		config.Passwords.PlatformAdmin,
		config.Passwords.DeletionWorker,
		config.Passwords.Migrator,
	) {
		return ErrInvalidComposeInitConfig
	}
	authorityValidationPassword := "state-derived-placeholder"
	for !distinctComposePasswords(
		config.Passwords.Runtime,
		config.Passwords.PlatformAdmin,
		config.Passwords.DeletionWorker,
		config.Passwords.Migrator,
		authorityValidationPassword,
	) {
		authorityValidationPassword += "-next"
	}
	if err := (platformmigrate.ComposeBootstrapConfig{
		DatabaseName:  config.DatabaseName,
		BootstrapRole: config.BootstrapRole,
		AuthorityRole: config.AuthorityRole,
		Roles:         config.Roles,
		Passwords: platformmigrate.ComposeRolePasswords{
			Runtime:        config.Passwords.Runtime,
			PlatformAdmin:  config.Passwords.PlatformAdmin,
			DeletionWorker: config.Passwords.DeletionWorker,
			Migrator:       config.Passwords.Migrator,
			Authority:      authorityValidationPassword,
		},
	}).Validate(); err != nil {
		return ErrInvalidComposeInitConfig
	}
	return nil
}

func distinctComposePasswords(passwords ...string) bool {
	seen := make(map[string]struct{}, len(passwords))
	for _, password := range passwords {
		if _, duplicate := seen[password]; duplicate {
			return false
		}
		seen[password] = struct{}{}
	}
	return true
}

func validComposeAuthorityStateRoot(root string) bool {
	return root != "" && filepath.IsAbs(root) && filepath.Clean(root) == root && filepath.Dir(root) != root
}

func validComposeCenterDeploymentIDPath(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path &&
		filepath.Dir(path) != path && filepath.Base(path) == "deployment-id"
}

func validComposePassword(password string) bool {
	if password == "" || !utf8.ValidString(password) {
		return false
	}
	for _, value := range password {
		if value == 0 || unicode.IsControl(value) {
			return false
		}
	}
	return true
}

type composePostgresEndpoint struct {
	Host         string
	Port         uint16
	Database     string
	Role         string
	Password     string
	LaunchDigest [32]byte
}

type composeInitDependencies struct {
	openPostgres       func(context.Context, composePostgresEndpoint) (*pgxpool.Pool, error)
	closePostgres      func(*pgxpool.Pool)
	preflightAuthority func(context.Context, *pgxpool.Pool, recordauthority.VerifiedComposeState, *SourceMaintenanceConfig) error
	prepareAuthority   func(context.Context, *pgxpool.Pool, string, *SourceMaintenanceConfig) (recordauthority.VerifiedComposeState, error)
	provisionBootstrap func(context.Context, *pgxpool.Pool, platformmigrate.ComposeBootstrapConfig) error
	convergeCurrent    func(context.Context, *pgxpool.Pool, string, string, string) error
	activateAuthority  func(context.Context, *pgxpool.Pool, string, recordauthority.VerifiedComposeState, platformmigrate.AppRoleSetV2) error
	publishAuthority   func(string, recordauthority.VerifiedComposeState, string) error
	prepareBirthLaunch func(context.Context, *pgxpool.Pool, recordauthority.VerifiedComposeState, *SourceMaintenanceConfig, string, string, string) (composeBirthLaunchPair, error)
	inspectBirthLaunch func(context.Context, *pgxpool.Pool, recordauthority.VerifiedComposeState, *SourceMaintenanceConfig, string, bool) (composeBirthLaunchPair, error)
	heartbeatAuthority func(context.Context, *pgxpool.Pool, recordauthority.VerifiedComposeState) error
	admitRuntime       func(context.Context, *pgxpool.Pool) error
	probeRuntime       func(context.Context, *pgxpool.Pool, recordbirth.LaunchBinding, *SourceMaintenanceConfig) error
	withMaintenance    func(context.Context, *pgxpool.Pool, func(*SourceMaintenanceConfig) error) error
	verifyCredentials  func(context.Context, *pgxpool.Pool, *SourceMaintenanceConfig, platformmigrate.AppRoleSetV2, string, platformmigrate.ComposeRolePasswords) error
}

// InitializeCompose provisions the fixed production Compose database contract.
func InitializeCompose(ctx context.Context, config ComposeInitConfig) error {
	return initializeCompose(ctx, config, openComposePostgres)
}

// initializeCompose is the narrow transport seam used by the strict
// PostgreSQL integration fixture. Every production dependency other than the
// connection opener remains wired here exactly as InitializeCompose uses it.
func initializeCompose(
	ctx context.Context,
	config ComposeInitConfig,
	openPostgres func(context.Context, composePostgresEndpoint) (*pgxpool.Pool, error),
) error {
	return initializeComposeWithDependencies(ctx, config, defaultComposeInitDependencies(openPostgres))
}

func defaultComposeInitDependencies(
	openPostgres func(context.Context, composePostgresEndpoint) (*pgxpool.Pool, error),
) composeInitDependencies {
	return composeInitDependencies{
		openPostgres:       openPostgres,
		closePostgres:      func(pool *pgxpool.Pool) { pool.Close() },
		preflightAuthority: preflightComposeAuthorityState,
		prepareAuthority:   prepareComposeAuthorityState,
		provisionBootstrap: platformmigrate.ProvisionComposeBootstrap,
		convergeCurrent: func(ctx context.Context, pool *pgxpool.Pool, runtimeRole, adminRole, deletionWorkerRole string) error {
			_, err := migrate.ConvergeAppACLCurrent(ctx, pool, runtimeRole, adminRole, deletionWorkerRole)
			return err
		},
		activateAuthority:  activateComposeAuthorityState,
		publishAuthority:   publishComposeAuthorityDeploymentID,
		prepareBirthLaunch: prepareComposeBirthLaunch,
		inspectBirthLaunch: func(ctx context.Context, pool *pgxpool.Pool, state recordauthority.VerifiedComposeState, maintenance *SourceMaintenanceConfig, directory string, opened bool) (composeBirthLaunchPair, error) {
			return inspectPublishedComposeBirthLaunch(ctx, pool, state, maintenance, directory, opened, normalComposeExecutablePaths())
		},
		heartbeatAuthority: heartbeatComposeAuthority,
		admitRuntime:       migrate.AdmitAppACLCurrentRuntime,
		probeRuntime:       probeComposeRuntimeLaunch,
		withMaintenance: func(ctx context.Context, pool *pgxpool.Pool, fn func(*SourceMaintenanceConfig) error) error {
			return withComposeManagementGuard(ctx, pool, SourceMaintenanceConfigPath, OrdinaryComposeGuardRoot, fn)
		},
		verifyCredentials: VerifySourceMaintenanceCredentials,
	}
}

// Source-configured operations keep their existing source guard. When the
// source control file is absent, acquire the ordinary guard before inspecting
// source admission and retain it through the complete operation.
func withComposeManagementGuard(ctx context.Context, pool *pgxpool.Pool, sourcePath, ordinaryRoot string, fn func(*SourceMaintenanceConfig) error) error {
	_, err := LoadSourceMaintenanceConfig(sourcePath)
	if errors.Is(err, os.ErrNotExist) {
		return WithOrdinaryComposeGuard(ctx, ordinaryRoot, false, func() error {
			return WithSourceMaintenance(ctx, pool, sourcePath, false, fn)
		})
	}
	return WithSourceMaintenance(ctx, pool, sourcePath, false, func(maintenance *SourceMaintenanceConfig) error {
		if maintenance != nil {
			return fn(maintenance)
		}
		return WithOrdinaryComposeGuard(ctx, ordinaryRoot, false, func() error { return fn(nil) })
	})
}

func initializeComposeWithDependencies(ctx context.Context, config ComposeInitConfig, deps composeInitDependencies) error {
	if err := config.Validate(); err != nil {
		return err
	}
	if deps.openPostgres == nil || deps.closePostgres == nil || deps.provisionBootstrap == nil ||
		deps.prepareAuthority == nil || deps.convergeCurrent == nil || deps.activateAuthority == nil ||
		deps.publishAuthority == nil || deps.prepareBirthLaunch == nil || deps.inspectBirthLaunch == nil ||
		deps.heartbeatAuthority == nil || deps.admitRuntime == nil || deps.probeRuntime == nil ||
		deps.withMaintenance == nil || deps.verifyCredentials == nil {
		return errors.New("Compose deployment initialization dependencies are invalid")
	}

	baseEndpoint := composePostgresEndpoint{
		Host:     config.DatabaseHost,
		Port:     config.DatabasePort,
		Database: config.DatabaseName,
	}
	bootstrapEndpoint := baseEndpoint
	bootstrapEndpoint.Role = config.BootstrapRole
	bootstrapEndpoint.Password = config.Passwords.Bootstrap
	bootstrapPool, err := deps.openPostgres(ctx, bootstrapEndpoint)
	if bootstrapPool != nil && err != nil {
		deps.closePostgres(bootstrapPool)
	}
	if err != nil || bootstrapPool == nil {
		return ErrComposeInitOpenBootstrap
	}
	defer deps.closePostgres(bootstrapPool)
	var operationErr error
	err = deps.withMaintenance(ctx, bootstrapPool, func(maintenance *SourceMaintenanceConfig) error {
		operationErr = initializeComposeGuarded(ctx, config, deps, baseEndpoint, bootstrapPool, maintenance)
		return operationErr
	})
	if operationErr != nil {
		return operationErr
	}
	if err != nil {
		return ErrComposeInitMaintenance
	}
	return nil
}

func initializeComposeGuarded(ctx context.Context, config ComposeInitConfig, deps composeInitDependencies, baseEndpoint composePostgresEndpoint, bootstrapPool *pgxpool.Pool, maintenance *SourceMaintenanceConfig) error {
	existing, stateErr := recordauthority.LoadComposeState(config.AuthorityStateRoot)
	if stateErr != nil && !errors.Is(stateErr, recordauthority.ErrComposeStateAbsent) {
		return ErrComposeInitPrepareAuthority
	}
	if stateErr == nil && deps.preflightAuthority != nil {
		if err := deps.preflightAuthority(ctx, bootstrapPool, existing, maintenance); errors.Is(err, ErrComposeColdUpgradeRequired) {
			return ErrComposeColdUpgradeRequired
		} else if err != nil {
			return ErrComposeInitPrepareAuthority
		}
	}
	passwords := platformmigrate.ComposeRolePasswords{
		Runtime:        config.Passwords.Runtime,
		PlatformAdmin:  config.Passwords.PlatformAdmin,
		DeletionWorker: config.Passwords.DeletionWorker,
		Migrator:       config.Passwords.Migrator,
	}
	if stateErr == nil {
		passwords.Authority = existing.DatabasePassword()
	}
	if err := deps.verifyCredentials(ctx, bootstrapPool, maintenance, config.Roles, config.AuthorityRole, passwords); err != nil {
		return ErrComposeInitMaintenance
	}
	authorityState, err := deps.prepareAuthority(ctx, bootstrapPool, config.AuthorityStateRoot, maintenance)
	if errors.Is(err, ErrComposeColdUpgradeRequired) {
		return ErrComposeColdUpgradeRequired
	}
	if err != nil {
		return ErrComposeInitPrepareAuthority
	}
	if !distinctComposePasswords(
		config.Passwords.Bootstrap,
		config.Passwords.Runtime,
		config.Passwords.PlatformAdmin,
		config.Passwords.DeletionWorker,
		config.Passwords.Migrator,
		authorityState.DatabasePassword(),
	) {
		return ErrComposeInitPrepareAuthority
	}
	bootstrapConfig := platformmigrate.ComposeBootstrapConfig{
		DatabaseName:  config.DatabaseName,
		BootstrapRole: config.BootstrapRole,
		AuthorityRole: config.AuthorityRole,
		Roles:         config.Roles,
		Passwords: platformmigrate.ComposeRolePasswords{
			Runtime:        config.Passwords.Runtime,
			PlatformAdmin:  config.Passwords.PlatformAdmin,
			DeletionWorker: config.Passwords.DeletionWorker,
			Migrator:       config.Passwords.Migrator,
			Authority:      authorityState.DatabasePassword(),
		},
	}
	if err := deps.verifyCredentials(ctx, bootstrapPool, maintenance, config.Roles, config.AuthorityRole, bootstrapConfig.Passwords); err != nil {
		return ErrComposeInitMaintenance
	}
	if err := deps.provisionBootstrap(ctx, bootstrapPool, bootstrapConfig); err != nil {
		return ErrComposeInitProvisionBootstrap
	}

	migratorEndpoint := baseEndpoint
	migratorEndpoint.Role = config.Roles.Migrator
	migratorEndpoint.Password = config.Passwords.Migrator
	migratorPool, err := deps.openPostgres(ctx, migratorEndpoint)
	if migratorPool != nil && err != nil {
		deps.closePostgres(migratorPool)
	}
	if err != nil || migratorPool == nil {
		return ErrComposeInitOpenMigrator
	}
	if err := deps.convergeCurrent(ctx, migratorPool, config.Roles.CenterRuntime, config.Roles.PlatformAdmin, config.Roles.DeletionWorker); err != nil {
		deps.closePostgres(migratorPool)
		return ErrComposeInitConvergeCurrent
	}
	if err := deps.activateAuthority(ctx, migratorPool, config.AuthorityStateRoot, authorityState, config.Roles); err != nil {
		deps.closePostgres(migratorPool)
		return ErrComposeInitActivateAuthority
	}
	launchDirectory := filepath.Dir(config.CenterDeploymentIDPath)
	launch, err := deps.prepareBirthLaunch(ctx, migratorPool, authorityState, maintenance, launchDirectory,
		config.Roles.CenterRuntime, config.Roles.DeletionWorker)
	if err != nil || launch.centerDigest == ([32]byte{}) {
		deps.closePostgres(migratorPool)
		return ErrComposeInitBirthLaunch
	}
	if err := deps.publishAuthority(config.AuthorityStateRoot, authorityState, config.CenterDeploymentIDPath); err != nil {
		deps.closePostgres(migratorPool)
		return ErrComposeInitPublishAuthority
	}
	deps.closePostgres(migratorPool)

	authorityEndpoint := baseEndpoint
	authorityEndpoint.Role = config.AuthorityRole
	authorityEndpoint.Password = authorityState.DatabasePassword()
	authorityPool, err := deps.openPostgres(ctx, authorityEndpoint)
	if authorityPool != nil && err != nil {
		deps.closePostgres(authorityPool)
	}
	if err != nil || authorityPool == nil {
		return ErrComposeInitOpenAuthority
	}
	defer deps.closePostgres(authorityPool)

	currentAuthority, err := recordauthority.LoadComposeState(config.AuthorityStateRoot)
	if err != nil || !sameVerifiedComposeAuthorityState(currentAuthority, authorityState) {
		return ErrComposeInitPrepareAuthority
	}
	if currentAuthority.MembershipVersion == 2 &&
		(recordauthority.VerifyActivationReceipt(config.AuthorityStateRoot, currentAuthority) != nil ||
			recordauthority.VerifyMembershipRotationReceipt(config.AuthorityStateRoot, currentAuthority) != nil) {
		return ErrComposeInitPrepareAuthority
	}
	verified, err := deps.inspectBirthLaunch(ctx, authorityPool, currentAuthority, maintenance, launchDirectory, true)
	if err != nil || verified.revision != launch.revision || !bytes.Equal(verified.centerBytes, launch.centerBytes) ||
		!bytes.Equal(verified.processorBytes, launch.processorBytes) || verified.centerDigest != launch.centerDigest || verified.processorDigest != launch.processorDigest {
		return ErrComposeInitBirthLaunch
	}
	if err := deps.heartbeatAuthority(ctx, authorityPool, currentAuthority); err != nil {
		return ErrComposeInitHeartbeatAuthority
	}

	runtimeEndpoint := baseEndpoint
	runtimeEndpoint.Role = config.Roles.CenterRuntime
	runtimeEndpoint.Password = config.Passwords.Runtime
	runtimeEndpoint.LaunchDigest = launch.centerDigest
	runtimePool, err := deps.openPostgres(ctx, runtimeEndpoint)
	if runtimePool != nil && err != nil {
		deps.closePostgres(runtimePool)
	}
	if err != nil || runtimePool == nil {
		return ErrComposeInitOpenRuntime
	}
	if err := deps.admitRuntime(ctx, runtimePool); err != nil {
		deps.closePostgres(runtimePool)
		return ErrComposeInitAdmitRuntime
	}
	if err := deps.probeRuntime(ctx, runtimePool, launch.center, maintenance); err != nil {
		deps.closePostgres(runtimePool)
		return ErrComposeInitAdmitRuntime
	}
	deps.closePostgres(runtimePool)

	return nil
}

func publishComposeAuthorityDeploymentID(root string, state recordauthority.VerifiedComposeState, destination string) error {
	if !validComposeAuthorityStateRoot(root) || !validComposeCenterDeploymentIDPath(destination) {
		return errors.New("Compose Center deployment identity paths are invalid")
	}
	canonical, err := recordauthority.LoadComposeState(root)
	if err != nil || !sameVerifiedComposeAuthorityState(canonical, state) {
		return errors.New("Compose Center deployment identity state verification failed")
	}
	parent := filepath.Dir(destination)
	parentInfo, err := os.Lstat(parent)
	if err != nil || !parentInfo.IsDir() || parentInfo.Mode().Perm() != 0o755 {
		return errors.New("Compose Center deployment identity directory is invalid")
	}
	body := []byte(string(canonical.DeploymentID) + "\n")
	if info, statErr := os.Lstat(destination); statErr == nil && info.Mode().IsRegular() && info.Mode().Perm() == 0o644 {
		file, openErr := os.Open(destination)
		if openErr == nil {
			existing, readErr := io.ReadAll(io.LimitReader(file, int64(len(body)+1)))
			closeErr := file.Close()
			if readErr == nil && closeErr == nil && bytes.Equal(existing, body) {
				return nil
			}
		}
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return errors.New("Compose Center deployment identity destination is invalid")
	}
	temporary, err := os.CreateTemp(parent, ".deployment-id.tmp-")
	if err != nil {
		return errors.New("create temporary Compose Center deployment identity failed")
	}
	temporaryPath := temporary.Name()
	published := false
	defer func() {
		_ = temporary.Close()
		if !published {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o644); err != nil {
		return errors.New("secure temporary Compose Center deployment identity failed")
	}
	if _, err := temporary.Write(body); err != nil {
		return errors.New("write temporary Compose Center deployment identity failed")
	}
	if err := temporary.Sync(); err != nil {
		return errors.New("sync temporary Compose Center deployment identity failed")
	}
	if err := temporary.Close(); err != nil {
		return errors.New("close temporary Compose Center deployment identity failed")
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return errors.New("publish Compose Center deployment identity failed")
	}
	published = true
	directory, err := os.Open(parent)
	if err != nil {
		return errors.New("open Compose Center deployment identity directory failed")
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil || closeErr != nil {
		return errors.New("sync Compose Center deployment identity directory failed")
	}
	return nil
}

type composeDatabaseContractState struct {
	Active       bool
	DeploymentID recordplatform.DeploymentID
}

func preflightComposeAuthorityState(ctx context.Context, pool *pgxpool.Pool, state recordauthority.VerifiedComposeState, maintenance *SourceMaintenanceConfig) error {
	if maintenance != nil {
		if state.MembershipVersion != 1 {
			return errors.New("source-configured Compose V2 is unsupported")
		}
		return nil
	}
	if state.MembershipVersion != 1 {
		return nil
	}
	contract, err := readComposeDatabaseContractState(ctx, pool)
	if err != nil {
		return err
	}
	if contract.Active {
		return ErrComposeColdUpgradeRequired
	}
	return nil
}

func prepareComposeAuthorityState(ctx context.Context, pool *pgxpool.Pool, root string, maintenance *SourceMaintenanceConfig) (recordauthority.VerifiedComposeState, error) {
	contract, err := readComposeDatabaseContractState(ctx, pool)
	if err != nil {
		return recordauthority.VerifiedComposeState{}, err
	}
	existing, loadErr := recordauthority.LoadComposeState(root)
	if loadErr == nil {
		if maintenance == nil && contract.Active && existing.MembershipVersion == 1 {
			return recordauthority.VerifiedComposeState{}, ErrComposeColdUpgradeRequired
		}
		if maintenance != nil && existing.MembershipVersion != 1 {
			return recordauthority.VerifiedComposeState{}, errors.New("source-configured Compose V2 is unsupported")
		}
	} else if !errors.Is(loadErr, recordauthority.ErrComposeStateAbsent) {
		return recordauthority.VerifiedComposeState{}, loadErr
	}
	state, err := reconcileComposeAuthorityState(root, contract)
	if err != nil || maintenance != nil {
		return state, err
	}
	if state.MembershipVersion == 2 {
		return state, nil
	}
	return recordauthority.PrepareComposeMembershipV2(root)
}

func reconcileComposeAuthorityState(root string, contract composeDatabaseContractState) (recordauthority.VerifiedComposeState, error) {
	state, err := recordauthority.LoadComposeState(root)
	if err == nil {
		if contract.Active && contract.DeploymentID != state.DeploymentID {
			return recordauthority.VerifiedComposeState{}, errors.New("Compose Records authority state does not match active database")
		}
		return state, nil
	}
	if !errors.Is(err, recordauthority.ErrComposeStateAbsent) {
		return recordauthority.VerifiedComposeState{}, errors.New("Compose Records authority state is not valid")
	}
	if contract.Active {
		return recordauthority.VerifiedComposeState{}, errors.New("Compose Records authority state is absent for active database")
	}
	state, err = recordauthority.CreateComposeState(root)
	if err != nil {
		return recordauthority.VerifiedComposeState{}, errors.New("create Compose Records authority state failed")
	}
	return state, nil
}

func readComposeDatabaseContractState(ctx context.Context, pool *pgxpool.Pool) (composeDatabaseContractState, error) {
	if pool == nil {
		return composeDatabaseContractState{}, errors.New("Compose Records authority has no bootstrap PostgreSQL pool")
	}
	var present bool
	if err := pool.QueryRow(ctx, `select pg_catalog.to_regclass('public.deployment_contract_state') is not null`).Scan(&present); err != nil {
		return composeDatabaseContractState{}, fmt.Errorf("inspect Compose Records database contract table: %w", err)
	}
	if !present {
		return composeDatabaseContractState{}, nil
	}
	var rowCount, defaultCount int64
	var deploymentID *string
	if err := pool.QueryRow(ctx, `
		select pg_catalog.count(*)::bigint,
		       pg_catalog.count(*) filter (where project_id = 'default')::bigint,
		       pg_catalog.min(deployment_id) filter (where project_id = 'default')
		from public.deployment_contract_state
	`).Scan(&rowCount, &defaultCount, &deploymentID); err != nil {
		return composeDatabaseContractState{}, fmt.Errorf("inspect Compose Records database contract singleton: %w", err)
	}
	if rowCount != 1 || defaultCount != 1 {
		return composeDatabaseContractState{}, errors.New("Compose Records database contract singleton is invalid")
	}
	if deploymentID == nil {
		return composeDatabaseContractState{}, nil
	}
	validated := recordplatform.DeploymentID(*deploymentID)
	if err := recordplatform.ValidateDeploymentID(validated); err != nil {
		return composeDatabaseContractState{}, errors.New("Compose Records database contract deployment is invalid")
	}
	return composeDatabaseContractState{Active: true, DeploymentID: validated}, nil
}

func activateComposeAuthorityState(ctx context.Context, pool *pgxpool.Pool, root string, state recordauthority.VerifiedComposeState, roles platformmigrate.AppRoleSetV2) error {
	if pool == nil {
		return errors.New("Compose Records activation has no migrator PostgreSQL pool")
	}
	command, err := state.ActivationCommand.MarshalBinary()
	if err != nil {
		return errors.New("Compose Records activation command is invalid")
	}
	if state.MembershipVersion == 2 {
		canonical, err := recordauthority.LoadComposeState(root)
		if err != nil || !sameVerifiedComposeAuthorityState(canonical, state) || canonical.MembershipRotation == nil {
			return errors.New("Compose Records membership state is invalid")
		}
		for _, verify := range []func(string, recordauthority.VerifiedComposeState) error{recordauthority.VerifyActivationReceipt, recordauthority.VerifyMembershipRotationReceipt} {
			if err := verify(root, canonical); err != nil && !errors.Is(err, recordauthority.ErrComposeActivationReceiptAbsent) && !errors.Is(err, recordauthority.ErrComposeMembershipRotationReceiptAbsent) {
				return errors.New("Compose Records membership receipt is invalid")
			}
		}
		rotation, err := canonical.MembershipRotation.MarshalBinary()
		if err != nil {
			return errors.New("Compose Records membership rotation is invalid")
		}
		receipt, err := migrate.ProjectComposeMembershipV2(ctx, pool, roles, command, rotation)
		if err != nil {
			return errors.New("project Compose Records membership failed")
		}
		genesisReceipt := recordplatform.ProjectionCASReceiptDigestV1(command)
		if err := recordauthority.PersistActivationReceipt(root, canonical, genesisReceipt[:]); err != nil {
			return errors.New("persist Compose Records activation receipt failed")
		}
		if err := recordauthority.PersistMembershipRotationReceipt(root, canonical, receipt); err != nil {
			return errors.New("persist Compose Records membership receipt failed")
		}
		return nil
	}
	var receipt []byte
	if err := pool.QueryRow(ctx, `select public.record_platform_cas_contract_activation_projection($1::bytea)`, command).Scan(&receipt); err != nil {
		return fmt.Errorf("project Compose Records activation: %w", err)
	}
	if err := recordauthority.PersistActivationReceipt(root, state, receipt); err != nil {
		return errors.New("persist Compose Records activation receipt failed")
	}
	return nil
}

func heartbeatComposeAuthority(ctx context.Context, pool *pgxpool.Pool, state recordauthority.VerifiedComposeState) error {
	_, err := heartbeatComposeAuthorityAt(ctx, pool, state, time.Now().UTC())
	return err
}

func heartbeatComposeAuthorityAt(
	ctx context.Context,
	pool *pgxpool.Pool,
	state recordauthority.VerifiedComposeState,
	issuedAt time.Time,
) (time.Time, error) {
	if pool == nil {
		return time.Time{}, errors.New("Compose Records authority heartbeat has no PostgreSQL pool")
	}
	var command []byte
	var wantExpiry time.Time
	var err error
	query := `select public.record_platform_compose_membership_heartbeat($1::bytea)`
	if state.MembershipVersion == 2 {
		command, wantExpiry, err = recordauthority.MarshalMembershipHeartbeatCommandV2(state, issuedAt)
		query = `select public.record_platform_compose_membership_heartbeat_v2($1::bytea)`
	} else {
		command, wantExpiry, err = recordauthority.MarshalMembershipHeartbeatCommandV1(state, issuedAt)
	}
	if err != nil {
		return time.Time{}, errors.New("Compose Records authority heartbeat command is invalid")
	}
	var gotExpiry time.Time
	if err := pool.QueryRow(ctx, query, command).Scan(&gotExpiry); err != nil {
		return time.Time{}, fmt.Errorf("renew Compose Records authority membership: %w", err)
	}
	if !gotExpiry.UTC().Equal(wantExpiry) {
		return time.Time{}, errors.New("Compose Records authority heartbeat receipt is invalid")
	}
	return wantExpiry, nil
}

func openComposePostgres(ctx context.Context, endpoint composePostgresEndpoint) (*pgxpool.Pool, error) {
	connectionURL := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(endpoint.Role, endpoint.Password),
		Host:   net.JoinHostPort(endpoint.Host, strconv.FormatUint(uint64(endpoint.Port), 10)),
		Path:   "/" + endpoint.Database,
	}
	query := connectionURL.Query()
	query.Set("sslmode", "disable")
	connectionURL.RawQuery = query.Encode()
	databaseURL := connectionURL.String()
	if endpoint.LaunchDigest != ([32]byte{}) {
		var err error
		databaseURL, err = recordbirth.WithRuntimeLaunch(databaseURL, endpoint.LaunchDigest)
		if err != nil {
			return nil, err
		}
	}
	return store.OpenPostgres(ctx, databaseURL)
}

func probeComposeRuntimeLaunch(ctx context.Context, pool *pgxpool.Pool, launch recordbirth.LaunchBinding, maintenance *SourceMaintenanceConfig) error {
	if pool == nil || launch.ProcessKind != "center" || launch.SourceGeneration > uint64(^uint64(0)>>1) {
		return errComposeBirthLaunch
	}
	body, err := recordbirth.EncodeLaunchBinding(launch)
	if err != nil {
		return errComposeBirthLaunch
	}
	digest := sha256.Sum256(body)
	var claimed string
	if err := pool.QueryRow(ctx, `select coalesce(current_setting('houfeng.birth_launch_digest',true),'')`).Scan(&claimed); err != nil || claimed != hex.EncodeToString(digest[:]) {
		return errComposeBirthLaunch
	}
	var reader *recordbirth.PostgresAuthority
	if launch.BirthInstalled {
		readerPool, observed, err := openComposeBirthContinuityReader(ctx, pool, maintenance)
		if err != nil {
			return errComposeBirthLaunch
		}
		defer readerPool.Close()
		reader = observed
	}
	if err := recordbirth.VerifyRuntimeLaunch(ctx, pool, reader, launch); err != nil {
		return errComposeBirthLaunch
	}
	var admitted int64
	if err := pool.QueryRow(ctx, `select public.source_application_admit()`).Scan(&admitted); err != nil || admitted < 0 || uint64(admitted) != launch.SourceGeneration {
		return errComposeBirthLaunch
	}
	return nil
}
