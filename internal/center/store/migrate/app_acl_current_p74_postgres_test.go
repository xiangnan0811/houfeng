package migrate

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
)

func testPostgresIntegrationAppACLCurrentP74Upgrade(t *testing.T) {
	t.Helper()
	for _, state := range []string{"missing", "default", "custom"} {
		state := state
		t.Run("settings_"+state, func(t *testing.T) {
			testPostgresIntegrationAppACLCurrentP74SettingsState(t, state)
		})
	}
	t.Run("rollback_on_settings_corruption", testPostgresIntegrationAppACLCurrentP74RollbackOnSettingsCorruption)
	t.Run("terminal_column_shape_corruption_rejected", testPostgresIntegrationAppACLCurrentP74TerminalColumnShapeCorruption)
	t.Run("fresh_genesis_publication_column_shape_corruption_rejected", func(t *testing.T) {
		testPostgresIntegrationAppACLCurrentP74GenesisColumnShapeCorruption(t, false)
	})
	t.Run("fresh_genesis_repeat_column_shape_corruption_rejected", func(t *testing.T) {
		testPostgresIntegrationAppACLCurrentP74GenesisColumnShapeCorruption(t, true)
	})
	t.Run("fresh_genesis_repeat_preserves_false_ownership", testPostgresIntegrationAppACLCurrentP74GenesisRepeatPreservesFalseOwnership)
	t.Run("p73_to_p74_successor_chain", testPostgresIntegrationAppACLCurrentP74SuccessorChain)
}

func testPostgresIntegrationAppACLCurrentP74SettingsState(t *testing.T, state string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	migratorDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
	predecessorProfile := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP74Migration)
	predecessor, _, _ := seedAppACLCurrentReleasedGenesis(t, ctx, fixture, migratorDB, predecessorProfile)
	configureAppACLCurrentP67SettingsState(t, ctx, migratorDB, state)
	if state == "custom" {
		if _, err := migratorDB.Exec(ctx, `
			update public.center_settings
			set telegram_bot_token = 'c74-custom-token-sentinel',
			    telegram_chat_id = 'c74-custom-chat-sentinel',
			    incident_defaults = incident_defaults || '{"sweep_interval_seconds":17}'::jsonb,
			    created_at = '2024-12-31 23:59:58+00'::timestamptz
			where settings_id = 'center'
		`); err != nil {
			t.Fatalf("set custom C74 settings sentinel: %v", err)
		}
	}
	beforeSettings := readAppACLCurrentSettingsPresenceSnapshot(t, ctx, migratorDB)
	if state == "missing" && beforeSettings.Present {
		t.Fatal("missing C74 center_settings state still has a row")
	}
	if state != "missing" && !beforeSettings.Present {
		t.Fatalf("%s C74 center_settings state has no row", state)
	}
	assertAppACLCurrentP74ColumnAbsent(t, ctx, migratorDB)

	_, _, currentInput := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
	runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	ddlTx, err := runtimeDB.Begin(ctx)
	if err != nil {
		t.Fatalf("begin runtime DDL admission probe: %v", err)
	}
	_, ddlErr := ddlTx.Exec(ctx, `alter table public.center_settings add column p74_runtime_ddl_probe text`)
	_ = ddlTx.Rollback(ctx)
	var pgErr *pgconn.PgError
	if !errors.As(ddlErr, &pgErr) || pgErr.Code != "42501" {
		t.Fatalf("runtime DDL admission error = %v, want PostgreSQL insufficient_privilege", ddlErr)
	}
	assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)

	successor, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
	if err != nil {
		t.Fatalf("C74 to C75 convergence with %s settings: %v", state, err)
	}
	if successor.ManifestRevision != predecessor.ManifestRevision+1 || successor.PreviousManifestDigest != predecessor.ManifestDigest {
		t.Fatalf("C74 to C75 successor = %#v, want revision %d linked to predecessor", successor, predecessor.ManifestRevision+1)
	}
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
		t.Fatalf("admit C75 runtime with %s settings: %v", state, err)
	}
	after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
	assertAppACLCurrentManifestHistoryPrefix(t, before.Manifest.Manifests, after.Manifest.Manifests)
	if len(after.Manifest.Manifests) != len(before.Manifest.Manifests)+1 {
		t.Fatalf("C75 manifest history length = %d, want %d", len(after.Manifest.Manifests), len(before.Manifest.Manifests)+1)
	}
	if len(after.Ledger) != len(before.Ledger)+2 ||
		!reflect.DeepEqual(after.Ledger[:len(before.Ledger)], before.Ledger) ||
		after.Ledger[len(after.Ledger)-2].Name != appACLCurrentP75Migration ||
		after.Ledger[len(after.Ledger)-1].Name != appACLCurrentP76Migration {
		t.Fatalf("C76 ledger tail = %#v, want predecessor prefix plus 0075 and 0076", after.Ledger)
	}
	if !appACLCurrentPrivilegesWithFamilyStateSelect(after.Catalog.DirectPrivileges, before.Catalog.DirectPrivileges, fixture.runtimeRole) ||
		!appACLCurrentPrivilegesWithFamilyStateSelect(after.Catalog.EffectivePrivileges, before.Catalog.EffectivePrivileges, fixture.runtimeRole) ||
		!reflect.DeepEqual(after.Catalog.ColumnACLs, before.Catalog.ColumnACLs) ||
		!reflect.DeepEqual(after.Catalog.DefaultACLs, before.Catalog.DefaultACLs) {
		t.Fatalf("C74 to C75 migration changed ACL state\nbefore direct/effective/column/default: %#v/%#v/%#v/%#v\nafter: %#v/%#v/%#v/%#v",
			before.Catalog.DirectPrivileges, before.Catalog.EffectivePrivileges, before.Catalog.ColumnACLs, before.Catalog.DefaultACLs,
			after.Catalog.DirectPrivileges, after.Catalog.EffectivePrivileges, after.Catalog.ColumnACLs, after.Catalog.DefaultACLs)
	}
	assertAppACLCurrentP74ColumnShape(t, ctx, migratorDB, false)

	afterSettings := readAppACLCurrentSettingsPresenceSnapshot(t, ctx, migratorDB)
	if beforeSettings.Present != afterSettings.Present {
		t.Fatalf("C75 changed center_settings presence from %t to %t", beforeSettings.Present, afterSettings.Present)
	}
	if beforeSettings.Present {
		wantRaw, err := appACLCurrentSettingsDeploymentOverrides(beforeSettings.Raw)
		if err != nil {
			t.Fatalf("build expected C75 settings JSON: %v", err)
		}
		wantExcept, err := appACLCurrentSettingsDeploymentOverrides(beforeSettings.SettingsExceptTransition)
		if err != nil {
			t.Fatalf("build expected C75 non-transition settings JSON: %v", err)
		}
		if !appACLCurrentJSONEqual(wantRaw, afterSettings.Raw) ||
			!appACLCurrentJSONEqual(wantExcept, afterSettings.SettingsExceptTransition) ||
			!appACLCurrentJSONEqual(beforeSettings.IncidentDefaults, afterSettings.IncidentDefaults) ||
			!beforeSettings.UpdatedAt.Equal(afterSettings.UpdatedAt) {
			t.Fatalf("C75 did not preserve existing center_settings fields\nbefore: %#v\nafter: %#v", beforeSettings, afterSettings)
		}
	}

	var managed bool
	err = runtimeDB.QueryRow(ctx, `select incident_sweep_runtime_managed from public.center_settings where settings_id = 'center'`).Scan(&managed)
	if state == "missing" {
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("missing C75 center_settings ownership query error = %v, want pgx.ErrNoRows", err)
		}
	} else {
		if err != nil {
			t.Fatalf("runtime role read C75 ownership column: %v", err)
		}
		if !managed {
			t.Fatal("C75 existing settings row did not receive true ownership value")
		}
		if _, err := runtimeDB.Exec(ctx, `update public.center_settings set incident_sweep_runtime_managed = false where settings_id = 'center'`); err != nil {
			t.Fatalf("runtime role write C75 ownership column: %v", err)
		}
		if err := migratorDB.QueryRow(ctx, `select incident_sweep_runtime_managed from public.center_settings where settings_id = 'center'`).Scan(&managed); err != nil {
			t.Fatalf("read false C75 ownership value: %v", err)
		}
		if managed {
			t.Fatal("runtime role false ownership update did not persist")
		}
	}

	beforeRepeat := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
	repeated, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
	if err != nil {
		t.Fatalf("C75 repeat with %s settings: %v", state, err)
	}
	afterRepeat := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
	if repeated.ManifestDigest != successor.ManifestDigest || !reflect.DeepEqual(afterRepeat, beforeRepeat) {
		t.Fatalf("C75 repeat changed durable state with %s settings\nbefore: %#v\nafter: %#v", state, beforeRepeat, afterRepeat)
	}
	if state != "missing" {
		if err := migratorDB.QueryRow(ctx, `select incident_sweep_runtime_managed from public.center_settings where settings_id = 'center'`).Scan(&managed); err != nil {
			t.Fatalf("read false C75 ownership after repeat: %v", err)
		}
		if managed {
			t.Fatal("C75 repeat changed explicit false ownership back to true")
		}
	}
	if state == "missing" {
		if _, err := runtimeDB.Exec(ctx, `insert into public.center_settings (settings_id) values ('center')`); err != nil {
			t.Fatalf("insert new C75 settings row with runtime defaults: %v", err)
		}
		if err := runtimeDB.QueryRow(ctx, `select incident_sweep_runtime_managed from public.center_settings where settings_id = 'center'`).Scan(&managed); err != nil {
			t.Fatalf("read new C75 settings default ownership: %v", err)
		}
		if managed {
			t.Fatal("new C75 settings row defaulted ownership to true")
		}
		beforeInserted := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
		repeated, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
		if err != nil {
			t.Fatalf("C75 repeat after new settings row: %v", err)
		}
		afterInserted := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
		if repeated.ManifestDigest != successor.ManifestDigest || !reflect.DeepEqual(afterInserted, beforeInserted) {
			t.Fatalf("C75 repeat changed new settings row durable state\nbefore: %#v\nafter: %#v", beforeInserted, afterInserted)
		}
		if err := runtimeDB.QueryRow(ctx, `select incident_sweep_runtime_managed from public.center_settings where settings_id = 'center'`).Scan(&managed); err != nil {
			t.Fatalf("read new C75 settings ownership after repeat: %v", err)
		}
		if managed {
			t.Fatal("C75 repeat changed new row default false ownership to true")
		}
	}
}
func testPostgresIntegrationAppACLCurrentP74SuccessorChain(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	migratorDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
	p73Profile := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP73Migration)
	seedAppACLCurrentReleasedGenesis(t, ctx, fixture, migratorDB, p73Profile)
	p74Profile := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP74Migration)
	p74Manifest, err := appendAppACLCurrentReleasedSuccessor(t, ctx, fixture, migratorDB, p74Profile)
	if err != nil {
		t.Fatalf("append exact P74 successor after P73: %v", err)
	}
	if p74Manifest.ManifestRevision != 2 {
		t.Fatalf("P74 successor chain revision = %d, want 2", p74Manifest.ManifestRevision)
	}
	_, _, currentInput := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
	if len(before.Ledger) == 0 || before.Ledger[len(before.Ledger)-1].Name != appACLCurrentP74Migration {
		t.Fatalf("P74 predecessor ledger tail = %#v, want 0074", before.Ledger)
	}
	runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)

	successor, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
	if err != nil {
		t.Fatalf("P74 successor chain convergence: %v", err)
	}
	if successor.ManifestRevision != 3 || successor.PreviousManifestDigest != p74Manifest.ManifestDigest {
		t.Fatalf("P74 successor chain result = %#v, want revision 3 linked to P74", successor)
	}
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
		t.Fatalf("admit P75 runtime after P74 successor chain: %v", err)
	}
	after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
	if len(after.Ledger) != len(before.Ledger)+2 ||
		!reflect.DeepEqual(after.Ledger[:len(before.Ledger)], before.Ledger) ||
		after.Ledger[len(after.Ledger)-2].Name != appACLCurrentP75Migration ||
		after.Ledger[len(after.Ledger)-1].Name != appACLCurrentP76Migration {
		t.Fatalf("P74 successor chain ledger tail = %#v, want predecessor prefix plus 0075 and 0076", after.Ledger)
	}
	if !appACLCurrentPrivilegesWithFamilyStateSelect(after.Catalog.DirectPrivileges, before.Catalog.DirectPrivileges, fixture.runtimeRole) ||
		!appACLCurrentPrivilegesWithFamilyStateSelect(after.Catalog.EffectivePrivileges, before.Catalog.EffectivePrivileges, fixture.runtimeRole) ||
		!reflect.DeepEqual(after.Catalog.ColumnACLs, before.Catalog.ColumnACLs) ||
		!reflect.DeepEqual(after.Catalog.DefaultACLs, before.Catalog.DefaultACLs) {
		t.Fatalf("P74 successor chain changed ACL state")
	}
	var managed bool
	if err := migratorDB.QueryRow(ctx, `select incident_sweep_runtime_managed from public.center_settings where settings_id = 'center'`).Scan(&managed); err != nil {
		t.Fatalf("read P74 successor chain ownership: %v", err)
	}
	if !managed {
		t.Fatal("P74 successor chain did not backfill existing settings ownership true")
	}
	if _, err := runtimeDB.Exec(ctx, `update public.center_settings set incident_sweep_runtime_managed = false where settings_id = 'center'`); err != nil {
		t.Fatalf("set P74 successor chain ownership false: %v", err)
	}
	beforeRepeat := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
	repeated, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
	if err != nil {
		t.Fatalf("P74 successor chain repeat: %v", err)
	}
	afterRepeat := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
	if repeated.ManifestDigest != successor.ManifestDigest || !reflect.DeepEqual(afterRepeat, beforeRepeat) {
		t.Fatalf("P74 successor chain repeat changed durable state")
	}
	if err := migratorDB.QueryRow(ctx, `select incident_sweep_runtime_managed from public.center_settings where settings_id = 'center'`).Scan(&managed); err != nil {
		t.Fatalf("read P74 successor chain ownership after repeat: %v", err)
	}
	if managed {
		t.Fatal("P74 successor chain repeat changed explicit false ownership to true")
	}
}

func testPostgresIntegrationAppACLCurrentP74RollbackOnSettingsCorruption(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	migratorDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
	predecessorProfile := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP74Migration)
	seedAppACLCurrentReleasedGenesis(t, ctx, fixture, migratorDB, predecessorProfile)
	configureAppACLCurrentP67SettingsState(t, ctx, migratorDB, "custom")
	_, _, currentInput := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
	beforeSettings := readAppACLCurrentSettingsPresenceSnapshot(t, ctx, migratorDB)
	dependencies := defaultAppACLCurrentConvergenceDependencies()
	applyPending := dependencies.applyPending
	dependencies.applyPending = func(ctx context.Context, tx pgx.Tx, source migrationSourceSnapshot, applied []MigrationChecksumEntry) error {
		if err := applyPending(ctx, tx, source, applied); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			update public.center_settings
			set incident_defaults = incident_defaults || '{"sweep_interval_seconds":999}'::jsonb,
			    telegram_bot_token = 'tampered-c75-token-sentinel',
			    updated_at = updated_at + interval '1 second',
			    incident_sweep_runtime_managed = false
			where settings_id = 'center'
		`)
		return err
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
	if err == nil {
		t.Fatal("C74 to C75 accepted a corrupted settings ownership value")
	}
	if !strings.Contains(err.Error(), "settings") {
		t.Fatalf("C74 to C75 corruption error = %v, want settings verification failure", err)
	}
	after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("C74 to C75 corruption rollback changed durable state\nbefore: %#v\nafter: %#v", before, after)
	}
	if settings := readAppACLCurrentSettingsPresenceSnapshot(t, ctx, migratorDB); !reflect.DeepEqual(settings, beforeSettings) {
		t.Fatalf("C74 to C75 corruption rollback changed settings\nbefore: %#v\nafter: %#v", beforeSettings, settings)
	}
	assertAppACLCurrentP74ColumnAbsent(t, ctx, migratorDB)
	runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)
}

func testPostgresIntegrationAppACLCurrentP74TerminalColumnShapeCorruption(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	migratorDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
	predecessorProfile := appACLCurrentReleasedPostgresProfile(t, appACLCurrentP74Migration)
	seedAppACLCurrentReleasedGenesis(t, ctx, fixture, migratorDB, predecessorProfile)
	configureAppACLCurrentP67SettingsState(t, ctx, migratorDB, "default")
	if _, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole); err != nil {
		t.Fatalf("seed C75 terminal state: %v", err)
	}
	_, _, currentInput := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	for _, tc := range []struct {
		name            string
		corrupt         func() error
		repair          func() error
		compareSnapshot bool
	}{
		{
			name: "default true",
			corrupt: func() error {
				_, err := migratorDB.Exec(ctx, `alter table public.center_settings alter column incident_sweep_runtime_managed set default true`)
				return err
			},
			repair: func() error {
				_, err := migratorDB.Exec(ctx, `alter table public.center_settings alter column incident_sweep_runtime_managed set default false`)
				return err
			},
			compareSnapshot: true,
		},
		{
			name: "default missing",
			corrupt: func() error {
				_, err := migratorDB.Exec(ctx, `alter table public.center_settings alter column incident_sweep_runtime_managed drop default`)
				return err
			},
			repair: func() error {
				_, err := migratorDB.Exec(ctx, `alter table public.center_settings alter column incident_sweep_runtime_managed set default false`)
				return err
			},
			compareSnapshot: true,
		},
		{
			name: "nullable",
			corrupt: func() error {
				_, err := migratorDB.Exec(ctx, `alter table public.center_settings alter column incident_sweep_runtime_managed drop not null`)
				return err
			},
			repair: func() error {
				_, err := migratorDB.Exec(ctx, `alter table public.center_settings alter column incident_sweep_runtime_managed set not null`)
				return err
			},
			compareSnapshot: true,
		},
		{
			name: "wrong type",
			corrupt: func() error {
				_, err := migratorDB.Exec(ctx, `
					alter table public.center_settings alter column incident_sweep_runtime_managed drop default;
					alter table public.center_settings alter column incident_sweep_runtime_managed type text using incident_sweep_runtime_managed::text
				`)
				return err
			},
			repair: func() error {
				_, err := migratorDB.Exec(ctx, `
					alter table public.center_settings alter column incident_sweep_runtime_managed type boolean using incident_sweep_runtime_managed::boolean;
					alter table public.center_settings alter column incident_sweep_runtime_managed set default false
				`)
				return err
			},
			compareSnapshot: true,
		},
		{
			name: "missing column",
			corrupt: func() error {
				_, err := migratorDB.Exec(ctx, `alter table public.center_settings drop column incident_sweep_runtime_managed`)
				return err
			},
			compareSnapshot: false,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.corrupt(); err != nil {
				t.Fatalf("corrupt C75 ownership column: %v", err)
			}
			if tc.repair != nil {
				defer func() {
					if err := tc.repair(); err != nil {
						t.Errorf("repair C75 ownership column: %v", err)
					}
				}()
			}
			before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
			_, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
			if err == nil || !strings.Contains(err.Error(), "ownership column") {
				t.Fatalf("C75 terminal column %s error = %v, want ownership column rejection", tc.name, err)
			}
			if tc.compareSnapshot {
				after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
				if !reflect.DeepEqual(after, before) {
					t.Fatalf("C75 terminal column %s rejection changed durable state\nbefore: %#v\nafter: %#v", tc.name, before, after)
				}
			}
		})
	}
}

func testPostgresIntegrationAppACLCurrentP74GenesisColumnShapeCorruption(t *testing.T, repeat bool) {
	t.Helper()
	for _, tc := range []struct {
		name string
		sql  string
	}{
		{name: "missing_column", sql: `alter table public.center_settings drop column incident_sweep_runtime_managed`},
		{name: "wrong_type", sql: `
			alter table public.center_settings alter column incident_sweep_runtime_managed drop default;
			alter table public.center_settings alter column incident_sweep_runtime_managed type text using incident_sweep_runtime_managed::text
		`},
		{name: "nullable", sql: `alter table public.center_settings alter column incident_sweep_runtime_managed drop not null`},
		{name: "default_true", sql: `alter table public.center_settings alter column incident_sweep_runtime_managed set default true`},
		{name: "default_missing", sql: `alter table public.center_settings alter column incident_sweep_runtime_managed drop default`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
			migratorDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
			dependencies := defaultAppACLCurrentConvergenceDependencies()
			beforeFresh := readAppACLConvergenceRollbackState(t, ctx, migratorDB)
			var before appACLCurrentPostgresDurableSnapshot
			var beforeSettings appACLCurrentSettingsPresenceSnapshot
			var beforeColumn []byte
			_, _, currentInput := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
			if repeat {
				genesis, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
				if err != nil {
					t.Fatalf("seed fresh C75 genesis: %v", err)
				}
				if genesis.ManifestRevision != 1 {
					t.Fatalf("fresh C75 genesis revision = %d, want 1", genesis.ManifestRevision)
				}
				if _, err := migratorDB.Exec(ctx, `insert into public.center_settings (settings_id, incident_sweep_runtime_managed) values ('center', false)`); err != nil {
					t.Fatalf("seed intentional false C75 ownership: %v", err)
				}
				if _, err := migratorDB.Exec(ctx, tc.sql); err != nil {
					t.Fatalf("corrupt fresh C75 genesis column: %v", err)
				}
				before = readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
				beforeSettings = readAppACLCurrentSettingsPresenceSnapshot(t, ctx, migratorDB)
				beforeColumn = readAppACLCurrentP74ColumnSnapshot(t, ctx, migratorDB)
				dependencies.applyPending = currentUnexpectedApplyPending(t, "corrupted genesis repeat")
				dependencies.applyDCL = currentUnexpectedApplyDCL(t, "corrupted genesis repeat")
				dependencies.insertGenesis = currentUnexpectedInsertGenesis(t, "corrupted genesis repeat")
				dependencies.readHeadForUpdate = currentUnexpectedHeadForUpdate(t, "corrupted genesis repeat")
			} else {
				applyPending := dependencies.applyPending
				dependencies.applyPending = func(ctx context.Context, tx pgx.Tx, source migrationSourceSnapshot, applied []MigrationChecksumEntry) error {
					if err := applyPending(ctx, tx, source, applied); err != nil {
						return err
					}
					_, err := tx.Exec(ctx, tc.sql)
					return err
				}
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
			if err == nil || !strings.Contains(err.Error(), "ownership column") {
				t.Fatalf("fresh C75 genesis column %s (repeat=%t) error = %v, want ownership column rejection", tc.name, repeat, err)
			}
			if !repeat {
				if after := readAppACLConvergenceRollbackState(t, ctx, migratorDB); after != beforeFresh {
					t.Fatalf("corrupt fresh C75 publication changed durable state\nbefore: %#v\nafter: %#v", beforeFresh, after)
				}
				return
			}
			after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
			afterSettings := readAppACLCurrentSettingsPresenceSnapshot(t, ctx, migratorDB)
			afterColumn := readAppACLCurrentP74ColumnSnapshot(t, ctx, migratorDB)
			if !reflect.DeepEqual(after, before) || !reflect.DeepEqual(afterSettings, beforeSettings) || !reflect.DeepEqual(afterColumn, beforeColumn) {
				t.Fatalf("corrupt fresh C75 genesis repeat changed durable state\nbefore: %#v/%#v/%s\nafter: %#v/%#v/%s", before, beforeSettings, beforeColumn, after, afterSettings, afterColumn)
			}
		})
	}
}

func testPostgresIntegrationAppACLCurrentP74GenesisRepeatPreservesFalseOwnership(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	migratorDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
	genesis, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
	if err != nil {
		t.Fatalf("seed pristine fresh C75 genesis: %v", err)
	}
	if genesis.ManifestRevision != 1 {
		t.Fatalf("fresh C75 genesis revision = %d, want 1", genesis.ManifestRevision)
	}
	_, _, currentInput := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	for _, state := range []string{"missing", "false_ownership"} {
		t.Run(state, func(t *testing.T) {
			if state == "false_ownership" {
				runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
				if _, err := runtimeDB.Exec(ctx, `insert into public.center_settings (settings_id, incident_sweep_runtime_managed) values ('center', false)`); err != nil {
					t.Fatalf("write intentional false C75 ownership: %v", err)
				}
			}
			before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
			beforeSettings := readAppACLCurrentSettingsPresenceSnapshot(t, ctx, migratorDB)
			repeated, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
			if err != nil {
				t.Fatalf("repeat pristine fresh C75 genesis: %v", err)
			}
			after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
			afterSettings := readAppACLCurrentSettingsPresenceSnapshot(t, ctx, migratorDB)
			if repeated.ManifestDigest != genesis.ManifestDigest || !reflect.DeepEqual(after, before) || !reflect.DeepEqual(afterSettings, beforeSettings) {
				t.Fatalf("pristine fresh C75 genesis repeat changed durable state\nbefore: %#v/%#v\nafter: %#v/%#v", before, beforeSettings, after, afterSettings)
			}
			assertAppACLCurrentP74ColumnShape(t, ctx, migratorDB, false)
		})
	}
}

func readAppACLCurrentP74ColumnSnapshot(t *testing.T, ctx context.Context, db *pgxpool.Pool) []byte {
	t.Helper()
	var snapshot []byte
	if err := db.QueryRow(ctx, `
		select coalesce(jsonb_agg(jsonb_build_object(
			'type', attributes.atttypid::regtype::text,
			'not_null', attributes.attnotnull,
			'default', pg_get_expr(defaults.adbin, defaults.adrelid)
		)), '[]'::jsonb)
		from pg_catalog.pg_attribute attributes
		join pg_catalog.pg_class relations on relations.oid = attributes.attrelid
		join pg_catalog.pg_namespace namespaces on namespaces.oid = relations.relnamespace
		left join pg_catalog.pg_attrdef defaults
		  on defaults.adrelid = attributes.attrelid and defaults.adnum = attributes.attnum
		where namespaces.nspname = 'public'
		  and relations.relname = 'center_settings'
		  and attributes.attname = 'incident_sweep_runtime_managed'
		  and not attributes.attisdropped
	`).Scan(&snapshot); err != nil {
		t.Fatalf("read C75 ownership column snapshot: %v", err)
	}
	return snapshot
}

func assertAppACLCurrentP74ColumnAbsent(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	var exists bool
	if err := db.QueryRow(ctx, `
		select exists (
			select 1
			from pg_catalog.pg_attribute attributes
			join pg_catalog.pg_class relations on relations.oid = attributes.attrelid
			join pg_catalog.pg_namespace namespaces on namespaces.oid = relations.relnamespace
			where namespaces.nspname = 'public'
			  and relations.relname = 'center_settings'
			  and attributes.attname = 'incident_sweep_runtime_managed'
			  and not attributes.attisdropped
		)
	`).Scan(&exists); err != nil {
		t.Fatalf("read C75 ownership column presence: %v", err)
	}
	if exists {
		t.Fatal("C74 rollback left incident_sweep_runtime_managed column behind")
	}
}

func assertAppACLCurrentP74ColumnShape(t *testing.T, ctx context.Context, db *pgxpool.Pool, wantCorruptDefault bool) {
	t.Helper()
	var isBoolean, notNull bool
	var defaultExpression *string
	if err := db.QueryRow(ctx, `
		select attributes.atttypid = 'boolean'::regtype,
		       attributes.attnotnull,
		       pg_get_expr(defaults.adbin, defaults.adrelid)
		from pg_catalog.pg_attribute attributes
		join pg_catalog.pg_class relations on relations.oid = attributes.attrelid
		join pg_catalog.pg_namespace namespaces on namespaces.oid = relations.relnamespace
		left join pg_catalog.pg_attrdef defaults
		  on defaults.adrelid = attributes.attrelid and defaults.adnum = attributes.attnum
		where namespaces.nspname = 'public'
		  and relations.relname = 'center_settings'
		  and attributes.attname = 'incident_sweep_runtime_managed'
		  and not attributes.attisdropped
	`).Scan(&isBoolean, &notNull, &defaultExpression); err != nil {
		t.Fatalf("read C75 ownership column shape: %v", err)
	}
	wantDefault := "false"
	if wantCorruptDefault {
		wantDefault = "true"
	}
	if !isBoolean || !notNull || defaultExpression == nil || strings.TrimSpace(strings.ToLower(*defaultExpression)) != wantDefault {
		t.Fatalf("C75 ownership column shape = boolean %t/not-null %t/default %v, want boolean true/not-null true/default %s", isBoolean, notNull, defaultExpression, wantDefault)
	}
}
