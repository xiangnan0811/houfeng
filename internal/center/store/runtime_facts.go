package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/runtimefacts"
	"houfeng/internal/center/targets"
)

const runtimeFactsMonitoringInstanceExistsSQL = `
		select 1
		from monitoring_instances
		where monitoring_instance_id = $1`

const runtimeFactsLatestHostSampleSQL = `
		select
			monitoring_instance_id,
			observed_at,
			received_at,
			agent_version,
			fingerprint,
			cpu_usage_pct,
			load_1,
			load_5,
			load_15,
			mem_used_pct,
			mem_available_bytes,
			mem_total_bytes,
			swap_used_pct,
			disk_used_pct,
			disk_total_bytes,
			inode_used_pct,
			net_in_bytes_per_sec,
			net_out_bytes_per_sec,
			cpu_iowait_pct,
			cpu_steal_pct,
			disk_read_bytes_per_sec,
			disk_write_bytes_per_sec,
			disk_busy_pct,
			uptime_seconds,
			maintenance_context,
			is_backfilled,
			sync_batch_id,
			containers
		from host_samples
		where monitoring_instance_id = $1
		order by observed_at desc, id desc
		limit 1`

const runtimeFactsHostSampleWindowSummarySQL = `
			select
				min(observed_at),
			max(observed_at),
			count(*)::integer
		from host_samples
		where monitoring_instance_id = $1
			and observed_at >= $2
			and observed_at <= $3`

const runtimeFactsHostMetricPointsSQL = `
		with bucketed as (
			select
				least(
					floor(extract(epoch from (observed_at - $2::timestamptz)) / $5::double precision)::integer,
					$4 - 1
				) as bucket,
				cpu_usage_pct,
				mem_used_pct,
				disk_used_pct,
				inode_used_pct,
				load_5,
				cpu_iowait_pct,
				net_in_bytes_per_sec,
				net_out_bytes_per_sec
			from host_samples
			where monitoring_instance_id = $1
				and observed_at >= $2
				and observed_at <= $3
		)
		select
			to_timestamp(extract(epoch from $2::timestamptz) + (bucket::double precision * $5::double precision))::timestamptz as observed_at,
			count(*)::integer as sample_count,
			avg(cpu_usage_pct)::double precision,
			avg(mem_used_pct)::double precision,
			avg(disk_used_pct)::double precision,
			avg(inode_used_pct)::double precision,
			avg(load_5)::double precision,
			avg(cpu_iowait_pct)::double precision,
			avg(net_in_bytes_per_sec)::double precision,
			avg(net_out_bytes_per_sec)::double precision
		from bucketed
		where bucket >= 0
			and bucket < $4
		group by bucket
		order by bucket asc`

const runtimeFactsRecentHostSamplesSQL = `
		select
			monitoring_instance_id,
			observed_at,
			received_at,
			agent_version,
			fingerprint,
			cpu_usage_pct,
			load_1,
			load_5,
			load_15,
			mem_used_pct,
			mem_available_bytes,
			mem_total_bytes,
			swap_used_pct,
			disk_used_pct,
			disk_total_bytes,
			inode_used_pct,
			net_in_bytes_per_sec,
			net_out_bytes_per_sec,
			cpu_iowait_pct,
			cpu_steal_pct,
			disk_read_bytes_per_sec,
			disk_write_bytes_per_sec,
			disk_busy_pct,
			uptime_seconds,
			maintenance_context,
			is_backfilled,
			sync_batch_id,
			containers
		from host_samples
		where monitoring_instance_id = $1
			and observed_at >= $2
			and observed_at <= $3
		order by observed_at asc, id asc`

const runtimeFactsTargetExistsSQL = `
		select 1
		from targets
		where target_id = $1`

const runtimeFactsLatestProbeObservationsSQL = `
		select
			latest.monitoring_instance_id,
			latest.target_id,
			latest.probe_item_id,
			latest.probe_kind,
			latest.observed_at,
			latest.received_at,
			latest.agent_version,
			latest.fingerprint,
			latest.result_kind,
			latest.latency_ms,
			latest.http_status,
			latest.tls_expiry_days,
			latest.error_code,
			latest.error_summary,
			latest.maintenance_context,
			latest.is_backfilled,
			latest.sync_batch_id
		from (
			select distinct on (po.probe_item_id, po.monitoring_instance_id)
				po.monitoring_instance_id,
				po.target_id,
				po.probe_item_id,
				pi.probe_kind,
				po.observed_at,
				po.received_at,
				po.agent_version,
				po.fingerprint,
				po.result_kind,
				po.latency_ms,
				po.http_status,
				po.tls_expiry_days,
				coalesce(po.error_code, '') as error_code,
				coalesce(po.error_summary, '') as error_summary,
				po.maintenance_context,
				po.is_backfilled,
				po.sync_batch_id,
				po.id
			from probe_observations po
			join probe_items pi on pi.probe_item_id = po.probe_item_id
			where po.target_id = $1
			order by po.probe_item_id, po.monitoring_instance_id, po.observed_at desc, po.id desc
		) latest
		order by latest.observed_at desc, latest.probe_item_id, latest.monitoring_instance_id`

const runtimeFactsRecentProbeObservationsSQL = `
		select
			po.monitoring_instance_id,
			po.target_id,
			po.probe_item_id,
			pi.probe_kind,
			po.observed_at,
			po.received_at,
			po.agent_version,
			po.fingerprint,
			po.result_kind,
			po.latency_ms,
			po.http_status,
			po.tls_expiry_days,
			coalesce(po.error_code, '') as error_code,
			coalesce(po.error_summary, '') as error_summary,
			po.maintenance_context,
			po.is_backfilled,
			po.sync_batch_id
		from probe_observations po
		join probe_items pi on pi.probe_item_id = po.probe_item_id
		where po.target_id = $1
			and po.observed_at >= $2
		order by po.observed_at desc, po.id desc
		limit $3`

type runtimeFactsQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type PostgresRuntimeFactsRepository struct {
	db runtimeFactsQueryer
}

func NewPostgresRuntimeFactsRepository(db *pgxpool.Pool) *PostgresRuntimeFactsRepository {
	return &PostgresRuntimeFactsRepository{db: db}
}

var _ runtimefacts.Repository = (*PostgresRuntimeFactsRepository)(nil)

func (r *PostgresRuntimeFactsRepository) GetMonitoringInstanceRuntimeFacts(ctx context.Context, monitoringInstanceID string, window runtimefacts.WindowRequest) (runtimefacts.MonitoringInstanceRuntimeFacts, error) {
	if window.BucketCount <= 0 || !window.EndedAt.After(window.StartedAt) {
		return runtimefacts.MonitoringInstanceRuntimeFacts{}, fmt.Errorf("invalid monitoring runtime window %q", window.Key)
	}

	var exists int
	if err := r.db.QueryRow(ctx, runtimeFactsMonitoringInstanceExistsSQL, monitoringInstanceID).Scan(&exists); errors.Is(err, pgx.ErrNoRows) {
		return runtimefacts.MonitoringInstanceRuntimeFacts{}, monitoringinstances.ErrMonitoringInstanceNotFound
	} else if err != nil {
		return runtimefacts.MonitoringInstanceRuntimeFacts{}, fmt.Errorf("query monitoring instance %q existence: %w", monitoringInstanceID, err)
	}

	facts := runtimefacts.MonitoringInstanceRuntimeFacts{
		MonitoringInstanceID: monitoringInstanceID,
		Window: runtimefacts.RuntimeWindowSummary{
			Key:         window.Key,
			StartedAt:   window.StartedAt,
			EndedAt:     window.EndedAt,
			BucketCount: window.BucketCount,
		},
		HostMetricPoints:  make([]runtimefacts.HostMetricPoint, 0),
		RecentHostSamples: make([]runtimefacts.HostSample, 0),
	}
	var latest runtimefacts.HostSample
	if err := scanHostSample(r.db.QueryRow(ctx, runtimeFactsLatestHostSampleSQL, monitoringInstanceID), &latest); errors.Is(err, pgx.ErrNoRows) {
	} else if err != nil {
		return runtimefacts.MonitoringInstanceRuntimeFacts{}, fmt.Errorf("query latest host sample for monitoring instance %q: %w", monitoringInstanceID, err)
	} else {
		facts.LatestHostSample = &latest
	}

	if err := scanRuntimeWindowSummary(r.db.QueryRow(ctx, runtimeFactsHostSampleWindowSummarySQL, monitoringInstanceID, window.StartedAt, window.EndedAt), &facts.Window); err != nil {
		return runtimefacts.MonitoringInstanceRuntimeFacts{}, fmt.Errorf("query host sample window summary for monitoring instance %q: %w", monitoringInstanceID, err)
	}

	bucketSeconds := window.EndedAt.Sub(window.StartedAt).Seconds() / float64(window.BucketCount)
	rows, err := r.db.Query(ctx, runtimeFactsHostMetricPointsSQL, monitoringInstanceID, window.StartedAt, window.EndedAt, window.BucketCount, bucketSeconds)
	if err != nil {
		return runtimefacts.MonitoringInstanceRuntimeFacts{}, fmt.Errorf("query host metric points for monitoring instance %q: %w", monitoringInstanceID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var point runtimefacts.HostMetricPoint
		if err := scanHostMetricPoint(rows, &point); err != nil {
			return runtimefacts.MonitoringInstanceRuntimeFacts{}, fmt.Errorf("scan host metric point for monitoring instance %q: %w", monitoringInstanceID, err)
		}
		facts.HostMetricPoints = append(facts.HostMetricPoints, point)
	}
	if err := rows.Err(); err != nil {
		return runtimefacts.MonitoringInstanceRuntimeFacts{}, fmt.Errorf("iterate host metric points for monitoring instance %q: %w", monitoringInstanceID, err)
	}

	if window.Key == "realtime" {
		recentRows, err := r.db.Query(ctx, runtimeFactsRecentHostSamplesSQL, monitoringInstanceID, window.StartedAt, window.EndedAt)
		if err != nil {
			return runtimefacts.MonitoringInstanceRuntimeFacts{}, fmt.Errorf("query recent host samples for monitoring instance %q: %w", monitoringInstanceID, err)
		}
		defer recentRows.Close()
		for recentRows.Next() {
			var sample runtimefacts.HostSample
			if err := scanHostSample(recentRows, &sample); err != nil {
				return runtimefacts.MonitoringInstanceRuntimeFacts{}, fmt.Errorf("scan recent host sample for monitoring instance %q: %w", monitoringInstanceID, err)
			}
			facts.RecentHostSamples = append(facts.RecentHostSamples, sample)
		}
		if err := recentRows.Err(); err != nil {
			return runtimefacts.MonitoringInstanceRuntimeFacts{}, fmt.Errorf("iterate recent host samples for monitoring instance %q: %w", monitoringInstanceID, err)
		}
	}

	return facts, nil
}

func (r *PostgresRuntimeFactsRepository) GetTargetRuntimeFacts(ctx context.Context, targetID string, since time.Time, limit int) (runtimefacts.TargetRuntimeFacts, error) {
	var exists int
	if err := r.db.QueryRow(ctx, runtimeFactsTargetExistsSQL, targetID).Scan(&exists); errors.Is(err, pgx.ErrNoRows) {
		return runtimefacts.TargetRuntimeFacts{}, targets.ErrTargetNotFound
	} else if err != nil {
		return runtimefacts.TargetRuntimeFacts{}, fmt.Errorf("query target %q existence: %w", targetID, err)
	}

	facts := runtimefacts.TargetRuntimeFacts{
		TargetID:                targetID,
		LatestProbeObservations: make([]runtimefacts.ProbeObservation, 0),
		RecentProbeObservations: make([]runtimefacts.ProbeObservation, 0),
	}

	latestRows, err := r.db.Query(ctx, runtimeFactsLatestProbeObservationsSQL, targetID)
	if err != nil {
		return runtimefacts.TargetRuntimeFacts{}, fmt.Errorf("query latest probe observations for target %q: %w", targetID, err)
	}
	defer latestRows.Close()
	for latestRows.Next() {
		var observation runtimefacts.ProbeObservation
		if err := scanProbeObservation(latestRows, &observation); err != nil {
			return runtimefacts.TargetRuntimeFacts{}, fmt.Errorf("scan latest probe observation for target %q: %w", targetID, err)
		}
		facts.LatestProbeObservations = append(facts.LatestProbeObservations, observation)
	}
	if err := latestRows.Err(); err != nil {
		return runtimefacts.TargetRuntimeFacts{}, fmt.Errorf("iterate latest probe observations for target %q: %w", targetID, err)
	}

	recentRows, err := r.db.Query(ctx, runtimeFactsRecentProbeObservationsSQL, targetID, since, limit)
	if err != nil {
		return runtimefacts.TargetRuntimeFacts{}, fmt.Errorf("query recent probe observations for target %q: %w", targetID, err)
	}
	defer recentRows.Close()
	for recentRows.Next() {
		var observation runtimefacts.ProbeObservation
		if err := scanProbeObservation(recentRows, &observation); err != nil {
			return runtimefacts.TargetRuntimeFacts{}, fmt.Errorf("scan recent probe observation for target %q: %w", targetID, err)
		}
		facts.RecentProbeObservations = append(facts.RecentProbeObservations, observation)
	}
	if err := recentRows.Err(); err != nil {
		return runtimefacts.TargetRuntimeFacts{}, fmt.Errorf("iterate recent probe observations for target %q: %w", targetID, err)
	}

	return facts, nil
}

type runtimeFactsScanner interface {
	Scan(...any) error
}

func scanRuntimeWindowSummary(scanner runtimeFactsScanner, summary *runtimefacts.RuntimeWindowSummary) error {
	var (
		availableStartedAt sql.NullTime
		availableEndedAt   sql.NullTime
	)
	if err := scanner.Scan(&availableStartedAt, &availableEndedAt, &summary.SampleCount); err != nil {
		return err
	}
	if availableStartedAt.Valid {
		startedAt := availableStartedAt.Time.UTC()
		summary.AvailableStartedAt = &startedAt
	}
	if availableEndedAt.Valid {
		endedAt := availableEndedAt.Time.UTC()
		summary.AvailableEndedAt = &endedAt
	}
	return nil
}

func scanHostMetricPoint(scanner runtimeFactsScanner, point *runtimefacts.HostMetricPoint) error {
	return scanner.Scan(
		&point.ObservedAt,
		&point.SampleCount,
		&point.CPUUsagePct,
		&point.MemUsedPct,
		&point.DiskUsedPct,
		&point.InodeUsedPct,
		&point.Load5,
		&point.CPUIOWaitPct,
		&point.NetInBytesPerSec,
		&point.NetOutBytesPerSec,
	)
}

func scanHostSample(scanner runtimeFactsScanner, sample *runtimefacts.HostSample) error {
	var containersJSON []byte
	if err := scanner.Scan(
		&sample.MonitoringInstanceID,
		&sample.ObservedAt,
		&sample.ReceivedAt,
		&sample.AgentVersion,
		&sample.Fingerprint,
		&sample.CPUUsagePct,
		&sample.Load1,
		&sample.Load5,
		&sample.Load15,
		&sample.MemUsedPct,
		&sample.MemAvailableBytes,
		&sample.MemTotalBytes,
		&sample.SwapUsedPct,
		&sample.DiskUsedPct,
		&sample.DiskTotalBytes,
		&sample.InodeUsedPct,
		&sample.NetInBytesPerSec,
		&sample.NetOutBytesPerSec,
		&sample.CPUIOWaitPct,
		&sample.CPUStealPct,
		&sample.DiskReadBytesPerSec,
		&sample.DiskWriteBytesPerSec,
		&sample.DiskBusyPct,
		&sample.UptimeSeconds,
		&sample.MaintenanceContext,
		&sample.IsBackfilled,
		&sample.SyncBatchID,
		&containersJSON,
	); err != nil {
		return err
	}
	if len(containersJSON) > 0 {
		_ = json.Unmarshal(containersJSON, &sample.Containers)
	}
	return nil
}

func scanProbeObservation(scanner runtimeFactsScanner, observation *runtimefacts.ProbeObservation) error {
	return scanner.Scan(
		&observation.MonitoringInstanceID,
		&observation.TargetID,
		&observation.ProbeItemID,
		&observation.ProbeKind,
		&observation.ObservedAt,
		&observation.ReceivedAt,
		&observation.AgentVersion,
		&observation.Fingerprint,
		&observation.ResultKind,
		&observation.LatencyMS,
		&observation.HTTPStatus,
		&observation.TLSExpiryDays,
		&observation.ErrorCode,
		&observation.ErrorSummary,
		&observation.MaintenanceContext,
		&observation.IsBackfilled,
		&observation.SyncBatchID,
	)
}
