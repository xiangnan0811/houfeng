package vpsoverview

import (
	"houfeng/internal/contracts/agentapi"
	"testing"
)

func TestProductOverviewPartialRetainsRiskFacts(t *testing.T) {
	for _, test := range []struct {
		name    string
		summary *agentapi.IPQualityProductSummary
		want    bool
	}{
		{"VPN", &agentapi.IPQualityProductSummary{RiskFlags: []string{"vpn"}}, true},
		{"blacklist", &agentapi.IPQualityProductSummary{BlacklistListedCount: 1}, true},
		{"unknown", &agentapi.IPQualityProductSummary{}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot := Snapshot{VPSID: "vps_product", IPAvailable: true, IPStatus: "partial", IPRiskLevel: "high", IPProductRevision: 1, IPProductSummary: test.summary}
			found := false
			for _, anomaly := range EvaluateAnomalies(snapshot) {
				if anomaly.RuleID == RuleIPQualityRiskElevated {
					found = true
				}
			}
			if found != test.want {
				t.Fatalf("risk=%v want %v", found, test.want)
			}
			source := IPQualitySource{Status: "partial", RiskLevel: "high", ProductRevision: 1, ProductSummary: test.summary}
			status := ipQualitySummaryStatus(source)
			if (status == "risk") != test.want || status == "high" {
				t.Fatalf("overview status=%s", status)
			}
		})
	}
}
