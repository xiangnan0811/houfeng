//go:build linux

package deploy

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/platformmigrate"
	"houfeng/internal/center/recordauthority"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/sourceenrollment"
	"houfeng/internal/center/sourceretirement"
	"houfeng/internal/center/store"
	"houfeng/internal/center/store/migrate"
)

// This fixture starts with quiescent, isolated databases and seeds an already
// installed birth lineage as the trusted owner. It exercises the installed
// launch and heartbeat paths; it is not an installation or recovery ceremony,
// and the initializer's runtime probe is not a Center HTTP startup.
func TestPostgresIntegrationComposeInstalledBirthLaunch(t *testing.T) {
	f := newInstalledBirthLaunchFixture(t)
	ctx := f.ctx

	if err := migrate.AdmitAppACLCurrentRuntime(ctx, f.runtime); err != nil {
		t.Fatalf("admit production runtime role: %v", err)
	}
	reader, err := recordbirth.NewPostgresContinuityReader(f.birthReader, f.runtime, f.domain)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := reader.Continuity(ctx, f.binding)
	if err != nil || observation.Binding != f.binding || !observation.Enabled || observation.Anchor != (recordbirth.CheckpointHead{}) || len(observation.Proof) == 0 {
		t.Fatalf("installed W2 continuity read failed or did not match the empty APP checkpoint: %v", err)
	}
	if err := recordbirth.VerifyRuntimeLaunch(ctx, f.runtime, reader, f.pair.center); err != nil {
		t.Fatalf("verify installed runtime launch against real W2: %v", err)
	}
	if err := probeComposeRuntimeLaunch(ctx, f.runtime, f.pair.center, &f.maintenance); err != nil {
		t.Fatalf("probe installed runtime launch: %v", err)
	}
	for _, query := range []string{
		`select 1 from public.source_authority_admission limit 1`,
		`select 1 from record_platform_internal.record_birth_keys limit 1`,
	} {
		_, err := f.authority.Exec(ctx, query)
		var postgresError *pgconn.PgError
		if !errors.As(err, &postgresError) || postgresError.Code != "42501" {
			t.Fatalf("heartbeat principal unexpectedly accessed a private authority table: %v", err)
		}
	}

	centerPath := filepath.Join(f.launchDirectory, composeCenterBirthLaunchFile)
	processorPath := filepath.Join(f.launchDirectory, composeProcessorBirthLaunchFile)
	centerBefore, err := os.Stat(centerPath)
	if err != nil {
		t.Fatal(err)
	}
	processorBefore, err := os.Stat(processorPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeInit := readComposeIntegrationMembership(t, ctx, f.app)
	if err := initializeComposeWithDependencies(ctx, f.config, f.deps); err != nil {
		t.Fatalf("ordinary initializer could not replay installed launch: %v", err)
	}
	centerAfter, err := os.Stat(centerPath)
	if err != nil || !os.SameFile(centerBefore, centerAfter) {
		t.Fatal("installed launch replay replaced the center publication")
	}
	processorAfter, err := os.Stat(processorPath)
	if err != nil || !os.SameFile(processorBefore, processorAfter) {
		t.Fatal("installed launch replay replaced the processor publication")
	}
	registry, err := readComposeBirthLaunchRegistry(ctx, f.owner)
	if err != nil || registry == nil || !registry.opened || registry.revision != f.pair.revision ||
		!bytes.Equal(registry.centerBytes, f.pair.centerBytes) || !bytes.Equal(registry.processorBytes, f.pair.processorBytes) {
		t.Fatalf("installed launch replay changed the registration: %v", err)
	}
	afterInit := readComposeIntegrationMembership(t, ctx, f.app)
	if !afterInit.UpdatedAt.After(beforeInit.UpdatedAt) {
		t.Fatal("installed initializer did not refresh membership")
	}

	beforeHeartbeat := afterInit
	entered, inspected, expiry, err := f.heartbeat()
	if err != nil || !entered || !inspected {
		t.Fatalf("guarded installed launch inspection and heartbeat failed: %v", err)
	}
	afterHeartbeat := readComposeIntegrationMembership(t, ctx, f.app)
	if !expiry.After(beforeHeartbeat.HeartbeatExpiresAt) || !afterHeartbeat.HeartbeatExpiresAt.Equal(expiry) ||
		!afterHeartbeat.UpdatedAt.After(beforeHeartbeat.UpdatedAt) {
		t.Fatal("guarded heartbeat did not renew the actual membership lease")
	}

	t.Run("wrong W2 physical database", func(t *testing.T) {
		// The clone has the same rows and provisioned function ACLs, but PostgreSQL
		// assigned it a different database OID/name. It is a real W2 database,
		// not a substitute response or a manually granted reader.
		f.assertWrongWitnessDatabase(t)
		changed := f.maintenance
		changed.BirthReaderDatabaseURL = f.cloneReaderURL
		f.replaceMaintenance(t, changed)
		f.assertLaunchRejectedWithoutHeartbeat(t)
	})

	t.Run("birth reader unavailable", func(t *testing.T) {
		role := pgx.Identifier{f.birthReaderRole}.Sanitize()
		if _, err := f.witnessAdmin.Exec(ctx, "alter role "+role+" nologin"); err != nil {
			t.Fatalf("disable fixture reader login: %v", err)
		}
		t.Cleanup(func() {
			if _, err := f.witnessAdmin.Exec(context.Background(), "alter role "+role+" login"); err != nil {
				t.Errorf("restore fixture reader login: %v", err)
			}
		})
		f.assertLaunchRejectedWithoutHeartbeat(t)
	})

	t.Run("birth producer cannot act as reader", func(t *testing.T) {
		producer, err := recordbirth.NewPostgresAuthority(f.birthProducer)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := producer.Continuity(ctx, f.binding); !errors.Is(err, recordbirth.ErrUnavailable) {
			t.Fatalf("production birth producer unexpectedly read continuity: %v", err)
		}
		changed := f.maintenance
		changed.BirthReaderDatabaseURL = f.producerURL
		f.replaceMaintenance(t, changed)
		f.assertLaunchRejectedWithoutHeartbeat(t)
	})

	t.Run("W2 anchor is not an APP ancestor", func(t *testing.T) {
		hash := sha256.Sum256([]byte("isolated fixture checkpoint absent from APP"))
		if _, err := f.witness.Exec(ctx, `update public.record_birth_checkpoint_anchor set sequence=1,checkpoint_hash=$1 where singleton`, hash[:]); err != nil {
			t.Fatalf("set fixture W2 anchor: %v", err)
		}
		t.Cleanup(func() {
			if _, err := f.witness.Exec(context.Background(), `update public.record_birth_checkpoint_anchor set sequence=0,checkpoint_hash=$1 where singleton`, make([]byte, 32)); err != nil {
				t.Errorf("restore fixture W2 anchor: %v", err)
			}
		})
		observation, err := reader.Continuity(ctx, f.binding)
		if err != nil || !observation.Enabled || observation.Anchor.Sequence != 1 || observation.Anchor.Hash != hash {
			t.Fatalf("real reader did not return the non-ancestor W2 anchor: %v", err)
		}
		var appHead int64
		if err := f.owner.QueryRow(ctx, `select sequence from record_platform_internal.record_birth_checkpoint_head where singleton`).Scan(&appHead); err != nil || appHead != 0 {
			t.Fatalf("fixture APP checkpoint head is not empty: %v", err)
		}
		f.assertLaunchRejectedWithoutHeartbeat(t)
	})

	t.Run("closed installed registration", func(t *testing.T) {
		if _, err := f.owner.Exec(ctx, `select record_platform_internal.record_birth_launch_close($1::bigint)`, f.pair.revision); err != nil {
			t.Fatalf("close fixture launch: %v", err)
		}
		t.Cleanup(func() {
			if _, err := f.owner.Exec(context.Background(), `select record_platform_internal.record_birth_launch_open($1::bigint)`, f.pair.revision); err != nil {
				t.Errorf("reopen fixture launch: %v", err)
			}
		})
		f.assertLaunchRejectedWithoutHeartbeat(t)
		before := readComposeIntegrationMembership(t, ctx, f.app)
		if err := initializeComposeWithDependencies(ctx, f.config, f.deps); err == nil {
			t.Fatal("ordinary initializer opened a closed installed registration")
		}
		if after := readComposeIntegrationMembership(t, ctx, f.app); !reflect.DeepEqual(before, after) {
			t.Fatal("rejected initializer refreshed membership")
		}
	})

	t.Run("stale registered binding", func(t *testing.T) {
		// Keep the published files and all birth/source state intact while the
		// trusted fixture owner simulates a different registered center image.
		stale := f.pair.center
		stale.BinaryDigest = sha256.Sum256([]byte("different fixture center image"))
		staleBytes, err := recordbirth.EncodeLaunchBinding(stale)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.owner.Exec(ctx, `update record_platform_internal.record_birth_launch_state set center_binding=$1 where singleton`, staleBytes); err != nil {
			t.Fatalf("change fixture launch registration: %v", err)
		}
		t.Cleanup(func() {
			if _, err := f.owner.Exec(context.Background(), `update record_platform_internal.record_birth_launch_state set center_binding=$1 where singleton`, f.pair.centerBytes); err != nil {
				t.Errorf("restore fixture launch registration: %v", err)
			}
		})
		f.assertLaunchRejectedWithoutHeartbeat(t)
	})

	// All negative cases restore their fixture mutations. The original runtime
	// and restricted authority must remain usable without repairing privileges.
	if err := probeComposeRuntimeLaunch(ctx, f.runtime, f.pair.center, &f.maintenance); err != nil {
		t.Fatalf("installed runtime did not recover after fixture restoration: %v", err)
	}
	entered, inspected, _, err = f.heartbeat()
	if err != nil || !entered || !inspected {
		t.Fatalf("installed heartbeat did not recover after fixture restoration: %v", err)
	}
}

type installedBirthLaunchFixture struct {
	ctx             context.Context
	app             *pgxpool.Pool
	owner           *pgxpool.Pool
	runtime         *pgxpool.Pool
	authority       *pgxpool.Pool
	witnessAdmin    *pgxpool.Pool
	witness         *pgxpool.Pool
	birthReader     *pgxpool.Pool
	birthProducer   *pgxpool.Pool
	birthReaderRole string
	cloneReaderURL  string
	producerURL     string
	controlPath     string
	launchDirectory string
	maintenance     SourceMaintenanceConfig
	domain          sourceretirement.DomainIdentity
	binding         recordbirth.Binding
	state           recordauthority.VerifiedComposeState
	pair            composeBirthLaunchPair
	config          ComposeInitConfig
	deps            composeInitDependencies
}

func newInstalledBirthLaunchFixture(t *testing.T) *installedBirthLaunchFixture {
	t.Helper()
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires the strict PostgreSQL integration runner")
	}
	fixtureRoot := os.Getenv("HOUFENG_BIRTH_LAUNCH_FIXTURE_ROOT")
	if fixtureRoot == "" || !filepath.IsAbs(fixtureRoot) || filepath.Clean(fixtureRoot) != fixtureRoot ||
		os.Getuid() != composeLaunchPublisherUID || os.Geteuid() != composeLaunchPublisherUID ||
		os.Getenv("HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER") == "" || os.Getenv("HOUFENG_RECORDS_RUN_ID") == "" ||
		os.Getenv("HOUFENG_DATABASE_URL") == "" || os.Getenv("HOUFENG_DELETION_WITNESS_DATABASE_URL") == "" {
		t.Fatal("installed launch fixture requires UID10002, a protected absolute fixture root, and ownership-checked independent APP/W2 databases")
	}
	groups, err := os.Getgroups()
	if err != nil {
		t.Fatal(err)
	}
	hasRuntimeGroup := os.Getegid() == composeLaunchRuntimeGID
	for _, group := range groups {
		hasRuntimeGroup = hasRuntimeGroup || group == composeLaunchRuntimeGID
	}
	if !hasRuntimeGroup {
		t.Fatal("installed launch publisher requires access to GID10001")
	}
	for _, executable := range []string{composeCenterExecutable, composeProcessorExecutable} {
		if _, err := recordbirth.DigestExecutable(executable); err != nil {
			t.Fatalf("installed launch requires the fixed executable %s: %v", executable, err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	t.Cleanup(cancel)
	directory, err := os.MkdirTemp(fixtureRoot, "birth-launch-installed-pg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	ordinaryDirectory := filepath.Join(directory, "ordinary-center")
	launchDirectory := filepath.Join(directory, "installed-center")
	controlDirectory := filepath.Join(directory, "maintenance")
	for _, dir := range []string{ordinaryDirectory, launchDirectory} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(controlDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	controlPath := filepath.Join(controlDirectory, "source-authority.json")

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
	config.AuthorityStateRoot = filepath.Join(directory, "authority")
	config.CenterDeploymentIDPath = filepath.Join(ordinaryDirectory, "deployment-id")
	if _, err := appAdmin.Exec(ctx, formatComposeIntegrationDDL(t, ctx, appAdmin, `CREATE DATABASE %I`, config.DatabaseName)); err != nil {
		t.Fatalf("create isolated APP database: %v", err)
	}
	t.Cleanup(func() { cleanupComposeIntegrationDatabase(t, context.Background(), appAdmin, config) })
	openApp := func(ctx context.Context, endpoint composePostgresEndpoint) (*pgxpool.Pool, error) {
		connection, err := composeIntegrationEndpointConfig(appConfig, endpoint)
		if err != nil {
			return nil, err
		}
		pool, err := pgxpool.NewWithConfig(ctx, connection)
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
	t.Cleanup(app.Close)
	deps := defaultComposeInitDependencies(openApp)
	// Only the endpoint and control-file location differ from production.
	deps.withMaintenance = func(ctx context.Context, pool *pgxpool.Pool, fn func(*SourceMaintenanceConfig) error) error {
		return WithSourceMaintenance(ctx, pool, controlPath, false, fn)
	}
	if err := initializeComposeWithDependencies(ctx, config, deps); err != nil {
		t.Fatalf("initialize the quiescent ordinary predecessor: %v", err)
	}
	state, err := recordauthority.LoadComposeState(config.AuthorityStateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := platformmigrate.ProvisionPostgresDomainIdentity(ctx, app, platformmigrate.DomainKindApplication); err != nil {
		t.Fatalf("provision isolated APP identity: %v", err)
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
	witnessDBName := "hf_blw_" + suffix
	cloneDBName := "hf_blc_" + suffix
	roles := platformmigrate.SourceAuthorityRoles{
		Owner: "hf_blo_" + suffix, Reader: "hf_blr_" + suffix,
		Producer: "hf_blp_" + suffix, Provisioner: "hf_blc_" + suffix,
		BirthProducer: "hf_blbp_" + suffix, BirthReader: "hf_blbr_" + suffix,
	}
	passwords := map[string]string{
		roles.Owner: "owner-" + suffix, roles.Reader: "source-reader-" + suffix,
		roles.Producer: "source-producer-" + suffix, roles.Provisioner: "source-provisioner-" + suffix,
		roles.BirthProducer: "birth-producer-" + suffix, roles.BirthReader: "birth-reader-" + suffix,
	}
	var createdRoles []string
	if _, err := witnessAdmin.Exec(ctx, "create database "+pgx.Identifier{witnessDBName}.Sanitize()); err != nil {
		t.Fatalf("create isolated W2 database: %v", err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		for _, name := range []string{cloneDBName, witnessDBName} {
			if _, err := witnessAdmin.Exec(cleanup, "drop database if exists "+pgx.Identifier{name}.Sanitize()+" with (force)"); err != nil {
				t.Errorf("drop fixture W2 database: %v", err)
			}
		}
		for _, role := range createdRoles {
			if _, err := witnessAdmin.Exec(cleanup, "drop role "+pgx.Identifier{role}.Sanitize()); err != nil {
				t.Errorf("drop fixture W2 role: %v", err)
			}
		}
	})
	for _, role := range []string{roles.Owner, roles.Reader, roles.Producer, roles.Provisioner, roles.BirthProducer, roles.BirthReader} {
		attribute := "login"
		if role == roles.Owner {
			attribute = "nologin"
		}
		var ddl string
		if err := witnessAdmin.QueryRow(ctx, `select format('create role %I `+attribute+` noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls password %L',$1::text,$2::text)`, role, passwords[role]).Scan(&ddl); err != nil {
			t.Fatal(err)
		}
		if _, err := witnessAdmin.Exec(ctx, ddl); err != nil {
			t.Fatalf("create fixture W2 role: %v", err)
		}
		createdRoles = append(createdRoles, role)
	}
	witnessConfig.ConnConfig.Database = witnessDBName
	witness, err := pgxpool.NewWithConfig(ctx, witnessConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(witness.Close)
	physical, err := platformmigrate.ProvisionSourceAuthority(ctx, witness, roles)
	if err != nil {
		t.Fatalf("provision production W2 source and birth role grants: %v", err)
	}
	domain := sourceretirement.DomainIdentity{ID: physical.ID, SystemIdentifier: physical.PostgresSystemIdentifier,
		DatabaseOID: physical.DatabaseOID, DatabaseName: physical.DatabaseName, Epoch: uint64(physical.IdentityEpoch)}
	var appSystem string
	if err := app.QueryRow(ctx, `select system_identifier::text from pg_control_system()`).Scan(&appSystem); err != nil || appSystem == domain.SystemIdentifier {
		t.Fatalf("APP and W2 must be independent PostgreSQL systems: %v", err)
	}
	openWitness := func(role, database string) *pgxpool.Pool {
		t.Helper()
		c := witnessConfig.Copy()
		c.ConnConfig.Database, c.ConnConfig.User, c.ConnConfig.Password = database, role, passwords[role]
		pool, err := pgxpool.NewWithConfig(ctx, c)
		if err != nil {
			t.Fatal("cannot construct direct W2 fixture connection")
		}
		var session, current string
		if err := pool.QueryRow(ctx, `select session_user,current_user`).Scan(&session, &current); err != nil || session != role || current != role {
			pool.Close()
			t.Fatalf("W2 direct principal authentication failed for %s", role)
		}
		t.Cleanup(pool.Close)
		return pool
	}
	witnessURLFor := func(role, database string) string {
		t.Helper()
		u, err := url.Parse(witnessURL)
		if err != nil {
			t.Fatal("invalid isolated W2 URL")
		}
		u.User = url.UserPassword(role, passwords[role])
		u.Path = "/" + database
		return u.String()
	}
	provisionerPool := openWitness(roles.Provisioner, witnessDBName)
	provisioner, err := sourceretirement.NewAuthority(provisionerPool, domain)
	if err != nil {
		t.Fatal(err)
	}
	maintenance, err := ProvisionSourceMaintenanceConfig(controlPath, domain, witnessURLFor(roles.Reader, witnessDBName))
	if err != nil {
		t.Fatalf("provision fixture source guard: %v", err)
	}
	gate, err := store.NewBootstrapDeploymentMembershipAdmissionGate(store.DeploymentMembershipIdentity{
		InstanceID: "compose-center", DeploymentID: state.DeploymentID, ProjectID: recordplatform.ProjectIDDefault,
		InstanceKind: "api", Capability: "records.runtime",
	})
	if err != nil {
		t.Fatal(err)
	}
	appPasswords := platformmigrate.ComposeRolePasswords{Runtime: config.Passwords.Runtime, PlatformAdmin: config.Passwords.PlatformAdmin,
		DeletionWorker: config.Passwords.DeletionWorker, Migrator: config.Passwords.Migrator, Authority: state.DatabasePassword()}
	enrollment := sourceenrollment.Config{DeploymentID: string(state.DeploymentID), Witness: domain, Roles: config.Roles,
		AuthorityRole: config.AuthorityRole, Passwords: appPasswords,
		Producer: platformmigrate.SourceRoleCredential{Principal: roles.Producer, Password: passwords[roles.Producer]}}
	initialAuthority, err := openApp(ctx, composePostgresEndpoint{Database: config.DatabaseName, Role: config.AuthorityRole, Password: state.DatabasePassword()})
	if err != nil {
		t.Fatal("open direct APP authority for source enrollment")
	}
	defer initialAuthority.Close()
	if err := WithSourceMaintenance(ctx, app, controlPath, true, func(observed *SourceMaintenanceConfig) error {
		if observed == nil || *observed != maintenance {
			return errors.New("fixture source control mismatch")
		}
		if err := heartbeatComposeAuthority(ctx, initialAuthority, state); err != nil {
			return err
		}
		_, err := sourceenrollment.Enroll(ctx, app, witness, provisioner, gate, enrollment)
		return err
	}); err != nil {
		t.Fatalf("enroll isolated source authority: %v", err)
	}
	if err := WithSourceMaintenance(ctx, app, controlPath, false, func(observed *SourceMaintenanceConfig) error {
		return VerifySourceMaintenanceCredentials(ctx, app, observed, config.Roles, config.AuthorityRole, appPasswords)
	}); err != nil {
		t.Fatalf("verify source enrollment credentials: %v", err)
	}

	// Fixture-only trusted-owner inserts model an existing, quiescent installed
	// lineage. They do not prove that enablement, fencing, or recovery ran.
	binding := recordbirth.Binding{AuthorityID: domain.ID, LineageID: "bl-" + strings.Repeat("a", 64),
		DeploymentID: string(state.DeploymentID), ProjectID: "default", Generation: 1,
		KeyID: "bk-" + strings.Repeat("b", 64), CutoverDigest: sha256.Sum256([]byte("installed launch fixture writer cutover"))}
	key := bytes.Repeat([]byte{0x42}, 32)
	keyDigest := sha256.Sum256(key)
	for _, target := range []struct {
		pool   *pgxpool.Pool
		schema string
	}{{app, "record_platform_internal"}, {witness, "public"}} {
		prefix := pgx.Identifier{target.schema}.Sanitize() + "."
		if _, err := target.pool.Exec(ctx, `insert into `+prefix+`record_birth_lineage(singleton,authority_id,lineage_id) values(true,$1,$2)`, binding.AuthorityID, binding.LineageID); err != nil {
			t.Fatalf("seed fixture birth lineage: %v", err)
		}
		if _, err := target.pool.Exec(ctx, `insert into `+prefix+`record_birth_keys(key_id,key_bytes,key_commitment) values($1,$2,$3)`, binding.KeyID, key, keyDigest[:]); err != nil {
			t.Fatalf("seed fixture birth key: %v", err)
		}
		if _, err := target.pool.Exec(ctx, `insert into `+prefix+`record_birth_bindings(generation,authority_id,lineage_id,deployment_id,project_id,key_id,cutover_digest) values($1,$2,$3,$4,$5,$6,$7)`,
			binding.Generation, binding.AuthorityID, binding.LineageID, binding.DeploymentID, binding.ProjectID, binding.KeyID, binding.CutoverDigest[:]); err != nil {
			t.Fatalf("seed fixture birth binding: %v", err)
		}
		if _, err := target.pool.Exec(ctx, `insert into `+prefix+`record_birth_enabled(singleton,generation,key_id,cutover_digest) values(true,$1,$2,$3)`, binding.Generation, binding.KeyID, binding.CutoverDigest[:]); err != nil {
			t.Fatalf("seed fixture enabled birth binding: %v", err)
		}
	}
	if _, err := witness.Exec(ctx, `insert into public.record_birth_principals(principal,purpose,generation) values($1,'producer',$3),($2,'reader',$3)`, roles.BirthProducer, roles.BirthReader, binding.Generation); err != nil {
		t.Fatalf("seed fixture birth principal bindings: %v", err)
	}
	installed := maintenance
	installed.BirthInstalled, installed.BirthLineageID, installed.BirthProtocolVersion = true, binding.LineageID, 1
	installed.BirthReaderDatabaseURL = witnessURLFor(roles.BirthReader, witnessDBName)
	if err := WithMaintenanceGuard(ctx, maintenance.GuardDirectory, maintenance.GuardIdentity, true, func() error {
		return publishSourceMaintenanceConfigReplacement(controlPath, maintenance, installed)
	}); err != nil {
		t.Fatalf("publish fixture installed expectation under the source guard: %v", err)
	}
	maintenance = installed

	// Clone only the isolated, quiescent W2 database for the physical-identity
	// rejection. No ACL is added to the clone; all functions/role ACLs originate
	// in ProvisionSourceAuthority. The original endpoint remains the source guard.
	provisionerPool.Close()
	witness.Close()
	if _, err := witnessAdmin.Exec(ctx, "create database "+pgx.Identifier{cloneDBName}.Sanitize()+" template "+pgx.Identifier{witnessDBName}.Sanitize()); err != nil {
		t.Fatalf("create isolated wrong-identity W2 database: %v", err)
	}
	witness, err = pgxpool.NewWithConfig(ctx, witnessConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(witness.Close)
	birthReader := openWitness(roles.BirthReader, witnessDBName)
	birthProducer := openWitness(roles.BirthProducer, witnessDBName)
	for _, check := range []struct {
		pool                    *pgxpool.Pool
		wantContinuity, reserve bool
	}{{birthReader, true, false}, {birthProducer, false, true}} {
		var continuity, reserve bool
		if err := check.pool.QueryRow(ctx, `select has_function_privilege(session_user,'public.record_birth_continuity(bytea)','execute'),has_function_privilege(session_user,'public.record_birth_reserve(bytea)','execute')`).Scan(&continuity, &reserve); err != nil || continuity != check.wantContinuity || reserve != check.reserve {
			t.Fatalf("production W2 birth role privileges do not match their purpose: %v", err)
		}
	}

	owner, err := openApp(ctx, composePostgresEndpoint{Database: config.DatabaseName, Role: config.Roles.Migrator, Password: config.Passwords.Migrator})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(owner.Close)
	old, err := readComposeBirthLaunchRegistry(ctx, owner)
	if err != nil || old == nil || !old.opened {
		t.Fatalf("read ordinary predecessor launch: %v", err)
	}
	if _, err := owner.Exec(ctx, `select record_platform_internal.record_birth_launch_close($1::bigint)`, old.revision); err != nil {
		t.Fatalf("close ordinary fixture launch: %v", err)
	}
	pair, err := expectedComposeBirthLaunch(ctx, owner, state, &maintenance)
	if err != nil || !pair.center.BirthInstalled || pair.center.BirthBinding != binding || pair.center.Witness != domain {
		t.Fatalf("construct installed fixture launch from actual authority and executables: %v", err)
	}
	if err := owner.QueryRow(ctx, `select record_platform_internal.record_birth_launch_stage($1::bytea,$2::bytea,$3::name,$4::name,$5::bigint)`,
		pair.centerBytes, pair.processorBytes, config.Roles.CenterRuntime, config.Roles.DeletionWorker, old.revision).Scan(&pair.revision); err != nil || pair.revision != old.revision+1 {
		t.Fatalf("stage fixture installed launch: %v", err)
	}
	if err := publishComposeBirthLaunchPair(launchDirectory, pair); err != nil {
		t.Fatalf("publish protected installed launch pair: %v", err)
	}
	if _, err := inspectPublishedComposeBirthLaunch(ctx, owner, state, &maintenance, launchDirectory, false); err != nil {
		t.Fatalf("inspect staged fixture launch against real W2: %v", err)
	}
	if _, err := owner.Exec(ctx, `select record_platform_internal.record_birth_launch_open($1::bigint)`, pair.revision); err != nil {
		t.Fatalf("open fixture installed launch: %v", err)
	}
	config.CenterDeploymentIDPath = filepath.Join(launchDirectory, "deployment-id")
	runtime, err := openApp(ctx, composePostgresEndpoint{Database: config.DatabaseName, Role: config.Roles.CenterRuntime,
		Password: config.Passwords.Runtime, LaunchDigest: pair.centerDigest})
	if err != nil {
		t.Fatal("open direct launch-bound APP runtime")
	}
	t.Cleanup(runtime.Close)
	authority, err := openApp(ctx, composePostgresEndpoint{Database: config.DatabaseName, Role: config.AuthorityRole, Password: state.DatabasePassword()})
	if err != nil {
		t.Fatal("open direct APP heartbeat authority")
	}
	t.Cleanup(authority.Close)
	return &installedBirthLaunchFixture{ctx: ctx, app: app, owner: owner, runtime: runtime, authority: authority,
		witnessAdmin: witnessAdmin, witness: witness, birthReader: birthReader, birthProducer: birthProducer,
		birthReaderRole: roles.BirthReader, cloneReaderURL: witnessURLFor(roles.BirthReader, cloneDBName),
		producerURL: witnessURLFor(roles.BirthProducer, witnessDBName), controlPath: controlPath,
		launchDirectory: launchDirectory, maintenance: maintenance, domain: domain, binding: binding,
		state: state, pair: pair, config: config, deps: deps}
}

// heartbeat mirrors the guarded inspection and signed heartbeat sequence in
// RunComposeAuthority, using the isolated fixture paths and real role pools.
func (f *installedBirthLaunchFixture) heartbeat() (entered, inspected bool, expiry time.Time, err error) {
	err = WithSourceMaintenanceAuthority(f.ctx, f.authority, f.controlPath, func(maintenance *SourceMaintenanceConfig) error {
		entered = true
		current, err := recordauthority.LoadComposeState(f.config.AuthorityStateRoot)
		if err != nil || !sameVerifiedComposeAuthorityState(current, f.state) {
			return ErrComposeAuthorityLoadState
		}
		if _, err := inspectPublishedComposeBirthLaunch(f.ctx, f.authority, current, maintenance, f.launchDirectory, true); err != nil {
			return err
		}
		inspected = true
		expiry, err = heartbeatComposeAuthorityAt(f.ctx, f.authority, current, time.Now().UTC().Add(5*time.Second))
		return err
	})
	return
}

func (f *installedBirthLaunchFixture) replaceMaintenance(t *testing.T, next SourceMaintenanceConfig) {
	t.Helper()
	replace := func(old, replacement SourceMaintenanceConfig) error {
		return WithMaintenanceGuard(f.ctx, old.GuardDirectory, old.GuardIdentity, true, func() error {
			return publishSourceMaintenanceConfigReplacement(f.controlPath, old, replacement)
		})
	}
	if err := replace(f.maintenance, next); err != nil {
		t.Fatalf("replace isolated fixture control: %v", err)
	}
	t.Cleanup(func() {
		if err := replace(next, f.maintenance); err != nil {
			t.Errorf("restore isolated fixture control: %v", err)
		}
	})
}

func (f *installedBirthLaunchFixture) assertLaunchRejectedWithoutHeartbeat(t *testing.T) {
	t.Helper()
	current, err := LoadSourceMaintenanceConfig(f.controlPath)
	if err != nil {
		t.Fatalf("load fixture control: %v", err)
	}
	before := readComposeIntegrationMembership(t, f.ctx, f.app)
	if err := probeComposeRuntimeLaunch(f.ctx, f.runtime, f.pair.center, &current); err == nil {
		t.Fatal("runtime accepted the invalid installed launch condition")
	}
	entered, inspected, _, err := f.heartbeat()
	if err == nil || !entered || inspected {
		t.Fatalf("heartbeat did not reject at installed launch inspection (entered=%t inspected=%t): %v", entered, inspected, err)
	}
	if after := readComposeIntegrationMembership(t, f.ctx, f.app); !reflect.DeepEqual(before, after) {
		t.Fatal("rejected runtime or heartbeat changed membership")
	}
}

func (f *installedBirthLaunchFixture) assertWrongWitnessDatabase(t *testing.T) {
	t.Helper()
	config, err := pgxpool.ParseConfig(f.cloneReaderURL)
	if err != nil {
		t.Fatal("invalid cloned W2 reader configuration")
	}
	pool, err := pgxpool.NewWithConfig(f.ctx, config)
	if err != nil {
		t.Fatal("cannot open cloned W2 reader")
	}
	defer pool.Close()
	var session, current string
	var databaseOID uint32
	var canRead bool
	if err := pool.QueryRow(f.ctx, `select session_user,current_user,(select oid from pg_database where datname=current_database()),has_function_privilege(session_user,'public.record_birth_continuity(bytea)','execute')`).Scan(&session, &current, &databaseOID, &canRead); err != nil || session != f.birthReaderRole || current != session || databaseOID == f.domain.DatabaseOID || !canRead {
		t.Fatalf("wrong-identity database did not preserve the real reader role and function ACL: %v", err)
	}
	command, err := json.Marshal(map[string]any{
		"authority_id": f.binding.AuthorityID, "lineage_id": f.binding.LineageID,
		"deployment_id": f.binding.DeploymentID, "project_id": f.binding.ProjectID,
		"generation": f.binding.Generation, "key_id": f.binding.KeyID, "cutover_digest": hex.EncodeToString(f.binding.CutoverDigest[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	var proof []byte
	err = pool.QueryRow(f.ctx, `select proof from public.record_birth_continuity($1::bytea)`, command).Scan(&proof)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || postgresError.Code != "42501" || postgresError.Message != "birth authority admission rejected" {
		t.Fatalf("wrong physical W2 database did not reject the otherwise provisioned reader: %v", err)
	}
}
