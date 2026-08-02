package migrate

import (
	"context"
	"errors"
	"io/fs"
	"reflect"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
)

func TestPostgresIntegrationAppACLCurrent(t *testing.T) {
	t.Run("fresh_and_runtime", testPostgresIntegrationAppACLCurrentFreshAndRuntime)
	t.Run("exact_repeat_is_read_only", testPostgresIntegrationAppACLCurrentExactRepeat)
	t.Run("prior_baseline_requires_rebuild_without_mutation", testPostgresIntegrationAppACLCurrentPriorBaseline)
}

func testPostgresIntegrationAppACLCurrentFreshAndRuntime(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)

	manifest, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtime, fixture.admin)
	if err != nil {
		t.Fatalf("ConvergeAppACLCurrent() fresh error = %v", err)
	}
	source, contract, input := appACLCurrentPostgresContract(t, fixture, migrations.FS, appACLCurrentMigrationFragments)
	snapshot := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, input)
	if manifest.ManifestDigest != snapshot.Manifest.Manifests[0].ManifestDigest {
		t.Fatalf("fresh manifest digest = %x, persisted %x", manifest.ManifestDigest, snapshot.Manifest.Manifests[0].ManifestDigest)
	}
	if len(snapshot.Manifest.AppliedMigrations) != len(source.sources.names) {
		t.Fatalf("fresh ledger source count = %d, want %d", len(snapshot.Manifest.AppliedMigrations), len(source.sources.names))
	}
	if len(snapshot.Manifest.Manifests) != 1 || snapshot.Manifest.Head == nil || snapshot.Manifest.Head.ManifestRevision != 1 {
		t.Fatalf("fresh manifest revisions/head = %#v/%#v, want one genesis", snapshot.Manifest.Manifests, snapshot.Manifest.Head)
	}
	if err := verifyAppACLEffectiveCatalogSnapshot(snapshot.Catalog, input); err != nil {
		t.Fatalf("verify fresh current catalog: %v", err)
	}
	if len(snapshot.Catalog.ColumnACLs) != 0 || len(snapshot.Catalog.DefaultACLs) != 0 {
		t.Fatalf("fresh column/default ACLs = %#v/%#v, want none", snapshot.Catalog.ColumnACLs, snapshot.Catalog.DefaultACLs)
	}
	for _, privilege := range snapshot.Catalog.DirectPrivileges {
		if privilege.Grantee == appACLEffectiveCatalogPublicGranteeR1 {
			t.Fatalf("fresh current catalog retained PUBLIC privilege %#v", privilege)
		}
	}
	if len(snapshot.Catalog.Owners) != len(contract.ManagedObjects) {
		t.Fatalf("fresh managed owner count = %d, want %d", len(snapshot.Catalog.Owners), len(contract.ManagedObjects))
	}
	for _, expected := range contract.ExpectedFunctions {
		if !containsAppACLCurrentPostgresFunction(snapshot.Catalog.Functions, expected) {
			t.Fatalf("fresh catalog functions = %#v, missing hardening %#v", snapshot.Catalog.Functions, expected)
		}
	}

	runtimeDB := fixture.openDirectRolePool(t, ctx, fixture.runtime)
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
		t.Fatalf("AdmitAppACLCurrentRuntime() direct runtime error = %v", err)
	}
}

func testPostgresIntegrationAppACLCurrentExactRepeat(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)

	first, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtime, fixture.admin)
	if err != nil {
		t.Fatalf("ConvergeAppACLCurrent() first error = %v", err)
	}
	_, _, input := appACLCurrentPostgresContract(t, fixture, migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, input)

	repeated, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtime, fixture.admin)
	if err != nil {
		t.Fatalf("ConvergeAppACLCurrent() repeat error = %v", err)
	}
	after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, input)
	if repeated.ManifestDigest != first.ManifestDigest {
		t.Fatalf("repeat manifest digest = %x, want %x", repeated.ManifestDigest, first.ManifestDigest)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("current exact repeat changed durable state\nbefore: %#v\nafter:  %#v", before, after)
	}
}

func testPostgresIntegrationAppACLCurrentPriorBaseline(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)

	if _, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtime, fixture.admin); err != nil {
		t.Fatalf("ConvergeAppACLCurrent() prior baseline error = %v", err)
	}
	_, _, priorInput := appACLCurrentPostgresContract(t, fixture, migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, priorInput)

	futureFS := appACLCurrentTestMigrationFS(t)
	futureFS["0052_future.sql"] = &fstest.MapFile{Data: []byte("select 'future';")}
	futureFragments := []AppACLCurrentMigrationFragment{{
		Migration:  "0052_future.sql",
		Privileges: func(string) []AppACLPrivilege { return nil },
	}}
	_, err := convergeAppACLCurrentWithDependencies(
		ctx,
		func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
			return migratorDB.BeginTx(ctx, options)
		},
		fixture.runtime,
		fixture.admin,
		futureFS,
		futureFragments,
		defaultAppACLCurrentConvergenceDependencies(),
	)
	if !errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
		t.Fatalf("future current convergence error = %v, want rebuild-required sentinel", err)
	}
	after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, priorInput)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("rejected prior baseline changed durable state\nbefore: %#v\nafter:  %#v", before, after)
	}
}

type appACLCurrentPostgresDurableSnapshot struct {
	Manifest      AppACLManifestRuntimeSnapshotV1
	Catalog       AppACLEffectiveCatalogSnapshotR1
	HeadUpdatedAt time.Time
}

func appACLCurrentPostgresContract(
	t *testing.T,
	fixture appACLConvergencePostgresFixture,
	migrationFS fs.FS,
	fragments []AppACLCurrentMigrationFragment,
) (appACLCurrentSourceContract, appACLEffectiveCatalogContract, appACLEffectiveCatalogVerifierInput) {
	t.Helper()
	source, err := compileAppACLCurrentSourceContract(migrationFS, fragments)
	if err != nil {
		t.Fatalf("compile current PostgreSQL source contract: %v", err)
	}
	contract, err := compileAppACLCurrentCatalogContract(source, fixture.databaseName, []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: fixture.runtime},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: fixture.admin},
	}, fixture.migrator)
	if err != nil {
		t.Fatalf("compile current PostgreSQL catalog contract: %v", err)
	}
	input, err := newAppACLEffectiveCatalogVerifierInput(contract, fixture.migrator)
	if err != nil {
		t.Fatalf("build current PostgreSQL catalog verifier input: %v", err)
	}
	return source, contract, input
}

func readAppACLCurrentPostgresDurableSnapshot(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	input appACLEffectiveCatalogVerifierInput,
) appACLCurrentPostgresDurableSnapshot {
	t.Helper()
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatalf("begin current durable snapshot: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	manifest, err := readAppACLManifestRuntimeSnapshotInTxV1(ctx, tx)
	if err != nil {
		t.Fatalf("read current durable manifest snapshot: %v", err)
	}
	catalog, err := readAppACLEffectiveCatalogSnapshotInTx(ctx, tx, input)
	if err != nil {
		t.Fatalf("read current durable catalog snapshot: %v", err)
	}
	var headUpdatedAt time.Time
	if err := tx.QueryRow(ctx, `
		select updated_at
		from public.app_acl_manifest_head
		where singleton
	`).Scan(&headUpdatedAt); err != nil {
		t.Fatalf("read current durable manifest head timestamp: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit current durable snapshot: %v", err)
	}
	return appACLCurrentPostgresDurableSnapshot{
		Manifest:      manifest,
		Catalog:       catalog,
		HeadUpdatedAt: headUpdatedAt,
	}
}

func containsAppACLCurrentPostgresFunction(
	functions []AppACLEffectiveCatalogFunctionR1,
	expected appACLEffectiveCatalogFunctionContract,
) bool {
	for _, function := range functions {
		if function.SchemaName == expected.SchemaName &&
			function.Identity == expected.SchemaName+"."+expected.Identity &&
			function.OwnerRole == expected.OwnerRole &&
			function.Kind == expected.Kind &&
			function.SecurityDefiner == expected.SecurityDefiner &&
			reflect.DeepEqual(function.Config, expected.Config) {
			return true
		}
	}
	return false
}
