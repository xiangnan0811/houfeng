package ipquality_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

	requests := map[string][]string{}
	var requestsMu sync.Mutex
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestsMu.Lock()
		requests[request.URL.Host] = append(requests[request.URL.Host], request.URL.String())
		requestsMu.Unlock()
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
			if request.URL.Path != "/" || request.URL.RawQuery != "format=json" {
				t.Fatalf("IPQuery URL = %q, want JSON query endpoint", request.URL.String())
			}
			body = `{
				"ip":"203.0.113.10",
				"isp":{"asn":"AS64500","org":"Example Transit"},
				"location":{"country_code":"US","country":"United States"},
				"risk":{"is_vpn":false,"is_tor":false,"is_proxy":false,"is_datacenter":true,"risk_score":22}
			}`
		case "proxycheck.io":
			body = `{"status":"ok","version":"24-June-2026","203.0.113.10":{"network":{"type":"business"},"location":{"country_name":"United States","country_code":"US"},"detections":{"proxy":false,"risk":15}}}`
		case "api.ip2location.io":
			status = http.StatusTooManyRequests
			body = `{"error":{"error_message":"rate limit"}}`
		case "ipwho.is":
			body = `{"success":true,"ip":"203.0.113.10","country_code":"US","country":"United States","asn":64500,"isp":"Example Transit"}`
		default:
			t.Fatalf("unexpected provider request to %s", request.URL.String())
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
	if report.Coverage.ExpectedServiceCount != 0 || len(report.ServiceUnlocks) != 0 {
		t.Fatalf("service coverage = %#v, want no services for provider-only collection", report.Coverage)
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
	if len(report.DiagnosticsJSON) == 0 || !strings.Contains(string(report.DiagnosticsJSON), `"source_version":"v2"`) ||
		!strings.Contains(string(report.DiagnosticsJSON), `"service_probe_revision":2`) {
		t.Fatalf("DiagnosticsJSON = %s, want v2 service-probe revision diagnostics", report.DiagnosticsJSON)
	}
	if len(report.RawJSON) == 0 || !strings.Contains(string(report.RawJSON), `"providers"`) {
		t.Fatalf("RawJSON = %s, want provider raw envelope", report.RawJSON)
	}
	requestsMu.Lock()
	defer requestsMu.Unlock()
	if len(requests["api.ipapi.is"]) != 1 || len(requests["api.ipquery.io"]) != 1 ||
		len(requests["proxycheck.io"]) != 1 || len(requests["api.ip2location.io"]) != 1 || len(requests["ipwho.is"]) != 1 ||
		len(requests) != 5 {
		t.Fatalf("requests = %#v, want five provider requests for the empty service set", requests)
	}
}

func TestHTTPCollectorDefaultSourcesParseProviderFactsForKnownVPSIP(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := ""
		switch request.URL.Host {
		case "api.ipapi.is":
			body = `{
				"ip":"209.33.173.4",
				"rir":"RIPE",
				"is_bogon":false,
				"is_mobile":false,
				"is_satellite":false,
				"is_crawler":false,
				"is_datacenter":true,
				"is_tor":false,
				"is_proxy":false,
				"is_vpn":false,
				"is_abuser":false,
				"datacenter":{"datacenter":"BAGE CLOUD LLC","domain":"bage.dev","network":"209.33.173.0 - 209.33.173.255"},
				"company":{"name":"BAGE CLOUD LLC","abuser_score":"0 (Very Low)","domain":"bage.dev","type":"hosting","network":"209.33.173.0 - 209.33.173.255","netname":"BGAE-JP-202603"},
				"asn":{"asn":63150,"abuser_score":"0.0128 (Elevated)","route":"209.33.173.0/24","descr":"BAGE - BAGE CLOUD LLC, US","country":"us","active":true,"org":"BAGE CLOUD LLC","domain":"bage.dev","abuse":"abuse@bage.dev","type":"hosting","created":"2024-01-26","updated":"2026-03-05","rir":"ARIN"},
				"location":{"continent":"AS","country":"Japan","country_code":"JP","state":"Tokyo To","city":"Tokyo","latitude":35.6895,"longitude":139.69171}
			}`
		case "api.ipquery.io":
			body = `{
				"ip":"209.33.173.4",
				"isp":{"asn":"AS7029","org":"Windstream Communications LLC","isp":"Windstream Communications LLC"},
				"location":{"country":"United States","country_code":"US","city":"Little Rock (River Mountain)","state":"Arkansas","latitude":34.75877200530601,"longitude":-92.40399800199567},
				"risk":{"is_mobile":false,"is_vpn":false,"is_tor":false,"is_proxy":false,"is_datacenter":false,"risk_score":0}
			}`
		case "proxycheck.io":
			body = `{
				"status":"ok","version":"24-June-2026",
				"209.33.173.4":{
					"network":{"asn":"AS63150","provider":"BAGE CLOUD LLC","organisation":"Bage Cloud LLC","type":"VPN"},
					"location":{"country_name":"Japan","country_code":"JP"},
					"detections":{"proxy":true,"vpn":true,"tor":false,"hosting":true,"scraper":false,"risk":66}
				}
			}`
		case "api.ip2location.io":
			body = `{"ip":"209.33.173.4","country_code":"JP","country_name":"Japan","region_name":"Tokyo","city_name":"Tokyo","latitude":35.6895,"longitude":139.69232,"asn":"63150","as":"Bage Cloud LLC","is_proxy":false}`
		case "ipwho.is":
			body = `{"ip":"209.33.173.4","success":true,"type":"IPv4","continent":"Asia","continent_code":"AS","country":"Japan","country_code":"JP","region":"Tokyo","region_code":"13","city":"Tokyo","latitude":35.7090259,"longitude":139.7319925,"connection":{"asn":63150,"org":"BAGE CLOUD LLC","isp":"BAGE CLOUD LLC","domain":"bage.dev"}}`
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
	}, time.Date(2026, time.June, 10, 5, 0, 0, 0, time.UTC))

	providers := providerResultsByName(report.ProviderResults)
	ipapi := providers["ipapi.is"]
	if ipapi.CompanyType != "hosting" || ipapi.RegionCode != "JP" ||
		ipapi.IsServer == nil || !*ipapi.IsServer ||
		ipapi.IsProxy == nil || *ipapi.IsProxy ||
		ipapi.IsVPN == nil || *ipapi.IsVPN ||
		ipapi.IsTor == nil || *ipapi.IsTor {
		t.Fatalf("ipapi.is row = %#v, want hosting JP datacenter context and false proxy/vpn/tor", ipapi)
	}
	proxycheck := providers["proxycheck.io"]
	if proxycheck.RiskScore != "66" || proxycheck.RiskLevel != "medium" ||
		proxycheck.UsageType != "VPN" || proxycheck.RegionCode != "JP" ||
		proxycheck.IsProxy == nil || !*proxycheck.IsProxy ||
		proxycheck.IsVPN == nil || !*proxycheck.IsVPN {
		t.Fatalf("proxycheck.io row = %#v, want risk 66 VPN proxy in JP", proxycheck)
	}
	ip2location := providers["ip2location.io"]
	if ip2location.RegionCode != "JP" || ip2location.RegionName != "Japan" ||
		ip2location.IsProxy == nil || *ip2location.IsProxy ||
		len(ip2location.ExtraJSON) == 0 || !strings.Contains(string(ip2location.ExtraJSON), `"asn":"63150"`) {
		t.Fatalf("ip2location.io row = %#v extra=%s, want JP proxy=false and ASN extra", ip2location, ip2location.ExtraJSON)
	}
	ipwhois := providers["ipwho.is"]
	if ipwhois.RegionCode != "JP" || ipwhois.RegionName != "Japan" ||
		ipwhois.CompanyType != "" || ipwhois.UsageType != "" || !strings.Contains(string(ipwhois.ExtraJSON), "BAGE CLOUD LLC") {
		t.Fatalf("ipwho.is row = %#v extra=%s, want JP and connection context without invented type", ipwhois, ipwhois.ExtraJSON)
	}
	ipquery := providers["ipquery.io"]
	if ipquery.RegionCode != "US" || !strings.Contains(string(ipquery.ExtraJSON), "Windstream Communications LLC") {
		t.Fatalf("ipquery.io row = %#v extra=%s, want conflicting US/Windstream evidence preserved", ipquery, ipquery.ExtraJSON)
	}
	if len(report.DiagnosticsJSON) == 0 || !strings.Contains(string(report.DiagnosticsJSON), `"ip_candidates"`) {
		t.Fatalf("DiagnosticsJSON = %s, want provider candidate diagnostics", report.DiagnosticsJSON)
	}
}

func TestHTTPCollectorDefaultSourcesIsolatesProviderTimeouts(t *testing.T) {
	t.Parallel()

	requests := map[string]int{}
	var requestsMu sync.Mutex
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestsMu.Lock()
		requests[request.URL.Host]++
		requestsMu.Unlock()
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
			return jsonResponse(request, `{"status":"ok","version":"24-June-2026","203.0.113.10":{"network":{},"location":{"country_name":"United States","country_code":"US"},"detections":{"proxy":false,"risk":9}}}`)
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
	if requests["api.ipapi.is"] != 1 {
		t.Fatalf("requests=%#v, failed canonical discovery must not be retried", requests)
	}
	if report.Status != agentapi.IPQualityStatusPartial {
		t.Fatalf("Status = %q, want partial from one timed out source", report.Status)
	}
}

func TestHTTPCollectorSameFamilyDiscoveryAmbiguityFailsServicesWithoutProviderConflict(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := `{}`
		switch request.URL.Host {
		case "api.ipapi.is":
			body = `{"ip":"203.0.113.10","version":4}`
		case "api.ipquery.io":
			if request.URL.Path == "/" {
				body = `{"ip":"203.0.113.11","risk":{"is_proxy":false}}`
			} else {
				body = `{"ip":"203.0.113.10","risk":{"is_proxy":false}}`
			}
		case "proxycheck.io":
			body = `{"status":"ok","version":"24-June-2026","203.0.113.10":{"network":{},"location":{},"detections":{"proxy":false}}}`
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
		Services:         []string{"netflix", "not-a-service"},
	}, time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC))

	if report.Status != agentapi.IPQualityStatusPartial {
		t.Fatalf("Status = %q, want partial for ambiguous service egress", report.Status)
	}
	if report.IPAddress != "203.0.113.10" {
		t.Fatalf("IPAddress = %q, want canonical IP from preferred provider", report.IPAddress)
	}
	if len(report.DiagnosticsJSON) == 0 || !strings.Contains(string(report.DiagnosticsJSON), `"ip_candidates"`) {
		t.Fatalf("DiagnosticsJSON = %s, want ip_candidates diagnostics", report.DiagnosticsJSON)
	}
	if strings.Contains(string(report.DiagnosticsJSON), `"ip_conflict":true`) ||
		!strings.Contains(string(report.DiagnosticsJSON), `"egress_ambiguous":true`) {
		t.Fatalf("DiagnosticsJSON = %s, want discovery ambiguity without final provider conflict", report.DiagnosticsJSON)
	}
	providers := providerResultsByName(report.ProviderResults)
	if providers["ipquery.io"].Status != "success" || report.Coverage.SuccessfulProviderCount != 5 {
		t.Fatalf("providers=%#v coverage=%#v, want targeted canonical requery to succeed", providers, report.Coverage)
	}
	services := serviceUnlocksByService(report.ServiceUnlocks)
	if services["netflix"].Status != "unknown" || services["netflix"].ProbeStatus != "failure" ||
		services["netflix"].ErrorCode != "egress_ambiguous" || services["netflix"].ErrorSummary != "service egress is ambiguous" {
		t.Fatalf("netflix=%#v, want ambiguous-egress failure without facts", services["netflix"])
	}
	if services["not-a-service"].ProbeStatus != "skipped" || services["not-a-service"].ErrorCode != "unsupported_service" {
		t.Fatalf("unknown service=%#v, want unsupported diagnostic", services["not-a-service"])
	}
	providersOnly := collector.Collect(context.Background(), &agentapi.IPQualityPlan{
		Enabled: true, TimeoutSeconds: 5,
	}, time.Now())
	if providersOnly.Status != agentapi.IPQualityStatusSuccess || len(providersOnly.ServiceUnlocks) != 0 {
		t.Fatalf("providers-only report=%#v, discovery ambiguity alone must not change status", providersOnly)
	}
}

func TestHTTPCollectorEquivalentIPv6CandidatesAreNotAmbiguous(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var body string
		switch request.URL.Host {
		case "api.ipapi.is":
			body = `{"ip":"2001:0db8:0:0:0:0:0:1","version":6}`
		case "api.ipquery.io":
			body = `{"ip":"2001:db8::1","risk":{"is_proxy":false}}`
		case "proxycheck.io":
			body = `{"status":"ok","version":"24-June-2026","2001:0db8:0:0:0:0:0:1":{"network":{},"location":{},"detections":{"proxy":false}}}`
		case "api.ip2location.io":
			body = `{"ip":"2001:db8::1","country_code":"US"}`
		case "ipwho.is":
			body = `{"success":true,"ip":"2001:db8::1","country_code":"US"}`
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

	if report.Status != agentapi.IPQualityStatusSuccess {
		t.Fatalf("Status = %q, want success for equivalent IPv6 candidates", report.Status)
	}
	if report.IPAddress != "2001:0db8:0:0:0:0:0:1" || report.IPVersion != 6 {
		t.Fatalf("IP facts = (%q,%d), want preferred IPv6 candidate", report.IPAddress, report.IPVersion)
	}
	if strings.Contains(string(report.DiagnosticsJSON), `"ip_conflict":true`) {
		t.Fatalf("DiagnosticsJSON = %s, must not mark equivalent IPv6 candidates as conflicting", report.DiagnosticsJSON)
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

func TestHTTPCollectorCustomLookupPreservesVersionForFlatAndNestedShapes(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantIP     string
		wantASN    string
		wantOrg    string
		wantRegion string
		wantLat    float64
		wantLon    float64
	}{
		{
			name:       "flat anonymous response with version",
			body:       `{"ip":"2001:db8::1","version":6,"asn":"AS64500 Example Transit","organization":"Example Transit","country_code":"US","country":"United States"}`,
			wantIP:     "2001:db8::1",
			wantASN:    "AS64500",
			wantOrg:    "Example Transit",
			wantRegion: "US",
		},
		{
			name: "nested ipapi response with ip_version",
			body: `{
				"ip":"2001:db8::2",
				"ip_version":6,
				"asn":{"asn":64501,"org":"Example Transit","country":"US"},
				"company":{"name":"Example Hosting","type":"hosting"},
				"location":{"country_code":"US","country":"United States","latitude":37.751,"longitude":-97.822}
			}`,
			wantIP:     "2001:db8::2",
			wantASN:    "AS64501",
			wantOrg:    "Example Transit",
			wantRegion: "US",
			wantLat:    37.751,
			wantLon:    -97.822,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()

			collector := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{
				Client:    server.Client(),
				LookupURL: server.URL,
			})
			report := collector.Collect(context.Background(), &agentapi.IPQualityPlan{
				Enabled:          true,
				TimeoutSeconds:   5,
				FrequencySeconds: 86400,
			}, time.Date(2026, time.June, 10, 5, 0, 0, 0, time.UTC))

			if report.Status != agentapi.IPQualityStatusSuccess {
				t.Fatalf("Status = %q, want success, error=%q", report.Status, report.ErrorSummary)
			}
			if report.IPAddress != test.wantIP || report.IPVersion != 6 {
				t.Fatalf("IP facts = (%q,%d), want (%q,6)", report.IPAddress, report.IPVersion, test.wantIP)
			}
			if report.ASN != test.wantASN || report.Organization != test.wantOrg || report.UseRegionCode != test.wantRegion {
				t.Fatalf("metadata = (%q,%q,%q), want (%q,%q,%q)", report.ASN, report.Organization, report.UseRegionCode, test.wantASN, test.wantOrg, test.wantRegion)
			}
			if test.wantLat != 0 && (report.Latitude == nil || *report.Latitude != test.wantLat) {
				t.Fatalf("Latitude = %v, want %v", report.Latitude, test.wantLat)
			}
			if test.wantLon != 0 && (report.Longitude == nil || *report.Longitude != test.wantLon) {
				t.Fatalf("Longitude = %v, want %v", report.Longitude, test.wantLon)
			}
		})
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

func TestHTTPCollectorServiceOutcomesKeepNormalizedInputOrder(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		switch request.URL.Host {
		case "api.ipapi.is":
			return jsonResponse(request, `{"ip":"203.0.113.10"}`)
		case "api.ipquery.io":
			return jsonResponse(request, `{"ip":"203.0.113.10"}`)
		case "proxycheck.io":
			return jsonResponse(request, `{"status":"ok","version":"24-June-2026","203.0.113.10":{"network":{},"location":{},"detections":{"proxy":false}}}`)
		case "api.ip2location.io":
			return jsonResponse(request, `{"ip":"203.0.113.10"}`)
		case "ipwho.is":
			return jsonResponse(request, `{"success":true,"ip":"203.0.113.10"}`)
		default:
			return jsonResponse(request, `{}`)
		}
	})}

	collector := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{Client: client})
	report := collector.Collect(context.Background(), &agentapi.IPQualityPlan{
		Enabled:        true,
		TimeoutSeconds: 5,
		Services:       []string{" Netflix ", "", "CHATGPT", "netflix", " reddit ", "chatgpt", "future-service"},
	}, time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC))

	if len(report.ServiceUnlocks) != 4 {
		t.Fatalf("ServiceUnlocks = %#v, want normalized outcomes", report.ServiceUnlocks)
	}
	for index, want := range []string{"netflix", "chatgpt", "reddit", "future-service"} {
		row := report.ServiceUnlocks[index]
		wantProbe := "failure"
		if want == "future-service" {
			wantProbe = "skipped"
		}
		if row.Service != want || row.Status != "unknown" || row.ProbeStatus != wantProbe {
			t.Fatalf("ServiceUnlocks[%d] = %#v, want normalized unknown service %q", index, row, want)
		}
	}
	if row := report.ServiceUnlocks[3]; row.Source != "default_probe_registry" || row.ErrorCode != "unsupported_service" {
		t.Fatalf("unknown service diagnostic = %#v", row)
	}
	if report.Status != agentapi.IPQualityStatusPartial || report.Coverage.ExpectedServiceCount != 4 ||
		report.Coverage.SkippedServiceCount != 1 || report.Coverage.SuccessfulServiceCount != 0 || report.Coverage.FailedServiceCount != 3 {
		t.Fatalf("unrecognized service responses must remain failed unknown outcomes: status=%s coverage=%#v", report.Status, report.Coverage)
	}
}

func TestHTTPCollectorSkipsDependentProvidersAndServicesWithoutCanonicalIP(t *testing.T) {
	var requestsMu sync.Mutex
	requests := map[string]int{}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestsMu.Lock()
		requests[request.URL.Host]++
		requestsMu.Unlock()
		switch request.URL.Host {
		case "api.ipapi.is":
			return jsonResponse(request, `{"error":"denied"}`)
		case "api.ipquery.io":
			return jsonResponse(request, `{"error":{"message":"denied"}}`)
		default:
			return jsonResponse(request, `{"unexpected":true}`)
		}
	})}

	collector := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{Client: client})
	report := collector.Collect(context.Background(), &agentapi.IPQualityPlan{
		Enabled:        true,
		TimeoutSeconds: 5,
		Services:       []string{"netflix"},
	}, time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC))

	if report.Status != agentapi.IPQualityStatusFailure {
		t.Fatalf("Status = %q, want failure when canonical providers fail", report.Status)
	}
	if len(report.ServiceUnlocks) != 0 {
		t.Fatalf("ServiceUnlocks = %#v, want no service probes", report.ServiceUnlocks)
	}
	providers := providerResultsByName(report.ProviderResults)
	for _, provider := range []string{"proxycheck.io", "ip2location.io", "ipwho.is"} {
		if providers[provider].ErrorCode != "missing_target_ip" {
			t.Fatalf("%s = %#v, want missing_target_ip without dependent request", provider, providers[provider])
		}
	}
	requestsMu.Lock()
	defer requestsMu.Unlock()
	if requests["api.ipapi.is"] != 1 || requests["api.ipquery.io"] != 1 || len(requests) != 2 {
		t.Fatalf("requests = %#v, want only canonical sources", requests)
	}
}

func TestHTTPCollectorDefaultSourcesPreCancelledContextDoesNotStartTransport(t *testing.T) {
	var requestsMu sync.Mutex
	requests := map[string]int{}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestsMu.Lock()
		requests[request.URL.Host]++
		requestsMu.Unlock()
		return nil, request.Context().Err()
	})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	collector := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{Client: client})
	report := collector.Collect(ctx, &agentapi.IPQualityPlan{
		Enabled:          true,
		TimeoutSeconds:   5,
		FrequencySeconds: 86400,
		Services:         []string{"netflix", "chatgpt"},
	}, time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC))

	requestsMu.Lock()
	requestCount := len(requests)
	requestsMu.Unlock()
	if requestCount != 0 {
		t.Fatalf("requests = %#v, want no transport starts for pre-cancelled context", requests)
	}
	providers := providerResultsByName(report.ProviderResults)
	for _, provider := range []string{"ipapi.is", "ipquery.io"} {
		if providers[provider].Status != "failure" || providers[provider].ErrorCode == "" {
			t.Fatalf("%s = %#v, want complete cancellation diagnostic", provider, providers[provider])
		}
	}
	for _, provider := range []string{"proxycheck.io", "ip2location.io", "ipwho.is"} {
		if providers[provider].ErrorCode != "missing_target_ip" {
			t.Fatalf("%s = %#v, want missing_target_ip diagnostic", provider, providers[provider])
		}
	}
	if len(report.ServiceUnlocks) != 0 {
		t.Fatalf("ServiceUnlocks = %#v, want no service probes without canonical IP", report.ServiceUnlocks)
	}
}

func TestHTTPCollectorDefaultSourcesParentCancellationJoinsInFlightRequests(t *testing.T) {
	var requestsMu sync.Mutex
	requests := map[string]int{}
	started := make(chan string, 2)
	returned := false
	postReturnStarts := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requestsMu.Lock()
		requests[request.URL.Host]++
		if returned {
			postReturnStarts++
		}
		requestsMu.Unlock()
		select {
		case started <- request.URL.Host:
		default:
		}
		if request.URL.Host != "api.ipapi.is" && request.URL.Host != "api.ipquery.io" {
			return nil, request.Context().Err()
		}
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	collector := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{Client: client})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan agentapi.IPQualityReportPayload, 1)
	go func() {
		done <- collector.Collect(ctx, &agentapi.IPQualityPlan{
			Enabled:          true,
			TimeoutSeconds:   5,
			FrequencySeconds: 86400,
			Services:         []string{"netflix"},
		}, time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC))
	}()

	<-started
	cancel()
	report := <-done

	requestsMu.Lock()
	returned = true
	postReturnCount := postReturnStarts
	snapshot := make(map[string]int, len(requests))
	for host, count := range requests {
		snapshot[host] = count
	}
	requestsMu.Unlock()
	if postReturnCount != 0 {
		t.Fatalf("transport starts after Collect returned = %d, want none", postReturnCount)
	}
	for host := range snapshot {
		if host != "api.ipapi.is" && host != "api.ipquery.io" {
			t.Fatalf("requests = %#v, want cancellation before dependent/service stages", snapshot)
		}
	}
	providers := providerResultsByName(report.ProviderResults)
	for _, provider := range []string{"ipapi.is", "ipquery.io"} {
		if providers[provider].Status != "failure" || providers[provider].ErrorCode == "" {
			t.Fatalf("%s = %#v, want in-flight cancellation diagnostic", provider, providers[provider])
		}
	}
	for _, provider := range []string{"proxycheck.io", "ip2location.io", "ipwho.is"} {
		if providers[provider].ErrorCode != "missing_target_ip" {
			t.Fatalf("%s = %#v, want missing_target_ip diagnostic", provider, providers[provider])
		}
	}
	if len(report.ServiceUnlocks) != 0 {
		t.Fatalf("ServiceUnlocks = %#v, want no post-cancellation probes", report.ServiceUnlocks)
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

func TestHTTPCollectorIPQueryCanonicalFallbackAndFailureMatrix(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		body     string
		timeout  bool
		wantCode string
	}{
		{name: "JSON fallback", body: `{"ip":"203.0.113.10","isp":{"asn":"AS64500","org":"Example"},"location":{"country_code":"US","country":"United States"},"risk":{"is_proxy":false,"risk_score":9}}`},
		{name: "plain text", body: "203.0.113.10", wantCode: "non_json_response"},
		{name: "HTML", body: "<html>challenge</html>", wantCode: "non_json_response"},
		{name: "empty", body: "", wantCode: "non_json_response"},
		{name: "business error", body: `{"error":"denied","ip":"203.0.113.10"}`, wantCode: "provider_error"},
		{name: "timeout", timeout: true, wantCode: "timeout"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var mu sync.Mutex
			requests := map[string]string{}
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				mu.Lock()
				requests[request.URL.Host] = request.URL.String()
				mu.Unlock()
				switch request.URL.Host {
				case "api.ipapi.is":
					return jsonResponse(request, `{"error":"unavailable"}`)
				case "api.ipquery.io":
					if request.URL.Path == "/" && request.URL.RawQuery == "" {
						response, err := jsonResponse(request, "203.0.113.10")
						response.Header.Set("Content-Type", "text/plain")
						return response, err
					}
					if request.URL.Path != "/" || request.URL.Query().Get("format") != "json" {
						t.Errorf("unexpected IPQuery URL %s", request.URL)
					}
					if scenario.timeout {
						<-request.Context().Done()
						return nil, request.Context().Err()
					}
					return jsonResponse(request, scenario.body)
				case "proxycheck.io":
					if request.URL.Path != "/v3/203.0.113.10" || request.URL.Query().Get("ver") != "24-June-2026" {
						t.Errorf("proxycheck target = %s", request.URL)
					}
					return jsonResponse(request, `{"status":"ok","version":"24-June-2026","203.0.113.10":{"network":{},"location":{},"detections":{"proxy":false}}}`)
				case "api.ip2location.io":
					if request.URL.Query().Get("ip") != "203.0.113.10" {
						t.Errorf("ip2location target = %s", request.URL)
					}
					return jsonResponse(request, `{"ip":"203.0.113.10","country_code":"US","is_proxy":false}`)
				case "ipwho.is":
					if request.URL.Path != "/203.0.113.10" {
						t.Errorf("ipwho target = %s", request.URL)
					}
					return jsonResponse(request, `{"success":true,"ip":"203.0.113.10","country_code":"US"}`)
				default:
					t.Errorf("unexpected network request %s", request.URL)
					return jsonResponse(request, `{}`)
				}
			})}
			report := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{Client: client}).Collect(
				context.Background(), &agentapi.IPQualityPlan{Enabled: true, TimeoutSeconds: 1}, time.Now())
			providers := providerResultsByName(report.ProviderResults)
			if scenario.wantCode == "" {
				if report.IPAddress != "203.0.113.10" || report.Status != agentapi.IPQualityStatusPartial || providers["ipquery.io"].Status != "success" {
					t.Fatalf("fallback report = %#v", report)
				}
				for _, name := range []string{"proxycheck.io", "ip2location.io", "ipwho.is"} {
					if providers[name].Status != "success" {
						t.Errorf("dependent %s = %#v", name, providers[name])
					}
				}
				if len(requests) != 5 || requests["api.ipquery.io"] != "https://api.ipquery.io/?format=json" {
					t.Fatalf("fallback requests = %#v", requests)
				}
				if report.Coverage.ExpectedServiceCount != 0 || len(report.ServiceUnlocks) != 0 {
					t.Fatalf("coverage = %#v", report.Coverage)
				}
			} else {
				if providers["ipquery.io"].Status != "failure" || providers["ipquery.io"].ErrorCode != scenario.wantCode {
					t.Fatalf("IPQuery failure = %#v, want %s", providers["ipquery.io"], scenario.wantCode)
				}
				if report.Status != agentapi.IPQualityStatusFailure || len(requests) != 2 || len(report.ServiceUnlocks) != 0 {
					t.Fatalf("failed canonical collection requested dependents/services: requests=%#v report=%#v", requests, report)
				}
			}
		})
	}
}

func TestHTTPCollectorCustomServiceBusinessConclusionMatrix(t *testing.T) {
	cases := []struct {
		name, body, wantStatus, wantProbe, wantCode, wantRegion, wantType string
		httpStatus                                                        int
	}{
		{name: "unlocked", body: `{"status":" UnLoCkEd ","region":"JP","unlock_type":"full"}`, wantStatus: "unlocked", wantProbe: "success", wantRegion: "JP", wantType: "full"},
		{name: "blocked business error", body: `{"status":"blocked","error_code":"region_denied","message":"business denied"}`, wantStatus: "blocked", wantProbe: "success", wantCode: "region_denied"},
		{name: "partial aliases", body: `{"unlock_status":"partial","unlock_region":"US","type":"originals"}`, wantStatus: "partial", wantProbe: "success", wantRegion: "US", wantType: "originals"},
		{name: "bool true", body: `{"unlocked":true,"country":"DE"}`, wantStatus: "unlocked", wantProbe: "success", wantRegion: "DE"},
		{name: "bool false", body: `{"unlocked":false,"country_code":"FR"}`, wantStatus: "blocked", wantProbe: "success", wantRegion: "FR"},
		{name: "status precedence", body: `{"status":"blocked","unlock_status":"unlocked","unlocked":true}`, wantStatus: "blocked", wantProbe: "success"},
		{name: "empty primary status uses bool", body: `{"status":"","unlock_status":"partial","unlocked":true}`, wantStatus: "unlocked", wantProbe: "success"},
		{name: "unknown beats bool", body: `{"status":"unknown","unlocked":true,"region":"US","unlock_type":"full"}`, wantCode: "invalid_response"},
		{name: "empty object", body: `{}`, wantCode: "invalid_response"},
		{name: "missing conclusion", body: `{"region":"US","unlock_type":"full"}`, wantCode: "invalid_response"},
		{name: "illegal status", body: `{"status":"normal","region":"US","unlock_type":"full"}`, wantCode: "invalid_response"},
		{name: "empty body", body: "", wantCode: "probe_failed"},
		{name: "whitespace", body: " \n\t", wantCode: "probe_failed"},
		{name: "HTML", body: "<html>not JSON</html>", wantCode: "probe_failed"},
		{name: "challenge", body: `{"challenge":"captcha"}`, wantCode: "invalid_response"},
	}
	for _, status := range []int{403, 451, 404, 429, 503} {
		cases = append(cases, struct {
			name, body, wantStatus, wantProbe, wantCode, wantRegion, wantType string
			httpStatus                                                        int
		}{name: fmt.Sprintf("HTTP %d", status), body: `{"status":"blocked","region":"US"}`, httpStatus: status, wantCode: "probe_failed"})
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/lookup":
					_, _ = io.WriteString(w, `{"ip":"203.0.113.10","version":4}`)
				case "/service/good":
					_, _ = io.WriteString(w, `{"status":"unlocked","region":"JP"}`)
				case "/service/subject":
					if scenario.httpStatus != 0 {
						w.WriteHeader(scenario.httpStatus)
					}
					_, _ = io.WriteString(w, scenario.body)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			report := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{
				Client: server.Client(), LookupURL: server.URL + "/lookup", ServiceURL: server.URL + "/service/{service}",
			}).Collect(context.Background(), &agentapi.IPQualityPlan{Enabled: true, TimeoutSeconds: 5, Services: []string{"good", "subject"}}, time.Now())
			if len(report.ServiceUnlocks) != 2 {
				t.Fatalf("service rows = %#v", report.ServiceUnlocks)
			}
			if good := report.ServiceUnlocks[0]; good.Status != "unlocked" || good.ProbeStatus != "success" || good.Region != "JP" {
				t.Fatalf("successful sibling lost: %#v", good)
			}
			wantStatus, wantProbe, wantReport := scenario.wantStatus, scenario.wantProbe, agentapi.IPQualityStatusSuccess
			if wantProbe == "" {
				wantStatus = "unknown"
				wantProbe = "failure"
				wantReport = agentapi.IPQualityStatusPartial
			}
			row := report.ServiceUnlocks[1]
			if row.Status != wantStatus || row.ProbeStatus != wantProbe || row.ErrorCode != scenario.wantCode ||
				row.Region != scenario.wantRegion || row.UnlockType != scenario.wantType || report.Status != wantReport {
				t.Fatalf("row=%#v report=%s want status=%s probe=%s code=%s region=%s type=%s report=%s",
					row, report.Status, wantStatus, wantProbe, scenario.wantCode, scenario.wantRegion, scenario.wantType, wantReport)
			}
			if scenario.wantCode == "invalid_response" && row.ErrorSummary != "service response did not establish a business conclusion" {
				t.Fatalf("invalid response diagnostic = %#v", row)
			}
			if scenario.name == "blocked business error" && row.ErrorSummary != "business denied" {
				t.Fatalf("business diagnostic lost: %#v", row)
			}
		})
	}
}

func TestHTTPCollectorDualStackPrefersIPv4AndRequeriesOtherCanonical(t *testing.T) {
	for _, ipv4Provider := range []string{"api.ipapi.is", "api.ipquery.io"} {
		t.Run(ipv4Provider, func(t *testing.T) {
			const targetIP = "203.0.113.10"
			const ipv6IP = "2001:db8::1"
			var mu sync.Mutex
			requests := map[string][]string{}
			ipqueryDiscovered := make(chan struct{})
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				mu.Lock()
				requests[request.URL.Host] = append(requests[request.URL.Host], request.URL.String())
				mu.Unlock()
				switch request.URL.Host {
				case "api.ipapi.is", "api.ipquery.io":
					discovery := request.URL.Query().Get("q") == "" && request.URL.Path == "/"
					if request.URL.Host == "api.ipapi.is" && request.URL.Path == "" {
						discovery = request.URL.Query().Get("q") == ""
					}
					ip := targetIP
					if discovery {
						if request.URL.Host == "api.ipquery.io" {
							close(ipqueryDiscovered)
						} else {
							// The lower-priority registry entry responds first.
							select {
							case <-ipqueryDiscovered:
							case <-request.Context().Done():
								return nil, request.Context().Err()
							}
						}
						if request.URL.Host != ipv4Provider {
							ip = ipv6IP
						}
					} else if request.URL.Host == "api.ipapi.is" {
						if request.URL.Query().Get("q") != targetIP {
							return nil, fmt.Errorf("unexpected targeted ipapi URL %s", request.URL.String())
						}
					} else if request.URL.Path != "/"+targetIP {
						return nil, fmt.Errorf("unexpected targeted IPQuery URL %s", request.URL.String())
					}
					// A provider-supplied family must not override target identity.
					return jsonResponse(request, fmt.Sprintf(`{"ip":%q,"version":6,"country_code":"US"}`, ip))
				case "proxycheck.io":
					if !strings.Contains(request.URL.Path, targetIP) {
						return nil, fmt.Errorf("unexpected ProxyCheck target %s", request.URL.String())
					}
					return jsonResponse(request, `{"status":"ok","version":"24-June-2026","203.0.113.10":{"network":{},"location":{},"detections":{"proxy":false}}}`)
				case "api.ip2location.io":
					if request.URL.Query().Get("ip") != targetIP {
						return nil, fmt.Errorf("unexpected IP2Location target %s", request.URL.String())
					}
					return jsonResponse(request, `{"ip":"203.0.113.10","country_code":"US"}`)
				case "ipwho.is":
					if request.URL.Path != "/"+targetIP {
						return nil, fmt.Errorf("unexpected IPWho target %s", request.URL.String())
					}
					return jsonResponse(request, `{"success":true,"ip":"203.0.113.10","country_code":"US"}`)
				default:
					return nil, fmt.Errorf("unexpected request %s", request.URL.String())
				}
			})}
			report := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{Client: client}).Collect(
				context.Background(), &agentapi.IPQualityPlan{Enabled: true, TimeoutSeconds: 5}, time.Now())
			if report.IPAddress != targetIP || report.IPVersion != 4 || report.Status != agentapi.IPQualityStatusSuccess {
				t.Fatalf("report=%#v, want successful selected IPv4 report", report)
			}
			var diagnostics struct {
				CollectionRevision int               `json:"collection_revision"`
				Family             int               `json:"ip_address_family"`
				Discovery          map[string]string `json:"discovery_ip_candidates"`
				Final              map[string]string `json:"ip_candidates"`
				Conflict           bool              `json:"ip_conflict"`
				Ambiguous          bool              `json:"egress_ambiguous"`
			}
			if err := json.Unmarshal(report.DiagnosticsJSON, &diagnostics); err != nil {
				t.Fatal(err)
			}
			if diagnostics.CollectionRevision != 3 || diagnostics.Family != 4 ||
				len(diagnostics.Discovery) != 2 || len(diagnostics.Final) != 5 || diagnostics.Conflict || diagnostics.Ambiguous {
				t.Fatalf("diagnostics=%s, want normal dual-stack discovery without conflict", report.DiagnosticsJSON)
			}
			for provider, ip := range diagnostics.Final {
				if ip != targetIP {
					t.Fatalf("%s final IP=%s, want selected target", provider, ip)
				}
			}
			mu.Lock()
			defer mu.Unlock()
			requestCount := 0
			for host, urls := range requests {
				requestCount += len(urls)
				want := 1
				if (host == "api.ipapi.is" || host == "api.ipquery.io") && host != ipv4Provider {
					want = 2
				}
				if len(urls) != want {
					t.Fatalf("%s requests=%#v, want %d", host, urls, want)
				}
			}
			if requestCount != 6 {
				t.Fatalf("requests=%#v, want exactly six database requests", requests)
			}
		})
	}
}

func TestHTTPCollectorTargetMismatchCannotPolluteReportFacts(t *testing.T) {
	for _, mismatchingProvider := range []string{"api.ipapi.is", "api.ipquery.io", "api.ip2location.io", "ipwho.is"} {
		t.Run(mismatchingProvider, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.Host == mismatchingProvider {
					// IPv6 discovery loses to IPv4, then the canonical requery
					// returns a different IPv4 with facts that must be discarded.
					ip := "203.0.113.11"
					if mismatchingProvider == "api.ipapi.is" && request.URL.Query().Get("q") == "" {
						ip = "2001:db8::1"
					}
					return jsonResponse(request, fmt.Sprintf(`{"success":true,"ip":%q,"country_code":"XX","country":"Wrong region","organization":"Wrong organization","asn":64599,"latitude":66,"longitude":77,"risk":{"is_vpn":true,"risk_score":99},"is_vpn":true,"risk_level":"high"}`, ip))
				}
				if request.URL.Host == "api.ipapi.is" {
					return jsonResponse(request, `{"ip":"203.0.113.10"}`)
				}
				switch request.URL.Host {
				case "api.ipquery.io", "api.ip2location.io":
					return jsonResponse(request, `{"ip":"203.0.113.10"}`)
				case "proxycheck.io":
					return jsonResponse(request, `{"status":"ok","version":"24-June-2026","203.0.113.10":{"network":{},"location":{},"detections":{"proxy":false}}}`)
				case "ipwho.is":
					return jsonResponse(request, `{"success":true,"ip":"203.0.113.10"}`)
				default:
					return nil, fmt.Errorf("unexpected request %s", request.URL.String())
				}
			})}
			report := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{Client: client}).Collect(
				context.Background(), &agentapi.IPQualityPlan{Enabled: true, TimeoutSeconds: 5}, time.Now())
			providerName := map[string]string{"api.ipapi.is": "ipapi.is", "api.ipquery.io": "ipquery.io", "api.ip2location.io": "ip2location.io", "ipwho.is": "ipwho.is"}[mismatchingProvider]
			result := providerResultsByName(report.ProviderResults)[providerName]
			if result.Status != "failure" || result.ErrorCode != "ip_mismatch" || result.ErrorSummary != "provider returned a different IP" {
				t.Fatalf("result=%#v, want precise mismatch failure", result)
			}
			if result.RegionCode != "" || result.RegionName != "" || result.RiskLevel != "" || result.RiskScore != "" ||
				result.IsVPN != nil || result.IsProxy != nil || result.ExtraJSON != nil {
				t.Fatalf("mismatch retained normalized facts=%#v", result)
			}
			if report.Status != agentapi.IPQualityStatusPartial || report.IPAddress != "203.0.113.10" || report.IPVersion != 4 ||
				report.UseRegionCode != "" || report.UseRegionName != "" || report.ASN != "" || report.Organization != "" ||
				report.Latitude != nil || report.Longitude != nil || report.RiskLevel != "" {
				t.Fatalf("mismatch polluted summary=%#v", report)
			}
			var diagnostics struct {
				Final    map[string]string `json:"ip_candidates"`
				Conflict bool              `json:"ip_conflict"`
			}
			if err := json.Unmarshal(report.DiagnosticsJSON, &diagnostics); err != nil {
				t.Fatal(err)
			}
			if !diagnostics.Conflict || len(diagnostics.Final) != 4 || diagnostics.Final[providerName] != "" {
				t.Fatalf("diagnostics=%s, want explicit mismatch excluded from final candidates", report.DiagnosticsJSON)
			}
			if !strings.Contains(string(report.RawJSON), `"Wrong organization"`) {
				t.Fatalf("raw=%s, want retained raw mismatch response", report.RawJSON)
			}
		})
	}
}

type cancellationBarrierBody struct {
	ctx       context.Context
	host      string
	cancelled chan<- string
	release   <-chan struct{}
	closed    chan<- string
}

func (body *cancellationBarrierBody) Read([]byte) (int, error) {
	<-body.ctx.Done()
	body.cancelled <- body.host
	<-body.release
	return 0, body.ctx.Err()
}

func (body *cancellationBarrierBody) Close() error {
	body.closed <- body.host
	return nil
}

func TestHTTPCollectorCancellationJoinsDependentRequeryAndBodyReaders(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan string, 4)
	cancelled := make(chan string, 4)
	closed := make(chan string, 4)
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "api.ipapi.is" {
			return jsonResponse(request, `{"ip":"203.0.113.10"}`)
		}
		if request.URL.Host == "api.ipquery.io" && request.URL.Path == "/" {
			return jsonResponse(request, `{"ip":"2001:db8::1"}`)
		}
		started <- request.URL.Host
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: &cancellationBarrierBody{
				ctx: request.Context(), host: request.URL.Host, cancelled: cancelled, release: release, closed: closed,
			},
			Request: request,
		}, nil
	})}
	done := make(chan agentapi.IPQualityReportPayload, 1)
	go func() {
		done <- agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{Client: client}).Collect(
			ctx, &agentapi.IPQualityPlan{Enabled: true, TimeoutSeconds: 15}, time.Now())
	}()
	for range 4 {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("all dependent and targeted requery workers must start concurrently")
		}
	}
	cancel()
	for range 4 {
		select {
		case <-cancelled:
		case <-time.After(3 * time.Second):
			t.Fatal("parent cancellation did not reach all response readers")
		}
	}
	select {
	case report := <-done:
		t.Fatalf("Collect returned before response readers exited: %#v", report)
	default:
	}
	close(release)
	var report agentapi.IPQualityReportPayload
	select {
	case report = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Collect did not join cancelled workers")
	}
	if len(closed) != 4 {
		t.Fatalf("closed response bodies=%d, want all four before return", len(closed))
	}
	if report.Status != agentapi.IPQualityStatusPartial || report.Coverage.SuccessfulProviderCount != 1 || report.Coverage.FailedProviderCount != 4 {
		t.Fatalf("report=%#v, want final joined failure rows with selected canonical retained", report)
	}
	for _, result := range report.ProviderResults {
		if result.SourceType == "default" && result.Provider != "ipapi.is" && result.Status != "failure" {
			t.Fatalf("worker outcome not finalized before return=%#v", result)
		}
	}
}

func TestHTTPCollectorProviderGroupsAndRiskDoNotDependOnServiceResults(t *testing.T) {
	for _, services := range [][]string{nil, {"chatgpt"}} {
		t.Run(fmt.Sprintf("services_%d", len(services)), func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				switch request.URL.Host {
				case "api.ipapi.is":
					return jsonResponse(request, `{"ip":"203.0.113.10","version":6,"asn":"AS64500","company":"Example Network","country":"Singapore","registered_country_code":"sg","latitude":1.25,"risk_level":"low"}`)
				case "api.ipquery.io":
					return jsonResponse(request, `{"ip":"203.0.113.10","isp":{"asn":"64501","org":"Other Network"},"location":{"country_code":"JP","country":"Japan","longitude":139.5},"risk":{"is_proxy":false,"is_vpn":true,"risk_score":0}}`)
				case "proxycheck.io":
					return jsonResponse(request, `{"status":"ok","version":"24-June-2026","203.0.113.10":{"network":{"asn":"64502","organisation":"Third Network"},"location":{"country_code":"US","country_name":"United States"},"detections":{"proxy":false,"risk":90.5}}}`)
				case "api.ip2location.io":
					return jsonResponse(request, `{"ip":"203.0.113.10","country_code":"US","country_name":"United States"}`)
				case "ipwho.is":
					return jsonResponse(request, `{"success":true,"ip":"203.0.113.10","country_code":"US","country":"United States"}`)
				case "api.openai.com", "ios.chat.openai.com":
					// Unrecognized responses cannot become service positives.
					return jsonResponse(request, `{}`)
				default:
					return nil, fmt.Errorf("unexpected test endpoint %s", request.URL)
				}
			})}
			report := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{Client: client}).Collect(
				context.Background(), &agentapi.IPQualityPlan{Enabled: true, TimeoutSeconds: 5, Services: services}, time.Now())
			if report.IPAddress != "203.0.113.10" || report.IPVersion != 4 ||
				report.UseRegionName != "Singapore" || report.UseRegionCode != "" ||
				report.RegisteredRegionCode != "SG" || report.RegisteredRegionName != "" ||
				report.ASN != "AS64500" || report.Organization != "Example Network" ||
				report.Latitude == nil || *report.Latitude != 1.25 || report.Longitude != nil || report.RiskLevel != "high" {
				t.Fatalf("default report mixed groups, overrode identity or hid maximum risk: %#v", report)
			}
			rows := providerResultsByName(report.ProviderResults)
			if rows["ipapi.is"].RegionCode != "" || rows["ipapi.is"].CompanyType != "" ||
				rows["ipquery.io"].RiskScore != "0" || rows["proxycheck.io"].RiskScore != "90.5" {
				t.Fatalf("provider facts=%#v", rows)
			}
			wantStatus := agentapi.IPQualityStatusSuccess
			if len(services) > 0 {
				wantStatus = agentapi.IPQualityStatusPartial
				if len(report.ServiceUnlocks) != 1 || report.ServiceUnlocks[0].Status != "unknown" || report.ServiceUnlocks[0].ProbeStatus != "failure" {
					t.Fatalf("generic JSON became service facts=%#v", report.ServiceUnlocks)
				}
			}
			if report.Status != wantStatus {
				t.Fatalf("status=%s want %s without changing risk", report.Status, wantStatus)
			}
		})
	}
}
