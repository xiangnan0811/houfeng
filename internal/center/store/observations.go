package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/observations"
)

type PostgresObservationRepository struct {
	db *pgxpool.Pool
}

func NewPostgresObservationRepository(db *pgxpool.Pool) *PostgresObservationRepository {
	return &PostgresObservationRepository{db: db}
}

var _ observations.Repository = (*PostgresObservationRepository)(nil)

func (r *PostgresObservationRepository) RecordBatch(ctx context.Context, batch observations.BatchWrite) error {
	if len(batch.HostSamples) == 0 && len(batch.ProbeObservations) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin observation batch tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := recordObservationBatch(ctx, tx, batch); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit observation batch tx: %w", err)
	}

	return nil
}

type sqlExec interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func recordObservationBatch(ctx context.Context, exec sqlExec, batch observations.BatchWrite) error {
	for _, sample := range batch.HostSamples {
		if _, err := exec.Exec(ctx, `
			insert into host_samples (
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
			) values (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25
			)`,
			sample.NodeID,
			sample.ObservedAt,
			sample.ReceivedAt,
			sample.AgentVersion,
			sample.Fingerprint,
			sample.CPUUsagePct,
			sample.Load1,
			sample.Load5,
			sample.Load15,
			sample.MemUsedPct,
			sample.MemAvailableBytes,
			sample.SwapUsedPct,
			sample.DiskUsedPct,
			sample.InodeUsedPct,
			sample.NetInBytesPerSec,
			sample.NetOutBytesPerSec,
			sample.CPUIOWaitPct,
			sample.CPUStealPct,
			sample.DiskReadBytesPerSec,
			sample.DiskWriteBytesPerSec,
			sample.DiskBusyPct,
			sample.UptimeSeconds,
			sample.MaintenanceContext,
			sample.IsBackfilled,
			sample.SyncBatchID,
		); err != nil {
			return fmt.Errorf("insert host sample: %w", err)
		}
	}

	for _, observation := range batch.ProbeObservations {
		if _, err := exec.Exec(ctx, `
			insert into probe_observations (
				node_id,
				target_id,
				probe_item_id,
				observed_at,
				received_at,
				agent_version,
				fingerprint,
				result_kind,
				latency_ms,
				http_status,
				tls_expiry_days,
				error_code,
				error_summary,
				maintenance_context,
				is_backfilled,
				sync_batch_id
			) values (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
			)`,
			observation.NodeID,
			observation.TargetID,
			observation.ProbeItemID,
			observation.ObservedAt,
			observation.ReceivedAt,
			observation.AgentVersion,
			observation.Fingerprint,
			observation.ResultKind,
			derefInt(observation.LatencyMS),
			derefInt(observation.HTTPStatus),
			derefInt(observation.TLSExpiryDays),
			observation.ErrorCode,
			observation.ErrorSummary,
			observation.MaintenanceContext,
			observation.IsBackfilled,
			observation.SyncBatchID,
		); err != nil {
			return fmt.Errorf("insert probe observation: %w", err)
		}
	}

	return nil
}

func derefInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}
