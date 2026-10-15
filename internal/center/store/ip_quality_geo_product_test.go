package store

import (
	"encoding/json"
	"testing"
	"time"

	"houfeng/internal/center/ipquality"
	centersettings "houfeng/internal/center/settings"
	"houfeng/internal/contracts/agentapi"
)

func TestIPTypeSummaryIsPersistedOnlyAtWriteAndNotRecomputedForHistory(t *testing.T) {
	now := time.Now().UTC()
	provider := ipquality.ProviderResultWrite{Provider: "maxmind", Status: "success", Details: &agentapi.IPQualityProviderDetails{
		QueriedIP: "8.8.8.8", ObservedAt: now, Method: "agent_public", APITier: "anonymous", CountryCode: "US", RegisteredCountryCode: "US",
	}}
	report := ipquality.ReportWrite{IPAddress: "8.8.8.8", IPVersion: 4, ObservedAt: now, Product: &agentapi.IPQualityAgentProduct{Revision: 1}, ProviderResults: []ipquality.ProviderResultWrite{provider}}
	prepared, err := prepareIPQualityProductWrite(report)
	if err != nil {
		t.Fatal(err)
	}
	var inserted agentapi.IPQualityProductSummary
	if err := json.Unmarshal(prepared.summaryJSON, &inserted); err != nil {
		t.Fatal(err)
	}
	if inserted.IPType == nil || inserted.IPType.Status != "native" {
		t.Fatal("insert did not freeze IP type")
	}
	for _, state := range []string{"pending", "failed", "complete", "partial", "skipped"} {
		legacy := []byte(`{"risk_flags":[],"high_risk_sources":[],"restricted_services":[],"geo_status":"unknown"}`)
		row := ipQualityStoredProductRow{reportID: "historical", ipAddress: "8.8.8.8", ipVersion: 4, observedAt: now,
			productJSON: prepared.productJSON, agentSummaryJSON: legacy, intelligenceState: &state,
			snapshotJSON: []byte(`{"providers":[],"blacklists":[]}`), intelligenceSummaryJSON: legacy}
		view, ok, err := buildStoredIPQualityProduct(row, []ipquality.ProviderResultWrite{provider})
		if err != nil || !ok || view.Summary.IPType != nil {
			t.Fatalf("old persisted %s summary was reclassified: %+v %v", state, view.Summary, err)
		}
		row.agentSummaryJSON = prepared.summaryJSON
		row.intelligenceSummaryJSON = prepared.summaryJSON
		provider.Details.RegisteredCountryCode = "JP"
		view, ok, err = buildStoredIPQualityProduct(row, []ipquality.ProviderResultWrite{provider})
		if err != nil || !ok || view.Summary.IPType == nil || view.Summary.IPType.Status != "native" {
			t.Fatalf("stored %s classification changed from live provider data", state)
		}
	}
}

func TestMaxMindCredentialRevocationDropsKeyedSnapshotAndCache(t *testing.T) {
	const account, key = "12345678", "private-license-key"
	original := centersettings.Default().IPQuality
	original.Sources.MaxMindAccountID, original.Sources.MaxMindLicenseKey = account, key
	fp := centersettings.MaxMindCredentialFingerprint(account, key)
	if intelligenceCredentials(original)["maxmind"] != fp {
		t.Fatal("collector/store pair fingerprint differs")
	}
	for _, change := range []string{"account", "key", "clear"} {
		current := original
		switch change {
		case "account":
			current.Sources.MaxMindAccountID = "87654321"
		case "key":
			current.Sources.MaxMindLicenseKey = "new-license-key"
		case "clear":
			current.Sources.MaxMindAccountID, current.Sources.MaxMindLicenseKey = "", ""
		}
		result := ipquality.IntelligenceSnapshot{
			CredentialFingerprints: map[string]string{"maxmind": fp},
			Providers:              []ipquality.ProviderResultRead{{Provider: "maxmind", Status: "success"}},
			CacheWrites:            []ipquality.SourceCacheEntry{{SourceID: "maxmind", CredentialFingerprint: fp}},
			SourceOutcomes:         []ipquality.SourceOutcome{{SourceID: "maxmind", Status: "success", ObservedAt: time.Now().UTC()}},
		}
		revokeIPQualityIntelligenceCredentials(&result, intelligenceCredentials(current))
		if len(result.Providers) != 0 || len(result.CacheWrites) != 0 || result.SourceOutcomes[0].ErrorCode != "settings_changed" {
			t.Fatalf("%s rotation retained revoked GeoLite facts", change)
		}
	}
}
