package ipquality

import (
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
			payload: map[string]any{"status": "warning", "203.0.113.11": map[string]any{"proxy": "no"}},
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
