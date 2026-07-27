package migrate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresIntegrationAppACLConvergenceFreshDirectMigrator(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)

	manifest, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin)
	if err != nil {
		t.Fatalf("ConvergeAppACLR1() fresh direct migrator error = %v", err)
	}
	if manifest.ManifestRevision != 1 || manifest.MigratorCatalogRole != fixture.migrator {
		t.Fatalf("fresh converged manifest = %#v, want revision 1 bound to %q", manifest, fixture.migrator)
	}

	var ledgerCount int
	if err := fixture.db.QueryRow(ctx, `select count(*)::int from public.schema_migrations`).Scan(&ledgerCount); err != nil {
		t.Fatalf("count converged migration ledger: %v", err)
	}
	if ledgerCount != len(appACLR1MigrationSourceContract) {
		t.Fatalf("converged migration ledger count = %d, want frozen r1 source count %d", ledgerCount, len(appACLR1MigrationSourceContract))
	}
	assertSingleIntValue(t, ctx, fixture.db, `select count(*)::int from public.app_acl_manifest_revisions`, 1)

	contract, err := CompileAppACLEffectiveCatalogContractR1(fixture.databaseName, []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: fixture.runtime},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: fixture.admin},
	})
	if err != nil {
		t.Fatalf("CompileAppACLEffectiveCatalogContractR1() error = %v", err)
	}
	input, err := NewAppACLEffectiveCatalogVerifierInputR1(contract, fixture.migrator)
	if err != nil {
		t.Fatalf("NewAppACLEffectiveCatalogVerifierInputR1() error = %v", err)
	}
	if err := VerifyPostgresAppACLEffectiveCatalogR1(ctx, migratorDB, input); err != nil {
		t.Fatalf("VerifyPostgresAppACLEffectiveCatalogR1() after convergence error = %v", err)
	}

	runtimeDB := fixture.openDirectRolePool(t, ctx, fixture.runtime)
	if _, err := VerifyPersistedAppACLManifestRuntimeV1(ctx, NewPostgresAppACLManifestRuntimeReader(runtimeDB), appACLR1InjectedMigrationFS(t)); err != nil {
		t.Fatalf("VerifyPersistedAppACLManifestRuntimeV1() direct runtime error = %v", err)
	}
	adminDB := fixture.openDirectRolePool(t, ctx, fixture.admin)
	for _, db := range []*pgxpool.Pool{runtimeDB, adminDB} {
		for _, projectorSQL := range []string{
			`select public.record_platform_cas_contract_activation_projection($1::bytea)`,
			`select public.record_platform_cas_domain_rotation_projection($1::bytea)`,
		} {
			_, err := db.Exec(ctx, projectorSQL, []byte{})
			requirePostgresSQLState(t, err, "42501")
		}
		_, err = db.Exec(ctx, `select record_platform_internal.digest($1::bytea, 'sha256')`, []byte("test"))
		requirePostgresSQLState(t, err, "42501")
		_, err = db.Exec(ctx, `update public.app_acl_manifest_head set manifest_revision = manifest_revision where singleton`)
		requirePostgresSQLState(t, err, "42501")
	}

	repeated, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin)
	if err != nil {
		t.Fatalf("ConvergeAppACLR1() exact r1 repeat error = %v", err)
	}
	if repeated.ManifestDigest != manifest.ManifestDigest {
		t.Fatalf("repeat manifest digest = %x, want %x", repeated.ManifestDigest, manifest.ManifestDigest)
	}
}

func TestPostgresIntegrationAppACLConvergenceRejectsExistingEmptyPublicLedgerWithoutFragments(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)

	if _, err := migratorDB.Exec(ctx, `
		create table public.schema_migrations (
			name text primary key,
			checksum text not null check (checksum ~ '^[0-9a-f]{64}$'),
			applied_at timestamptz not null default now()
		)
	`); err != nil {
		t.Fatalf("create empty public migration ledger: %v", err)
	}
	before := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
	if !before.MigrationLedgerExists || before.MonitoringInstancesExists || before.ManifestRevisionsExists || before.ManifestHeadExists || before.InternalSchemaExists {
		t.Fatalf("empty public ledger setup state = %#v", before)
	}
	beforeLedger := readAppACLConvergenceLedgerState(t, ctx, fixture.db)
	if beforeLedger != "" {
		t.Fatalf("empty public migration ledger state = %q, want no rows", beforeLedger)
	}

	_, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin)
	if err == nil || !strings.Contains(err.Error(), "public migration ledger") {
		t.Fatalf("ConvergeAppACLR1() error = %v, want pre-existing public-ledger rejection", err)
	}

	after := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
	if after != before {
		t.Fatalf("empty public ledger state after rejected convergence = %#v, want no APP fragment %#v", after, before)
	}
	afterLedger := readAppACLConvergenceLedgerState(t, ctx, fixture.db)
	if afterLedger != beforeLedger {
		t.Fatalf("empty public migration ledger after rejected convergence = %q, want unchanged %q", afterLedger, beforeLedger)
	}
	assertSingleIntValue(t, ctx, fixture.db, `select count(*)::int from public.schema_migrations`, 0)
}

func TestPostgresIntegrationAppACLConvergenceAdoptsEligibleNullHeadAndRepeatsReadOnly(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)

	r1Migrations := appACLR1InjectedMigrationFS(t)
	if err := Apply(ctx, migratorDB); err != nil {
		t.Fatalf("Apply() eligible legacy fixture materialization error = %v", err)
	}
	assertSingleIntValue(t, ctx, fixture.db, `select count(*)::int from public.schema_migrations`, len(appACLR1MigrationSourceContract))
	legacySnapshot, err := NewPostgresAppACLManifestRuntimeReader(migratorDB).ReadAppACLManifestRuntimeSnapshotV1(ctx)
	if err != nil {
		t.Fatalf("ReadAppACLManifestRuntimeSnapshotV1() eligible legacy fixture error = %v", err)
	}
	embeddedMigrations, err := CanonicalMigrationSetFromFS(r1Migrations)
	if err != nil {
		t.Fatalf("CanonicalMigrationSetFromFS() error = %v", err)
	}
	legacyMigrations, err := CanonicalMigrationSetBodyV1(legacySnapshot.AppliedMigrations)
	if err != nil {
		t.Fatalf("CanonicalMigrationSetBodyV1() eligible legacy fixture error = %v", err)
	}
	if !bytes.Equal(legacyMigrations, embeddedMigrations) {
		t.Fatal("eligible legacy fixture ledger does not exactly match embedded migrations")
	}
	if legacySnapshot.Head != nil || len(legacySnapshot.Manifests) != 0 {
		t.Fatalf("eligible legacy fixture manifest state = %#v / %#v, want null head and no revisions", legacySnapshot.Head, legacySnapshot.Manifests)
	}

	manifest, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin)
	if err != nil {
		t.Fatalf("ConvergeAppACLR1() eligible null-head adoption error = %v", err)
	}
	if manifest.ManifestRevision != 1 || manifest.MigratorCatalogRole != fixture.migrator {
		t.Fatalf("adopted manifest = %#v, want revision 1 bound to %q", manifest, fixture.migrator)
	}
	parsedPrivileges, err := ParseCanonicalPrivilegeSetBodyV1(manifest.CanonicalPrivilegeSet)
	if err != nil {
		t.Fatalf("ParseCanonicalPrivilegeSetBodyV1() adopted manifest error = %v", err)
	}
	if len(parsedPrivileges.Privileges) != appACLEffectiveCatalogR1PrivilegeCount {
		t.Fatalf("adopted manifest privilege count = %d, want %d", len(parsedPrivileges.Privileges), appACLEffectiveCatalogR1PrivilegeCount)
	}
	assertSingleIntValue(t, ctx, fixture.db, `select count(*)::int from public.app_acl_manifest_revisions`, 1)
	var firstHeadUpdatedAt time.Time
	if err := fixture.db.QueryRow(ctx, `select updated_at from public.app_acl_manifest_head where singleton`).Scan(&firstHeadUpdatedAt); err != nil {
		t.Fatalf("read adopted manifest head timestamp: %v", err)
	}

	runtimeDB := fixture.openDirectRolePool(t, ctx, fixture.runtime)
	if _, err := VerifyPersistedAppACLManifestRuntimeV1(ctx, NewPostgresAppACLManifestRuntimeReader(runtimeDB), r1Migrations); err != nil {
		t.Fatalf("VerifyPersistedAppACLManifestRuntimeV1() adopted direct runtime error = %v", err)
	}
	if err := AdmitAppACLRuntime(ctx, runtimeDB); err != nil {
		t.Fatalf("AdmitAppACLRuntime() adopted direct runtime error = %v", err)
	}
	contract, err := CompileAppACLEffectiveCatalogContractR1(fixture.databaseName, []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: fixture.runtime},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: fixture.admin},
	})
	if err != nil {
		t.Fatalf("CompileAppACLEffectiveCatalogContractR1() adopted state error = %v", err)
	}
	input, err := NewAppACLEffectiveCatalogVerifierInputR1(contract, fixture.migrator)
	if err != nil {
		t.Fatalf("NewAppACLEffectiveCatalogVerifierInputR1() adopted state error = %v", err)
	}
	if err := VerifyPostgresAppACLEffectiveCatalogR1(ctx, migratorDB, input); err != nil {
		t.Fatalf("VerifyPostgresAppACLEffectiveCatalogR1() adopted state error = %v", err)
	}

	repeated, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin)
	if err != nil {
		t.Fatalf("ConvergeAppACLR1() adopted exact r1 repeat error = %v", err)
	}
	if repeated.ManifestDigest != manifest.ManifestDigest {
		t.Fatalf("adopted repeat manifest digest = %x, want %x", repeated.ManifestDigest, manifest.ManifestDigest)
	}
	assertSingleIntValue(t, ctx, fixture.db, `select count(*)::int from public.app_acl_manifest_revisions`, 1)
	var repeatedHeadUpdatedAt time.Time
	if err := fixture.db.QueryRow(ctx, `select updated_at from public.app_acl_manifest_head where singleton`).Scan(&repeatedHeadUpdatedAt); err != nil {
		t.Fatalf("read repeated adopted manifest head timestamp: %v", err)
	}
	if !repeatedHeadUpdatedAt.Equal(firstHeadUpdatedAt) {
		t.Fatalf("adopted read-only repeat updated manifest head at %s, want %s", repeatedHeadUpdatedAt, firstHeadUpdatedAt)
	}
}

func TestPostgresIntegrationAppACLConvergenceRollsBackLateCutpoints(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		install func(appACLConvergenceDependencies, error) appACLConvergenceDependencies
	}{
		{
			name: "after_ledger",
			install: func(dependencies appACLConvergenceDependencies, cutpoint error) appACLConvergenceDependencies {
				original := dependencies.ensureLedger
				dependencies.ensureLedger = func(ctx context.Context, tx pgx.Tx, sources map[string]migrationSource) error {
					if err := original(ctx, tx, sources); err != nil {
						return err
					}
					return cutpoint
				}
				return dependencies
			},
		},
		{
			name: "after_pending_sql_and_ledger",
			install: func(dependencies appACLConvergenceDependencies, cutpoint error) appACLConvergenceDependencies {
				original := dependencies.applyPending
				dependencies.applyPending = func(ctx context.Context, tx pgx.Tx, sources migrationSourceSnapshot, applied []MigrationChecksumEntry) error {
					if err := original(ctx, tx, sources, applied); err != nil {
						return err
					}
					return cutpoint
				}
				return dependencies
			},
		},
		{
			name: "after_dcl",
			install: func(dependencies appACLConvergenceDependencies, cutpoint error) appACLConvergenceDependencies {
				original := dependencies.applyDCL
				dependencies.applyDCL = func(ctx context.Context, tx pgx.Tx, contract AppACLEffectiveCatalogContractR1) error {
					if err := original(ctx, tx, contract); err != nil {
						return err
					}
					return cutpoint
				}
				return dependencies
			},
		},
		{
			name: "after_catalog",
			install: func(dependencies appACLConvergenceDependencies, cutpoint error) appACLConvergenceDependencies {
				original := dependencies.verifyCatalog
				dependencies.verifyCatalog = func(snapshot AppACLEffectiveCatalogSnapshotR1, input AppACLEffectiveCatalogVerifierInputR1) error {
					if err := original(snapshot, input); err != nil {
						return err
					}
					return cutpoint
				}
				return dependencies
			},
		},
		{
			name: "after_manifest_head_cas",
			install: func(dependencies appACLConvergenceDependencies, cutpoint error) appACLConvergenceDependencies {
				original := dependencies.insertGenesis
				dependencies.insertGenesis = func(ctx context.Context, tx pgx.Tx, migrations []byte, privileges []byte, migrator string) (AppACLManifestPersistedV1, error) {
					manifest, err := original(ctx, tx, migrations, privileges, migrator)
					if err != nil {
						return AppACLManifestPersistedV1{}, err
					}
					return manifest, cutpoint
				}
				return dependencies
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newAppACLConvergencePostgresFixture(t, ctx)
			migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)
			before := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
			cutpoint := errors.New("controlled app ACL convergence cutpoint")
			dependencies := tc.install(defaultAppACLConvergenceDependencies(), cutpoint)
			sources := appACLConvergenceSourcesForPostgresTest(t)
			attempts := 0

			_, err := convergeAppACLR1WithDependencies(
				ctx,
				func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
					attempts++
					if options.IsoLevel != pgx.Serializable {
						t.Fatalf("convergence transaction isolation = %v, want SERIALIZABLE", options.IsoLevel)
					}
					return migratorDB.BeginTx(ctx, options)
				},
				fixture.runtime,
				fixture.admin,
				sources,
				dependencies,
			)
			if !errors.Is(err, cutpoint) {
				t.Fatalf("convergeAppACLR1WithDependencies() %s error = %v, want controlled cutpoint", tc.name, err)
			}
			if attempts != 1 {
				t.Fatalf("ordinary %s cutpoint transaction attempts = %d, want 1", tc.name, attempts)
			}
			after := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
			if after != before {
				t.Fatalf("%s rollback state = %#v, want original %#v", tc.name, after, before)
			}
		})
	}
}

func TestPostgresIntegrationAppACLConvergenceRetriesWholeClosureAfterSerializationFailure(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	sources := appACLConvergenceSourcesForPostgresTest(t)
	dependencies := defaultAppACLConvergenceDependencies()
	const serializationSentinel = "app_acl_convergence_serialization_sentinel"
	if _, err := fixture.db.Exec(ctx, `
		create table public.app_acl_convergence_serialization_sentinel (
			singleton boolean primary key default true check (singleton),
			value integer not null
		)
	`); err != nil {
		t.Fatalf("create serialization conflict sentinel: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.app_acl_convergence_serialization_sentinel (value) values (0)`); err != nil {
		t.Fatalf("insert serialization conflict sentinel: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `grant select, update on table public.app_acl_convergence_serialization_sentinel to `+quotePostgresIdentifier(fixture.migrator)); err != nil {
		t.Fatalf("grant direct migrator serialization conflict sentinel access: %v", err)
	}
	before := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
	originalReadDatabaseName := dependencies.readDatabaseName
	conflictWriterCommitted := false
	dependencies.readDatabaseName = func(ctx context.Context, tx pgx.Tx) (string, error) {
		databaseName, err := originalReadDatabaseName(ctx, tx)
		if err != nil || conflictWriterCommitted {
			return databaseName, err
		}
		var value int
		if err := tx.QueryRow(ctx, `select value from public.app_acl_convergence_serialization_sentinel where singleton`).Scan(&value); err != nil {
			return "", fmt.Errorf("read serialization conflict sentinel from convergence transaction: %w", err)
		}
		writer, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
		if err != nil {
			return "", fmt.Errorf("begin concurrent serialization conflict transaction: %w", err)
		}
		defer func() { _ = writer.Rollback(ctx) }()
		if _, err := writer.Exec(ctx, `update public.app_acl_convergence_serialization_sentinel set value = value + 1 where singleton`); err != nil {
			return "", fmt.Errorf("write concurrent serialization conflict sentinel: %w", err)
		}
		if err := writer.Commit(ctx); err != nil {
			return "", fmt.Errorf("commit concurrent serialization conflict transaction: %w", err)
		}
		conflictWriterCommitted = true
		return databaseName, nil
	}
	originalEnsureLedger := dependencies.ensureLedger
	originalApplyPending := dependencies.applyPending
	originalApplyDCL := dependencies.applyDCL
	originalReadCatalog := dependencies.readCatalog
	ensureLedgerCalls := 0
	applyPendingCalls := 0
	applyDCLCalls := 0
	readCatalogCalls := 0
	dependencies.ensureLedger = func(ctx context.Context, tx pgx.Tx, sources map[string]migrationSource) error {
		ensureLedgerCalls++
		return originalEnsureLedger(ctx, tx, sources)
	}
	dependencies.applyPending = func(ctx context.Context, tx pgx.Tx, sources migrationSourceSnapshot, applied []MigrationChecksumEntry) error {
		applyPendingCalls++
		return originalApplyPending(ctx, tx, sources, applied)
	}
	dependencies.applyDCL = func(ctx context.Context, tx pgx.Tx, contract AppACLEffectiveCatalogContractR1) error {
		applyDCLCalls++
		return originalApplyDCL(ctx, tx, contract)
	}
	dependencies.readCatalog = func(ctx context.Context, tx pgx.Tx, input AppACLEffectiveCatalogVerifierInputR1) (AppACLEffectiveCatalogSnapshotR1, error) {
		readCatalogCalls++
		return originalReadCatalog(ctx, tx, input)
	}
	originalInsertGenesis := dependencies.insertGenesis
	insertCalls := 0
	serverSerializationFailure := false
	dependencies.insertGenesis = func(ctx context.Context, tx pgx.Tx, migrations []byte, privileges []byte, migrator string) (AppACLManifestPersistedV1, error) {
		manifest, err := originalInsertGenesis(ctx, tx, migrations, privileges, migrator)
		if err != nil {
			return AppACLManifestPersistedV1{}, err
		}
		insertCalls++
		if !serverSerializationFailure {
			_, err := tx.Exec(ctx, `update public.app_acl_convergence_serialization_sentinel set value = value + 1 where singleton`)
			if err == nil {
				return AppACLManifestPersistedV1{}, fmt.Errorf("expected PostgreSQL serialization conflict after concurrent sentinel write")
			}
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "40001" {
				return AppACLManifestPersistedV1{}, fmt.Errorf("write convergence serialization sentinel: %w", err)
			}
			serverSerializationFailure = true
			return AppACLManifestPersistedV1{}, fmt.Errorf("server serialization conflict during convergence sentinel write: %w", err)
		}
		return manifest, nil
	}
	attempts := 0

	manifest, err := convergeAppACLR1WithDependencies(
		ctx,
		func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
			attempts++
			if options.IsoLevel != pgx.Serializable {
				t.Fatalf("convergence retry transaction isolation = %v, want SERIALIZABLE", options.IsoLevel)
			}
			if attempts == 2 {
				afterFirstAttempt := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
				if afterFirstAttempt != before {
					t.Fatalf("serialization rollback state = %#v, want original %#v", afterFirstAttempt, before)
				}
			}
			return migratorDB.BeginTx(ctx, options)
		},
		fixture.runtime,
		fixture.admin,
		sources,
		dependencies,
	)
	if err != nil {
		t.Fatalf("convergeAppACLR1WithDependencies() serialization retry error = %v", err)
	}
	if !conflictWriterCommitted || !serverSerializationFailure {
		t.Fatalf("real PostgreSQL serialization conflict state = writer:%t failure:%t, want true:true", conflictWriterCommitted, serverSerializationFailure)
	}
	if attempts != 2 || insertCalls != 2 {
		t.Fatalf("serialization retry attempts/inserts = (%d, %d), want (2, 2)", attempts, insertCalls)
	}
	if ensureLedgerCalls != 2 || applyPendingCalls != 2 || applyDCLCalls != 2 || readCatalogCalls != 3 {
		t.Fatalf("serialization retry full closure calls = ledger %d, pending %d, DCL %d, catalog %d, want 2, 2, 2, 3", ensureLedgerCalls, applyPendingCalls, applyDCLCalls, readCatalogCalls)
	}
	if manifest.ManifestRevision != 1 || manifest.MigratorCatalogRole != fixture.migrator {
		t.Fatalf("serialization retry manifest = %#v, want revision 1 bound to %q", manifest, fixture.migrator)
	}
	assertSingleIntValue(t, ctx, fixture.db, `select count(*)::int from public.schema_migrations`, 52)
	assertSingleIntValue(t, ctx, fixture.db, `select count(*)::int from public.app_acl_manifest_revisions`, 1)
	assertSingleIntValue(t, ctx, fixture.db, `select value from public.`+serializationSentinel+` where singleton`, 1)
	contract, err := CompileAppACLEffectiveCatalogContractR1(fixture.databaseName, []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: fixture.runtime},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: fixture.admin},
	})
	if err != nil {
		t.Fatalf("CompileAppACLEffectiveCatalogContractR1() retry result error = %v", err)
	}
	input, err := NewAppACLEffectiveCatalogVerifierInputR1(contract, fixture.migrator)
	if err != nil {
		t.Fatalf("NewAppACLEffectiveCatalogVerifierInputR1() retry result error = %v", err)
	}
	if err := VerifyPostgresAppACLEffectiveCatalogR1(ctx, migratorDB, input); err != nil {
		t.Fatalf("VerifyPostgresAppACLEffectiveCatalogR1() retry result error = %v", err)
	}
}

func TestPostgresIntegrationAppACLConvergenceRejectsWrongOwnerOrSchemaLegacyAdoptionWithoutRepair(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		mutate  func(*testing.T, context.Context, appACLConvergencePostgresFixture)
		want    func(appACLLegacyConvergenceState, appACLConvergencePostgresFixture) bool
		message string
	}{
		{
			name: "wrong_owner",
			mutate: func(t *testing.T, ctx context.Context, fixture appACLConvergencePostgresFixture) {
				t.Helper()
				if _, err := fixture.db.Exec(ctx, `alter table public.app_acl_manifest_head owner to `+quotePostgresIdentifier(fixture.bootstrapOwner)); err != nil {
					t.Fatalf("assign wrong legacy owner: %v", err)
				}
			},
			want: func(state appACLLegacyConvergenceState, fixture appACLConvergencePostgresFixture) bool {
				return state.ManifestHeadSchema == "public" && state.ManifestHeadOwner == fixture.bootstrapOwner
			},
			message: "wrong owner",
		},
		{
			name: "wrong_schema",
			mutate: func(t *testing.T, ctx context.Context, fixture appACLConvergencePostgresFixture) {
				t.Helper()
				if _, err := fixture.db.Exec(ctx, `alter table public.app_acl_manifest_head set schema record_platform_internal`); err != nil {
					t.Fatalf("move legacy relation to wrong schema: %v", err)
				}
			},
			want: func(state appACLLegacyConvergenceState, fixture appACLConvergencePostgresFixture) bool {
				return state.ManifestHeadSchema == "record_platform_internal" && state.ManifestHeadOwner == fixture.migrator
			},
			message: "wrong schema",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newAppACLConvergencePostgresFixture(t, ctx)
			migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)
			if err := Apply(ctx, migratorDB); err != nil {
				t.Fatalf("Apply() legacy %s fixture materialization error = %v", tc.message, err)
			}
			tc.mutate(t, ctx, fixture)
			before := readAppACLLegacyConvergenceState(t, ctx, fixture.db)
			if !tc.want(before, fixture) {
				t.Fatalf("legacy %s setup state = %#v", tc.message, before)
			}
			beforeHead := readAppACLLegacyManifestHeadState(t, ctx, fixture.db, before.ManifestHeadSchema)

			_, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin)
			if err == nil {
				t.Fatalf("ConvergeAppACLR1() accepted legacy %s state", tc.message)
			}
			after := readAppACLLegacyConvergenceState(t, ctx, fixture.db)
			if after != before {
				t.Fatalf("legacy %s state after rejected convergence = %#v, want no repair/no fragment %#v", tc.message, after, before)
			}
			afterHead := readAppACLLegacyManifestHeadState(t, ctx, fixture.db, after.ManifestHeadSchema)
			if afterHead != beforeHead {
				t.Fatalf("legacy %s manifest head after rejected convergence = %#v, want no repair/no fragment %#v", tc.message, afterHead, beforeHead)
			}
		})
	}
}

func TestPostgresIntegrationAppACLConvergenceRejectsRecognizedNonPublicLegacyLedgerWithoutReplay(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	quotedSchema := quotePostgresIdentifier(fixture.migrator)
	quotedLedger := quotedSchema + `.` + quotePostgresIdentifier("schema_migrations")
	quotedMonitoringInstances := quotedSchema + `.` + quotePostgresIdentifier("monitoring_instances")
	if _, err := migratorDB.Exec(ctx, `create schema `+quotedSchema); err != nil {
		t.Fatalf("create direct-migrator legacy schema: %v", err)
	}
	if _, err := migratorDB.Exec(ctx, `create table `+quotedLedger+` (name text primary key, checksum text)`); err != nil {
		t.Fatalf("create non-public legacy migration ledger: %v", err)
	}
	if _, err := migratorDB.Exec(ctx, `create table `+quotedMonitoringInstances+` (monitoring_instance_id text primary key)`); err != nil {
		t.Fatalf("create non-public managed legacy relation: %v", err)
	}
	sources := appACLConvergenceSourcesForPostgresTest(t)
	legacySource := sources.sources["0001_initial_schema.sql"]
	if _, err := migratorDB.Exec(ctx, `insert into `+quotedLedger+` (name, checksum) values ($1, $2)`, "0001_initial_schema.sql", legacySource.checksum); err != nil {
		t.Fatalf("record recognized non-public legacy migration: %v", err)
	}
	before := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
	if before.MigrationLedgerExists || before.MonitoringInstancesExists || before.ManifestRevisionsExists || before.ManifestHeadExists {
		t.Fatalf("non-public legacy setup unexpectedly has public convergence state: %#v", before)
	}

	_, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin)
	if err == nil || !strings.Contains(err.Error(), "non-public") {
		t.Fatalf("ConvergeAppACLR1() error = %v, want recognized non-public legacy ledger rejection", err)
	}
	after := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
	if after != before {
		t.Fatalf("recognized non-public legacy ledger after convergence = %#v, want no public replay/repair %#v", after, before)
	}
	assertSingleIntValue(t, ctx, fixture.db, `select count(*)::int from `+quotedLedger, 1)
}

func TestPostgresIntegrationAppACLConvergenceRejectsRecognizedInternalSchemaLegacyLedgerWithoutReplay(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	quotedSchema := quotePostgresIdentifier("record_platform_internal")
	quotedLedger := quotedSchema + `.` + quotePostgresIdentifier("schema_migrations")
	if _, err := migratorDB.Exec(ctx, `create schema `+quotedSchema); err != nil {
		t.Fatalf("create internal legacy schema: %v", err)
	}
	if _, err := migratorDB.Exec(ctx, `create table `+quotedLedger+` (name text primary key, checksum text)`); err != nil {
		t.Fatalf("create internal non-public migration ledger: %v", err)
	}
	sources := appACLConvergenceSourcesForPostgresTest(t)
	legacySource := sources.sources["0001_initial_schema.sql"]
	if _, err := migratorDB.Exec(ctx, `insert into `+quotedLedger+` (name, checksum) values ($1, $2)`, "0001_initial_schema.sql", legacySource.checksum); err != nil {
		t.Fatalf("record recognized internal legacy migration: %v", err)
	}
	before := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
	if before.MigrationLedgerExists || before.MonitoringInstancesExists || before.ManifestRevisionsExists || before.ManifestHeadExists {
		t.Fatalf("internal legacy setup unexpectedly has public convergence state: %#v", before)
	}

	_, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin)
	if err == nil || !strings.Contains(err.Error(), "non-public application migration ledger") {
		t.Fatalf("ConvergeAppACLR1() error = %v, want internal non-public legacy ledger rejection before replay", err)
	}
	after := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
	if after != before {
		t.Fatalf("recognized internal legacy ledger after convergence = %#v, want no public replay/repair %#v", after, before)
	}
	assertSingleIntValue(t, ctx, fixture.db, `select count(*)::int from `+quotedLedger, 1)
}

func TestPostgresIntegrationAppACLConvergenceRejectsFreshManagedTableOutsidePublicWithoutLedger(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	const legacySchema = "legacy_shadow"
	quotedSchema := quotePostgresIdentifier(legacySchema)
	quotedMonitoringInstances := quotedSchema + `.` + quotePostgresIdentifier("monitoring_instances")
	if _, err := fixture.db.Exec(ctx, `create schema `+quotedSchema); err != nil {
		t.Fatalf("create non-public legacy schema: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `create table `+quotedMonitoringInstances+` (monitoring_instance_id text primary key)`); err != nil {
		t.Fatalf("create non-public managed table without a ledger: %v", err)
	}
	before := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
	if before.MigrationLedgerExists || before.MonitoringInstancesExists || before.ManifestRevisionsExists || before.ManifestHeadExists || before.InternalSchemaExists {
		t.Fatalf("non-public managed-table setup unexpectedly has public convergence state: %#v", before)
	}

	_, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin)
	if err == nil || !strings.Contains(err.Error(), "non-public managed object") {
		t.Fatalf("ConvergeAppACLR1() error = %v, want non-public managed-table rejection", err)
	}
	after := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
	if after != before {
		t.Fatalf("non-public managed-table state after convergence = %#v, want no public replay/repair %#v", after, before)
	}
}

func TestPostgresIntegrationAppACLConvergenceRejectsFreshManagedTableInPublicWithoutLedger(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	if _, err := migratorDB.Exec(ctx, `create table public.monitoring_instances (monitoring_instance_id text primary key)`); err != nil {
		t.Fatalf("create correctly placed public managed table: %v", err)
	}
	before := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
	if before.MigrationLedgerExists || !before.MonitoringInstancesExists || before.ManifestRevisionsExists || before.ManifestHeadExists || before.InternalSchemaExists {
		t.Fatalf("correctly placed public managed-table setup state = %#v", before)
	}

	_, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin)
	if err == nil || !strings.Contains(err.Error(), "fresh app ACL convergence") {
		t.Fatalf("ConvergeAppACLR1() error = %v, want fresh-state public managed-table rejection", err)
	}
	after := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
	if after != before {
		t.Fatalf("correctly placed public managed-table state after convergence = %#v, want no adoption %#v", after, before)
	}
}

func TestPostgresIntegrationAppACLConvergenceRejectsNoLedgerNonPublicManagedObjectAcrossPreStates(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		setup func(*testing.T, context.Context, appACLConvergencePostgresFixture, *pgxpool.Pool)
	}{
		{
			name: "fresh",
			setup: func(t *testing.T, _ context.Context, _ appACLConvergencePostgresFixture, _ *pgxpool.Pool) {
				t.Helper()
			},
		},
		{
			name: "null_head_adoption",
			setup: func(t *testing.T, ctx context.Context, _ appACLConvergencePostgresFixture, migratorDB *pgxpool.Pool) {
				t.Helper()
				if err := Apply(ctx, migratorDB); err != nil {
					t.Fatalf("Apply() null-head legacy fixture materialization error: %v", err)
				}
			},
		},
		{
			name: "exact_r1_repeat",
			setup: func(t *testing.T, ctx context.Context, fixture appACLConvergencePostgresFixture, migratorDB *pgxpool.Pool) {
				t.Helper()
				if _, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin); err != nil {
					t.Fatalf("ConvergeAppACLR1() exact-r1 fixture materialization error: %v", err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newAppACLConvergencePostgresFixture(t, ctx)
			migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)
			tc.setup(t, ctx, fixture, migratorDB)

			const shadowSchema = "legacy_shadow"
			quotedSchema := quotePostgresIdentifier(shadowSchema)
			quotedMonitoringInstances := quotedSchema + `.` + quotePostgresIdentifier("monitoring_instances")
			if _, err := migratorDB.Exec(ctx, `create schema `+quotedSchema); err != nil {
				t.Fatalf("create no-ledger shadow schema: %v", err)
			}
			if _, err := migratorDB.Exec(ctx, `create table `+quotedMonitoringInstances+` (monitoring_instance_id text primary key)`); err != nil {
				t.Fatalf("create no-ledger shadow managed table: %v", err)
			}

			before := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
			switch tc.name {
			case "fresh":
				if before.MigrationLedgerExists || before.MonitoringInstancesExists || before.ManifestRevisionsExists || before.ManifestHeadExists || before.InternalSchemaExists {
					t.Fatalf("fresh no-ledger shadow setup state = %#v", before)
				}
			case "null_head_adoption":
				if !before.MigrationLedgerExists || !before.MonitoringInstancesExists || !before.ManifestRevisionsExists || !before.ManifestHeadExists || !before.InternalSchemaExists {
					t.Fatalf("null-head correctly placed inventory setup state = %#v", before)
				}
			case "exact_r1_repeat":
				if !before.MigrationLedgerExists || !before.MonitoringInstancesExists || !before.ManifestRevisionsExists || !before.ManifestHeadExists || !before.InternalSchemaExists {
					t.Fatalf("exact-r1 correctly placed inventory setup state = %#v", before)
				}
			}
			beforeLedger := ""
			if before.MigrationLedgerExists {
				beforeLedger = readAppACLConvergenceLedgerState(t, ctx, fixture.db)
			}
			beforeRevisions := ""
			var beforeHead appACLLegacyManifestHeadState
			if before.ManifestRevisionsExists {
				beforeRevisions = readAppACLConvergenceManifestRevisionState(t, ctx, fixture.db)
				beforeHead = readAppACLLegacyManifestHeadState(t, ctx, fixture.db, "public")
			}

			_, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin)
			if err == nil || !strings.Contains(err.Error(), "non-public managed object") {
				t.Fatalf("ConvergeAppACLR1() no-ledger shadow error = %v, want mode-independent placement rejection", err)
			}

			after := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
			if after != before {
				t.Fatalf("%s no-ledger shadow state after rejected convergence = %#v, want unchanged %#v", tc.name, after, before)
			}
			if before.MigrationLedgerExists {
				if afterLedger := readAppACLConvergenceLedgerState(t, ctx, fixture.db); afterLedger != beforeLedger {
					t.Fatalf("%s no-ledger shadow ledger = %q, want unchanged %q", tc.name, afterLedger, beforeLedger)
				}
			}
			if before.ManifestRevisionsExists {
				if afterRevisions := readAppACLConvergenceManifestRevisionState(t, ctx, fixture.db); afterRevisions != beforeRevisions {
					t.Fatalf("%s no-ledger shadow revisions = %q, want unchanged %q", tc.name, afterRevisions, beforeRevisions)
				}
				if afterHead := readAppACLLegacyManifestHeadState(t, ctx, fixture.db, "public"); afterHead != beforeHead {
					t.Fatalf("%s no-ledger shadow manifest head = %#v, want unchanged %#v", tc.name, afterHead, beforeHead)
				}
			}
			assertSingleIntValue(t, ctx, fixture.db, `select count(*)::int from `+quotedMonitoringInstances, 0)
		})
	}
}

func TestPostgresIntegrationAppACLConvergenceRejectsNoLedgerNonPublicManagedFunctionAcrossPreStates(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name  string
		setup func(*testing.T, context.Context, appACLConvergencePostgresFixture, *pgxpool.Pool)
	}{
		{
			name: "fresh",
			setup: func(t *testing.T, _ context.Context, _ appACLConvergencePostgresFixture, _ *pgxpool.Pool) {
				t.Helper()
			},
		},
		{
			name: "null_head_adoption",
			setup: func(t *testing.T, ctx context.Context, _ appACLConvergencePostgresFixture, migratorDB *pgxpool.Pool) {
				t.Helper()
				if err := Apply(ctx, migratorDB); err != nil {
					t.Fatalf("Apply() null-head legacy fixture materialization error: %v", err)
				}
			},
		},
		{
			name: "exact_r1_repeat",
			setup: func(t *testing.T, ctx context.Context, fixture appACLConvergencePostgresFixture, migratorDB *pgxpool.Pool) {
				t.Helper()
				if _, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin); err != nil {
					t.Fatalf("ConvergeAppACLR1() exact-r1 fixture materialization error: %v", err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newAppACLConvergencePostgresFixture(t, ctx)
			migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)
			tc.setup(t, ctx, fixture, migratorDB)

			const shadowSchema = "legacy_shadow"
			const managedFunction = "record_platform_cas_contract_activation_projection"
			quotedSchema := quotePostgresIdentifier(shadowSchema)
			quotedFunction := quotedSchema + `.` + quotePostgresIdentifier(managedFunction)
			if _, err := migratorDB.Exec(ctx, `create schema `+quotedSchema); err != nil {
				t.Fatalf("create no-ledger function shadow schema: %v", err)
			}
			if _, err := migratorDB.Exec(ctx, `create function `+quotedFunction+`(bytea) returns bytea language sql immutable as $$ select $1 $$`); err != nil {
				t.Fatalf("create no-ledger shadow managed function: %v", err)
			}

			before := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
			switch tc.name {
			case "fresh":
				if before.MigrationLedgerExists || before.MonitoringInstancesExists || before.ManifestRevisionsExists || before.ManifestHeadExists || before.InternalSchemaExists {
					t.Fatalf("fresh no-ledger function shadow setup state = %#v", before)
				}
			case "null_head_adoption":
				if !before.MigrationLedgerExists || !before.MonitoringInstancesExists || !before.ManifestRevisionsExists || !before.ManifestHeadExists || !before.InternalSchemaExists {
					t.Fatalf("null-head correctly placed inventory setup state = %#v", before)
				}
			case "exact_r1_repeat":
				if !before.MigrationLedgerExists || !before.MonitoringInstancesExists || !before.ManifestRevisionsExists || !before.ManifestHeadExists || !before.InternalSchemaExists {
					t.Fatalf("exact-r1 correctly placed inventory setup state = %#v", before)
				}
			}
			beforeLedger := ""
			if before.MigrationLedgerExists {
				beforeLedger = readAppACLConvergenceLedgerState(t, ctx, fixture.db)
			}
			beforeRevisions := ""
			var beforeHead appACLLegacyManifestHeadState
			if before.ManifestRevisionsExists {
				beforeRevisions = readAppACLConvergenceManifestRevisionState(t, ctx, fixture.db)
				beforeHead = readAppACLLegacyManifestHeadState(t, ctx, fixture.db, "public")
			}

			_, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin)
			if err == nil || !strings.Contains(err.Error(), "non-public managed object") {
				t.Fatalf("ConvergeAppACLR1() no-ledger function shadow error = %v, want mode-independent placement rejection", err)
			}

			after := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
			if after != before {
				t.Fatalf("%s no-ledger function shadow state after rejected convergence = %#v, want unchanged %#v", tc.name, after, before)
			}
			if before.MigrationLedgerExists {
				if afterLedger := readAppACLConvergenceLedgerState(t, ctx, fixture.db); afterLedger != beforeLedger {
					t.Fatalf("%s no-ledger function shadow ledger = %q, want unchanged %q", tc.name, afterLedger, beforeLedger)
				}
			}
			if before.ManifestRevisionsExists {
				if afterRevisions := readAppACLConvergenceManifestRevisionState(t, ctx, fixture.db); afterRevisions != beforeRevisions {
					t.Fatalf("%s no-ledger function shadow revisions = %q, want unchanged %q", tc.name, afterRevisions, beforeRevisions)
				}
				if afterHead := readAppACLLegacyManifestHeadState(t, ctx, fixture.db, "public"); afterHead != beforeHead {
					t.Fatalf("%s no-ledger function shadow manifest head = %#v, want unchanged %#v", tc.name, afterHead, beforeHead)
				}
			}
			assertSingleIntValue(t, ctx, fixture.db, `
				select count(*)::int
				from pg_catalog.pg_proc procedure
				join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
				where namespace.nspname = 'legacy_shadow'
				  and procedure.proname = 'record_platform_cas_contract_activation_projection'
				  and pg_catalog.pg_get_function_identity_arguments(procedure.oid) = 'bytea'
			`, 1)
		})
	}
}

func TestPostgresIntegrationAppACLConvergenceRejectsFreshExistingInternalSchemaWithoutLedger(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	quotedInternalSchema := quotePostgresIdentifier(appACLManagedInternalSchemaR1)
	if _, err := fixture.db.Exec(ctx, `create schema `+quotedInternalSchema); err != nil {
		t.Fatalf("create empty managed internal schema: %v", err)
	}
	before := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
	if before.MigrationLedgerExists || before.MonitoringInstancesExists || before.ManifestRevisionsExists || before.ManifestHeadExists || !before.InternalSchemaExists {
		t.Fatalf("empty internal-schema setup state = %#v", before)
	}

	_, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin)
	if err == nil || !strings.Contains(err.Error(), "fresh app ACL convergence") {
		t.Fatalf("ConvergeAppACLR1() error = %v, want fresh-state internal-schema rejection", err)
	}
	after := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
	if after != before {
		t.Fatalf("empty internal-schema state after convergence = %#v, want no public replay/repair %#v", after, before)
	}
}

func TestPostgresIntegrationAppACLConvergenceRejectsReadableForeignLedgerWithEmbeddedMigrationWithoutReplay(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	const foreignSchema = "foreign_readable_history"
	quotedSchema := quotePostgresIdentifier(foreignSchema)
	quotedLedger := quotedSchema + `.` + quotePostgresIdentifier("schema_migrations")
	quotedMigrator := quotePostgresIdentifier(fixture.migrator)
	if _, err := fixture.db.Exec(ctx, `create schema `+quotedSchema); err != nil {
		t.Fatalf("create foreign readable schema: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `create table `+quotedLedger+` (name text primary key, checksum text)`); err != nil {
		t.Fatalf("create foreign readable migration ledger: %v", err)
	}
	sources := appACLConvergenceSourcesForPostgresTest(t)
	legacySource := sources.sources["0001_initial_schema.sql"]
	if _, err := fixture.db.Exec(ctx, `insert into `+quotedLedger+` (name, checksum) values ($1, $2)`, "0001_initial_schema.sql", legacySource.checksum); err != nil {
		t.Fatalf("record embedded migration in foreign ledger: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `grant usage on schema `+quotedSchema+` to `+quotedMigrator); err != nil {
		t.Fatalf("grant direct migrator schema usage: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `grant select on table `+quotedLedger+` to `+quotedMigrator); err != nil {
		t.Fatalf("grant direct migrator ledger read: %v", err)
	}
	var embeddedName string
	if err := migratorDB.QueryRow(ctx, `select name from `+quotedLedger).Scan(&embeddedName); err != nil || embeddedName != "0001_initial_schema.sql" {
		t.Fatalf("read granted foreign migration ledger = %q, %v; want embedded migration", embeddedName, err)
	}
	before := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
	if before.MigrationLedgerExists || before.MonitoringInstancesExists || before.ManifestRevisionsExists || before.ManifestHeadExists || before.InternalSchemaExists {
		t.Fatalf("readable foreign-ledger setup unexpectedly has public convergence state: %#v", before)
	}

	_, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin)
	if err == nil || !strings.Contains(err.Error(), "non-public application migration ledger") {
		t.Fatalf("ConvergeAppACLR1() error = %v, want readable foreign legacy ledger rejection", err)
	}
	after := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
	if after != before {
		t.Fatalf("readable foreign-ledger state after convergence = %#v, want no public replay/repair %#v", after, before)
	}
	assertSingleIntValue(t, ctx, fixture.db, `select count(*)::int from `+quotedLedger, 1)
}

func TestPostgresIntegrationAppACLConvergenceRejectsNonPublicFixedSurfaceAlongsideLegacyShape(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	const legacySchema = "legacy_app_surface"
	quotedSchema := quotePostgresIdentifier(legacySchema)
	quotedLedger := quotedSchema + `.` + quotePostgresIdentifier("schema_migrations")
	quotedMonitoringInstances := quotedSchema + `.` + quotePostgresIdentifier("monitoring_instances")
	if _, err := migratorDB.Exec(ctx, `create schema `+quotedSchema); err != nil {
		t.Fatalf("create direct-migrator fixed-surface schema: %v", err)
	}
	if _, err := migratorDB.Exec(ctx, `create table `+quotedLedger+` (name text primary key, checksum text)`); err != nil {
		t.Fatalf("create fixed-surface legacy-shaped ledger: %v", err)
	}
	if _, err := migratorDB.Exec(ctx, `insert into `+quotedLedger+` (name, checksum) values ('third_party_history_2026_07', 'not-an-app-checksum')`); err != nil {
		t.Fatalf("record unrelated fixed-surface ledger row: %v", err)
	}
	if _, err := migratorDB.Exec(ctx, `create table `+quotedMonitoringInstances+` (monitoring_instance_id text primary key)`); err != nil {
		t.Fatalf("create non-public fixed managed relation: %v", err)
	}
	before := readAppACLConvergenceRollbackState(t, ctx, fixture.db)

	_, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin)
	if err == nil || !strings.Contains(err.Error(), "managed object") {
		t.Fatalf("ConvergeAppACLR1() fixed-surface legacy error = %v, want non-public managed-object rejection", err)
	}
	after := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
	if after != before {
		t.Fatalf("non-public fixed-surface state after convergence = %#v, want no public replay/repair %#v", after, before)
	}
}

func TestPostgresIntegrationAppACLConvergenceAllowsDirectMigratorOwnedUnrelatedSchemaMigrations(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	const unrelatedSchema = "third_party_history"
	quotedSchema := quotePostgresIdentifier(unrelatedSchema)
	quotedLedger := quotedSchema + `.` + quotePostgresIdentifier("schema_migrations")
	if _, err := migratorDB.Exec(ctx, `create schema `+quotedSchema); err != nil {
		t.Fatalf("create direct-migrator unrelated schema: %v", err)
	}
	if _, err := migratorDB.Exec(ctx, `create table `+quotedLedger+` (name text primary key, checksum text)`); err != nil {
		t.Fatalf("create unrelated schema ledger: %v", err)
	}
	if _, err := migratorDB.Exec(ctx, `insert into `+quotedLedger+` (name, checksum) values ('third_party_history_2026_07', 'not-an-app-checksum')`); err != nil {
		t.Fatalf("record unrelated schema migration: %v", err)
	}

	if _, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin); err != nil {
		t.Fatalf("ConvergeAppACLR1() direct-migrator unrelated schema error = %v", err)
	}
	assertSingleIntValue(t, ctx, fixture.db, `select count(*)::int from `+quotedLedger, 1)
	assertSingleIntValue(t, ctx, fixture.db, `select count(*)::int from public.schema_migrations`, 52)
}

func TestPostgresIntegrationAppACLConvergenceIgnoresUnreadableUnrelatedMigrationLedger(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	const unrelatedSchema = "third_party_private_history"
	quotedSchema := quotePostgresIdentifier(unrelatedSchema)
	quotedLedger := quotedSchema + `.` + quotePostgresIdentifier("schema_migrations")
	if _, err := fixture.db.Exec(ctx, `create schema `+quotedSchema); err != nil {
		t.Fatalf("create unrelated private schema: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `create table `+quotedLedger+` (name text primary key, checksum text)`); err != nil {
		t.Fatalf("create unrelated private migration ledger: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into `+quotedLedger+` (name, checksum) values ('third_party_history_2026_07', 'not-an-app-checksum')`); err != nil {
		t.Fatalf("record unrelated private migration: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `revoke all privileges on table `+quotedLedger+` from public`); err != nil {
		t.Fatalf("revoke public access to unrelated private ledger: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `revoke all privileges on schema `+quotedSchema+` from public`); err != nil {
		t.Fatalf("revoke public usage on unrelated private schema: %v", err)
	}
	_, err := migratorDB.Exec(ctx, `select 1 from `+quotedLedger+` limit 1`)
	requirePostgresSQLState(t, err, "42501")

	if _, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin); err != nil {
		t.Fatalf("ConvergeAppACLR1() unreadable unrelated ledger error = %v", err)
	}
	assertSingleIntValue(t, ctx, fixture.db, `select count(*)::int from `+quotedLedger, 1)
	assertSingleIntValue(t, ctx, fixture.db, `select count(*)::int from public.schema_migrations`, 52)
}

func TestPostgresIntegrationAppACLConvergenceIgnoresMultipleUnreadableUnrelatedMigrationLedgers(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	for _, schemaName := range []string{
		"third_party_private_history_one",
		"third_party_private_history_two",
	} {
		quotedSchema := quotePostgresIdentifier(schemaName)
		quotedLedger := quotedSchema + `.` + quotePostgresIdentifier("schema_migrations")
		if _, err := fixture.db.Exec(ctx, `create schema `+quotedSchema); err != nil {
			t.Fatalf("create unrelated private schema %q: %v", schemaName, err)
		}
		if _, err := fixture.db.Exec(ctx, `create table `+quotedLedger+` (name text primary key, checksum text)`); err != nil {
			t.Fatalf("create unrelated private migration ledger in %q: %v", schemaName, err)
		}
		if _, err := fixture.db.Exec(ctx, `insert into `+quotedLedger+` (name, checksum) values ('third_party_history_2026_07', 'not-an-app-checksum')`); err != nil {
			t.Fatalf("record unrelated private migration in %q: %v", schemaName, err)
		}
		if _, err := fixture.db.Exec(ctx, `revoke all privileges on table `+quotedLedger+` from public`); err != nil {
			t.Fatalf("revoke public access to unrelated private ledger in %q: %v", schemaName, err)
		}
		if _, err := fixture.db.Exec(ctx, `revoke all privileges on schema `+quotedSchema+` from public`); err != nil {
			t.Fatalf("revoke public usage on unrelated private schema %q: %v", schemaName, err)
		}
		_, err := migratorDB.Exec(ctx, `select 1 from `+quotedLedger+` limit 1`)
		requirePostgresSQLState(t, err, "42501")
	}

	if _, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin); err != nil {
		t.Fatalf("ConvergeAppACLR1() multiple unreadable unrelated ledgers error = %v", err)
	}
	for _, schemaName := range []string{
		"third_party_private_history_one",
		"third_party_private_history_two",
	} {
		quotedLedger := quotePostgresIdentifier(schemaName) + `.` + quotePostgresIdentifier("schema_migrations")
		assertSingleIntValue(t, ctx, fixture.db, `select count(*)::int from `+quotedLedger, 1)
	}
	assertSingleIntValue(t, ctx, fixture.db, `select count(*)::int from public.schema_migrations`, 52)
}

func TestPostgresIntegrationAppACLConvergenceRejectsUnreadableDirectMigratorLedger(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	const legacySchema = "direct_migrator_private_history"
	quotedSchema := quotePostgresIdentifier(legacySchema)
	quotedLedger := quotedSchema + `.` + quotePostgresIdentifier("schema_migrations")
	quotedMigrator := quotePostgresIdentifier(fixture.migrator)
	if _, err := fixture.db.Exec(ctx, `create schema `+quotedSchema); err != nil {
		t.Fatalf("create direct-migrator private schema: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `grant usage, create on schema `+quotedSchema+` to `+quotedMigrator); err != nil {
		t.Fatalf("grant direct migrator schema usage/create: %v", err)
	}
	if _, err := migratorDB.Exec(ctx, `create table `+quotedLedger+` (name text primary key, checksum text)`); err != nil {
		t.Fatalf("create direct-migrator-owned private ledger: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `revoke all privileges on schema `+quotedSchema+` from `+quotedMigrator); err != nil {
		t.Fatalf("revoke direct migrator schema usage: %v", err)
	}
	_, err := migratorDB.Exec(ctx, `select 1 from `+quotedLedger+` limit 1`)
	requirePostgresSQLState(t, err, "42501")
	before := readAppACLConvergenceRollbackState(t, ctx, fixture.db)

	_, err = ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin)
	if err == nil || !strings.Contains(err.Error(), "read non-public migration ledger") {
		t.Fatalf("ConvergeAppACLR1() unreadable direct-migrator ledger error = %v, want fail-closed legacy read rejection", err)
	}
	after := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
	if after != before {
		t.Fatalf("unreadable direct-migrator ledger state = %#v, want no public replay/repair %#v", after, before)
	}
}

func TestPostgresIntegrationAppACLConvergenceRejectsDirectMigratorOwnedForcedRLSLedgerWithoutReplay(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	const legacySchema = "direct_migrator_forced_rls_history"
	quotedSchema := quotePostgresIdentifier(legacySchema)
	quotedLedger := quotedSchema + `.` + quotePostgresIdentifier("schema_migrations")
	if _, err := migratorDB.Exec(ctx, `create schema `+quotedSchema); err != nil {
		t.Fatalf("create forced-RLS legacy schema: %v", err)
	}
	if _, err := migratorDB.Exec(ctx, `create table `+quotedLedger+` (name text primary key, checksum text)`); err != nil {
		t.Fatalf("create forced-RLS direct-migrator ledger: %v", err)
	}
	sources := appACLConvergenceSourcesForPostgresTest(t)
	legacySource := sources.sources["0001_initial_schema.sql"]
	if _, err := migratorDB.Exec(ctx, `insert into `+quotedLedger+` (name, checksum) values ($1, $2)`, "0001_initial_schema.sql", legacySource.checksum); err != nil {
		t.Fatalf("record embedded migration in forced-RLS ledger: %v", err)
	}
	if _, err := migratorDB.Exec(ctx, `alter table `+quotedLedger+` enable row level security`); err != nil {
		t.Fatalf("enable forced-RLS ledger row security: %v", err)
	}
	if _, err := migratorDB.Exec(ctx, `alter table `+quotedLedger+` force row level security`); err != nil {
		t.Fatalf("force forced-RLS ledger row security: %v", err)
	}
	if _, err := migratorDB.Exec(ctx, `create policy app_acl_convergence_deny_all on `+quotedLedger+` using (false)`); err != nil {
		t.Fatalf("create forced-RLS ledger deny-all policy: %v", err)
	}

	var owner string
	var rowSecurityEnabled, rowSecurityForced bool
	if err := fixture.db.QueryRow(ctx, `
		select pg_catalog.pg_get_userbyid(relation.relowner), relation.relrowsecurity, relation.relforcerowsecurity
		from pg_catalog.pg_class relation
		join pg_catalog.pg_namespace namespace on namespace.oid = relation.relnamespace
		where namespace.nspname = $1
		  and relation.relname = 'schema_migrations'
		  and relation.relkind = 'r'
	`, legacySchema).Scan(&owner, &rowSecurityEnabled, &rowSecurityForced); err != nil {
		t.Fatalf("read forced-RLS ledger catalog facts: %v", err)
	}
	if owner != fixture.migrator || !rowSecurityEnabled || !rowSecurityForced {
		t.Fatalf("forced-RLS ledger catalog facts = owner %q, enabled %t, forced %t; want direct migrator owner with enabled forced row security", owner, rowSecurityEnabled, rowSecurityForced)
	}
	assertSingleIntValue(t, ctx, migratorDB, `select count(*)::int from `+quotedLedger, 0)
	assertSingleIntValue(t, ctx, fixture.db, `select count(*)::int from `+quotedLedger, 1)
	before := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
	if before.MigrationLedgerExists || before.MonitoringInstancesExists || before.ManifestRevisionsExists || before.ManifestHeadExists || before.InternalSchemaExists {
		t.Fatalf("forced-RLS ledger setup unexpectedly has public convergence state: %#v", before)
	}

	_, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin)
	if err == nil || !strings.Contains(err.Error(), "forced row security") {
		t.Fatalf("ConvergeAppACLR1() forced-RLS direct-migrator ledger error = %v, want fail-closed forced-row-security rejection", err)
	}
	after := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
	if after != before {
		t.Fatalf("forced-RLS direct-migrator ledger state = %#v, want no public replay/repair %#v", after, before)
	}
	assertSingleIntValue(t, ctx, fixture.db, `select count(*)::int from `+quotedLedger, 1)
}

func TestPostgresIntegrationAppACLConvergenceRejectsHeadLedgerHoleWithoutRepair(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	if _, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin); err != nil {
		t.Fatalf("ConvergeAppACLR1() setup error = %v", err)
	}
	if _, err := migratorDB.Exec(ctx, `delete from public.schema_migrations where name = '0051_create_record_platform_foundation.sql'`); err != nil {
		t.Fatalf("delete r1 migration ledger tail: %v", err)
	}
	for _, statement := range []string{
		`drop trigger rp_domain_identity_immutable on public.record_platform_domain_identity`,
		`drop trigger rp_domain_attestations_immutable on public.record_platform_domain_attestations`,
		`drop trigger app_acl_manifest_revisions_immutable on public.app_acl_manifest_revisions`,
	} {
		if _, err := migratorDB.Exec(ctx, statement); err != nil {
			t.Fatalf("remove r1 trigger for replay sentinel %q: %v", statement, err)
		}
	}
	var beforeHeadUpdatedAt time.Time
	if err := fixture.db.QueryRow(ctx, `select updated_at from public.app_acl_manifest_head where singleton`).Scan(&beforeHeadUpdatedAt); err != nil {
		t.Fatalf("read r1 head before ledger-hole convergence: %v", err)
	}

	_, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin)
	if err == nil || !strings.Contains(err.Error(), "ledger") {
		t.Fatalf("ConvergeAppACLR1() head ledger-hole error = %v, want read-only ledger rejection", err)
	}
	assertSingleIntValue(t, ctx, fixture.db, `select count(*)::int from public.schema_migrations`, 51)
	assertSingleIntValue(t, ctx, fixture.db, `
		select count(*)::int
		from pg_catalog.pg_trigger trigger
		where trigger.tgrelid in (
			'public.record_platform_domain_identity'::regclass,
			'public.record_platform_domain_attestations'::regclass,
			'public.app_acl_manifest_revisions'::regclass
		)
		  and trigger.tgname in (
			'rp_domain_identity_immutable',
			'rp_domain_attestations_immutable',
			'app_acl_manifest_revisions_immutable'
		)
	`, 0)
	var afterHeadUpdatedAt time.Time
	if err := fixture.db.QueryRow(ctx, `select updated_at from public.app_acl_manifest_head where singleton`).Scan(&afterHeadUpdatedAt); err != nil {
		t.Fatalf("read r1 head after ledger-hole convergence: %v", err)
	}
	if !afterHeadUpdatedAt.Equal(beforeHeadUpdatedAt) {
		t.Fatalf("head ledger-hole convergence updated manifest head at %s, want %s", afterHeadUpdatedAt, beforeHeadUpdatedAt)
	}
}

func TestPostgresIntegrationAppACLConvergenceRejectsHeadLedgerDriftWithoutRepair(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name   string
		mutate func(*testing.T, context.Context, *pgxpool.Pool)
		want   string
	}{
		{
			name: "unknown_filename",
			mutate: func(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
				t.Helper()
				if _, err := db.Exec(ctx, `insert into public.schema_migrations (name, checksum) values ('9999_unknown_app_history.sql', $1)`, strings.Repeat("a", 64)); err != nil {
					t.Fatalf("insert unknown head ledger row: %v", err)
				}
			},
			want: "exact prefix",
		},
		{
			name: "checksum_mismatch",
			mutate: func(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
				t.Helper()
				if _, err := db.Exec(ctx, `update public.schema_migrations set checksum = $1 where name = '0051_create_record_platform_foundation.sql'`, strings.Repeat("0", 64)); err != nil {
					t.Fatalf("mutate head ledger checksum: %v", err)
				}
			},
			want: "checksum mismatch",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newAppACLConvergencePostgresFixture(t, ctx)
			migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)
			if _, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin); err != nil {
				t.Fatalf("ConvergeAppACLR1() setup error = %v", err)
			}
			tc.mutate(t, ctx, migratorDB)
			before := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
			beforeLedger := readAppACLConvergenceLedgerState(t, ctx, fixture.db)
			beforeRevisions := readAppACLConvergenceManifestRevisionState(t, ctx, fixture.db)
			beforeHead := readAppACLLegacyManifestHeadState(t, ctx, fixture.db, "public")

			_, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ConvergeAppACLR1() head %s error = %v, want read-only ledger rejection containing %q", tc.name, err, tc.want)
			}
			after := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
			if after != before {
				t.Fatalf("head %s convergence state = %#v, want no ACL/catalog mutation %#v", tc.name, after, before)
			}
			if afterLedger := readAppACLConvergenceLedgerState(t, ctx, fixture.db); afterLedger != beforeLedger {
				t.Fatalf("head %s ledger = %q, want unchanged %q", tc.name, afterLedger, beforeLedger)
			}
			if afterRevisions := readAppACLConvergenceManifestRevisionState(t, ctx, fixture.db); afterRevisions != beforeRevisions {
				t.Fatalf("head %s revisions = %q, want unchanged %q", tc.name, afterRevisions, beforeRevisions)
			}
			if afterHead := readAppACLLegacyManifestHeadState(t, ctx, fixture.db, "public"); afterHead != beforeHead {
				t.Fatalf("head %s manifest head = %#v, want unchanged %#v", tc.name, afterHead, beforeHead)
			}
		})
	}
}

func TestPostgresIntegrationAppACLConvergenceRejectsPgcryptoOutsideInternalSchemaWithoutFragments(t *testing.T) {
	ctx := context.Background()
	fixture := newAppACLConvergencePostgresFixture(t, ctx)
	migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)
	if _, err := migratorDB.Exec(ctx, `create extension pgcrypto with schema public`); err != nil {
		t.Fatalf("preinstall pgcrypto in public as direct migrator: %v", err)
	}
	before := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
	if before.PgcryptoExtensionSchemaName != "public" || before.MigrationLedgerExists || before.MonitoringInstancesExists || before.ManifestRevisionsExists || before.ManifestHeadExists || before.InternalSchemaExists {
		t.Fatalf("wrong-schema pgcrypto setup state = %#v", before)
	}

	_, err := ConvergeAppACLR1(ctx, migratorDB, fixture.runtime, fixture.admin)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55000" || pgErr.Message != "pgcrypto must be installed in record_platform_internal" {
		t.Fatalf("ConvergeAppACLR1() wrong-schema pgcrypto error = %v, want SQLSTATE 55000 schema rejection", err)
	}
	after := readAppACLConvergenceRollbackState(t, ctx, fixture.db)
	if after != before {
		t.Fatalf("wrong-schema pgcrypto state after rejected convergence = %#v, want no fragment/no repair %#v", after, before)
	}
}

type appACLConvergenceRollbackState struct {
	DatabaseACL                 string
	PublicSchemaACL             string
	InternalSchemaACL           string
	RelationACLState            string
	FunctionACLState            string
	MigrationLedgerExists       bool
	MonitoringInstancesExists   bool
	ManifestRevisionsExists     bool
	ManifestHeadExists          bool
	InternalSchemaExists        bool
	PgcryptoExtensionSchemaName string
}

func readAppACLConvergenceRollbackState(t *testing.T, ctx context.Context, db *pgxpool.Pool) appACLConvergenceRollbackState {
	t.Helper()
	var state appACLConvergenceRollbackState
	if err := db.QueryRow(ctx, `
		select coalesce(database.datacl::text, ''),
		       coalesce(public_namespace.nspacl::text, ''),
		       coalesce(internal_namespace.nspacl::text, ''),
		       coalesce((
				select string_agg(
					namespace.nspname || ':' || relation.relkind::text || ':' || relation.relname || ':' || coalesce(relation.relacl::text, ''),
					E'\n' order by namespace.nspname, relation.relkind, relation.relname
				)
				from pg_catalog.pg_class relation
				join pg_catalog.pg_namespace namespace on namespace.oid = relation.relnamespace
				where namespace.nspname in ('public', 'record_platform_internal')
			), ''),
		       coalesce((
				select string_agg(
					namespace.nspname || ':' || procedure.proname || ':' || pg_catalog.pg_get_function_identity_arguments(procedure.oid) || ':' || coalesce(procedure.proacl::text, ''),
					E'\n' order by namespace.nspname, procedure.proname, pg_catalog.pg_get_function_identity_arguments(procedure.oid)
				)
				from pg_catalog.pg_proc procedure
				join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
				where namespace.nspname in ('public', 'record_platform_internal')
			), ''),
		       pg_catalog.to_regclass('public.schema_migrations') is not null,
		       pg_catalog.to_regclass('public.monitoring_instances') is not null,
		       pg_catalog.to_regclass('public.app_acl_manifest_revisions') is not null,
		       pg_catalog.to_regclass('public.app_acl_manifest_head') is not null,
		       exists (select 1 from pg_catalog.pg_namespace where nspname = 'record_platform_internal'),
		       coalesce((
				select extension_namespace.nspname
				from pg_catalog.pg_extension extension
				join pg_catalog.pg_namespace extension_namespace on extension_namespace.oid = extension.extnamespace
				where extension.extname = 'pgcrypto'
			), '')
		from pg_catalog.pg_database database
		join pg_catalog.pg_namespace public_namespace on public_namespace.nspname = 'public'
		left join pg_catalog.pg_namespace internal_namespace on internal_namespace.nspname = 'record_platform_internal'
		where database.datname = pg_catalog.current_database()
	`).Scan(
		&state.DatabaseACL,
		&state.PublicSchemaACL,
		&state.InternalSchemaACL,
		&state.RelationACLState,
		&state.FunctionACLState,
		&state.MigrationLedgerExists,
		&state.MonitoringInstancesExists,
		&state.ManifestRevisionsExists,
		&state.ManifestHeadExists,
		&state.InternalSchemaExists,
		&state.PgcryptoExtensionSchemaName,
	); err != nil {
		t.Fatalf("read app ACL convergence rollback state: %v", err)
	}
	return state
}

func readAppACLConvergenceLedgerState(t *testing.T, ctx context.Context, db *pgxpool.Pool) string {
	t.Helper()
	var state string
	if err := db.QueryRow(ctx, `
		select coalesce(string_agg(name || ':' || checksum, E'\n' order by name::text COLLATE "C"), '')
		from public.schema_migrations
	`).Scan(&state); err != nil {
		t.Fatalf("read app ACL convergence migration ledger state: %v", err)
	}
	return state
}

func readAppACLConvergenceManifestRevisionState(t *testing.T, ctx context.Context, db *pgxpool.Pool) string {
	t.Helper()
	var state string
	if err := db.QueryRow(ctx, `
		select coalesce(string_agg(
			manifest_revision::text || ':' || migrator_catalog_role || ':' || encode(manifest_digest, 'hex'),
			E'\n' order by manifest_revision
		), '')
		from public.app_acl_manifest_revisions
	`).Scan(&state); err != nil {
		t.Fatalf("read app ACL convergence manifest revision state: %v", err)
	}
	return state
}

func appACLConvergenceSourcesForPostgresTest(t *testing.T) migrationSourceSnapshot {
	t.Helper()
	sources, err := snapshotMigrationSources(appACLR1InjectedMigrationFS(t))
	if err != nil {
		t.Fatalf("snapshotMigrationSources() error = %v", err)
	}
	return sources
}

type appACLLegacyConvergenceState struct {
	ManifestHeadSchema    string
	ManifestHeadOwner     string
	ManifestHeadACL       string
	MigrationLedgerCount  int
	ManifestRevisionCount int
	DatabaseACL           string
	PublicSchemaACL       string
}

func readAppACLLegacyConvergenceState(t *testing.T, ctx context.Context, db *pgxpool.Pool) appACLLegacyConvergenceState {
	t.Helper()
	var state appACLLegacyConvergenceState
	if err := db.QueryRow(ctx, `
		select relation_namespace.nspname,
		       pg_catalog.pg_get_userbyid(relation.relowner),
		       coalesce(relation.relacl::text, ''),
		       (select count(*)::int from public.schema_migrations),
		       (select count(*)::int from public.app_acl_manifest_revisions),
		       coalesce(database.datacl::text, ''),
		       coalesce(public_namespace.nspacl::text, '')
		from pg_catalog.pg_class relation
		join pg_catalog.pg_namespace relation_namespace on relation_namespace.oid = relation.relnamespace
		join pg_catalog.pg_database database on database.datname = pg_catalog.current_database()
		join pg_catalog.pg_namespace public_namespace on public_namespace.nspname = 'public'
		where relation.relname = 'app_acl_manifest_head'
		  and relation.relkind = 'r'
	`).Scan(
		&state.ManifestHeadSchema,
		&state.ManifestHeadOwner,
		&state.ManifestHeadACL,
		&state.MigrationLedgerCount,
		&state.ManifestRevisionCount,
		&state.DatabaseACL,
		&state.PublicSchemaACL,
	); err != nil {
		t.Fatalf("read legacy app ACL convergence state: %v", err)
	}
	return state
}

type appACLLegacyManifestHeadState struct {
	Singleton         bool
	ManifestRevision  int64
	ManifestDigestHex string
	UpdatedAt         time.Time
}

func readAppACLLegacyManifestHeadState(t *testing.T, ctx context.Context, db *pgxpool.Pool, schema string) appACLLegacyManifestHeadState {
	t.Helper()
	var state appACLLegacyManifestHeadState
	if err := db.QueryRow(ctx, `
		select singleton,
		       coalesce(manifest_revision, 0),
		       coalesce(encode(manifest_digest, 'hex'), ''),
		       updated_at
		from `+quotePostgresIdentifier(schema)+`.`+quotePostgresIdentifier("app_acl_manifest_head")+`
		where singleton
	`).Scan(&state.Singleton, &state.ManifestRevision, &state.ManifestDigestHex, &state.UpdatedAt); err != nil {
		t.Fatalf("read legacy manifest head state in %q: %v", schema, err)
	}
	return state
}

type appACLConvergencePostgresFixture struct {
	db             *pgxpool.Pool
	databaseName   string
	bootstrapOwner string
	runtime        string
	admin          string
	migrator       string
	rolePasswords  map[string]string
}

func newAppACLConvergencePostgresFixture(t *testing.T, ctx context.Context) appACLConvergencePostgresFixture {
	t.Helper()
	db := openTemporaryPostgresDatabase(t, ctx)
	roles := appACLEffectiveCatalogTestRoleNames()
	fixture := appACLConvergencePostgresFixture{
		db:            db,
		runtime:       roles.centerRuntime,
		admin:         roles.platformAdmin,
		migrator:      roles.migrator,
		rolePasswords: make(map[string]string, 3),
	}
	if err := db.QueryRow(ctx, `select pg_catalog.current_database(), current_user`).Scan(&fixture.databaseName, &fixture.bootstrapOwner); err != nil {
		t.Fatalf("read fresh convergence database identity: %v", err)
	}
	for _, role := range []string{fixture.runtime, fixture.admin, fixture.migrator} {
		password := appACLEffectiveCatalogTemporaryPassword(t)
		fixture.rolePasswords[role] = password
		quotedRole := quotePostgresIdentifier(role)
		if _, err := db.Exec(ctx, `create role `+quotedRole+` login noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls password '`+password+`'`); err != nil {
			t.Fatalf("create direct convergence role %q: %v", role, err)
		}
		fixture.dropRole(t, role)
	}
	quotedDatabase := quotePostgresIdentifier(fixture.databaseName)
	quotedMigrator := quotePostgresIdentifier(fixture.migrator)
	quotedBootstrapOwner := quotePostgresIdentifier(fixture.bootstrapOwner)
	if _, err := db.Exec(ctx, `alter database `+quotedDatabase+` owner to `+quotedMigrator); err != nil {
		t.Fatalf("assign fresh convergence database to direct migrator: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := db.Exec(cleanupCtx, `alter database `+quotedDatabase+` owner to `+quotedBootstrapOwner); err != nil {
			t.Errorf("restore fresh convergence database owner %q: %v", fixture.bootstrapOwner, err)
		}
	})
	return fixture
}

func (fixture appACLConvergencePostgresFixture) openDirectRolePool(t *testing.T, ctx context.Context, role string) *pgxpool.Pool {
	t.Helper()
	password, ok := fixture.rolePasswords[role]
	if !ok {
		t.Fatalf("no direct convergence password for role %q", role)
	}
	config := fixture.db.Config().Copy()
	config.MaxConns = 1
	config.MinConns = 0
	config.ConnConfig.User = role
	config.ConnConfig.Password = password
	config.AfterConnect = nil
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open direct convergence role pool %q: %v", role, err)
	}
	t.Cleanup(pool.Close)
	var sessionUser, currentUser string
	if err := pool.QueryRow(ctx, `select session_user, current_user`).Scan(&sessionUser, &currentUser); err != nil {
		t.Fatalf("read direct convergence role %q identities: %v", role, err)
	}
	if sessionUser != role || currentUser != role {
		t.Fatalf("direct convergence role %q identities = (%q, %q), want (%q, %q)", role, sessionUser, currentUser, role, role)
	}
	return pool
}

func (fixture appACLConvergencePostgresFixture) dropRole(t *testing.T, role string) {
	t.Helper()
	quotedRole := quotePostgresIdentifier(role)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if role == fixture.migrator {
			if _, err := fixture.db.Exec(cleanupCtx, `reassign owned by `+quotedRole+` to `+quotePostgresIdentifier(fixture.bootstrapOwner)); err != nil {
				t.Errorf("reassign direct convergence migrator %q ownership: %v", role, err)
			}
		}
		if _, err := fixture.db.Exec(cleanupCtx, `drop owned by `+quotedRole); err != nil {
			t.Errorf("drop owned by direct convergence role %q: %v", role, err)
		}
		if _, err := fixture.db.Exec(cleanupCtx, `drop role if exists `+quotedRole); err != nil {
			t.Errorf("drop direct convergence role %q: %v", role, err)
		}
	})
}
