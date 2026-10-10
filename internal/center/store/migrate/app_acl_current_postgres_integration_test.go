package migrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"strings"
	"sync"
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
	t.Run("registered_successor", testPostgresIntegrationAppACLCurrentRegisteredSuccessor)
	t.Run("registered_successor_rejections", testPostgresIntegrationAppACLCurrentRegisteredSuccessorRejectsInvalidPredecessor)
	t.Run("runtime_update_acl_drift", testPostgresIntegrationAppACLCurrentRuntimeUpdateDrift)
	t.Run("p67_upgrade", testPostgresIntegrationAppACLCurrentP67Upgrade)
	t.Run("p68_upgrade", testPostgresIntegrationAppACLCurrentP68Upgrade)
	t.Run("p70_upgrade", testPostgresIntegrationAppACLCurrentP70Upgrade)
	t.Run("p71_upgrade", testPostgresIntegrationAppACLCurrentP71Upgrade)
	t.Run("p72_upgrade", testPostgresIntegrationAppACLCurrentP72Upgrade)
	t.Run("registered_settings_presence_matrix", testPostgresIntegrationAppACLCurrentSettingsPresenceMatrix)
	t.Run("missing_settings_predecessor_suffixes", testPostgresIntegrationAppACLCurrentMissingSettingsPredecessorSuffixes)
	t.Run("missing_settings_p67_rollback_and_drift", testPostgresIntegrationAppACLCurrentMissingSettingsP67RollbackAndDrift)
	t.Run("missing_settings_concurrent_initialization", testPostgresIntegrationAppACLCurrentMissingSettingsConcurrentInitialization)
	t.Run("missing_settings_snapshot_fence", testPostgresIntegrationAppACLCurrentMissingSettingsSnapshotFence)
}

func testPostgresIntegrationAppACLCurrentP67Upgrade(t *testing.T) {
	t.Helper()
	profiles := map[string]appACLCurrentReleasedPostgresProfileData{
		"P62": appACLCurrentReleasedPostgresProfile(t, "0062_create_vps_create_idempotency.sql"),
		"P64": appACLCurrentReleasedPostgresProfile(t, "0064_add_network_rates_valid.sql"),
		"P63": appACLCurrentReleasedPostgresProfile(t, "0063_tune_heartbeat_incident_policy.sql"),
		"P66": appACLCurrentReleasedPostgresProfile(t, appACLCurrentP66LastMigration),
		"P67": appACLCurrentReleasedPostgresProfile(t, "0067_refactor_vps_monitoring_lifecycle.sql"),
	}
	chains := [][]string{
		{"P67"},
		{"P62", "P67"},
		{"P64", "P67"},
		{"P62", "P64", "P67"},
		{"P63", "P67"},
		{"P62", "P63", "P67"},
		{"P66", "P67"},
		{"P62", "P66", "P67"},
		{"P64", "P66", "P67"},
		{"P62", "P64", "P66", "P67"},
		{"P63", "P66", "P67"},
		{"P62", "P63", "P66", "P67"},
	}
	for index, chain := range chains {
		chain := append([]string(nil), chain...)
		t.Run(fmt.Sprintf("%02d_%s", index, chain[len(chain)-1]), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			suffix := fmt.Sprintf("%d_%d", time.Now().UnixNano(), index)
			var fixture exactAppACLCurrentSuccessorPostgresFixture
			if chain[0] == "P62" {
				// P62's released manifest digest binds the published database
				// and role identities; use the exact fixture for that profile.
				fixture = newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
			} else {
				roles := appACLEffectiveCatalogTestRoleNames()
				fixture = newExactAppACLCurrentSuccessorPostgresFixtureWithNames(
					t,
					ctx,
					"houfeng_p67_"+suffix,
					roles.centerRuntime,
					roles.platformAdmin,
					roles.migrator,
				)
			}
			migratorDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
			var predecessor AppACLManifestPersistedV1
			for chainIndex, profileName := range chain {
				profile, ok := profiles[profileName]
				if !ok {
					t.Fatalf("missing released profile %q", profileName)
				}
				if chainIndex == 0 {
					predecessor, _, _ = seedAppACLCurrentReleasedGenesis(t, ctx, fixture, migratorDB, profile)
					continue
				}
				var err error
				predecessor, err = appendAppACLCurrentReleasedSuccessor(t, ctx, fixture, migratorDB, profile)
				if err != nil {
					t.Fatalf("append released %s successor: %v", profileName, err)
				}
			}
			_, _, currentInput := appACLCurrentPostgresContract(
				t,
				fixture.asConvergenceFixture(),
				migrations.FS,
				appACLCurrentMigrationFragments,
			)
			before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
			if len(before.Manifest.Manifests) != len(chain) {
				t.Fatalf("released %s history length = %d, want %d", chain[len(chain)-1], len(before.Manifest.Manifests), len(chain))
			}
			if index == 0 {
				if _, err := migratorDB.Exec(ctx, `
					insert into public.vps_assets (
					  vps_id, display_name, lifecycle_status, usage_status, renewal_decision, archived_at
					) values (
					  'vps_acl_p67_existing', 'existing business row', 'archived', 'unknown', 'keep', '2025-01-02 03:04:05+00'::timestamptz
					)
				`); err != nil {
					t.Fatalf("seed existing business row before P67 to current upgrade: %v", err)
				}
			}
			runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
			assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)

			cutpoint := errors.New("controlled P67 successor rollback cutpoint")
			dependencies := defaultAppACLCurrentConvergenceDependencies()
			applyDCL := dependencies.applyDCL
			dependencies.applyDCL = func(
				ctx context.Context,
				tx pgx.Tx,
				contract appACLEffectiveCatalogContract,
			) error {
				if err := applyDCL(ctx, tx, contract); err != nil {
					return err
				}
				return cutpoint
			}
			_, err := convergeAppACLCurrentWithDependencies(
				ctx,
				func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
					return migratorDB.BeginTx(ctx, options)
				},
				fixture.runtimeRole,
				fixture.adminRole,
				migrations.FS,
				appACLCurrentMigrationFragments,
				dependencies,
			)
			if !errors.Is(err, cutpoint) {
				t.Fatalf("P67 to current rollback error = %v, want controlled cutpoint", err)
			}
			afterRollback := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
			if !reflect.DeepEqual(before, afterRollback) {
				t.Fatalf("P67 to current rollback changed durable state\nbefore: %#v\nafter: %#v", before, afterRollback)
			}
			assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)
			if index == 0 {
				var businessRows int
				if err := migratorDB.QueryRow(ctx, `
					select count(*) from public.vps_assets where vps_id = 'vps_acl_p67_existing'
				`).Scan(&businessRows); err != nil {
					t.Fatalf("read existing business row after rollback: %v", err)
				}
				if businessRows != 1 {
					t.Fatalf("existing business rows after rollback = %d, want 1", businessRows)
				}
			}

			successor, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
			if err != nil {
				t.Fatalf("P67 to current upgrade: %v", err)
			}
			if successor.ManifestRevision != predecessor.ManifestRevision+1 ||
				successor.PreviousManifestDigest != predecessor.ManifestDigest {
				t.Fatalf("P67 to current successor = %#v, want revision %d linked to predecessor", successor, predecessor.ManifestRevision+1)
			}
			if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
				t.Fatalf("admit P67 to current runtime: %v", err)
			}
			var runtimeExecute, publicExecute bool
			if err := migratorDB.QueryRow(ctx, `
				select
				  pg_catalog.has_function_privilege($1::pg_catalog.name, procedure.oid, 'EXECUTE'),
				  exists (
				    select 1
				    from pg_catalog.aclexplode(coalesce(procedure.proacl, pg_catalog.acldefault('f', procedure.proowner))) acl
				    where acl.grantee = 0
				      and acl.privilege_type = 'EXECUTE'
				  )
				from pg_catalog.pg_proc procedure
				join pg_catalog.pg_namespace namespace on namespace.oid = procedure.pronamespace
				where namespace.nspname = 'public'
				  and procedure.oid = 'public.houfeng_parse_host_address(text)'::pg_catalog.regprocedure
			`, fixture.runtimeRole).Scan(&runtimeExecute, &publicExecute); err != nil {
				t.Fatalf("read P67 parser function ACL: %v", err)
			}
			if !runtimeExecute || publicExecute {
				t.Fatalf("P67 parser function ACL runtime=%t public=%t, want runtime only", runtimeExecute, publicExecute)
			}
			var parsed bool
			if err := runtimeDB.QueryRow(ctx, `
				select public.houfeng_parse_host_address(' 2001:db8::1 ') is not null
			`).Scan(&parsed); err != nil {
				t.Fatalf("execute P67 parser as runtime: %v", err)
			}
			if !parsed {
				t.Fatal("P67 parser returned NULL for valid IPv6")
			}
			if index == 0 {
				var businessRows int
				if err := migratorDB.QueryRow(ctx, `
					select count(*) from public.vps_assets where vps_id = 'vps_acl_p67_existing'
				`).Scan(&businessRows); err != nil {
					t.Fatalf("read existing business row after upgrade: %v", err)
				}
				if businessRows != 1 {
					t.Fatalf("existing business rows after upgrade = %d, want 1", businessRows)
				}
			}

			afterUpgrade := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
			repeated, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
			if err != nil {
				t.Fatalf("repeat P67 to current upgrade: %v", err)
			}
			afterRepeat := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
			if repeated.ManifestDigest != successor.ManifestDigest || !reflect.DeepEqual(afterUpgrade, afterRepeat) {
				t.Fatalf("P67 to current repeat changed durable state\nbefore: %#v\nafter: %#v", afterUpgrade, afterRepeat)
			}

			runtimeIdentifier := pgx.Identifier{fixture.runtimeRole}.Sanitize()
			if _, err := migratorDB.Exec(ctx, `revoke execute on function public.houfeng_parse_host_address(text) from `+runtimeIdentifier); err != nil {
				t.Fatalf("revoke P67 parser runtime EXECUTE: %v", err)
			}
			if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err == nil || errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
				t.Fatalf("P67 parser runtime ACL drift admission = %v, want concrete rejection", err)
			}
			if _, err := migratorDB.Exec(ctx, `grant execute on function public.houfeng_parse_host_address(text) to `+runtimeIdentifier); err != nil {
				t.Fatalf("restore P67 parser runtime EXECUTE: %v", err)
			}
			if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
				t.Fatalf("admit P67 parser runtime after ACL restoration: %v", err)
			}
		})
	}
}

type appACLCurrentSettingsPresenceSnapshot struct {
	Present                  bool
	Raw                      []byte
	IncidentDefaults         []byte
	SettingsExceptTransition []byte
	UpdatedAt                time.Time
}

func readAppACLCurrentSettingsPresenceSnapshot(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
) appACLCurrentSettingsPresenceSnapshot {
	t.Helper()
	var snapshot appACLCurrentSettingsPresenceSnapshot
	err := db.QueryRow(ctx, `
		select to_jsonb(settings),
		       incident_defaults,
		       to_jsonb(settings) - array['incident_defaults', 'updated_at']::text[],
		       updated_at
		from public.center_settings settings
		where settings_id = 'center'
	`).Scan(&snapshot.Raw, &snapshot.IncidentDefaults, &snapshot.SettingsExceptTransition, &snapshot.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return snapshot
	}
	if err != nil {
		t.Fatalf("read center_settings presence snapshot: %v", err)
	}
	snapshot.Present = true
	var fields map[string]json.RawMessage
	if !json.Valid(snapshot.Raw) || json.Unmarshal(snapshot.Raw, &fields) != nil || fields == nil {
		t.Fatalf("center_settings full JSON snapshot is invalid: %s", snapshot.Raw)
	}
	for _, name := range []string{"settings_id", "incident_defaults", "updated_at"} {
		if _, ok := fields[name]; !ok {
			t.Fatalf("center_settings full JSON snapshot is missing %q: %s", name, snapshot.Raw)
		}
	}
	return snapshot
}

func assertAppACLCurrentSettingsPresenceSnapshotEqual(
	t *testing.T,
	before, after appACLCurrentSettingsPresenceSnapshot,
) {
	t.Helper()
	if before.Present != after.Present {
		t.Fatalf("center_settings presence changed from %t to %t", before.Present, after.Present)
	}
	if !before.Present {
		return
	}
	if !appACLCurrentJSONEqual(before.Raw, after.Raw) ||
		!appACLCurrentJSONEqual(before.IncidentDefaults, after.IncidentDefaults) ||
		!appACLCurrentJSONEqual(before.SettingsExceptTransition, after.SettingsExceptTransition) ||
		!before.UpdatedAt.Equal(after.UpdatedAt) {
		t.Fatalf("center_settings snapshot changed\nbefore: %#v\nafter: %#v", before, after)
	}
}

func configureAppACLCurrentP67SettingsState(t *testing.T, ctx context.Context, db *pgxpool.Pool, state string) {
	t.Helper()
	switch state {
	case "missing":
		if _, err := db.Exec(ctx, `delete from public.center_settings where settings_id = 'center'`); err != nil {
			t.Fatalf("delete center_settings singleton for missing state: %v", err)
		}
	case "default":
		if _, err := db.Exec(ctx, `
			delete from public.center_settings where settings_id = 'center';
			insert into public.center_settings (settings_id) values ('center')
		`); err != nil {
			t.Fatalf("reset center_settings singleton to schema default: %v", err)
		}
	case "custom":
		if _, err := db.Exec(ctx, `
			update public.center_settings
			set incident_defaults = jsonb_set(incident_defaults, '{stale_threshold_intervals}', '20'::jsonb, false),
			    updated_at = '2025-01-02 03:04:05+00'::timestamptz
			where settings_id = 'center'
		`); err != nil {
			t.Fatalf("set custom center_settings singleton: %v", err)
		}
	default:
		t.Fatalf("unknown center_settings state %q", state)
	}
}

func testPostgresIntegrationAppACLCurrentSettingsPresenceMatrix(t *testing.T) {
	t.Helper()
	profile := appACLCurrentReleasedPostgresProfile(t, "0067_refactor_vps_monitoring_lifecycle.sql")
	for _, state := range []string{"missing", "default", "custom"} {
		state := state
		t.Run(state, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
			migratorDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
			predecessor, _, _ := seedAppACLCurrentReleasedGenesis(t, ctx, fixture, migratorDB, profile)
			configureAppACLCurrentP67SettingsState(t, ctx, migratorDB, state)
			beforeSettings := readAppACLCurrentSettingsPresenceSnapshot(t, ctx, migratorDB)
			if state == "missing" && beforeSettings.Present {
				t.Fatal("missing center_settings state still has a row")
			}
			if state != "missing" && !beforeSettings.Present {
				t.Fatalf("%s center_settings state has no row", state)
			}
			if state == "default" || state == "custom" {
				var threshold int
				if err := migratorDB.QueryRow(ctx, `select (incident_defaults->>'stale_threshold_intervals')::int from public.center_settings where settings_id = 'center'`).Scan(&threshold); err != nil {
					t.Fatalf("read %s stale threshold: %v", state, err)
				}
				wantThreshold := 20
				if state == "default" {
					wantThreshold = 12
				}
				if threshold != wantThreshold {
					t.Fatalf("%s stale threshold = %d, want %d", state, threshold, wantThreshold)
				}
			}
			if state == "custom" && !beforeSettings.UpdatedAt.Equal(time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)) {
				t.Fatalf("custom settings updated_at = %s, want fixed timestamp", beforeSettings.UpdatedAt)
			}

			_, _, currentInput := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
			before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
			runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
			assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)
			successor, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
			if err != nil {
				t.Fatalf("P67 %s settings convergence: %v", state, err)
			}
			if successor.ManifestRevision != predecessor.ManifestRevision+1 ||
				successor.PreviousManifestDigest != predecessor.ManifestDigest {
				t.Fatalf("P67 %s successor = %#v, want revision %d linked to predecessor", state, successor, predecessor.ManifestRevision+1)
			}
			if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
				t.Fatalf("admit P67 %s successor runtime: %v", state, err)
			}
			after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
			afterSettings := readAppACLCurrentSettingsPresenceSnapshot(t, ctx, migratorDB)
			assertAppACLCurrentSettingsPresenceSnapshotEqual(t, beforeSettings, afterSettings)
			assertAppACLCurrentManifestHistoryPrefix(t, before.Manifest.Manifests, after.Manifest.Manifests)

			beforeRepeat := after
			repeated, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
			if err != nil {
				t.Fatalf("repeat P67 %s settings convergence: %v", state, err)
			}
			afterRepeat := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
			if repeated.ManifestDigest != successor.ManifestDigest || !reflect.DeepEqual(afterRepeat, beforeRepeat) {
				t.Fatalf("P67 %s settings repeat changed durable state\nbefore: %#v\nafter: %#v", state, beforeRepeat, afterRepeat)
			}
			assertAppACLCurrentSettingsPresenceSnapshotEqual(t, beforeSettings, readAppACLCurrentSettingsPresenceSnapshot(t, ctx, migratorDB))
		})
	}
}

func testPostgresIntegrationAppACLCurrentMissingSettingsPredecessorSuffixes(t *testing.T) {
	t.Helper()
	profiles := []struct {
		name          string
		lastMigration string
	}{
		{name: "P62", lastMigration: "0062_create_vps_create_idempotency.sql"},
		{name: "P63", lastMigration: "0063_tune_heartbeat_incident_policy.sql"},
		{name: "P64", lastMigration: "0064_add_network_rates_valid.sql"},
		{name: "P66", lastMigration: appACLCurrentP66LastMigration},
	}
	for _, profileCase := range profiles {
		profileCase := profileCase
		t.Run(profileCase.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
			migratorDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
			predecessor, _, _ := seedAppACLCurrentReleasedGenesis(
				t,
				ctx,
				fixture,
				migratorDB,
				appACLCurrentReleasedPostgresProfile(t, profileCase.lastMigration),
			)
			if _, err := migratorDB.Exec(ctx, `delete from public.center_settings where settings_id = 'center'`); err != nil {
				t.Fatalf("delete %s predecessor center_settings singleton: %v", profileCase.name, err)
			}
			if settings := readAppACLCurrentSettingsPresenceSnapshot(t, ctx, migratorDB); settings.Present {
				t.Fatalf("%s predecessor still has center_settings row", profileCase.name)
			}
			_, _, currentInput := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
			before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
			runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
			assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)
			successor, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
			if err != nil {
				t.Fatalf("missing-settings %s convergence: %v", profileCase.name, err)
			}
			if successor.ManifestRevision != predecessor.ManifestRevision+1 ||
				successor.PreviousManifestDigest != predecessor.ManifestDigest {
				t.Fatalf("missing-settings %s successor = %#v, want revision %d linked to predecessor", profileCase.name, successor, predecessor.ManifestRevision+1)
			}
			if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
				t.Fatalf("admit missing-settings %s successor runtime: %v", profileCase.name, err)
			}
			after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
			assertAppACLCurrentManifestHistoryPrefix(t, before.Manifest.Manifests, after.Manifest.Manifests)
			if settings := readAppACLCurrentSettingsPresenceSnapshot(t, ctx, migratorDB); settings.Present {
				t.Fatalf("missing-settings %s convergence inserted center_settings row", profileCase.name)
			}
			beforeRepeat := after
			repeated, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
			if err != nil {
				t.Fatalf("repeat missing-settings %s convergence: %v", profileCase.name, err)
			}
			afterRepeat := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
			if repeated.ManifestDigest != successor.ManifestDigest || !reflect.DeepEqual(afterRepeat, beforeRepeat) {
				t.Fatalf("repeat missing-settings %s changed durable state\nbefore: %#v\nafter: %#v", profileCase.name, beforeRepeat, afterRepeat)
			}
		})
	}
}

func testPostgresIntegrationAppACLCurrentMissingSettingsP67RollbackAndDrift(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	migratorDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
	predecessor, _, _ := seedAppACLCurrentReleasedGenesis(
		t,
		ctx,
		fixture,
		migratorDB,
		appACLCurrentReleasedPostgresProfile(t, "0067_refactor_vps_monitoring_lifecycle.sql"),
	)
	if _, err := migratorDB.Exec(ctx, `delete from public.center_settings where settings_id = 'center'`); err != nil {
		t.Fatalf("delete P67 missing-settings singleton: %v", err)
	}
	_, _, currentInput := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
	runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)

	cutpoint := errors.New("controlled P67 missing-settings rollback cutpoint")
	dependencies := defaultAppACLCurrentConvergenceDependencies()
	applyDCL := dependencies.applyDCL
	dependencies.applyDCL = func(ctx context.Context, tx pgx.Tx, contract appACLEffectiveCatalogContract) error {
		if err := applyDCL(ctx, tx, contract); err != nil {
			return err
		}
		return cutpoint
	}
	_, err := convergeAppACLCurrentWithDependencies(
		ctx,
		func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
			return migratorDB.BeginTx(ctx, options)
		},
		fixture.runtimeRole,
		fixture.adminRole,
		migrations.FS,
		appACLCurrentMigrationFragments,
		dependencies,
	)
	if !errors.Is(err, cutpoint) {
		t.Fatalf("P67 missing-settings rollback error = %v, want controlled cutpoint", err)
	}
	afterRollback := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
	if !reflect.DeepEqual(afterRollback, before) {
		t.Fatalf("P67 missing-settings rollback changed durable state\nbefore: %#v\nafter: %#v", before, afterRollback)
	}
	if settings := readAppACLCurrentSettingsPresenceSnapshot(t, ctx, migratorDB); settings.Present {
		t.Fatal("P67 missing-settings rollback inserted center_settings row")
	}
	assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)

	successor, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
	if err != nil {
		t.Fatalf("P67 missing-settings convergence after rollback: %v", err)
	}
	if successor.ManifestRevision != predecessor.ManifestRevision+1 ||
		successor.PreviousManifestDigest != predecessor.ManifestDigest {
		t.Fatalf("P67 missing-settings successor = %#v, want revision %d linked to predecessor", successor, predecessor.ManifestRevision+1)
	}
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
		t.Fatalf("admit P67 missing-settings successor runtime: %v", err)
	}
	afterUpgrade := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
	assertAppACLCurrentManifestHistoryPrefix(t, before.Manifest.Manifests, afterUpgrade.Manifest.Manifests)
	if settings := readAppACLCurrentSettingsPresenceSnapshot(t, ctx, migratorDB); settings.Present {
		t.Fatal("P67 missing-settings convergence inserted center_settings row")
	}

	runtimeIdentifier := pgx.Identifier{fixture.runtimeRole}.Sanitize()
	if _, err := migratorDB.Exec(ctx, `revoke execute on function public.houfeng_parse_host_address(text) from `+runtimeIdentifier); err != nil {
		t.Fatalf("revoke P67 missing-settings parser runtime EXECUTE: %v", err)
	}
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err == nil || errors.Is(err, ErrDevelopmentDatabaseRebuildRequired) {
		t.Fatalf("P67 missing-settings parser runtime ACL drift admission = %v, want concrete rejection", err)
	}
	if _, err := migratorDB.Exec(ctx, `grant execute on function public.houfeng_parse_host_address(text) to `+runtimeIdentifier); err != nil {
		t.Fatalf("restore P67 missing-settings parser runtime EXECUTE: %v", err)
	}
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
		t.Fatalf("admit P67 missing-settings parser runtime after ACL restoration: %v", err)
	}
}

func testPostgresIntegrationAppACLCurrentMissingSettingsConcurrentInitialization(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	seedDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
	predecessor, _, _ := seedAppACLCurrentReleasedGenesis(
		t,
		ctx,
		fixture,
		seedDB,
		appACLCurrentReleasedPostgresProfile(t, "0067_refactor_vps_monitoring_lifecycle.sql"),
	)
	if _, err := seedDB.Exec(ctx, `delete from public.center_settings where settings_id = 'center'`); err != nil {
		t.Fatalf("delete concurrent P67 missing-settings singleton: %v", err)
	}
	_, _, currentInput := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, seedDB, currentInput)
	first := fixture.openRolePool(t, ctx, fixture.migratorRole)
	second := fixture.openRolePool(t, ctx, fixture.migratorRole)
	if err := first.Ping(ctx); err != nil {
		t.Fatalf("ping first concurrent migrator connection: %v", err)
	}
	if err := second.Ping(ctx); err != nil {
		t.Fatalf("ping second concurrent migrator connection: %v", err)
	}
	start := make(chan struct{})
	type result struct {
		manifest AppACLManifestPersistedV1
		err      error
	}
	results := make(chan result, 2)
	var waitGroup sync.WaitGroup
	for _, db := range []*pgxpool.Pool{first, second} {
		db := db
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			<-start
			manifest, err := ConvergeAppACLCurrent(ctx, db, fixture.runtimeRole, fixture.adminRole)
			results <- result{manifest: manifest, err: err}
		}()
	}
	close(start)
	waitGroup.Wait()
	close(results)

	successes := make([]AppACLManifestPersistedV1, 0, 2)
	for outcome := range results {
		if outcome.err == nil {
			successes = append(successes, outcome.manifest)
			continue
		}
		var pgErr *pgconn.PgError
		if !errors.As(outcome.err, &pgErr) || pgErr.Code != "40001" {
			t.Fatalf("concurrent missing-settings convergence error = %v, want success or serialization retry", outcome.err)
		}
	}
	if len(successes) == 0 {
		t.Fatal("concurrent missing-settings convergence produced no successful result")
	}
	for _, manifest := range successes {
		if manifest.ManifestRevision != predecessor.ManifestRevision+1 ||
			manifest.PreviousManifestDigest != predecessor.ManifestDigest {
			t.Fatalf("concurrent missing-settings successor = %#v, want one successor linked to predecessor", manifest)
		}
	}
	for index := 1; index < len(successes); index++ {
		if successes[index].ManifestDigest != successes[0].ManifestDigest {
			t.Fatalf("concurrent missing-settings manifests differ: %#v and %#v", successes[0], successes[index])
		}
	}
	after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, seedDB, currentInput)
	assertAppACLCurrentManifestHistoryPrefix(t, before.Manifest.Manifests, after.Manifest.Manifests)
	if len(after.Manifest.Manifests) != 2 {
		t.Fatalf("concurrent missing-settings manifest history length = %d, want exactly 2", len(after.Manifest.Manifests))
	}
	if settings := readAppACLCurrentSettingsPresenceSnapshot(t, ctx, seedDB); settings.Present {
		t.Fatal("concurrent missing-settings convergence inserted center_settings row")
	}
	runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
		t.Fatalf("admit concurrent missing-settings successor runtime: %v", err)
	}
	assertSingleIntValue(t, ctx, seedDB, `select count(*)::int from public.schema_migrations where name = '0068_normalize_ip_quality_host_address_identity.sql'`, 1)
}

func testPostgresIntegrationAppACLCurrentMissingSettingsSnapshotFence(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	seedDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
	predecessor, _, _ := seedAppACLCurrentReleasedGenesis(
		t,
		ctx,
		fixture,
		seedDB,
		appACLCurrentReleasedPostgresProfile(t, "0067_refactor_vps_monitoring_lifecycle.sql"),
	)
	if _, err := seedDB.Exec(ctx, `delete from public.center_settings where settings_id = 'center'`); err != nil {
		t.Fatalf("delete snapshot-fence missing-settings singleton: %v", err)
	}

	staleDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
	staleTx, err := staleDB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		t.Fatalf("begin stale snapshot-fence transaction: %v", err)
	}
	defer func() {
		_ = staleTx.Rollback(ctx)
	}()
	var staleRevision int64
	if err := staleTx.QueryRow(ctx, `
		select manifest_revision
		from public.app_acl_manifest_head
		where singleton
	`).Scan(&staleRevision); err != nil {
		t.Fatalf("establish stale snapshot-fence head snapshot: %v", err)
	}
	if staleRevision != int64(predecessor.ManifestRevision) {
		t.Fatalf("stale snapshot-fence revision = %d, want predecessor %d", staleRevision, predecessor.ManifestRevision)
	}

	firstDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
	first, err := ConvergeAppACLCurrent(ctx, firstDB, fixture.runtimeRole, fixture.adminRole)
	if err != nil {
		t.Fatalf("first snapshot-fence convergence: %v", err)
	}
	if first.ManifestRevision != predecessor.ManifestRevision+1 {
		t.Fatalf("first snapshot-fence successor revision = %d, want %d", first.ManifestRevision, predecessor.ManifestRevision+1)
	}

	retryDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
	dependencies := defaultAppACLCurrentConvergenceDependencies()
	headLocks := 0
	readCatalogCalls := 0
	originalReadHeadForUpdate := dependencies.readHeadForUpdate
	dependencies.readHeadForUpdate = func(ctx context.Context, tx pgx.Tx) (*AppACLManifestHeadV1, error) {
		headLocks++
		return originalReadHeadForUpdate(ctx, tx)
	}
	originalReadCatalog := dependencies.readCatalog
	dependencies.readCatalog = func(ctx context.Context, tx pgx.Tx, input appACLEffectiveCatalogVerifierInput) (AppACLEffectiveCatalogSnapshotR1, error) {
		readCatalogCalls++
		return originalReadCatalog(ctx, tx, input)
	}
	attempts := 0
	begin := func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
		attempts++
		if attempts == 1 {
			return staleTx, nil
		}
		return retryDB.BeginTx(ctx, options)
	}
	recovered, err := convergeAppACLCurrentWithDependencies(
		ctx,
		begin,
		fixture.runtimeRole,
		fixture.adminRole,
		migrations.FS,
		appACLCurrentMigrationFragments,
		dependencies,
	)
	if err != nil {
		t.Fatalf("stale snapshot-fence convergence retry: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("snapshot-fence convergence attempts = %d, want exactly 2", attempts)
	}
	if headLocks != 1 {
		t.Fatalf("snapshot-fence predecessor head locks = %d, want exactly 1 before retry", headLocks)
	}
	if readCatalogCalls != 1 {
		t.Fatalf("snapshot-fence catalog reads = %d, want exactly 1 after retry", readCatalogCalls)
	}
	if recovered.ManifestDigest != first.ManifestDigest {
		t.Fatalf("snapshot-fence recovered manifest digest = %x, first = %x", recovered.ManifestDigest, first.ManifestDigest)
	}

	runtimeIdentifier := pgx.Identifier{fixture.runtimeRole}.Sanitize()
	if _, err := seedDB.Exec(ctx, `grant delete on table public.record_access_groups to `+runtimeIdentifier); err != nil {
		t.Fatalf("grant negative-control unexpected group DELETE: %v", err)
	}
	negativeAttempts := 0
	negativeDependencies := defaultAppACLCurrentConvergenceDependencies()
	_, err = convergeAppACLCurrentWithDependencies(
		ctx,
		func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
			negativeAttempts++
			return firstDB.BeginTx(ctx, options)
		},
		fixture.runtimeRole,
		fixture.adminRole,
		migrations.FS,
		appACLCurrentMigrationFragments,
		negativeDependencies,
	)
	if err == nil ||
		!strings.Contains(err.Error(), "unexpected") ||
		!strings.Contains(err.Error(), "record_access_groups") {
		t.Fatalf("negative-control unexpected group DELETE error = %v, want strict catalog rejection", err)
	}
	if negativeAttempts != 1 {
		t.Fatalf("negative-control catalog rejection attempts = %d, want exactly 1", negativeAttempts)
	}
	if isAppACLConvergenceRetryable(err) {
		t.Fatalf("negative-control catalog rejection unexpectedly classified retryable: %v", err)
	}
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
	assertRecordCollaborationAppACLCurrentRolePrivileges(t, ctx, fixture, runtimeDB)

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
	for _, table := range []string{"blob_gc_deletions", "blob_publication_intents", "attachment_purge_receipts", "content_workspace_purge_receipts"} {
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
	futureDependencies := defaultAppACLCurrentConvergenceDependencies()
	// This fixture deliberately models an unrelated post-R1 source contract,
	// not the production 0062 -> 0063 registered transition.
	futureDependencies.transitionDefinitions = nil
	_, err := convergeAppACLCurrentWithDependencies(
		ctx,
		func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
			return migratorDB.BeginTx(ctx, options)
		},
		fixture.runtime,
		fixture.admin,
		futureFS,
		futureFragments,
		futureDependencies,
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
	// A predecessor snapshot must use its own managed inventory. New current
	// functions do not exist before the successor transaction executes.
	if len(manifest.AppliedMigrations) > 0 && len(manifest.AppliedMigrations) < currentRootSourceCount {
		last := manifest.AppliedMigrations[len(manifest.AppliedMigrations)-1].Filename
		var fragments []AppACLCurrentMigrationFragment
		for _, fragment := range appACLCurrentMigrationFragments {
			if fragment.Migration <= last {
				fragments = append(fragments, fragment)
			}
		}
		fixture := appACLConvergencePostgresFixture{databaseName: input.Contract.DatabaseName, runtime: input.Contract.RoleBindings[0].CatalogRole, admin: input.Contract.RoleBindings[1].CatalogRole, migrator: input.MigratorRole}
		_, _, input = appACLCurrentPostgresContract(t, fixture, vpsStateRepairMigrationFSThrough(t, last), fragments)
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
