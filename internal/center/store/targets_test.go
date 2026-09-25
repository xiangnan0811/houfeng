package store

import (
	"context"
	"encoding/json"
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
	eventAt := time.Date(2026, time.July, 1, 20, 0, 0, 0, time.FixedZone("CST", 8*60*60))

	tests := []struct {
		name           string
		action         func(context.Context, *PostgresTargetRepository, string) (targets.TargetRecord, error)
		targetID       string
		sourceStatus   string
		returnedStatus string
		wantEventType  incidents.EventType
		wantSummary    string
		wantPayload    string
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
		},
		{
			name: "archive enabled target",
			action: func(ctx context.Context, repo *PostgresTargetRepository, targetID string) (targets.TargetRecord, error) {
				return repo.ArchiveTarget(ctx, targetID)
			},
			targetID:       "tg_archive",
			sourceStatus:   targets.RunStatusEnabled,
			returnedStatus: targets.RunStatusArchived,
			wantEventType:  incidents.EventTargetArchived,
			wantSummary:    "归档",
			wantPayload:    targets.RunStatusArchived,
		},
		{
			name: "restore archived target to paused",
			action: func(ctx context.Context, repo *PostgresTargetRepository, targetID string) (targets.TargetRecord, error) {
				return repo.RestoreArchivedTargetToPaused(ctx, targetID)
			},
			targetID:       "tg_restore",
			sourceStatus:   targets.RunStatusArchived,
			returnedStatus: targets.RunStatusPaused,
			wantEventType:  incidents.EventTargetRestoredToPaused,
			wantSummary:    "恢复",
			wantPayload:    targets.RunStatusPaused,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var (
				queryRows int
				eventArgs []any
				committed bool
			)
			tx := &fakeTargetTx{
				queryRow: func(_ context.Context, _ string, args ...any) pgx.Row {
					queryRows++
					status := tt.sourceStatus
					if queryRows == 3 {
						status = tt.returnedStatus
					}
					return fakeTargetRow{scan: func(dest ...any) error {
						scanTargetRecordDestinations(dest, targets.TargetRecord{TargetID: tt.targetID, RunStatus: status, UpdatedAt: eventAt})
						return nil
					}}
				},
				query: func(context.Context, string, ...any) (pgx.Rows, error) {
					return &fakeTargetRows{}, nil
				},
				exec: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
					if strings.Contains(sql, "insert into state_change_events") {
						eventArgs = append([]any(nil), args...)
					}
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
			if len(eventArgs) != 8 {
				t.Fatalf("event args = %#v, want 8 fields", eventArgs)
			}
			if eventArgs[1] != string(incidents.ObjectTypeTarget) {
				t.Fatalf("object_type = %#v, want %q", eventArgs[1], incidents.ObjectTypeTarget)
			}
			if eventArgs[2] != tt.targetID {
				t.Fatalf("object_id = %#v, want %q", eventArgs[2], tt.targetID)
			}
			if eventArgs[3] != string(tt.wantEventType) {
				t.Fatalf("event_type = %#v, want %q", eventArgs[3], tt.wantEventType)
			}
			if summary, ok := eventArgs[5].(string); !ok || !strings.Contains(summary, tt.wantSummary) {
				t.Fatalf("summary = %#v, want substring %q", eventArgs[5], tt.wantSummary)
			}
			payload, ok := eventArgs[6].([]byte)
			if !ok || !strings.Contains(string(payload), tt.wantPayload) {
				t.Fatalf("payload = %#v, want status %q", eventArgs[6], tt.wantPayload)
			}
			if !committed {
				t.Fatal("transaction was not committed")
			}
		})
	}
}

func TestTargetRuntimeControlRejectsInvalidTransition(t *testing.T) {
	t.Parallel()

	tx := &fakeTargetTx{
		queryRow: func(context.Context, string, ...any) pgx.Row {
			return fakeTargetRow{scan: func(dest ...any) error {
				scanTargetRecordDestinations(dest, targets.TargetRecord{
					TargetID:  "tg_archived",
					RunStatus: targets.RunStatusArchived,
				})
				return nil
			}}
		},
		query: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &fakeTargetRows{}, nil
		},
	}
	repo := &PostgresTargetRepository{db: fakeTargetDB{
		beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
			return tx, nil
		},
	}}

	_, err := repo.ResumeTargetRun(context.Background(), "tg_archived")
	if !errors.Is(err, ErrInvalidTargetRuntimeTransition) {
		t.Fatalf("ResumeTargetRun() error = %v, want ErrInvalidTargetRuntimeTransition", err)
	}
}

func TestUpdateProbeItemScopesByTargetAndProbeItem(t *testing.T) {
	t.Parallel()

	var gotSQL string
	var gotArgs []any
	repo := &PostgresTargetRepository{db: fakeTargetDB{queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
		gotSQL = sql
		gotArgs = append([]any(nil), args...)
		return fakeTargetRow{scan: func(dest ...any) error {
			*(dest[0].(*string)) = "pb_001"
			*(dest[1].(*string)) = "tg_001"
			*(dest[2].(*string)) = targets.ProbeKindTCP
			*(dest[3].(*bool)) = false
			*(dest[4].(*string)) = targets.FrequencyTier5m
			*(dest[5].(*int)) = 7
			*(dest[6].(*[]byte)) = []byte(`{"port":443}`)
			*(dest[7].(*time.Time)) = time.Date(2026, time.April, 27, 9, 0, 0, 0, time.UTC)
			*(dest[8].(*time.Time)) = time.Date(2026, time.April, 27, 10, 0, 0, 0, time.UTC)
			return nil
		}}
	}}}

	record, err := repo.UpdateProbeItem(context.Background(), "tg_001", "pb_001", targets.UpdateProbeItemInput{
		ProbeKind:      targets.ProbeKindTCP,
		Enabled:        false,
		FrequencyTier:  targets.FrequencyTier5m,
		TimeoutSeconds: 7,
		Config:         json.RawMessage(`{"port":443}`),
	})
	if err != nil {
		t.Fatalf("UpdateProbeItem() error = %v", err)
	}
	if record.ProbeItemID != "pb_001" || record.TargetID != "tg_001" || record.Enabled {
		t.Fatalf("record = %#v, want updated scoped probe item", record)
	}
	for _, snippet := range []string{"update probe_items", "where target_id = $1", "probe_item_id = $2", "returning"} {
		if !strings.Contains(gotSQL, snippet) {
			t.Fatalf("SQL missing %q in %q", snippet, gotSQL)
		}
	}
	if len(gotArgs) != 7 || gotArgs[0] != "tg_001" || gotArgs[1] != "pb_001" {
		t.Fatalf("args = %#v, want target/probe ids first", gotArgs)
	}
}

func TestDeleteProbeItemScopesByTargetAndProbeItem(t *testing.T) {
	t.Parallel()

	var gotSQL string
	var gotArgs []any
	repo := &PostgresTargetRepository{db: fakeTargetDB{exec: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		gotSQL = sql
		gotArgs = append([]any(nil), args...)
		return pgconn.NewCommandTag("DELETE 1"), nil
	}}}

	if err := repo.DeleteProbeItem(context.Background(), "tg_001", "pb_001"); err != nil {
		t.Fatalf("DeleteProbeItem() error = %v", err)
	}
	if !strings.Contains(gotSQL, "delete from probe_items") || !strings.Contains(gotSQL, "target_id = $1") || !strings.Contains(gotSQL, "probe_item_id = $2") {
		t.Fatalf("SQL = %q, want scoped delete", gotSQL)
	}
	if len(gotArgs) != 2 || gotArgs[0] != "tg_001" || gotArgs[1] != "pb_001" {
		t.Fatalf("args = %#v, want target/probe ids", gotArgs)
	}
}

func TestUpdateProbeItemReturnsProbeItemNotFoundWhenTargetExists(t *testing.T) {
	t.Parallel()

	repo := &PostgresTargetRepository{db: fakeTargetDB{queryRow: func(_ context.Context, sql string, _ ...any) pgx.Row {
		if strings.Contains(sql, "update probe_items") {
			return fakeTargetRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
		}
		return fakeTargetRow{scan: func(dest ...any) error {
			scanTargetRecordDestinations(dest, targets.TargetRecord{TargetID: "tg_001"})
			return nil
		}}
	}}}

	_, err := repo.UpdateProbeItem(context.Background(), "tg_001", "pb_missing", targets.UpdateProbeItemInput{
		ProbeKind:      targets.ProbeKindTCP,
		Enabled:        true,
		FrequencyTier:  targets.FrequencyTier1m,
		TimeoutSeconds: 5,
		Config:         json.RawMessage(`{"port":443}`),
	})
	if !errors.Is(err, targets.ErrProbeItemNotFound) {
		t.Fatalf("UpdateProbeItem() error = %v, want ErrProbeItemNotFound", err)
	}
}

func TestDeleteProbeItemReturnsTargetNotFoundWhenTargetMissing(t *testing.T) {
	t.Parallel()

	repo := &PostgresTargetRepository{db: fakeTargetDB{
		exec: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 0"), nil
		},
		queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return fakeTargetRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
		},
	}}

	err := repo.DeleteProbeItem(context.Background(), "tg_missing", "pb_missing")
	if !errors.Is(err, targets.ErrTargetNotFound) {
		t.Fatalf("DeleteProbeItem() error = %v, want ErrTargetNotFound", err)
	}
}

func TestDeleteProbeItemReturnsProbeItemNotFoundWhenTargetExists(t *testing.T) {
	t.Parallel()

	repo := &PostgresTargetRepository{db: fakeTargetDB{
		exec: func(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
			return pgconn.NewCommandTag("DELETE 0"), nil
		},
		queryRow: func(_ context.Context, _ string, _ ...any) pgx.Row {
			return fakeTargetRow{scan: func(dest ...any) error {
				scanTargetRecordDestinations(dest, targets.TargetRecord{TargetID: "tg_001"})
				return nil
			}}
		},
	}}

	err := repo.DeleteProbeItem(context.Background(), "tg_001", "pb_missing")
	if !errors.Is(err, targets.ErrProbeItemNotFound) {
		t.Fatalf("DeleteProbeItem() error = %v, want ErrProbeItemNotFound", err)
	}
}

func TestUpdateTargetMetadataMapsNotFound(t *testing.T) {
	t.Parallel()

	repo := &PostgresTargetRepository{db: fakeTargetDB{
		queryRow: func(context.Context, string, ...any) pgx.Row {
			return fakeTargetRow{scan: func(dest ...any) error {
				return pgx.ErrNoRows
			}}
		},
	}}

	_, err := repo.UpdateTargetMetadata(context.Background(), "tg_missing", targets.UpdateMetadataInput{})
	if !errors.Is(err, targets.ErrTargetNotFound) {
		t.Fatalf("UpdateTargetMetadata() error = %v, want ErrTargetNotFound", err)
	}
}

func TestUpdateTargetMetadataMapsPreconditionMissToConflictWhenTargetExists(t *testing.T) {
	t.Parallel()

	expectedUpdatedAt := time.Date(2026, time.April, 27, 9, 55, 0, 0, time.UTC)
	queryCount := 0
	repo := &PostgresTargetRepository{db: fakeTargetDB{
		queryRow: func(context.Context, string, ...any) pgx.Row {
			queryCount++
			if queryCount == 1 {
				return fakeTargetRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
			}
			return fakeTargetRow{scan: func(dest ...any) error {
				*(dest[0].(*bool)) = true
				return nil
			}}
		},
	}}

	_, err := repo.UpdateTargetMetadata(context.Background(), "tg_001", targets.UpdateMetadataInput{
		Labels:            []string{"edge"},
		Note:              "updated",
		ExpectedUpdatedAt: &expectedUpdatedAt,
	})
	if !errors.Is(err, targets.ErrTargetMetadataConflict) {
		t.Fatalf("UpdateTargetMetadata() error = %v, want ErrTargetMetadataConflict", err)
	}
}

func TestPostgresTargetListHidesTargetsLinkedOnlyToArchivedVPS(t *testing.T) {
	var seenSQL string
	repo := &PostgresTargetRepository{db: fakeTargetDB{
		query: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			seenSQL = sql
			return &fakeTargetRows{}, nil
		},
	}}

	if _, err := repo.ListTargets(context.Background()); err != nil {
		t.Fatalf("ListTargets() error = %v", err)
	}
	for _, snippet := range []string{
		"not exists",
		"asset_services",
		"asset_domains",
		"v.lifecycle_status not in ('cancelled', 'archived')",
	} {
		if !strings.Contains(seenSQL, snippet) {
			t.Fatalf("ListTargets SQL missing %q in %s", snippet, seenSQL)
		}
	}
}

type fakeTargetRows struct{}

func (r *fakeTargetRows) Close()                                       {}
func (r *fakeTargetRows) Err() error                                   { return nil }
func (r *fakeTargetRows) CommandTag() pgconn.CommandTag                { return pgconn.NewCommandTag("SELECT 0") }
func (r *fakeTargetRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeTargetRows) Next() bool                                   { return false }
func (r *fakeTargetRows) Scan(...any) error                            { return nil }
func (r *fakeTargetRows) Values() ([]any, error)                       { return nil, nil }
func (r *fakeTargetRows) RawValues() [][]byte                          { return nil }
func (r *fakeTargetRows) Conn() *pgx.Conn                              { return nil }

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
		return &fakeTargetTx{queryRow: f.queryRow, query: f.query, exec: f.exec}, nil
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
	query    func(context.Context, string, ...any) (pgx.Rows, error)
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
func (f *fakeTargetTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.query != nil {
		return f.query(ctx, sql, args...)
	}
	return &fakeTargetRows{}, nil
}
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
	*(dest[5].(*[]string)) = append([]string(nil), record.ExecutionMonitoringInstanceLabels...)
	*(dest[6].(*string)) = record.RunStatus
	*(dest[7].(*string)) = record.Group
	*(dest[8].(*[]string)) = append([]string(nil), record.Labels...)
	*(dest[9].(*string)) = record.Note
	*(dest[10].(*string)) = record.CurrentHealthStatus
	*(dest[11].(*int)) = record.CurrentActiveIncidentCount
	*(dest[12].(**time.Time)) = cloneTimePtr(record.LastSuccessAt)
	*(dest[13].(**time.Time)) = cloneTimePtr(record.LastFailureAt)
	*(dest[14].(*string)) = record.CurrentPrimaryIssueSummary
	*(dest[15].(*time.Time)) = record.CreatedAt
	*(dest[16].(*time.Time)) = record.UpdatedAt
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
