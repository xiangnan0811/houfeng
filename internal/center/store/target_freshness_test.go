package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/targets"
)

func TestComputeTargetObservationFreshnessThresholdBoundaries(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, time.October, 8, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name          string
		frequencyTier string
		timeout       int
		now           time.Time
		wantState     string
		wantThreshold int
	}{
		{
			name:          "http one minute before equality",
			frequencyTier: targets.FrequencyTier1m,
			timeout:       5,
			now:           base.Add(3*time.Minute + 4*time.Second),
			wantState:     "fresh",
			wantThreshold: 185,
		},
		{
			name:          "http one minute at equality",
			frequencyTier: targets.FrequencyTier1m,
			timeout:       5,
			now:           base.Add(3*time.Minute + 5*time.Second),
			wantState:     "stale",
			wantThreshold: 185,
		},
		{
			name:          "five seconds keeps sixty second floor",
			frequencyTier: targets.FrequencyTier5s,
			timeout:       3,
			now:           base.Add(62 * time.Second),
			wantState:     "fresh",
			wantThreshold: 63,
		},
		{
			name:          "six hours uses three periods",
			frequencyTier: targets.FrequencyTier6h,
			timeout:       5,
			now:           base.Add(64805 * time.Second),
			wantState:     "stale",
			wantThreshold: 64805,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lastObserved := base
			got, err := computeTargetObservationFreshness(targetFreshnessTargetInput{
				TargetID:         "tg_threshold",
				LifecycleStatus:  targets.LifecycleActive,
				RunStatus:        targets.RunStatusEnabled,
				TargetType:       targets.TargetTypeService,
				HasObservation:   true,
				FreshnessResetAt: base.Add(-time.Hour),
				Probes: []targetFreshnessProbeInput{{
					ProbeItemID:      "pb_http",
					ProbeKind:        targets.ProbeKindHTTP,
					FrequencyTier:    tc.frequencyTier,
					TimeoutSeconds:   tc.timeout,
					FreshnessResetAt: base.Add(-time.Hour),
					LastLiveObserved: &lastObserved,
				}},
			}, centersettings.Default().OverrideRules, tc.now)
			if err != nil {
				t.Fatalf("computeTargetObservationFreshness() error = %v", err)
			}
			if got.State != tc.wantState {
				t.Fatalf("state = %q, want %q", got.State, tc.wantState)
			}
			if got.Probes[0].StaleAfterSeconds != tc.wantThreshold {
				t.Fatalf("stale_after_seconds = %d, want %d", got.Probes[0].StaleAfterSeconds, tc.wantThreshold)
			}
			if got.EvaluatedAt != tc.now {
				t.Fatalf("evaluated_at = %v, want injected now %v", got.EvaluatedAt, tc.now)
			}
		})
	}
}

func TestComputeTargetObservationFreshnessStatesAndResetEvidence(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, time.October, 8, 10, 0, 0, 0, time.UTC)
	oldObservation := base.Add(-2 * time.Minute)
	reset := base.Add(-time.Minute)
	cases := []struct {
		name       string
		runStatus  string
		historical bool
		probes     []targetFreshnessProbeInput
		wantState  string
		wantCounts [3]int
		wantLength int
	}{
		{
			name:       "no enabled probes is uncovered",
			runStatus:  targets.RunStatusEnabled,
			historical: false,
			wantState:  "uncovered",
			wantLength: 0,
		},
		{
			name:       "all pending is pending",
			runStatus:  targets.RunStatusEnabled,
			historical: true,
			probes: []targetFreshnessProbeInput{{
				ProbeItemID:      "pb_pending",
				ProbeKind:        targets.ProbeKindHTTP,
				FrequencyTier:    targets.FrequencyTier1m,
				TimeoutSeconds:   0,
				FreshnessResetAt: base,
			}},
			wantState:  "pending",
			wantCounts: [3]int{0, 1, 0},
			wantLength: 1,
		},
		{
			name:       "all stale is stale",
			runStatus:  targets.RunStatusEnabled,
			historical: true,
			probes: []targetFreshnessProbeInput{{
				ProbeItemID:      "pb_stale",
				ProbeKind:        targets.ProbeKindHTTP,
				FrequencyTier:    targets.FrequencyTier1m,
				TimeoutSeconds:   0,
				FreshnessResetAt: base.Add(-time.Hour),
				LastLiveObserved: new(base.Add(-time.Hour)),
			}},
			wantState:  "stale",
			wantCounts: [3]int{0, 0, 1},
			wantLength: 1,
		},
		{
			name:       "stale and fresh is partial",
			runStatus:  targets.RunStatusEnabled,
			historical: true,
			probes: []targetFreshnessProbeInput{
				{
					ProbeItemID:      "pb_stale",
					ProbeKind:        targets.ProbeKindHTTP,
					FrequencyTier:    targets.FrequencyTier1m,
					FreshnessResetAt: base.Add(-time.Hour),
					LastLiveObserved: new(base.Add(-time.Hour)),
				},
				{
					ProbeItemID:      "pb_fresh",
					ProbeKind:        targets.ProbeKindTLS,
					FrequencyTier:    targets.FrequencyTier6h,
					FreshnessResetAt: base.Add(-time.Hour),
					LastLiveObserved: new(base),
				},
			},
			wantState:  "partial",
			wantCounts: [3]int{1, 0, 1},
			wantLength: 2,
		},
		{
			name:       "reset makes old evidence pending",
			runStatus:  targets.RunStatusEnabled,
			historical: true,
			probes: []targetFreshnessProbeInput{{
				ProbeItemID:      "pb_reset",
				ProbeKind:        targets.ProbeKindHTTP,
				FrequencyTier:    targets.FrequencyTier1m,
				TimeoutSeconds:   5,
				FreshnessResetAt: reset,
				LastLiveObserved: &oldObservation,
			}},
			wantState:  "pending",
			wantCounts: [3]int{0, 1, 0},
			wantLength: 1,
		},
		{
			name:       "paused is inactive with config count",
			runStatus:  targets.RunStatusPaused,
			historical: true,
			probes: []targetFreshnessProbeInput{{
				ProbeItemID:      "pb_paused",
				ProbeKind:        targets.ProbeKindHTTP,
				FrequencyTier:    targets.FrequencyTier1m,
				FreshnessResetAt: base.Add(-time.Hour),
				LastLiveObserved: new(base.Add(-time.Hour)),
			}},
			wantState:  "inactive",
			wantCounts: [3]int{0, 0, 0},
			wantLength: 0,
		},
		{
			name:       "no history remains unobserved even after deadline",
			runStatus:  targets.RunStatusEnabled,
			historical: false,
			probes: []targetFreshnessProbeInput{{
				ProbeItemID:      "pb_unobserved",
				ProbeKind:        targets.ProbeKindHTTP,
				FrequencyTier:    targets.FrequencyTier1m,
				FreshnessResetAt: base.Add(-time.Hour),
			}},
			wantState:  "unobserved",
			wantCounts: [3]int{0, 0, 1},
			wantLength: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := computeTargetObservationFreshness(targetFreshnessTargetInput{
				TargetID:         "tg_states",
				LifecycleStatus:  targets.LifecycleActive,
				RunStatus:        tc.runStatus,
				TargetType:       targets.TargetTypeService,
				HasObservation:   tc.historical,
				FreshnessResetAt: base.Add(-time.Hour),
				Probes:           tc.probes,
			}, centersettings.Default().OverrideRules, base.Add(2*time.Minute))
			if err != nil {
				t.Fatalf("computeTargetObservationFreshness() error = %v", err)
			}
			if got.State != tc.wantState {
				t.Fatalf("state = %q, want %q", got.State, tc.wantState)
			}
			if got.EnabledProbeCount != len(tc.probes) {
				t.Fatalf("enabled_probe_count = %d, want %d", got.EnabledProbeCount, len(tc.probes))
			}
			if got.FreshProbeCount != tc.wantCounts[0] || got.PendingProbeCount != tc.wantCounts[1] || got.StaleProbeCount != tc.wantCounts[2] {
				t.Fatalf("counts = fresh:%d pending:%d stale:%d, want %#v", got.FreshProbeCount, got.PendingProbeCount, got.StaleProbeCount, tc.wantCounts)
			}
			if len(got.Probes) != tc.wantLength {
				t.Fatalf("len(probes) = %d, want %d", len(got.Probes), tc.wantLength)
			}
			if tc.name == "reset makes old evidence pending" && (got.Probes[0].LastObservedAt == nil || !got.Probes[0].LastObservedAt.Equal(oldObservation)) {
				t.Fatalf("last observed evidence = %v, want historical %v", got.Probes[0].LastObservedAt, oldObservation)
			}
		})
	}
}

func TestComputeTargetObservationFreshnessUsesTypeThenLabelOverrides(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, time.October, 8, 10, 0, 0, 0, time.UTC)
	typeTier := targets.FrequencyTier5m
	labelTier := targets.FrequencyTier1m
	rules := centersettings.OverrideRules{
		TargetTypes: []centersettings.TargetTypeOverrideRule{{
			TargetType: targets.TargetTypeService,
			Overrides: centersettings.SettingsOverrideFields{
				ProbeFrequencyDefaults: &centersettings.ProbeFrequencyOverride{HTTP: &typeTier},
			},
		}},
		TargetLabels: []centersettings.TargetLabelOverrideRule{{
			Label: "edge",
			Overrides: centersettings.SettingsOverrideFields{
				ProbeFrequencyDefaults: &centersettings.ProbeFrequencyOverride{HTTP: &labelTier},
			},
		}},
	}
	lastObserved := base
	got, err := computeTargetObservationFreshness(targetFreshnessTargetInput{
		LifecycleStatus:  targets.LifecycleActive,
		RunStatus:        targets.RunStatusEnabled,
		TargetType:       targets.TargetTypeService,
		TargetLabels:     []string{"edge"},
		HasObservation:   true,
		FreshnessResetAt: base.Add(-time.Hour),
		Probes: []targetFreshnessProbeInput{{
			ProbeItemID:      "pb_override",
			ProbeKind:        targets.ProbeKindHTTP,
			FrequencyTier:    targets.FrequencyTier6h,
			TimeoutSeconds:   5,
			FreshnessResetAt: base.Add(-time.Hour),
			LastLiveObserved: &lastObserved,
		}},
	}, rules, base.Add(185*time.Second-1))
	if err != nil {
		t.Fatalf("computeTargetObservationFreshness() error = %v", err)
	}
	if got.Probes[0].EffectiveFrequencyTier != labelTier || got.Probes[0].StaleAfterSeconds != 185 {
		t.Fatalf("probe freshness = %#v, want label override 1m/185s", got.Probes[0])
	}
}

func TestComputeTargetObservationFreshnessRejectsMalformedInputs(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, time.October, 8, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		probe targetFreshnessProbeInput
		want  string
	}{
		{
			name: "unknown frequency",
			probe: targetFreshnessProbeInput{
				ProbeItemID:   "pb_bad_frequency",
				ProbeKind:     targets.ProbeKindHTTP,
				FrequencyTier: "unknown",
			},
			want: "unknown frequency tier",
		},
		{
			name: "negative timeout",
			probe: targetFreshnessProbeInput{
				ProbeItemID:    "pb_bad_timeout",
				ProbeKind:      targets.ProbeKindHTTP,
				FrequencyTier:  targets.FrequencyTier1m,
				TimeoutSeconds: -1,
			},
			want: "must not be negative",
		},
		{
			name: "unknown probe kind",
			probe: targetFreshnessProbeInput{
				ProbeItemID:   "pb_bad_kind",
				ProbeKind:     "udp",
				FrequencyTier: targets.FrequencyTier1m,
			},
			want: "unknown probe kind",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := computeTargetObservationFreshness(targetFreshnessTargetInput{
				LifecycleStatus:  targets.LifecycleActive,
				RunStatus:        targets.RunStatusEnabled,
				TargetType:       targets.TargetTypeService,
				FreshnessResetAt: base,
				Probes:           []targetFreshnessProbeInput{tc.probe},
			}, centersettings.Default().OverrideRules, base)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}
func TestComputeTargetObservationFreshnessControlsAreInactive(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, time.October, 8, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name            string
		lifecycleStatus string
		runStatus       string
	}{
		{name: "maintenance", lifecycleStatus: targets.LifecycleActive, runStatus: targets.RunStatusMaintenance},
		{name: "retired", lifecycleStatus: targets.LifecycleRetired, runStatus: targets.RunStatusEnabled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := computeTargetObservationFreshness(targetFreshnessTargetInput{
				LifecycleStatus:  tc.lifecycleStatus,
				RunStatus:        tc.runStatus,
				TargetType:       targets.TargetTypeService,
				HasObservation:   true,
				FreshnessResetAt: base.Add(-time.Hour),
				Probes: []targetFreshnessProbeInput{{
					ProbeItemID:      "pb_control",
					ProbeKind:        targets.ProbeKindHTTP,
					FrequencyTier:    targets.FrequencyTier1m,
					FreshnessResetAt: base.Add(-time.Hour),
					LastLiveObserved: new(base),
				}},
			}, centersettings.Default().OverrideRules, base.Add(time.Minute))
			if err != nil {
				t.Fatalf("computeTargetObservationFreshness() error = %v", err)
			}
			if got.State != "inactive" || got.EnabledProbeCount != 1 || len(got.Probes) != 0 || got.FreshProbeCount != 0 || got.PendingProbeCount != 0 || got.StaleProbeCount != 0 {
				t.Fatalf("freshness = %#v, want inactive with config count and empty state counts", got)
			}
		})
	}
}

func TestFreshnessStaleAfterSeconds(t *testing.T) {
	t.Parallel()
	if got, err := freshnessStaleAfterSeconds(targets.FrequencyTier15m, 7); err != nil || got != 2707 {
		t.Fatalf("freshnessStaleAfterSeconds(15m,7) = %d, %v, want 2707,nil", got, err)
	}
}

func TestLoadTargetFreshnessNilAndEmptyIDSemantics(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.October, 8, 10, 0, 0, 0, time.UTC)

	t.Run("empty nonnil IDs skip all reads", func(t *testing.T) {
		queryer := &freshnessQueryerStub{}
		got, err := loadTargetFreshness(context.Background(), queryer, []string{}, now)
		if err != nil {
			t.Fatalf("loadTargetFreshness() error = %v", err)
		}
		if len(got) != 0 || queryer.queryRowCalls != 0 || queryer.queryCalls != 0 {
			t.Fatalf("result/calls = %#v/%d/%d, want empty and zero calls", got, queryer.queryRowCalls, queryer.queryCalls)
		}
	})

	t.Run("nil IDs use current visibility query", func(t *testing.T) {
		queryer := &freshnessQueryerStub{}
		got, err := loadTargetFreshness(context.Background(), queryer, nil, now)
		if err != nil {
			t.Fatalf("loadTargetFreshness() error = %v", err)
		}
		if len(got) != 0 || queryer.queryRowCalls != 1 || queryer.queryCalls != 1 {
			t.Fatalf("result/calls = %#v/%d/%d, want empty result and one settings plus one target query", got, queryer.queryRowCalls, queryer.queryCalls)
		}
		if !strings.Contains(queryer.lastSQL, "lifecycle_status = 'active'") {
			t.Fatalf("target SQL = %q, want current visibility predicate", queryer.lastSQL)
		}
	})
}

func TestLoadTargetFreshnessPropagatesOverrideValidationErrors(t *testing.T) {
	t.Parallel()
	queryer := &freshnessQueryerStub{
		queryRow: func(dest ...any) error {
			*(dest[0].(*[]byte)) = []byte(`{"target_types":[{"target_type":"service","overrides":{}}]}`)
			*(dest[1].(*[]byte)) = []byte(`{"heartbeat_interval_seconds":5,"stale_threshold_intervals":12,"sweep_interval_seconds":5,"notify_on_started":true,"notify_on_escalated":true,"notify_on_recovered":true}`)
			return nil
		},
	}
	_, err := loadTargetFreshness(context.Background(), queryer, nil, time.Date(2026, time.October, 8, 10, 0, 0, 0, time.UTC))
	if err == nil || !strings.Contains(err.Error(), "validate target freshness override rules") {
		t.Fatalf("loadTargetFreshness() error = %v, want override validation error", err)
	}
	if queryer.queryCalls != 0 {
		t.Fatalf("target query calls = %d, want 0 after settings validation failure", queryer.queryCalls)
	}
}

type freshnessQueryerStub struct {
	queryRow      func(dest ...any) error
	queryRowCalls int
	queryCalls    int
	lastSQL       string
}

func (q *freshnessQueryerStub) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	q.queryRowCalls++
	if q.queryRow == nil {
		return freshnessRow(func(...any) error { return pgx.ErrNoRows })
	}
	return freshnessRow(q.queryRow)
}

func (q *freshnessQueryerStub) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	q.queryCalls++
	q.lastSQL = sql
	return &fakeTargetRows{}, nil
}

type freshnessRow func(dest ...any) error

func (r freshnessRow) Scan(dest ...any) error {
	return r(dest...)
}
