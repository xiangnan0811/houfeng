package assetdecisions

import (
	"houfeng/internal/center/ipquality"
	"houfeng/internal/center/vpsassets"
	"houfeng/internal/contracts/agentapi"
	"strings"
	"testing"
)

func TestProductPartialRiskSurvivesEnrichmentState(t *testing.T) {
	for _, state := range []string{"pending", "running", "failed", "skipped", "partial", "complete"} {
		t.Run(state, func(t *testing.T) {
			input := fact("product_vps", "Product", "pv_1", "Provider", "Japan", "", "Tokyo", vpsassets.UsageInUse, sub("product_sub", 10, 120))
			input.VPS.IPQualitySummary = &ipquality.Summary{ProductRevision: 1, Status: "partial", IntelligenceState: state, RiskLevel: "low", ProductSummary: &agentapi.IPQualityProductSummary{RiskFlags: []string{"vpn"}, BlacklistListedCount: 1, RestrictedServices: []string{"reddit"}}}
			member := buildMember(input, ListFilters{RenewWithinDays: 30})
			if !hasEvidence(member, EvidenceIPQualityRisk) || !hasEvidence(member, EvidenceMediaUnlockBlocked) {
				t.Fatalf("known product facts suppressed: %+v", member.EvidenceChips)
			}
			if strings.Contains(ipQualityRiskDetails(input), "risk=low") {
				t.Fatal("product detail inherited obsolete rating")
			}
			input.VPS.IPQualitySummary.Ambiguous = true
			member = buildMember(input, ListFilters{RenewWithinDays: 30})
			if hasEvidence(member, EvidenceIPQualityRisk) || hasEvidence(member, EvidenceMediaUnlockBlocked) {
				t.Fatal("ambiguous ownership became risk")
			}
		})
	}
}

func TestProductIgnoresLegacyRiskWithoutFacts(t *testing.T) {
	input := fact("product_vps", "Product", "pv_1", "Provider", "Japan", "", "Tokyo", vpsassets.UsageInUse, sub("product_sub", 10, 120))
	input.VPS.IPQualitySummary = &ipquality.Summary{ProductRevision: 1, Status: "partial", RiskLevel: "high", ProductSummary: &agentapi.IPQualityProductSummary{}}
	input.IPQualityProviderRiskSignalCount = 3
	if ipQualityRiskSignal(input) {
		t.Fatal("product used legacy rating")
	}
}
