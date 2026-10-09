package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/targets"
)

// targetFreshnessProjection is the read-model projection shared by target
// records and dashboard summaries. Group is kept beside the DTO because the
// dashboard aggregates the same current target set by its normalized group.
type targetFreshnessProjection struct {
	Freshness targets.TargetObservationFreshness
	Group     string
}

type targetFreshnessTargetInput struct {
	TargetID         string
	LifecycleStatus  string
	RunStatus        string
	TargetType       string
	TargetLabels     []string
	Group            string
	HasObservation   bool
	FreshnessResetAt time.Time
	Probes           []targetFreshnessProbeInput
}

type targetFreshnessProbeInput struct {
	ProbeItemID      string
	ProbeKind        string
	FrequencyTier    string
	TimeoutSeconds   int
	FreshnessResetAt time.Time
	LastLiveObserved *time.Time
}

const targetFreshnessSettingsSQL = `
	select override_rules, incident_defaults
	from center_settings
	where settings_id = $1`

const targetFreshnessSelectBaseSQL = `
	select
		t.target_id,
		t.lifecycle_status,
		t.run_status,
		t.target_type,
		t.labels,
		t."group",
		(t.last_success_at is not null or t.last_failure_at is not null) as has_observation,
		t.freshness_reset_at,
		p.probe_item_id,
		p.probe_kind,
		p.frequency_tier,
		p.timeout_seconds,
		p.freshness_reset_at,
		p.last_live_observed_at
	from targets t
	left join probe_items p
		on p.target_id = t.target_id
		and p.enabled = true
	where `

// loadTargetFreshness performs a fixed-size read: one settings row and one
// target/probe set query. A nil targetIDs means the current visibility set;
// an empty, non-nil slice deliberately means no rows.
func loadTargetFreshness(ctx context.Context, q dashboardQueryer, targetIDs []string, now time.Time) (map[string]targetFreshnessProjection, error) {
	if targetIDs != nil && len(targetIDs) == 0 {
		return map[string]targetFreshnessProjection{}, nil
	}
	if q == nil {
		return nil, errors.New("load target freshness: nil queryer")
	}

	overrideRules, err := loadTargetFreshnessOverrideRules(ctx, q)
	if err != nil {
		return nil, err
	}

	query := targetFreshnessSelectBaseSQL
	var args []any
	if targetIDs == nil {
		query += targetCurrentVisibilitySQL("t")
	} else {
		query += "t.target_id = any($1::text[])"
		args = append(args, targetIDs)
	}
	query += " order by t.target_id, p.probe_item_id"

	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query target freshness rows: %w", err)
	}
	defer rows.Close()

	grouped := make(map[string]targetFreshnessTargetInput)
	for rows.Next() {
		var (
			targetID, lifecycleStatus, runStatus, targetType, group string
			labels                                                  []string
			hasObservation                                          bool
			targetFreshnessResetAt                                  time.Time
			probeItemID                                             *string
			probeKind                                               *string
			frequencyTier                                           *string
			timeoutSeconds                                          *int
			probeFreshnessResetAt                                   *time.Time
			lastLiveObservedAt                                      *time.Time
		)
		if err := rows.Scan(
			&targetID,
			&lifecycleStatus,
			&runStatus,
			&targetType,
			&labels,
			&group,
			&hasObservation,
			&targetFreshnessResetAt,
			&probeItemID,
			&probeKind,
			&frequencyTier,
			&timeoutSeconds,
			&probeFreshnessResetAt,
			&lastLiveObservedAt,
		); err != nil {
			return nil, fmt.Errorf("scan target freshness row: %w", err)
		}

		target, ok := grouped[targetID]
		if !ok {
			target = targetFreshnessTargetInput{
				TargetID:         targetID,
				LifecycleStatus:  lifecycleStatus,
				RunStatus:        runStatus,
				TargetType:       targetType,
				TargetLabels:     append([]string(nil), labels...),
				Group:            group,
				HasObservation:   hasObservation,
				FreshnessResetAt: targetFreshnessResetAt,
				Probes:           make([]targetFreshnessProbeInput, 0),
			}
			grouped[targetID] = target
		}

		if probeItemID == nil {
			continue
		}
		if probeKind == nil || frequencyTier == nil || timeoutSeconds == nil || probeFreshnessResetAt == nil {
			return nil, fmt.Errorf("scan target freshness row for %q: enabled probe %q has incomplete configuration", targetID, *probeItemID)
		}
		target.Probes = append(target.Probes, targetFreshnessProbeInput{
			ProbeItemID:      *probeItemID,
			ProbeKind:        *probeKind,
			FrequencyTier:    *frequencyTier,
			TimeoutSeconds:   *timeoutSeconds,
			FreshnessResetAt: *probeFreshnessResetAt,
			LastLiveObserved: cloneFreshnessTimePtr(lastLiveObservedAt),
		})
		grouped[targetID] = target
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate target freshness rows: %w", err)
	}

	projections := make(map[string]targetFreshnessProjection, len(grouped))
	for targetID, input := range grouped {
		freshness, err := computeTargetObservationFreshness(input, overrideRules, now)
		if err != nil {
			return nil, fmt.Errorf("compute target freshness for %q: %w", targetID, err)
		}
		projections[targetID] = targetFreshnessProjection{Freshness: freshness, Group: input.Group}
	}
	return projections, nil
}

func loadTargetFreshnessOverrideRules(ctx context.Context, q dashboardQueryer) (centersettings.OverrideRules, error) {
	var (
		overrideRulesRaw    []byte
		incidentDefaultsRaw []byte
	)
	err := q.QueryRow(ctx, targetFreshnessSettingsSQL, centersettings.SingletonID).Scan(
		&overrideRulesRaw,
		&incidentDefaultsRaw,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return centersettings.Default().OverrideRules, nil
	}
	if err != nil {
		return centersettings.OverrideRules{}, fmt.Errorf("query target freshness settings: %w", err)
	}

	defaults := centersettings.Default()
	if len(incidentDefaultsRaw) > 0 {
		var incidentDefaults centersettings.IncidentDefaults
		if err := decodeSettingsJSON(incidentDefaultsRaw, &incidentDefaults); err != nil {
			return centersettings.OverrideRules{}, fmt.Errorf("decode target freshness incident defaults: %w", err)
		}
		defaults.IncidentDefaults = incidentDefaults
	}
	if len(overrideRulesRaw) > 0 {
		if err := decodeSettingsJSON(overrideRulesRaw, &defaults.OverrideRules); err != nil {
			return centersettings.OverrideRules{}, fmt.Errorf("decode target freshness override rules: %w", err)
		}
	}
	validated, err := centersettings.Validate(defaults)
	if err != nil {
		return centersettings.OverrideRules{}, fmt.Errorf("validate target freshness override rules against stored incident defaults: %w", err)
	}
	return validated.OverrideRules, nil
}

func computeTargetObservationFreshness(input targetFreshnessTargetInput, overrideRules centersettings.OverrideRules, now time.Time) (targets.TargetObservationFreshness, error) {
	now = now.UTC()
	freshness := targets.TargetObservationFreshness{
		State:       "unobserved",
		EvaluatedAt: now,
		Probes:      make([]targets.ProbeObservationFreshness, 0, len(input.Probes)),
	}
	freshness.EnabledProbeCount = len(input.Probes)

	for _, probe := range input.Probes {
		if !targets.IsValidProbeKind(probe.ProbeKind) {
			return targets.TargetObservationFreshness{}, fmt.Errorf("probe %q has unknown probe kind %q", probe.ProbeItemID, probe.ProbeKind)
		}
		effectiveTier := resolveProbeAssignmentFrequencyTier(
			probe.FrequencyTier,
			probe.ProbeKind,
			input.TargetType,
			input.TargetLabels,
			overrideRules,
		)
		staleAfterSeconds, err := freshnessStaleAfterSeconds(effectiveTier, probe.TimeoutSeconds)
		if err != nil {
			return targets.TargetObservationFreshness{}, fmt.Errorf("probe %q: %w", probe.ProbeItemID, err)
		}
		expectedSince := input.FreshnessResetAt.UTC()
		probeResetAt := probe.FreshnessResetAt.UTC()
		if probeResetAt.After(expectedSince) {
			expectedSince = probeResetAt
		}
		lastObservedAt := cloneFreshnessTimePtr(probe.LastLiveObserved)
		currentEvidence := lastObservedAt != nil && !lastObservedAt.Before(expectedSince)
		deadlineAt := expectedSince.Add(time.Duration(staleAfterSeconds) * time.Second)
		if currentEvidence {
			deadlineAt = lastObservedAt.Add(time.Duration(staleAfterSeconds) * time.Second)
		}
		state := "pending"
		if !now.Before(deadlineAt) {
			state = "stale"
		} else if currentEvidence {
			state = "fresh"
		}
		freshness.Probes = append(freshness.Probes, targets.ProbeObservationFreshness{
			ProbeItemID:            probe.ProbeItemID,
			State:                  state,
			EffectiveFrequencyTier: effectiveTier,
			StaleAfterSeconds:      staleAfterSeconds,
			LastObservedAt:         lastObservedAt,
			ExpectedSince:          expectedSince,
			DeadlineAt:             deadlineAt,
		})
		switch state {
		case "fresh":
			freshness.FreshProbeCount++
		case "pending":
			freshness.PendingProbeCount++
		case "stale":
			freshness.StaleProbeCount++
		}
	}

	sort.Slice(freshness.Probes, func(i, j int) bool {
		return freshness.Probes[i].ProbeItemID < freshness.Probes[j].ProbeItemID
	})
	if input.LifecycleStatus == targets.LifecycleRetired || input.RunStatus == targets.RunStatusPaused || input.RunStatus == targets.RunStatusMaintenance || input.RunStatus == targets.RunStatusArchived {
		freshness.State = "inactive"
		freshness.Probes = freshness.Probes[:0]
		freshness.FreshProbeCount = 0
		freshness.PendingProbeCount = 0
		freshness.StaleProbeCount = 0
		return freshness, nil
	}
	if freshness.EnabledProbeCount == 0 {
		freshness.State = "uncovered"
		return freshness, nil
	}
	if !input.HasObservation {
		freshness.State = "unobserved"
		return freshness, nil
	}
	switch {
	case freshness.StaleProbeCount == freshness.EnabledProbeCount:
		freshness.State = "stale"
	case freshness.FreshProbeCount == freshness.EnabledProbeCount:
		freshness.State = "fresh"
	case freshness.PendingProbeCount == freshness.EnabledProbeCount:
		freshness.State = "pending"
	default:
		freshness.State = "partial"
	}
	return freshness, nil
}

func freshnessStaleAfterSeconds(effectiveTier string, timeoutSeconds int) (int, error) {
	if timeoutSeconds < 0 {
		return 0, fmt.Errorf("timeout seconds must not be negative: %d", timeoutSeconds)
	}
	frequencySeconds, err := frequencyTierSeconds(effectiveTier)
	if err != nil {
		return 0, err
	}
	base := int64(frequencySeconds) * 3
	if base < 60 {
		base = 60
	}
	maxInt := int64(^uint(0) >> 1)
	if base > maxInt-int64(timeoutSeconds) {
		return 0, fmt.Errorf("stale threshold overflows integer seconds")
	}
	return int(base) + timeoutSeconds, nil
}

func frequencyTierSeconds(tier string) (int, error) {
	switch tier {
	case targets.FrequencyTier5s:
		return 5, nil
	case targets.FrequencyTier1m:
		return 60, nil
	case targets.FrequencyTier5m:
		return 5 * 60, nil
	case targets.FrequencyTier15m:
		return 15 * 60, nil
	case targets.FrequencyTier6h:
		return 6 * 60 * 60, nil
	default:
		return 0, fmt.Errorf("unknown frequency tier %q", tier)
	}
}

func cloneFreshnessTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}

func hydrateTargetRecord(ctx context.Context, q dashboardQueryer, record targets.TargetRecord, now time.Time) (targets.TargetRecord, error) {
	projections, err := loadTargetFreshness(ctx, q, []string{record.TargetID}, now)
	if err != nil {
		return targets.TargetRecord{}, err
	}
	projection, ok := projections[record.TargetID]
	if !ok {
		return targets.TargetRecord{}, fmt.Errorf("%w: target freshness projection missing for %q", targets.ErrTargetNotFound, record.TargetID)
	}
	record.ObservationFreshness = projection.Freshness
	return record, nil
}

func hydrateTargetRecords(ctx context.Context, q dashboardQueryer, records []targets.TargetRecord, now time.Time) ([]targets.TargetRecord, error) {
	if len(records) == 0 {
		return records, nil
	}
	targetIDs := make([]string, 0, len(records))
	for _, record := range records {
		targetIDs = append(targetIDs, record.TargetID)
	}
	projections, err := loadTargetFreshness(ctx, q, targetIDs, now)
	if err != nil {
		return nil, err
	}
	hydrated := make([]targets.TargetRecord, 0, len(records))
	for _, record := range records {
		projection, ok := projections[record.TargetID]
		if !ok {
			// A target deleted or becoming invisible between the list query and
			// this batch read is omitted rather than returned with zero freshness.
			continue
		}
		record.ObservationFreshness = projection.Freshness
		hydrated = append(hydrated, record)
	}
	return hydrated, nil
}
