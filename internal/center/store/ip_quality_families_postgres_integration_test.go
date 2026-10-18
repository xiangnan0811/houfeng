package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/db/migrations"
	"houfeng/internal/center/evidence"
	"houfeng/internal/center/ipquality"
	"houfeng/internal/center/monitoringinstances"
	storemigrate "houfeng/internal/center/store/migrate"
	"houfeng/internal/center/syncing"
	"houfeng/internal/contracts/agentapi"
)

func pairedIPQualityTestReport(prefix, batchID, collectionID, requestID string, version int, observedAt time.Time, availability, status string) ipquality.ReportWrite {
	report := ipQualitySyncFullReport(prefix, batchID, observedAt, observedAt.Add(time.Second), requestID)
	report.IPVersion = version
	report.IPAddress = "198.51.100.10"
	report.Status = status
	report.ErrorCode = ""
	report.ErrorSummary = ""
	report.AgentVersion = fmt.Sprintf("agent/family-%d", version)
	report.ASN = fmt.Sprintf("AS%d", 64500+version)
	report.Organization = fmt.Sprintf("Family %d Transit", version)
	report.UseRegionCode = "US"
	report.RiskLevel = "low"
	if version == 6 {
		report.IPAddress = "2001:db8::10"
		report.UseRegionCode = "JP"
		report.RiskLevel = "high"
		report.ProviderResults[0].RiskLevel = "high"
		report.ProviderResults[0].IsProxy = new(true)
		report.ServiceUnlocks[0].Status = "blocked"
	}
	for i := range report.ProviderResults {
		report.ProviderResults[i].RegionCode = report.UseRegionCode
	}
	for i := range report.ServiceUnlocks {
		report.ServiceUnlocks[i].Region = report.UseRegionCode
	}
	if status == "failure" {
		report.IPAddress = "0.0.0.0"
		if version == 6 {
			report.IPAddress = "::"
		}
		report.ErrorCode = "lookup_failed"
		if availability == "unavailable" {
			report.ErrorCode = "address_family_unavailable"
		}
		report.ProviderResults = nil
		report.ServiceUnlocks = nil
		report.ASN = ""
		report.Organization = ""
		report.UseRegionCode = ""
		report.RiskLevel = ""
	}
	report.DiagnosticsJSON = json.RawMessage(fmt.Sprintf(`{"collection_revision":4,"collection_id":%q,"expected_ip_versions":[4,6],"ip_address_family":%d,"family_availability":%q,"collect_request_id":%q}`, collectionID, version, availability, requestID))
	report.CollectionMetadata = agentapi.ParseCollectionMetadata(report.DiagnosticsJSON)
	return report
}

// Invoked by the mandatory sync round-trip anchor, not a separately skippable test.
func assertIPQualityFamilySyncReadModel(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, pool *pgxpool.Pool) {
	t.Helper()
	for index, tc := range []struct {
		name                         string
		availability4, availability6 string
		status4, status6             string
		wantDefault                  int
		wantAggregate                string
	}{
		{"dual stack distinct facts", "available", "available", "success", "success", 4, "success"},
		{"IPv4 only", "available", "unavailable", "success", "failure", 4, "success"},
		{"IPv6 only", "unavailable", "available", "failure", "success", 6, "success"},
		{"IPv4 lookup failed IPv6 succeeded", "available", "available", "failure", "success", 4, "partial"},
		{"enumeration unknown and lookup failures", "unknown", "unknown", "failure", "failure", 4, "failure"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prefix := fmt.Sprintf("ipq_family_roundtrip_%d", index)
			seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
			repository := NewPostgresSyncRepository(pool)
			repository.receptionFault = nil
			requestID := "ipqc_" + prefix
			observedAt := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
			requests := ipquality.NewCollectRequests(func() (string, error) { return requestID, nil })
			if _, err := requests.Request("mi_"+prefix, observedAt.Add(-time.Minute)); err != nil {
				t.Fatal(err)
			}
			service := syncing.NewService(repository).WithIPQualityCollectCoordinator(requests)
			batch, _ := syncInterleavingBatches(prefix)
			batch.LiveSignal = nil
			batch.Heartbeats[0].ObservedAt = observedAt
			batch.Observations.HostSamples = nil
			batch.Observations.ProbeObservations = nil
			batch.IPQualityReports = []ipquality.ReportWrite{
				pairedIPQualityTestReport(prefix, "sync_live_"+prefix, prefix, requestID, 4, observedAt, tc.availability4, tc.status4),
				pairedIPQualityTestReport(prefix, "sync_live_"+prefix, prefix, requestID, 6, observedAt, tc.availability6, tc.status6),
			}
			repository.now = func() time.Time { return observedAt.Add(time.Second) }
			result, err := service.SyncBatch(ctx, prepareSyncFixture(batch))
			if err != nil || result.Disposition != syncing.ResultDispositionRecorded {
				t.Fatalf("sync=%#v err=%v", result, err)
			}
			completed, ok := requests.Latest("mi_"+prefix, time.Now().UTC())
			if !ok || completed.Status != ipquality.CollectRequestCompleted || completed.ReportStatus != tc.wantAggregate || completed.CollectionID != prefix || len(completed.FamilyResults) != 2 {
				t.Fatalf("paired completion=%#v, want aggregate %s", completed, tc.wantAggregate)
			}
			read := NewPostgresIPQualityRepository(pool)
			defaultReport, err := read.GetVPSIPQuality(ctx, "vps_"+prefix, 0)
			if err != nil {
				t.Fatal(err)
			}
			if defaultReport.DefaultIPVersion != tc.wantDefault || defaultReport.SelectedIPVersion != tc.wantDefault {
				t.Fatalf("default=%#v", defaultReport)
			}
			for familyIndex, version := range []int{4, 6} {
				selected, err := read.GetVPSIPQuality(ctx, "vps_"+prefix, version)
				if err != nil {
					t.Fatal(err)
				}
				status, availability := tc.status4, tc.availability4
				if version == 6 {
					status, availability = tc.status6, tc.availability6
				}
				family := selected.Families[familyIndex]
				if selected.SelectedIPVersion != version || family.IPVersion != version || family.Availability != availability || family.LatestAttempt == nil || family.LatestAttempt.ReportStatus != status || family.LatestAttempt.CollectionScope != "dual_stack" || family.LatestAttempt.CollectionID != prefix {
					t.Fatalf("selected family=%#v report=%#v", family, selected)
				}
				if status == "failure" {
					assertIPQualitySnapshotEmpty(t, selected)
					if family.LatestReportSummary != nil {
						t.Fatal("placeholder acquired valid report summary")
					}
					continue
				}
				if selected.LatestReport == nil || selected.Summary == nil || selected.LatestReport.IPVersion != version || selected.Summary.IPVersion != version || len(selected.History) != 1 || selected.History[0].IPVersion != version || len(selected.ProviderResults) != 3 || len(selected.ServiceUnlocks) != 2 {
					t.Fatalf("family facts mixed or lost: %#v", selected)
				}
				wantRegion := "US"
				if version == 6 {
					wantRegion = "JP"
				}
				if selected.LatestReport.UseRegionCode != wantRegion || selected.ProviderResults[0].RegionCode != wantRegion || selected.ServiceUnlocks[0].Region != wantRegion {
					t.Fatalf("family regions mixed: %#v", selected)
				}
				wantUnlock, wantRisk := "unlocked", "low"
				if version == 6 {
					wantUnlock, wantRisk = "blocked", "high"
				}
				if selected.Summary.RiskLevel != wantRisk {
					t.Fatalf("family risk crossed reports: %#v", selected.Summary)
				}
				for _, unlock := range selected.ServiceUnlocks {
					if unlock.Source == "netflix_title_probe" && unlock.Status != wantUnlock {
						t.Fatalf("family %d service crossed reports: %#v", version, unlock)
					}
				}
				historical, err := read.GetVPSIPQualityReportDetail(ctx, "vps_"+prefix, selected.LatestReport.ReportID)
				if err != nil || historical.SelectedIPVersion != version || historical.Summary.ReportID != selected.Summary.ReportID || historical.LatestReport.IPVersion != version {
					t.Fatalf("historical identity=%#v err=%v", historical, err)
				}
				if len(historical.History) != 1 || historical.History[0].ReportID != selected.LatestReport.ReportID || historical.History[0].IPVersion != version {
					t.Fatalf("historical detail lost or mixed family history: %#v", historical.History)
				}
			}
			latest, err := read.ListLatestSummariesForVPS(ctx, []string{"vps_" + prefix})
			if err != nil {
				t.Fatal(err)
			}
			defaultStatus := tc.status4
			if tc.wantDefault == 6 {
				defaultStatus = tc.status6
			}
			if defaultStatus == "failure" {
				if len(latest) != 0 {
					t.Fatalf("default failure fell back to other family: %#v", latest)
				}
			} else if latest["vps_"+prefix].IPVersion != tc.wantDefault {
				t.Fatalf("latest batch summary=%#v", latest)
			}
			if stored := readIPQualitySyncStoredReports(t, ctx, fixture, "mi_"+prefix, "sync_live_"+prefix); len(stored) != 2 {
				t.Fatalf("paired atomic rows=%#v", stored)
			}
			// The asset-decision consumer must use the same default report ID
			// and its child rollups, never the newest report of either family.
			facts, err := NewPostgresAssetDecisionRepository(pool).loadFacts(ctx)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, fact := range facts {
				if fact.VPS.VPSID != "vps_"+prefix {
					continue
				}
				found = true
				summary := fact.VPS.IPQualitySummary
				if defaultStatus == "failure" {
					if summary != nil || fact.IPQualityProviderRiskSignalCount != 0 || len(fact.IPQualityBlockedServices) != 0 {
						t.Fatalf("asset decisions borrowed other family: %#v", fact)
					}
				} else if summary == nil || summary.IPVersion != tc.wantDefault || summary.IPAddress != defaultReport.Summary.IPAddress {
					t.Fatalf("asset default facts=%#v, want selected family %d", fact, tc.wantDefault)
				}
				if defaultStatus != "failure" {
					wantSignals := 0
					if tc.wantDefault == 6 {
						wantSignals = 1
					}
					if fact.IPQualityProviderRiskSignalCount != wantSignals || len(fact.IPQualityBlockedServices) != wantSignals {
						t.Fatalf("default child rollups mixed IPv4/IPv6: %#v", fact)
					}
					if wantSignals == 1 && fact.IPQualityBlockedServices[0] != "netflix:JP" {
						t.Fatalf("default service region borrowed other family: %#v", fact.IPQualityBlockedServices)
					}
				}
			}
			if !found {
				t.Fatal("family VPS missing from asset decision facts")
			}
			ev, err := read.LoadIPQualityEvidence(ctx, "vps_"+prefix, evidence.TimeWindow{Start: observedAt.Add(-time.Second), End: observedAt.Add(time.Second)})
			if tc.status4 == "success" {
				if err != nil || ev.IPVersion != 4 {
					t.Fatalf("window did not prefer valid IPv4: %#v err=%v", ev, err)
				}
			} else if tc.status6 == "success" {
				if err != nil || ev.IPVersion != 6 {
					t.Fatalf("IPv4 placeholder entered evidence or hid valid IPv6: %#v err=%v", ev, err)
				}
			} else if !errors.Is(err, evidence.ErrSourceEmpty) {
				t.Fatalf("failure placeholders became evidence: %#v err=%v", ev, err)
			}
			if index == 0 {
				var older []ipquality.ReportWrite
				for i := 1; i <= 35; i++ {
					for _, version := range []int{4, 6} {
						report := pairedIPQualityTestReport(prefix, "family_history_"+prefix, fmt.Sprintf("history_%d", i), "", version, observedAt.Add(-time.Duration(i)*time.Minute), "available", "success")
						report.ProviderResults = nil
						report.ServiceUnlocks = nil
						older = append(older, report)
					}
				}
				if err := read.SaveReports(ctx, older); err != nil {
					t.Fatal(err)
				}
				for _, version := range []int{4, 6} {
					got, err := read.GetVPSIPQuality(ctx, "vps_"+prefix, version)
					if err != nil || len(got.History) != 30 || !got.History[0].ObservedAt.Equal(observedAt) || !got.History[29].ObservedAt.Equal(observedAt.Add(-29*time.Minute)) {
						t.Fatalf("history family %d lost limit or stable time order: %#v err=%v", version, got.History, err)
					}
					for _, summary := range got.History {
						if summary.IPVersion != version {
							t.Fatalf("cross-family history: %#v", summary)
						}
					}
				}
			}
		})
	}
}

func assertIPQualityFamilyStaleAndHistoricalSelection(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, pool *pgxpool.Pool) {
	t.Helper()
	const prefix = "ipq_family_stale"
	seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
	repo := NewPostgresIPQualityRepository(pool)
	oldTime := time.Now().UTC().Add(-time.Hour)
	old4 := pairedIPQualityTestReport(prefix, "family_old_"+prefix, "old", "", 4, oldTime, "available", "success")
	old6 := pairedIPQualityTestReport(prefix, "family_old_"+prefix, "old", "", 6, oldTime.Add(time.Minute), "available", "success")
	if err := repo.SaveReports(ctx, []ipquality.ReportWrite{old4, old6}); err != nil {
		t.Fatal(err)
	}
	original, err := repo.GetVPSIPQuality(ctx, "vps_"+prefix, 4)
	if err != nil {
		t.Fatal(err)
	}
	unavailable := pairedIPQualityTestReport(prefix, "family_new_"+prefix, "new", "", 4, time.Now().UTC().Add(-time.Minute), "unavailable", "failure")
	if err := repo.SaveReports(ctx, []ipquality.ReportWrite{unavailable}); err != nil {
		t.Fatal(err)
	}
	fresh, err := repo.GetVPSIPQuality(ctx, "vps_"+prefix, 0)
	if err != nil || fresh.DefaultIPVersion != 6 {
		t.Fatalf("fresh unavailable default=%#v err=%v", fresh, err)
	}
	explicit, err := repo.GetVPSIPQuality(ctx, "vps_"+prefix, 4)
	if err != nil || explicit.LatestReport == nil || explicit.LatestReport.ReportID != original.LatestReport.ReportID || len(explicit.History) != 1 || explicit.Families[0].LatestAttempt.Stale {
		t.Fatalf("old valid IPv4 lost=%#v err=%v", explicit, err)
	}
	// Changing only the policy ages the same terminal attempt without rewriting history.
	if _, err := pool.Exec(ctx, `insert into center_settings (settings_id, ip_quality_settings) values ('center', '{"enabled":true,"stale_after_seconds":60}'::jsonb) on conflict (settings_id) do update set ip_quality_settings=excluded.ip_quality_settings`); err != nil {
		t.Fatal(err)
	}
	stale, err := repo.GetVPSIPQuality(ctx, "vps_"+prefix, 0)
	if err != nil || stale.DefaultIPVersion != 4 || !stale.Families[0].LatestAttempt.Stale || stale.LatestReport.ReportID != original.LatestReport.ReportID {
		t.Fatalf("stale unavailable remained excluded=%#v err=%v", stale, err)
	}
	historical, err := repo.GetVPSIPQualityReportDetail(ctx, "vps_"+prefix, fresh.LatestReport.ReportID)
	if err != nil || historical.SelectedIPVersion != 6 || historical.DefaultIPVersion != 4 || historical.LatestReport.ReportID != fresh.LatestReport.ReportID {
		t.Fatalf("history switched to default=%#v err=%v", historical, err)
	}
	// Evidence chooses within its own window: the newer IPv6 cannot replace IPv4,
	// and current unavailable metadata cannot erase the earlier valid IPv4.
	ev, err := repo.LoadIPQualityEvidence(ctx, "vps_"+prefix, evidence.TimeWindow{Start: oldTime.Add(-time.Minute), End: oldTime.Add(2 * time.Minute)})
	if err != nil || ev.IPVersion != 4 || ev.ReportID != original.LatestReport.ReportID {
		t.Fatalf("historical window evidence=%#v err=%v", ev, err)
	}
	ev6, err := repo.LoadIPQualityEvidence(ctx, "vps_"+prefix, evidence.TimeWindow{Start: oldTime.Add(30 * time.Second), End: oldTime.Add(2 * time.Minute)})
	if err != nil || ev6.IPVersion != 6 {
		t.Fatalf("IPv6-only window evidence=%#v err=%v", ev6, err)
	}
}

func assertIPQualityFamilySnapshot(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture) {
	t.Helper()
	const prefix = "ipq_family_snapshot"
	seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
	pool := fixture.openDirectRuntimePool(t, ctx, "ip-quality-family-snapshot-writer", 1)
	now := time.Now().UTC().Add(-time.Minute)
	old := pairedIPQualityTestReport(prefix, "family_snapshot_old", "old", "", 4, now, "available", "success")
	// This anchor shares a schema with the subsequent host-address fallback
	// cases. Keep its linked reports off those cases' .10/.11/.20 addresses.
	old.IPAddress = "198.51.100.240"
	if err := NewPostgresIPQualityRepository(pool).SaveReports(ctx, []ipquality.ReportWrite{old}); err != nil {
		t.Fatal(err)
	}
	before := runIPQualitySnapshotReadBarrier(t, ctx, fixture, "family-state", "vps_"+prefix,
		func(repo *PostgresIPQualityRepository) (ipquality.VPSReport, error) {
			return repo.GetVPSIPQuality(ctx, "vps_"+prefix, 0)
		},
		func(ctx context.Context, repo *PostgresIPQualityRepository, _ *pgxpool.Pool) error {
			new6 := pairedIPQualityTestReport(prefix, "family_snapshot_new", "new", "", 6, now.Add(30*time.Second), "available", "success")
			new6.IPAddress = "2001:db8::240"
			return repo.SaveReports(ctx, []ipquality.ReportWrite{
				pairedIPQualityTestReport(prefix, "family_snapshot_new", "new", "", 4, now.Add(30*time.Second), "unavailable", "failure"),
				new6,
			})
		})
	if before.DefaultIPVersion != 4 || before.SelectedIPVersion != 4 || before.Families[0].LatestAttempt.CollectionID != "old" || before.Families[1].LatestAttempt != nil || before.LatestReport == nil || before.LatestReport.IPVersion != 4 || before.LatestReport.IPAddress != old.IPAddress {
		t.Fatalf("family state mixed across snapshot=%#v", before)
	}
	after, err := NewPostgresIPQualityRepository(pool).GetVPSIPQuality(ctx, "vps_"+prefix, 0)
	if err != nil || after.DefaultIPVersion != 6 || after.Families[0].LatestAttempt.CollectionID != "new" || after.Families[1].LatestAttempt.CollectionID != "new" || after.LatestReport.IPVersion != 6 || after.LatestReport.IPAddress != "2001:db8::240" {
		t.Fatalf("later family snapshot=%#v err=%v", after, err)
	}
}

func assertIPQualityFamilyFallbackAndLinkIsolation(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, pool *pgxpool.Pool) {
	t.Helper()
	const prefix = "ipq_family_identity"
	seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
	repo := NewPostgresIPQualityRepository(pool)
	observedAt := time.Now().UTC().Add(-5 * time.Minute)
	valid := pairedIPQualityTestReport(prefix, "identity_valid", "valid", "", 4, observedAt, "available", "success")
	valid.IPAddress = syncInterleavingIPAddress(prefix)
	// Legacy rows retain unknown availability and legacy scope, even with valid facts.
	valid.DiagnosticsJSON = json.RawMessage(`{"source_version":"v2"}`)
	failure := pairedIPQualityTestReport(prefix, "identity_failure", "failure", "", 4, observedAt.Add(time.Minute), "unavailable", "failure")
	if err := repo.SaveReports(ctx, []ipquality.ReportWrite{valid, failure}); err != nil {
		t.Fatal(err)
	}
	linked, err := repo.GetVPSIPQuality(ctx, "vps_"+prefix, 4)
	if err != nil || linked.Summary == nil || linked.Families[0].LatestAttempt.CollectionID != "failure" {
		t.Fatalf("active link attempt=%#v err=%v", linked, err)
	}
	if _, err := pool.Exec(ctx, `update vps_monitoring_instance_links set unlinked_at=now() where vps_id=$1 and unlinked_at is null`, "vps_"+prefix); err != nil {
		t.Fatal(err)
	}
	fallback, err := repo.GetVPSIPQuality(ctx, "vps_"+prefix, 0)
	if err != nil || fallback.Summary == nil || fallback.Summary.ReportID != linked.Summary.ReportID || fallback.Summary.AssignmentMode != "ip_match" || fallback.Families[0].Availability != "unknown" || fallback.Families[0].LatestAttempt.CollectionScope != "legacy_single_family" || fallback.Families[0].LatestAttempt.ReportStatus != "success" {
		t.Fatalf("failure placeholder contaminated unlinked fallback=%#v err=%v", fallback, err)
	}
	if _, err := NewPostgresMonitoringInstanceRepository(pool).RetireMonitoringInstance(ctx, "mi_"+prefix, monitoringinstances.LifecycleActionInput{
		Reason: "replace family predecessor", IdempotencyKey: "retire-family-" + prefix,
	}); err != nil {
		t.Fatal(err)
	}
	seedIPQualityIdentityMonitoring(t, ctx, pool, "mi_family_replacement", "vps_"+prefix, "vnl_family_replacement")
	replacement, err := repo.GetVPSIPQuality(ctx, "vps_"+prefix, 4)
	if err != nil {
		t.Fatal(err)
	}
	assertIPQualitySnapshotEmpty(t, replacement)
	if replacement.Families[0].LatestAttempt != nil || replacement.Families[0].Availability != "unknown" {
		t.Fatalf("old instance attempt leaked through replacement link: %#v", replacement)
	}
	malformed := pairedIPQualityTestReport(prefix, "identity_malformed", "malformed", "", 4, observedAt.Add(2*time.Minute), "unavailable", "failure")
	malformed.MonitoringInstanceID = "mi_family_replacement"
	malformed.DiagnosticsJSON = json.RawMessage(`{"collection_revision":6,"collection_id":"unsupported","expected_ip_versions":[4,6],"ip_address_family":4,"family_availability":"unavailable"}`)
	if err := repo.SaveReports(ctx, []ipquality.ReportWrite{malformed}); err != nil {
		t.Fatal(err)
	}
	invalid, err := repo.GetVPSIPQuality(ctx, "vps_"+prefix, 4)
	if err != nil || invalid.Families[0].Availability != "unknown" || invalid.Families[0].LatestAttempt.CollectionScope != "dual_stack" || invalid.Families[0].LatestAttempt.CollectionID != "" {
		t.Fatalf("invalid paired attempt masqueraded valid/legacy: %#v err=%v", invalid, err)
	}
	placeholder := pairedIPQualityTestReport(prefix, "identity_placeholder_success", "placeholder", "", 6, observedAt.Add(3*time.Minute), "available", "success")
	placeholder.MonitoringInstanceID = "mi_family_replacement"
	placeholder.IPAddress = "::"
	if err := repo.SaveReports(ctx, []ipquality.ReportWrite{placeholder}); err != nil {
		t.Fatal(err)
	}
	empty6, err := repo.GetVPSIPQuality(ctx, "vps_"+prefix, 6)
	if err != nil {
		t.Fatal(err)
	}
	assertIPQualitySnapshotEmpty(t, empty6)
	if empty6.Families[1].LatestAttempt != nil {
		t.Fatalf("success placeholder became an attempt: %#v", empty6.Families[1])
	}

	// Ambiguity is checked after selection, not used to choose an older report.
	const ambiguityPrefix = "ipq_family_ambiguity"
	const ambiguityVPS = "vps_family_ambiguity"
	const ambiguityMI = "mi_family_ambiguity"
	seedIPQualitySnapshotIPMatchFixture(t, ctx, fixture, ambiguityPrefix, ambiguityVPS, ambiguityMI, "198.51.100.230")
	if _, err := pool.Exec(ctx, `update vps_assets set ipv6='2001:db8::230' where vps_id=$1`, ambiguityVPS); err != nil {
		t.Fatal(err)
	}
	old6 := pairedIPQualityTestReport(ambiguityPrefix, "ambiguity_old", "old6", "", 6, observedAt, "available", "success")
	old6.MonitoringInstanceID = ambiguityMI
	old6.IPAddress = "2001:db8::230"
	new4 := pairedIPQualityTestReport(ambiguityPrefix, "ambiguity_new", "new4", "", 4, observedAt.Add(time.Minute), "available", "success")
	new4.MonitoringInstanceID = ambiguityMI
	new4.IPAddress = "198.51.100.230"
	if err := repo.SaveReports(ctx, []ipquality.ReportWrite{old6, new4}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into vps_assets (vps_id,display_name,ipv4,lifecycle_status) values ('vps_family_ambiguity_peer','peer','198.51.100.230','active')`); err != nil {
		t.Fatal(err)
	}
	summary, err := repo.GetLatestVPSIPQualitySummary(ctx, ambiguityVPS)
	if err != nil || summary != nil {
		t.Fatalf("ambiguous default borrowed IPv6: %#v err=%v", summary, err)
	}
	full, err := repo.GetVPSIPQuality(ctx, ambiguityVPS, 0)
	if err != nil || full.DefaultIPVersion != 4 || full.Summary == nil || !full.Summary.Ambiguous || full.Summary.IPVersion != 4 {
		t.Fatalf("full report lost selected ambiguity: %#v err=%v", full, err)
	}
}

func assertIPQualityFamilyContradictoryMetadata(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, pool *pgxpool.Pool) {
	t.Helper()
	for index, tc := range []struct {
		name, availability, status, address, errorCode string
	}{
		{"available with unavailable terminal", "available", "failure", "0.0.0.0", "address_family_unavailable"},
		{"unknown with unavailable terminal", "unknown", "failure", "0.0.0.0", "address_family_unavailable"},
		{"unavailable with lookup terminal", "unavailable", "failure", "0.0.0.0", "lookup_failed"},
		{"unavailable with real successful exit", "unavailable", "success", "198.51.100.10", ""},
		{"unknown with real successful exit", "unknown", "success", "198.51.100.10", ""},
		{"available real exit with reserved lookup error", "available", "failure", "198.51.100.10", "lookup_failed"},
		{"available real exit with reserved unavailable error", "available", "failure", "198.51.100.10", "address_family_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prefix := fmt.Sprintf("ipq_family_conflict_%d", index)
			seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
			observedAt := time.Now().UTC().Add(-10 * time.Second)
			report := pairedIPQualityTestReport(prefix, "family_conflict_"+prefix, prefix, "", 4, observedAt, tc.availability, tc.status)
			report.IPAddress = tc.address
			report.ErrorCode = tc.errorCode
			report6 := pairedIPQualityTestReport(prefix, "family_conflict_"+prefix, prefix, "", 6, observedAt, "available", "success")
			repo := NewPostgresIPQualityRepository(pool)
			if err := repo.SaveReports(ctx, []ipquality.ReportWrite{report, report6}); err != nil {
				t.Fatal(err)
			}
			got, err := repo.GetVPSIPQuality(ctx, "vps_"+prefix, 0)
			if err != nil {
				t.Fatal(err)
			}
			if got.DefaultIPVersion != 4 || got.SelectedIPVersion != 4 || got.Families[0].Availability != "unknown" || got.Families[0].LatestAttempt == nil || got.Families[0].LatestAttempt.Stale {
				t.Fatalf("contradictory metadata asserted current availability or excluded IPv4: %#v", got)
			}
			if got.Families[0].LatestAttempt.CollectionScope != "dual_stack" {
				t.Fatalf("invalid paired terminal downgraded to legacy: %#v", got.Families[0])
			}
			if tc.status == "failure" {
				assertIPQualitySnapshotEmpty(t, got)
			}
		})
	}
}

func assertIPQualityAddressFamilyMigrationPreservesLegacyRows(t *testing.T, ctx context.Context) {
	t.Helper()
	db := openRecordPlatformTemporaryPostgresDatabase(t, ctx)
	// Follow the existing CPU sampling predecessor-migration pattern; its
	// helper is private to package store_test, so this anchor uses a local helper.
	// No current family-state view may exist while these legacy facts are seeded.
	if err := applyIPQualityMigrationsThrough(t, ctx, db, "0075_add_settings_deployment_overrides.sql"); err != nil {
		t.Fatalf("prepare C75 predecessor for IP quality preservation: %v", err)
	}
	var predecessor, absent bool
	if err := db.QueryRow(ctx, `
		select exists (select 1 from schema_migrations where name = '0075_add_settings_deployment_overrides.sql')
			and not exists (select 1 from schema_migrations where name = '0076_ip_quality_address_families.sql'),
			to_regclass('public.ip_quality_vps_family_state') is null`).Scan(&predecessor, &absent); err != nil {
		t.Fatal(err)
	}
	if !predecessor || !absent {
		t.Fatal("legacy IP quality rows must be seeded on C75, before the family-state migration")
	}
	const prefix = "ipq_c75_preservation"
	if _, err := db.Exec(ctx, `
		insert into vps_assets (vps_id, display_name, ipv4, ipv6, lifecycle_status)
		values ($1, 'C75 legacy IP quality', '198.51.100.245', '2001:db8::245', 'active')`, "vps_"+prefix); err != nil {
		t.Fatal(err)
	}
	seedIPQualityIdentityMonitoring(t, ctx, db, "mi_"+prefix, "vps_"+prefix, "vnl_"+prefix)
	observedAt := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	report4 := ipQualitySyncFullReport(prefix, "legacy_family_4", observedAt, observedAt.Add(time.Second), "ipqc_c75_legacy")
	report4.IPAddress = "198.51.100.245"
	report4.DiagnosticsJSON = json.RawMessage(`{"source_version":"v2","collection_revision":3,"service_probe_revision":2,"ip_address_family":4,"collect_request_id":"ipqc_c75_legacy","legacy_marker":"IPv4 kept"}`)
	report6 := ipQualitySyncFullReport(prefix, "legacy_family_6", observedAt.Add(time.Minute), observedAt.Add(time.Minute+time.Second), "ipqc_c75_legacy")
	report6.IPVersion = 6
	report6.IPAddress = "2001:db8::245"
	report6.DiagnosticsJSON = json.RawMessage(`{"source_version":"v2","collection_revision":3,"service_probe_revision":2,"ip_address_family":6,"collect_request_id":"ipqc_c75_legacy","legacy_marker":"IPv6 kept"}`)
	failure := ipQualitySyncFullReport(prefix, "legacy_lookup_failure", observedAt.Add(2*time.Minute), observedAt.Add(2*time.Minute+time.Second), "")
	failure.IPAddress = "0.0.0.0"
	failure.Status = "failure"
	failure.ErrorCode = "lookup_failed"
	failure.ProviderResults = nil
	failure.ServiceUnlocks = nil
	seedIPQualityC75Reports(t, ctx, db, prefix, []ipquality.ReportWrite{report4, report6, failure})
	// Compare every C75 column, including IDs, timestamps, diagnostics,
	// coverage and raw/extra JSON. C77's additive columns are checked separately.
	snapshot := func() []byte {
		t.Helper()
		var raw []byte
		if err := db.QueryRow(ctx, `
			select jsonb_build_object(
				'reports', (select coalesce(jsonb_agg(to_jsonb(r) - 'product_json' - 'product_summary_json' order by r.report_id), '[]'::jsonb) from ip_quality_reports r),
				'providers', (select coalesce(jsonb_agg(to_jsonb(p) - 'details_json' order by p.result_id), '[]'::jsonb) from ip_quality_provider_results p),
				'services', (select coalesce(jsonb_agg(to_jsonb(s) - 'checks_json' order by s.unlock_id), '[]'::jsonb) from ip_quality_service_unlocks s)
			)`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	before := snapshot()
	var reportCount, providerCount, serviceCount int
	if err := db.QueryRow(ctx, `
		select (select count(*) from ip_quality_reports),
			(select count(*) from ip_quality_provider_results),
			(select count(*) from ip_quality_service_unlocks)`).Scan(&reportCount, &providerCount, &serviceCount); err != nil {
		t.Fatal(err)
	}
	if reportCount != 3 || providerCount != 6 || serviceCount != 4 {
		t.Fatalf("predecessor facts missing: reports=%d providers=%d services=%d", reportCount, providerCount, serviceCount)
	}
	if err := applyIPQualityMigrationsThrough(t, ctx, db, "0076_ip_quality_address_families.sql"); err != nil {
		t.Fatalf("apply only 0076 after legacy IP quality rows: %v", err)
	}
	if !bytes.Equal(before, snapshot()) {
		t.Fatal("0076 changed legacy report/provider/service rows")
	}
	// Prove the C75→C76 transition before installing C77 for today's reader.
	// The current repository deliberately has no historical-schema write shim.
	if err := storemigrate.Apply(ctx, db); err != nil {
		t.Fatalf("upgrade preserved C76 facts to current schema: %v", err)
	}
	if !bytes.Equal(before, snapshot()) {
		t.Fatal("current migration changed legacy report/provider/service facts")
	}
	var legacyProductFieldsUnchanged bool
	if err := db.QueryRow(ctx, `
		select not exists (select 1 from ip_quality_reports where product_json is not null or product_summary_json is not null)
			and not exists (select 1 from ip_quality_provider_results where details_json is not null)
			and not exists (select 1 from ip_quality_service_unlocks where checks_json is not null)
			and not exists (select 1 from ip_quality_intelligence)
			and not exists (select 1 from ip_quality_source_cache)
	`).Scan(&legacyProductFieldsUnchanged); err != nil || !legacyProductFieldsUnchanged {
		t.Fatalf("current migration fabricated product facts/jobs/cache for C75 history: %t/%v", legacyProductFieldsUnchanged, err)
	}
	repository := NewPostgresIPQualityRepository(db)
	for _, version := range []int{0, 4, 6} {
		got, err := repository.GetVPSIPQuality(ctx, "vps_"+prefix, version)
		if err != nil {
			t.Fatal(err)
		}
		wantVersion := version
		if wantVersion == 0 {
			wantVersion = 4
		}
		if got.DefaultIPVersion != 4 || got.SelectedIPVersion != wantVersion || got.LatestReport == nil || got.LatestReport.IPVersion != wantVersion ||
			len(got.History) != 1 || len(got.ProviderResults) != 3 || len(got.ServiceUnlocks) != 2 {
			t.Fatalf("migrated legacy family facts lost identity or gained placeholder history: %#v", got)
		}
		if agentapi.ParseCollectionMetadata(got.LatestReport.DiagnosticsJSON).Paired {
			t.Fatalf("migration rewrote legacy diagnostics as paired: %s", got.LatestReport.DiagnosticsJSON)
		}
		for _, family := range got.Families {
			if family.Availability != "unknown" || family.LatestAttempt == nil || family.LatestAttempt.CollectionScope != "legacy_single_family" || family.LatestAttempt.CollectionID != "" {
				t.Fatalf("migration fabricated verified availability/collection metadata: %#v", family)
			}
		}
	}
	if err := storemigrate.Apply(ctx, db); err != nil {
		t.Fatalf("reapply current migrations: %v", err)
	}
	if !bytes.Equal(before, snapshot()) {
		t.Fatal("repeated migration application changed legacy IP quality rows")
	}
	t.Logf("C75→0076 preservation evidence: %d report, %d provider and %d service complete rows unchanged; legacy diagnostics and independent family reads retained", reportCount, providerCount, serviceCount)
}

func applyIPQualityMigrationsThrough(t *testing.T, ctx context.Context, db *pgxpool.Pool, through string) error {
	t.Helper()
	if _, err := db.Exec(ctx, `create table if not exists schema_migrations (name text primary key, checksum text not null, applied_at timestamptz not null default now())`); err != nil {
		return err
	}
	names, err := storemigrate.Names()
	if err != nil {
		return err
	}
	for _, name := range names {
		if name > through {
			break
		}
		payload, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		digest := sha256.Sum256(payload)
		var appliedChecksum string
		err = db.QueryRow(ctx, `select checksum from schema_migrations where name = $1`, name).Scan(&appliedChecksum)
		if err == nil {
			if appliedChecksum != hex.EncodeToString(digest[:]) {
				return fmt.Errorf("historical migration %s checksum mismatch", name)
			}
			if name == through {
				return nil
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("read historical migration %s: %w", name, err)
		}
		tx, err := db.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(payload)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("execute migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `insert into schema_migrations (name, checksum) values ($1, $2)`, name, hex.EncodeToString(digest[:])); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
		if name == through {
			return nil
		}
	}
	return fmt.Errorf("migration %q not found", through)
}

// Seed the frozen C75 SQL shape directly. Using SaveReports here would make
// historical migration evidence depend on columns introduced after C75.
func seedIPQualityC75Reports(t *testing.T, ctx context.Context, db *pgxpool.Pool, prefix string, reports []ipquality.ReportWrite) {
	t.Helper()
	tx, err := db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for index, report := range reports {
		reportID := fmt.Sprintf("ipq_%s_%d", prefix, index)
		if _, err := tx.Exec(ctx, `
			insert into ip_quality_reports (
				report_id, monitoring_instance_id, observed_at, received_at, agent_version, fingerprint,
				sync_batch_id, ip_address, ip_version, status, asn, organization, latitude, longitude,
				use_region_code, use_region_name, registered_region_code, registered_region_name,
				risk_level, error_code, error_summary, is_backfilled, raw_json, coverage_json, diagnostics_json
			) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23::jsonb,$24::jsonb,$25::jsonb)`,
			reportID, report.MonitoringInstanceID, report.ObservedAt, report.ReceivedAt, report.AgentVersion, report.Fingerprint,
			report.SyncBatchID, report.IPAddress, report.IPVersion, report.Status, report.ASN, report.Organization,
			report.Latitude, report.Longitude, report.UseRegionCode, report.UseRegionName, report.RegisteredRegionCode,
			report.RegisteredRegionName, report.RiskLevel, report.ErrorCode, report.ErrorSummary, report.IsBackfilled,
			ipquality.SanitizeRawJSON(report.RawJSON), ipquality.SanitizeExtraJSON(report.CoverageJSON),
			ipquality.SanitizeExtraJSON(report.DiagnosticsJSON),
		); err != nil {
			t.Fatalf("seed C75 report %d with historical columns: %v", index, err)
		}
		for providerIndex, provider := range report.ProviderResults {
			if _, err := tx.Exec(ctx, `
				insert into ip_quality_provider_results (
					result_id, report_id, provider, status, source_type, latency_ms, usage_type, company_type,
					risk_level, risk_score, region_code, region_name, is_proxy, is_tor, is_vpn, is_server,
					is_abuser, is_robot, error_code, error_summary, extra_json
				) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21::jsonb)`,
				fmt.Sprintf("%s_provider_%d", reportID, providerIndex), reportID, provider.Provider,
				provider.Status, provider.SourceType, provider.LatencyMS, provider.UsageType, provider.CompanyType,
				provider.RiskLevel, provider.RiskScore, provider.RegionCode, provider.RegionName,
				provider.IsProxy, provider.IsTor, provider.IsVPN, provider.IsServer, provider.IsAbuser, provider.IsRobot,
				provider.ErrorCode, provider.ErrorSummary, ipquality.SanitizeExtraJSON(provider.ExtraJSON),
			); err != nil {
				t.Fatalf("seed C75 provider %d/%d with historical columns: %v", index, providerIndex, err)
			}
		}
		for serviceIndex, service := range report.ServiceUnlocks {
			if _, err := tx.Exec(ctx, `
				insert into ip_quality_service_unlocks (
					unlock_id, report_id, service, source, status, probe_status, latency_ms, region,
					unlock_type, error_code, error_summary, extra_json
				) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb)`,
				fmt.Sprintf("%s_service_%d", reportID, serviceIndex), reportID, service.Service, service.Source,
				service.Status, service.ProbeStatus, service.LatencyMS, service.Region, service.UnlockType,
				service.ErrorCode, service.ErrorSummary, ipquality.SanitizeExtraJSON(service.ExtraJSON),
			); err != nil {
				t.Fatalf("seed C75 service %d/%d with historical columns: %v", index, serviceIndex, err)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit C75 report fixture: %v", err)
	}
}
