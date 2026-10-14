package ipquality

import (
	"context"
	"encoding/json"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"houfeng/internal/contracts/agentapi"
	"houfeng/internal/ipidentity"
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
	if providerErrorValue(payload, "error") || providerErrorValue(payload, "error_code") {
		return providerFailure("ipapi.is", "provider_error", payload)
	}
	facts := ipapiISFactsFromPayload(payload)
	if !validIPAddress(facts.ip) {
		return invalidProviderResponse("ipapi.is", payload)
	}
	result := providerResultFromMap(payload)
	result.Provider = "ipapi.is"
	result.CompanyType = firstNonEmpty(
		stringFromMap(nestedMap(payload, "company"), "type"),
		stringFromMap(payload, "company_type"),
	)
	result.UsageType = firstNonEmpty(stringFromMap(payload, "usage_type"), stringFromNestedMap(payload, "asn", "type"))
	result.RegionCode = facts.regionCode
	result.RegionName = facts.regionName
	return successfulProviderResult(result, facts.ip, payload, ipapiExtraPayload(payload))
}

func parseIPQueryProvider(payload map[string]any, _ string) providerSourceOutcome {
	if providerErrorValue(payload, "error") {
		return providerFailure("ipquery.io", "provider_error", payload)
	}
	ip := stringFromMap(payload, "ip", "ip_address")
	if !validIPAddress(ip) {
		return invalidProviderResponse("ipquery.io", payload)
	}
	risk := nestedMap(payload, "risk")
	isp := nestedMap(payload, "isp")
	location := nestedMap(payload, "location")
	result := agentapi.IPQualityProviderResultPayload{
		Provider:    "ipquery.io",
		UsageType:   stringFromNestedMap(payload, "risk", "usage_type"),
		CompanyType: stringFromNestedMap(payload, "company", "type"),
		RiskScore:   providerScore(risk, "risk_score", "score"),
		RegionCode:  countryCode(firstNonEmpty(stringFromMap(location, "country_code"), stringFromMap(payload, "country_code"))),
		RegionName:  firstNonEmpty(stringFromMap(location, "country"), stringFromMap(payload, "country")),
		IsProxy:     boolFromMap(risk, "is_proxy", "proxy"),
		IsTor:       boolFromMap(risk, "is_tor", "tor"),
		IsVPN:       boolFromMap(risk, "is_vpn", "vpn"),
		IsServer:    boolFromMap(risk, "is_datacenter", "datacenter", "is_server"),
		IsAbuser:    boolFromMap(risk, "is_abuser", "abuser"),
		IsRobot:     boolFromMap(risk, "is_robot", "is_crawler", "bot"),
	}
	if result.RiskScore != "" {
		result.RiskLevel = riskLevelFromScore(result.RiskScore)
	}
	return successfulProviderResult(result, ip, payload, map[string]any{"risk": risk, "isp": isp, "location": location})
}

func parseProxycheckProvider(payload map[string]any, targetIP string) providerSourceOutcome {
	status := strings.ToLower(strings.TrimSpace(stringFromMap(payload, "status")))
	switch status {
	case "ok", "warning":
	case "denied", "error":
		return providerFailure("proxycheck.io", "provider_error", payload)
	default:
		return invalidProviderResponse("proxycheck.io", payload)
	}
	version, validVersion := payload["version"].(string)
	if !validVersion || version != "24-June-2026" || !validIPAddress(targetIP) {
		return invalidProviderResponse("proxycheck.io", payload)
	}
	ipPayload, validTarget := payload[targetIP].(map[string]any)
	network, validNetwork := ipPayload["network"].(map[string]any)
	location, validLocation := ipPayload["location"].(map[string]any)
	detections, validDetections := ipPayload["detections"].(map[string]any)
	if !validTarget || !validNetwork || !validLocation || !validDetections {
		return invalidProviderResponse("proxycheck.io", payload)
	}
	for _, key := range []string{"proxy", "vpn", "tor", "hosting", "scraper", "compromised", "anonymous"} {
		if value, exists := detections[key]; exists && value != nil {
			if _, ok := value.(bool); !ok {
				return invalidProviderResponse("proxycheck.io", payload)
			}
		}
	}
	score := ""
	if value := detections["risk"]; value != nil {
		switch value.(type) {
		case json.Number, float64:
			score = providerScore(detections, "risk")
		default:
			return invalidProviderResponse("proxycheck.io", payload)
		}
		if riskLevelFromScore(score) == "" {
			return invalidProviderResponse("proxycheck.io", payload)
		}
	}
	result := agentapi.IPQualityProviderResultPayload{
		Provider:   "proxycheck.io",
		UsageType:  stringFromMap(network, "type"),
		RiskScore:  score,
		RiskLevel:  riskLevelFromScore(score),
		RegionCode: countryCode(stringFromMap(location, "country_code")),
		RegionName: stringFromMap(location, "country_name"),
		IsProxy:    explicitBoolFromMap(detections, "proxy"),
		IsVPN:      explicitBoolFromMap(detections, "vpn"),
		IsTor:      explicitBoolFromMap(detections, "tor"),
		IsServer:   explicitBoolFromMap(detections, "hosting"),
		IsRobot:    explicitBoolFromMap(detections, "scraper"),
	}
	return successfulProviderResult(result, targetIP, payload, map[string]any{
		"api_version": "24-June-2026", "network": network, "location": location, "detections": detections,
		"attack_history": ipPayload["attack_history"],
	})
}

func parseIP2LocationProvider(payload map[string]any, _ string) providerSourceOutcome {
	if providerErrorValue(payload, "error") {
		return providerFailure("ip2location.io", "provider_error", payload)
	}
	ip := stringFromMap(payload, "ip", "ip_address")
	if !validIPAddress(ip) {
		return invalidProviderResponse("ip2location.io", payload)
	}
	proxy := nestedMap(payload, "proxy")
	result := agentapi.IPQualityProviderResultPayload{
		Provider:   "ip2location.io",
		UsageType:  firstNonEmpty(stringFromMap(payload, "usage_type"), stringFromNestedMap(payload, "as_info", "as_usage_type")),
		RiskScore:  providerScore(payload, "fraud_score"),
		RegionCode: countryCode(stringFromMap(payload, "country_code")),
		RegionName: stringFromMap(payload, "country_name"),
		IsProxy:    firstBool(explicitBoolFromMap(payload, "is_proxy"), explicitBoolFromMap(proxy, "is_proxy")),
		IsVPN:      firstBool(explicitBoolFromMap(payload, "is_vpn"), explicitBoolFromMap(proxy, "is_vpn")),
		IsTor:      firstBool(explicitBoolFromMap(payload, "is_tor"), explicitBoolFromMap(proxy, "is_tor")),
		IsServer:   firstBool(explicitBoolFromMap(payload, "is_data_center", "is_datacenter"), explicitBoolFromMap(proxy, "is_data_center")),
		IsRobot:    firstBool(explicitBoolFromMap(payload, "is_web_crawler"), explicitBoolFromMap(proxy, "is_web_crawler")),
	}
	result.RiskLevel = riskLevelFromScore(result.RiskScore)
	return successfulProviderResult(result, ip, payload, payload)
}

func parseIPWhoIsProvider(payload map[string]any, _ string) providerSourceOutcome {
	success, ok := payload["success"].(bool)
	if !ok {
		return invalidProviderResponse("ipwho.is", payload)
	}
	if !success {
		return providerFailure("ipwho.is", "provider_error", payload)
	}
	ip := stringFromMap(payload, "ip", "ip_address")
	if !validIPAddress(ip) {
		return invalidProviderResponse("ipwho.is", payload)
	}
	security := nestedMap(payload, "security")
	result := agentapi.IPQualityProviderResultPayload{
		Provider:   "ipwho.is",
		RegionCode: countryCode(stringFromMap(payload, "country_code")),
		RegionName: stringFromMap(payload, "country"),
		IsProxy:    explicitBoolFromMap(security, "proxy"),
		IsVPN:      explicitBoolFromMap(security, "vpn"),
		IsTor:      explicitBoolFromMap(security, "tor"),
		IsServer:   explicitBoolFromMap(security, "hosting"),
	}
	return successfulProviderResult(result, ip, payload, map[string]any{
		"connection": payload["connection"], "security": payload["security"],
	})
}

func successfulProviderResult(result agentapi.IPQualityProviderResultPayload, ip string, payload, extra map[string]any) providerSourceOutcome {
	// Copy the evidence map so adding metadata never changes the response.
	evidence := make(map[string]any, len(extra)+1)
	for key, value := range extra {
		evidence[key] = value
	}
	capability := "geo_only"
	if result.IsProxy != nil || result.IsVPN != nil || result.IsTor != nil ||
		result.IsServer != nil || result.IsAbuser != nil || result.IsRobot != nil || result.RiskScore != "" {
		capability = "risk"
	}
	evidence["capability"] = capability
	result.Status = sourceStatusSuccess
	result.ExtraJSON = extraJSON(evidence)
	return providerSourceOutcome{Result: result, IPAddress: ip, Payload: payload}
}

func explicitBoolFromMap(payload map[string]any, keys ...string) *bool {
	for _, key := range keys {
		if value, ok := payload[key].(bool); ok {
			return &value
		}
	}
	return nil
}

func providerScore(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		switch value := payload[key].(type) {
		case json.Number:
			return value.String()
		case float64:
			return strconv.FormatFloat(value, 'f', -1, 64)
		case string:
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func providerErrorValue(payload map[string]any, key string) bool {
	value, ok := payload[key]
	if !ok || value == nil {
		return false
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) != ""
	case map[string]any:
		return len(typed) > 0
	case []any:
		return len(typed) > 0
	case bool:
		return typed
	default:
		return strings.TrimSpace(stringFromMap(payload, key)) != ""
	}
}

func providerFailure(provider, code string, payload map[string]any) providerSourceOutcome {
	return providerSourceOutcome{
		Result: agentapi.IPQualityProviderResultPayload{
			Provider:     provider,
			Status:       sourceStatusFailure,
			ErrorCode:    code,
			ErrorSummary: "provider reported failure",
			ExtraJSON:    extraJSON(payload),
		},
		Payload: payload,
	}
}

func invalidProviderResponse(provider string, payload map[string]any) providerSourceOutcome {
	return providerSourceOutcome{
		Result: agentapi.IPQualityProviderResultPayload{
			Provider:     provider,
			Status:       sourceStatusFailure,
			ErrorCode:    "invalid_response",
			ErrorSummary: "invalid provider response",
			ExtraJSON:    extraJSON(payload),
		},
		Payload: payload,
	}
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
			level := strings.ToLower(strings.TrimSpace(outcome.Result.RiskLevel))
			if riskLevelRank(level) > riskLevelRank(report.RiskLevel) {
				report.RiskLevel = level
			}
		}
	}
}

func reportFactsFromProvider(outcome providerSourceOutcome) ipapiISFacts {
	payload := outcome.Payload
	facts := ipapiISFacts{
		regionCode: outcome.Result.RegionCode, regionName: outcome.Result.RegionName,
	}
	var network, location map[string]any
	switch outcome.Result.Provider {
	case "ipapi.is":
		return ipapiISFactsFromPayload(payload)
	case "ipquery.io":
		network, location = nestedMap(payload, "isp"), nestedMap(payload, "location")
		facts.asn, _ = normalizeASN(stringFromMap(network, "asn"))
		facts.organization = stringFromMap(network, "org")
	case "ipwho.is":
		network, location = nestedMap(payload, "connection"), payload
		facts.asn, _ = normalizeASN(stringFromMap(network, "asn"))
		facts.organization = firstNonEmpty(stringFromMap(network, "org"), stringFromMap(network, "isp"))
	case "proxycheck.io":
		business := nestedMap(payload, outcome.IPAddress)
		network, location = nestedMap(business, "network"), nestedMap(business, "location")
		facts.asn, _ = normalizeASN(stringFromMap(network, "asn"))
		facts.organization = firstNonEmpty(stringFromMap(network, "organisation"), stringFromMap(network, "provider"))
	case "ip2location.io":
		location = payload
		facts.asn, _ = normalizeASN(stringFromMap(payload, "asn"))
		facts.organization = stringFromMap(payload, "as")
	}
	facts.latitude = floatPtrFromMap(location, "latitude", "lat")
	facts.longitude = floatPtrFromMap(location, "longitude", "lon", "lng")
	return facts
}

func riskLevelRank(level string) int {
	switch level {
	case "low":
		return 1
	case "medium":
		return 2
	case "high":
		return 3
	case "critical":
		return 4
	default:
		return 0
	}
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
	asn, asnOrg := ipapiASNFromPayload(payload)
	company := stringFromMap(payload, "company")
	organization := firstNonEmpty(
		stringFromMap(payload, "organization", "org", "isp"),
		stringFromNestedMap(payload, "asn", "org", "organization", "name"),
		stringFromNestedMap(payload, "company", "name"),
		company,
		asnOrg,
	)
	return ipapiISFacts{
		ip:           firstNonEmpty(stringFromMap(payload, "ip", "ip_address")),
		asn:          asn,
		organization: organization,
		regionCode: countryCode(firstNonEmpty(
			stringFromMap(payload, "use_region_code", "country_code"),
			stringFromNestedMap(payload, "location", "country_code"),
		)),
		regionName: firstNonEmpty(
			stringFromMap(payload, "use_region_name", "country_name", "country", "region", "city"),
			stringFromNestedMap(payload, "location", "country"),
		),
		registeredRegionCode: countryCode(firstNonEmpty(
			stringFromMap(payload, "registered_region_code"),
			stringFromMap(payload, "registered_country_code"),
			stringFromNestedMap(payload, "asn", "country"),
			stringFromNestedMap(payload, "asn", "country_code"),
		)),
		registeredRegionName: firstNonEmpty(stringFromMap(payload, "registered_region_name"), stringFromMap(payload, "registered_country_name")),
		latitude:             firstFloatPtr(floatPtrFromMap(payload, "latitude", "lat"), floatPtrFromNestedMap(payload, "location", "latitude", "lat")),
		longitude:            firstFloatPtr(floatPtrFromMap(payload, "longitude", "lon", "lng"), floatPtrFromNestedMap(payload, "location", "longitude", "lon", "lng")),
	}
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

func ipapiExtraPayload(payload map[string]any) map[string]any {
	return map[string]any{
		"asn":          payload["asn"],
		"company":      payload["company"],
		"location":     payload["location"],
		"city":         payload["city"],
		"region":       payload["region"],
		"country":      payload["country"],
		"timezone":     payload["timezone"],
		"risk_flags":   pickKeys(payload, "is_datacenter", "is_tor", "is_proxy", "is_vpn", "is_abuser", "is_crawler"),
		"abuser_score": payload["abuser_score"],
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

var decimalRiskScore = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

// These bands are Houfeng reference bands, not equivalent vendor ratings.
func riskLevelFromScore(score string) string {
	trimmed := strings.TrimSpace(score)
	if !decimalRiskScore.MatchString(trimmed) {
		return ""
	}
	n, err := strconv.ParseFloat(trimmed, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 100 {
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

func countryCode(value string) string {
	value = strings.TrimSpace(value)
	if len(value) != 2 {
		return ""
	}
	for _, char := range value {
		if !((char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z')) {
			return ""
		}
	}
	return strings.ToUpper(value)
}
