package store

import (
	"context"
	"errors"
	"reflect"
	"strings"
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
	if !containsSQL([]string{capturedSQL}, "status = $1") {
		t.Fatalf("capturedSQL = %q, want default status = active filter", capturedSQL)
	}
	if len(capturedArgs) != 2 || capturedArgs[0] != incidents.IncidentStatusActive || capturedArgs[1] != 50 {
		t.Fatalf("capturedArgs = %#v, want [active, 50]", capturedArgs)
	}
}

func TestPostgresIncidentRepositoryListActiveIncidentsIncludeResolved(t *testing.T) {
	var (
		capturedSQL  string
		capturedArgs []any
	)
	repo := &PostgresIncidentRepository{query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
		capturedSQL = sql
		capturedArgs = args
		return &fakeDashboardRows{}, nil
	}}

	_, err := repo.ListActiveIncidents(context.Background(), IncidentsFilter{IncludeResolved: true, Limit: 25})
	if err != nil {
		t.Fatalf("ListActiveIncidents() error = %v", err)
	}
	if containsSQL([]string{capturedSQL}, "status =") {
		t.Fatalf("capturedSQL = %q, want no status filter when include_resolved=true", capturedSQL)
	}
	if len(capturedArgs) != 1 || capturedArgs[0] != 25 {
		t.Fatalf("capturedArgs = %#v, want only [limit]", capturedArgs)
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
			filter:    IncidentsFilter{ObjectType: incidents.ObjectTypeMonitoringInstance, Limit: 25},
			wantParts: []string{"object_type = $1", "status = $2"},
			wantArgs:  []any{string(incidents.ObjectTypeMonitoringInstance), incidents.IncidentStatusActive, 25},
		},
		{
			name:      "object id",
			filter:    IncidentsFilter{ObjectID: "mi_001", Limit: 25},
			wantParts: []string{"object_id = $1", "status = $2"},
			wantArgs:  []any{"mi_001", incidents.IncidentStatusActive, 25},
		},
		{
			name:      "severity",
			filter:    IncidentsFilter{Severity: incidents.SeverityAlert, Limit: 25},
			wantParts: []string{"severity = $1", "status = $2"},
			wantArgs:  []any{string(incidents.SeverityAlert), incidents.IncidentStatusActive, 25},
		},
		{
			name:   "combined filters",
			filter: IncidentsFilter{ObjectType: incidents.ObjectTypeTarget, ObjectID: "tg_001", Severity: incidents.SeverityCritical, Limit: 25},
			wantParts: []string{
				"object_type = $1",
				"object_id = $2",
				"severity = $3",
				"status = $4",
			},
			wantArgs: []any{string(incidents.ObjectTypeTarget), "tg_001", string(incidents.SeverityCritical), incidents.IncidentStatusActive, 25},
		},
		{
			name:      "include resolved drops status filter",
			filter:    IncidentsFilter{ObjectID: "mi_001", IncludeResolved: true, Limit: 25},
			wantParts: []string{"object_id = $1"},
			wantArgs:  []any{"mi_001", 25},
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
				*(dest[1].(*incidents.IncidentClass)) = incidents.IncidentMonitoringInstanceDiskPressure
				*(dest[2].(*incidents.ObjectType)) = incidents.ObjectTypeMonitoringInstance
				*(dest[3].(*string)) = "mi_001"
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
	if records[1].IncidentID != "inc_001" || records[1].ObjectID != "mi_001" {
		t.Fatalf("records[1] = %#v, want second row decoded", records[1])
	}
}

func TestPostgresIncidentRepositoryAppliesMutationAndProjectsMonitoringInstanceSummary(t *testing.T) {
	tx := &fakeIncidentTx{
		objectRowVersion: "41",
		summaryCount:     2,
		summarySeverity:  string(incidents.SeverityAlert),
		summaryText:      "HTTP 探针连续失败 3 次",
	}
	repo := &PostgresIncidentRepository{beginTx: func(context.Context, pgx.TxOptions) (incidentStoreTx, error) { return tx, nil }}
	now := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)

	err := repo.ApplyIncidentMutation(context.Background(), incidents.IncidentMutation{
		ObjectType:               incidents.ObjectTypeMonitoringInstance,
		ObjectID:                 "mi_001",
		ExpectedObjectRowVersion: "41",
		Active: []incidents.IncidentRecord{{
			IncidentID:      "inc_monitoring_instance_mi_001_monitoring_instance_disk_pressure",
			ObjectType:      incidents.ObjectTypeMonitoringInstance,
			ObjectID:        "mi_001",
			IncidentClass:   incidents.IncidentMonitoringInstanceDiskPressure,
			Severity:        incidents.SeverityAlert,
			StartedAt:       now,
			LastEvaluatedAt: now,
			Status:          incidents.IncidentStatusActive,
			SourceSummary:   "磁盘使用率 92.0%",
		}},
		Events: []incidents.StateChangeEventRecord{{
			IncidentID:          "inc_monitoring_instance_mi_001_monitoring_instance_disk_pressure",
			IncidentClass:       incidents.IncidentMonitoringInstanceDiskPressure,
			ObjectType:          incidents.ObjectTypeMonitoringInstance,
			ObjectID:            "mi_001",
			EventType:           incidents.EventIncidentStarted,
			Severity:            incidents.SeverityAlert,
			Summary:             "磁盘使用率 92.0%",
			CreatedAt:           now,
			Provenance:          incidents.MonitoringEventProvenanceAgentSync,
			ProducerVersion:     incidents.MonitoringEventProducerVersion,
			RuleVersion:         incidents.MonitoringEventIncidentRuleVersion,
			PriorState:          "normal",
			ResultingState:      "alert",
			CorrectionOfEventID: "",
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
	assertContainsSQL(t, tx.execSQL, "on conflict (incident_id) do update")
	assertContainsSQL(t, tx.execSQL, "insert into state_change_events")
	assertNotContainsSQL(t, tx.execSQL, "insert into notification_records")
	assertContainsSQL(t, tx.execSQL, "update monitoring_instances")
}

func TestPostgresIncidentRepositoryGuardsMatchedObjectRowVersionBeforeMutation(t *testing.T) {
	tests := []struct {
		name       string
		objectType incidents.ObjectType
		objectID   string
		guardSQL   string
	}{
		{
			name:       "monitoring instance",
			objectType: incidents.ObjectTypeMonitoringInstance,
			objectID:   "mi_001",
			guardSQL:   `select xmin::text from monitoring_instances where monitoring_instance_id = $1 for update`,
		},
		{
			name:       "target",
			objectType: incidents.ObjectTypeTarget,
			objectID:   "tg_001",
			guardSQL:   `select xmin::text from targets where target_id = $1 for update`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &fakeIncidentTx{
				objectRowVersion: "47",
				summarySeverity:  string(incidents.SeverityNormal),
			}
			repo := &PostgresIncidentRepository{beginTx: func(context.Context, pgx.TxOptions) (incidentStoreTx, error) {
				return tx, nil
			}}

			err := repo.ApplyIncidentMutation(context.Background(), incidents.IncidentMutation{
				ObjectType:               tt.objectType,
				ObjectID:                 tt.objectID,
				ExpectedObjectRowVersion: "47",
			})
			if err != nil {
				t.Fatalf("ApplyIncidentMutation() error = %v", err)
			}
			if len(tx.operations) == 0 {
				t.Fatal("operations = empty, want row-version guard")
			}
			guard := tx.operations[0]
			if guard.kind != incidentTxOperationQueryRow {
				t.Fatalf("first operation kind = %q, want %q", guard.kind, incidentTxOperationQueryRow)
			}
			if canonicalIncidentSQL(guard.sql) != canonicalIncidentSQL(tt.guardSQL) {
				t.Fatalf("first operation SQL = %q, want exact guard %q", guard.sql, tt.guardSQL)
			}
			if len(guard.args) != 1 || guard.args[0] != tt.objectID {
				t.Fatalf("guard args = %#v, want [%q]", guard.args, tt.objectID)
			}
			if tx.commitCalls != 1 {
				t.Fatalf("commitCalls = %d, want 1", tx.commitCalls)
			}
		})
	}
}

func TestPostgresIncidentRepositoryRejectsObjectRowVersionMismatchWithoutSideEffects(t *testing.T) {
	const (
		currentToken  = "current-private-xmin-token"
		expectedToken = "stale-private-xmin-token"
	)
	tx := &fakeIncidentTx{objectRowVersion: currentToken}
	repo := &PostgresIncidentRepository{beginTx: func(context.Context, pgx.TxOptions) (incidentStoreTx, error) {
		return tx, nil
	}}

	err := repo.ApplyIncidentMutation(context.Background(), incidents.IncidentMutation{
		ObjectType:               incidents.ObjectTypeMonitoringInstance,
		ObjectID:                 "mi_001",
		ExpectedObjectRowVersion: expectedToken,
		Events: []incidents.StateChangeEventRecord{{
			IncidentID: "inc_should_not_write",
		}},
	})
	if !errors.Is(err, incidents.ErrIncidentProjectionConflict) {
		t.Fatalf("ApplyIncidentMutation() error = %v, want ErrIncidentProjectionConflict", err)
	}
	if strings.Contains(err.Error(), currentToken) || strings.Contains(err.Error(), expectedToken) {
		t.Fatalf("error = %q, want opaque row-version tokens excluded", err)
	}
	if len(tx.operations) != 1 || tx.operations[0].kind != incidentTxOperationQueryRow {
		t.Fatalf("operations = %#v, want only row-version guard", tx.operations)
	}
	if len(tx.execSQL) != 0 {
		t.Fatalf("execSQL = %#v, want zero destructive DML", tx.execSQL)
	}
	if tx.commitCalls != 0 {
		t.Fatalf("commitCalls = %d, want 0", tx.commitCalls)
	}
}

func TestPostgresIncidentRepositoryRejectsInvalidRowVersionGuardsBeforeDML(t *testing.T) {
	ordinaryGuardErr := errors.New("guard database unavailable")
	tests := []struct {
		name             string
		mutation         incidents.IncidentMutation
		objectRowVersion string
		guardErr         error
		wantCause        error
		wantMissing      bool
		wantErrorPart    string
		wantOperations   int
	}{
		{
			name: "empty token",
			mutation: incidents.IncidentMutation{
				ObjectType: incidents.ObjectTypeMonitoringInstance,
				ObjectID:   "mi_001",
			},
			wantErrorPart:  "incident mutation object row version is required",
			wantOperations: 0,
		},
		{
			name: "missing monitoring instance",
			mutation: incidents.IncidentMutation{
				ObjectType:               incidents.ObjectTypeMonitoringInstance,
				ObjectID:                 "mi_missing",
				ExpectedObjectRowVersion: "53",
			},
			guardErr:       pgx.ErrNoRows,
			wantCause:      pgx.ErrNoRows,
			wantMissing:    true,
			wantErrorPart:  "incident mutation object not found",
			wantOperations: 1,
		},
		{
			name: "missing target",
			mutation: incidents.IncidentMutation{
				ObjectType:               incidents.ObjectTypeTarget,
				ObjectID:                 "tg_missing",
				ExpectedObjectRowVersion: "55",
			},
			guardErr:       pgx.ErrNoRows,
			wantCause:      pgx.ErrNoRows,
			wantMissing:    true,
			wantErrorPart:  "incident mutation object not found",
			wantOperations: 1,
		},
		{
			name: "ordinary guard database error",
			mutation: incidents.IncidentMutation{
				ObjectType:               incidents.ObjectTypeTarget,
				ObjectID:                 "tg_guard_error",
				ExpectedObjectRowVersion: "57",
			},
			guardErr:       ordinaryGuardErr,
			wantCause:      ordinaryGuardErr,
			wantErrorPart:  "query incident mutation object row version",
			wantOperations: 1,
		},
		{
			name: "unsupported object",
			mutation: incidents.IncidentMutation{
				ObjectType:               incidents.ObjectTypeVPS,
				ObjectID:                 "vps_001",
				ExpectedObjectRowVersion: "59",
			},
			wantErrorPart:  "unsupported incident mutation object type",
			wantOperations: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := &fakeIncidentTx{objectRowVersion: tt.objectRowVersion, objectGuardErr: tt.guardErr}
			repo := &PostgresIncidentRepository{beginTx: func(context.Context, pgx.TxOptions) (incidentStoreTx, error) {
				return tx, nil
			}}

			err := repo.ApplyIncidentMutation(context.Background(), tt.mutation)
			if err == nil || !strings.Contains(err.Error(), tt.wantErrorPart) {
				t.Fatalf("ApplyIncidentMutation() error = %v, want error containing %q", err, tt.wantErrorPart)
			}
			if tt.wantCause != nil && !errors.Is(err, tt.wantCause) {
				t.Fatalf("ApplyIncidentMutation() error = %v, want cause %v", err, tt.wantCause)
			}
			if gotMissing := errors.Is(err, incidents.ErrIncidentProjectionObjectNotFound); gotMissing != tt.wantMissing {
				t.Fatalf("ApplyIncidentMutation() missing classification = %t, want %t (error %v)", gotMissing, tt.wantMissing, err)
			}
			if len(tx.operations) != tt.wantOperations {
				t.Fatalf("operations = %#v, want %d", tx.operations, tt.wantOperations)
			}
			if len(tx.execSQL) != 0 {
				t.Fatalf("execSQL = %#v, want zero destructive DML", tx.execSQL)
			}
			if tx.commitCalls != 0 {
				t.Fatalf("commitCalls = %d, want 0", tx.commitCalls)
			}
		})
	}
}

func TestIncidentMutationCannotCarryNotificationRecords(t *testing.T) {
	if _, exists := reflect.TypeOf(incidents.IncidentMutation{}).FieldByName("Notifications"); exists {
		t.Fatal("IncidentMutation.Notifications exists, want notification persistence after successful mutation only")
	}
}

func TestPostgresIncidentRepositoryAppendsNotificationRecordsSeparately(t *testing.T) {
	tx := &fakeIncidentTx{}
	repo := &PostgresIncidentRepository{beginTx: func(context.Context, pgx.TxOptions) (incidentStoreTx, error) {
		return tx, nil
	}}

	err := repo.AppendNotificationRecords(context.Background(), []incidents.NotificationRecordWrite{{
		IncidentID:     "inc_001",
		ObjectType:     incidents.ObjectTypeMonitoringInstance,
		ObjectID:       "mi_001",
		Channel:        incidents.NotificationChannelTelegram,
		DeliveryStatus: incidents.DeliveryStatusSent,
		Summary:        "healthy boundary",
	}})
	if err != nil {
		t.Fatalf("AppendNotificationRecords() error = %v", err)
	}
	if len(tx.operations) != 1 || tx.operations[0].kind != incidentTxOperationExec {
		t.Fatalf("operations = %#v, want one notification Exec", tx.operations)
	}
	if canonicalIncidentSQL(tx.operations[0].sql) != canonicalIncidentSQL(`
		insert into notification_records (
			notification_id, incident_id, object_type, object_id, channel, delivery_status, summary, sent_at
		) values ($1,$2,$3,$4,$5,$6,$7,$8)`) {
		t.Fatalf("notification SQL = %q, want exact notification insert", tx.operations[0].sql)
	}
	if tx.commitCalls != 1 {
		t.Fatalf("commitCalls = %d, want 1", tx.commitCalls)
	}
}

func TestPostgresIncidentRepositoryUpsertsExistingActiveIncident(t *testing.T) {
	tx := &fakeIncidentTx{
		objectRowVersion:                      "61",
		failActiveIncidentInsertWithoutUpsert: true,
		summaryCount:                          1,
		summarySeverity:                       string(incidents.SeverityNotice),
		summaryText:                           "心跳缺失",
	}
	repo := &PostgresIncidentRepository{beginTx: func(context.Context, pgx.TxOptions) (incidentStoreTx, error) { return tx, nil }}
	now := time.Date(2026, time.June, 6, 9, 26, 35, 0, time.UTC)

	err := repo.ApplyIncidentMutation(context.Background(), incidents.IncidentMutation{
		ObjectType:               incidents.ObjectTypeMonitoringInstance,
		ObjectID:                 "mi_1a4c1d9de957a4b7",
		ExpectedObjectRowVersion: "61",
		Active: []incidents.IncidentRecord{{
			IncidentID:      "inc_monitoring_instance_mi_1a4c1d9de957a4b7_monitoring_instance_heartbeat_missing",
			ObjectType:      incidents.ObjectTypeMonitoringInstance,
			ObjectID:        "mi_1a4c1d9de957a4b7",
			IncidentClass:   incidents.IncidentMonitoringInstanceHeartbeatMissing,
			Severity:        incidents.SeverityNotice,
			StartedAt:       now.Add(-10 * time.Minute),
			LastEvaluatedAt: now,
			Status:          incidents.IncidentStatusActive,
			SourceSummary:   "监控实例超过预期心跳窗口未上报",
		}},
	})
	if err != nil {
		t.Fatalf("ApplyIncidentMutation() error = %v", err)
	}
	if tx.commitCalls != 1 {
		t.Fatalf("commitCalls = %d, want 1", tx.commitCalls)
	}
	assertContainsSQL(t, tx.execSQL, "on conflict (incident_id) do update")
	assertContainsSQL(t, tx.execSQL, "started_at = least(active_incidents.started_at, excluded.started_at)")
	assertContainsSQL(t, tx.execSQL, "last_evaluated_at = excluded.last_evaluated_at")
	assertContainsSQL(t, tx.execSQL, "update monitoring_instances")
}

func TestPostgresIncidentRepositoryProjectsNormalSummaryWhenActiveSetIsEmpty(t *testing.T) {
	tx := &fakeIncidentTx{objectRowVersion: "67", summarySeverity: string(incidents.SeverityNormal)}
	repo := &PostgresIncidentRepository{beginTx: func(context.Context, pgx.TxOptions) (incidentStoreTx, error) { return tx, nil }}

	err := repo.ApplyIncidentMutation(context.Background(), incidents.IncidentMutation{
		ObjectType:               incidents.ObjectTypeTarget,
		ObjectID:                 "tg_001",
		ExpectedObjectRowVersion: "67",
	})
	if err != nil {
		t.Fatalf("ApplyIncidentMutation() error = %v", err)
	}
	assertContainsSQL(t, tx.execSQL, "delete from active_incidents")
	assertContainsSQL(t, tx.execSQL, "update targets")
}

func TestPostgresIncidentRepositoryFailsWhenObjectSummaryUpdateTouchesNoRows(t *testing.T) {
	tx := &fakeIncidentTx{
		objectRowVersion: "71",
		summarySeverity:  string(incidents.SeverityAlert),
		updateRows:       -1,
	}
	repo := &PostgresIncidentRepository{beginTx: func(context.Context, pgx.TxOptions) (incidentStoreTx, error) { return tx, nil }}
	now := time.Date(2026, time.April, 25, 12, 0, 0, 0, time.UTC)

	err := repo.ApplyIncidentMutation(context.Background(), incidents.IncidentMutation{
		ObjectType:               incidents.ObjectTypeMonitoringInstance,
		ObjectID:                 "mi_missing",
		ExpectedObjectRowVersion: "71",
		Active: []incidents.IncidentRecord{{
			IncidentID:      "inc_monitoring_instance_mi_missing_monitoring_instance_disk_pressure",
			ObjectType:      incidents.ObjectTypeMonitoringInstance,
			ObjectID:        "mi_missing",
			IncidentClass:   incidents.IncidentMonitoringInstanceDiskPressure,
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

type incidentTxOperationKind string

const (
	incidentTxOperationExec     incidentTxOperationKind = "exec"
	incidentTxOperationQueryRow incidentTxOperationKind = "query_row"
)

type incidentTxOperation struct {
	kind incidentTxOperationKind
	sql  string
	args []any
}

type fakeIncidentTx struct {
	execSQL                               []string
	operations                            []incidentTxOperation
	commitCalls                           int
	rollbackCalls                         int
	objectRowVersion                      string
	objectGuardErr                        error
	summaryCount                          int
	summarySeverity                       string
	summaryText                           string
	updateRows                            int64
	failActiveIncidentInsertWithoutUpsert bool
}

func (f *fakeIncidentTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.execSQL = append(f.execSQL, sql)
	f.operations = append(f.operations, incidentTxOperation{kind: incidentTxOperationExec, sql: sql, args: append([]any(nil), args...)})
	if containsSQL([]string{sql}, "insert into active_incidents") && f.failActiveIncidentInsertWithoutUpsert && !containsSQL([]string{sql}, "on conflict (incident_id)") {
		return pgconn.CommandTag{}, &pgconn.PgError{
			Code:           "23505",
			Message:        "duplicate key value violates unique constraint \"active_incidents_pkey\"",
			ConstraintName: "active_incidents_pkey",
		}
	}
	if containsSQL([]string{sql}, "update monitoring_instances") || containsSQL([]string{sql}, "update targets") {
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

func (f *fakeIncidentTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.operations = append(f.operations, incidentTxOperation{kind: incidentTxOperationQueryRow, sql: sql, args: append([]any(nil), args...)})
	canonical := canonicalIncidentSQL(sql)
	if canonical == canonicalIncidentSQL(`select xmin::text from monitoring_instances where monitoring_instance_id = $1 for update`) ||
		canonical == canonicalIncidentSQL(`select xmin::text from targets where target_id = $1 for update`) {
		return fakeRow{scan: func(dest ...any) error {
			if f.objectGuardErr != nil {
				return f.objectGuardErr
			}
			*(dest[0].(*string)) = f.objectRowVersion
			return nil
		}}
	}
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

func assertNotContainsSQL(t *testing.T, sqls []string, unwanted string) {
	t.Helper()
	for _, sql := range sqls {
		if containsSQL([]string{sql}, unwanted) {
			t.Fatalf("SQL unexpectedly contains %q in %#v", unwanted, sqls)
		}
	}
}

func canonicalIncidentSQL(sql string) string {
	return strings.Join(strings.Fields(sql), " ")
}
