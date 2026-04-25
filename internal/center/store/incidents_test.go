package store

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/incidents"
)

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
