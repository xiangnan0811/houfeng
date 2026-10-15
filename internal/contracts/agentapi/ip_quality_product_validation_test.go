package agentapi

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

func TestValidateIPQualityProductAcceptsLegacyEighteenProviders(t *testing.T) {
	report := IPQualityReportPayload{IPAddress: "203.0.113.10", IPVersion: 4}
	for name := range ipQualityProviderRegistry {
		report.ProviderResults = append(report.ProviderResults, IPQualityProviderResultPayload{Provider: name})
	}
	report.ProviderResults = append(report.ProviderResults, IPQualityProviderResultPayload{Provider: "lookup"})
	if len(ipQualityProviderRegistry) != 18 {
		t.Fatalf("provider registry = %d, want 18 legacy IDs", len(ipQualityProviderRegistry))
	}
	if err := ValidateIPQualityProduct(report); err != nil {
		t.Fatalf("legacy 18 plus custom lookup rejected: %v", err)
	}
	if err := ValidateIPQualityProviderDetails("https://evil.example/list", nil, 0, report.IPAddress); err != nil {
		t.Fatalf("legacy unknown provider without details rejected: %v", err)
	}
}

func TestValidateIPQualityProductAcceptsTypedFactsWithoutRewriting(t *testing.T) {
	report := validProductReport()
	if err := ValidateIPQualityProduct(report); err != nil {
		t.Fatalf("valid product rejected: %v", err)
	}
	if report.ProviderResults[0].Details.Latitude == nil || *report.ProviderResults[0].Details.Latitude != 0 ||
		report.ProviderResults[0].Details.Longitude == nil || *report.ProviderResults[0].Details.Longitude != 0 {
		t.Fatal("zero coordinates were treated as missing")
	}
	if report.ProviderResults[0].Details.Scores[0].Value == nil || *report.ProviderResults[0].Details.Scores[0].Value != 0.0183 {
		t.Fatal("company-network score was rewritten")
	}
	expanded := validProductReport()
	expanded.IPAddress = "2001:db8::1"
	expanded.IPVersion = 6
	expanded.Product.Mail[0].TargetIP = "2606:4700:4700::1111"
	expanded.ProviderResults[0].Details.QueriedIP = "2001:0db8:0000:0000:0000:0000:0000:0001"
	expanded.ProviderResults[0].Details.Route = "2001:db8::/32"
	if err := ValidateIPQualityProduct(expanded); err != nil {
		t.Fatalf("equivalent IPv6 target rejected: %v", err)
	}
	if expanded.ProviderResults[0].Details.QueriedIP != "2001:0db8:0000:0000:0000:0000:0000:0001" {
		t.Fatal("queried IP text was normalized")
	}
}

func TestValidateIPQualityProductRejectsUnsafeTypedFieldsWithoutTruncation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*IPQualityReportPayload)
		intact func(IPQualityReportPayload) bool
	}{
		{name: "details without product", mutate: func(r *IPQualityReportPayload) { r.Product = nil }},
		{name: "checks without product", mutate: func(r *IPQualityReportPayload) {
			r.Product = nil
			r.ProviderResults[0].Details = nil
		}},
		{name: "revision zero", mutate: func(r *IPQualityReportPayload) { r.Product.Revision = 0 }},
		{name: "revision two", mutate: func(r *IPQualityReportPayload) { r.Product.Revision = 2 }},
		{name: "seventeen providers", mutate: func(r *IPQualityReportPayload) {
			r.ProviderResults = make([]IPQualityProviderResultPayload, 17)
			i := 0
			for name := range ipQualityProviderRegistry {
				if i == 17 {
					break
				}
				r.ProviderResults[i] = IPQualityProviderResultPayload{Provider: name}
				i++
			}
		}},
		{name: "unknown provider", mutate: func(r *IPQualityReportPayload) {
			r.ProviderResults[0].Provider = "https://evil.example/list"
			r.ProviderResults[0].Details = nil
		}},
		{name: "center method", mutate: func(r *IPQualityReportPayload) { r.ProviderResults[0].Details.Method = "center_api" }},
		{name: "authenticated tier", mutate: func(r *IPQualityReportPayload) { r.ProviderResults[0].Details.APITier = "authenticated" }},
		{name: "queried ip mismatch", mutate: func(r *IPQualityReportPayload) { r.ProviderResults[0].Details.QueriedIP = "203.0.113.11" }},
		{name: "queried family mismatch", mutate: func(r *IPQualityReportPayload) { r.ProviderResults[0].Details.QueriedIP = "2001:db8::1" }},
		{name: "unpaired latitude", mutate: func(r *IPQualityReportPayload) { r.ProviderResults[0].Details.Longitude = nil }},
		{name: "latitude out of range", mutate: func(r *IPQualityReportPayload) { *r.ProviderResults[0].Details.Latitude = 91 }},
		{name: "longitude out of range", mutate: func(r *IPQualityReportPayload) { *r.ProviderResults[0].Details.Longitude = 181 }},
		{name: "nan coordinate", mutate: func(r *IPQualityReportPayload) { *r.ProviderResults[0].Details.Latitude = math.NaN() }},
		{name: "lowercase country", mutate: func(r *IPQualityReportPayload) { r.ProviderResults[0].Details.CountryCode = "us" }},
		{name: "long country", mutate: func(r *IPQualityReportPayload) { r.ProviderResults[0].Details.CountryCode = "USA" }},
		{name: "lowercase asn", mutate: func(r *IPQualityReportPayload) { r.ProviderResults[0].Details.ASN = "as64500" }},
		{name: "route misses target", mutate: func(r *IPQualityReportPayload) { r.ProviderResults[0].Details.Route = "198.51.100.0/24" }},
		{name: "overlong city", mutate: func(r *IPQualityReportPayload) {
			r.ProviderResults[0].Details.City = strings.Repeat("北", 86)
		}, intact: func(r IPQualityReportPayload) bool {
			return len(r.ProviderResults[0].Details.City) > ipQualityProductStringMax
		}},
		{name: "control character", mutate: func(r *IPQualityReportPayload) { r.ProviderResults[0].Details.City = "Town\n" }, intact: func(r IPQualityReportPayload) bool {
			return r.ProviderResults[0].Details.City == "Town\n"
		}},
		{name: "overlong error code", mutate: func(r *IPQualityReportPayload) {
			r.Product.Mail[0].ErrorCode = strings.Repeat("e", ipQualityProductErrorCodeMax+1)
		}},
		{name: "nine scores", mutate: func(r *IPQualityReportPayload) {
			score := r.ProviderResults[0].Details.Scores[0]
			r.ProviderResults[0].Details.Scores = make([]IPQualityScore, 9)
			for i := range r.ProviderResults[0].Details.Scores {
				r.ProviderResults[0].Details.Scores[i] = score
				r.ProviderResults[0].Details.Scores[i].ID = "score"
			}
		}},
		{name: "bad score scope", mutate: func(r *IPQualityReportPayload) { r.ProviderResults[0].Details.Scores[0].Scope = "network" }},
		{name: "bad score direction", mutate: func(r *IPQualityReportPayload) { r.ProviderResults[0].Details.Scores[0].Direction = "official" }},
		{name: "score min above max", mutate: func(r *IPQualityReportPayload) {
			min, max := 2.0, 1.0
			r.ProviderResults[0].Details.Scores[0].Min = &min
			r.ProviderResults[0].Details.Scores[0].Max = &max
		}},
		{name: "score outside range", mutate: func(r *IPQualityReportPayload) {
			min, max := 0.0, 0.01
			r.ProviderResults[0].Details.Scores[0].Min = &min
			r.ProviderResults[0].Details.Scores[0].Max = &max
		}},
		{name: "unknown field state", mutate: func(r *IPQualityReportPayload) {
			r.ProviderResults[0].Details.FieldStates["fraud_score"] = "known"
		}},
		{name: "bad field state", mutate: func(r *IPQualityReportPayload) {
			r.ProviderResults[0].Details.FieldStates["is_proxy"] = "missing"
		}},
		{name: "mail domain mismatch", mutate: func(r *IPQualityReportPayload) { r.Product.Mail[0].Domain = "yahoo.com" }},
		{name: "mail status", mutate: func(r *IPQualityReportPayload) { r.Product.Mail[0].Status = "open" }},
		{name: "private mail target", mutate: func(r *IPQualityReportPayload) { r.Product.Mail[0].TargetIP = "10.1.2.3" }},
		{name: "documentation mail target", mutate: func(r *IPQualityReportPayload) { r.Product.Mail[0].TargetIP = "203.0.113.20" }},
		{name: "metadata mail target", mutate: func(r *IPQualityReportPayload) { r.Product.Mail[0].TargetIP = "169.254.169.254" }},
		{name: "cross family mail target", mutate: func(r *IPQualityReportPayload) { r.Product.Mail[0].TargetIP = "2606:4700:4700::1111" }},
		{name: "unknown mail provider", mutate: func(r *IPQualityReportPayload) {
			r.Product.Mail[0].Provider = "https://smtp.example"
			r.Product.Mail[0].Domain = "example"
		}},
		{name: "unknown check", mutate: func(r *IPQualityReportPayload) { r.ServiceUnlocks[0].Checks[0].ID = "favicon" }},
		{name: "check on other service", mutate: func(r *IPQualityReportPayload) { r.ServiceUnlocks[0].Service = "netflix" }},
		{name: "five checks", mutate: func(r *IPQualityReportPayload) {
			check := r.ServiceUnlocks[0].Checks[0]
			r.ServiceUnlocks[0].Checks = []IPQualityServiceCheck{check, check, check, check, check}
		}},
		{name: "http status", mutate: func(r *IPQualityReportPayload) {
			status := 99
			r.ServiceUnlocks[0].Checks[0].HTTPStatus = &status
		}},
		{name: "ptr url", mutate: func(r *IPQualityReportPayload) { r.Product.PTRNames = []string{"https://ptr.example"} }},
		{name: "five ptr names", mutate: func(r *IPQualityReportPayload) {
			r.Product.PTRNames = []string{"a.example", "b.example", "c.example", "d.example", "e.example"}
		}},
		{name: "duplicate ptr", mutate: func(r *IPQualityReportPayload) {
			r.Product.PTRNames = []string{"Mail.Example.net", "mail.example.net"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := validProductReport()
			tc.mutate(&report)
			err := ValidateIPQualityProduct(report)
			if !errors.Is(err, ErrInvalidIPQualityProduct) {
				t.Fatalf("error = %v, want ErrInvalidIPQualityProduct", err)
			}
			if tc.intact != nil && !tc.intact(report) {
				t.Fatal("rejected value was truncated or rewritten")
			}
		})
	}
}

func TestValidateIPQualityProductRejectsOversizedDetailsWithoutTruncation(t *testing.T) {
	report := validProductReport()
	score := report.ProviderResults[0].Details.Scores[0]
	score.Label = strings.Repeat("a", ipQualityProductStringMax)
	score.RawValue = score.Label
	score.Unit = score.Label
	score.SourceLabel = score.Label
	report.ProviderResults[0].Details.Scores = make([]IPQualityScore, ipQualityProductScoreMax)
	for i := range report.ProviderResults[0].Details.Scores {
		report.ProviderResults[0].Details.Scores[i] = score
		report.ProviderResults[0].Details.Scores[i].ID = "score"
	}
	err := ValidateIPQualityProduct(report)
	if !errors.Is(err, ErrInvalidIPQualityProduct) {
		t.Fatalf("error = %v, want oversized details rejected", err)
	}
	if len(report.ProviderResults[0].Details.Scores[0].Label) != ipQualityProductStringMax {
		t.Fatalf("label length = %d, want unchanged %d", len(report.ProviderResults[0].Details.Scores[0].Label), ipQualityProductStringMax)
	}
}

func TestValidateIPQualityHelpersMatchDirectWriteBoundary(t *testing.T) {
	report := validProductReport()
	if err := ValidateIPQualityAgentProduct(nil, report.IPAddress, report.IPVersion, 18); err != nil {
		t.Fatalf("nil product rejected legacy count: %v", err)
	}
	if err := ValidateIPQualityAgentProduct(report.Product, report.IPAddress, report.IPVersion, 17); !errors.Is(err, ErrInvalidIPQualityProduct) {
		t.Fatalf("revision 1 count error = %v", err)
	}
	if err := ValidateIPQualityAgentProduct(report.Product, report.IPAddress, report.IPVersion, 16); err != nil {
		t.Fatalf("revision 1 within cap rejected: %v", err)
	}
	if err := ValidateIPQualityProviderDetails("ipapi.is", nil, IPQualityProductRevision, report.IPAddress); err != nil {
		t.Fatalf("registered provider without details rejected: %v", err)
	}
	if err := ValidateIPQualityProviderDetails("lookup", nil, IPQualityProductRevision, report.IPAddress); !errors.Is(err, ErrInvalidIPQualityProduct) {
		t.Fatalf("unregistered revision 1 provider error = %v", err)
	}
	if err := ValidateIPQualityProviderDetails("lookup", report.ProviderResults[0].Details, 0, report.IPAddress); !errors.Is(err, ErrInvalidIPQualityProduct) {
		t.Fatalf("details without revision error = %v", err)
	}
	if err := ValidateIPQualityServiceChecks("netflix", nil, 0); err != nil {
		t.Fatalf("empty checks rejected: %v", err)
	}
	if err := ValidateIPQualityServiceChecks("chatgpt", report.ServiceUnlocks[0].Checks, 0); !errors.Is(err, ErrInvalidIPQualityProduct) {
		t.Fatalf("checks without revision error = %v", err)
	}
	absent := report.ProviderResults[0].Details.Scores[0]
	absent.Value = nil
	report.ProviderResults[0].Details.Scores[0] = absent
	if err := ValidateIPQualityProviderDetails("ipapi.is", report.ProviderResults[0].Details, IPQualityProductRevision, report.IPAddress); err != nil {
		t.Fatalf("absent score value rejected: %v", err)
	}
}

func validProductReport() IPQualityReportPayload {
	lat, lon := 0.0, 0.0
	value := 0.0183
	pref := uint16(10)
	greeting := 220
	latency := 40
	httpStatus := 200
	observed := time.Date(2026, 4, 23, 9, 0, 1, 0, time.UTC)
	return IPQualityReportPayload{
		IPAddress: "203.0.113.10",
		IPVersion: 4,
		Product: &IPQualityAgentProduct{
			Revision: IPQualityProductRevision,
			PTRNames: []string{"mail.example.net"},
			Mail: []IPQualityMailResult{{
				Provider:     "gmail",
				Domain:       "gmail.com",
				Status:       "reachable",
				MXHost:       "gmail-smtp-in.l.google.com",
				TargetIP:     "1.1.1.1",
				MXPreference: &pref,
				GreetingCode: &greeting,
				LatencyMS:    &latency,
				ObservedAt:   observed,
			}},
		},
		ProviderResults: []IPQualityProviderResultPayload{{
			Provider: "ipapi.is",
			Details: &IPQualityProviderDetails{
				QueriedIP:   "203.0.113.10",
				ObservedAt:  observed,
				Method:      "agent_public",
				APITier:     "anonymous",
				CountryCode: "US",
				ASN:         "AS64500",
				Route:       "203.0.113.0/24",
				Latitude:    &lat,
				Longitude:   &lon,
				Scores: []IPQualityScore{{
					ID:          "abuser_score",
					Label:       "Abuser",
					RawValue:    "0.0183 (Elevated)",
					Unit:        "ratio",
					Scope:       "company_network",
					Direction:   "higher_risk",
					SourceLabel: "Elevated",
					Value:       &value,
				}},
				FieldStates: map[string]string{"is_proxy": "unsupported"},
			},
		}},
		ServiceUnlocks: []IPQualityServiceUnlockPayload{{
			Service: "chatgpt",
			Checks: []IPQualityServiceCheck{
				{ID: "web", Label: "Web", Status: "available", Region: "US", Rule: "openai_cookie_requirements", HTTPStatus: &httpStatus},
				{ID: "app", Label: "App", Status: "unknown", ErrorCode: "invalid_response", Rule: "openai_app_page"},
			},
		}},
	}
}
