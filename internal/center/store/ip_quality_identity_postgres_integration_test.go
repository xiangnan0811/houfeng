package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/ipquality"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/vpsassets"
	"houfeng/internal/center/vpsoverview"
	"houfeng/internal/ipidentity"
)

func TestPostgresIntegrationIPQualityAddressIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "ip-quality-address-identity", 6)

	assertIPQualityAddressIdentitySQLMatrix(t, ctx, runtimePool)
	identityFixture := seedIPQualityAddressIdentityFixture(t, ctx, runtimePool)
	ipQualityRepository := NewPostgresIPQualityRepository(runtimePool)

	assertIPQualityAddressIdentityOverviewDisabled(t, ctx, runtimePool, identityFixture.activeVPSID)
	assertIPQualityAddressIdentityOverviewBeforeUnlink(t, ctx, runtimePool, ipQualityRepository, identityFixture)
	assertIPQualityAddressIdentityAssignments(t, ctx, ipQualityRepository, runtimePool, identityFixture)
	assertIPQualityAddressIdentityOverviewAfterUnlink(t, ctx, runtimePool, ipQualityRepository, identityFixture)
	assertIPQualityAddressIdentityVPSPatchHistory(t, ctx, runtimePool, identityFixture.patchVPSID)

}

type ipQualityAddressIdentityFixture struct {
	activeVPSID           string
	activeCompetitorVPSID string
	suppressedVPSID       string
	suppressedLinkID      string
	ambiguousVPSAID       string
	ambiguousVPSBID       string
	familyIPv4VPSID       string
	familyMappedIPv6VPSID string
	failureVPSID          string
	patchVPSID            string

	slotsAmbiguousLatestVPSID      string
	slotsAmbiguousLatestPeerVPSID  string
	slotsUniqueLatestVPSID         string
	slotsUniqueLatestPeerVPSID     string
	slotsAmbiguousLatestSharedIPv6 string
	slotsAmbiguousLatestUniqueIPv4 string
	slotsUniqueLatestSharedIPv6    string
	slotsUniqueLatestUniqueIPv4    string

	activeReportIP       string
	suppressedFallbackIP string
	ambiguousReportIP    string
	familyMappedReportIP string
}

func seedIPQualityAddressIdentityFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool) ipQualityAddressIdentityFixture {
	t.Helper()
	fixture := ipQualityAddressIdentityFixture{
		activeVPSID:                    "vps_identity_active",
		activeCompetitorVPSID:          "vps_identity_active_competitor",
		suppressedVPSID:                "vps_identity_suppressed",
		suppressedLinkID:               "vnl_identity_suppressed",
		ambiguousVPSAID:                "vps_identity_ambiguous_a",
		ambiguousVPSBID:                "vps_identity_ambiguous_b",
		familyIPv4VPSID:                "vps_identity_family_v4",
		familyMappedIPv6VPSID:          "vps_identity_family_mapped6",
		failureVPSID:                   "vps_identity_failure",
		patchVPSID:                     "vps_identity_patch",
		slotsAmbiguousLatestVPSID:      "vps_identity_slots_ambiguous_latest",
		slotsAmbiguousLatestPeerVPSID:  "vps_identity_slots_ambiguous_peer",
		slotsUniqueLatestVPSID:         "vps_identity_slots_unique_latest",
		slotsUniqueLatestPeerVPSID:     "vps_identity_slots_unique_peer",
		slotsAmbiguousLatestSharedIPv6: "2001:db8::150",
		slotsAmbiguousLatestUniqueIPv4: "198.18.0.150",
		slotsUniqueLatestSharedIPv6:    "2001:db8::151",
		slotsUniqueLatestUniqueIPv4:    "198.18.0.151",
		activeReportIP:                 "203.0.113.77",
		suppressedFallbackIP:           "2001:0db8:0:0:0:0:0:88",
		ambiguousReportIP:              "2001:0DB8:0:0:0:0:0:99",
		familyMappedReportIP:           "0:0:0:0:0:ffff:c000:22c",
	}

	vpsRows := []struct {
		id   string
		ipv4 string
		ipv6 string
	}{
		{fixture.activeVPSID, "", "2001:db8::77"},
		{fixture.activeCompetitorVPSID, fixture.activeReportIP, ""},
		{fixture.suppressedVPSID, "", "2001:db8::88"},
		{fixture.ambiguousVPSAID, "", "2001:db8::99"},
		{fixture.ambiguousVPSBID, "", fixture.ambiguousReportIP},
		{fixture.familyIPv4VPSID, "192.0.2.44", ""},
		{fixture.familyMappedIPv6VPSID, "", "::ffff:192.0.2.44"},
		{fixture.failureVPSID, "198.51.100.60", ""},
		{fixture.patchVPSID, "198.51.100.61", "2001:db8::10"},
		{fixture.slotsAmbiguousLatestVPSID, fixture.slotsAmbiguousLatestUniqueIPv4, fixture.slotsAmbiguousLatestSharedIPv6},
		{fixture.slotsAmbiguousLatestPeerVPSID, "", fixture.slotsAmbiguousLatestSharedIPv6},
		{fixture.slotsUniqueLatestVPSID, fixture.slotsUniqueLatestUniqueIPv4, fixture.slotsUniqueLatestSharedIPv6},
		{fixture.slotsUniqueLatestPeerVPSID, "", fixture.slotsUniqueLatestSharedIPv6},
		{"vps_identity_suppressed_fallback_owner", "", ""},
		{"vps_identity_ambiguous_owner", "", ""},
		{"vps_identity_family_owner", "", ""},
		{"vps_identity_failure_owner", "", ""},
	}
	for _, row := range vpsRows {
		if _, err := pool.Exec(ctx, `
			insert into public.vps_assets (vps_id, display_name, ipv4, ipv6, lifecycle_status)
			values ($1, $2, $3, $4, 'active')`, row.id, row.id, row.ipv4, row.ipv6); err != nil {
			t.Fatalf("seed VPS %q: %v", row.id, err)
		}
	}

	seedIPQualityIdentityMonitoring(t, ctx, pool, "mi_identity_active", fixture.activeVPSID, "vnl_identity_active")
	seedIPQualityIdentityMonitoring(t, ctx, pool, "mi_identity_suppressed_link", fixture.suppressedVPSID, fixture.suppressedLinkID)
	seedIPQualityIdentityMonitoring(t, ctx, pool, "mi_identity_suppressed_fallback", "vps_identity_suppressed_fallback_owner", "")
	seedIPQualityIdentityMonitoring(t, ctx, pool, "mi_identity_ambiguous", "vps_identity_ambiguous_owner", "")
	seedIPQualityIdentityMonitoring(t, ctx, pool, "mi_identity_family", "vps_identity_family_owner", "")
	seedIPQualityIdentityMonitoring(t, ctx, pool, "mi_identity_failure", "vps_identity_failure_owner", "")
	seedIPQualityIdentityMonitoring(t, ctx, pool, "mi_identity_slots_ambiguous", fixture.slotsAmbiguousLatestVPSID, "")
	seedIPQualityIdentityMonitoring(t, ctx, pool, "mi_identity_slots_unique", fixture.slotsUniqueLatestVPSID, "")

	repository := NewPostgresIPQualityRepository(pool)
	saveIPQualityIdentityReport(t, ctx, pool, repository, "mi_identity_active", fixture.activeReportIP, 4, "success", "active")
	saveIPQualityIdentityReport(t, ctx, pool, repository, "mi_identity_suppressed_link", "2001:db8::88", 6, "failure", "suppressed-link-failure")
	saveIPQualityIdentityReport(t, ctx, pool, repository, "mi_identity_suppressed_fallback", fixture.suppressedFallbackIP, 6, "success", "suppressed-fallback")
	saveIPQualityIdentityReportAt(t, ctx, pool, repository, "mi_identity_ambiguous", fixture.ambiguousReportIP, 6, "partial", "ambiguous", "high", time.Now().UTC().Add(-10*24*time.Hour))
	saveIPQualityIdentityReport(t, ctx, pool, repository, "mi_identity_family", fixture.familyMappedReportIP, 6, "success", "family")
	saveIPQualityIdentityReport(t, ctx, pool, repository, "mi_identity_failure", "198.51.100.60", 4, "failure", "failure-placeholder")

	now := time.Now().UTC()
	saveIPQualityIdentityReportAt(t, ctx, pool, repository, "mi_identity_slots_ambiguous", fixture.slotsAmbiguousLatestUniqueIPv4, 4, "success", "slots-ambiguous-unique-older", "low", now.Add(-4*time.Minute))
	saveIPQualityIdentityReportAt(t, ctx, pool, repository, "mi_identity_slots_ambiguous", "2001:0DB8:0:0:0:0:0:150", 6, "success", "slots-ambiguous-shared-latest", "high", now.Add(-3*time.Minute))
	saveIPQualityIdentityReportAt(t, ctx, pool, repository, "mi_identity_slots_unique", "2001:0DB8:0:0:0:0:0:151", 6, "success", "slots-unique-shared-older", "high", now.Add(-3*time.Minute))
	saveIPQualityIdentityReportAt(t, ctx, pool, repository, "mi_identity_slots_unique", fixture.slotsUniqueLatestUniqueIPv4, 4, "success", "slots-unique-unique-latest", "low", now.Add(-2*time.Minute))

	return fixture
}

func seedIPQualityIdentityMonitoring(t *testing.T, ctx context.Context, pool *pgxpool.Pool, monitoringInstanceID, vpsID, linkID string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		insert into public.monitoring_instances (
			vps_id, monitoring_instance_id, display_name, region, city, provider, lifecycle_status,
			monitoring_status, binding_status, binding_fingerprint, binding_epoch_started_at, sync_token_hash
		) values ($1, $2, $3, '', '', 'Identity Test Provider', $4, $5, $6, $7, now(), $8)`,
		vpsID, monitoringInstanceID, monitoringInstanceID,
		monitoringinstances.LifecycleInUse, monitoringinstances.MonitoringEnabled, monitoringinstances.BindingBound,
		"fingerprint-"+monitoringInstanceID, "sync-token-hash-"+monitoringInstanceID); err != nil {
		t.Fatalf("seed monitoring instance %q: %v", monitoringInstanceID, err)
	}
	if linkID == "" {
		return
	}
	if _, err := pool.Exec(ctx, `
		insert into public.vps_monitoring_instance_links (link_id, vps_id, monitoring_instance_id, note)
		values ($1, $2, $3, '')`, linkID, vpsID, monitoringInstanceID); err != nil {
		t.Fatalf("seed monitoring link %q: %v", linkID, err)
	}
}

func saveIPQualityIdentityReport(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *PostgresIPQualityRepository,
	monitoringInstanceID, ipAddress string,
	ipVersion int,
	status, suffix string,
) string {
	t.Helper()
	return saveIPQualityIdentityReportAt(
		t,
		ctx,
		pool,
		repository,
		monitoringInstanceID,
		ipAddress,
		ipVersion,
		status,
		suffix,
		"",
		time.Now().UTC().Add(-time.Duration(len(suffix))*time.Second),
	)
}

func saveIPQualityIdentityReportAt(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *PostgresIPQualityRepository,
	monitoringInstanceID, ipAddress string,
	ipVersion int,
	status, suffix, riskLevel string,
	observedAt time.Time,
) string {
	t.Helper()
	syncBatchID := "sync_identity_" + suffix
	if err := repository.SaveReports(ctx, []ipquality.ReportWrite{{
		MonitoringInstanceID: monitoringInstanceID,
		ObservedAt:           observedAt,
		ReceivedAt:           observedAt.Add(time.Second),
		AgentVersion:         "identity-test-agent/v1",
		Fingerprint:          "identity-test-fingerprint",
		SyncBatchID:          syncBatchID,
		IPAddress:            ipAddress,
		IPVersion:            ipVersion,
		Status:               status,
		RiskLevel:            riskLevel,
	}}); err != nil {
		t.Fatalf("save identity report %q: %v", suffix, err)
	}
	var reportID string
	if err := pool.QueryRow(ctx, `
		select report_id
		from public.ip_quality_reports
		where sync_batch_id = $1`, syncBatchID).Scan(&reportID); err != nil {
		t.Fatalf("read identity report %q: %v", suffix, err)
	}
	return reportID
}

func assertIPQualityAddressIdentityAssignments(
	t *testing.T,
	ctx context.Context,
	repository *PostgresIPQualityRepository,
	pool *pgxpool.Pool,
	fixture ipQualityAddressIdentityFixture,
) {
	t.Helper()

	active, err := repository.GetVPSIPQuality(ctx, fixture.activeVPSID)
	if err != nil {
		t.Fatalf("GetVPSIPQuality active-link report: %v", err)
	}
	if active.Summary == nil || active.Summary.Ambiguous || active.Summary.AssignmentMode != "link" || active.Summary.IPAddress != fixture.activeReportIP || active.LatestReport == nil || active.LatestReport.IPAddress != fixture.activeReportIP {
		t.Fatalf("active-link assignment = %#v, latest=%#v, want link report %q", active.Summary, active.LatestReport, fixture.activeReportIP)
	}

	suppressed, err := repository.GetVPSIPQuality(ctx, fixture.suppressedVPSID)
	if err != nil {
		t.Fatalf("GetVPSIPQuality active-link suppression: %v", err)
	}
	if suppressed.Summary != nil || suppressed.LatestReport != nil || len(suppressed.History) != 0 {
		t.Fatalf("active link without valid report leaked fallback assignment: %#v", suppressed)
	}
	if _, err := pool.Exec(ctx, `
		update public.vps_monitoring_instance_links
		set unlinked_at = now()
		where link_id = $1`, fixture.suppressedLinkID); err != nil {
		t.Fatalf("unlink suppressed monitoring instance: %v", err)
	}
	fallback, err := repository.GetVPSIPQuality(ctx, fixture.suppressedVPSID)
	if err != nil {
		t.Fatalf("GetVPSIPQuality after unlink: %v", err)
	}
	if fallback.Summary == nil || fallback.Summary.AssignmentMode != "ip_match" || fallback.Summary.IPAddress != fixture.suppressedFallbackIP || fallback.LatestReport == nil || fallback.LatestReport.IPAddress != fixture.suppressedFallbackIP {
		t.Fatalf("fallback assignment after unlink = %#v, latest=%#v, want ip_match report %q", fallback.Summary, fallback.LatestReport, fixture.suppressedFallbackIP)
	}

	for _, vpsID := range []string{fixture.ambiguousVPSAID, fixture.ambiguousVPSBID} {
		ambiguous, err := repository.GetVPSIPQuality(ctx, vpsID)
		if err != nil {
			t.Fatalf("GetVPSIPQuality ambiguous %q: %v", vpsID, err)
		}
		if ambiguous.Summary == nil || !ambiguous.Summary.Ambiguous || ambiguous.Summary.AssignmentMode != "ip_match" || ambiguous.Summary.IPAddress != fixture.ambiguousReportIP {
			t.Fatalf("ambiguous assignment for %q = %#v, want ambiguous ip_match report %q", vpsID, ambiguous.Summary, fixture.ambiguousReportIP)
		}
	}

	familyIPv4, err := repository.GetVPSIPQuality(ctx, fixture.familyIPv4VPSID)
	if err != nil {
		t.Fatalf("GetVPSIPQuality IPv4/mapped IPv6 separation: %v", err)
	}
	if familyIPv4.Summary != nil || familyIPv4.LatestReport != nil {
		t.Fatalf("mapped IPv6 report crossed into IPv4 VPS identity: %#v", familyIPv4)
	}
	familyMappedIPv6, err := repository.GetVPSIPQuality(ctx, fixture.familyMappedIPv6VPSID)
	if err != nil {
		t.Fatalf("GetVPSIPQuality mapped IPv6 identity: %v", err)
	}
	if familyMappedIPv6.Summary == nil || familyMappedIPv6.Summary.AssignmentMode != "ip_match" || familyMappedIPv6.Summary.IPAddress != fixture.familyMappedReportIP {
		t.Fatalf("mapped IPv6 assignment = %#v, want same-family ip_match report %q", familyMappedIPv6.Summary, fixture.familyMappedReportIP)
	}

	failure, err := repository.GetVPSIPQuality(ctx, fixture.failureVPSID)
	if err != nil {
		t.Fatalf("GetVPSIPQuality failure placeholder: %v", err)
	}
	if failure.Summary != nil || failure.LatestReport != nil || len(failure.History) != 0 {
		t.Fatalf("failure placeholder entered VPS read model: %#v", failure)
	}
}

type ipQualityAddressIdentityOverviewCase struct {
	name         string
	vpsID        string
	wantSummary  bool
	reportSuffix string
	wantStatus   string
	wantRisk     string
}

func assertIPQualityAddressIdentityOverviewBeforeUnlink(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *PostgresIPQualityRepository,
	fixture ipQualityAddressIdentityFixture,
) {
	t.Helper()
	enableIPQualityAddressIdentityOverview(t, ctx, pool)
	assertIPQualityAddressIdentityOverviewCases(t, ctx, pool, repository, []ipQualityAddressIdentityOverviewCase{
		{
			name:         "active link wins over matching no-link competitor",
			vpsID:        fixture.activeVPSID,
			wantSummary:  true,
			reportSuffix: "active",
			wantStatus:   "success",
		},
		{
			name:  "active link suppresses fallback while its only report is failure",
			vpsID: fixture.suppressedVPSID,
		},
	})
}

func assertIPQualityAddressIdentityOverviewAfterUnlink(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *PostgresIPQualityRepository,
	fixture ipQualityAddressIdentityFixture,
) {
	t.Helper()
	enableIPQualityAddressIdentityOverview(t, ctx, pool)
	assertIPQualityAddressIdentityOverviewCases(t, ctx, pool, repository, []ipQualityAddressIdentityOverviewCase{
		{
			name:         "equivalent IPv6 fallback after unlink",
			vpsID:        fixture.suppressedVPSID,
			wantSummary:  true,
			reportSuffix: "suppressed-fallback",
			wantStatus:   "success",
		},
		{
			name:  "semantic and exact text IPv6 overlap is ambiguous",
			vpsID: fixture.ambiguousVPSAID,
		},
		{
			name:  "exact text IPv6 overlap is also ambiguous",
			vpsID: fixture.ambiguousVPSBID,
		},
		{
			name:  "ordinary IPv4 does not match mapped IPv6",
			vpsID: fixture.familyIPv4VPSID,
		},
		{
			name:         "mapped IPv6 matches only mapped IPv6",
			vpsID:        fixture.familyMappedIPv6VPSID,
			wantSummary:  true,
			reportSuffix: "family",
			wantStatus:   "success",
		},
		{
			name:  "failure report is filtered",
			vpsID: fixture.failureVPSID,
		},
		{
			name:  "newest ambiguous shared IPv6 hides older unique IPv4",
			vpsID: fixture.slotsAmbiguousLatestVPSID,
		},
		{
			name:         "newest unique IPv4 wins over older ambiguous shared IPv6",
			vpsID:        fixture.slotsUniqueLatestVPSID,
			wantSummary:  true,
			reportSuffix: "slots-unique-unique-latest",
			wantStatus:   "success",
			wantRisk:     "low",
		},
	})

	overview := loadOverviewThroughBootstrapEquivalent(t, ctx, pool, fixture.ambiguousVPSAID)
	if overview.Summary.IPQuality.Status != "missing" || overview.Summary.IPQuality.Detail != "missing" ||
		overview.Summary.IPQuality.Section.State != vpsoverview.SectionReady {
		t.Fatalf("ambiguous selected report Overview IP quality = %#v, want missing with empty risk", overview.Summary.IPQuality)
	}
	var sawMissing bool
	for _, anomaly := range overview.Anomalies {
		switch anomaly.RuleID {
		case vpsoverview.RuleIPQualityMissing:
			sawMissing = true
		case vpsoverview.RuleIPQualityRiskElevated, vpsoverview.RuleIPQualityPartial, vpsoverview.RuleIPQualityStale:
			t.Fatalf("ambiguous selected report emitted judgement anomaly %#v; all selected-report risk/status/stale fields must be empty", anomaly)
		}
	}
	if !sawMissing {
		t.Fatalf("ambiguous selected report Overview anomalies = %#v, want %s", overviewRuleIDs(overview), vpsoverview.RuleIPQualityMissing)
	}
}

func enableIPQualityAddressIdentityOverview(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		insert into public.center_settings (settings_id, ip_quality_settings)
		values ('center', '{"enabled":true,"frequency_seconds":86400,"timeout_seconds":15,"stale_after_seconds":604800,"services":[]}'::jsonb)
		on conflict (settings_id) do update set ip_quality_settings = excluded.ip_quality_settings, updated_at = now()`); err != nil {
		t.Fatalf("enable center IP quality settings: %v", err)
	}
}

func assertIPQualityAddressIdentityOverviewCases(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *PostgresIPQualityRepository,
	cases []ipQualityAddressIdentityOverviewCase,
) {
	t.Helper()
	overviewRepository := newIPQualityAddressIdentityOverviewRepository(t, pool)
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			summary, err := repository.GetLatestVPSIPQualitySummary(ctx, testCase.vpsID)
			if err != nil {
				t.Fatalf("GetLatestVPSIPQualitySummary(%q): %v", testCase.vpsID, err)
			}
			if !testCase.wantSummary {
				if summary != nil {
					t.Fatalf("GetLatestVPSIPQualitySummary(%q) = %#v, want nil after candidate ambiguity/filtering", testCase.vpsID, summary)
				}
			} else {
				if summary == nil {
					t.Fatalf("GetLatestVPSIPQualitySummary(%q) = nil, want status/risk %q/%q",
						testCase.vpsID, testCase.wantStatus, testCase.wantRisk)
				}
				if summary.VPSID != testCase.vpsID || summary.Status != testCase.wantStatus ||
					summary.RiskLevel != testCase.wantRisk || summary.Stale {
					t.Fatalf("GetLatestVPSIPQualitySummary(%q) = %#v, want vps/status/risk %q/%q/%q and fresh",
						testCase.vpsID, summary, testCase.vpsID, testCase.wantStatus, testCase.wantRisk)
				}
				if testCase.reportSuffix != "" {
					var expectedObservedAt time.Time
					if err := pool.QueryRow(ctx, `
						select observed_at
						from public.ip_quality_reports
						where sync_batch_id = $1`,
						"sync_identity_"+testCase.reportSuffix,
					).Scan(&expectedObservedAt); err != nil {
						t.Fatalf("read expected report timestamp %q: %v", testCase.reportSuffix, err)
					}
					if !summary.ObservedAt.Equal(expectedObservedAt) {
						t.Fatalf("GetLatestVPSIPQualitySummary(%q) observed_at = %s, want named report %q at %s",
							testCase.vpsID, summary.ObservedAt, testCase.reportSuffix, expectedObservedAt)
					}
				}
			}

			source, err := overviewRepository.LoadIPQuality(ctx, testCase.vpsID)
			if err != nil {
				t.Fatalf("VPSOverviewRepository.LoadIPQuality(%q): %v", testCase.vpsID, err)
			}
			if source.Section.State != vpsoverview.SectionReady {
				t.Fatalf("VPSOverviewRepository.LoadIPQuality(%q) section = %#v, want ready", testCase.vpsID, source.Section)
			}
			if !testCase.wantSummary {
				if source.Status != "missing" || source.RiskLevel != "" || source.Stale {
					t.Fatalf("VPSOverviewRepository.LoadIPQuality(%q) = %#v, want missing with empty risk and fresh=false", testCase.vpsID, source)
				}
				return
			}
			if source.Status != testCase.wantStatus || source.RiskLevel != testCase.wantRisk || source.Stale {
				t.Fatalf("VPSOverviewRepository.LoadIPQuality(%q) = %#v, want status/risk/fresh %q/%q/false",
					testCase.vpsID, source, testCase.wantStatus, testCase.wantRisk)
			}
		})
	}
}

func newIPQualityAddressIdentityOverviewRepository(t *testing.T, pool *pgxpool.Pool) *VPSOverviewRepository {
	t.Helper()
	repository, err := NewVPSOverviewRepository(
		NewPostgresVPSAssetRepository(pool),
		NewPostgresVPSMonitoringInstanceLinkRepository(pool),
		NewPostgresIPQualityRepository(pool),
		NewPostgresSettingsRepository(pool),
		NewPostgresSubscriptionRepository(pool),
		NewPostgresAssetServiceRepository(pool),
		NewPostgresAssetDomainRepository(pool),
	)
	if err != nil {
		t.Fatalf("NewVPSOverviewRepository: %v", err)
	}
	return repository
}

func assertIPQualityAddressIdentityVPSPatchHistory(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vpsID string) {
	t.Helper()
	repository := NewPostgresVPSAssetRepository(pool)

	representation := "2001:0DB8:0:0:0:0:0:10"
	patched, err := repository.PatchVPSAsset(ctx, vpsID, vpsassets.PatchInput{IPv6: vpsassets.PatchString(representation)})
	if err != nil {
		t.Fatalf("PATCH equivalent IPv6 representation: %v", err)
	}
	if patched.IPv6 != representation {
		t.Fatalf("equivalent PATCH stored IPv6 = %q, want original text %q", patched.IPv6, representation)
	}
	assertIPQualityIdentityHistoryCount(t, ctx, pool, vpsID, 0)

	patch := func(value string) {
		t.Helper()
		if _, err := repository.PatchVPSAsset(ctx, vpsID, vpsassets.PatchInput{IPv6: vpsassets.PatchString(value)}); err != nil {
			t.Fatalf("PATCH IPv6 %q: %v", value, err)
		}
	}
	patch("2001:db8::11")
	// 新写入的非法地址会被拒绝；升级前已存入的非法文本用 SQL 模拟，修正与删除它仍要记史。
	if _, err := repository.PatchVPSAsset(ctx, vpsID, vpsassets.PatchInput{IPv6: vpsassets.PatchString("legacy-invalid-ip")}); !errors.Is(err, vpsassets.ErrInvalidVPSAssetInput) {
		t.Fatalf("PATCH malformed IPv6 = %v, want invalid input", err)
	}
	if _, err := pool.Exec(ctx, `update public.vps_assets set ipv6 = 'legacy-invalid-ip' where vps_id = $1`, vpsID); err != nil {
		t.Fatalf("seed legacy invalid IPv6: %v", err)
	}
	patch("2001:db8::12")
	patch("")
	// 存量非法文本也可以直接清空，清空同样要记史。
	if _, err := pool.Exec(ctx, `update public.vps_assets set ipv6 = 'legacy-invalid-ip' where vps_id = $1`, vpsID); err != nil {
		t.Fatalf("reseed legacy invalid IPv6: %v", err)
	}
	patch("")
	stored, err := repository.GetVPSAsset(ctx, vpsID)
	if err != nil {
		t.Fatalf("read patched VPS asset: %v", err)
	}
	if stored.IPv6 != "" {
		t.Fatalf("patched VPS IPv6 = %q, want deletion to preserve empty text", stored.IPv6)
	}

	type historyValue struct {
		fromIPv4 string
		toIPv4   string
		fromIPv6 string
		toIPv6   string
	}
	rows, err := pool.Query(ctx, `
		select from_ipv4, to_ipv4, from_ipv6, to_ipv6
		from public.ip_histories
		where vps_id = $1
		order by changed_at, created_at, ip_history_id`, vpsID)
	if err != nil {
		t.Fatalf("query VPS IP history: %v", err)
	}
	defer rows.Close()
	var got []historyValue
	for rows.Next() {
		var value historyValue
		if err := rows.Scan(&value.fromIPv4, &value.toIPv4, &value.fromIPv6, &value.toIPv6); err != nil {
			t.Fatalf("scan VPS IP history: %v", err)
		}
		got = append(got, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate VPS IP history: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("VPS IP history rows = %d, want 4 (no row for equivalent representation or the SQL-seeded legacy values)", len(got))
	}
	want := map[historyValue]bool{
		{fromIPv4: "198.51.100.61", toIPv4: "198.51.100.61", fromIPv6: representation, toIPv6: "2001:db8::11"}:      true,
		{fromIPv4: "198.51.100.61", toIPv4: "198.51.100.61", fromIPv6: "legacy-invalid-ip", toIPv6: "2001:db8::12"}: true,
		{fromIPv4: "198.51.100.61", toIPv4: "198.51.100.61", fromIPv6: "2001:db8::12", toIPv6: ""}:                  true,
		{fromIPv4: "198.51.100.61", toIPv4: "198.51.100.61", fromIPv6: "legacy-invalid-ip", toIPv6: ""}:             true,
	}
	for _, value := range got {
		if !want[value] {
			t.Fatalf("unexpected VPS IP history row = %#v", value)
		}
		delete(want, value)
	}
	if len(want) != 0 {
		t.Fatalf("missing VPS IP history rows = %#v", want)
	}
}

func assertIPQualityIdentityHistoryCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vpsID string, want int) {
	t.Helper()
	var got int
	if err := pool.QueryRow(ctx, `select count(*)::int from public.ip_histories where vps_id = $1`, vpsID).Scan(&got); err != nil {
		t.Fatalf("count VPS IP history: %v", err)
	}
	if got != want {
		t.Fatalf("VPS IP history count = %d, want %d", got, want)
	}
}

func assertIPQualityAddressIdentityOverviewDisabled(t *testing.T, ctx context.Context, pool *pgxpool.Pool, vpsID string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		insert into public.center_settings (settings_id, ip_quality_settings)
		values ('center', '{"enabled":false,"frequency_seconds":86400,"timeout_seconds":15,"stale_after_seconds":604800,"services":[]}'::jsonb)
		on conflict (settings_id) do update set ip_quality_settings = excluded.ip_quality_settings, updated_at = now()`); err != nil {
		t.Fatalf("disable center IP quality settings: %v", err)
	}
	repository := newIPQualityAddressIdentityOverviewRepository(t, pool)
	result, err := repository.LoadIPQuality(ctx, vpsID)
	if err != nil {
		t.Fatalf("LoadIPQuality disabled: %v", err)
	}
	if result.Status != "not_configured" || result.Section.State != vpsoverview.SectionReady {
		t.Fatalf("disabled Overview IP quality = %#v, want not_configured/ready without summary", result)
	}
}

type ipQualityAddressIdentityParserCase struct {
	name      string
	value     string
	wantValid bool
}

func assertIPQualityAddressIdentitySQLMatrix(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	base := "2001:db8::1"
	cases := []ipQualityAddressIdentityParserCase{
		{name: "empty", value: "", wantValid: false},
		{name: "space only", value: " \t\n ", wantValid: false},
		{name: "IPv4", value: "192.0.2.1", wantValid: true},
		{name: "IPv6 expanded", value: "2001:0db8:0:0:0:0:0:1", wantValid: true},
		{name: "IPv6 compressed uppercase", value: "2001:DB8::1", wantValid: true},
		{name: "IPv6 different", value: "2001:db8::2", wantValid: true},
		{name: "mapped IPv6 dotted", value: "::ffff:192.0.2.1", wantValid: true},
		{name: "mapped IPv6 hexadecimal", value: "0:0:0:0:0:ffff:c000:201", wantValid: true},
		{name: "CIDR", value: "192.0.2.1/32", wantValid: false},
		{name: "zone", value: "fe80::1%eth0", wantValid: false},
		{name: "port", value: "192.0.2.1:443", wantValid: false},
		{name: "short IPv4", value: "192.0.2", wantValid: false},
		{name: "leading zero IPv4", value: "192.0.2.01", wantValid: false},
		{name: "out of range IPv4", value: "192.0.2.256", wantValid: false},
		{name: "zero width space", value: "\u200b" + base, wantValid: false},
	}
	for _, whitespace := range []rune{
		'\t', '\n', '\v', '\f', '\r', ' ', '\u0085', '\u00a0', '\u1680',
		'\u2000', '\u2001', '\u2002', '\u2003', '\u2004', '\u2005', '\u2006',
		'\u2007', '\u2008', '\u2009', '\u200a', '\u2028', '\u2029', '\u202f',
		'\u205f', '\u3000',
	} {
		cases = append(cases, ipQualityAddressIdentityParserCase{
			name:      fmt.Sprintf("unicode whitespace U+%04X", whitespace),
			value:     string(whitespace) + base + string(whitespace),
			wantValid: true,
		})
	}

	values := make([]string, len(cases))
	for index, testCase := range cases {
		values[index] = testCase.value
		_, goValid := ipidentity.Parse(testCase.value)
		if goValid != testCase.wantValid {
			t.Fatalf("Go Parse(%q) valid = %t, want %t (%s)", testCase.value, goValid, testCase.wantValid, testCase.name)
		}
	}
	var nullIsNull bool
	if err := pool.QueryRow(ctx, `select public.houfeng_parse_host_address(null::text) is null`).Scan(&nullIsNull); err != nil {
		t.Fatalf("SQL parser NULL input: %v", err)
	}
	if !nullIsNull {
		t.Fatal("SQL parser NULL input is not NULL")
	}

	rows, err := pool.Query(ctx, `
		with inputs as (
			select value, ordinal::int as idx
			from unnest($1::text[]) with ordinality as input(value, ordinal)
		), parsed as (
			select idx, public.houfeng_parse_host_address(value) as parsed_addr
			from inputs
		), pairs as (
			select
				left_value.idx as left_idx,
				right_value.idx as right_idx,
				left_value.parsed_addr is not null as left_valid,
				right_value.parsed_addr is not null as right_valid,
				coalesce(left_value.parsed_addr = right_value.parsed_addr, false) as equal
			from parsed left_value
			cross join parsed right_value
		)
		select left_idx, right_idx, left_valid, right_valid, equal
		from pairs
		order by left_idx, right_idx`, values)
	if err != nil {
		t.Fatalf("SQL parser matrix query: %v", err)
	}
	defer rows.Close()
	pairCount := 0
	for rows.Next() {
		var leftIndex, rightIndex int
		var leftValid, rightValid, equal bool
		if err := rows.Scan(&leftIndex, &rightIndex, &leftValid, &rightValid, &equal); err != nil {
			t.Fatalf("scan SQL parser matrix row: %v", err)
		}
		if leftIndex < 1 || leftIndex > len(cases) || rightIndex < 1 || rightIndex > len(cases) {
			t.Fatalf("SQL parser matrix indexes = (%d,%d), cases=%d", leftIndex, rightIndex, len(cases))
		}
		leftCase := cases[leftIndex-1]
		rightCase := cases[rightIndex-1]
		if leftValid != leftCase.wantValid || rightValid != rightCase.wantValid {
			t.Fatalf("SQL parser validity for (%s,%s) = (%t,%t), want (%t,%t)", leftCase.name, rightCase.name, leftValid, rightValid, leftCase.wantValid, rightCase.wantValid)
		}
		leftAddress, leftOK := ipidentity.Parse(leftCase.value)
		rightAddress, rightOK := ipidentity.Parse(rightCase.value)
		wantEqual := leftOK && rightOK && leftAddress == rightAddress
		if equal != wantEqual {
			t.Fatalf("SQL parser equality for (%s,%s) = %t, want %t", leftCase.name, rightCase.name, equal, wantEqual)
		}
		pairCount++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate SQL parser matrix: %v", err)
	}
	if want := len(cases) * len(cases); pairCount != want {
		t.Fatalf("SQL parser matrix rows = %d, want %d", pairCount, want)
	}
}
