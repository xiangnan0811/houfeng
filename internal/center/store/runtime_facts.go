package store

import (
	"context"
	"errors"
	"fmt"

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
		sync_batch_id
	from host_samples
	where node_id = $1
	order by observed_at desc, id desc
	limit 1`

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

func (r *PostgresRuntimeFactsRepository) GetNodeRuntimeFacts(ctx context.Context, nodeID string) (runtimefacts.NodeRuntimeFacts, error) {
	var exists int
	if err := r.db.QueryRow(ctx, runtimeFactsNodeExistsSQL, nodeID).Scan(&exists); errors.Is(err, pgx.ErrNoRows) {
		return runtimefacts.NodeRuntimeFacts{}, nodes.ErrNodeNotFound
	} else if err != nil {
		return runtimefacts.NodeRuntimeFacts{}, fmt.Errorf("query node %q existence: %w", nodeID, err)
	}

	facts := runtimefacts.NodeRuntimeFacts{NodeID: nodeID}
	var latest runtimefacts.HostSample
	if err := r.db.QueryRow(ctx, runtimeFactsLatestHostSampleSQL, nodeID).Scan(
		&latest.NodeID,
		&latest.ObservedAt,
		&latest.ReceivedAt,
		&latest.AgentVersion,
		&latest.Fingerprint,
		&latest.CPUUsagePct,
		&latest.Load1,
		&latest.Load5,
		&latest.Load15,
		&latest.MemUsedPct,
		&latest.MemAvailableBytes,
		&latest.SwapUsedPct,
		&latest.DiskUsedPct,
		&latest.InodeUsedPct,
		&latest.NetInBytesPerSec,
		&latest.NetOutBytesPerSec,
		&latest.CPUIOWaitPct,
		&latest.CPUStealPct,
		&latest.DiskReadBytesPerSec,
		&latest.DiskWriteBytesPerSec,
		&latest.DiskBusyPct,
		&latest.UptimeSeconds,
		&latest.MaintenanceContext,
		&latest.IsBackfilled,
		&latest.SyncBatchID,
	); errors.Is(err, pgx.ErrNoRows) {
		return facts, nil
	} else if err != nil {
		return runtimefacts.NodeRuntimeFacts{}, fmt.Errorf("query latest host sample for node %q: %w", nodeID, err)
	}

	facts.LatestHostSample = &latest
	return facts, nil
}

func (r *PostgresRuntimeFactsRepository) GetTargetRuntimeFacts(ctx context.Context, targetID string) (runtimefacts.TargetRuntimeFacts, error) {
	var exists int
	if err := r.db.QueryRow(ctx, runtimeFactsTargetExistsSQL, targetID).Scan(&exists); errors.Is(err, pgx.ErrNoRows) {
		return runtimefacts.TargetRuntimeFacts{}, targets.ErrTargetNotFound
	} else if err != nil {
		return runtimefacts.TargetRuntimeFacts{}, fmt.Errorf("query target %q existence: %w", targetID, err)
	}

	rows, err := r.db.Query(ctx, runtimeFactsLatestProbeObservationsSQL, targetID)
	if err != nil {
		return runtimefacts.TargetRuntimeFacts{}, fmt.Errorf("query latest probe observations for target %q: %w", targetID, err)
	}
	defer rows.Close()

	facts := runtimefacts.TargetRuntimeFacts{
		TargetID:                targetID,
		LatestProbeObservations: make([]runtimefacts.ProbeObservation, 0),
	}
	for rows.Next() {
		var observation runtimefacts.ProbeObservation
		if err := rows.Scan(
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
		); err != nil {
			return runtimefacts.TargetRuntimeFacts{}, fmt.Errorf("scan latest probe observation for target %q: %w", targetID, err)
		}
		facts.LatestProbeObservations = append(facts.LatestProbeObservations, observation)
	}
	if err := rows.Err(); err != nil {
		return runtimefacts.TargetRuntimeFacts{}, fmt.Errorf("iterate latest probe observations for target %q: %w", targetID, err)
	}

	return facts, nil
}
