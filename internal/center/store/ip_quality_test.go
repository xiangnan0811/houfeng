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
			"select state.ip_version, state.latest_report_id, state.availability": fakeIPQualityFamilyRows("ipq_001", nil, nil),
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

	got, err := repo.GetVPSIPQuality(context.Background(), "vps_001", 0)
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
	if got.Product != nil || got.Summary.ProductRevision != 0 || got.Summary.ProductSummary != nil || got.Summary.IntelligenceState != "" || got.LatestReport.Product != nil || got.History[0].ProductRevision != 0 {
		t.Fatalf("legacy report invented product fields: summary=%#v product=%#v", got.Summary, got.Product)
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

	if _, err := historyForVPS(context.Background(), db, "vps_001", 4); err != nil {
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

	got, err := repo.GetVPSIPQuality(context.Background(), "vps_001", 0)
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
		_, err := (&PostgresIPQualityRepository{}).GetVPSIPQuality(ctx, "vps_001", 0)
		if err == nil || !strings.Contains(err.Error(), "without transaction support") {
			t.Fatalf("GetVPSIPQuality() error = %v, want transaction support error", err)
		}
	})

	t.Run("begin error", func(t *testing.T) {
		want := errors.New("begin failed")
		repo := &PostgresIPQualityRepository{
			beginTx: func(context.Context, pgx.TxOptions) (ipQualityTx, error) { return nil, want },
		}
		_, err := repo.GetVPSIPQuality(ctx, "vps_001", 0)
		if !errors.Is(err, want) {
			t.Fatalf("GetVPSIPQuality() error = %v, want %v", err, want)
		}
	})

	t.Run("nil transaction", func(t *testing.T) {
		repo := &PostgresIPQualityRepository{
			beginTx: func(context.Context, pgx.TxOptions) (ipQualityTx, error) { return nil, nil },
		}
		_, err := repo.GetVPSIPQuality(ctx, "vps_001", 0)
		if err == nil || !strings.Contains(err.Error(), "nil transaction") {
			t.Fatalf("GetVPSIPQuality() error = %v, want nil transaction error", err)
		}
	})

	t.Run("query error", func(t *testing.T) {
		want := errors.New("summary query failed")
		db := &fakeIPQualityDB{queryErr: want}
		repo := newFakeIPQualityReadRepository(db)
		_, err := repo.GetVPSIPQuality(ctx, "vps_001", 0)
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
		_, err := repo.GetVPSIPQuality(ctx, "vps_001", 0)
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
				"select state.ip_version, state.latest_report_id, state.availability": fakeIPQualityFamilyRows("ipq_missing", nil, nil),
				"from ip_quality_assigned_vps_reports": &fakeIPQualityRows{rows: []fakeIPQualityScan{{
					scan: scanIPQualitySummaryRow("vps_001", "ipq_missing", time.Now().UTC(), agentapi.IPQualityStatusSuccess),
				}}},
				"select latest.vps_id": &fakeIPQualityRows{rows: []fakeIPQualityScan{{
					scan: scanIPQualitySummaryRow("vps_001", "ipq_missing", time.Now().UTC(), agentapi.IPQualityStatusSuccess),
				}}},
				"from ip_quality_reports r": &fakeIPQualityRows{},
			},
		}
		repo := newFakeIPQualityReadRepository(db)
		_, err := repo.GetVPSIPQuality(ctx, "vps_001", 0)
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
					*(dest[5].(*int)) = 6
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
	if got == nil || got.IPVersion != 6 || got.Status != agentapi.IPQualityStatusSuccess || got.RiskLevel != "high" || !got.Stale {
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
			"select state.ip_version, state.latest_report_id, state.availability": fakeIPQualityFamilyRows("ipq_valid", nil, nil),
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

	if _, err := repo.GetVPSIPQuality(context.Background(), "vps_001", 0); err != nil {
		t.Fatalf("GetVPSIPQuality() error = %v", err)
	}

	joined := strings.ToLower(strings.Join(db.queries, "\n"))
	if strings.Contains(joined, "join ip_quality_reports r on r.monitoring_instance_id") ||
		strings.Contains(joined, "r.ip_address in (nullif") {
		t.Fatalf("repository query bypassed filtered IP quality read views: %s", joined)
	}
	if !strings.Contains(joined, "ip_quality_vps_family_state") {
		t.Fatalf("repository queries = %s, want shared family selection view", joined)
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
	if db.queryRows == nil {
		db.queryRows = make(map[string]pgx.Rows)
	}
	if _, ok := db.queryRows["select state.ip_version, state.latest_report_id, state.availability"]; !ok {
		db.queryRows["select state.ip_version, state.latest_report_id, state.availability"] = &fakeIPQualityRows{}
	}
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
	if strings.Contains(sql, ipQualityProductBatchMarker) || strings.Contains(sql, ipQualityProductProviderMarker) {
		return &fakeIPQualityRows{}, nil
	}
	return nil, errors.New("unexpected query")
}

type fakeIPQualityTx struct {
	querySource   *fakeIPQualityDB
	execSQL       []string
	execArgs      [][]any
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

func (f *fakeIPQualityTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.execSQL = append(f.execSQL, sql)
	f.execArgs = append(f.execArgs, append([]any(nil), args...))
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

func fakeIPQualityFamilyRows(reportID string, observedAt *time.Time, diagnostics []byte) *fakeIPQualityRows {
	return &fakeIPQualityRows{rows: []fakeIPQualityScan{{
		scan: func(dest ...any) error {
			*(dest[0].(*int)) = 4
			if reportID != "" {
				*(dest[1].(**string)) = &reportID
			}
			*(dest[2].(*string)) = "unknown"
			*(dest[3].(*bool)) = false
			*(dest[4].(*bool)) = true
			*(dest[5].(**time.Time)) = observedAt
			if observedAt != nil {
				status := "failure"
				code := "lookup_failed"
				*(dest[6].(**string)) = &status
				*(dest[7].(**string)) = &code
			}
			*(dest[8].(*[]byte)) = diagnostics
			return nil
		},
	}}}
}

func TestIPQualityReadFamilySelectionNeverFallsBackFromEmptyExplicitFamily(t *testing.T) {
	now := time.Now().UTC()
	db := &fakeIPQualityDB{queryRows: map[string]pgx.Rows{
		"select state.ip_version, state.latest_report_id, state.availability": fakeIPQualityFamilyRows("ipq_ipv4", &now, []byte(`{"collection_revision":4,"collection_id":"paired","expected_ip_versions":[4,6],"ip_address_family":4,"family_availability":"available"}`)),
		"from ip_quality_assigned_vps_reports": &fakeIPQualityRows{rows: []fakeIPQualityScan{{
			scan: scanIPQualitySummaryRow("vps_001", "ipq_ipv4", now.Add(-time.Hour), "success"),
		}}},
	}}
	result, err := newFakeIPQualityReadRepository(db).GetVPSIPQuality(context.Background(), "vps_001", 6)
	if err != nil {
		t.Fatal(err)
	}
	if result.SelectedIPVersion != 6 || result.DefaultIPVersion != 4 || result.Summary != nil || result.LatestReport != nil || len(result.History) != 0 {
		t.Fatalf("explicit empty IPv6 borrowed IPv4: %#v", result)
	}
	if len(result.Families) != 2 || result.Families[0].LatestReportSummary == nil || result.Families[0].LatestAttempt.CollectionID != "paired" || result.Families[1].Availability != "unknown" {
		t.Fatalf("navigation lost other family evidence: %#v", result.Families)
	}
}

func TestIPQualityAttemptsKeepInvalidPairedMetadataDistinctFromLegacy(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name        string
		diagnostics string
		scope       string
		id          string
	}{
		{"legacy", `{"source_version":"v2"}`, "legacy_single_family", ""},
		{"valid paired", `{"collection_revision":4,"collection_id":"paired","expected_ip_versions":[4,6],"ip_address_family":4,"family_availability":"available"}`, "dual_stack", "paired"},
		{"unsupported revision", `{"collection_revision":6,"collection_id":"paired","expected_ip_versions":[4,6],"ip_address_family":4,"family_availability":"available"}`, "dual_stack", ""},
		{"selected family", `{"collection_revision":5,"collection_id":"single","expected_ip_versions":[4],"ip_address_family":4,"family_availability":"available"}`, "single_family", "single"},
		{"invalid family", `{"collection_revision":4,"collection_id":"paired","expected_ip_versions":[4,6],"ip_address_family":6,"family_availability":"available"}`, "dual_stack", ""},
		{"malformed paired", `{"collection_id":"paired","expected_ip_versions":[6,4]}`, "dual_stack", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := &fakeIPQualityDB{queryRows: map[string]pgx.Rows{
				"select state.ip_version, state.latest_report_id, state.availability": fakeIPQualityFamilyRows("", &now, []byte(tc.diagnostics)),
			}}
			got, err := newFakeIPQualityReadRepository(db).GetVPSIPQuality(context.Background(), "vps_001", 4)
			if err != nil {
				t.Fatal(err)
			}
			attempt := got.Families[0].LatestAttempt
			if attempt == nil || attempt.CollectionScope != tc.scope || attempt.CollectionID != tc.id || attempt.ReportStatus != "failure" || attempt.ErrorCode != "lookup_failed" {
				t.Fatalf("attempt=%#v, want %s/%s failure", attempt, tc.scope, tc.id)
			}
			if got.Summary != nil || got.LatestReport != nil || len(got.History) != 0 {
				t.Fatalf("failure attempt became facts: %#v", got)
			}
		})
	}
}

func TestIPQualityReadRejectsInvalidFamilyBeforeStartingTransaction(t *testing.T) {
	for _, version := range []int{-1, 1, 5, 46} {
		db := &fakeIPQualityDB{}
		_, err := newFakeIPQualityReadRepository(db).GetVPSIPQuality(context.Background(), "vps_001", version)
		if !errors.Is(err, ipquality.ErrInvalidIPQualityReport) || len(db.beginOptions) != 0 {
			t.Fatalf("family %d: err=%v begins=%#v", version, err, db.beginOptions)
		}
	}
}

func TestSaveReportsParsesCollectionMetadataBeforeSanitizingDiagnostics(t *testing.T) {
	tx := &fakeIPQualityTx{}
	repo := &PostgresIPQualityRepository{
		beginTx:     func(context.Context, pgx.TxOptions) (ipQualityTx, error) { return tx, nil },
		newReportID: func() (string, error) { return "ipq_metadata", nil },
	}
	report := ipQualityReportWrite()
	report.DiagnosticsJSON = json.RawMessage(`{"collection_revision":4,"collection_id":"paired","expected_ip_versions":[4,6],"ip_address_family":4,"family_availability":"available","collect_request_id":"ipqc_test"}`)
	reports := []ipquality.ReportWrite{report}
	if err := repo.SaveReports(context.Background(), reports); err != nil {
		t.Fatal(err)
	}
	if !reports[0].CollectionMetadata.Valid || reports[0].CollectionMetadata.CollectionID != "paired" || reports[0].CollectionMetadata.CollectRequestID != "ipqc_test" {
		t.Fatalf("direct construction metadata=%#v", reports[0].CollectionMetadata)
	}
}

func TestSaveReportsStoresRevision1SummaryWithoutRewritingLegacyJSON(t *testing.T) {
	tx := &fakeIPQualityTx{}
	repo := &PostgresIPQualityRepository{
		beginTx:     func(context.Context, pgx.TxOptions) (ipQualityTx, error) { return tx, nil },
		newReportID: func() (string, error) { return "ipq_product", nil },
	}
	report := ipQualityRevision1ReportWrite()
	if err := repo.SaveReports(context.Background(), []ipquality.ReportWrite{report}); err != nil {
		t.Fatal(err)
	}
	if tx.commitCalls != 1 {
		t.Fatalf("commitCalls = %d, want 1", tx.commitCalls)
	}
	reportSQL, reportArgs := ipQualityExec(t, tx, "insert into ip_quality_reports")
	if !strings.Contains(reportSQL, "product_json") || !strings.Contains(reportSQL, "product_summary_json") {
		t.Fatalf("report SQL = %s, want typed product columns", reportSQL)
	}
	raw, _ := reportArgs[22].([]byte)
	if strings.Contains(string(raw), "ptr.example.net") || strings.Contains(string(raw), "gmail.com") || strings.Contains(string(raw), `"revision"`) {
		t.Fatalf("raw JSON = %s, want legacy payload without product fields", raw)
	}
	product, _ := reportArgs[25].([]byte)
	summary, _ := reportArgs[26].([]byte)
	if !strings.Contains(string(product), `"ptr.example.net"`) || !strings.Contains(string(summary), `"vpn"`) {
		t.Fatalf("product/summary = %s / %s, want stored PTR and agent risk flag", product, summary)
	}
	_, enqueueArgs := ipQualityExec(t, tx, "insert into ip_quality_intelligence")
	if enqueueArgs[0] != "ipq_product" || !enqueueArgs[1].(time.Time).Equal(report.ReceivedAt.Add(10*time.Minute)) {
		t.Fatalf("enqueue args = %#v, want pending deadline", enqueueArgs)
	}
	if _, providerArgs := ipQualityExec(t, tx, "details_json"); !strings.Contains(string(providerArgs[len(providerArgs)-1].([]byte)), `"agent_public"`) {
		t.Fatalf("provider args = %#v, want anonymous details", providerArgs)
	}
	if _, serviceArgs := ipQualityExec(t, tx, "checks_json"); !strings.Contains(string(serviceArgs[len(serviceArgs)-1].([]byte)), `"title_81280792"`) {
		t.Fatalf("service args = %#v, want typed check", serviceArgs)
	}
}

func TestSaveReportsSkipsIntelligenceForBackfillAndPlaceholder(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*ipquality.ReportWrite)
	}{
		{name: "backfilled", mutate: func(report *ipquality.ReportWrite) { report.IsBackfilled = true }},
		{name: "placeholder", mutate: func(report *ipquality.ReportWrite) {
			report.IPAddress = "0.0.0.0"
			report.ProviderResults[0].Details.QueriedIP = "0.0.0.0"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &fakeIPQualityTx{}
			repo := &PostgresIPQualityRepository{
				beginTx:     func(context.Context, pgx.TxOptions) (ipQualityTx, error) { return tx, nil },
				newReportID: func() (string, error) { return "ipq_" + tc.name, nil },
			}
			report := ipQualityRevision1ReportWrite()
			tc.mutate(&report)
			if err := repo.SaveReports(context.Background(), []ipquality.ReportWrite{report}); err != nil {
				t.Fatal(err)
			}
			if containsSQL(tx.execSQL, "insert into ip_quality_intelligence") {
				t.Fatalf("execSQL = %#v, want summary without a job", tx.execSQL)
			}
			_, args := ipQualityExec(t, tx, "insert into ip_quality_reports")
			if args[25] == nil || args[26] == nil {
				t.Fatalf("product columns = %#v/%#v, want agent summary", args[25], args[26])
			}
		})
	}
}

func TestSaveReportsRejectsDetailsWithoutProductRevision(t *testing.T) {
	tx := &fakeIPQualityTx{}
	repo := &PostgresIPQualityRepository{
		beginTx: func(context.Context, pgx.TxOptions) (ipQualityTx, error) { return tx, nil },
	}
	report := ipQualityReportWrite()
	report.ProviderResults[0].Details = &agentapi.IPQualityProviderDetails{QueriedIP: report.IPAddress, Method: "agent_public", APITier: "anonymous", ObservedAt: report.ObservedAt}
	err := repo.SaveReports(context.Background(), []ipquality.ReportWrite{report})
	if !errors.Is(err, ipquality.ErrInvalidIPQualityReport) || tx.commitCalls != 0 || len(tx.execSQL) != 0 {
		t.Fatalf("SaveReports() = %v commit=%d exec=%#v, want rejection before write", err, tx.commitCalls, tx.execSQL)
	}
}

func TestIPQualityReadUsesStoredSummaryAndDoesNotRecomputeTerminalFacts(t *testing.T) {
	now := time.Date(2026, time.June, 1, 10, 0, 0, 0, time.UTC)
	agentSummary := []byte(`{"risk_flags":["vpn"],"high_risk_sources":[],"restricted_services":[],"blacklist_listed_count":0,"blacklist_checked_count":0,"service_available_count":1,"service_partial_count":0,"service_restricted_count":0,"service_unknown_count":0,"geo_status":"unknown"}`)
	terminalSummary := []byte(`{"risk_flags":["abuse"],"high_risk_sources":["abuseipdb"],"restricted_services":["netflix"],"blacklist_listed_count":1,"blacklist_checked_count":1,"service_available_count":0,"service_partial_count":0,"service_restricted_count":1,"service_unknown_count":0,"geo_status":"consistent"}`)
	product := []byte(`{"revision":1,"ptr_names":["historical.example.net"],"mail":[]}`)
	completed := now.Add(time.Minute)
	state := "complete"
	db := &fakeIPQualityDB{queryRows: map[string]pgx.Rows{
		"select state.ip_version, state.latest_report_id, state.availability": fakeIPQualityFamilyRows("ipq_product", nil, nil),
		"from ip_quality_reports r":            &fakeIPQualityRows{rows: []fakeIPQualityScan{{scan: scanIPQualityReportRow("ipq_product", "mi_001", now, agentapi.IPQualityStatusSuccess)}}},
		"from ip_quality_provider_results":     &fakeIPQualityRows{},
		"from ip_quality_service_unlocks":      &fakeIPQualityRows{},
		"from ip_quality_assigned_vps_reports": &fakeIPQualityRows{rows: []fakeIPQualityScan{{scan: scanIPQualitySummaryRow("vps_001", "ipq_product", now, agentapi.IPQualityStatusSuccess)}}},
		ipQualityProductBatchMarker: &fakeIPQualityRows{rows: []fakeIPQualityScan{{scan: func(dest ...any) error {
			*(dest[0].(*string)) = "ipq_product"
			*(dest[1].(*string)) = "203.0.113.10"
			*(dest[2].(*int)) = 4
			*(dest[3].(*time.Time)) = now
			*(dest[4].(*[]byte)) = product
			*(dest[5].(*[]byte)) = agentSummary
			*(dest[6].(**string)) = &state
			*(dest[7].(**time.Time)) = &completed
			*(dest[8].(*[]byte)) = []byte(`{"providers":[{"provider":"abuseipdb","status":"success","details":{"method":"center_api","api_tier":"authenticated","queried_ip":"203.0.113.10","scores":[],"field_states":{}}}],"blacklists":[{"source":"spamcop","status":"listed","codes":["127.0.0.2"]}],"registration":{"status":"success","network_country_code":"US"},"asn":{"status":"success","origins":["AS64500"]},"source_outcomes":[{"source_id":"ripe","status":"success"}]}`)
			*(dest[9].(*[]byte)) = terminalSummary
			return nil
		}}}},
	}}
	got, err := newFakeIPQualityReadRepository(db).GetVPSIPQuality(context.Background(), "vps_001", 4)
	if err != nil {
		t.Fatal(err)
	}
	if got.Product == nil || got.Product.ReportID != "ipq_product" || got.Product.State != "complete" || got.Product.CompletedAt == nil || !got.Product.CompletedAt.Equal(completed) {
		t.Fatalf("product = %#v, want completed stored view", got.Product)
	}
	if len(got.Product.PTRNames) != 1 || got.Product.PTRNames[0] != "historical.example.net" {
		t.Fatalf("PTR = %#v, want historical stored name", got.Product.PTRNames)
	}
	if got.Summary.ProductRevision != 1 || got.Summary.IntelligenceState != "complete" || got.Summary.ProductSummary == nil || got.Summary.ProductSummary.BlacklistListedCount != 1 || got.Summary.ProductSummary.GeoStatus != "consistent" || len(got.Summary.ProductSummary.RiskFlags) != 1 || got.Summary.ProductSummary.RiskFlags[0] != "abuse" {
		t.Fatalf("summary = %#v, want stored terminal summary rather than recomputed agent summary", got.Summary)
	}
	if got.Summary.RiskLevel != "low" {
		t.Fatalf("old risk level = %q, want preserved historical field", got.Summary.RiskLevel)
	}
	if len(got.Product.Blacklists) != 1 || got.Product.Blacklists[0].Status != "listed" || len(got.Product.Providers) != 1 || got.Product.Providers[0].Provider != "abuseipdb" {
		t.Fatalf("view facts = %#v, want snapshot samples", got.Product)
	}
	if got.History[0].ProductRevision != 1 || got.Families[0].LatestReportSummary.IntelligenceState != "complete" {
		t.Fatalf("history/family did not share the batch summary: %#v %#v", got.History, got.Families)
	}
}

func TestFailedSnapshotDiagnosticsKeepAgentSummary(t *testing.T) {
	now := time.Date(2026, time.June, 1, 10, 0, 0, 0, time.UTC)
	completed := now.Add(time.Minute)
	product := []byte(`{"revision":1,"ptr_names":["historical.example.net"],"mail":[]}`)
	agentSummary := []byte(`{"risk_flags":["vpn"],"high_risk_sources":[],"restricted_services":[],"blacklist_listed_count":0,"blacklist_checked_count":0,"service_available_count":1,"service_partial_count":0,"service_restricted_count":0,"service_unknown_count":0,"geo_status":"unknown"}`)
	intelligenceSummary := []byte(`{"risk_flags":["abuse"],"high_risk_sources":["abuseipdb"],"restricted_services":["netflix"],"blacklist_listed_count":1,"blacklist_checked_count":1,"service_available_count":0,"service_partial_count":0,"service_restricted_count":1,"service_unknown_count":0,"geo_status":"consistent"}`)
	snapshot, err := json.Marshal(ipquality.IntelligenceSnapshot{
		Providers: []ipquality.ProviderResultRead{{
			Provider: "abuseipdb", Status: "failure", SourceType: "default", ErrorCode: "http_failed",
			Details: &agentapi.IPQualityProviderDetails{
				QueriedIP: "203.0.113.10", ObservedAt: now, Method: "center_api", APITier: "authenticated",
				Scores: []agentapi.IPQualityScore{}, FieldStates: map[string]string{},
			},
		}},
		Blacklists: []agentapi.IPQualityBlacklistResult{{
			Source: "spamcop", Category: "spam", Status: "unknown", QueryMethod: "dnsbl",
			ErrorCode: "dns_failed", Codes: []string{}, ObservedAt: now,
		}},
		Registration:   agentapi.IPQualityRegistration{Status: "unknown"},
		ASN:            agentapi.IPQualityASN{Status: "unknown", Origins: []string{}},
		SourceOutcomes: []ipquality.SourceOutcome{{SourceID: "abuseipdb", Status: "failure", ErrorCode: "http_failed", ObservedAt: now}},
	})
	if err != nil {
		t.Fatal(err)
	}
	vpn := true
	providers := []ipquality.ProviderResultWrite{{Provider: "ipapi.is", Status: "success", IsVPN: &vpn}}
	failed := "failed"
	view, ok, err := buildStoredIPQualityProduct(ipQualityStoredProductRow{
		reportID: "ipq_failed", ipAddress: "203.0.113.10", ipVersion: 4, observedAt: now,
		productJSON: product, agentSummaryJSON: agentSummary, intelligenceState: &failed, completedAt: &completed,
		snapshotJSON: snapshot, intelligenceSummaryJSON: intelligenceSummary,
	}, providers)
	if err != nil || !ok || view.State != "failed" || view.Summary.BlacklistListedCount != 0 || len(view.Summary.RiskFlags) != 1 || view.Summary.RiskFlags[0] != "vpn" {
		t.Fatalf("failed view = %#v ok=%t err=%v, want agent VPN summary", view.Summary, ok, err)
	}
	foundFailure, foundVPN, foundUnknown := false, false, false
	for _, row := range view.Providers {
		if row.Provider == "abuseipdb" && row.Status == "failure" && row.ErrorCode == "http_failed" && row.IsVPN == nil && row.Details != nil && row.Details.Method == "center_api" && row.Details.City == "" {
			foundFailure = true
		}
		if row.Provider == "ipapi.is" && row.IsVPN != nil && *row.IsVPN {
			foundVPN = true
		}
	}
	for _, row := range view.Blacklists {
		if row.Source == "spamcop" && row.Status == "unknown" && row.ErrorCode == "dns_failed" {
			foundUnknown = true
		}
	}
	if !foundFailure || !foundVPN || !foundUnknown {
		t.Fatalf("diagnostics = providers:%#v blacklists:%#v", view.Providers, view.Blacklists)
	}
	facts, err := interpretStoredIPQualityProduct(product, agentSummary, &failed, &completed, nil, intelligenceSummary, true)
	if err != nil || facts.snapshot != nil || facts.summary == nil || len(facts.summary.RiskFlags) != 1 || facts.summary.RiskFlags[0] != "vpn" {
		t.Fatalf("overview failed facts = %#v err=%v, want agent summary and no invented snapshot", facts, err)
	}
	if _, err := interpretStoredIPQualityProduct(product, agentSummary, &failed, &completed, []byte(`{`), intelligenceSummary, true); err == nil {
		t.Fatal("corrupt failed snapshot was ignored")
	}
	swept, ok, err := buildStoredIPQualityProduct(ipQualityStoredProductRow{
		reportID: "ipq_swept", ipAddress: "203.0.113.10", ipVersion: 4, observedAt: now,
		productJSON: product, agentSummaryJSON: agentSummary, intelligenceState: &failed, completedAt: &completed,
		intelligenceSummaryJSON: intelligenceSummary,
	}, providers)
	if err != nil || !ok || swept.State != "failed" || len(swept.Blacklists) != 0 || len(swept.Providers) != 1 || swept.Providers[0].Provider != "ipapi.is" || swept.Summary.RiskFlags[0] != "vpn" {
		t.Fatalf("swept view = %#v err=%v, want empty center diagnostics", swept, err)
	}
	skipped := "skipped"
	cleared, err := json.Marshal(ipquality.IntelligenceSnapshot{
		Blacklists: []agentapi.IPQualityBlacklistResult{{Source: "spamcop", Status: "listed", Codes: []string{"127.0.0.2"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	empty, ok, err := buildStoredIPQualityProduct(ipQualityStoredProductRow{
		reportID: "ipq_skipped", ipAddress: "203.0.113.10", ipVersion: 4, observedAt: now,
		productJSON: product, agentSummaryJSON: agentSummary, intelligenceState: &skipped, completedAt: &completed,
		snapshotJSON: cleared, intelligenceSummaryJSON: intelligenceSummary,
	}, providers)
	if err != nil || !ok || empty.State != "skipped" || len(empty.Blacklists) != 0 || len(empty.Providers) != 1 || empty.Summary.RiskFlags[0] != "vpn" || empty.Summary.BlacklistListedCount != 0 {
		t.Fatalf("skipped view = %#v err=%v, want cleared snapshot to stay empty", empty, err)
	}
}

func ipQualityRevision1ReportWrite() ipquality.ReportWrite {
	report := ipQualityReportWrite()
	report.ReceivedAt = report.ObservedAt.Add(time.Second)
	report.RiskLevel = "low"
	report.IPAddress = "203.0.113.10"
	report.Product = &agentapi.IPQualityAgentProduct{
		Revision: agentapi.IPQualityProductRevision,
		PTRNames: []string{"ptr.example.net"},
		Mail: []agentapi.IPQualityMailResult{{
			Provider: "gmail", Domain: "gmail.com", Status: "reachable",
			MXHost: "gmail-smtp-in.l.google.com", TargetIP: "1.1.1.1",
			ObservedAt: report.ObservedAt, GreetingCode: new(220), LatencyMS: new(40),
		}},
	}
	report.ProviderResults[0].Provider = "ipapi.is"
	report.ProviderResults[0].IsVPN = new(true)
	report.ProviderResults[0].Details = &agentapi.IPQualityProviderDetails{
		QueriedIP: report.IPAddress, ObservedAt: report.ObservedAt,
		Method: "agent_public", APITier: "anonymous", CountryCode: "US",
		Scores: []agentapi.IPQualityScore{{
			ID: "risk", Scope: "ip", Direction: "higher_risk", SourceLabel: "high",
			Value: new(90.0), Min: new(0.0), Max: new(100.0),
		}},
		FieldStates: map[string]string{"is_vpn": "known"},
	}
	report.ServiceUnlocks[0].Checks = []agentapi.IPQualityServiceCheck{{
		ID: "title_81280792", Status: "available", Region: "US", HTTPStatus: new(200),
	}}
	return report
}

func ipQualityExec(t *testing.T, tx *fakeIPQualityTx, fragment string) (string, []any) {
	t.Helper()
	for i, sql := range tx.execSQL {
		if strings.Contains(sql, fragment) {
			return sql, tx.execArgs[i]
		}
	}
	t.Fatalf("exec SQL missing %q: %#v", fragment, tx.execSQL)
	return "", nil
}
