package ipquality

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A test executable is not an Agent. Explicitly reject this private entrypoint
// so general collector tests cannot recursively execute tests or use the WAN.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "ip-quality-fetch" {
		os.Exit(2)
	}
	os.Exit(m.Run())
}

type detectorFixture struct {
	status int
	body   string
	err    error
}

func shellFixtureHelper(t *testing.T, fixtures map[string]detectorFixture, target string, family int) (string, string) {
	t.Helper()
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	for id, fixture := range fixtures {
		work := t.TempDir()
		client := &http.Client{Transport: serviceTestTransport(func(req *http.Request) (*http.Response, error) {
			if fixture.err != nil {
				return nil, fixture.err
			}
			return serviceTestResponse(req, fixture.status, fixture.body), nil
		})}
		facts, err := fetchDetectorEndpoint(context.Background(), client, id, target, family, work)
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(facts)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, id+".json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	helper := filepath.Join(dir, "fetch")
	source := fmt.Sprintf("#!/bin/bash\nset -eu\n[[ $1 == ip-quality-fetch && $3 == %q && $4 == %q ]] || exit 2\nprintf '%%s\\n' \"$2\" >> %q\n/bin/cp %q/\"$2.json\" \"$2.json\"\n", target, itoa(family), calls, dir)
	if err := os.WriteFile(helper, []byte(source), 0700); err != nil {
		t.Fatal(err)
	}
	return helper, calls
}
func shellReport(t *testing.T, module string, fixtures map[string]detectorFixture) detectorReport {
	t.Helper()
	helper, _ := shellFixtureHelper(t, fixtures, "23.160.160.61", 4)
	report, err := runDetectorWithHelper(context.Background(), module, "23.160.160.61", 4, helper)
	if err != nil {
		t.Fatal(err)
	}
	return report
}
func premiumFixture() string {
	return `<title>YouTube Premium</title><script>var ytInitialData = {"contentRegion":"US","contents":{"twoColumnBrowseResultsRenderer":{"tabs":[{"tabRenderer":{"content":{"sectionListRenderer":{"contents":[{"itemSectionRenderer":{"contents":[{"sectionGroupViewModel":{"sections":[{"lpHeaderSectionViewModel":{"subtitles":[{"content":"YouTube and YouTube Music ad-free, offline, and in the background"}]}},{"lpOfferCardSectionViewModel":{"offerCards":[{"lpOfferCardViewModel":{"title":"Individual"}}]}}]}}]}}]}}}}]}}};</script>`
}
func netflixFixture(id, content string) string {
	return `<meta property="al:android:app_name" content="Netflix"><meta property="al:android:url" content="nflx://www.netflix.com/title/` + id + `"><h1>Title</h1><body>` + content + `</body>`
}

func TestDetectorActualBashYouTubeEvidenceBoundaries(t *testing.T) {
	for _, test := range []struct{ name, body, status string }{
		{"real_renderer", premiumFixture(), "unlocked"},
		{"denial_precedes_offer", premiumFixture() + `<p>Premium is not available in your country</p>`, "blocked"},
		{"country_only", `<title>YouTube Premium</title><script>var ytInitialData={"contentRegion":"US"};</script>`, "unknown"},
		{"arbitrary_marketing", `<title>YouTube Premium</title><script>var ytInitialData={"contentRegion":"US","marketing":"ad-free"};</script>`, "unknown"},
		{"untrusted_script", strings.Replace(premiumFixture(), "var ytInitialData = ", "var unrelated = ", 1), "unknown"},
		{"comment", `<title>YouTube Premium</title><!-- ad-free --><script>var ytInitialData={};</script>`, "unknown"},
		{"challenge", premiumFixture() + `<div class="g-recaptcha">Verify you are human</div>`, "unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := shellReport(t, "youtube-premium", map[string]detectorFixture{"youtube-premium": {200, test.body, nil}})
			if r.Result.Status != test.status {
				t.Fatalf("%+v", r)
			}
		})
	}
}
func TestDetectorActualBashNetflixOriginalsControl(t *testing.T) {
	denial := "Oh no! This title currently isn’t available to watch in your country."
	for _, test := range []struct{ name, original, want, scope string }{
		{"original_positive", "Seasons Episodes Trailers", "partial", "originals_only"},
		{"all_denied", denial, "blocked", "catalog_unavailable"},
		{"original_unknown", "", "unknown", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := netflixFixture("80057281", test.original)
			if test.original == "" {
				original = `<html>Welcome</html>`
			}
			fixtures := map[string]detectorFixture{"netflix-81280792": {200, netflixFixture("81280792", denial), nil}, "netflix-70143836": {200, netflixFixture("70143836", denial), nil}, "netflix-80057281": {200, original, nil}}
			r := shellReport(t, "netflix", fixtures)
			if r.Result.Status != test.want || r.Result.UnlockType != test.scope || len(r.Diagnostics) != 3 {
				t.Fatalf("%+v", r)
			}
		})
	}
	helper, calls := shellFixtureHelper(t, map[string]detectorFixture{"netflix-81280792": {200, netflixFixture("81280792", "Episodes"), nil}, "netflix-70143836": {200, netflixFixture("70143836", denial), nil}}, "23.160.160.61", 4)
	r, err := runDetectorWithHelper(context.Background(), "netflix", "23.160.160.61", 4, helper)
	if err != nil || r.Result.Status != "unlocked" {
		t.Fatalf("%+v %v", r, err)
	}
	data, _ := os.ReadFile(calls)
	if strings.Contains(string(data), "80057281") {
		t.Fatal("unnecessary originals control request")
	}
}

func TestDetectorNetflixVisibleAriaHiddenDenial(t *testing.T) {
	denial := `<div data-uia="locally-unavailable"><div role="alert"></div><div aria-hidden="true"><div>Oh no! This title currently isn’t available to watch in your country.</div></div></div>`
	fixtures := map[string]detectorFixture{
		"netflix-81280792": {200, netflixFixture("81280792", denial), nil},
		"netflix-70143836": {200, netflixFixture("70143836", denial), nil},
		"netflix-80057281": {200, netflixFixture("80057281", "Seasons Episodes Trailers"), nil},
	}
	report := shellReport(t, "netflix", fixtures)
	if report.Result.Status != "partial" || report.Result.UnlockType != "originals_only" {
		t.Fatalf("visible country denial lost: %+v", report)
	}
	// Actual visual hiding remains excluded; accessibility hiding does not.
	for _, attr := range []string{`hidden`, `style="display:none"`, `style="visibility:hidden"`} {
		fixtures["netflix-81280792"] = detectorFixture{200, netflixFixture("81280792", "<div "+attr+">"+denial+"</div>Episodes"), nil}
		report = shellReport(t, "netflix", fixtures)
		if report.Result.Status != "unlocked" {
			t.Fatalf("non-visible country denial became evidence (%s): %+v", attr, report)
		}
	}
}
func TestDetectorActualBashChatGPTAnonymousAccess(t *testing.T) {
	for _, test := range []struct {
		name, cookie, ios, want string
		cookieStatus, iosStatus int
	}{
		{"cookie_false_plus_dc", `{"cookie_consent_required":false}`, `{"type":"dc","cf_details":"Request is not allowed. Please try again later."}`, "unlocked", 200, 403},
		{"cookie_true_plus_dc", `{"cookie_consent_required":true}`, `{"type":"dc"}`, "unlocked", 200, 403},
		{"explicit_conflict", `{"cookie_consent_required":false}`, `{"error":{"code":"unsupported_country"}}`, "partial", 200, 403},
		{"explicit_denial", `{"error":{"code":"unsupported_country_region_territory"}}`, `{"type":"dc"}`, "blocked", 403, 403},
		{"empty_json", `{}`, `{}`, "unknown", 200, 200},
		{"null_boolean", `{"cookie_consent_required":null}`, `{}`, "unknown", 200, 200},
		{"string_boolean", `{"cookie_consent_required":"false"}`, `{}`, "unknown", 200, 200},
		{"challenge", `<div class="g-recaptcha">Verify you are human</div>`, `{}`, "unknown", 200, 200},
		{"unrelated_vpn", `{}`, `<title>ChatGPT</title><body>OpenAI help: disable your VPN</body>`, "unknown", 200, 200},
		{"branded_error", `{}`, `<title>ChatGPT</title><body>OpenAI help: Service unavailable</body>`, "unknown", 200, 200},
		{"branded_generic", `{}`, `<title>ChatGPT</title><body>OpenAI</body>`, "unknown", 200, 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := shellReport(t, "chatgpt", map[string]detectorFixture{"openai-cookie": {test.cookieStatus, test.cookie, nil}, "openai-ios": {test.iosStatus, test.ios, nil}})
			if r.Result.Status != test.want {
				t.Fatalf("%+v", r)
			}
			if strings.Contains(test.name, "plus_dc") && r.Diagnostics[1].ErrorCode != "client_access_denied" {
				t.Fatalf("lost endpoint diagnostic: %+v", r)
			}
		})
	}
	r := shellReport(t, "chatgpt", map[string]detectorFixture{"openai-cookie": {0, "", context.DeadlineExceeded}, "openai-ios": {0, "", context.DeadlineExceeded}})
	if r.Result.Status != "unknown" {
		t.Fatal(r)
	}
}
func TestDetectorActualBashPrimeCompleteResponse(t *testing.T) {
	for _, test := range []struct {
		name, body, want string
		status           int
	}{
		{"large_valid", `<title>Prime Video</title><script>{"currentTerritory":"US"}</script>` + strings.Repeat(" ", 5_173_650), "unlocked", 200},
		{"denial_precedes_territory", `<title>Prime Video</title><script>{"currentTerritory":"US","isServiceRestricted":true}</script>`, "blocked", 200},
		{"missing_business", `<title>Prime Video</title><body>Welcome</body>`, "unknown", 200},
		{"false_without_territory", `<title>Prime Video</title><script>{"isServiceRestricted":false}</script>`, "unknown", 200},
		{"over_limit", `<title>Prime Video</title><script>{"currentTerritory":"US"}</script>` + strings.Repeat(" ", 8*1024*1024), "unknown", 200},
		{"server_error", `<title>Prime Video</title><script>{"currentTerritory":"US"}</script>`, "unknown", 503},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := shellReport(t, "amazon-prime-video", map[string]detectorFixture{"amazon-prime-video": {test.status, test.body, nil}})
			if r.Result.Status != test.want {
				t.Fatalf("%+v", r)
			}
			if test.name == "large_valid" && r.Result.Region != "US" {
				t.Fatal(r)
			}
			if test.name == "over_limit" && r.Result.ErrorCode != "response_too_large" {
				t.Fatal(r)
			}
		})
	}
}
func TestDetectorActualBashGeoFallbackAndIdentity(t *testing.T) {
	for _, test := range []struct {
		name, body string
		count      int
	}{
		{"us_native", `{"data":{"ip":"23.160.160.61","country":"US","abuse":{"country":"US"}}}`, 1},
		{"different_abuse", `{"data":{"ip":"23.160.160.61","country":"US","abuse":{"country":"CA"}}}`, 1},
		{"missing_abuse", `{"data":{"ip":"23.160.160.61","country":"US"}}`, 1},
		{"missing_country", `{"data":{"ip":"23.160.160.61"}}`, 0},
		{"wrong_ip", `{"data":{"ip":"23.160.160.62","country":"US","abuse":{"country":"US"}}}`, 0},
		{"missing_ip", `{"data":{"country":"US"}}`, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := shellReport(t, "geo", map[string]detectorFixture{"geo-maxmind": {403, `<div class="g-recaptcha">Verify you are human</div>`, nil}, "geo-ipinfo": {200, test.body, nil}})
			if len(r.Result.Samples) != test.count || len(r.Diagnostics) != 2 {
				t.Fatal(r)
			}
			if test.count > 0 && r.Result.Samples[0].RegisteredCountryCode != "" {
				t.Fatal("abuse country became registered country")
			}
		})
	}
	maxmind := `{"IP":"23.160.160.61","Country":{"IsoCode":"US","RegisteredCountry":{"IsoCode":"US"}},"City":{"Name":"Los Angeles"}}`
	helper, calls := shellFixtureHelper(t, map[string]detectorFixture{"geo-maxmind": {200, maxmind, nil}}, "23.160.160.61", 4)
	r, err := runDetectorWithHelper(context.Background(), "geo", "23.160.160.61", 4, helper)
	if err != nil || len(r.Result.Samples) != 1 || r.Result.Samples[0].RegisteredCountryCode != "US" {
		t.Fatalf("%+v %v", r, err)
	}
	data, _ := os.ReadFile(calls)
	if strings.Contains(string(data), "geo-ipinfo") {
		t.Fatal("fallback ran with complete MaxMind evidence")
	}
}
func TestDetectorFetchGzipCompletenessAndPrivateFiles(t *testing.T) {
	for _, size := range []int{5 * 1024 * 1024, 8*1024*1024 + 1} {
		var compressed bytes.Buffer
		z := gzip.NewWriter(&compressed)
		_, _ = io.WriteString(z, strings.Repeat("x", size))
		_ = z.Close()
		client := &http.Client{Transport: serviceTestTransport(func(req *http.Request) (*http.Response, error) {
			r := serviceTestResponse(req, 200, compressed.String())
			r.Header.Set("Content-Encoding", "gzip")
			return r, nil
		})}
		dir := t.TempDir()
		facts, err := fetchDetectorEndpoint(context.Background(), client, "amazon-prime-video", "23.160.160.61", 4, dir)
		if err != nil {
			t.Fatal(err)
		}
		if size > 8*1024*1024 && facts.ErrorCode != "response_too_large" {
			t.Fatal(facts)
		}
		info, err := os.Stat(filepath.Join(dir, "amazon-prime-video.body"))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("private body %v %v", info, err)
		}
		if _, err = fetchDetectorEndpoint(context.Background(), client, "amazon-prime-video", "23.160.160.61", 4, dir); err == nil {
			t.Fatal("overwrote exclusive response file")
		}
	}
}
func TestDetectorFetchClosedInputsAndRedirects(t *testing.T) {
	for _, args := range [][]string{{}, {"https://localhost/", "23.160.160.61", "4"}, {"openai-cookie", "127.0.0.1", "4"}, {"openai-cookie", "23.160.160.61", "6"}, {"openai-cookie", "23.160.160.61", "4", "arbitrary-output"}} {
		if RunIPQualityFetch(args) != 2 {
			t.Fatal(args)
		}
	}
	for _, destination := range []string{"http://www.netflix.com/title/1", "https://127.0.0.1/", "https://private.example/", "https://user:secret@www.netflix.com/", "https://www.netflix.com:8443/"} {
		calls := 0
		client := &http.Client{Transport: serviceTestTransport(func(req *http.Request) (*http.Response, error) {
			calls++
			r := serviceTestResponse(req, 302, "")
			r.Header.Set("Location", destination)
			return r, nil
		})}
		facts, err := fetchDetectorEndpoint(context.Background(), client, "netflix-81280792", "23.160.160.61", 4, t.TempDir())
		if err != nil || facts.ErrorCode != "unexpected_redirect" || calls != 1 {
			t.Fatalf("%s %+v %v calls=%d", destination, facts, err, calls)
		}
	}
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "169.254.1.1", "::1", "fc00::1", "::ffff:23.160.160.61"} {
		family := 4
		if netip.MustParseAddr(ip).Is6() {
			family = 6
		}
		if validDetectorTarget(ip, family) {
			t.Fatal("accepted nonpublic address", ip)
		}
	}
}
func TestDetectorDependenciesAndOutputBounds(t *testing.T) {
	if _, err := detectorDependency("houfeng-certainly-missing-dependency"); detectorErrorCode(err) != "detector_dependency_missing" {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &detectorOutput{limit: 10, cancel: cancel}
	if _, err := w.Write([]byte("01234567890")); err == nil || !w.exceeded || ctx.Err() == nil || w.Len() != 0 {
		t.Fatal("unbounded output")
	}
}

func TestDetectorOutputIOCopyCannotBypassLimit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	output := &detectorOutput{limit: 10, cancel: cancel}
	if _, promoted := any(output).(io.ReaderFrom); promoted {
		t.Fatal("output exposes an uncapped ReaderFrom fast path")
	}
	// LimitReader has no WriterTo, so this exercises the destination fast-path
	// selection used by os/exec's pipe-copy goroutine.
	reader := io.LimitReader(strings.NewReader(strings.Repeat("x", 100)), 100)
	if _, err := io.Copy(output, reader); err == nil || !output.exceeded || ctx.Err() == nil || output.Len() > 10 {
		t.Fatal("io.Copy bypassed the output limit")
	}
}
func TestDetectorCancellationReclaimsDescendants(t *testing.T) {
	dir := t.TempDir()
	pidfile := filepath.Join(dir, "pid")
	helper := filepath.Join(dir, "fetch")
	script := fmt.Sprintf("#!/bin/bash\nsleep 30 &\nprintf '%%s' $! > %q\nwait\n", pidfile)
	if err := os.WriteFile(helper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, err := runDetectorWithHelper(ctx, "youtube-premium", "23.160.160.61", 4, helper)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	data, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	assertNoRunningDetectorChild(t, pid)
}
func TestDetectorUnselectedFamilyHasNoRequests(t *testing.T) {
	target := "2606:4700:4700::1111"
	helper, calls := shellFixtureHelper(t, map[string]detectorFixture{"openai-cookie": {200, `{"cookie_consent_required":false}`, nil}, "openai-ios": {403, `{"type":"dc"}`, nil}}, target, 6)
	report, err := runDetectorWithHelper(context.Background(), "chatgpt", target, 6, helper)
	if err != nil || report.IPVersion != 6 || report.Result.Status != "unlocked" {
		t.Fatalf("%+v %v", report, err)
	}
	data, _ := os.ReadFile(calls)
	if len(strings.Fields(string(data))) != 2 {
		t.Fatalf("unexpected endpoints: %s", data)
	}
}
func TestDetectorMaliciousDataNeverBecomesJQCode(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "must-not-exist")
	payload := map[string]any{"data": map[string]any{"ip": "23.160.160.61", "country": "US", "city": "\"; system(\"touch " + sentinel + "\"); #", "abuse": map[string]any{"country": "US"}}}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	r := shellReport(t, "geo", map[string]detectorFixture{"geo-maxmind": {403, `{}`, nil}, "geo-ipinfo": {200, string(body), nil}})
	if len(r.Result.Samples) != 1 || !strings.Contains(r.Result.Samples[0].City, "system(") {
		t.Fatal(r)
	}
	if _, err = os.Stat(sentinel); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("response became executable code", err)
	}
}

func TestDetectorProcessOutputLimitsAndMissingDependency(t *testing.T) {
	for _, test := range []struct{ name, program, code string }{
		{"stdout", `while :; do printf '%1024s' x; done`, "detector_output_limit"},
		{"stderr", `while :; do printf '%1024s' x >&2; done`, "detector_output_limit"},
		{"dependency", `exit 70`, "detector_dependency_missing"},
		{"invalid_json", `printf '{}\n'`, "detector_invalid_output"},
		{"multiple_json", `printf '{}\n{}\n'`, "detector_invalid_output"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_, err := runDetectorProgram(ctx, "youtube-premium", "23.160.160.61", 4, "/unused", []byte(test.program))
			if detectorErrorCode(err) != test.code {
				t.Fatalf("error %v, want %s", err, test.code)
			}
		})
	}
}

func TestDetectorEnvironmentAndClosedStandardInput(t *testing.T) {
	t.Setenv("HOUFENG_AGENT_TOKEN", "never-inherit")
	t.Setenv("BASH_ENV", "/must/not/be/sourced")
	t.Setenv("ENV", "/must/not/be/sourced")
	t.Setenv("HTTPS_PROXY", "http://secret-proxy.invalid")
	program := []byte(`[[ -z ${HOUFENG_AGENT_TOKEN+x} && -z ${BASH_ENV+x} && -z ${ENV+x} && -z ${HTTPS_PROXY+x} ]] || exit 99
 read -r unexpected && exit 99
 exit 70`)
	_, err := runDetectorProgram(context.Background(), "youtube-premium", "23.160.160.61", 4, "/unused", program)
	if detectorErrorCode(err) != "detector_dependency_missing" {
		t.Fatalf("environment or stdin boundary failed: %v", err)
	}
}

func TestDetectorSchemaRejectsWrongIdentityAndNullConclusion(t *testing.T) {
	valid := detectorReport{SchemaRevision: 1, Module: "youtube-premium", TargetIP: "23.160.160.61", IPVersion: 4, UpstreamCommit: detectorUpstreamCommit, ForkRevision: 1, Result: detectorResult{Status: "unlocked", Rule: "youtube_premium_offer", Samples: []detectorGeoSample{}}, Diagnostics: []detectorDiagnostic{{Endpoint: "youtube-premium", Outcome: "positive", HTTPStatus: 200}}}
	body, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		strings.Replace(string(body), `"ip_version":4`, `"ip_version":6`, 1),
		strings.Replace(string(body), `"target_ip":"23.160.160.61"`, `"target_ip":"23.160.160.62"`, 1),
		strings.Replace(string(body), `"status":"unlocked"`, `"status":null`, 1),
		strings.Replace(string(body), `"status":"unlocked"`, `"status":"null"`, 1),
		strings.Replace(string(body), `"schema_revision":1`, `"schema_revision":2`, 1),
		strings.Replace(string(body), `"endpoint":"youtube-premium"`, `"endpoint":"openai-cookie"`, 1),
		string(body) + string(body),
	} {
		var result detectorReport
		if decodeDetectorReport([]byte(bad), "youtube-premium", "23.160.160.61", 4, &result) == nil {
			t.Fatalf("accepted invalid report %s", bad)
		}
	}
}

func TestDetectorFetchForcedFamilyAndRebindingBoundary(t *testing.T) {
	for _, family := range []int{4, 6} {
		address := netip.MustParseAddr("8.8.8.8")
		expectedDNS, expectedTCP := "ip4", "tcp4"
		if family == 6 {
			address = netip.MustParseAddr("2606:4700:4700::1111")
			expectedDNS, expectedTCP = "ip6", "tcp6"
		}
		dialed := 0
		lookup := func(_ context.Context, network, host string) ([]netip.Addr, error) {
			if network != expectedDNS || host != "www.youtube.com" {
				t.Fatalf("lookup %s %s", network, host)
			}
			return []netip.Addr{address}, nil
		}
		dial := func(_ context.Context, network, target string) (net.Conn, error) {
			dialed++
			if network != expectedTCP || target != net.JoinHostPort(address.String(), "443") {
				t.Fatalf("dial %s %s", network, target)
			}
			return nil, nil
		}
		if _, err := dialDetectorAddress(context.Background(), family, "www.youtube.com:443", lookup, dial); err != nil || dialed != 1 {
			t.Fatalf("family %d: %d %v", family, dialed, err)
		}
		for _, forbidden := range []string{"127.0.0.1", "10.0.0.1", "100.64.0.1", "169.254.169.254", "::1", "fc00::1", "64:ff9b::a00:1"} {
			lookup := func(context.Context, string, string) ([]netip.Addr, error) {
				return []netip.Addr{netip.MustParseAddr(forbidden)}, nil
			}
			if _, err := dialDetectorAddress(context.Background(), family, "www.youtube.com:443", lookup, dial); !errors.Is(err, errServiceRedirect) {
				t.Fatalf("unsafe resolved address %s accepted: %v", forbidden, err)
			}
		}
		client := detectorHTTPClient(family)
		if client.Transport.(*http.Transport).Proxy != nil {
			t.Fatal("inherited environment proxy")
		}
		client.CloseIdleConnections()
	}
}

func TestDetectorAnonymousProviderMappingKeepsAbuseSeparate(t *testing.T) {
	r := shellReport(t, "geo", map[string]detectorFixture{"geo-maxmind": {403, `{}`, nil}, "geo-ipinfo": {200, `{"data":{"ip":"23.160.160.61","country":"US","city":"Los Angeles","asn":{"asn":"AS1234","name":"Example"},"abuse":{"country":"US"}}}`, nil}})
	now := time.Now().UTC()
	rows := scriptGeoOutcomes(r, "23.160.160.61", now, now)
	var found bool
	for _, row := range rows {
		if row.Result.Provider != "ipinfo" {
			continue
		}
		found = true
		if row.Result.Status != "success" || row.Result.Details == nil || row.Result.Details.ASN != "AS1234" || row.Result.Details.RegisteredCountryCode != "" || row.Result.IsProxy != nil || row.Result.IsVPN != nil {
			t.Fatalf("invented/missing fields: %+v", row)
		}
		// Inspect the public snake_case provenance, not an arbitrary raw upstream body.
		var payload map[string]any
		if json.Unmarshal(row.Result.ExtraJSON, &payload) != nil {
			t.Fatal("invalid evidence")
		}
		classification, _ := payload["geo_classification"].(map[string]any)
		if classification["basis"] != "abuse_contact_country" || classification["country_code"] != "US" || classification["comparison_country_code"] != "US" {
			t.Fatal(classification)
		}
	}
	if !found {
		t.Fatal("missing real fallback provider row")
	}
}

func TestDetectorPinnedEmbeddedAssetHashes(t *testing.T) {
	manifest, err := detectorAssets.ReadFile("detector_assets/SHA256SUMS")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(manifest)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatal(line)
		}
		data, err := detectorAssets.ReadFile("detector_assets/" + fields[1])
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if fmt.Sprintf("%x", sum) != fields[0] {
			t.Fatalf("pinned asset hash mismatch: %s", fields[1])
		}
	}
}

func TestDetectorNormalExitKillsLeftoverChild(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "child-pid")
	program := []byte(fmt.Sprintf("sleep 30 &\nprintf '%%s' $! > %q\nexit 70\n", pidfile))
	_, err := runDetectorProgram(context.Background(), "youtube-premium", "23.160.160.61", 4, "/unused", program)
	if detectorErrorCode(err) != "detector_dependency_missing" {
		t.Fatal(err)
	}
	data, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	assertNoRunningDetectorChild(t, pid)
}

func TestDetectorFetchRejectsTruncatedCompressedAndOversizeService(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, _ = io.WriteString(writer, premiumFixture())
	_ = writer.Close()
	truncated := compressed.Bytes()[:compressed.Len()-5]
	for _, test := range []struct{ name, body, encoding, code string }{
		{"incomplete_gzip", string(truncated), "gzip", "invalid_response"},
		{"service_limit", premiumFixture() + strings.Repeat(" ", serviceBodyLimit), "", "response_too_large"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &http.Client{Transport: serviceTestTransport(func(req *http.Request) (*http.Response, error) {
				if req.Method != http.MethodGet || req.Header.Get("Cookie") != "" || req.Header.Get("Authorization") != "" || req.Header.Get("Accept-Language") != "en-US,en;q=0.9" {
					t.Fatalf("unsafe anonymous request: %+v", req)
				}
				r := serviceTestResponse(req, 200, test.body)
				r.Header.Set("Content-Encoding", test.encoding)
				return r, nil
			})}
			facts, err := fetchDetectorEndpoint(context.Background(), client, "youtube-premium", "23.160.160.61", 4, t.TempDir())
			if err != nil || facts.ErrorCode != test.code || len(facts.YouTubeOffers) != 0 {
				t.Fatalf("parsed incomplete response: %+v %v", facts, err)
			}
		})
	}
}

func assertNoRunningDetectorChild(t *testing.T, pid int) {
	t.Helper()
	// SIGKILL delivery can finish just after kill(2) returns. Bound that scheduling
	// window without treating a still-running descendant as success.
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return
		}
		stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err == nil {
			end := strings.LastIndex(string(stat), ")")
			if end >= 0 && strings.HasPrefix(string(stat)[end+1:], " Z") {
				return
			}
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("detector descendant %d remained running", pid)
		}
	}
}

func TestScriptServicesCanonicalAndAmbiguityBarriers(t *testing.T) {
	services := []string{"netflix", "youtube-premium", "amazon-prime-video", "chatgpt"}
	for _, service := range services {
		for _, target := range []string{"", "127.0.0.1", "23.160.160.061"} {
			_, err := runDetectorWithHelper(context.Background(), service, target, 4, "/must-not-execute")
			if detectorErrorCode(err) != "missing_target_ip" {
				t.Fatalf("%s %s: %v", service, target, err)
			}
		}
	}
	for _, row := range ambiguousServiceOutcomes(services, 4) {
		if row.Result.Status != "unknown" || row.Result.ErrorCode != "egress_ambiguous" || row.Result.Checks != nil {
			t.Fatalf("fabricated checks after ambiguity: %+v", row)
		}
		var evidence map[string]any
		if json.Unmarshal(row.Raw, &evidence) != nil || evidence["probe_revision"] != float64(5) || evidence["evidence_kind"] != "ipquality_script" {
			t.Fatalf("wrong per-row version: %s", row.Raw)
		}
	}
}
