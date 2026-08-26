package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/assetlinks"
	"houfeng/internal/center/ipquality"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/subscriptions"
	"houfeng/internal/center/vpsassets"
	"houfeng/internal/center/vpsoverview"
)

type fakeIPQualityAvailability struct {
	enabled bool
	err     error
	calls   int
}

func (fake *fakeIPQualityAvailability) IPQualityEnabled(context.Context) (bool, error) {
	fake.calls++
	return fake.enabled, fake.err
}

func testOverviewRepository(
	t *testing.T,
	ip *fakeIPQuality,
	availability *fakeIPQualityAvailability,
) *VPSOverviewRepository {
	t.Helper()
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	repo, err := NewVPSOverviewRepository(
		&fakeVPSRepo{record: vpsassets.Record{
			VPSID: "vps_7c2a4e18b09d5f31", DisplayName: "Alpha", ProviderName: "Example",
			LifecycleStatus: vpsassets.LifecycleActive, RenewalDecision: vpsassets.RenewalKeep,
			Labels: []string{}, UpdatedAt: now,
		}},
		&fakeMonitoringLinks{links: []assetlinks.MonitoringInstanceSummary{{
			MonitoringInstanceID: "mi_1", CurrentHealthStatus: "正常", MonitoringStatus: "启用",
			LifecycleStatus: "在用", LastHeartbeatAt: &now,
		}}},
		ip,
		availability,
		&fakeSubscriptions{rows: []subscriptions.Record{{
			Status: subscriptions.StatusActive, UpdatedAt: now,
		}}},
		&fakeServices{},
		&fakeDomains{},
	)
	if err != nil {
		t.Fatalf("NewVPSOverviewRepository: %v", err)
	}
	return repo
}

func testOverviewServiceActor(t *testing.T) recordauth.ActorScope {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID:    "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
		Role:      recordauth.RoleProjectAdmin,
		ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("actor: %v", err)
	}
	return actor
}

func overviewRuleIDs(overview vpsoverview.Overview) []string {
	ids := make([]string, 0, len(overview.Anomalies))
	for _, anomaly := range overview.Anomalies {
		ids = append(ids, anomaly.RuleID)
	}
	return ids
}

func TestLoadIPQualityDisabledWithoutReportIsNotConfigured(t *testing.T) {
	t.Parallel()

	repo := testOverviewRepository(t, &fakeIPQuality{}, &fakeIPQualityAvailability{enabled: false})
	got, err := repo.LoadIPQuality(context.Background(), "vps_7c2a4e18b09d5f31")
	if err != nil {
		t.Fatalf("LoadIPQuality: %v", err)
	}
	if got.Status != "not_configured" || got.RiskLevel != "" || got.Stale {
		t.Fatalf("ip quality = %#v, want not_configured without leftover judgement fields", got)
	}
	if got.Section.State != vpsoverview.SectionReady || got.Section.ReasonCode != "" {
		t.Fatalf("section = %#v, want ready without historical note", got.Section)
	}
}

func TestLoadIPQualityDisabledLeftoverHighRiskDoesNotJudge(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	repo := testOverviewRepository(t, &fakeIPQuality{report: ipquality.VPSReport{Summary: &ipquality.Summary{
		Status: "partial", RiskLevel: "high", Stale: true, ObservedAt: now,
	}}}, &fakeIPQualityAvailability{enabled: false})
	got, err := repo.LoadIPQuality(context.Background(), "vps_7c2a4e18b09d5f31")
	if err != nil {
		t.Fatalf("LoadIPQuality: %v", err)
	}
	if got.Status != "not_configured" || got.RiskLevel != "" || got.Stale {
		t.Fatalf("ip quality = %#v, leftover report must not drive current judgement", got)
	}
	if got.Section.ReasonCode != "ip_quality_disabled_has_history" {
		t.Fatalf("reason = %q, want ip_quality_disabled_has_history", got.Section.ReasonCode)
	}
}

func TestLoadIPQualityEnabledWithoutReportIsMissing(t *testing.T) {
	t.Parallel()

	repo := testOverviewRepository(t, &fakeIPQuality{}, &fakeIPQualityAvailability{enabled: true})
	got, err := repo.LoadIPQuality(context.Background(), "vps_7c2a4e18b09d5f31")
	if err != nil {
		t.Fatalf("LoadIPQuality: %v", err)
	}
	if got.Status != "missing" {
		t.Fatalf("status = %q, want missing", got.Status)
	}
}

func TestLoadIPQualityEnabledUsesLatestSummaryOnly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	ip := &fakeIPQuality{summary: &ipquality.Summary{
		Status: "success", RiskLevel: "low", ObservedAt: now,
	}}
	repo := testOverviewRepository(t, ip, &fakeIPQualityAvailability{enabled: true})
	got, err := repo.LoadIPQuality(context.Background(), "vps_7c2a4e18b09d5f31")
	if err != nil {
		t.Fatalf("LoadIPQuality: %v", err)
	}
	if ip.fullReportCalls != 0 {
		t.Fatalf("GetVPSIPQuality calls = %d, want 0 (summary-only)", ip.fullReportCalls)
	}
	if ip.summaryCalls != 1 {
		t.Fatalf("GetLatestVPSIPQualitySummary calls = %d, want 1", ip.summaryCalls)
	}
	if got.Status != "success" || got.RiskLevel != "low" {
		t.Fatalf("ip quality = %#v", got)
	}
}

func TestOverviewServiceWiresRepositoryAvailabilityNotEvaluatorStub(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	cases := []struct {
		name        string
		enabled     bool
		summary     *ipquality.Summary
		wantRule    string
		forbid      []string
		wantOverall string
	}{
		{
			name:        "default disabled without report",
			enabled:     false,
			wantOverall: "healthy",
			forbid:      []string{vpsoverview.RuleIPQualityMissing},
		},
		{
			name:    "disabled leftover high-risk",
			enabled: false,
			summary: &ipquality.Summary{Status: "partial", RiskLevel: "high", Stale: true, ObservedAt: now},
			forbid: []string{
				vpsoverview.RuleIPQualityMissing,
				vpsoverview.RuleIPQualityStale,
				vpsoverview.RuleIPQualityPartial,
				vpsoverview.RuleIPQualityRiskElevated,
			},
			wantOverall: "healthy",
		},
		{
			name:        "enabled without report",
			enabled:     true,
			wantRule:    vpsoverview.RuleIPQualityMissing,
			wantOverall: "notice",
		},
	}
	for _, test := range cases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repo := testOverviewRepository(t, &fakeIPQuality{
				summary: test.summary,
				report:  ipquality.VPSReport{Summary: test.summary},
			}, &fakeIPQualityAvailability{enabled: test.enabled})
			service, err := vpsoverview.NewServiceWithClock(
				repo,
				&overviewActivityLister{},
				func() time.Time { return now },
				time.Second,
			)
			if err != nil {
				t.Fatalf("NewServiceWithClock: %v", err)
			}
			overview, err := service.Get(context.Background(), vpsoverview.Request{
				Actor: testOverviewServiceActor(t), VPSID: "vps_7c2a4e18b09d5f31",
			})
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			rules := overviewRuleIDs(overview)
			if test.wantRule != "" {
				found := false
				for _, rule := range rules {
					if rule == test.wantRule {
						found = true
					}
				}
				if !found {
					t.Fatalf("anomalies = %#v, want %s", rules, test.wantRule)
				}
			}
			for _, forbidden := range test.forbid {
				for _, rule := range rules {
					if rule == forbidden {
						t.Fatalf("anomalies = %#v, must not emit %s", rules, forbidden)
					}
				}
			}
			if overview.Summary.Overall.Status != test.wantOverall {
				t.Fatalf("overall = %s anomalies=%#v, want %s", overview.Summary.Overall.Status, rules, test.wantOverall)
			}
			if overview.Summary.IPQuality.Status == "high" || overview.Summary.IPQuality.Detail == "partial" {
				t.Fatalf("summary leaked leftover judgement fields: %#v", overview.Summary.IPQuality)
			}
		})
	}
}

type overviewActivityLister struct{}

func (overviewActivityLister) List(context.Context, activity.ListRequest) (activity.ListResult, error) {
	return activity.ListResult{Items: []activity.Event{}, Freshness: activity.Freshness{State: "ready"}}, nil
}

func TestPostgresSettingsRepositoryIPQualityEnabledIsCheapExtract(t *testing.T) {
	t.Parallel()

	var sqls []string
	repo := &PostgresSettingsRepository{db: fakeSettingsQueryer{
		queryRow: func(_ context.Context, sql string, args ...any) pgx.Row {
			sqls = append(sqls, sql)
			if len(args) != 1 {
				t.Fatalf("args = %#v, want settings singleton id", args)
			}
			return fakeSettingsRow{scan: func(dest ...any) error {
				*(dest[0].(*bool)) = false
				return nil
			}}
		},
	}}
	enabled, err := repo.IPQualityEnabled(context.Background())
	if err != nil {
		t.Fatalf("IPQualityEnabled: %v", err)
	}
	if enabled {
		t.Fatal("default/disabled extract must be false")
	}
	if len(sqls) != 1 {
		t.Fatalf("queries = %#v, want exactly one cheap extract", sqls)
	}
	joined := strings.ToLower(sqls[0])
	if strings.Contains(joined, "telegram_bot_token") || strings.Contains(joined, getCenterSettingsSQL) {
		t.Fatalf("loaded full settings document: %s", sqls[0])
	}
	if !strings.Contains(joined, "ip_quality_settings") || !strings.Contains(joined, "enabled") {
		t.Fatalf("sql = %s, want enabled extract from ip_quality_settings", sqls[0])
	}
}
