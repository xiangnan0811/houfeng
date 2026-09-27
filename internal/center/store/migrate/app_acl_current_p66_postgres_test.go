package migrate

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"houfeng/db/migrations"
)

const appACLCurrentP66LastMigration = "0066_constrain_monitoring_and_target_state_values.sql"

func TestPostgresIntegrationAppACLCurrentP66ReleaseProfile(t *testing.T) {
	profile := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP66LastMigration)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), os.Getpid())
	fixture := newExactAppACLCurrentSuccessorPostgresFixtureWithNames(
		t,
		ctx,
		"houfeng_p66_"+suffix,
		"houfeng_p66_runtime_"+suffix,
		"houfeng_p66_admin_"+suffix,
		"houfeng_p66_migrator_"+suffix,
	)
	migratorDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
	predecessor, _, _ := seedAppACLCurrentReleasedGenesis(t, ctx, fixture, migratorDB, profile)

	assertAppACLCurrentSuccessorRejectsLegacyVPS(t, ctx, migratorDB)
	_, _, currentInput := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	beforeUpgrade := readAppACLCurrentTransitionDurableState(t, ctx, migratorDB, currentInput)
	runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)

	successor, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
	if err != nil {
		t.Fatalf("ConvergeAppACLCurrent() P66 genesis upgrade: %v", err)
	}
	if successor.ManifestRevision != 2 || successor.PreviousManifestDigest != predecessor.ManifestDigest {
		t.Fatalf("P66 genesis successor = %#v, want revision 2 linked to P66 genesis", successor)
	}
	assertAppACLCurrentSuccessorUpgradeEffects(t, ctx, migratorDB)
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
		t.Fatalf("AdmitAppACLCurrentRuntime() P66 successor: %v", err)
	}
	afterUpgrade := readAppACLCurrentTransitionDurableState(t, ctx, migratorDB, currentInput)
	assertAppACLCurrentManifestHistoryPrefix(t, beforeUpgrade.Base.Manifest.Manifests, afterUpgrade.Base.Manifest.Manifests)
	if !bytes.Equal(beforeUpgrade.IncidentDefaults, afterUpgrade.IncidentDefaults) ||
		beforeUpgrade.SettingsExceptTransitionDigest != afterUpgrade.SettingsExceptTransitionDigest ||
		!beforeUpgrade.SettingsUpdated.Equal(afterUpgrade.SettingsUpdated) ||
		beforeUpgrade.HeartbeatRowsDigest != afterUpgrade.HeartbeatRowsDigest {
		t.Fatal("P66 upgrade changed heartbeat state or settings outside the exact 0067 lifecycle transform")
	}

	beforeRepeat := afterUpgrade.Base
	repeated, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
	if err != nil {
		t.Fatalf("ConvergeAppACLCurrent() P66 successor repeat: %v", err)
	}
	afterRepeat := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
	if repeated.ManifestDigest != successor.ManifestDigest || !reflect.DeepEqual(afterRepeat, beforeRepeat) {
		t.Fatalf("P66 successor repeat changed durable state\nbefore: %#v\nafter:  %#v", beforeRepeat, afterRepeat)
	}
}

func TestPostgresIntegrationAppACLCurrentP66ChainedReleaseProfile(t *testing.T) {
	p63Profile := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP63LastMigration)
	p66Profile := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP66LastMigration)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	state := seedExactAppACLCurrentPredecessor(t, ctx, 3)
	p63, err := appendAppACLCurrentReleasedSuccessor(t, ctx, state.fixture, state.migratorDB, p63Profile)
	if err != nil {
		t.Fatalf("append released P63 profile to P62 genesis: %v", err)
	}
	if p63.ManifestRevision != 2 || fmt.Sprintf("%x", p63.ManifestDigest) != "7f1ade2bdb153e2859c92b674a3850edd079e1ef1468e317f3b9a91995eba431" {
		t.Fatalf("P62 to P63 history = revision %d digest %x, want released revision-2 identity", p63.ManifestRevision, p63.ManifestDigest)
	}
	p66, err := appendAppACLCurrentReleasedSuccessor(t, ctx, state.fixture, state.migratorDB, p66Profile)
	if err != nil {
		t.Fatalf("append released P66 profile to P62-to-P63 history: %v", err)
	}
	if p66.ManifestRevision != 3 || p66.PreviousManifestDigest != p63.ManifestDigest {
		t.Fatalf("P62 to P63 to P66 history = %#v, want revision 3 linked to P63", p66)
	}

	assertAppACLCurrentSuccessorRejectsLegacyVPS(t, ctx, state.migratorDB)
	_, _, currentInput := appACLCurrentPostgresContract(t, state.fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	beforeUpgrade := readAppACLCurrentTransitionDurableState(t, ctx, state.migratorDB, currentInput)
	if len(beforeUpgrade.Base.Manifest.Manifests) != 3 {
		t.Fatalf("P62 to P63 to P66 history length = %d, want 3", len(beforeUpgrade.Base.Manifest.Manifests))
	}
	runtimeDB := state.fixture.openRolePool(t, ctx, state.fixture.runtimeRole)
	assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)

	successor, err := ConvergeAppACLCurrent(ctx, state.migratorDB, state.fixture.runtimeRole, state.fixture.adminRole)
	if err != nil {
		t.Fatalf("ConvergeAppACLCurrent() P62 to P63 to P66 chain: %v", err)
	}
	if successor.ManifestRevision != 4 || successor.PreviousManifestDigest != p66.ManifestDigest {
		t.Fatalf("P62 to P63 to P66 current successor = %#v, want revision 4 linked to P66", successor)
	}
	assertAppACLCurrentSuccessorUpgradeEffects(t, ctx, state.migratorDB)
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
		t.Fatalf("AdmitAppACLCurrentRuntime() after P62 to P63 to P66 upgrade: %v", err)
	}
	afterUpgrade := readAppACLCurrentTransitionDurableState(t, ctx, state.migratorDB, currentInput)
	assertAppACLCurrentManifestHistoryPrefix(t, beforeUpgrade.Base.Manifest.Manifests, afterUpgrade.Base.Manifest.Manifests)
	if !bytes.Equal(beforeUpgrade.IncidentDefaults, afterUpgrade.IncidentDefaults) ||
		beforeUpgrade.SettingsExceptTransitionDigest != afterUpgrade.SettingsExceptTransitionDigest ||
		!beforeUpgrade.SettingsUpdated.Equal(afterUpgrade.SettingsUpdated) ||
		beforeUpgrade.HeartbeatRowsDigest != afterUpgrade.HeartbeatRowsDigest {
		t.Fatal("P62 to P63 to P66 upgrade changed heartbeat state or settings outside the exact 0067 lifecycle transform")
	}

	beforeRepeat := afterUpgrade.Base
	repeated, err := ConvergeAppACLCurrent(ctx, state.migratorDB, state.fixture.runtimeRole, state.fixture.adminRole)
	if err != nil {
		t.Fatalf("ConvergeAppACLCurrent() P62 to P63 to P66 exact repeat: %v", err)
	}
	afterRepeat := readAppACLCurrentPostgresDurableSnapshot(t, ctx, state.migratorDB, currentInput)
	if repeated.ManifestDigest != successor.ManifestDigest || !reflect.DeepEqual(afterRepeat, beforeRepeat) {
		t.Fatalf("P62 to P63 to P66 repeat changed durable state\nbefore: %#v\nafter:  %#v", beforeRepeat, afterRepeat)
	}
}

func TestPostgresIntegrationAppACLCurrentP66TransitionRollbackCutpoints(t *testing.T) {
	profile := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP66LastMigration)
	testAppACLCurrentReleasedTransitionRollback(t, profile, false)
}
