package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/agentplan"
	"houfeng/internal/center/nodes"
	"houfeng/internal/center/targets"
	"houfeng/internal/contracts/agentapi"
)

const selectAgentPlanNodeLabelsSQL = `
	select labels
	from nodes
	where node_id = $1`

const selectAgentPlanAssignmentsSQL = `
	select
		t.target_id,
		t.host,
		t.base_port,
		t.run_status,
		p.probe_item_id,
		p.probe_kind,
		p.frequency_tier,
		p.timeout_seconds,
		p.config
	from targets t
	join probe_items p on p.target_id = t.target_id
	where p.enabled = true
		and t.run_status = any($1)
		and t.execution_node_labels && $2
	order by t.target_id, p.probe_item_id`

type agentPlanQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type PostgresAgentPlanRepository struct {
	db agentPlanQueryer
}

func NewPostgresAgentPlanRepository(db *pgxpool.Pool) *PostgresAgentPlanRepository {
	return &PostgresAgentPlanRepository{db: db}
}

var _ agentplan.Repository = (*PostgresAgentPlanRepository)(nil)

func (r *PostgresAgentPlanRepository) BuildSyncPlan(ctx context.Context, nodeID string) (agentplan.SyncPlan, error) {
	return buildSyncPlan(ctx, r.db, nodeID)
}

func buildSyncPlan(ctx context.Context, queryer agentPlanQueryer, nodeID string) (agentplan.SyncPlan, error) {
	var labels []string
	if err := queryer.QueryRow(ctx, selectAgentPlanNodeLabelsSQL, nodeID).Scan(&labels); errors.Is(err, pgx.ErrNoRows) {
		return agentplan.SyncPlan{}, nodes.ErrNodeNotFound
	} else if err != nil {
		return agentplan.SyncPlan{}, fmt.Errorf("query labels for node %q: %w", nodeID, err)
	}

	plan := agentplan.SyncPlan{
		HostSampleFrequencyTier: hostSampleFrequencyTier(labels),
		ProbeAssignments:        make([]agentplan.ProbeAssignment, 0),
	}
	if len(labels) == 0 {
		return plan, nil
	}

	rows, err := queryer.Query(ctx, selectAgentPlanAssignmentsSQL, []string{targets.RunStatusEnabled, targets.RunStatusMaintenance}, labels)
	if err != nil {
		return agentplan.SyncPlan{}, fmt.Errorf("query sync-plan assignments for node %q: %w", nodeID, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			assignment agentplan.ProbeAssignment
			runStatus  string
			config     []byte
		)
		if err := rows.Scan(
			&assignment.TargetID,
			&assignment.TargetHost,
			&assignment.TargetBasePort,
			&runStatus,
			&assignment.ProbeItemID,
			&assignment.ProbeKind,
			&assignment.FrequencyTier,
			&assignment.TimeoutSeconds,
			&config,
		); err != nil {
			return agentplan.SyncPlan{}, fmt.Errorf("scan sync-plan assignment for node %q: %w", nodeID, err)
		}
		assignment.MaintenanceContext = runStatus == targets.RunStatusMaintenance
		assignment.Config = json.RawMessage(append([]byte(nil), config...))
		plan.ProbeAssignments = append(plan.ProbeAssignments, assignment)
	}
	if err := rows.Err(); err != nil {
		return agentplan.SyncPlan{}, fmt.Errorf("iterate sync-plan assignments for node %q: %w", nodeID, err)
	}

	return plan, nil
}

func hostSampleFrequencyTier(labels []string) string {
	for _, label := range labels {
		if label == "核心" {
			return agentapi.FrequencyTier1m
		}
	}
	return agentapi.FrequencyTier5m
}
