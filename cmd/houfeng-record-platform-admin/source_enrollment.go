package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sys/unix"

	"houfeng/internal/center/deploy"
	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/sourceenrollment"
	"houfeng/internal/center/sourceretirement"
	"houfeng/internal/center/store"
)

const (
	sourceAppBootstrapDatabaseURLEnv       = "HOUFENG_SOURCE_APP_BOOTSTRAP_DATABASE_URL"
	sourceWitnessProvisionerDatabaseURLEnv = "HOUFENG_SOURCE_WITNESS_PROVISIONER_DATABASE_URL"
	sourceMaintenanceConfigEnv             = "HOUFENG_SOURCE_MAINTENANCE_CONFIG"
)

var (
	errSourceEnrollmentInput      = errors.New("source enrollment request or configuration is invalid")
	errSourceEnrollmentOperation  = errors.New("source enrollment failed; external offline fencing of all old writers and endpoints is required before enrollment and through activation; resolve uncertain results before resuming")
	errSourceEnrollmentOutput     = errors.New("source enrollment output failed; resolve uncertain results before resuming")
	errSourceMaintenanceProvision = errors.New("source maintenance provisioning failed")
)

type sourceEnrollmentWitness struct {
	ID               string `json:"id"`
	SystemIdentifier string `json:"system_identifier"`
	DatabaseOID      uint32 `json:"database_oid"`
	DatabaseName     string `json:"database_name"`
	Epoch            uint64 `json:"epoch"`
}

func (w sourceEnrollmentWitness) domain() sourceretirement.DomainIdentity {
	return sourceretirement.DomainIdentity{ID: w.ID, SystemIdentifier: w.SystemIdentifier, DatabaseOID: w.DatabaseOID, DatabaseName: w.DatabaseName, Epoch: w.Epoch}
}

type sourceMaintenanceProvisionRequest struct {
	ConfigPath        string                  `json:"config_path"`
	Witness           sourceEnrollmentWitness `json:"witness"`
	ReaderDatabaseURL string                  `json:"reader_database_url"`
}

type sourceEnrollmentRoles struct {
	CenterRuntime  string `json:"center_runtime"`
	PlatformAdmin  string `json:"platform_admin"`
	DeletionWorker string `json:"deletion_worker"`
	Migrator       string `json:"migrator"`
}

type sourceEnrollmentPasswords struct {
	Runtime        string `json:"runtime"`
	PlatformAdmin  string `json:"platform_admin"`
	DeletionWorker string `json:"deletion_worker"`
	Migrator       string `json:"migrator"`
	Authority      string `json:"authority"`
}

type sourceEnrollmentProducer struct {
	Principal string `json:"principal"`
	Password  string `json:"password"`
}

type sourceEnrollmentConfig struct {
	DeploymentID  string                    `json:"deployment_id"`
	Witness       sourceEnrollmentWitness   `json:"witness"`
	Roles         sourceEnrollmentRoles     `json:"roles"`
	AuthorityRole string                    `json:"authority_role"`
	Passwords     sourceEnrollmentPasswords `json:"passwords"`
	Producer      sourceEnrollmentProducer  `json:"producer"`
}

type sourceEnrollmentMembership struct {
	InstanceID   string `json:"instance_id"`
	DeploymentID string `json:"deployment_id"`
	ProjectID    string `json:"project_id"`
	InstanceKind string `json:"instance_kind"`
	Capability   string `json:"capability"`
}

type sourceEnrollmentRequest struct {
	ConfigPath string                     `json:"config_path"`
	Config     sourceEnrollmentConfig     `json:"config"`
	Membership sourceEnrollmentMembership `json:"membership"`
}

type sourceComposeFenceRequest struct {
	RecoveryID          string `json:"recovery_id"`
	Project             string `json:"project"`
	WorkingDirectory    string `json:"working_directory"`
	DatabaseContainerID string `json:"database_container_id"`
	ControlConfigPath   string `json:"control_config_path"`
}

type sourceComposeEnrollmentRequest struct {
	ConfigPath  string                     `json:"config_path"`
	Config      sourceEnrollmentConfig     `json:"config"`
	Membership  sourceEnrollmentMembership `json:"membership"`
	Application sourceEnrollmentWitness    `json:"application"`
	Fence       sourceComposeFenceRequest  `json:"fence"`
}

func (c sourceEnrollmentConfig) enrollmentConfig() sourceenrollment.Config {
	return sourceenrollment.Config{
		DeploymentID: c.DeploymentID, Witness: c.Witness.domain(), AuthorityRole: c.AuthorityRole,
		Roles:     platformmigrate.AppRoleSetV2{CenterRuntime: c.Roles.CenterRuntime, PlatformAdmin: c.Roles.PlatformAdmin, DeletionWorker: c.Roles.DeletionWorker, Migrator: c.Roles.Migrator},
		Passwords: platformmigrate.ComposeRolePasswords{Runtime: c.Passwords.Runtime, PlatformAdmin: c.Passwords.PlatformAdmin, DeletionWorker: c.Passwords.DeletionWorker, Migrator: c.Passwords.Migrator, Authority: c.Passwords.Authority},
		Producer:  platformmigrate.SourceRoleCredential{Principal: c.Producer.Principal, Password: c.Producer.Password},
	}
}

// Each object has an exact, case-sensitive schema, including nested objects.
// nil denotes a scalar, not an optional member.
type sourceRequestSchema map[string]sourceRequestSchema

var sourceWitnessSchema = sourceRequestSchema{"id": nil, "system_identifier": nil, "database_oid": nil, "database_name": nil, "epoch": nil}
var sourceProvisionSchema = sourceRequestSchema{"config_path": nil, "witness": sourceWitnessSchema, "reader_database_url": nil}
var sourceEnrollSchema = sourceRequestSchema{
	"config_path": nil,
	"config": {
		"deployment_id": nil, "witness": sourceWitnessSchema,
		"roles":          {"center_runtime": nil, "platform_admin": nil, "deletion_worker": nil, "migrator": nil},
		"authority_role": nil,
		"passwords":      {"runtime": nil, "platform_admin": nil, "deletion_worker": nil, "migrator": nil, "authority": nil},
		"producer":       {"principal": nil, "password": nil},
	},
	"membership": {"instance_id": nil, "deployment_id": nil, "project_id": nil, "instance_kind": nil, "capability": nil},
}

var sourceComposeEnrollSchema = sourceRequestSchema{
	"config_path": nil,
	"config":      sourceEnrollSchema["config"],
	"membership":  sourceEnrollSchema["membership"],
	"application": sourceWitnessSchema,
	"fence":       {"recovery_id": nil, "project": nil, "working_directory": nil, "database_container_id": nil, "control_config_path": nil},
}

func checkSourceRequestObject(body []byte, schema sourceRequestSchema) error {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || len(fields) != len(schema) {
		return errSourceEnrollmentInput
	}
	for name, child := range schema {
		value, ok := fields[name]
		if !ok || strings.TrimSpace(string(value)) == "null" {
			return errSourceEnrollmentInput
		}
		if child != nil && checkSourceRequestObject(value, child) != nil {
			return errSourceEnrollmentInput
		}
	}
	return nil
}

func decodeSourceEnrollmentRequest(body []byte, destination any, schema sourceRequestSchema) error {
	// The shared scanner rejects duplicate/case-folded aliases recursively and
	// trailing data; the schema additionally requires exact nested spelling.
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if scanSourceRetirementJSONValue(decoder, 0) != nil {
		return errSourceEnrollmentInput
	}
	if _, err := decoder.Token(); err != io.EOF || checkSourceRequestObject(body, schema) != nil {
		return errSourceEnrollmentInput
	}
	decoder = json.NewDecoder(strings.NewReader(string(body)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(destination) != nil {
		return errSourceEnrollmentInput
	}
	return nil
}

// Credential-bearing requests are private owned regular files, opened without
// following the final symlink and checked again against the opened descriptor.
func readSourceEnrollmentRequest(path string) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil || !sourceEnrollmentPrivateFile(before) || before.Size() > sourceRetirementFileLimit {
		return nil, errSourceEnrollmentInput
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, errSourceEnrollmentInput
	}
	file := os.NewFile(uintptr(fd), "source-enrollment-request")
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !sourceEnrollmentPrivateFile(opened) || !os.SameFile(before, opened) || opened.Size() > sourceRetirementFileLimit {
		return nil, errSourceEnrollmentInput
	}
	body, err := io.ReadAll(io.LimitReader(file, sourceRetirementFileLimit+1))
	if err != nil || len(body) > sourceRetirementFileLimit {
		return nil, errSourceEnrollmentInput
	}
	after, err := file.Stat()
	if err != nil || !sourceEnrollmentPrivateFile(after) || !os.SameFile(opened, after) || after.Size() != opened.Size() || !after.ModTime().Equal(opened.ModTime()) {
		return nil, errSourceEnrollmentInput
	}
	return body, nil
}

func sourceEnrollmentPrivateFile(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && info.Mode().IsRegular() && info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) == 0 &&
		(info.Mode().Perm() == 0o600 || info.Mode().Perm() == 0o400) && stat.Uid == uint32(os.Geteuid())
}

func sourceMaintenanceConfigPath() string {
	if value := os.Getenv(sourceMaintenanceConfigEnv); value != "" {
		return value
	}
	return deploy.SourceMaintenanceConfigPath
}

// enroll-offline is a low-level trusted operator entrypoint: its caller must
// externally stop and isolate all old writers and endpoints through activation.
// enroll-compose fences the known Compose project, but still requires the
// operator to prevent unsupervised host writers and external restarts.
func runSourceEnrollmentCommand(ctx context.Context, args []string) error {
	if len(args) != 4 || args[0] != "source-authority" || args[2] != "--request-file" || args[3] == "" || (args[1] != "maintenance-provision" && args[1] != "enroll-offline" && args[1] != "enroll-compose") {
		return errSourceEnrollmentInput
	}
	body, err := readSourceEnrollmentRequest(args[3])
	if err != nil {
		return errSourceEnrollmentInput
	}
	if args[1] == "maintenance-provision" {
		var request sourceMaintenanceProvisionRequest
		if decodeSourceEnrollmentRequest(body, &request, sourceProvisionSchema) != nil {
			return errSourceEnrollmentInput
		}
		if _, err := platformmigrate.ParseSourceAuthorityPostgresConfig(request.ReaderDatabaseURL); err != nil {
			return errSourceEnrollmentInput
		}
		if _, err := deploy.ProvisionSourceMaintenanceConfig(request.ConfigPath, request.Witness.domain(), request.ReaderDatabaseURL); err != nil {
			return errSourceMaintenanceProvision
		}
		if json.NewEncoder(os.Stdout).Encode(struct {
			Success bool `json:"success"`
		}{true}) != nil {
			return errSourceEnrollmentOutput
		}
		return nil
	}
	if args[1] == "enroll-compose" {
		var request sourceComposeEnrollmentRequest
		if decodeSourceEnrollmentRequest(body, &request, sourceComposeEnrollSchema) != nil {
			return errSourceEnrollmentInput
		}
		return enrollSourceCompose(ctx, request)
	}
	var request sourceEnrollmentRequest
	if decodeSourceEnrollmentRequest(body, &request, sourceEnrollSchema) != nil {
		return errSourceEnrollmentInput
	}
	return enrollSourceOffline(ctx, request)
}

// enrollSourceOffline relies on the operator's external offline fence; neither
// the maintenance guard nor this command establishes endpoint isolation.
func enrollSourceOffline(ctx context.Context, request sourceEnrollmentRequest) error {
	expected, err := deploy.LoadSourceMaintenanceConfig(request.ConfigPath)
	if err != nil || expected.Witness != request.Config.Witness.domain() || request.Config.DeploymentID != request.Membership.DeploymentID {
		return errSourceEnrollmentOperation
	}
	gate, err := store.NewBootstrapDeploymentMembershipAdmissionGate(store.DeploymentMembershipIdentity{
		InstanceID: request.Membership.InstanceID, DeploymentID: recordplatform.DeploymentID(request.Membership.DeploymentID),
		ProjectID: recordplatform.ProjectID(request.Membership.ProjectID), InstanceKind: request.Membership.InstanceKind, Capability: request.Membership.Capability,
	})
	if err != nil {
		return errSourceEnrollmentInput
	}
	pools := make([]*pgxpool.Pool, 0, 3)
	defer func() {
		for _, pool := range pools {
			pool.Close()
		}
	}()
	for _, name := range []string{sourceAppBootstrapDatabaseURLEnv, sourceWitnessBootstrapDatabaseURLEnv, sourceWitnessProvisionerDatabaseURLEnv} {
		value, ok := os.LookupEnv(name)
		if !ok || strings.TrimSpace(value) == "" {
			return errSourceEnrollmentInput
		}
		config, err := platformmigrate.ParseSourceAuthorityPostgresConfig(strings.TrimSpace(value))
		if err != nil {
			return errSourceEnrollmentInput
		}
		pool, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			return errSourceEnrollmentOperation
		}
		pools = append(pools, pool)
	}
	if err := pools[0].Ping(ctx); err != nil {
		return errSourceEnrollmentOperation
	}
	authority, err := sourceretirement.NewAuthority(pools[2], expected.Witness)
	if err != nil {
		return errSourceEnrollmentOperation
	}
	var result sourceretirement.ApplicationEnrollment
	err = deploy.WithSourceMaintenance(ctx, pools[0], request.ConfigPath, true, func(current *deploy.SourceMaintenanceConfig) error {
		if current == nil || *current != expected {
			return errSourceEnrollmentOperation
		}
		var err error
		result, err = sourceenrollment.Enroll(ctx, pools[0], pools[1], authority, gate, request.Config.enrollmentConfig())
		return err
	})
	if err != nil {
		return errSourceEnrollmentOperation
	}
	if json.NewEncoder(os.Stdout).Encode(result) != nil {
		return errSourceEnrollmentOutput
	}
	return nil
}

var errSourceComposeEnrollment = errors.New("compose source enrollment failed; keep the project fenced and external host writers and restart controllers stopped; resolve uncertainty by resuming the same recovery ID")

// sourceComposeIdentityGate rechecks the requested physical identity inside
// each coordinator APP transaction, before any witness mutation or APP commit.
type sourceComposeIdentityGate struct {
	membership *store.BootstrapDeploymentMembershipAdmissionGate
	expected   sourceretirement.DomainIdentity
}

func sourceComposeCredentialsValid(config sourceenrollment.Config) bool {
	if config.Roles.Validate() != nil {
		return false
	}
	credentials := []platformmigrate.SourceRoleCredential{
		{Principal: config.Roles.CenterRuntime, Password: config.Passwords.Runtime},
		{Principal: config.Roles.PlatformAdmin, Password: config.Passwords.PlatformAdmin},
		{Principal: config.Roles.DeletionWorker, Password: config.Passwords.DeletionWorker},
		{Principal: config.Roles.Migrator, Password: config.Passwords.Migrator},
		{Principal: config.AuthorityRole, Password: config.Passwords.Authority},
		config.Producer,
	}
	seen := make(map[string]bool, len(credentials))
	for _, credential := range credentials {
		if credential.Principal == "" || credential.Password == "" || seen[credential.Principal] {
			return false
		}
		seen[credential.Principal] = true
	}
	return true
}

func (gate sourceComposeIdentityGate) Admit(ctx context.Context, tx pgx.Tx) error {
	if err := checkSourceComposeIdentity(ctx, tx, gate.expected); err != nil {
		return errSourceComposeEnrollment
	}
	return gate.membership.Admit(ctx, tx)
}

func checkSourceComposeIdentity(ctx context.Context, tx pgx.Tx, expected sourceretirement.DomainIdentity) error {
	actual, err := platformmigrate.ReadSourceApplicationIdentity(ctx, tx)
	if err != nil || actual.IdentityEpoch <= 0 ||
		(sourceretirement.DomainIdentity{
			ID: actual.ID, SystemIdentifier: actual.PostgresSystemIdentifier,
			DatabaseOID: actual.DatabaseOID, DatabaseName: actual.DatabaseName, Epoch: uint64(actual.IdentityEpoch),
		}) != expected {
		return errSourceComposeEnrollment
	}
	return nil
}

// sourceComposeAppConfig pins every pgx connection fallback to the isolated
// Docker address; a TLS fallback must never reconnect to the original endpoint.
func sourceComposeAppConfig(config *pgxpool.Config, address string) (*pgxpool.Config, error) {
	if config == nil || config.ConnConfig == nil || config.ConnConfig.User != "postgres" ||
		config.ConnConfig.Database != "houfeng" || net.ParseIP(address) == nil {
		return nil, errSourceComposeEnrollment
	}
	pinned := config.Copy()
	pinned.MaxConns = 1
	pinned.MinConns = 0
	pinned.ConnConfig.Host = address
	pinned.ConnConfig.Port = 5432
	for _, fallback := range pinned.ConnConfig.Fallbacks {
		fallback.Host = address
		fallback.Port = 5432
	}
	return pinned, nil
}

// sourceComposeDrain runs only after the physical identity has been confirmed.
// An isolated Compose DB can still have previously established client sessions;
// prepared transactions or any other database are not safely attributable here.
func sourceComposeDrain(ctx context.Context, conn *pgxpool.Conn) error {
	var otherDatabases, prepared int64
	if err := conn.QueryRow(ctx, `select count(*) from pg_catalog.pg_database
		where datname not in ('houfeng', 'postgres', 'template0', 'template1')`).Scan(&otherDatabases); err != nil || otherDatabases != 0 {
		return errSourceComposeEnrollment
	}
	if err := conn.QueryRow(ctx, `select count(*) from pg_catalog.pg_prepared_xacts`).Scan(&prepared); err != nil || prepared != 0 {
		return errSourceComposeEnrollment
	}
	var terminated bool
	if err := conn.QueryRow(ctx, `select coalesce(bool_and(pg_catalog.pg_terminate_backend(pid, 5000)), true)
		from pg_catalog.pg_stat_activity
		where backend_type = 'client backend' and pid <> pg_catalog.pg_backend_pid()`).Scan(&terminated); err != nil || !terminated {
		return errSourceComposeEnrollment
	}
	var remaining int64
	if err := conn.QueryRow(ctx, `select count(*) from pg_catalog.pg_stat_activity
		where backend_type = 'client backend' and pid <> pg_catalog.pg_backend_pid()`).Scan(&remaining); err != nil || remaining != 0 {
		return errSourceComposeEnrollment
	}
	return nil
}

// enrollSourceCompose fences the known Compose inventory, not unmanaged host
// processes or external restart controllers. The operator must stop those
// separately and maintain that condition through activation.
func enrollSourceCompose(ctx context.Context, request sourceComposeEnrollmentRequest) error {
	if request.ConfigPath == "" || request.ConfigPath != request.Fence.ControlConfigPath ||
		request.Config.DeploymentID != request.Membership.DeploymentID {
		return errSourceEnrollmentInput
	}
	expectedConfig, err := deploy.LoadSourceMaintenanceConfig(request.ConfigPath)
	if err != nil || expectedConfig.Witness != request.Config.Witness.domain() {
		return errSourceComposeEnrollment
	}
	expectedApp := request.Application.domain()
	if expectedApp.DatabaseName != "houfeng" || expectedApp.Epoch != 1 || expectedApp.SystemIdentifier == expectedConfig.Witness.SystemIdentifier {
		return errSourceEnrollmentInput
	}
	if _, err := sourceretirement.NewAuthority(&pgxpool.Pool{}, expectedApp); err != nil {
		return errSourceEnrollmentInput
	}
	membership, err := store.NewBootstrapDeploymentMembershipAdmissionGate(store.DeploymentMembershipIdentity{
		InstanceID: request.Membership.InstanceID, DeploymentID: recordplatform.DeploymentID(request.Membership.DeploymentID),
		ProjectID: recordplatform.ProjectID(request.Membership.ProjectID), InstanceKind: request.Membership.InstanceKind,
		Capability: request.Membership.Capability,
	})
	if err != nil || !sourceComposeCredentialsValid(request.Config.enrollmentConfig()) {
		return errSourceEnrollmentInput
	}
	configs := make([]*pgxpool.Config, 0, 3)
	for _, name := range []string{sourceAppBootstrapDatabaseURLEnv, sourceWitnessBootstrapDatabaseURLEnv, sourceWitnessProvisionerDatabaseURLEnv} {
		value, ok := os.LookupEnv(name)
		if !ok || strings.TrimSpace(value) == "" {
			return errSourceEnrollmentInput
		}
		config, err := platformmigrate.ParseSourceAuthorityPostgresConfig(strings.TrimSpace(value))
		if err != nil {
			return errSourceEnrollmentInput
		}
		configs = append(configs, config)
	}
	if configs[0].ConnConfig.User != "postgres" || configs[0].ConnConfig.Database != "houfeng" {
		return errSourceEnrollmentInput
	}
	fenceRequest := deploy.ComposeOfflineFenceRequest{
		RecoveryID: request.Fence.RecoveryID, Project: request.Fence.Project,
		WorkingDirectory: request.Fence.WorkingDirectory, DatabaseContainerID: request.Fence.DatabaseContainerID,
		ControlConfigPath: request.Fence.ControlConfigPath,
	}
	var enrollment sourceretirement.ApplicationEnrollment
	var observation deploy.ComposeOfflineFence
	err = deploy.FenceComposeSource(ctx, fenceRequest, func(fence deploy.ComposeOfflineFence) error {
		current, err := deploy.LoadSourceMaintenanceConfig(request.ConfigPath)
		if err != nil || current != expectedConfig {
			return errSourceComposeEnrollment
		}
		appConfig, err := sourceComposeAppConfig(configs[0], fence.DatabaseAddress)
		if err != nil {
			return errSourceComposeEnrollment
		}
		// Every newly established connection must independently match the
		// requested physical APP, even if pgx reconnects during the ceremony.
		appConfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted, AccessMode: pgx.ReadOnly})
			if err != nil {
				return errSourceComposeEnrollment
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if checkSourceComposeIdentity(ctx, tx, expectedApp) != nil || tx.Commit(ctx) != nil {
				return errSourceComposeEnrollment
			}
			return nil
		}
		app, err := pgxpool.NewWithConfig(ctx, appConfig)
		if err != nil {
			return errSourceComposeEnrollment
		}
		defer app.Close()
		conn, err := app.Acquire(ctx)
		if err != nil {
			return errSourceComposeEnrollment
		}
		// AfterConnect's read-only identity transaction has ended before the
		// drain and before any W2 or filesystem operations.
		err = sourceComposeDrain(ctx, conn)
		conn.Release()
		if err != nil {
			return errSourceComposeEnrollment
		}
		witnessBootstrap, err := pgxpool.NewWithConfig(ctx, configs[1])
		if err != nil {
			return errSourceComposeEnrollment
		}
		defer witnessBootstrap.Close()
		witnessProvisioner, err := pgxpool.NewWithConfig(ctx, configs[2])
		if err != nil {
			return errSourceComposeEnrollment
		}
		defer witnessProvisioner.Close()
		authority, err := sourceretirement.NewAuthority(witnessProvisioner, expectedConfig.Witness)
		if err != nil {
			return errSourceComposeEnrollment
		}
		enrollment, err = sourceenrollment.Enroll(ctx, app, witnessBootstrap, authority,
			sourceComposeIdentityGate{membership: membership, expected: expectedApp}, request.Config.enrollmentConfig())
		if err != nil || enrollment.Application != expectedApp {
			return errSourceComposeEnrollment
		}
		observation = fence
		return nil
	})
	if err != nil {
		return errSourceComposeEnrollment
	}
	result := struct {
		Enrollment sourceretirement.ApplicationEnrollment `json:"enrollment"`
		Fence      struct {
			RecoveryID           string   `json:"recovery_id"`
			DatabaseContainerID  string   `json:"database_container_id"`
			MaintenanceNetworkID string   `json:"maintenance_network_id"`
			DatabaseAddress      string   `json:"database_address"`
			InventoryDigest      [32]byte `json:"inventory_digest"`
		} `json:"fence"`
	}{Enrollment: enrollment}
	result.Fence.RecoveryID = observation.RecoveryID
	result.Fence.DatabaseContainerID = observation.DatabaseContainerID
	result.Fence.MaintenanceNetworkID = observation.MaintenanceNetworkID
	result.Fence.DatabaseAddress = observation.DatabaseAddress
	result.Fence.InventoryDigest = observation.InventoryDigest
	if json.NewEncoder(os.Stdout).Encode(result) != nil {
		return errSourceComposeEnrollment
	}
	return nil
}
