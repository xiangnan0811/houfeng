package ipquality

import (
	"encoding/json"
	"testing"

	"houfeng/internal/contracts/agentapi"
)

func geoTestProvider(provider, method, use, registered string) ProviderResultRead {
	tier := "anonymous"
	if method == "center_api" {
		tier = "authenticated"
	}
	return ProviderResultRead{Provider: provider, Status: "success", Details: &agentapi.IPQualityProviderDetails{
		QueriedIP: "8.8.8.8", Method: method, APITier: tier, CountryCode: use, RegisteredCountryCode: registered,
	}}
}

func geoTestFallback(use, comparison string) ProviderResultRead {
	row := geoTestProvider("ipinfo", "agent_public", use, "")
	row.ExtraJSON, _ = json.Marshal(map[string]any{"geo_classification": map[string]any{
		"basis": "abuse_contact_country", "country_code": use, "comparison_country_code": comparison,
		"upstream_commit": "2384a67c756eb35231f5982b34731e522be3653e", "fork_revision": 1,
	}})
	return row
}

func TestIPTypeSameSourcePriorityAndConflict(t *testing.T) {
	for _, test := range []struct {
		name                    string
		rows                    []ProviderResultRead
		status, basis, provider string
	}{
		{"no evidence", nil, "unknown", "", ""},
		{"anonymous registered", []ProviderResultRead{geoTestProvider("maxmind", "agent_public", "US", "US")}, "native", "registered_country", "maxmind"},
		{"registered abroad", []ProviderResultRead{geoTestProvider("maxmind", "agent_public", "US", "JP")}, "broadcast", "registered_country", "maxmind"},
		{"abuse fallback", []ProviderResultRead{geoTestFallback("US", "US")}, "native", "abuse_contact_country", "ipinfo"},
		{"abuse abroad", []ProviderResultRead{geoTestFallback("US", "JP")}, "broadcast", "abuse_contact_country", "ipinfo"},
		{"incomplete fallback", []ProviderResultRead{geoTestFallback("US", "")}, "unknown", "", ""},
		{"never mix country sources", []ProviderResultRead{geoTestProvider("maxmind", "agent_public", "US", ""), geoTestProvider("ipapi.is", "agent_public", "", "US")}, "unknown", "", ""},
		{"registered outranks fallback", []ProviderResultRead{geoTestFallback("US", "US"), geoTestProvider("maxmind", "agent_public", "US", "JP")}, "broadcast", "registered_country", "maxmind"},
		{"authenticated outranks anonymous", []ProviderResultRead{geoTestProvider("maxmind", "agent_public", "US", "JP"), geoTestProvider("maxmind", "center_api", "US", "US")}, "native", "registered_country", "maxmind"},
		{"same tier conflict", []ProviderResultRead{geoTestProvider("maxmind", "agent_public", "US", "US"), geoTestProvider("ipapi.is", "agent_public", "US", "JP")}, "conflict", "registered_country", ""},
		{"same tier agrees", []ProviderResultRead{geoTestProvider("maxmind", "agent_public", "US", "US"), geoTestProvider("ipapi.is", "agent_public", "US", "US")}, "native", "registered_country", "ipapi.is"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := classifyIPType("8.8.8.8", test.rows)
			if got.Status != test.status || got.Basis != test.basis || got.Provider != test.provider {
				t.Fatalf("classification=%+v", got)
			}
		})
	}
}

func TestIPTypeCountriesKeepUsageAndComparisonMeaning(t *testing.T) {
	for _, row := range []ProviderResultRead{
		geoTestProvider("maxmind", "center_api", "US", "JP"),
		geoTestFallback("US", "JP"),
	} {
		got := classifyIPType("8.8.8.8", []ProviderResultRead{row})
		if got.Status != "broadcast" || got.CountryCode != "US" || got.ComparisonCountryCode != "JP" {
			t.Fatalf("usage/registration orientation reversed: %+v", got)
		}
	}
}

func TestIPTypeCannotBorrowFailedOtherFamilyOrRegistrationEvidence(t *testing.T) {
	failed := geoTestProvider("maxmind", "center_api", "US", "US")
	failed.Status = "failure"
	other := geoTestProvider("maxmind", "agent_public", "US", "US")
	other.Details.QueriedIP = "2001:4860:4860::8888"
	for _, rows := range [][]ProviderResultRead{{failed}, {other}} {
		if got := classifyIPType("8.8.8.8", rows); got.Status != "unknown" {
			t.Fatalf("invalid source classified=%+v", got)
		}
	}
	view := BuildProductView(ReportWrite{IPAddress: "8.8.8.8"}, &IntelligenceSnapshot{
		Registration: agentapi.IPQualityRegistration{Status: "success", NetworkCountryCode: "US"}, ASN: agentapi.IPQualityASN{CountryCode: "US"},
	})
	if view.Summary.IPType.Status != "unknown" {
		t.Fatal("RDAP or ASN country became IP type")
	}
	fallback := geoTestFallback("US", "US")
	if fallback.Details.RegisteredCountryCode != "" {
		t.Fatal("abuse contact masqueraded as registered country")
	}
}

func TestIPTypeNewSummaryKeepsProductRevisionOne(t *testing.T) {
	view := BuildProductView(ReportWrite{IPAddress: "8.8.8.8", Product: &agentapi.IPQualityAgentProduct{Revision: 1}}, nil)
	if view.Revision != 1 || view.Summary.IPType == nil || view.Summary.IPType.Status != "unknown" {
		t.Fatal("new product missing explicit unknown IP type")
	}
}

func TestProductHighRiskLabelsAreExplicitAndIPScoped(t *testing.T) {
	for _, test := range []struct {
		label, scope string
		high         bool
	}{
		{"High", "ip", true}, {" vErY hIgH ", "ip", true}, {"Critical", "ip", true},
		{"Elevated", "ip", false}, {"Very High", "asn", false}, {"Critical", "company_network", false},
	} {
		view := BuildProductView(ReportWrite{ProviderResults: []ProviderResultWrite{{Provider: "ipapi.is", Status: "success", Details: &agentapi.IPQualityProviderDetails{
			Scores: []agentapi.IPQualityScore{{SourceLabel: test.label, Scope: test.scope}},
		}}}}, nil)
		if (len(view.Summary.HighRiskSources) != 0) != test.high {
			t.Fatalf("label=%s scope=%s high=%v", test.label, test.scope, view.Summary.HighRiskSources)
		}
	}
}
