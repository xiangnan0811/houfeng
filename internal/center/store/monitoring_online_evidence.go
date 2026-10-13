package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/runtimefacts"
)

// GetMonitoringInstanceOnlineEvidence reads trusted heartbeat evidence without
// consulting sample collection, binding state, or sample permissions.
func (r *PostgresMonitoringInstanceRepository) GetMonitoringInstanceOnlineEvidence(ctx context.Context, monitoringInstanceID string) (runtimefacts.OnlineEvidenceStreamMessage, error) {
	evidence := runtimefacts.OnlineEvidenceStreamMessage{
		Type:                 "online_evidence",
		MonitoringInstanceID: monitoringInstanceID,
	}
	err := r.db.QueryRow(ctx, `
		select last_trusted_online_at, clock_timestamp()
		from monitoring_instances
		where monitoring_instance_id = $1`, monitoringInstanceID).Scan(&evidence.LastTrustedOnlineAt, &evidence.ReadAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return runtimefacts.OnlineEvidenceStreamMessage{}, monitoringinstances.ErrMonitoringInstanceNotFound
	}
	if err != nil {
		return runtimefacts.OnlineEvidenceStreamMessage{}, fmt.Errorf("read monitoring instance online evidence: %w", err)
	}
	return evidence, nil
}
