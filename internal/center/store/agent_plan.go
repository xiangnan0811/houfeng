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
	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/targets"
	"houfeng/internal/contracts/agentapi"
)

const selectAgentPlanNodeLabelsSQL = `
	select labels,
		coalesce(nullif(cs.host_sample_frequency_tier, ''), '5m') as host_sample_frequency_tier,
		coalesce(
			cs.override_rules,
			'{"node_labels":[],"target_types":[],"target_labels":[]}'::jsonb
		) as override_rules,
		cs.settings_id is not null as settings_row_present
	from nodes n
	left join center_settings cs on cs.settings_id = $2
	where n.node_id = $1`

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
		p.config,
		t.target_type,
		t.labels
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

type agentPlanSettingsSnapshot struct {
	HostSampleFrequencyTier string
	OverrideRules           centersettings.OverrideRules
}

func NewPostgresAgentPlanRepository(db *pgxpool.Pool) *PostgresAgentPlanRepository {
	return &PostgresAgentPlanRepository{db: db}
}

var _ agentplan.Repository = (*PostgresAgentPlanRepository)(nil)

func (r *PostgresAgentPlanRepository) BuildSyncPlan(ctx context.Context, nodeID string) (agentplan.SyncPlan, error) {
	return buildSyncPlan(ctx, r.db, nodeID)
}

func buildSyncPlan(ctx context.Context, queryer agentPlanQueryer, nodeID string) (agentplan.SyncPlan, error) {
	var (
		labels             []string
		hostSampleTier     string
		overrideRulesJSON  []byte
		settingsRowPresent bool
	)
	if err := queryer.QueryRow(ctx, selectAgentPlanNodeLabelsSQL, nodeID, centersettings.SingletonID).Scan(&labels, &hostSampleTier, &overrideRulesJSON, &settingsRowPresent); errors.Is(err, pgx.ErrNoRows) {
		return agentplan.SyncPlan{}, nodes.ErrNodeNotFound
	} else if err != nil {
		return agentplan.SyncPlan{}, fmt.Errorf("query labels for node %q: %w", nodeID, err)
	}

	settings, err := resolveAgentPlanSettings(settingsRowPresent, labels, hostSampleTier, overrideRulesJSON)
	if err != nil {
		return agentplan.SyncPlan{}, fmt.Errorf("resolve sync-plan settings for node %q: %w", nodeID, err)
	}

	plan := agentplan.SyncPlan{
		HostSampleFrequencyTier: resolveHostSampleFrequencyTier(settings.HostSampleFrequencyTier, labels, settings.OverrideRules),
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
			assignment   agentplan.ProbeAssignment
			runStatus    string
			config       []byte
			targetType   string
			targetLabels []string
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
			&targetType,
			&targetLabels,
		); err != nil {
			return agentplan.SyncPlan{}, fmt.Errorf("scan sync-plan assignment for node %q: %w", nodeID, err)
		}
		assignment.MaintenanceContext = runStatus == targets.RunStatusMaintenance
		assignment.Config = json.RawMessage(append([]byte(nil), config...))
		assignment.FrequencyTier = resolveProbeAssignmentFrequencyTier(
			assignment.FrequencyTier,
			assignment.ProbeKind,
			targetType,
			targetLabels,
			settings.OverrideRules,
		)
		plan.ProbeAssignments = append(plan.ProbeAssignments, assignment)
	}
	if err := rows.Err(); err != nil {
		return agentplan.SyncPlan{}, fmt.Errorf("iterate sync-plan assignments for node %q: %w", nodeID, err)
	}

	return plan, nil
}

func resolveAgentPlanSettings(settingsRowPresent bool, nodeLabels []string, hostSampleTier string, overrideRulesJSON []byte) (agentPlanSettingsSnapshot, error) {
	settings := agentPlanSettingsSnapshot{
		HostSampleFrequencyTier: centersettings.Default().HostSampleFrequencyTier,
		OverrideRules:           centersettings.Default().OverrideRules,
	}
	if !settingsRowPresent {
		settings.HostSampleFrequencyTier = legacyHostSampleFrequencyTier(nodeLabels)
		return settings, nil
	}
	if hostSampleTier != "" {
		settings.HostSampleFrequencyTier = hostSampleTier
	}
	if len(overrideRulesJSON) == 0 {
		return settings, nil
	}
	if err := decodeSettingsJSON(overrideRulesJSON, &settings.OverrideRules); err != nil {
		return agentPlanSettingsSnapshot{}, fmt.Errorf("decode override rules: %w", err)
	}
	return settings, nil
}

func resolveHostSampleFrequencyTier(base string, nodeLabels []string, overrideRules centersettings.OverrideRules) string {
	labelSet := labelSet(nodeLabels)
	for _, rule := range overrideRules.NodeLabels {
		if _, ok := labelSet[rule.Label]; !ok {
			continue
		}
		if rule.Overrides.HostSampleFrequencyTier != nil {
			return *rule.Overrides.HostSampleFrequencyTier
		}
	}
	return base
}

func resolveProbeAssignmentFrequencyTier(base, probeKind, targetType string, targetLabels []string, overrideRules centersettings.OverrideRules) string {
	tier := base

	for _, rule := range overrideRules.TargetTypes {
		if rule.TargetType != targetType {
			continue
		}
		if overrideTier, ok := probeFrequencyTierOverride(probeKind, rule.Overrides.ProbeFrequencyDefaults); ok {
			tier = overrideTier
		}
		break
	}

	labelMatches := labelSet(targetLabels)
	for _, rule := range overrideRules.TargetLabels {
		if _, ok := labelMatches[rule.Label]; !ok {
			continue
		}
		if overrideTier, ok := probeFrequencyTierOverride(probeKind, rule.Overrides.ProbeFrequencyDefaults); ok {
			return overrideTier
		}
	}

	return tier
}

func probeFrequencyTierOverride(probeKind string, override *centersettings.ProbeFrequencyOverride) (string, bool) {
	if override == nil {
		return "", false
	}

	switch probeKind {
	case agentapi.ProbeKindTCP:
		if override.TCP != nil {
			return *override.TCP, true
		}
	case agentapi.ProbeKindHTTP:
		if override.HTTP != nil {
			return *override.HTTP, true
		}
	case agentapi.ProbeKindTLS:
		if override.TLS != nil {
			return *override.TLS, true
		}
	}

	return "", false
}

func labelSet(labels []string) map[string]struct{} {
	set := make(map[string]struct{}, len(labels))
	for _, label := range labels {
		set[label] = struct{}{}
	}
	return set
}

func legacyHostSampleFrequencyTier(labels []string) string {
	for _, label := range labels {
		if label == "核心" {
			return agentapi.FrequencyTier1m
		}
	}
	return agentapi.FrequencyTier5m
}
