package deploy

import (
	"bytes"
	"context"
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
	ErrComposeInitOpenAuthority      = errors.New("open Compose Records authority PostgreSQL connection failed")
	ErrComposeInitHeartbeatAuthority = errors.New("establish Compose Records authority membership failed")
	ErrComposeInitOpenRuntime        = errors.New("open Compose runtime PostgreSQL connection failed")
	ErrComposeInitAdmitRuntime       = errors.New("admit current Compose runtime database failed")
)

type ComposeInitPasswords struct {
	Bootstrap     string
	Runtime       string
	PlatformAdmin string
	Migrator      string
}

type ComposeInitConfig struct {
	DatabaseHost           string
	DatabasePort           uint16
	DatabaseName           string
	BootstrapRole          string
	AuthorityRole          string
	AuthorityStateRoot     string
	CenterDeploymentIDPath string
	Roles                  platformmigrate.AppRoleSetV1
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
		Roles: platformmigrate.AppRoleSetV1{
			CenterRuntime: composeRuntimeRole,
			PlatformAdmin: composeAdminRole,
			Migrator:      composeMigratorRole,
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
		config.Passwords.Migrator,
	) {
		return ErrInvalidComposeInitConfig
	}
	authorityValidationPassword := "state-derived-placeholder"
	for !distinctComposePasswords(
		config.Passwords.Runtime,
		config.Passwords.PlatformAdmin,
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
			Runtime:       config.Passwords.Runtime,
			PlatformAdmin: config.Passwords.PlatformAdmin,
			Migrator:      config.Passwords.Migrator,
			Authority:     authorityValidationPassword,
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
	Host     string
	Port     uint16
	Database string
	Role     string
	Password string
}

type composeInitDependencies struct {
	openPostgres       func(context.Context, composePostgresEndpoint) (*pgxpool.Pool, error)
	closePostgres      func(*pgxpool.Pool)
	prepareAuthority   func(context.Context, *pgxpool.Pool, string) (recordauthority.VerifiedComposeState, error)
	provisionBootstrap func(context.Context, *pgxpool.Pool, platformmigrate.ComposeBootstrapConfig) error
	convergeCurrent    func(context.Context, *pgxpool.Pool, string, string) error
	activateAuthority  func(context.Context, *pgxpool.Pool, string, recordauthority.VerifiedComposeState) error
	publishAuthority   func(string, recordauthority.VerifiedComposeState, string) error
	heartbeatAuthority func(context.Context, *pgxpool.Pool, recordauthority.VerifiedComposeState) error
	admitRuntime       func(context.Context, *pgxpool.Pool) error
}

// InitializeCompose provisions the fixed production Compose database contract.
func InitializeCompose(ctx context.Context, config ComposeInitConfig) error {
	return initializeComposeWithDependencies(ctx, config, composeInitDependencies{
		openPostgres:       openComposePostgres,
		closePostgres:      func(pool *pgxpool.Pool) { pool.Close() },
		prepareAuthority:   prepareComposeAuthorityState,
		provisionBootstrap: platformmigrate.ProvisionComposeBootstrap,
		convergeCurrent: func(ctx context.Context, pool *pgxpool.Pool, runtimeRole, adminRole string) error {
			_, err := migrate.ConvergeAppACLCurrent(ctx, pool, runtimeRole, adminRole)
			return err
		},
		activateAuthority:  activateComposeAuthorityState,
		publishAuthority:   publishComposeAuthorityDeploymentID,
		heartbeatAuthority: heartbeatComposeAuthority,
		admitRuntime:       migrate.AdmitAppACLCurrentRuntime,
	})
}

func initializeComposeWithDependencies(ctx context.Context, config ComposeInitConfig, deps composeInitDependencies) error {
	if err := config.Validate(); err != nil {
		return err
	}
	if deps.openPostgres == nil || deps.closePostgres == nil || deps.provisionBootstrap == nil ||
		deps.prepareAuthority == nil || deps.convergeCurrent == nil || deps.activateAuthority == nil ||
		deps.publishAuthority == nil || deps.heartbeatAuthority == nil || deps.admitRuntime == nil {
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
	authorityState, err := deps.prepareAuthority(ctx, bootstrapPool, config.AuthorityStateRoot)
	if err != nil {
		deps.closePostgres(bootstrapPool)
		return ErrComposeInitPrepareAuthority
	}
	if !distinctComposePasswords(
		config.Passwords.Bootstrap,
		config.Passwords.Runtime,
		config.Passwords.PlatformAdmin,
		config.Passwords.Migrator,
		authorityState.DatabasePassword(),
	) {
		deps.closePostgres(bootstrapPool)
		return ErrComposeInitPrepareAuthority
	}
	bootstrapConfig := platformmigrate.ComposeBootstrapConfig{
		DatabaseName:  config.DatabaseName,
		BootstrapRole: config.BootstrapRole,
		AuthorityRole: config.AuthorityRole,
		Roles:         config.Roles,
		Passwords: platformmigrate.ComposeRolePasswords{
			Runtime:       config.Passwords.Runtime,
			PlatformAdmin: config.Passwords.PlatformAdmin,
			Migrator:      config.Passwords.Migrator,
			Authority:     authorityState.DatabasePassword(),
		},
	}
	if err := deps.provisionBootstrap(ctx, bootstrapPool, bootstrapConfig); err != nil {
		deps.closePostgres(bootstrapPool)
		return ErrComposeInitProvisionBootstrap
	}
	deps.closePostgres(bootstrapPool)

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
	if err := deps.convergeCurrent(ctx, migratorPool, config.Roles.CenterRuntime, config.Roles.PlatformAdmin); err != nil {
		deps.closePostgres(migratorPool)
		return ErrComposeInitConvergeCurrent
	}
	if err := deps.activateAuthority(ctx, migratorPool, config.AuthorityStateRoot, authorityState); err != nil {
		deps.closePostgres(migratorPool)
		return ErrComposeInitActivateAuthority
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
	if err := deps.heartbeatAuthority(ctx, authorityPool, authorityState); err != nil {
		deps.closePostgres(authorityPool)
		return ErrComposeInitHeartbeatAuthority
	}
	deps.closePostgres(authorityPool)

	runtimeEndpoint := baseEndpoint
	runtimeEndpoint.Role = config.Roles.CenterRuntime
	runtimeEndpoint.Password = config.Passwords.Runtime
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

func prepareComposeAuthorityState(ctx context.Context, pool *pgxpool.Pool, root string) (recordauthority.VerifiedComposeState, error) {
	contract, err := readComposeDatabaseContractState(ctx, pool)
	if err != nil {
		return recordauthority.VerifiedComposeState{}, err
	}
	return reconcileComposeAuthorityState(root, contract)
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

func activateComposeAuthorityState(ctx context.Context, pool *pgxpool.Pool, root string, state recordauthority.VerifiedComposeState) error {
	if pool == nil {
		return errors.New("Compose Records activation has no migrator PostgreSQL pool")
	}
	command, err := state.ActivationCommand.MarshalBinary()
	if err != nil {
		return errors.New("Compose Records activation command is invalid")
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
	command, wantExpiry, err := recordauthority.MarshalMembershipHeartbeatCommandV1(state, issuedAt)
	if err != nil {
		return time.Time{}, errors.New("Compose Records authority heartbeat command is invalid")
	}
	var gotExpiry time.Time
	if err := pool.QueryRow(ctx, `select public.record_platform_compose_membership_heartbeat($1::bytea)`, command).Scan(&gotExpiry); err != nil {
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
	return store.OpenPostgres(ctx, connectionURL.String())
}
