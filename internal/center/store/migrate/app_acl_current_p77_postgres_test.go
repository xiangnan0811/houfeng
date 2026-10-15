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

const appACLCurrentP78Migration = "0078_ip_quality_collection_scope.sql"

func assertAppACLCurrentLedgerSuffix(t *testing.T, before, after []appACLCurrentPostgresLedgerRow, names ...string) {
	t.Helper()
	if len(after) != len(before)+len(names) || !reflect.DeepEqual(after[:len(before)], before) {
		t.Fatalf("migration ledger must preserve its exact predecessor prefix and append %v\nbefore: %#v\nafter: %#v", names, before, after)
	}
	for i, name := range names {
		if after[len(before)+i].Name != name {
			t.Fatalf("migration ledger suffix[%d] = %q, want %q", i, after[len(before)+i].Name, name)
		}
	}
}

func TestPostgresIntegrationIPQualityCollectionScopeMigration(t *testing.T) {
	t.Run("fresh_c78", testPostgresIntegrationAppACLCurrentP75Fresh)
	for _, chained := range []bool{false, true} {
		name := "exact_c77_genesis"
		if chained {
			name = "released_c76_c77_chain"
		}
		t.Run(name, func(t *testing.T) {
			testPostgresIntegrationAppACLCurrentP77Upgrade(t, chained)
		})
	}
	for _, cutpoint := range []string{"after_migration", "after_manifest_head"} {
		t.Run("rollback_"+cutpoint, func(t *testing.T) {
			testPostgresIntegrationAppACLCurrentP77Rollback(t, cutpoint)
		})
	}
	for _, current := range []bool{false, true} {
		phase := "predecessor_"
		if current {
			phase = "current_"
		}
		for _, drift := range []string{"missing_select", "runtime_update", "admin_select", "public_select", "grant_option", "ledger_checksum"} {
			t.Run(phase+drift, func(t *testing.T) {
				testPostgresIntegrationAppACLCurrentP77Drift(t, current, drift)
			})
		}
	}
}

func seedAppACLCurrentP77Predecessor(t *testing.T, ctx context.Context, fixture exactAppACLCurrentSuccessorPostgresFixture, db *pgxpool.Pool, chained bool) AppACLManifestPersistedV1 {
	t.Helper()
	c77 := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP77Migration)
	if !chained {
		manifest, _, _ := seedAppACLCurrentReleasedGenesis(t, ctx, fixture, db, c77)
		return manifest
	}
	c76 := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP76Migration)
	first, _, _ := seedAppACLCurrentReleasedGenesis(t, ctx, fixture, db, c76)
	_, _, input := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input)
	second, err := appendAppACLCurrentReleasedSuccessor(t, ctx, fixture, db, c77)
	if err != nil {
		t.Fatalf("materialize independently frozen C77 successor: %v", err)
	}
	after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input)
	assertAppACLCurrentManifestHistoryPrefix(t, before.Manifest.Manifests, after.Manifest.Manifests)
	assertAppACLCurrentLedgerSuffix(t, before.Ledger, after.Ledger, appACLCurrentP77Migration)
	if second.ManifestRevision != first.ManifestRevision+1 || second.PreviousManifestDigest != first.ManifestDigest ||
		!appACLCurrentPrivilegesWithProductTables(after.Catalog.DirectPrivileges, before.Catalog.DirectPrivileges, fixture.runtimeRole) ||
		!appACLCurrentPrivilegesWithProductTables(after.Catalog.EffectivePrivileges, before.Catalog.EffectivePrivileges, fixture.runtimeRole) {
		t.Fatal("released C76 to C77 fixture did not preserve its exact manifest link and seven-grant delta")
	}
	return second
}

func testPostgresIntegrationAppACLCurrentP77Upgrade(t *testing.T, chained bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	db := fixture.openRolePool(t, ctx, fixture.migratorRole)
	predecessor := seedAppACLCurrentP77Predecessor(t, ctx, fixture, db, chained)
	seedAppACLCurrentP77Rows(t, ctx, db)
	_, _, input := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input)
	beforeRows := readAppACLCurrentP77Rows(t, ctx, db)
	beforeSettings := readAppACLCurrentSettingsPresenceSnapshot(t, ctx, db)
	if len(before.Ledger) != 78 || before.Ledger[77].Name != appACLCurrentP77Migration {
		t.Fatalf("predecessor must be exact frozen C77: %#v", before.Ledger)
	}
	runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)
	assertAppACLCurrentP77FamilyAvailability(t, ctx, runtimeDB, "unknown", false)
	successor, err := ConvergeAppACLCurrent(ctx, db, fixture.runtimeRole, fixture.adminRole)
	if err != nil {
		t.Fatalf("C77 to C78 convergence: %v", err)
	}
	if successor.ManifestRevision != predecessor.ManifestRevision+1 || successor.PreviousManifestDigest != predecessor.ManifestDigest {
		t.Fatalf("C78 must append exactly one manifest linked to C77: %#v", successor)
	}
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
		t.Fatalf("C78 app-role admission: %v", err)
	}
	after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input)
	assertAppACLCurrentManifestHistoryPrefix(t, before.Manifest.Manifests, after.Manifest.Manifests)
	if len(after.Manifest.Manifests) != len(before.Manifest.Manifests)+1 {
		t.Fatalf("C78 changed the historical manifest inventory: %#v", after.Manifest)
	}
	assertAppACLCurrentLedgerSuffix(t, before.Ledger, after.Ledger, appACLCurrentP78Migration)
	if !reflect.DeepEqual(after.Catalog, before.Catalog) {
		t.Fatal("C78 must preserve all owners, direct/effective privileges, column/default ACLs and functions without new grants")
	}
	if readAppACLCurrentP77Rows(t, ctx, db) != beforeRows {
		t.Fatal("C78 rewrote historical reports, provider/service facts, product state or source cache")
	}
	assertAppACLCurrentSettingsPresenceSnapshotEqual(t, beforeSettings, readAppACLCurrentSettingsPresenceSnapshot(t, ctx, db))
	assertAppACLCurrentP77FamilyAvailability(t, ctx, runtimeDB, "unavailable", true)
	repeated, err := ConvergeAppACLCurrent(ctx, db, fixture.runtimeRole, fixture.adminRole)
	if err != nil {
		t.Fatalf("C78 repeat: %v", err)
	}
	if repeated.ManifestDigest != successor.ManifestDigest ||
		!reflect.DeepEqual(readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input), after) ||
		readAppACLCurrentP77Rows(t, ctx, db) != beforeRows {
		t.Fatal("C78 repeat changed durable state or historical facts")
	}
}

func seedAppACLCurrentP77Rows(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	seedAppACLCurrentP76IPQualityRows(t, ctx, db)
	if _, err := db.Exec(ctx, `
		update public.vps_assets set ipv6 = '2001:db8::77' where vps_id = 'vps_p76_existing';
		insert into public.vps_monitoring_instance_links (link_id, vps_id, monitoring_instance_id)
		values ('link_p77_existing', 'vps_p76_existing', 'mi_p76_existing');
		update public.ip_quality_reports set observed_at = now(), received_at = now(),
			ip_address = '0.0.0.0', status = 'failure', error_code = 'address_family_unavailable',
			diagnostics_json = '{"collection_revision":5,"collection_id":"c77-single","expected_ip_versions":[4],"ip_address_family":4,"family_availability":"unavailable"}',
			product_json = '{"revision":1}', product_summary_json = '{"historical":true}'
		where report_id = 'ipq_p76_existing';
		insert into public.ip_quality_intelligence (report_id, state, deadline_at, data_json)
		values ('ipq_p76_existing', 'pending', now() + interval '10 minutes', '{"historical":true}');
		insert into public.ip_quality_source_cache (source_id, resource, credential_fingerprint, fetched_at, expires_at, payload_json)
		values ('ripe_network', '203.0.113.76', '', now(), now() + interval '1 hour', '{"historical":true}');
	`); err != nil {
		t.Fatalf("seed C77 persisted report/product/cache facts: %v", err)
	}
}

func readAppACLCurrentP77Rows(t *testing.T, ctx context.Context, db *pgxpool.Pool) string {
	t.Helper()
	var body string
	if err := db.QueryRow(ctx, `select jsonb_build_object(
		'reports', (select jsonb_agg(to_jsonb(r) order by report_id) from public.ip_quality_reports r),
		'providers', (select jsonb_agg(to_jsonb(p) order by result_id) from public.ip_quality_provider_results p),
		'services', (select jsonb_agg(to_jsonb(s) order by unlock_id) from public.ip_quality_service_unlocks s),
		'intelligence', (select jsonb_agg(to_jsonb(i) order by report_id) from public.ip_quality_intelligence i),
		'cache', (select jsonb_agg(to_jsonb(c) order by source_id, resource, credential_fingerprint) from public.ip_quality_source_cache c)
	)::text`).Scan(&body); err != nil {
		t.Fatalf("read complete persisted C77 facts: %v", err)
	}
	return body
}

func assertAppACLCurrentP77FamilyAvailability(t *testing.T, ctx context.Context, db *pgxpool.Pool, want string, ipv6Default bool) {
	t.Helper()
	var availability string
	var defaultFamily int
	var attempt string
	var latestReport *string
	if err := db.QueryRow(ctx, `select availability, latest_attempt_id, latest_report_id,
		(select ip_version from public.ip_quality_vps_family_state where vps_id = 'vps_p76_existing' and is_default)
		from public.ip_quality_vps_family_state where vps_id = 'vps_p76_existing' and ip_version = 4`,
	).Scan(&availability, &attempt, &latestReport, &defaultFamily); err != nil {
		t.Fatalf("app role family-state view access: %v", err)
	}
	wantDefault := 4
	if ipv6Default {
		wantDefault = 6
	}
	if availability != want || attempt != "ipq_p76_existing" || latestReport != nil || defaultFamily != wantDefault {
		t.Fatalf("family-state = %s/%s/%v/default=%d, want %s/current attempt/no valid placeholder/default=%d", availability, attempt, latestReport, defaultFamily, want, wantDefault)
	}
}

func testPostgresIntegrationAppACLCurrentP77Rollback(t *testing.T, cutpoint string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	db := fixture.openRolePool(t, ctx, fixture.migratorRole)
	seedAppACLCurrentP77Predecessor(t, ctx, fixture, db, false)
	seedAppACLCurrentP77Rows(t, ctx, db)
	_, _, input := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input)
	beforeRows := readAppACLCurrentP77Rows(t, ctx, db)
	var beforeView string
	if err := db.QueryRow(ctx, `select pg_get_viewdef('public.ip_quality_vps_family_state'::regclass, true)`).Scan(&beforeView); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("C78 transactional rollback cutpoint")
	dependencies := defaultAppACLCurrentConvergenceDependencies()
	reached := false
	if cutpoint == "after_migration" {
		apply := dependencies.applyPending
		dependencies.applyPending = func(ctx context.Context, tx pgx.Tx, sources migrationSourceSnapshot, applied []MigrationChecksumEntry) error {
			if err := apply(ctx, tx, sources, applied); err != nil {
				return err
			}
			var availability string
			if err := tx.QueryRow(ctx, `select availability from public.ip_quality_vps_family_state where vps_id = 'vps_p76_existing' and ip_version = 4`).Scan(&availability); err != nil || availability != "unavailable" {
				t.Fatalf("cutpoint must observe the real migrated view: %s/%v", availability, err)
			}
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
			reached = true
			return manifest, injected
		}
	}
	_, err := convergeAppACLCurrentWithDependencies(ctx, db.BeginTx, fixture.runtimeRole, fixture.adminRole, migrations.FS, appACLCurrentMigrationFragments, dependencies)
	if !reached || !errors.Is(err, injected) {
		t.Fatalf("rollback did not reach %s: reached=%t error=%v", cutpoint, reached, err)
	}
	var afterView string
	if err := db.QueryRow(ctx, `select pg_get_viewdef('public.ip_quality_vps_family_state'::regclass, true)`).Scan(&afterView); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(readAppACLCurrentPostgresDurableSnapshot(t, ctx, db, input), before) ||
		readAppACLCurrentP77Rows(t, ctx, db) != beforeRows || afterView != beforeView {
		t.Fatal("failed C78 transaction changed predecessor ledger, manifests/head, ACLs, data or view definition")
	}
	runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)
	assertAppACLCurrentP77FamilyAvailability(t, ctx, runtimeDB, "unknown", false)
	if _, err := ConvergeAppACLCurrent(ctx, db, fixture.runtimeRole, fixture.adminRole); err != nil {
		t.Fatalf("C78 recovery after rollback: %v", err)
	}
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
		t.Fatalf("runtime admission after rollback recovery: %v", err)
	}
	assertAppACLCurrentP77FamilyAvailability(t, ctx, runtimeDB, "unavailable", true)
}

func testPostgresIntegrationAppACLCurrentP77Drift(t *testing.T, current bool, drift string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	db := fixture.openRolePool(t, ctx, fixture.migratorRole)
	seedAppACLCurrentP77Predecessor(t, ctx, fixture, db, false)
	seedAppACLCurrentP77Rows(t, ctx, db)
	if current {
		if _, err := ConvergeAppACLCurrent(ctx, db, fixture.runtimeRole, fixture.adminRole); err != nil {
			t.Fatalf("C78 convergence before drift: %v", err)
		}
	}
	var sql string
	switch drift {
	case "missing_select":
		sql = "revoke select on public.ip_quality_vps_family_state from " + pgx.Identifier{fixture.runtimeRole}.Sanitize()
	case "runtime_update":
		sql = "grant update on public.ip_quality_vps_family_state to " + pgx.Identifier{fixture.runtimeRole}.Sanitize()
	case "admin_select":
		sql = "grant select on public.ip_quality_vps_family_state to " + pgx.Identifier{fixture.adminRole}.Sanitize()
	case "public_select":
		sql = "grant select on public.ip_quality_vps_family_state to public"
	case "grant_option":
		sql = "grant select on public.ip_quality_vps_family_state to " + pgx.Identifier{fixture.runtimeRole}.Sanitize() + " with grant option"
	case "ledger_checksum":
		sql = "update public.schema_migrations set checksum = repeat('0',64) where name = '0077_ip_quality_product.sql'"
	default:
		t.Fatalf("unknown drift %q", drift)
	}
	if _, err := db.Exec(ctx, sql); err != nil {
		t.Fatalf("inject C77/C78 drift: %v", err)
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
}
