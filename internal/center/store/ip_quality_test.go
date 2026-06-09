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

	"houfeng/internal/center/ipquality"
	"houfeng/internal/contracts/agentapi"
)

func TestPostgresIPQualityRepositorySaveReportsWritesReportProvidersAndUnlocks(t *testing.T) {
	t.Parallel()

	tx := &fakeIPQualityTx{}
	repo := &PostgresIPQualityRepository{
		beginTx: func(context.Context, pgx.TxOptions) (ipQualityTx, error) {
			return tx, nil
		},
		newReportID: func() (string, error) {
			return "ipq_001", nil
		},
	}
	report := ipQualityReportWrite()

	if err := repo.SaveReports(context.Background(), []ipquality.ReportWrite{report}); err != nil {
		t.Fatalf("SaveReports() error = %v", err)
	}
	if tx.commitCalls != 1 {
		t.Fatalf("commitCalls = %d, want 1", tx.commitCalls)
	}
	if !containsSQL(tx.execSQL, "insert into ip_quality_reports") {
		t.Fatalf("execSQL = %#v, want ip_quality_reports insert", tx.execSQL)
	}
	if !containsSQL(tx.execSQL, "insert into ip_quality_provider_results") {
		t.Fatalf("execSQL = %#v, want provider result insert", tx.execSQL)
	}
	if !containsSQL(tx.execSQL, "insert into ip_quality_service_unlocks") {
		t.Fatalf("execSQL = %#v, want service unlock insert", tx.execSQL)
	}
	reportArgs := tx.argsForSQL("insert into ip_quality_reports")
	if len(reportArgs) == 0 || reportArgs[0] != "ipq_001" {
		t.Fatalf("report insert args = %#v, want generated report id first", reportArgs)
	}
	if got := string(reportArgs[22].([]byte)); got != `{"Info":{"ASN":"AS64500"}}` {
		t.Fatalf("raw json arg = %s, want sanitized raw json", got)
	}
	providerArgs := tx.argsForSQL("insert into ip_quality_provider_results")
	if providerArgs[1] != "ipq_001" || providerArgs[2] != "ipinfo" {
		t.Fatalf("provider insert args = %#v, want report id and provider", providerArgs)
	}
	unlockArgs := tx.argsForSQL("insert into ip_quality_service_unlocks")
	if unlockArgs[1] != "ipq_001" || unlockArgs[2] != "netflix" {
		t.Fatalf("unlock insert args = %#v, want report id and service", unlockArgs)
	}
}

func TestPostgresIPQualityRepositorySaveReportsRollsBackInvalidReport(t *testing.T) {
	t.Parallel()

	tx := &fakeIPQualityTx{}
	repo := &PostgresIPQualityRepository{
		beginTx: func(context.Context, pgx.TxOptions) (ipQualityTx, error) {
			return tx, nil
		},
		newReportID: func() (string, error) {
			return "ipq_001", nil
		},
	}
	report := ipQualityReportWrite()
	report.IPAddress = ""

	err := repo.SaveReports(context.Background(), []ipquality.ReportWrite{report})
	if !errors.Is(err, ipquality.ErrInvalidIPQualityReport) {
		t.Fatalf("SaveReports() error = %v, want ErrInvalidIPQualityReport", err)
	}
	if tx.commitCalls != 0 {
		t.Fatalf("commitCalls = %d, want 0", tx.commitCalls)
	}
	if len(tx.execSQL) != 0 {
		t.Fatalf("execSQL = %#v, want no writes", tx.execSQL)
	}
}

func TestPostgresIPQualityRepositorySaveReportsKeepsDiagnosticFailureReports(t *testing.T) {
	t.Parallel()

	tx := &fakeIPQualityTx{}
	repo := &PostgresIPQualityRepository{
		beginTx: func(context.Context, pgx.TxOptions) (ipQualityTx, error) {
			return tx, nil
		},
		newReportID: func() (string, error) {
			return "ipq_failure", nil
		},
	}
	report := ipQualityReportWrite()
	report.IPAddress = "0.0.0.0"
	report.Status = agentapi.IPQualityStatusFailure
	report.RiskLevel = ""
	report.ErrorCode = "lookup_failed"
	report.ErrorSummary = "non_json_response: http status 200 content-type \"text/html\""
	report.ProviderResults = nil
	report.ServiceUnlocks = nil
	report.RawJSON = nil

	if err := repo.SaveReports(context.Background(), []ipquality.ReportWrite{report}); err != nil {
		t.Fatalf("SaveReports() error = %v", err)
	}

	if !containsSQL(tx.execSQL, "insert into ip_quality_reports") {
		t.Fatalf("execSQL = %#v, want diagnostic failure report insert", tx.execSQL)
	}
	if containsSQL(tx.execSQL, "insert into ip_quality_provider_results") || containsSQL(tx.execSQL, "insert into ip_quality_service_unlocks") {
		t.Fatalf("execSQL = %#v, want no provider/service rows for lookup failure", tx.execSQL)
	}
	args := tx.argsForSQL("insert into ip_quality_reports")
	if args[7] != "0.0.0.0" || args[9] != agentapi.IPQualityStatusFailure || args[19] != "lookup_failed" {
		t.Fatalf("failure report args = %#v, want placeholder failure diagnostic saved", args)
	}
}

func TestPostgresIPQualityRepositoryGetVPSIPQualityReturnsLatestMatricesAndHistory(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 1, 10, 0, 0, 0, time.UTC)
	db := &fakeIPQualityDB{
		queryRows: map[string]pgx.Rows{
			"from ip_quality_reports r": &fakeIPQualityRows{rows: []fakeIPQualityScan{{
				scan: scanIPQualityReportRow("ipq_001", "mi_001", now, agentapi.IPQualityStatusSuccess),
			}}},
			"from ip_quality_provider_results": &fakeIPQualityRows{rows: []fakeIPQualityScan{{
				scan: func(dest ...any) error {
					*(dest[0].(*string)) = "ipinfo"
					*(dest[1].(*string)) = "hosting"
					*(dest[2].(*string)) = "hosting"
					*(dest[3].(*string)) = "low"
					*(dest[4].(*string)) = "12"
					*(dest[5].(*string)) = "US"
					*(dest[6].(*string)) = "United States"
					isVPN := false
					isServer := true
					*(dest[7].(**bool)) = nil
					*(dest[8].(**bool)) = nil
					*(dest[9].(**bool)) = &isVPN
					*(dest[10].(**bool)) = &isServer
					*(dest[11].(**bool)) = nil
					*(dest[12].(**bool)) = nil
					*(dest[13].(*string)) = ""
					*(dest[14].(*string)) = ""
					return nil
				},
			}}},
			"from ip_quality_service_unlocks": &fakeIPQualityRows{rows: []fakeIPQualityScan{{
				scan: func(dest ...any) error {
					*(dest[0].(*string)) = "netflix"
					*(dest[1].(*string)) = "unlocked"
					*(dest[2].(*string)) = "US"
					*(dest[3].(*string)) = "full"
					*(dest[4].(*string)) = ""
					*(dest[5].(*string)) = ""
					return nil
				},
			}}},
			"select latest.vps_id": &fakeIPQualityRows{rows: []fakeIPQualityScan{{
				scan: func(dest ...any) error {
					*(dest[0].(*string)) = "vps_001"
					*(dest[1].(*time.Time)) = now
					*(dest[2].(*string)) = "203.0.113.10"
					*(dest[3].(*int)) = 4
					*(dest[4].(*string)) = agentapi.IPQualityStatusSuccess
					*(dest[5].(*string)) = "low"
					*(dest[6].(*string)) = "US"
					*(dest[7].(*string)) = "United States"
					*(dest[8].(*string)) = "AS64500"
					*(dest[9].(*string)) = "Example Network"
					*(dest[10].(*bool)) = false
					*(dest[11].(*bool)) = false
					*(dest[12].(*string)) = "link"
					*(dest[13].(*string)) = ""
					*(dest[14].(*string)) = ""
					*(dest[15].(*int)) = 1
					*(dest[16].(*int)) = 1
					return nil
				},
			}}},
			"from ip_quality_assigned_vps_reports": &fakeIPQualityRows{rows: []fakeIPQualityScan{{
				scan: func(dest ...any) error {
					*(dest[0].(*string)) = "vps_001"
					*(dest[1].(*time.Time)) = now
					*(dest[2].(*string)) = "203.0.113.10"
					*(dest[3].(*int)) = 4
					*(dest[4].(*string)) = agentapi.IPQualityStatusSuccess
					*(dest[5].(*string)) = "low"
					*(dest[6].(*string)) = "US"
					*(dest[7].(*string)) = "United States"
					*(dest[8].(*string)) = "AS64500"
					*(dest[9].(*string)) = "Example Network"
					*(dest[10].(*bool)) = false
					*(dest[11].(*bool)) = false
					*(dest[12].(*string)) = "link"
					*(dest[13].(*string)) = ""
					*(dest[14].(*string)) = ""
					*(dest[15].(*int)) = 1
					*(dest[16].(*int)) = 1
					return nil
				},
			}}},
		},
	}
	repo := &PostgresIPQualityRepository{db: db}

	got, err := repo.GetVPSIPQuality(context.Background(), "vps_001")
	if err != nil {
		t.Fatalf("GetVPSIPQuality() error = %v", err)
	}
	if got.Summary == nil || got.Summary.IPAddress != "203.0.113.10" {
		t.Fatalf("Summary = %#v, want latest summary", got.Summary)
	}
	if got.LatestReport == nil || got.LatestReport.ReportID != "ipq_001" {
		t.Fatalf("LatestReport = %#v, want ipq_001", got.LatestReport)
	}
	if len(got.ProviderResults) != 1 || got.ProviderResults[0].Provider != "ipinfo" {
		t.Fatalf("ProviderResults = %#v, want ipinfo result", got.ProviderResults)
	}
	if got.ProviderResults[0].IsVPN == nil || *got.ProviderResults[0].IsVPN {
		t.Fatalf("ProviderResults[0].IsVPN = %#v, want false pointer", got.ProviderResults[0].IsVPN)
	}
	if len(got.ServiceUnlocks) != 1 || got.ServiceUnlocks[0].Service != "netflix" {
		t.Fatalf("ServiceUnlocks = %#v, want netflix unlock", got.ServiceUnlocks)
	}
	if len(got.History) != 1 || got.History[0].VPSID != "vps_001" {
		t.Fatalf("History = %#v, want latest summary history", got.History)
	}
}

func TestPostgresIPQualityRepositoryHistoryDoesNotReadLatestOnlyView(t *testing.T) {
	t.Parallel()

	db := &fakeIPQualityDB{
		queryRows: map[string]pgx.Rows{
			"from ip_quality_assigned_vps_reports": &fakeIPQualityRows{},
		},
	}
	repo := &PostgresIPQualityRepository{db: db}

	if _, err := repo.historyForVPS(context.Background(), "vps_001"); err != nil {
		t.Fatalf("historyForVPS() error = %v", err)
	}
	if len(db.queries) != 1 {
		t.Fatalf("query count = %d, want 1", len(db.queries))
	}
	if strings.Contains(db.queries[0], "ip_quality_latest_vps_summaries") {
		t.Fatalf("history query used latest-only view: %s", db.queries[0])
	}
}

func TestPostgresIPQualityRepositoryGetVPSIPQualityReturnsEmptyWhenFilteredViewsHaveNoReports(t *testing.T) {
	t.Parallel()

	db := &fakeIPQualityDB{
		queryRows: map[string]pgx.Rows{
			"select latest.vps_id":      &fakeIPQualityRows{},
			"from ip_quality_reports r": &fakeIPQualityRows{},
		},
	}
	repo := &PostgresIPQualityRepository{db: db}

	got, err := repo.GetVPSIPQuality(context.Background(), "vps_001")
	if err != nil {
		t.Fatalf("GetVPSIPQuality() error = %v", err)
	}
	if got.Summary != nil || got.LatestReport != nil {
		t.Fatalf("VPSReport = %#v, want no summary/latest report when filtered views are empty", got)
	}
	if len(got.ProviderResults) != 0 || len(got.ServiceUnlocks) != 0 || len(got.History) != 0 {
		t.Fatalf("VPSReport matrices/history = %#v/%#v/%#v, want empty slices", got.ProviderResults, got.ServiceUnlocks, got.History)
	}
	if len(db.queries) != 2 {
		t.Fatalf("query count = %d, want summary + latest report checks only", len(db.queries))
	}
}

func TestPostgresIPQualityRepositoryReadsVPSIPQualityThroughFilteredViews(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 1, 10, 0, 0, 0, time.UTC)
	db := &fakeIPQualityDB{
		queryRows: map[string]pgx.Rows{
			"from ip_quality_reports r": &fakeIPQualityRows{rows: []fakeIPQualityScan{{
				scan: scanIPQualityReportRow("ipq_valid", "mi_001", now, agentapi.IPQualityStatusSuccess),
			}}},
			"from ip_quality_provider_results": &fakeIPQualityRows{},
			"from ip_quality_service_unlocks":  &fakeIPQualityRows{},
			"select latest.vps_id":             &fakeIPQualityRows{},
			"from ip_quality_assigned_vps_reports": &fakeIPQualityRows{rows: []fakeIPQualityScan{{
				scan: func(dest ...any) error {
					*(dest[0].(*string)) = "vps_001"
					*(dest[1].(*time.Time)) = now
					*(dest[2].(*string)) = "203.0.113.10"
					*(dest[3].(*int)) = 4
					*(dest[4].(*string)) = agentapi.IPQualityStatusSuccess
					*(dest[5].(*string)) = "low"
					*(dest[6].(*string)) = "US"
					*(dest[7].(*string)) = "United States"
					*(dest[8].(*string)) = "AS64500"
					*(dest[9].(*string)) = "Example Network"
					*(dest[10].(*bool)) = false
					*(dest[11].(*bool)) = false
					*(dest[12].(*string)) = "link"
					*(dest[13].(*string)) = ""
					*(dest[14].(*string)) = ""
					*(dest[15].(*int)) = 0
					*(dest[16].(*int)) = 0
					return nil
				},
			}}},
		},
	}
	repo := &PostgresIPQualityRepository{db: db}

	if _, err := repo.GetVPSIPQuality(context.Background(), "vps_001"); err != nil {
		t.Fatalf("GetVPSIPQuality() error = %v", err)
	}

	joined := strings.ToLower(strings.Join(db.queries, "\n"))
	if strings.Contains(joined, "join ip_quality_reports r on r.monitoring_instance_id") ||
		strings.Contains(joined, "r.ip_address in (nullif") {
		t.Fatalf("repository query bypassed filtered IP quality read views: %s", joined)
	}
	for _, want := range []string{"ip_quality_latest_vps_summaries", "ip_quality_assigned_vps_reports"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("repository queries = %s, want %s", joined, want)
		}
	}
}

func ipQualityReportWrite() ipquality.ReportWrite {
	observedAt := time.Date(2026, time.June, 1, 10, 0, 0, 0, time.UTC)
	return ipquality.ReportWrite{
		MonitoringInstanceID: "mi_001",
		ObservedAt:           observedAt,
		ReceivedAt:           observedAt.Add(time.Second),
		AgentVersion:         "agent/v0.1.0",
		Fingerprint:          "fp-001",
		SyncBatchID:          "sync_001",
		IPAddress:            "203.0.113.10",
		IPVersion:            4,
		Status:               agentapi.IPQualityStatusSuccess,
		ASN:                  "AS64500",
		Organization:         "Example Network",
		UseRegionCode:        "US",
		RiskLevel:            "low",
		RawJSON:              json.RawMessage(`{"Info":{"ASN":"AS64500"}}`),
		ProviderResults: []ipquality.ProviderResultWrite{{
			Provider:    "ipinfo",
			UsageType:   "hosting",
			CompanyType: "hosting",
			RiskLevel:   "low",
		}},
		ServiceUnlocks: []ipquality.ServiceUnlockWrite{{
			Service:    "netflix",
			Status:     "unlocked",
			Region:     "US",
			UnlockType: "full",
		}},
	}
}

type fakeIPQualityDB struct {
	queryRows map[string]pgx.Rows
	queryErr  error
	queries   []string
}

func (f *fakeIPQualityDB) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	f.queries = append(f.queries, sql)
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	for key, rows := range f.queryRows {
		if strings.Contains(sql, key) {
			if typed, ok := rows.(*fakeIPQualityRows); ok {
				return &fakeIPQualityRows{rows: typed.rows}, nil
			}
			return rows, nil
		}
	}
	return nil, errors.New("unexpected query")
}

type fakeIPQualityTx struct {
	execSQL     []string
	execArgs    [][]any
	commitCalls int
}

func (f *fakeIPQualityTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.execSQL = append(f.execSQL, sql)
	f.execArgs = append(f.execArgs, append([]any(nil), args...))
	return pgconn.CommandTag{}, nil
}

func (f *fakeIPQualityTx) Commit(context.Context) error {
	f.commitCalls++
	return nil
}

func (f *fakeIPQualityTx) Rollback(context.Context) error { return nil }

func (f *fakeIPQualityTx) argsForSQL(want string) []any {
	for i, sql := range f.execSQL {
		if strings.Contains(sql, want) {
			return f.execArgs[i]
		}
	}
	return nil
}

type fakeIPQualityScan struct{ scan func(dest ...any) error }

type fakeIPQualityRows struct {
	rows []fakeIPQualityScan
	idx  int
}

func (f *fakeIPQualityRows) Close()                                       {}
func (f *fakeIPQualityRows) Err() error                                   { return nil }
func (f *fakeIPQualityRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (f *fakeIPQualityRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (f *fakeIPQualityRows) RawValues() [][]byte                          { return nil }
func (f *fakeIPQualityRows) Values() ([]any, error)                       { return nil, nil }
func (f *fakeIPQualityRows) Conn() *pgx.Conn                              { return nil }
func (f *fakeIPQualityRows) Next() bool {
	if f.idx >= len(f.rows) {
		return false
	}
	f.idx++
	return true
}
func (f *fakeIPQualityRows) Scan(dest ...any) error { return f.rows[f.idx-1].scan(dest...) }

func scanIPQualityReportRow(reportID, monitoringInstanceID string, observedAt time.Time, status string) func(dest ...any) error {
	return func(dest ...any) error {
		*(dest[0].(*string)) = reportID
		*(dest[1].(*string)) = monitoringInstanceID
		*(dest[2].(*time.Time)) = observedAt
		*(dest[3].(*time.Time)) = observedAt.Add(time.Second)
		*(dest[4].(*string)) = "agent/v0.1.0"
		*(dest[5].(*string)) = "fp-001"
		*(dest[6].(*string)) = "sync_001"
		*(dest[7].(*string)) = "203.0.113.10"
		*(dest[8].(*int)) = 4
		*(dest[9].(*string)) = status
		*(dest[10].(*string)) = "AS64500"
		*(dest[11].(*string)) = "Example Network"
		*(dest[12].(**float64)) = nil
		*(dest[13].(**float64)) = nil
		*(dest[14].(*string)) = "US"
		*(dest[15].(*string)) = "United States"
		*(dest[16].(*string)) = "US"
		*(dest[17].(*string)) = "United States"
		*(dest[18].(*string)) = "low"
		*(dest[19].(*string)) = ""
		*(dest[20].(*string)) = ""
		*(dest[21].(*bool)) = false
		*(dest[22].(*[]byte)) = []byte(`{"Info":{"ASN":"AS64500"}}`)
		*(dest[23].(*time.Time)) = observedAt.Add(2 * time.Second)
		return nil
	}
}
