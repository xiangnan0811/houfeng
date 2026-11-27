package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/incidents"
)

func TestPostgresIncidentRepositoryCommitsMatchingObjectVersion(t *testing.T) {
	for _, objectType := range []incidents.ObjectType{incidents.ObjectTypeMonitoringInstance, incidents.ObjectTypeTarget} {
		t.Run(string(objectType), func(t *testing.T) {
			tx := &fakeIncidentTx{objectRowVersion: "47", summarySeverity: string(incidents.SeverityNormal)}
			repo := &PostgresIncidentRepository{beginTx: func(context.Context, pgx.TxOptions) (incidentStoreTx, error) { return tx, nil }}
			err := repo.ApplyIncidentMutation(context.Background(), incidents.IncidentMutation{
				ObjectType: objectType, ObjectID: "source_001", ExpectedObjectRowVersion: "47",
			})
			if err != nil {
				t.Fatalf("ApplyIncidentMutation() error = %v", err)
			}
			if tx.commitCalls != 1 {
				t.Fatal("matching object version was not committed")
			}
		})
	}
}

func TestPostgresIncidentRepositoryRejectsUnavailableSourceWithoutMutation(t *testing.T) {
	for _, blocked := range []string{"monitoring_instance/mi_001", "vps/vps_001"} {
		t.Run(blocked, func(t *testing.T) {
			tx := &fakeIncidentTx{sourceState: fakeSourceWriterState{
				blocked: map[string]bool{blocked: true},
				parents: map[string]string{"mi_001": "vps_001"},
			}}
			repo := &PostgresIncidentRepository{beginTx: func(context.Context, pgx.TxOptions) (incidentStoreTx, error) { return tx, nil }}
			err := repo.ApplyIncidentMutation(context.Background(), incidents.IncidentMutation{
				ObjectType: incidents.ObjectTypeMonitoringInstance, ObjectID: "mi_001", ExpectedObjectRowVersion: "47",
			})
			if !errors.Is(err, incidents.ErrIncidentProjectionObjectNotFound) {
				t.Fatalf("ApplyIncidentMutation() error = %v, want source unavailable", err)
			}
			if len(tx.execSQL) != 0 || tx.commitCalls != 0 || tx.rollbackCalls != 1 {
				t.Fatal("unavailable source was mutated or failed to roll back")
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
	}{
		{
			name: "empty token",
			mutation: incidents.IncidentMutation{
				ObjectType: incidents.ObjectTypeMonitoringInstance,
				ObjectID:   "mi_001",
			},
			wantErrorPart: "incident mutation object row version is required",
		},
		{
			name: "missing monitoring instance",
			mutation: incidents.IncidentMutation{
				ObjectType:               incidents.ObjectTypeMonitoringInstance,
				ObjectID:                 "mi_missing",
				ExpectedObjectRowVersion: "53",
			},
			guardErr:      pgx.ErrNoRows,
			wantCause:     pgx.ErrNoRows,
			wantMissing:   true,
			wantErrorPart: "incident mutation object not found",
		},
		{
			name: "missing target",
			mutation: incidents.IncidentMutation{
				ObjectType:               incidents.ObjectTypeTarget,
				ObjectID:                 "tg_missing",
				ExpectedObjectRowVersion: "55",
			},
			guardErr:      pgx.ErrNoRows,
			wantCause:     pgx.ErrNoRows,
			wantMissing:   true,
			wantErrorPart: "incident mutation object not found",
		},
		{
			name: "ordinary guard database error",
			mutation: incidents.IncidentMutation{
				ObjectType:               incidents.ObjectTypeTarget,
				ObjectID:                 "tg_guard_error",
				ExpectedObjectRowVersion: "57",
			},
			guardErr:      ordinaryGuardErr,
			wantCause:     ordinaryGuardErr,
			wantErrorPart: "query incident mutation object row version",
		},
		{
			name: "unsupported object",
			mutation: incidents.IncidentMutation{
				ObjectType:               incidents.ObjectTypeVPS,
				ObjectID:                 "vps_001",
				ExpectedObjectRowVersion: "59",
			},
			wantErrorPart: "unsupported incident mutation object type",
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
			if len(tx.execSQL) != 0 {
				t.Fatalf("execSQL = %#v, want zero destructive DML", tx.execSQL)
			}
			if tx.commitCalls != 0 {
				t.Fatalf("commitCalls = %d, want 0", tx.commitCalls)
			}
		})
	}
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

type fakeIncidentTx struct {
	sourceState      fakeSourceWriterState
	execSQL          []string
	commitCalls      int
	rollbackCalls    int
	objectRowVersion string
	objectGuardErr   error
	summaryCount     int
	summarySeverity  string
	summaryText      string
	updateRows       int64
}

func (f *fakeIncidentTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(sql, "source_application_admit()") {
		return pgconn.CommandTag{}, f.sourceState.admissionErr
	}
	if strings.Contains(sql, "pg_advisory_xact_lock_shared") {
		return pgconn.CommandTag{}, nil
	}
	f.execSQL = append(f.execSQL, sql)
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
	if row, ok := f.sourceState.queryRow(sql, args...); ok {
		return row
	}
	canonical := canonicalIncidentSQL(sql)
	if canonical == canonicalIncidentSQL(incidentMonitoringInstanceRowVersionGuardSQL) ||
		canonical == canonicalIncidentSQL(incidentTargetRowVersionGuardSQL) {
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
