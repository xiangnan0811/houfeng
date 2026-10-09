package migrate

import (
	"context"
	"reflect"
	"testing"
	"time"

	"houfeng/db/migrations"
)

const appACLCurrentP72Migration = "0072_add_target_observation_freshness.sql"

func testPostgresIntegrationAppACLCurrentP71Upgrade(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	migratorDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
	predecessorProfile := appACLCurrentReleasedPostgresProfile(t, "0071_add_access_management.sql")
	predecessor, _, _ := seedAppACLCurrentReleasedGenesis(t, ctx, fixture, migratorDB, predecessorProfile)

	_, _, currentInput := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
	if len(before.Ledger) == 0 || before.Ledger[len(before.Ledger)-1].Name != "0071_add_access_management.sql" {
		t.Fatalf("C71 predecessor ledger tail = %#v, want 0071", before.Ledger)
	}
	runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)

	successor, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
	if err != nil {
		t.Fatalf("C71 to C72 convergence: %v", err)
	}
	if successor.ManifestRevision != predecessor.ManifestRevision+1 || successor.PreviousManifestDigest != predecessor.ManifestDigest {
		t.Fatalf("C71 to C72 successor = %#v, want revision %d linked to predecessor", successor, predecessor.ManifestRevision+1)
	}
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
		t.Fatalf("admit C72 runtime: %v", err)
	}

	after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
	assertAppACLCurrentManifestHistoryPrefix(t, before.Manifest.Manifests, after.Manifest.Manifests)
	if len(after.Manifest.Manifests) != len(before.Manifest.Manifests)+1 {
		t.Fatalf("C72 manifest history length = %d, want %d", len(after.Manifest.Manifests), len(before.Manifest.Manifests)+1)
	}
	if len(after.Ledger) != len(before.Ledger)+1 ||
		!reflect.DeepEqual(after.Ledger[:len(before.Ledger)], before.Ledger) ||
		after.Ledger[len(after.Ledger)-1].Name != appACLCurrentP72Migration {
		t.Fatalf("C72 ledger tail = %#v, want predecessor prefix plus one 0072 row", after.Ledger)
	}
	if !reflect.DeepEqual(after.Catalog.DirectPrivileges, before.Catalog.DirectPrivileges) ||
		!reflect.DeepEqual(after.Catalog.EffectivePrivileges, before.Catalog.EffectivePrivileges) ||
		!reflect.DeepEqual(after.Catalog.ColumnACLs, before.Catalog.ColumnACLs) ||
		!reflect.DeepEqual(after.Catalog.DefaultACLs, before.Catalog.DefaultACLs) {
		t.Fatalf("C71 to C72 migration changed ACL state\nbefore direct/effective/column/default: %#v/%#v/%#v/%#v\nafter: %#v/%#v/%#v/%#v",
			before.Catalog.DirectPrivileges, before.Catalog.EffectivePrivileges, before.Catalog.ColumnACLs, before.Catalog.DefaultACLs,
			after.Catalog.DirectPrivileges, after.Catalog.EffectivePrivileges, after.Catalog.ColumnACLs, after.Catalog.DefaultACLs)
	}

	beforeRepeat := after
	repeated, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
	if err != nil {
		t.Fatalf("repeat C71 to C72 convergence: %v", err)
	}
	afterRepeat := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
	if repeated.ManifestDigest != successor.ManifestDigest || !reflect.DeepEqual(afterRepeat, beforeRepeat) {
		t.Fatalf("C72 repeat changed durable state\nbefore: %#v\nafter:  %#v", beforeRepeat, afterRepeat)
	}
}
