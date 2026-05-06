package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/nodes"
	"houfeng/internal/center/runtimefacts"
	"houfeng/internal/center/targets"
)

const runtimeFactsNodeExistsSQL = `
		select 1
		from nodes
		where node_id = $1`

const runtimeFactsLatestHostSampleSQL = `
		select
			node_id,
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
			swap_used_pct,
			disk_used_pct,
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
		where node_id = $1
		order by observed_at desc, id desc
		limit 1`

const runtimeFactsRecentHostSamplesSQL = `
		select
			node_id,
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
			swap_used_pct,
			disk_used_pct,
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
		where node_id = $1
			and observed_at >= $2
		order by observed_at desc, id desc
		limit $3`

const runtimeFactsTargetExistsSQL = `
		select 1
		from targets
		where target_id = $1`

const runtimeFactsLatestProbeObservationsSQL = `
		select
			latest.node_id,
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
			select distinct on (po.probe_item_id, po.node_id)
				po.node_id,
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
			order by po.probe_item_id, po.node_id, po.observed_at desc, po.id desc
		) latest
		order by latest.observed_at desc, latest.probe_item_id, latest.node_id`

const runtimeFactsRecentProbeObservationsSQL = `
		select
			po.node_id,
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

func (r *PostgresRuntimeFactsRepository) GetNodeRuntimeFacts(ctx context.Context, nodeID string, since time.Time, limit int) (runtimefacts.NodeRuntimeFacts, error) {
	var exists int
	if err := r.db.QueryRow(ctx, runtimeFactsNodeExistsSQL, nodeID).Scan(&exists); errors.Is(err, pgx.ErrNoRows) {
		return runtimefacts.NodeRuntimeFacts{}, nodes.ErrNodeNotFound
	} else if err != nil {
		return runtimefacts.NodeRuntimeFacts{}, fmt.Errorf("query node %q existence: %w", nodeID, err)
	}

	facts := runtimefacts.NodeRuntimeFacts{
		NodeID:            nodeID,
		RecentHostSamples: make([]runtimefacts.HostSample, 0),
	}
	var latest runtimefacts.HostSample
	if err := scanHostSample(r.db.QueryRow(ctx, runtimeFactsLatestHostSampleSQL, nodeID), &latest); errors.Is(err, pgx.ErrNoRows) {
		return facts, nil
	} else if err != nil {
		return runtimefacts.NodeRuntimeFacts{}, fmt.Errorf("query latest host sample for node %q: %w", nodeID, err)
	}
	facts.LatestHostSample = &latest

	rows, err := r.db.Query(ctx, runtimeFactsRecentHostSamplesSQL, nodeID, since, limit)
	if err != nil {
		return runtimefacts.NodeRuntimeFacts{}, fmt.Errorf("query recent host samples for node %q: %w", nodeID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var sample runtimefacts.HostSample
		if err := scanHostSample(rows, &sample); err != nil {
			return runtimefacts.NodeRuntimeFacts{}, fmt.Errorf("scan recent host sample for node %q: %w", nodeID, err)
		}
		facts.RecentHostSamples = append(facts.RecentHostSamples, sample)
	}
	if err := rows.Err(); err != nil {
		return runtimefacts.NodeRuntimeFacts{}, fmt.Errorf("iterate recent host samples for node %q: %w", nodeID, err)
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

func scanHostSample(scanner runtimeFactsScanner, sample *runtimefacts.HostSample) error {
	var containersJSON []byte
	if err := scanner.Scan(
		&sample.NodeID,
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
		&sample.SwapUsedPct,
		&sample.DiskUsedPct,
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
		&observation.NodeID,
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
