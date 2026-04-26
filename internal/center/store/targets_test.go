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
	"houfeng/internal/center/targets"
)

func TestTargetRuntimeControlTransitionsWriteEvents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		action          func(context.Context, *PostgresTargetRepository, string) (targets.TargetRecord, error)
		targetID        string
		sourceStatus    string
		returnedStatus  string
		wantEventType   incidents.EventType
		wantSummary     string
		wantPayload     string
		wantSQLSnippets []string
	}{
		{
			name: "enabled to maintenance",
			action: func(ctx context.Context, repo *PostgresTargetRepository, targetID string) (targets.TargetRecord, error) {
				return repo.SetTargetMaintenance(ctx, targetID)
			},
			targetID:       "tg_maintenance",
			sourceStatus:   targets.RunStatusEnabled,
			returnedStatus: targets.RunStatusMaintenance,
			wantEventType:  incidents.EventTargetMaintenanceEntered,
			wantSummary:    "进入维护",
			wantPayload:    targets.RunStatusMaintenance,
			wantSQLSnippets: []string{
				"set run_status = '维护中'",
				"where target_id = $1",
				"run_status = '启用'",
			},
		},
		{
			name: "maintenance to enabled",
			action: func(ctx context.Context, repo *PostgresTargetRepository, targetID string) (targets.TargetRecord, error) {
				return repo.ResumeTargetRun(ctx, targetID)
			},
			targetID:       "tg_resume_maintenance",
			sourceStatus:   targets.RunStatusMaintenance,
			returnedStatus: targets.RunStatusEnabled,
			wantEventType:  incidents.EventTargetMaintenanceExited,
			wantSummary:    "退出维护",
			wantPayload:    targets.RunStatusEnabled,
			wantSQLSnippets: []string{
				"set run_status = '启用'",
				"where target_id = $1",
				"run_status in ('维护中', '暂停')",
			},
		},
		{
			name: "enabled to paused",
			action: func(ctx context.Context, repo *PostgresTargetRepository, targetID string) (targets.TargetRecord, error) {
				return repo.PauseTargetRun(ctx, targetID)
			},
			targetID:       "tg_pause_enabled",
			sourceStatus:   targets.RunStatusEnabled,
			returnedStatus: targets.RunStatusPaused,
			wantEventType:  incidents.EventTargetPaused,
			wantSummary:    "暂停",
			wantPayload:    targets.RunStatusPaused,
			wantSQLSnippets: []string{
				"set run_status = '暂停'",
				"where target_id = $1",
				"run_status in ('启用', '维护中')",
			},
		},
		{
			name: "paused to enabled",
			action: func(ctx context.Context, repo *PostgresTargetRepository, targetID string) (targets.TargetRecord, error) {
				return repo.ResumeTargetRun(ctx, targetID)
			},
			targetID:       "tg_resume_paused",
			sourceStatus:   targets.RunStatusPaused,
			returnedStatus: targets.RunStatusEnabled,
			wantEventType:  incidents.EventTargetResumed,
			wantSummary:    "恢复",
			wantPayload:    targets.RunStatusEnabled,
			wantSQLSnippets: []string{
				"set run_status = '启用'",
				"where target_id = $1",
				"run_status in ('维护中', '暂停')",
			},
		},
		{
			name: "maintenance to paused",
			action: func(ctx context.Context, repo *PostgresTargetRepository, targetID string) (targets.TargetRecord, error) {
				return repo.PauseTargetRun(ctx, targetID)
			},
			targetID:       "tg_pause_maintenance",
			sourceStatus:   targets.RunStatusMaintenance,
			returnedStatus: targets.RunStatusPaused,
			wantEventType:  incidents.EventTargetPaused,
			wantSummary:    "暂停",
			wantPayload:    targets.RunStatusPaused,
			wantSQLSnippets: []string{
				"set run_status = '暂停'",
				"where target_id = $1",
				"run_status in ('启用', '维护中')",
			},
		},
		{
			name: "archive active target",
			action: func(ctx context.Context, repo *PostgresTargetRepository, targetID string) (targets.TargetRecord, error) {
				return repo.ArchiveTarget(ctx, targetID)
			},
			targetID:       "tg_archive",
			sourceStatus:   targets.RunStatusEnabled,
			returnedStatus: targets.RunStatusArchived,
			wantEventType:  incidents.EventTargetArchived,
			wantSummary:    "归档",
			wantPayload:    targets.RunStatusArchived,
			wantSQLSnippets: []string{
				"set run_status = '已归档'",
				"where target_id = $1",
				"run_status in ('启用', '维护中', '暂停')",
			},
		},
		{
			name: "restore archived to paused",
			action: func(ctx context.Context, repo *PostgresTargetRepository, targetID string) (targets.TargetRecord, error) {
				return repo.RestoreArchivedTargetToPaused(ctx, targetID)
			},
			targetID:       "tg_restore",
			sourceStatus:   targets.RunStatusArchived,
			returnedStatus: targets.RunStatusPaused,
			wantEventType:  incidents.EventTargetRestoredToPaused,
			wantSummary:    "恢复",
			wantPayload:    targets.RunStatusPaused,
			wantSQLSnippets: []string{
				"set run_status = '暂停'",
				"where target_id = $1",
				"run_status = '已归档'",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var (
				gotSQL    string
				execSQL   string
				execArgs  []any
				committed bool
			)
			tx := &fakeTargetTx{
				queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
					gotSQL = sql
					if len(args) != 1 || args[0] != tt.targetID {
						t.Fatalf("QueryRow args = %#v, want target id %q", args, tt.targetID)
					}
					return fakeTargetRow{scan: func(dest ...any) error {
						scanTargetRecordDestinations(dest, targets.TargetRecord{TargetID: tt.targetID, RunStatus: tt.returnedStatus})
						if len(dest) > 16 {
							*(dest[16].(*string)) = tt.sourceStatus
						}
						return nil
					}}
				},
				exec: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					execSQL = sql
					execArgs = append([]any(nil), args...)
					return pgconn.NewCommandTag("INSERT 1"), nil
				},
				commit: func(context.Context) error {
					committed = true
					return nil
				},
			}
			repo := &PostgresTargetRepository{db: fakeTargetDB{beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return tx, nil }}}

			record, err := tt.action(context.Background(), repo, tt.targetID)
			if err != nil {
				t.Fatalf("runtime control action error = %v", err)
			}
			if record.RunStatus != tt.returnedStatus {
				t.Fatalf("RunStatus = %q, want %q", record.RunStatus, tt.returnedStatus)
			}
			for _, snippet := range tt.wantSQLSnippets {
				if !strings.Contains(gotSQL, snippet) {
					t.Fatalf("runtime control SQL missing %q in %q", snippet, gotSQL)
				}
			}
			if !strings.Contains(execSQL, "insert into state_change_events") {
				t.Fatalf("event SQL = %q, want state_change_events insert", execSQL)
			}
			if len(execArgs) != 8 {
				t.Fatalf("len(execArgs) = %d, want 8", len(execArgs))
			}
			if execArgs[1] != string(incidents.ObjectTypeTarget) {
				t.Fatalf("object_type = %#v, want %q", execArgs[1], incidents.ObjectTypeTarget)
			}
			if execArgs[2] != tt.targetID {
				t.Fatalf("object_id = %#v, want %q", execArgs[2], tt.targetID)
			}
			if execArgs[3] != string(tt.wantEventType) {
				t.Fatalf("event_type = %#v, want %q", execArgs[3], tt.wantEventType)
			}
			if summary, ok := execArgs[5].(string); !ok || !strings.Contains(summary, tt.wantSummary) {
				t.Fatalf("summary = %#v, want substring %q", execArgs[5], tt.wantSummary)
			}
			payload, ok := execArgs[6].([]byte)
			if !ok || !strings.Contains(string(payload), tt.wantPayload) {
				t.Fatalf("payload = %#v, want status %q", execArgs[6], tt.wantPayload)
			}
			if !committed {
				t.Fatal("transaction was not committed")
			}
		})
	}
}

func TestTargetRuntimeControlRejectsInvalidTransition(t *testing.T) {
	t.Parallel()

	repo := &PostgresTargetRepository{db: fakeTargetDB{
		queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return fakeTargetRow{scan: func(dest ...any) error {
				*(dest[0].(*bool)) = true
				return nil
			}}
		},
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			return &fakeTargetTx{queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
				return fakeTargetRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
			}}, nil
		},
	}}

	_, err := repo.ResumeTargetRun(context.Background(), "tg_archived")
	if !errors.Is(err, ErrInvalidTargetRuntimeTransition) {
		t.Fatalf("ResumeTargetRun() error = %v, want ErrInvalidTargetRuntimeTransition", err)
	}
}

type fakeTargetDB struct {
	queryRow func(context.Context, string, ...any) pgx.Row
	query    func(context.Context, string, ...any) (pgx.Rows, error)
	exec     func(context.Context, string, ...any) (pgconn.CommandTag, error)
	beginTx  func(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

func (f fakeTargetDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRow == nil {
		return fakeTargetRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
	}
	return f.queryRow(ctx, sql, args...)
}

func (f fakeTargetDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.query == nil {
		return nil, nil
	}
	return f.query(ctx, sql, args...)
}

func (f fakeTargetDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.exec == nil {
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}
	return f.exec(ctx, sql, args...)
}

func (f fakeTargetDB) BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error) {
	if f.beginTx == nil {
		return &fakeTargetTx{queryRow: f.queryRow, exec: f.exec}, nil
	}
	return f.beginTx(ctx, txOptions)
}

type fakeTargetRow struct {
	scan func(dest ...any) error
}

func (r fakeTargetRow) Scan(dest ...any) error {
	return r.scan(dest...)
}

type fakeTargetTx struct {
	queryRow func(context.Context, string, ...any) pgx.Row
	exec     func(context.Context, string, ...any) (pgconn.CommandTag, error)
	commit   func(context.Context) error
	rollback func(context.Context) error
}

func (f *fakeTargetTx) Begin(context.Context) (pgx.Tx, error) { return f, nil }
func (f *fakeTargetTx) Commit(ctx context.Context) error {
	if f.commit != nil {
		return f.commit(ctx)
	}
	return nil
}
func (f *fakeTargetTx) Rollback(ctx context.Context) error {
	if f.rollback != nil {
		return f.rollback(ctx)
	}
	return nil
}
func (f *fakeTargetTx) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (f *fakeTargetTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults { return nil }
func (f *fakeTargetTx) LargeObjects() pgx.LargeObjects                         { return pgx.LargeObjects{} }
func (f *fakeTargetTx) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (f *fakeTargetTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if f.exec != nil {
		return f.exec(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("INSERT 1"), nil
}
func (f *fakeTargetTx) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (f *fakeTargetTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRow != nil {
		return f.queryRow(ctx, sql, args...)
	}
	return fakeTargetRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
}
func (f *fakeTargetTx) Conn() *pgx.Conn { return nil }

func scanTargetRecordDestinations(dest []any, record targets.TargetRecord) {
	*(dest[0].(*string)) = record.TargetID
	*(dest[1].(*string)) = record.Name
	*(dest[2].(*string)) = record.TargetType
	*(dest[3].(*string)) = record.Host
	*(dest[4].(**int)) = cloneIntPtr(record.BasePort)
	*(dest[5].(*[]string)) = append([]string(nil), record.ExecutionNodeLabels...)
	*(dest[6].(*string)) = record.RunStatus
	*(dest[7].(*[]string)) = append([]string(nil), record.Labels...)
	*(dest[8].(*string)) = record.Note
	*(dest[9].(*string)) = record.CurrentHealthStatus
	*(dest[10].(*int)) = record.CurrentActiveIncidentCount
	*(dest[11].(**time.Time)) = cloneTimePtr(record.LastSuccessAt)
	*(dest[12].(**time.Time)) = cloneTimePtr(record.LastFailureAt)
	*(dest[13].(*string)) = record.CurrentPrimaryIssueSummary
	*(dest[14].(*time.Time)) = record.CreatedAt
	*(dest[15].(*time.Time)) = record.UpdatedAt
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
