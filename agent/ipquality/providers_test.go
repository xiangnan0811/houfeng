package ipquality

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"houfeng/internal/contracts/agentapi"
	"houfeng/internal/ipqualitydata"
)

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

func TestProviderBusinessFailureMatrix(t *testing.T) {
	for _, test := range []struct {
		name               string
		parse              func(map[string]any, string) providerSourceOutcome
		body, target, code string
	}{
		{"ipapi business", parseIPAPIISProvider, `{"error":"rate limited","ip":"203.0.113.10"}`, "", "provider_error"},
		{"ipapi invalid IP", parseIPAPIISProvider, `{"ip":"not-an-ip"}`, "", "invalid_response"},
		{"ipquery envelope", parseIPQueryProvider, `{"error":{"message":"denied"},"ip":"203.0.113.10"}`, "", "provider_error"},
		{"ipquery missing IP", parseIPQueryProvider, `{"location":{"country_code":"US"}}`, "", "invalid_response"},
		{"proxy denied", parseProxycheckProvider, `{"status":"denied"}`, "203.0.113.10", "provider_error"},
		{"proxy invalid shape", parseProxycheckProvider, `{"status":"ok"}`, "203.0.113.10", "invalid_response"},
		{"ip2 error", parseIP2LocationProvider, `{"error":{"message":"quota"}}`, "203.0.113.10", "provider_error"},
		{"ip2 missing IP", parseIP2LocationProvider, `{"country_code":"US"}`, "203.0.113.10", "invalid_response"},
		{"who failure", parseIPWhoIsProvider, `{"success":false,"ip":"203.0.113.10"}`, "", "provider_error"},
		{"who invalid success", parseIPWhoIsProvider, `{"success":"true","ip":"203.0.113.10"}`, "", "invalid_response"},
		{"who missing IP", parseIPWhoIsProvider, `{"success":true}`, "", "invalid_response"},
		{"different target", parseIPQueryProvider, `{"ip":"203.0.113.11","risk":{"is_vpn":true},"location":{"country_code":"US"}}`, "203.0.113.10", "ip_mismatch"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := test.parse(providerTestPayload(t, test.body), test.target).Result
			if result.Status != sourceStatusFailure || result.ErrorCode != test.code {
				t.Fatalf("failure=%#v", result)
			}
			if result.Details != nil || result.IsVPN != nil || result.RegionCode != "" || result.RiskScore != "" || result.ExtraJSON != nil {
				t.Fatalf("failure retained facts=%#v", result)
			}
		})
	}
}

func TestIPAPIISAnonymousFlatProvidesGeographyWithoutInventedRisk(t *testing.T) {
	payload := providerTestPayload(t, `{"ip":"203.0.113.10","asn":"AS64500 Example Transit","company":"Example Hosting","city":"Tokyo","region":"Tokyo","country":"Japan","lat":35.6895,"lon":139.6917,"timezone":"Asia/Tokyo","docs":"https://ipapi.is/"}`)
	outcome := parseIPAPIISProvider(payload, "")
	result, details := outcome.Result, outcome.Result.Details
	if result.Status != sourceStatusSuccess || details == nil {
		t.Fatalf("result=%#v", result)
	}
	if details.ASN != "AS64500" || details.ASName != "Example Transit" || details.CompanyName != "Example Hosting" || details.City != "Tokyo" || details.Region != "Tokyo" || details.Timezone != "Asia/Tokyo" {
		t.Fatalf("flat details=%#v", details)
	}
	if result.RegionName != "Japan" || details.CountryCode != "" || details.Latitude == nil || *details.Latitude != 35.6895 || details.Longitude == nil || *details.Longitude != 139.6917 {
		t.Fatalf("geography=%#v", details)
	}
	for _, field := range []string{"is_proxy", "is_vpn", "is_tor", "is_abuser", "is_robot", "is_server"} {
		if details.FieldStates[field] != "unsupported" {
			t.Fatalf("%s state=%s", field, details.FieldStates[field])
		}
	}
	if result.RiskLevel != "" || result.RiskScore != "" || len(details.Scores) != 0 || result.UsageType != "" || result.CompanyType != "" {
		t.Fatalf("invented risk/type=%#v", result)
	}
	var report agentapi.IPQualityReportPayload
	applyLookupPayload(&report, payload)
	if report.ASN != "AS64500" || report.Organization == "" || report.Latitude == nil || *report.Latitude != 35.6895 {
		t.Fatalf("discovery lost basic facts=%#v", report)
	}
}

func TestProviderFieldStatesDistinguishFalseUnsupportedMissingAndMalformed(t *testing.T) {
	query := parseIPQueryProvider(providerTestPayload(t, `{"ip":"203.0.113.10","risk":{"is_proxy":false,"is_vpn":true,"is_robot":"false"},"location":{"country_code":"US","latitude":0,"longitude":0}}`), "").Result
	if query.IsProxy == nil || *query.IsProxy || query.IsVPN == nil || !*query.IsVPN || query.IsAbuser != nil || query.IsRobot != nil {
		t.Fatalf("boolean facts=%#v", query)
	}
	states := query.Details.FieldStates
	if states["is_proxy"] != "known" || states["is_abuser"] != "unknown" || states["is_robot"] != "error" || states["is_bogon"] != "unsupported" {
		t.Fatalf("states=%#v", states)
	}
	if query.Details.Latitude == nil || *query.Details.Latitude != 0 || query.Details.Longitude == nil || *query.Details.Longitude != 0 {
		t.Fatal("zero coordinates disappeared")
	}
	basic := parseIP2LocationProvider(providerTestPayload(t, `{"ip":"203.0.113.10","country_code":"US"}`), "").Result
	if basic.IsVPN != nil || basic.Details.FieldStates["is_vpn"] != "unsupported" || len(basic.Details.Scores) != 0 {
		t.Fatalf("free tier invented advanced fields=%#v", basic)
	}
	who := parseIPWhoIsProvider(providerTestPayload(t, `{"success":true,"ip":"203.0.113.10","connection":{"type":"hosting"}}`), "").Result
	if who.IsServer != nil || who.UsageType != "" || who.Details.FieldStates["is_server"] != "unsupported" {
		t.Fatalf("network type inferred unsupported security=%#v", who)
	}
}

func TestProxycheckV3RawScoreAndIndependentDiagnostics(t *testing.T) {
	outcome := parseProxycheckProvider(providerTestPayload(t, `{"status":"ok","version":"24-June-2026","203.0.113.10":{"network":{"type":"Hosting","asn":"AS64500","organisation":"Example Network"},"location":{"country_code":"jp","country_name":"Japan","latitude":35.5,"longitude":139.5},"detections":{"proxy":false,"vpn":true,"tor":false,"hosting":true,"scraper":false,"risk":50.5,"compromised":true,"anonymous":true},"attack_history":[{"type":"test"}]}}`), "203.0.113.10")
	result := outcome.Result
	if result.Status != sourceStatusSuccess || result.RiskScore != "50.5" || result.RiskLevel != "" || result.IsProxy == nil || *result.IsProxy || result.IsVPN == nil || !*result.IsVPN || result.IsAbuser != nil || result.RegionCode != "JP" {
		t.Fatalf("facts=%#v", result)
	}
	if len(result.Details.Scores) != 1 || result.Details.Scores[0].Scope != "ip" || *result.Details.Scores[0].Value != 50.5 || result.Details.Scores[0].SourceLabel != "" {
		t.Fatalf("scores=%#v", result.Details.Scores)
	}
	if !strings.Contains(string(result.ExtraJSON), `"compromised":true`) {
		t.Fatalf("missing independent diagnostic=%s", result.ExtraJSON)
	}
	report := agentapi.IPQualityReportPayload{IPAddress: "203.0.113.10", IPVersion: 4}
	applyReportFallbacksFromProviders(&report, []providerSourceOutcome{outcome})
	if report.ASN != "AS64500" || report.Organization != "Example Network" || report.UseRegionCode != "JP" || report.Latitude == nil || *report.Latitude != 35.5 || report.RiskLevel != "" {
		t.Fatalf("carrier=%#v", report)
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
		{"v2 shape", func(p map[string]any) { p[ip] = map[string]any{"proxy": "no", "risk": 33} }},
	}
	for _, key := range []string{"network", "location", "detections"} {
		for _, value := range []any{nil, "not an object", []any{}} {
			tests = append(tests, struct {
				name   string
				change func(map[string]any)
			}{"invalid " + key, func(p map[string]any) { nestedMap(p, ip)[key] = value }})
		}
	}
	for _, key := range []string{"proxy", "vpn", "tor", "hosting", "scraper", "compromised", "anonymous"} {
		tests = append(tests, struct {
			name   string
			change func(map[string]any)
		}{"invalid " + key, func(p map[string]any) { nestedMap(nestedMap(p, ip), "detections")[key] = "false" }})
	}
	for _, value := range []any{"33", true, json.Number("-1"), json.Number("101"), json.Number("66junk"), math.NaN(), math.Inf(1)} {
		tests = append(tests, struct {
			name   string
			change func(map[string]any)
		}{"invalid risk", func(p map[string]any) { nestedMap(nestedMap(p, ip), "detections")["risk"] = value }})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := providerTestPayload(t, `{"status":"ok","version":"24-June-2026","203.0.113.10":{"network":{},"location":{},"detections":{"proxy":false,"risk":33}}}`)
			test.change(payload)
			result := parseProxycheckProvider(payload, ip).Result
			if result.Status != sourceStatusFailure || result.ErrorCode != "invalid_response" || result.Details != nil || result.RiskScore != "" || result.IsProxy != nil {
				t.Fatalf("invalid response retained facts=%#v", result)
			}
		})
	}
}

func TestRawScoresHaveNoInventedBandsAndAbsentIsNotZero(t *testing.T) {
	proxy := parseProxycheckProvider(providerTestPayload(t, `{"status":"ok","version":"24-June-2026","203.0.113.10":{"network":{},"location":{},"detections":{"hosting":true,"proxy":null,"risk":0}}}`), "203.0.113.10").Result
	query := parseIPQueryProvider(providerTestPayload(t, `{"ip":"203.0.113.10","risk":{"risk_score":20}}`), "").Result
	for _, result := range []agentapi.IPQualityProviderResultPayload{proxy, query} {
		if result.RiskLevel != "" || len(result.Details.Scores) != 1 || result.Details.Scores[0].SourceLabel != "" {
			t.Fatalf("invented vendor rating=%#v", result)
		}
	}
	if *proxy.Details.Scores[0].Value != 0 || *query.Details.Scores[0].Value != 20 || proxy.IsProxy != nil || proxy.Details.FieldStates["is_proxy"] != "unknown" {
		t.Fatal("zero, twenty, or null changed")
	}
	for _, body := range []string{`{"ip":"203.0.113.10"}`, `{"ip":"203.0.113.10","risk":{"risk_score":null}}`} {
		result := parseIPQueryProvider(providerTestPayload(t, body), "").Result
		if result.RiskScore != "" || len(result.Details.Scores) != 0 {
			t.Fatalf("absent became zero=%#v", result)
		}
	}
}

func TestProviderNetworkAndLocationRowsRemainIndependent(t *testing.T) {
	api := parseIPAPIISProvider(providerTestPayload(t, `{"ip":"203.0.113.10","asn":{"asn":64500,"org":"API Network","country":"US"},"company":{"name":"API Company","type":"hosting"},"location":{"country_code":"sg","country":"Singapore","city":"Singapore","latitude":1.25,"longitude":103.8}}`), "")
	query := parseIPQueryProvider(providerTestPayload(t, `{"ip":"203.0.113.10","isp":{"asn":"64501","org":"Query Network"},"location":{"country_code":"JP","country":"Japan","city":"Tokyo","latitude":35.5,"longitude":139.5},"risk":{"is_vpn":true,"risk_score":90}}`), "")
	ip2 := parseIP2LocationProvider(providerTestPayload(t, `{"ip":"203.0.113.10","country_code":"US","region_name":"California","city_name":"Los Angeles","latitude":34,"longitude":-118,"usage_type":"DCH","as_info":{"as_number":64502,"as_name":"Location Network","as_domain":"example.net","as_usage_type":"ISP"},"fraud_score":80.25,"is_vpn":false,"proxy":{"is_vpn":true,"is_tor":false,"is_web_crawler":false}}`), "")
	who := parseIPWhoIsProvider(providerTestPayload(t, `{"success":true,"ip":"203.0.113.10","country_code":"DE","city":"Berlin","latitude":52.5,"longitude":13.4,"connection":{"asn":64503,"org":"","isp":"Who Network","domain":"who.example"},"security":{"proxy":false,"vpn":true,"tor":false,"hosting":true}}`), "")
	for _, test := range []struct {
		outcome   providerSourceOutcome
		asn, city string
		lat       float64
	}{
		{api, "AS64500", "Singapore", 1.25}, {query, "AS64501", "Tokyo", 35.5}, {ip2, "AS64502", "Los Angeles", 34}, {who, "AS64503", "Berlin", 52.5},
	} {
		d := test.outcome.Result.Details
		if d == nil || d.ASN != test.asn || d.City != test.city || d.Latitude == nil || *d.Latitude != test.lat {
			t.Fatalf("independent row=%#v", d)
		}
	}
	if ip2.Result.UsageType != "DCH" || ip2.Result.IsVPN == nil || *ip2.Result.IsVPN || who.Result.Details.ASName != "Who Network" {
		t.Fatal("explicit usage/false/fallback network lost")
	}
	report := agentapi.IPQualityReportPayload{IPAddress: "203.0.113.10", IPVersion: 4}
	applyReportFallbacksFromProviders(&report, []providerSourceOutcome{who, query, api, ip2})
	if report.ASN != "AS64500" || report.UseRegionCode != "SG" || report.RegisteredRegionCode != "" || report.RiskLevel != "" {
		t.Fatalf("legacy carrier mixed facts=%#v", report)
	}
	if api.Result.Details.RegisteredCountryCode != "" || api.Result.Details.ASCountryCode != "US" {
		t.Fatal("ASN management country became registration country")
	}
}

func TestMalformedCoordinatesNeverCrossSourceBoundaries(t *testing.T) {
	for _, body := range []string{
		`{"ip":"203.0.113.10","location":{"latitude":1.25}}`,
		`{"ip":"203.0.113.10","location":{"latitude":91,"longitude":0}}`,
		`{"ip":"203.0.113.10","location":{"latitude":0,"longitude":"junk"}}`,
	} {
		result := parseIPQueryProvider(providerTestPayload(t, body), "").Result
		if result.Details.Latitude != nil || result.Details.Longitude != nil || result.Details.FieldStates["latitude"] != "error" || result.Details.FieldStates["longitude"] != "error" {
			t.Fatalf("invalid pair retained=%#v", result.Details)
		}
	}
}

func TestRegisteredCountryNeverFallsBackToASNCountry(t *testing.T) {
	for _, test := range []struct{ body, want string }{
		{`{"ip":"203.0.113.10","asn":{"country":"us"}}`, ""},
		{`{"ip":"203.0.113.10","country_code":"JP","country":"Japan"}`, ""},
		{`{"ip":"203.0.113.10","registered_country_code":"sg","asn":{"country":"us"}}`, "SG"},
		{`{"ip":"203.0.113.10","registered_country_code":"Singapore"}`, ""},
	} {
		outcome := parseIPAPIISProvider(providerTestPayload(t, test.body), "")
		report := agentapi.IPQualityReportPayload{IPAddress: "203.0.113.10", IPVersion: 4}
		applyReportFallbacksFromProviders(&report, []providerSourceOutcome{outcome})
		if report.RegisteredRegionCode != test.want || outcome.Result.Details.RegisteredCountryCode != test.want {
			t.Fatalf("registration=%#v / %#v", report, outcome.Result.Details)
		}
	}
}

func TestEquivalentIPv6DiscoveryPreservesPreferredTextualTarget(t *testing.T) {
	const preferred = "2001:0db8:0:0:0:0:0:1"
	const normalized = "2001:db8::1"
	api := parseIPAPIISProvider(providerTestPayload(t, `{"ip":"2001:0db8:0:0:0:0:0:1","asn":"AS64500"}`), "")
	query := parseIPQueryProvider(providerTestPayload(t, `{"ip":"2001:db8::1","risk":{"is_proxy":false}}`), "")
	if api.Result.Status != sourceStatusSuccess || api.IPAddress != preferred || api.Result.Details.QueriedIP != normalized {
		t.Fatalf("preferred discovery spelling lost=%#v", api)
	}
	if discoveryEgressAmbiguous([]providerSourceOutcome{api, query}) {
		t.Fatal("equivalent IPv6 discovery became ambiguous")
	}
	proxy := parseProxycheckProvider(providerTestPayload(t, `{"status":"ok","version":"24-June-2026","2001:0db8:0:0:0:0:0:1":{"network":{},"location":{},"detections":{"proxy":false}}}`), api.IPAddress)
	if proxy.Result.Status != sourceStatusSuccess || proxy.IPAddress != preferred || proxy.Result.Details.QueriedIP != normalized {
		t.Fatalf("dependent source lost preferred target=%#v", proxy)
	}
	report := agentapi.IPQualityReportPayload{IPAddress: preferred, IPVersion: 6}
	applyReportFallbacksFromProviders(&report, []providerSourceOutcome{api, query, proxy})
	if report.IPAddress != preferred || report.ASN != "AS64500" {
		t.Fatalf("report identity changed=%#v", report)
	}
	wrong := parseIPQueryProvider(providerTestPayload(t, `{"ip":"2001:db8::2","risk":{"is_vpn":true}}`), preferred)
	if wrong.Result.Status != sourceStatusFailure || wrong.Result.ErrorCode != "ip_mismatch" || wrong.Result.Details != nil || wrong.Result.IsVPN != nil {
		t.Fatalf("different IPv6 target accepted=%#v", wrong)
	}
}

func TestMalformedProviderDetailsFailLocallyBeforeOutboundValidation(t *testing.T) {
	const target = "8.8.8.8"
	for _, test := range []struct {
		name, provider, body string
	}{
		{"timezone", "ipapi.is", `{"ip":"8.8.8.8","timezone":"not a timezone"}`},
		{"route outside target", "ipapi.is", `{"ip":"8.8.8.8","asn":{"asn":15169,"route":"1.1.1.0/24"}}`},
		{"route opposite family", "ipapi.is", `{"ip":"8.8.8.8","asn":{"asn":15169,"route":"2001:4860::/32"}}`},
		{"AS domain", "ipapi.is", `{"ip":"8.8.8.8","asn":{"domain":"https://example.net"}}`},
		{"company domain", "ipapi.is", `{"ip":"8.8.8.8","company":{"domain":"bad domain"}}`},
		{"control text", "ipquery.io", `{"ip":"8.8.8.8","location":{"city":"New\u0001York"}}`},
		{"URL text", "ipquery.io", `{"ip":"8.8.8.8","isp":{"org":"https://example.net"}}`},
		{"proxy route", "proxycheck.io", `{"status":"ok","version":"24-June-2026","8.8.8.8":{"network":{"range":"invalid"},"location":{},"detections":{"proxy":true}}}`},
		{"location domain", "ip2location.io", `{"ip":"8.8.8.8","as_info":{"as_domain":"bad domain"}}`},
		{"who domain", "ipwho.is", `{"success":true,"ip":"8.8.8.8","connection":{"domain":"https://example.net"},"security":{"vpn":true}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			payload := providerTestPayload(t, test.body)
			// These are normalized external facts that the strict outbound
			// validator rejects, not merely malformed JSON or business failures.
			normalized, err := ipqualitydata.ParseProvider(test.provider, payload, target, time.Now().UTC(), "agent_public", "anonymous")
			if err != nil || normalized.Status != sourceStatusSuccess {
				t.Fatalf("fixture did not reach the normalized details boundary: %#v %v", normalized, err)
			}
			if err := agentapi.ValidateIPQualityProviderDetails(test.provider, normalized.Details, agentapi.IPQualityProductRevision, target); err == nil {
				t.Fatal("strict wire validator accepted malformed normalized facts")
			}
			bad := parsePublicProvider(test.provider, payload, target)
			if bad.Result.Status != sourceStatusFailure || bad.Result.ErrorCode != "invalid_response" ||
				bad.Result.Details != nil || bad.Result.IsProxy != nil || bad.Result.IsVPN != nil ||
				bad.Result.RegionCode != "" || bad.Result.ExtraJSON != nil || bad.IPAddress != "" {
				t.Fatalf("malformed provider retained successful facts: %#v", bad)
			}
			goodProvider := "ipquery.io"
			if test.provider == goodProvider {
				goodProvider = "ipapi.is"
			}
			good := parsePublicProvider(goodProvider, providerTestPayload(t, `{"ip":"8.8.8.8","country_code":"US","location":{"country_code":"US"}}`), target)
			if good.Result.Status != sourceStatusSuccess || good.Result.Details.CountryCode != "US" {
				t.Fatalf("unrelated provider was lost: %#v", good)
			}
			report := agentapi.IPQualityReportPayload{
				IPAddress: target, IPVersion: 4, Status: agentapi.IPQualityStatusPartial,
				Product:         &agentapi.IPQualityAgentProduct{Revision: agentapi.IPQualityProductRevision},
				ProviderResults: []agentapi.IPQualityProviderResultPayload{bad.Result, good.Result},
			}
			if err := agentapi.ValidateIPQualityProduct(report); err != nil {
				t.Fatalf("one malformed source poisoned the outbound report: %v", err)
			}
		})
	}
}
