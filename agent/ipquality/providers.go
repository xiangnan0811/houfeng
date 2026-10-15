package ipquality

import (
	"context"
	"net/url"
	"strings"
	"time"

	"houfeng/internal/contracts/agentapi"
	"houfeng/internal/ipidentity"
	"houfeng/internal/ipqualitydata"
)

type jsonProviderSource struct {
	name       string
	urlForIP   func(string) string
	parse      func(map[string]any, string) providerSourceOutcome
	needsIP    bool
	sourceType string
}

func (s jsonProviderSource) Name() string { return s.name }

func (s jsonProviderSource) Collect(ctx context.Context, collector *HTTPCollector, targetIP string) providerSourceOutcome {
	startedAt := time.Now()
	if s.needsIP && strings.TrimSpace(targetIP) == "" {
		return sourceFailure(s.name, s.sourceType, "missing_target_ip", "source requires a canonical IP from an earlier provider", elapsedMillis(startedAt), nil)
	}
	if err := ctx.Err(); err != nil {
		return sourceFailure(s.name, s.sourceType, errorCodeForHTTPError(err), err.Error(), elapsedMillis(startedAt), nil)
	}
	payload, raw, err := collector.getJSON(ctx, s.urlForIP(targetIP))
	latency := elapsedMillis(startedAt)
	if err != nil {
		return sourceFailure(s.name, s.sourceType, errorCodeForHTTPError(err), err.Error(), latency, raw)
	}
	outcome := s.parse(payload, targetIP)
	outcome.Result.Provider = s.name
	if outcome.Result.Status == "" {
		outcome.Result.Status = sourceStatusSuccess
	}
	if outcome.Result.SourceType == "" {
		outcome.Result.SourceType = s.sourceType
	}
	outcome.Result.LatencyMS = latency
	outcome.Raw = raw
	outcome.Payload = payload
	if outcome.Result.Status != sourceStatusSuccess {
		return outcome
	}
	if len(outcome.Result.ExtraJSON) == 0 {
		outcome.Result.ExtraJSON = extraJSON(payload)
	}
	if strings.TrimSpace(outcome.IPAddress) == "" {
		outcome.IPAddress = stringFromMap(payload, "ip", "ip_address", "query")
	}
	address, validAddress := ipidentity.Parse(outcome.IPAddress)
	if !validAddress || address.IsUnspecified() {
		return sourceFailure(s.name, s.sourceType, "invalid_response", "invalid provider response", latency, raw)
	}
	if targetIP != "" {
		target, validTarget := ipidentity.Parse(targetIP)
		if !validTarget || address != target {
			failure := sourceFailure(s.name, s.sourceType, "ip_mismatch", "", latency, raw)
			failure.Result.ErrorSummary = "provider returned a different IP"
			return failure
		}
	}
	return outcome
}

func defaultProviderSources() (canonical []providerSource, targetDependent []providerSource) {
	return []providerSource{
		jsonProviderSource{
			name:       "ipapi.is",
			sourceType: sourceTypeDefault,
			urlForIP: func(ip string) string {
				if strings.TrimSpace(ip) == "" {
					return defaultLookupURL
				}
				return defaultLookupURL + "/?q=" + url.QueryEscape(strings.TrimSpace(ip))
			},
			parse: parseIPAPIISProvider,
		},
		jsonProviderSource{
			name:       "ipquery.io",
			sourceType: sourceTypeDefault,
			urlForIP: func(ip string) string {
				if strings.TrimSpace(ip) == "" {
					return "https://api.ipquery.io/?format=json"
				}
				return "https://api.ipquery.io/" + url.PathEscape(strings.TrimSpace(ip))
			},
			parse: parseIPQueryProvider,
		},
	}, []providerSource{
		jsonProviderSource{
			name:       "proxycheck.io",
			sourceType: sourceTypeDefault,
			needsIP:    true,
			urlForIP: func(ip string) string {
				return "https://proxycheck.io/v3/" + url.PathEscape(strings.TrimSpace(ip)) + "?ver=24-June-2026"
			},
			parse: parseProxycheckProvider,
		},
		jsonProviderSource{
			name:       "ip2location.io",
			sourceType: sourceTypeDefault,
			needsIP:    true,
			urlForIP: func(ip string) string {
				return "https://api.ip2location.io/?ip=" + url.QueryEscape(strings.TrimSpace(ip))
			},
			parse: parseIP2LocationProvider,
		},
		jsonProviderSource{
			name:       "ipwho.is",
			sourceType: sourceTypeDefault,
			needsIP:    true,
			urlForIP: func(ip string) string {
				return "https://ipwho.is/" + url.PathEscape(strings.TrimSpace(ip))
			},
			parse: parseIPWhoIsProvider,
		},
	}
}

func parseIPAPIISProvider(payload map[string]any, target string) providerSourceOutcome {
	return parsePublicProvider("ipapi.is", payload, target)
}

func parseIPQueryProvider(payload map[string]any, target string) providerSourceOutcome {
	return parsePublicProvider("ipquery.io", payload, target)
}

func parseProxycheckProvider(payload map[string]any, target string) providerSourceOutcome {
	return parsePublicProvider("proxycheck.io", payload, target)
}

func parseIP2LocationProvider(payload map[string]any, target string) providerSourceOutcome {
	return parsePublicProvider("ip2location.io", payload, target)
}

func parseIPWhoIsProvider(payload map[string]any, target string) providerSourceOutcome {
	return parsePublicProvider("ipwho.is", payload, target)
}

func parsePublicProvider(provider string, payload map[string]any, target string) providerSourceOutcome {
	result, err := ipqualitydata.ParseProvider(provider, payload, target, time.Now().UTC(), "agent_public", "anonymous")
	outcome := providerSourceOutcome{Result: result, Payload: payload}
	if err != nil {
		return outcome
	}
	// Provider parsing normalizes fields independently; validate the complete
	// row before any of its facts can enter a report or the durable queue.
	if err := agentapi.ValidateIPQualityProviderDetails(provider, result.Details, agentapi.IPQualityProductRevision, result.Details.QueriedIP); err != nil {
		return sourceFailure(provider, sourceTypeDefault, "invalid_response", "", nil, nil)
	}
	// Preserve the preferred discovery source's textual target for requests
	// and report identity. Details use the normalized IP for comparisons.
	outcome.IPAddress = stringFromMap(payload, "ip", "ip_address")
	if provider == "proxycheck.io" {
		outcome.IPAddress = strings.TrimSpace(target)
	}
	// Diagnostics retain safe, typed business fields rather than arbitrary API
	// echoes. Original responses remain isolated in the collector's raw envelope.
	capability := "geo_only"
	if result.IsProxy != nil || result.IsVPN != nil || result.IsTor != nil ||
		result.IsServer != nil || result.IsAbuser != nil || result.IsRobot != nil || len(result.Details.Scores) > 0 {
		capability = "risk"
	}
	evidence := map[string]any{"capability": capability, "details": result.Details}
	if provider == "proxycheck.io" {
		business := nestedMap(payload, target)
		detections := nestedMap(business, "detections")
		evidence["api_version"] = "24-June-2026"
		// These are independent diagnostics, never mapped to IsAbuser.
		evidence["compromised"] = detections["compromised"]
		evidence["anonymous"] = detections["anonymous"]
	}
	outcome.Result.ExtraJSON = extraJSON(evidence)
	return outcome
}

func successfulProviderOutcomes(outcomes []providerSourceOutcome) []providerSourceOutcome {
	out := make([]providerSourceOutcome, 0, len(outcomes))
	for _, outcome := range outcomes {
		if strings.EqualFold(strings.TrimSpace(outcome.Result.Status), sourceStatusSuccess) {
			out = append(out, outcome)
		}
	}
	return out
}

func applyReportFallbacksFromProviders(report *agentapi.IPQualityReportPayload, outcomes []providerSourceOutcome) {
	target, validTarget := ipidentity.Parse(report.IPAddress)
	if !validTarget || target.IsUnspecified() {
		return
	}
	var geography, network, registered bool
	report.RiskLevel = ""
	for _, provider := range []string{"ipapi.is", "ipquery.io", "ipwho.is", "proxycheck.io", "ip2location.io"} {
		for _, outcome := range outcomes {
			if outcome.Result.Provider != provider || outcome.Result.Status != sourceStatusSuccess {
				continue
			}
			address, validAddress := ipidentity.Parse(outcome.IPAddress)
			if !validAddress || address != target {
				continue
			}
			facts := reportFactsFromProvider(outcome)
			if !geography && (facts.regionCode != "" || facts.regionName != "" || facts.latitude != nil || facts.longitude != nil) {
				report.UseRegionCode, report.UseRegionName = facts.regionCode, facts.regionName
				report.Latitude, report.Longitude = facts.latitude, facts.longitude
				geography = true
			}
			if !network && (facts.asn != "" || facts.organization != "") {
				report.ASN, report.Organization = facts.asn, facts.organization
				network = true
			}
			if !registered && (facts.registeredRegionCode != "" || facts.registeredRegionName != "") {
				report.RegisteredRegionCode, report.RegisteredRegionName = facts.registeredRegionCode, facts.registeredRegionName
				registered = true
			}
		}
	}
}

func reportFactsFromProvider(outcome providerSourceOutcome) ipapiISFacts {
	if details := outcome.Result.Details; details != nil {
		organization := firstNonEmpty(details.ASName, details.CompanyName)
		if _, nestedASN := outcome.Payload["asn"].(map[string]any); !nestedASN && outcome.Result.Provider == "ipapi.is" {
			organization = firstNonEmpty(details.CompanyName, details.ASName)
		}
		return ipapiISFacts{
			ip: outcome.IPAddress, asn: details.ASN,
			organization: organization,
			regionCode:   details.CountryCode, regionName: outcome.Result.RegionName,
			registeredRegionCode: details.RegisteredCountryCode,
			latitude:             details.Latitude, longitude: details.Longitude,
		}
	}
	return ipapiISFacts{}
}

type ipapiISFacts struct {
	ip                   string
	asn                  string
	organization         string
	regionCode           string
	regionName           string
	registeredRegionCode string
	registeredRegionName string
	latitude             *float64
	longitude            *float64
}

func ipapiISFactsFromPayload(payload map[string]any) ipapiISFacts {
	outcome := parseIPAPIISProvider(payload, "")
	if outcome.Result.Status != sourceStatusSuccess {
		return ipapiISFacts{}
	}
	facts := reportFactsFromProvider(outcome)
	// This legacy report field remains separate from country codes and ASN
	// management country. It is never inferred from ASN data.
	facts.registeredRegionName = firstNonEmpty(stringFromMap(payload, "registered_region_name"), stringFromMap(payload, "registered_country_name"))
	return facts
}

func ipapiASNFromPayload(payload map[string]any) (string, string) {
	value := firstNonEmpty(
		stringFromNestedMap(payload, "asn", "asn", "as", "as_number"),
		stringFromMap(payload, "asn", "as", "as_number"),
	)
	org := firstNonEmpty(
		stringFromNestedMap(payload, "asn", "org", "organization", "name"),
		stringFromMap(payload, "asn_org"),
	)
	if value == "" {
		return "", org
	}
	asn, inlineOrg := normalizeASN(value)
	if org == "" {
		org = inlineOrg
	}
	return asn, org
}

func normalizeASN(value string) (string, string) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", ""
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return trimmed, ""
	}
	token := strings.TrimSpace(fields[0])
	upper := strings.ToUpper(token)
	digits := token
	if strings.HasPrefix(upper, "AS") {
		digits = token[2:]
	}
	if digits == "" {
		return trimmed, ""
	}
	for _, char := range digits {
		if char < '0' || char > '9' {
			return trimmed, ""
		}
	}
	return "AS" + digits, strings.TrimSpace(strings.TrimPrefix(trimmed, token))
}
