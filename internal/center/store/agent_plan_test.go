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
				if len(dest) > 1 {
					*(dest[1].(*string)) = monitoringinstances.LifecycleInUse
				}
				if len(dest) > 2 {
					*(dest[2].(*string)) = monitoringinstances.MonitoringEnabled
				}
				if len(dest) > 3 {
					*(dest[3].(*string)) = agentapi.FrequencyTier15m
				}
				if len(dest) > 4 {
					*(dest[4].(*[]byte)) = mustMarshalAgentPlanJSON(t, centersettings.OverrideRules{
						MonitoringInstanceLabels: []centersettings.MonitoringInstanceLabelOverrideRule{},
						TargetTypes:              []centersettings.TargetTypeOverrideRule{},
						TargetLabels:             []centersettings.TargetLabelOverrideRule{},
					})
				}
				if len(dest) > 5 {
					*(dest[5].(*bool)) = true
				}
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
				if len(dest) > 1 {
					*(dest[1].(*string)) = monitoringinstances.LifecycleInUse
				}
				if len(dest) > 2 {
					*(dest[2].(*string)) = monitoringinstances.MonitoringEnabled
				}
				if len(dest) > 3 {
					*(dest[3].(*string)) = agentapi.FrequencyTier15m
				}
				if len(dest) > 4 {
					*(dest[4].(*[]byte)) = mustMarshalAgentPlanJSON(t, centersettings.OverrideRules{
						MonitoringInstanceLabels: []centersettings.MonitoringInstanceLabelOverrideRule{{
							Label: "核心",
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
				}
				if len(dest) > 5 {
					*(dest[5].(*bool)) = true
				}
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
				if len(dest) > 1 {
					*(dest[1].(*string)) = monitoringinstances.LifecycleInUse
				}
				if len(dest) > 2 {
					*(dest[2].(*string)) = monitoringinstances.MonitoringEnabled
				}
				if len(dest) > 3 {
					*(dest[3].(*string)) = agentapi.FrequencyTier5m
				}
				if len(dest) > 4 {
					*(dest[4].(*[]byte)) = mustMarshalAgentPlanJSON(t, centersettings.OverrideRules{
						MonitoringInstanceLabels: []centersettings.MonitoringInstanceLabelOverrideRule{},
						TargetTypes:              []centersettings.TargetTypeOverrideRule{},
						TargetLabels:             []centersettings.TargetLabelOverrideRule{},
					})
				}
				if len(dest) > 5 {
					*(dest[5].(*bool)) = false
				}
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
				if len(dest) > 1 {
					*(dest[1].(*string)) = monitoringinstances.LifecycleInUse
				}
				if len(dest) > 2 {
					*(dest[2].(*string)) = monitoringinstances.MonitoringEnabled
				}
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

func TestBuildSyncPlanSuppressesPausedAndRetiredMonitoringInstances(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name             string
		lifecycleStatus  string
		monitoringStatus string
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

func (f fakeAgentPlanRow) Scan(dest ...any) error { return f.scan(dest...) }

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
