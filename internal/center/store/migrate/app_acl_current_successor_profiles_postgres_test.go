package migrate

import (
	"bytes"
	"context"
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
		assertAppACLCurrentSuccessorRejectsLegacyVPS(t, ctx, migratorDB)
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
		assertAppACLCurrentSuccessorRejectsLegacyVPS(t, ctx, migratorDB)
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
	partialDCLGrantTarget := ""
	if names := profile.source.sources.names; len(names) > 0 && names[len(names)-1] == appACLCurrentP66LastMigration {
		partialDCLGrantTarget = `"monitoring_agent_sessions"`
	}
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
			assertAppACLCurrentSuccessorRejectsLegacyVPS(t, ctx, migratorDB)
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
					dclFaultTx = &appACLCurrentSuccessorDCLFaultTx{
						Tx: transaction, failure: cutpoint, lifecycleGrantTarget: partialDCLGrantTarget,
					}
					return dclFaultTx, nil
				}
				return transaction, nil
			}
			partialDCLApplied := false
			manifestHeadWritten := false
			switch tc.kind {
			case "partial_dcl":
				// Fail after one successful matching 0065 UPDATE or 0067 lifecycle grant.
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
	contract, privileges, input := appACLCurrentReleasedFixtureContract(t, fixture, profile)
	profileLastMigration := ""
	if names := profile.source.sources.names; len(names) > 0 {
		profileLastMigration = names[len(names)-1]
	}
	if profileLastMigration == "" {
		return AppACLManifestPersistedV1{}, fmt.Errorf("released APP profile has no migrations")
	}
	isP66 := profileLastMigration == appACLCurrentP66LastMigration
	isP67 := profileLastMigration == "0067_refactor_vps_monitoring_lifecycle.sql"
	isP68 := profileLastMigration == "0068_normalize_ip_quality_host_address_identity.sql"
	tx, err := migratorDB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("begin released APP profile fixture transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	applied, err := readAppliedAppMigrationsV1(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if err := applyPendingMigrationSourcesInTx(ctx, tx, profile.source.sources, applied); err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("apply released APP profile migration suffix: %w", err)
	}
	applied, err = readAppliedAppMigrationsV1(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if err := compareAppACLCurrentMigrationEntries(profile.source.sources.canonicalSet, applied, "released APP profile fixture migration ledger"); err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	manifests, err := readAppACLManifestRevisionsV1(ctx, tx)
	if err != nil {
		return AppACLManifestPersistedV1{}, err
	}
	if len(manifests) == 0 {
		return AppACLManifestPersistedV1{}, fmt.Errorf("released APP profile fixture has no predecessor history")
	}
	for index, manifest := range manifests {
		if manifest.ManifestRevision != uint64(index+1) {
			return AppACLManifestPersistedV1{}, fmt.Errorf("released APP profile fixture history revision %d has row %d", manifest.ManifestRevision, index+1)
		}
		if index == 0 {
			if manifest.PreviousManifestDigest != ([32]byte{}) {
				return AppACLManifestPersistedV1{}, fmt.Errorf("released APP profile fixture genesis has a predecessor digest")
			}
		} else if manifest.PreviousManifestDigest != manifests[index-1].ManifestDigest {
			return AppACLManifestPersistedV1{}, fmt.Errorf("released APP profile fixture revision %d is not linked to revision %d", manifest.ManifestRevision, index)
		}
	}
	previous := manifests[len(manifests)-1]
	if isP66 {
		if err := validateAppACLCurrentReleasedP66PrivilegeDelta(previous.CanonicalPrivilegeSet, privileges); err != nil {
			return AppACLManifestPersistedV1{}, fmt.Errorf("released P66 privilege body delta: %w", err)
		}
		for _, table := range []string{"asset_services", "asset_domains"} {
			statement := "grant update on table " +
				pgx.Identifier{appACLManagedPublicSchemaR1, table}.Sanitize() +
				" to " + pgx.Identifier{fixture.runtimeRole}.Sanitize()
			if _, err := tx.Exec(ctx, statement); err != nil {
				return AppACLManifestPersistedV1{}, fmt.Errorf("grant released P66 runtime UPDATE on %s: %w", table, err)
			}
		}
	} else if isP67 {
		added, err := validateAppACLCurrentReleasedP67PrivilegeDelta(previous.CanonicalPrivilegeSet, privileges)
		if err != nil {
			return AppACLManifestPersistedV1{}, fmt.Errorf("released P67 privilege body delta: %w", err)
		}
		for _, privilege := range added {
			if privilege.Subject != AppACLSubjectCenterRuntime ||
				privilege.ObjectClass != AppACLObjectClassTable ||
				privilege.SchemaName != appACLManagedPublicSchemaR1 {
				return AppACLManifestPersistedV1{}, fmt.Errorf("released P67 added unsupported runtime privilege %#v", privilege)
			}
			statement := "grant " + strings.ToLower(string(privilege.Privilege)) + " on table " +
				pgx.Identifier{privilege.SchemaName, privilege.ObjectIdentity}.Sanitize() +
				" to " + pgx.Identifier{fixture.runtimeRole}.Sanitize()
			if _, err := tx.Exec(ctx, statement); err != nil {
				return AppACLManifestPersistedV1{}, fmt.Errorf("grant released P67 runtime privilege on %s: %w", privilege.ObjectIdentity, err)
			}
		}
	} else if isP68 {
		if err := validateAppACLCurrentReleasedP68PreviousContract(t, fixture, previous); err != nil {
			return AppACLManifestPersistedV1{}, err
		}
		dependencies := defaultAppACLCurrentConvergenceDependencies()
		if err := dependencies.applyDCL(ctx, tx, contract); err != nil {
			return AppACLManifestPersistedV1{}, fmt.Errorf("apply released P68 catalog DCL: %w", err)
		}
	} else if !bytes.Equal(previous.CanonicalPrivilegeSet, privileges) {
		return AppACLManifestPersistedV1{}, fmt.Errorf("released profile privilege body changes the exact predecessor grants")
	}
	catalog, err := readAppACLEffectiveCatalogSnapshotInTx(ctx, tx, input)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("read exact released APP profile fixture catalog: %w", err)
	}
	if err := verifyAppACLEffectiveCatalogSnapshot(catalog, input); err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("verify exact released APP profile fixture catalog: %w", err)
	}
	manifest, err := insertAppACLManifestSuccessorV1(ctx, tx, previous, profile.source.sources.canonicalSet, privileges)
	if err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("insert released APP profile fixture manifest successor: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return AppACLManifestPersistedV1{}, fmt.Errorf("commit released APP profile fixture manifest successor: %w", err)
	}
	return manifest, nil
}

func validateAppACLCurrentReleasedP68PreviousContract(
	t *testing.T,
	fixture exactAppACLCurrentSuccessorPostgresFixture,
	previous AppACLManifestPersistedV1,
) error {
	t.Helper()
	entries, err := ParseCanonicalMigrationSetBodyV1(previous.CanonicalMigrationSet)
	if err != nil {
		return fmt.Errorf("parse released P68 predecessor migration body: %w", err)
	}
	if len(entries) == 0 {
		return fmt.Errorf("released P68 predecessor migration body is empty")
	}
	lastMigration := entries[len(entries)-1].Filename
	registered := appACLCurrentReleasedPostgresProfile(t, lastMigration)
	if !bytes.Equal(previous.CanonicalMigrationSet, registered.source.sources.canonicalSet) {
		return fmt.Errorf("released P68 predecessor source differs from registered %s profile", lastMigration)
	}
	_, privileges, _ := appACLCurrentReleasedFixtureContract(t, fixture, registered)
	if !bytes.Equal(previous.CanonicalPrivilegeSet, privileges) {
		return fmt.Errorf("released P68 predecessor privileges differ from registered %s profile under fixture bindings", lastMigration)
	}
	return nil
}

func validateAppACLCurrentReleasedP66PrivilegeDelta(previousBody, p66Body []byte) error {
	previous, err := ParseCanonicalPrivilegeSetBodyV1(previousBody)
	if err != nil {
		return fmt.Errorf("parse predecessor privileges: %w", err)
	}
	p66, err := ParseCanonicalPrivilegeSetBodyV1(p66Body)
	if err != nil {
		return fmt.Errorf("parse P66 privileges: %w", err)
	}
	if !reflect.DeepEqual(previous.RoleBindings, p66.RoleBindings) {
		return fmt.Errorf("role bindings changed")
	}
	remaining := make(map[AppACLPrivilege]struct{}, len(previous.Privileges))
	for _, privilege := range previous.Privileges {
		remaining[privilege] = struct{}{}
	}
	added := make([]AppACLPrivilege, 0, 2)
	for _, privilege := range p66.Privileges {
		if _, exists := remaining[privilege]; exists {
			delete(remaining, privilege)
			continue
		}
		added = append(added, privilege)
	}
	if len(remaining) != 0 {
		return fmt.Errorf("P66 removed predecessor privileges: %#v", remaining)
	}
	want, err := canonicalPrivileges([]AppACLPrivilege{
		{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: "asset_services", Privilege: AppACLPrivilegeUpdate},
		{Subject: AppACLSubjectCenterRuntime, ObjectClass: AppACLObjectClassTable, SchemaName: appACLManagedPublicSchemaR1, ObjectIdentity: "asset_domains", Privilege: AppACLPrivilegeUpdate},
	})
	if err != nil {
		return fmt.Errorf("canonicalize expected 0065 UPDATE grants: %w", err)
	}
	if !reflect.DeepEqual(added, want) {
		return fmt.Errorf("P66 added privileges %#v, want exactly the two 0065 runtime UPDATE grants %#v", added, want)
	}
	return nil
}
func validateAppACLCurrentReleasedP67PrivilegeDelta(previousBody, p67Body []byte) ([]AppACLPrivilege, error) {
	previous, err := ParseCanonicalPrivilegeSetBodyV1(previousBody)
	if err != nil {
		return nil, fmt.Errorf("parse predecessor privileges: %w", err)
	}
	p67, err := ParseCanonicalPrivilegeSetBodyV1(p67Body)
	if err != nil {
		return nil, fmt.Errorf("parse P67 privileges: %w", err)
	}
	if !reflect.DeepEqual(previous.RoleBindings, p67.RoleBindings) {
		return nil, fmt.Errorf("role bindings changed")
	}
	oldPrivileges := make(map[AppACLPrivilege]struct{}, len(previous.Privileges))
	for _, privilege := range previous.Privileges {
		oldPrivileges[privilege] = struct{}{}
	}
	p67Privileges := make(map[AppACLPrivilege]struct{}, len(p67.Privileges))
	for _, privilege := range p67.Privileges {
		p67Privileges[privilege] = struct{}{}
	}
	for privilege := range oldPrivileges {
		if _, retained := p67Privileges[privilege]; !retained {
			return nil, fmt.Errorf("P67 removed predecessor privilege %#v", privilege)
		}
	}
	added := make([]AppACLPrivilege, 0, len(p67.Privileges)-len(previous.Privileges))
	for _, privilege := range p67.Privileges {
		if _, existed := oldPrivileges[privilege]; !existed {
			added = append(added, privilege)
		}
	}
	expected := make(map[AppACLPrivilege]struct{})
	for _, table := range []string{"asset_services", "asset_domains"} {
		privilege := AppACLPrivilege{
			Subject:        AppACLSubjectCenterRuntime,
			ObjectClass:    AppACLObjectClassTable,
			SchemaName:     appACLManagedPublicSchemaR1,
			ObjectIdentity: table,
			Privilege:      AppACLPrivilegeUpdate,
		}
		if _, alreadyPresent := oldPrivileges[privilege]; !alreadyPresent {
			expected[privilege] = struct{}{}
		}
	}
	for _, privilege := range vpsMonitoringLifecycleAppACLCurrentMigrationFragment().Privileges(appACLCurrentTransitionDatabase) {
		expected[privilege] = struct{}{}
	}
	if len(added) != len(expected) {
		return nil, fmt.Errorf("P67 added privileges %#v, want exact 0065 and 0067 additions", added)
	}
	for _, privilege := range added {
		if _, wanted := expected[privilege]; !wanted {
			return nil, fmt.Errorf("P67 added unexpected privilege %#v", privilege)
		}
	}
	return added, nil
}

func assertAppACLCurrentSuccessorRejectsLegacyVPS(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	if _, err := db.Exec(ctx, `
		insert into public.vps_assets (
		  vps_id, display_name, lifecycle_status, usage_status, renewal_decision, archived_at
		) values (
		  'vps_acl_successor_archive', 'legacy archived state fixture', 'archived', 'unknown', 'keep', '2025-01-02 03:04:05+00'::timestamptz
		)
	`); err != nil {
		t.Fatalf("seed archived VPS before current successor migrations: %v", err)
	}
	// Legacy business rows are now an explicit rebuild boundary. Exercise the
	// refusal before removing only this test fixture so empty-schema catalog
	// transition and rollback checks can still cover the released ACL profiles.
	payload, err := migrations.FS.ReadFile("0067_refactor_vps_monitoring_lifecycle.sql")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, migrationErr := tx.Exec(ctx, string(payload))
	_ = tx.Rollback(ctx)
	if migrationErr == nil || !strings.Contains(migrationErr.Error(), "requires a fresh installation") {
		t.Fatalf("current lifecycle migration legacy refusal = %v", migrationErr)
	}
	if _, err := db.Exec(ctx, `delete from public.vps_assets where vps_id='vps_acl_successor_archive'`); err != nil {
		t.Fatalf("remove refused legacy fixture: %v", err)
	}
}

func assertAppACLCurrentSuccessorUpgradeEffects(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	assertSingleIntValue(t, ctx, db, `select count(*)::int from public.schema_migrations where name in ('0065_extend_vps_lifecycle_audit_and_snapshot.sql', '0066_constrain_monitoring_and_target_state_values.sql')`, 2)
	assertSingleIntValue(t, ctx, db, `select count(*)::int from public.schema_migrations`, currentRootSourceCount)
	assertSingleIntValue(t, ctx, db, `select count(*)::int from public.schema_migrations where name='0067_refactor_vps_monitoring_lifecycle.sql'`, 1)
	assertSingleIntValue(t, ctx, db, `select count(*)::int from public.vps_assets`, 0)
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
		"vps_assets_validity_allowed",
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
	failure              error
	lifecycleGrantTarget string
	matchingNewGrants    int
	successfulNewGrants  int
}

func (tx *appACLCurrentSuccessorDCLFaultTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	statement := strings.ToLower(strings.TrimSpace(sql))
	matches := strings.HasPrefix(statement, "grant update on table ") &&
		(strings.Contains(statement, `"asset_services"`) || strings.Contains(statement, `"asset_domains"`))
	if tx.lifecycleGrantTarget != "" {
		matches = strings.HasPrefix(statement, "grant ") && strings.Contains(statement, tx.lifecycleGrantTarget)
	}
	if matches {
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
