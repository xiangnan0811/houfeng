package store

import (
	"context"
	"fmt"
	"houfeng/internal/center/monitoringinstances"
)

func (r *PostgresMonitoringInstanceRepository) ListMonitoringInstancePhases(ctx context.Context, id string) ([]monitoringinstances.Phase, error) {
	if _, err := r.GetMonitoringInstance(ctx, id); err != nil {
		return nil, err
	}
	rows, err := r.db.Query(ctx, `select session_id,capability,fingerprint_hash,started_at,ended_at,last_trusted_online_at,ever_connected from monitoring_agent_sessions where monitoring_instance_id=$1 order by started_at desc,session_id`, id)
	if err != nil {
		return nil, fmt.Errorf("query monitoring phases: %w", err)
	}
	defer rows.Close()
	phases := []monitoringinstances.Phase{}
	for rows.Next() {
		var p monitoringinstances.Phase
		if err := rows.Scan(&p.SessionID, &p.Capability, &p.FingerprintHash, &p.StartedAt, &p.EndedAt, &p.LastTrustedOnlineAt, &p.EverConnected); err != nil {
			return nil, err
		}
		phases = append(phases, p)
	}
	return phases, rows.Err()
}
