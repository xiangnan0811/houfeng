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
