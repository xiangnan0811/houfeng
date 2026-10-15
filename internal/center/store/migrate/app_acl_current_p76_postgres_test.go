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

const appACLCurrentP77Migration = "0077_ip_quality_product.sql"

func TestPostgresIntegrationIPQualityProductMigration(t *testing.T) {
	t.Run("c75_to_c76_to_c77_history", testPostgresIntegrationAppACLCurrentP76HistoricalChain)
	for _, state := range []string{"missing", "default", "custom"} {
		t.Run("c76_upgrade_settings_"+state, func(t *testing.T) {
			testPostgresIntegrationAppACLCurrentP76Upgrade(t, state)
		})
	}
	t.Run("rollback_after_product_dcl", testPostgresIntegrationAppACLCurrentP76Rollback)
	for _, drift := range []string{"runtime_intelligence_delete", "admin_cache_select", "public_intelligence_select", "runtime_cache_grant_option", "records_cache_select"} {
		t.Run("extra_permission_"+drift, func(t *testing.T) {
			testPostgresIntegrationAppACLCurrentP76Drift(t, drift)
		})
	}
}

func testPostgresIntegrationAppACLCurrentP76Upgrade(t *testing.T, state string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	migratorDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
	profile := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP76Migration)
	predecessor, _, _ := seedAppACLCurrentReleasedGenesis(t, ctx, fixture, migratorDB, profile)
	configureAppACLCurrentP67SettingsState(t, ctx, migratorDB, state)
	seedAppACLCurrentP76IPQualityRows(t, ctx, migratorDB)
	beforeRows := readAppACLCurrentP76IPQualityRows(t, ctx, migratorDB)
	beforeSettings := readAppACLCurrentSettingsPresenceSnapshot(t, ctx, migratorDB)
	_, contract, input := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, input)
	if len(before.Ledger) != 77 || before.Ledger[76].Name != appACLCurrentP76Migration {
		t.Fatalf("predecessor is not exact C76: %#v", before.Ledger)
	}
	runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)
	successor, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
	if err != nil {
		t.Fatalf("C76 to C77 convergence: %v", err)
	}
	if successor.ManifestRevision != predecessor.ManifestRevision+1 || successor.PreviousManifestDigest != predecessor.ManifestDigest {
		t.Fatalf("C77 successor must link exactly one revision to C76: %#v", successor)
	}
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
		t.Fatalf("C77 runtime admission: %v", err)
	}
	after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, input)
	assertAppACLCurrentManifestHistoryPrefix(t, before.Manifest.Manifests, after.Manifest.Manifests)
	if len(after.Manifest.Manifests) != len(before.Manifest.Manifests)+1 {
		t.Fatalf("current successor must append exactly one manifest: %#v", after.Manifest)
	}
	assertAppACLCurrentLedgerSuffix(t, before.Ledger, after.Ledger,
		appACLCurrentP77Migration, appACLCurrentP78Migration)
	for _, pair := range [][2][]AppACLEffectiveCatalogPrivilegeObservationR1{
		{before.Catalog.DirectPrivileges, after.Catalog.DirectPrivileges},
		{before.Catalog.EffectivePrivileges, after.Catalog.EffectivePrivileges},
	} {
		if !appACLCurrentPrivilegesWithProductTables(pair[1], pair[0], fixture.runtimeRole) {
			t.Fatalf("C76 to C77 privileges changed beyond seven runtime product grants")
		}
	}
	if !reflect.DeepEqual(after.Catalog.ColumnACLs, before.Catalog.ColumnACLs) ||
		!reflect.DeepEqual(after.Catalog.DefaultACLs, before.Catalog.DefaultACLs) ||
		!reflect.DeepEqual(after.Catalog.Functions, before.Catalog.Functions) {
		t.Fatal("C77 changed predecessor column/default/function ACLs")
	}
	var retainedOwners []AppACLEffectiveCatalogObjectOwnerR1
	newOwners := make(map[string]int, 2)
	for _, owner := range after.Catalog.Owners {
		if owner.ObjectClass == AppACLObjectClassTable && owner.SchemaName == "public" &&
			(owner.ObjectIdentity == "ip_quality_intelligence" || owner.ObjectIdentity == "ip_quality_source_cache") {
			if owner.OwnerRole != fixture.migratorRole {
				t.Fatalf("product table has wrong owner: %#v", owner)
			}
			newOwners[owner.ObjectIdentity]++
		} else {
			retainedOwners = append(retainedOwners, owner)
		}
	}
	if newOwners["ip_quality_intelligence"] != 1 || newOwners["ip_quality_source_cache"] != 1 || !reflect.DeepEqual(retainedOwners, before.Catalog.Owners) {
		t.Fatal("C77 owner inventory changed beyond two new migrator-owned tables")
	}
	if err := verifyAppACLEffectiveCatalogSnapshot(after.Catalog, input); err != nil {
		t.Fatalf("new manifest authorization list differs from final catalog: %v", err)
	}
	// Also pin the manifest-facing contract, not merely successful SQL GRANTs.
	var productPrivileges []AppACLPrivilege
	for _, privilege := range contract.Privileges {
		if privilege.ObjectIdentity == "ip_quality_intelligence" || privilege.ObjectIdentity == "ip_quality_source_cache" {
			productPrivileges = append(productPrivileges, privilege)
		}
	}
	if !reflect.DeepEqual(productPrivileges, appACLCurrentProductExpectedPrivileges()) {
		t.Fatalf("manifest product privileges = %#v, want only exact runtime grants", productPrivileges)
	}
	if got := readAppACLCurrentP76IPQualityRows(t, ctx, migratorDB); got != beforeRows {
		t.Fatalf("C77 rewrote predecessor report/provider/service facts\nbefore: %#v\nafter: %#v", beforeRows, got)
	}
	assertAppACLCurrentSettingsPresenceSnapshotEqual(t, beforeSettings, readAppACLCurrentSettingsPresenceSnapshot(t, ctx, migratorDB))
	assertAppACLCurrentP76LegacyProductNulls(t, ctx, runtimeDB)
	assertAppACLCurrentP76RuntimeProductAccess(t, ctx, runtimeDB)
	repeated, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
	if err != nil {
		t.Fatalf("C77 repeat: %v", err)
	}
	afterRepeat := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, input)
	if repeated.ManifestDigest != successor.ManifestDigest || !reflect.DeepEqual(afterRepeat, after) || readAppACLCurrentP76IPQualityRows(t, ctx, migratorDB) != beforeRows {
		t.Fatal("C77 repeat changed durable state or historical facts")
	}
}

func appACLCurrentPrivilegesWithProductTables(after, before []AppACLEffectiveCatalogPrivilegeObservationR1, runtimeRole string) bool {
	if len(after) != len(before)+7 {
		return false
	}
	want := make(map[AppACLEffectiveCatalogPrivilegeObservationR1]struct{}, len(after))
	for _, privilege := range before {
		want[privilege] = struct{}{}
	}
	for _, privilege := range appACLCurrentProductExpectedObservations(runtimeRole) {
		want[privilege] = struct{}{}
	}
	if len(want) != len(after) {
		return false
	}
	for _, privilege := range after {
		if _, found := want[privilege]; !found {
			return false
		}
		delete(want, privilege)
	}
	return len(want) == 0
}

func seedAppACLCurrentP76IPQualityRows(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	if _, err := db.Exec(ctx, `
		insert into public.vps_assets (vps_id, display_name, lifecycle_status, usage_status, renewal_decision)
		values ('vps_p76_existing', 'C76 IP quality', 'active', 'in_use', 'keep');
		insert into public.monitoring_instances (
			monitoring_instance_id, display_name, region, city, provider, lifecycle_status,
			monitoring_status, binding_status, vps_id
		) values ('mi_p76_existing', 'C76 IP quality', 'US', 'Seattle', 'Test Provider', '已接入', '启用', '未绑定', 'vps_p76_existing');
		insert into public.ip_quality_reports (
			report_id, monitoring_instance_id, observed_at, received_at, agent_version, fingerprint,
			sync_batch_id, ip_address, ip_version, status, risk_level, raw_json, coverage_json, diagnostics_json
		) values ('ipq_p76_existing', 'mi_p76_existing', '2026-01-02 03:04:05+00', '2026-01-02 03:04:06+00',
			'c76-agent', 'c76-fingerprint', 'c76-batch', '203.0.113.76', 4, 'partial', 'high',
			'{"historical":true}', '{"expected_provider_count":1}', '{"collection_revision":4}');
		insert into public.ip_quality_provider_results (
			result_id, report_id, provider, status, source_type, risk_level, risk_score, is_vpn, is_proxy, extra_json
		) values ('ipr_p76_existing', 'ipq_p76_existing', 'ipquery.io', 'success', 'default', 'high', '91', true, false, '{"c76":"provider"}');
		insert into public.ip_quality_service_unlocks (
			unlock_id, report_id, service, source, status, probe_status, region, extra_json
		) values ('ips_p76_existing', 'ipq_p76_existing', 'chatgpt', 'openai_status_probe', 'blocked', 'success', 'US', '{"c76":"service"}');
	`); err != nil {
		t.Fatalf("seed C76 report and children: %v", err)
	}
}

type appACLCurrentP76IPQualityRows struct {
	Report   string
	Provider string
	Service  string
}

func readAppACLCurrentP76IPQualityRows(t *testing.T, ctx context.Context, db *pgxpool.Pool) appACLCurrentP76IPQualityRows {
	t.Helper()
	var rows appACLCurrentP76IPQualityRows
	if err := db.QueryRow(ctx, `select
		(select (to_jsonb(r) - 'product_json' - 'product_summary_json')::text from public.ip_quality_reports r where report_id = 'ipq_p76_existing'),
		(select (to_jsonb(p) - 'details_json')::text from public.ip_quality_provider_results p where result_id = 'ipr_p76_existing'),
		(select (to_jsonb(s) - 'checks_json')::text from public.ip_quality_service_unlocks s where unlock_id = 'ips_p76_existing')
	`).Scan(&rows.Report, &rows.Provider, &rows.Service); err != nil {
		t.Fatalf("read preserved IP quality rows: %v", err)
	}
	return rows
}

func assertAppACLCurrentP76LegacyProductNulls(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	var unchanged bool
	if err := db.QueryRow(ctx, `select
		(select product_json is null and product_summary_json is null from public.ip_quality_reports where report_id = 'ipq_p76_existing')
		and (select details_json is null from public.ip_quality_provider_results where result_id = 'ipr_p76_existing')
		and (select checks_json is null from public.ip_quality_service_unlocks where unlock_id = 'ips_p76_existing')
		and not exists (select 1 from public.ip_quality_intelligence)
		and not exists (select 1 from public.ip_quality_source_cache)
	`).Scan(&unchanged); err != nil || !unchanged {
		t.Fatalf("historical new fields must remain NULL with no synthetic jobs/cache: %t/%v", unchanged, err)
	}
}

func assertAppACLCurrentP76RuntimeProductAccess(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `insert into public.ip_quality_intelligence (report_id, state, deadline_at)
		values ('ipq_p76_existing', 'pending', now() + interval '10 minutes');
		update public.ip_quality_intelligence set state = 'running', owner_id = 'runtime-owner', owner_generation = owner_generation + 1,
			data_json = '{"providers":[]}' where report_id = 'ipq_p76_existing';
		insert into public.ip_quality_source_cache (source_id, resource, credential_fingerprint, fetched_at, expires_at, blocked_until, payload_json)
		values ('ripe_network', '203.0.113.76', '', now(), now() + interval '8 hours', now() + interval '1 hour', '{"asn":"AS64500"}');
		update public.ip_quality_source_cache set payload_json = '{"asn":"AS64501"}' where source_id = 'ripe_network';
	`); err != nil {
		t.Fatalf("runtime product INSERT/UPDATE: %v", err)
	}
	var generation int64
	var state, asn string
	if err := tx.QueryRow(ctx, `select owner_generation, state from public.ip_quality_intelligence where report_id = 'ipq_p76_existing'`).Scan(&generation, &state); err != nil || generation != 1 || state != "running" {
		t.Fatalf("runtime intelligence SELECT = %d/%s/%v", generation, state, err)
	}
	if err := tx.QueryRow(ctx, `select payload_json->>'asn' from public.ip_quality_source_cache where source_id = 'ripe_network'`).Scan(&asn); err != nil || asn != "AS64501" {
		t.Fatalf("runtime cache SELECT = %s/%v", asn, err)
	}
	if _, err := tx.Exec(ctx, `delete from public.ip_quality_source_cache where source_id = 'ripe_network'`); err != nil {
		t.Fatalf("runtime cache DELETE: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(ctx, `delete from public.ip_quality_intelligence where report_id = 'ipq_p76_existing'`)
	requirePostgresSQLState(t, err, "42501")
}

func testPostgresIntegrationAppACLCurrentP76Rollback(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	migratorDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
	profile := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP76Migration)
	seedAppACLCurrentReleasedGenesis(t, ctx, fixture, migratorDB, profile)
	seedAppACLCurrentP76IPQualityRows(t, ctx, migratorDB)
	_, _, input := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, input)
	beforeRows := readAppACLCurrentP76IPQualityRows(t, ctx, migratorDB)
	dependencies := defaultAppACLCurrentConvergenceDependencies()
	apply := dependencies.applyDCL
	injected := errors.New("product DCL rollback cutpoint")
	dependencies.applyDCL = func(ctx context.Context, tx pgx.Tx, contract appACLEffectiveCatalogContract) error {
		if err := apply(ctx, tx, contract); err != nil {
			return err
		}
		return injected
	}
	_, err := convergeAppACLCurrentWithDependencies(ctx, migratorDB.BeginTx, fixture.runtimeRole, fixture.adminRole, migrations.FS, appACLCurrentMigrationFragments, dependencies)
	if !errors.Is(err, injected) {
		t.Fatalf("rollback cutpoint error = %v, want injected failure", err)
	}
	after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, input)
	if !reflect.DeepEqual(after, before) || readAppACLCurrentP76IPQualityRows(t, ctx, migratorDB) != beforeRows {
		t.Fatal("failed C77 migration changed C76 ledger/manifest/catalog/rows")
	}
	var absent bool
	if err := migratorDB.QueryRow(ctx, `select to_regclass('public.ip_quality_intelligence') is null and to_regclass('public.ip_quality_source_cache') is null
		and not exists (select 1 from information_schema.columns where table_schema = 'public' and
			(table_name, column_name) in (('ip_quality_reports','product_json'), ('ip_quality_reports','product_summary_json'),
			('ip_quality_provider_results','details_json'), ('ip_quality_service_unlocks','checks_json')))`,
	).Scan(&absent); err != nil || !absent {
		t.Fatalf("rollback retained C77 tables/columns: %t/%v", absent, err)
	}
}

func testPostgresIntegrationAppACLCurrentP76Drift(t *testing.T, drift string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	migratorDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
	profile := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP76Migration)
	seedAppACLCurrentReleasedGenesis(t, ctx, fixture, migratorDB, profile)
	if _, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole); err != nil {
		t.Fatalf("C77 convergence before drift: %v", err)
	}
	var statement string
	switch drift {
	case "runtime_intelligence_delete":
		statement = "grant delete on public.ip_quality_intelligence to " + pgx.Identifier{fixture.runtimeRole}.Sanitize()
	case "admin_cache_select":
		statement = "grant select on public.ip_quality_source_cache to " + pgx.Identifier{fixture.adminRole}.Sanitize()
	case "public_intelligence_select":
		statement = "grant select on public.ip_quality_intelligence to public"
	case "records_cache_select":
		// The minimal APP fixture does not provision the optional Records
		// authority. Use an isolated test principal, not a production role or
		// membership that could accidentally broaden the APP runtime.
		recordsRole := recordsAuthorityCatalogRole + "_" + appACLEffectiveCatalogTestRoleNames().suffix
		quotedRole := pgx.Identifier{recordsRole}.Sanitize()
		if _, err := fixture.db.Exec(ctx, "create role "+quotedRole+" nologin noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls"); err != nil {
			t.Fatalf("create separate Records drift principal: %v", err)
		}
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cleanupCancel()
			if _, err := fixture.db.Exec(cleanupCtx, "revoke all privileges on public.ip_quality_source_cache from "+quotedRole); err != nil {
				t.Errorf("revoke isolated Records drift grant: %v", err)
			}
			if _, err := fixture.db.Exec(cleanupCtx, "drop role "+quotedRole); err != nil {
				t.Errorf("drop isolated Records drift principal: %v", err)
			}
		})
		statement = "grant select on public.ip_quality_source_cache to " + quotedRole
	case "runtime_cache_grant_option":
		statement = "grant select on public.ip_quality_source_cache to " + pgx.Identifier{fixture.runtimeRole}.Sanitize() + " with grant option"
	default:
		t.Fatalf("unknown drift %q", drift)
	}
	if _, err := migratorDB.Exec(ctx, statement); err != nil {
		t.Fatalf("inject product ACL drift: %v", err)
	}
	_, _, input := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, input)
	runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err == nil {
		t.Fatal("runtime admitted unexpected product privilege")
	}
	if _, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole); err == nil {
		t.Fatal("convergence accepted or repaired unexpected product privilege")
	}
	if after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, input); !reflect.DeepEqual(after, before) {
		t.Fatal("rejected product ACL drift mutated durable state")
	}
}

func testPostgresIntegrationAppACLCurrentP76HistoricalChain(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	migratorDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
	c75 := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP75Migration)
	c76 := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP76Migration)
	first, _, _ := seedAppACLCurrentReleasedGenesis(t, ctx, fixture, migratorDB, c75)
	seedAppACLCurrentP76IPQualityRows(t, ctx, migratorDB)
	beforeRows := readAppACLCurrentP76IPQualityRows(t, ctx, migratorDB)
	_, _, input := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, input)
	second, err := appendAppACLCurrentReleasedSuccessor(t, ctx, fixture, migratorDB, c76)
	if err != nil {
		t.Fatalf("historical C75 to C76 migration: %v", err)
	}
	middle := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, input)
	if second.ManifestRevision != first.ManifestRevision+1 || second.PreviousManifestDigest != first.ManifestDigest ||
		len(middle.Ledger) != len(before.Ledger)+1 || middle.Ledger[len(middle.Ledger)-1].Name != appACLCurrentP76Migration ||
		!reflect.DeepEqual(middle.Ledger[:len(before.Ledger)], before.Ledger) ||
		readAppACLCurrentP76IPQualityRows(t, ctx, migratorDB) != beforeRows {
		t.Fatal("historical C75 to C76 changed its predecessor ledger/rows/history")
	}
	assertAppACLCurrentManifestHistoryPrefix(t, before.Manifest.Manifests, middle.Manifest.Manifests)
	if !reflect.DeepEqual(before.Catalog.ColumnACLs, middle.Catalog.ColumnACLs) ||
		!reflect.DeepEqual(before.Catalog.DefaultACLs, middle.Catalog.DefaultACLs) ||
		!reflect.DeepEqual(before.Catalog.Functions, middle.Catalog.Functions) {
		t.Fatal("historical C75 to C76 changed column/default/function ACLs")
	}
	var retainedOwners []AppACLEffectiveCatalogObjectOwnerR1
	familyViewOwners := 0
	for _, owner := range middle.Catalog.Owners {
		if owner == (AppACLEffectiveCatalogObjectOwnerR1{ObjectClass: AppACLObjectClassView, SchemaName: "public", ObjectIdentity: "ip_quality_vps_family_state", OwnerRole: fixture.migratorRole}) {
			familyViewOwners++
		} else {
			retainedOwners = append(retainedOwners, owner)
		}
	}
	if familyViewOwners != 1 || !reflect.DeepEqual(retainedOwners, before.Catalog.Owners) {
		t.Fatal("historical C75 to C76 ownership delta is not exactly the family-state view")
	}
	for _, pair := range [][2][]AppACLEffectiveCatalogPrivilegeObservationR1{
		{before.Catalog.DirectPrivileges, middle.Catalog.DirectPrivileges},
		{before.Catalog.EffectivePrivileges, middle.Catalog.EffectivePrivileges},
	} {
		var retained []AppACLEffectiveCatalogPrivilegeObservationR1
		added := 0
		for _, privilege := range pair[1] {
			if privilege == appACLCurrentFamilyStateSelectObservation(fixture.runtimeRole) {
				added++
			} else {
				retained = append(retained, privilege)
			}
		}
		if added != 1 || !reflect.DeepEqual(retained, pair[0]) {
			t.Fatal("historical C75 to C76 ACL delta is not exactly runtime family-state SELECT")
		}
	}
	third, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
	if err != nil {
		t.Fatalf("chained C76 to C77 convergence: %v", err)
	}
	after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, input)
	assertAppACLCurrentManifestHistoryPrefix(t, middle.Manifest.Manifests, after.Manifest.Manifests)
	if third.ManifestRevision != second.ManifestRevision+1 || third.PreviousManifestDigest != second.ManifestDigest ||
		!appACLCurrentPrivilegesWithProductTables(after.Catalog.DirectPrivileges, middle.Catalog.DirectPrivileges, fixture.runtimeRole) ||
		readAppACLCurrentP76IPQualityRows(t, ctx, migratorDB) != beforeRows {
		t.Fatal("C77 failed to preserve the historical C75 to C76 evidence")
	}
	if err := verifyAppACLEffectiveCatalogSnapshot(after.Catalog, input); err != nil {
		t.Fatalf("chained C77 final catalog differs from manifest: %v", err)
	}
	runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
		t.Fatalf("chained C77 runtime admission: %v", err)
	}
}
