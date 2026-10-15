package ipqualitydata

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"houfeng/internal/contracts/agentapi"
)

func decodedProvider(t *testing.T, raw string) map[string]any {
	t.Helper()
	var payload map[string]any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestAuthenticatedIPAPIScopedRatiosAreNotIPRisk(t *testing.T) {
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.FixedZone("offset", 3600))
	payload := decodedProvider(t, `{"ip":"203.0.113.10","company":{"name":"Example Company","domain":"example.com","type":"hosting","abuser_score":"0.0183 (Elevated)"},"asn":{"asn":64500,"org":"Example Network","country":"US","domain":"example.net","type":"isp","route":"203.0.113.0/24","abuser_score":"0.25 (High)"},"location":{"country_code":"JP","city":"Tokyo","latitude":0,"longitude":0},"is_vpn":false,"is_proxy":true,"is_anycast":false,"key":"secret-request-echo","message":"secret-request-echo"}`)
	result, err := ParseProvider("ipapi.is", payload, "203.0.113.10", at, "center_api", "authenticated")
	if err != nil {
		t.Fatal(err)
	}
	d := result.Details
	if d.Method != "center_api" || d.APITier != "authenticated" || !d.ObservedAt.Equal(at) || d.ObservedAt.Location() != time.UTC || d.QueriedIP != "203.0.113.10" {
		t.Fatalf("identity=%#v", d)
	}
	if result.RiskLevel != "" || result.RiskScore != "" || d.RegisteredCountryCode != "" || d.ASCountryCode != "US" || result.UsageType != "isp" || result.CompanyType != "hosting" {
		t.Fatalf("scope/type=%#v", result)
	}
	if len(d.Scores) != 2 {
		t.Fatalf("scores=%#v", d.Scores)
	}
	foundCompany := false
	for _, score := range d.Scores {
		if score.Scope == "ip" || score.Unit != "ratio" || score.Direction != "higher_risk" || score.Value == nil || *score.Min != 0 || *score.Max != 1 {
			t.Fatalf("wrong scope=%#v", score)
		}
		if score.Scope == "company_network" {
			foundCompany = true
			if *score.Value != 0.0183 || score.RawValue != "0.0183 (Elevated)" || score.SourceLabel != "Elevated" {
				t.Fatalf("ratio=%#v", score)
			}
		}
	}
	if !foundCompany {
		t.Fatal("company network ratio missing")
	}
	if result.IsVPN == nil || *result.IsVPN || result.IsProxy == nil || !*result.IsProxy || d.FieldStates["is_abuser"] != "unknown" {
		t.Fatalf("flags=%#v", result)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret-request-echo") || result.ExtraJSON != nil {
		t.Fatalf("unchecked response echo escaped: %s", encoded)
	}
	if payload["key"] != "secret-request-echo" {
		t.Fatal("parser mutated response")
	}
}

func TestAbuseIPDBConfidenceAndReportMetadataDoNotInventOtherFlags(t *testing.T) {
	for _, body := range []string{
		`{"data":{"ipAddress":"203.0.113.10","abuseConfidenceScore":0,"totalReports":0,"isTor":false,"usageType":"Data Center/Web Hosting/Transit","isp":"Example","domain":"example.com","lastReportedAt":null,"countryCode":"US","echo":"secret"}}`,
		`{"data":{"ipAddress":"203.0.113.10","totalReports":2,"lastReportedAt":"2026-10-13T12:00:00Z"},"key":"secret"}`,
	} {
		result, err := ParseProvider("abuseipdb", decodedProvider(t, body), "203.0.113.10", time.Now(), "center_api", "authenticated")
		if err != nil {
			t.Fatal(err)
		}
		if result.IsProxy != nil || result.IsVPN != nil || result.IsAbuser != nil || result.RiskLevel != "" || result.Details.FieldStates["is_abuser"] != "unsupported" {
			t.Fatalf("invented negatives=%#v", result)
		}
		if strings.Contains(body, `"abuseConfidenceScore"`) {
			if len(result.Details.Scores) != 1 || *result.Details.Scores[0].Value != 0 || result.Details.Scores[0].Scope != "ip" || *result.Details.Scores[0].Max != 100 || result.IsTor == nil || *result.IsTor {
				t.Fatalf("confidence=%#v", result)
			}
		} else if len(result.Details.Scores) != 0 || result.RiskScore != "" {
			t.Fatal("absent confidence became zero")
		}
		encoded, _ := json.Marshal(result)
		if strings.Contains(string(encoded), "secret") {
			t.Fatalf("credential echo=%s", encoded)
		}
		if !strings.Contains(string(result.ExtraJSON), `"total_reports"`) {
			t.Fatalf("report metadata absent=%s", result.ExtraJSON)
		}
	}
}

func TestInvalidScoresNeverBecomeBusinessFacts(t *testing.T) {
	for _, value := range []string{`-1`, `100.001`, `"NaN"`, `"Inf"`, `"66junk"`, `"0x1p4"`, `"6_6"`, `"1e999"`} {
		for _, test := range []struct{ provider, body string }{
			{"ipquery.io", `{"ip":"203.0.113.10","risk":{"risk_score":` + value + `,"is_vpn":true}}`},
			{"ip2location.io", `{"ip":"203.0.113.10","fraud_score":` + value + `}`},
			{"abuseipdb", `{"data":{"ipAddress":"203.0.113.10","abuseConfidenceScore":` + value + `}}`},
		} {
			result, err := ParseProvider(test.provider, decodedProvider(t, test.body), "203.0.113.10", time.Now(), "center_api", "authenticated")
			if err == nil || result.Status != "failure" || result.Details != nil || result.IsVPN != nil || result.RiskScore != "" {
				t.Fatalf("invalid %s %s retained facts=%#v err=%v", test.provider, value, result, err)
			}
		}
	}
	for _, value := range []string{"1.01 (High)", "-0.1 (Low)", "0.1 (High", "not-a-ratio"} {
		payload := map[string]any{"ip": "203.0.113.10", "company": map[string]any{"abuser_score": value}}
		if result, err := ParseProvider("ipapi.is", payload, "203.0.113.10", time.Now(), "center_api", "authenticated"); err == nil || result.Details != nil {
			t.Fatalf("bad ratio=%#v err=%v", result, err)
		}
	}
}

func TestProviderTargetAndMetadataValidation(t *testing.T) {
	for _, test := range []struct{ target, method, tier string }{
		{"203.0.113.11", "center_api", "authenticated"},
		{"2001:db8::1", "center_api", "authenticated"},
		{"invalid", "center_api", "authenticated"},
		{"203.0.113.10", "invalid", "authenticated"},
		{"203.0.113.10", "center_api", "invalid"},
	} {
		result, err := ParseProvider("ipapi.is", map[string]any{"ip": "203.0.113.10", "is_vpn": true}, test.target, time.Now(), test.method, test.tier)
		if err == nil || result.Status != "failure" || result.Details != nil || result.IsVPN != nil {
			t.Fatalf("boundary=%#v err=%v", result, err)
		}
	}
	result, err := ParseProvider("ipapi.is", map[string]any{"ip": "2001:db8:0:0::1", "asn": "AS64500"}, "2001:db8::1", time.Now(), "agent_public", "anonymous")
	if err != nil || result.Details.QueriedIP != "2001:db8::1" {
		t.Fatalf("equivalent IPv6 identity=%#v err=%v", result, err)
	}
}

func TestProviderDetailsDoNotTruncateOverlongFactsIntoValidValues(t *testing.T) {
	result, err := ParseProvider("ipquery.io", map[string]any{"ip": "203.0.113.10", "location": map[string]any{"city": strings.Repeat("x", 257), "country_code": "US"}}, "203.0.113.10", time.Now(), "agent_public", "anonymous")
	if err != nil {
		t.Fatal(err)
	}
	if result.Details.City != "" || result.Details.FieldStates["city"] != "error" || result.Details.CountryCode != "US" {
		t.Fatalf("truncated fact=%#v", result.Details)
	}
}

// Keep the reusable parser's return contract explicit for its Center consumer.
var _ func(string, map[string]any, string, time.Time, string, string) (agentapi.IPQualityProviderResultPayload, error) = ParseProvider

func TestAnonymousIPAPIFlatCountryRecognizesOnlyExplicitISO2(t *testing.T) {
	for _, test := range []struct{ country, code string }{
		{"US", "US"}, {"jp", "JP"}, {"Japan", ""}, {"United States", ""}, {"U1", ""},
	} {
		payload := map[string]any{
			"ip": "8.8.8.8", "asn": "AS15169", "company": "Google LLC",
			"country": test.country, "city": "Mountain View", "region": "California",
			"lat": 0, "lon": 0,
		}
		result, err := ParseProvider("ipapi.is", payload, "8.8.8.8", time.Now(), "agent_public", "anonymous")
		if err != nil {
			t.Fatal(err)
		}
		d := result.Details
		if d.CountryCode != test.code || result.RegionCode != test.code || result.RegionName != test.country {
			t.Fatalf("country %q became code=%q carrier=%q name=%q", test.country, d.CountryCode, result.RegionCode, result.RegionName)
		}
		if test.code != "" && d.FieldStates["country_code"] != "known" {
			t.Fatalf("ISO2 state=%q", d.FieldStates["country_code"])
		}
		if d.City != "Mountain View" || d.Region != "California" || d.Latitude == nil || *d.Latitude != 0 || d.Longitude == nil || *d.Longitude != 0 {
			t.Fatalf("flat geography lost=%#v", d)
		}
		if result.IsVPN != nil || d.FieldStates["is_vpn"] != "unsupported" || len(d.Scores) != 0 {
			t.Fatalf("flat country invented risk=%#v", result)
		}
	}
}
