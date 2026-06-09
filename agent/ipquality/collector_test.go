package ipquality_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agentipquality "houfeng/agent/ipquality"
	"houfeng/internal/contracts/agentapi"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestHTTPCollectorDefaultsToJSONLookupAndSkipsServiceUnlocks(t *testing.T) {
	var requests []string
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests = append(requests, request.URL.String())
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"ip":"203.0.113.10","version":4}`)),
			Request:    request,
		}, nil
	})}
	collector := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{
		Client:       client,
		AgentVersion: "test-agent",
		Fingerprint:  "fp-001",
		SyncBatchID:  "sync-001",
	})

	report := collector.Collect(context.Background(), &agentapi.IPQualityPlan{
		Enabled:          true,
		TimeoutSeconds:   5,
		FrequencySeconds: 86400,
		Services:         []string{"netflix", "chatgpt"},
	}, time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC))

	if report.Status != agentapi.IPQualityStatusSuccess {
		t.Fatalf("Status = %q, want success, error=%q", report.Status, report.ErrorSummary)
	}
	if len(requests) != 1 {
		t.Fatalf("requests = %#v, want only lookup request with default service probing disabled", requests)
	}
	if requests[0] != "https://api.ipapi.is" {
		t.Fatalf("lookup URL = %q, want JSON API endpoint", requests[0])
	}
	if len(report.ServiceUnlocks) != 0 {
		t.Fatalf("ServiceUnlocks = %#v, want none with default service URL disabled", report.ServiceUnlocks)
	}
}

func TestHTTPCollectorParsesIPAPIISNestedLookupPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/lookup" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"ip":"203.0.113.10",
			"is_datacenter":true,
			"is_tor":false,
			"is_proxy":true,
			"is_vpn":false,
			"is_abuser":true,
			"is_crawler":false,
			"company":{"name":"Example Hosting LLC","type":"hosting"},
			"asn":{"asn":64500,"org":"Example Transit","country":"US","type":"hosting"},
			"location":{"country_code":"US","country":"United States","latitude":37.751,"longitude":-97.822}
		}`))
	}))
	defer server.Close()

	collector := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{
		Client:       server.Client(),
		LookupURL:    server.URL + "/lookup",
		AgentVersion: "test-agent",
		Fingerprint:  "fp-001",
		SyncBatchID:  "sync-001",
	})

	report := collector.Collect(context.Background(), &agentapi.IPQualityPlan{
		Enabled:          true,
		TimeoutSeconds:   5,
		FrequencySeconds: 86400,
	}, time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC))

	if report.Status != agentapi.IPQualityStatusSuccess {
		t.Fatalf("Status = %q, want success, error=%q", report.Status, report.ErrorSummary)
	}
	if report.IPAddress != "203.0.113.10" || report.IPVersion != 4 {
		t.Fatalf("IP facts = (%q,%d), want IPv4 lookup facts", report.IPAddress, report.IPVersion)
	}
	if report.ASN != "AS64500" || report.Organization != "Example Transit" {
		t.Fatalf("ASN/Organization = (%q,%q), want nested asn facts", report.ASN, report.Organization)
	}
	if report.UseRegionCode != "US" || report.UseRegionName != "United States" {
		t.Fatalf("region = (%q,%q), want nested location facts", report.UseRegionCode, report.UseRegionName)
	}
	if report.Latitude == nil || *report.Latitude != 37.751 || report.Longitude == nil || *report.Longitude != -97.822 {
		t.Fatalf("coordinates = (%v,%v), want nested location coordinates", report.Latitude, report.Longitude)
	}
	if len(report.ProviderResults) != 1 {
		t.Fatalf("ProviderResults = %#v, want lookup provider result", report.ProviderResults)
	}
	provider := report.ProviderResults[0]
	if provider.Provider != "ipapi.is" {
		t.Fatalf("Provider = %q, want ipapi.is", provider.Provider)
	}
	if provider.UsageType != "hosting" || provider.CompanyType != "hosting" || provider.RegionCode != "US" {
		t.Fatalf("ProviderResults[0] = %#v, want nested usage/company/region", provider)
	}
	if provider.IsProxy == nil || !*provider.IsProxy || provider.IsAbuser == nil || !*provider.IsAbuser {
		t.Fatalf("ProviderResults[0] proxy/abuser = (%v,%v), want true pointers", provider.IsProxy, provider.IsAbuser)
	}
	if provider.IsVPN == nil || *provider.IsVPN || provider.IsTor == nil || *provider.IsTor || provider.IsRobot == nil || *provider.IsRobot {
		t.Fatalf("ProviderResults[0] vpn/tor/robot = (%v,%v,%v), want false pointers", provider.IsVPN, provider.IsTor, provider.IsRobot)
	}
}

func TestHTTPCollectorReturnsCleanFailureForHTMLLookupResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><body>not json</body></html>`))
	}))
	defer server.Close()

	collector := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{
		Client:       server.Client(),
		LookupURL:    server.URL,
		AgentVersion: "test-agent",
		Fingerprint:  "fp-001",
		SyncBatchID:  "sync-001",
	})

	report := collector.Collect(context.Background(), &agentapi.IPQualityPlan{
		Enabled:          true,
		TimeoutSeconds:   5,
		FrequencySeconds: 86400,
	}, time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC))

	if report.Status != agentapi.IPQualityStatusFailure {
		t.Fatalf("Status = %q, want failure", report.Status)
	}
	if report.ErrorCode != "lookup_failed" {
		t.Fatalf("ErrorCode = %q, want lookup_failed", report.ErrorCode)
	}
	if !strings.Contains(report.ErrorSummary, "non_json_response") {
		t.Fatalf("ErrorSummary = %q, want non_json_response diagnostic", report.ErrorSummary)
	}
	if strings.Contains(report.ErrorSummary, "<html") || strings.Contains(string(report.RawJSON), "<html") {
		t.Fatalf("HTML leaked into failure report: summary=%q raw=%s", report.ErrorSummary, report.RawJSON)
	}
	if len(report.RawJSON) != 0 && !json.Valid(report.RawJSON) {
		t.Fatalf("RawJSON = %s, want empty or valid JSON", report.RawJSON)
	}
}

func TestHTTPCollectorCollectsIPMetadataProvidersAndServiceUnlocks(t *testing.T) {
	var sawUserAgent bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.UserAgent(), "houfeng-agent") {
			sawUserAgent = true
		}
		switch r.URL.Path {
		case "/lookup":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"ip":"203.0.113.10",
				"version":4,
				"asn":"AS64500",
				"organization":"Example Transit",
				"latitude":1.25,
				"longitude":2.5,
				"use_region_code":"US",
				"use_region_name":"United States",
				"registered_region_code":"SG",
				"registered_region_name":"Singapore",
				"risk_level":"medium",
				"provider_results":[{
					"provider":"ipinfo",
					"usage_type":"hosting",
					"company_type":"business",
					"risk_level":"medium",
					"region_code":"US",
					"region_name":"United States",
					"is_proxy":true,
					"is_vpn":false
				}]
			}`))
		case "/unlock/netflix":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"unlocked","region":"US","unlock_type":"native"}`))
		case "/unlock/chatgpt":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"blocked","region":"","unlock_type":"","error_code":"blocked","error_summary":"service blocked"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	collector := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{
		Client:       server.Client(),
		LookupURL:    server.URL + "/lookup",
		ServiceURL:   server.URL + "/unlock/{service}",
		AgentVersion: "test-agent",
		Fingerprint:  "fp-001",
		SyncBatchID:  "sync-001",
	})
	observedAt := time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC)

	report := collector.Collect(context.Background(), &agentapi.IPQualityPlan{
		Enabled:          true,
		TimeoutSeconds:   5,
		FrequencySeconds: 86400,
		Services:         []string{"netflix", "chatgpt"},
	}, observedAt)

	if report.Status != agentapi.IPQualityStatusSuccess {
		t.Fatalf("Status = %q, want success, error=%q", report.Status, report.ErrorSummary)
	}
	if report.IPAddress != "203.0.113.10" || report.IPVersion != 4 {
		t.Fatalf("IP facts = (%q,%d), want lookup facts", report.IPAddress, report.IPVersion)
	}
	if report.AgentVersion != "test-agent" || report.Fingerprint != "fp-001" || report.SyncBatchID != "sync-001" {
		t.Fatalf("metadata = %#v, want injected metadata", report)
	}
	if report.ASN != "AS64500" || report.Organization != "Example Transit" || report.UseRegionCode != "US" || report.RegisteredRegionCode != "SG" {
		t.Fatalf("geo/asn facts not populated: %#v", report)
	}
	if report.Latitude == nil || *report.Latitude != 1.25 || report.Longitude == nil || *report.Longitude != 2.5 {
		t.Fatalf("coordinates = (%v,%v), want lookup coordinates", report.Latitude, report.Longitude)
	}
	if len(report.ProviderResults) != 1 || report.ProviderResults[0].Provider != "ipinfo" {
		t.Fatalf("ProviderResults = %#v, want ipinfo result", report.ProviderResults)
	}
	if report.ProviderResults[0].IsProxy == nil || !*report.ProviderResults[0].IsProxy {
		t.Fatalf("ProviderResults[0].IsProxy = %#v, want true", report.ProviderResults[0].IsProxy)
	}
	if len(report.ServiceUnlocks) != 2 {
		t.Fatalf("len(ServiceUnlocks) = %d, want 2", len(report.ServiceUnlocks))
	}
	if report.ServiceUnlocks[0].Service != "netflix" || report.ServiceUnlocks[0].Status != "unlocked" || report.ServiceUnlocks[0].Region != "US" {
		t.Fatalf("first ServiceUnlock = %#v, want netflix unlocked US", report.ServiceUnlocks[0])
	}
	if report.ServiceUnlocks[1].Service != "chatgpt" || report.ServiceUnlocks[1].Status != "blocked" {
		t.Fatalf("second ServiceUnlock = %#v, want chatgpt blocked", report.ServiceUnlocks[1])
	}
	if len(report.RawJSON) == 0 || !strings.Contains(string(report.RawJSON), `"lookup"`) {
		t.Fatalf("RawJSON = %s, want sanitized lookup/service payload", report.RawJSON)
	}
	if !sawUserAgent {
		t.Fatal("collector did not send houfeng-agent User-Agent")
	}
}

func TestHTTPCollectorSanitizesSensitiveRawJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/lookup":
			_, _ = w.Write([]byte(`{
				"ip":"203.0.113.10",
				"version":4,
				"token":"lookup-secret",
				"nested":{"api_key":"nested-secret","Authorization":"Bearer lookup-token"}
			}`))
		case "/unlock/netflix":
			_, _ = w.Write([]byte(`{"status":"unlocked","region":"US","access_token":"service-secret"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	collector := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{
		Client:       server.Client(),
		LookupURL:    server.URL + "/lookup",
		ServiceURL:   server.URL + "/unlock/{service}",
		AgentVersion: "test-agent",
		Fingerprint:  "fp-001",
		SyncBatchID:  "sync-001",
	})

	report := collector.Collect(context.Background(), &agentapi.IPQualityPlan{
		Enabled:          true,
		TimeoutSeconds:   5,
		FrequencySeconds: 86400,
		Services:         []string{"netflix"},
	}, time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC))

	raw := string(report.RawJSON)
	for _, secret := range []string{"lookup-secret", "nested-secret", "lookup-token", "service-secret"} {
		if strings.Contains(raw, secret) {
			t.Fatalf("RawJSON leaked secret %q: %s", secret, raw)
		}
	}
	if !strings.Contains(raw, `"token":"[redacted]"`) || !strings.Contains(raw, `"api_key":"[redacted]"`) || !strings.Contains(raw, `"access_token":"[redacted]"`) {
		t.Fatalf("RawJSON = %s, want sensitive fields redacted", raw)
	}
	var decoded map[string]any
	if err := json.Unmarshal(report.RawJSON, &decoded); err != nil {
		t.Fatalf("RawJSON is not valid JSON: %v; payload=%s", err, raw)
	}
}

func TestHTTPCollectorMapsBooleanUnlockedServiceStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/lookup":
			_, _ = w.Write([]byte(`{"ip":"203.0.113.10","version":4}`))
		case "/unlock/netflix":
			_, _ = w.Write([]byte(`{"unlocked":true,"country_code":"JP"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	collector := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{
		Client:       server.Client(),
		LookupURL:    server.URL + "/lookup",
		ServiceURL:   server.URL + "/unlock/{service}",
		AgentVersion: "test-agent",
		Fingerprint:  "fp-001",
		SyncBatchID:  "sync-001",
	})

	report := collector.Collect(context.Background(), &agentapi.IPQualityPlan{
		Enabled:          true,
		TimeoutSeconds:   5,
		FrequencySeconds: 86400,
		Services:         []string{"netflix"},
	}, time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC))

	if len(report.ServiceUnlocks) != 1 {
		t.Fatalf("len(ServiceUnlocks) = %d, want 1", len(report.ServiceUnlocks))
	}
	if report.ServiceUnlocks[0].Status != "unlocked" || report.ServiceUnlocks[0].Region != "JP" {
		t.Fatalf("ServiceUnlocks[0] = %#v, want unlocked JP from boolean field", report.ServiceUnlocks[0])
	}
}

func TestHTTPCollectorReturnsPartialWhenServiceProbeFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/lookup":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ip":"203.0.113.10","version":4}`))
		case "/unlock/netflix":
			http.Error(w, "upstream down", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	collector := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{
		Client:       server.Client(),
		LookupURL:    server.URL + "/lookup",
		ServiceURL:   server.URL + "/unlock/{service}",
		AgentVersion: "test-agent",
		Fingerprint:  "fp-001",
		SyncBatchID:  "sync-001",
	})

	report := collector.Collect(context.Background(), &agentapi.IPQualityPlan{
		Enabled:          true,
		TimeoutSeconds:   5,
		FrequencySeconds: 86400,
		Services:         []string{"netflix"},
	}, time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC))

	if report.Status != agentapi.IPQualityStatusPartial {
		t.Fatalf("Status = %q, want partial", report.Status)
	}
	if report.IPAddress != "203.0.113.10" {
		t.Fatalf("IPAddress = %q, want lookup IP retained", report.IPAddress)
	}
	if len(report.ServiceUnlocks) != 1 || report.ServiceUnlocks[0].Status != "unknown" || report.ServiceUnlocks[0].ErrorCode == "" {
		t.Fatalf("ServiceUnlocks = %#v, want unknown service failure", report.ServiceUnlocks)
	}
}

func TestHTTPCollectorReturnsFailureWhenLookupFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "lookup unavailable", http.StatusBadGateway)
	}))
	defer server.Close()

	collector := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{
		Client:       server.Client(),
		LookupURL:    server.URL,
		ServiceURL:   server.URL + "/unlock/{service}",
		AgentVersion: "test-agent",
		Fingerprint:  "fp-001",
		SyncBatchID:  "sync-001",
	})

	report := collector.Collect(context.Background(), &agentapi.IPQualityPlan{
		Enabled:          true,
		TimeoutSeconds:   5,
		FrequencySeconds: 86400,
		Services:         []string{"netflix"},
	}, time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC))

	if report.Status != agentapi.IPQualityStatusFailure {
		t.Fatalf("Status = %q, want failure", report.Status)
	}
	if report.IPAddress != "0.0.0.0" || report.IPVersion != 4 {
		t.Fatalf("fallback IP facts = (%q,%d), want valid placeholder for failed report", report.IPAddress, report.IPVersion)
	}
	if report.ErrorCode == "" || report.ErrorSummary == "" {
		t.Fatalf("failure report missing error: %#v", report)
	}
	if len(report.ServiceUnlocks) != 0 {
		t.Fatalf("ServiceUnlocks = %#v, want none when lookup fails", report.ServiceUnlocks)
	}
}
