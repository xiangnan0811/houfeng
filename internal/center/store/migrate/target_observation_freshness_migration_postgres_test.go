package migrate

import (
	"context"
	"testing"
	"time"
)

func TestPostgresIntegrationTargetFreshnessUpgrade(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	db := openTemporaryPostgresDatabase(t, ctx)

	applyPostgresMigrationsThrough(t, ctx, db, "0071_add_access_management.sql")
	oldSuccess := time.Date(2025, time.January, 2, 3, 4, 5, 0, time.UTC)
	oldFailure := time.Date(2025, time.January, 3, 4, 5, 6, 0, time.UTC)
	liveObserved := time.Date(2025, time.January, 4, 5, 6, 7, 0, time.UTC)
	liveReceived := liveObserved.Add(2 * time.Second)
	futureObserved := liveObserved.Add(5 * time.Minute)
	futureReceived := liveObserved.Add(3 * time.Second)
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin C71 freshness fixture seed: %v", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	if _, err := tx.Exec(ctx, `
		insert into public.vps_assets (vps_id, display_name, lifecycle_status)
		values ('vps_freshness_upgrade', 'Freshness upgrade VPS', 'active')
	`); err != nil {
		t.Fatalf("seed C71 freshness VPS: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into public.monitoring_instances (
			monitoring_instance_id, display_name, region, city, provider,
			lifecycle_status, monitoring_status, binding_status, vps_id
		) values ('mi_freshness_upgrade', 'Freshness upgrade fixture', 'HK', 'Hong Kong', 'test', '已接入', '启用', '未绑定', 'vps_freshness_upgrade')
	`); err != nil {
		t.Fatalf("seed C71 freshness monitoring instance: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into public.targets (
			target_id, name, target_type, host, run_status, lifecycle_status,
			current_health_status, last_success_at, last_failure_at, created_at, updated_at
		) values (
			'target_freshness_upgrade', 'Freshness upgrade target', 'service', 'example.invalid', '启用', 'active',
			'normal', $1, $2, '2024-01-01 00:00:00+00'::timestamptz, '2024-01-01 00:00:00+00'::timestamptz
		)
	`, oldSuccess, oldFailure); err != nil {
		t.Fatalf("seed C71 freshness target: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into public.probe_items (
			probe_item_id, target_id, probe_kind, enabled, frequency_tier, timeout_seconds,
			config, created_at, updated_at
		) values
			('probe_freshness_live', 'target_freshness_upgrade', 'http', true, '1m', 5, '{}'::jsonb, '2024-01-01 00:00:00+00'::timestamptz, '2024-01-01 00:00:00+00'::timestamptz),
			('probe_freshness_excluded', 'target_freshness_upgrade', 'tls', true, '6h', 5, '{}'::jsonb, '2024-01-01 00:00:00+00'::timestamptz, '2024-01-01 00:00:00+00'::timestamptz),
			('probe_freshness_disabled', 'target_freshness_upgrade', 'tcp', false, '5s', 3, '{}'::jsonb, '2024-01-01 00:00:00+00'::timestamptz, '2024-01-01 00:00:00+00'::timestamptz)
	`); err != nil {
		t.Fatalf("seed C71 freshness probe items: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		insert into public.probe_observations (
			monitoring_instance_id, target_id, probe_item_id, observed_at, received_at,
			result_kind, maintenance_context, is_backfilled, sync_batch_id
		) values
			('mi_freshness_upgrade', 'target_freshness_upgrade', 'probe_freshness_live', $1, $2, 'success', false, false, 'freshness-live-1'),
			('mi_freshness_upgrade', 'target_freshness_upgrade', 'probe_freshness_live', $3, $4, 'failure', false, false, 'freshness-live-2'),
			('mi_freshness_upgrade', 'target_freshness_upgrade', 'probe_freshness_excluded', $3, $3 + interval '1 second', 'success', true, false, 'freshness-maintenance'),
			('mi_freshness_upgrade', 'target_freshness_upgrade', 'probe_freshness_excluded', $3, $3 + interval '2 seconds', 'failure', false, true, 'freshness-backfill')
	`, liveObserved, liveReceived, futureObserved, futureReceived); err != nil {
		t.Fatalf("seed C71 freshness observations: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit C71 freshness fixture seed: %v", err)
	}

	applyPostgresMigrationsThrough(t, ctx, db, "0072_add_target_observation_freshness.sql")

	var targetReset, liveReset, excludedReset, disabledReset time.Time
	if err := db.QueryRow(ctx, `
		select freshness_reset_at
		from public.targets
		where target_id = 'target_freshness_upgrade'
	`).Scan(&targetReset); err != nil {
		t.Fatalf("read target freshness reset: %v", err)
	}
	if err := db.QueryRow(ctx, `
		select
			max(freshness_reset_at) filter (where probe_item_id = 'probe_freshness_live'),
			max(freshness_reset_at) filter (where probe_item_id = 'probe_freshness_excluded'),
			max(freshness_reset_at) filter (where probe_item_id = 'probe_freshness_disabled')
		from public.probe_items
	`).Scan(&liveReset, &excludedReset, &disabledReset); err != nil {
		t.Fatalf("read probe freshness resets: %v", err)
	}
	if targetReset.IsZero() || liveReset.IsZero() || excludedReset.IsZero() || disabledReset.IsZero() {
		t.Fatalf("freshness reset defaults = target %s/live %s/excluded %s/disabled %s, want all nonzero", targetReset, liveReset, excludedReset, disabledReset)
	}
	if !targetReset.Equal(liveReset) || !targetReset.Equal(excludedReset) || !targetReset.Equal(disabledReset) {
		t.Fatalf("freshness reset defaults were not one migration transaction timestamp: target=%s live=%s excluded=%s disabled=%s", targetReset, liveReset, excludedReset, disabledReset)
	}

	var gotSuccess, gotFailure time.Time
	if err := db.QueryRow(ctx, `
		select last_success_at, last_failure_at
		from public.targets
		where target_id = 'target_freshness_upgrade'
	`).Scan(&gotSuccess, &gotFailure); err != nil {
		t.Fatalf("read preserved target health timestamps: %v", err)
	}
	if !gotSuccess.Equal(oldSuccess) || !gotFailure.Equal(oldFailure) {
		t.Fatalf("target health timestamps = %s/%s, want preserved %s/%s", gotSuccess, gotFailure, oldSuccess, oldFailure)
	}

	var gotLive *time.Time
	if err := db.QueryRow(ctx, `
		select last_live_observed_at
		from public.probe_items
		where probe_item_id = 'probe_freshness_live'
	`).Scan(&gotLive); err != nil {
		t.Fatalf("read live observation projection: %v", err)
	}
	if gotLive == nil || !gotLive.Equal(futureReceived) {
		t.Fatalf("live observation projection = %v, want effective min(observed,received) %s", gotLive, futureReceived)
	}
	var excludedLive *time.Time
	if err := db.QueryRow(ctx, `
		select last_live_observed_at
		from public.probe_items
		where probe_item_id = 'probe_freshness_excluded'
	`).Scan(&excludedLive); err != nil {
		t.Fatalf("read excluded observation projection: %v", err)
	}
	if excludedLive != nil {
		t.Fatalf("maintenance/backfill observation projection = %s, want null", excludedLive)
	}
	var disabledLive *time.Time
	if err := db.QueryRow(ctx, `
		select last_live_observed_at
		from public.probe_items
		where probe_item_id = 'probe_freshness_disabled'
	`).Scan(&disabledLive); err != nil {
		t.Fatalf("read no-observation projection: %v", err)
	}
	if disabledLive != nil {
		t.Fatalf("probe without observations projection = %s, want null", disabledLive)
	}

	var targetDefault, probeDefault string
	if err := db.QueryRow(ctx, `
		select
			(select column_default from information_schema.columns where table_schema = 'public' and table_name = 'targets' and column_name = 'freshness_reset_at'),
			(select column_default from information_schema.columns where table_schema = 'public' and table_name = 'probe_items' and column_name = 'freshness_reset_at')
	`).Scan(&targetDefault, &probeDefault); err != nil {
		t.Fatalf("read freshness reset defaults: %v", err)
	}
	if targetDefault == "" || probeDefault == "" {
		t.Fatalf("freshness reset defaults = %q/%q, want nonempty now() defaults", targetDefault, probeDefault)
	}

	newCreatedAt := time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
	if _, err := db.Exec(ctx, `
		insert into public.targets (target_id, name, target_type, host, run_status, lifecycle_status, created_at, updated_at)
		values ('target_freshness_new', 'New freshness target', 'service', 'new.invalid', '启用', 'active', $1, $1)
	`, newCreatedAt); err != nil {
		t.Fatalf("insert post-migration target: %v", err)
	}
	if _, err := db.Exec(ctx, `
		insert into public.probe_items (probe_item_id, target_id, probe_kind, frequency_tier, timeout_seconds, created_at, updated_at)
		values ('probe_freshness_new', 'target_freshness_new', 'http', '1m', 5, $1, $1)
	`, newCreatedAt); err != nil {
		t.Fatalf("insert post-migration freshness rows: %v", err)
	}
	var newTargetReset, newProbeReset time.Time
	if err := db.QueryRow(ctx, `
		select targets.freshness_reset_at, probe_items.freshness_reset_at
		from public.targets
		join public.probe_items using (target_id)
		where targets.target_id = 'target_freshness_new'
	`).Scan(&newTargetReset, &newProbeReset); err != nil {
		t.Fatalf("read post-migration freshness defaults: %v", err)
	}
	if !newTargetReset.After(newCreatedAt) || !newProbeReset.After(newCreatedAt) {
		t.Fatalf("post-migration freshness defaults = %s/%s, want after creation time %s", newTargetReset, newProbeReset, newCreatedAt)
	}
}
