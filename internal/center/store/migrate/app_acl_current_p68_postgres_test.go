package migrate

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
)

const appACLCurrentP68LastMigration = "0068_normalize_ip_quality_host_address_identity.sql"
const appACLCurrentP69Migration = "0069_add_cpu_rates_valid.sql"

type appACLCurrentP68History struct {
	name     string
	profiles []string
}

func testPostgresIntegrationAppACLCurrentP68Upgrade(t *testing.T) {
	t.Helper()
	profiles := map[string]appACLCurrentReleasedPostgresProfileData{
		"P62": appACLCurrentReleasedPostgresProfile(t, "0062_create_vps_create_idempotency.sql"),
		"P64": appACLCurrentReleasedPostgresProfile(t, "0064_add_network_rates_valid.sql"),
		"P63": appACLCurrentReleasedPostgresProfile(t, "0063_tune_heartbeat_incident_policy.sql"),
		"P66": appACLCurrentReleasedPostgresProfile(t, appACLCurrentP66LastMigration),
		"P67": appACLCurrentReleasedPostgresProfile(t, "0067_refactor_vps_monitoring_lifecycle.sql"),
		"P68": appACLCurrentReleasedPostgresProfile(t, appACLCurrentP68LastMigration),
	}

	// Keep this oracle independent from the production registry. These are the
	// 23 released predecessor chains followed by the C68 profile, plus the C68
	// genesis itself.
	histories := []appACLCurrentP68History{
		{name: "p68_only", profiles: []string{"P68"}},
		{name: "p62_p68", profiles: []string{"P62", "P68"}},
		{name: "p64_p68", profiles: []string{"P64", "P68"}},
		{name: "p62_p64_p68", profiles: []string{"P62", "P64", "P68"}},
		{name: "p63_p68", profiles: []string{"P63", "P68"}},
		{name: "p62_p63_p68", profiles: []string{"P62", "P63", "P68"}},
		{name: "p66_p68", profiles: []string{"P66", "P68"}},
		{name: "p62_p66_p68", profiles: []string{"P62", "P66", "P68"}},
		{name: "p64_p66_p68", profiles: []string{"P64", "P66", "P68"}},
		{name: "p62_p64_p66_p68", profiles: []string{"P62", "P64", "P66", "P68"}},
		{name: "p63_p66_p68", profiles: []string{"P63", "P66", "P68"}},
		{name: "p62_p63_p66_p68", profiles: []string{"P62", "P63", "P66", "P68"}},
		{name: "p67_p68", profiles: []string{"P67", "P68"}},
		{name: "p62_p67_p68", profiles: []string{"P62", "P67", "P68"}},
		{name: "p64_p67_p68", profiles: []string{"P64", "P67", "P68"}},
		{name: "p62_p64_p67_p68", profiles: []string{"P62", "P64", "P67", "P68"}},
		{name: "p63_p67_p68", profiles: []string{"P63", "P67", "P68"}},
		{name: "p62_p63_p67_p68", profiles: []string{"P62", "P63", "P67", "P68"}},
		{name: "p66_p67_p68", profiles: []string{"P66", "P67", "P68"}},
		{name: "p62_p66_p67_p68", profiles: []string{"P62", "P66", "P67", "P68"}},
		{name: "p64_p66_p67_p68", profiles: []string{"P64", "P66", "P67", "P68"}},
		{name: "p62_p64_p66_p67_p68", profiles: []string{"P62", "P64", "P66", "P67", "P68"}},
		{name: "p63_p66_p67_p68", profiles: []string{"P63", "P66", "P67", "P68"}},
		{name: "p62_p63_p66_p67_p68", profiles: []string{"P62", "P63", "P66", "P67", "P68"}},
	}
	if len(histories) != 24 {
		t.Fatalf("P68 historical fixture cases = %d, want 24", len(histories))
	}

	for index, history := range histories {
		history := history
		t.Run(fmt.Sprintf("%02d_%s", index, history.name), func(t *testing.T) {
			if history.name == "p68_only" {
				runAppACLCurrentP68OnlyUpgrade(t, profiles["P68"])
				return
			}
			runAppACLCurrentP68HistoryUpgrade(t, history, profiles, false)
		})
	}

	testPostgresIntegrationAppACLCurrentP68SettingsPresence(t, profiles["P68"])
	testPostgresIntegrationAppACLCurrentP68FailureCutpoints(t, profiles["P68"])
}

func runAppACLCurrentP68HistoryUpgrade(
	t *testing.T,
	history appACLCurrentP68History,
	profiles map[string]appACLCurrentReleasedPostgresProfileData,
	withLegacyCPU bool,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var fixture exactAppACLCurrentSuccessorPostgresFixture
	if history.profiles[0] == "P62" {
		fixture = newExactAppACLCurrentSuccessorPostgresFixture(t, ctx)
	} else {
		roles := appACLEffectiveCatalogTestRoleNames()
		fixture = newExactAppACLCurrentSuccessorPostgresFixtureWithNames(
			t,
			ctx,
			"houfeng_p68_"+roles.suffix,
			roles.centerRuntime,
			roles.platformAdmin,
			roles.migrator,
		)
	}
	migratorDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
	var predecessor AppACLManifestPersistedV1
	for index, profileName := range history.profiles {
		profile, ok := profiles[profileName]
		if !ok {
			t.Fatalf("missing released P68 profile %q", profileName)
		}
		if index == 0 {
			predecessor, _, _ = seedAppACLCurrentReleasedGenesis(t, ctx, fixture, migratorDB, profile)
			continue
		}
		var err error
		predecessor, err = appendAppACLCurrentReleasedSuccessor(t, ctx, fixture, migratorDB, profile)
		if err != nil {
			t.Fatalf("append released %s successor: %v", profileName, err)
		}
	}
	if predecessor.ManifestRevision != uint64(len(history.profiles)) {
		t.Fatalf("C68 predecessor revision = %d, want %d", predecessor.ManifestRevision, len(history.profiles))
	}
	if withLegacyCPU {
		seedAppACLCurrentP68LegacyCPURows(t, ctx, migratorDB)
	}
	_, _, currentInput := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
	before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
	runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
	assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)

	successor, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
	if err != nil {
		t.Fatalf("C68 to C69 convergence: %v", err)
	}
	if successor.ManifestRevision != predecessor.ManifestRevision+1 || successor.PreviousManifestDigest != predecessor.ManifestDigest {
		t.Fatalf("C68 to C69 successor = %#v, want revision %d linked to predecessor", successor, predecessor.ManifestRevision+1)
	}
	if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
		t.Fatalf("admit C69 runtime: %v", err)
	}
	after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
	assertAppACLCurrentManifestHistoryPrefix(t, before.Manifest.Manifests, after.Manifest.Manifests)
	assertAppACLCurrentP68LedgerAppend(t, before.Ledger, after.Ledger)
	if len(after.Manifest.Manifests) != len(before.Manifest.Manifests)+1 {
		t.Fatalf("C69 manifest history length = %d, want %d", len(after.Manifest.Manifests), len(before.Manifest.Manifests)+1)
	}
	if withLegacyCPU {
		assertAppACLCurrentP68LegacyRowsAfterUpgrade(t, ctx, migratorDB)
		assertAppACLCurrentP68CPUVerificationRows(t, ctx, migratorDB)
		if !reflect.DeepEqual(before.Catalog, after.Catalog) {
			t.Fatal("P68-only C69 upgrade changed effective ACL catalog")
		}
	}

	beforeRepeat := after
	repeated, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
	if err != nil {
		t.Fatalf("repeat C68 to C69 convergence: %v", err)
	}
	afterRepeat := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
	if repeated.ManifestDigest != successor.ManifestDigest || !reflect.DeepEqual(afterRepeat, beforeRepeat) {
		t.Fatalf("C69 repeat changed durable state\nbefore: %#v\nafter:  %#v", beforeRepeat, afterRepeat)
	}
}

func runAppACLCurrentP68OnlyUpgrade(t *testing.T, profile appACLCurrentReleasedPostgresProfileData) {
	t.Helper()
	runAppACLCurrentP68HistoryUpgrade(t, appACLCurrentP68History{name: "p68_only", profiles: []string{"P68"}}, map[string]appACLCurrentReleasedPostgresProfileData{"P68": profile}, true)
}

func assertAppACLCurrentP68LedgerAppend(t *testing.T, before, after []appACLCurrentPostgresLedgerRow) {
	t.Helper()
	if len(after) != len(before)+1 || !reflect.DeepEqual(after[:len(before)], before) {
		t.Fatalf("C69 ledger did not preserve its predecessor prefix\nbefore: %#v\nafter:  %#v", before, after)
	}
	count := 0
	for _, row := range after {
		if row.Name == appACLCurrentP69Migration {
			count++
		}
	}
	if count != 1 || after[len(after)-1].Name != appACLCurrentP69Migration {
		t.Fatalf("C69 ledger tail/count = %q/%d, want one final 0069 row", after[len(after)-1].Name, count)
	}
}

func testPostgresIntegrationAppACLCurrentP68SettingsPresence(t *testing.T, profile appACLCurrentReleasedPostgresProfileData) {
	t.Helper()
	for _, state := range []string{"missing", "default", "custom"} {
		t.Run("p68_only_settings_"+state, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			roles := appACLEffectiveCatalogTestRoleNames()
			fixture := newExactAppACLCurrentSuccessorPostgresFixtureWithNames(
				t,
				ctx,
				"houfeng_p68_settings_"+roles.suffix,
				roles.centerRuntime,
				roles.platformAdmin,
				roles.migrator,
			)
			migratorDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
			predecessor, _, _ := seedAppACLCurrentReleasedGenesis(t, ctx, fixture, migratorDB, profile)
			configureAppACLCurrentP67SettingsState(t, ctx, migratorDB, state)
			beforeSettings := readAppACLCurrentSettingsPresenceSnapshot(t, ctx, migratorDB)
			_, _, currentInput := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
			before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
			runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
			assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)

			successor, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
			if err != nil {
				t.Fatalf("P68-only settings=%s convergence: %v", state, err)
			}
			if successor.ManifestRevision != predecessor.ManifestRevision+1 {
				t.Fatalf("P68-only settings=%s revision = %d, want %d", state, successor.ManifestRevision, predecessor.ManifestRevision+1)
			}
			assertAppACLCurrentSettingsPresenceSnapshotEqual(t, beforeSettings, readAppACLCurrentSettingsPresenceSnapshot(t, ctx, migratorDB))
			if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
				t.Fatalf("admit P68-only settings=%s runtime: %v", state, err)
			}
			beforeRepeat := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
			repeated, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
			if err != nil {
				t.Fatalf("repeat P68-only settings=%s convergence: %v", state, err)
			}
			afterRepeat := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
			if repeated.ManifestDigest != successor.ManifestDigest || !reflect.DeepEqual(afterRepeat, beforeRepeat) || reflect.DeepEqual(before, beforeRepeat) {
				t.Fatalf("P68-only settings=%s repeat state mismatch", state)
			}
		})
	}
}

func testPostgresIntegrationAppACLCurrentP68FailureCutpoints(t *testing.T, profile appACLCurrentReleasedPostgresProfileData) {
	t.Helper()
	for _, stage := range []string{"apply_pending", "apply_dcl"} {
		t.Run("p68_only_"+stage+"_rollback", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			roles := appACLEffectiveCatalogTestRoleNames()
			fixture := newExactAppACLCurrentSuccessorPostgresFixtureWithNames(
				t,
				ctx,
				"houfeng_p68_failure_"+roles.suffix,
				roles.centerRuntime,
				roles.platformAdmin,
				roles.migrator,
			)
			migratorDB := fixture.openRolePool(t, ctx, fixture.migratorRole)
			seedAppACLCurrentReleasedGenesis(t, ctx, fixture, migratorDB, profile)
			seedAppACLCurrentP68LegacyCPURows(t, ctx, migratorDB)
			configureAppACLCurrentP67SettingsState(t, ctx, migratorDB, "custom")
			_, _, currentInput := appACLCurrentPostgresContract(t, fixture.asConvergenceFixture(), migrations.FS, appACLCurrentMigrationFragments)
			before := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
			beforeRows := readAppACLCurrentP68LegacyCPURows(t, ctx, migratorDB)
			runtimeDB := fixture.openRolePool(t, ctx, fixture.runtimeRole)
			assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)

			cutpoint := errors.New("controlled C69 " + stage + " rollback")
			dependencies := defaultAppACLCurrentConvergenceDependencies()
			if stage == "apply_pending" {
				applyPending := dependencies.applyPending
				dependencies.applyPending = func(ctx context.Context, tx pgx.Tx, source migrationSourceSnapshot, applied []MigrationChecksumEntry) error {
					if err := applyPending(ctx, tx, source, applied); err != nil {
						return err
					}
					return cutpoint
				}
			} else {
				applyDCL := dependencies.applyDCL
				dependencies.applyDCL = func(ctx context.Context, tx pgx.Tx, contract appACLEffectiveCatalogContract) error {
					if err := applyDCL(ctx, tx, contract); err != nil {
						return err
					}
					return cutpoint
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
			if !errors.Is(err, cutpoint) {
				t.Fatalf("C69 %s rollback error = %v, want controlled cutpoint", stage, err)
			}
			assertAppACLCurrentP68CPUColumnsAbsent(t, ctx, migratorDB)
			if afterRows := readAppACLCurrentP68LegacyCPURows(t, ctx, migratorDB); !reflect.DeepEqual(afterRows, beforeRows) {
				t.Fatalf("C69 %s rollback changed legacy CPU rows\nbefore: %#v\nafter:  %#v", stage, beforeRows, afterRows)
			}
			after := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("C69 %s rollback changed durable manifest/catalog state\nbefore: %#v\nafter:  %#v", stage, before, after)
			}
			assertAppACLCurrentRuntimeRejectsPredecessor(t, ctx, runtimeDB)

			successor, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
			if err != nil {
				t.Fatalf("C69 retry after %s rollback: %v", stage, err)
			}
			if successor.ManifestRevision != 2 {
				t.Fatalf("C69 retry after %s revision = %d, want 2", stage, successor.ManifestRevision)
			}
			assertAppACLCurrentP68CPUColumnsPresent(t, ctx, migratorDB)
			assertAppACLCurrentP68LegacyRowsAfterUpgrade(t, ctx, migratorDB)
			if err := AdmitAppACLCurrentRuntime(ctx, runtimeDB); err != nil {
				t.Fatalf("admit C69 after %s retry: %v", stage, err)
			}
			beforeRepeat := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
			repeated, err := ConvergeAppACLCurrent(ctx, migratorDB, fixture.runtimeRole, fixture.adminRole)
			if err != nil {
				t.Fatalf("C69 repeat after %s retry: %v", stage, err)
			}
			afterRepeat := readAppACLCurrentPostgresDurableSnapshot(t, ctx, migratorDB, currentInput)
			if repeated.ManifestDigest != successor.ManifestDigest || !reflect.DeepEqual(afterRepeat, beforeRepeat) {
				t.Fatalf("C69 repeat after %s changed durable state", stage)
			}
		})
	}
}

type appACLCurrentP68LegacyCPURows struct {
	hostUsage      float64
	hostIOWait     float64
	hostSteal      float64
	hostMemory     float64
	dailySample    int
	dailyAvgCPU    float64
	dailyMaxCPU    float64
	dailyAvgIOWait float64
	dailyMaxIOWait float64
	dailyAvgSteal  float64
	dailyMaxSteal  float64
	dailyFinalized bool
}

func seedAppACLCurrentP68LegacyCPURows(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	if _, err := db.Exec(ctx, `
		insert into public.vps_assets (vps_id, display_name, lifecycle_status, usage_status, renewal_decision)
		values ('vps_p68_legacy', 'P68 legacy CPU fixture', 'active', 'in_use', 'keep');
		insert into public.monitoring_instances (
			monitoring_instance_id, display_name, region, city, provider, lifecycle_status,
			monitoring_status, binding_status, vps_id
		) values ('mi_p68_legacy', 'P68 legacy CPU fixture', 'HK', 'Hong Kong', 'Test Provider', '已接入', '启用', '未绑定', 'vps_p68_legacy');
		insert into public.host_samples (
			monitoring_instance_id, observed_at, received_at, agent_version, fingerprint,
			cpu_usage_pct, load_1, load_5, load_15, mem_used_pct, mem_available_bytes,
			mem_total_bytes, swap_used_pct, disk_used_pct, disk_total_bytes, inode_used_pct,
			net_in_bytes_per_sec, net_out_bytes_per_sec, network_rates_valid,
			cpu_iowait_pct, cpu_steal_pct, disk_read_bytes_per_sec, disk_write_bytes_per_sec,
			disk_busy_pct, uptime_seconds, maintenance_context, is_backfilled, sync_batch_id
		) values (
			'mi_p68_legacy', '2024-08-11 01:02:03+00', '2024-08-11 01:02:04+00', 'v1.15.7', 'p68-legacy-fingerprint',
			37.5, 1.5, 2.5, 3.5, 61.0, 250000, 1000000, 4.0, 12.0, 4000000, 8.0,
			100, 50, true, 4.5, 1.25, 10, 20, 5.0, 3600, false, false, 'p68-legacy-host'
		);
		insert into public.monitoring_instance_host_sample_daily_aggregates (
			monitoring_instance_id, bucket_date, sample_count,
			avg_cpu_usage_pct, max_cpu_usage_pct, avg_load_5, max_load_5,
			avg_mem_used_pct, max_mem_used_pct, avg_cpu_iowait_pct, max_cpu_iowait_pct,
			avg_cpu_steal_pct, max_cpu_steal_pct, avg_disk_busy_pct, max_disk_busy_pct,
			backfilled_sample_count, maintenance_sample_count, finalized
		) values (
			'mi_p68_legacy', '2024-08-11', 5,
			37.5, 42.0, 2.5, 3.5, 61.0, 65.0, 4.5, 5.5,
			1.25, 2.25, 5.0, 6.0, 1, 1, true
		)
	`); err != nil {
		t.Fatalf("seed P68 legacy CPU rows: %v", err)
	}
}

func readAppACLCurrentP68LegacyCPURows(t *testing.T, ctx context.Context, db *pgxpool.Pool) appACLCurrentP68LegacyCPURows {
	t.Helper()
	var rows appACLCurrentP68LegacyCPURows
	if err := db.QueryRow(ctx, `
		select cpu_usage_pct, cpu_iowait_pct, cpu_steal_pct, mem_used_pct
		from public.host_samples where sync_batch_id = 'p68-legacy-host'
	`).Scan(&rows.hostUsage, &rows.hostIOWait, &rows.hostSteal, &rows.hostMemory); err != nil {
		t.Fatalf("read P68 legacy host row: %v", err)
	}
	if err := db.QueryRow(ctx, `
		select sample_count, avg_cpu_usage_pct, max_cpu_usage_pct,
		       avg_cpu_iowait_pct, max_cpu_iowait_pct,
		       avg_cpu_steal_pct, max_cpu_steal_pct, finalized
		from public.monitoring_instance_host_sample_daily_aggregates
		where monitoring_instance_id = 'mi_p68_legacy' and bucket_date = '2024-08-11'
	`).Scan(&rows.dailySample, &rows.dailyAvgCPU, &rows.dailyMaxCPU, &rows.dailyAvgIOWait, &rows.dailyMaxIOWait, &rows.dailyAvgSteal, &rows.dailyMaxSteal, &rows.dailyFinalized); err != nil {
		t.Fatalf("read P68 legacy daily row: %v", err)
	}
	return rows
}

func assertAppACLCurrentP68CPUColumnsAbsent(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	var present [7]bool
	if err := db.QueryRow(ctx, `
		select
		  exists (select 1 from information_schema.columns where table_schema = 'public' and table_name = 'host_samples' and column_name = 'cpu_rates_valid'),
		  exists (select 1 from information_schema.columns where table_schema = 'public' and table_name = 'monitoring_instance_host_sample_daily_aggregates' and column_name = 'cpu_valid_sample_count'),
		  exists (select 1 from information_schema.columns where table_schema = 'public' and table_name = 'monitoring_instance_host_sample_daily_aggregates' and column_name = 'cpu_valid_backfilled_sample_count'),
		  exists (select 1 from information_schema.columns where table_schema = 'public' and table_name = 'monitoring_instance_host_sample_daily_aggregates' and column_name = 'cpu_valid_maintenance_sample_count'),
		  exists (select 1 from pg_catalog.pg_constraint where conname = 'host_daily_cpu_valid_count_check'),
		  exists (select 1 from pg_catalog.pg_constraint where conname = 'host_daily_cpu_valid_backfilled_count_check'),
		  exists (select 1 from pg_catalog.pg_constraint where conname = 'host_daily_cpu_valid_maintenance_count_check')
	`).Scan(&present[0], &present[1], &present[2], &present[3], &present[4], &present[5], &present[6]); err != nil {
		t.Fatalf("read rolled-back C69 CPU schema: %v", err)
	}
	for index, value := range present {
		if value {
			t.Fatalf("rolled-back C69 CPU schema item %d still exists", index)
		}
	}
}

func assertAppACLCurrentP68CPUColumnsPresent(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	var present [7]bool
	if err := db.QueryRow(ctx, `
		select
		  exists (select 1 from information_schema.columns where table_schema = 'public' and table_name = 'host_samples' and column_name = 'cpu_rates_valid'),
		  exists (select 1 from information_schema.columns where table_schema = 'public' and table_name = 'monitoring_instance_host_sample_daily_aggregates' and column_name = 'cpu_valid_sample_count'),
		  exists (select 1 from information_schema.columns where table_schema = 'public' and table_name = 'monitoring_instance_host_sample_daily_aggregates' and column_name = 'cpu_valid_backfilled_sample_count'),
		  exists (select 1 from information_schema.columns where table_schema = 'public' and table_name = 'monitoring_instance_host_sample_daily_aggregates' and column_name = 'cpu_valid_maintenance_sample_count'),
		  exists (select 1 from pg_catalog.pg_constraint where conname = 'host_daily_cpu_valid_count_check'),
		  exists (select 1 from pg_catalog.pg_constraint where conname = 'host_daily_cpu_valid_backfilled_count_check'),
		  exists (select 1 from pg_catalog.pg_constraint where conname = 'host_daily_cpu_valid_maintenance_count_check')
	`).Scan(&present[0], &present[1], &present[2], &present[3], &present[4], &present[5], &present[6]); err != nil {
		t.Fatalf("read committed C69 CPU schema: %v", err)
	}
	for index, value := range present {
		if !value {
			t.Fatalf("committed C69 CPU schema item %d is absent", index)
		}
	}
}

func assertAppACLCurrentP68LegacyRowsAfterUpgrade(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	assertAppACLCurrentP68CPUColumnsPresent(t, ctx, db)
	before := appACLCurrentP68LegacyCPURows{
		hostUsage:      37.5,
		hostIOWait:     4.5,
		hostSteal:      1.25,
		hostMemory:     61.0,
		dailySample:    5,
		dailyAvgCPU:    37.5,
		dailyMaxCPU:    42.0,
		dailyAvgIOWait: 4.5,
		dailyMaxIOWait: 5.5,
		dailyAvgSteal:  1.25,
		dailyMaxSteal:  2.25,
		dailyFinalized: true,
	}
	if got := readAppACLCurrentP68LegacyCPURows(t, ctx, db); !reflect.DeepEqual(got, before) {
		t.Fatalf("C69 changed legacy CPU values/finalized state: %#v", got)
	}
	var marker *bool
	var validCount, backfilledCount, maintenanceCount *int
	if err := db.QueryRow(ctx, `
		select cpu_rates_valid,
		       (select cpu_valid_sample_count from public.monitoring_instance_host_sample_daily_aggregates where monitoring_instance_id = 'mi_p68_legacy' and bucket_date = '2024-08-11'),
		       (select cpu_valid_backfilled_sample_count from public.monitoring_instance_host_sample_daily_aggregates where monitoring_instance_id = 'mi_p68_legacy' and bucket_date = '2024-08-11'),
		       (select cpu_valid_maintenance_sample_count from public.monitoring_instance_host_sample_daily_aggregates where monitoring_instance_id = 'mi_p68_legacy' and bucket_date = '2024-08-11')
		from public.host_samples where sync_batch_id = 'p68-legacy-host'
	`).Scan(&marker, &validCount, &backfilledCount, &maintenanceCount); err != nil {
		t.Fatalf("read C69 legacy CPU NULL markers: %v", err)
	}
	if marker != nil || validCount != nil || backfilledCount != nil || maintenanceCount != nil {
		t.Fatalf("C69 backfilled legacy CPU markers/counts = %v/%v/%v/%v, want NULL", marker, validCount, backfilledCount, maintenanceCount)
	}
}

func assertAppACLCurrentP68CPUVerificationRows(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()
	if _, err := db.Exec(ctx, `
		insert into public.monitoring_instance_host_sample_daily_aggregates (
			monitoring_instance_id, bucket_date, sample_count,
			cpu_valid_sample_count, cpu_valid_backfilled_sample_count, cpu_valid_maintenance_sample_count,
			avg_cpu_usage_pct, max_cpu_usage_pct, avg_load_5, max_load_5,
			avg_mem_used_pct, max_mem_used_pct, avg_cpu_iowait_pct, max_cpu_iowait_pct,
			avg_cpu_steal_pct, max_cpu_steal_pct, avg_disk_busy_pct, max_disk_busy_pct,
			backfilled_sample_count, maintenance_sample_count, finalized
		) values ('mi_p68_legacy', '2024-08-12', 1, 0, 0, 0, null, null, 1, 1, 1, 1, null, null, null, null, 1, 1, 0, 0, false),
		('mi_p68_legacy', '2024-08-13', 5, 2, 1, 1, 20, 20, 1, 1, 1, 1, 2, 2, 3, 3, 1, 1, 0, 0, false)
	`); err != nil {
		t.Fatalf("insert C69 nullable/count verification rows: %v", err)
	}
	var nullCPUValues int
	if err := db.QueryRow(ctx, `
		select count(*)::int
		from public.monitoring_instance_host_sample_daily_aggregates
		where monitoring_instance_id = 'mi_p68_legacy' and bucket_date = '2024-08-12'
		  and avg_cpu_usage_pct is null and max_cpu_usage_pct is null
		  and avg_cpu_iowait_pct is null and max_cpu_iowait_pct is null
		  and avg_cpu_steal_pct is null and max_cpu_steal_pct is null
	`).Scan(&nullCPUValues); err != nil {
		t.Fatalf("read C69 nullable CPU verification row: %v", err)
	}
	if nullCPUValues != 1 {
		t.Fatalf("C69 nullable CPU average/max row count = %d, want 1", nullCPUValues)
	}
	for _, tc := range []struct {
		name  string
		query string
	}{
		{name: "negative_cpu_count", query: `update public.monitoring_instance_host_sample_daily_aggregates set cpu_valid_sample_count = -1 where monitoring_instance_id = 'mi_p68_legacy' and bucket_date = '2024-08-13'`},
		{name: "negative_backfilled_count", query: `update public.monitoring_instance_host_sample_daily_aggregates set cpu_valid_backfilled_sample_count = -1 where monitoring_instance_id = 'mi_p68_legacy' and bucket_date = '2024-08-13'`},
		{name: "negative_maintenance_count", query: `update public.monitoring_instance_host_sample_daily_aggregates set cpu_valid_maintenance_sample_count = -1 where monitoring_instance_id = 'mi_p68_legacy' and bucket_date = '2024-08-13'`},
		{name: "cpu_count_exceeds_host_count", query: `update public.monitoring_instance_host_sample_daily_aggregates set cpu_valid_sample_count = 6 where monitoring_instance_id = 'mi_p68_legacy' and bucket_date = '2024-08-13'`},
		{name: "backfilled_count_exceeds_cpu_count", query: `update public.monitoring_instance_host_sample_daily_aggregates set cpu_valid_backfilled_sample_count = 3 where monitoring_instance_id = 'mi_p68_legacy' and bucket_date = '2024-08-13'`},
		{name: "maintenance_count_exceeds_cpu_count", query: `update public.monitoring_instance_host_sample_daily_aggregates set cpu_valid_maintenance_sample_count = 3 where monitoring_instance_id = 'mi_p68_legacy' and bucket_date = '2024-08-13'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.Exec(ctx, tc.query)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
				t.Fatalf("invalid C69 CPU count %s error = %v, want CHECK violation", tc.name, err)
			}
		})
	}
}
