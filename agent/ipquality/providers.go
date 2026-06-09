package ipquality

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"houfeng/internal/contracts/agentapi"
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
	payload, raw, err := collector.getJSON(ctx, s.urlForIP(targetIP))
	latency := elapsedMillis(startedAt)
	if err != nil {
		return sourceFailure(s.name, s.sourceType, errorCodeForHTTPError(err), err.Error(), latency, raw)
	}
	outcome := s.parse(payload, targetIP)
	outcome.Result.Provider = s.name
	outcome.Result.Status = sourceStatusSuccess
	outcome.Result.SourceType = s.sourceType
	outcome.Result.LatencyMS = latency
	outcome.Raw = raw
	if len(outcome.Result.ExtraJSON) == 0 {
		outcome.Result.ExtraJSON = extraJSON(payload)
	}
	if strings.TrimSpace(outcome.IPAddress) == "" {
		outcome.IPAddress = stringFromMap(payload, "ip", "ip_address", "query")
	}
	outcome.Payload = payload
	return outcome
}

func defaultProviderSources() []providerSource {
	return []providerSource{
		jsonProviderSource{
			name:       "ipapi.is",
			sourceType: sourceTypeDefault,
			urlForIP:   func(string) string { return defaultLookupURL },
			parse:      parseIPAPIISProvider,
		},
		jsonProviderSource{
			name:       "ipquery.io",
			sourceType: sourceTypeDefault,
			urlForIP: func(ip string) string {
				if strings.TrimSpace(ip) == "" {
					return "https://api.ipquery.io/"
				}
				return "https://api.ipquery.io/" + url.PathEscape(strings.TrimSpace(ip))
			},
			parse: parseIPQueryProvider,
		},
		jsonProviderSource{
			name:       "proxycheck.io",
			sourceType: sourceTypeDefault,
			needsIP:    true,
			urlForIP: func(ip string) string {
				return "https://proxycheck.io/v2/" + url.PathEscape(strings.TrimSpace(ip)) + "?vpn=1&asn=1&risk=1"
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

func optionalProviderDiagnostics() []agentapi.IPQualityProviderResultPayload {
	optional := []string{
		"maxmind",
		"ipinfo",
		"ipregistry",
		"ipdata",
		"ipqualityscore",
		"abuseipdb",
		"scamalytics",
		"ipapi.co",
		"ip-api.com",
		"db-ip",
		"ippure",
		"meowvps",
		"ip.net.coffee",
	}
	out := make([]agentapi.IPQualityProviderResultPayload, 0, len(optional))
	for _, provider := range optional {
		out = append(out, agentapi.IPQualityProviderResultPayload{
			Provider:     provider,
			Status:       sourceStatusNotConfigured,
			SourceType:   sourceTypeOptional,
			ErrorCode:    "not_configured",
			ErrorSummary: "optional IP quality source requires configuration or is not enabled for default agent collection",
		})
	}
	return out
}

func parseIPAPIISProvider(payload map[string]any, _ string) providerSourceOutcome {
	result := providerResultFromMap(payload)
	result.Provider = "ipapi.is"
	result.ExtraJSON = extraJSON(map[string]any{
		"asn":          payload["asn"],
		"company":      payload["company"],
		"location":     payload["location"],
		"risk_flags":   pickKeys(payload, "is_datacenter", "is_tor", "is_proxy", "is_vpn", "is_abuser", "is_crawler"),
		"abuser_score": payload["abuser_score"],
	})
	return providerSourceOutcome{Result: result, IPAddress: stringFromMap(payload, "ip", "ip_address")}
}

func parseIPQueryProvider(payload map[string]any, _ string) providerSourceOutcome {
	risk := nestedMap(payload, "risk")
	isp := nestedMap(payload, "isp")
	location := nestedMap(payload, "location")
	result := agentapi.IPQualityProviderResultPayload{
		UsageType:   stringFromNestedMap(payload, "risk", "usage_type"),
		CompanyType: stringFromNestedMap(payload, "company", "type"),
		RiskScore:   stringFromMap(risk, "risk_score", "score"),
		RegionCode:  firstNonEmpty(stringFromMap(location, "country_code"), stringFromMap(payload, "country_code")),
		RegionName:  firstNonEmpty(stringFromMap(location, "country"), stringFromMap(payload, "country")),
		IsProxy:     boolFromMap(risk, "is_proxy", "proxy"),
		IsTor:       boolFromMap(risk, "is_tor", "tor"),
		IsVPN:       boolFromMap(risk, "is_vpn", "vpn"),
		IsServer:    boolFromMap(risk, "is_datacenter", "datacenter", "is_server"),
		IsAbuser:    boolFromMap(risk, "is_abuser", "abuser"),
		IsRobot:     boolFromMap(risk, "is_robot", "is_crawler", "bot"),
		ExtraJSON: extraJSON(map[string]any{
			"risk":     risk,
			"isp":      isp,
			"location": location,
		}),
	}
	if result.RiskScore != "" {
		result.RiskLevel = riskLevelFromScore(result.RiskScore)
	}
	return providerSourceOutcome{Result: result, IPAddress: stringFromMap(payload, "ip", "ip_address")}
}

func parseProxycheckProvider(payload map[string]any, targetIP string) providerSourceOutcome {
	ipPayload := nestedMap(payload, targetIP)
	if ipPayload == nil {
		for key, value := range payload {
			if validIPAddress(key) {
				if child, ok := value.(map[string]any); ok {
					ipPayload = child
					targetIP = key
					break
				}
			}
		}
	}
	if ipPayload == nil {
		ipPayload = payload
	}
	proxy := boolFromMap(ipPayload, "proxy")
	result := agentapi.IPQualityProviderResultPayload{
		UsageType:  stringFromMap(ipPayload, "type"),
		RiskScore:  stringFromMap(ipPayload, "risk"),
		RegionCode: stringFromMap(ipPayload, "isocode", "country_code"),
		RegionName: stringFromMap(ipPayload, "country"),
		IsProxy:    proxy,
		IsVPN:      boolFromMap(ipPayload, "vpn"),
		IsTor:      boolFromMap(ipPayload, "tor"),
		IsServer:   boolFromMap(ipPayload, "hosting", "datacenter"),
		IsAbuser:   boolFromMap(ipPayload, "abuse"),
		IsRobot:    boolFromMap(ipPayload, "bot"),
		ExtraJSON:  extraJSON(ipPayload),
	}
	if result.RiskScore != "" {
		result.RiskLevel = riskLevelFromScore(result.RiskScore)
	}
	return providerSourceOutcome{Result: result, IPAddress: targetIP}
}

func parseIP2LocationProvider(payload map[string]any, targetIP string) providerSourceOutcome {
	result := providerResultFromMap(payload)
	result.Provider = "ip2location.io"
	result.RegionCode = firstNonEmpty(result.RegionCode, stringFromMap(payload, "country_code"))
	result.RegionName = firstNonEmpty(result.RegionName, stringFromMap(payload, "country_name"))
	result.IsProxy = firstBool(result.IsProxy, boolFromMap(payload, "is_proxy", "proxy"))
	result.IsVPN = firstBool(result.IsVPN, boolFromMap(payload, "is_vpn", "vpn"))
	result.IsTor = firstBool(result.IsTor, boolFromMap(payload, "is_tor", "tor"))
	result.IsServer = firstBool(result.IsServer, boolFromMap(payload, "is_datacenter", "is_data_center", "hosting"))
	result.ExtraJSON = extraJSON(payload)
	return providerSourceOutcome{Result: result, IPAddress: firstNonEmpty(stringFromMap(payload, "ip", "ip_address"), targetIP)}
}

func parseIPWhoIsProvider(payload map[string]any, targetIP string) providerSourceOutcome {
	result := agentapi.IPQualityProviderResultPayload{
		RegionCode:  stringFromMap(payload, "country_code"),
		RegionName:  stringFromMap(payload, "country"),
		UsageType:   stringFromNestedMap(payload, "connection", "type"),
		CompanyType: stringFromNestedMap(payload, "connection", "type"),
		ExtraJSON: extraJSON(map[string]any{
			"asn":        payload["asn"],
			"isp":        payload["isp"],
			"org":        payload["org"],
			"connection": payload["connection"],
		}),
	}
	return providerSourceOutcome{Result: result, IPAddress: firstNonEmpty(stringFromMap(payload, "ip"), targetIP)}
}

func successfulProviderOutcomes(outcomes []providerSourceOutcome) []providerSourceOutcome {
	out := make([]providerSourceOutcome, 0, len(outcomes))
	for _, outcome := range outcomes {
		if outcome.Result.Status == sourceStatusSuccess {
			out = append(out, outcome)
		}
	}
	return out
}

func preferredProviderOutcome(outcomes []providerSourceOutcome) providerSourceOutcome {
	order := map[string]int{
		"ipapi.is":       0,
		"ipquery.io":     1,
		"ipwho.is":       2,
		"proxycheck.io":  3,
		"ip2location.io": 4,
	}
	best := outcomes[0]
	bestRank := 999
	for _, outcome := range outcomes {
		rank, ok := order[outcome.Result.Provider]
		if !ok {
			rank = 900
		}
		if rank < bestRank {
			best = outcome
			bestRank = rank
		}
	}
	return best
}

func applyReportFallbacksFromProviders(report *agentapi.IPQualityReportPayload, outcomes []providerSourceOutcome) {
	for _, outcome := range outcomes {
		payload := outcome.Payload
		if report.ASN == "" {
			report.ASN = asnStringFromPayload(payload)
		}
		if report.Organization == "" {
			report.Organization = firstNonEmpty(
				stringFromMap(payload, "organization", "org", "isp"),
				stringFromNestedMap(payload, "asn", "org", "organization", "name"),
				stringFromNestedMap(payload, "isp", "org", "organization", "name"),
				stringFromNestedMap(payload, "connection", "org"),
			)
		}
		if report.UseRegionCode == "" {
			report.UseRegionCode = firstNonEmpty(stringFromMap(payload, "country_code"), stringFromNestedMap(payload, "location", "country_code"))
		}
		if report.UseRegionName == "" {
			report.UseRegionName = firstNonEmpty(stringFromMap(payload, "country", "country_name"), stringFromNestedMap(payload, "location", "country"))
		}
		if report.Latitude == nil {
			report.Latitude = firstFloatPtr(floatPtrFromMap(payload, "latitude", "lat"), floatPtrFromNestedMap(payload, "location", "latitude", "lat"))
		}
		if report.Longitude == nil {
			report.Longitude = firstFloatPtr(floatPtrFromMap(payload, "longitude", "lon", "lng"), floatPtrFromNestedMap(payload, "location", "longitude", "lon", "lng"))
		}
		if report.RiskLevel == "" {
			report.RiskLevel = outcome.Result.RiskLevel
		}
	}
}

func pickKeys(payload map[string]any, keys ...string) map[string]any {
	out := map[string]any{}
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			out[key] = value
		}
	}
	return out
}

func firstBool(values ...*bool) *bool {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func firstFloatPtr(values ...*float64) *float64 {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func riskLevelFromScore(score string) string {
	trimmed := strings.TrimSpace(score)
	if trimmed == "" {
		return ""
	}
	var n int
	if _, err := fmt.Sscanf(trimmed, "%d", &n); err != nil {
		return ""
	}
	if n >= 80 {
		return "high"
	}
	if n >= 40 {
		return "medium"
	}
	return "low"
}
