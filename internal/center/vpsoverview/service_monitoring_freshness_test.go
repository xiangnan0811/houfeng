package vpsoverview

import (
	"context"
	"testing"
	"time"

	"houfeng/internal/center/activity"
)

func TestServiceMonitoringFirstHeartbeatIsReadyUnknownWithoutRetryAnomaly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name   string
		status string
	}{
		{name: "ordinary pending", status: "启用"},
		{name: "paused", status: "暂停"},
		{name: "maintenance", status: "维护中"},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			sources := &fakeSources{bundle: SourceBundle{
				Identity: Identity{
					VPSID: "vps_7c2a4e18b09d5f31", DisplayName: "Alpha",
					LifecycleStatus: "active", RenewalDecision: "keep", Labels: []string{}, UpdatedAt: now,
				},
				MonitoringSection: SectionState{
					State:      SectionReady,
					ReasonCode: "monitoring_first_heartbeat_missing",
				},
				MonitoringHealth:    "unknown",
				MonitoringStatus:    test.status,
				MonitoringDetail:    "等待首次心跳",
				IPSection:           SectionState{State: SectionReady},
				IPStatus:            "not_configured",
				RenewalSection:      SectionState{State: SectionReady},
				ActiveSubscriptions: 1,
				Facts:               []Fact{},
				Relations:           []RelationSummary{},
			}}
			activityLister := &fakeActivity{result: activity.ListResult{
				Items: []activity.Event{}, Freshness: activity.Freshness{State: "ready"},
			}}
			service, err := NewServiceWithClock(sources, activityLister, func() time.Time { return now }, time.Second)
			if err != nil {
				t.Fatalf("NewServiceWithClock: %v", err)
			}

			overview, err := service.Get(context.Background(), Request{
				Actor: testOverviewActor(t), VPSID: "vps_7c2a4e18b09d5f31",
			})
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			for _, anomaly := range overview.Anomalies {
				switch anomaly.RuleID {
				case RuleSourceUnavailable, RuleMonitoringHealthAbnormal:
					t.Fatalf("anomalies = %#v, must not treat missing heartbeat as an actionable monitoring failure", overview.Anomalies)
				}
			}
			if overview.Summary.Overall.Status != "healthy" {
				t.Fatalf("overall status = %q, want healthy for missing first heartbeat", overview.Summary.Overall.Status)
			}
			if overview.Summary.Monitoring.Status != "unknown" {
				t.Fatalf("monitoring summary status = %q, want unknown", overview.Summary.Monitoring.Status)
			}
			if overview.Summary.Monitoring.Detail != "等待首次心跳" {
				t.Fatalf("monitoring summary detail = %q, want waiting detail", overview.Summary.Monitoring.Detail)
			}
			if overview.Summary.Monitoring.Section.State != SectionReady || overview.Summary.Monitoring.Section.ReasonCode != "monitoring_first_heartbeat_missing" {
				t.Fatalf("monitoring section = %#v, want ready with first-heartbeat reason", overview.Summary.Monitoring.Section)
			}
			for _, relation := range overview.Relations {
				if relation.Kind == "monitoring_instances" {
					if relation.Status == "正常" {
						t.Fatalf("monitoring relation = %#v, must not label missing heartbeat normal", relation)
					}
					if relation.Status != "unknown" {
						t.Fatalf("monitoring relation status = %q, want unknown", relation.Status)
					}
				}
			}
		})
	}
}

func TestServiceMonitoringKnownAdverseHealthAndIncidentsRemainActionable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 20, 8, 0, 0, 0, time.UTC)
	observed := now.Add(-time.Minute)
	sources := &fakeSources{bundle: SourceBundle{
		Identity: Identity{
			VPSID: "vps_7c2a4e18b09d5f31", DisplayName: "Alpha",
			LifecycleStatus: "active", RenewalDecision: "keep", Labels: []string{}, UpdatedAt: now,
		},
		MonitoringSection:   SectionState{State: SectionReady, ObservedAt: &observed},
		MonitoringHealth:    "告警",
		MonitoringStatus:    "启用",
		MonitoringDetail:    "健康检查失败",
		ActiveIncidents:     1,
		IPSection:           SectionState{State: SectionReady},
		IPStatus:            "not_configured",
		RenewalSection:      SectionState{State: SectionReady},
		ActiveSubscriptions: 1,
		Facts:               []Fact{},
		Relations:           []RelationSummary{},
	}}
	activityLister := &fakeActivity{result: activity.ListResult{
		Items: []activity.Event{}, Freshness: activity.Freshness{State: "ready"},
	}}
	service, err := NewServiceWithClock(sources, activityLister, func() time.Time { return now }, time.Second)
	if err != nil {
		t.Fatalf("NewServiceWithClock: %v", err)
	}

	overview, err := service.Get(context.Background(), Request{
		Actor: testOverviewActor(t), VPSID: "vps_7c2a4e18b09d5f31",
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	foundHealth, foundIncident := false, false
	for _, anomaly := range overview.Anomalies {
		switch anomaly.RuleID {
		case RuleMonitoringHealthAbnormal:
			foundHealth = true
		case RuleMonitoringIncidentsOpen:
			foundIncident = true
		}
	}
	if !foundHealth || !foundIncident {
		t.Fatalf("anomalies = %#v, want known adverse health and open-incident warnings", overview.Anomalies)
	}
	if overview.Summary.Overall.Status != "attention" {
		t.Fatalf("overall status = %q, want attention for known adverse monitoring evidence", overview.Summary.Overall.Status)
	}
}
