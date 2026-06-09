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

func TestHTTPCollectorCustomLookupKeepsLegacyJSONLookupAndSkipsServiceUnlocksWithoutServiceURL(t *testing.T) {
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
		LookupURL:    "https://api.ipapi.is",
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

func TestHTTPCollectorDefaultSourcesCollectProviderCoverageAndServiceDiagnostics(t *testing.T) {
	t.Parallel()

	requests := map[string]int{}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests[request.URL.Host]++
		body := ""
		status := http.StatusOK
		switch request.URL.Host {
		case "api.ipapi.is":
			body = `{
				"ip":"203.0.113.10",
				"is_datacenter":true,
				"is_proxy":false,
				"is_vpn":false,
				"is_tor":false,
				"company":{"name":"Example Hosting","type":"hosting"},
				"asn":{"asn":64500,"org":"Example Transit","country":"US","type":"hosting"},
				"location":{"country_code":"US","country":"United States","latitude":37.751,"longitude":-97.822}
			}`
		case "api.ipquery.io":
			body = `{
				"ip":"203.0.113.10",
				"isp":{"asn":"AS64500","org":"Example Transit"},
				"location":{"country_code":"US","country":"United States"},
				"risk":{"is_vpn":false,"is_tor":false,"is_proxy":false,"is_datacenter":true,"risk_score":22}
			}`
		case "proxycheck.io":
			body = `{"status":"ok","203.0.113.10":{"proxy":"no","type":"business","risk":15,"country":"US","isocode":"US"}}`
		case "api.ip2location.io":
			status = http.StatusTooManyRequests
			body = `{"error":{"error_message":"rate limit"}}`
		case "ipwho.is":
			body = `{"success":true,"ip":"203.0.113.10","country_code":"US","country":"United States","asn":64500,"isp":"Example Transit"}`
		case "www.netflix.com":
			body = `<html><body>watch-title</body></html>`
		case "chat.openai.com":
			body = `{"status":"normal"}`
		case "www.youtube.com":
			body = `<html><script>var ytInitialData={"countryCode":"US"};</script><body>YouTube Premium</body></html>`
		case "www.primevideo.com":
			body = `<html><script>window.__APOLLO_STATE__={"currentTerritory":"US"}</script></html>`
		case "www.tiktok.com":
			body = `<html><script>window.SIGI_STATE={"Region":"US"}</script></html>`
		case "www.reddit.com":
			body = `<html data-country-code="US"></html>`
		default:
			t.Fatalf("unexpected request to %s", request.URL.String())
		}
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
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
		Services:         []string{"netflix", "chatgpt", "youtube-premium", "amazon-prime-video", "disney-plus", "tiktok", "reddit"},
	}, time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC))

	if report.Status != agentapi.IPQualityStatusPartial {
		t.Fatalf("Status = %q, want partial because one default provider failed", report.Status)
	}
	if report.IPAddress != "203.0.113.10" || report.IPVersion != 4 {
		t.Fatalf("IP facts = (%q,%d), want canonical provider IP", report.IPAddress, report.IPVersion)
	}
	if report.Coverage == nil {
		t.Fatal("Coverage = nil, want source coverage")
	}
	if report.Coverage.ExpectedProviderCount < 6 || report.Coverage.SuccessfulProviderCount != 4 ||
		report.Coverage.FailedProviderCount != 1 || report.Coverage.NotConfiguredProviderCount < 1 {
		t.Fatalf("provider coverage = %#v, want default + optional source counts", report.Coverage)
	}
	if report.Coverage.ExpectedServiceCount != 7 || report.Coverage.SuccessfulServiceCount != 6 ||
		report.Coverage.SkippedServiceCount != 1 {
		t.Fatalf("service coverage = %#v, want six probes plus Disney skipped diagnostic", report.Coverage)
	}
	providers := providerResultsByName(report.ProviderResults)
	for _, provider := range []string{"ipapi.is", "ipquery.io", "proxycheck.io", "ip2location.io", "ipwho.is", "maxmind"} {
		if _, ok := providers[provider]; !ok {
			t.Fatalf("ProviderResults missing %s: %#v", provider, report.ProviderResults)
		}
	}
	if providers["ip2location.io"].Status != "failure" || providers["ip2location.io"].ErrorCode == "" {
		t.Fatalf("ip2location row = %#v, want failure diagnostic", providers["ip2location.io"])
	}
	if providers["maxmind"].Status != "not_configured" || providers["maxmind"].SourceType != "optional" {
		t.Fatalf("maxmind row = %#v, want optional not_configured diagnostic", providers["maxmind"])
	}
	if providers["ipquery.io"].ExtraJSON == nil || !strings.Contains(string(providers["ipquery.io"].ExtraJSON), `"risk_score":22`) {
		t.Fatalf("ipquery extra_json = %s, want provider-specific details", providers["ipquery.io"].ExtraJSON)
	}
	services := serviceUnlocksByService(report.ServiceUnlocks)
	for _, service := range []string{"netflix", "chatgpt", "youtube-premium", "amazon-prime-video", "disney-plus", "tiktok", "reddit"} {
		if _, ok := services[service]; !ok {
			t.Fatalf("ServiceUnlocks missing %s: %#v", service, report.ServiceUnlocks)
		}
	}
	if services["disney-plus"].ProbeStatus != "skipped" || services["disney-plus"].ErrorCode != "unsupported_default_probe" {
		t.Fatalf("Disney+ row = %#v, want skipped diagnostic", services["disney-plus"])
	}
	if services["netflix"].Source == "" || services["netflix"].LatencyMS == nil || services["netflix"].ExtraJSON == nil {
		t.Fatalf("Netflix row = %#v, want source/latency/extra details", services["netflix"])
	}
	if len(report.DiagnosticsJSON) == 0 || !strings.Contains(string(report.DiagnosticsJSON), `"source_version":"v2"`) {
		t.Fatalf("DiagnosticsJSON = %s, want v2 source diagnostics", report.DiagnosticsJSON)
	}
	if len(report.RawJSON) == 0 || !strings.Contains(string(report.RawJSON), `"providers"`) || !strings.Contains(string(report.RawJSON), `"services"`) {
		t.Fatalf("RawJSON = %s, want provider/service raw envelope", report.RawJSON)
	}
}

func TestHTTPCollectorDefaultSourcesIsolatesProviderTimeouts(t *testing.T) {
	t.Parallel()

	requests := map[string]int{}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests[request.URL.Host]++
		switch request.URL.Host {
		case "api.ipapi.is":
			<-request.Context().Done()
			return nil, request.Context().Err()
		case "api.ipquery.io":
			if err := request.Context().Err(); err != nil {
				return nil, err
			}
			return jsonResponse(request, `{
				"ip":"203.0.113.10",
				"location":{"country_code":"US","country":"United States"},
				"risk":{"is_vpn":false,"is_proxy":false,"is_tor":false,"risk_score":9}
			}`)
		case "proxycheck.io":
			if err := request.Context().Err(); err != nil {
				return nil, err
			}
			return jsonResponse(request, `{"status":"ok","203.0.113.10":{"proxy":"no","risk":9,"country":"US","isocode":"US"}}`)
		case "api.ip2location.io":
			if err := request.Context().Err(); err != nil {
				return nil, err
			}
			return jsonResponse(request, `{"ip":"203.0.113.10","country_code":"US","country_name":"United States","is_proxy":false}`)
		case "ipwho.is":
			if err := request.Context().Err(); err != nil {
				return nil, err
			}
			return jsonResponse(request, `{"success":true,"ip":"203.0.113.10","country_code":"US","country":"United States"}`)
		default:
			t.Fatalf("unexpected request to %s", request.URL.String())
			return nil, nil
		}
	})}
	collector := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{Client: client})

	report := collector.Collect(context.Background(), &agentapi.IPQualityPlan{
		Enabled:          true,
		TimeoutSeconds:   1,
		FrequencySeconds: 86400,
	}, time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC))

	providers := providerResultsByName(report.ProviderResults)
	if providers["ipapi.is"].Status != "failure" || providers["ipapi.is"].ErrorCode != "timeout" {
		t.Fatalf("ipapi.is row = %#v, want isolated timeout failure", providers["ipapi.is"])
	}
	if providers["ipquery.io"].Status != "success" || report.IPAddress != "203.0.113.10" {
		t.Fatalf("ipquery/report = %#v/%#v, want later provider success after first timeout", providers["ipquery.io"], report)
	}
	if requests["api.ipquery.io"] == 0 || requests["proxycheck.io"] == 0 || requests["ipwho.is"] == 0 {
		t.Fatalf("requests = %#v, want later provider sources still attempted", requests)
	}
	if report.Status != agentapi.IPQualityStatusPartial {
		t.Fatalf("Status = %q, want partial from one timed out source", report.Status)
	}
}

func TestHTTPCollectorDefaultSourcesIsolatesServiceProbeTimeouts(t *testing.T) {
	t.Parallel()

	requests := map[string]int{}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests[request.URL.Host]++
		switch request.URL.Host {
		case "api.ipapi.is":
			return jsonResponse(request, `{"ip":"203.0.113.10","version":4,"location":{"country_code":"US","country":"United States"}}`)
		case "api.ipquery.io":
			return jsonResponse(request, `{"ip":"203.0.113.10","location":{"country_code":"US","country":"United States"},"risk":{"risk_score":9}}`)
		case "proxycheck.io":
			return jsonResponse(request, `{"status":"ok","203.0.113.10":{"proxy":"no","risk":9,"country":"US","isocode":"US"}}`)
		case "api.ip2location.io":
			return jsonResponse(request, `{"ip":"203.0.113.10","country_code":"US","country_name":"United States","is_proxy":false}`)
		case "ipwho.is":
			return jsonResponse(request, `{"success":true,"ip":"203.0.113.10","country_code":"US","country":"United States"}`)
		case "www.netflix.com":
			<-request.Context().Done()
			return nil, request.Context().Err()
		case "chat.openai.com":
			if err := request.Context().Err(); err != nil {
				return nil, err
			}
			return jsonResponse(request, `{"status":"normal","countryCode":"US"}`)
		default:
			t.Fatalf("unexpected request to %s", request.URL.String())
			return nil, nil
		}
	})}
	collector := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{Client: client})

	report := collector.Collect(context.Background(), &agentapi.IPQualityPlan{
		Enabled:          true,
		TimeoutSeconds:   1,
		FrequencySeconds: 86400,
		Services:         []string{"netflix", "chatgpt"},
	}, time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC))

	services := serviceUnlocksByService(report.ServiceUnlocks)
	if services["netflix"].ProbeStatus != "failure" || services["netflix"].ErrorCode != "timeout" {
		t.Fatalf("Netflix row = %#v, want isolated timeout failure", services["netflix"])
	}
	if services["chatgpt"].ProbeStatus != "success" || services["chatgpt"].Status != "unlocked" {
		t.Fatalf("ChatGPT row = %#v, want later service probe success after first timeout", services["chatgpt"])
	}
	if requests["chat.openai.com"] == 0 {
		t.Fatalf("requests = %#v, want later service probe still attempted", requests)
	}
	if report.Status != agentapi.IPQualityStatusPartial {
		t.Fatalf("Status = %q, want partial from one timed out service probe", report.Status)
	}
}

func TestHTTPCollectorDefaultServiceProbeHTTPStatusFailureIsUnknown(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Host {
		case "api.ipapi.is":
			return jsonResponse(request, `{"ip":"203.0.113.10","version":4,"location":{"country_code":"US","country":"United States"}}`)
		case "api.ipquery.io":
			return jsonResponse(request, `{"ip":"203.0.113.10","location":{"country_code":"US","country":"United States"},"risk":{"risk_score":9}}`)
		case "proxycheck.io":
			return jsonResponse(request, `{"status":"ok","203.0.113.10":{"proxy":"no","risk":9,"country":"US","isocode":"US"}}`)
		case "api.ip2location.io":
			return jsonResponse(request, `{"ip":"203.0.113.10","country_code":"US","country_name":"United States","is_proxy":false}`)
		case "ipwho.is":
			return jsonResponse(request, `{"success":true,"ip":"203.0.113.10","country_code":"US","country":"United States"}`)
		case "www.netflix.com":
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Content-Type": []string{"text/html"}},
				Body:       io.NopCloser(strings.NewReader(`<html>rate limited</html>`)),
				Request:    request,
			}, nil
		default:
			t.Fatalf("unexpected request to %s", request.URL.String())
			return nil, nil
		}
	})}
	collector := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{Client: client})

	report := collector.Collect(context.Background(), &agentapi.IPQualityPlan{
		Enabled:          true,
		TimeoutSeconds:   5,
		FrequencySeconds: 86400,
		Services:         []string{"netflix"},
	}, time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC))

	services := serviceUnlocksByService(report.ServiceUnlocks)
	if services["netflix"].ProbeStatus != "failure" || services["netflix"].Status != "unknown" ||
		services["netflix"].ErrorCode != "http_status" {
		t.Fatalf("Netflix row = %#v, want unknown failure for rate limited probe", services["netflix"])
	}
	if report.Status != agentapi.IPQualityStatusPartial {
		t.Fatalf("Status = %q, want partial from service probe HTTP failure", report.Status)
	}
}

func TestHTTPCollectorMarksAmbiguousProviderIPCandidates(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := `{}`
		switch request.URL.Host {
		case "api.ipapi.is":
			body = `{"ip":"203.0.113.10","version":4}`
		case "api.ipquery.io":
			body = `{"ip":"203.0.113.11","risk":{"is_proxy":false}}`
		case "proxycheck.io":
			body = `{"status":"ok","203.0.113.10":{"proxy":"no"}}`
		case "api.ip2location.io":
			body = `{"ip":"203.0.113.10","country_code":"US"}`
		case "ipwho.is":
			body = `{"success":true,"ip":"203.0.113.10","country_code":"US"}`
		default:
			t.Fatalf("unexpected request to %s", request.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})}
	collector := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{Client: client})

	report := collector.Collect(context.Background(), &agentapi.IPQualityPlan{
		Enabled:          true,
		TimeoutSeconds:   5,
		FrequencySeconds: 86400,
	}, time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC))

	if report.Status != agentapi.IPQualityStatusPartial {
		t.Fatalf("Status = %q, want partial for conflicting provider IP candidates", report.Status)
	}
	if report.IPAddress != "203.0.113.10" {
		t.Fatalf("IPAddress = %q, want canonical IP from preferred provider", report.IPAddress)
	}
	if len(report.DiagnosticsJSON) == 0 || !strings.Contains(string(report.DiagnosticsJSON), `"ip_candidates"`) {
		t.Fatalf("DiagnosticsJSON = %s, want ip_candidates diagnostics", report.DiagnosticsJSON)
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

func providerResultsByName(results []agentapi.IPQualityProviderResultPayload) map[string]agentapi.IPQualityProviderResultPayload {
	out := make(map[string]agentapi.IPQualityProviderResultPayload, len(results))
	for _, result := range results {
		out[result.Provider] = result
	}
	return out
}

func serviceUnlocksByService(results []agentapi.IPQualityServiceUnlockPayload) map[string]agentapi.IPQualityServiceUnlockPayload {
	out := make(map[string]agentapi.IPQualityServiceUnlockPayload, len(results))
	for _, result := range results {
		out[result.Service] = result
	}
	return out
}

func jsonResponse(request *http.Request, body string) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}, nil
}
