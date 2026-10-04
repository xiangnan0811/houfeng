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
	if tx.commitCalls != 1 {
		t.Fatalf("commitCalls = %d, want 1 for diagnostic failure report", tx.commitCalls)
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
					*(dest[1].(*string)) = "success"
					*(dest[2].(*string)) = "default"
					*(dest[3].(**int)) = intPtr(73)
					*(dest[4].(*string)) = "hosting"
					*(dest[5].(*string)) = "hosting"
					*(dest[6].(*string)) = "low"
					*(dest[7].(*string)) = "12"
					*(dest[8].(*string)) = "US"
					*(dest[9].(*string)) = "United States"
					isVPN := false
					isServer := true
					*(dest[10].(**bool)) = nil
					*(dest[11].(**bool)) = nil
					*(dest[12].(**bool)) = &isVPN
					*(dest[13].(**bool)) = &isServer
					*(dest[14].(**bool)) = nil
					*(dest[15].(**bool)) = nil
					*(dest[16].(*string)) = ""
					*(dest[17].(*string)) = ""
					*(dest[18].(*[]byte)) = []byte(`{"risk":{"score":12}}`)
					return nil
				},
			}}},
			"from ip_quality_service_unlocks": &fakeIPQualityRows{rows: []fakeIPQualityScan{{
				scan: func(dest ...any) error {
					*(dest[0].(*string)) = "netflix"
					*(dest[1].(*string)) = "netflix_title_probe"
					*(dest[2].(*string)) = "unlocked"
					*(dest[3].(*string)) = "success"
					*(dest[4].(**int)) = intPtr(211)
					*(dest[5].(*string)) = "US"
					*(dest[6].(*string)) = "full"
					*(dest[7].(*string)) = ""
					*(dest[8].(*string)) = ""
					*(dest[9].(*[]byte)) = []byte(`{"title_probe":"full_catalog"}`)
					return nil
				},
			}}},
			"select latest.vps_id": &fakeIPQualityRows{rows: []fakeIPQualityScan{{
				scan: func(dest ...any) error {
					*(dest[0].(*string)) = "vps_001"
					*(dest[1].(*string)) = "ipq_001"
					*(dest[2].(*time.Time)) = now
					*(dest[3].(*string)) = "203.0.113.10"
					*(dest[4].(*int)) = 4
					*(dest[5].(*string)) = agentapi.IPQualityStatusSuccess
					*(dest[6].(*string)) = "low"
					*(dest[7].(*string)) = "US"
					*(dest[8].(*string)) = "United States"
					*(dest[9].(*string)) = "AS64500"
					*(dest[10].(*string)) = "Example Network"
					*(dest[11].(*bool)) = false
					*(dest[12].(*bool)) = false
					*(dest[13].(*string)) = "link"
					*(dest[14].(*string)) = ""
					*(dest[15].(*string)) = ""
					*(dest[16].(*int)) = 1
					*(dest[17].(*int)) = 1
					*(dest[18].(*[]byte)) = []byte(`{"expected_provider_count":2,"successful_provider_count":1}`)
					return nil
				},
			}}},
			"from ip_quality_assigned_vps_reports": &fakeIPQualityRows{rows: []fakeIPQualityScan{{
				scan: func(dest ...any) error {
					*(dest[0].(*string)) = "vps_001"
					*(dest[1].(*string)) = "ipq_001"
					*(dest[2].(*time.Time)) = now
					*(dest[3].(*string)) = "203.0.113.10"
					*(dest[4].(*int)) = 4
					*(dest[5].(*string)) = agentapi.IPQualityStatusSuccess
					*(dest[6].(*string)) = "low"
					*(dest[7].(*string)) = "US"
					*(dest[8].(*string)) = "United States"
					*(dest[9].(*string)) = "AS64500"
					*(dest[10].(*string)) = "Example Network"
					*(dest[11].(*bool)) = false
					*(dest[12].(*bool)) = false
					*(dest[13].(*string)) = "link"
					*(dest[14].(*string)) = ""
					*(dest[15].(*string)) = ""
					*(dest[16].(*int)) = 1
					*(dest[17].(*int)) = 1
					*(dest[18].(*[]byte)) = []byte(`{"expected_provider_count":2,"successful_provider_count":1}`)
					return nil
				},
			}}},
		},
	}
	repo := newFakeIPQualityReadRepository(db)

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
	if got.ProviderResults[0].Status != "success" || got.ProviderResults[0].SourceType != "default" ||
		got.ProviderResults[0].LatencyMS == nil || *got.ProviderResults[0].LatencyMS != 73 ||
		string(got.ProviderResults[0].ExtraJSON) != `{"risk":{"score":12}}` {
		t.Fatalf("ProviderResults[0] source details = %#v, want source detail fields", got.ProviderResults[0])
	}
	if len(got.ServiceUnlocks) != 1 || got.ServiceUnlocks[0].Service != "netflix" {
		t.Fatalf("ServiceUnlocks = %#v, want netflix unlock", got.ServiceUnlocks)
	}
	if got.ServiceUnlocks[0].Source != "netflix_title_probe" || got.ServiceUnlocks[0].ProbeStatus != "success" ||
		got.ServiceUnlocks[0].LatencyMS == nil || *got.ServiceUnlocks[0].LatencyMS != 211 ||
		string(got.ServiceUnlocks[0].ExtraJSON) != `{"title_probe":"full_catalog"}` {
		t.Fatalf("ServiceUnlocks[0] source details = %#v, want source detail fields", got.ServiceUnlocks[0])
	}
	if len(got.History) != 1 || got.History[0].VPSID != "vps_001" {
		t.Fatalf("History = %#v, want latest summary history", got.History)
	}
	if got.History[0].ReportID != "ipq_001" || got.History[0].Coverage == nil ||
		got.History[0].Coverage.ExpectedProviderCount != 2 {
		t.Fatalf("History[0] = %#v, want report_id and coverage", got.History[0])
	}
	if len(db.beginOptions) != 1 ||
		db.beginOptions[0].IsoLevel != pgx.RepeatableRead ||
		db.beginOptions[0].AccessMode != pgx.ReadOnly {
		t.Fatalf("begin options = %#v, want repeatable-read read-only", db.beginOptions)
	}
	if db.readTx == nil || db.readTx.commitCalls != 1 {
		t.Fatalf("read transaction commitCalls = %#v, want one explicit commit", db.readTx)
	}
}

func TestPostgresIPQualityRepositoryGetVPSIPQualityReportDetailUsesAssignedReport(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 1, 10, 0, 0, 0, time.UTC)
	db := &fakeIPQualityDB{
		queryRows: map[string]pgx.Rows{
			"join ip_quality_assigned_vps_reports assigned": &fakeIPQualityRows{rows: []fakeIPQualityScan{{
				scan: scanIPQualityReportRow("ipq_001", "mi_001", now, agentapi.IPQualityStatusSuccess),
			}}},
			"from ip_quality_assigned_vps_reports assigned": &fakeIPQualityRows{rows: []fakeIPQualityScan{{
				scan: func(dest ...any) error {
					*(dest[0].(*string)) = "vps_001"
					*(dest[1].(*string)) = "ipq_001"
					*(dest[2].(*time.Time)) = now
					*(dest[3].(*string)) = "203.0.113.10"
					*(dest[4].(*int)) = 4
					*(dest[5].(*string)) = agentapi.IPQualityStatusSuccess
					*(dest[6].(*string)) = "low"
					*(dest[7].(*string)) = "US"
					*(dest[8].(*string)) = "United States"
					*(dest[9].(*string)) = "AS64500"
					*(dest[10].(*string)) = "Example Network"
					*(dest[11].(*bool)) = false
					*(dest[12].(*bool)) = false
					*(dest[13].(*string)) = "link"
					*(dest[14].(*string)) = ""
					*(dest[15].(*string)) = ""
					*(dest[16].(*int)) = 1
					*(dest[17].(*int)) = 1
					*(dest[18].(*[]byte)) = []byte(`{"expected_provider_count":2,"successful_provider_count":1}`)
					return nil
				},
			}}},
			"from ip_quality_provider_results": &fakeIPQualityRows{},
			"from ip_quality_service_unlocks":  &fakeIPQualityRows{},
		},
	}
	repo := newFakeIPQualityReadRepository(db)

	got, err := repo.GetVPSIPQualityReportDetail(context.Background(), "vps_001", "ipq_001")
	if err != nil {
		t.Fatalf("GetVPSIPQualityReportDetail() error = %v", err)
	}
	if got.LatestReport == nil || got.LatestReport.ReportID != "ipq_001" {
		t.Fatalf("LatestReport = %#v, want requested report", got.LatestReport)
	}
	if got.Summary == nil || got.Summary.ReportID != "ipq_001" || got.Summary.Coverage == nil ||
		got.Summary.Coverage.ExpectedProviderCount != 2 {
		t.Fatalf("Summary = %#v, want requested report summary with coverage", got.Summary)
	}
	joined := strings.ToLower(strings.Join(db.queries, "\n"))
	if !strings.Contains(joined, "ip_quality_assigned_vps_reports") || !strings.Contains(joined, "assigned.vps_id = $1") {
		t.Fatalf("detail query = %s, want assigned VPS report guard", joined)
	}
	if len(db.beginOptions) != 1 ||
		db.beginOptions[0].IsoLevel != pgx.RepeatableRead ||
		db.beginOptions[0].AccessMode != pgx.ReadOnly {
		t.Fatalf("begin options = %#v, want repeatable-read read-only", db.beginOptions)
	}
	if db.readTx == nil || db.readTx.commitCalls != 1 {
		t.Fatalf("read transaction commitCalls = %#v, want one explicit commit", db.readTx)
	}
}

func TestPostgresIPQualityRepositoryHistoryDoesNotReadLatestOnlyView(t *testing.T) {
	t.Parallel()

	db := &fakeIPQualityDB{
		queryRows: map[string]pgx.Rows{
			"from ip_quality_assigned_vps_reports": &fakeIPQualityRows{},
		},
	}

	if _, err := historyForVPS(context.Background(), db, "vps_001"); err != nil {
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
	repo := newFakeIPQualityReadRepository(db)

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
	if len(db.queries) != 1 {
		t.Fatalf("query count = %d, want latest summary only", len(db.queries))
	}
	if db.readTx == nil {
		t.Fatal("read transaction = nil, want transaction")
	}
	if db.readTx.commitCalls != 1 {
		t.Fatalf("read transaction commitCalls = %d, want 1", db.readTx.commitCalls)
	}
}
func TestPostgresIPQualityRepositoryReadTransactionFailureContracts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("missing begin function", func(t *testing.T) {
		_, err := (&PostgresIPQualityRepository{}).GetVPSIPQuality(ctx, "vps_001")
		if err == nil || !strings.Contains(err.Error(), "without transaction support") {
			t.Fatalf("GetVPSIPQuality() error = %v, want transaction support error", err)
		}
	})

	t.Run("begin error", func(t *testing.T) {
		want := errors.New("begin failed")
		repo := &PostgresIPQualityRepository{
			beginTx: func(context.Context, pgx.TxOptions) (ipQualityTx, error) { return nil, want },
		}
		_, err := repo.GetVPSIPQuality(ctx, "vps_001")
		if !errors.Is(err, want) {
			t.Fatalf("GetVPSIPQuality() error = %v, want %v", err, want)
		}
	})

	t.Run("nil transaction", func(t *testing.T) {
		repo := &PostgresIPQualityRepository{
			beginTx: func(context.Context, pgx.TxOptions) (ipQualityTx, error) { return nil, nil },
		}
		_, err := repo.GetVPSIPQuality(ctx, "vps_001")
		if err == nil || !strings.Contains(err.Error(), "nil transaction") {
			t.Fatalf("GetVPSIPQuality() error = %v, want nil transaction error", err)
		}
	})

	t.Run("query error", func(t *testing.T) {
		want := errors.New("summary query failed")
		db := &fakeIPQualityDB{queryErr: want}
		repo := newFakeIPQualityReadRepository(db)
		_, err := repo.GetVPSIPQuality(ctx, "vps_001")
		if !errors.Is(err, want) {
			t.Fatalf("GetVPSIPQuality() error = %v, want %v", err, want)
		}
		if db.readTx.commitCalls != 0 {
			t.Fatalf("commitCalls = %d, want 0 after query error", db.readTx.commitCalls)
		}
	})

	t.Run("commit error", func(t *testing.T) {
		want := errors.New("commit failed")
		db := &fakeIPQualityDB{
			queryRows: map[string]pgx.Rows{"select latest.vps_id": &fakeIPQualityRows{}},
		}
		repo := newFakeIPQualityReadRepository(db)
		db.readTx.commitErr = want
		_, err := repo.GetVPSIPQuality(ctx, "vps_001")
		if !errors.Is(err, want) {
			t.Fatalf("GetVPSIPQuality() error = %v, want %v", err, want)
		}
		if db.readTx.commitCalls != 1 {
			t.Fatalf("commitCalls = %d, want 1", db.readTx.commitCalls)
		}
	})

	t.Run("latest summary without assigned report", func(t *testing.T) {
		db := &fakeIPQualityDB{
			queryRows: map[string]pgx.Rows{
				"select latest.vps_id": &fakeIPQualityRows{rows: []fakeIPQualityScan{{
					scan: scanIPQualitySummaryRow("vps_001", "ipq_missing", time.Now().UTC(), agentapi.IPQualityStatusSuccess),
				}}},
				"from ip_quality_reports r": &fakeIPQualityRows{},
			},
		}
		repo := newFakeIPQualityReadRepository(db)
		_, err := repo.GetVPSIPQuality(ctx, "vps_001")
		if err == nil || !strings.Contains(err.Error(), "assigned report missing from read snapshot") {
			t.Fatalf("GetVPSIPQuality() error = %v, want missing assigned report", err)
		}
		if db.readTx.commitCalls != 0 {
			t.Fatalf("commitCalls = %d, want 0 after snapshot association error", db.readTx.commitCalls)
		}
	})

	t.Run("missing historical report returns committed empty", func(t *testing.T) {
		db := &fakeIPQualityDB{
			queryRows: map[string]pgx.Rows{
				"join ip_quality_assigned_vps_reports assigned": &fakeIPQualityRows{},
			},
		}
		repo := newFakeIPQualityReadRepository(db)
		got, err := repo.GetVPSIPQualityReportDetail(ctx, "vps_001", "ipq_missing")
		if err != nil {
			t.Fatalf("GetVPSIPQualityReportDetail() error = %v", err)
		}
		assertIPQualitySnapshotEmpty(t, got)
		if db.readTx.commitCalls != 1 {
			t.Fatalf("commitCalls = %d, want 1 for empty historical response", db.readTx.commitCalls)
		}
	})

	t.Run("assigned report without summary", func(t *testing.T) {
		db := &fakeIPQualityDB{
			queryRows: map[string]pgx.Rows{
				"join ip_quality_assigned_vps_reports assigned": &fakeIPQualityRows{rows: []fakeIPQualityScan{{
					scan: scanIPQualityReportRow("ipq_001", "mi_001", time.Now().UTC(), agentapi.IPQualityStatusSuccess),
				}}},
				"from ip_quality_assigned_vps_reports assigned": &fakeIPQualityRows{},
			},
		}
		repo := newFakeIPQualityReadRepository(db)
		_, err := repo.GetVPSIPQualityReportDetail(ctx, "vps_001", "ipq_001")
		if err == nil || !strings.Contains(err.Error(), "assigned summary missing from read snapshot") {
			t.Fatalf("GetVPSIPQualityReportDetail() error = %v, want missing assigned summary", err)
		}
		if db.readTx.commitCalls != 0 {
			t.Fatalf("commitCalls = %d, want 0 after snapshot association error", db.readTx.commitCalls)
		}
	})
}

func TestPostgresIPQualityRepositoryGetLatestVPSIPQualitySummaryDoesNotLoadDetailTables(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 1, 10, 0, 0, 0, time.UTC)
	db := &fakeIPQualityDB{
		queryRows: map[string]pgx.Rows{
			"vps overview ip quality summary": &fakeIPQualityRows{rows: []fakeIPQualityScan{{
				scan: func(dest ...any) error {
					*(dest[0].(*string)) = "vps_001"
					*(dest[1].(*string)) = agentapi.IPQualityStatusSuccess
					*(dest[2].(*string)) = "high"
					*(dest[3].(*bool)) = true
					*(dest[4].(*time.Time)) = now
					return nil
				},
			}}},
			"from ip_quality_provider_results": &fakeIPQualityRows{rows: []fakeIPQualityScan{{
				scan: func(...any) error {
					t.Fatal("overview summary must not query provider results")
					return nil
				},
			}}},
			"from ip_quality_service_unlocks": &fakeIPQualityRows{rows: []fakeIPQualityScan{{
				scan: func(...any) error {
					t.Fatal("overview summary must not query service unlocks")
					return nil
				},
			}}},
			"from ip_quality_assigned_vps_reports": &fakeIPQualityRows{rows: []fakeIPQualityScan{{
				scan: func(...any) error {
					t.Fatal("overview summary must not query 30-history assigned reports")
					return nil
				},
			}}},
		},
	}
	repo := newFakeIPQualityReadRepository(db)

	got, err := repo.GetLatestVPSIPQualitySummary(context.Background(), "vps_001")
	if err != nil {
		t.Fatalf("GetLatestVPSIPQualitySummary() error = %v", err)
	}
	if got == nil || got.Status != agentapi.IPQualityStatusSuccess || got.RiskLevel != "high" || !got.Stale {
		t.Fatalf("summary = %#v", got)
	}
	joined := strings.ToLower(strings.Join(db.queries, "\n"))
	if !strings.Contains(joined, "vps overview ip quality summary") {
		t.Fatalf("summary-only query missing overview marker: %s", joined)
	}
	if strings.Contains(joined, "ip_quality_provider_results") ||
		strings.Contains(joined, "ip_quality_service_unlocks") ||
		strings.Contains(joined, "limit 30") ||
		strings.Contains(joined, "raw_json") ||
		strings.Contains(joined, "diagnostics_json") {
		t.Fatalf("summary-only queries leaked detail tables: %s", joined)
	}
	if strings.Contains(joined, "provider_count") || strings.Contains(joined, "unlockable_count") {
		t.Fatalf("overview summary selected detail aggregates: %s", joined)
	}
	if strings.Contains(joined, "ip_quality_latest_vps_summaries") ||
		strings.Contains(joined, "ip_quality_assigned_vps_reports") {
		t.Fatalf("overview summary still uses the detail-joining latest summaries view: %s", joined)
	}
	if len(db.queries) != 1 {
		t.Fatalf("query count = %d, want 1 summary query", len(db.queries))
	}
}

func TestPostgresIPQualityRepositoryReadsVPSIPQualityThroughFilteredAssignedView(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 1, 10, 0, 0, 0, time.UTC)
	db := &fakeIPQualityDB{
		queryRows: map[string]pgx.Rows{
			"from ip_quality_reports r": &fakeIPQualityRows{rows: []fakeIPQualityScan{{
				scan: scanIPQualityReportRow("ipq_valid", "mi_001", now, agentapi.IPQualityStatusSuccess),
			}}},
			"from ip_quality_provider_results": &fakeIPQualityRows{},
			"from ip_quality_service_unlocks":  &fakeIPQualityRows{},
			"select latest.vps_id": &fakeIPQualityRows{rows: []fakeIPQualityScan{{
				scan: scanIPQualitySummaryRow("vps_001", "ipq_valid", now, agentapi.IPQualityStatusSuccess),
			}}},
			"from ip_quality_assigned_vps_reports": &fakeIPQualityRows{rows: []fakeIPQualityScan{{
				scan: func(dest ...any) error {
					*(dest[0].(*string)) = "vps_001"
					*(dest[1].(*string)) = "ipq_valid"
					*(dest[2].(*time.Time)) = now
					*(dest[3].(*string)) = "203.0.113.10"
					*(dest[4].(*int)) = 4
					*(dest[5].(*string)) = agentapi.IPQualityStatusSuccess
					*(dest[6].(*string)) = "low"
					*(dest[7].(*string)) = "US"
					*(dest[8].(*string)) = "United States"
					*(dest[9].(*string)) = "AS64500"
					*(dest[10].(*string)) = "Example Network"
					*(dest[11].(*bool)) = false
					*(dest[12].(*bool)) = false
					*(dest[13].(*string)) = "link"
					*(dest[14].(*string)) = ""
					*(dest[15].(*string)) = ""
					*(dest[16].(*int)) = 0
					*(dest[17].(*int)) = 0
					*(dest[18].(*[]byte)) = []byte(`{}`)
					return nil
				},
			}}},
		},
	}
	repo := newFakeIPQualityReadRepository(db)

	if _, err := repo.GetVPSIPQuality(context.Background(), "vps_001"); err != nil {
		t.Fatalf("GetVPSIPQuality() error = %v", err)
	}

	joined := strings.ToLower(strings.Join(db.queries, "\n"))
	if strings.Contains(joined, "join ip_quality_reports r on r.monitoring_instance_id") ||
		strings.Contains(joined, "r.ip_address in (nullif") {
		t.Fatalf("repository query bypassed filtered IP quality read views: %s", joined)
	}
	if strings.Contains(joined, "ip_quality_latest_vps_summaries") {
		t.Fatalf("repository queries = %s, legacy latest view cannot express replay-safe ordering", joined)
	}
	if !strings.Contains(joined, "ip_quality_assigned_vps_reports") {
		t.Fatalf("repository queries = %s, want filtered assigned reports view", joined)
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
		CoverageJSON:         json.RawMessage(`{"expected_provider_count":2,"successful_provider_count":1}`),
		DiagnosticsJSON:      json.RawMessage(`{"source_version":"v2"}`),
		ProviderResults: []ipquality.ProviderResultWrite{{
			Provider:    "ipinfo",
			Status:      "success",
			SourceType:  "default",
			LatencyMS:   intPtr(73),
			UsageType:   "hosting",
			CompanyType: "hosting",
			RiskLevel:   "low",
			ExtraJSON:   json.RawMessage(`{"risk":{"score":12}}`),
		}},
		ServiceUnlocks: []ipquality.ServiceUnlockWrite{{
			Service:     "netflix",
			Source:      "netflix_title_probe",
			Status:      "unlocked",
			ProbeStatus: "success",
			LatencyMS:   intPtr(211),
			Region:      "US",
			UnlockType:  "full",
			ExtraJSON:   json.RawMessage(`{"title_probe":"full_catalog"}`),
		}},
	}
}

type fakeIPQualityDB struct {
	queryRows    map[string]pgx.Rows
	queryErr     error
	queries      []string
	readTx       *fakeIPQualityTx
	beginOptions []pgx.TxOptions
}

func newFakeIPQualityReadRepository(db *fakeIPQualityDB) *PostgresIPQualityRepository {
	db.readTx = &fakeIPQualityTx{querySource: db}
	return &PostgresIPQualityRepository{
		db: db,
		beginTx: func(_ context.Context, options pgx.TxOptions) (ipQualityTx, error) {
			db.beginOptions = append(db.beginOptions, options)
			return db.readTx, nil
		},
	}
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
	querySource   *fakeIPQualityDB
	execSQL       []string
	commitCalls   int
	commitErr     error
	rollbackCalls int
}

func (f *fakeIPQualityTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if f.querySource == nil {
		return nil, errors.New("fake IP quality transaction has no query source")
	}
	return f.querySource.Query(ctx, sql, args...)
}

func (f *fakeIPQualityTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.execSQL = append(f.execSQL, sql)
	return pgconn.CommandTag{}, nil
}

func (f *fakeIPQualityTx) Commit(context.Context) error {
	f.commitCalls++
	return f.commitErr
}

func (f *fakeIPQualityTx) Rollback(context.Context) error {
	f.rollbackCalls++
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

func scanIPQualitySummaryRow(vpsID, reportID string, observedAt time.Time, status string) func(dest ...any) error {
	return func(dest ...any) error {
		*(dest[0].(*string)) = vpsID
		*(dest[1].(*string)) = reportID
		*(dest[2].(*time.Time)) = observedAt
		*(dest[3].(*string)) = "203.0.113.10"
		*(dest[4].(*int)) = 4
		*(dest[5].(*string)) = status
		*(dest[6].(*string)) = "low"
		*(dest[7].(*string)) = "US"
		*(dest[8].(*string)) = "United States"
		*(dest[9].(*string)) = "AS64500"
		*(dest[10].(*string)) = "Example Network"
		*(dest[11].(*bool)) = false
		*(dest[12].(*bool)) = false
		*(dest[13].(*string)) = "link"
		*(dest[14].(*string)) = ""
		*(dest[15].(*string)) = ""
		*(dest[16].(*int)) = 0
		*(dest[17].(*int)) = 0
		*(dest[18].(*[]byte)) = []byte(`{}`)
		return nil
	}
}

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
		*(dest[23].(*[]byte)) = []byte(`{"expected_provider_count":2,"successful_provider_count":1}`)
		*(dest[24].(*[]byte)) = []byte(`{"source_version":"v2"}`)
		*(dest[25].(*time.Time)) = observedAt.Add(2 * time.Second)
		return nil
	}
}
