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

	"houfeng/internal/center/commandaudits"
)

func TestListCommandAuditsUsesBoundedTwoQueryKeysetAndLiteralFilters(t *testing.T) {
	t.Parallel()

	startedFrom := time.Date(2026, time.June, 12, 0, 0, 0, 0, time.UTC)
	startedTo := time.Date(2026, time.July, 12, 0, 0, 0, 0, time.UTC)
	beforeStartedAt := time.Date(2026, time.July, 11, 0, 0, 0, 0, time.UTC)
	firstStartedAt := beforeStartedAt.Add(-time.Minute)
	rejectedStartedAt := beforeStartedAt.Add(-2 * time.Minute)
	normalActionID := "act_001"
	actorUserID := "usr_001"
	completedExitCode := 0

	db := &fakeCommandAuditDB{query: func(call int, _ string, _ ...any) (pgx.Rows, error) {
		switch call {
		case 0:
			return &fakeCommandAuditRows{scans: []fakeCommandAuditScan{
				commandAuditActionScan("act_001", &normalActionID, "mi_001", "Tokyo Edge", false, "uptime", "standard", "succeeded", &actorUserID, "admin", "管理员", firstStartedAt),
				commandAuditActionScan("cmd_aud_rejected", nil, "mi_deleted", "", true, "systemctl_status", "sensitive", "rejected", &actorUserID, "", "", rejectedStartedAt),
				commandAuditActionScan("act_extra", commandAuditStringPtr("act_extra"), "mi_extra", "Extra", false, "uptime", "standard", "queued", nil, "", "", rejectedStartedAt.Add(-time.Minute)),
			}}, nil
		case 1:
			return &fakeCommandAuditRows{scans: []fakeCommandAuditScan{
				commandAuditEventScan("act_001", "cmd_aud_queued", "queued", "web", firstStartedAt, nil, ""),
				commandAuditEventScan("act_001", "cmd_aud_completed", "completed", "agent_sync", firstStartedAt.Add(time.Minute), &completedExitCode, ""),
				commandAuditEventScan("cmd_aud_rejected", "cmd_aud_rejected", "rejected", "web", rejectedStartedAt, nil, "sensitive_confirmation_required"),
			}}, nil
		default:
			t.Fatalf("unexpected query call %d", call)
			return nil, nil
		}
	}}
	repo := newPostgresCommandAuditRepository(db)

	page, err := repo.ListCommandAudits(context.Background(), commandaudits.Query{
		StartedFrom:        &startedFrom,
		StartedTo:          startedTo,
		MonitoringInstance: `edge%_\x`,
		CommandID:          "uptime",
		Sensitivity:        "standard",
		Outcome:            "succeeded",
		Actor:              `admin%_\x`,
		ActionID:           "act_001",
		Limit:              2,
		BeforeStartedAt:    &beforeStartedAt,
		BeforeID:           "act_before",
	})
	if err != nil {
		t.Fatalf("ListCommandAudits() error = %v", err)
	}
	if db.queryCalls != 2 {
		t.Fatalf("query calls = %d, want exactly 2", db.queryCalls)
	}
	if !page.HasMore || len(page.Items) != 2 {
		t.Fatalf("page = %#v, want two items and HasMore", page)
	}
	if page.Items[0].ID != "act_001" || page.Items[0].ActionID != "act_001" || page.Items[0].Outcome != "succeeded" {
		t.Fatalf("first item = %#v", page.Items[0])
	}
	if len(page.Items[0].Events) != 2 || page.Items[0].Events[1].ExitCode == nil || *page.Items[0].Events[1].ExitCode != 0 {
		t.Fatalf("first events = %#v", page.Items[0].Events)
	}
	if page.Items[1].ID != "cmd_aud_rejected" || page.Items[1].ActionID != "" || !page.Items[1].MonitoringInstance.Deleted {
		t.Fatalf("rejected item = %#v", page.Items[1])
	}
	if page.Items[1].MonitoringInstance.Name != "mi_deleted" {
		t.Fatalf("deleted instance fallback name = %q, want stable id", page.Items[1].MonitoringInstance.Name)
	}
	if page.Items[1].Actor == nil || page.Items[1].Actor.Username != "usr_001" || page.Items[1].Actor.DisplayName != "usr_001" {
		t.Fatalf("actor fallback = %#v", page.Items[1].Actor)
	}
	if len(page.Items[1].Events) != 1 || page.Items[1].Events[0].RejectionReason != "sensitive_confirmation_required" {
		t.Fatalf("rejected events = %#v", page.Items[1].Events)
	}

	firstSQL := oneLineStoreSQL(db.sql[0])
	for _, want := range []string{
		"event_type in ('queued', 'rejected')",
		"occurred_at <= $2",
		"when s.event_type = 'rejected' then 'rejected'",
		"when action_state.has_completed and action_state.completed_exit_code = 0 then 'succeeded'",
		"when action_state.has_completed then 'failed'",
		"when action_state.has_dispatched then 'dispatched'",
		"else 'queued'",
		"ilike $3 escape '\\'",
		"ilike $7 escape '\\'",
		"(c.started_at, c.id) < ($9::timestamptz, $10::text)",
		"order by started_at desc, id desc",
		"limit $11",
	} {
		if !strings.Contains(strings.ToLower(firstSQL), strings.ToLower(want)) {
			t.Fatalf("action query missing %q: %s", want, firstSQL)
		}
	}
	firstArgs := db.args[0]
	if len(firstArgs) != 11 {
		t.Fatalf("first query args = %#v, want 11", firstArgs)
	}
	if firstArgs[2] != `%edge\%\_\\x%` || firstArgs[6] != `%admin\%\_\\x%` {
		t.Fatalf("literal filter args = (%q, %q)", firstArgs[2], firstArgs[6])
	}
	if firstArgs[10] != 3 {
		t.Fatalf("limit arg = %#v, want limit+1 = 3", firstArgs[10])
	}
	secondArgs := db.args[1]
	if len(secondArgs) != 3 {
		t.Fatalf("event query args = %#v, want normal ids, rejected ids, upper bound", secondArgs)
	}
	if !reflect.DeepEqual(secondArgs[0], []string{"act_001"}) || !reflect.DeepEqual(secondArgs[1], []string{"cmd_aud_rejected"}) || secondArgs[2] != startedTo {
		t.Fatalf("event query args = %#v", secondArgs)
	}
	secondSQL := oneLineStoreSQL(db.sql[1])
	if !strings.Contains(strings.ToLower(secondSQL), "order by group_id asc, occurred_at asc, audit_id asc") {
		t.Fatalf("event query ordering = %s", secondSQL)
	}
}

func TestListCommandAuditsMapsAllOutcomesAndPreservesStableActionOrder(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.July, 12, 9, 0, 0, 0, time.UTC)
	outcomes := []string{"rejected", "succeeded", "failed", "dispatched", "queued"}
	actionIDs := []string{"", "act_success", "act_failure", "act_dispatched", "act_queued"}
	db := &fakeCommandAuditDB{query: func(call int, _ string, _ ...any) (pgx.Rows, error) {
		if call == 1 {
			return &fakeCommandAuditRows{}, nil
		}
		scans := make([]fakeCommandAuditScan, 0, len(outcomes))
		for i, outcome := range outcomes {
			id := actionIDs[i]
			var actionID *string
			if id == "" {
				id = "cmd_aud_rejected"
			} else {
				actionID = commandAuditStringPtr(id)
			}
			scans = append(scans, commandAuditActionScan(
				id,
				actionID,
				"mi_001",
				"Tokyo",
				false,
				"uptime",
				"standard",
				outcome,
				nil,
				"",
				"",
				startedAt,
			))
		}
		return &fakeCommandAuditRows{scans: scans}, nil
	}}
	repo := newPostgresCommandAuditRepository(db)

	page, err := repo.ListCommandAudits(context.Background(), commandaudits.Query{StartedTo: startedAt.Add(time.Minute), Limit: 5})
	if err != nil {
		t.Fatalf("ListCommandAudits() error = %v", err)
	}
	if page.HasMore || len(page.Items) != len(outcomes) {
		t.Fatalf("page = %#v", page)
	}
	for i, want := range outcomes {
		if page.Items[i].Outcome != want {
			t.Fatalf("item[%d].Outcome = %q, want %q", i, page.Items[i].Outcome, want)
		}
		if page.Items[i].Actor != nil {
			t.Fatalf("item[%d].Actor = %#v, want nil", i, page.Items[i].Actor)
		}
		if page.Items[i].Events == nil {
			t.Fatalf("item[%d].Events = nil, want empty array", i)
		}
	}
}

func TestListCommandAuditsReturnsEmptyNonNilItemsWithConstantQueryCount(t *testing.T) {
	t.Parallel()

	db := &fakeCommandAuditDB{query: func(_ int, _ string, _ ...any) (pgx.Rows, error) {
		return &fakeCommandAuditRows{}, nil
	}}
	repo := newPostgresCommandAuditRepository(db)

	page, err := repo.ListCommandAudits(context.Background(), commandaudits.Query{
		StartedTo: time.Date(2026, time.July, 12, 0, 0, 0, 0, time.UTC),
		Limit:     20,
	})
	if err != nil {
		t.Fatalf("ListCommandAudits() error = %v", err)
	}
	if page.Items == nil || len(page.Items) != 0 || page.HasMore {
		t.Fatalf("page = %#v, want empty non-nil page", page)
	}
	if db.queryCalls != 2 {
		t.Fatalf("query calls = %d, want exactly 2 for empty page", db.queryCalls)
	}
}

func TestListCommandAuditsPropagatesBothQueryStagesAndRowErrors(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("database failed")
	now := time.Date(2026, time.July, 12, 0, 0, 0, 0, time.UTC)
	actionID := "act_001"
	actionRow := commandAuditActionScan("act_001", &actionID, "mi_001", "Tokyo", false, "uptime", "standard", "queued", nil, "", "", now)

	tests := []struct {
		name string
		db   *fakeCommandAuditDB
		want string
	}{
		{
			name: "action query",
			db: &fakeCommandAuditDB{query: func(_ int, _ string, _ ...any) (pgx.Rows, error) {
				return nil, wantErr
			}},
			want: "query command audit actions",
		},
		{
			name: "action scan",
			db: &fakeCommandAuditDB{query: func(call int, _ string, _ ...any) (pgx.Rows, error) {
				if call == 0 {
					return &fakeCommandAuditRows{scans: []fakeCommandAuditScan{func(...any) error { return wantErr }}}, nil
				}
				return &fakeCommandAuditRows{}, nil
			}},
			want: "scan command audit action",
		},
		{
			name: "action rows",
			db: &fakeCommandAuditDB{query: func(call int, _ string, _ ...any) (pgx.Rows, error) {
				if call == 0 {
					return &fakeCommandAuditRows{err: wantErr}, nil
				}
				return &fakeCommandAuditRows{}, nil
			}},
			want: "iterate command audit actions",
		},
		{
			name: "event query",
			db: &fakeCommandAuditDB{query: func(call int, _ string, _ ...any) (pgx.Rows, error) {
				if call == 0 {
					return &fakeCommandAuditRows{scans: []fakeCommandAuditScan{actionRow}}, nil
				}
				return nil, wantErr
			}},
			want: "query command audit events",
		},
		{
			name: "event scan",
			db: &fakeCommandAuditDB{query: func(call int, _ string, _ ...any) (pgx.Rows, error) {
				if call == 0 {
					return &fakeCommandAuditRows{scans: []fakeCommandAuditScan{actionRow}}, nil
				}
				return &fakeCommandAuditRows{scans: []fakeCommandAuditScan{func(...any) error { return wantErr }}}, nil
			}},
			want: "scan command audit event",
		},
		{
			name: "event rows",
			db: &fakeCommandAuditDB{query: func(call int, _ string, _ ...any) (pgx.Rows, error) {
				if call == 0 {
					return &fakeCommandAuditRows{scans: []fakeCommandAuditScan{actionRow}}, nil
				}
				return &fakeCommandAuditRows{err: wantErr}, nil
			}},
			want: "iterate command audit events",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newPostgresCommandAuditRepository(tt.db)
			_, err := repo.ListCommandAudits(context.Background(), commandaudits.Query{StartedTo: now, Limit: 20})
			if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ListCommandAudits() error = %v, want wrapped %q", err, tt.want)
			}
		})
	}
}

func TestCommandAuditReadTypesContainNoOutputOrDetailsFields(t *testing.T) {
	t.Parallel()

	for _, value := range []any{commandaudits.Action{}, commandaudits.Event{}} {
		typeOf := reflect.TypeOf(value)
		for i := 0; i < typeOf.NumField(); i++ {
			field := strings.ToLower(typeOf.Field(i).Name + " " + typeOf.Field(i).Tag.Get("json"))
			if strings.Contains(field, "stdout") || strings.Contains(field, "stderr") || strings.Contains(field, "details") {
				t.Fatalf("%s contains forbidden output/details field %q", typeOf.Name(), typeOf.Field(i).Name)
			}
		}
	}
}

type fakeCommandAuditDB struct {
	query      func(call int, sql string, args ...any) (pgx.Rows, error)
	queryCalls int
	sql        []string
	args       [][]any
}

func (f *fakeCommandAuditDB) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	call := f.queryCalls
	f.queryCalls++
	f.sql = append(f.sql, sql)
	f.args = append(f.args, append([]any(nil), args...))
	return f.query(call, sql, args...)
}

type fakeCommandAuditScan func(dest ...any) error

type fakeCommandAuditRows struct {
	scans []fakeCommandAuditScan
	idx   int
	err   error
}

func (f *fakeCommandAuditRows) Close()                                       {}
func (f *fakeCommandAuditRows) Err() error                                   { return f.err }
func (f *fakeCommandAuditRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (f *fakeCommandAuditRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (f *fakeCommandAuditRows) RawValues() [][]byte                          { return nil }
func (f *fakeCommandAuditRows) Values() ([]any, error)                       { return nil, nil }
func (f *fakeCommandAuditRows) Conn() *pgx.Conn                              { return nil }
func (f *fakeCommandAuditRows) Next() bool {
	if f.idx >= len(f.scans) {
		return false
	}
	f.idx++
	return true
}
func (f *fakeCommandAuditRows) Scan(dest ...any) error {
	return f.scans[f.idx-1](dest...)
}

func commandAuditActionScan(id string, actionID *string, monitoringInstanceID, monitoringInstanceName string, deleted bool, commandID, sensitivity, outcome string, actorUserID *string, actorUsername, actorDisplayName string, startedAt time.Time) fakeCommandAuditScan {
	return func(dest ...any) error {
		*(dest[0].(*string)) = id
		*(dest[1].(**string)) = actionID
		*(dest[2].(*string)) = monitoringInstanceID
		*(dest[3].(*string)) = monitoringInstanceName
		*(dest[4].(*bool)) = deleted
		*(dest[5].(*string)) = commandID
		*(dest[6].(*string)) = sensitivity
		*(dest[7].(*string)) = outcome
		*(dest[8].(**string)) = actorUserID
		*(dest[9].(*string)) = actorUsername
		*(dest[10].(*string)) = actorDisplayName
		*(dest[11].(*time.Time)) = startedAt
		return nil
	}
}

func commandAuditEventScan(groupID, auditID, eventType, source string, occurredAt time.Time, exitCode *int, rejectionReason string) fakeCommandAuditScan {
	return func(dest ...any) error {
		*(dest[0].(*string)) = groupID
		*(dest[1].(*string)) = auditID
		*(dest[2].(*string)) = eventType
		*(dest[3].(*string)) = source
		*(dest[4].(*time.Time)) = occurredAt
		*(dest[5].(**int)) = exitCode
		*(dest[6].(*string)) = rejectionReason
		return nil
	}
}

func commandAuditStringPtr(value string) *string {
	return &value
}
