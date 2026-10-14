package ipquality

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"houfeng/internal/contracts/agentapi"
)

func TestProviderBusinessFailureMatrix(t *testing.T) {
	tests := []struct {
		name    string
		parse   func(map[string]any, string) providerSourceOutcome
		payload map[string]any
		target  string
		code    string
		summary string
	}{
		{
			name:    "ipapi business error",
			parse:   parseIPAPIISProvider,
			payload: map[string]any{"error": "rate limited", "ip": "203.0.113.10"},
			code:    "provider_error",
			summary: "provider reported failure",
		},
		{
			name:    "ipapi invalid ip",
			parse:   parseIPAPIISProvider,
			payload: map[string]any{"ip": "not-an-ip"},
			code:    "invalid_response",
			summary: "invalid provider response",
		},
		{
			name:    "ipquery business error envelope",
			parse:   parseIPQueryProvider,
			payload: map[string]any{"error": map[string]any{"message": "denied"}, "ip": "203.0.113.10"},
			code:    "provider_error",
			summary: "provider reported failure",
		},
		{
			name:    "ipquery missing ip",
			parse:   parseIPQueryProvider,
			payload: map[string]any{"location": map[string]any{"country_code": "US"}},
			code:    "invalid_response",
			summary: "invalid provider response",
		},
		{
			name:    "proxycheck denied",
			parse:   parseProxycheckProvider,
			payload: map[string]any{"status": "denied"},
			target:  "203.0.113.10",
			code:    "provider_error",
			summary: "provider reported failure",
		},
		{
			name:    "proxycheck unknown status",
			parse:   parseProxycheckProvider,
			payload: map[string]any{"status": "ok"},
			target:  "203.0.113.10",
			code:    "invalid_response",
			summary: "invalid provider response",
		},
		{
			name:    "proxycheck missing target object",
			parse:   parseProxycheckProvider,
			payload: map[string]any{"status": "warning", "version": "24-June-2026", "203.0.113.11": map[string]any{"network": map[string]any{}, "location": map[string]any{}, "detections": map[string]any{"proxy": false}}},
			target:  "203.0.113.10",
			code:    "invalid_response",
			summary: "invalid provider response",
		},
		{
			name:    "ip2location error object",
			parse:   parseIP2LocationProvider,
			payload: map[string]any{"error": map[string]any{"error_message": "quota"}},
			target:  "203.0.113.10",
			code:    "provider_error",
			summary: "provider reported failure",
		},
		{
			name:    "ip2location target fallback forbidden",
			parse:   parseIP2LocationProvider,
			payload: map[string]any{"country_code": "US"},
			target:  "203.0.113.10",
			code:    "invalid_response",
			summary: "invalid provider response",
		},
		{
			name:    "ipwho false",
			parse:   parseIPWhoIsProvider,
			payload: map[string]any{"success": false, "ip": "203.0.113.10"},
			target:  "203.0.113.10",
			code:    "provider_error",
			summary: "provider reported failure",
		},
		{
			name:    "ipwho success wrong type",
			parse:   parseIPWhoIsProvider,
			payload: map[string]any{"success": "true", "ip": "203.0.113.10"},
			target:  "203.0.113.10",
			code:    "invalid_response",
			summary: "invalid provider response",
		},
		{
			name:    "ipwho missing ip",
			parse:   parseIPWhoIsProvider,
			payload: map[string]any{"success": true},
			target:  "203.0.113.10",
			code:    "invalid_response",
			summary: "invalid provider response",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			outcome := test.parse(test.payload, test.target)
			if outcome.Result.Status != sourceStatusFailure {
				t.Fatalf("status = %q, want failure: %#v", outcome.Result.Status, outcome.Result)
			}
			if outcome.Result.ErrorCode != test.code || outcome.Result.ErrorSummary != test.summary {
				t.Fatalf("failure = %#v, want code=%q summary=%q", outcome.Result, test.code, test.summary)
			}
		})
	}
}

func TestIPAPIISAnonymousFactsUseSharedLookupPath(t *testing.T) {
	payload := map[string]any{
		"ip":       "203.0.113.10",
		"asn":      "AS64500 Example Transit",
		"company":  "Example Hosting",
		"city":     "Tokyo",
		"region":   "Tokyo",
		"country":  "Japan",
		"latitude": 35.6895,
		"lon":      139.6917,
	}

	provider := parseIPAPIISProvider(payload, "")
	if provider.Result.Status == sourceStatusFailure || provider.Result.RegionName != "Japan" {
		t.Fatalf("provider result = %#v, want anonymous region facts", provider.Result)
	}
	if provider.Result.RegionCode != "" || provider.Result.CompanyType != "" ||
		provider.Result.IsProxy != nil || provider.Result.IsVPN != nil || !strings.Contains(string(provider.Result.ExtraJSON), `"capability":"geo_only"`) {
		t.Fatalf("anonymous result invented risk, country code or company type: %#v", provider.Result)
	}

	var report agentapi.IPQualityReportPayload
	applyLookupPayload(&report, payload)
	if report.IPAddress != "203.0.113.10" || report.ASN != "AS64500" ||
		report.Organization != "Example Hosting" || report.UseRegionName != "Japan" {
		t.Fatalf("report facts = %#v, want shared anonymous facts", report)
	}
	if report.Latitude == nil || *report.Latitude != 35.6895 || report.Longitude == nil || *report.Longitude != 139.6917 {
		t.Fatalf("coordinates = (%v,%v), want anonymous coordinates", report.Latitude, report.Longitude)
	}
}

func providerTestPayload(t *testing.T, body string) map[string]any {
	t.Helper()
	var payload map[string]any
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestRiskScoreRequiresCompleteFiniteDecimalInRange(t *testing.T) {
	for _, test := range []struct{ score, level string }{
		{"0", "low"}, {"0.0", "low"}, {"33", "low"}, {"39.999", "low"},
		{"40", "medium"}, {"66.5", "medium"}, {"79.999", "medium"},
		{"80", "high"}, {"100", "high"}, {" 80.25 ", "high"}, {"6.65e1", "medium"},
		{"", ""}, {"NaN", ""}, {"Inf", ""}, {"-Inf", ""}, {"-1", ""},
		{"100.001", ""}, {"66junk", ""}, {"0x1p4", ""}, {"6_6", ""}, {"1e999", ""},
	} {
		t.Run(test.score, func(t *testing.T) {
			if got := riskLevelFromScore(test.score); got != test.level {
				t.Fatalf("riskLevelFromScore(%q)=%q, want %q", test.score, got, test.level)
			}
		})
	}
}

func TestProxycheckV3NormalizedFactsAndSummary(t *testing.T) {
	payload := providerTestPayload(t, `{"status":"ok","version":"24-June-2026","203.0.113.10":{
		"network":{"type":"Hosting","asn":"AS64500","organisation":"Example Network"},
		"location":{"country_code":"jp","country_name":"Japan","latitude":35.5,"longitude":139.5},
		"detections":{"proxy":false,"vpn":true,"tor":false,"hosting":true,"scraper":false,"risk":50.5,"compromised":true,"anonymous":true,"confidence":99},
		"attack_history":[{"type":"test"}]}}`)
	outcome := parseProxycheckProvider(payload, "203.0.113.10")
	outcome.Result.Provider = "proxycheck.io"
	result := outcome.Result
	if result.Status != sourceStatusSuccess || result.RiskScore != "50.5" || result.RiskLevel != "medium" ||
		result.IsProxy == nil || *result.IsProxy || result.IsVPN == nil || !*result.IsVPN ||
		result.IsRobot == nil || *result.IsRobot || result.IsAbuser != nil || result.RegionCode != "JP" {
		t.Fatalf("normalized v3 result=%#v", result)
	}
	for _, evidence := range []string{`"api_version":"24-June-2026"`, `"compromised":true`, `"anonymous":true`, `"confidence":99`, `"attack_history"`, `"capability":"risk"`} {
		if !strings.Contains(string(result.ExtraJSON), evidence) {
			t.Fatalf("extra=%s missing %s", result.ExtraJSON, evidence)
		}
	}
	report := agentapi.IPQualityReportPayload{IPAddress: "203.0.113.10", IPVersion: 4}
	applyReportFallbacksFromProviders(&report, []providerSourceOutcome{outcome})
	if report.ASN != "AS64500" || report.Organization != "Example Network" ||
		report.UseRegionCode != "JP" || report.UseRegionName != "Japan" ||
		report.Latitude == nil || *report.Latitude != 35.5 || report.RiskLevel != "medium" {
		t.Fatalf("summary=%#v", report)
	}
}

func TestProxycheckV3StrictBoundaryMatrix(t *testing.T) {
	const ip = "203.0.113.10"
	tests := []struct {
		name   string
		change func(map[string]any)
	}{
		{"missing version", func(p map[string]any) { delete(p, "version") }},
		{"wrong version", func(p map[string]any) { p["version"] = "v2" }},
		{"nonstr version", func(p map[string]any) { p["version"] = true }},
		{"missing target", func(p map[string]any) { delete(p, ip) }},
		{"wrong target", func(p map[string]any) { p["203.0.113.11"] = p[ip]; delete(p, ip) }},
		{"empty business", func(p map[string]any) { p[ip] = map[string]any{} }},
		{"v2 shape", func(p map[string]any) { p[ip] = map[string]any{"proxy": "no", "risk": 33} }},
	}
	for _, key := range []string{"network", "location", "detections"} {
		for _, value := range []any{nil, "not an object", []any{}} {
			tests = append(tests, struct {
				name   string
				change func(map[string]any)
			}{
				"invalid object " + key, func(p map[string]any) { nestedMap(p, ip)[key] = value },
			})
		}
	}
	for _, key := range []string{"proxy", "vpn", "tor", "hosting", "scraper", "compromised", "anonymous"} {
		tests = append(tests, struct {
			name   string
			change func(map[string]any)
		}{
			"invalid boolean " + key, func(p map[string]any) { nestedMap(nestedMap(p, ip), "detections")[key] = "false" },
		})
	}
	for _, value := range []any{"33", true, json.Number("-1"), json.Number("101"), json.Number("66junk"), math.NaN(), math.Inf(1)} {
		tests = append(tests, struct {
			name   string
			change func(map[string]any)
		}{
			"invalid risk", func(p map[string]any) { nestedMap(nestedMap(p, ip), "detections")["risk"] = value },
		})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := providerTestPayload(t, `{"status":"ok","version":"24-June-2026","203.0.113.10":{"network":{},"location":{},"detections":{"proxy":false,"risk":33}}}`)
			test.change(payload)
			result := parseProxycheckProvider(payload, ip).Result
			if result.Status != sourceStatusFailure || result.ErrorCode != "invalid_response" ||
				result.RiskLevel != "" || result.RiskScore != "" || result.IsProxy != nil {
				t.Fatalf("invalid v3 response retained facts: %#v", result)
			}
		})
	}
}

func TestProxycheckV3HostingNullAndZeroAreNotNegativeInference(t *testing.T) {
	for _, status := range []string{"ok", "warning"} {
		payload := providerTestPayload(t, `{"version":"24-June-2026","203.0.113.10":{"network":{"type":"VPN"},"location":{},"detections":{"hosting":true,"proxy":null,"vpn":null,"risk":33}}}`)
		payload["status"] = status
		result := parseProxycheckProvider(payload, "203.0.113.10").Result
		if result.Status != sourceStatusSuccess || result.RiskLevel != "low" || result.IsServer == nil || !*result.IsServer ||
			result.IsProxy != nil || result.IsVPN != nil || result.IsTor != nil || result.IsRobot != nil || result.IsAbuser != nil {
			t.Fatalf("hosting result=%#v, must not infer negative flags from type", result)
		}
		nestedMap(nestedMap(payload, "203.0.113.10"), "detections")["risk"] = json.Number("0")
		if result := parseProxycheckProvider(payload, "203.0.113.10").Result; result.RiskScore != "0" || result.RiskLevel != "low" {
			t.Fatalf("zero score result=%#v", result)
		}
		nestedMap(nestedMap(payload, "203.0.113.10"), "detections")["risk"] = nil
		if result := parseProxycheckProvider(payload, "203.0.113.10").Result; result.RiskScore != "" || result.RiskLevel != "" {
			t.Fatalf("null score result=%#v", result)
		}
	}
}

func TestProviderSpecificNormalizedMappings(t *testing.T) {
	ipquery := parseIPQueryProvider(providerTestPayload(t, `{"ip":"203.0.113.10","isp":{"asn":"64500","org":"Query Network"},"location":{"country_code":"jp","country":"Japan"},"risk":{"is_proxy":false,"is_vpn":false,"is_tor":false,"is_datacenter":false,"risk_score":0}}`), "")
	if ipquery.Result.RiskScore != "0" || ipquery.Result.RiskLevel != "low" || ipquery.Result.RegionCode != "JP" ||
		ipquery.Result.IsProxy == nil || *ipquery.Result.IsProxy || ipquery.Result.IsServer == nil || *ipquery.Result.IsServer ||
		ipquery.Result.IsAbuser != nil || ipquery.Result.IsRobot != nil || ipquery.Result.CompanyType != "" || ipquery.Result.UsageType != "" {
		t.Fatalf("IPQuery facts=%#v", ipquery.Result)
	}
	ipquery.Result.Provider = "ipquery.io"
	report := agentapi.IPQualityReportPayload{IPAddress: "203.0.113.10", IPVersion: 4}
	applyReportFallbacksFromProviders(&report, []providerSourceOutcome{ipquery})
	if report.ASN != "AS64500" || report.Organization != "Query Network" || report.UseRegionCode != "JP" || report.UseRegionName != "Japan" {
		t.Fatalf("IPQuery-only summary=%#v", report)
	}
	ip2 := parseIP2LocationProvider(providerTestPayload(t, `{"ip":"203.0.113.10","country_code":"us","country_name":"United States","asn":"64501","as":"Location Network","usage_type":"DCH","as_info":{"as_usage_type":"ISP"},"fraud_score":80.25,"is_vpn":false,"is_tor":false,"is_data_center":false,"is_spammer":true,"proxy":{"is_vpn":true,"is_tor":true,"is_data_center":true,"is_web_crawler":false}}`), "")
	if ip2.Result.UsageType != "DCH" || ip2.Result.RiskScore != "80.25" || ip2.Result.RiskLevel != "high" ||
		ip2.Result.IsVPN == nil || *ip2.Result.IsVPN || ip2.Result.IsTor == nil || *ip2.Result.IsTor ||
		ip2.Result.IsServer == nil || *ip2.Result.IsServer || ip2.Result.IsRobot == nil || *ip2.Result.IsRobot || ip2.Result.IsAbuser != nil {
		t.Fatalf("IP2Location facts=%#v", ip2.Result)
	}
	basic := parseIP2LocationProvider(providerTestPayload(t, `{"ip":"203.0.113.10","country_code":"US","country_name":"United States"}`), "")
	if basic.Result.IsVPN != nil || basic.Result.IsTor != nil || basic.Result.IsServer != nil || basic.Result.IsRobot != nil ||
		basic.Result.RiskLevel != "" || !strings.Contains(string(basic.Result.ExtraJSON), `"capability":"geo_only"`) {
		t.Fatalf("basic IP2Location inferred advanced facts=%#v", basic.Result)
	}
	who := parseIPWhoIsProvider(providerTestPayload(t, `{"success":true,"ip":"203.0.113.10","connection":{"asn":64502,"org":"","isp":"Who Network","type":"hosting"},"security":{"proxy":false,"vpn":true,"tor":false,"hosting":true}}`), "")
	who.Result.Provider = "ipwho.is"
	if who.Result.CompanyType != "" || who.Result.UsageType != "" || who.Result.IsProxy == nil || *who.Result.IsProxy ||
		who.Result.IsVPN == nil || !*who.Result.IsVPN || who.Result.IsServer == nil || !*who.Result.IsServer {
		t.Fatalf("ipwho security=%#v", who.Result)
	}
	if facts := reportFactsFromProvider(who); facts.asn != "AS64502" || facts.organization != "Who Network" {
		t.Fatalf("ipwho network=%#v", facts)
	}
	if facts := reportFactsFromProvider(ip2); facts.asn != "AS64501" || facts.organization != "Location Network" {
		t.Fatalf("IP2Location network=%#v", facts)
	}
	noSecurity := parseIPWhoIsProvider(providerTestPayload(t, `{"success":true,"ip":"203.0.113.10","connection":{"type":"hosting"}}`), "")
	if noSecurity.Result.IsServer != nil || noSecurity.Result.IsProxy != nil || noSecurity.Result.IsVPN != nil {
		t.Fatalf("missing ipwho security invented facts=%#v", noSecurity.Result)
	}
}

func TestReportFactsStayInProviderGroupsAndFixedIdentity(t *testing.T) {
	ipapi := parseIPAPIISProvider(providerTestPayload(t, `{"ip":"203.0.113.10","asn":"AS64500","country":"Singapore","registered_country_code":"sg","latitude":1.25}`), "")
	query := parseIPQueryProvider(providerTestPayload(t, `{"ip":"203.0.113.10","isp":{"asn":"64501","org":"Other Network"},"location":{"country_code":"JP","country":"Japan","longitude":139.5},"risk":{"risk_score":90}}`), "")
	query.Result.Provider = "ipquery.io"
	ipapi.Result.RiskLevel = "low"
	report := agentapi.IPQualityReportPayload{IPAddress: "203.0.113.10", IPVersion: 4}
	// Reverse input order to verify grouping follows provider preference, not completion.
	applyReportFallbacksFromProviders(&report, []providerSourceOutcome{query, ipapi})
	if report.IPAddress != "203.0.113.10" || report.IPVersion != 4 ||
		report.UseRegionName != "Singapore" || report.UseRegionCode != "" ||
		report.Latitude == nil || *report.Latitude != 1.25 || report.Longitude != nil ||
		report.ASN != "AS64500" || report.Organization != "" ||
		report.RegisteredRegionCode != "SG" || report.RegisteredRegionName != "" || report.RiskLevel != "high" {
		t.Fatalf("mixed provider groups or fixed identity changed: %#v", report)
	}
	query.IPAddress = "203.0.113.11"
	query.Result.RiskLevel = "critical"
	applyReportFallbacksFromProviders(&report, []providerSourceOutcome{query, ipapi})
	if report.RiskLevel != "low" || report.UseRegionName != "Singapore" {
		t.Fatalf("different-target success polluted report=%#v", report)
	}
	query.IPAddress = "203.0.113.10"
	query.Result.Status = sourceStatusFailure
	applyReportFallbacksFromProviders(&report, []providerSourceOutcome{query, ipapi})
	if report.RiskLevel != "low" {
		t.Fatalf("failed provider polluted risk=%#v", report)
	}
	ipapi.Result.RiskLevel = ""
	applyReportFallbacksFromProviders(&report, []providerSourceOutcome{ipapi})
	if report.RiskLevel != "" {
		t.Fatalf("missing risk was inferred=%#v", report)
	}
}

func TestRegisteredRegionGroupDoesNotInferUseCountry(t *testing.T) {
	for _, test := range []struct{ body, code, name string }{
		{`{"ip":"203.0.113.10","country_code":"JP","country":"Japan"}`, "", ""},
		{`{"ip":"203.0.113.10","asn":{"country":"us"},"registered_country_name":"United States"}`, "US", "United States"},
		{`{"ip":"203.0.113.10","registered_country_code":"Singapore","registered_country_name":"Singapore"}`, "", "Singapore"},
	} {
		outcome := parseIPAPIISProvider(providerTestPayload(t, test.body), "")
		report := agentapi.IPQualityReportPayload{IPAddress: "203.0.113.10", IPVersion: 4}
		applyReportFallbacksFromProviders(&report, []providerSourceOutcome{outcome})
		if report.RegisteredRegionCode != test.code || report.RegisteredRegionName != test.name {
			t.Fatalf("registered region=%q/%q want %q/%q", report.RegisteredRegionCode, report.RegisteredRegionName, test.code, test.name)
		}
	}
}

func TestReportRiskUsesMaximumKnownSuccessfulLevel(t *testing.T) {
	for _, test := range []struct{ first, second, want string }{
		{"low", "medium", "medium"}, {"high", "low", "high"}, {"medium", "critical", "critical"},
		{"", "vendor_specific", ""}, {"HIGH", "", "high"},
	} {
		report := agentapi.IPQualityReportPayload{IPAddress: "203.0.113.10", IPVersion: 4}
		outcomes := []providerSourceOutcome{
			{Result: agentapi.IPQualityProviderResultPayload{Provider: "ipapi.is", Status: sourceStatusSuccess, RiskLevel: test.first}, IPAddress: report.IPAddress},
			{Result: agentapi.IPQualityProviderResultPayload{Provider: "ipquery.io", Status: sourceStatusSuccess, RiskLevel: test.second}, IPAddress: report.IPAddress},
			{Result: agentapi.IPQualityProviderResultPayload{Provider: "proxycheck.io", Status: sourceStatusFailure, RiskLevel: "critical"}, IPAddress: report.IPAddress},
		}
		applyReportFallbacksFromProviders(&report, outcomes)
		if report.RiskLevel != test.want {
			t.Fatalf("risk(%q,%q)=%q want %q", test.first, test.second, report.RiskLevel, test.want)
		}
	}
}

func TestIP2LocationAdvancedFallbackFalseZeroMissing(t *testing.T) {
	payload := providerTestPayload(t, `{"ip":"203.0.113.10","as_info":{"as_usage_type":"ISP"},"fraud_score":0,"proxy":{"is_vpn":false,"is_tor":false,"is_data_center":false,"is_web_crawler":false}}`)
	result := parseIP2LocationProvider(payload, "").Result
	if result.UsageType != "ISP" || result.RiskScore != "0" || result.RiskLevel != "low" ||
		result.IsVPN == nil || *result.IsVPN || result.IsTor == nil || *result.IsTor ||
		result.IsServer == nil || *result.IsServer || result.IsRobot == nil || *result.IsRobot ||
		result.IsProxy != nil || result.IsAbuser != nil || !strings.Contains(string(result.ExtraJSON), `"capability":"risk"`) {
		t.Fatalf("nested advanced fields=%#v", result)
	}
	if _, mutated := payload["capability"]; mutated {
		t.Fatal("capability metadata mutated provider payload")
	}
	delete(payload, "fraud_score")
	if result := parseIP2LocationProvider(payload, "").Result; result.RiskScore != "" || result.RiskLevel != "" {
		t.Fatalf("missing fraud score was converted into zero=%#v", result)
	}
	payload["fraud_score"] = "66junk"
	if result := parseIP2LocationProvider(payload, "").Result; result.RiskLevel != "" {
		t.Fatalf("invalid fraud score created a rating=%#v", result)
	}
}

func TestProxycheckV3NullDetectionFieldsRemainUnknown(t *testing.T) {
	payload := providerTestPayload(t, `{"status":"ok","version":"24-June-2026","203.0.113.10":{"network":{},"location":{},"detections":{"proxy":null,"vpn":null,"tor":null,"hosting":null,"scraper":null,"risk":null}}}`)
	result := parseProxycheckProvider(payload, "203.0.113.10").Result
	if result.Status != sourceStatusSuccess || result.IsProxy != nil || result.IsVPN != nil ||
		result.IsTor != nil || result.IsServer != nil || result.IsRobot != nil || result.IsAbuser != nil ||
		result.RiskScore != "" || result.RiskLevel != "" || !strings.Contains(string(result.ExtraJSON), `"capability":"geo_only"`) {
		t.Fatalf("null optional fields invented facts=%#v", result)
	}
}

func TestIPAPIProviderDoesNotTreatAddressTypeAsUsageType(t *testing.T) {
	result := parseIPAPIISProvider(providerTestPayload(t, `{"ip":"203.0.113.10","type":"IPv4","country":"Singapore","company":"Example Network"}`), "").Result
	if result.UsageType != "" || result.CompanyType != "" || result.RegionCode != "" || result.RegionName != "Singapore" {
		t.Fatalf("address/country/company fields became unrelated normalized facts=%#v", result)
	}
}
