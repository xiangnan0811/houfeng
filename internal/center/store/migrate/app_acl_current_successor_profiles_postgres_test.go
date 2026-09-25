package migrate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
)

const appACLCurrentP64LastMigration = "0064_add_network_rates_valid.sql"

func TestPostgresIntegrationAppACLCurrentP64ReleaseProfiles(t *testing.T) {
	profile := appACLCurrentP64PostgresProfile(t)
	t.Run("p64_genesis_with_custom_database_and_roles", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), os.Getpid())
		roles := appACLEffectiveCatalogTestRoleNames()
		fixture := newExactAppACLCurrentSuccessorPostgresFixtureWithNames(
			t,
			ctx,
			"houfeng_p64_"+suffix,
			roles.centerRuntime,
			roles.platformAdmin,
			roles.migrator,
		)
		migratorDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
		predecessor, _, _ := seedAppACLCurrentReleasedGenesis(t, ctx, fixture, migratorDB, profile)
		seedAppACLCurrentSuccessorArchivedVPS(t, ctx, migratorDB)
		_, _, currentInput := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
		beforeUpgrade := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
		runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
		assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)

		successor, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
		if err != nil {
			t.Fatalf("ConvergeAppACLCurrent() P64 custom genesis upgrade: %v", err)
		}
		if successor.ManifestRevision != 2 || successor.PreviousManifestDigest != predecessor.ManifestDigest {
			t.Fatalf("P64 custom genesis successor = %#v, want revision 2 linked to P64 genesis", successor)
		}
		assertAppACLCurrentSuccessorUpgradeEffects(t, ctx, migratorDB)
		if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
			t.Fatalf("AdmitAppACLCurrentRuntime() P64 custom genesis successor: %v", err)
		}
		afterUpgrade := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
		assertAppACLCurrentManifestHistoryPrefix(t, beforeUpgrade.Manifest.Manifests, afterUpgrade.Manifest.Manifests)
		beforeRepeat := afterUpgrade
		repeated, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
		if err != nil {
			t.Fatalf("ConvergeAppACLCurrent() P64 custom genesis repeat: %v", err)
		}
		afterRepeat := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
		if repeated.ManifestDigest != successor.ManifestDigest || !reflect.DeepEqual(afterRepeat, beforeRepeat) {
			t.Fatalf("P64 custom genesis repeat changed durable state\nbefore: %#v\nafter:  %#v", beforeRepeat, afterRepeat)
		}
	})

	t.Run("p62_to_p64_chain_upgrade_and_repeat", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		state := seedExactAppACLCurrentPredecessor(t, ctx, 3)
		fixture := state.fixture
		migratorDB := state.migratorDB
		p64, err := appendAppACLCurrentReleasedSuccessor(t, ctx, fixture, migratorDB, profile)
		if err != nil {
			t.Fatalf("construct exact released P62 to P64 manifest history: %v", err)
		}
		if p64.ManifestRevision != 2 {
			t.Fatalf("P64 release successor revision = %d, want 2", p64.ManifestRevision)
		}
		seedAppACLCurrentSuccessorArchivedVPS(t, ctx, migratorDB)
		_, _, currentInput := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
		beforeUpgrade := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
		if len(beforeUpgrade.Manifest.Manifests) != 2 {
			t.Fatalf("P62 to P64 predecessor history length = %d, want 2", len(beforeUpgrade.Manifest.Manifests))
		}
		runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
		assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)

		successor, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
		if err != nil {
			t.Fatalf("ConvergeAppACLCurrent() P62 to P64 chain upgrade: %v", err)
		}
		if successor.ManifestRevision != 3 || successor.PreviousManifestDigest != p64.ManifestDigest {
			t.Fatalf("P62 to P64 chain successor = %#v, want revision 3 linked to P64", successor)
		}
		assertAppACLCurrentSuccessorUpgradeEffects(t, ctx, migratorDB)
		if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
			t.Fatalf("AdmitAppACLCurrentRuntime() P62 to P64 chain successor: %v", err)
		}
		afterUpgrade := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
		assertAppACLCurrentManifestHistoryPrefix(t, beforeUpgrade.Manifest.Manifests, afterUpgrade.Manifest.Manifests)
		beforeRepeat := afterUpgrade
		repeated, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
		if err != nil {
			t.Fatalf("ConvergeAppACLCurrent() P62 to P64 chain repeat: %v", err)
		}
		afterRepeat := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
		if repeated.ManifestDigest != successor.ManifestDigest || !reflect.DeepEqual(afterRepeat, beforeRepeat) {
			t.Fatalf("P62 to P64 chain repeat changed durable state\nbefore: %#v\nafter:  %#v", beforeRepeat, afterRepeat)
		}
	})
}

func TestPostgresIntegrationAppACLCurrentP64TransitionRollbackCutpoints(t *testing.T) {
	profile := appACLCurrentP64PostgresProfile(t)
	testAppACLCurrentReleasedTransitionRollback(t, profile, false)
}

func testAppACLCurrentReleasedTransitionRollback(t *testing.T, profile appACLCurrentReleasedPostgresProfileData, fromP62 bool) {
	t.Helper()
	for _, tc := range []struct {
		name string
		kind string
	}{
		{name: "after_first_real_dcl_grant", kind: "partial_dcl"},
		{name: "after_complete_dcl", kind: "after_dcl"},
		{name: "after_successor_manifest_and_head_cas", kind: "after_manifest_head"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			var fixture exactAppACLCurrentSuccessorPostgresFixture
			var migratorDB *pgxpool.Pool
			wantRevision := uint64(2)
			if fromP62 {
				state := seedExactAppACLCurrentPredecessor(t, ctx, 3)
				fixture, migratorDB = state.fixture, state.migratorDB
				if _, err := appendAppACLCurrentReleasedSuccessor(t, ctx, fixture, migratorDB, profile); err != nil {
					t.Fatal(err)
				}
				wantRevision = 3
			} else {
				fixture = newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
				migratorDB = fixture.openRolePool(t, ctx, fixture.migratorRole)
				seedAppACLCurrentReleasedGenesis(t, ctx, fixture, migratorDB, profile)
			}
			seedAppACLCurrentSuccessorArchivedVPS(t, ctx, migratorDB)
			_, _, currentInput := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
			before := readAppACLCurrentTransitionDurableState(t, ctx, migratorDB, currentInput)
			runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
			assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)

			cutpoint := errors.New("controlled current successor PostgreSQL cutpoint")
			dependencies := defaultAppACLCurrentConvergenceDependencies()
			var dclFaultTx *appACLCurrentSuccessorDCLFaultTx
			begin := func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
				transaction, err := migratorDB.BeginTx(ctx, options)
				if err != nil {
					return nil, err
				}
				if tc.kind == "partial_dcl" {
					dclFaultTx = &appACLCurrentSuccessorDCLFaultTx{Tx: transaction, failure: cutpoint}
					return dclFaultTx, nil
				}
				return transaction, nil
			}
			partialDCLApplied := false
			manifestHeadWritten := false
			switch tc.kind {
			case "partial_dcl":
				// appACLCurrentSuccessorDCLFaultTx fails after one real new UPDATE grant.
			case "after_dcl":
				applyDCL := dependencies.applyDCL
				dependencies.applyDCL = func(ctx context.Context, tx pgx.Tx, contract appACLEffectiveCatalogContract) error {
					if err := applyDCL(ctx, tx, contract); err != nil {
						return err
					}
					partialDCLApplied = true
					return cutpoint
				}
			case "after_manifest_head":
				insertSuccessor := dependencies.insertSuccessor
				dependencies.insertSuccessor = func(ctx context.Context, tx pgx.Tx, previous AppACLManifestPersistedV1, migrationsBody, privilegesBody []byte) (AppACLManifestPersistedV1, error) {
					inserted, err := insertSuccessor(ctx, tx, previous, migrationsBody, privilegesBody)
					if err != nil {
						return AppACLManifestPersistedV1{}, err
					}
					manifestHeadWritten = inserted.ManifestRevision == previous.ManifestRevision+1
					return inserted, cutpoint
				}
			}

			attempts := 0
			_, err := convergeAppACLCurrentWithDependencies(
				ctx,
				func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
					attempts++
					return begin(ctx, options)
				},
				fixture.runtimeRole,
				fixture.adminRole,
				migrations.FS,
				appACLCurrentMigrationFragments,
				dependencies,
			)
			if !errors.Is(err, cutpoint) {
				t.Fatalf("ConvergeAppACLCurrent() %s error = %v, want controlled rollback cutpoint", tc.kind, err)
			}
			if attempts != 1 {
				t.Fatalf("controlled %s cutpoint transaction attempts = %d, want 1", tc.kind, attempts)
			}
			if tc.kind == "partial_dcl" {
				if dclFaultTx == nil || dclFaultTx.successfulNewGrants != 1 || dclFaultTx.matchingNewGrants != 2 {
					t.Fatalf("partial DCL grants = successful %d, intercepted %d; want one successful and fail the second", dclFaultTx.successfulNewGrants, dclFaultTx.matchingNewGrants)
				}
			} else if tc.kind == "after_dcl" && !partialDCLApplied {
				t.Fatal("complete DCL cutpoint was reached before DCL applied")
			} else if tc.kind == "after_manifest_head" && !manifestHeadWritten {
				t.Fatal("manifest/head cutpoint was reached before the successor was inserted")
			}

			after := readAppACLCurrentTransitionDurableState(t, ctx, migratorDB, currentInput)
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("%s rollback did not restore migration, ACL, VPS data, constraints, and manifest/head state\nbefore: %#v\nafter:  %#v", tc.kind, before, after)
			}
			assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)

			retried, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
			if err != nil {
				t.Fatalf("ConvergeAppACLCurrent() retry after %s rollback: %v", tc.kind, err)
			}
			if retried.ManifestRevision != wantRevision {
				t.Fatalf("retry after %s rollback manifest revision = %d, want %d", tc.kind, retried.ManifestRevision, wantRevision)
			}
			if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
				t.Fatalf("AdmitAppACLCurrentRuntime() after retry from %s: %v", tc.kind, err)
			}
		})
	}
}

func appACLCurrentP64PostgresProfile(t *testing.T) appACLCurrentReleasedPostgresProfileData {
	t.Helper()
	return appACLCurrentReleasedPostgresProfile(t, appACLCurrentP64LastMigration)
}

func appACLCurrentReleasedPostgresProfile(t *testing.T, lastMigration string) appACLCurrentReleasedPostgresProfileData {
	t.Helper()
	current, err := compileAppACLCurrentSourceContract(migrations.FS, appACLCurrentMigrationFragments)
	if err != nil {
		t.Fatalf("compile current APP ACL source for P64 profile fixture: %v", err)
	}
	transitions, err := compileAppACLCurrentTransitions(current, appACLCurrentTransitionDefinitions)
	if err != nil {
		t.Fatalf("compile exact current APP ACL release profiles: %v", err)
	}
	var releasedTransition *appACLCurrentTransition
	for index := range transitions {
		transition := &transitions[index]
		last := transition.predecessor.sources.names
		if len(last) != 0 && last[len(last)-1] == lastMigration {
			releasedTransition = transition
			break
		}
	}
	if releasedTransition == nil {
		t.Fatal("compiled release profiles do not include the exact P64 predecessor")
	}

	profileFS := appACLCurrentTransitionTestFS(t)
	for _, name := range current.sources.names {
		if name > lastMigration {
			delete(profileFS, name)
		}
	}
	profileFragments := make([]AppACLCurrentMigrationFragment, 0, len(appACLCurrentMigrationFragments))
	for _, fragment := range appACLCurrentMigrationFragments {
		if fragment.Migration <= lastMigration {
			profileFragments = append(profileFragments, cloneAppACLCurrentMigrationFragment(fragment))
		}
	}
	profileSource, err := compileAppACLCurrentSourceContract(profileFS, profileFragments)
	if err != nil {
		t.Fatalf("compile exact P64 fixture source: %v", err)
	}
	if !bytes.Equal(profileSource.sources.canonicalSet, releasedTransition.predecessor.sources.canonicalSet) {
		t.Fatal("P64 fixture source differs from the transition compiler's independently checked release profile")
	}
	fixedContract, err := compileAppACLCurrentCatalogContract(profileSource, appACLCurrentTransitionDatabase, appACLCurrentTransitionBindings, appACLCurrentTransitionMigrator)
	if err != nil {
		t.Fatalf("compile fixed-binding P64 catalog profile: %v", err)
	}
	privileges, err := CanonicalPrivilegeSetBodyV1(fixedContract.RoleBindings, fixedContract.Privileges)
	if err != nil {
		t.Fatalf("encode fixed-binding P64 privilege body: %v", err)
	}
	if !bytes.Equal(privileges, releasedTransition.predecessorPrivilegeBody) {
		t.Fatal("P64 fixture privilege body differs from the transition compiler's independently checked release profile")
	}
	return appACLCurrentReleasedPostgresProfileData{
		fs:              profileFS,
		fragments:       profileFragments,
		source:          profileSource,
		privilegeGolden: append([]byte(nil), privileges...),
	}
}

type appACLCurrentReleasedPostgresProfileData struct {
	fs              fstest.MapFS
	fragments       []AppACLCurrentMigrationFragment
	source          appACLCurrentSourceContract
	privilegeGolden []byte
}

func seedAppACLCurrentReleasedGenesis(
	t *testing.T,
	ctx context.Context,
	fixture exactAppACLCurrentSuccessorPostgresFixture,
	migratorDB *pgxpool.Pool,
	profile appACLCurrentReleasedPostgresProfileData,
) (AppACLManifestPersistedV1, appACLEffectiveCatalogContract, appACLEffectiveCatalogVerifierInput) {
	t.Helper()
	contract, privileges, input := appACLCurrentReleasedFixtureContract(t, fixture, profile)
	dependencies := defaultAppACLCurrentConvergenceDependencies()
	dependencies.transitionDefinitions = nil
	manifest, err := convergeAppACLCurrentWithDependencies(
		ctx,
		func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
			return migratorDB.BeginTx(ctx, options)
		},
		fixture.runtimeRole,
		fixture.adminRole,
		profile.fs,
		profile.fragments,
		dependencies,
	)
	if err != nil {
		t.Fatalf("converge exact P64 release profile genesis: %v", err)
	}
	if manifest.ManifestRevision != 1 || manifest.PreviousManifestDigest != ([32]byte{}) ||
		!bytes.Equal(manifest.CanonicalMigrationSet, profile.source.sources.canonicalSet) ||
		!bytes.Equal(manifest.CanonicalPrivilegeSet, privileges) {
		t.Fatalf("P64 release genesis does not preserve exact source/privilege profile: %#v", manifest)
	}
	seedAppACLCurrentReleasedTransitionSettings(t, ctx, migratorDB)
	return manifest, contract, input
}

func appACLCurrentReleasedFixtureContract(
	t *testing.T,
	fixture exactAppACLCurrentSuccessorPostgresFixture,
	profile appACLCurrentReleasedPostgresProfileData,
) (appACLEffectiveCatalogContract, []byte, appACLEffectiveCatalogVerifierInput) {
	t.Helper()
	bindings := []AppACLRoleBinding{
		{Subject: AppACLSubjectCenterRuntime, CatalogRole: fixture.runtimeRole},
		{Subject: AppACLSubjectPlatformAdmin, CatalogRole: fixture.adminRole},
	}
	contract, err := compileAppACLCurrentCatalogContract(profile.source, fixture.databaseName, bindings, fixture.migratorRole)
	if err != nil {
		t.Fatalf("compile P64 fixture ACL contract: %v", err)
	}
	privileges, err := CanonicalPrivilegeSetBodyV1(contract.RoleBindings, contract.Privileges)
	if err != nil {
		t.Fatalf("encode P64 fixture ACL privileges: %v", err)
	}
	input, err := newAppACLEffectiveCatalogVerifierInput(contract, fixture.migratorRole)
	if err != nil {
		t.Fatalf("build P64 fixture catalog verifier input: %v", err)
	}
	return contract, privileges, input
}

func seedAppACLCurrentReleasedTransitionSettings(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	if _, err := db.Exec(ctx, `
		insert into public.center_settings (settings_id, incident_defaults, override_rules, updated_at)
		values (
		  'center',
		  jsonb_build_object(
		    'heartbeat_interval_seconds', 5,
		    'stale_threshold_intervals', 12,
		    'sweep_interval_seconds', 5,
		    'notify_on_started', true,
		    'notify_on_escalated', true,
		    'notify_on_recovered', true
		  ),
		  '{"monitoring_instance_labels":[{"label":"edge","overrides":{"incident_defaults":{"stale_threshold_intervals":3}}}],"target_types":[],"target_labels":[]}'::jsonb,
		  '2025-01-02 03:04:05+00'::timestamptz
		)
	`); err != nil {
		t.Fatalf("seed P64 settings transition snapshot: %v", err)
	}
}

func appendAppACLCurrentReleasedSuccessor(
	t *testing.T,
	ctx context.Context,
	fixture exactAppACLCurrentSuccessorPostgresFixture,
	migratorDB *pgxpool.Pool,
	profile appACLCurrentReleasedPostgresProfileData,
) (AppACLManifestPersistedV1, error) {
	t.Helper()
	_, privileges, input := appACLCurrentReleasedFixtureContract(t, fixture, profile)
	tx, err := migratorDB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("begin P62-to-P64 fixture release transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	applied, err := readAppliedAppMigrationsV1(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if err := applyPendingMigrationSourcesInTx(ctx, tx, profile.source.sources, applied); err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("apply released P64 migration suffix: %w", err)
	}
	applied, err = readAppliedAppMigrationsV1(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if err := compareAppACLCurrentMigrationEntries(profile.source.sources.canonicalSet, applied, "P64 release fixture migration ledger"); err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	catalog, err := readAppACLEffectiveCatalogSnapshotInTx(ctx, tx, input)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("read exact P64 release fixture catalog: %w", err)
	}
	if err := verifyAppACLEffectiveCatalogSnapshot(catalog, input); err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("verify exact P64 release fixture catalog: %w", err)
	}
	manifests, err := readAppACLManifestRevisionsV1(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if len(manifests) != 1 {
		return AppACLManifestPersistedV1{}, fmt.Errorf("P62-to-P64 fixture history has %d rows before release append, want one", len(manifests))
	}
	if !bytes.Equal(manifests[0].CanonicalPrivilegeSet, privileges) {
		return AppACLManifestPersistedV1{}, fmt.Errorf("released P64 privilege body changes the exact P62 fixture grants")
	}
	manifest, err := insertAppACLManifestSuccessorV1(ctx, tx, manifests[0], profile.source.sources.canonicalSet, privileges)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("insert released P64 fixture manifest successor: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("commit released P64 fixture manifest successor: %w", err)
	}
	return manifest, nil
}

func seedAppACLCurrentSuccessorArchivedVPS(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	if _, err := db.Exec(ctx, `
		insert into public.vps_assets (
		  vps_id, display_name, lifecycle_status, usage_status, renewal_decision, archived_at
		) values (
		  'vps_acl_successor_archive', 'P64 archived state fixture', 'archived', 'in_use', 'keep', '2025-01-02 03:04:05+00'::timestamptz
		)
	`); err != nil {
		t.Fatalf("seed archived VPS before current successor migrations: %v", err)
	}
}

func assertAppACLCurrentSuccessorUpgradeEffects(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	assertSingleIntValue(t, ctx, db, `select count(*)::int from public.schema_migrations where name in ('0065_extend_vps_lifecycle_audit_and_snapshot.sql', '0066_constrain_monitoring_and_target_state_values.sql')`, 2)
	assertSingleIntValue(t, ctx, db, `select count(*)::int from public.schema_migrations`, currentRootSourceCount)
	var currentUsage string
	var archivedState []byte
	if err := db.QueryRow(ctx, `
		select usage_status, archived_state_snapshot
		from public.vps_assets
		where vps_id = 'vps_acl_successor_archive'
	`).Scan(&currentUsage, &archivedState); err != nil {
		t.Fatalf("read migrated archived VPS state: %v", err)
	}
	var snapshot struct {
		LifecycleStatus string `json:"lifecycle_status"`
		UsageStatus     string `json:"usage_status"`
		RenewalDecision string `json:"renewal_decision"`
		Source          string `json:"source"`
	}
	if err := json.Unmarshal(archivedState, &snapshot); err != nil {
		t.Fatalf("decode migrated archived VPS snapshot: %v", err)
	}
	if currentUsage != "unknown" || snapshot.LifecycleStatus != "archived" || snapshot.UsageStatus != "in_use" ||
		snapshot.RenewalDecision != "keep" || snapshot.Source != "migration_observation" {
		t.Fatalf("migrated archived VPS current state/snapshot = %q/%#v, want unknown and preserved migration observation", currentUsage, snapshot)
	}
	var validatedConstraints int
	if err := db.QueryRow(ctx, `
		select count(*)::int
		from pg_catalog.pg_constraint
		where convalidated
		  and conname = any($1::text[])
	`, []string{
		"asset_lifecycle_actions_type_allowed",
		"asset_lifecycle_action_steps_object_type_allowed",
		"asset_lifecycle_action_steps_step_type_allowed",
		"vps_assets_state_combination_valid",
		"monitoring_instances_lifecycle_status_allowed",
		"monitoring_instances_monitoring_status_allowed",
		"monitoring_instances_binding_status_allowed",
		"targets_run_status_allowed",
	}).Scan(&validatedConstraints); err != nil {
		t.Fatalf("read post-upgrade validated lifecycle/state constraints: %v", err)
	}
	if validatedConstraints != 8 {
		t.Fatalf("post-upgrade validated migration constraints = %d, want 8", validatedConstraints)
	}
}

func assertAppACLCurrentRuntimeRejectsPredecessor(t *testing.T, ctx context.Context, runtimeDB *pgxpool.Pool) {
	t.Helper()
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); !errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
		t.Fatalf("AdmitAppACLCurrentRuntime() predecessor error = %v, want rebuild-required", err)
	}
}

func assertAppACLCurrentManifestHistoryPrefix(t *testing.T, before, after []AppACLManifestPersistedV1) {
	t.Helper()
	if len(before) == 0 || len(after) < len(before) || !reflect.DeepEqual(after[:len(before)], before) {
		t.Fatalf("current successor did not preserve complete historical manifest prefix\nbefore: %#v\nafter:  %#v", before, after)
	}
}

type appACLCurrentSuccessorDCLFaultTx struct {
	pgx.Tx
	failure             error
	matchingNewGrants   int
	successfulNewGrants int
}

func (tx *appACLCurrentSuccessorDCLFaultTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	statement := strings.ToLower(strings.TrimSpace(sql))
	if strings.HasPrefix(statement, "grant update on table ") &&
		(strings.Contains(statement, `"asset_services"`) || strings.Contains(statement, `"asset_domains"`)) {
		tx.matchingNewGrants++
		if tx.matchingNewGrants == 2 {
			return pgconn.CommandTag{}, tx.failure
		}
		commandTag, err := tx.Tx.Exec(ctx, sql, args...)
		if err == nil {
			tx.successfulNewGrants++
		}
		return commandTag, err
	}
	return tx.Tx.Exec(ctx, sql, args...)
}
