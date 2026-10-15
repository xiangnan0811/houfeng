package ipqualitydata

import (
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"houfeng/internal/contracts/agentapi"
	"houfeng/internal/ipidentity"
)

// ParseProvider decodes only business fields. It never returns response envelopes,
// request data, or credentials in ExtraJSON. A failure has no usable facts.
// An empty target permits canonical discovery; all explicit targets must match.
func ParseProvider(provider string, payload map[string]any, target string, observedAt time.Time, method, apiTier string) (agentapi.IPQualityProviderResultPayload, error) {
	failure := func(code string) (agentapi.IPQualityProviderResultPayload, error) {
		summary := "invalid provider response"
		if code == "provider_error" {
			summary = "provider reported failure"
		}
		if code == "ip_mismatch" {
			summary = "provider returned a different IP"
		}
		return agentapi.IPQualityProviderResultPayload{Provider: provider, Status: "failure", SourceType: "default", ErrorCode: code, ErrorSummary: summary}, errors.New(code)
	}
	if (method != "agent_public" && method != "center_api") || (apiTier != "anonymous" && apiTier != "authenticated") || observedAt.IsZero() {
		return failure("invalid_response")
	}
	if responseError(payload["error"]) || responseError(payload["error_code"]) || responseError(payload["errors"]) {
		return failure("provider_error")
	}
	b := newProviderBuilder(provider, observedAt, method, apiTier)
	ip := text(payload, "ip", "ip_address")
	var code string
	switch provider {
	case "ipapi.is":
		parseIPAPI(b, payload)
	case "ipquery.io":
		parseIPQuery(b, payload)
	case "proxycheck.io":
		ip = target
		code = parseProxyCheck(b, payload, target)
	case "ip2location.io":
		parseIP2Location(b, payload)
	case "ipwho.is":
		ok, valid := payload["success"].(bool)
		if !valid {
			return failure("invalid_response")
		}
		if !ok {
			return failure("provider_error")
		}
		parseIPWho(b, payload)
	case "abuseipdb":
		data, valid := payload["data"].(map[string]any)
		if !valid {
			return failure("invalid_response")
		}
		ip = text(data, "ipAddress")
		parseAbuseIPDB(b, data)
	default:
		return failure("unsupported_provider")
	}
	if code != "" {
		return failure(code)
	}
	address, valid := ipidentity.Parse(ip)
	if !valid || address.IsUnspecified() {
		return failure("invalid_response")
	}
	if target != "" {
		queried, valid := ipidentity.Parse(target)
		if !valid || queried.IsUnspecified() {
			return failure("invalid_response")
		}
		if address != queried {
			return failure("ip_mismatch")
		}
	}
	if b.invalid {
		return failure("invalid_response")
	}
	b.details.QueriedIP = address.String()
	b.result.RegionCode = b.details.CountryCode
	b.result.Details = &b.details
	return b.result, nil
}

var providerFields = []string{
	"is_proxy", "is_vpn", "is_tor", "is_abuser", "is_robot", "is_server", "usage_type", "company_type",
	"country_code", "region", "city", "postal_code", "timezone", "latitude", "longitude", "registered_country_code",
	"asn", "as_name", "as_domain", "as_country_code", "route", "company_name", "company_domain", "is_mobile", "is_anycast", "is_bogon",
}

type providerBuilder struct {
	result  agentapi.IPQualityProviderResultPayload
	details agentapi.IPQualityProviderDetails
	invalid bool
}

func newProviderBuilder(provider string, at time.Time, method, tier string) *providerBuilder {
	states := make(map[string]string, len(providerFields))
	for _, key := range providerFields {
		states[key] = "unsupported"
	}
	return &providerBuilder{
		result:  agentapi.IPQualityProviderResultPayload{Provider: provider, Status: "success", SourceType: "default"},
		details: agentapi.IPQualityProviderDetails{ObservedAt: at.UTC(), Method: method, APITier: tier, Scores: []agentapi.IPQualityScore{}, FieldStates: states},
	}
}

func object(p map[string]any, key string) map[string]any {
	value, _ := p[key].(map[string]any)
	return value
}
func firstValue(p map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, exists := p[key]; exists && value != nil {
			if v, ok := value.(string); ok && strings.TrimSpace(v) == "" {
				continue
			}
			return value
		}
	}
	return nil
}
func text(p map[string]any, keys ...string) string {
	value, _ := firstValue(p, keys...).(string)
	return strings.TrimSpace(value)
}
func responseError(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return strings.TrimSpace(v) != ""
	case map[string]any:
		return len(v) != 0
	case []any:
		return len(v) != 0
	default:
		return true
	}
}
func (b *providerBuilder) str(dst *string, field string, value any) {
	b.details.FieldStates[field] = "unknown"
	if value == nil {
		return
	}
	v, ok := value.(string)
	if !ok || len(v) > 256 {
		b.details.FieldStates[field] = "error"
		return
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return
	}
	*dst = v
	b.details.FieldStates[field] = "known"
}
func (b *providerBuilder) country(dst *string, field string, value any) {
	b.str(dst, field, value)
	if *dst == "" {
		return
	}
	if len(*dst) != 2 {
		*dst = ""
		b.details.FieldStates[field] = "error"
		return
	}
	for _, r := range *dst {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
			*dst = ""
			b.details.FieldStates[field] = "error"
			return
		}
	}
	*dst = strings.ToUpper(*dst)
}
func (b *providerBuilder) boolean(dst **bool, field string, value any) {
	b.details.FieldStates[field] = "unknown"
	if value == nil {
		return
	}
	v, ok := value.(bool)
	if !ok {
		b.details.FieldStates[field] = "error"
		return
	}
	*dst = &v
	b.details.FieldStates[field] = "known"
}

var decimalNumber = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

func number(value any) (float64, string, bool) {
	var raw string
	switch v := value.(type) {
	case json.Number:
		raw = v.String()
	case float64:
		raw = strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		raw = strconv.Itoa(v)
	case int64:
		raw = strconv.FormatInt(v, 10)
	case string:
		raw = strings.TrimSpace(v)
	default:
		return 0, "", false
	}
	if !decimalNumber.MatchString(raw) {
		return 0, raw, false
	}
	n, err := strconv.ParseFloat(raw, 64)
	return n, raw, err == nil && !math.IsNaN(n) && !math.IsInf(n, 0)
}
func (b *providerBuilder) coordinates(lat, lon any) {
	b.details.FieldStates["latitude"], b.details.FieldStates["longitude"] = "unknown", "unknown"
	if lat == nil && lon == nil {
		return
	}
	x, _, validLat := number(lat)
	y, _, validLon := number(lon)
	if !validLat || !validLon || x < -90 || x > 90 || y < -180 || y > 180 {
		b.details.FieldStates["latitude"], b.details.FieldStates["longitude"] = "error", "error"
		return
	}
	b.details.Latitude, b.details.Longitude = &x, &y
	b.details.FieldStates["latitude"], b.details.FieldStates["longitude"] = "known", "known"
}
func (b *providerBuilder) asn(value any) {
	b.details.FieldStates["asn"] = "unknown"
	if value == nil {
		return
	}
	v, ok := value.(string)
	if !ok {
		_, v, ok = number(value)
	}
	if !ok || len(v) > 256 {
		b.details.FieldStates["asn"] = "error"
		return
	}
	fields := strings.Fields(v)
	if len(fields) == 0 {
		return
	}
	token := strings.TrimPrefix(strings.ToUpper(fields[0]), "AS")
	n, err := strconv.ParseUint(token, 10, 32)
	if err != nil || n == 0 {
		b.details.FieldStates["asn"] = "error"
		return
	}
	b.details.ASN = "AS" + strconv.FormatUint(n, 10)
	b.details.FieldStates["asn"] = "known"
	if len(fields) > 1 {
		b.str(&b.details.ASName, "as_name", strings.Join(fields[1:], " "))
	}
}
func (b *providerBuilder) score(value any, id, label, scope string, ratio bool) {
	if value == nil {
		return
	}
	raw := ""
	sourceLabel := ""
	if ratio {
		if v, ok := value.(string); ok {
			raw = strings.TrimSpace(v)
			if i := strings.IndexByte(raw, '('); i >= 0 {
				if !strings.HasSuffix(raw, ")") {
					b.invalid = true
					return
				}
				sourceLabel = strings.TrimSpace(raw[i+1 : len(raw)-1])
				value = strings.TrimSpace(raw[:i])
			}
		}
	}
	n, numericRaw, valid := number(value)
	maxValue := 100.0
	unit := "score"
	if ratio {
		maxValue, unit = 1, "ratio"
	}
	if !valid || n < 0 || n > maxValue || len(raw) > 256 || len(numericRaw) > 256 || len(sourceLabel) > 256 {
		b.invalid = true
		return
	}
	if raw == "" {
		raw = numericRaw
	}
	minimum := 0.0
	b.details.Scores = append(b.details.Scores, agentapi.IPQualityScore{ID: id, Label: label, RawValue: raw, Unit: unit, Scope: scope, Direction: "higher_risk", SourceLabel: sourceLabel, Value: &n, Min: &minimum, Max: &maxValue})
	if scope == "ip" {
		b.result.RiskScore = numericRaw
	}
}

func (b *providerBuilder) location(p map[string]any) {
	b.country(&b.details.CountryCode, "country_code", firstValue(p, "country_code"))
	b.str(&b.details.Region, "region", firstValue(p, "region", "region_name", "state"))
	b.str(&b.details.City, "city", firstValue(p, "city", "city_name"))
	b.str(&b.details.PostalCode, "postal_code", firstValue(p, "postal", "postal_code", "zip_code", "zip"))
	tz := firstValue(p, "timezone")
	if nested, ok := tz.(map[string]any); ok {
		tz = firstValue(nested, "id")
	}
	b.str(&b.details.Timezone, "timezone", tz)
	b.coordinates(firstValue(p, "latitude", "lat"), firstValue(p, "longitude", "lon", "lng"))
	b.result.RegionName = text(p, "country_name", "country")
	if len(b.result.RegionName) > 256 {
		b.result.RegionName = ""
	}
}

func parseIPAPI(b *providerBuilder, p map[string]any) {
	location := object(p, "location")
	if location == nil {
		location = p
	}
	b.location(location)
	if b.details.FieldStates["country_code"] == "unknown" {
		if value := firstValue(p, "country_code", "use_region_code"); value != nil {
			b.country(&b.details.CountryCode, "country_code", value)
		}
	}
	// Anonymous flat responses use country for either an ISO2 code or a
	// country name. Accept explicit codes, never infer codes from names.
	if object(p, "location") == nil && b.details.FieldStates["country_code"] == "unknown" {
		value := text(p, "country")
		validCode := len(value) == 2
		for _, char := range value {
			if !(char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z') {
				validCode = false
			}
		}
		if validCode {
			b.country(&b.details.CountryCode, "country_code", value)
		}
	}
	if b.result.RegionName == "" {
		value := text(p, "use_region_name", "country_name", "country")
		if len(value) <= 256 {
			b.result.RegionName = value
		}
	}
	b.country(&b.details.RegisteredCountryCode, "registered_country_code", firstValue(p, "registered_country_code", "registered_region_code"))
	asn := object(p, "asn")
	company := object(p, "company")
	if asn != nil {
		b.asn(firstValue(asn, "asn", "as", "as_number"))
		b.str(&b.details.ASName, "as_name", firstValue(asn, "org", "organization", "name"))
		b.str(&b.details.ASDomain, "as_domain", firstValue(asn, "domain"))
		b.country(&b.details.ASCountryCode, "as_country_code", firstValue(asn, "country", "country_code"))
		b.str(&b.details.Route, "route", firstValue(asn, "route"))
		b.str(&b.result.UsageType, "usage_type", firstValue(p, "usage_type"))
		if b.result.UsageType == "" {
			b.str(&b.result.UsageType, "usage_type", firstValue(asn, "type"))
		}
		b.score(asn["abuser_score"], "asn_abuser_score", "ASN abuser score", "asn", true)
	} else {
		b.asn(firstValue(p, "asn", "as", "as_number"))
	}
	if company != nil {
		b.str(&b.details.CompanyName, "company_name", firstValue(company, "name"))
		b.str(&b.details.CompanyDomain, "company_domain", firstValue(company, "domain"))
		b.str(&b.result.CompanyType, "company_type", firstValue(company, "type"))
		b.score(company["abuser_score"], "company_abuser_score", "Company network abuser score", "company_network", true)
	} else {
		b.str(&b.details.CompanyName, "company_name", firstValue(p, "company", "organization", "org", "isp"))
	}
	if value := firstValue(p, "usage_type"); value != nil {
		b.str(&b.result.UsageType, "usage_type", value)
	}
	if value := firstValue(p, "company_type"); value != nil {
		b.str(&b.result.CompanyType, "company_type", value)
	}
	// Official anonymous flat responses only provide basic geography/network data.
	flatAnonymous := b.details.APITier == "anonymous" && asn == nil && company == nil
	flags := []struct {
		dst   **bool
		field string
		keys  []string
	}{
		{&b.result.IsProxy, "is_proxy", []string{"is_proxy"}}, {&b.result.IsVPN, "is_vpn", []string{"is_vpn"}},
		{&b.result.IsTor, "is_tor", []string{"is_tor"}}, {&b.result.IsServer, "is_server", []string{"is_datacenter", "is_server"}},
		{&b.result.IsAbuser, "is_abuser", []string{"is_abuser"}}, {&b.result.IsRobot, "is_robot", []string{"is_crawler", "is_robot"}},
	}
	for _, flag := range flags {
		value := firstValue(p, flag.keys...)
		if value != nil || !flatAnonymous {
			b.boolean(flag.dst, flag.field, value)
		}
	}
	b.boolean(&b.details.IsMobile, "is_mobile", firstValue(p, "is_mobile"))
	b.boolean(&b.details.IsAnycast, "is_anycast", firstValue(p, "is_anycast"))
	b.boolean(&b.details.IsBogon, "is_bogon", firstValue(p, "is_bogon"))
}

func parseIPQuery(b *providerBuilder, p map[string]any) {
	risk, isp := object(p, "risk"), object(p, "isp")
	b.location(object(p, "location"))
	b.asn(firstValue(isp, "asn"))
	b.str(&b.details.ASName, "as_name", firstValue(isp, "org"))
	b.str(&b.details.CompanyName, "company_name", firstValue(isp, "isp"))
	b.str(&b.result.UsageType, "usage_type", firstValue(risk, "usage_type"))
	b.str(&b.result.CompanyType, "company_type", firstValue(object(p, "company"), "type"))
	b.boolean(&b.result.IsProxy, "is_proxy", firstValue(risk, "is_proxy", "proxy"))
	b.boolean(&b.result.IsVPN, "is_vpn", firstValue(risk, "is_vpn", "vpn"))
	b.boolean(&b.result.IsTor, "is_tor", firstValue(risk, "is_tor", "tor"))
	b.boolean(&b.result.IsServer, "is_server", firstValue(risk, "is_datacenter", "datacenter", "is_server"))
	b.boolean(&b.result.IsAbuser, "is_abuser", firstValue(risk, "is_abuser", "abuser"))
	b.boolean(&b.result.IsRobot, "is_robot", firstValue(risk, "is_robot", "is_crawler", "bot"))
	b.score(firstValue(risk, "risk_score", "score"), "risk_score", "Risk score", "ip", false)
}

func parseProxyCheck(b *providerBuilder, p map[string]any, target string) string {
	switch strings.ToLower(text(p, "status")) {
	case "denied", "error":
		return "provider_error"
	case "ok", "warning":
	default:
		return "invalid_response"
	}
	if version, valid := p["version"].(string); !valid || version != "24-June-2026" {
		return "invalid_response"
	}
	business := object(p, target)
	network, location, detections := object(business, "network"), object(business, "location"), object(business, "detections")
	if business == nil || network == nil || location == nil || detections == nil {
		return "invalid_response"
	}
	for _, key := range []string{"proxy", "vpn", "tor", "hosting", "scraper", "compromised", "anonymous"} {
		if v := detections[key]; v != nil {
			if _, valid := v.(bool); !valid {
				return "invalid_response"
			}
		}
	}
	if v := detections["risk"]; v != nil {
		switch v.(type) {
		case json.Number, float64:
		default:
			return "invalid_response"
		}
	}
	b.location(location)
	b.asn(firstValue(network, "asn"))
	b.str(&b.details.ASName, "as_name", firstValue(network, "organisation", "provider"))
	b.str(&b.details.ASDomain, "as_domain", firstValue(network, "domain"))
	b.str(&b.details.Route, "route", firstValue(network, "range", "route"))
	b.str(&b.result.UsageType, "usage_type", firstValue(network, "type"))
	b.boolean(&b.result.IsProxy, "is_proxy", detections["proxy"])
	b.boolean(&b.result.IsVPN, "is_vpn", detections["vpn"])
	b.boolean(&b.result.IsTor, "is_tor", detections["tor"])
	b.boolean(&b.result.IsServer, "is_server", detections["hosting"])
	b.boolean(&b.result.IsRobot, "is_robot", detections["scraper"])
	// compromised and attack_history are not evidence of this address being an abuser.
	b.score(detections["risk"], "risk", "Risk", "ip", false)
	return ""
}

func parseIP2Location(b *providerBuilder, p map[string]any) {
	b.location(p)
	asInfo, proxy := object(p, "as_info"), object(p, "proxy")
	b.asn(firstValue(p, "asn"))
	if b.details.ASN == "" {
		b.asn(firstValue(asInfo, "as_number", "asn"))
	}
	b.str(&b.details.ASName, "as_name", firstValue(p, "as"))
	if b.details.ASName == "" {
		b.str(&b.details.ASName, "as_name", firstValue(asInfo, "as_name"))
	}
	b.str(&b.details.ASDomain, "as_domain", firstValue(asInfo, "as_domain"))
	if value := firstValue(asInfo, "as_country_code"); value != nil {
		b.country(&b.details.ASCountryCode, "as_country_code", value)
	}
	if value := firstValue(p, "isp"); value != nil {
		b.str(&b.details.CompanyName, "company_name", value)
	}
	if value := firstValue(p, "company_type"); value != nil {
		b.str(&b.result.CompanyType, "company_type", value)
	}
	b.str(&b.result.UsageType, "usage_type", firstValue(p, "usage_type"))
	if b.result.UsageType == "" {
		b.str(&b.result.UsageType, "usage_type", firstValue(asInfo, "as_usage_type"))
	}
	b.score(firstValue(p, "fraud_score"), "fraud_score", "Fraud score", "ip", false)
	flags := []struct {
		dst   **bool
		field string
		keys  []string
	}{
		{&b.result.IsProxy, "is_proxy", []string{"is_proxy"}}, {&b.result.IsVPN, "is_vpn", []string{"is_vpn"}},
		{&b.result.IsTor, "is_tor", []string{"is_tor"}}, {&b.result.IsServer, "is_server", []string{"is_data_center", "is_datacenter"}},
		{&b.result.IsRobot, "is_robot", []string{"is_web_crawler"}},
	}
	for _, flag := range flags {
		v := firstValue(p, flag.keys...)
		if v == nil {
			v = firstValue(proxy, flag.keys...)
		}
		if v != nil || proxy != nil {
			b.boolean(flag.dst, flag.field, v)
		}
	}
}

func parseIPWho(b *providerBuilder, p map[string]any) {
	location := object(p, "location")
	if location == nil {
		location = p
	}
	b.location(location)
	connection, security := object(p, "connection"), object(p, "security")
	b.asn(firstValue(connection, "asn"))
	b.str(&b.details.ASName, "as_name", firstValue(connection, "org", "isp"))
	b.str(&b.details.CompanyName, "company_name", firstValue(connection, "isp"))
	b.str(&b.details.CompanyDomain, "company_domain", firstValue(connection, "domain"))
	if security != nil {
		b.boolean(&b.result.IsProxy, "is_proxy", security["proxy"])
		b.boolean(&b.result.IsVPN, "is_vpn", security["vpn"])
		b.boolean(&b.result.IsTor, "is_tor", security["tor"])
		b.boolean(&b.result.IsServer, "is_server", security["hosting"])
	}
}

func parseAbuseIPDB(b *providerBuilder, p map[string]any) {
	b.country(&b.details.CountryCode, "country_code", firstValue(p, "countryCode"))
	b.str(&b.result.UsageType, "usage_type", firstValue(p, "usageType"))
	b.str(&b.details.CompanyName, "company_name", firstValue(p, "isp"))
	b.str(&b.details.CompanyDomain, "company_domain", firstValue(p, "domain"))
	b.boolean(&b.result.IsTor, "is_tor", firstValue(p, "isTor"))
	b.score(firstValue(p, "abuseConfidenceScore"), "abuse_confidence_score", "Abuse confidence score", "ip", false)
	// totalReports=0 never manufactures negative proxy/VPN/abuse flags.
	metadata := make(map[string]any, 2)
	if value := p["totalReports"]; value != nil {
		n, _, valid := number(value)
		if valid && n >= 0 && n <= 9007199254740991 && math.Trunc(n) == n {
			metadata["total_reports"] = int64(n)
		}
	}
	if value, ok := p["lastReportedAt"].(string); ok {
		if at, err := time.Parse(time.RFC3339, value); err == nil {
			metadata["last_reported_at"] = at.UTC()
		}
	}
	if len(metadata) > 0 {
		b.result.ExtraJSON, _ = json.Marshal(metadata)
	}
}
