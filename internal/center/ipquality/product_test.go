package ipquality

import (
	"encoding/json"
	"reflect"
	"testing"

	"houfeng/internal/contracts/agentapi"
)

func TestProductGeographyRequiresNetworkCountryAndIndependentSources(t *testing.T) {
	for _, test := range []struct{ name, network, first, second, expected string }{
		{"asn country cannot replace network", "", "US", "US", "unknown"},
		{"same country", "US", "US", "US", "consistent"},
		{"different country", "US", "JP", "JP", "discrepant"},
		{"conflict", "US", "US", "JP", "conflict"},
	} {
		t.Run(test.name, func(t *testing.T) {
			report := ReportWrite{Product: &agentapi.IPQualityAgentProduct{Revision: 1}, ProviderResults: []ProviderResultWrite{
				{Provider: "ipapi.is", Status: "success", Details: &agentapi.IPQualityProviderDetails{CountryCode: test.first}},
				{Provider: "ipquery.io", Status: "success", Details: &agentapi.IPQualityProviderDetails{CountryCode: test.second}},
			}}
			snapshot := &IntelligenceSnapshot{Registration: agentapi.IPQualityRegistration{Status: "success", NetworkCountryCode: test.network}, ASN: agentapi.IPQualityASN{CountryCode: "US"}}
			view := BuildProductView(report, snapshot)
			if view.Summary.GeoStatus != test.expected {
				t.Fatalf("geo=%s want %s", view.Summary.GeoStatus, test.expected)
			}
		})
	}
}

func TestProductFactsSurviveMailFailureAndEnhancedFailure(t *testing.T) {
	positive := true
	report := ReportWrite{Status: "partial", Product: &agentapi.IPQualityAgentProduct{Revision: 1, PTRNames: []string{"historical.example"}, Mail: []agentapi.IPQualityMailResult{{Provider: "gmail", Status: "timeout"}}}, ProviderResults: []ProviderResultWrite{
		{Provider: "ipapi.is", Status: "success", IsVPN: &positive, Details: &agentapi.IPQualityProviderDetails{Method: "agent_public", CountryCode: "US", Scores: []agentapi.IPQualityScore{{Scope: "company_network", SourceLabel: "High"}}}},
	}, ServiceUnlocks: []ServiceUnlockWrite{
		{Service: "chatgpt", ProbeStatus: "success", Checks: []agentapi.IPQualityServiceCheck{{ID: "web", Status: "available"}, {ID: "app", Status: "unknown"}}},
		{Service: "netflix", ProbeStatus: "success", Checks: []agentapi.IPQualityServiceCheck{{ID: "title_81280792", Status: "restricted"}, {ID: "title_70143836", Status: "restricted"}}},
	}}
	snapshot := &IntelligenceSnapshot{Providers: []ProviderResultRead{{Provider: "ipapi.is", Status: "failure", Details: &agentapi.IPQualityProviderDetails{Method: "center_api", CountryCode: "JP"}}}, Blacklists: []agentapi.IPQualityBlacklistResult{{Status: "listed"}, {Status: "unknown"}}, SourceOutcomes: []SourceOutcome{{Status: "failure"}}}
	view := BuildProductView(report, snapshot)
	if len(view.Summary.RiskFlags) != 1 || view.Summary.RiskFlags[0] != "vpn" || len(view.Summary.HighRiskSources) != 0 {
		t.Fatalf("risk facts: %+v", view.Summary)
	}
	if view.Summary.ServicePartialCount != 2 || view.Summary.ServiceRestrictedCount != 0 || len(view.Summary.RestrictedServices) != 0 {
		t.Fatalf("service scope: %+v", view.Summary)
	}
	if view.Summary.BlacklistCheckedCount != 1 || view.Summary.BlacklistListedCount != 1 {
		t.Fatalf("unknown counted clean: %+v", view.Summary)
	}
	if view.PTRNames[0] != "historical.example" || view.State != "failed" {
		t.Fatalf("historical identity/state: %+v", view)
	}
}

func TestProductAuthenticatedSuccessSelectsOneVendor(t *testing.T) {
	positive := true
	report := ReportWrite{ProviderResults: []ProviderResultWrite{{Provider: "ipapi.is", Status: "success", IsVPN: &positive, Details: &agentapi.IPQualityProviderDetails{Method: "agent_public", CountryCode: "US"}}}}
	snapshot := &IntelligenceSnapshot{Providers: []ProviderResultRead{{Provider: "ipapi.is", Status: "success", Details: &agentapi.IPQualityProviderDetails{Method: "center_api", CountryCode: "US"}}}, Registration: agentapi.IPQualityRegistration{Status: "success", NetworkCountryCode: "US"}}
	view := BuildProductView(report, snapshot)
	if len(view.Summary.RiskFlags) != 0 || view.Summary.GeoStatus != "unknown" {
		t.Fatalf("duplicate vendor facts: %+v", view.Summary)
	}
}

func TestProductServiceSourcesAreOrderIndependent(t *testing.T) {
	sample := func(status, probeStatus string) ServiceUnlockWrite {
		return ServiceUnlockWrite{Service: "chatgpt", Status: status, ProbeStatus: probeStatus}
	}
	for _, test := range []struct {
		name       string
		rows       []ServiceUnlockWrite
		counts     [4]int // available, partial, restricted, unknown
		restricted []string
	}{
		{
			name: "failure and unknown cannot mask available",
			rows: []ServiceUnlockWrite{
				sample("blocked", "failure"), sample("unknown", "success"), sample("unlocked", "success"),
			},
			counts: [4]int{1, 0, 0, 0},
		},
		{
			name: "failure and unknown cannot mask blocked",
			rows: []ServiceUnlockWrite{
				sample("unlocked", "failure"), sample("unknown", "success"), sample("blocked", "success"),
			},
			counts: [4]int{0, 0, 1, 0}, restricted: []string{"chatgpt"},
		},
		{
			name: "opposing successful sources are partial",
			rows: []ServiceUnlockWrite{
				sample("unlocked", "success"), sample("blocked", "success"), sample("unknown", "success"),
			},
			counts: [4]int{0, 1, 0, 0}, restricted: []string{"chatgpt"},
		},
		{
			name: "partial survives available and blocked",
			rows: []ServiceUnlockWrite{
				sample("partial", "success"), sample("unlocked", "success"), sample("blocked", "success"),
			},
			counts: [4]int{0, 1, 0, 0}, restricted: []string{"chatgpt"},
		},
		{
			name: "partial survives unknown",
			rows: []ServiceUnlockWrite{
				sample("partial", "success"), sample("unknown", "success"), sample("blocked", "failure"),
			},
			counts: [4]int{0, 1, 0, 0},
		},
		{
			name:   "available sources count once",
			rows:   []ServiceUnlockWrite{sample("unlocked", "success"), sample("unlocked", "success")},
			counts: [4]int{1, 0, 0, 0},
		},
		{
			name:   "blocked sources count once",
			rows:   []ServiceUnlockWrite{sample("blocked", "success"), sample("blocked", "success")},
			counts: [4]int{0, 0, 1, 0}, restricted: []string{"chatgpt"},
		},
		{
			name: "only unknown and failures count once as unknown",
			rows: []ServiceUnlockWrite{
				sample("unknown", "success"), sample("blocked", "failure"), sample("unlocked", "failure"),
			},
			counts: [4]int{0, 0, 0, 1},
		},
		{
			name:   "only failures are unknown",
			rows:   []ServiceUnlockWrite{sample("blocked", "failure"), sample("unlocked", "failure")},
			counts: [4]int{0, 0, 0, 1},
		},
		{
			name: "skipped and unconfigured sources do not count",
			rows: []ServiceUnlockWrite{sample("blocked", "skipped"), sample("unlocked", "not_configured")},
		},
		{
			name: "typed blocked checks override legacy available status",
			rows: []ServiceUnlockWrite{
				{Service: "chatgpt", Status: "unlocked", ProbeStatus: "success", Checks: []agentapi.IPQualityServiceCheck{
					{ID: "web", Status: "restricted"}, {ID: "app", Status: "restricted"},
				}},
				sample("unknown", "success"),
			},
			counts: [4]int{0, 0, 1, 0}, restricted: []string{"chatgpt"},
		},
		{
			name: "typed unknown checks cannot mask conclusive source",
			rows: []ServiceUnlockWrite{
				{Service: "chatgpt", Status: "blocked", ProbeStatus: "success", Checks: []agentapi.IPQualityServiceCheck{
					{ID: "web", Status: "unknown"}, {ID: "app", Status: "unknown"},
				}},
				sample("unlocked", "success"),
			},
			counts: [4]int{1, 0, 0, 0},
		},
		{
			name: "legacy Netflix sample denials are partial not service blocked",
			rows: []ServiceUnlockWrite{
				{Service: "netflix", Status: "blocked", ProbeStatus: "success", Checks: []agentapi.IPQualityServiceCheck{
					{ID: "title_81280792", Status: "restricted"}, {ID: "title_70143836", Status: "restricted"},
				}},
				{Service: "netflix", Status: "unknown", ProbeStatus: "success"},
			},
			counts: [4]int{0, 1, 0, 0},
		},
		{
			name: "restrictions are sorted including conflicting service",
			rows: []ServiceUnlockWrite{
				{Service: "youtube-premium", Status: "blocked", ProbeStatus: "success"},
				{Service: "disney-plus", Status: "blocked", ProbeStatus: "success"},
				sample("unlocked", "success"), sample("blocked", "success"),
			},
			counts: [4]int{0, 1, 2, 0}, restricted: []string{"chatgpt", "disney-plus", "youtube-premium"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows := append([]ServiceUnlockWrite(nil), test.rows...)
			for i := range rows {
				rows[i].Source = string(rune('a' + i))
			}
			wantRestricted := test.restricted
			if wantRestricted == nil {
				wantRestricted = []string{}
			}
			var permute func(int)
			permute = func(index int) {
				if index < len(rows) {
					for i := index; i < len(rows); i++ {
						rows[index], rows[i] = rows[i], rows[index]
						permute(index + 1)
						rows[index], rows[i] = rows[i], rows[index]
					}
					return
				}
				before, err := json.Marshal(rows)
				if err != nil {
					t.Fatal(err)
				}
				report := ReportWrite{ServiceUnlocks: rows}
				initial := BuildProductView(report, nil).Summary
				final := BuildProductView(report, &IntelligenceSnapshot{State: "complete"}).Summary
				if !reflect.DeepEqual(initial, final) {
					t.Fatalf("initial/final service facts differ for %s: %+v / %+v", before, initial, final)
				}
				gotCounts := [4]int{initial.ServiceAvailableCount, initial.ServicePartialCount, initial.ServiceRestrictedCount, initial.ServiceUnknownCount}
				if gotCounts != test.counts || !reflect.DeepEqual(initial.RestrictedServices, wantRestricted) {
					t.Fatalf("sources %s: counts=%v restricted=%v, want %v / %v", before, gotCounts, initial.RestrictedServices, test.counts, wantRestricted)
				}
				after, err := json.Marshal(rows)
				if err != nil {
					t.Fatal(err)
				}
				if string(before) != string(after) {
					t.Fatalf("summary projection changed source evidence: before=%s after=%s", before, after)
				}
			}
			permute(0)
		})
	}
}
