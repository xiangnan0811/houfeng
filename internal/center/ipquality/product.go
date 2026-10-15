package ipquality

import (
	"encoding/json"
	"sort"
	"strings"

	"houfeng/internal/contracts/agentapi"
	"houfeng/internal/ipqualitydata"
)

// BuildProductView projects only the selected report and its one-time snapshot.
// It performs no IO and never borrows facts from another address or family.
func BuildProductView(report ReportWrite, snapshot *IntelligenceSnapshot) IPQualityProductView {
	view := IPQualityProductView{
		Revision: 1, IPAddress: report.IPAddress, IPVersion: report.IPVersion,
		State: "skipped", AgentObservedAt: report.ObservedAt,
		PTRNames: []string{}, Providers: []ProviderResultRead{}, Mail: []agentapi.IPQualityMailResult{},
		Blacklists:   []agentapi.IPQualityBlacklistResult{},
		Registration: agentapi.IPQualityRegistration{Status: "unknown"},
		ASN:          agentapi.IPQualityASN{Status: "unknown", Origins: []string{}},
		Summary:      agentapi.IPQualityProductSummary{RiskFlags: []string{}, HighRiskSources: []string{}, RestrictedServices: []string{}, GeoStatus: "unknown"},
	}
	if report.Product != nil {
		view.PTRNames = append(view.PTRNames, report.Product.PTRNames...)
		view.Mail = append(view.Mail, report.Product.Mail...)
	}
	for _, row := range report.ProviderResults {
		view.Providers = append(view.Providers, ProviderResultRead{
			Provider: row.Provider, Status: row.Status, SourceType: row.SourceType, LatencyMS: row.LatencyMS,
			UsageType: row.UsageType, CompanyType: row.CompanyType, RiskLevel: row.RiskLevel, RiskScore: row.RiskScore,
			RegionCode: row.RegionCode, RegionName: row.RegionName, IsProxy: row.IsProxy, IsTor: row.IsTor,
			IsVPN: row.IsVPN, IsServer: row.IsServer, IsAbuser: row.IsAbuser, IsRobot: row.IsRobot,
			ErrorCode: row.ErrorCode, ErrorSummary: row.ErrorSummary, ExtraJSON: row.ExtraJSON, Details: row.Details,
		})
	}
	if snapshot != nil {
		view.State = snapshot.State
		if view.State == "" {
			view.State = IntelligenceState(*snapshot)
		}
		view.CompletedAt = snapshot.CompletedAt
		view.Providers = append(view.Providers, snapshot.Providers...)
		view.Blacklists = append(view.Blacklists, snapshot.Blacklists...)
		view.Registration, view.ASN = snapshot.Registration, snapshot.ASN
		if view.ASN.Origins == nil {
			view.ASN.Origins = []string{}
		}
	}
	selected := SelectedProductProviders(view.Providers)
	flags, high := map[string]bool{}, map[string]bool{}
	countries := map[string]int{}
	for _, row := range selected {
		if row.Status != "success" {
			continue
		}
		for _, flag := range []struct {
			name  string
			value *bool
		}{
			{"proxy", row.IsProxy}, {"vpn", row.IsVPN}, {"tor", row.IsTor}, {"abuse", row.IsAbuser}, {"robot", row.IsRobot},
		} {
			if flag.value != nil && *flag.value {
				flags[flag.name] = true
			}
		}
		if row.Details == nil {
			continue
		}
		country := strings.ToUpper(strings.TrimSpace(row.Details.CountryCode))
		if country != "" {
			countries[country]++
		}
		for _, score := range row.Details.Scores {
			label := strings.ToLower(strings.TrimSpace(score.SourceLabel))
			if score.Scope == "ip" && (label == "high" || label == "very high" || label == "critical") {
				high[row.Provider] = true
			}
		}
	}
	for flag := range flags {
		view.Summary.RiskFlags = append(view.Summary.RiskFlags, flag)
	}
	for source := range high {
		view.Summary.HighRiskSources = append(view.Summary.HighRiskSources, source)
	}
	sort.Strings(view.Summary.RiskFlags)
	sort.Strings(view.Summary.HighRiskSources)
	if len(countries) > 1 {
		view.Summary.GeoStatus = "conflict"
	} else if view.Registration.Status == "success" && view.Registration.NetworkCountryCode != "" {
		for country, count := range countries {
			if count < 2 {
				continue
			}
			view.Summary.GeoStatus = "discrepant"
			if strings.EqualFold(country, view.Registration.NetworkCountryCode) {
				view.Summary.GeoStatus = "consistent"
			}
		}
	}
	view.Summary.IPType = classifyIPType(report.IPAddress, view.Providers)
	for _, row := range view.Blacklists {
		if row.Status == "listed" {
			view.Summary.BlacklistListedCount++
		}
		if row.Status == "listed" || row.Status == "not_listed" {
			view.Summary.BlacklistCheckedCount++
		}
	}
	type serviceEvidence struct {
		available  bool
		restricted bool
		partial    bool
	}
	services := make(map[string]serviceEvidence, len(report.ServiceUnlocks))
	for _, row := range report.ServiceUnlocks {
		if row.ProbeStatus == "skipped" || row.ProbeStatus == "not_configured" {
			continue
		}
		evidence := services[row.Service]
		if row.ProbeStatus != "success" {
			services[row.Service] = evidence
			continue
		}
		status := row.Status
		if len(row.Checks) > 0 {
			status, _ = ipqualitydata.AggregateServiceChecks(row.Service, row.Checks)
		}
		// Normalize each source independently before combining service facts.
		// Unknown and failed sources cannot erase another source's conclusion.
		switch status {
		case "unlocked":
			evidence.available = true
		case "partial":
			evidence.partial = true
		case "blocked":
			evidence.restricted = true
		}
		services[row.Service] = evidence
	}
	for service, evidence := range services {
		switch {
		case evidence.partial || (evidence.available && evidence.restricted):
			view.Summary.ServicePartialCount++
		case evidence.available:
			view.Summary.ServiceAvailableCount++
		case evidence.restricted:
			view.Summary.ServiceRestrictedCount++
		default:
			view.Summary.ServiceUnknownCount++
		}
		if evidence.restricted {
			view.Summary.RestrictedServices = append(view.Summary.RestrictedServices, service)
		}
	}
	sort.Strings(view.Summary.RestrictedServices)
	return view
}

// SelectedProductProviders selects a successful authenticated sample when present,
// without treating two methods from one vendor as independent databases.
func SelectedProductProviders(rows []ProviderResultRead) []ProviderResultRead {
	result := make([]ProviderResultRead, 0, len(rows))
	indices := make(map[string]int, len(rows))
	for _, row := range rows {
		index, exists := indices[row.Provider]
		if !exists {
			indices[row.Provider] = len(result)
			result = append(result, row)
			continue
		}
		current := result[index]
		if row.Status == "success" && (current.Status != "success" || (row.Details != nil && row.Details.Method == "center_api")) {
			result[index] = row
		}
	}
	return result
}

func IntelligenceState(snapshot IntelligenceSnapshot) string {
	success, failure := 0, 0
	for _, outcome := range snapshot.SourceOutcomes {
		if outcome.Status == "success" {
			success++
		}
		if outcome.Status == "failure" {
			failure++
		}
	}
	if failure > 0 && success > 0 {
		return "partial"
	}
	if failure > 0 {
		return "failed"
	}
	if success > 0 {
		return "complete"
	}
	return "skipped"
}

// ProductHasRisk tests explicit facts, never collection completeness or a score band.
func ProductHasRisk(summary *agentapi.IPQualityProductSummary) bool {
	return summary != nil && (len(summary.RiskFlags) > 0 || len(summary.HighRiskSources) > 0 || summary.BlacklistListedCount > 0)
}

// classifyIPType compares complete pairs from a single successful source.
// An incomplete registered pair never borrows the other half from RDAP or a
// different vendor. All candidates remain in Providers for provenance details.
func classifyIPType(target string, rows []ProviderResultRead) *agentapi.IPQualityIPType {
	type candidate struct {
		tier  int
		value agentapi.IPQualityIPType
	}
	var candidates []candidate
	for _, row := range rows {
		d := row.Details
		if row.Status != "success" || d == nil || d.QueriedIP != target {
			continue
		}
		use, registered := country(d.CountryCode), country(d.RegisteredCountryCode)
		if use != "" && registered != "" {
			tier := 1
			if d.Method == "center_api" && d.APITier == "authenticated" {
				tier = 0
			} else if d.Method != "agent_public" || d.APITier != "anonymous" {
				continue
			}
			candidates = append(candidates, candidate{tier, agentapi.IPQualityIPType{
				Basis: "registered_country", Provider: row.Provider, Method: d.Method,
				CountryCode: use, ComparisonCountryCode: registered,
			}})
			continue
		}
		if row.Provider != "ipinfo" || d.Method != "agent_public" || d.APITier != "anonymous" {
			continue
		}
		var extra struct {
			GeoClassification agentapi.IPQualityIPType `json:"geo_classification"`
		}
		if json.Unmarshal(row.ExtraJSON, &extra) != nil {
			continue
		}
		g := extra.GeoClassification
		comparison := country(g.ComparisonCountryCode)
		if g.Basis != "abuse_contact_country" || use == "" || country(g.CountryCode) != use || comparison == "" ||
			g.UpstreamCommit != "2384a67c756eb35231f5982b34731e522be3653e" || g.ForkRevision != 1 {
			continue
		}
		candidates = append(candidates, candidate{2, agentapi.IPQualityIPType{
			Basis: g.Basis, Provider: row.Provider, Method: d.Method, CountryCode: use,
			ComparisonCountryCode: comparison, UpstreamCommit: g.UpstreamCommit, ForkRevision: g.ForkRevision,
		}})
	}
	if len(candidates) == 0 {
		return &agentapi.IPQualityIPType{Status: "unknown"}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].tier != candidates[j].tier {
			return candidates[i].tier < candidates[j].tier
		}
		return candidates[i].value.Provider < candidates[j].value.Provider
	})
	result := candidates[0].value
	result.Status = "native"
	if result.CountryCode != result.ComparisonCountryCode {
		result.Status = "broadcast"
	}
	for _, c := range candidates[1:] {
		if c.tier != candidates[0].tier {
			break
		}
		if c.value.CountryCode != result.CountryCode || c.value.ComparisonCountryCode != result.ComparisonCountryCode {
			// Do not present an arbitrarily selected country pair as authoritative.
			return &agentapi.IPQualityIPType{Status: "conflict", Basis: result.Basis}
		}
	}
	return &result
}
