package deploy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recordauthority"
	"houfeng/internal/center/store"
)

func TestPostgresIntegrationComposeInitialize(t *testing.T) {
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set HOUFENG_POSTGRES_INTEGRATION=1 through the strict PostgreSQL runner")
	}
	if os.Getenv("HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER") == "" || os.Getenv("HOUFENG_RECORDS_RUN_ID") == "" {
		t.Fatal("Compose initialization integration requires the ownership-checked ephemeral PostgreSQL runner")
	}
	ctx := context.Background()
	baseURL := os.Getenv("HOUFENG_DATABASE_URL")
	baseConfig, err := pgxpool.ParseConfig(baseURL)
	if err != nil {
		t.Fatalf("parse strict PostgreSQL fixture URL: %v", err)
	}
	if baseConfig.ConnConfig.User != "postgres" {
		t.Fatalf("strict PostgreSQL fixture user = %q, want OID-10 bootstrap role postgres", baseConfig.ConnConfig.User)
	}
	bootstrapPool, err := store.OpenPostgres(ctx, baseURL)
	if err != nil {
		t.Fatalf("open strict PostgreSQL fixture: %v", err)
	}
	t.Cleanup(bootstrapPool.Close)
	assertComposeIntegrationFixtureHasNoProductionState(t, ctx, bootstrapPool)

	config := uniqueComposeIntegrationConfig(t, baseConfig.ConnConfig.Password)
	createDatabaseDDL := formatComposeIntegrationDDL(t, ctx, bootstrapPool, `CREATE DATABASE %I`, config.DatabaseName)
	if _, err := bootstrapPool.Exec(ctx, createDatabaseDDL); err != nil {
		t.Fatalf("create Compose integration database: %v", err)
	}
	t.Cleanup(func() { cleanupComposeIntegrationDatabase(t, context.Background(), bootstrapPool, config) })

	openPostgres := func(ctx context.Context, endpoint composePostgresEndpoint) (*pgxpool.Pool, error) {
		config := baseConfig.Copy()
		config.ConnConfig.Database = endpoint.Database
		config.ConnConfig.User = endpoint.Role
		config.ConnConfig.Password = endpoint.Password
		pool, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			return nil, err
		}
		if err := pool.Ping(ctx); err != nil {
			pool.Close()
			return nil, err
		}
		return pool, nil
	}
	if err := initializeCompose(ctx, config, openPostgres); err != nil {
		t.Fatalf("fresh Compose initialization error = %v", err)
	}
	assertComposeIntegrationCatalog(t, ctx, bootstrapPool, openPostgres, config)
	assertComposeIntegrationAuthorityProof(t, ctx, openPostgres, config)
	assertComposeIntegrationHeartbeatRejectionsDoNotMutate(t, ctx, openPostgres, config)
	beforeRepeat := readComposeIntegrationDurableCounts(t, ctx, openPostgres, config)
	passwordsBeforeRepeat := readComposeIntegrationPasswordHashes(t, ctx, bootstrapPool, config)
	if err := initializeCompose(ctx, config, openPostgres); err != nil {
		t.Fatalf("exact-repeat Compose initialization error = %v", err)
	}
	afterRepeat := readComposeIntegrationDurableCounts(t, ctx, openPostgres, config)
	if beforeRepeat != afterRepeat {
		t.Fatalf("exact-repeat durable counts = %#v, want unchanged %#v", afterRepeat, beforeRepeat)
	}
	passwordsAfterRepeat := readComposeIntegrationPasswordHashes(t, ctx, bootstrapPool, config)
	if !reflect.DeepEqual(passwordsAfterRepeat, passwordsBeforeRepeat) {
		t.Fatal("exact-repeat Compose initialization rewrote unchanged password verifiers")
	}

	rotated := config
	rotated.Passwords.Runtime = "runtime:/?# rotated secret"
	rotated.Passwords.PlatformAdmin = "admin:/?# rotated secret"
	rotated.Passwords.Migrator = "migrator:/?# rotated secret"
	if err := initializeCompose(ctx, rotated, openPostgres); err != nil {
		t.Fatalf("password-rotation Compose initialization error = %v", err)
	}
	passwordsAfterRotation := readComposeIntegrationPasswordHashes(t, ctx, bootstrapPool, rotated)
	for _, role := range []string{rotated.Roles.CenterRuntime, rotated.Roles.PlatformAdmin, rotated.Roles.Migrator} {
		if passwordsAfterRotation[role] == passwordsAfterRepeat[role] {
			t.Fatalf("approved password rotation did not change verifier for %q", role)
		}
	}
	if passwordsAfterRotation[rotated.AuthorityRole] != passwordsAfterRepeat[rotated.AuthorityRole] {
		t.Fatal("operator password rotation changed the state-derived authority verifier")
	}
	assertComposeIntegrationCatalog(t, ctx, bootstrapPool, openPostgres, rotated)
	assertComposeIntegrationAuthorityProof(t, ctx, openPostgres, rotated)

	grantDDL := formatComposeIntegrationDDL(t, ctx, bootstrapPool, `GRANT pg_read_all_data TO %I`, rotated.Roles.CenterRuntime)
	if _, err := bootstrapPool.Exec(ctx, grantDDL); err != nil {
		t.Fatalf("introduce Compose membership drift: %v", err)
	}
	passwordsBeforeRejectedRotation := readComposeIntegrationPasswordHashes(t, ctx, bootstrapPool, rotated)
	rejected := rotated
	rejected.Passwords.Runtime = "runtime rejected rotation"
	rejected.Passwords.PlatformAdmin = "admin rejected rotation"
	rejected.Passwords.Migrator = "migrator rejected rotation"
	if err := initializeCompose(ctx, rejected, openPostgres); err == nil {
		t.Fatal("Compose initialization accepted existing role membership drift")
	}
	passwordsAfterRejectedRotation := readComposeIntegrationPasswordHashes(t, ctx, bootstrapPool, rotated)
	if !reflect.DeepEqual(passwordsAfterRejectedRotation, passwordsBeforeRejectedRotation) {
		t.Fatal("membership-drift rejection partially rotated role passwords")
	}
}

func assertComposeIntegrationFixtureHasNoProductionState(t *testing.T, ctx context.Context, bootstrapPool *pgxpool.Pool) {
	t.Helper()
	productionRoles := []string{composeRuntimeRole, composeAdminRole, composeMigratorRole, composeAuthorityRole}
	var roleCount, databaseCount int
	if err := bootstrapPool.QueryRow(ctx, `
		select (select count(*)::integer from pg_catalog.pg_roles where rolname = any($1::name[])),
		       (select count(*)::integer from pg_catalog.pg_database where datname = $2)
	`, productionRoles, composeDatabaseName).Scan(&roleCount, &databaseCount); err != nil {
		t.Fatalf("inspect isolated Compose integration fixture: %v", err)
	}
	if roleCount != 0 || databaseCount != 0 {
		t.Fatalf("isolated Compose integration fixture contains pre-existing production state (roles=%d database=%d); refusing to mutate it", roleCount, databaseCount)
	}
}

func uniqueComposeIntegrationConfig(t *testing.T, bootstrapPassword string) ComposeInitConfig {
	t.Helper()
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("generate unique Compose integration suffix: %v", err)
	}
	suffix := hex.EncodeToString(random)
	stateParent := t.TempDir()
	centerConfigDirectory := filepath.Join(t.TempDir(), "center-config")
	if err := os.Mkdir(centerConfigDirectory, 0o755); err != nil {
		t.Fatalf("create Compose integration Center config directory: %v", err)
	}
	config := ComposeInitConfig{
		DatabaseHost:           composeDatabaseHost,
		DatabasePort:           composeDatabasePort,
		DatabaseName:           "hf_compose_" + suffix,
		BootstrapRole:          composeBootstrapRole,
		AuthorityRole:          composeAuthorityRole,
		AuthorityStateRoot:     filepath.Join(stateParent, "records-authority"),
		CenterDeploymentIDPath: filepath.Join(centerConfigDirectory, "deployment-id"),
		Roles: platformmigrate.AppRoleSetV1{
			CenterRuntime: "hf_runtime_" + suffix,
			PlatformAdmin: "hf_admin_" + suffix,
			Migrator:      "hf_migrator_" + suffix,
		},
		Passwords: ComposeInitPasswords{
			Bootstrap:     bootstrapPassword,
			Runtime:       "runtime:/?# first secret",
			PlatformAdmin: "admin:/?# first secret",
			Migrator:      "migrator:/?# first secret",
		},
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("validate unique Compose integration config: %v", err)
	}
	return config
}

type composeIntegrationDurableCounts struct {
	Migrations  int
	Manifests   int
	Head        int
	Contract    int
	Memberships int
}

func readComposeIntegrationDurableCounts(t *testing.T, ctx context.Context, openPostgres func(context.Context, composePostgresEndpoint) (*pgxpool.Pool, error), config ComposeInitConfig) composeIntegrationDurableCounts {
	t.Helper()
	pool, err := openPostgres(ctx, composePostgresEndpoint{Database: config.DatabaseName, Role: config.Roles.Migrator, Password: config.Passwords.Migrator})
	if err != nil {
		t.Fatalf("open direct migrator for durable counts: %v", err)
	}
	defer pool.Close()
	var counts composeIntegrationDurableCounts
	if err := pool.QueryRow(ctx, `
			select (select count(*)::integer from public.schema_migrations),
			       (select count(*)::integer from public.app_acl_manifest_revisions),
			       (select count(*)::integer from public.app_acl_manifest_head),
			       (select count(*)::integer from public.deployment_contract_state),
			       (select count(*)::integer from public.deployment_membership)
		`).Scan(&counts.Migrations, &counts.Manifests, &counts.Head, &counts.Contract, &counts.Memberships); err != nil {
		t.Fatalf("read Compose durable counts: %v", err)
	}
	return counts
}

type composeIntegrationMembershipSnapshot struct {
	DeploymentID         string
	ProjectID            string
	InstanceID           string
	InstanceKind         string
	Capability           string
	DeploymentEpoch      int64
	FenceContractVersion int64
	LoadBalancerAdmitted bool
	QueueAdmitted        bool
	HeartbeatExpiresAt   time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

func readComposeIntegrationMembership(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) composeIntegrationMembershipSnapshot {
	t.Helper()
	var snapshot composeIntegrationMembershipSnapshot
	if err := pool.QueryRow(ctx, `
		select deployment_id,
		       project_id,
		       instance_id,
		       instance_kind,
		       capability,
		       deployment_epoch,
		       fence_contract_version,
		       load_balancer_admitted,
		       queue_admitted,
		       heartbeat_expires_at,
		       created_at,
		       updated_at
		from public.deployment_membership
		where instance_id = 'compose-center'
	`).Scan(
		&snapshot.DeploymentID,
		&snapshot.ProjectID,
		&snapshot.InstanceID,
		&snapshot.InstanceKind,
		&snapshot.Capability,
		&snapshot.DeploymentEpoch,
		&snapshot.FenceContractVersion,
		&snapshot.LoadBalancerAdmitted,
		&snapshot.QueueAdmitted,
		&snapshot.HeartbeatExpiresAt,
		&snapshot.CreatedAt,
		&snapshot.UpdatedAt,
	); err != nil {
		t.Fatalf("read Compose membership: %v", err)
	}
	return snapshot
}

func assertComposeIntegrationAuthorityProof(
	t *testing.T,
	ctx context.Context,
	openPostgres func(context.Context, composePostgresEndpoint) (*pgxpool.Pool, error),
	config ComposeInitConfig,
) {
	t.Helper()
	state, err := recordauthority.LoadComposeState(config.AuthorityStateRoot)
	if err != nil {
		t.Fatalf("load verified Compose authority state: %v", err)
	}
	if err := recordauthority.VerifyActivationReceipt(config.AuthorityStateRoot, state); err != nil {
		t.Fatalf("verify Compose activation receipt: %v", err)
	}
	publishedDeploymentID, err := os.ReadFile(config.CenterDeploymentIDPath)
	if err != nil {
		t.Fatalf("read published Compose Center deployment identity: %v", err)
	}
	if got, want := string(publishedDeploymentID), string(state.DeploymentID)+"\n"; got != want {
		t.Fatalf("published Compose Center deployment identity = %q, want %q", got, want)
	}

	migratorPool, err := openPostgres(ctx, composePostgresEndpoint{
		Database: config.DatabaseName,
		Role:     config.Roles.Migrator,
		Password: config.Passwords.Migrator,
	})
	if err != nil {
		t.Fatalf("open Compose migrator for authority proof: %v", err)
	}
	defer migratorPool.Close()
	var deploymentID, profile string
	var deploymentEpoch, fenceContractVersion int64
	if err := migratorPool.QueryRow(ctx, `
		select deployment_id, active_profile, active_domain_identity_epoch, minimum_fence_contract_version
		from public.deployment_contract_state
		where project_id = 'default'
	`).Scan(&deploymentID, &profile, &deploymentEpoch, &fenceContractVersion); err != nil {
		t.Fatalf("read active Compose deployment contract: %v", err)
	}
	if deploymentID != string(state.DeploymentID) || profile != "postgres_sync" ||
		deploymentEpoch != int64(state.Memberships[0].DeploymentEpoch) ||
		fenceContractVersion != int64(state.Memberships[0].FenceContractVersion) {
		t.Fatalf("active Compose deployment contract does not match verified authority state")
	}
	membership := readComposeIntegrationMembership(t, ctx, migratorPool)
	wantMembership := state.Memberships[0]
	if membership.DeploymentID != string(state.DeploymentID) || membership.ProjectID != "default" ||
		membership.InstanceID != wantMembership.InstanceID || membership.InstanceKind != wantMembership.InstanceKind ||
		membership.Capability != wantMembership.Capability ||
		membership.DeploymentEpoch != int64(wantMembership.DeploymentEpoch) ||
		membership.FenceContractVersion != int64(wantMembership.FenceContractVersion) ||
		membership.LoadBalancerAdmitted != wantMembership.LoadBalancerAdmitted ||
		membership.QueueAdmitted != wantMembership.QueueAdmitted {
		t.Fatalf("Compose membership = %#v, want exact verified authority inventory %#v", membership, wantMembership)
	}
	now := time.Now().UTC()
	if !membership.HeartbeatExpiresAt.After(now) || membership.HeartbeatExpiresAt.After(now.Add(120*time.Second)) {
		t.Fatalf("Compose membership expiry %s is not a fresh bounded lease at %s", membership.HeartbeatExpiresAt, now)
	}
}

func assertComposeIntegrationHeartbeatRejectionsDoNotMutate(
	t *testing.T,
	ctx context.Context,
	openPostgres func(context.Context, composePostgresEndpoint) (*pgxpool.Pool, error),
	config ComposeInitConfig,
) {
	t.Helper()
	state, err := recordauthority.LoadComposeState(config.AuthorityStateRoot)
	if err != nil {
		t.Fatalf("load Compose authority state for heartbeat rejection assertions: %v", err)
	}
	migratorPool, err := openPostgres(ctx, composePostgresEndpoint{
		Database: config.DatabaseName,
		Role:     config.Roles.Migrator,
		Password: config.Passwords.Migrator,
	})
	if err != nil {
		t.Fatalf("open Compose migrator for heartbeat rejection assertions: %v", err)
	}
	defer migratorPool.Close()
	authorityPool, err := openPostgres(ctx, composePostgresEndpoint{
		Database: config.DatabaseName,
		Role:     config.AuthorityRole,
		Password: state.DatabasePassword(),
	})
	if err != nil {
		t.Fatalf("open Compose authority for heartbeat rejection assertions: %v", err)
	}
	defer authorityPool.Close()

	before := readComposeIntegrationMembership(t, ctx, migratorPool)
	validCommand, _, err := recordauthority.MarshalMembershipHeartbeatCommandV1(state, time.Now().UTC())
	if err != nil {
		t.Fatalf("marshal valid heartbeat baseline: %v", err)
	}
	foreignDeployment := append([]byte(nil), validCommand...)
	if foreignDeployment[40] == '0' {
		foreignDeployment[40] = '1'
	} else {
		foreignDeployment[40] = '0'
	}
	mismatchedEpoch := append([]byte(nil), validCommand...)
	mismatchedEpoch[150]++
	for name, command := range map[string][]byte{
		"foreign deployment": foreignDeployment,
		"mismatched epoch":   mismatchedEpoch,
		"truncated command":  validCommand[:len(validCommand)-1],
	} {
		var ignored time.Time
		if err := authorityPool.QueryRow(ctx, `select public.record_platform_compose_membership_heartbeat($1::bytea)`, command).Scan(&ignored); err == nil {
			t.Fatalf("Compose heartbeat accepted %s", name)
		}
		after := readComposeIntegrationMembership(t, ctx, migratorPool)
		if after != before {
			t.Fatalf("rejected %s heartbeat mutated membership: before=%#v after=%#v", name, before, after)
		}
	}
	for name, issuedAt := range map[string]time.Time{
		"stale issue time":  time.Now().UTC().Add(-45 * time.Second),
		"future issue time": time.Now().UTC().Add(2 * time.Minute),
	} {
		if _, err := heartbeatComposeAuthorityAt(ctx, authorityPool, state, issuedAt); err == nil {
			t.Fatalf("Compose heartbeat accepted %s", name)
		}
		after := readComposeIntegrationMembership(t, ctx, migratorPool)
		if after != before {
			t.Fatalf("rejected %s heartbeat mutated membership: before=%#v after=%#v", name, before, after)
		}
	}
}

func assertComposeIntegrationCatalog(
	t *testing.T,
	ctx context.Context,
	bootstrapPool *pgxpool.Pool,
	openPostgres func(context.Context, composePostgresEndpoint) (*pgxpool.Pool, error),
	config ComposeInitConfig,
) {
	t.Helper()
	for _, role := range []string{config.Roles.CenterRuntime, config.Roles.PlatformAdmin, config.Roles.Migrator, config.AuthorityRole} {
		var canLogin, inherit, superuser, createDB, createRole, replication, bypassRLS bool
		if err := bootstrapPool.QueryRow(ctx, `
			select rolcanlogin, rolinherit, rolsuper, rolcreatedb, rolcreaterole, rolreplication, rolbypassrls
			from pg_catalog.pg_roles
			where rolname = $1
		`, role).Scan(&canLogin, &inherit, &superuser, &createDB, &createRole, &replication, &bypassRLS); err != nil {
			t.Fatalf("read Compose role %q: %v", role, err)
		}
		if !canLogin || inherit || superuser || createDB || createRole || replication || bypassRLS {
			t.Fatalf("Compose role %q is not a constrained direct login", role)
		}
	}
	var owner string
	if err := bootstrapPool.QueryRow(ctx, `
		select owner.rolname
		from pg_catalog.pg_database database
		join pg_catalog.pg_roles owner on owner.oid = database.datdba
		where database.datname = $1
	`, config.DatabaseName).Scan(&owner); err != nil {
		t.Fatalf("read Compose database owner: %v", err)
	}
	if owner != config.Roles.Migrator {
		t.Fatalf("Compose database owner = %q, want %q", owner, config.Roles.Migrator)
	}
	var publicExecute bool
	if err := bootstrapPool.QueryRow(ctx, `
		select exists (
			select 1
			from pg_catalog.pg_proc procedure
			join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
			cross join lateral pg_catalog.aclexplode(procedure.proacl) acl_grant
			where namespace.nspname = 'pg_catalog'
			  and procedure.proname = 'pg_control_system'
			  and procedure.pronargs = 0
			  and acl_grant.grantee = 0
			  and acl_grant.privilege_type = 'EXECUTE'
		)
	`).Scan(&publicExecute); err != nil {
		t.Fatalf("read pg_control_system PUBLIC privilege: %v", err)
	}
	if publicExecute {
		t.Fatal("Compose pre-R1 contract retained PUBLIC pg_control_system() EXECUTE")
	}

	authorityState, err := recordauthority.LoadComposeState(config.AuthorityStateRoot)
	if err != nil {
		t.Fatalf("load Compose authority state for catalog assertions: %v", err)
	}
	authorityPool, err := openPostgres(ctx, composePostgresEndpoint{
		Database: config.DatabaseName,
		Role:     config.AuthorityRole,
		Password: authorityState.DatabasePassword(),
	})
	if err != nil {
		t.Fatalf("open constrained Compose authority role: %v", err)
	}
	defer authorityPool.Close()
	var heartbeat, projector, membershipSelect, membershipInsert, membershipUpdate, membershipDelete bool
	if err := authorityPool.QueryRow(ctx, `
		select has_function_privilege(current_user, 'public.record_platform_compose_membership_heartbeat(bytea)', 'EXECUTE'),
		       has_function_privilege(current_user, 'public.record_platform_cas_contract_activation_projection(bytea)', 'EXECUTE'),
		       has_table_privilege(current_user, 'public.deployment_membership', 'SELECT'),
		       has_table_privilege(current_user, 'public.deployment_membership', 'INSERT'),
		       has_table_privilege(current_user, 'public.deployment_membership', 'UPDATE'),
		       has_table_privilege(current_user, 'public.deployment_membership', 'DELETE')
	`).Scan(&heartbeat, &projector, &membershipSelect, &membershipInsert, &membershipUpdate, &membershipDelete); err != nil {
		t.Fatalf("read Compose authority privileges: %v", err)
	}
	if !heartbeat || projector || membershipSelect || membershipInsert || membershipUpdate || membershipDelete {
		t.Fatalf("Compose authority privilege closure = heartbeat:%t projector:%t membership:[%t %t %t %t]", heartbeat, projector, membershipSelect, membershipInsert, membershipUpdate, membershipDelete)
	}
	for _, endpoint := range []composePostgresEndpoint{
		{Database: config.DatabaseName, Role: config.Roles.CenterRuntime, Password: config.Passwords.Runtime},
		{Database: config.DatabaseName, Role: config.Roles.PlatformAdmin, Password: config.Passwords.PlatformAdmin},
	} {
		pool, openErr := openPostgres(ctx, endpoint)
		if openErr != nil {
			t.Fatalf("open Compose role %q for heartbeat privilege assertion: %v", endpoint.Role, openErr)
		}
		var permitted bool
		queryErr := pool.QueryRow(ctx, `select has_function_privilege(current_user, 'public.record_platform_compose_membership_heartbeat(bytea)', 'EXECUTE')`).Scan(&permitted)
		pool.Close()
		if queryErr != nil {
			t.Fatalf("read Compose role %q heartbeat privilege: %v", endpoint.Role, queryErr)
		}
		if permitted {
			t.Fatalf("Compose role %q can execute the authority heartbeat", endpoint.Role)
		}
	}
	var publicHeartbeat bool
	if err := authorityPool.QueryRow(ctx, `
		select exists (
			select 1
			from pg_catalog.pg_proc procedure
			join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
			cross join lateral pg_catalog.aclexplode(procedure.proacl) acl_grant
			where namespace.nspname = 'public'
			  and procedure.proname = 'record_platform_compose_membership_heartbeat'
			  and procedure.pronargs = 1
			  and acl_grant.grantee = 0
			  and acl_grant.privilege_type = 'EXECUTE'
		)
	`).Scan(&publicHeartbeat); err != nil {
		t.Fatalf("read Compose heartbeat PUBLIC privilege: %v", err)
	}
	if publicHeartbeat {
		t.Fatal("PUBLIC can execute the Compose authority heartbeat")
	}
}

func readComposeIntegrationPasswordHashes(t *testing.T, ctx context.Context, bootstrapPool *pgxpool.Pool, config ComposeInitConfig) map[string]string {
	t.Helper()
	rows, err := bootstrapPool.Query(ctx, `select rolname, rolpassword from pg_catalog.pg_authid where rolname = any($1::name[]) order by rolname`, []string{config.Roles.CenterRuntime, config.Roles.PlatformAdmin, config.Roles.Migrator, config.AuthorityRole})
	if err != nil {
		t.Fatalf("read Compose role password hashes: %v", err)
	}
	defer rows.Close()
	values := make(map[string]string, 4)
	for rows.Next() {
		var role, passwordHash string
		if err := rows.Scan(&role, &passwordHash); err != nil {
			t.Fatalf("scan Compose role password hash: %v", err)
		}
		values[role] = passwordHash
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate Compose role password hashes: %v", err)
	}
	return values
}

func formatComposeIntegrationDDL(t *testing.T, ctx context.Context, pool *pgxpool.Pool, format string, arguments ...any) string {
	t.Helper()
	queryArguments := append([]any{format}, arguments...)
	placeholders := make([]string, len(arguments))
	for index := range arguments {
		placeholders[index] = "$" + string(rune('2'+index)) + "::text"
	}
	query := `select pg_catalog.format($1::text, ` + strings.Join(placeholders, ", ") + `)`
	var ddl string
	if err := pool.QueryRow(ctx, query, queryArguments...).Scan(&ddl); err != nil {
		t.Fatalf("format integration DDL: %v", err)
	}
	return ddl
}

func cleanupComposeIntegrationDatabase(t *testing.T, ctx context.Context, bootstrapPool *pgxpool.Pool, config ComposeInitConfig) {
	t.Helper()
	_, _ = bootstrapPool.Exec(ctx, `select pg_catalog.pg_terminate_backend(pid) from pg_catalog.pg_stat_activity where datname = $1 and pid <> pg_backend_pid()`, config.DatabaseName)
	for _, formatAndArguments := range []struct {
		format    string
		arguments []any
	}{
		{format: `DROP DATABASE IF EXISTS %I`, arguments: []any{config.DatabaseName}},
		{format: `DROP ROLE IF EXISTS %I, %I, %I, %I`, arguments: []any{config.Roles.CenterRuntime, config.Roles.PlatformAdmin, config.Roles.Migrator, config.AuthorityRole}},
	} {
		ddl := formatComposeIntegrationDDL(t, ctx, bootstrapPool, formatAndArguments.format, formatAndArguments.arguments...)
		if _, err := bootstrapPool.Exec(ctx, ddl); err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("cleanup Compose integration database: %v", err)
		}
	}
}
