package store

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/ipquality"
	"houfeng/internal/center/monitoringinstances"
	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/center/syncing"
	"houfeng/internal/contracts/agentapi"
)

func TestPostgresIntegrationIPQualityProductRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	fixture := newRecordsPostgresFixture(t, ctx)
	t.Run("typed round trip keeps legacy JSON and stored summaries", func(t *testing.T) {
		assertIPQualityProductRoundTrip(t, ctx, fixture)
	})
	t.Run("sync replay enqueues one job", func(t *testing.T) {
		assertIPQualityProductSyncReplay(t, ctx, fixture)
	})
	t.Run("rollback leaves no intelligence job", func(t *testing.T) {
		assertIPQualityProductRollback(t, ctx, fixture)
	})
}

func assertIPQualityProductRoundTrip(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture) {
	t.Helper()
	const prefix = "ipq_product_roundtrip"
	seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "ip-quality-product-roundtrip", 4)
	repo := NewPostgresIPQualityRepository(runtimePool)

	legacyObserved := time.Date(2026, time.June, 1, 8, 0, 0, 0, time.UTC)
	legacy := ipQualitySyncStandaloneReport(prefix, "sync_legacy_"+prefix, "agent/legacy", legacyObserved, legacyObserved.Add(time.Second))
	legacy.RawJSON = json.RawMessage(`{"legacy":"keep-me","token":"secret-token"}`)
	legacy.RiskLevel = "high"
	if err := repo.SaveReports(ctx, []ipquality.ReportWrite{legacy}); err != nil {
		t.Fatal(err)
	}
	legacyID, legacyRaw := readIPQualityProductStoredRaw(t, ctx, fixture, "mi_"+prefix, legacy.SyncBatchID)
	if count := countIPQualityIntelligence(t, ctx, fixture, legacyID); count != 0 {
		t.Fatalf("legacy intelligence jobs = %d, want 0", count)
	}

	backfillObserved := legacyObserved.Add(time.Hour)
	backfill := ipQualityProductIntegrationReport(prefix, backfillObserved, backfillObserved.Add(time.Second), true)
	backfill.RiskLevel = "high"
	backfill.SyncBatchID = "sync_backfill_" + prefix
	liveObserved := backfillObserved.Add(time.Hour)
	live := ipQualityProductIntegrationReport(prefix, liveObserved, liveObserved.Add(time.Second), false)
	live.RiskLevel = "low"
	live.SyncBatchID = "sync_live_" + prefix
	placeholder := ipQualityProductIntegrationReport(prefix, liveObserved.Add(time.Minute), liveObserved.Add(2*time.Minute), false)
	placeholder.IPAddress = "0.0.0.0"
	placeholder.Status = agentapi.IPQualityStatusFailure
	placeholder.ErrorCode = "lookup_failed"
	placeholder.SyncBatchID = "sync_placeholder_" + prefix
	placeholder.ProviderResults[0].Details.QueriedIP = "0.0.0.0"
	if err := repo.SaveReports(ctx, []ipquality.ReportWrite{backfill, live, placeholder}); err != nil {
		t.Fatal(err)
	}
	if raw := readIPQualityProductStoredRawByID(t, ctx, fixture, legacyID); raw != legacyRaw {
		t.Fatalf("legacy raw JSON changed after product writes: got %s want %s", raw, legacyRaw)
	}
	if productNull, summaryNull := readIPQualityProductColumnNulls(t, ctx, fixture, legacyID); !productNull || !summaryNull {
		t.Fatalf("legacy product columns rewritten: product null=%t summary null=%t", productNull, summaryNull)
	}
	backfillID := readIPQualityProductReportID(t, ctx, fixture, "mi_"+prefix, backfill.SyncBatchID)
	liveID := readIPQualityProductReportID(t, ctx, fixture, "mi_"+prefix, live.SyncBatchID)
	placeholderID := readIPQualityProductReportID(t, ctx, fixture, "mi_"+prefix, placeholder.SyncBatchID)
	if countIPQualityIntelligence(t, ctx, fixture, backfillID) != 0 || countIPQualityIntelligence(t, ctx, fixture, placeholderID) != 0 {
		t.Fatal("backfill or placeholder created an intelligence job")
	}
	if productNull, summaryNull := readIPQualityProductColumnNulls(t, ctx, fixture, placeholderID); productNull || summaryNull {
		t.Fatal("placeholder revision 1 did not store its agent product summary")
	}
	if countIPQualityIntelligence(t, ctx, fixture, liveID) != 1 || !ipQualityIntelligenceDeadlineMatches(t, ctx, fixture, liveID) {
		t.Fatal("live revision 1 did not enqueue one pending job at received_at+10m")
	}

	page, err := repo.GetVPSIPQuality(ctx, "vps_"+prefix, 4)
	if err != nil {
		t.Fatal(err)
	}
	wantAgent := ipquality.BuildProductView(live, nil).Summary
	if page.Summary == nil || page.LatestReport == nil || page.LatestReport.ReportID != liveID || page.Product == nil {
		t.Fatalf("page = %#v, want live product report", page)
	}
	if page.Summary.ProductRevision != 1 || page.Summary.IntelligenceState != "pending" || page.Summary.ProductSummary == nil || !reflect.DeepEqual(*page.Summary.ProductSummary, wantAgent) {
		t.Fatalf("pending summary = %#v, want agent-only %#v", page.Summary.ProductSummary, wantAgent)
	}
	if page.Summary.RiskLevel != "low" || page.Product.State != "pending" || page.Product.CompletedAt != nil || len(page.Product.PTRNames) != 1 || page.Product.PTRNames[0] != "ptr.example.net" {
		t.Fatalf("pending view = %#v, want stored PTR and no terminal time", page.Product)
	}
	if page.LatestReport.Product == nil || !reflect.DeepEqual(page.LatestReport.Product.PTRNames, page.Product.PTRNames) {
		t.Fatalf("report product = %#v, want stored PTR", page.LatestReport.Product)
	}
	if len(page.ProviderResults) != 1 || page.ProviderResults[0].Details == nil || page.ProviderResults[0].Details.Method != "agent_public" || page.ProviderResults[0].Details.QueriedIP != live.IPAddress {
		t.Fatalf("provider details = %#v, want anonymous queried exit", page.ProviderResults)
	}
	if len(page.ServiceUnlocks) != 1 || len(page.ServiceUnlocks[0].Checks) != 1 || page.ServiceUnlocks[0].Checks[0].ID != "title_81280792" {
		t.Fatalf("service checks = %#v, want registered title check", page.ServiceUnlocks)
	}
	if len(page.History) < 2 || page.History[0].ReportID != liveID || page.History[0].ProductRevision != 1 || page.Families[0].LatestReportSummary == nil || page.Families[0].LatestReportSummary.IntelligenceState != "pending" {
		t.Fatalf("history/family = %#v %#v, want shared pending summary", page.History, page.Families)
	}
	emptyFamily, err := repo.GetVPSIPQuality(ctx, "vps_"+prefix, 6)
	if err != nil || emptyFamily.Summary != nil || emptyFamily.Product != nil || emptyFamily.Families[0].LatestReportSummary == nil || emptyFamily.Families[0].LatestReportSummary.ReportID != liveID {
		t.Fatalf("IPv6 selection borrowed or dropped IPv4 product: %#v %v", emptyFamily, err)
	}

	detail, err := repo.GetVPSIPQualityReportDetail(ctx, "vps_"+prefix, backfillID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Summary == nil || detail.Summary.ProductRevision != 1 || detail.Summary.IntelligenceState != "skipped" || detail.Summary.ProductSummary == nil || detail.Summary.RiskLevel != "high" || len(detail.Summary.ProductSummary.RiskFlags) != 1 || detail.Summary.ProductSummary.RiskFlags[0] != "vpn" {
		t.Fatalf("backfill detail = %#v, want skipped agent summary without old-rating fallback", detail.Summary)
	}
	legacyDetail, err := repo.GetVPSIPQualityReportDetail(ctx, "vps_"+prefix, legacyID)
	if err != nil || legacyDetail.Product != nil || legacyDetail.Summary == nil || legacyDetail.Summary.ProductRevision != 0 || legacyDetail.Summary.ProductSummary != nil || legacyDetail.Summary.RiskLevel != "high" {
		t.Fatalf("legacy detail = %#v product=%#v err=%v, want revision 0", legacyDetail.Summary, legacyDetail.Product, err)
	}

	latest, err := repo.ListLatestSummariesForVPS(ctx, []string{"vps_" + prefix})
	if err != nil {
		t.Fatal(err)
	}
	if latest["vps_"+prefix].ReportID != liveID || latest["vps_"+prefix].ProductRevision != 1 || latest["vps_"+prefix].IntelligenceState != "pending" || !reflect.DeepEqual(latest["vps_"+prefix].ProductSummary, page.Summary.ProductSummary) {
		t.Fatalf("batch summary = %#v, want same pending facts", latest["vps_"+prefix])
	}
	overview, err := repo.GetLatestVPSIPQualitySummary(ctx, "vps_"+prefix)
	if err != nil || overview == nil || overview.ProductRevision != 1 || overview.IntelligenceState != "pending" || !reflect.DeepEqual(overview.ProductSummary, page.Summary.ProductSummary) {
		t.Fatalf("overview summary = %#v err=%v, want same pending facts", overview, err)
	}
	tx, err := runtimePool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	views, err := LoadIPQualityProducts(ctx, tx, []string{liveID, legacyID, backfillID})
	_ = tx.Rollback(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := views[legacyID]; ok || views[liveID].State != "pending" || views[backfillID].State != "skipped" {
		t.Fatalf("LoadIPQualityProducts = %#v, want legacy omitted and stored states", views)
	}

	completed := live.ReceivedAt.Add(time.Minute)
	terminal := agentapi.IPQualityProductSummary{
		RiskFlags: []string{"abuse"}, HighRiskSources: []string{"abuseipdb"}, RestrictedServices: []string{"netflix"},
		BlacklistListedCount: 1, BlacklistCheckedCount: 1, ServiceRestrictedCount: 1, GeoStatus: "consistent",
	}
	snapshot := ipquality.IntelligenceSnapshot{
		Providers:      []ipquality.ProviderResultRead{{Provider: "abuseipdb", Status: "success"}},
		Blacklists:     []agentapi.IPQualityBlacklistResult{{Source: "spamcop", Status: "listed", Codes: []string{"127.0.0.2"}}},
		Registration:   agentapi.IPQualityRegistration{Status: "success", NetworkCountryCode: "US"},
		ASN:            agentapi.IPQualityASN{Status: "success", Origins: []string{"AS64500"}},
		SourceOutcomes: []ipquality.SourceOutcome{{SourceID: "ripe", Status: "success", ObservedAt: completed}},
	}
	writeIPQualityTerminalSnapshot(t, ctx, fixture, liveID, completed, snapshot, terminal)
	if raw := readIPQualityProductStoredRawByID(t, ctx, fixture, legacyID); raw != legacyRaw {
		t.Fatal("terminal snapshot rewrote legacy raw JSON")
	}
	completedPage, err := repo.GetVPSIPQuality(ctx, "vps_"+prefix, 4)
	if err != nil {
		t.Fatal(err)
	}
	if completedPage.Product == nil || completedPage.Product.State != "complete" || completedPage.Product.CompletedAt == nil || !completedPage.Product.CompletedAt.Equal(completed) {
		t.Fatalf("completed product = %#v, want stored completion", completedPage.Product)
	}
	wantTerminal := normalizeIPQualityProductSummary(terminal)
	if completedPage.Summary == nil || completedPage.Summary.IntelligenceState != "complete" || completedPage.Summary.ProductSummary == nil || !reflect.DeepEqual(*completedPage.Summary.ProductSummary, wantTerminal) || reflect.DeepEqual(*completedPage.Summary.ProductSummary, wantAgent) {
		t.Fatalf("completed summary = %#v, want stored terminal summary", completedPage.Summary.ProductSummary)
	}
	if len(completedPage.Product.Blacklists) != 1 || completedPage.Product.Blacklists[0].Status != "listed" || len(completedPage.Product.PTRNames) != 1 || completedPage.Product.PTRNames[0] != "ptr.example.net" {
		t.Fatalf("completed view = %#v, want snapshot facts and historical PTR", completedPage.Product)
	}
	completedOverview, err := repo.GetLatestVPSIPQualitySummary(ctx, "vps_"+prefix)
	if err != nil || completedOverview == nil || !reflect.DeepEqual(completedOverview.ProductSummary, completedPage.Summary.ProductSummary) || completedOverview.IntelligenceState != "complete" {
		t.Fatalf("completed overview = %#v err=%v, want the same stored summary", completedOverview, err)
	}
}

func assertIPQualityProductSyncReplay(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture) {
	t.Helper()
	const prefix = "ipq_product_replay"
	seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "ip-quality-product-replay", 2)
	repository := NewPostgresSyncRepository(runtimePool)
	repository.receptionFault = nil
	receivedAt := time.Date(2026, time.June, 2, 9, 0, 0, 0, time.UTC)
	repository.now = func() time.Time { return receivedAt }
	batch, _ := syncInterleavingBatches(prefix)
	report := ipQualityProductIntegrationReport(prefix, receivedAt.Add(-time.Minute), receivedAt, false)
	report.SyncBatchID = batch.Heartbeats[0].SyncBatchID
	batch.IPQualityReports = []ipquality.ReportWrite{report}
	first, err := repository.ApplyBatch(ctx, prepareSyncFixture(batch))
	if err != nil || first.Disposition != syncing.ResultDispositionRecorded {
		t.Fatalf("first sync = %#v %v, want recorded", first, err)
	}
	replay, err := repository.ApplyBatch(ctx, prepareSyncFixture(batch))
	if err != nil || replay.Disposition != syncing.ResultDispositionExactDuplicate {
		t.Fatalf("replay = %#v %v, want exact duplicate", replay, err)
	}
	var reports, jobs int
	if err := fixture.db.QueryRow(ctx, `
		select
			(select count(*)::int from public.ip_quality_reports where monitoring_instance_id = $1 and sync_batch_id = $2),
			(select count(*)::int from public.ip_quality_intelligence i
				join public.ip_quality_reports r on r.report_id = i.report_id
				where r.monitoring_instance_id = $1 and r.sync_batch_id = $2)`,
		"mi_"+prefix, report.SyncBatchID).Scan(&reports, &jobs); err != nil {
		t.Fatal(err)
	}
	if reports != 1 || jobs != 1 {
		t.Fatalf("replay reports/jobs = %d/%d, want 1/1", reports, jobs)
	}
}

func assertIPQualityProductRollback(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture) {
	t.Helper()
	const prefix = "ipq_product_rollback"
	seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "ip-quality-product-rollback", 2)
	repo := NewPostgresIPQualityRepository(runtimePool)
	observed := time.Date(2026, time.June, 3, 9, 0, 0, 0, time.UTC)
	good := ipQualityProductIntegrationReport(prefix, observed, observed.Add(time.Second), false)
	bad := good
	bad.SyncBatchID = good.SyncBatchID + "_bad"
	bad.ObservedAt = observed.Add(time.Minute)
	bad.ServiceUnlocks = append(bad.ServiceUnlocks, bad.ServiceUnlocks[0])
	err := repo.SaveReports(ctx, []ipquality.ReportWrite{good, bad})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("duplicate service SaveReports() = %v, want unique violation", err)
	}
	if reports, jobs := countIPQualityInstanceProductRows(t, ctx, fixture, "mi_"+prefix); reports != 0 || jobs != 0 {
		t.Fatalf("rows after SaveReports rollback = %d reports/%d jobs, want 0/0", reports, jobs)
	}

	tx, err := runtimePool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	direct := good
	direct.SyncBatchID = good.SyncBatchID + "_direct"
	if err := writeIPQualityReport(ctx, tx, "ipq_product_direct_rollback", direct, nil); err != nil {
		t.Fatal(err)
	}
	var visible int
	if err := tx.QueryRow(ctx, `select count(*)::int from public.ip_quality_intelligence where report_id = 'ipq_product_direct_rollback'`).Scan(&visible); err != nil || visible != 1 {
		t.Fatalf("in-transaction job count = %d err=%v, want 1", visible, err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if reports, jobs := countIPQualityInstanceProductRows(t, ctx, fixture, "mi_"+prefix); reports != 0 || jobs != 0 {
		t.Fatalf("rows after direct rollback = %d reports/%d jobs, want 0/0", reports, jobs)
	}
}

func TestPostgresIntegrationIPQualityProductServiceSourceSummaries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "ip-quality-service-source-summary", 4)
	repository := NewPostgresIPQualityRepository(pool)
	settingsRepository := NewPostgresSettingsRepository(pool, centersettings.DeploymentDefaults{})
	if _, err := settingsRepository.MutateSettings(ctx, func(settings centersettings.CenterSettings) (centersettings.CenterSettings, error) {
		settings.IPQuality.Enabled = true
		return settings, nil
	}); err != nil {
		t.Fatal(err)
	}
	sources := []ipquality.ServiceUnlockWrite{
		{
			Service: "netflix", Source: "z_catalog", Status: "unlocked", ProbeStatus: "success",
			Checks: []agentapi.IPQualityServiceCheck{{ID: "title_81280792", Status: "available"}},
		},
		{Service: "netflix", Source: "a_policy", Status: "blocked", ProbeStatus: "success"},
		{Service: "netflix", Source: "m_timeout", Status: "unknown", ProbeStatus: "failure", ErrorCode: "request_timeout"},
		{
			Service: "youtube-premium", Source: "premium_policy", Status: "blocked", ProbeStatus: "success",
			Checks: []agentapi.IPQualityServiceCheck{{ID: "premium", Status: "restricted"}},
		},
		{Service: "chatgpt", Source: "cookie", Status: "unlocked", ProbeStatus: "success"},
		{Service: "chatgpt", Source: "mobile", Status: "unknown", ProbeStatus: "success"},
		{Service: "disney-plus", Source: "web", Status: "unknown", ProbeStatus: "success"},
	}
	var want *agentapi.IPQualityProductSummary
	for i, order := range [][3]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
		prefix := "ipq_sources_" + string(rune('a'+i))
		seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
		now := recordEvidenceParticipantDatabaseNow(t, ctx, fixture)
		report := ipQualityProductIntegrationReport(prefix, now.Add(-time.Second), now, false)
		report.ServiceUnlocks = []ipquality.ServiceUnlockWrite{sources[order[0]], sources[order[1]], sources[order[2]]}
		report.ServiceUnlocks = append(report.ServiceUnlocks, sources[3:]...)
		if err := repository.SaveReports(ctx, []ipquality.ReportWrite{report}); err != nil {
			t.Fatalf("order %v SaveReports: %v", order, err)
		}
		reportID := readIPQualityProductReportID(t, ctx, fixture, report.MonitoringInstanceID, report.SyncBatchID)
		assertConsumers := func(state string) {
			t.Helper()
			page, err := repository.GetVPSIPQuality(ctx, "vps_"+prefix, 4)
			if err != nil {
				t.Fatal(err)
			}
			if page.Summary == nil || page.Summary.ProductSummary == nil || page.Summary.IntelligenceState != state ||
				page.Product == nil || page.Product.State != state {
				t.Fatalf("order %v %s page lacks product summary: %#v", order, state, page)
			}
			summary := page.Summary.ProductSummary
			if summary.ServiceAvailableCount != 1 || summary.ServicePartialCount != 1 ||
				summary.ServiceRestrictedCount != 1 || summary.ServiceUnknownCount != 1 ||
				!reflect.DeepEqual(summary.RestrictedServices, []string{"netflix", "youtube-premium"}) {
				t.Fatalf("order %v %s lost grouped service facts: %+v", order, state, summary)
			}
			if want == nil {
				copy := *summary
				want = &copy
			}
			if !reflect.DeepEqual(summary, want) || !reflect.DeepEqual(page.Product.Summary, *want) {
				t.Fatalf("order %v %s differs from initial facts: summary=%+v product=%+v want=%+v", order, state, summary, page.Product.Summary, want)
			}
			if len(page.ServiceUnlocks) != len(sources) {
				t.Fatalf("order %v %s dropped raw sources: %+v", order, state, page.ServiceUnlocks)
			}
			remaining := make(map[string]ipquality.ServiceUnlockWrite, len(sources))
			for _, source := range sources {
				remaining[source.Service+"|"+source.Source] = source
			}
			for _, source := range page.ServiceUnlocks {
				key := source.Service + "|" + source.Source
				expected, ok := remaining[key]
				if !ok || source.Status != expected.Status || source.ProbeStatus != expected.ProbeStatus ||
					len(source.Checks) != len(expected.Checks) || (len(source.Checks) > 0 && !reflect.DeepEqual(source.Checks, expected.Checks)) {
					t.Fatalf("order %v %s rewrote raw source %s: %+v want %+v", order, state, key, source, expected)
				}
				delete(remaining, key)
			}
			if len(remaining) != 0 {
				t.Fatalf("order %v %s missing source evidence: %+v", order, state, remaining)
			}
			overview, err := repository.GetLatestVPSIPQualitySummary(ctx, "vps_"+prefix)
			if err != nil || overview == nil || overview.IntelligenceState != state || !reflect.DeepEqual(overview.ProductSummary, want) {
				t.Fatalf("order %v %s overview=%+v err=%v", order, state, overview, err)
			}
			latest, err := repository.ListLatestSummariesForVPS(ctx, []string{"vps_" + prefix})
			if err != nil || !reflect.DeepEqual(latest["vps_"+prefix].ProductSummary, want) {
				t.Fatalf("order %v %s latest=%+v err=%v", order, state, latest, err)
			}
			detail, err := repository.GetVPSIPQualityReportDetail(ctx, "vps_"+prefix, reportID)
			if err != nil || detail.Summary == nil || !reflect.DeepEqual(detail.Summary.ProductSummary, want) {
				t.Fatalf("order %v %s detail=%+v err=%v", order, state, detail, err)
			}
			if len(page.History) != 1 || !reflect.DeepEqual(page.History[0].ProductSummary, want) ||
				len(page.Families) == 0 || page.Families[0].LatestReportSummary == nil ||
				!reflect.DeepEqual(page.Families[0].LatestReportSummary.ProductSummary, want) {
				t.Fatalf("order %v %s history/family summaries diverged: %+v / %+v", order, state, page.History, page.Families)
			}
		}
		assertConsumers("pending")
		claim, err := repository.ClaimIPQualityIntelligence(ctx, "service-source-summary-worker")
		if err != nil || claim == nil || claim.ReportID != reportID {
			t.Fatalf("order %v claim=%+v err=%v", order, claim, err)
		}
		result := ipquality.IntelligenceSnapshot{
			State:        "complete",
			Registration: agentapi.IPQualityRegistration{Status: "unknown"},
			ASN:          agentapi.IPQualityASN{Status: "unknown", Origins: []string{}},
			SourceOutcomes: []ipquality.SourceOutcome{
				{SourceID: "ripe-ris", Status: "success", ObservedAt: now},
			},
		}
		if err := repository.CompleteIPQualityIntelligence(ctx, *claim, result); err != nil {
			t.Fatalf("order %v finalize: %v", order, err)
		}
		assertConsumers("complete")
		var initialJSON, finalJSON []byte
		if err := pool.QueryRow(ctx, `
			select report.product_summary_json, intelligence.summary_json
			from ip_quality_reports report
			join ip_quality_intelligence intelligence on intelligence.report_id = report.report_id
			where report.report_id = $1`, reportID).Scan(&initialJSON, &finalJSON); err != nil {
			t.Fatal(err)
		}
		for _, raw := range [][]byte{initialJSON, finalJSON} {
			var persisted agentapi.IPQualityProductSummary
			if err := json.Unmarshal(raw, &persisted); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(persisted, *want) {
				t.Fatalf("order %v persisted initial/final facts differ: got=%+v want=%+v", order, persisted, want)
			}
		}
	}
}

func ipQualityProductIntegrationReport(prefix string, observed, received time.Time, backfilled bool) ipquality.ReportWrite {
	report := ipQualityRevision1ReportWrite()
	report.MonitoringInstanceID = "mi_" + prefix
	report.Fingerprint = "fp_" + prefix
	report.AgentVersion = "agent/product-v1"
	report.SyncBatchID = "sync_product_" + prefix
	report.ObservedAt = observed
	report.ReceivedAt = received
	report.IsBackfilled = backfilled
	report.IPAddress = syncInterleavingIPAddress(prefix)
	report.Product.Mail[0].ObservedAt = observed
	report.ProviderResults[0].Details.QueriedIP = report.IPAddress
	report.ProviderResults[0].Details.ObservedAt = observed
	return report
}

func readIPQualityProductStoredRaw(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, monitoringInstanceID, syncBatchID string) (string, string) {
	t.Helper()
	var reportID, raw string
	if err := fixture.db.QueryRow(ctx, `
		select report_id, raw_json::text
		from public.ip_quality_reports
		where monitoring_instance_id = $1 and sync_batch_id = $2`, monitoringInstanceID, syncBatchID).Scan(&reportID, &raw); err != nil {
		t.Fatal(err)
	}
	return reportID, raw
}

func readIPQualityProductStoredRawByID(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, reportID string) string {
	t.Helper()
	var raw string
	if err := fixture.db.QueryRow(ctx, `select raw_json::text from public.ip_quality_reports where report_id = $1`, reportID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func readIPQualityProductColumnNulls(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, reportID string) (bool, bool) {
	t.Helper()
	var productNull, summaryNull bool
	if err := fixture.db.QueryRow(ctx, `
		select product_json is null, product_summary_json is null
		from public.ip_quality_reports where report_id = $1`, reportID).Scan(&productNull, &summaryNull); err != nil {
		t.Fatal(err)
	}
	return productNull, summaryNull
}

func readIPQualityProductReportID(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, monitoringInstanceID, syncBatchID string) string {
	t.Helper()
	reportID, _ := readIPQualityProductStoredRaw(t, ctx, fixture, monitoringInstanceID, syncBatchID)
	return reportID
}

func countIPQualityIntelligence(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, reportID string) int {
	t.Helper()
	var count int
	if err := fixture.db.QueryRow(ctx, `select count(*)::int from public.ip_quality_intelligence where report_id = $1`, reportID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func ipQualityIntelligenceDeadlineMatches(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, reportID string) bool {
	t.Helper()
	var matches bool
	if err := fixture.db.QueryRow(ctx, `
		select state = 'pending'
			and owner_id is null
			and owner_generation = 0
			and attempt_count = 0
			and deadline_at = received_at + interval '10 minutes'
		from public.ip_quality_intelligence i
		join public.ip_quality_reports r on r.report_id = i.report_id
		where i.report_id = $1`, reportID).Scan(&matches); err != nil {
		t.Fatal(err)
	}
	return matches
}

func countIPQualityInstanceProductRows(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, monitoringInstanceID string) (int, int) {
	t.Helper()
	var reports, jobs int
	if err := fixture.db.QueryRow(ctx, `
		select
			(select count(*)::int from public.ip_quality_reports where monitoring_instance_id = $1),
			(select count(*)::int from public.ip_quality_intelligence i
				join public.ip_quality_reports r on r.report_id = i.report_id
				where r.monitoring_instance_id = $1)`, monitoringInstanceID).Scan(&reports, &jobs); err != nil {
		t.Fatal(err)
	}
	return reports, jobs
}

func writeIPQualityTerminalSnapshot(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, reportID string, completed time.Time, snapshot ipquality.IntelligenceSnapshot, summary agentapi.IPQualityProductSummary) {
	t.Helper()
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	summaryJSON, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	tag, err := fixture.db.Exec(ctx, `
		update public.ip_quality_intelligence
		set state = 'complete', completed_at = $2, data_json = $3::jsonb, summary_json = $4::jsonb
		where report_id = $1 and state = 'pending'`, reportID, completed, snapshotJSON, summaryJSON)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("seed terminal snapshot rows=%d err=%v", tag.RowsAffected(), err)
	}
}
