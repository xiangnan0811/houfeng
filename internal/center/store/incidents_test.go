package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/incidents"
)

func TestPostgresIncidentRepositoryListActiveIncidentsUsesDefaultLimit(t *testing.T) {
	var (
		capturedSQL  string
		capturedArgs []any
	)
	repo := &PostgresIncidentRepository{query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
		capturedSQL = sql
		capturedArgs = args
		return &fakeDashboardRows{}, nil
	}}

	_, err := repo.ListActiveIncidents(context.Background(), IncidentsFilter{})
	if err != nil {
		t.Fatalf("ListActiveIncidents() error = %v", err)
	}
	if !containsSQL([]string{capturedSQL}, "from active_incidents") {
		t.Fatalf("capturedSQL = %q, want active incidents query", capturedSQL)
	}
	if !containsSQL([]string{capturedSQL}, "order by case severity") {
		t.Fatalf("capturedSQL = %q, want severity ordering", capturedSQL)
	}
	if !containsSQL([]string{capturedSQL}, "started_at desc") || !containsSQL([]string{capturedSQL}, "incident_id asc") {
		t.Fatalf("capturedSQL = %q, want stable ordering", capturedSQL)
	}
	if len(capturedArgs) != 1 || capturedArgs[0] != 50 {
		t.Fatalf("capturedArgs = %#v, want default limit 50", capturedArgs)
	}
}

func TestPostgresIncidentRepositoryListActiveIncidentsBuildsOptionalFilters(t *testing.T) {
	testCases := []struct {
		name      string
		filter    IncidentsFilter
		wantParts []string
		wantArgs  []any
	}{
		{
			name:      "object type",
			filter:    IncidentsFilter{ObjectType: incidents.ObjectTypeNode, Limit: 25},
			wantParts: []string{"object_type = $1"},
			wantArgs:  []any{string(incidents.ObjectTypeNode), 25},
		},
		{
			name:      "object id",
			filter:    IncidentsFilter{ObjectID: "nd_001", Limit: 25},
			wantParts: []string{"object_id = $1"},
			wantArgs:  []any{"nd_001", 25},
		},
		{
			name:      "severity",
			filter:    IncidentsFilter{Severity: incidents.SeverityAlert, Limit: 25},
			wantParts: []string{"severity = $1"},
			wantArgs:  []any{string(incidents.SeverityAlert), 25},
		},
		{
			name:   "combined filters",
			filter: IncidentsFilter{ObjectType: incidents.ObjectTypeTarget, ObjectID: "tg_001", Severity: incidents.SeverityCritical, Limit: 25},
			wantParts: []string{
				"object_type = $1",
				"object_id = $2",
				"severity = $3",
			},
			wantArgs: []any{string(incidents.ObjectTypeTarget), "tg_001", string(incidents.SeverityCritical), 25},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var (
				capturedSQL  string
				capturedArgs []any
			)
			repo := &PostgresIncidentRepository{query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
				capturedSQL = sql
				capturedArgs = args
				return &fakeDashboardRows{}, nil
			}}

			_, err := repo.ListActiveIncidents(context.Background(), tc.filter)
			if err != nil {
				t.Fatalf("ListActiveIncidents() error = %v", err)
			}
			for _, want := range tc.wantParts {
				if !containsSQL([]string{capturedSQL}, want) {
					t.Fatalf("capturedSQL = %q, want %q", capturedSQL, want)
				}
			}
			if len(capturedArgs) != len(tc.wantArgs) {
				t.Fatalf("capturedArgs = %#v, want %#v", capturedArgs, tc.wantArgs)
			}
			for i := range tc.wantArgs {
				if capturedArgs[i] != tc.wantArgs[i] {
					t.Fatalf("capturedArgs[%d] = %#v, want %#v (all args %#v)", i, capturedArgs[i], tc.wantArgs[i], capturedArgs)
				}
			}
		})
	}
}

func TestPostgresIncidentRepositoryListActiveIncidentsScansRows(t *testing.T) {
	now := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)
	repo := &PostgresIncidentRepository{query: func(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
		return &fakeDashboardRows{rows: []fakeDashboardScan{
			{scan: func(dest ...any) error {
				*(dest[0].(*string)) = "inc_002"
				*(dest[1].(*incidents.IncidentClass)) = incidents.IncidentTargetProbeFailure
				*(dest[2].(*incidents.ObjectType)) = incidents.ObjectTypeTarget
				*(dest[3].(*string)) = "tg_001"
				*(dest[4].(*incidents.Severity)) = incidents.SeverityCritical
				*(dest[5].(*time.Time)) = now
				*(dest[6].(*time.Time)) = now.Add(2 * time.Minute)
				*(dest[7].(*string)) = "HTTP 500"
				return nil
			}},
			{scan: func(dest ...any) error {
				*(dest[0].(*string)) = "inc_001"
				*(dest[1].(*incidents.IncidentClass)) = incidents.IncidentNodeDiskPressure
				*(dest[2].(*incidents.ObjectType)) = incidents.ObjectTypeNode
				*(dest[3].(*string)) = "nd_001"
				*(dest[4].(*incidents.Severity)) = incidents.SeverityAlert
				*(dest[5].(*time.Time)) = now.Add(-time.Minute)
				*(dest[6].(*time.Time)) = now
				*(dest[7].(*string)) = "磁盘使用率 92.0%"
				return nil
			}},
		}}, nil
	}}

	records, err := repo.ListActiveIncidents(context.Background(), IncidentsFilter{Limit: 2})
	if err != nil {
		t.Fatalf("ListActiveIncidents() error = %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}
	if records[0].IncidentID != "inc_002" || records[0].SourceSummary != "HTTP 500" {
		t.Fatalf("records[0] = %#v, want first row decoded", records[0])
	}
	if records[1].IncidentID != "inc_001" || records[1].ObjectID != "nd_001" {
		t.Fatalf("records[1] = %#v, want second row decoded", records[1])
	}
}

func TestPostgresIncidentRepositoryAppliesMutationAndProjectsNodeSummary(t *testing.T) {
	tx := &fakeIncidentTx{
		summaryCount:    2,
		summarySeverity: string(incidents.SeverityAlert),
		summaryText:     "HTTP 探针连续失败 3 次",
	}
	repo := &PostgresIncidentRepository{beginTx: func(context.Context, pgx.TxOptions) (incidentStoreTx, error) { return tx, nil }}
	now := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)
	sentAt := now.Add(time.Second)

	err := repo.ApplyIncidentMutation(context.Background(), incidents.IncidentMutation{
		ObjectType: incidents.ObjectTypeNode,
		ObjectID:   "nd_001",
		Active: []incidents.IncidentRecord{{
			IncidentID:      "inc_node_nd_001_node_disk_pressure",
			ObjectType:      incidents.ObjectTypeNode,
			ObjectID:        "nd_001",
			IncidentClass:   incidents.IncidentNodeDiskPressure,
			Severity:        incidents.SeverityAlert,
			StartedAt:       now,
			LastEvaluatedAt: now,
			Status:          incidents.IncidentStatusActive,
			SourceSummary:   "磁盘使用率 92.0%",
		}},
		Events: []incidents.StateChangeEventRecord{{
			IncidentID:    "inc_node_nd_001_node_disk_pressure",
			IncidentClass: incidents.IncidentNodeDiskPressure,
			ObjectType:    incidents.ObjectTypeNode,
			ObjectID:      "nd_001",
			EventType:     incidents.EventIncidentStarted,
			Severity:      incidents.SeverityAlert,
			Summary:       "磁盘使用率 92.0%",
			CreatedAt:     now,
		}},
		Notifications: []incidents.NotificationRecordWrite{{
			IncidentID:     "inc_node_nd_001_node_disk_pressure",
			ObjectType:     incidents.ObjectTypeNode,
			ObjectID:       "nd_001",
			Channel:        "telegram",
			DeliveryStatus: incidents.DeliveryStatusSent,
			Summary:        "磁盘使用率 92.0%",
			SentAt:         &sentAt,
		}},
	})
	if err != nil {
		t.Fatalf("ApplyIncidentMutation() error = %v", err)
	}
	if tx.commitCalls != 1 {
		t.Fatalf("commitCalls = %d, want 1", tx.commitCalls)
	}
	assertContainsSQL(t, tx.execSQL, "delete from active_incidents")
	assertContainsSQL(t, tx.execSQL, "insert into active_incidents")
	assertContainsSQL(t, tx.execSQL, "insert into state_change_events")
	assertContainsSQL(t, tx.execSQL, "insert into notification_records")
	assertContainsSQL(t, tx.execSQL, "update nodes")
}

func TestPostgresIncidentRepositoryProjectsNormalSummaryWhenActiveSetIsEmpty(t *testing.T) {
	tx := &fakeIncidentTx{summarySeverity: string(incidents.SeverityNormal)}
	repo := &PostgresIncidentRepository{beginTx: func(context.Context, pgx.TxOptions) (incidentStoreTx, error) { return tx, nil }}

	err := repo.ApplyIncidentMutation(context.Background(), incidents.IncidentMutation{
		ObjectType: incidents.ObjectTypeTarget,
		ObjectID:   "tg_001",
	})
	if err != nil {
		t.Fatalf("ApplyIncidentMutation() error = %v", err)
	}
	assertContainsSQL(t, tx.execSQL, "delete from active_incidents")
	assertContainsSQL(t, tx.execSQL, "update targets")
}

func TestPostgresIncidentRepositoryFailsWhenObjectSummaryUpdateTouchesNoRows(t *testing.T) {
	tx := &fakeIncidentTx{
		summarySeverity: string(incidents.SeverityAlert),
		updateRows:      -1,
	}
	repo := &PostgresIncidentRepository{beginTx: func(context.Context, pgx.TxOptions) (incidentStoreTx, error) { return tx, nil }}
	now := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)

	err := repo.ApplyIncidentMutation(context.Background(), incidents.IncidentMutation{
		ObjectType: incidents.ObjectTypeNode,
		ObjectID:   "nd_missing",
		Active: []incidents.IncidentRecord{{
			IncidentID:      "inc_node_nd_missing_node_disk_pressure",
			ObjectType:      incidents.ObjectTypeNode,
			ObjectID:        "nd_missing",
			IncidentClass:   incidents.IncidentNodeDiskPressure,
			Severity:        incidents.SeverityAlert,
			StartedAt:       now,
			LastEvaluatedAt: now,
			Status:          incidents.IncidentStatusActive,
			SourceSummary:   "磁盘使用率 92.0%",
		}},
	})
	if err == nil {
		t.Fatal("ApplyIncidentMutation() error = nil, want non-nil")
	}
	if tx.commitCalls != 0 {
		t.Fatalf("commitCalls = %d, want 0", tx.commitCalls)
	}
}

type fakeIncidentTx struct {
	execSQL         []string
	commitCalls     int
	rollbackCalls   int
	summaryCount    int
	summarySeverity string
	summaryText     string
	updateRows      int64
}

func (f *fakeIncidentTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.execSQL = append(f.execSQL, sql)
	if containsSQL([]string{sql}, "update nodes") || containsSQL([]string{sql}, "update targets") {
		rows := f.updateRows
		if rows == 0 {
			rows = 1
		}
		if rows < 0 {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (f *fakeIncidentTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return fakeRow{scan: func(dest ...any) error {
		*(dest[0].(*int)) = f.summaryCount
		*(dest[1].(*string)) = f.summarySeverity
		*(dest[2].(*string)) = f.summaryText
		return nil
	}}
}

func (f *fakeIncidentTx) Commit(context.Context) error   { f.commitCalls++; return nil }
func (f *fakeIncidentTx) Rollback(context.Context) error { f.rollbackCalls++; return nil }

func assertContainsSQL(t *testing.T, sqls []string, want string) {
	t.Helper()
	for _, sql := range sqls {
		if containsSQL([]string{sql}, want) {
			return
		}
	}
	t.Fatalf("SQL missing %q in %#v", want, sqls)
}
