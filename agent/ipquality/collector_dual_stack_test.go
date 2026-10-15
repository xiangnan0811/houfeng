package ipquality_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	agentipquality "houfeng/agent/ipquality"
	"houfeng/internal/contracts/agentapi"
)

func dualLocalAddresses() ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("192.168.1.2"), netip.MustParseAddr("fd00::2")}, nil
}

// Each transport is a family boundary, not a second implementation of the
// collector's discovery, target validation, provider parsing or service rules.
func dualFixtureResponse(request *http.Request, ip, region, asn string) (*http.Response, error) {
	switch request.URL.Host {
	case "api.ipapi.is", "api.ipquery.io":
		return jsonResponse(request, fmt.Sprintf(`{"ip":%q,"asn":%q,"country_code":%q}`, ip, asn, region))
	case "proxycheck.io":
		if !strings.Contains(request.URL.Path, ip) {
			return nil, errors.New("wrong family target")
		}
		return jsonResponse(request, fmt.Sprintf(`{"status":"ok","version":"24-June-2026",%q:{"network":{},"location":{"country_code":%q},"detections":{"proxy":false}}}`, ip, region))
	case "api.ip2location.io":
		if request.URL.Query().Get("ip") != ip {
			return nil, errors.New("wrong family target")
		}
		return jsonResponse(request, fmt.Sprintf(`{"ip":%q,"country_code":%q}`, ip, region))
	case "ipwho.is":
		if request.URL.Path != "/"+ip {
			return nil, errors.New("wrong family target")
		}
		return jsonResponse(request, fmt.Sprintf(`{"success":true,"ip":%q,"country_code":%q}`, ip, region))
	case "www.reddit.com":
		return jsonResponse(request, fmt.Sprintf(`<html><shreddit-app country="%s"></shreddit-app></html>`, region))
	default:
		return nil, errors.New("unexpected endpoint")
	}
}

func TestHTTPCollectorDualStackKeepsIndependentFactsAndTransports(t *testing.T) {
	var mu sync.Mutex
	self, services := [2]int{}, [2]int{}
	familyClient := func(index int, ip, region, asn string) *http.Client {
		return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			mu.Lock()
			if r.URL.Host == "api.ipapi.is" || r.URL.Host == "api.ipquery.io" {
				self[index]++
			}
			if r.URL.Host == "www.reddit.com" {
				services[index]++
			}
			mu.Unlock()
			return dualFixtureResponse(r, ip, region, asn)
		})}
	}
	generic := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Errorf("family collection used generic transport for %s", r.URL)
		return nil, errors.New("generic transport must not be used")
	})}
	collector := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{
		Client: generic, Client4: familyClient(0, "198.51.100.10", "US", "AS64500"), Client6: familyClient(1, "2001:db8::10", "JP", "AS64501"), LocalAddresses: dualLocalAddresses,
	})
	now := time.Now().UTC()
	reports := collector.Collect(context.Background(), &agentapi.IPQualityPlan{Enabled: true, TimeoutSeconds: 5, Services: []string{"reddit"}}, now)
	if len(reports) != 2 {
		t.Fatalf("reports=%#v", reports)
	}
	for index, expected := range []struct {
		ip, region, asn string
		family          int
	}{{"198.51.100.10", "US", "AS64500", 4}, {"2001:db8::10", "JP", "AS64501", 6}} {
		r := reports[index]
		m := agentapi.ParseCollectionMetadata(r.DiagnosticsJSON)
		if r.Status != "partial" || r.IPAddress != expected.ip || r.IPVersion != expected.family || r.ASN != expected.asn || r.UseRegionCode != expected.region || !r.ObservedAt.Equal(now) || !m.Valid || m.Availability != "available" {
			t.Fatalf("family report=%#v metadata=%#v", r, m)
		}
		if len(r.ServiceUnlocks) != 1 || r.ServiceUnlocks[0].Status != "unlocked" || r.ServiceUnlocks[0].Region != expected.region {
			t.Fatalf("family services=%#v", r.ServiceUnlocks)
		}
		assertRejectedDetectorGeoAttempts(t, r)
	}
	if agentapi.ParseCollectionMetadata(reports[0].DiagnosticsJSON).CollectionID != agentapi.ParseCollectionMetadata(reports[1].DiagnosticsJSON).CollectionID {
		t.Fatal("families do not share round identity")
	}
	if self != [2]int{2, 2} || services != [2]int{1, 1} {
		t.Fatalf("self=%v services=%v", self, services)
	}
}

func TestHTTPCollectorUnavailableFamilyMakesNoExternalRequests(t *testing.T) {
	for _, available := range []int{4, 6} {
		t.Run(fmt.Sprint(available), func(t *testing.T) {
			ip, region := "198.51.100.10", "US"
			local := ipv4LocalAddresses
			if available == 6 {
				ip, local = "2001:db8::10", ipv6LocalAddresses
			}
			good := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { return dualFixtureResponse(r, ip, region, "AS64500") })}
			absent := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				t.Errorf("absent family requested %s", r.URL)
				return nil, errors.New("must not request")
			})}
			four, six := good, absent
			if available == 6 {
				four, six = absent, good
			}
			reports := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{Client: good, Client4: four, Client6: six, LocalAddresses: local}).Collect(context.Background(), &agentapi.IPQualityPlan{Enabled: true, TimeoutSeconds: 5, Services: []string{"reddit"}}, time.Now())
			missing := reports[1]
			if available == 6 {
				missing = reports[0]
			}
			if missing.Status != "failure" || missing.ErrorCode != "address_family_unavailable" || agentapi.ParseCollectionMetadata(missing.DiagnosticsJSON).Availability != "unavailable" {
				t.Fatalf("missing=%#v", missing)
			}
			if missing.Coverage.FailedProviderCount != 0 || missing.Coverage.SkippedProviderCount != 5 || missing.Coverage.NotConfiguredProviderCount != 0 || missing.Coverage.SkippedServiceCount != 1 || missing.Coverage.FailedServiceCount != 0 {
				t.Fatalf("coverage=%#v", missing.Coverage)
			}
		})
	}
}

func TestHTTPCollectorUnknownAvailabilityStillAttemptsBothFamilies(t *testing.T) {
	var mu sync.Mutex
	requests := 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		requests++
		mu.Unlock()
		return nil, errors.New("transport failed")
	})}
	reports := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{Client: client, LocalAddresses: func() ([]netip.Addr, error) { return nil, errors.New("interface enumeration failed") }}).Collect(context.Background(), &agentapi.IPQualityPlan{Enabled: true, TimeoutSeconds: 1}, time.Now())
	if requests != 4 || len(reports) != 2 {
		t.Fatalf("requests=%d reports=%#v", requests, reports)
	}
	for _, r := range reports {
		if r.Status != "failure" || r.ErrorCode != "lookup_failed" || agentapi.ParseCollectionMetadata(r.DiagnosticsJSON).Availability != "unknown" {
			t.Fatalf("report=%#v", r)
		}
	}
}

func TestHTTPCollectorCanonicalBarrierAcrossFamilies(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	fourDone := make(chan struct{}, 2)
	dependentStarted := make(chan struct{}, 8)
	var mu sync.Mutex
	canonicalDone, earlyDependent := false, false
	dependent := func(r *http.Request, ip, region string) (*http.Response, error) {
		mu.Lock()
		if !canonicalDone {
			earlyDependent = true
		}
		mu.Unlock()
		dependentStarted <- struct{}{}
		return dualFixtureResponse(r, ip, region, "AS64500")
	}
	four := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.ipapi.is" && r.URL.Host != "api.ipquery.io" {
			return dependent(r, "198.51.100.10", "US")
		}
		response, err := dualFixtureResponse(r, "198.51.100.10", "US", "AS64500")
		fourDone <- struct{}{}
		return response, err
	})}
	six := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.ipapi.is" && r.URL.Host != "api.ipquery.io" {
			return dependent(r, "2001:db8::10", "JP")
		}
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return dualFixtureResponse(r, "2001:db8::10", "JP", "AS64501")
	})}
	generic := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Errorf("family collection used generic transport for %s", r.URL)
		return nil, errors.New("generic transport must not be used")
	})}
	done := make(chan []agentapi.IPQualityReportPayload, 1)
	go func() {
		done <- agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{Client: generic, Client4: four, Client6: six, LocalAddresses: dualLocalAddresses}).Collect(context.Background(), &agentapi.IPQualityPlan{Enabled: true, TimeoutSeconds: 5}, time.Now())
	}()
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("canonical phase did not start both IPv6 requests")
		}
	}
	for range 2 {
		select {
		case <-fourDone:
		case <-time.After(time.Second):
			t.Fatal("IPv4 canonical phase did not finish")
		}
	}
	select {
	case <-dependentStarted:
		close(release)
		<-done
		t.Fatal("dependent started while IPv6 discovery still blocked")
	case <-time.After(50 * time.Millisecond):
	}
	mu.Lock()
	canonicalDone = true
	mu.Unlock()
	close(release)
	select {
	case reports := <-done:
		if len(reports) != 2 || reports[0].Status != "partial" || reports[1].Status != "partial" {
			t.Fatalf("reports=%#v", reports)
		}
		for _, report := range reports {
			assertRejectedDetectorGeoAttempts(t, report)
		}
	case <-time.After(time.Second):
		t.Fatal("round did not finish")
	}
	if earlyDependent {
		t.Fatal("dependent request crossed canonical barrier")
	}
}

func TestHTTPCollectorSharedDeadlinePreservesFastFamilyTerminal(t *testing.T) {
	four := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return dualFixtureResponse(r, "198.51.100.10", "US", "AS64500")
	})}
	six := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })}
	started := time.Now()
	reports := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{Client: four, Client4: four, Client6: six, LocalAddresses: dualLocalAddresses}).Collect(context.Background(), &agentapi.IPQualityPlan{Enabled: true, TimeoutSeconds: 1}, started)
	if time.Since(started) > 1500*time.Millisecond {
		t.Fatal("per-family timeouts multiplied total budget")
	}
	if len(reports) != 2 || reports[0].Status != "partial" || reports[1].Status != "failure" || reports[1].IPAddress != "::" || reports[1].ErrorCode != "lookup_failed" || agentapi.ParseCollectionMetadata(reports[1].DiagnosticsJSON).Availability != "available" {
		t.Fatalf("reports=%#v", reports)
	}
	if reports[0].IPAddress != "198.51.100.10" || reports[0].IPVersion != 4 || reports[1].IPVersion != 6 ||
		!reports[0].ObservedAt.Equal(started.UTC()) || !reports[1].ObservedAt.Equal(started.UTC()) ||
		agentapi.ParseCollectionMetadata(reports[0].DiagnosticsJSON).CollectionID != agentapi.ParseCollectionMetadata(reports[1].DiagnosticsJSON).CollectionID {
		t.Fatalf("deadline lost independent family terminals or round identity: %#v", reports)
	}
}

func TestHTTPCollectorSharesProviderAndServiceConcurrencyLimits(t *testing.T) {
	var mu sync.Mutex
	providerActive, providerMax, serviceActive, serviceMax := 0, 0, 0, 0
	serviceStarted := make(chan struct{}, 32)
	releaseServices := make(chan struct{})
	retainedServices := []string{"disney-plus", "tiktok", "reddit"}
	enter := func(provider bool) func() {
		mu.Lock()
		if provider {
			providerActive++
			providerMax = max(providerMax, providerActive)
		} else {
			serviceActive++
			serviceMax = max(serviceMax, serviceActive)
		}
		mu.Unlock()
		return func() {
			mu.Lock()
			defer mu.Unlock()
			if provider {
				providerActive--
			} else {
				serviceActive--
			}
		}
	}
	familyClient := func(ip string) *http.Client {
		return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			provider := r.URL.Host == "api.ipapi.is" || r.URL.Host == "api.ipquery.io" ||
				r.URL.Host == "proxycheck.io" || r.URL.Host == "api.ip2location.io" || r.URL.Host == "ipwho.is"
			leave := enter(provider)
			defer leave()
			if provider {
				select {
				case <-time.After(20 * time.Millisecond):
				case <-r.Context().Done():
					return nil, r.Context().Err()
				}
				return dualFixtureResponse(r, ip, "US", "AS64500")
			}
			serviceStarted <- struct{}{}
			select {
			case <-releaseServices:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
			return nil, errors.New("controlled service transport failure")
		})}
	}
	generic := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Errorf("family collection used generic transport for %s", r.URL)
		return nil, errors.New("generic transport must not be used")
	})}
	done := make(chan []agentapi.IPQualityReportPayload, 1)
	go func() {
		done <- agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{Client: generic, Client4: familyClient("198.51.100.10"), Client6: familyClient("2001:db8::10"), LocalAddresses: dualLocalAddresses}).Collect(context.Background(), &agentapi.IPQualityPlan{Enabled: true, TimeoutSeconds: 5, Services: []string{"netflix", "chatgpt", "youtube-premium", "amazon-prime-video", "disney-plus", "tiktok", "reddit"}}, time.Now())
	}()
	// Only retained Go services use these transports. The four actual Bash
	// modules fail at the explicitly rejected fetch helper instead of using WAN.
	// The seven-slot invariant is exercised with eight real HTTP jobs by
	// TestServiceSemaphoreSharesSevenSlotsAcrossFamilies.
	for range len(retainedServices) * 2 {
		select {
		case <-serviceStarted:
		case <-time.After(time.Second):
			close(releaseServices)
			<-done
			t.Fatal("retained service transports did not all start")
		}
	}
	// Every retained service has a blocked first endpoint in both families.
	select {
	case <-serviceStarted:
		close(releaseServices)
		<-done
		t.Fatal("a blocked retained service issued an unexpected extra endpoint")
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseServices)
	select {
	case reports := <-done:
		if len(reports) != 2 || reports[0].Status != "partial" || reports[1].Status != "partial" {
			t.Fatalf("reports=%#v", reports)
		}
		for _, report := range reports {
			assertRejectedDetectorGeoAttempts(t, report)
			rows := serviceUnlocksByService(report.ServiceUnlocks)
			for _, service := range []string{"netflix", "chatgpt", "youtube-premium", "amazon-prime-video"} {
				if rows[service].Status != "unknown" || rows[service].ProbeStatus != "failure" || rows[service].ErrorCode != "detector_fetch_failed" {
					t.Fatalf("scripted service must retain the rejected helper failure: %#v", rows[service])
				}
			}
		}
	case <-time.After(time.Second):
		t.Fatal("workers were not joined")
	}
	mu.Lock()
	defer mu.Unlock()
	if providerMax > 4 || serviceMax > 7 || providerActive != 0 || serviceActive != 0 {
		t.Fatalf("provider active/max=%d/%d service active/max=%d/%d", providerActive, providerMax, serviceActive, serviceMax)
	}
}

func TestHTTPCollectorJSONBoundaryClassifiesBeforeHTTPStatus(t *testing.T) {
	for _, scenario := range []struct {
		name, body, code string
		status           int
	}{
		{"HTML503", "<html>private upstream body</html>", "non_json_response", 503},
		{"malformed503", "{bad", "non_json_response", 503},
		{"array429", "[]", "non_json_response", 429},
		{"JSON429", `{"error":"limited"}`, "http_status", 429},
		{"JSON503", `{"error":"unavailable"}`, "http_status", 503},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host == "api.ipapi.is" {
					return &http.Response{StatusCode: scenario.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(scenario.body)), Request: r}, nil
				}
				return dualFixtureResponse(r, "198.51.100.10", "US", "AS64500")
			})}
			reports := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{Client: client, LocalAddresses: ipv4LocalAddresses}).Collect(context.Background(), &agentapi.IPQualityPlan{Enabled: true, TimeoutSeconds: 1}, time.Now())
			report := reports[0]
			row := providerResultsByName(report.ProviderResults)["ipapi.is"]
			if report.Status != "partial" || row.Status != "failure" || row.ErrorCode != scenario.code {
				t.Fatalf("JSON/status classification=%#v report=%#v", row, report)
			}
			if strings.Contains(row.ErrorSummary, scenario.body) || strings.Contains(string(report.RawJSON), "<html>") || (scenario.code == "non_json_response" && strings.Contains(string(report.RawJSON), scenario.body)) {
				t.Fatal("unsafe or malformed response leaked into diagnostics")
			}
		})
	}
}

func TestHTTPCollectorForcedDiscoveryResolvesUnknownAvailability(t *testing.T) {
	familyClient := func(ip string) *http.Client {
		return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { return dualFixtureResponse(r, ip, "US", "AS64500") })}
	}
	four, six := familyClient("198.51.100.10"), familyClient("2001:db8::10")
	dependent := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.String(), "2001:db8") || strings.Contains(r.URL.Query().Get("ip"), "2001:db8") {
			return six.Transport.RoundTrip(r)
		}
		return four.Transport.RoundTrip(r)
	})}
	reports := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{Client: dependent, Client4: four, Client6: six, LocalAddresses: func() ([]netip.Addr, error) { return nil, errors.New("local addresses unavailable") }}).Collect(context.Background(), &agentapi.IPQualityPlan{Enabled: true, TimeoutSeconds: 1}, time.Now())
	for _, report := range reports {
		if report.Status != "partial" || agentapi.ParseCollectionMetadata(report.DiagnosticsJSON).Availability != "available" {
			t.Fatalf("successful forced discovery did not establish family availability: %#v", report)
		}
		assertRejectedDetectorGeoAttempts(t, report)
	}
}

func TestHTTPCollectorIgnoresNonUsableLocalAddresses(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Errorf("non-usable local address caused external request %s", r.URL)
		return nil, errors.New("unexpected request")
	})}
	reports := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{Client: client, LocalAddresses: func() ([]netip.Addr, error) {
		return []netip.Addr{
			netip.MustParseAddr("0.0.0.0"), netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("169.254.0.1"), netip.MustParseAddr("224.0.0.1"),
			netip.MustParseAddr("::"), netip.MustParseAddr("::1"), netip.MustParseAddr("fe80::1"), netip.MustParseAddr("ff02::1"),
		}, nil
	}}).Collect(context.Background(), &agentapi.IPQualityPlan{Enabled: true, TimeoutSeconds: 1, Services: []string{"reddit"}}, time.Now())
	for _, report := range reports {
		if report.Status != "failure" || report.ErrorCode != "address_family_unavailable" || agentapi.ParseCollectionMetadata(report.DiagnosticsJSON).Availability != "unavailable" {
			t.Fatalf("non-usable address incorrectly admitted: %#v", report)
		}
	}
}

func TestHTTPCollectorSelectiveScopeNeverDiscoversUnselectedFamily(t *testing.T) {
	for _, selected := range []int{4, 6} {
		t.Run(fmt.Sprint(selected), func(t *testing.T) {
			var mu sync.Mutex
			calls := [2]int{}
			client := func(family int) *http.Client {
				return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					mu.Lock()
					calls[(family-4)/2]++
					mu.Unlock()
					return nil, errors.New("controlled discovery failure")
				})}
			}
			collector := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{
				Client4: client(4), Client6: client(6), LocalAddresses: dualLocalAddresses,
			})
			reports := collector.Collect(context.Background(), &agentapi.IPQualityPlan{
				Enabled: true, TimeoutSeconds: 1, CollectionRevision: 5,
				CollectRequestID: "selected-family", CollectIPVersions: []int{selected},
			}, time.Now())
			if len(reports) != 1 || reports[0].IPVersion != selected || reports[0].ErrorCode != "lookup_failed" {
				t.Fatalf("scope produced unselected or misleading terminal: %#v", reports)
			}
			meta := agentapi.ParseCollectionMetadata(reports[0].DiagnosticsJSON)
			if !meta.Valid || meta.Revision != 5 || len(meta.ExpectedIPVersions) != 1 || meta.ExpectedIPVersions[0] != selected {
				t.Fatalf("scope metadata=%#v", meta)
			}
			if calls[(selected-4)/2] != 2 || calls[(6-selected)/2] != 0 {
				t.Fatalf("selected IPv%d discovery calls=%v", selected, calls)
			}
		})
	}
}

func TestHTTPCollectorSelectiveAbsentFamilyDoesNotTouchAvailableFamily(t *testing.T) {
	never := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Errorf("unselected or absent family made request to %s", r.URL.Host)
		return nil, errors.New("unexpected external request")
	})}
	collector := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{
		Client: never, Client4: never, Client6: never, LocalAddresses: ipv4LocalAddresses,
	})
	reports := collector.Collect(context.Background(), &agentapi.IPQualityPlan{
		Enabled: true, TimeoutSeconds: 1, CollectionRevision: 5,
		CollectRequestID: "absent-six", CollectIPVersions: []int{6}, Services: []string{"reddit"},
	}, time.Now())
	if len(reports) != 1 || reports[0].IPVersion != 6 || reports[0].ErrorCode != "address_family_unavailable" ||
		agentapi.ParseCollectionMetadata(reports[0].DiagnosticsJSON).Availability != "unavailable" {
		t.Fatalf("absent-only result=%#v", reports)
	}
}

func TestHTTPCollectorSelectedFamilyOwnsDependentQueriesAndCanonicalRequery(t *testing.T) {
	for _, selected := range []int{4, 6} {
		for _, wrongResponse := range []string{"", "api.ipquery.io", "proxycheck.io", "api.ip2location.io", "ipwho.is"} {
			t.Run(fmt.Sprintf("IPv%d/wrong=%s", selected, wrongResponse), func(t *testing.T) {
				target, alternate, opposite := "198.51.100.10", "198.51.100.11", "2001:db8::10"
				if selected == 6 {
					target, alternate, opposite = "2001:db8::10", "2001:db8::11", "198.51.100.10"
				}
				var mu sync.Mutex
				calls := map[string]int{}
				selectedClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					mu.Lock()
					calls[r.URL.Host]++
					mu.Unlock()
					ip := target
					if r.URL.Host == "api.ipquery.io" && r.URL.Path == "/" {
						// Disagree within the selected family to require a
						// canonical-target requery, not a cross-family retry.
						ip = alternate
					} else if r.URL.Host == wrongResponse {
						ip = opposite
					}
					if r.URL.Host == "proxycheck.io" {
						if r.URL.Path != "/v3/"+target {
							return nil, errors.New("dependent request lost canonical target")
						}
						return jsonResponse(r, fmt.Sprintf(`{"status":"ok","version":"24-June-2026",%q:{"network":{},"location":{},"detections":{"proxy":false}}}`, ip))
					}
					if r.URL.Host == "api.ip2location.io" && r.URL.Query().Get("ip") != target {
						return nil, errors.New("dependent request lost canonical target")
					}
					if r.URL.Host == "ipwho.is" && r.URL.Path != "/"+target {
						return nil, errors.New("dependent request lost canonical target")
					}
					if r.URL.Host == "api.ipquery.io" && r.URL.Path != "/" && r.URL.Path != "/"+target {
						return nil, errors.New("canonical requery lost target")
					}
					if r.URL.Host == "ipwho.is" {
						return jsonResponse(r, fmt.Sprintf(`{"success":true,"ip":%q,"country_code":"US"}`, ip))
					}
					return jsonResponse(r, fmt.Sprintf(`{"ip":%q,"country_code":"US"}`, ip))
				})}
				never := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					t.Errorf("selected IPv%d used generic/opposite transport for %s", selected, r.URL)
					return nil, errors.New("unselected transport must not be used")
				})}
				four, six := selectedClient, never
				if selected == 6 {
					four, six = never, selectedClient
				}
				collector := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{
					Client: never, Client4: four, Client6: six, LocalAddresses: dualLocalAddresses,
				})
				reports := collector.Collect(context.Background(), &agentapi.IPQualityPlan{
					Enabled: true, TimeoutSeconds: 5, CollectionRevision: 5,
					CollectRequestID: "selected-requery", CollectIPVersions: []int{selected},
				}, time.Now())
				if len(reports) != 1 || reports[0].IPAddress != target || reports[0].IPVersion != selected {
					t.Fatalf("canonical family identity changed: %#v", reports)
				}
				for _, row := range reports[0].ProviderResults {
					wantFailure := (wrongResponse == "api.ipquery.io" && row.Provider == "ipquery.io") ||
						(wrongResponse == "proxycheck.io" && row.Provider == wrongResponse) ||
						(wrongResponse == "api.ip2location.io" && row.Provider == "ip2location.io") ||
						(wrongResponse == "ipwho.is" && row.Provider == wrongResponse)
					if wantFailure {
						if row.Status != "failure" || row.Details != nil || row.IsProxy != nil {
							t.Fatalf("opposite-family facts survived: %#v", row)
						}
					} else if row.Status != "success" {
						t.Fatalf("selected-family provider failed: %#v", row)
					}
				}
				if len(reports[0].ProviderResults) != 5 || len(calls) != 5 || calls["api.ipquery.io"] != 2 ||
					calls["api.ipapi.is"] != 1 || calls["proxycheck.io"] != 1 || calls["api.ip2location.io"] != 1 || calls["ipwho.is"] != 1 {
					t.Fatalf("dependent/requery coverage changed: calls=%v report=%#v", calls, reports[0])
				}
			})
		}
	}
}
