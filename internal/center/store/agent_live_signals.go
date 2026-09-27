package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/syncing"
)

// The asset graph shared lock and instance/session row locks must already be
// held. Archive takes the graph exclusive lock, so a committed live signal and
// an archive decision cannot pass each other.
func recordTrustedLiveSignal(ctx context.Context, tx syncBatchTx, batch syncing.Batch, state acceptedSyncBatchState, receivedAt time.Time) error {
	if batch.LiveSignal == nil {
		return nil
	}
	// Archive safety compares against the same database clock. Process wall
	// time may differ from PostgreSQL and must never make a fresh signal stale.
	if err := tx.QueryRow(ctx, `select clock_timestamp()`).Scan(&receivedAt); err != nil {
		return fmt.Errorf("read trusted signal receiver time: %w", err)
	}
	tag, err := tx.Exec(ctx, `insert into agent_live_signals(session_id,signal_id,received_at) values($1,$2,$3) on conflict do nothing`, batch.SessionID, batch.LiveSignal.ID, receivedAt)
	if err != nil {
		return fmt.Errorf("record trusted live signal: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	if _, err = tx.Exec(ctx, `update monitoring_agent_sessions set last_trusted_online_at=$2,ever_connected=true where session_id=$1`, batch.SessionID, receivedAt); err != nil {
		return fmt.Errorf("touch session online evidence: %w", err)
	}
	// Enrollment is confirmed by the first trusted live signal, independently of
	// performance collection. A retired session can never re-enroll an instance.
	if _, err = tx.Exec(ctx, `update monitoring_instances set last_trusted_online_at=$2,ever_connected=true,
 lifecycle_status=case when $3 and lifecycle_status=$4 then $5 else lifecycle_status end,
 updated_at=now() where monitoring_instance_id=$1`, batch.MonitoringInstanceID, receivedAt, state.Capability == "full" && !state.Archived, monitoringinstances.LifecyclePendingEnrollment, monitoringinstances.LifecycleInUse); err != nil {
		return fmt.Errorf("touch instance online evidence: %w", err)
	}
	if state.Archived {
		details, _ := json.Marshal(map[string]any{"monitoring_instance_id": batch.MonitoringInstanceID, "session_id": batch.SessionID, "last_trusted_online_at": receivedAt})
		dedupeKey := state.VPSID
		if state.VPSArchivedAt != nil {
			dedupeKey += ":" + state.VPSArchivedAt.UTC().Format(time.RFC3339Nano)
		}
		if err := upsertVPSFollowup(ctx, tx, state.VPSID, "archived_online", dedupeKey, "已归档 VPS 的 Agent 再次在线，请核对资源是否仍在运行", details); err != nil {
			return err
		}
	}
	return nil
}
