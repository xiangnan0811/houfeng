package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/monitoringinstances"
	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/targets"
	"houfeng/internal/contracts/agentapi"
)

func TestBuildSyncPlanUsesPersistedSettings(t *testing.T) {
	t.Parallel()

	var seenStatuses []string
	var seenLabels []string
	repo := &PostgresAgentPlanRepository{db: fakeAgentPlanQueryer{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			if sql != selectAgentPlanMonitoringInstanceLabelsSQL {
				return fakeAgentPlanRow{scan: func(dest ...any) error { return errors.New("unexpected QueryRow") }}
			}
			if args[0] != "mi_001" {
				return fakeAgentPlanRow{scan: func(dest ...any) error { return errors.New("unexpected monitoringInstance id") }}
			}
			return fakeAgentPlanRow{scan: func(dest ...any) error {
				*(dest[0].(*[]string)) = []string{"edge", "核心"}
				*(dest[1].(*string)) = monitoringinstances.LifecycleInUse
				*(dest[2].(*string)) = monitoringinstances.MonitoringEnabled
				*(dest[3].(*string)) = agentapi.FrequencyTier15m
				*(dest[4].(*[]byte)) = mustMarshalAgentPlanJSON(t, centersettings.OverrideRules{
					MonitoringInstanceLabels: []centersettings.MonitoringInstanceLabelOverrideRule{},
					TargetTypes:              []centersettings.TargetTypeOverrideRule{},
					TargetLabels:             []centersettings.TargetLabelOverrideRule{},
				})
				*(dest[5].(*bool)) = true
				return nil
			}}
		},
		query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			if sql != selectAgentPlanAssignmentsSQL {
				return nil, errors.New("unexpected Query")
			}
			seenStatuses = append(seenStatuses, args[0].([]string)...)
			seenLabels = append(seenLabels, args[1].([]string)...)
			return &fakeAgentPlanRows{rows: []fakeAgentPlanScan{
				{scan: func(dest ...any) error {
					*(dest[0].(*string)) = "tg_enabled"
					*(dest[1].(*string)) = "api.example.test"
					port := 443
					*(dest[2].(**int)) = &port
					*(dest[3].(*string)) = targets.RunStatusEnabled
					*(dest[4].(*string)) = "pb_http"
					*(dest[5].(*string)) = agentapi.ProbeKindHTTP
					*(dest[6].(*string)) = agentapi.FrequencyTier1m
					*(dest[7].(*int)) = 5
					*(dest[8].(*[]byte)) = []byte(`{"path":"/healthz"}`)
					return nil
				}},
				{scan: func(dest ...any) error {
					*(dest[0].(*string)) = "tg_maint"
					*(dest[1].(*string)) = "cache.example.test"
					*(dest[2].(**int)) = nil
					*(dest[3].(*string)) = targets.RunStatusMaintenance
					*(dest[4].(*string)) = "pb_tcp"
					*(dest[5].(*string)) = agentapi.ProbeKindTCP
					*(dest[6].(*string)) = agentapi.FrequencyTier5m
					*(dest[7].(*int)) = 3
					*(dest[8].(*[]byte)) = []byte(`{"port":11211}`)
					return nil
				}},
			}}, nil
		},
	}}

	plan, err := repo.BuildSyncPlan(context.Background(), "mi_001")
	if err != nil {
		t.Fatalf("BuildSyncPlan() error = %v", err)
	}
	if plan.HostSampleFrequencyTier != agentapi.FrequencyTier15m {
		t.Fatalf("HostSampleFrequencyTier = %q, want %q", plan.HostSampleFrequencyTier, agentapi.FrequencyTier15m)
	}
	if plan.IPQualityPlan == nil {
		t.Fatal("IPQualityPlan = nil, want persisted default plan")
	}
	if plan.IPQualityPlan.FrequencySeconds != centersettings.Default().IPQuality.FrequencySeconds {
		t.Fatalf("IPQualityPlan.FrequencySeconds = %d, want %d", plan.IPQualityPlan.FrequencySeconds, centersettings.Default().IPQuality.FrequencySeconds)
	}
	if len(plan.ProbeAssignments) != 2 {
		t.Fatalf("len(ProbeAssignments) = %d, want 2", len(plan.ProbeAssignments))
	}
	if plan.ProbeAssignments[0].FrequencyTier != agentapi.FrequencyTier1m {
		t.Fatalf("ProbeAssignments[0].FrequencyTier = %q, want %q", plan.ProbeAssignments[0].FrequencyTier, agentapi.FrequencyTier1m)
	}
	if plan.ProbeAssignments[1].MaintenanceContext != true {
		t.Fatalf("MaintenanceContext = %v, want true", plan.ProbeAssignments[1].MaintenanceContext)
	}
	if plan.ProbeAssignments[1].FrequencyTier != agentapi.FrequencyTier5m {
		t.Fatalf("ProbeAssignments[1].FrequencyTier = %q, want %q", plan.ProbeAssignments[1].FrequencyTier, agentapi.FrequencyTier5m)
	}
	if plan.ProbeAssignments[1].TargetBasePort != nil {
		t.Fatalf("TargetBasePort = %v, want nil", *plan.ProbeAssignments[1].TargetBasePort)
	}
	if string(plan.ProbeAssignments[0].Config) != `{"path":"/healthz"}` {
		t.Fatalf("Config = %s, want %s", plan.ProbeAssignments[0].Config, `{"path":"/healthz"}`)
	}
	if len(seenStatuses) != 2 || seenStatuses[0] != targets.RunStatusEnabled || seenStatuses[1] != targets.RunStatusMaintenance {
		t.Fatalf("statuses = %#v, want enabled+maintenance", seenStatuses)
	}
	if len(seenLabels) != 2 || seenLabels[0] != "edge" || seenLabels[1] != "核心" {
		t.Fatalf("labels = %#v, want edge+核心", seenLabels)
	}
}

func TestBuildSyncPlanIncludesIPQualityPlanFromSettings(t *testing.T) {
	t.Parallel()

	repo := &PostgresAgentPlanRepository{db: fakeAgentPlanQueryer{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			if sql != selectAgentPlanMonitoringInstanceLabelsSQL {
				return fakeAgentPlanRow{scan: func(dest ...any) error { return errors.New("unexpected QueryRow") }}
			}
			if args[0] != "mi_001" {
				return fakeAgentPlanRow{scan: func(dest ...any) error { return errors.New("unexpected monitoringInstance id") }}
			}
			return fakeAgentPlanRow{scan: func(dest ...any) error {
				*(dest[0].(*[]string)) = []string{"edge"}
				*(dest[1].(*string)) = monitoringinstances.LifecycleInUse
				*(dest[2].(*string)) = monitoringinstances.MonitoringEnabled
				*(dest[3].(*string)) = agentapi.FrequencyTier15m
				*(dest[4].(*[]byte)) = mustMarshalAgentPlanJSON(t, centersettings.OverrideRules{})
				*(dest[5].(*bool)) = true
				*(dest[7].(*[]byte)) = mustMarshalAgentPlanJSON(t, centersettings.IPQualitySettings{
					Enabled:          true,
					FrequencySeconds: 259200,
					TimeoutSeconds:   20,
					Services:         []string{"netflix", "chatgpt"},
				})
				return nil
			}}
		},
	}}

	plan, err := repo.BuildSyncPlan(context.Background(), "mi_001")
	if err != nil {
		t.Fatalf("BuildSyncPlan() error = %v", err)
	}
	if plan.IPQualityPlan == nil {
		t.Fatal("IPQualityPlan = nil, want non-nil")
	}
	if !plan.IPQualityPlan.Enabled {
		t.Fatal("IPQualityPlan.Enabled = false, want true")
	}
	if plan.IPQualityPlan.FrequencySeconds != 259200 {
		t.Fatalf("IPQualityPlan.FrequencySeconds = %d, want 259200", plan.IPQualityPlan.FrequencySeconds)
	}
	if plan.IPQualityPlan.TimeoutSeconds != 20 {
		t.Fatalf("IPQualityPlan.TimeoutSeconds = %d, want 20", plan.IPQualityPlan.TimeoutSeconds)
	}
	if len(plan.IPQualityPlan.Services) != 2 || plan.IPQualityPlan.Services[1] != "chatgpt" {
		t.Fatalf("IPQualityPlan.Services = %#v, want netflix/chatgpt", plan.IPQualityPlan.Services)
	}
}

func TestBuildSyncPlanAppliesSettingsOverrides(t *testing.T) {
	t.Parallel()

	hostTier1m := agentapi.FrequencyTier1m
	httpTier15m := agentapi.FrequencyTier15m
	tlsTier6h := agentapi.FrequencyTier6h
	tlsTier5m := agentapi.FrequencyTier5m

	repo := &PostgresAgentPlanRepository{db: fakeAgentPlanQueryer{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			if sql != selectAgentPlanMonitoringInstanceLabelsSQL {
				return fakeAgentPlanRow{scan: func(dest ...any) error { return errors.New("unexpected QueryRow") }}
			}
			if args[0] != "mi_001" {
				return fakeAgentPlanRow{scan: func(dest ...any) error { return errors.New("unexpected monitoringInstance id") }}
			}
			return fakeAgentPlanRow{scan: func(dest ...any) error {
				*(dest[0].(*[]string)) = []string{"edge", "核心"}
				*(dest[1].(*string)) = monitoringinstances.LifecycleInUse
				*(dest[2].(*string)) = monitoringinstances.MonitoringEnabled
				*(dest[3].(*string)) = agentapi.FrequencyTier15m
				*(dest[4].(*[]byte)) = mustMarshalAgentPlanJSON(t, centersettings.OverrideRules{
					MonitoringInstanceLabels: []centersettings.MonitoringInstanceLabelOverrideRule{{
						Label: " 核心 ",
						Overrides: centersettings.SettingsOverrideFields{
							HostSampleFrequencyTier: &hostTier1m,
						},
					}},
					TargetTypes: []centersettings.TargetTypeOverrideRule{{
						TargetType: targets.TargetTypeService,
						Overrides: centersettings.SettingsOverrideFields{
							ProbeFrequencyDefaults: &centersettings.ProbeFrequencyOverride{
								HTTP: &httpTier15m,
								TLS:  &tlsTier6h,
							},
						},
					}},
					TargetLabels: []centersettings.TargetLabelOverrideRule{{
						Label: "slow-lane",
						Overrides: centersettings.SettingsOverrideFields{
							ProbeFrequencyDefaults: &centersettings.ProbeFrequencyOverride{
								TLS: &tlsTier5m,
							},
						},
					}},
				})
				*(dest[5].(*bool)) = true
				return nil
			}}
		},
		query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			if sql != selectAgentPlanAssignmentsSQL {
				return nil, errors.New("unexpected Query")
			}
			return &fakeAgentPlanRows{rows: []fakeAgentPlanScan{
				{scan: func(dest ...any) error {
					*(dest[0].(*string)) = "tg_http"
					*(dest[1].(*string)) = "api.example.test"
					port := 443
					*(dest[2].(**int)) = &port
					*(dest[3].(*string)) = targets.RunStatusEnabled
					*(dest[4].(*string)) = "pb_http"
					*(dest[5].(*string)) = agentapi.ProbeKindHTTP
					*(dest[6].(*string)) = agentapi.FrequencyTier5m
					*(dest[7].(*int)) = 5
					*(dest[8].(*[]byte)) = []byte(`{"path":"/healthz"}`)
					if len(dest) > 9 {
						*(dest[9].(*string)) = targets.TargetTypeService
					}
					if len(dest) > 10 {
						*(dest[10].(*[]string)) = []string{"api"}
					}
					return nil
				}},
				{scan: func(dest ...any) error {
					*(dest[0].(*string)) = "tg_tls"
					*(dest[1].(*string)) = "tls.example.test"
					port := 8443
					*(dest[2].(**int)) = &port
					*(dest[3].(*string)) = targets.RunStatusEnabled
					*(dest[4].(*string)) = "pb_tls"
					*(dest[5].(*string)) = agentapi.ProbeKindTLS
					*(dest[6].(*string)) = agentapi.FrequencyTier15m
					*(dest[7].(*int)) = 10
					*(dest[8].(*[]byte)) = []byte(`{"server_name":"tls.example.test"}`)
					if len(dest) > 9 {
						*(dest[9].(*string)) = targets.TargetTypeService
					}
					if len(dest) > 10 {
						*(dest[10].(*[]string)) = []string{"slow-lane"}
					}
					return nil
				}},
			}}, nil
		},
	}}

	plan, err := repo.BuildSyncPlan(context.Background(), "mi_001")
	if err != nil {
		t.Fatalf("BuildSyncPlan() error = %v", err)
	}

	if plan.HostSampleFrequencyTier != agentapi.FrequencyTier1m {
		t.Fatalf("HostSampleFrequencyTier = %q, want %q", plan.HostSampleFrequencyTier, agentapi.FrequencyTier1m)
	}
	if len(plan.ProbeAssignments) != 2 {
		t.Fatalf("len(ProbeAssignments) = %d, want 2", len(plan.ProbeAssignments))
	}
	if plan.ProbeAssignments[0].FrequencyTier != agentapi.FrequencyTier15m {
		t.Fatalf("ProbeAssignments[0].FrequencyTier = %q, want %q", plan.ProbeAssignments[0].FrequencyTier, agentapi.FrequencyTier15m)
	}
	if plan.ProbeAssignments[1].FrequencyTier != agentapi.FrequencyTier5m {
		t.Fatalf("ProbeAssignments[1].FrequencyTier = %q, want %q", plan.ProbeAssignments[1].FrequencyTier, agentapi.FrequencyTier5m)
	}
}

func TestBuildSyncPlanReturnsAssignmentsWhenSettingsRowMissing(t *testing.T) {
	t.Parallel()

	repo := &PostgresAgentPlanRepository{db: fakeAgentPlanQueryer{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			if sql != selectAgentPlanMonitoringInstanceLabelsSQL {
				return fakeAgentPlanRow{scan: func(dest ...any) error { return errors.New("unexpected QueryRow") }}
			}
			return fakeAgentPlanRow{scan: func(dest ...any) error {
				*(dest[0].(*[]string)) = []string{"edge", "核心"}
				*(dest[1].(*string)) = monitoringinstances.LifecycleInUse
				*(dest[2].(*string)) = monitoringinstances.MonitoringEnabled
				*(dest[3].(*string)) = agentapi.FrequencyTier5m
				*(dest[4].(*[]byte)) = mustMarshalAgentPlanJSON(t, centersettings.OverrideRules{
					MonitoringInstanceLabels: []centersettings.MonitoringInstanceLabelOverrideRule{},
					TargetTypes:              []centersettings.TargetTypeOverrideRule{},
					TargetLabels:             []centersettings.TargetLabelOverrideRule{},
				})
				*(dest[5].(*bool)) = false
				return nil
			}}
		},
		query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			if sql != selectAgentPlanAssignmentsSQL {
				return nil, errors.New("unexpected Query")
			}
			return &fakeAgentPlanRows{rows: []fakeAgentPlanScan{
				{scan: func(dest ...any) error {
					*(dest[0].(*string)) = "tg_enabled"
					*(dest[1].(*string)) = "api.example.test"
					port := 443
					*(dest[2].(**int)) = &port
					*(dest[3].(*string)) = targets.RunStatusEnabled
					*(dest[4].(*string)) = "pb_http"
					*(dest[5].(*string)) = agentapi.ProbeKindHTTP
					*(dest[6].(*string)) = agentapi.FrequencyTier1m
					*(dest[7].(*int)) = 5
					*(dest[8].(*[]byte)) = []byte(`{"path":"/healthz"}`)
					if len(dest) > 9 {
						*(dest[9].(*string)) = targets.TargetTypeService
					}
					if len(dest) > 10 {
						*(dest[10].(*[]string)) = []string{"api"}
					}
					return nil
				}},
			}}, nil
		},
	}}

	plan, err := repo.BuildSyncPlan(context.Background(), "mi_001")
	if err != nil {
		t.Fatalf("BuildSyncPlan() error = %v", err)
	}
	if plan.HostSampleFrequencyTier != agentapi.FrequencyTier5s {
		t.Fatalf("HostSampleFrequencyTier = %q, want %q", plan.HostSampleFrequencyTier, agentapi.FrequencyTier5s)
	}
	if len(plan.ProbeAssignments) != 1 {
		t.Fatalf("len(ProbeAssignments) = %d, want 1", len(plan.ProbeAssignments))
	}
}

func TestBuildSyncPlanReturnsMonitoringInstanceNotFound(t *testing.T) {
	t.Parallel()

	repo := &PostgresAgentPlanRepository{db: fakeAgentPlanQueryer{
		queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return fakeAgentPlanRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
		},
	}}

	_, err := repo.BuildSyncPlan(context.Background(), "mi_missing")
	if !errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) {
		t.Fatalf("BuildSyncPlan() error = %v, want ErrMonitoringInstanceNotFound", err)
	}
}

func TestBuildSyncPlanReturnsDefaultCadenceAndNoAssignmentsForLabelLessMonitoringInstance(t *testing.T) {
	t.Parallel()

	repo := &PostgresAgentPlanRepository{db: fakeAgentPlanQueryer{
		queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return fakeAgentPlanRow{scan: func(dest ...any) error {
				*(dest[0].(*[]string)) = nil
				*(dest[1].(*string)) = monitoringinstances.LifecycleInUse
				*(dest[2].(*string)) = monitoringinstances.MonitoringEnabled
				return nil
			}}
		},
	}}

	plan, err := repo.BuildSyncPlan(context.Background(), "mi_001")
	if err != nil {
		t.Fatalf("BuildSyncPlan() error = %v", err)
	}
	if plan.HostSampleFrequencyTier != agentapi.FrequencyTier5s {
		t.Fatalf("HostSampleFrequencyTier = %q, want %q", plan.HostSampleFrequencyTier, agentapi.FrequencyTier5s)
	}
	if len(plan.ProbeAssignments) != 0 {
		t.Fatalf("len(ProbeAssignments) = %d, want 0", len(plan.ProbeAssignments))
	}
}

func TestBuildSyncPlanSQLIncludesEnabledAndLabelOverlapFilters(t *testing.T) {
	t.Parallel()

	if !json.Valid([]byte(`{"probe_kind":"http"}`)) {
		t.Fatal("json sanity check failed")
	}
	if !containsSQL([]string{selectAgentPlanAssignmentsSQL}, "p.enabled = true") {
		t.Fatalf("selectAgentPlanAssignmentsSQL = %q, want enabled filter", selectAgentPlanAssignmentsSQL)
	}
	if !containsSQL([]string{selectAgentPlanAssignmentsSQL}, "t.execution_monitoring_instance_labels && $2") {
		t.Fatalf("selectAgentPlanAssignmentsSQL = %q, want label overlap filter", selectAgentPlanAssignmentsSQL)
	}
	if !containsSQL([]string{selectAgentPlanAssignmentsSQL}, "t.run_status = any($1)") {
		t.Fatalf("selectAgentPlanAssignmentsSQL = %q, want run_status filter", selectAgentPlanAssignmentsSQL)
	}
}
func TestResolveAgentPlanSettingsUsesCanonicalDefaultsWhenRowMissing(t *testing.T) {
	t.Parallel()

	got, err := resolveAgentPlanSettings(false, []string{"core"}, "", nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("resolveAgentPlanSettings() error = %v", err)
	}
	defaults := centersettings.Default()
	if got.HostSampleFrequencyTier != agentapi.FrequencyTier5s {
		t.Fatalf("HostSampleFrequencyTier = %q, want %q", got.HostSampleFrequencyTier, agentapi.FrequencyTier5s)
	}
	if got.ProbeFrequencyDefaults != defaults.ProbeFrequencyDefaults {
		t.Fatalf("ProbeFrequencyDefaults = %#v, want %#v", got.ProbeFrequencyDefaults, defaults.ProbeFrequencyDefaults)
	}
	if got.IncidentDefaults != defaults.IncidentDefaults {
		t.Fatalf("IncidentDefaults = %#v, want %#v", got.IncidentDefaults, defaults.IncidentDefaults)
	}
	if len(got.OverrideRules.MonitoringInstanceLabels) != 0 ||
		len(got.OverrideRules.TargetTypes) != 0 ||
		len(got.OverrideRules.TargetLabels) != 0 {
		t.Fatalf("OverrideRules = %#v, want canonical empty rule arrays", got.OverrideRules)
	}
	if got.IPQuality.FrequencySeconds != defaults.IPQuality.FrequencySeconds {
		t.Fatalf("IPQuality.FrequencySeconds = %d, want %d", got.IPQuality.FrequencySeconds, defaults.IPQuality.FrequencySeconds)
	}
}

func TestBuildSyncPlanAcceptsOverrideAgainstPersistedIncidentDefaults(t *testing.T) {
	t.Parallel()

	defaults := centersettings.Default()
	defaults.IncidentDefaults.CPUWarningPct = 50
	defaults.IncidentDefaults.CPUAlertPct = 60
	defaults.IncidentDefaults.CPUCriticalPct = 70
	overrideCritical := 75
	overrideRules := centersettings.OverrideRules{
		MonitoringInstanceLabels: []centersettings.MonitoringInstanceLabelOverrideRule{{
			Label: "core",
			Overrides: centersettings.SettingsOverrideFields{
				IncidentDefaults: &centersettings.IncidentDefaultsOverride{
					CPUCriticalPct: &overrideCritical,
				},
			},
		}},
	}

	repo := &PostgresAgentPlanRepository{db: fakeAgentPlanQueryer{
		queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
			if sql != selectAgentPlanMonitoringInstanceLabelsSQL {
				return fakeAgentPlanRow{scan: func(dest ...any) error { return errors.New("unexpected QueryRow") }}
			}
			return fakeAgentPlanRow{scan: func(dest ...any) error {
				*(dest[0].(*[]string)) = []string{"core"}
				*(dest[1].(*string)) = monitoringinstances.LifecycleInUse
				*(dest[2].(*string)) = monitoringinstances.MonitoringEnabled
				*(dest[3].(*string)) = agentapi.FrequencyTier5s
				*(dest[4].(*[]byte)) = mustMarshalAgentPlanJSON(t, overrideRules)
				*(dest[5].(*bool)) = true
				*(dest[6].(*bool)) = false
				*(dest[7].(*[]byte)) = mustMarshalAgentPlanJSON(t, defaults.IPQuality)
				*(dest[8].(*[]byte)) = mustMarshalAgentPlanJSON(t, defaults.ProbeFrequencyDefaults)
				*(dest[9].(*[]byte)) = mustMarshalAgentPlanJSON(t, defaults.IncidentDefaults)
				return nil
			}}
		},
		query: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			if sql != selectAgentPlanAssignmentsSQL {
				return nil, errors.New("unexpected Query")
			}
			return &fakeAgentPlanRows{}, nil
		},
	}}

	if _, err := repo.BuildSyncPlan(context.Background(), "mi_001"); err != nil {
		t.Fatalf("BuildSyncPlan() error = %v, want valid persisted-policy snapshot", err)
	}
}
func TestBuildSyncPlanRejectsMalformedPersistedPolicy(t *testing.T) {
	t.Parallel()

	defaults := centersettings.Default()
	defaultOverrideRules := mustMarshalAgentPlanJSON(t, defaults.OverrideRules)
	defaultProbeDefaults := mustMarshalAgentPlanJSON(t, defaults.ProbeFrequencyDefaults)
	defaultIncidentDefaults := mustMarshalAgentPlanJSON(t, defaults.IncidentDefaults)
	testCases := []struct {
		name                string
		overrideJSON        []byte
		probeJSON           []byte
		incidentJSON        []byte
		wantInvalidSettings bool
	}{
		{
			name:         "malformed override json",
			overrideJSON: []byte(`{"target_types":`),
			probeJSON:    defaultProbeDefaults,
			incidentJSON: defaultIncidentDefaults,
		},
		{
			name:         "unknown incident default field",
			overrideJSON: defaultOverrideRules,
			probeJSON:    defaultProbeDefaults,
			incidentJSON: []byte(`{"unexpected":true}`),
		},
		{
			name:                "invalid persisted probe tier",
			overrideJSON:        defaultOverrideRules,
			probeJSON:           []byte(`{"tcp":"30s","http":"5s","tls":"6h"}`),
			incidentJSON:        defaultIncidentDefaults,
			wantInvalidSettings: true,
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			repo := &PostgresAgentPlanRepository{db: fakeAgentPlanQueryer{
				queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
					if sql != selectAgentPlanMonitoringInstanceLabelsSQL {
						return fakeAgentPlanRow{scan: func(dest ...any) error { return errors.New("unexpected QueryRow") }}
					}
					return fakeAgentPlanRow{scan: func(dest ...any) error {
						*(dest[0].(*[]string)) = []string{"core"}
						*(dest[1].(*string)) = monitoringinstances.LifecycleInUse
						*(dest[2].(*string)) = monitoringinstances.MonitoringEnabled
						*(dest[3].(*string)) = agentapi.FrequencyTier5s
						*(dest[4].(*[]byte)) = append([]byte(nil), testCase.overrideJSON...)
						*(dest[5].(*bool)) = true
						*(dest[6].(*bool)) = false
						*(dest[7].(*[]byte)) = mustMarshalAgentPlanJSON(t, defaults.IPQuality)
						*(dest[8].(*[]byte)) = append([]byte(nil), testCase.probeJSON...)
						*(dest[9].(*[]byte)) = append([]byte(nil), testCase.incidentJSON...)
						return nil
					}}
				},
				query: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
					if sql != selectAgentPlanAssignmentsSQL {
						return nil, errors.New("unexpected Query")
					}
					return &fakeAgentPlanRows{}, nil
				},
			}}

			_, err := repo.BuildSyncPlan(context.Background(), "mi_001")
			if err == nil {
				t.Fatal("BuildSyncPlan() error = nil, want malformed persisted policy rejection")
			}
			if testCase.wantInvalidSettings && !errors.Is(err, centersettings.ErrInvalidSettings) {
				t.Fatalf("BuildSyncPlan() error = %v, want ErrInvalidSettings", err)
			}
		})
	}
}
func TestResolveProbeAssignmentFrequencyTierUsesExactPrecedence(t *testing.T) {
	t.Parallel()

	t.Run("each probe kind uses its matching type override", func(t *testing.T) {
		t.Parallel()

		tcpTier := agentapi.FrequencyTier1m
		httpTier := agentapi.FrequencyTier5m
		tlsTier := agentapi.FrequencyTier6h
		rules := centersettings.OverrideRules{
			TargetTypes: []centersettings.TargetTypeOverrideRule{{
				TargetType: targets.TargetTypeService,
				Overrides: centersettings.SettingsOverrideFields{
					ProbeFrequencyDefaults: &centersettings.ProbeFrequencyOverride{
						TCP:  &tcpTier,
						HTTP: &httpTier,
						TLS:  &tlsTier,
					},
				},
			}},
		}
		for _, testCase := range []struct {
			kind string
			want string
		}{
			{kind: agentapi.ProbeKindTCP, want: tcpTier},
			{kind: agentapi.ProbeKindHTTP, want: httpTier},
			{kind: agentapi.ProbeKindTLS, want: tlsTier},
		} {
			testCase := testCase
			t.Run(testCase.kind, func(t *testing.T) {
				t.Parallel()

				got := resolveProbeAssignmentFrequencyTier(
					agentapi.FrequencyTier15m,
					testCase.kind,
					targets.TargetTypeService,
					nil,
					rules,
				)
				if got != testCase.want {
					t.Fatalf("resolveProbeAssignmentFrequencyTier() = %q, want %q", got, testCase.want)
				}
			})
		}
	})

	t.Run("target label overrides matching type", func(t *testing.T) {
		t.Parallel()

		typeTier := agentapi.FrequencyTier1m
		labelTier := agentapi.FrequencyTier5m
		got := resolveProbeAssignmentFrequencyTier(
			agentapi.FrequencyTier15m,
			agentapi.ProbeKindHTTP,
			targets.TargetTypeService,
			[]string{"external"},
			centersettings.OverrideRules{
				TargetTypes: []centersettings.TargetTypeOverrideRule{{
					TargetType: targets.TargetTypeService,
					Overrides: centersettings.SettingsOverrideFields{
						ProbeFrequencyDefaults: &centersettings.ProbeFrequencyOverride{HTTP: &typeTier},
					},
				}},
				TargetLabels: []centersettings.TargetLabelOverrideRule{{
					Label: "external",
					Overrides: centersettings.SettingsOverrideFields{
						ProbeFrequencyDefaults: &centersettings.ProbeFrequencyOverride{HTTP: &labelTier},
					},
				}},
			},
		)
		if got != labelTier {
			t.Fatalf("resolveProbeAssignmentFrequencyTier() = %q, want target-label tier %q", got, labelTier)
		}
	})

	t.Run("matching type without kind still permits target label", func(t *testing.T) {
		t.Parallel()

		labelTier := agentapi.FrequencyTier1m
		got := resolveProbeAssignmentFrequencyTier(
			agentapi.FrequencyTier15m,
			agentapi.ProbeKindTLS,
			targets.TargetTypeService,
			[]string{"external"},
			centersettings.OverrideRules{
				TargetTypes: []centersettings.TargetTypeOverrideRule{{
					TargetType: targets.TargetTypeService,
					Overrides: centersettings.SettingsOverrideFields{
						ProbeFrequencyDefaults: &centersettings.ProbeFrequencyOverride{HTTP: new(agentapi.FrequencyTier5m)},
					},
				}},
				TargetLabels: []centersettings.TargetLabelOverrideRule{{
					Label: "external",
					Overrides: centersettings.SettingsOverrideFields{
						ProbeFrequencyDefaults: &centersettings.ProbeFrequencyOverride{TLS: &labelTier},
					},
				}},
			},
		)
		if got != labelTier {
			t.Fatalf("resolveProbeAssignmentFrequencyTier() = %q, want target-label tier %q", got, labelTier)
		}
	})

	t.Run("matching target label without kind is skipped in rule order", func(t *testing.T) {
		t.Parallel()

		labelTier := agentapi.FrequencyTier5m
		got := resolveProbeAssignmentFrequencyTier(
			agentapi.FrequencyTier15m,
			agentapi.ProbeKindTLS,
			targets.TargetTypeService,
			[]string{"first", "second"},
			centersettings.OverrideRules{
				TargetLabels: []centersettings.TargetLabelOverrideRule{
					{
						Label: "first",
						Overrides: centersettings.SettingsOverrideFields{
							ProbeFrequencyDefaults: &centersettings.ProbeFrequencyOverride{HTTP: new(agentapi.FrequencyTier1m)},
						},
					},
					{
						Label: "second",
						Overrides: centersettings.SettingsOverrideFields{
							ProbeFrequencyDefaults: &centersettings.ProbeFrequencyOverride{TLS: &labelTier},
						},
					},
				},
			},
		)
		if got != labelTier {
			t.Fatalf("resolveProbeAssignmentFrequencyTier() = %q, want later matching label tier %q", got, labelTier)
		}
	})

	t.Run("exact labels preserve persisted probe base", func(t *testing.T) {
		t.Parallel()

		base := agentapi.FrequencyTier6h
		overrideTier := agentapi.FrequencyTier1m
		rules := centersettings.OverrideRules{TargetLabels: []centersettings.TargetLabelOverrideRule{{
			Label: "external",
			Overrides: centersettings.SettingsOverrideFields{
				ProbeFrequencyDefaults: &centersettings.ProbeFrequencyOverride{HTTP: &overrideTier},
			},
		}}}
		if got := resolveProbeAssignmentFrequencyTier(base, agentapi.ProbeKindHTTP, targets.TargetTypeService, []string{"External"}, rules); got != base {
			t.Fatalf("case-mismatched target label tier = %q, want persisted base %q", got, base)
		}
		if got := resolveProbeAssignmentFrequencyTier(base, agentapi.ProbeKindHTTP, targets.TargetTypeService, nil, rules); got != base {
			t.Fatalf("missing target label tier = %q, want persisted base %q", got, base)
		}
	})
}

func TestResolveHostSampleFrequencyTierUsesExactRuleOrder(t *testing.T) {
	t.Parallel()

	firstTier := agentapi.FrequencyTier1m
	laterTier := agentapi.FrequencyTier5m
	rules := centersettings.OverrideRules{MonitoringInstanceLabels: []centersettings.MonitoringInstanceLabelOverrideRule{
		{Label: "edge", Overrides: centersettings.SettingsOverrideFields{HostSampleFrequencyTier: &firstTier}},
		{Label: "core", Overrides: centersettings.SettingsOverrideFields{HostSampleFrequencyTier: &laterTier}},
	}}
	if got := centersettings.ResolveHostSampleFrequencyTier(agentapi.FrequencyTier15m, []string{"core", "edge"}, rules); got != firstTier {
		t.Fatalf("ResolveHostSampleFrequencyTier() = %q, want first rule tier %q", got, firstTier)
	}
	if got := centersettings.ResolveHostSampleFrequencyTier(agentapi.FrequencyTier15m, []string{"EDGE"}, rules); got != agentapi.FrequencyTier15m {
		t.Fatalf("ResolveHostSampleFrequencyTier() case-mismatch = %q, want persisted base %q", got, agentapi.FrequencyTier15m)
	}
}

func TestBuildSyncPlanSuppressesPausedAndRetiredMonitoringInstances(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		lifecycleStatus  string
		monitoringStatus string
		archived         bool
	}{
		{
			name:             "paused monitoringInstance",
			lifecycleStatus:  monitoringinstances.LifecycleInUse,
			monitoringStatus: monitoringinstances.MonitoringPaused,
		},
		{
			name:             "retired monitoringInstance",
			lifecycleStatus:  monitoringinstances.LifecycleRetired,
			monitoringStatus: monitoringinstances.MonitoringEnabled,
		},
		{
			name:             "archived monitoringInstance",
			lifecycleStatus:  monitoringinstances.LifecycleInUse,
			monitoringStatus: monitoringinstances.MonitoringEnabled,
			archived:         true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			queryCalled := false
			repo := &PostgresAgentPlanRepository{db: fakeAgentPlanQueryer{
				queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
					if sql != selectAgentPlanMonitoringInstanceLabelsSQL {
						return fakeAgentPlanRow{scan: func(dest ...any) error { return errors.New("unexpected QueryRow") }}
					}
					if args[0] != "mi_001" {
						return fakeAgentPlanRow{scan: func(dest ...any) error { return errors.New("unexpected monitoringInstance id") }}
					}
					return fakeAgentPlanRow{scan: func(dest ...any) error {
						*(dest[0].(*[]string)) = []string{"edge"}
						*(dest[1].(*string)) = tc.lifecycleStatus
						*(dest[2].(*string)) = tc.monitoringStatus
						*(dest[3].(*string)) = agentapi.FrequencyTier15m
						*(dest[4].(*[]byte)) = mustMarshalAgentPlanJSON(t, centersettings.OverrideRules{})
						*(dest[5].(*bool)) = true
						if archived, ok := dest[6].(*bool); ok {
							*archived = tc.archived
						}
						return nil
					}}
				},
				query: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
					queryCalled = true
					return &fakeAgentPlanRows{}, nil
				},
			}}

			plan, err := repo.BuildSyncPlan(context.Background(), "mi_001")
			if err != nil {
				t.Fatalf("BuildSyncPlan() error = %v", err)
			}
			if plan.HostSampleFrequencyTier != "" {
				t.Fatalf("HostSampleFrequencyTier = %q, want empty", plan.HostSampleFrequencyTier)
			}
			if plan.IPQualityPlan != nil {
				t.Fatalf("IPQualityPlan = %#v, want nil for suppressed monitoringInstance", plan.IPQualityPlan)
			}
			if plan.HostSampleMaintenanceContext {
				t.Fatalf("HostSampleMaintenanceContext = true, want false")
			}
			if len(plan.ProbeAssignments) != 0 {
				t.Fatalf("len(ProbeAssignments) = %d, want 0", len(plan.ProbeAssignments))
			}
			if queryCalled {
				t.Fatal("assignment query ran for suppressed monitoringInstance")
			}
		})
	}
}

func TestBuildSyncPlanMarksMonitoringInstanceMaintenanceContext(t *testing.T) {
	t.Parallel()

	repo := &PostgresAgentPlanRepository{db: fakeAgentPlanQueryer{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			if sql != selectAgentPlanMonitoringInstanceLabelsSQL {
				return fakeAgentPlanRow{scan: func(dest ...any) error { return errors.New("unexpected QueryRow") }}
			}
			if args[0] != "mi_001" {
				return fakeAgentPlanRow{scan: func(dest ...any) error { return errors.New("unexpected monitoringInstance id") }}
			}
			return fakeAgentPlanRow{scan: func(dest ...any) error {
				*(dest[0].(*[]string)) = []string{"edge"}
				*(dest[1].(*string)) = monitoringinstances.LifecycleInUse
				*(dest[2].(*string)) = monitoringinstances.MonitoringMaintenance
				*(dest[3].(*string)) = agentapi.FrequencyTier15m
				*(dest[4].(*[]byte)) = mustMarshalAgentPlanJSON(t, centersettings.OverrideRules{})
				*(dest[5].(*bool)) = true
				return nil
			}}
		},
		query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			if sql != selectAgentPlanAssignmentsSQL {
				return nil, errors.New("unexpected Query")
			}
			return &fakeAgentPlanRows{rows: []fakeAgentPlanScan{
				{scan: func(dest ...any) error {
					*(dest[0].(*string)) = "tg_enabled"
					*(dest[1].(*string)) = "api.example.test"
					port := 443
					*(dest[2].(**int)) = &port
					*(dest[3].(*string)) = targets.RunStatusEnabled
					*(dest[4].(*string)) = "pb_http"
					*(dest[5].(*string)) = agentapi.ProbeKindHTTP
					*(dest[6].(*string)) = agentapi.FrequencyTier1m
					*(dest[7].(*int)) = 5
					*(dest[8].(*[]byte)) = []byte(`{"path":"/healthz"}`)
					*(dest[9].(*string)) = targets.TargetTypeService
					*(dest[10].(*[]string)) = []string{"api"}
					return nil
				}},
				{scan: func(dest ...any) error {
					*(dest[0].(*string)) = "tg_maint"
					*(dest[1].(*string)) = "cache.example.test"
					*(dest[2].(**int)) = nil
					*(dest[3].(*string)) = targets.RunStatusMaintenance
					*(dest[4].(*string)) = "pb_tcp"
					*(dest[5].(*string)) = agentapi.ProbeKindTCP
					*(dest[6].(*string)) = agentapi.FrequencyTier5m
					*(dest[7].(*int)) = 3
					*(dest[8].(*[]byte)) = []byte(`{"port":11211}`)
					*(dest[9].(*string)) = targets.TargetTypeService
					*(dest[10].(*[]string)) = []string{"cache"}
					return nil
				}},
			}}, nil
		},
	}}

	plan, err := repo.BuildSyncPlan(context.Background(), "mi_001")
	if err != nil {
		t.Fatalf("BuildSyncPlan() error = %v", err)
	}
	if plan.HostSampleFrequencyTier != agentapi.FrequencyTier15m {
		t.Fatalf("HostSampleFrequencyTier = %q, want %q", plan.HostSampleFrequencyTier, agentapi.FrequencyTier15m)
	}
	if !plan.HostSampleMaintenanceContext {
		t.Fatal("HostSampleMaintenanceContext = false, want true")
	}
	if len(plan.ProbeAssignments) != 2 {
		t.Fatalf("len(ProbeAssignments) = %d, want 2", len(plan.ProbeAssignments))
	}
	for i, assignment := range plan.ProbeAssignments {
		if !assignment.MaintenanceContext {
			t.Fatalf("ProbeAssignments[%d].MaintenanceContext = false, want true", i)
		}
	}
}

func mustMarshalAgentPlanJSON(t *testing.T, value any) []byte {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return data
}

type fakeAgentPlanQueryer struct {
	queryRow func(context.Context, string, ...any) pgx.Row
	query    func(context.Context, string, ...any) (pgx.Rows, error)
}

func (f fakeAgentPlanQueryer) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return f.queryRow(ctx, sql, args...)
}

func (f fakeAgentPlanQueryer) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.query == nil {
		return &fakeAgentPlanRows{}, nil
	}
	return f.query(ctx, sql, args...)
}

type fakeAgentPlanRow struct {
	scan func(dest ...any) error
}

func (f fakeAgentPlanRow) Scan(dest ...any) error {
	if len(dest) != 10 {
		return errors.New("unexpected agent-plan snapshot column count")
	}
	if err := f.scan(dest...); err != nil {
		return err
	}
	fillDefaultAgentPlanScanFields(dest)
	return nil
}

func fillDefaultAgentPlanScanFields(dest []any) {
	if value, ok := dest[1].(*string); ok && *value == "" {
		*value = monitoringinstances.LifecycleInUse
	}
	if value, ok := dest[2].(*string); ok && *value == "" {
		*value = monitoringinstances.MonitoringEnabled
	}
	if value, ok := dest[3].(*string); ok && *value == "" {
		*value = agentapi.FrequencyTier5s
	}
	if value, ok := dest[4].(*[]byte); ok && len(*value) == 0 {
		*value = []byte(`{"monitoring_instance_labels":[],"target_types":[],"target_labels":[]}`)
	}
	if value, ok := dest[7].(*[]byte); ok && len(*value) == 0 {
		*value = []byte(`{"enabled":false,"frequency_seconds":86400,"timeout_seconds":15,"services":["netflix","chatgpt","youtube-premium","amazon-prime-video","disney-plus","tiktok","reddit"]}`)
	}
	if value, ok := dest[8].(*[]byte); ok && len(*value) == 0 {
		*value = []byte(`{"tcp":"5s","http":"5s","tls":"6h"}`)
	}
	if value, ok := dest[9].(*[]byte); ok && len(*value) == 0 {
		if defaultsJSON, err := json.Marshal(centersettings.Default().IncidentDefaults); err == nil {
			*value = defaultsJSON
		}
	}
}

type fakeAgentPlanScan struct{ scan func(dest ...any) error }

type fakeAgentPlanRows struct {
	rows []fakeAgentPlanScan
	idx  int
	err  error
}

func (f *fakeAgentPlanRows) Close()                                       {}
func (f *fakeAgentPlanRows) Err() error                                   { return f.err }
func (f *fakeAgentPlanRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (f *fakeAgentPlanRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (f *fakeAgentPlanRows) RawValues() [][]byte                          { return nil }
func (f *fakeAgentPlanRows) Values() ([]any, error)                       { return nil, nil }
func (f *fakeAgentPlanRows) Conn() *pgx.Conn                              { return nil }
func (f *fakeAgentPlanRows) Next() bool {
	if f.idx >= len(f.rows) {
		return false
	}
	f.idx++
	return true
}
func (f *fakeAgentPlanRows) Scan(dest ...any) error { return f.rows[f.idx-1].scan(dest...) }
