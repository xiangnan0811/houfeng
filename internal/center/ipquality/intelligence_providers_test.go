package ipquality

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"houfeng/internal/center/settings"
	"houfeng/internal/contracts/agentapi"
)

func TestIntelligenceRISMOASAndPreciseRoutingFields(t *testing.T) {
	data := []byte(`{"data":{"prefix":"8.8.8.0/24","asns":[15169,64500]}}`)
	ambiguous, err := parseRISNetwork(data, "8.8.8.8", "")
	if err != nil || ambiguous.Number != "" || len(ambiguous.Origins) != 2 {
		t.Fatalf("MOAS selected arbitrary origin: %+v %v", ambiguous, err)
	}
	matched, err := parseRISNetwork(data, "8.8.8.8", "AS15169")
	if err != nil || matched.Number != "AS15169" {
		t.Fatalf("Agent/RIS origin not selected: %+v %v", matched, err)
	}
	err = parseRISRouting([]byte(`{"data":{"announced_space":{"v4":{"prefixes":120,"ips":9007199254740993},"v6":{"prefixes":4,"48s":18446744073709551616}},"query_time":"2026-10-14T10:20:30"}}`), &matched)
	if err != nil || matched.IPv4Addresses == nil || *matched.IPv4Addresses != "9007199254740993" || matched.IPv6_48s == nil || *matched.IPv6_48s != "18446744073709551616" || matched.ObservedAt == nil {
		t.Fatalf("precise announced space lost: %+v %v", matched, err)
	}
	err = parseRISRouting([]byte(`{"data":{"announced_space":{"v4":{"prefixes":"120","ips":5.5},"v6":{"prefixes":0,"48s":0}}}}`), &matched)
	if err != nil || matched.IPv4Prefixes != nil || matched.IPv4Addresses != nil || matched.IPv6_48s == nil || *matched.IPv6_48s != "0" {
		t.Fatalf("invalid types guessed or zero dropped: %+v %v", matched, err)
	}
	if _, err := parseRISNetwork(data, "1.1.1.1", ""); err == nil {
		t.Fatal("mismatched target prefix accepted")
	}
}

func TestIntelligenceBootstrapLongestPrefixAndRIRAllowlist(t *testing.T) {
	data := []byte(`{"services":[[["8.0.0.0/8"],["https://rdap.arin.net/registry/"]],[["8.8.8.0/24"],["http://127.0.0.1/","https://rdap.db.ripe.net/"]],[["1.0.0.0/8"],["https://evil.example/"]]]}`)
	bootstrap, err := parseBootstrap(data, "ipv4")
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := bootstrap.endpoint("8.8.8.8", false)
	if err != nil || endpoint != "https://rdap.db.ripe.net/ip/8.8.8.8" {
		t.Fatalf("longest prefix selection: %q %v", endpoint, err)
	}
	if _, err := bootstrap.endpoint("1.1.1.1", false); err == nil {
		t.Fatal("untrusted service accepted")
	}
	asn, err := parseBootstrap([]byte(`{"services":[[["1-65535"],["https://rdap.arin.net/registry/"]]]}`), "asn")
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err = asn.endpoint("AS15169", true)
	if err != nil || endpoint != "https://rdap.arin.net/registry/autnum/15169" {
		t.Fatalf("ASN bootstrap: %q %v", endpoint, err)
	}
}

func TestIntelligenceRDAPCountryProvenance(t *testing.T) {
	data := []byte(`{"objectClassName":"ip network","handle":"NET-8","name":"PUBLIC","startAddress":"8.8.8.0","endAddress":"8.8.8.255","entities":[{"roles":["administrative"],"vcardArray":["vcard",[["adr",{},"text",["","","Main","City","State","000","US"]]]]},{"roles":["registrant"],"vcardArray":["vcard",[["fn",{},"text","Network Org"],["adr",{},"text",["","","1 Somewhere US","City","State","000","JP"]]]]}],"events":[{"eventAction":"registration","eventDate":"2000-01-01T00:00:00Z"}]}`)
	registration, err := parseRDAPNetwork(data, "8.8.8.8", "https://rdap.arin.net/registry/ip/8.8.8.8")
	if err != nil || registration.NetworkCountryCode != "" || registration.RegistrantCountryCode != "JP" || registration.RegistrantName != "Network Org" || registration.RegisteredAt == nil {
		t.Fatalf("country provenance lost: %+v %v", registration, err)
	}
	data = []byte(strings.Replace(string(data), `"handle":"NET-8"`, `"handle":"NET-8","country":"us"`, 1))
	registration, err = parseRDAPNetwork(data, "8.8.8.8", "https://rdap.arin.net/registry/ip/8.8.8.8")
	if err != nil || registration.NetworkCountryCode != "US" {
		t.Fatalf("explicit network country not used: %+v %v", registration, err)
	}
	if _, err := parseRDAPNetwork(data, "1.1.1.1", "https://rdap.arin.net/registry/ip/1.1.1.1"); err == nil {
		t.Fatal("unrelated network accepted")
	}
	asn, err := parseRDAPASN([]byte(`{"objectClassName":"autnum","startAutnum":15169,"endAutnum":15169,"name":"GOOGLE","country":"US"}`), "AS15169", "https://rdap.arin.net/registry/autnum/15169")
	if err != nil || asn.CountryCode != "US" {
		t.Fatalf("ASN country: %+v %v", asn, err)
	}
	// The ASN result is distinct from IP registration; no fill from ASN country.
	if registration.RegistrantCountryCode == asn.CountryCode {
		t.Fatal("registration country overwritten")
	}
}

func TestIntelligenceKeyedASNWhitelistAndIdentity(t *testing.T) {
	data := []byte(`{"asn":15169,"org":"GOOGLE","domain":"google.com","type":"hosting","rir":"ARIN","created":"2000-01-01","updated":"2026-01-01","key":"DO-NOT-PERSIST","echo":{"key":"DO-NOT-PERSIST"}}`)
	result, err := parseKeyedASN(data, "AS15169")
	if err != nil || result.Domain != "google.com" || result.Type != "hosting" || result.RegisteredAt == nil || result.UpdatedAt == nil {
		t.Fatalf("keyed ASN metadata missing: %+v %v", result, err)
	}
	serialized, _ := json.Marshal(result)
	if strings.Contains(string(serialized), "DO-NOT-PERSIST") {
		t.Fatal("response echo persisted")
	}
	if _, err := parseKeyedASN(data, "AS64500"); err == nil {
		t.Fatal("unrelated ASN accepted")
	}
}

func TestIntelligenceSnapshotValidationSeparateFromAgent(t *testing.T) {
	details := &agentapi.IPQualityProviderDetails{QueriedIP: "8.8.8.8", ObservedAt: testIntelligenceTime(), Method: "center_api", APITier: "authenticated", CountryCode: "US", Scores: []agentapi.IPQualityScore{}, FieldStates: map[string]string{}}
	snapshot := IntelligenceSnapshot{Providers: []ProviderResultRead{{Provider: "ipapi.is", Status: "success", Details: details}}}
	if err := ValidateIntelligenceSnapshot(snapshot, "8.8.8.8"); err != nil {
		t.Fatal(err)
	}
	if err := agentapi.ValidateIPQualityProviderDetails("ipapi.is", details, 1, "8.8.8.8"); err == nil {
		t.Fatal("Agent accepts authenticated Center facts")
	}
	details.QueriedIP = "1.1.1.1"
	if err := ValidateIntelligenceSnapshot(snapshot, "8.8.8.8"); err == nil {
		t.Fatal("Center accepts unrelated IP")
	}
	details.QueriedIP = "8.8.8.8"
	details.Scores = []agentapi.IPQualityScore{{ID: "bad", Scope: "ip", Direction: "higher_risk", RawValue: strings.Repeat("x", 257)}}
	if err := ValidateIntelligenceSnapshot(snapshot, "8.8.8.8"); err == nil {
		t.Fatal("oversized field accepted")
	}
}

func risEvidenceTransport(calls *[]string) http.RoundTripper {
	var mu sync.Mutex
	return intelligenceRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() != "stat.ripe.net" {
			return intelligenceResponse(req, 404, `{}`), nil
		}
		resource := req.URL.Query().Get("resource")
		mu.Lock()
		*calls = append(*calls, req.URL.Path+"?"+resource)
		mu.Unlock()
		var body string
		switch req.URL.Path {
		case "/data/network-info/data.json":
			body = `{"data":{"prefix":"8.8.8.0/24","asns":[15169,64500]}}`
		case "/data/as-overview/data.json":
			holder := map[string]string{"AS15169": "GOOGLE", "AS64500": "PRIVATE"}[resource]
			if holder == "" {
				return intelligenceResponse(req, 404, `{}`), nil
			}
			body = `{"data":{"holder":"` + holder + `","announced":true}}`
		case "/data/routing-status/data.json":
			space := map[string]string{
				"AS15169": `{"v4":{"prefixes":1,"ips":256},"v6":{"prefixes":2,"48s":4}}`,
				"AS64500": `{"v4":{"prefixes":9,"ips":999},"v6":{"prefixes":8,"48s":7}}`,
			}[resource]
			if space == "" {
				return intelligenceResponse(req, 404, `{}`), nil
			}
			body = `{"data":{"announced_space":` + space + `,"query_time":"2026-10-14T12:00:00"}}`
		default:
			return intelligenceResponse(req, 404, `{}`), nil
		}
		return intelligenceResponse(req, 200, body), nil
	})
}

func countRISCalls(calls []string, path, resource string) int {
	n := 0
	want := path + "?" + resource
	for _, call := range calls {
		if call == want {
			n++
		}
	}
	return n
}

func TestIntelligenceCachedRISUsesCurrentAgentASN(t *testing.T) {
	config := settings.IPQualitySettings{Enabled: true}
	claim := func(asn string) IntelligenceClaim {
		return IntelligenceClaim{Report: ReportWrite{IPAddress: "8.8.8.8", IPVersion: 4, ASN: asn}, DeadlineAt: time.Now().Add(time.Minute)}
	}
	newCollector := func(repo *intelligenceTestRepo, calls *[]string) *IntelligenceCollector {
		collector := NewIntelligenceCollector(repo)
		collector.http = &http.Client{Transport: risEvidenceTransport(calls)}
		collector.dns = fixtureIntelligenceDNS("", dnsNXDomainCode(), false)
		return collector
	}
	assertNoStale := func(t *testing.T, asn agentapi.IPQualityASN, number, name, addresses, prefixes48 string) {
		t.Helper()
		if asn.Number != number || asn.Name != name || asn.Route != "8.8.8.0/24" || len(asn.Origins) != 2 || asn.Domain != "" || asn.CountryCode != "" || asn.RIR != "" {
			t.Fatalf("selected ASN identity: %+v", asn)
		}
		if number == "" {
			if asn.IPv4Prefixes != nil || asn.IPv4Addresses != nil || asn.IPv6Prefixes != nil || asn.IPv6_48s != nil || asn.Name != "" {
				t.Fatalf("unselected ASN leaked metadata: %+v", asn)
			}
			return
		}
		if asn.IPv4Addresses == nil || *asn.IPv4Addresses != addresses || asn.IPv6_48s == nil || *asn.IPv6_48s != prefixes48 || asn.Name == "GOOGLE" && name != "GOOGLE" || asn.Name == "STALE-HOLDER" {
			t.Fatalf("stale selected metadata: %+v", asn)
		}
	}
	t.Run("changed A to B", func(t *testing.T) {
		repo := newIntelligenceTestRepo()
		var calls []string
		collector := newCollector(repo, &calls)
		first := collector.Collect(context.Background(), claim("AS15169"), config)
		assertNoStale(t, first.ASN, "AS15169", "GOOGLE", "256", "4")
		if countRISCalls(calls, "/data/network-info/data.json", "8.8.8.8") != 1 || countRISCalls(calls, "/data/as-overview/data.json", "AS15169") != 1 {
			t.Fatalf("first selection calls: %v", calls)
		}
		repo.save(first.CacheWrites)
		calls = nil
		second := collector.Collect(context.Background(), claim("AS64500"), config)
		assertNoStale(t, second.ASN, "AS64500", "PRIVATE", "999", "7")
		if countRISCalls(calls, "/data/network-info/data.json", "8.8.8.8") != 0 || countRISCalls(calls, "/data/as-overview/data.json", "AS64500") != 1 || countRISCalls(calls, "/data/routing-status/data.json", "AS64500") != 1 || countRISCalls(calls, "/data/as-overview/data.json", "AS15169") != 0 {
			t.Fatalf("changed primary reused stale cache or missed fetch: %v", calls)
		}
	})
	t.Run("unavailable to matched", func(t *testing.T) {
		repo := newIntelligenceTestRepo()
		staleAddresses, stalePrefixes, stale48s := "1", "1", "1"
		observed := time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)
		payload, err := json.Marshal(agentapi.IPQualityASN{
			Status: "success", Name: "STALE-HOLDER", CountryCode: "US", RIR: "ARIN", Route: "8.8.8.0/24",
			SourceURL: "https://stat.ripe.net/data/network-info/data.json?resource=8.8.8.8", ObservedAt: &observed,
			IPv4Prefixes: &stalePrefixes, IPv4Addresses: &staleAddresses, IPv6Prefixes: &stalePrefixes, IPv6_48s: &stale48s,
			Origins: []string{"AS15169", "AS64500"},
		})
		if err != nil {
			t.Fatal(err)
		}
		repo.save([]SourceCacheEntry{{
			SourceID: "ripe-ris", Resource: "8.8.8.8", FetchedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour), PayloadJSON: payload,
		}})
		var calls []string
		result := newCollector(repo, &calls).Collect(context.Background(), claim("AS64500"), config)
		assertNoStale(t, result.ASN, "AS64500", "PRIVATE", "999", "7")
		if result.ASN.ObservedAt == nil || !result.ASN.ObservedAt.Equal(time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)) {
			t.Fatalf("routing observation not applied: %+v", result.ASN.ObservedAt)
		}
		if countRISCalls(calls, "/data/network-info/data.json", "8.8.8.8") != 0 || countRISCalls(calls, "/data/as-overview/data.json", "AS64500") != 1 || countRISCalls(calls, "/data/routing-status/data.json", "AS64500") != 1 {
			t.Fatalf("unavailable primary did not fetch the matched ASN: %v", calls)
		}
	})
	t.Run("near expiry reselection does not renew network cache", func(t *testing.T) {
		repo := newIntelligenceTestRepo()
		var calls []string
		collector := newCollector(repo, &calls)
		first := collector.Collect(context.Background(), claim("AS15169"), config)
		assertNoStale(t, first.ASN, "AS15169", "GOOGLE", "256", "4")
		repo.save(first.CacheWrites)
		entry, err := repo.LoadIPQualitySourceCache(context.Background(), "ripe-ris", "8.8.8.8", "")
		if err != nil || entry == nil {
			t.Fatalf("missing network cache: entry=%+v err=%v", entry, err)
		}
		original := *entry
		original.ExpiresAt = time.Now().UTC().Add(30 * time.Second)
		original.FetchedAt = original.ExpiresAt.Add(-8 * time.Hour)
		repo.save([]SourceCacheEntry{original})
		calls = nil
		result := collector.Collect(context.Background(), claim("AS64500"), config)
		assertNoStale(t, result.ASN, "AS64500", "PRIVATE", "999", "7")
		if countRISCalls(calls, "/data/network-info/data.json", "8.8.8.8") != 0 || countRISCalls(calls, "/data/as-overview/data.json", "AS64500") != 1 || countRISCalls(calls, "/data/routing-status/data.json", "AS64500") != 1 {
			t.Fatalf("near-expiry hit did not reselect from retained origins: %v", calls)
		}
		if len(result.CacheWrites) != 0 {
			t.Fatalf("cached network evidence queued a lifetime extension: %+v", result.CacheWrites)
		}
		repo.save(result.CacheWrites)
		after, err := repo.LoadIPQualitySourceCache(context.Background(), "ripe-ris", "8.8.8.8", "")
		if err != nil || after == nil {
			t.Fatalf("network cache lost: entry=%+v err=%v", after, err)
		}
		if !after.FetchedAt.Equal(original.FetchedAt) || !after.ExpiresAt.Equal(original.ExpiresAt) || string(after.PayloadJSON) != string(original.PayloadJSON) {
			t.Fatalf("reselection renewed retained network evidence: before=%+v after=%+v", original, after)
		}
	})
	t.Run("no unambiguous primary", func(t *testing.T) {
		repo := newIntelligenceTestRepo()
		var calls []string
		collector := newCollector(repo, &calls)
		first := collector.Collect(context.Background(), claim("AS15169"), config)
		repo.save(first.CacheWrites)
		calls = nil
		result := collector.Collect(context.Background(), claim("AS999"), config)
		assertNoStale(t, result.ASN, "", "", "", "")
		if countRISCalls(calls, "/data/network-info/data.json", "8.8.8.8") != 0 || countRISCalls(calls, "/data/as-overview/data.json", "AS15169") != 0 || countRISCalls(calls, "/data/routing-status/data.json", "AS15169") != 0 {
			t.Fatalf("ambiguous primary kept or refetched stale metadata: %v asn=%+v", calls, result.ASN)
		}
	})
}
