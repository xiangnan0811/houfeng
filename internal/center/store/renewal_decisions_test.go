package store

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/renewals"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
)

func TestPostgresRenewalDecisionMigrationDefinesTableConstraintsAndIndexes(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "migrations", "0020_create_renewal_decisions.sql"))
	if err != nil {
		t.Fatalf("ReadFile(renewal decisions migration) error = %v", err)
	}
	text := string(source)
	for _, snippet := range []string{
		"create table if not exists renewal_decisions",
		"decision_id text primary key",
		"vps_id text not null references vps_assets(vps_id) on delete cascade",
		"from_decision text",
		"to_decision text not null",
		"reason text not null default ''",
		"decided_at timestamptz not null default now()",
		"renewal_decisions_from_allowed",
		"renewal_decisions_to_allowed",
		"idx_renewal_decisions_vps_time",
		"on renewal_decisions (vps_id, decided_at desc, created_at desc)",
		"idx_renewal_decisions_to_decision",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("renewal decisions migration missing %q", snippet)
		}
	}
}

func TestPostgresExperienceLogsMigrationDefinesTableConstraintsAndIndexes(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "migrations", "0022_create_experience_logs.sql"))
	if err != nil {
		t.Fatalf("ReadFile(experience logs migration) error = %v", err)
	}
	text := string(source)
	for _, snippet := range []string{
		"create table if not exists experience_logs",
		"experience_log_id text primary key",
		"vps_id text not null references vps_assets(vps_id) on delete cascade",
		"category text not null",
		"severity text not null",
		"summary text not null",
		"details text not null default ''",
		"occurred_at timestamptz not null default now()",
		"experience_logs_category_allowed",
		"experience_logs_severity_allowed",
		"experience_logs_summary_not_blank",
		"idx_experience_logs_vps_time",
		"on experience_logs (vps_id, occurred_at desc, created_at desc)",
		"idx_experience_logs_category",
		"idx_experience_logs_severity",
	} {
		if !strings.Contains(text, snippet) {
			t.Fatalf("experience logs migration missing %q", snippet)
		}
	}
}

func TestPostgresRenewalDecisionCreateListAndTimeline(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 9, 13, 0, 0, 0, time.UTC)
	renewAt := subscriptions.NewDate(time.Date(2026, time.June, 1, 8, 0, 0, 0, time.UTC))
	patchedRenewAt := subscriptions.NewDate(time.Date(2026, time.December, 1, 8, 0, 0, 0, time.UTC))
	previous := vpsassets.RenewalKeep
	var rowCalls []string
	var rowArgs [][]any
	var queryCalls []string
	var queryArgs [][]any
	repo := &PostgresRenewalDecisionRepository{db: fakeRenewalDecisionDB{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			rowCalls = append(rowCalls, sql)
			rowArgs = append(rowArgs, append([]any(nil), args...))
			switch {
			case strings.Contains(sql, "insert into renewal_decisions"):
				return fakeRenewalDecisionRow{scan: func(dest ...any) error {
					decisionID, ok := args[0].(string)
					if !ok || !strings.HasPrefix(decisionID, "rdec_") {
						t.Fatalf("decision id arg = %#v, want rdec_ prefix", args[0])
					}
					scanRenewalDecisionRecordDestinations(dest, renewals.DecisionRecord{
						DecisionID:   decisionID,
						VPSID:        "vps_001",
						FromDecision: &previous,
						ToDecision:   vpsassets.RenewalCancel,
						Reason:       "too expensive",
						DecidedAt:    now,
						CreatedAt:    now,
					})
					return nil
				}}
			case strings.Contains(sql, "select exists"):
				return fakeRenewalDecisionRow{scan: func(dest ...any) error {
					*(dest[0].(*bool)) = true
					return nil
				}}
			default:
				t.Fatalf("unexpected QueryRow SQL %q", sql)
				return fakeRenewalDecisionRow{scan: func(dest ...any) error { return nil }}
			}
		},
		query: func(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
			queryCalls = append(queryCalls, sql)
			queryArgs = append(queryArgs, append([]any(nil), args...))
			switch {
			case strings.Contains(sql, "from renewal_decisions"):
				return &fakeRenewalDecisionRows{rows: []fakeRenewalDecisionScan{{
					scan: func(dest ...any) error {
						scanRenewalDecisionRecordDestinations(dest, renewals.DecisionRecord{
							DecisionID:   "rdec_001",
							VPSID:        "vps_001",
							FromDecision: &previous,
							ToDecision:   vpsassets.RenewalCancel,
							Reason:       "too expensive",
							DecidedAt:    now,
							CreatedAt:    now,
						})
						return nil
					},
				}}}, nil
			case strings.Contains(sql, "from price_histories"):
				return &fakeRenewalDecisionRows{rows: []fakeRenewalDecisionScan{{
					scan: func(dest ...any) error {
						scanPriceHistoryRecordDestinations(dest, priceHistoryFixture("ph_001", now, renewAt, patchedRenewAt))
						return nil
					},
				}}}, nil
			case strings.Contains(sql, "from ip_histories"):
				return &fakeRenewalDecisionRows{rows: []fakeRenewalDecisionScan{{
					scan: func(dest ...any) error {
						scanIPHistoryRecordDestinations(dest, "iph_001", now)
						return nil
					},
				}}}, nil
			case strings.Contains(sql, "from vps_spec_snapshots"):
				return &fakeRenewalDecisionRows{rows: []fakeRenewalDecisionScan{{
					scan: func(dest ...any) error {
						scanSpecSnapshotRecordDestinations(dest, "vss_001", now)
						return nil
					},
				}}}, nil
			case strings.Contains(sql, "from experience_logs"):
				return &fakeRenewalDecisionRows{rows: []fakeRenewalDecisionScan{{
					scan: func(dest ...any) error {
						scanExperienceLogRecordDestinations(dest, "elog_001", now)
						return nil
					},
				}}}, nil
			default:
				t.Fatalf("unexpected Query SQL %q", sql)
				return &fakeRenewalDecisionRows{}, nil
			}
		},
	}}

	created, err := repo.CreateRenewalDecision(context.Background(), renewals.CreateDecisionInput{
		VPSID:        " vps_001 ",
		FromDecision: &previous,
		ToDecision:   vpsassets.RenewalCancel,
		Reason:       " too expensive ",
		DecidedAt:    &now,
	})
	if err != nil {
		t.Fatalf("CreateRenewalDecision() error = %v", err)
	}
	if !strings.HasPrefix(created.DecisionID, "rdec_") || created.Reason != "too expensive" {
		t.Fatalf("created = %#v, want normalized decision history", created)
	}
	if len(rowArgs[0]) != 6 || rowArgs[0][1] != "vps_001" || rowArgs[0][2] != "keep" || rowArgs[0][3] != "cancel" || rowArgs[0][4] != "too expensive" {
		t.Fatalf("create args = %#v, want normalized decision args", rowArgs[0])
	}

	records, err := repo.ListRenewalDecisionsForVPS(context.Background(), " vps_001 ")
	if err != nil {
		t.Fatalf("ListRenewalDecisionsForVPS() error = %v", err)
	}
	if len(records) != 1 || records[0].DecisionID != "rdec_001" {
		t.Fatalf("records = %#v, want one decision", records)
	}
	if len(queryCalls) != 1 || !strings.Contains(queryCalls[0], "order by decided_at desc, created_at desc, decision_id desc") || queryArgs[0][0] != "vps_001" {
		t.Fatalf("list query = %q args=%#v, want timeline order and vps id", queryCalls[0], queryArgs[0])
	}

	timeline, err := repo.GetVPSTimeline(context.Background(), " vps_001 ")
	if err != nil {
		t.Fatalf("GetVPSTimeline() error = %v", err)
	}
	if timeline.VPSID != "vps_001" ||
		len(timeline.RenewalDecisions) != 1 ||
		len(timeline.PriceHistories) != 1 ||
		len(timeline.IPHistories) != 1 ||
		len(timeline.SpecSnapshots) != 1 ||
		len(timeline.ExperienceLogs) != 1 {
		t.Fatalf("timeline = %#v, want one record per history type", timeline)
	}

	logs, err := repo.ListExperienceLogsForVPS(context.Background(), " vps_001 ")
	if err != nil {
		t.Fatalf("ListExperienceLogsForVPS() error = %v", err)
	}
	if len(logs) != 1 || logs[0].ExperienceLogID != "elog_001" {
		t.Fatalf("logs = %#v, want one experience log", logs)
	}
	if !strings.Contains(queryCalls[len(queryCalls)-1], "order by occurred_at desc, created_at desc, experience_log_id desc") {
		t.Fatalf("experience log query = %q, want timeline order", queryCalls[len(queryCalls)-1])
	}
}

func TestPostgresRenewalDecisionCreatesExperienceLog(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 10, 9, 30, 0, 0, time.UTC)
	var rowArgs [][]any
	repo := &PostgresRenewalDecisionRepository{db: fakeRenewalDecisionDB{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			rowArgs = append(rowArgs, append([]any(nil), args...))
			if !strings.Contains(sql, "insert into experience_logs") {
				t.Fatalf("unexpected QueryRow SQL %q", sql)
			}
			return fakeRenewalDecisionRow{scan: func(dest ...any) error {
				experienceLogID, ok := args[0].(string)
				if !ok || !strings.HasPrefix(experienceLogID, "elog_") {
					t.Fatalf("experience log id arg = %#v, want elog_ prefix", args[0])
				}
				scanExperienceLogRecordDestinations(dest, experienceLogID, now)
				return nil
			}}
		},
	}}

	record, err := repo.CreateExperienceLog(context.Background(), renewals.CreateExperienceLogInput{
		VPSID:      " vps_001 ",
		Category:   " network ",
		Severity:   " warning ",
		Summary:    " packet loss ",
		Details:    " opened provider ticket ",
		OccurredAt: &now,
	})
	if err != nil {
		t.Fatalf("CreateExperienceLog() error = %v", err)
	}
	if !strings.HasPrefix(record.ExperienceLogID, "elog_") || record.Summary != "packet loss" || record.Category != renewals.ExperienceNetwork {
		t.Fatalf("record = %#v, want normalized experience log", record)
	}
	if len(rowArgs[0]) != 7 || rowArgs[0][1] != "vps_001" || rowArgs[0][2] != "network" || rowArgs[0][3] != "warning" || rowArgs[0][4] != "packet loss" || rowArgs[0][5] != "opened provider ticket" {
		t.Fatalf("create args = %#v, want normalized experience log args", rowArgs[0])
	}
}

func TestPostgresRenewalDecisionMapsErrors(t *testing.T) {
	t.Parallel()

	if _, err := (&PostgresRenewalDecisionRepository{db: fakeRenewalDecisionDB{}}).CreateRenewalDecision(context.Background(), renewals.CreateDecisionInput{VPSID: " ", ToDecision: vpsassets.RenewalCancel}); !errors.Is(err, renewals.ErrInvalidRenewalDecisionInput) {
		t.Fatalf("CreateRenewalDecision(blank vps) error = %v, want ErrInvalidRenewalDecisionInput", err)
	}
	if _, err := (&PostgresRenewalDecisionRepository{db: fakeRenewalDecisionDB{}}).ListRenewalDecisionsForVPS(context.Background(), " "); !errors.Is(err, renewals.ErrInvalidRenewalDecisionInput) {
		t.Fatalf("ListRenewalDecisionsForVPS(blank vps) error = %v, want ErrInvalidRenewalDecisionInput", err)
	}
	if _, err := (&PostgresRenewalDecisionRepository{db: fakeRenewalDecisionDB{}}).CreateExperienceLog(context.Background(), renewals.CreateExperienceLogInput{VPSID: " ", Category: renewals.ExperienceNetwork, Severity: renewals.ExperienceSeverityWarning, Summary: "packet loss"}); !errors.Is(err, renewals.ErrInvalidAssetHistoryInput) {
		t.Fatalf("CreateExperienceLog(blank vps) error = %v, want ErrInvalidAssetHistoryInput", err)
	}
	if _, err := (&PostgresRenewalDecisionRepository{db: fakeRenewalDecisionDB{}}).ListExperienceLogsForVPS(context.Background(), " "); !errors.Is(err, renewals.ErrInvalidAssetHistoryInput) {
		t.Fatalf("ListExperienceLogsForVPS(blank vps) error = %v, want ErrInvalidAssetHistoryInput", err)
	}

	repo := &PostgresRenewalDecisionRepository{db: fakeRenewalDecisionDB{
		queryRow: func(context.Context, string, ...any) pgx.Row {
			return fakeRenewalDecisionRow{scan: func(dest ...any) error {
				*(dest[0].(*bool)) = false
				return nil
			}}
		},
	}}
	if _, err := repo.GetVPSTimeline(context.Background(), "vps_missing"); !errors.Is(err, renewals.ErrRenewalTimelineNotFound) {
		t.Fatalf("GetVPSTimeline(missing) error = %v, want ErrRenewalTimelineNotFound", err)
	}
	if _, err := repo.ListExperienceLogsForVPS(context.Background(), "vps_missing"); !errors.Is(err, renewals.ErrAssetTimelineNotFound) {
		t.Fatalf("ListExperienceLogsForVPS(missing) error = %v, want ErrAssetTimelineNotFound", err)
	}

	fkErr := &pgconn.PgError{Code: "23503", ConstraintName: "renewal_decisions_vps_id_fkey"}
	repo = &PostgresRenewalDecisionRepository{db: fakeRenewalDecisionDB{
		queryRow: func(context.Context, string, ...any) pgx.Row {
			return fakeRenewalDecisionRow{scan: func(dest ...any) error { return fkErr }}
		},
	}}
	if _, err := repo.CreateRenewalDecision(context.Background(), renewals.CreateDecisionInput{VPSID: "vps_missing", ToDecision: vpsassets.RenewalCancel}); !errors.Is(err, renewals.ErrRenewalTimelineNotFound) {
		t.Fatalf("CreateRenewalDecision(fk) error = %v, want ErrRenewalTimelineNotFound", err)
	}

	repo = &PostgresRenewalDecisionRepository{db: fakeRenewalDecisionDB{
		queryRow: func(context.Context, string, ...any) pgx.Row {
			return fakeRenewalDecisionRow{scan: func(dest ...any) error { return fkErr }}
		},
	}}
	if _, err := repo.CreateExperienceLog(context.Background(), renewals.CreateExperienceLogInput{VPSID: "vps_missing", Category: renewals.ExperienceNetwork, Severity: renewals.ExperienceSeverityWarning, Summary: "packet loss"}); !errors.Is(err, renewals.ErrAssetTimelineNotFound) {
		t.Fatalf("CreateExperienceLog(fk) error = %v, want ErrAssetTimelineNotFound", err)
	}
}

type fakeRenewalDecisionDB struct {
	query    func(context.Context, string, ...any) (pgx.Rows, error)
	queryRow func(context.Context, string, ...any) pgx.Row
}

func (f fakeRenewalDecisionDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.query == nil {
		return &fakeRenewalDecisionRows{}, nil
	}
	return f.query(ctx, sql, args...)
}

func (f fakeRenewalDecisionDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if f.queryRow == nil {
		return fakeRenewalDecisionRow{scan: func(dest ...any) error { return pgx.ErrNoRows }}
	}
	return f.queryRow(ctx, sql, args...)
}

type fakeRenewalDecisionRow struct {
	scan func(dest ...any) error
}

func (r fakeRenewalDecisionRow) Scan(dest ...any) error {
	return r.scan(dest...)
}

type fakeRenewalDecisionScan struct {
	scan func(dest ...any) error
}

type fakeRenewalDecisionRows struct {
	rows []fakeRenewalDecisionScan
	idx  int
	err  error
}

func (f *fakeRenewalDecisionRows) Close()                                       {}
func (f *fakeRenewalDecisionRows) Err() error                                   { return f.err }
func (f *fakeRenewalDecisionRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (f *fakeRenewalDecisionRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (f *fakeRenewalDecisionRows) RawValues() [][]byte                          { return nil }
func (f *fakeRenewalDecisionRows) Values() ([]any, error)                       { return nil, nil }
func (f *fakeRenewalDecisionRows) Conn() *pgx.Conn                              { return nil }
func (f *fakeRenewalDecisionRows) Next() bool {
	if f.idx >= len(f.rows) {
		return false
	}
	f.idx++
	return true
}
func (f *fakeRenewalDecisionRows) Scan(dest ...any) error {
	return f.rows[f.idx-1].scan(dest...)
}

func scanRenewalDecisionRecordDestinations(dest []any, record renewals.DecisionRecord) {
	*(dest[0].(*string)) = record.DecisionID
	*(dest[1].(*string)) = record.VPSID
	if record.FromDecision == nil {
		*(dest[2].(**string)) = nil
	} else {
		value := string(*record.FromDecision)
		*(dest[2].(**string)) = &value
	}
	*(dest[3].(*string)) = string(record.ToDecision)
	*(dest[4].(*string)) = record.Reason
	*(dest[5].(*time.Time)) = record.DecidedAt
	*(dest[6].(*time.Time)) = record.CreatedAt
}

func scanExperienceLogRecordDestinations(dest []any, experienceLogID string, now time.Time) {
	*(dest[0].(*string)) = experienceLogID
	*(dest[1].(*string)) = "vps_001"
	*(dest[2].(*string)) = string(renewals.ExperienceNetwork)
	*(dest[3].(*string)) = string(renewals.ExperienceSeverityWarning)
	*(dest[4].(*string)) = "packet loss"
	*(dest[5].(*string)) = "opened provider ticket"
	*(dest[6].(*time.Time)) = now
	*(dest[7].(*time.Time)) = now
}
