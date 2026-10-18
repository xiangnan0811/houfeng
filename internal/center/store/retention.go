package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/retention"
)

type retentionTx interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Commit(context.Context) error
	Rollback(context.Context) error
}

type PostgresRetentionRepository struct {
	beginTx func(context.Context, pgx.TxOptions) (retentionTx, error)
}

func NewPostgresRetentionRepository(db *pgxpool.Pool) *PostgresRetentionRepository {
	return &PostgresRetentionRepository{
		beginTx: func(ctx context.Context, opts pgx.TxOptions) (retentionTx, error) {
			return db.BeginTx(ctx, opts)
		},
	}
}

var _ retention.Repository = (*PostgresRetentionRepository)(nil)

// Waits between whole Repeatable Read attempts; three attempts in total.
var retentionRetryDelays = [...]time.Duration{50 * time.Millisecond, 100 * time.Millisecond}

// ApplyRetention retries only PostgreSQL serialization failures and
// deadlocks. Every attempt is rolled back before waiting, reuses the same
// now/policy, and only the committed attempt's counts are returned.
func (r *PostgresRetentionRepository) ApplyRetention(ctx context.Context, policy retention.Policy, now time.Time) (retention.Result, error) {
	if policy.RawLayerDays < 30 || policy.RawLayerDays > 365 || policy.AggregateLayerDays <= 0 {
		return retention.Result{}, fmt.Errorf("invalid retention policy")
	}
	for attempt := 0; ; attempt++ {
		result, err := r.applyRetentionAttempt(ctx, policy, now)
		if err == nil {
			return result, nil
		}
		var pgErr *pgconn.PgError
		if attempt == len(retentionRetryDelays) || !errors.As(err, &pgErr) || (pgErr.Code != "40001" && pgErr.Code != "40P01") {
			return retention.Result{}, err
		}
		timer := time.NewTimer(retentionRetryDelays[attempt])
		select {
		case <-ctx.Done():
			timer.Stop()
			return retention.Result{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func (r *PostgresRetentionRepository) applyRetentionAttempt(ctx context.Context, policy retention.Policy, now time.Time) (retention.Result, error) {
	tx, err := r.beginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return retention.Result{}, fmt.Errorf("begin retention transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	var locked bool
	if err := tx.QueryRow(ctx, `select pg_try_advisory_xact_lock(hashtextextended('houfeng:retention', 0))`).Scan(&locked); err != nil {
		return retention.Result{}, fmt.Errorf("lock retention transaction: %w", err)
	}
	if !locked {
		return retention.Result{Skipped: true}, nil
	}

	stableBefore := startOfUTCDay(now)
	rawCutoff := now.UTC().AddDate(0, 0, -policy.RawLayerDays)
	aggregateCutoff := startOfUTCDay(now.UTC().AddDate(0, 0, -policy.AggregateLayerDays))

	var result retention.Result
	if result.MonitoringInstanceAggregateRows, err = execRows(ctx, tx, upsertMonitoringInstanceHostDailyAggregatesSQL, "upsert monitoringInstance host daily aggregates", stableBefore, rawCutoff); err != nil {
		return retention.Result{}, err
	}
	if result.TargetAggregateRows, err = execRows(ctx, tx, upsertTargetProbeDailyAggregatesSQL, "upsert target probe daily aggregates", stableBefore, rawCutoff); err != nil {
		return retention.Result{}, err
	}
	if result.DeletedHeartbeats, err = execRows(ctx, tx, deleteExpiredHeartbeatsSQL, "delete expired heartbeats", rawCutoff); err != nil {
		return retention.Result{}, err
	}
	if result.DeletedHostSamples, err = execRows(ctx, tx, deleteExpiredHostSamplesSQL, "delete expired host samples", rawCutoff); err != nil {
		return retention.Result{}, err
	}
	if result.DeletedProbeObservations, err = execRows(ctx, tx, deleteExpiredProbeObservationsSQL, "delete expired probe observations", rawCutoff); err != nil {
		return retention.Result{}, err
	}
	if result.DeletedMonitoringInstanceAggregates, err = execRows(ctx, tx, deleteExpiredMonitoringInstanceAggregatesSQL, "delete expired monitoringInstance aggregates", aggregateCutoff); err != nil {
		return retention.Result{}, err
	}
	if result.DeletedTargetAggregates, err = execRows(ctx, tx, deleteExpiredTargetAggregatesSQL, "delete expired target aggregates", aggregateCutoff); err != nil {
		return retention.Result{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return retention.Result{}, fmt.Errorf("commit retention transaction: %w", err)
	}

	return result, nil
}

// Command output expiry is independent of the committed raw/aggregate pass.
// Read Committed rechecks the current row after waiting for a concurrent writer.
func (r *PostgresRetentionRepository) ClearExpiredCommandOutputs(ctx context.Context, now time.Time) (int64, error) {
	tx, err := r.beginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return 0, fmt.Errorf("begin command output cleanup transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	count, err := execRows(ctx, tx, clearExpiredCommandActionOutputsSQL, "clear expired command action outputs", now.UTC())
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit command output cleanup transaction: %w", err)
	}
	return count, nil
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

// A UTC day becomes immutable before any of its raw rows can be removed.
// Later passes may still see survivors or late backfill for that day; those
// incomplete inputs must never replace its complete pre-cleanup aggregate.
const upsertMonitoringInstanceHostDailyAggregatesSQL = `
	insert into monitoring_instance_host_sample_daily_aggregates (
		monitoring_instance_id, bucket_date, sample_count,
		cpu_valid_sample_count, cpu_valid_backfilled_sample_count, cpu_valid_maintenance_sample_count,
		avg_cpu_usage_pct, max_cpu_usage_pct,
		avg_load_5, max_load_5,
		avg_mem_used_pct, max_mem_used_pct,
		avg_cpu_iowait_pct, max_cpu_iowait_pct,
		avg_cpu_steal_pct, max_cpu_steal_pct,
		avg_disk_busy_pct, max_disk_busy_pct,
		backfilled_sample_count, maintenance_sample_count, updated_at, finalized
	)
	select
		monitoring_instance_id,
		(observed_at at time zone 'UTC')::date as bucket_date,
		count(*)::integer,
		count(*) filter (
			where cpu_rates_valid is not false
				and cpu_usage_pct between 0 and 100
				and cpu_iowait_pct between 0 and 100
				and cpu_steal_pct between 0 and 100
		)::integer,
		count(*) filter (
			where cpu_rates_valid is not false
				and cpu_usage_pct between 0 and 100
				and cpu_iowait_pct between 0 and 100
				and cpu_steal_pct between 0 and 100
				and is_backfilled
		)::integer,
		count(*) filter (
			where cpu_rates_valid is not false
				and cpu_usage_pct between 0 and 100
				and cpu_iowait_pct between 0 and 100
				and cpu_steal_pct between 0 and 100
				and maintenance_context
		)::integer,
		avg(cpu_usage_pct) filter (
			where cpu_rates_valid is not false
				and cpu_usage_pct between 0 and 100
				and cpu_iowait_pct between 0 and 100
				and cpu_steal_pct between 0 and 100
		),
		max(cpu_usage_pct) filter (
			where cpu_rates_valid is not false
				and cpu_usage_pct between 0 and 100
				and cpu_iowait_pct between 0 and 100
				and cpu_steal_pct between 0 and 100
		),
		avg(load_5), max(load_5),
		avg(mem_used_pct), max(mem_used_pct),
		avg(cpu_iowait_pct) filter (
			where cpu_rates_valid is not false
				and cpu_usage_pct between 0 and 100
				and cpu_iowait_pct between 0 and 100
				and cpu_steal_pct between 0 and 100
		),
		max(cpu_iowait_pct) filter (
			where cpu_rates_valid is not false
				and cpu_usage_pct between 0 and 100
				and cpu_iowait_pct between 0 and 100
				and cpu_steal_pct between 0 and 100
		),
		avg(cpu_steal_pct) filter (
			where cpu_rates_valid is not false
				and cpu_usage_pct between 0 and 100
				and cpu_iowait_pct between 0 and 100
				and cpu_steal_pct between 0 and 100
		),
		max(cpu_steal_pct) filter (
			where cpu_rates_valid is not false
				and cpu_usage_pct between 0 and 100
				and cpu_iowait_pct between 0 and 100
				and cpu_steal_pct between 0 and 100
		),
		avg(disk_busy_pct), max(disk_busy_pct),
		count(*) filter (where is_backfilled)::integer,
		count(*) filter (where maintenance_context)::integer,
		now(),
		((observed_at at time zone 'UTC')::date::timestamp at time zone 'UTC') < $2::timestamptz
	from host_samples raw
	where observed_at < $1
		and not exists (
			select 1 from monitoring_instance_host_sample_daily_aggregates finalized_bucket
			where finalized_bucket.monitoring_instance_id = raw.monitoring_instance_id
				and finalized_bucket.bucket_date = (raw.observed_at at time zone 'UTC')::date
				and finalized_bucket.finalized
		)
	group by monitoring_instance_id, (observed_at at time zone 'UTC')::date
	on conflict (monitoring_instance_id, bucket_date) do update set
		sample_count = excluded.sample_count,
		cpu_valid_sample_count = excluded.cpu_valid_sample_count,
		cpu_valid_backfilled_sample_count = excluded.cpu_valid_backfilled_sample_count,
		cpu_valid_maintenance_sample_count = excluded.cpu_valid_maintenance_sample_count,
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
		updated_at = now(),
		finalized = excluded.finalized
	where not monitoring_instance_host_sample_daily_aggregates.finalized`

const upsertTargetProbeDailyAggregatesSQL = `
	insert into target_probe_daily_aggregates (
		target_id, probe_item_id, bucket_date,
		observation_count, success_count, failure_count,
		avg_latency_ms, p95_latency_ms, min_tls_expiry_days,
		backfilled_observation_count, maintenance_observation_count, updated_at, finalized
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
		now(),
		((observed_at at time zone 'UTC')::date::timestamp at time zone 'UTC') < $2::timestamptz
	from probe_observations raw
	where observed_at < $1
		and not exists (
			select 1 from target_probe_daily_aggregates finalized_bucket
			where finalized_bucket.target_id = raw.target_id
				and finalized_bucket.probe_item_id = raw.probe_item_id
				and finalized_bucket.bucket_date = (raw.observed_at at time zone 'UTC')::date
				and finalized_bucket.finalized
		)
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
		updated_at = now(),
		finalized = excluded.finalized
	where not target_probe_daily_aggregates.finalized`

const deleteExpiredHeartbeatsSQL = `delete from monitoring_instance_heartbeats where observed_at < $1`
const deleteExpiredHostSamplesSQL = `delete from host_samples where observed_at < $1`
const deleteExpiredProbeObservationsSQL = `delete from probe_observations where observed_at < $1`
const deleteExpiredMonitoringInstanceAggregatesSQL = `delete from monitoring_instance_host_sample_daily_aggregates where bucket_date < $1::date`
const deleteExpiredTargetAggregatesSQL = `delete from target_probe_daily_aggregates where bucket_date < $1::date`
const clearExpiredCommandActionOutputsSQL = `
	update monitoring_instances
	set last_action = (last_action - 'stdout' - 'stderr') || jsonb_build_object('output_expired', true),
		updated_at = now()
	where last_action->>'status' = 'done'
		and coalesce((last_action->>'output_expired')::boolean, false) = false
		and last_action ? 'output_expires_at'
		and (last_action->>'output_expires_at')::timestamptz <= $1`
