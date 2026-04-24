package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/nodes"
	"houfeng/internal/center/targets"
	"houfeng/internal/contracts/agentapi"
)

func TestPostgresAgentPlanRepositoryBuildSyncPlanReturnsAssignments(t *testing.T) {
	t.Parallel()

	var seenStatuses []string
	var seenLabels []string
	repo := &PostgresAgentPlanRepository{db: fakeAgentPlanQueryer{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			if sql != selectAgentPlanNodeLabelsSQL {
				return fakeAgentPlanRow{scan: func(dest ...any) error { return errors.New("unexpected QueryRow") }}
			}
			if args[0] != "nd_001" {
				return fakeAgentPlanRow{scan: func(dest ...any) error { return errors.New("unexpected node id") }}
			}
			return fakeAgentPlanRow{scan: func(dest ...any) error {
				*(dest[0].(*[]string)) = []string{"edge", "核心"}
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

	plan, err := repo.BuildSyncPlan(context.Background(), "nd_001")
	if err != nil {
		t.Fatalf("BuildSyncPlan() error = %v", err)
	}
	if plan.HostSampleFrequencyTier != agentapi.FrequencyTier1m {
		t.Fatalf("HostSampleFrequencyTier = %q, want %q", plan.HostSampleFrequencyTier, agentapi.FrequencyTier1m)
	}
	if len(plan.ProbeAssignments) != 2 {
		t.Fatalf("len(ProbeAssignments) = %d, want 2", len(plan.ProbeAssignments))
	}
	if plan.ProbeAssignments[1].MaintenanceContext != true {
		t.Fatalf("MaintenanceContext = %v, want true", plan.ProbeAssignments[1].MaintenanceContext)
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

func TestBuildSyncPlanReturnsNodeNotFound(t *testing.T) {
	t.Parallel()

	repo := &PostgresAgentPlanRepository{db: fakeAgentPlanQueryer{
		queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return fakeAgentPlanRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
		},
	}}

	_, err := repo.BuildSyncPlan(context.Background(), "nd_missing")
	if !errors.Is(err, nodes.ErrNodeNotFound) {
		t.Fatalf("BuildSyncPlan() error = %v, want ErrNodeNotFound", err)
	}
}

func TestBuildSyncPlanReturnsDefaultCadenceAndNoAssignmentsForLabelLessNode(t *testing.T) {
	t.Parallel()

	repo := &PostgresAgentPlanRepository{db: fakeAgentPlanQueryer{
		queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return fakeAgentPlanRow{scan: func(dest ...any) error {
				*(dest[0].(*[]string)) = nil
				return nil
			}}
		},
	}}

	plan, err := repo.BuildSyncPlan(context.Background(), "nd_001")
	if err != nil {
		t.Fatalf("BuildSyncPlan() error = %v", err)
	}
	if plan.HostSampleFrequencyTier != agentapi.FrequencyTier5m {
		t.Fatalf("HostSampleFrequencyTier = %q, want %q", plan.HostSampleFrequencyTier, agentapi.FrequencyTier5m)
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
	if !containsSQL([]string{selectAgentPlanAssignmentsSQL}, "t.execution_node_labels && $2") {
		t.Fatalf("selectAgentPlanAssignmentsSQL = %q, want label overlap filter", selectAgentPlanAssignmentsSQL)
	}
	if !containsSQL([]string{selectAgentPlanAssignmentsSQL}, "t.run_status = any($1)") {
		t.Fatalf("selectAgentPlanAssignmentsSQL = %q, want run_status filter", selectAgentPlanAssignmentsSQL)
	}
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
