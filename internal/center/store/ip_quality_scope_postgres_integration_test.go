package store

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"houfeng/internal/center/ipquality"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/contracts/agentapi"
)

func TestPostgresIntegrationIPQualityCollectionScope(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture := newRecordsPostgresFixture(t, ctx)
	pool := fixture.openDirectRuntimePool(t, ctx, "ip-quality-scope", 4)
	repo := NewPostgresIPQualityRepository(pool)
	t.Run("Go and SQL accept identical family metadata", func(t *testing.T) {
		for index, tc := range []struct {
			name, revision, expected, family string
			valid                            bool
		}{
			{"rev4 dual", "4", "[4,6]", "4", true},
			{"rev4 single", "4", "[4]", "4", false},
			{"rev5 four", "5", "[4]", "4", true},
			{"rev5 six excludes four", "5", "[6]", "4", false},
			{"rev5 dual", "5", "[4,6]", "4", true},
			{"reverse", "5", "[6,4]", "4", false},
			{"duplicate", "5", "[4,4]", "4", false},
			{"empty", "5", "[]", "4", false},
			{"null", "5", "null", "4", false},
			{"string array", "5", "[\"4\"]", "4", false},
			{"fractional family", "5", "[4.0]", "4", false},
			{"fractional revision", "5.0", "[4]", "4", false},
			{"string revision", "\"5\"", "[4]", "4", false},
			{"future", "6", "[4]", "4", false},
			{"exponent revision", "5e0", "[4]", "4", false},
			{"exponent family", "5", "[4e0]", "4", false},
			{"old paired", "3", "[4]", "4", false},
			{"family mismatch", "5", "[4]", "6", false},
			{"family string", "5", "[4]", "\"4\"", false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				prefix := fmt.Sprintf("ipq_scope_truth_%d", index)
				seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
				now := time.Now().UTC().Add(-time.Minute)
				report := pairedIPQualityTestReport(prefix, "scope_truth", prefix, "scope_request", 4, now, "unavailable", "failure")
				report.DiagnosticsJSON = json.RawMessage(fmt.Sprintf(`{"collection_revision":%s,"collection_id":%q,"expected_ip_versions":%s,"ip_address_family":%s,"family_availability":"unavailable"}`, tc.revision, prefix, tc.expected, tc.family))
				meta := agentapi.ParseCollectionMetadata(report.DiagnosticsJSON)
				valid := meta.Valid && meta.IPVersion == report.IPVersion
				if valid != tc.valid || !meta.Paired {
					t.Fatalf("Go metadata=%+v want valid=%v", meta, tc.valid)
				}
				if err := repo.SaveReports(ctx, []ipquality.ReportWrite{report}); err != nil {
					t.Fatal(err)
				}
				selected, err := repo.GetVPSIPQuality(ctx, "vps_"+prefix, 4)
				if err != nil {
					t.Fatal(err)
				}
				want := "unknown"
				if tc.valid {
					want = "unavailable"
				}
				if selected.Families[0].Availability != want {
					t.Fatalf("SQL availability=%q Go valid=%v", selected.Families[0].Availability, valid)
				}
				if selected.Summary != nil || selected.LatestReport != nil || len(selected.History) != 0 {
					t.Fatal("failure placeholder became an IP fact")
				}
			})
		}
	})
	t.Run("Go and SQL agree on terminal address legality", func(t *testing.T) {
		for index, tc := range []struct {
			address, availability, code string
			family                      int
			valid                       bool
		}{
			{"198.51.100.10", "available", "timeout", 4, true},
			{"10.0.0.1", "available", "timeout", 4, true},
			{"127.0.0.1", "available", "timeout", 4, false},
			{"169.254.1.1", "available", "timeout", 4, false},
			{"224.0.0.1", "available", "timeout", 4, false},
			{"255.255.255.255", "available", "timeout", 4, false},
			{"2001:db8::1", "available", "timeout", 6, true},
			{"fd00::1", "available", "timeout", 6, true},
			{"::1", "available", "timeout", 6, false},
			{"fe80::1", "available", "timeout", 6, false},
			{"ff02::1", "available", "timeout", 6, false},
			{"::ffff:127.0.0.1", "available", "timeout", 6, false},
			{"::ffff:198.51.100.10", "available", "timeout", 6, false},
			{"0.0.0.0", "unavailable", "address_family_unavailable", 4, true},
			{"::", "unavailable", "address_family_unavailable", 6, true},
			{"0.0.0.0", "unknown", "lookup_failed", 4, true},
			{"::", "unknown", "lookup_failed", 6, true},
			{"0.0.0.0", "available", "timeout", 4, false},
			{"::", "available", "timeout", 6, false},
		} {
			t.Run(tc.address+"/"+tc.code, func(t *testing.T) {
				prefix := fmt.Sprintf("ipq_address_truth_%d", index)
				seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
				now := time.Now().UTC().Add(-time.Minute)
				registry := ipquality.NewCollectRequests(func() (string, error) { return prefix, nil })
				report := pairedIPQualityTestReport(prefix, "address_truth", prefix, prefix, tc.family, now, tc.availability, "failure")
				if _, err := registry.Request(report.MonitoringInstanceID, now, []int{tc.family}); err != nil {
					t.Fatal(err)
				}
				report.IPAddress, report.ErrorCode = tc.address, tc.code
				report.DiagnosticsJSON = json.RawMessage(fmt.Sprintf(`{"collection_revision":5,"collection_id":%q,"expected_ip_versions":[%d],"ip_address_family":%d,"family_availability":%q,"collect_request_id":%q}`, prefix, tc.family, tc.family, tc.availability, prefix))
				report.CollectionMetadata = agentapi.ParseCollectionMetadata(report.DiagnosticsJSON)
				registry.ObserveReports(report.MonitoringInstanceID, []ipquality.ReportWrite{report}, now.Add(time.Second))
				request, _ := registry.Latest(report.MonitoringInstanceID, now.Add(time.Second))
				if completed := request.Status == ipquality.CollectRequestCompleted; completed != tc.valid {
					t.Fatalf("Go terminal=%v want=%v: %+v", completed, tc.valid, request)
				}
				if err := repo.SaveReports(ctx, []ipquality.ReportWrite{report}); err != nil {
					t.Fatal(err)
				}
				selected, err := repo.GetVPSIPQuality(ctx, "vps_"+prefix, tc.family)
				if err != nil {
					t.Fatal(err)
				}
				want := "unknown"
				if tc.valid {
					want = tc.availability
				}
				if got := selected.Families[(tc.family-4)/2].Availability; got != want {
					t.Fatalf("SQL availability=%s want=%s for Go valid=%v", got, want, tc.valid)
				}
			})
		}
	})
	t.Run("single-family writes retain other-family facts and attempts", func(t *testing.T) {
		const prefix = "ipq_scope_isolation"
		seedSyncInterleavingFixture(t, ctx, fixture, prefix, monitoringinstances.LifecycleInUse)
		now := time.Now().UTC().Truncate(time.Microsecond).Add(-time.Minute)
		four := pairedIPQualityTestReport(prefix, "scope_initial", prefix, "initial", 4, now, "available", "success")
		six := pairedIPQualityTestReport(prefix, "scope_initial", prefix, "initial", 6, now, "unavailable", "failure")
		if err := repo.SaveReports(ctx, []ipquality.ReportWrite{four, six}); err != nil {
			t.Fatal(err)
		}
		before, err := repo.GetVPSIPQuality(ctx, "vps_"+prefix, 4)
		if err != nil {
			t.Fatal(err)
		}
		six = pairedIPQualityTestReport(prefix, "scope_six", "six_only", "single", 6, now.Add(time.Second), "available", "success")
		six.DiagnosticsJSON = json.RawMessage(`{"collection_revision":5,"collection_id":"six_only","expected_ip_versions":[6],"ip_address_family":6,"family_availability":"available","collect_request_id":"single"}`)
		if err := repo.SaveReports(ctx, []ipquality.ReportWrite{six}); err != nil {
			t.Fatal(err)
		}
		after, err := repo.GetVPSIPQuality(ctx, "vps_"+prefix, 6)
		if err != nil {
			t.Fatal(err)
		}
		if after.SelectedIPVersion != 6 || after.DefaultIPVersion != 4 || after.Summary == nil || after.Summary.IPVersion != 6 || len(after.History) != 1 {
			t.Fatalf("selected/default facts=%+v", after)
		}
		if after.Families[1].LatestAttempt.CollectionScope != "single_family" || after.Families[1].LatestAttempt.CollectionID != "six_only" {
			t.Fatalf("single scope=%+v", after.Families[1])
		}
		if after.Families[0].LatestReportSummary.ReportID != before.Summary.ReportID || !after.Families[0].LatestAttempt.ObservedAt.Equal(before.Families[0].LatestAttempt.ObservedAt) {
			t.Fatal("single IPv6 collection changed IPv4")
		}
		history, err := repo.GetVPSIPQualityReportDetail(ctx, "vps_"+prefix, before.Summary.ReportID)
		if err != nil || history.SelectedIPVersion != 4 || history.Summary.ReportID != before.Summary.ReportID || len(history.History) != 1 {
			t.Fatalf("history changed: %+v %v", history, err)
		}
		latest, err := repo.GetLatestVPSIPQualitySummary(ctx, "vps_"+prefix)
		if err != nil || latest == nil || latest.IPVersion != 4 || !latest.ObservedAt.Equal(before.Summary.ObservedAt) {
			t.Fatalf("default summary borrowed newer IPv6: %+v %v", latest, err)
		}
	})
}
