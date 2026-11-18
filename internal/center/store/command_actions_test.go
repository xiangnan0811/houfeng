package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/monitoringinstances"
)

type fakeCommandActionAuditExecutor struct {
	tag  pgconn.CommandTag
	err  error
	sql  string
	args []any
}

func (f *fakeCommandActionAuditExecutor) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.sql = sql
	f.args = append([]any(nil), args...)
	return f.tag, f.err
}

func TestInsertCommandActionAuditRequiresExactlyOneSnapshotSource(t *testing.T) {
	tests := []struct {
		name string
		tag  pgconn.CommandTag
	}{
		{name: "missing instance or actor", tag: pgconn.NewCommandTag("INSERT 0 0")},
		{name: "unexpected duplicate", tag: pgconn.NewCommandTag("INSERT 0 2")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := &fakeCommandActionAuditExecutor{tag: tt.tag}
			err := insertCommandActionAudit(context.Background(), exec, commandActionAuditEvent{
				ActionID:             "act_001",
				MonitoringInstanceID: "mi_001",
				CommandID:            "uptime",
				Sensitivity:          "standard",
				EventType:            "queued",
				Source:               "web",
			})
			if err == nil || !strings.Contains(err.Error(), "inserted exactly one row") {
				t.Fatalf("insertCommandActionAudit() error = %v, want exact-row error", err)
			}
		})
	}
}

func TestInsertCommandActionAuditValidatesEventIdentityBeforeExec(t *testing.T) {
	tests := []struct {
		name  string
		event commandActionAuditEvent
	}{
		{
			name: "queued action missing",
			event: commandActionAuditEvent{
				MonitoringInstanceID: "mi_001", CommandID: "uptime", Sensitivity: "standard", EventType: "queued", Source: "web",
			},
		},
		{
			name: "rejected action present",
			event: commandActionAuditEvent{
				ActionID: "act_001", MonitoringInstanceID: "mi_001", CommandID: "systemctl_status", Sensitivity: "sensitive", EventType: "rejected", Source: "web",
			},
		},
		{
			name: "rejected non web source",
			event: commandActionAuditEvent{
				MonitoringInstanceID: "mi_001", CommandID: "systemctl_status", Sensitivity: "sensitive", EventType: "rejected", Source: "agent_sync",
			},
		},
		{
			name: "unsupported event",
			event: commandActionAuditEvent{
				ActionID: "act_001", MonitoringInstanceID: "mi_001", CommandID: "uptime", Sensitivity: "standard", EventType: "unknown", Source: "web",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := &fakeCommandActionAuditExecutor{tag: pgconn.NewCommandTag("INSERT 0 1")}
			if err := insertCommandActionAudit(context.Background(), exec, tt.event); err == nil {
				t.Fatal("insertCommandActionAudit() error = nil")
			}
			if exec.sql != "" {
				t.Fatalf("executor called with %s", oneLineStoreSQL(exec.sql))
			}
		})
	}
}

func TestInsertCommandActionAuditWrapsExecutorError(t *testing.T) {
	wantErr := errors.New("write failed")
	exec := &fakeCommandActionAuditExecutor{err: wantErr}
	err := insertCommandActionAudit(context.Background(), exec, commandActionAuditEvent{
		ActionID:             "act_001",
		MonitoringInstanceID: "mi_001",
		CommandID:            "uptime",
		Sensitivity:          "standard",
		EventType:            "queued",
		Source:               "web",
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("insertCommandActionAudit() error = %v, want wrapped error", err)
	}
}

func TestRecordRejectedCommandActionSourceAuthority(t *testing.T) {
	for _, tt := range []struct {
		name    string
		blocked bool
	}{
		{name: "writable parent"},
		{name: "retired parent", blocked: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			writes := 0
			tx := &fakeMonitoringInstanceTx{
				sourceState: fakeSourceWriterState{
					parents: map[string]string{"mi_001": "vps_001"},
					blocked: map[string]bool{"vps/vps_001": tt.blocked},
				},
				exec: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
					if strings.Contains(sql, "pg_advisory_xact_lock") {
						return pgconn.NewCommandTag("SELECT 1"), nil
					}
					writes++
					return pgconn.NewCommandTag("INSERT 0 1"), nil
				},
			}
			repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }}}
			err := repo.RecordRejectedCommandAction(context.Background(), "mi_001", monitoringinstances.RejectedCommandActionInput{
				CommandID: "systemctl_status", Sensitivity: "sensitive",
				ActorUserID: "usr_001", Source: monitoringinstances.CommandActionSourceWeb,
			})
			if tt.blocked {
				if !errors.Is(err, monitoringinstances.ErrMonitoringInstanceNotFound) || writes != 0 || tx.commitCalls != 0 || tx.rollbackCalls != 1 {
					t.Fatalf("error = %v, writes = %d, commits = %d, rollbacks = %d", err, writes, tx.commitCalls, tx.rollbackCalls)
				}
			} else if err != nil || writes != 1 || tx.commitCalls != 1 {
				t.Fatalf("error = %v, writes = %d, commits = %d", err, writes, tx.commitCalls)
			}
		})
	}
}

func oneLineStoreSQL(sql string) string {
	return strings.Join(strings.Fields(sql), " ")
}
