package migrate

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
)

const appACLCurrentP76Migration = "0076_ip_quality_address_families.sql"

func testPostgresIntegrationAppACLCurrentP75Upgrade(t *testing.T) {
	t.Helper()
	t.Run("fresh_current", testPostgresIntegrationAppACLCurrentP75Fresh)
	for _, state := range []string{"missing", "default", "custom"} {
		t.Run("c75_upgrade_settings_"+state, func(t *testing.T) {
			testPostgresIntegrationAppACLCurrentP75SettingsState(t, state)
		})
	}
	for _, drift := range []string{"runtime_update", "admin_select", "runtime_grant_option", "public_select"} {
		t.Run("extra_permission_"+drift, func(t *testing.T) {
			testPostgresIntegrationAppACLCurrentP75ExtraPermission(t, drift)
		})
	}
}

func testPostgresIntegrationAppACLCurrentP75Fresh(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	migratorDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
	runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	manifest, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
	if err != nil {
		t.Fatalf("fresh C76 convergence: %v", err)
	}
	if manifest.ManifestRevision != 1 {
		t.Fatalf("fresh C76 revision = %d, want 1", manifest.ManifestRevision)
	}
	assertAppACLCurrentP75FamilyStateRead(t, ctx, runtimeDB)
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
		t.Fatalf("fresh C76 runtime admission: %v", err)
	}
	_, _, input := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, input)
	if len(before.Ledger) != 80 || before.Ledger[len(before.Ledger)-1].Name != appACLCurrentP79Migration {
		t.Fatalf("fresh current ledger = %#v, want 80 sources ending in 0079", before.Ledger)
	}
	repeated, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
	if err != nil {
		t.Fatalf("fresh C76 repeat: %v", err)
	}
	after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, input)
	if repeated.ManifestDigest != manifest.ManifestDigest || !reflect.DeepEqual(after, before) {
		t.Fatal("fresh C76 repeat changed durable state")
	}
}

func testPostgresIntegrationAppACLCurrentP75SettingsState(t *testing.T, state string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	migratorDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
	profile := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP75Migration)
	predecessor, _, _ := seedAppACLCurrentReleasedGenesis(t, ctx, fixture, migratorDB, profile)
	configureAppACLCurrentP67SettingsState(t, ctx, migratorDB, state)
	beforeSettings := readAppACLCurrentSettingsPresenceSnapshot(t, ctx, migratorDB)
	_, _, input := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, input)
	runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)
	successor, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
	if err != nil {
		t.Fatalf("C75 to C76 convergence: %v", err)
	}
	if successor.ManifestRevision != predecessor.ManifestRevision+1 || successor.PreviousManifestDigest != predecessor.ManifestDigest {
		t.Fatalf("C76 successor = %#v, want linked predecessor revision + 1", successor)
	}
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
		t.Fatalf("C76 runtime admission: %v", err)
	}
	assertAppACLCurrentP75FamilyStateRead(t, ctx, runtimeDB)
	after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, input)
	assertAppACLCurrentManifestHistoryPrefix(t, before.Manifest.Manifests, after.Manifest.Manifests)
	if len(after.Manifest.Manifests) != len(before.Manifest.Manifests)+1 {
		t.Fatalf("current successor must append exactly one manifest: %#v", after.Manifest)
	}
	assertAppACLCurrentLedgerSuffix(t, before.Ledger, after.Ledger,
		appACLCurrentP76Migration, appACLCurrentP77Migration, appACLCurrentP78Migration, appACLCurrentP79Migration)
	if !appACLCurrentPrivilegesWithFamilyStateAndProductTables(after.Catalog.DirectPrivileges, before.Catalog.DirectPrivileges, fixture.runtimeRole) ||
		!appACLCurrentPrivilegesWithFamilyStateAndProductTables(after.Catalog.EffectivePrivileges, before.Catalog.EffectivePrivileges, fixture.runtimeRole) ||
		!reflect.DeepEqual(after.Catalog.ColumnACLs, before.Catalog.ColumnACLs) ||
		!reflect.DeepEqual(after.Catalog.DefaultACLs, before.Catalog.DefaultACLs) ||
		!reflect.DeepEqual(after.Catalog.Functions, before.Catalog.Functions) {
		t.Fatalf("C75 to C76 changed ACLs beyond runtime family-state SELECT")
	}
	wantOwner := AppACLEffectiveCatalogObjectOwnerR1{
		ObjectClass: AppACLObjectClassView, SchemaName: "public",
		ObjectIdentity: "ip_quality_vps_family_state", OwnerRole: fixture.migratorRole,
	}
	var retainedOwners []AppACLEffectiveCatalogObjectOwnerR1
	newOwners := 0
	for _, owner := range after.Catalog.Owners {
		if owner == wantOwner || owner == (AppACLEffectiveCatalogObjectOwnerR1{ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "ip_quality_intelligence", OwnerRole: fixture.migratorRole}) ||
			owner == (AppACLEffectiveCatalogObjectOwnerR1{ObjectClass: AppACLObjectClassTable, SchemaName: "public", ObjectIdentity: "ip_quality_source_cache", OwnerRole: fixture.migratorRole}) {
			newOwners++
		} else {
			retainedOwners = append(retainedOwners, owner)
		}
	}
	if newOwners != 3 || !reflect.DeepEqual(retainedOwners, before.Catalog.Owners) {
		t.Fatalf("C77 object inventory changed beyond the family-state view and product tables")
	}
	assertAppACLCurrentSettingsPresenceSnapshotEqual(t, beforeSettings, readAppACLCurrentSettingsPresenceSnapshot(t, ctx, migratorDB))
	repeated, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
	if err != nil {
		t.Fatalf("C76 repeat: %v", err)
	}
	afterRepeat := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, input)
	if repeated.ManifestDigest != successor.ManifestDigest || !reflect.DeepEqual(afterRepeat, after) {
		t.Fatal("C76 successor repeat changed durable state")
	}
}

func testPostgresIntegrationAppACLCurrentP75ExtraPermission(t *testing.T, drift string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	migratorDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
	profile := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP75Migration)
	seedAppACLCurrentReleasedGenesis(t, ctx, fixture, migratorDB, profile)
	if _, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole); err != nil {
		t.Fatalf("C75 to C76 convergence before drift: %v", err)
	}
	runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
		t.Fatalf("admit C76 before drift: %v", err)
	}
	var sql string
	switch drift {
	case "runtime_update":
		sql = "grant update on public.ip_quality_vps_family_state to " + pgx.Identifier{fixture.runtimeRole}.Sanitize()
	case "admin_select":
		sql = "grant select on public.ip_quality_vps_family_state to " + pgx.Identifier{fixture.adminRole}.Sanitize()
	case "runtime_grant_option":
		sql = "grant select on public.ip_quality_vps_family_state to " + pgx.Identifier{fixture.runtimeRole}.Sanitize() + " with grant option"
	case "public_select":
		sql = "grant select on public.ip_quality_vps_family_state to public"
	default:
		t.Fatalf("unknown drift %q", drift)
	}
	if _, err := migratorDB.Exec(ctx, sql); err != nil {
		t.Fatalf("inject extra view permission: %v", err)
	}
	_, _, input := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, input)
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err == nil {
		t.Fatal("runtime admission accepted extra family-state permission")
	}
	if _, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole); err == nil {
		t.Fatal("convergence repaired or accepted extra family-state permission")
	}
	after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, input)
	if !reflect.DeepEqual(after, before) {
		t.Fatal("rejected admission/convergence changed durable drift state")
	}
}

func assertAppACLCurrentP75FamilyStateRead(t *testing.T, ctx context.Context, runtimeDB *pgxpool.Pool) {
	t.Helper()
	var families int
	if err := runtimeDB.QueryRow(ctx, `select count(*) from public.ip_quality_vps_family_state where ip_version in (4, 6)`).Scan(&families); err != nil {
		t.Fatalf("runtime SELECT on family-state view: %v", err)
	}
}

func appACLCurrentFamilyStateSelectObservation(runtimeRole string) AppACLEffectiveCatalogPrivilegeObservationR1 {
	return AppACLEffectiveCatalogPrivilegeObservationR1{
		Grantee: runtimeRole, ObjectClass: AppACLObjectClassView, SchemaName: "public",
		ObjectIdentity: "ip_quality_vps_family_state", Privilege: AppACLPrivilegeSelect,
	}
}

// Compare the entire predecessor inventory plus the family-state SELECT and
// product table grants required by the current target. No other delta is allowed.
func appACLCurrentPrivilegesWithFamilyStateAndProductTables(after, before []AppACLEffectiveCatalogPrivilegeObservationR1, runtimeRole string) bool {
	if len(after) != len(before)+8 {
		return false
	}
	want := make(map[AppACLEffectiveCatalogPrivilegeObservationR1]struct{}, len(after))
	for _, privilege := range before {
		want[privilege] = struct{}{}
	}
	want[appACLCurrentFamilyStateSelectObservation(runtimeRole)] = struct{}{}
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
