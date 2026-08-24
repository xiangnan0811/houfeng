package vpsoverview

import (
	"testing"
	"time"
)

func TestEvaluateAnomaliesHealthyProducesEmptySlice(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	got := EvaluateAnomalies(Snapshot{
		GeneratedAt:           now,
		VPSID:                 "vps_7c2a4e18b09d5f31",
		MonitoringAvailable:   true,
		MonitoringHealth:      "正常",
		IPAvailable:           true,
		IPStatus:              "success",
		IPRiskLevel:           "low",
		SubscriptionAvailable: true,
		ActiveSubscriptions:   1,
		LifecycleStatus:       "active",
	})
	if got == nil {
		t.Fatal("healthy anomalies must be non-nil empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("healthy anomalies = %#v, want empty", got)
	}
}

func TestEvaluateAnomaliesTable(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	renew := now.Add(3 * 24 * time.Hour)
	observed := now.Add(-time.Hour)

	cases := []struct {
		name string
		in   Snapshot
		want string
	}{
		{
			name: "monitoring health",
			in: Snapshot{
				GeneratedAt: now, VPSID: "vps_7c2a4e18b09d5f31",
				MonitoringAvailable: true, MonitoringHealth: "告警", MonitoringObserved: &observed,
			},
			want: RuleMonitoringHealthAbnormal,
		},
		{
			name: "open incidents",
			in: Snapshot{
				GeneratedAt: now, VPSID: "vps_7c2a4e18b09d5f31",
				MonitoringAvailable: true, MonitoringHealth: "正常", ActiveIncidents: 2,
			},
			want: RuleMonitoringIncidentsOpen,
		},
		{
			name: "ip risk",
			in: Snapshot{
				GeneratedAt: now, VPSID: "vps_7c2a4e18b09d5f31",
				IPAvailable: true, IPRiskLevel: "high", IPObservedAt: &observed,
			},
			want: RuleIPQualityRiskElevated,
		},
		{
			name: "ip stale",
			in: Snapshot{
				GeneratedAt: now, VPSID: "vps_7c2a4e18b09d5f31",
				IPAvailable: true, IPStale: true,
			},
			want: RuleIPQualityStale,
		},
		{
			name: "ip partial",
			in: Snapshot{
				GeneratedAt: now, VPSID: "vps_7c2a4e18b09d5f31",
				IPAvailable: true, IPStatus: "partial",
			},
			want: RuleIPQualityPartial,
		},
		{
			name: "renewal due",
			in: Snapshot{
				GeneratedAt: now, VPSID: "vps_7c2a4e18b09d5f31",
				SubscriptionAvailable: true, ActiveSubscriptions: 1, NextRenewAt: &renew,
				LifecycleStatus: "active",
			},
			want: RuleRenewalDueSoon,
		},
		{
			name: "missing subscription",
			in: Snapshot{
				GeneratedAt: now, VPSID: "vps_7c2a4e18b09d5f31",
				SubscriptionAvailable: true, ActiveSubscriptions: 0, LifecycleStatus: "active",
			},
			want: RuleRenewalSubscriptionMissing,
		},
		{
			name: "lifecycle blocker",
			in: Snapshot{
				GeneratedAt: now, VPSID: "vps_7c2a4e18b09d5f31",
				LifecycleStatus: "to_cancel",
			},
			want: RuleLifecycleBlocker,
		},
		{
			name: "source unavailable",
			in: Snapshot{
				GeneratedAt: now, VPSID: "vps_7c2a4e18b09d5f31",
				JudgementSourcesUnavailable: []string{"monitoring"},
			},
			want: RuleSourceUnavailable,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := EvaluateAnomalies(tt.in)
			if len(got) == 0 {
				t.Fatal("expected at least one anomaly")
			}
			found := false
			for _, anomaly := range got {
				if anomaly.RuleID == tt.want {
					found = true
					if anomaly.Secondaries == nil {
						t.Fatal("secondary_actions must be non-nil")
					}
				}
			}
			if !found {
				t.Fatalf("anomalies = %#v, want rule %s", got, tt.want)
			}
		})
	}
}

func TestEvaluateAnomaliesActionDestinations(t *testing.T) {
	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	renew := now.Add(3 * 24 * time.Hour)
	const vpsID = "vps_7c2a4e18b09d5f31"

	tests := []struct {
		name        string
		snapshot    Snapshot
		ruleID      string
		actionID    string
		actionLabel string
		route       string
	}{
		{
			name: "monitoring health opens the abnormal monitoring work surface",
			snapshot: Snapshot{
				GeneratedAt: now, VPSID: vpsID, MonitoringAvailable: true, MonitoringHealth: "告警",
			},
			ruleID: RuleMonitoringHealthAbnormal, actionID: "open_monitoring",
			actionLabel: "查看监控", route: "/monitoring?abnormal=1",
		},
		{
			name: "open incidents opens monitoring instance events",
			snapshot: Snapshot{
				GeneratedAt: now, VPSID: vpsID, MonitoringAvailable: true,
				MonitoringHealth: "正常", ActiveIncidents: 1,
			},
			ruleID: RuleMonitoringIncidentsOpen, actionID: "open_incidents",
			actionLabel: "查看事件", route: "/events?object_type=monitoring_instance",
		},
		{
			name: "elevated IP risk opens the scoped IP quality route",
			snapshot: Snapshot{
				GeneratedAt: now, VPSID: vpsID, IPAvailable: true, IPStatus: "success", IPRiskLevel: "high",
			},
			ruleID: RuleIPQualityRiskElevated, actionID: "open_ip_quality",
			actionLabel: "查看 IP 质量", route: "/vps/" + vpsID + "/ip-quality",
		},
		{
			name: "stale IP evidence opens the scoped IP quality route",
			snapshot: Snapshot{
				GeneratedAt: now, VPSID: vpsID, IPAvailable: true, IPStatus: "success", IPStale: true,
			},
			ruleID: RuleIPQualityStale, actionID: "open_ip_quality",
			actionLabel: "查看 IP 质量", route: "/vps/" + vpsID + "/ip-quality",
		},
		{
			name: "partial IP evidence opens the scoped IP quality route",
			snapshot: Snapshot{
				GeneratedAt: now, VPSID: vpsID, IPAvailable: true, IPStatus: "partial",
			},
			ruleID: RuleIPQualityPartial, actionID: "open_ip_quality",
			actionLabel: "查看 IP 质量", route: "/vps/" + vpsID + "/ip-quality",
		},
		{
			name: "missing subscription is a page-owned command",
			snapshot: Snapshot{
				GeneratedAt: now, VPSID: vpsID, SubscriptionAvailable: true,
				LifecycleStatus: "active",
			},
			ruleID: RuleRenewalSubscriptionMissing, actionID: "open_subscription",
			actionLabel: "管理订阅",
		},
		{
			name: "renewal due is a page-owned decision command",
			snapshot: Snapshot{
				GeneratedAt: now, VPSID: vpsID, SubscriptionAvailable: true,
				ActiveSubscriptions: 1, NextRenewAt: &renew, LifecycleStatus: "active",
			},
			ruleID: RuleRenewalDueSoon, actionID: "open_renewal_decision",
			actionLabel: "查看续费",
		},
		{
			name:     "lifecycle blocker is a page-owned management command",
			snapshot: Snapshot{GeneratedAt: now, VPSID: vpsID, LifecycleStatus: "to_cancel"},
			ruleID:   RuleLifecycleBlocker, actionID: "open_management",
			actionLabel: "打开管理",
		},
		{
			name: "source unavailable is a page-owned refresh command",
			snapshot: Snapshot{
				GeneratedAt: now, VPSID: vpsID, JudgementSourcesUnavailable: []string{"monitoring"},
			},
			ruleID: RuleSourceUnavailable, actionID: "retry_overview",
			actionLabel: "重试",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			anomalies := EvaluateAnomalies(tt.snapshot)
			var got *AnomalyAction
			for index := range anomalies {
				if anomalies[index].RuleID == tt.ruleID {
					got = anomalies[index].Primary
					break
				}
			}
			if got == nil {
				t.Fatalf("rule %s action missing from %#v", tt.ruleID, anomalies)
			}
			if got.ID != tt.actionID || got.Label != tt.actionLabel || got.Route != tt.route {
				t.Fatalf("rule %s action = %#v, want id=%q label=%q route=%q", tt.ruleID, got, tt.actionID, tt.actionLabel, tt.route)
			}
		})
	}
}

func TestEvaluateAnomaliesMergesUnavailableSourcesIntoOneRow(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	got := EvaluateAnomalies(Snapshot{
		GeneratedAt:                 now,
		VPSID:                       "vps_7c2a4e18b09d5f31",
		JudgementSourcesUnavailable: []string{"renewal", "monitoring", "ip_quality"},
	})
	unavailable := 0
	var detail string
	for _, anomaly := range got {
		if anomaly.RuleID == RuleSourceUnavailable {
			unavailable++
			detail = anomaly.Detail
		}
	}
	if unavailable != 1 {
		t.Fatalf("source.unavailable rows = %d, want 1", unavailable)
	}
	if detail != "ip_quality, monitoring, renewal" {
		t.Fatalf("detail = %q, want sorted joined sources", detail)
	}
}

func TestEvaluateAnomaliesOrdersBySeverityThenRuleID(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	got := EvaluateAnomalies(Snapshot{
		GeneratedAt:                 now,
		VPSID:                       "vps_7c2a4e18b09d5f31",
		MonitoringAvailable:         true,
		MonitoringHealth:            "严重",
		IPAvailable:                 true,
		IPStale:                     true,
		JudgementSourcesUnavailable: []string{"renewal"},
	})
	if len(got) < 2 {
		t.Fatalf("got %d anomalies", len(got))
	}
	if got[0].RuleID != RuleMonitoringHealthAbnormal {
		t.Fatalf("first = %s, want critical monitoring", got[0].RuleID)
	}
}
