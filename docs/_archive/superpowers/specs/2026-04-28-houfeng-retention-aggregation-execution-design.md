# Houfeng Retention and Aggregation Execution Design

## Context

Houfeng V1 has completed the runtime-semantics and agent-reliability phases from `docs/superpowers/specs/2026-04-28-houfeng-v1-completion-sequencing-design.md`.

The next V1 gap is that retention policy exists in settings and the UI, but it is currently only persisted. `SettingsPage` still states that cleanup and aggregation are not executed. The frozen V1 baseline requires layered data retention:

- raw observations are retained for short/medium-term troubleshooting;
- aggregate data is retained longer for summary and trend-oriented views;
- event and notification history is retained independently;
- backfilled facts remain raw history but must not produce retroactive notification noise.

This design covers Phase 3 only: make retention policy executable and create the smallest aggregation foundation needed by later V1 phases. It does not implement Dashboard abnormal summaries, advanced Events filters, trend degradation rules, trend UI, or visual QA.

## Goals

1. Add a center-side retention worker that periodically applies the persisted retention policy.
2. Delete expired raw observation rows from `node_heartbeats`, `host_samples`, and `probe_observations`.
3. Delete expired historical `state_change_events` and `notification_records` according to their own retention windows.
4. Preserve current truth tables and object state: `nodes`, `targets`, `probe_items`, and `active_incidents` are never deleted by retention.
5. Add minimal daily aggregate tables/read-model writes before raw rows expire, so later V1 trend/detail work has a durable base.
6. Update Settings copy so it accurately states that retention and aggregation are executed by the center worker.

## Non-Goals

This phase must not add:

- PostgreSQL table partitioning;
- TimescaleDB or other external dependencies;
- a generic time-series engine;
- arbitrary user-defined retention classes;
- trend degradation incident rules;
- trend charts or dashboard summary UI;
- per-object retention settings;
- manual retention-run admin UI.

## Recommended Approach

Use a narrow in-process Go worker owned by the center process.

The worker reads `center_settings.retention_policy`, runs a repository method in one bounded pass, logs errors, waits for the next interval, and retries. The center should keep serving HTTP if a single retention pass fails. The worker should stop cleanly with the existing app context.

This is intentionally simpler than cron/systemd timer orchestration and simpler than table partitioning. It matches the current single-binary center architecture and keeps V1 operations small.

## Data Model

### Existing tables retained by policy

Raw layer:

- `node_heartbeats`, time column `observed_at`
- `host_samples`, time column `observed_at`
- `probe_observations`, time column `observed_at`

Event layer:

- `state_change_events`, time column `created_at`

Notification layer:

- `notification_records`, time column `created_at`

Current-state tables excluded from retention deletion:

- `nodes`
- `targets`
- `probe_items`
- `active_incidents`
- `center_settings`

### New aggregate tables

Add daily aggregate tables rather than broad generic rollups.

#### `node_host_sample_daily_aggregates`

Primary key:

- `node_id`
- `bucket_date`

Columns:

- `node_id text not null references nodes(node_id) on delete cascade`
- `bucket_date date not null`
- `sample_count integer not null`
- `avg_cpu_usage_pct double precision not null`
- `max_cpu_usage_pct double precision not null`
- `avg_load_5 double precision not null`
- `max_load_5 double precision not null`
- `avg_mem_used_pct double precision not null`
- `max_mem_used_pct double precision not null`
- `avg_cpu_iowait_pct double precision not null`
- `max_cpu_iowait_pct double precision not null`
- `avg_cpu_steal_pct double precision not null`
- `max_cpu_steal_pct double precision not null`
- `avg_disk_busy_pct double precision not null`
- `max_disk_busy_pct double precision not null`
- `backfilled_sample_count integer not null`
- `maintenance_sample_count integer not null`
- `updated_at timestamptz not null default now()`

Rationale: these fields cover the frozen V1 Node trend direction: load, iowait, steal, memory, CPU, and disk pressure. This phase does not evaluate trends; it only preserves daily facts.

#### `target_probe_daily_aggregates`

Primary key:

- `target_id`
- `probe_item_id`
- `bucket_date`

Columns:

- `target_id text not null references targets(target_id) on delete cascade`
- `probe_item_id text not null references probe_items(probe_item_id) on delete cascade`
- `bucket_date date not null`
- `observation_count integer not null`
- `success_count integer not null`
- `failure_count integer not null`
- `avg_latency_ms double precision`
- `p95_latency_ms double precision`
- `min_tls_expiry_days integer`
- `backfilled_observation_count integer not null`
- `maintenance_observation_count integer not null`
- `updated_at timestamptz not null default now()`

Rationale: these fields cover the frozen V1 Target trend direction: latency degradation, failure ratio, and TLS expiry. This phase stores daily aggregates only.

### Aggregate retention

`aggregate_layer_days` applies to the new aggregate tables using `bucket_date`.

Aggregate rows older than `current_date - aggregate_layer_days` are deleted. This keeps aggregate retention separately tunable from raw retention.

## Retention Pass Semantics

Each pass receives a single `now` value from the worker clock so cutoffs are internally consistent.

The pass must execute in this order:

1. Load settings with `settings.Repository.GetSettings(ctx)`.
2. Validate/normalize the retention policy using existing settings validation.
3. Upsert daily aggregates from raw `host_samples` and `probe_observations` that are old enough to be stable.
4. Delete expired raw rows.
5. Delete expired aggregate rows.
6. Delete expired state-change event rows.
7. Delete expired notification rows.
8. Return counts for observability and tests.

### Stable aggregation window

A daily bucket is eligible for aggregation when:

- its date is strictly before `now.UTC().Truncate(24h)`; and
- it may be affected by raw retention cleanup in the current pass.

The initial V1 implementation may aggregate all historical full-day buckets still present in raw tables on every pass, using `insert ... on conflict do update`. This is acceptable because the data volume is V1-scale and the write is idempotent.

### Backfilled data

Backfilled rows are included in aggregates because they are real historical facts. Aggregate tables also store backfilled counts so later trend logic can choose whether to suppress or annotate those points.

Backfilled data must not trigger retroactive notification sends. That behavior is already covered by Phase 2 incident tests and remains unchanged.

### Maintenance context

Maintenance rows are included in aggregates, and maintenance counts are stored separately. Later trend views can render or suppress maintenance periods without losing historical context.

## Worker Behavior

Add a `retention.Worker` package under `internal/center/retention`.

The worker owns:

- repository dependency;
- settings repository dependency;
- logger;
- interval;
- clock function for tests.

Expected behavior:

- Run one pass shortly after startup rather than waiting a full interval.
- Run again every configured interval.
- Stop when context is canceled.
- Log pass summary with deleted/aggregated counts.
- Log pass errors and continue on next tick.

The worker interval can be fixed for V1, for example one hour, unless tests inject a smaller interval. No new setting is required.

## Repository Interface

Create a small domain API rather than adding retention methods to unrelated repositories.

```go
type Policy struct {
    RawLayerDays          int
    AggregateLayerDays    int
    EventLayerDays        int
    NotificationLayerDays int
}

type Result struct {
    NodeAggregateRows         int64
    TargetAggregateRows       int64
    DeletedHeartbeats         int64
    DeletedHostSamples        int64
    DeletedProbeObservations  int64
    DeletedNodeAggregates     int64
    DeletedTargetAggregates   int64
    DeletedEvents             int64
    DeletedNotifications      int64
}

type Repository interface {
    ApplyRetention(ctx context.Context, policy Policy, now time.Time) (Result, error)
}
```

The PostgreSQL implementation should use raw SQL and existing pgx patterns. It should keep each pass in a transaction so aggregate writes and deletes are internally consistent.

## Settings Copy

Update the Settings retention section from the current “policy is stored only” message to truthful execution copy.

Required meaning:

- retention policy is applied by the center background worker;
- raw rows are cleaned after the raw window;
- aggregate rows support longer-term trend/summary foundations;
- events and notification records follow independent retention windows.

Do not claim that trend degradation or trend charts are complete in this phase.

## Error Handling

- Invalid settings should not normally happen because settings writes are validated. If stored settings fail validation, the worker logs the error and skips that pass.
- Database errors abort the pass transaction and are returned to the worker.
- Worker errors do not stop HTTP serving.
- Context cancellation should stop the worker and return nil or `context.Canceled` in the same style as existing app workers.

## Testing Strategy

### Retention domain/repository tests

Use existing store fake patterns to verify SQL shape and arguments where no live PostgreSQL is available.

Required coverage:

- retention computes raw/event/notification/aggregate cutoffs from one `now`;
- raw cleanup deletes by `observed_at` for the three raw tables;
- event cleanup deletes by `created_at`;
- notification cleanup deletes by `created_at`;
- active incidents are not deleted;
- aggregate upsert SQL groups by UTC day and uses `on conflict do update`;
- aggregate cleanup deletes old aggregate rows by `bucket_date`;
- transaction rollback happens on mid-pass failure.

### Worker tests

Required coverage:

- worker runs one pass on startup;
- worker continues after repository failure;
- worker stops on context cancellation;
- worker uses the latest settings on each pass.

### Bootstrap/app tests

Required coverage:

- center bootstrap wires the retention worker alongside the incident worker;
- existing app worker orchestration still runs multiple workers and stops cleanly.

### Frontend tests

Required coverage:

- Settings page displays the new truthful retention copy;
- existing retention form serialization remains unchanged.

### Phase verification

Run:

```bash
go test ./internal/center/retention -v
go test ./internal/center/store -run 'TestPostgresRetentionRepository' -v
go test ./cmd/houfeng-center -v
go test ./internal/center/app -v
cd web && npm test -- --run SettingsPage
cd web && npm run build
go test ./...
./scripts/verify.sh
```

## Implementation Boundaries

- Do not change user-facing retention input shape.
- Do not add new runtime configuration unless needed for tests.
- Do not delete current-state rows.
- Do not implement trend rules or visual trend surfaces.
- Do not add dependencies.
- Keep SQL-first style consistent with the rest of the store package.

## Acceptance Criteria

Phase 3 is complete when:

1. A center background worker executes retention passes from persisted settings.
2. Raw observation retention is enforced.
3. Event and notification retention are enforced.
4. Minimal daily aggregates are created and retained.
5. Current state tables are preserved.
6. Settings page copy accurately reflects executed retention/aggregation.
7. Focused tests, full Go tests, web tests/build, and repository verification pass.
