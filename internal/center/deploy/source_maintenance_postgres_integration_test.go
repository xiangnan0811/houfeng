//go:build linux

package deploy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recordauthority"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/sourceenrollment"
	"houfeng/internal/center/sourceretirement"
	"houfeng/internal/center/store"
)

// This fixture starts with isolated, already-quiescent APP and W2 databases. It
// tests the maintenance interlock, not the offline cutover or recovery protocol.
func TestPostgresIntegrationSourceMaintenanceInterlock(t *testing.T) {
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("set HOUFENG_POSTGRES_INTEGRATION=1 through the strict PostgreSQL runner")
	}
	if os.Getenv("HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER") == "" || os.Getenv("HOUFENG_RECORDS_RUN_ID") == "" || os.Getenv("HOUFENG_DATABASE_URL") == "" || os.Getenv("HOUFENG_DELETION_WITNESS_DATABASE_URL") == "" {
		t.Fatal("source maintenance requires the ownership-checked independent APP and W2 PostgreSQL runner")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	appConfig, err := pgxpool.ParseConfig(os.Getenv("HOUFENG_DATABASE_URL"))
	if err != nil || appConfig.ConnConfig.User != "postgres" {
		t.Fatal("APP fixture requires a direct postgres bootstrap URL")
	}
	appAdmin, err := pgxpool.NewWithConfig(ctx, appConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(appAdmin.Close)
	assertComposeIntegrationFixtureHasNoProductionState(t, ctx, appAdmin)
	config := uniqueComposeIntegrationConfig(t, appConfig.ConnConfig.Password)
	if _, err := appAdmin.Exec(ctx, formatComposeIntegrationDDL(t, ctx, appAdmin, `CREATE DATABASE %I`, config.DatabaseName)); err != nil {
		t.Fatalf("create isolated APP database: %v", err)
	}
	t.Cleanup(func() { cleanupComposeIntegrationDatabase(t, context.Background(), appAdmin, config) })
	openApp := func(ctx context.Context, endpoint composePostgresEndpoint) (*pgxpool.Pool, error) {
		c, err := composeIntegrationEndpointConfig(appConfig, endpoint)
		if err != nil {
			return nil, err
		}
		pool, err := pgxpool.NewWithConfig(ctx, c)
		if err != nil {
			return nil, err
		}
		if err := pool.Ping(ctx); err != nil {
			pool.Close()
			return nil, err
		}
		return pool, nil
	}
	app, err := openApp(ctx, composePostgresEndpoint{Database: config.DatabaseName, Role: config.BootstrapRole, Password: config.Passwords.Bootstrap})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	controlPath := filepath.Join(t.TempDir(), "source-authority.json")
	if err := os.Chmod(filepath.Dir(controlPath), 0o700); err != nil {
		t.Fatal(err)
	}
	deps := composeLaunchIsolatedInitDependencies(openApp)
	// This fixture proves the real source/W2 interlock only. Protected UID launch
	// publication and runtime launch verification require their own fixture.
	// The source guard and credential checks below remain production paths.
	deps.withMaintenance = func(ctx context.Context, pool *pgxpool.Pool, fn func(*SourceMaintenanceConfig) error) error {
		return WithSourceMaintenance(ctx, pool, controlPath, false, fn)
	}
	initialize := func(c ComposeInitConfig) error { return initializeComposeWithDependencies(ctx, c, deps) }
	if err := initialize(config); err != nil {
		t.Fatalf("unenrolled initialization: %v", err)
	}
	if err := initialize(config); err != nil {
		t.Fatalf("unenrolled exact initialization: %v", err)
	}
	state, err := recordauthority.LoadComposeState(config.AuthorityStateRoot)
	if err != nil {
		t.Fatal(err)
	}
	authorityPool, err := openApp(ctx, composePostgresEndpoint{Database: config.DatabaseName, Role: config.AuthorityRole, Password: state.DatabasePassword()})
	if err != nil {
		t.Fatal(err)
	}
	defer authorityPool.Close()
	if err := heartbeatComposeAuthority(ctx, authorityPool, state); err != nil {
		t.Fatalf("unenrolled signed heartbeat: %v", err)
	}
	if _, err := platformmigrate.ProvisionPostgresDomainIdentity(ctx, app, platformmigrate.DomainKindApplication); err != nil {
		t.Fatalf("provision APP identity: %v", err)
	}

	witnessURL := os.Getenv("HOUFENG_DELETION_WITNESS_DATABASE_URL")
	witnessConfig, err := pgxpool.ParseConfig(witnessURL)
	if err != nil || witnessConfig.ConnConfig.User != "postgres" {
		t.Fatal("W2 fixture requires a direct postgres bootstrap URL")
	}
	witnessAdmin, err := pgxpool.NewWithConfig(ctx, witnessConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(witnessAdmin.Close)
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	suffix := hex.EncodeToString(random[:])
	witnessDBName := "hf_sm_" + suffix
	roles := platformmigrate.SourceAuthorityRoles{Owner: "hf_sm_o_" + suffix, Reader: "hf_sm_r_" + suffix, Producer: "hf_sm_p_" + suffix, Provisioner: "hf_sm_c_" + suffix}
	recoveryRole := "hf_sm_x_" + suffix
	createdRoles := []string{}
	if _, err := witnessAdmin.Exec(ctx, "create database "+pgx.Identifier{witnessDBName}.Sanitize()); err != nil {
		t.Fatalf("create isolated W2 database: %v", err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if _, err := witnessAdmin.Exec(cleanup, "drop database "+pgx.Identifier{witnessDBName}.Sanitize()+" with (force)"); err != nil {
			t.Errorf("drop isolated W2 database: %v", err)
		}
		for _, role := range createdRoles {
			if _, err := witnessAdmin.Exec(cleanup, "drop role "+pgx.Identifier{role}.Sanitize()); err != nil {
				t.Errorf("drop isolated W2 role: %v", err)
			}
		}
	})
	witnessPassword := "source-maintenance-" + suffix
	createRole := func(role string, login bool) {
		t.Helper()
		attribute := "login"
		if !login {
			attribute = "nologin"
		}
		var ddl string
		if err := witnessAdmin.QueryRow(ctx, `select format('create role %I `+attribute+` noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls password %L', $1::text, $2::text)`, role, witnessPassword).Scan(&ddl); err != nil {
			t.Fatal(err)
		}
		if _, err := witnessAdmin.Exec(ctx, ddl); err != nil {
			t.Fatalf("create W2 role: %v", err)
		}
		createdRoles = append(createdRoles, role)
	}
	createRole(roles.Owner, false)
	createRole(roles.Reader, true)
	createRole(roles.Producer, true)
	createRole(roles.Provisioner, true)
	witnessConfig.ConnConfig.Database = witnessDBName
	witness, err := pgxpool.NewWithConfig(ctx, witnessConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer witness.Close()
	physical, err := platformmigrate.ProvisionSourceAuthority(ctx, witness, roles)
	if err != nil {
		t.Fatalf("provision W2 roles and schema: %v", err)
	}
	domain := sourceretirement.DomainIdentity{ID: physical.ID, SystemIdentifier: physical.PostgresSystemIdentifier, DatabaseOID: physical.DatabaseOID, DatabaseName: physical.DatabaseName, Epoch: uint64(physical.IdentityEpoch)}
	var appSystem string
	if err := app.QueryRow(ctx, `select system_identifier::text from pg_control_system()`).Scan(&appSystem); err != nil || appSystem == domain.SystemIdentifier {
		t.Fatalf("APP and W2 must be independent PostgreSQL systems: %v", err)
	}
	openWitness := func(role string) *pgxpool.Pool {
		t.Helper()
		c := witnessConfig.Copy()
		c.ConnConfig.User, c.ConnConfig.Password = role, witnessPassword
		pool, err := pgxpool.NewWithConfig(ctx, c)
		if err != nil {
			t.Fatal(err)
		}
		var session, current string
		if err := pool.QueryRow(ctx, `select session_user, current_user`).Scan(&session, &current); err != nil || session != role || current != role {
			pool.Close()
			t.Fatalf("W2 direct role authentication failed for %s: %v", role, err)
		}
		t.Cleanup(pool.Close)
		return pool
	}
	readerPool := openWitness(roles.Reader)
	producerPool := openWitness(roles.Producer)
	provisionerPool := openWitness(roles.Provisioner)
	reader, err := sourceretirement.NewAuthority(readerPool, domain)
	if err != nil {
		t.Fatal(err)
	}
	producer, err := sourceretirement.NewAuthority(producerPool, domain)
	if err != nil {
		t.Fatal(err)
	}
	provisioner, err := sourceretirement.NewAuthority(provisionerPool, domain)
	if err != nil {
		t.Fatal(err)
	}
	for name, principal := range map[string]*sourceretirement.Authority{"reader": reader, "producer": producer, "provisioner": provisioner} {
		if _, err := principal.Snapshot(ctx); err != nil {
			t.Fatalf("%s W2 snapshot: %v", name, err)
		}
	}
	if _, err := readerPool.Exec(ctx, `delete from public.source_authority_state`); err == nil {
		t.Fatal("W2 reader obtained direct mutation privilege")
	}
	if _, err := producerPool.Exec(ctx, `delete from public.source_authority_state`); err == nil {
		t.Fatal("W2 producer obtained direct mutation privilege")
	}
	readerURL, err := url.Parse(witnessURL)
	if err != nil {
		t.Fatal(err)
	}
	readerURL.User = url.UserPassword(roles.Reader, witnessPassword)
	readerURL.Path = "/" + witnessDBName
	maintenance, err := ProvisionSourceMaintenanceConfig(controlPath, domain, readerURL.String())
	if err != nil {
		t.Fatalf("explicitly provision protected source guard: %v", err)
	}
	if maintenance.BirthInstalled || maintenance.BirthLineageID != "" || maintenance.BirthProtocolVersion != 0 || maintenance.BirthReaderDatabaseURL != "" {
		t.Fatal("new source maintenance config must explicitly start without a birth installation")
	}
	configData, err := os.ReadFile(controlPath)
	if err != nil {
		t.Fatal(err)
	}
	var configFields map[string]json.RawMessage
	if err := json.Unmarshal(configData, &configFields); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"birth_installed": "false", "birth_lineage_id": `""`, "birth_protocol_version": "0", "birth_reader_database_url": `""`,
	} {
		if string(configFields[name]) != want {
			t.Fatalf("new config omitted or changed %s", name)
		}
	}
	if repeat, err := ProvisionSourceMaintenanceConfig(controlPath, domain, readerURL.String()); err != nil || repeat != maintenance {
		t.Fatalf("exact protected source guard provisioning replay: %v", err)
	}
	gate, err := store.NewBootstrapDeploymentMembershipAdmissionGate(store.DeploymentMembershipIdentity{InstanceID: "compose-center", DeploymentID: state.DeploymentID, ProjectID: recordplatform.ProjectIDDefault, InstanceKind: "api", Capability: "records.runtime"})
	if err != nil {
		t.Fatal(err)
	}
	passwords := platformmigrate.ComposeRolePasswords{Runtime: config.Passwords.Runtime, PlatformAdmin: config.Passwords.PlatformAdmin, DeletionWorker: config.Passwords.DeletionWorker, Migrator: config.Passwords.Migrator, Authority: state.DatabasePassword()}
	enrollment := sourceenrollment.Config{DeploymentID: string(state.DeploymentID), Witness: domain, Roles: config.Roles, AuthorityRole: config.AuthorityRole, Passwords: passwords, Producer: platformmigrate.SourceRoleCredential{Principal: roles.Producer, Password: witnessPassword}}
	if err := WithSourceMaintenance(ctx, app, controlPath, true, func(observed *SourceMaintenanceConfig) error {
		if observed == nil || *observed != maintenance {
			return errors.New("protected enrollment configuration mismatch")
		}
		_, err := sourceenrollment.Enroll(ctx, app, witness, provisioner, gate, enrollment)
		return err
	}); err != nil {
		t.Fatalf("enroll quiescent isolated APP under exclusive guard: %v", err)
	}
	if err := WithSourceMaintenance(ctx, app, controlPath, false, func(observed *SourceMaintenanceConfig) error {
		return VerifySourceMaintenanceCredentials(ctx, app, observed, config.Roles, config.AuthorityRole, passwords)
	}); err != nil {
		t.Fatalf("verify enrolled current credentials: %v", err)
	}
	if err := initialize(config); err != nil {
		t.Fatalf("enrolled exact initialization: %v", err)
	}
	guardedHeartbeat := func() error {
		return WithSourceMaintenanceAuthority(ctx, authorityPool, controlPath, func(_ *SourceMaintenanceConfig) error {
			return heartbeatComposeAuthority(ctx, authorityPool, state)
		})
	}
	if err := guardedHeartbeat(); err != nil {
		t.Fatalf("enrolled guarded signed heartbeat: %v", err)
	}

	// A rejected operation must not mutate the password verifiers, durable
	// deployment ID publication, or the signed membership lease.
	assertDeniedWithoutEffects := func(name string, operation func() error) {
		t.Helper()
		beforeHashes := readComposeIntegrationPasswordHashes(t, ctx, appAdmin, config)
		beforeMembership := readComposeIntegrationMembership(t, ctx, app)
		beforeID, err := os.ReadFile(config.CenterDeploymentIDPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := operation(); err == nil {
			t.Fatalf("%s unexpectedly succeeded", name)
		}
		if !reflect.DeepEqual(beforeHashes, readComposeIntegrationPasswordHashes(t, ctx, appAdmin, config)) {
			t.Fatalf("%s changed APP role password verifiers", name)
		}
		if after := readComposeIntegrationMembership(t, ctx, app); !reflect.DeepEqual(beforeMembership, after) {
			t.Fatalf("%s changed signed membership", name)
		}
		afterID, err := os.ReadFile(config.CenterDeploymentIDPath)
		if err != nil || string(afterID) != string(beforeID) {
			t.Fatalf("%s changed deployment ID publication: %v", name, err)
		}
	}
	rejectedInitialization := func(c ComposeInitConfig) error {
		err := initialize(c)
		if !errors.Is(err, ErrComposeInitMaintenance) {
			t.Fatalf("initialization error = %v, want maintenance rejection", err)
		}
		return err
	}
	for _, stale := range []struct {
		name   string
		mutate func(*ComposeInitConfig)
	}{
		{"runtime", func(c *ComposeInitConfig) { c.Passwords.Runtime += "-stale" }},
		{"admin", func(c *ComposeInitConfig) { c.Passwords.PlatformAdmin += "-stale" }},
		{"worker", func(c *ComposeInitConfig) { c.Passwords.DeletionWorker += "-stale" }},
		{"migrator", func(c *ComposeInitConfig) { c.Passwords.Migrator += "-stale" }},
	} {
		changed := config
		stale.mutate(&changed)
		assertDeniedWithoutEffects("stale "+stale.name+" environment initialization", func() error { return rejectedInitialization(changed) })
	}
	for _, stale := range []struct {
		name   string
		mutate func(*platformmigrate.ComposeRolePasswords)
	}{
		{"runtime", func(p *platformmigrate.ComposeRolePasswords) { p.Runtime += "-stale" }},
		{"admin", func(p *platformmigrate.ComposeRolePasswords) { p.PlatformAdmin += "-stale" }},
		{"worker", func(p *platformmigrate.ComposeRolePasswords) { p.DeletionWorker += "-stale" }},
		{"migrator", func(p *platformmigrate.ComposeRolePasswords) { p.Migrator += "-stale" }},
		{"authority", func(p *platformmigrate.ComposeRolePasswords) { p.Authority += "-stale" }},
	} {
		changed := passwords
		stale.mutate(&changed)
		assertDeniedWithoutEffects("stale "+stale.name+" credential verification", func() error {
			return WithSourceMaintenance(ctx, app, controlPath, false, func(observed *SourceMaintenanceConfig) error {
				return VerifySourceMaintenanceCredentials(ctx, app, observed, config.Roles, config.AuthorityRole, changed)
			})
		})
	}

	missingPath := controlPath + ".saved"
	if err := os.Rename(controlPath, missingPath); err != nil {
		t.Fatal(err)
	}
	assertDeniedWithoutEffects("missing configuration initialization", func() error { return rejectedInitialization(config) })
	assertDeniedWithoutEffects("missing configuration heartbeat", guardedHeartbeat)
	if err := os.Rename(missingPath, controlPath); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(maintenance.GuardDirectory, maintenanceGuardFilename)
	savedLock := lock + ".saved"
	if err := os.Rename(lock, savedLock); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, maintenance.GuardIdentity.Nonce[:], 0o600); err != nil {
		t.Fatal(err)
	}
	assertDeniedWithoutEffects("replaced guard initialization", func() error { return rejectedInitialization(config) })
	assertDeniedWithoutEffects("replaced guard heartbeat", guardedHeartbeat)
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(savedLock, lock); err != nil {
		t.Fatal(err)
	}
	if err := guardedHeartbeat(); err != nil {
		t.Fatalf("heartbeat after exact guard restoration: %v", err)
	}

	if _, err := app.Exec(ctx, `update public.source_authority_admission set mode='closed' where singleton`); err != nil {
		t.Fatal(err)
	}
	assertDeniedWithoutEffects("closed APP initialization", func() error { return rejectedInitialization(config) })
	assertDeniedWithoutEffects("closed APP guarded heartbeat", guardedHeartbeat)
	// Exercise the SQL effect-time guard through the real signed heartbeat,
	// independently of the outer W2/guard check.
	assertDeniedWithoutEffects("closed APP direct signed heartbeat", func() error { return heartbeatComposeAuthority(ctx, authorityPool, state) })
	if _, err := app.Exec(ctx, `update public.source_authority_admission set mode='active' where singleton`); err != nil {
		t.Fatal(err)
	}

	createRole(recoveryRole, true)
	const recoveryID = "maintenance-test-recovery"
	if err := provisioner.BindPrincipal(ctx, recoveryRole, "recovery", 2, recoveryID); err != nil {
		t.Fatalf("bind recovery principal through provisioner: %v", err)
	}
	var endpointDigest [32]byte
	endpointDigest[0] = 1
	if err := WithSourceMaintenance(ctx, app, controlPath, true, func(_ *SourceMaintenanceConfig) error {
		_, err := provisioner.BeginRecovery(ctx, recoveryID, endpointDigest, recoveryRole)
		return err
	}); err != nil {
		t.Fatalf("enter W2 recovering under exclusive guard: %v", err)
	}
	assertDeniedWithoutEffects("recovering W2 initialization", func() error { return rejectedInitialization(config) })
	assertDeniedWithoutEffects("recovering W2 heartbeat", guardedHeartbeat)
	if _, err := producer.Snapshot(ctx); err == nil {
		t.Fatal("old producer remained admitted after W2 generation change")
	}
}
