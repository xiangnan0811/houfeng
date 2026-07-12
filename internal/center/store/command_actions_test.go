package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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

func TestInsertCommandActionAuditUsesCurrentIdentitySnapshots(t *testing.T) {
	exec := &fakeCommandActionAuditExecutor{tag: pgconn.NewCommandTag("INSERT 0 1")}
	occurredAt := time.Date(2026, time.July, 12, 8, 30, 0, 0, time.UTC)

	err := insertCommandActionAudit(context.Background(), exec, commandActionAuditEvent{
		ActionID:             "act_001",
		MonitoringInstanceID: "mi_001",
		CommandID:            "uptime",
		Sensitivity:          "standard",
		EventType:            "queued",
		ActorUserID:          "usr_001",
		Source:               "web",
		OccurredAt:           occurredAt,
	})
	if err != nil {
		t.Fatalf("insertCommandActionAudit() error = %v", err)
	}

	lowerSQL := strings.ToLower(oneLineStoreSQL(exec.sql))
	for _, want := range []string{
		"insert into monitoring_instance_command_action_audit",
		"monitoring_instance_name_snapshot",
		"actor_username_snapshot",
		"actor_display_name_snapshot",
		"select $1",
		"mi.display_name",
		"coalesce(actor.username, '')",
		"coalesce(actor.display_name, '')",
		"from monitoring_instances mi",
		"left join users actor",
		"actor.user_id = nullif($6, '')",
		"mi.monitoring_instance_id = $3",
		"actor.user_id is not null",
	} {
		if !strings.Contains(lowerSQL, want) {
			t.Fatalf("audit insert SQL missing %q: %s", want, oneLineStoreSQL(exec.sql))
		}
	}
	if strings.Contains(lowerSQL, "values (") {
		t.Fatalf("audit insert must use INSERT SELECT, got %s", oneLineStoreSQL(exec.sql))
	}
	if strings.Contains(lowerSQL, "stdout") || strings.Contains(lowerSQL, "stderr") {
		t.Fatalf("audit insert SQL leaked output fields: %s", oneLineStoreSQL(exec.sql))
	}
	if len(exec.args) != 9 {
		t.Fatalf("audit insert args = %#v, want 9 values", exec.args)
	}
	if exec.args[1] != "act_001" || exec.args[2] != "mi_001" || exec.args[5] != "usr_001" || exec.args[8] != occurredAt {
		t.Fatalf("audit insert args = %#v", exec.args)
	}
}

func TestInsertCommandActionAuditWritesFixedRejectedReasonWithoutAction(t *testing.T) {
	exec := &fakeCommandActionAuditExecutor{tag: pgconn.NewCommandTag("INSERT 0 1")}

	err := insertCommandActionAudit(context.Background(), exec, commandActionAuditEvent{
		MonitoringInstanceID: "mi_001",
		CommandID:            "systemctl_status",
		Sensitivity:          "sensitive",
		EventType:            "rejected",
		ActorUserID:          "usr_001",
		Source:               "web",
	})
	if err != nil {
		t.Fatalf("insertCommandActionAudit() error = %v", err)
	}

	lowerSQL := strings.ToLower(oneLineStoreSQL(exec.sql))
	for _, want := range []string{
		"'rejected'",
		"jsonb_build_object('reason', 'sensitive_confirmation_required')",
		"mi.archived_at is null",
		"mi.binding_status = '已绑定'",
		"mi.monitoring_status <> '暂停'",
	} {
		if !strings.Contains(lowerSQL, want) {
			t.Fatalf("rejected audit SQL missing %q: %s", want, oneLineStoreSQL(exec.sql))
		}
	}
	if exec.args[1] != nil {
		t.Fatalf("rejected action arg = %#v, want nil", exec.args[1])
	}
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

func TestRecordRejectedCommandActionUsesSnapshotAuditHelper(t *testing.T) {
	var sql string
	var args []any
	repo := &PostgresMonitoringInstanceRepository{db: fakeMonitoringInstanceDB{
		exec: func(_ context.Context, query string, queryArgs ...any) (pgconn.CommandTag, error) {
			sql = query
			args = append([]any(nil), queryArgs...)
			return pgconn.NewCommandTag("INSERT 0 1"), nil
		},
	}}
	occurredAt := time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC)

	err := repo.RecordRejectedCommandAction(context.Background(), "mi_001", monitoringinstances.RejectedCommandActionInput{
		CommandID:   "systemctl_status",
		Sensitivity: "sensitive",
		ActorUserID: "usr_001",
		Source:      monitoringinstances.CommandActionSourceWeb,
		OccurredAt:  occurredAt,
	})
	if err != nil {
		t.Fatalf("RecordRejectedCommandAction() error = %v", err)
	}
	if !strings.Contains(strings.ToLower(sql), "'rejected'") {
		t.Fatalf("audit SQL = %s, want rejected event", oneLineStoreSQL(sql))
	}
	if len(args) != 9 || args[1] != nil || args[2] != "mi_001" || args[3] != "systemctl_status" || args[4] != "sensitive" || args[5] != "usr_001" || args[6] != monitoringinstances.CommandActionSourceWeb || args[8] != occurredAt {
		t.Fatalf("audit args = %#v", args)
	}
}

func oneLineStoreSQL(sql string) string {
	return strings.Join(strings.Fields(sql), " ")
}
