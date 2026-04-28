# Houfeng Retention and Aggregation Execution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make V1 retention settings executable by adding a center retention worker, SQL-first cleanup, and minimal daily aggregate storage for later trend/detail phases.

**Architecture:** Add two narrow daily aggregate tables via raw SQL migration, introduce `internal/center/retention` as the domain worker/policy boundary, and implement PostgreSQL retention passes in `internal/center/store`. Wire the worker into center bootstrap beside the existing incident worker and update Settings copy to describe executed retention without claiming trend UI is complete.

**Tech Stack:** Go, pgx, raw SQL migrations, existing center worker orchestration, React/Vite/Testing Library, Vitest

---

## Scope

This plan implements Phase 3 from `docs/superpowers/specs/2026-04-28-houfeng-retention-aggregation-execution-design.md`.

In scope:

- daily aggregate schema for Node host samples and Target probe observations;
- retention domain policy/result types and background worker;
- PostgreSQL retention pass with aggregate upserts and cleanup deletes;
- center bootstrap wiring for the retention worker;
- truthful Settings retention copy;
- focused and full verification.

Out of scope:

- trend degradation rules;
- trend charts or detail trend UI;
- dashboard abnormal summaries;
- advanced Events filters;
- PostgreSQL partitioning;
- new runtime configuration;
- new dependencies.

## Planned file structure

### Migration

- Create: `db/migrations/0008_add_retention_aggregates.sql`
  - Adds `node_host_sample_daily_aggregates`.
  - Adds `target_probe_daily_aggregates`.
- Modify: `internal/center/store/migrate/migrate_test.go`
  - Locks migration order includes `0008_add_retention_aggregates.sql`.

### Retention domain worker

- Create: `internal/center/retention/types.go`
  - `Policy`, `Result`, `Repository`, `SettingsRepository`, `Worker` option types.
- Create: `internal/center/retention/worker.go`
  - Converts settings to policy and runs retention pass on startup and interval.
- Create: `internal/center/retention/worker_test.go`
  - Locks startup pass, continuing after failure, cancellation, and latest-settings behavior.

### PostgreSQL retention repository

- Create: `internal/center/store/retention.go`
  - `PostgresRetentionRepository` with transaction-bound aggregate upserts and cleanup deletes.
- Create: `internal/center/store/retention_test.go`
  - SQL-shape and transaction tests with fake tx.

### Bootstrap wiring

- Modify: `cmd/houfeng-center/bootstrap.go`
  - Constructs retention repository and worker.
  - Passes incident worker and retention worker to `centerapp.New`.
  - Changes `bootstrapDeps.newApp` to variadic workers.
- Modify: `cmd/houfeng-center/bootstrap_test.go`
  - Locks bootstrap receives two workers.
- Modify: `internal/center/app/app_test.go`
  - Adds multi-worker shutdown evidence.

### Settings copy

- Modify: `web/src/pages/SettingsPage.tsx`
  - Replace stored-only retention copy with worker-backed copy.
- Modify: `web/src/pages/SettingsPage.test.tsx`
  - Update copy expectation; keep serialization tests unchanged.

---

## Task 1: Add daily aggregate migration

**Files:**
- Create: `db/migrations/0008_add_retention_aggregates.sql`
- Modify: `internal/center/store/migrate/migrate_test.go`

- [ ] **Step 1: Write the failing migration-order test**

In `internal/center/store/migrate/migrate_test.go`, update `TestNamesIncludesBaselineAndFollowupMigrations`:

```go
if len(names) < 9 {
	t.Fatalf("len(Names()) = %d, want at least 9", len(names))
}
// keep existing indexes 0..7 unchanged
if names[8] != "0008_add_retention_aggregates.sql" {
	t.Fatalf("ninth migration = %q, want %q", names[8], "0008_add_retention_aggregates.sql")
}
```

- [ ] **Step 2: Run migration test and confirm failure**

Run:

```bash
go test ./internal/center/store/migrate -run TestNamesIncludesBaselineAndFollowupMigrations -v
```

Expected: FAIL because `0008_add_retention_aggregates.sql` does not exist.

- [ ] **Step 3: Add aggregate migration**

Create `db/migrations/0008_add_retention_aggregates.sql` with exactly:

```sql
create table if not exists node_host_sample_daily_aggregates (
  node_id text not null references nodes(node_id) on delete cascade,
  bucket_date date not null,
  sample_count integer not null,
  avg_cpu_usage_pct double precision not null,
  max_cpu_usage_pct double precision not null,
  avg_load_5 double precision not null,
  max_load_5 double precision not null,
  avg_mem_used_pct double precision not null,
  max_mem_used_pct double precision not null,
  avg_cpu_iowait_pct double precision not null,
  max_cpu_iowait_pct double precision not null,
  avg_cpu_steal_pct double precision not null,
  max_cpu_steal_pct double precision not null,
  avg_disk_busy_pct double precision not null,
  max_disk_busy_pct double precision not null,
  backfilled_sample_count integer not null,
  maintenance_sample_count integer not null,
  updated_at timestamptz not null default now(),
  primary key (node_id, bucket_date)
);

create index if not exists idx_node_host_sample_daily_aggregates_bucket
  on node_host_sample_daily_aggregates (bucket_date desc);

create table if not exists target_probe_daily_aggregates (
  target_id text not null references targets(target_id) on delete cascade,
  probe_item_id text not null references probe_items(probe_item_id) on delete cascade,
  bucket_date date not null,
  observation_count integer not null,
  success_count integer not null,
  failure_count integer not null,
  avg_latency_ms double precision,
  p95_latency_ms double precision,
  min_tls_expiry_days integer,
  backfilled_observation_count integer not null,
  maintenance_observation_count integer not null,
  updated_at timestamptz not null default now(),
  primary key (target_id, probe_item_id, bucket_date)
);

create index if not exists idx_target_probe_daily_aggregates_bucket
  on target_probe_daily_aggregates (bucket_date desc);
```

- [ ] **Step 4: Run migration tests**

Run:

```bash
go test ./internal/center/store/migrate -v
```

Expected: PASS.

- [ ] **Step 5: Commit migration**

Run:

```bash
git add db/migrations/0008_add_retention_aggregates.sql internal/center/store/migrate/migrate_test.go
git commit -m "Add retention aggregate schema" -m "Phase 3 needs durable daily aggregate buckets before raw observations expire. The schema keeps Node and Target aggregates narrow and SQL-first for later V1 trend work.\n\nConstraint: No partitioning or external time-series dependency in V1\nConfidence: high\nScope-risk: narrow\nTested: go test ./internal/center/store/migrate -v"
```

---

## Task 2: Add retention domain worker

**Files:**
- Create: `internal/center/retention/types.go`
- Create: `internal/center/retention/worker.go`
- Create: `internal/center/retention/worker_test.go`

- [ ] **Step 1: Write failing worker tests**

Create `internal/center/retention/worker_test.go` with:

```go
package retention

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	centersettings "houfeng/internal/center/settings"
)

type fakeRepository struct {
	results []Result
	errs    []error
	calls   []Policy
	nows    []time.Time
}

func (f *fakeRepository) ApplyRetention(_ context.Context, policy Policy, now time.Time) (Result, error) {
	f.calls = append(f.calls, policy)
	f.nows = append(f.nows, now)
	idx := len(f.calls) - 1
	if idx < len(f.errs) && f.errs[idx] != nil {
		return Result{}, f.errs[idx]
	}
	if idx < len(f.results) {
		return f.results[idx], nil
	}
	return Result{}, nil
}

type fakeSettingsRepository struct {
	records []centersettings.CenterSettings
	errs    []error
	calls   int
}

func (f *fakeSettingsRepository) GetSettings(context.Context) (centersettings.CenterSettings, error) {
	idx := f.calls
	f.calls++
	if idx < len(f.errs) && f.errs[idx] != nil {
		return centersettings.CenterSettings{}, f.errs[idx]
	}
	if idx < len(f.records) {
		return f.records[idx], nil
	}
	return centersettings.Default(), nil
}

func TestWorkerRunsRetentionPassOnStartup(t *testing.T) {
	repo := &fakeRepository{}
	settingsRepo := &fakeSettingsRepository{records: []centersettings.CenterSettings{settingsWithRetention(3, 30, 90, 180)}}
	worker := NewWorker(repo, settingsRepo, slog.Default(), time.Hour)
	now := time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC)
	worker.now = func() time.Time { return now }

	ctx, cancel := context.WithCancel(context.Background())
	worker.afterPass = cancel
	if err := worker.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(repo.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(repo.calls))
	}
	if repo.calls[0].RawLayerDays != 3 || repo.calls[0].AggregateLayerDays != 30 || repo.calls[0].EventLayerDays != 90 || repo.calls[0].NotificationLayerDays != 180 {
		t.Fatalf("policy = %#v, want settings retention policy", repo.calls[0])
	}
	if !repo.nows[0].Equal(now) {
		t.Fatalf("now = %s, want %s", repo.nows[0], now)
	}
}

func TestWorkerContinuesAfterRepositoryFailureAndReloadsSettings(t *testing.T) {
	repo := &fakeRepository{errs: []error{errors.New("retention boom")}}
	settingsRepo := &fakeSettingsRepository{records: []centersettings.CenterSettings{
		settingsWithRetention(7, 30, 90, 180),
		settingsWithRetention(14, 60, 120, 240),
	}}
	worker := NewWorker(repo, settingsRepo, slog.Default(), time.Millisecond)
	calls := 0
	worker.now = func() time.Time {
		calls++
		return time.Date(2026, time.April, 28, 12, 0, calls, 0, time.UTC)
	}
	ctx, cancel := context.WithCancel(context.Background())
	worker.afterPass = func() {
		if len(repo.calls) == 2 {
			cancel()
		}
	}

	if err := worker.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(repo.calls) != 2 {
		t.Fatalf("len(calls) = %d, want 2", len(repo.calls))
	}
	if repo.calls[0].RawLayerDays != 7 || repo.calls[1].RawLayerDays != 14 {
		t.Fatalf("policies = %#v, want latest settings each pass", repo.calls)
	}
}

func TestWorkerStopsOnContextCancellationBeforeFirstPass(t *testing.T) {
	repo := &fakeRepository{}
	worker := NewWorker(repo, &fakeSettingsRepository{}, slog.Default(), time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := worker.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(repo.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0", len(repo.calls))
	}
}

func settingsWithRetention(raw, aggregate, event, notification int) centersettings.CenterSettings {
	record := centersettings.Default()
	record.RetentionPolicy = centersettings.RetentionPolicy{
		RawLayerDays:          raw,
		AggregateLayerDays:    aggregate,
		EventLayerDays:        event,
		NotificationLayerDays: notification,
	}
	return record
}
```

- [ ] **Step 2: Run worker tests and confirm failure**

Run:

```bash
go test ./internal/center/retention -v
```

Expected: FAIL because the package/types do not exist.

- [ ] **Step 3: Implement retention types**

Create `internal/center/retention/types.go`:

```go
package retention

import (
	"context"
	"time"

	centersettings "houfeng/internal/center/settings"
)

const DefaultWorkerInterval = time.Hour

type Policy struct {
	RawLayerDays          int
	AggregateLayerDays    int
	EventLayerDays        int
	NotificationLayerDays int
}

type Result struct {
	NodeAggregateRows        int64
	TargetAggregateRows      int64
	DeletedHeartbeats        int64
	DeletedHostSamples       int64
	DeletedProbeObservations int64
	DeletedNodeAggregates    int64
	DeletedTargetAggregates  int64
	DeletedEvents            int64
	DeletedNotifications     int64
}

type Repository interface {
	ApplyRetention(context.Context, Policy, time.Time) (Result, error)
}

type SettingsRepository interface {
	GetSettings(context.Context) (centersettings.CenterSettings, error)
}

func PolicyFromSettings(record centersettings.CenterSettings) (Policy, error) {
	validated, err := centersettings.Validate(record)
	if err != nil {
		return Policy{}, err
	}
	return Policy{
		RawLayerDays:          validated.RetentionPolicy.RawLayerDays,
		AggregateLayerDays:    validated.RetentionPolicy.AggregateLayerDays,
		EventLayerDays:        validated.RetentionPolicy.EventLayerDays,
		NotificationLayerDays: validated.RetentionPolicy.NotificationLayerDays,
	}, nil
}
```

- [ ] **Step 4: Implement worker**

Create `internal/center/retention/worker.go`:

```go
package retention

import (
	"context"
	"log/slog"
	"time"
)

type Worker struct {
	repo         Repository
	settingsRepo SettingsRepository
	logger       *slog.Logger
	interval     time.Duration
	now          func() time.Time
	afterPass    func()
}

func NewWorker(repo Repository, settingsRepo SettingsRepository, logger *slog.Logger, interval time.Duration) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = DefaultWorkerInterval
	}
	return &Worker{
		repo:         repo,
		settingsRepo: settingsRepo,
		logger:       logger,
		interval:     interval,
		now:          func() time.Time { return time.Now().UTC() },
	}
}

func (w *Worker) Run(ctx context.Context) error {
	if w.repo == nil || w.settingsRepo == nil {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		w.runOnce(ctx)
		if w.afterPass != nil {
			w.afterPass()
		}

		timer := time.NewTimer(w.interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil
		case <-timer.C:
		}
	}
}

func (w *Worker) runOnce(ctx context.Context) {
	record, err := w.settingsRepo.GetSettings(ctx)
	if err != nil {
		w.logger.Error("load retention settings failed", "error", err)
		return
	}
	policy, err := PolicyFromSettings(record)
	if err != nil {
		w.logger.Error("validate retention settings failed", "error", err)
		return
	}
	result, err := w.repo.ApplyRetention(ctx, policy, w.now().UTC())
	if err != nil {
		w.logger.Error("apply retention failed", "error", err)
		return
	}
	w.logger.Info(
		"retention pass completed",
		"node_aggregate_rows", result.NodeAggregateRows,
		"target_aggregate_rows", result.TargetAggregateRows,
		"deleted_heartbeats", result.DeletedHeartbeats,
		"deleted_host_samples", result.DeletedHostSamples,
		"deleted_probe_observations", result.DeletedProbeObservations,
		"deleted_events", result.DeletedEvents,
		"deleted_notifications", result.DeletedNotifications,
	)
}
```

- [ ] **Step 5: Run worker tests**

Run:

```bash
go test ./internal/center/retention -v
```

Expected: PASS.

- [ ] **Step 6: Commit retention worker**

Run:

```bash
git add internal/center/retention/types.go internal/center/retention/worker.go internal/center/retention/worker_test.go
git commit -m "Add retention worker boundary" -m "Retention execution needs a narrow background boundary that reloads persisted settings each pass and retries after transient failures without stopping the center.\n\nConstraint: Center remains a single Go process in V1\nConfidence: high\nScope-risk: narrow\nTested: go test ./internal/center/retention -v"
```

---

## Task 3: Implement PostgreSQL retention repository

**Files:**
- Create: `internal/center/store/retention.go`
- Create: `internal/center/store/retention_test.go`

- [ ] **Step 1: Write failing store tests**

Create `internal/center/store/retention_test.go` with fake tx coverage:

```go
package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/retention"
)

func TestPostgresRetentionRepositoryAppliesAggregatesAndCleanupInTransaction(t *testing.T) {
	t.Parallel()
	tx := &fakeRetentionTx{}
	repo := &PostgresRetentionRepository{beginTx: func(context.Context, pgx.TxOptions) (retentionTx, error) { return tx, nil }}
	now := time.Date(2026, time.April, 28, 12, 30, 0, 0, time.UTC)

	result, err := repo.ApplyRetention(context.Background(), retention.Policy{RawLayerDays: 7, AggregateLayerDays: 30, EventLayerDays: 90, NotificationLayerDays: 180}, now)
	if err != nil {
		t.Fatalf("ApplyRetention() error = %v", err)
	}
	for _, want := range []string{
		"insert into node_host_sample_daily_aggregates",
		"insert into target_probe_daily_aggregates",
		"delete from node_heartbeats",
		"delete from host_samples",
		"delete from probe_observations",
		"delete from node_host_sample_daily_aggregates",
		"delete from target_probe_daily_aggregates",
		"delete from state_change_events",
		"delete from notification_records",
	} {
		if !containsSQL(tx.execSQL, want) {
			t.Fatalf("execSQL = %#v, want %q", tx.execSQL, want)
		}
	}
	if containsSQL(tx.execSQL, "delete from active_incidents") {
		t.Fatalf("execSQL = %#v, must not delete active_incidents", tx.execSQL)
	}
	if tx.commitCalls != 1 || tx.rollbackCalls == 0 {
		t.Fatalf("commitCalls=%d rollbackCalls=%d, want commit and deferred rollback", tx.commitCalls, tx.rollbackCalls)
	}
	if result.NodeAggregateRows != 1 || result.TargetAggregateRows != 1 || result.DeletedHeartbeats != 1 || result.DeletedNotifications != 1 {
		t.Fatalf("result = %#v, want command-tag counts", result)
	}
}

func TestPostgresRetentionRepositoryUsesExpectedCutoffs(t *testing.T) {
	t.Parallel()
	tx := &fakeRetentionTx{}
	repo := &PostgresRetentionRepository{beginTx: func(context.Context, pgx.TxOptions) (retentionTx, error) { return tx, nil }}
	now := time.Date(2026, time.April, 28, 12, 30, 0, 0, time.UTC)

	_, err := repo.ApplyRetention(context.Background(), retention.Policy{RawLayerDays: 7, AggregateLayerDays: 30, EventLayerDays: 90, NotificationLayerDays: 180}, now)
	if err != nil {
		t.Fatalf("ApplyRetention() error = %v", err)
	}
	if got := tx.argsForSQL("insert into node_host_sample_daily_aggregates")[0].(time.Time); !got.Equal(time.Date(2026, time.April, 28, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("aggregate stable cutoff = %s, want start of current UTC day", got)
	}
	if got := tx.argsForSQL("delete from node_heartbeats")[0].(time.Time); !got.Equal(now.AddDate(0, 0, -7)) {
		t.Fatalf("raw cutoff = %s, want %s", got, now.AddDate(0, 0, -7))
	}
	if got := tx.argsForSQL("delete from state_change_events")[0].(time.Time); !got.Equal(now.AddDate(0, 0, -90)) {
		t.Fatalf("event cutoff = %s, want %s", got, now.AddDate(0, 0, -90))
	}
}

func TestPostgresRetentionRepositoryRollsBackOnFailure(t *testing.T) {
	t.Parallel()
	tx := &fakeRetentionTx{execErrForSQLSubstring: "delete from host_samples", execErr: errors.New("delete boom")}
	repo := &PostgresRetentionRepository{beginTx: func(context.Context, pgx.TxOptions) (retentionTx, error) { return tx, nil }}

	_, err := repo.ApplyRetention(context.Background(), retention.Policy{RawLayerDays: 7, AggregateLayerDays: 30, EventLayerDays: 90, NotificationLayerDays: 180}, time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC))
	if err == nil || !strings.Contains(err.Error(), "delete expired host samples") {
		t.Fatalf("ApplyRetention() error = %v, want host sample context", err)
	}
	if tx.commitCalls != 0 || tx.rollbackCalls == 0 {
		t.Fatalf("commitCalls=%d rollbackCalls=%d, want rollback without commit", tx.commitCalls, tx.rollbackCalls)
	}
}

type fakeRetentionTx struct {
	execSQL                []string
	execArgs               [][]any
	execErrForSQLSubstring string
	execErr                error
	commitCalls            int
	rollbackCalls          int
}

func (f *fakeRetentionTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.execSQL = append(f.execSQL, sql)
	f.execArgs = append(f.execArgs, append([]any(nil), args...))
	if f.execErr != nil && strings.Contains(sql, f.execErrForSQLSubstring) {
		return pgconn.CommandTag{}, f.execErr
	}
	return pgconn.NewCommandTag("DELETE 1"), nil
}

func (f *fakeRetentionTx) Commit(context.Context) error { f.commitCalls++; return nil }
func (f *fakeRetentionTx) Rollback(context.Context) error { f.rollbackCalls++; return nil }

func (f *fakeRetentionTx) argsForSQL(substring string) []any {
	for i, sql := range f.execSQL {
		if strings.Contains(sql, substring) {
			return f.execArgs[i]
		}
	}
	return nil
}
```

- [ ] **Step 2: Run store tests and confirm failure**

Run:

```bash
go test ./internal/center/store -run TestPostgresRetentionRepository -v
```

Expected: FAIL because `PostgresRetentionRepository` does not exist.

- [ ] **Step 3: Implement retention repository**

Create `internal/center/store/retention.go` with:

```go
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/retention"
)

type retentionTx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Commit(context.Context) error
	Rollback(context.Context) error
}

type PostgresRetentionRepository struct {
	beginTx func(context.Context, pgx.TxOptions) (retentionTx, error)
}

func NewPostgresRetentionRepository(db *pgxpool.Pool) *PostgresRetentionRepository {
	return &PostgresRetentionRepository{beginTx: func(ctx context.Context, opts pgx.TxOptions) (retentionTx, error) { return db.BeginTx(ctx, opts) }}
}

var _ retention.Repository = (*PostgresRetentionRepository)(nil)

func (r *PostgresRetentionRepository) ApplyRetention(ctx context.Context, policy retention.Policy, now time.Time) (retention.Result, error) {
	tx, err := r.beginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return retention.Result{}, fmt.Errorf("begin retention transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	stableBefore := startOfUTCDay(now)
	rawCutoff := now.UTC().AddDate(0, 0, -policy.RawLayerDays)
	aggregateCutoff := startOfUTCDay(now.UTC().AddDate(0, 0, -policy.AggregateLayerDays))
	eventCutoff := now.UTC().AddDate(0, 0, -policy.EventLayerDays)
	notificationCutoff := now.UTC().AddDate(0, 0, -policy.NotificationLayerDays)

	var result retention.Result
	if result.NodeAggregateRows, err = execRows(ctx, tx, upsertNodeHostDailyAggregatesSQL, "upsert node host daily aggregates", stableBefore); err != nil { return retention.Result{}, err }
	if result.TargetAggregateRows, err = execRows(ctx, tx, upsertTargetProbeDailyAggregatesSQL, "upsert target probe daily aggregates", stableBefore); err != nil { return retention.Result{}, err }
	if result.DeletedHeartbeats, err = execRows(ctx, tx, deleteExpiredHeartbeatsSQL, "delete expired heartbeats", rawCutoff); err != nil { return retention.Result{}, err }
	if result.DeletedHostSamples, err = execRows(ctx, tx, deleteExpiredHostSamplesSQL, "delete expired host samples", rawCutoff); err != nil { return retention.Result{}, err }
	if result.DeletedProbeObservations, err = execRows(ctx, tx, deleteExpiredProbeObservationsSQL, "delete expired probe observations", rawCutoff); err != nil { return retention.Result{}, err }
	if result.DeletedNodeAggregates, err = execRows(ctx, tx, deleteExpiredNodeAggregatesSQL, "delete expired node aggregates", aggregateCutoff); err != nil { return retention.Result{}, err }
	if result.DeletedTargetAggregates, err = execRows(ctx, tx, deleteExpiredTargetAggregatesSQL, "delete expired target aggregates", aggregateCutoff); err != nil { return retention.Result{}, err }
	if result.DeletedEvents, err = execRows(ctx, tx, deleteExpiredEventsSQL, "delete expired events", eventCutoff); err != nil { return retention.Result{}, err }
	if result.DeletedNotifications, err = execRows(ctx, tx, deleteExpiredNotificationsSQL, "delete expired notifications", notificationCutoff); err != nil { return retention.Result{}, err }

	if err := tx.Commit(ctx); err != nil {
		return retention.Result{}, fmt.Errorf("commit retention transaction: %w", err)
	}
	return result, nil
}

func execRows(ctx context.Context, tx retentionTx, sql, label string, args ...any) (int64, error) {
	tag, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", label, err)
	}
	return tag.RowsAffected(), nil
}

func startOfUTCDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
```

Add SQL constants in the same file:

```go
const upsertNodeHostDailyAggregatesSQL = `
	insert into node_host_sample_daily_aggregates (
		node_id, bucket_date, sample_count,
		avg_cpu_usage_pct, max_cpu_usage_pct,
		avg_load_5, max_load_5,
		avg_mem_used_pct, max_mem_used_pct,
		avg_cpu_iowait_pct, max_cpu_iowait_pct,
		avg_cpu_steal_pct, max_cpu_steal_pct,
		avg_disk_busy_pct, max_disk_busy_pct,
		backfilled_sample_count, maintenance_sample_count, updated_at
	)
	select
		node_id,
		(observed_at at time zone 'UTC')::date as bucket_date,
		count(*)::integer,
		avg(cpu_usage_pct), max(cpu_usage_pct),
		avg(load_5), max(load_5),
		avg(mem_used_pct), max(mem_used_pct),
		avg(cpu_iowait_pct), max(cpu_iowait_pct),
		avg(cpu_steal_pct), max(cpu_steal_pct),
		avg(disk_busy_pct), max(disk_busy_pct),
		count(*) filter (where is_backfilled)::integer,
		count(*) filter (where maintenance_context)::integer,
		now()
	from host_samples
	where observed_at < $1
	group by node_id, (observed_at at time zone 'UTC')::date
	on conflict (node_id, bucket_date) do update set
		sample_count = excluded.sample_count,
		avg_cpu_usage_pct = excluded.avg_cpu_usage_pct,
		max_cpu_usage_pct = excluded.max_cpu_usage_pct,
		avg_load_5 = excluded.avg_load_5,
		max_load_5 = excluded.max_load_5,
		avg_mem_used_pct = excluded.avg_mem_used_pct,
		max_mem_used_pct = excluded.max_mem_used_pct,
		avg_cpu_iowait_pct = excluded.avg_cpu_iowait_pct,
		max_cpu_iowait_pct = excluded.max_cpu_iowait_pct,
		avg_cpu_steal_pct = excluded.avg_cpu_steal_pct,
		max_cpu_steal_pct = excluded.max_cpu_steal_pct,
		avg_disk_busy_pct = excluded.avg_disk_busy_pct,
		max_disk_busy_pct = excluded.max_disk_busy_pct,
		backfilled_sample_count = excluded.backfilled_sample_count,
		maintenance_sample_count = excluded.maintenance_sample_count,
		updated_at = now()`

const upsertTargetProbeDailyAggregatesSQL = `
	insert into target_probe_daily_aggregates (
		target_id, probe_item_id, bucket_date,
		observation_count, success_count, failure_count,
		avg_latency_ms, p95_latency_ms, min_tls_expiry_days,
		backfilled_observation_count, maintenance_observation_count, updated_at
	)
	select
		target_id,
		probe_item_id,
		(observed_at at time zone 'UTC')::date as bucket_date,
		count(*)::integer,
		count(*) filter (where result_kind = 'success')::integer,
		count(*) filter (where result_kind = 'failure')::integer,
		avg(latency_ms) filter (where latency_ms is not null),
		percentile_cont(0.95) within group (order by latency_ms) filter (where latency_ms is not null),
		min(tls_expiry_days) filter (where tls_expiry_days is not null),
		count(*) filter (where is_backfilled)::integer,
		count(*) filter (where maintenance_context)::integer,
		now()
	from probe_observations
	where observed_at < $1
	group by target_id, probe_item_id, (observed_at at time zone 'UTC')::date
	on conflict (target_id, probe_item_id, bucket_date) do update set
		observation_count = excluded.observation_count,
		success_count = excluded.success_count,
		failure_count = excluded.failure_count,
		avg_latency_ms = excluded.avg_latency_ms,
		p95_latency_ms = excluded.p95_latency_ms,
		min_tls_expiry_days = excluded.min_tls_expiry_days,
		backfilled_observation_count = excluded.backfilled_observation_count,
		maintenance_observation_count = excluded.maintenance_observation_count,
		updated_at = now()`

const deleteExpiredHeartbeatsSQL = `delete from node_heartbeats where observed_at < $1`
const deleteExpiredHostSamplesSQL = `delete from host_samples where observed_at < $1`
const deleteExpiredProbeObservationsSQL = `delete from probe_observations where observed_at < $1`
const deleteExpiredNodeAggregatesSQL = `delete from node_host_sample_daily_aggregates where bucket_date < $1::date`
const deleteExpiredTargetAggregatesSQL = `delete from target_probe_daily_aggregates where bucket_date < $1::date`
const deleteExpiredEventsSQL = `delete from state_change_events where created_at < $1`
const deleteExpiredNotificationsSQL = `delete from notification_records where created_at < $1`
```

- [ ] **Step 4: Run store tests**

Run:

```bash
go test ./internal/center/store -run TestPostgresRetentionRepository -v
```

Expected: PASS.

- [ ] **Step 5: Commit store repository**

Run:

```bash
git add internal/center/store/retention.go internal/center/store/retention_test.go
git commit -m "Execute retention policy in PostgreSQL" -m "Retention settings now have a SQL-first execution path that upserts daily aggregates before removing expired raw, aggregate, event, and notification history.\n\nConstraint: Current-state tables must not be deleted by retention\nRejected: Live PostgreSQL integration tests | existing store tests use pgx fakes in this repo\nConfidence: high\nScope-risk: moderate\nTested: go test ./internal/center/store -run TestPostgresRetentionRepository -v"
```

---

## Task 4: Wire retention worker into center bootstrap

**Files:**
- Modify: `cmd/houfeng-center/bootstrap.go`
- Modify: `cmd/houfeng-center/bootstrap_test.go`
- Modify: `internal/center/app/app_test.go`

- [ ] **Step 1: Add failing bootstrap/app tests**

In `cmd/houfeng-center/bootstrap_test.go`, change the `newApp` callback type in tests from:

```go
newApp: func(addr string, handler http.Handler, worker centerapp.Worker) appRunner {
```

to:

```go
newApp: func(addr string, handler http.Handler, workers ...centerapp.Worker) appRunner {
```

In `TestBootstrapCenterBuildsAppOnSuccess`, replace the worker assertion with:

```go
if len(workers) != 2 {
	t.Fatalf("len(workers) = %d, want incident and retention workers", len(workers))
}
for i, worker := range workers {
	if worker == nil {
		t.Fatalf("workers[%d] = nil", i)
	}
}
```

In `internal/center/app/app_test.go`, add:

```go
func TestAppWaitsForMultipleWorkerShutdownBeforeReturning(t *testing.T) {
	first := &fakeWorker{exited: make(chan struct{})}
	second := &fakeWorker{exited: make(chan struct{})}
	app := centerapp.New("127.0.0.1:0", http.NewServeMux(), first, second)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- app.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return after context cancellation")
	}

	for name, exited := range map[string]chan struct{}{"first": first.exited, "second": second.exited} {
		select {
		case <-exited:
		case <-time.After(time.Second):
			t.Fatalf("%s worker did not exit before Run() returned", name)
		}
	}
}
```

- [ ] **Step 2: Run focused tests and confirm failure**

Run:

```bash
go test ./cmd/houfeng-center -run TestBootstrapCenterBuildsAppOnSuccess -v
go test ./internal/center/app -run TestAppWaitsForMultipleWorkerShutdownBeforeReturning -v
```

Expected: bootstrap test FAIL because bootstrap only passes one worker; app test may already PASS because `app.New` is variadic.

- [ ] **Step 3: Wire retention worker**

In `cmd/houfeng-center/bootstrap.go`:

1. Add import:

```go
"houfeng/internal/center/retention"
```

2. Change `bootstrapDeps.newApp` type:

```go
newApp func(string, http.Handler, ...centerapp.Worker) appRunner
```

3. After `settingsRepo := store.NewPostgresSettingsRepository(db.Pool())`, add:

```go
retentionRepo := store.NewPostgresRetentionRepository(db.Pool())
```

4. Before `router := deps.newRouter(...)`, add:

```go
retentionWorker := retention.NewWorker(retentionRepo, settingsRepo, slog.Default(), retention.DefaultWorkerInterval)
```

5. Change return line from:

```go
return deps.newApp(cfg.HTTPAddr, router, incidentSvc), db.Close, nil
```

to:

```go
return deps.newApp(cfg.HTTPAddr, router, incidentSvc, retentionWorker), db.Close, nil
```

6. Change default `newApp` assignment:

```go
d.newApp = func(addr string, handler http.Handler, workers ...centerapp.Worker) appRunner {
	return centerapp.New(addr, handler, workers...)
}
```

- [ ] **Step 4: Run bootstrap/app tests**

Run:

```bash
go test ./cmd/houfeng-center -v
go test ./internal/center/app -v
```

Expected: PASS.

- [ ] **Step 5: Commit bootstrap wiring**

Run:

```bash
git add cmd/houfeng-center/bootstrap.go cmd/houfeng-center/bootstrap_test.go internal/center/app/app_test.go
git commit -m "Run retention worker with the center" -m "Retention execution must happen automatically from the single center process. Bootstrap now wires retention beside incident evaluation while preserving multi-worker shutdown behavior.\n\nConstraint: No external scheduler for V1\nConfidence: high\nScope-risk: moderate\nTested: go test ./cmd/houfeng-center -v\nTested: go test ./internal/center/app -v"
```

---

## Task 5: Update Settings retention copy

**Files:**
- Modify: `web/src/pages/SettingsPage.tsx`
- Modify: `web/src/pages/SettingsPage.test.tsx`

- [ ] **Step 1: Add failing SettingsPage copy test**

In `web/src/pages/SettingsPage.test.tsx`, replace the old retention copy assertion:

```ts
expect(
  screen.getByText('当前仅保存保留策略，尚未自动执行清理或聚合任务。'),
).toBeInTheDocument()
```

with:

```ts
expect(
  screen.getByText('中心后台会按这些窗口自动清理原始观测、事件和通知记录，并维护日级聚合数据作为后续趋势与摘要基础。'),
).toBeInTheDocument()
expect(screen.queryByText('当前仅保存保留策略，尚未自动执行清理或聚合任务。')).not.toBeInTheDocument()
```

- [ ] **Step 2: Run SettingsPage test and confirm failure**

Run:

```bash
cd web && npm test -- --run SettingsPage
```

Expected: FAIL because `SettingsPage` still renders old copy.

- [ ] **Step 3: Update SettingsPage copy**

In `web/src/pages/SettingsPage.tsx`, replace:

```tsx
<SectionIntro>当前仅保存保留策略，尚未自动执行清理或聚合任务。</SectionIntro>
```

with:

```tsx
<SectionIntro>中心后台会按这些窗口自动清理原始观测、事件和通知记录，并维护日级聚合数据作为后续趋势与摘要基础。</SectionIntro>
```

- [ ] **Step 4: Run web focused tests and build**

Run:

```bash
cd web && npm test -- --run SettingsPage
cd web && npm run build
```

Expected: PASS.

- [ ] **Step 5: Commit Settings copy**

Run:

```bash
git add web/src/pages/SettingsPage.tsx web/src/pages/SettingsPage.test.tsx
git commit -m "Make retention settings copy truthful" -m "Retention policy is now executed by the center worker, so Settings should no longer describe it as stored-only. The copy stays narrow and does not claim trend UI is complete.\n\nConstraint: Trend surfaces remain Phase 5 scope\nConfidence: high\nScope-risk: narrow\nTested: cd web && npm test -- --run SettingsPage\nTested: cd web && npm run build"
```

---

## Task 6: Phase-level verification and review

**Files:**
- No planned source edits unless verification exposes a regression.

- [ ] **Step 1: Run focused backend suites**

Run:

```bash
go test ./internal/center/retention -v
go test ./internal/center/store/migrate -v
go test ./internal/center/store -run 'TestPostgresRetentionRepository|TestCenterSettingsRepository' -v
go test ./cmd/houfeng-center -v
go test ./internal/center/app -v
```

Expected: PASS.

- [ ] **Step 2: Run focused frontend suites**

Run:

```bash
cd web && npm test -- --run SettingsPage
cd web && npm run build
```

Expected: PASS.

- [ ] **Step 3: Run full Go tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 4: Run full repository verification**

Run:

```bash
./scripts/verify.sh
```

Expected: PASS.

- [ ] **Step 5: Final review**

Dispatch a final code review over the Phase 3 commits. Required checks:

- no current-state table is deleted by retention;
- worker does not stop HTTP serving on a failed pass;
- aggregate upserts happen before raw deletion;
- Settings copy does not claim trend UI or trend rules are complete;
- no new dependency was introduced.

- [ ] **Step 6: Inspect git status**

Run:

```bash
git status --short --branch
```

Expected: clean `main`.

---

## Dependency and parallelization notes

Recommended task order:

1. Task 1 migration and Task 2 worker can run in parallel.
2. Task 3 depends on Task 1 and Task 2 types.
3. Task 4 depends on Task 2 and Task 3.
4. Task 5 can run in parallel with Task 3 after the design is accepted because it touches only web Settings copy.
5. Task 6 must run after all commits land.

Safe `subagent-driven-development` split:

- Subagent A: Task 1 migration only.
- Subagent B: Task 2 retention worker only.
- Subagent C: Task 5 Settings copy only.
- Task 3 starts after Task 1 and Task 2 finish.
- Task 4 starts after Task 3 finishes.
- Task 6 stays in leader session.

## Self-review checklist

- Spec coverage:
  - retention worker: Task 2 and Task 4.
  - raw/event/notification cleanup: Task 3.
  - daily aggregate tables and upserts: Task 1 and Task 3.
  - current-state preservation: Task 3 tests and final review.
  - Settings copy truthfulness: Task 5.
- Non-goals:
  - no trend rules;
  - no trend UI;
  - no partitioning;
  - no new dependencies;
  - no manual admin run UI.
- Verification:
  - focused Go tests;
  - focused web tests/build;
  - full Go tests;
  - full `./scripts/verify.sh`.
