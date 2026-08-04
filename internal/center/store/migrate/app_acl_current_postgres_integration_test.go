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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
)

func TestPostgresIntegrationAppACLCurrent(t *testing.T) {
	t.Run("fresh_and_runtime", testPostgresIntegrationAppACLCurrentFreshAndRuntime)
	t.Run("exact_repeat_is_read_only", testPostgresIntegrationAppACLCurrentExactRepeat)
	t.Run("prior_baseline_requires_rebuild_without_mutation", testPostgresIntegrationAppACLCurrentPriorBaseline)
	t.Run("unrelated_same_name_objects_are_ignored", testPostgresIntegrationAppACLCurrentUnrelatedSameNames)
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
	if len(snapshot.Ledger) != len(source.sources.names) {
		t.Fatalf("fresh durable ledger row count = %d, want %d", len(snapshot.Ledger), len(source.sources.names))
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
	assertRecordsCoreAppACLCurrentRolePrivileges(t, ctx, &fixture, runtimeDB)
}

func assertRecordsCoreAppACLCurrentRolePrivileges(
	t *testing.T,
	ctx context.Context,
	fixture *appACLConvergencePostgresFixture,
	runtimeDB *pgxpool.Pool,
) {
	t.Helper()

	if _, err := runtimeDB.Exec(ctx, `insert into public.records (record_id) values ('rec_acl')`); err != nil {
		t.Fatalf("runtime insert records-core root: %v", err)
	}
	revisionTx, err := runtimeDB.Begin(ctx)
	if err != nil {
		t.Fatalf("runtime begin records-core revision transaction: %v", err)
	}
	defer func() { _ = revisionTx.Rollback(ctx) }()
	if _, err := revisionTx.Exec(ctx, `
		insert into public.record_revisions (
			revision_id, record_id, revision_no, title, body_markdown,
			markdown_dialect_version, record_type, impact_level,
			visibility_scope, visibility_digest, author_id, canonical_hash
		) values (
			'rrv_acl', 'rec_acl', 1, 'ACL', '# ACL', 1, 'note', 'informational',
			'{}'::jsonb, decode(repeat('41', 32), 'hex'), 'usr_acl', decode(repeat('42', 32), 'hex')
		)
	`); err != nil {
		t.Fatalf("runtime insert immutable records-core revision: %v", err)
	}
	if _, err := revisionTx.Exec(ctx, `
		insert into public.record_revision_subjects (
			revision_id, ordinal, registry_version, subject_kind, relation_role,
			source_id, is_primary, identity_snapshot, capture_authorization,
			capture_authorization_digest
		) values (
			'rrv_acl', 0, 1, 'vps', 'affected', 'vps_acl', true,
			'{}'::jsonb, '{}'::jsonb, decode(repeat('43', 32), 'hex')
		)
	`); err != nil {
		t.Fatalf("runtime insert records-core primary subject: %v", err)
	}
	if err := revisionTx.Commit(ctx); err != nil {
		t.Fatalf("runtime commit records-core revision transaction: %v", err)
	}
	assertRecordAttachmentsAppACLCurrentRolePrivileges(t, ctx, fixture, runtimeDB)

	_, err = runtimeDB.Exec(ctx, `update public.record_revisions set title = 'mutated' where revision_id = 'rrv_acl'`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("runtime update immutable records-core revision error = %v, want SQLSTATE 42501", err)
	}

	purgeTx, err := runtimeDB.Begin(ctx)
	if err != nil {
		t.Fatalf("runtime begin records-core purge transaction: %v", err)
	}
	defer func() { _ = purgeTx.Rollback(ctx) }()
	for _, statement := range []string{
		`delete from public.record_revision_subjects where revision_id = 'rrv_acl'`,
		`delete from public.record_revisions where revision_id = 'rrv_acl'`,
		`delete from public.records where record_id = 'rec_acl'`,
	} {
		if _, err := purgeTx.Exec(ctx, statement); err != nil {
			t.Fatalf("runtime execute records-core purge statement %q: %v", statement, err)
		}
	}
	if err := purgeTx.Commit(ctx); err != nil {
		t.Fatalf("runtime commit records-core purge transaction: %v", err)
	}

	adminDB := fixture.openDirectRolePool(t, ctx, fixture.admin)
	var receiptCount int
	if err := adminDB.QueryRow(ctx, `select count(*)::int from public.record_core_purge_receipts`).Scan(&receiptCount); err != nil {
		t.Fatalf("platform admin read content-free records-core purge receipts: %v", err)
	}
	if receiptCount != 0 {
		t.Fatalf("fresh records-core purge receipt count = %d, want 0", receiptCount)
	}
	_, err = adminDB.Exec(ctx, `select record_id from public.records limit 1`)
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("platform admin read records-core content table error = %v, want SQLSTATE 42501", err)
	}
}

func assertRecordAttachmentsAppACLCurrentRolePrivileges(
	t *testing.T,
	ctx context.Context,
	fixture *appACLConvergencePostgresFixture,
	runtimeDB *pgxpool.Pool,
) {
	t.Helper()

	if _, err := runtimeDB.Exec(ctx, `
		insert into public.blob_objects (
			blob_key, sha256_digest, object_version, size_bytes, backend_kind
		) values (
			'sha256/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
			decode(repeat('aa', 32), 'hex'), 'local-v1', 7, 'local'
		)
	`); err != nil {
		t.Fatalf("runtime insert attachment blob: %v", err)
	}
	if _, err := runtimeDB.Exec(ctx, `insert into public.attachment_quota_accounts (project_id) values ('default')`); err != nil {
		t.Fatalf("runtime insert attachment quota account: %v", err)
	}
	if _, err := runtimeDB.Exec(ctx, `
		update public.attachment_quota_accounts
		set logical_bytes = 7, physical_bytes = 7, quota_version = quota_version + 1
		where project_id = 'default'
	`); err != nil {
		t.Fatalf("runtime update attachment quota account: %v", err)
	}
	if _, err := runtimeDB.Exec(ctx, `
		insert into public.record_attachments (
			attachment_id, record_id, attachment_state, display_name, media_type,
			logical_size_bytes, blob_key, blob_object_version, created_by
		) values (
			'att_acl', 'rec_acl', 'available', 'acl.txt', 'text/plain', 7,
			'sha256/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',
			'local-v1', 'usr_acl'
		)
	`); err != nil {
		t.Fatalf("runtime insert logical attachment: %v", err)
	}
	if _, err := runtimeDB.Exec(ctx, `
		insert into public.record_revision_attachments (
			record_id, revision_id, ordinal, attachment_id
		) values ('rec_acl', 'rrv_acl', 0, 'att_acl')
	`); err != nil {
		t.Fatalf("runtime insert immutable revision attachment: %v", err)
	}

	for _, mutation := range []struct {
		name      string
		statement string
	}{
		{name: "blob", statement: `update public.blob_objects set size_bytes = 8 where blob_key like 'sha256/%'`},
		{name: "revision reference", statement: `update public.record_revision_attachments set ordinal = 1 where revision_id = 'rrv_acl'`},
	} {
		_, err := runtimeDB.Exec(ctx, mutation.statement)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Fatalf("runtime update immutable attachment %s error = %v, want SQLSTATE 42501", mutation.name, err)
		}
	}

	for _, statement := range []string{
		`delete from public.record_revision_attachments where revision_id = 'rrv_acl'`,
		`delete from public.record_attachments where attachment_id = 'att_acl'`,
		`delete from public.blob_objects where blob_key like 'sha256/%'`,
		`delete from public.attachment_quota_accounts where project_id = 'default'`,
	} {
		if _, err := runtimeDB.Exec(ctx, statement); err != nil {
			t.Fatalf("runtime clean attachment ACL fixture with %q: %v", statement, err)
		}
	}

	adminDB := fixture.openDirectRolePool(t, ctx, fixture.admin)
	for _, table := range []string{"attachment_purge_receipts", "content_workspace_purge_receipts"} {
		var count int
		if err := adminDB.QueryRow(ctx, `select count(*)::int from public.`+table).Scan(&count); err != nil {
			t.Fatalf("platform admin read content-free %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("fresh %s count = %d, want 0", table, count)
		}
	}
	_, err := adminDB.Exec(ctx, `select blob_key from public.blob_objects limit 1`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("platform admin read attachment content table error = %v, want SQLSTATE 42501", err)
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

func testPostgresIntegrationAppACLCurrentUnrelatedSameNames(t *testing.T) {
	for _, tc := range []struct {
		name      string
		createSQL string
	}{
		{
			name:      "relation",
			createSQL: `create table third_party_current.monitoring_instances (id bigint primary key)`,
		},
		{
			name: "function",
			createSQL: `
				create function third_party_current.record_platform_cas_contract_activation_projection(bytea)
				returns bytea language sql immutable as $$ select $1 $$
			`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := newAppACLConvergencePostgresFixture(t, ctx)
			migratorDB := fixture.openDirectRolePool(t, ctx, fixture.migrator)
			if _, err := migratorDB.Exec(ctx, `create schema third_party_current`); err != nil {
				t.Fatalf("create unrelated current schema: %v", err)
			}
			if _, err := migratorDB.Exec(ctx, tc.createSQL); err != nil {
				t.Fatalf("create unrelated same-name %s: %v", tc.name, err)
			}

			if _, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtime, fixture.admin); err != nil {
				t.Fatalf("ConvergeAppACLCurrent() with unrelated same-name %s error = %v", tc.name, err)
			}
			runtimeDB := fixture.openDirectRolePool(t, ctx, fixture.runtime)
			if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
				t.Fatalf("AdmitAppACLCurrentRuntime() with unrelated same-name %s error = %v", tc.name, err)
			}
		})
	}
}

type appACLCurrentPostgresDurableSnapshot struct {
	Manifest      AppACLManifestRuntimeSnapshotV1
	Catalog       AppACLEffectiveCatalogSnapshotR1
	Ledger        []appACLCurrentPostgresLedgerRow
	HeadUpdatedAt time.Time
}

type appACLCurrentPostgresLedgerRow struct {
	Name      string
	Checksum  string
	AppliedAt time.Time
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
	ledgerRows, err := tx.Query(ctx, `
		select name, checksum, applied_at
		from public.schema_migrations
		order by name::text collate "C"
	`)
	if err != nil {
		t.Fatalf("read current durable ledger rows: %v", err)
	}
	ledger := make([]appACLCurrentPostgresLedgerRow, 0, len(manifest.AppliedMigrations))
	for ledgerRows.Next() {
		var row appACLCurrentPostgresLedgerRow
		if err := ledgerRows.Scan(&row.Name, &row.Checksum, &row.AppliedAt); err != nil {
			ledgerRows.Close()
			t.Fatalf("scan current durable ledger row: %v", err)
		}
		ledger = append(ledger, row)
	}
	if err := ledgerRows.Err(); err != nil {
		ledgerRows.Close()
		t.Fatalf("iterate current durable ledger rows: %v", err)
	}
	ledgerRows.Close()
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
		Ledger:        ledger,
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
