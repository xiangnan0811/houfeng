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
	return &PostgresRetentionRepository{
		beginTx: func(ctx context.Context, opts pgx.TxOptions) (retentionTx, error) {
			return db.BeginTx(ctx, opts)
		},
	}
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
	if result.NodeAggregateRows, err = execRows(ctx, tx, upsertNodeHostDailyAggregatesSQL, "upsert node host daily aggregates", stableBefore); err != nil {
		return retention.Result{}, err
	}
	if result.TargetAggregateRows, err = execRows(ctx, tx, upsertTargetProbeDailyAggregatesSQL, "upsert target probe daily aggregates", stableBefore); err != nil {
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
	if result.DeletedNodeAggregates, err = execRows(ctx, tx, deleteExpiredNodeAggregatesSQL, "delete expired node aggregates", aggregateCutoff); err != nil {
		return retention.Result{}, err
	}
	if result.DeletedTargetAggregates, err = execRows(ctx, tx, deleteExpiredTargetAggregatesSQL, "delete expired target aggregates", aggregateCutoff); err != nil {
		return retention.Result{}, err
	}
	if result.DeletedEvents, err = execRows(ctx, tx, deleteExpiredEventsSQL, "delete expired events", eventCutoff); err != nil {
		return retention.Result{}, err
	}
	if result.DeletedNotifications, err = execRows(ctx, tx, deleteExpiredNotificationsSQL, "delete expired notifications", notificationCutoff); err != nil {
		return retention.Result{}, err
	}

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
