package migrate

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
)

const appACLCurrentP79Migration = "0079_ip_quality_cache_expiry.sql"
const appACLCurrentCacheExpiryIndexDefinition = "CREATE INDEX idx_ip_quality_source_cache_expires_at ON public.ip_quality_source_cache USING btree (expires_at)"

func TestPostgresIntegrationIPQualityCacheExpiryMigration(t *testing.T) {
	t.Run("fresh_c80", testPostgresIntegrationAppACLCurrentP78Fresh)
	for _, chained := range []bool{false, true} {
		name := "independent_c78_genesis"
		if chained {
			name = "released_c76_c77_c78_history"
		}
		t.Run(name, func(t *testing.T) {
			testPostgresIntegrationAppACLCurrentP78Upgrade(t, chained)
		})
	}
	for _, cutpoint := range []string{"after_migration", "after_manifest_head"} {
		t.Run("rollback_"+cutpoint, func(t *testing.T) {
			testPostgresIntegrationAppACLCurrentP78Rollback(t, cutpoint)
		})
	}
	for _, current := range []bool{false, true} {
		phase := "predecessor_"
		if current {
			phase = "current_"
		}
		for _, drift := range []string{"missing_delete", "runtime_extra_delete", "admin_select", "public_select", "grant_option", "ledger_checksum"} {
			t.Run(phase+drift, func(t *testing.T) {
				testPostgresIntegrationAppACLCurrentP78Drift(t, current, drift)
			})
		}
	}
}

func assertAppACLCurrentCacheExpiryIndex(t *testing.T, ctx context.Context, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, present bool) {
	t.Helper()
	var definition *string
	if err := db.QueryRow(ctx, `select pg_get_indexdef(to_regclass('public.idx_ip_quality_source_cache_expires_at'))`).Scan(&definition); err != nil {
		t.Fatal(err)
	}
	if !present {
		if definition != nil {
			t.Fatalf("predecessor or rolled-back migration contains expiry index: %s", *definition)
		}
		return
	}
	if definition == nil || *definition != appACLCurrentCacheExpiryIndexDefinition {
		t.Fatalf("expiry index definition = %v, want %s", definition, appACLCurrentCacheExpiryIndexDefinition)
	}
}

func testPostgresIntegrationAppACLCurrentP78Fresh(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	db := fixture.openRolePool(t, ctx, fixture.migratorRole)
	runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	manifest, err := ConvergeAppACLCurrent(ctx, db, fixture.runtimeRole, fixture.adminRole)
	if err != nil || manifest.ManifestRevision != 1 {
		t.Fatalf("fresh C79 convergence = %#v/%v", manifest, err)
	}
	assertAppACLCurrentCacheExpiryIndex(t, ctx, db, true)
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
		t.Fatalf("fresh C79 runtime admission: %v", err)
	}
	_, _, input := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input)
	if len(before.Ledger) != 81 || before.Ledger[80].Name != appACLCurrentP80Migration {
		t.Fatalf("fresh current must end in exactly 0080: %#v", before.Ledger)
	}
	repeated, err := ConvergeAppACLCurrent(ctx, db, fixture.runtimeRole, fixture.adminRole)
	if err != nil || repeated.ManifestDigest != manifest.ManifestDigest ||
		!reflect.DeepEqual(readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input), before) {
		t.Fatalf("fresh C79 repeat changed durable state: %v", err)
	}
	assertAppACLCurrentCacheExpiryIndex(t, ctx, db, true)
}

func seedAppACLCurrentP78Predecessor(t *testing.T, ctx context.Context, fixture exactAppACLCurrentSuccessorPostgresFixture, db *pgxpool.Pool, chained bool) AppACLManifestPersistedV1 {
	t.Helper()
	c78 := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP78Migration)
	if !chained {
		manifest, _, _ := seedAppACLCurrentReleasedGenesis(t, ctx, fixture, db, c78)
		return manifest
	}
	previous := seedAppACLCurrentP77Predecessor(t, ctx, fixture, db, true)
	_, _, input := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input)
	manifest, err := appendAppACLCurrentReleasedSuccessor(t, ctx, fixture, db, c78)
	if err != nil {
		t.Fatalf("materialize independently frozen C78 successor: %v", err)
	}
	after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input)
	assertAppACLCurrentManifestHistoryPrefix(t, before.Manifest.Manifests, after.Manifest.Manifests)
	assertAppACLCurrentLedgerSuffix(t, before.Ledger, after.Ledger, appACLCurrentP78Migration)
	if manifest.ManifestRevision != previous.ManifestRevision+1 || manifest.PreviousManifestDigest != previous.ManifestDigest ||
		!reflect.DeepEqual(after.Catalog, before.Catalog) {
		t.Fatal("released C77 to C78 fixture changed its exact manifest link or authorization")
	}
	return manifest
}

func testPostgresIntegrationAppACLCurrentP78Upgrade(t *testing.T, chained bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	db := fixture.openRolePool(t, ctx, fixture.migratorRole)
	predecessor := seedAppACLCurrentP78Predecessor(t, ctx, fixture, db, chained)
	seedAppACLCurrentP77Rows(t, ctx, db)
	_, _, input := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input)
	beforeRows := readAppACLCurrentP77Rows(t, ctx, db)
	beforeSettings := readAppACLCurrentSettingsPresenceSnapshot(t, ctx, db)
	if len(before.Ledger) != 79 || before.Ledger[78].Name != appACLCurrentP78Migration {
		t.Fatalf("predecessor must be exactly frozen C78: %#v", before.Ledger)
	}
	assertAppACLCurrentCacheExpiryIndex(t, ctx, db, false)
	runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)
	successor, err := ConvergeAppACLCurrent(ctx, db, fixture.runtimeRole, fixture.adminRole)
	if err != nil {
		t.Fatalf("C78 to C79 convergence: %v", err)
	}
	if successor.ManifestRevision != predecessor.ManifestRevision+1 || successor.PreviousManifestDigest != predecessor.ManifestDigest {
		t.Fatalf("C79 must append exactly one manifest linked to C78: %#v", successor)
	}
	assertAppACLCurrentCacheExpiryIndex(t, ctx, db, true)
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
		t.Fatalf("C79 runtime admission: %v", err)
	}
	after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input)
	assertAppACLCurrentManifestHistoryPrefix(t, before.Manifest.Manifests, after.Manifest.Manifests)
	assertAppACLCurrentLedgerSuffix(t, before.Ledger, after.Ledger, appACLCurrentP79Migration, appACLCurrentP80Migration)
	if len(after.Manifest.Manifests) != len(before.Manifest.Manifests)+1 || !reflect.DeepEqual(appACLCurrentCatalogBeforeReplay(t, before.Catalog, after.Catalog), before.Catalog) ||
		readAppACLCurrentP77Rows(t, ctx, db) != beforeRows {
		t.Fatal("C79 changed historical manifests, owners/ACLs, business reports or source cache facts")
	}
	assertAppACLCurrentSettingsPresenceSnapshotEqual(t, beforeSettings, readAppACLCurrentSettingsPresenceSnapshot(t, ctx, db))
	repeated, err := ConvergeAppACLCurrent(ctx, db, fixture.runtimeRole, fixture.adminRole)
	if err != nil || repeated.ManifestDigest != successor.ManifestDigest ||
		!reflect.DeepEqual(readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input), after) || readAppACLCurrentP77Rows(t, ctx, db) != beforeRows {
		t.Fatalf("C79 repeat changed durable state or persisted facts: %v", err)
	}
	assertAppACLCurrentCacheExpiryIndex(t, ctx, db, true)
}

func testPostgresIntegrationAppACLCurrentP78Rollback(t *testing.T, cutpoint string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	db := fixture.openRolePool(t, ctx, fixture.migratorRole)
	seedAppACLCurrentP78Predecessor(t, ctx, fixture, db, false)
	seedAppACLCurrentP77Rows(t, ctx, db)
	_, _, input := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input)
	beforeRows := readAppACLCurrentP77Rows(t, ctx, db)
	beforeSettings := readAppACLCurrentSettingsPresenceSnapshot(t, ctx, db)
	assertAppACLCurrentCacheExpiryIndex(t, ctx, db, false)
	injected := errors.New("C79 transactional rollback cutpoint")
	dependencies := defaultAppACLCurrentConvergenceDependencies()
	reached := false
	if cutpoint == "after_migration" {
		apply := dependencies.applyPending
		dependencies.applyPending = func(ctx context.Context, tx pgx.Tx, sources migrationSourceSnapshot, applied []MigrationChecksumEntry) error {
			if err := apply(ctx, tx, sources, applied); err != nil {
				return err
			}
			assertAppACLCurrentCacheExpiryIndex(t, ctx, tx, true)
			reached = true
			return injected
		}
	} else {
		insert := dependencies.insertSuccessor
		dependencies.insertSuccessor = func(ctx context.Context, tx pgx.Tx, previous AppACLManifestPersistedV1, sources, privileges []byte) (AppACLManifestPersistedV1, error) {
			manifest, err := insert(ctx, tx, previous, sources, privileges)
			if err != nil {
				return manifest, err
			}
			assertAppACLCurrentCacheExpiryIndex(t, ctx, tx, true)
			reached = true
			return manifest, injected
		}
	}
	_, err := convergeAppACLCurrentWithDependencies(ctx, db.BeginTx, fixture.runtimeRole, fixture.adminRole, migrations.FS, appACLCurrentMigrationFragments, dependencies)
	if !reached || !errors.Is(err, injected) {
		t.Fatalf("rollback did not reach %s: reached=%t error=%v", cutpoint, reached, err)
	}
	assertAppACLCurrentCacheExpiryIndex(t, ctx, db, false)
	if !reflect.DeepEqual(readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input), before) || readAppACLCurrentP77Rows(t, ctx, db) != beforeRows {
		t.Fatal("failed C79 transaction changed predecessor ledger, manifests/head, ACLs or persisted facts")
	}
	assertAppACLCurrentSettingsPresenceSnapshotEqual(t, beforeSettings, readAppACLCurrentSettingsPresenceSnapshot(t, ctx, db))
	runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)
	if _, err := ConvergeAppACLCurrent(ctx, db, fixture.runtimeRole, fixture.adminRole); err != nil {
		t.Fatalf("C79 recovery after rollback: %v", err)
	}
	assertAppACLCurrentCacheExpiryIndex(t, ctx, db, true)
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
		t.Fatalf("runtime admission after rollback recovery: %v", err)
	}
}

func testPostgresIntegrationAppACLCurrentP78Drift(t *testing.T, current bool, drift string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	db := fixture.openRolePool(t, ctx, fixture.migratorRole)
	seedAppACLCurrentP78Predecessor(t, ctx, fixture, db, false)
	seedAppACLCurrentP77Rows(t, ctx, db)
	if current {
		if _, err := ConvergeAppACLCurrent(ctx, db, fixture.runtimeRole, fixture.adminRole); err != nil {
			t.Fatalf("C79 convergence before drift: %v", err)
		}
	}
	var sql string
	switch drift {
	case "missing_delete":
		sql = "revoke delete on public.ip_quality_source_cache from " + pgx.Identifier{fixture.runtimeRole}.Sanitize()
	case "runtime_extra_delete":
		sql = "grant delete on public.ip_quality_intelligence to " + pgx.Identifier{fixture.runtimeRole}.Sanitize()
	case "admin_select":
		sql = "grant select on public.ip_quality_source_cache to " + pgx.Identifier{fixture.adminRole}.Sanitize()
	case "public_select":
		sql = "grant select on public.ip_quality_source_cache to public"
	case "grant_option":
		sql = "grant select on public.ip_quality_source_cache to " + pgx.Identifier{fixture.runtimeRole}.Sanitize() + " with grant option"
	case "ledger_checksum":
		sql = "update public.schema_migrations set checksum = repeat('0',64) where name = '0078_ip_quality_collection_scope.sql'"
	default:
		t.Fatalf("unknown drift %q", drift)
	}
	if _, err := db.Exec(ctx, sql); err != nil {
		t.Fatalf("inject C78/C79 drift: %v", err)
	}
	_, _, input := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input)
	beforeRows := readAppACLCurrentP77Rows(t, ctx, db)
	runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err == nil {
		t.Fatal("runtime admitted predecessor or drifted current state")
	}
	if _, err := ConvergeAppACLCurrent(ctx, db, fixture.runtimeRole, fixture.adminRole); err == nil {
		t.Fatal("convergence accepted or repaired unapproved drift")
	}
	if !reflect.DeepEqual(readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input), before) || readAppACLCurrentP77Rows(t, ctx, db) != beforeRows {
		t.Fatal("rejected drift changed ledger, manifest/head, privileges or persisted facts")
	}
	assertAppACLCurrentCacheExpiryIndex(t, ctx, db, current)
}
