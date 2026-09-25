package migrate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
)

const appACLCurrentP63LastMigration = "0063_tune_heartbeat_incident_policy.sql"

func TestPostgresIntegrationAppACLCurrentP63ReleaseProfiles(t *testing.T) {
	profile := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP63LastMigration)
	for _, fromP62 := range []bool{false, true} {
		name := "genesis"
		if fromP62 {
			name = "released_P62_to_P63_chain"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			var fixture exactAppACLCurrentSuccessorPostgresFixture
			var migratorDB *pgxpool.Pool
			var predecessor AppACLManifestPersistedV1
			if fromP62 {
				state := seedExactAppACLCurrentPredecessor(t, ctx, 20)
				fixture, migratorDB = state.fixture, state.migratorDB
				var err error
				predecessor, err = appendAppACLCurrentReleasedSuccessor(t, ctx, fixture, migratorDB, profile)
				if err != nil {
					t.Fatal(err)
				}
				// Matches the released v0.79.6 history observed on the failed upgrade.
				if fmt.Sprintf("%x", predecessor.ManifestDigest) != "7f1ade2bdb153e2859c92b674a3850edd079e1ef1468e317f3b9a91995eba431" {
					t.Fatal("fixture does not reproduce the released P62-to-P63 manifest identity")
				}
			} else {
				roles := appACLEffectiveCatalogTestRoleNames()
				fixture = newExactAppACLCurrentSuccessorPostgresFixtureWithNames(t, ctx,
					fmt.Sprintf("houfeng_p63_%d", time.Now().UnixNano()), roles.centerRuntime, roles.platformAdmin, roles.migrator)
				migratorDB = fixture.openRolePool(t, ctx, fixture.migratorRole)
				predecessor, _, _ = seedAppACLCurrentReleasedGenesis(t, ctx, fixture, migratorDB, profile)
			}
			seedAppACLCurrentSuccessorArchivedVPS(t, ctx, migratorDB)
			_, _, input := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
			before := readAppACLCurrentTransitionDurableState(t, ctx, migratorDB, input)
			runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
			assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)
			successor, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
			if err != nil {
				t.Fatalf("upgrade released P63: %v", err)
			}
			if successor.ManifestRevision != predecessor.ManifestRevision+1 || successor.PreviousManifestDigest != predecessor.ManifestDigest {
				t.Fatal("P63 successor does not append exactly one revision to the released history")
			}
			assertAppACLCurrentSuccessorUpgradeEffects(t, ctx, migratorDB)
			if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
				t.Fatalf("admit P63 successor runtime: %v", err)
			}
			after := readAppACLCurrentTransitionDurableState(t, ctx, migratorDB, input)
			assertAppACLCurrentManifestHistoryPrefix(t, before.Base.Manifest.Manifests, after.Base.Manifest.Manifests)
			if !bytes.Equal(before.IncidentDefaults, after.IncidentDefaults) || before.SettingsExceptTransitionDigest != after.SettingsExceptTransitionDigest || !before.SettingsUpdated.Equal(after.SettingsUpdated) || before.HeartbeatRowsDigest != after.HeartbeatRowsDigest {
				t.Fatal("P63 upgrade changed existing settings or heartbeat rows")
			}
			var networkRatesColumn bool
			if err := migratorDB.QueryRow(ctx, `select exists(select 1 from pg_catalog.pg_attribute where attrelid='public.host_samples'::regclass and attname='network_rates_valid' and atttypid='boolean'::regtype and not attnotnull and not atthasdef and not attisdropped)`).Scan(&networkRatesColumn); err != nil || !networkRatesColumn {
				t.Fatalf("0064 nullable network rates marker missing or incorrect: %v", err)
			}
			repeated, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
			if err != nil {
				t.Fatalf("repeat P63 successor: %v", err)
			}
			if repeated.ManifestDigest != successor.ManifestDigest || !reflect.DeepEqual(after, readAppACLCurrentTransitionDurableState(t, ctx, migratorDB, input)) {
				t.Fatal("P63 successor repeat changed durable state")
			}
		})
	}
}

func TestPostgresIntegrationAppACLCurrentP63RollbackCutpoints(t *testing.T) {
	profile := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP63LastMigration)
	testAppACLCurrentReleasedTransitionRollback(t, profile, true)
}

func TestPostgresIntegrationAppACLCurrentP63DriftIsReadOnly(t *testing.T) {
	profile := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP63LastMigration)
	for _, tc := range []struct{ name, sql string }{
		{"catalog", `grant delete on public.asset_services to houfeng_runtime`},
		{"heartbeat_index", `drop index public.idx_monitoring_instance_heartbeats_live_received`},
		{"checksum", `update public.schema_migrations set checksum=repeat('0',64) where name='0063_tune_heartbeat_incident_policy.sql'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			state := seedExactAppACLCurrentPredecessor(t, ctx, 3)
			if _, err := appendAppACLCurrentReleasedSuccessor(t, ctx, state.fixture, state.migratorDB, profile); err != nil {
				t.Fatal(err)
			}
			if _, err := state.migratorDB.Exec(ctx, tc.sql); err != nil {
				t.Fatal(err)
			}
			before := readAppACLCurrentTransitionDurableState(t, ctx, state.migratorDB, state.input)
			if _, err := ConvergeAppACLCurrent(ctx, state.migratorDB, state.fixture.runtimeRole, state.fixture.adminRole); err == nil {
				t.Fatal("P63 drift was accepted")
			}
			if !reflect.DeepEqual(before, readAppACLCurrentTransitionDurableState(t, ctx, state.migratorDB, state.input)) {
				t.Fatal("P63 drift rejection changed durable state")
			}
		})
	}
}

func TestAppACLCurrentP63RejectsUnregisteredHistoryAndDrift(t *testing.T) {
	current, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatal(err)
	}
	transitions, err := compileAppACLCurrentTransitions(current, appACLCurrentTransitionDefinitions)
	if err != nil {
		t.Fatal(err)
	}
	p63 := transitions[2]
	privileges, err := appACLCurrentTransitionPrivilegeBody(current)
	if err != nil {
		t.Fatal(err)
	}
	genesis := appACLCurrentShapeManifest(t, 1, [32]byte{}, p63.predecessor, p63.predecessorPrivilegeBody)
	applied := appACLCurrentShapeApplied(t, p63.predecessor)
	for _, tc := range []struct {
		name   string
		mutate func() ([]MigrationChecksumEntry, []AppACLManifestPersistedV1)
	}{
		{"checksum", func() ([]MigrationChecksumEntry, []AppACLManifestPersistedV1) {
			entries := append([]MigrationChecksumEntry(nil), applied...)
			entries[len(entries)-1].Checksum[0] ^= 1
			return entries, []AppACLManifestPersistedV1{genesis}
		}},
		{"unknown_P63_to_P64_chain", func() ([]MigrationChecksumEntry, []AppACLManifestPersistedV1) {
			p64 := transitions[1]
			next := appACLCurrentShapeManifest(t, 2, genesis.ManifestDigest, p64.predecessor, p64.predecessorPrivilegeBody)
			return appACLCurrentShapeApplied(t, p64.predecessor), []AppACLManifestPersistedV1{genesis, next}
		}},
		{"extra_P63_revision", func() ([]MigrationChecksumEntry, []AppACLManifestPersistedV1) {
			next := appACLCurrentShapeManifest(t, 2, genesis.ManifestDigest, p63.predecessor, p63.predecessorPrivilegeBody)
			return applied, []AppACLManifestPersistedV1{genesis, next}
		}},
		{"privilege_drift", func() ([]MigrationChecksumEntry, []AppACLManifestPersistedV1) {
			// Current privileges contain two UPDATE grants absent from released P63.
			wrong := appACLCurrentShapeManifest(t, 1, [32]byte{}, p63.predecessor, privileges)
			return applied, []AppACLManifestPersistedV1{wrong}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ledger, manifests := tc.mutate()
			_, err := appACLCurrentClassifyShape(t, current, transitions, ledger, manifests, "houfeng", appACLCurrentTransitionBindings, privileges)
			if !errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
				t.Fatalf("drift accepted or wrong error: %v", err)
			}
		})
	}
}
