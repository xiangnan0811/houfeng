package ipquality

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"houfeng/internal/center/settings"
	"houfeng/internal/contracts/agentapi"
)

type intelligenceRoundTripper func(*http.Request) (*http.Response, error)

func (fn intelligenceRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
func intelligenceResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}
}

type intelligenceTestRepo struct {
	enabled            atomic.Bool
	eligibilityFailure atomic.Bool
	mu                 sync.Mutex
	cache              map[string]SourceCacheEntry
	eligibilityFn      func(context.Context, IntelligenceClaim) (bool, error)
}

func newIntelligenceTestRepo() *intelligenceTestRepo {
	r := &intelligenceTestRepo{cache: map[string]SourceCacheEntry{}}
	r.enabled.Store(true)
	return r
}
func (*intelligenceTestRepo) ClaimIPQualityIntelligence(context.Context, string) (*IntelligenceClaim, error) {
	return nil, errors.New("unused")
}
func (*intelligenceTestRepo) CompleteIPQualityIntelligence(context.Context, IntelligenceClaim, IntelligenceSnapshot) error {
	return errors.New("unused")
}
func (r *intelligenceTestRepo) IPQualityIntelligenceEligible(ctx context.Context, claim IntelligenceClaim) (bool, error) {
	if r.eligibilityFn != nil {
		return r.eligibilityFn(ctx, claim)
	}
	if r.eligibilityFailure.Load() {
		return false, errors.New("eligibility unavailable")
	}
	return r.enabled.Load(), nil
}
func cacheTestKey(source, resource, fp string) string { return source + "|" + resource + "|" + fp }
func (r *intelligenceTestRepo) LoadIPQualitySourceCache(ctx context.Context, source, resource, fp string) (*SourceCacheEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.cache[cacheTestKey(source, resource, fp)]
	if !ok {
		return nil, nil
	}
	return &entry, nil
}
func (r *intelligenceTestRepo) save(entries []SourceCacheEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, entry := range entries {
		r.cache[cacheTestKey(entry.SourceID, entry.Resource, entry.CredentialFingerprint)] = entry
	}
}

func TestIntelligencePinnedDialRejectsPrivateAndRebinding(t *testing.T) {
	for _, addresses := range [][]string{{"127.0.0.1"}, {"10.0.0.1"}, {"169.254.169.254"}, {"::1"}, {"fd00::1"}, {"::ffff:127.0.0.1"}, {"100.64.0.1"}, {"64:ff9b::a00:1"}, {"8.8.8.8", "127.0.0.1"}} {
		resolve := func(context.Context, string, string) ([]netip.Addr, error) {
			result := make([]netip.Addr, 0, len(addresses))
			for _, s := range addresses {
				result = append(result, netip.MustParseAddr(s))
			}
			return result, nil
		}
		dial := func(context.Context, string, string) (net.Conn, error) {
			t.Error("dialed nonpublic/mixed DNS answer")
			return nil, errors.New("unexpected")
		}
		if _, err := pinnedIntelligenceDial(resolve, dial)(context.Background(), "tcp", "stat.ripe.net:443"); err == nil {
			t.Fatalf("accepted %v", addresses)
		}
	}
	lookups := 0
	dialed := ""
	resolve := func(context.Context, string, string) ([]netip.Addr, error) {
		lookups++
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	}
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		dialed = address
		left, right := net.Pipe()
		right.Close()
		return left, nil
	}
	conn, err := pinnedIntelligenceDial(resolve, dial)(context.Background(), "tcp", "stat.ripe.net:443")
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if lookups != 1 || dialed != "8.8.8.8:443" {
		t.Fatalf("not pinned: lookups=%d dial=%q", lookups, dialed)
	}
	if _, err := pinnedIntelligenceDial(resolve, dial)(context.Background(), "tcp", "evil.example:443"); err == nil {
		t.Fatal("unknown host accepted")
	}
	transport := newIntelligenceHTTPClient().Transport.(*http.Transport)
	if transport.Proxy != nil {
		t.Fatal("environment proxy enabled")
	}
}

func TestIntelligenceRedirectBoundaryAndAuthorization(t *testing.T) {
	first, _ := http.NewRequest(http.MethodGet, "https://api.abuseipdb.com/api/v2/check", nil)
	for _, endpoint := range []string{"http://api.abuseipdb.com/", "https://api.ipapi.is/", "https://evil.example/", "https://user:pass@api.abuseipdb.com/", "https://api.abuseipdb.com:8443/"} {
		req, _ := http.NewRequest(http.MethodGet, endpoint, nil)
		req.Header.Set("Key", "private-key")
		if err := intelligenceRedirect(req, []*http.Request{first}); err == nil {
			t.Fatalf("unsafe redirect accepted: %s", endpoint)
		}
	}
	rdap, _ := http.NewRequest(http.MethodGet, "https://rdap.arin.net/registry/ip/8.8.8.8", nil)
	req, _ := http.NewRequest(http.MethodGet, "https://rdap.apnic.net/ip/8.8.8.8", nil)
	req.Header.Set("Authorization", "private-key")
	req.Header.Set("Key", "private-key")
	if err := intelligenceRedirect(req, []*http.Request{rdap}); err != nil || req.Header.Get("Key") != "" || req.Header.Get("Authorization") != "" {
		t.Fatalf("RIR redirect auth boundary: %v", err)
	}
	if err := intelligenceRedirect(req, []*http.Request{rdap, rdap, rdap, rdap}); err == nil {
		t.Fatal("unbounded redirects")
	}
}

func TestIntelligenceRequestQuotaIsDeferredAndStopsCurrentRound(t *testing.T) {
	repo := newIntelligenceTestRepo()
	collector := NewIntelligenceCollector(repo)
	calls := 0
	collector.http = &http.Client{Transport: intelligenceRoundTripper(func(req *http.Request) (*http.Response, error) {
		calls++
		resp := intelligenceResponse(req, 429, `{"echo":"private-key"}`)
		resp.Header.Set("Retry-After", "99999999")
		return resp, nil
	})}
	s := &intelligenceSession{collector: collector, repo: repo, ctx: context.Background(), slots: make(chan struct{}, 4), quotas: map[string]time.Time{}}
	fp := credentialFingerprint("private-key")
	for range 2 {
		if _, err := s.request("abuseipdb", "8.8.8.8", fp, http.MethodGet, "https://api.abuseipdb.com/api/v2/check", nil, http.Header{"Key": []string{"private-key"}}, 1024); err == nil {
			t.Fatal("429 accepted")
		}
	}
	if calls != 1 || len(s.snapshot.CacheWrites) != 1 {
		t.Fatalf("quota did not stop round: calls=%d writes=%+v", calls, s.snapshot.CacheWrites)
	}
	entry := s.snapshot.CacheWrites[0]
	if entry.Resource != "@quota" || entry.BlockedUntil == nil || entry.BlockedUntil.Sub(entry.FetchedAt) != 24*time.Hour || string(entry.PayloadJSON) != "{}" {
		t.Fatalf("quota metadata: %+v", entry)
	}
	if len(repo.cache) != 0 {
		t.Fatal("cache written before finalize")
	}
	for _, test := range []struct {
		raw      string
		duration time.Duration
	}{{"0", time.Minute}, {"", time.Hour}, {"garbage", time.Hour}, {"86401", 24 * time.Hour}} {
		if got := quotaUntil(test.raw, testIntelligenceTime()).Sub(testIntelligenceTime()); got != test.duration {
			t.Fatalf("Retry-After %q=%v", test.raw, got)
		}
	}
}

func TestIntelligenceRequestSharedFourSlotsAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	arrivals := make(chan struct{}, 12)
	var active, maximum atomic.Int32
	collector := NewIntelligenceCollector(nil)
	collector.http = &http.Client{Transport: intelligenceRoundTripper(func(req *http.Request) (*http.Response, error) {
		count := active.Add(1)
		defer active.Add(-1)
		for {
			old := maximum.Load()
			if count <= old || maximum.CompareAndSwap(old, count) {
				break
			}
		}
		arrivals <- struct{}{}
		<-req.Context().Done()
		return nil, req.Context().Err()
	})}
	s := &intelligenceSession{collector: collector, ctx: ctx, slots: make(chan struct{}, 4), quotas: map[string]time.Time{}}
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = s.request("ripe-ris", "8.8.8.8", "", http.MethodGet, "https://stat.ripe.net/data/network-info/data.json", nil, nil, 1024)
		}()
	}
	for range 4 {
		select {
		case <-arrivals:
		case <-time.After(time.Second):
			t.Fatal("four requests not started")
		}
	}
	select {
	case <-arrivals:
		t.Fatal("fifth request exceeded shared concurrency")
	default:
	}
	cancel()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancellation leaked requests")
	}
	if maximum.Load() != 4 || active.Load() != 0 || len(s.slots) != 0 {
		t.Fatalf("slots/socket lifecycle: maximum=%d active=%d slots=%d", maximum.Load(), active.Load(), len(s.slots))
	}
}

func fixtureIntelligenceHTTP(req *http.Request) (*http.Response, error) {
	var body string
	switch req.URL.Hostname() + req.URL.Path {
	case "stat.ripe.net/data/network-info/data.json":
		body = `{"data":{"prefix":"8.8.8.0/24","asns":[15169]}}`
	case "stat.ripe.net/data/as-overview/data.json":
		body = `{"data":{"holder":"GOOGLE","announced":true}}`
	case "stat.ripe.net/data/routing-status/data.json":
		body = `{"data":{"announced_space":{"v4":{"prefixes":1,"ips":256},"v6":{"prefixes":2,"48s":65536}},"query_time":"2026-10-14T12:00:00"}}`
	case "data.iana.org/rdap/ipv4.json":
		body = `{"services":[[["8.0.0.0/8"],["https://rdap.arin.net/registry/"]]]}`
	case "data.iana.org/rdap/asn.json":
		body = `{"services":[[["1-65535"],["https://rdap.arin.net/registry/"]]]}`
	case "rdap.arin.net/registry/ip/8.8.8.8":
		body = `{"objectClassName":"ip network","handle":"NET-8","startAddress":"8.8.8.0","endAddress":"8.8.8.255","country":"US","name":"PUBLIC"}`
	case "rdap.arin.net/registry/autnum/15169":
		body = `{"objectClassName":"autnum","startAutnum":15169,"endAutnum":15169,"country":"US","name":"GOOGLE","events":[{"eventAction":"registration","eventDate":"2000-01-01T00:00:00Z"}]}`
	case "www.spamhaus.org/drop/drop_v4.json":
		body = `{"cidr":"1.0.0.0/8","sblid":"SBL1","rir":"apnic"}` + "\n" + `{"type":"metadata","timestamp":1791979200,"records":1,"copyright":"Spamhaus","terms":"Terms"}`
	case "api.ipapi.is/":
		var query map[string]string
		if err := json.NewDecoder(req.Body).Decode(&query); err != nil {
			return nil, err
		}
		if strings.HasPrefix(query["q"], "AS") {
			body = `{"asn":15169,"domain":"google.com","type":"business","rir":"KEYED-RIR","created":"2010-01-01","org":"Keyed Name"}`
		} else {
			body = `{"ip":"8.8.8.8","asn":{"asn":15169,"org":"GOOGLE","abuser_score":"0.0183 (Elevated)"},"company":{"name":"Google","abuser_score":"0.0183 (Elevated)"},"location":{"country_code":"US","latitude":0,"longitude":0},"key":"DO-NOT-PERSIST"}`
		}
	case "api.abuseipdb.com/api/v2/check":
		body = `{"data":{"ipAddress":"8.8.8.8","abuseConfidenceScore":20,"totalReports":0,"lastReportedAt":null,"isTor":false,"usageType":"Data Center/Web Hosting/Transit","isp":"Google","domain":"google.com"},"key":"DO-NOT-PERSIST"}`
	default:
		return intelligenceResponse(req, 404, `{}`), nil
	}
	return intelligenceResponse(req, 200, body), nil
}

func TestIntelligenceCollectorPublicAndAuthenticatedSnapshotCacheIsolation(t *testing.T) {
	repo := newIntelligenceTestRepo()
	collector := NewIntelligenceCollector(repo)
	var calls atomic.Int32
	collector.http = &http.Client{Transport: intelligenceRoundTripper(func(req *http.Request) (*http.Response, error) { calls.Add(1); return fixtureIntelligenceHTTP(req) })}
	collector.dns = fixtureIntelligenceDNS("", dnsNXDomainCode(), false)
	claim := IntelligenceClaim{Report: ReportWrite{IPAddress: "8.8.8.8", IPVersion: 4, ASN: "AS15169"}, DeadlineAt: time.Now().Add(time.Minute)}
	config := settings.IPQualitySettings{Enabled: true}
	public := collector.Collect(context.Background(), claim, config)
	if public.State != "complete" || len(public.Providers) != 0 || len(public.Blacklists) != 3 || public.Registration.NetworkCountryCode != "US" || public.ASN.Number != "AS15169" {
		t.Fatalf("public snapshot: %+v", public)
	}
	config.Sources.IPAPIISKey = "one-private-key"
	config.Sources.AbuseIPDBKey = "another-private-key"
	auth := collector.Collect(context.Background(), claim, config)
	if auth.State != "complete" || len(auth.Providers) != 2 || len(auth.CredentialFingerprints) != 2 || auth.PublicASN == nil || auth.PublicASN.Name != "GOOGLE" || auth.ASN.Name != "Keyed Name" || auth.ASN.RIR != "KEYED-RIR" {
		t.Fatalf("enhanced snapshot: %+v", auth)
	}
	encoded, _ := json.Marshal(auth)
	for _, secret := range []string{"DO-NOT-PERSIST", "one-private-key", "another-private-key", credentialFingerprint(config.Sources.IPAPIISKey)} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("snapshot serializes private execution data")
		}
	}
	repo.save(auth.CacheWrites)
	previous := calls.Load()
	cached := collector.Collect(context.Background(), claim, config)
	if cached.State != "complete" || calls.Load() != previous {
		t.Fatalf("validated cache not reused: state=%s before=%d after=%d", cached.State, previous, calls.Load())
	}
	config.Sources.IPAPIISKey = "replacement-key"
	changed := collector.Collect(context.Background(), claim, config)
	if changed.State != "complete" || calls.Load() != previous+2 {
		t.Fatalf("changed key hit old namespace: state=%s calls=%d expected=%d", changed.State, calls.Load(), previous+2)
	}
	config.Sources = settings.IPQualitySources{}
	cleared := collector.Collect(context.Background(), claim, config)
	if len(cleared.Providers) != 0 || len(cleared.CredentialFingerprints) != 0 || cleared.ASN.Domain != "" {
		t.Fatalf("cleared key reused authenticated data: %+v", cleared)
	}
}
func dnsNXDomainCode() int { return 3 }

func TestIntelligenceLifecycleCancellationDiscardsDeferredCache(t *testing.T) {
	repo := newIntelligenceTestRepo()
	collector := NewIntelligenceCollector(repo)
	collector.dns = fixtureIntelligenceDNS("", dnsNXDomainCode(), false)
	collector.http = &http.Client{Transport: intelligenceRoundTripper(func(req *http.Request) (*http.Response, error) {
		resp, err := fixtureIntelligenceHTTP(req)
		repo.enabled.Store(false)
		return resp, err
	})}
	result := collector.Collect(context.Background(), IntelligenceClaim{Report: ReportWrite{IPAddress: "8.8.8.8", IPVersion: 4}, DeadlineAt: time.Now().Add(time.Minute)}, settings.IPQualitySettings{Enabled: true})
	if result.State != "skipped" || len(result.CacheWrites) != 0 {
		t.Fatalf("ineligible lifecycle saved data/cache: %+v", result)
	}
}

func hasOutcomeCode(snapshot IntelligenceSnapshot, code string) bool {
	for _, outcome := range snapshot.SourceOutcomes {
		if outcome.ErrorCode == code {
			return true
		}
	}
	return false
}

func TestIntelligenceEligibilityErrorIsNotLifecycleSkip(t *testing.T) {
	claim := IntelligenceClaim{Report: ReportWrite{IPAddress: "8.8.8.8", IPVersion: 4, ASN: "AS15169"}, DeadlineAt: time.Now().Add(time.Minute)}
	config := settings.IPQualitySettings{Enabled: true}
	t.Run("initial repository error", func(t *testing.T) {
		repo := newIntelligenceTestRepo()
		repo.eligibilityFailure.Store(true)
		collector := NewIntelligenceCollector(repo)
		var calls atomic.Int32
		collector.http = &http.Client{Transport: intelligenceRoundTripper(func(req *http.Request) (*http.Response, error) {
			calls.Add(1)
			return fixtureIntelligenceHTTP(req)
		})}
		collector.dns = fixtureIntelligenceDNS("", dnsNXDomainCode(), false)
		result := collector.Collect(context.Background(), claim, config)
		if result.State != "failed" || result.CompletedAt == nil || len(result.CacheWrites) != 0 || calls.Load() != 0 || hasOutcomeCode(result, "lifecycle_inactive") {
			t.Fatalf("repository error looked like revocation: state=%s calls=%d %+v", result.State, calls.Load(), result.SourceOutcomes)
		}
	})
	t.Run("initial definitive false", func(t *testing.T) {
		repo := newIntelligenceTestRepo()
		repo.enabled.Store(false)
		collector := NewIntelligenceCollector(repo)
		var calls atomic.Int32
		collector.http = &http.Client{Transport: intelligenceRoundTripper(func(req *http.Request) (*http.Response, error) {
			calls.Add(1)
			return fixtureIntelligenceHTTP(req)
		})}
		collector.dns = fixtureIntelligenceDNS("", dnsNXDomainCode(), false)
		result := collector.Collect(context.Background(), claim, config)
		if result.State != "skipped" || calls.Load() != 0 || len(result.CacheWrites) != 0 {
			t.Fatalf("definitive false did not skip: state=%s calls=%d", result.State, calls.Load())
		}
	})
	t.Run("disabled settings", func(t *testing.T) {
		result := NewIntelligenceCollector(newIntelligenceTestRepo()).Collect(context.Background(), claim, settings.IPQualitySettings{})
		if result.State != "skipped" || len(result.CacheWrites) != 0 {
			t.Fatalf("disabled settings not skipped: %s", result.State)
		}
	})
}

func TestIntelligenceEligibilityRepositoryErrorPreservesFacts(t *testing.T) {
	repo := newIntelligenceTestRepo()
	collector := NewIntelligenceCollector(repo)
	collector.http = &http.Client{Transport: intelligenceRoundTripper(fixtureIntelligenceHTTP)}
	collector.dns = fixtureIntelligenceDNS("", dnsNXDomainCode(), false)
	claim := IntelligenceClaim{Report: ReportWrite{IPAddress: "8.8.8.8", IPVersion: 4, ASN: "AS15169"}, DeadlineAt: time.Now().Add(time.Minute)}
	config := settings.IPQualitySettings{Enabled: true}
	seeded := collector.Collect(context.Background(), claim, config)
	if seeded.State != "complete" || seeded.ASN.Number != "AS15169" || seeded.ASN.Name != "GOOGLE" || seeded.Registration.NetworkCountryCode != "US" {
		t.Fatalf("seed: state=%s asn=%+v reg=%+v", seeded.State, seeded.ASN, seeded.Registration)
	}
	repo.save(seeded.CacheWrites)
	var checks atomic.Int32
	repo.eligibilityFn = func(context.Context, IntelligenceClaim) (bool, error) {
		if checks.Add(1) == 1 {
			return true, nil
		}
		return false, errors.New("repository unavailable")
	}
	var network atomic.Int32
	collector.http = &http.Client{Transport: intelligenceRoundTripper(func(req *http.Request) (*http.Response, error) {
		network.Add(1)
		return fixtureIntelligenceHTTP(req)
	})}
	result := collector.Collect(context.Background(), claim, config)
	if result.State != "partial" || result.ASN.Number != "AS15169" || result.ASN.Name != "GOOGLE" || result.Registration.NetworkCountryCode != "US" || hasOutcomeCode(result, "lifecycle_inactive") || !hasOutcomeCode(result, "eligibility_failed") || network.Load() != 0 {
		t.Fatalf("repository error discarded facts: state=%s network=%d asn=%+v reg=%+v outcomes=%+v", result.State, network.Load(), result.ASN, result.Registration, result.SourceOutcomes)
	}
}

func TestIntelligenceCancellationAndBudgetAreNotLifecycleSkip(t *testing.T) {
	config := settings.IPQualitySettings{Enabled: true}
	claim := func() IntelligenceClaim {
		return IntelligenceClaim{Report: ReportWrite{IPAddress: "8.8.8.8", IPVersion: 4, ASN: "AS15169"}, DeadlineAt: time.Now().Add(time.Minute)}
	}
	t.Run("already canceled", func(t *testing.T) {
		parent, cancel := context.WithCancel(context.Background())
		cancel()
		collector := NewIntelligenceCollector(newIntelligenceTestRepo())
		var calls atomic.Int32
		collector.http = &http.Client{Transport: intelligenceRoundTripper(func(req *http.Request) (*http.Response, error) {
			calls.Add(1)
			return fixtureIntelligenceHTTP(req)
		})}
		collector.dns = fixtureIntelligenceDNS("", dnsNXDomainCode(), false)
		result := collector.Collect(parent, claim(), config)
		if result.State != "failed" || calls.Load() != 0 || hasOutcomeCode(result, "lifecycle_inactive") {
			t.Fatalf("canceled start skipped: state=%s calls=%d", result.State, calls.Load())
		}
	})
	t.Run("expired budget before start", func(t *testing.T) {
		collector := NewIntelligenceCollector(newIntelligenceTestRepo())
		collector.dns = fixtureIntelligenceDNS("", dnsNXDomainCode(), false)
		expired := claim()
		expired.DeadlineAt = time.Now().Add(-time.Second)
		result := collector.Collect(context.Background(), expired, config)
		if result.State != "failed" || hasOutcomeCode(result, "lifecycle_inactive") {
			t.Fatalf("expired budget skipped: %s", result.State)
		}
	})
	t.Run("budget during collection", func(t *testing.T) {
		collector := NewIntelligenceCollector(newIntelligenceTestRepo())
		collector.dns = fixtureIntelligenceDNS("", dnsNXDomainCode(), false)
		collector.http = &http.Client{Transport: intelligenceRoundTripper(func(req *http.Request) (*http.Response, error) {
			<-req.Context().Done()
			return nil, req.Context().Err()
		})}
		running := claim()
		running.DeadlineAt = time.Now().Add(200 * time.Millisecond)
		result := collector.Collect(context.Background(), running, config)
		if result.State == "skipped" || (result.State != "partial" && result.State != "failed") || hasOutcomeCode(result, "lifecycle_inactive") || len(result.Blacklists) == 0 {
			t.Fatalf("budget end skipped: state=%s blacklists=%d outcomes=%+v", result.State, len(result.Blacklists), result.SourceOutcomes)
		}
	})
	t.Run("cancel after cached facts", func(t *testing.T) {
		repo := newIntelligenceTestRepo()
		collector := NewIntelligenceCollector(repo)
		collector.dns = fixtureIntelligenceDNS("", dnsNXDomainCode(), false)
		collector.http = &http.Client{Transport: intelligenceRoundTripper(fixtureIntelligenceHTTP)}
		seeded := collector.Collect(context.Background(), claim(), config)
		if seeded.State != "complete" || seeded.ASN.Name != "GOOGLE" {
			t.Fatalf("seed: %+v", seeded.ASN)
		}
		repo.save(seeded.CacheWrites)
		parent, cancel := context.WithCancel(context.Background())
		defer cancel()
		collector.http = &http.Client{Transport: intelligenceRoundTripper(func(req *http.Request) (*http.Response, error) {
			cancel()
			<-req.Context().Done()
			return nil, req.Context().Err()
		})}
		keyed := config
		keyed.Sources.IPAPIISKey = "one-private-key"
		result := collector.Collect(parent, claim(), keyed)
		if result.State == "skipped" || result.ASN.Number != "AS15169" || result.ASN.Name != "GOOGLE" || hasOutcomeCode(result, "lifecycle_inactive") {
			t.Fatalf("cancel discarded cached facts: state=%s asn=%+v outcomes=%+v", result.State, result.ASN, result.SourceOutcomes)
		}
	})
}

func TestIntelligenceFinalEligibilityRecheck(t *testing.T) {
	claim := IntelligenceClaim{Report: ReportWrite{IPAddress: "8.8.8.8", IPVersion: 4, ASN: "AS15169"}, DeadlineAt: time.Now().Add(time.Minute)}
	config := settings.IPQualitySettings{Enabled: true}
	collect := func(repo *intelligenceTestRepo) IntelligenceSnapshot {
		collector := NewIntelligenceCollector(repo)
		collector.http = &http.Client{Transport: intelligenceRoundTripper(fixtureIntelligenceHTTP)}
		collector.dns = fixtureIntelligenceDNS("", dnsNXDomainCode(), false)
		return collector.Collect(context.Background(), claim, config)
	}
	t.Run("definitive false clears", func(t *testing.T) {
		repo := newIntelligenceTestRepo()
		repo.eligibilityFn = func(ctx context.Context, _ IntelligenceClaim) (bool, error) {
			if _, ok := ctx.Deadline(); !ok {
				return false, nil
			}
			return true, nil
		}
		result := collect(repo)
		if result.State != "skipped" || len(result.CacheWrites) != 0 {
			t.Fatalf("authoritative false did not skip/clear: state=%s writes=%d", result.State, len(result.CacheWrites))
		}
	})
	t.Run("repository error keeps facts", func(t *testing.T) {
		repo := newIntelligenceTestRepo()
		repo.eligibilityFn = func(ctx context.Context, _ IntelligenceClaim) (bool, error) {
			if _, ok := ctx.Deadline(); !ok {
				return false, errors.New("repository unavailable")
			}
			return true, nil
		}
		result := collect(repo)
		if result.State == "skipped" || result.ASN.Number != "AS15169" || result.Registration.NetworkCountryCode != "US" || len(result.CacheWrites) == 0 || hasOutcomeCode(result, "lifecycle_inactive") {
			t.Fatalf("final eligibility error discarded facts: state=%s writes=%d asn=%+v", result.State, len(result.CacheWrites), result.ASN)
		}
	})
}

func TestIntelligenceRequestBoundedBodyAndSafeErrors(t *testing.T) {
	collector := NewIntelligenceCollector(nil)
	collector.http = &http.Client{Transport: intelligenceRoundTripper(func(req *http.Request) (*http.Response, error) {
		return intelligenceResponse(req, 200, strings.Repeat("x", 17)), nil
	})}
	s := &intelligenceSession{collector: collector, ctx: context.Background(), slots: make(chan struct{}, 4), quotas: map[string]time.Time{}}
	if _, err := s.request("ripe-ris", "8.8.8.8", "", http.MethodGet, "https://stat.ripe.net/data/network-info/data.json", nil, nil, 16); safeIntelligenceError(err) != "response_too_large" {
		t.Fatalf("body limit not enforced: %v", err)
	}
	unsafe := &url.URL{Scheme: "https", Host: "127.0.0.1"}
	if validIntelligenceURL(unsafe) {
		t.Fatal("literal private target allowed")
	}
}

func TestIntelligenceDROPFailureAndIPv6DatasetIsolation(t *testing.T) {
	for _, broken := range []bool{false, true} {
		collector := NewIntelligenceCollector(nil)
		collector.http = &http.Client{Transport: intelligenceRoundTripper(func(req *http.Request) (*http.Response, error) {
			if req.URL.String() != "https://www.spamhaus.org/drop/drop_v6.json" {
				t.Errorf("IPv6 queried wrong dataset: %s", req.URL)
			}
			body := `{"cidr":"2001:4860::/32","sblid":"SBL6","rir":"arin"}` + "\n"
			if !broken {
				body += `{"type":"metadata","timestamp":1791979200,"records":1,"copyright":"Spamhaus","terms":"Terms"}`
			}
			return intelligenceResponse(req, 200, body), nil
		})}
		s := &intelligenceSession{collector: collector, ctx: context.Background(), slots: make(chan struct{}, 4), quotas: map[string]time.Time{}}
		s.collectDROP(netip.MustParseAddr("2001:4860:4860::8888"))
		if len(s.snapshot.Blacklists) != 1 {
			t.Fatal("missing DROP result")
		}
		row := s.snapshot.Blacklists[0]
		if broken {
			if row.Status != "unknown" || row.DatasetAt != nil || len(s.snapshot.CacheWrites) != 0 {
				t.Fatalf("truncated dataset produced clean/matched evidence: %+v", row)
			}
		} else if row.Status != "listed" || row.MatchedRange != "2001:4860::/32" || row.DatasetAt == nil || len(s.snapshot.CacheWrites) != 1 {
			t.Fatalf("valid IPv6 match lost: %+v", row)
		}
	}
}

func TestIntelligenceExpiredAndMalformedCachesCannotSupplyFacts(t *testing.T) {
	repo := newIntelligenceTestRepo()
	now := time.Now().UTC()
	repo.cache[cacheTestKey("rdap-ip", "8.8.8.8", "")] = SourceCacheEntry{
		SourceID: "rdap-ip", Resource: "8.8.8.8", FetchedAt: now.Add(-25 * time.Hour), ExpiresAt: now.Add(-time.Hour),
		PayloadJSON: json.RawMessage(`{"status":"success","network_country_code":"JP","start_address":"8.8.8.0","end_address":"8.8.8.255"}`),
	}
	collector := NewIntelligenceCollector(repo)
	collector.http = &http.Client{Transport: intelligenceRoundTripper(fixtureIntelligenceHTTP)}
	s := &intelligenceSession{collector: collector, repo: repo, ctx: context.Background(), claim: IntelligenceClaim{Report: ReportWrite{IPAddress: "8.8.8.8", IPVersion: 4}}, slots: make(chan struct{}, 4), quotas: map[string]time.Time{}}
	s.collectRegistration()
	if s.snapshot.Registration.NetworkCountryCode != "US" {
		t.Fatalf("expired country reused: %+v", s.snapshot.Registration)
	}
	for _, entry := range []SourceCacheEntry{
		{SourceID: "rdap-ip", Resource: "8.8.8.8", PayloadJSON: json.RawMessage(`{"status":"success","start_address":"1.1.1.0","end_address":"1.1.1.255"}`)},
		{SourceID: "rdap-ip", Resource: "bootstrap:ipv4", PayloadJSON: json.RawMessage(`{"entries":[{"resource":"8.0.0.0/8","base_url":"https://evil.example/"}]}`)},
		{SourceID: "abuseipdb", Resource: "8.8.8.8", CredentialFingerprint: credentialFingerprint("test-key"), PayloadJSON: json.RawMessage(`{"provider":"abuseipdb","status":"success","key":"echo"}`)},
		{SourceID: "rdap-asn", Resource: "AS15169", PayloadJSON: json.RawMessage(`{"status":"success","number":"AS15169","ipv4_addresses":"1.5"}`)},
	} {
		if err := ValidateIntelligenceCacheEntry(entry); err == nil {
			t.Fatalf("invalid cache accepted for source %s", entry.SourceID)
		}
	}
}

func TestIntelligenceProviderWhitelistPersistsNoEchoAndKeepsZeroReports(t *testing.T) {
	collector := NewIntelligenceCollector(nil)
	collector.http = &http.Client{Transport: intelligenceRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.Path != "/api/v2/check" || req.URL.Query().Get("maxAgeInDays") != "90" || req.Header.Get("Key") != "private-key" {
			t.Error("AbuseIPDB read-only query contract changed")
		}
		return intelligenceResponse(req, 200, `{"data":{"ipAddress":"8.8.8.8","abuseConfidenceScore":0,"totalReports":0,"isTor":false,"usageType":"ISP","isp":"Example","domain":"example.com","key":"DO-NOT-PERSIST"},"echo":{"headers":{"Key":"DO-NOT-PERSIST"}}}`), nil
	})}
	s := &intelligenceSession{collector: collector, ctx: context.Background(), claim: IntelligenceClaim{Report: ReportWrite{IPAddress: "8.8.8.8", IPVersion: 4}}, slots: make(chan struct{}, 4), quotas: map[string]time.Time{}}
	s.collectProvider("abuseipdb", "private-key")
	if len(s.snapshot.Providers) != 1 || len(s.snapshot.CacheWrites) != 1 {
		t.Fatalf("provider failed: %+v", s.snapshot.SourceOutcomes)
	}
	row := s.snapshot.Providers[0]
	if string(row.ExtraJSON) != `{"total_reports":0}` || row.IsTor == nil || *row.IsTor || row.IsAbuser != nil || row.IsVPN != nil || row.Details.Scores[0].Value == nil || *row.Details.Scores[0].Value != 0 {
		t.Fatalf("zero reports/score or risk state lost: %+v", row)
	}
	if strings.Contains(string(s.snapshot.CacheWrites[0].PayloadJSON), "DO-NOT-PERSIST") || strings.Contains(string(s.snapshot.CacheWrites[0].PayloadJSON), "private-key") {
		t.Fatal("unchecked echo entered cache")
	}
}

func TestIntelligenceAlreadyCanceledNeverPerformsNetwork(t *testing.T) {
	collector := NewIntelligenceCollector(nil)
	collector.http = &http.Client{Transport: intelligenceRoundTripper(func(req *http.Request) (*http.Response, error) {
		t.Error("network after cancellation")
		return nil, context.Canceled
	})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := collector.Collect(ctx, IntelligenceClaim{Report: ReportWrite{IPAddress: "8.8.8.8", IPVersion: 4}}, settings.IPQualitySettings{Enabled: true, Sources: settings.IPQualitySources{IPAPIISKey: "configured-key"}})
	if result.State != "failed" || hasOutcomeCode(result, "lifecycle_inactive") || len(result.CacheWrites) != 0 || result.CredentialFingerprints["ipapi.is"] != credentialFingerprint("configured-key") {
		t.Fatalf("cancellation/fingerprint contract: state=%s outcomes=%+v", result.State, result.SourceOutcomes)
	}
}

func TestIntelligenceAuthenticatedFailureNeverRetainsFactsOrCredentialEcho(t *testing.T) {
	for _, body := range []string{
		`{"data":{"ipAddress":"1.1.1.1","abuseConfidenceScore":100,"isTor":true}}`,
		`{"data":{"ipAddress":"8.8.8.8","abuseConfidenceScore":100,"isp":"credential-private-key-echo"}}`,
	} {
		collector := NewIntelligenceCollector(nil)
		collector.http = &http.Client{Transport: intelligenceRoundTripper(func(req *http.Request) (*http.Response, error) { return intelligenceResponse(req, 200, body), nil })}
		s := &intelligenceSession{collector: collector, ctx: context.Background(), claim: IntelligenceClaim{Report: ReportWrite{IPAddress: "8.8.8.8", IPVersion: 4}}, slots: make(chan struct{}, 4), quotas: map[string]time.Time{}}
		s.collectProvider("abuseipdb", "private-key")
		if len(s.snapshot.Providers) != 1 || len(s.snapshot.CacheWrites) != 0 || len(s.snapshot.SourceOutcomes) != 1 || s.snapshot.SourceOutcomes[0].Status != "failure" {
			t.Fatal("failed authenticated source lost diagnostic sample or retained cache")
		}
		row := s.snapshot.Providers[0]
		if row.Status != "failure" || row.ErrorCode == "" || row.Details == nil || row.Details.Method != "center_api" || row.Details.APITier != "authenticated" {
			t.Fatal("failure sample lacks safe identity")
		}
		if err := ValidateIntelligenceSnapshot(s.snapshot, "8.8.8.8"); err != nil {
			t.Fatalf("failure sample rejected: %v", err)
		}
		encoded, _ := json.Marshal(s.snapshot)
		if strings.Contains(string(encoded), "private-key") {
			t.Fatal("credential echo reached snapshot")
		}
	}
}

func TestIntelligenceConfiguredFailurePreservesPublicFactsAndDiagnosticSample(t *testing.T) {
	report := ReportWrite{
		IPAddress: "8.8.8.8", IPVersion: 4, ASN: "AS15169", Status: "success", ObservedAt: testIntelligenceTime(),
		Product: &agentapi.IPQualityAgentProduct{Revision: 1, PTRNames: []string{}, Mail: []agentapi.IPQualityMailResult{}},
		ProviderResults: []ProviderResultWrite{{
			Provider: "ipapi.is", Status: "success", IsVPN: new(true),
			Details: &agentapi.IPQualityProviderDetails{
				QueriedIP: "8.8.8.8", ObservedAt: testIntelligenceTime(), Method: "agent_public", APITier: "anonymous",
				CountryCode: "US", City: "Public city", Scores: []agentapi.IPQualityScore{},
				FieldStates: map[string]string{"is_vpn": "known", "country_code": "known", "city": "known"},
			},
		}},
	}
	collector := NewIntelligenceCollector(nil)
	collector.dns = fixtureIntelligenceDNS("", dnsNXDomainCode(), false)
	collector.http = &http.Client{Transport: intelligenceRoundTripper(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() == "api.ipapi.is" {
			return intelligenceResponse(req, 500, `{"error":"private-key-echo"}`), nil
		}
		return fixtureIntelligenceHTTP(req)
	})}
	snapshot := collector.Collect(context.Background(), IntelligenceClaim{Report: report, DeadlineAt: time.Now().Add(time.Minute)}, settings.IPQualitySettings{
		Enabled: true, Sources: settings.IPQualitySources{IPAPIISKey: "private-key"},
	})
	if snapshot.State != "partial" || len(snapshot.Providers) != 1 || snapshot.Providers[0].Status != "failure" {
		t.Fatalf("configured failure diagnostic missing: state=%s providers=%+v", snapshot.State, snapshot.Providers)
	}
	if err := ValidateIntelligenceSnapshot(snapshot, report.IPAddress); err != nil {
		t.Fatalf("valid failure sample rejected: %v", err)
	}
	for _, entry := range snapshot.CacheWrites {
		if entry.SourceID == "ipapi.is" && entry.Resource != "@quota" {
			t.Fatal("failed authenticated sample cached")
		}
	}
	view := BuildProductView(report, &snapshot)
	if len(view.Providers) != 2 || view.Providers[0].Status != "success" || view.Providers[1].Status != "failure" {
		t.Fatalf("public success/failed enhancement samples not both visible: %+v", view.Providers)
	}
	selected := SelectedProductProviders(view.Providers)
	if len(selected) != 1 || selected[0].Status != "success" || selected[0].Details.Method != "agent_public" || selected[0].Details.City != "Public city" {
		t.Fatalf("failed enhancement replaced public facts: %+v", selected)
	}
	if len(view.Summary.RiskFlags) != 1 || view.Summary.RiskFlags[0] != "vpn" {
		t.Fatalf("public VPN fact lost on enhancement failure: %+v", view.Summary)
	}
}

func TestIntelligenceFailureSampleRejectsAllBusinessFactsAndCache(t *testing.T) {
	failure := func() ProviderResultRead {
		return ProviderResultRead{
			Provider: "ipapi.is", Status: "failure", ErrorCode: "http_failed",
			Details: &agentapi.IPQualityProviderDetails{
				QueriedIP: "8.8.8.8", ObservedAt: testIntelligenceTime(), Method: "center_api", APITier: "authenticated",
				Scores: []agentapi.IPQualityScore{}, FieldStates: map[string]string{},
			},
		}
	}
	for name, mutate := range map[string]func(*ProviderResultRead){
		"negative carrier fact": func(row *ProviderResultRead) { row.IsVPN = new(false) },
		"positive carrier fact": func(row *ProviderResultRead) { row.IsTor = new(true) },
		"location":              func(row *ProviderResultRead) { row.Details.City = "Retained city" },
		"typed negative fact":   func(row *ProviderResultRead) { row.Details.IsMobile = new(false) },
		"score":                 func(row *ProviderResultRead) { row.Details.Scores = []agentapi.IPQualityScore{{ID: "score"}} },
		"raw extra":             func(row *ProviderResultRead) { row.ExtraJSON = json.RawMessage(`{}`) },
		"wrong identity":        func(row *ProviderResultRead) { row.Details.QueriedIP = "1.1.1.1" },
		"missing error":         func(row *ProviderResultRead) { row.ErrorCode = "" },
	} {
		t.Run(name, func(t *testing.T) {
			row := failure()
			mutate(&row)
			if err := ValidateIntelligenceSnapshot(IntelligenceSnapshot{Providers: []ProviderResultRead{row}}, "8.8.8.8"); err == nil {
				t.Fatal("invalid failed sample accepted")
			}
		})
	}
	row := failure()
	if err := ValidateIntelligenceSnapshot(IntelligenceSnapshot{Providers: []ProviderResultRead{row}}, "8.8.8.8"); err != nil {
		t.Fatalf("metadata-only failure rejected: %v", err)
	}
	data, _ := json.Marshal(row)
	if err := ValidateIntelligenceCacheEntry(SourceCacheEntry{SourceID: "ipapi.is", Resource: "8.8.8.8", CredentialFingerprint: credentialFingerprint("private-key"), PayloadJSON: data}); err == nil {
		t.Fatal("failure admitted to successful-data cache")
	}
}
