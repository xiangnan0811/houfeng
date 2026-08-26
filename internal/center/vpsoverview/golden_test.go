package vpsoverview

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"houfeng/internal/center/activity"
)

func goldenMachineEnumOverview() Overview {
	ready := SectionState{State: SectionReady}
	generated := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 8, 20, 8, 58, 0, 0, time.UTC)
	snapshot := Snapshot{
		GeneratedAt:           generated,
		VPSID:                 "vps_001",
		MonitoringAvailable:   true,
		MonitoringStatus:      "unlinked",
		MonitoringDetail:      "未关联监控实例",
		IPAvailable:           true,
		IPStatus:              "missing",
		SubscriptionAvailable: true,
		ActiveSubscriptions:   1,
		LifecycleStatus:       "active",
		RenewalDecision:       "keep",
	}
	return Overview{
		GeneratedAt: generated,
		Identity: Identity{
			VPSID: "vps_001", DisplayName: "Tokyo Edge", ProviderName: "Example Cloud",
			ProductName: "VPS", Country: "JP", Region: "Tokyo", City: "Tokyo", Datacenter: "TK1",
			IPv4: "192.0.2.10", LifecycleStatus: "active", UsageStatus: "in_use",
			RenewalDecision: "keep", Importance: "high", Labels: []string{"edge"},
			UpdatedAt: updated,
		},
		Anomalies: EvaluateAnomalies(snapshot),
		Summary: Summary{
			Overall:    SummaryCell{Status: "notice", Section: ready},
			Monitoring: SummaryCell{Status: "unlinked", Detail: "未关联监控实例", Section: ready},
			IPQuality:  SummaryCell{Status: "missing", Detail: "missing", Section: ready},
			Renewal:    SummaryCell{Status: "keep", Section: ready},
		},
		RecentActivity: ActivitySection{Section: ready, Items: []activity.Event{}},
		Facts:          []Fact{{Key: "ipv4", Label: "IPv4", Value: "192.0.2.10"}},
		Relations: []RelationSummary{
			{Kind: "monitoring_instances", Count: 0, Status: "unlinked", Label: "监控实例", Section: ready},
			{Kind: "subscriptions", Count: 1, Route: "/subscriptions?vps_id=vps_001", Label: "订阅", Section: ready},
			{Kind: "services", Count: 0, Label: "服务", Section: ready},
			{Kind: "domains", Count: 0, Label: "域名", Section: ready},
		},
		Capabilities: []string{CapabilityRecordsV2Read},
	}
}

func TestGoldenOverviewMachineEnumsWire(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "overview.machine-enums.v1.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	got, err := json.Marshal(goldenMachineEnumOverview())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var wantCompact, gotCompact bytes.Buffer
	if err := json.Compact(&wantCompact, want); err != nil {
		t.Fatalf("compact golden: %v", err)
	}
	if err := json.Compact(&gotCompact, got); err != nil {
		t.Fatalf("compact got: %v", err)
	}
	if wantCompact.String() != gotCompact.String() {
		t.Fatalf("golden mismatch\ngot:  %s\nwant: %s", gotCompact.String(), wantCompact.String())
	}
}
