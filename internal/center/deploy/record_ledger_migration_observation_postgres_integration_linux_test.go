//go:build linux

package deploy

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sys/unix"

	"houfeng/internal/center/store/migrate"
)

// This double only represents the coordinator's synchronous checkpoint seam.
// It does not model or establish root, cgroup, pidfd, or guest authority.
type migrationPGCheckpointer struct {
	calls  []ledgerMaintenanceCheckpoint
	deny   string
	before func(context.Context, ledgerMaintenanceCheckpoint) error
}

func (c *migrationPGCheckpointer) Checkpoint(ctx context.Context, request ledgerMaintenanceCheckpoint) (ledgerMaintenanceACK, error) {
	if err := ctx.Err(); err != nil {
		return ledgerMaintenanceACK{}, err
	}
	c.calls = append(c.calls, request)
	if c.before != nil {
		if err := c.before(ctx, request); err != nil {
			return ledgerMaintenanceACK{}, err
		}
	}
	if request.Kind == c.deny {
		return ledgerMaintenanceACK{}, errors.New("test checkpoint refused")
	}
	return ledgerMaintenanceACK{WitnessSequence: uint64(len(c.calls)), WitnessDigest: strings.Repeat("a", 64)}, nil
}

func (c *migrationPGCheckpointer) ReadyDisposition(context.Context, ledgerMaintenanceCheckpoint) (ledgerMaintenanceReadyDisposition, error) {
	return ledgerMaintenanceReadyDisposition{}, ErrRecordLedgerFence
}

func (c *migrationPGCheckpointer) Healthy() bool { return c.deny == "" }

func migrationPGControl(t *testing.T) int {
	t.Helper()
	fd, err := unix.Open(t.TempDir(), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	return fd
}

func migrationPGPublish(t *testing.T, fd int, name string, record any) {
	t.Helper()
	if _, err := ledgerPublishExact(fd, name, record); err != nil {
		t.Fatalf("publish %s: %v", name, err)
	}
}

func migrationPGRequireFourDomains(t *testing.T, ctx context.Context) {
	t.Helper()
	seen := make(map[string]bool)
	for _, name := range []string{"HOUFENG_DATABASE_URL", "HOUFENG_DELETION_LEDGER_DATABASE_URL", "HOUFENG_DELETION_WITNESS_DATABASE_URL", "HOUFENG_RECOVERY_CONTROL_DATABASE_URL"} {
		conn, err := pgx.Connect(ctx, os.Getenv(name))
		if err != nil {
			t.Fatalf("open isolated domain %s: %v", name, err)
		}
		var system, session, current string
		var version int
		err = conn.QueryRow(ctx, `select system_identifier::text,pg_catalog.current_setting('server_version_num')::int,session_user,current_user from pg_catalog.pg_control_system()`).Scan(&system, &version, &session, &current)
		_ = conn.Close(ctx)
		if err != nil || system == "" || seen[system] || version/10000 != 16 || session != "postgres" || current != session {
			t.Fatalf("domain %s is not an independent direct PG16 bootstrap: %v", name, err)
		}
		seen[system] = true
		t.Logf("%s PostgreSQL server_version_num=%d", name, version)
	}
}

// This exercises actual C80 and C81 PostgreSQL catalogs and role transitions.
// The isolated witness double does not replace the root/guest acceptance lane.
func TestPostgresIntegrationLegacyC81FreshObservationRecovery(t *testing.T) {
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires the isolated PostgreSQL integration runner")
	}
	for _, name := range []string{"HOUFENG_RECORD_PLATFORM_EPHEMERAL_OWNER", "HOUFENG_RECORDS_RUN_ID", "HOUFENG_DATABASE_URL", "HOUFENG_DELETION_LEDGER_DATABASE_URL", "HOUFENG_DELETION_WITNESS_DATABASE_URL", "HOUFENG_RECOVERY_CONTROL_DATABASE_URL"} {
		if os.Getenv(name) == "" {
			t.Fatalf("missing isolated four-domain fixture %s", name)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	migrationPGRequireFourDomains(t, ctx)
	controlPool, err := pgxpool.New(ctx, os.Getenv("HOUFENG_RECOVERY_CONTROL_DATABASE_URL"))
	if err != nil || controlPool.Ping(ctx) != nil {
		t.Fatal("open isolated recovery-control PostgreSQL")
	}
	t.Cleanup(controlPool.Close)
	var workerExists bool
	if controlPool.QueryRow(ctx, `select exists(select 1 from pg_catalog.pg_roles where rolname=$1)`, composeDeletionWorkerRole).Scan(&workerExists) != nil || workerExists {
		t.Fatal("the isolated cluster already contains the fixed migration worker")
	}
	workerOwned := false
	// Registered before the fixture so its database is dropped before this role.
	t.Cleanup(func() {
		if !workerOwned {
			return
		}
		cleanup, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		if _, err := controlPool.Exec(cleanup, "drop role if exists "+pgx.Identifier{composeDeletionWorkerRole}.Sanitize()); err != nil {
			t.Errorf("drop isolated migration worker: %v", err)
		}
	})
	binary := buildC80EffectsFixture(t, ctx)
	db := newC80EffectsDatabase(t, ctx, binary)
	appConfig := db.app.Config().Copy()
	appConfig.MaxConns = 1
	db.app.Close()
	app, err := pgxpool.NewWithConfig(ctx, appConfig)
	if err != nil || app.Ping(ctx) != nil {
		t.Fatal("open single bootstrap connection to pinned C80")
	}
	t.Cleanup(app.Close)

	c80, err := migrate.ReadAppACLMaintenanceSnapshot(ctx, app, nil)
	if err != nil || !c80.LegacyC80 || c80.Manifest.Head == nil || len(c80.Manifest.Manifests) == 0 {
		t.Fatalf("read real pinned C80 catalog: %v", err)
	}
	migratorRole := c80.Manifest.Manifests[len(c80.Manifest.Manifests)-1].MigratorCatalogRole
	request := RecordLedgerMaintenanceRequest{Compose: ComposeOfflineFenceRequest{RecoveryID: "observation-test"}, AppIdentity: db.identity,
		AppAdminRole: db.adminRole, AppPreviousLogins: []string{db.runtimeRole, migratorRole}}
	original, err := ledgerAppPrincipalInventory(ctx, app, request, "postgres")
	if err != nil || !original.LegacyC80 {
		t.Fatalf("derive exact C80 principals: %v", err)
	}
	original.RoleBaseline, err = ledgerReadAppRoleSnapshot(ctx, app, ledgerAppRoleNames(original))
	if err != nil || ledgerValidateOriginalRoles(original) != nil {
		t.Fatalf("read complete C80 role snapshot: %v", err)
	}
	roles := append(append([]string{}, original.Previous...), original.Admin)
	sort.Strings(roles)
	for _, role := range roles {
		secret, verifier, err := ledgerMigrationSecret()
		if err != nil {
			t.Fatal(err)
		}
		original.Targets = append(original.Targets, ledgerAppRoleTarget{Role: role, Secret: secret, Verifier: verifier})
		if role == original.Admin {
			old, ok := ledgerAppRoleByName(original.RoleBaseline, role)
			if !ok || old.Verifier == nil {
				t.Fatal("missing original administrator verifier")
			}
			original.Credential = ledgerAdminCredential{Version: 1, RecoveryID: original.RecoveryID, Role: role,
				NewURL: ledgerRoleTestURL(appConfig, db.identity.DatabaseName, role, secret), PreviousVerifier: *old.Verifier, NewVerifier: verifier}
		}
	}
	if ledgerValidateAppJournal(original, original.Credential) != nil {
		t.Fatal("invalid real C80 inventory")
	}
	fenced := ledgerAppExpectedRoles(original, ledgerAppTarget)
	if err := ledgerPredecessorApplyRoles(ctx, app, original, original.RoleBaseline, fenced); err != nil {
		t.Fatalf("fence original C80 roles: %v", err)
	}
	bootstrap, err := ledgerCurrentAppSession(ctx, app)
	if err != nil {
		t.Fatal(err)
	}
	intent := ledgerMigrationIntent{Version: 1, PhaseGeneration: 2, PreviousPhaseGeneration: 1, RecoveryID: original.RecoveryID,
		Application: original.Application, RuntimeRole: db.runtimeRole, AdminRole: original.Admin, MigratorRole: migratorRole,
		WorkerRole: composeDeletionWorkerRole, WorkerAbsent: true, ProgramSHA256: strings.Repeat("a", 64),
		Bootstrap: ledgerPredecessorBackend{PID: bootstrap.PID, Started: bootstrap.Started, Role: bootstrap.Role}}
	control := migrationPGControl(t)
	checkpoint := &migrationPGCheckpointer{}
	checkpoint.before = func(c context.Context, request ledgerMaintenanceCheckpoint) error {
		if request.Kind != "preparation" {
			return nil
		}
		var exists bool
		if err := controlPool.QueryRow(c, `select exists(select 1 from pg_catalog.pg_roles where rolname=$1)`, composeDeletionWorkerRole).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return errors.New("worker was visible before preparation checkpoint")
		}
		return nil
	}
	workerOwned = true
	preparation, migratorURL, err := ledgerMigrationPrepareRoles(ctx, app, control, intent, original, "127.0.0.1", checkpoint)
	checkpoint.before = nil
	if err != nil {
		t.Fatalf("prepare roles before C81: %v", err)
	}
	if len(checkpoint.calls) != 1 || checkpoint.calls[0].Kind != "preparation" {
		t.Fatal("preparation did not require the checkpoint seam")
	}
	parsed, err := url.Parse(migratorURL)
	if err != nil || parsed.User == nil {
		t.Fatal("invalid prepared direct migrator URL")
	}
	password, ok := parsed.User.Password()
	if !ok {
		t.Fatal("prepared migrator credential missing")
	}
	migratorConfig := appConfig.Copy()
	migratorConfig.ConnConfig.User, migratorConfig.ConnConfig.Password = migratorRole, password
	migrator, err := pgxpool.NewWithConfig(ctx, migratorConfig)
	if err != nil || migrator.Ping(ctx) != nil {
		t.Fatal("open direct prepared migrator")
	}
	var migrationBackend ledgerPredecessorBackend
	if migrator.QueryRow(ctx, `select pid,backend_start,usename::text from pg_catalog.pg_stat_activity where pid=pg_catalog.pg_backend_pid()`).Scan(&migrationBackend.PID, &migrationBackend.Started, &migrationBackend.Role) != nil {
		t.Fatal("read real migrator backend")
	}
	successor, err := migrate.ConvergeAppACLCurrent(ctx, migrator, db.runtimeRole, db.adminRole, composeDeletionWorkerRole)
	migrator.Close()
	if err != nil || successor.PreviousManifestDigest != original.ManifestDigest {
		t.Fatalf("commit real C80 to C81 convergence: %v", err)
	}
	var c81 bool
	if app.QueryRow(ctx, `select exists(select 1 from public.schema_migrations where name='0081_record_deletion_successor.sql')`).Scan(&c81) != nil || !c81 {
		t.Fatal("C81 migration did not commit")
	}
	// Capture an actual original normal solely in a separate selection fixture.
	// The recovery journal under test intentionally has no normal file.
	originalNormal, err := ledgerMigrationObserveNormal(ctx, app, intent, preparation, original)
	if err != nil || originalNormal.Inventory.ManifestDigest != successor.ManifestDigest {
		t.Fatalf("observe actual original normal: %v", err)
	}
	restriction, err := ledgerMigrationRestrict(ctx, app, control, intent, original, &preparation, checkpoint)
	if err != nil || restriction == nil {
		t.Fatalf("refence committed C81: %v", err)
	}
	if _, found, err := fenceReadFile(control, ledgerMigrationName("normal", intent.PhaseGeneration)); err != nil || found {
		t.Fatal("lost-normal recovery journal unexpectedly has normal")
	}
	catalog, rolesFenced, err := ledgerMigrationFencedCatalog(ctx, app, intent, &preparation, original)
	if err != nil || catalog.LegacyC80 || !reflect.DeepEqual(rolesFenced, preparation.FinalTarget) {
		t.Fatalf("read exact fenced C81: %v", err)
	}

	t.Run("original normal selection uses captured catalog", func(t *testing.T) {
		fd := migrationPGControl(t)
		intentDigest, _ := ledgerMigrationDigest(intent)
		ready := ledgerMigrationReady{Version: 1, PhaseGeneration: intent.PhaseGeneration, IntentDigest: intentDigest,
			Child: ledgerPredecessorProcess{PID: os.Getpid(), Starttime: 1, ExecutableSHA256: intent.ProgramSHA256}, Backend: migrationBackend}
		// The ready tuple above is synthetic journal metadata, not child or root evidence.
		migrationPGPublish(t, fd, ledgerMigrationName("preparation", intent.PhaseGeneration), preparation)
		migrationPGPublish(t, fd, ledgerMigrationName("ready", intent.PhaseGeneration), ready)
		migrationPGPublish(t, fd, ledgerMigrationName("normal", intent.PhaseGeneration), originalNormal)
		migrationPGPublish(t, fd, ledgerMigrationName("restrict", intent.PhaseGeneration), *restriction)
		migrationPGPublish(t, fd, ledgerMigrationName("successor", intent.PhaseGeneration), originalNormal.Inventory)
		normalDigest, _ := ledgerMigrationDigest(originalNormal)
		successorDigest, _ := ledgerMigrationDigest(originalNormal.Inventory)
		activation := ledgerMigrationActivation{NormalSource: "original", NormalAttempt: 0, NormalDigest: normalDigest, SuccessorDigest: successorDigest, ManifestDigest: hexDigest(originalNormal.Inventory.ManifestDigest)}
		selected, err := ledgerMigrationSelectedInventory(fd, intent, activation, original)
		if err != nil || !reflect.DeepEqual(selected, originalNormal.Inventory) {
			t.Fatalf("select actual original normal: %v", err)
		}
		activation.NormalAttempt = 1
		if _, err := ledgerMigrationSelectedInventory(fd, intent, activation, original); err == nil {
			t.Fatal("original normal accepted a nonzero observation attempt")
		}
	})

	migrationPGRejectDrift(t, ctx, app, intent, preparation, original)
	work, cancelWork, err := ledgerMaintenanceWorkContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelWork()
	cleanup, cancelCleanup, err := ledgerMaintenanceCallbackCleanupContext(work)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelCleanup()
	f := &RecordLedgerFence{app: app, control: control, maintenanceCheckpointer: checkpoint}

	// Publish an acknowledged intent, then model interruption before its grant.
	// Re-entry must settle the original record without granting its credentials.
	to, err := ledgerMigrationFreshRoles(preparation, original, nil)
	if err != nil {
		t.Fatal(err)
	}
	intentDigest, _ := ledgerMigrationDigest(intent)
	preparationDigest, _ := ledgerMigrationDigest(preparation)
	catalogDigest, _ := ledgerMigrationDigest(catalog.Catalog)
	worker, found := ledgerAppRoleByName(preparation.Normal, composeDeletionWorkerRole)
	if !found {
		t.Fatal("missing real worker")
	}
	observation := ledgerMigrationObservationIntent{Version: 1, PhaseGeneration: intent.PhaseGeneration, Attempt: 1,
		RecoveryID: intent.RecoveryID, Application: intent.Application, IntentDigest: intentDigest, PreparationDigest: preparationDigest,
		Bootstrap: intent.Bootstrap, WorkerOID: worker.OID, ManifestDigest: hexDigest(catalog.Manifest.Head.ManifestDigest), CatalogDigest: catalogDigest,
		From: rolesFenced, To: to, FinalTarget: preparation.FinalTarget, EmergencyTarget: preparation.EmergencyTarget}
	if ledgerMigrationValidateObservationIntent(observation, intent, preparation, original) != nil {
		t.Fatal("invalid actual-C81 observation intent")
	}
	if err := ledgerMigrationPublishObservation(work, f, intent, 1, "intent", observation); err != nil {
		t.Fatal(err)
	}
	if err := ledgerMigrationSettleObservation(work, cleanup, f, intent, preparation, original, observation); err != nil {
		t.Fatalf("settle interrupted pre-grant attempt: %v", err)
	}
	var settled ledgerMigrationObservationSettled
	if found, err := ledgerMigrationReadObservation(control, "settled", intent.PhaseGeneration, 1, &settled); err != nil || !found || settled.Outcome != "grant_not_committed" || settled.NormalDigest != nil {
		t.Fatalf("no-grant recovery outcome = %+v, err=%v", settled, err)
	}
	afterNoGrant, err := ledgerReadAppRoleSnapshot(ctx, app, ledgerAppRoleNames(ledgerMigrationRestrictionInventory(original, preparation, to)))
	if err != nil || !reflect.DeepEqual(afterNoGrant, rolesFenced) {
		t.Fatal("interrupted attempt changed the fenced role state")
	}
	settledDigest, err := ledgerMigrationDigest(settled)
	if err != nil {
		t.Fatal(err)
	}
	normalObserved := false
	checkpoint.before = func(c context.Context, request ledgerMaintenanceCheckpoint) error {
		if request.Kind != "observe-normal" {
			return nil
		}
		for _, prepared := range preparation.Normal.Roles {
			if prepared.Name == original.Bootstrap {
				continue
			}
			var oid uint32
			var login, connect bool
			var verifier *string
			if err := app.QueryRow(c, `select oid,rolcanlogin,pg_catalog.has_database_privilege(oid,pg_catalog.current_database(),'CONNECT'),rolpassword from pg_catalog.pg_authid where rolname=$1`, prepared.Name).Scan(&oid, &login, &connect, &verifier); err != nil {
				return err
			}
			prior, found := ledgerAppRoleByName(observation.To, prepared.Name)
			expectedConnect := prepared.Connect || prepared.Name == composeDeletionWorkerRole
			if !found || oid != prepared.OID || login != prepared.Login || connect != expectedConnect || verifier == nil || reflect.DeepEqual(verifier, prepared.Verifier) || reflect.DeepEqual(verifier, prior.Verifier) {
				return fmt.Errorf("actual normal role mismatch: %s", prepared.Name)
			}
		}
		normalObserved = true
		return nil
	}
	fresh, err := ledgerMigrationFreshObservation(work, cleanup, f, intent, preparation, original, catalog, rolesFenced, []ledgerMigrationObservationIntent{observation}, &settledDigest)
	checkpoint.before = nil
	if err != nil {
		t.Fatalf("fresh grant, observation and refence: %v", err)
	}
	if !normalObserved {
		t.Fatal("fresh normal was not independently read from PostgreSQL before refencing")
	}
	if fresh.Attempt != 2 || fresh.Inventory.ManifestDigest != successor.ManifestDigest {
		t.Fatal("fresh normal did not bind the committed C81 successor")
	}
	selected, err := ledgerMigrationSelectObservation(control, intent, preparation, original, 2)
	if err != nil || !reflect.DeepEqual(selected, fresh) {
		t.Fatalf("select settled fresh normal: %v", err)
	}
	actualFinal, err := ledgerReadAppRoleSnapshot(ctx, app, ledgerAppRoleNames(fresh.Inventory))
	if err != nil || !reflect.DeepEqual(actualFinal, preparation.FinalTarget) {
		t.Fatal("fresh observation did not refence the actual database")
	}
	for _, role := range fresh.Inventory.RoleBaseline.Roles {
		if role.Name == original.Bootstrap {
			continue
		}
		prepared, ok1 := ledgerAppRoleByName(preparation.Normal, role.Name)
		prior, ok2 := ledgerAppRoleByName(observation.To, role.Name)
		if !ok1 || !ok2 || role.Verifier == nil || reflect.DeepEqual(role.Verifier, prepared.Verifier) || reflect.DeepEqual(role.Verifier, prior.Verifier) {
			t.Fatalf("fresh verifier was not rotated for %s", role.Name)
		}
	}
	if _, found, err := fenceReadFile(control, ledgerMigrationName("normal", intent.PhaseGeneration)); err != nil || found {
		t.Fatal("fresh observation fabricated the original normal record")
	}

	// A refused intent ACK must leave no business roles enabled. The separate
	// control directory avoids changing the completed observation lineage.
	t.Run("checkpoint refusal prevents fresh grant", func(t *testing.T) {
		fd := migrationPGControl(t)
		refuse := &migrationPGCheckpointer{deny: "observe-intent"}
		failedFence := &RecordLedgerFence{app: app, control: fd, maintenanceCheckpointer: refuse}
		currentCatalog, currentRoles, err := ledgerMigrationFencedCatalog(work, app, intent, &preparation, original)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ledgerMigrationFreshObservation(work, cleanup, failedFence, intent, preparation, original, currentCatalog, currentRoles, nil, nil); err == nil {
			t.Fatal("fresh grant proceeded after refused intent ACK")
		}
		actual, err := ledgerReadAppRoleSnapshot(cleanup, app, ledgerAppRoleNames(fresh.Inventory))
		if err != nil || !reflect.DeepEqual(actual, preparation.EmergencyTarget) {
			t.Fatal("checkpoint refusal did not leave exact emergency closure")
		}
		if len(refuse.calls) != 1 || refuse.calls[0].Kind != "observe-intent" {
			t.Fatal("unexpected checkpoint sequence after refusal")
		}
	})
}

func migrationPGRejectDrift(t *testing.T, ctx context.Context, app *pgxpool.Pool, intent ledgerMigrationIntent, preparation ledgerMigrationPreparation, original ledgerAppInventory) {
	t.Helper()
	runtime := pgx.Identifier{intent.RuntimeRole}.Sanitize()
	t.Run("role attribute drift", func(t *testing.T) {
		role, ok := ledgerAppRoleByName(preparation.FinalTarget, intent.RuntimeRole)
		if !ok {
			t.Fatal("missing runtime role")
		}
		if _, err := app.Exec(ctx, "alter role "+runtime+" connection limit 7"); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := app.Exec(context.Background(), fmt.Sprintf("alter role %s connection limit %d", runtime, role.ConnLimit)); err != nil {
				t.Errorf("restore runtime connection limit: %v", err)
			}
		}()
		if _, _, err := ledgerMigrationFencedCatalog(ctx, app, intent, &preparation, original); err == nil {
			t.Fatal("accepted runtime role attribute drift")
		}
	})
	t.Run("verifier drift", func(t *testing.T) {
		role, ok := ledgerAppRoleByName(preparation.FinalTarget, intent.RuntimeRole)
		if !ok || role.Verifier == nil {
			t.Fatal("missing fenced runtime verifier")
		}
		_, verifier, err := ledgerMigrationSecret()
		if err != nil {
			t.Fatal(err)
		}
		ledgerRoleTestSetVerifier(t, ctx, app, intent.RuntimeRole, verifier)
		defer ledgerRoleTestSetVerifier(t, context.Background(), app, intent.RuntimeRole, *role.Verifier)
		if _, _, err := ledgerMigrationFencedCatalog(ctx, app, intent, &preparation, original); err == nil {
			t.Fatal("accepted runtime verifier drift")
		}
	})
	t.Run("worker OID drift", func(t *testing.T) {
		old, ok := ledgerAppRoleByName(preparation.Normal, composeDeletionWorkerRole)
		if !ok {
			t.Fatal("missing worker")
		}
		backup := "hf_observation_worker_" + original.Application.DatabaseName[len(original.Application.DatabaseName)-16:]
		var exists bool
		if app.QueryRow(ctx, `select exists(select 1 from pg_catalog.pg_roles where rolname=$1)`, backup).Scan(&exists) != nil || exists {
			t.Fatal("worker backup role collision")
		}
		workerName, backupName := pgx.Identifier{composeDeletionWorkerRole}.Sanitize(), pgx.Identifier{backup}.Sanitize()
		if _, err := app.Exec(ctx, "alter role "+workerName+" rename to "+backupName); err != nil {
			t.Fatal(err)
		}
		replacement := false
		defer func() {
			if replacement {
				if _, err := app.Exec(context.Background(), "drop role "+workerName); err != nil {
					t.Errorf("drop replacement worker: %v", err)
					return
				}
			}
			if _, err := app.Exec(context.Background(), "alter role "+backupName+" rename to "+workerName); err != nil {
				t.Errorf("restore worker name: %v", err)
			}
		}()
		if _, err := app.Exec(ctx, "create role "+workerName+" nologin noinherit"); err != nil {
			t.Fatal(err)
		}
		replacement = true
		var oid uint32
		if app.QueryRow(ctx, `select oid from pg_catalog.pg_roles where rolname=$1`, composeDeletionWorkerRole).Scan(&oid) != nil || oid == old.OID {
			t.Fatal("replacement worker did not have a different OID")
		}
		if _, _, err := ledgerMigrationFencedCatalog(ctx, app, intent, &preparation, original); err == nil {
			t.Fatal("accepted a replacement worker OID")
		}
	})
}
