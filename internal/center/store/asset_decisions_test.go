package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/assetdecisions"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

func TestPostgresAssetDecisionRepositoryLoadsFactsWithAggregateQuery(t *testing.T) {
	now := time.Date(2026, time.June, 4, 9, 0, 0, 0, time.UTC)
	capturedSQL := ""
	repo := &PostgresAssetDecisionRepository{db: fakeAssetDecisionQueryer{
		query: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			capturedSQL = sql
			providerID := "pv_001"
			renewAt := now.AddDate(0, 0, 7)
			return &fakeAssetDecisionRows{rows: []fakeAssetDecisionScan{{scan: func(dest ...any) error {
				*(dest[0].(*string)) = "vps_001"
				*(dest[1].(*string)) = "Frankfurt Primary"
				*(dest[2].(**string)) = &providerID
				*(dest[3].(*string)) = "Hetzner"
				*(dest[4].(*string)) = "CX22"
				*(dest[5].(*string)) = "order-1"
				*(dest[6].(*string)) = "Germany"
				*(dest[7].(*string)) = "Hesse"
				*(dest[8].(*string)) = "Falkenstein"
				*(dest[9].(*string)) = "FSN1"
				*(dest[10].(*string)) = "192.0.2.10"
				*(dest[11].(*string)) = ""
				*(dest[12].(*string)) = "192.0.2.10"
				*(dest[13].(*int)) = 22
				*(dest[14].(*string)) = "root"
				*(dest[15].(*string)) = "Debian"
				*(dest[16].(*string)) = "kvm"
				*(dest[17].(*vpsassets.LifecycleStatus)) = vpsassets.LifecycleActive
				*(dest[18].(*vpsassets.UsageStatus)) = vpsassets.UsageInUse
				*(dest[19].(*vpsassets.RenewalDecision)) = vpsassets.RenewalUnreviewed
				*(dest[20].(*string)) = "high"
				*(dest[21].(*[]string)) = []string{"edge"}
				*(dest[22].(*string)) = "note"
				*(dest[23].(*int)) = 2
				*(dest[24].(*int)) = 2
				*(dest[25].(*int)) = 1
				*(dest[26].(*time.Time)) = now
				*(dest[27].(*time.Time)) = now
				*(dest[28].(**time.Time)) = nil
				*(dest[29].(*int)) = 1
				*(dest[30].(*int)) = 1
				*(dest[31].(*int)) = 0
				*(dest[32].(*int)) = 2
				*(dest[33].(*int)) = 1
				*(dest[34].(*int)) = 1
				*(dest[35].(*int)) = 1
				*(dest[36].(*int)) = 2
				*(dest[37].(*int)) = 2
				*(dest[38].(*int)) = 1
				*(dest[39].(*int)) = 3
				*(dest[40].(*string)) = "CPU steal 高"
				*(dest[41].(*bool)) = true
				*(dest[42].(*string)) = "sub_001"
				*(dest[43].(*string)) = "vps_001"
				*(dest[44].(*float64)) = 12
				*(dest[45].(*string)) = "USD"
				*(dest[46].(*string)) = "monthly"
				*(dest[47].(*int)) = 1
				*(dest[48].(*string)) = "month"
				*(dest[49].(*int)) = 1
				*(dest[50].(*float64)) = 12
				*(dest[51].(**time.Time)) = nil
				*(dest[52].(**time.Time)) = &renewAt
				*(dest[53].(*bool)) = true
				*(dest[54].(*bool)) = false
				*(dest[55].(*string)) = "auto"
				*(dest[56].(*subscriptions.Status)) = subscriptions.StatusActive
				*(dest[57].(*string)) = "card"
				*(dest[58].(*string)) = "Frankfurt Primary"
				*(dest[59].(*string)) = "prod"
				*(dest[60].(*[]string)) = []string{"prod"}
				*(dest[61].(**time.Time)) = nil
				*(dest[62].(**time.Time)) = nil
				*(dest[63].(*string)) = "subscription note"
				*(dest[64].(**time.Time)) = &now
				*(dest[65].(**time.Time)) = &now
				return nil
			}}}}, nil
		},
	}}

	groups, err := repo.ListGroups(context.Background(), assetdecisions.ListFilters{RenewWithinDays: 30})
	if err != nil {
		t.Fatalf("ListGroups() error = %v", err)
	}
	if len(groups) == 0 {
		t.Fatal("groups = empty, want derived groups from loaded fact")
	}
	for _, want := range []string{
		"from vps_assets",
		"primary_subscriptions",
		"subscription_rollup",
		"from asset_services",
		"from asset_domains",
		"from vps_monitoring_instance_links",
		"join monitoring_instances",
		"join targets",
		"left join providers",
	} {
		if !strings.Contains(capturedSQL, want) {
			t.Fatalf("capturedSQL = %q, want %q", capturedSQL, want)
		}
	}
}

func TestPostgresAssetDecisionRepositoryGetGroupMissing(t *testing.T) {
	repo := &PostgresAssetDecisionRepository{db: fakeAssetDecisionQueryer{
		query: func(context.Context, string, ...any) (pgx.Rows, error) {
			return &fakeAssetDecisionRows{}, nil
		},
	}}

	_, err := repo.GetGroup(context.Background(), "adg_auto_missing", assetdecisions.ListFilters{RenewWithinDays: 30})
	if err != assetdecisions.ErrAssetDecisionGroupNotFound {
		t.Fatalf("GetGroup() error = %v, want not found sentinel", err)
	}
}

func TestPostgresAssetDecisionRepositoryListRecordsScansSnapshots(t *testing.T) {
	now := time.Date(2026, time.June, 5, 9, 0, 0, 0, time.UTC)
	repo := &PostgresAssetDecisionRepository{db: fakeAssetDecisionQueryer{
		query: func(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "asset_decision_records_with_counts") {
				t.Fatalf("query SQL = %q, want records view", sql)
			}
			snapshot, err := json.Marshal(assetdecisions.EvidenceSnapshot{"member_count": float64(2)})
			if err != nil {
				t.Fatalf("marshal snapshot: %v", err)
			}
			return &fakeAssetDecisionRows{rows: []fakeAssetDecisionScan{{scan: scanAssetDecisionRecordSummaryValues(
				"adr_001",
				"德国主备取舍",
				"保留主力并退掉弱承载",
				string(assetdecisions.RecordStatusDraft),
				"auto_group",
				"adg_auto_001",
				string(assetdecisions.GroupRegionPortfolio),
				string(assetdecisions.ViewRegion),
				"country=Germany",
				"Germany",
				30,
				2,
				1,
				1,
				0,
				0,
				0,
				snapshot,
				now,
				now,
				nil,
				nil,
			)}}}, nil
		},
	}}

	records, err := repo.ListRecords(context.Background())
	if err != nil {
		t.Fatalf("ListRecords() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records len = %d, want 1", len(records))
	}
	record := records[0]
	if record.RecordID != "adr_001" || record.Status != assetdecisions.RecordStatusDraft || record.MemberCount != 2 {
		t.Fatalf("record = %#v, want scanned summary", record)
	}
	if record.FollowupTodoCount != 1 || record.FollowupInProgressCount != 1 {
		t.Fatalf("record followup counts = %#v, want todo/in_progress counts", record)
	}
	if got := record.EvidenceSnapshot["member_count"]; got != float64(2) {
		t.Fatalf("snapshot member_count = %#v, want 2", got)
	}
}

func TestPostgresAssetDecisionRepositoryCreateRecordPersistsGroupAndMemberSnapshots(t *testing.T) {
	now := time.Date(2026, time.June, 4, 9, 0, 0, 0, time.UTC)
	var execs []fakeAssetDecisionExecCall
	tx := &fakeAssetDecisionTx{exec: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		execs = append(execs, fakeAssetDecisionExecCall{sql: sql, args: args})
		return pgconn.CommandTag{}, nil
	}}
	repo := &PostgresAssetDecisionRepository{
		db: fakeAssetDecisionQueryer{
			query: func(context.Context, string, ...any) (pgx.Rows, error) {
				return fakeAssetDecisionFactRows(now), nil
			},
		},
		beginTx: func(context.Context, pgx.TxOptions) (assetDecisionTx, error) {
			return tx, nil
		},
	}

	detail, err := repo.CreateRecord(context.Background(), assetdecisions.CreateRecordInput{
		SourceGroupID:   assetdecisions.StableGroupID(assetdecisions.GroupRenewalAttention, "30"),
		RenewWithinDays: 30,
		Title:           "  续费窗口取舍  ",
		Goal:            "退掉闲置机器",
		Status:          assetdecisions.RecordStatusDecided,
		Members: []assetdecisions.CreateRecordMemberInput{{
			VPSID:         "vps_001",
			DecidedRole:   assetdecisions.RoleRetireCandidate,
			DecidedAction: assetdecisions.ActionMigrate,
			Reason:        "先迁移承载服务",
		}},
	})
	if err != nil {
		t.Fatalf("CreateRecord() error = %v", err)
	}
	if !tx.committed {
		t.Fatal("transaction committed = false, want true")
	}
	if detail.RecordID == "" || !strings.HasPrefix(detail.RecordID, "adr_") {
		t.Fatalf("record id = %q, want generated adr id", detail.RecordID)
	}
	if detail.Title != "续费窗口取舍" || detail.Status != assetdecisions.RecordStatusDecided || detail.MemberCount != 1 {
		t.Fatalf("detail summary = %#v, want normalized summary", detail.RecordSummary)
	}
	if detail.FollowupTodoCount != 1 {
		t.Fatalf("detail followup todo count = %d, want 1", detail.FollowupTodoCount)
	}
	if detail.DecidedAt == nil || detail.CompletedAt != nil {
		t.Fatalf("timestamps decided=%v completed=%v, want decided only", detail.DecidedAt, detail.CompletedAt)
	}
	if len(detail.Members) != 1 || detail.Members[0].DecidedRole != assetdecisions.RoleRetireCandidate || detail.Members[0].DecidedAction != assetdecisions.ActionMigrate || detail.Members[0].FollowupStatus != assetdecisions.FollowupTodo {
		t.Fatalf("members = %#v, want user decided role/action", detail.Members)
	}
	if got := detail.EvidenceSnapshot["group_id"]; got != detail.SourceGroupID {
		t.Fatalf("snapshot group_id = %#v, want source group id", got)
	}
	if len(execs) != 2 {
		t.Fatalf("exec count = %d, want record and member inserts", len(execs))
	}
	if !strings.Contains(execs[0].sql, "insert into asset_decision_records") {
		t.Fatalf("record insert SQL = %q", execs[0].sql)
	}
	if !strings.Contains(execs[1].sql, "insert into asset_decision_record_members") {
		t.Fatalf("member insert SQL = %q", execs[1].sql)
	}
}

func TestPostgresAssetDecisionRepositoryCreateRecordRejectsUnknownMember(t *testing.T) {
	now := time.Date(2026, time.June, 4, 9, 0, 0, 0, time.UTC)
	repo := &PostgresAssetDecisionRepository{
		db: fakeAssetDecisionQueryer{
			query: func(context.Context, string, ...any) (pgx.Rows, error) {
				return fakeAssetDecisionFactRows(now), nil
			},
		},
		beginTx: func(context.Context, pgx.TxOptions) (assetDecisionTx, error) {
			t.Fatal("beginTx should not be called for invalid member")
			return nil, nil
		},
	}

	_, err := repo.CreateRecord(context.Background(), assetdecisions.CreateRecordInput{
		SourceGroupID:   assetdecisions.StableGroupID(assetdecisions.GroupRenewalAttention, "30"),
		RenewWithinDays: 30,
		Members:         []assetdecisions.CreateRecordMemberInput{{VPSID: "vps_missing"}},
	})
	if err != assetdecisions.ErrInvalidAssetDecisionInput {
		t.Fatalf("CreateRecord() error = %v, want invalid input", err)
	}
}

func TestPostgresAssetDecisionRepositoryGetAndPatchRecord(t *testing.T) {
	now := time.Date(2026, time.June, 5, 9, 0, 0, 0, time.UTC)
	summarySnapshot, err := json.Marshal(assetdecisions.EvidenceSnapshot{"scope": "provider"})
	if err != nil {
		t.Fatalf("marshal summary snapshot: %v", err)
	}
	memberSnapshot, err := json.Marshal(assetdecisions.EvidenceSnapshot{"service_count": float64(2)})
	if err != nil {
		t.Fatalf("marshal member snapshot: %v", err)
	}
	repo := &PostgresAssetDecisionRepository{db: fakeAssetDecisionQueryer{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			if !strings.Contains(sql, "asset_decision_records") || args[0] != "adr_001" {
				t.Fatalf("QueryRow sql=%q args=%#v, want record lookup/update", sql, args)
			}
			return fakeAssetDecisionRow{scan: scanAssetDecisionRecordSummaryValues(
				"adr_001",
				"服务商组合推进",
				"保留主力",
				string(assetdecisions.RecordStatusInProgress),
				"auto_group",
				"adg_auto_provider",
				string(assetdecisions.GroupProviderPortfolio),
				string(assetdecisions.ViewProvider),
				"provider=pv_001",
				"Hetzner",
				60,
				1,
				0,
				1,
				0,
				0,
				0,
				summarySnapshot,
				now,
				now,
				&now,
				nil,
			)}
		},
		query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			if !strings.Contains(sql, "asset_decision_record_members") || args[0] != "adr_001" {
				t.Fatalf("query sql=%q args=%#v, want members", sql, args)
			}
			return &fakeAssetDecisionRows{rows: []fakeAssetDecisionScan{{scan: scanAssetDecisionRecordMemberValues(
				"adr_001",
				"vps_001",
				"Frankfurt Primary",
				string(assetdecisions.RolePrimaryCandidate),
				string(assetdecisions.RolePrimaryCandidate),
				string(assetdecisions.ActionKeep),
				string(assetdecisions.ActionKeep),
				"主力保留",
				string(assetdecisions.FollowupInProgress),
				"已进入迁移窗口",
				&now,
				memberSnapshot,
				now,
				now,
			)}}}, nil
		},
	}}

	detail, err := repo.GetRecord(context.Background(), " adr_001 ")
	if err != nil {
		t.Fatalf("GetRecord() error = %v", err)
	}
	if detail.RecordID != "adr_001" || len(detail.Members) != 1 || detail.Members[0].DisplayName != "Frankfurt Primary" {
		t.Fatalf("detail = %#v, want scanned detail", detail)
	}
	if detail.FollowupInProgressCount != 1 || detail.Members[0].FollowupStatus != assetdecisions.FollowupInProgress || detail.Members[0].FollowupUpdatedAt == nil {
		t.Fatalf("detail followup = %#v members=%#v, want scanned followup", detail.RecordSummary, detail.Members)
	}
}

func TestPostgresAssetDecisionRepositoryPatchRecordUpdatesMemberFollowup(t *testing.T) {
	now := time.Date(2026, time.June, 5, 9, 0, 0, 0, time.UTC)
	summarySnapshot, err := json.Marshal(assetdecisions.EvidenceSnapshot{"scope": "provider"})
	if err != nil {
		t.Fatalf("marshal summary snapshot: %v", err)
	}
	memberSnapshot, err := json.Marshal(assetdecisions.EvidenceSnapshot{"service_count": float64(2)})
	if err != nil {
		t.Fatalf("marshal member snapshot: %v", err)
	}
	var execs []fakeAssetDecisionExecCall
	tx := &fakeAssetDecisionTx{exec: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		execs = append(execs, fakeAssetDecisionExecCall{sql: sql, args: args})
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}}
	repo := &PostgresAssetDecisionRepository{
		db: fakeAssetDecisionQueryer{
			queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
				if !strings.Contains(sql, "asset_decision_records_with_counts") || args[0] != "adr_001" {
					t.Fatalf("QueryRow sql=%q args=%#v, want record lookup after patch", sql, args)
				}
				return fakeAssetDecisionRow{scan: scanAssetDecisionRecordSummaryValues(
					"adr_001",
					"服务商组合推进",
					"保留主力",
					string(assetdecisions.RecordStatusInProgress),
					"auto_group",
					"adg_auto_provider",
					string(assetdecisions.GroupProviderPortfolio),
					string(assetdecisions.ViewProvider),
					"provider=pv_001",
					"Hetzner",
					60,
					1,
					0,
					0,
					1,
					0,
					0,
					summarySnapshot,
					now,
					now,
					&now,
					nil,
				)}
			},
			query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
				if !strings.Contains(sql, "asset_decision_record_members") || args[0] != "adr_001" {
					t.Fatalf("query sql=%q args=%#v, want members after patch", sql, args)
				}
				return &fakeAssetDecisionRows{rows: []fakeAssetDecisionScan{{scan: scanAssetDecisionRecordMemberValues(
					"adr_001",
					"vps_001",
					"Frankfurt Primary",
					string(assetdecisions.RolePrimaryCandidate),
					string(assetdecisions.RolePrimaryCandidate),
					string(assetdecisions.ActionKeep),
					string(assetdecisions.ActionKeep),
					"主力保留",
					string(assetdecisions.FollowupBlocked),
					"等待迁移窗口",
					&now,
					memberSnapshot,
					now,
					now,
				)}}}, nil
			},
		},
		beginTx: func(context.Context, pgx.TxOptions) (assetDecisionTx, error) {
			return tx, nil
		},
	}

	updated, err := repo.PatchRecord(context.Background(), "adr_001", assetdecisions.PatchRecordInput{
		Status: assetdecisions.PatchRecordStatus{Set: true, Value: assetdecisions.RecordStatusInProgress},
		Members: []assetdecisions.PatchRecordMemberInput{{
			VPSID:          " vps_001 ",
			FollowupStatus: assetdecisions.PatchFollowupStatus{Set: true, Value: assetdecisions.FollowupBlocked},
			FollowupNote:   assetdecisions.PatchString{Set: true, Value: " 等待迁移窗口 "},
		}},
	})
	if err != nil {
		t.Fatalf("PatchRecord() error = %v", err)
	}
	if !tx.committed {
		t.Fatal("transaction committed = false, want true")
	}
	if len(execs) != 2 {
		t.Fatalf("exec count = %d, want record and member updates", len(execs))
	}
	if !strings.Contains(execs[0].sql, "update asset_decision_records") || !strings.Contains(execs[1].sql, "update asset_decision_record_members") {
		t.Fatalf("exec SQL = %#v, want record then member updates", execs)
	}
	if execs[1].args[1] != "vps_001" || execs[1].args[5] != "等待迁移窗口" {
		t.Fatalf("member update args = %#v, want normalized member patch", execs[1].args)
	}
	if updated.Status != assetdecisions.RecordStatusInProgress || updated.DecidedAt == nil || updated.FollowupBlockedCount != 1 {
		t.Fatalf("updated = %#v, want in_progress with blocked followup count", updated.RecordSummary)
	}
	if len(updated.Members) != 1 || updated.Members[0].FollowupStatus != assetdecisions.FollowupBlocked || updated.Members[0].FollowupNote != "等待迁移窗口" {
		t.Fatalf("updated members = %#v, want blocked member followup", updated.Members)
	}
}

func TestPostgresAssetDecisionRepositoryPatchRecordUnknownMemberRollsBack(t *testing.T) {
	var execs []fakeAssetDecisionExecCall
	tx := &fakeAssetDecisionTx{exec: func(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
		execs = append(execs, fakeAssetDecisionExecCall{sql: sql, args: args})
		if strings.Contains(sql, "asset_decision_record_members") {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		return pgconn.NewCommandTag("UPDATE 1"), nil
	}}
	repo := &PostgresAssetDecisionRepository{
		db: fakeAssetDecisionQueryer{},
		beginTx: func(context.Context, pgx.TxOptions) (assetDecisionTx, error) {
			return tx, nil
		},
	}

	_, err := repo.PatchRecord(context.Background(), "adr_001", assetdecisions.PatchRecordInput{
		Members: []assetdecisions.PatchRecordMemberInput{{
			VPSID:          "vps_missing",
			FollowupStatus: assetdecisions.PatchFollowupStatus{Set: true, Value: assetdecisions.FollowupDone},
		}},
	})
	if err != assetdecisions.ErrInvalidAssetDecisionInput {
		t.Fatalf("PatchRecord() error = %v, want invalid input", err)
	}
	if tx.committed {
		t.Fatal("transaction committed = true, want rollback")
	}
	if !tx.rolled {
		t.Fatal("transaction rolled = false, want rollback")
	}
	if len(execs) != 2 {
		t.Fatalf("exec count = %d, want record update then member update", len(execs))
	}
}

type fakeAssetDecisionQueryer struct {
	query    func(context.Context, string, ...any) (pgx.Rows, error)
	queryRow func(context.Context, string, ...any) pgx.Row
}

func (f fakeAssetDecisionQueryer) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.query == nil {
		return &fakeAssetDecisionRows{}, nil
	}
	return f.query(ctx, sql, args...)
}

func (f fakeAssetDecisionQueryer) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRow == nil {
		return fakeAssetDecisionRow{scan: func(...any) error {
			return pgx.ErrNoRows
		}}
	}
	return f.queryRow(ctx, sql, args...)
}

type fakeAssetDecisionRow struct {
	scan func(dest ...any) error
}

func (r fakeAssetDecisionRow) Scan(dest ...any) error {
	if r.scan == nil {
		return nil
	}
	return r.scan(dest...)
}

type fakeAssetDecisionScan struct {
	scan func(dest ...any) error
}

type fakeAssetDecisionRows struct {
	rows []fakeAssetDecisionScan
	idx  int
	err  error
}

func (r *fakeAssetDecisionRows) Close() {}

func (r *fakeAssetDecisionRows) Err() error {
	return r.err
}

func (r *fakeAssetDecisionRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}

func (r *fakeAssetDecisionRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}

func (r *fakeAssetDecisionRows) Next() bool {
	if r.idx >= len(r.rows) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeAssetDecisionRows) Scan(dest ...any) error {
	return r.rows[r.idx-1].scan(dest...)
}

func (r *fakeAssetDecisionRows) Values() ([]any, error) {
	return nil, nil
}

func (r *fakeAssetDecisionRows) RawValues() [][]byte {
	return nil
}

func (r *fakeAssetDecisionRows) Conn() *pgx.Conn {
	return nil
}

type fakeAssetDecisionExecCall struct {
	sql  string
	args []any
}

type fakeAssetDecisionTx struct {
	exec      func(context.Context, string, ...any) (pgconn.CommandTag, error)
	query     func(context.Context, string, ...any) (pgx.Rows, error)
	queryRow  func(context.Context, string, ...any) pgx.Row
	committed bool
	rolled    bool
}

func (tx *fakeAssetDecisionTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if tx.exec == nil {
		return pgconn.CommandTag{}, nil
	}
	return tx.exec(ctx, sql, args...)
}

func (tx *fakeAssetDecisionTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if tx.query == nil {
		return &fakeAssetDecisionRows{}, nil
	}
	return tx.query(ctx, sql, args...)
}

func (tx *fakeAssetDecisionTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if tx.queryRow == nil {
		return fakeAssetDecisionRow{}
	}
	return tx.queryRow(ctx, sql, args...)
}

func (tx *fakeAssetDecisionTx) Commit(context.Context) error {
	tx.committed = true
	return nil
}

func (tx *fakeAssetDecisionTx) Rollback(context.Context) error {
	tx.rolled = true
	return nil
}

func scanAssetDecisionRecordSummaryValues(
	recordID string,
	title string,
	goal string,
	status string,
	sourceType string,
	sourceGroupID string,
	sourceGroupType string,
	sourceView string,
	scopeKey string,
	scopeLabel string,
	renewWithinDays int,
	memberCount int,
	followupTodoCount int,
	followupInProgressCount int,
	followupBlockedCount int,
	followupDoneCount int,
	followupSkippedCount int,
	evidenceSnapshot []byte,
	createdAt time.Time,
	updatedAt time.Time,
	decidedAt *time.Time,
	completedAt *time.Time,
) func(dest ...any) error {
	return func(dest ...any) error {
		*(dest[0].(*string)) = recordID
		*(dest[1].(*string)) = title
		*(dest[2].(*string)) = goal
		*(dest[3].(*string)) = status
		*(dest[4].(*string)) = sourceType
		*(dest[5].(*string)) = sourceGroupID
		*(dest[6].(*string)) = sourceGroupType
		*(dest[7].(*string)) = sourceView
		*(dest[8].(*string)) = scopeKey
		*(dest[9].(*string)) = scopeLabel
		*(dest[10].(*int)) = renewWithinDays
		*(dest[11].(*int)) = memberCount
		*(dest[12].(*int)) = followupTodoCount
		*(dest[13].(*int)) = followupInProgressCount
		*(dest[14].(*int)) = followupBlockedCount
		*(dest[15].(*int)) = followupDoneCount
		*(dest[16].(*int)) = followupSkippedCount
		*(dest[17].(*[]byte)) = evidenceSnapshot
		*(dest[18].(*time.Time)) = createdAt
		*(dest[19].(*time.Time)) = updatedAt
		*(dest[20].(**time.Time)) = decidedAt
		*(dest[21].(**time.Time)) = completedAt
		return nil
	}
}

func scanAssetDecisionRecordMemberValues(
	recordID string,
	vpsID string,
	displayName string,
	suggestedRole string,
	decidedRole string,
	suggestedAction string,
	decidedAction string,
	reason string,
	followupStatus string,
	followupNote string,
	followupUpdatedAt *time.Time,
	evidenceSnapshot []byte,
	createdAt time.Time,
	updatedAt time.Time,
) func(dest ...any) error {
	return func(dest ...any) error {
		*(dest[0].(*string)) = recordID
		*(dest[1].(*string)) = vpsID
		*(dest[2].(*string)) = displayName
		*(dest[3].(*string)) = suggestedRole
		*(dest[4].(*string)) = decidedRole
		*(dest[5].(*string)) = suggestedAction
		*(dest[6].(*string)) = decidedAction
		*(dest[7].(*string)) = reason
		*(dest[8].(*string)) = followupStatus
		*(dest[9].(*string)) = followupNote
		*(dest[10].(**time.Time)) = followupUpdatedAt
		*(dest[11].(*[]byte)) = evidenceSnapshot
		*(dest[12].(*time.Time)) = createdAt
		*(dest[13].(*time.Time)) = updatedAt
		return nil
	}
}

func fakeAssetDecisionFactRows(now time.Time) pgx.Rows {
	renewAt := now.AddDate(0, 0, 7)
	providerID := "pv_001"
	return &fakeAssetDecisionRows{rows: []fakeAssetDecisionScan{{scan: func(dest ...any) error {
		*(dest[0].(*string)) = "vps_001"
		*(dest[1].(*string)) = "Frankfurt Primary"
		*(dest[2].(**string)) = &providerID
		*(dest[3].(*string)) = "Hetzner"
		*(dest[4].(*string)) = "CX22"
		*(dest[5].(*string)) = "order-1"
		*(dest[6].(*string)) = "Germany"
		*(dest[7].(*string)) = "Hesse"
		*(dest[8].(*string)) = "Falkenstein"
		*(dest[9].(*string)) = "FSN1"
		*(dest[10].(*string)) = "192.0.2.10"
		*(dest[11].(*string)) = ""
		*(dest[12].(*string)) = "192.0.2.10"
		*(dest[13].(*int)) = 22
		*(dest[14].(*string)) = "root"
		*(dest[15].(*string)) = "Debian"
		*(dest[16].(*string)) = "kvm"
		*(dest[17].(*vpsassets.LifecycleStatus)) = vpsassets.LifecycleActive
		*(dest[18].(*vpsassets.UsageStatus)) = vpsassets.UsageInUse
		*(dest[19].(*vpsassets.RenewalDecision)) = vpsassets.RenewalUnreviewed
		*(dest[20].(*string)) = "high"
		*(dest[21].(*[]string)) = []string{"edge"}
		*(dest[22].(*string)) = "note"
		*(dest[23].(*int)) = 1
		*(dest[24].(*int)) = 1
		*(dest[25].(*int)) = 1
		*(dest[26].(*time.Time)) = now
		*(dest[27].(*time.Time)) = now
		*(dest[28].(**time.Time)) = nil
		*(dest[29].(*int)) = 1
		*(dest[30].(*int)) = 1
		*(dest[31].(*int)) = 0
		*(dest[32].(*int)) = 1
		*(dest[33].(*int)) = 0
		*(dest[34].(*int)) = 1
		*(dest[35].(*int)) = 1
		*(dest[36].(*int)) = 1
		*(dest[37].(*int)) = 1
		*(dest[38].(*int)) = 0
		*(dest[39].(*int)) = 0
		*(dest[40].(*string)) = ""
		*(dest[41].(*bool)) = true
		*(dest[42].(*string)) = "sub_001"
		*(dest[43].(*string)) = "vps_001"
		*(dest[44].(*float64)) = 12
		*(dest[45].(*string)) = "USD"
		*(dest[46].(*string)) = "monthly"
		*(dest[47].(*int)) = 1
		*(dest[48].(*string)) = "month"
		*(dest[49].(*int)) = 1
		*(dest[50].(*float64)) = 12
		*(dest[51].(**time.Time)) = nil
		*(dest[52].(**time.Time)) = &renewAt
		*(dest[53].(*bool)) = true
		*(dest[54].(*bool)) = false
		*(dest[55].(*string)) = "auto"
		*(dest[56].(*subscriptions.Status)) = subscriptions.StatusActive
		*(dest[57].(*string)) = "card"
		*(dest[58].(*string)) = "Frankfurt Primary"
		*(dest[59].(*string)) = "prod"
		*(dest[60].(*[]string)) = []string{"prod"}
		*(dest[61].(**time.Time)) = nil
		*(dest[62].(**time.Time)) = nil
		*(dest[63].(*string)) = "subscription note"
		*(dest[64].(**time.Time)) = &now
		*(dest[65].(**time.Time)) = &now
		return nil
	}}}}
}
