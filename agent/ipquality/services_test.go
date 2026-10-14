package ipquality

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"
)

type serviceTestTransport func(*http.Request) (*http.Response, error)

func (f serviceTestTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
func serviceTestResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}
}
func serviceTestCollector(handler serviceTestTransport) *HTTPCollector {
	return NewHTTPCollector(HTTPCollectorOptions{Client: &http.Client{Transport: handler}})
}

// Shared deterministic protocol fixtures, also usable by collector integration
// tests. No network request or fixture contains real credentials.
func anonymousServiceFixture(req *http.Request) string {
	switch req.URL.Hostname() {
	case "www.netflix.com":
		id := strings.TrimPrefix(req.URL.Path, "/title/")
		name := "LEGO Ninjago"
		if id == "70143836" {
			name = "Breaking Bad"
		}
		return fmt.Sprintf(`<html><meta property="al:android:app_name" content="Netflix"><meta property="al:android:url" content="nflx://www.netflix.com/title/%s"><h1> %s </h1><script>{"country":{"id":"US","countryName":"United States"}}</script></html>`, id, name)
	case "api.openai.com":
		return `{"cookie_consent_required":false}`
	case "ios.chat.openai.com":
		return `<html><title>ChatGPT</title><body>OpenAI</body></html>`
	case "www.youtube.com":
		return `<html><body>Enjoy ad-free videos</body><script>var ytInitialData = {"contentRegion":"US"};</script></html>`
	case "www.primevideo.com":
		return `<html><title>Prime Video</title><script>{"currentTerritory":"US","isServiceRestricted":false}</script></html>`
	case "disney.api.edge.bamgrid.com":
		switch req.URL.Path {
		case "/devices":
			return `{"assertion":"test-device-assertion"}`
		case "/token":
			return `{"refresh_token":"test-refresh-token"}`
		default:
			return `{"extensions":{"sdk":{"session":{"location":{"countryCode":"US"},"inSupportedLocation":true}}}}`
		}
	case "www.tiktok.com":
		return `<html><title>TikTok</title><script id="__UNIVERSAL_DATA_FOR_REHYDRATION__">{"region":"US"}</script></html>`
	case "www.reddit.com":
		return `<html><shreddit-app country="US"></shreddit-app></html>`
	default:
		return ""
	}
}

func TestServiceProbeBudgetStartsAfterSharedSlotAcquisition(t *testing.T) {
	for _, test := range []struct {
		name    string
		budget  time.Duration
		success int
	}{
		{"queued_probe_gets_own_budget", 30 * time.Second, 2},
		{"parent_deadline_still_bounds_round", 12 * time.Second, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				collector := serviceTestCollector(func(req *http.Request) (*http.Response, error) {
					select {
					case <-time.After(9 * time.Second):
						return serviceTestResponse(req, 200, anonymousServiceFixture(req)), nil
					case <-req.Context().Done():
						return nil, req.Context().Err()
					}
				})
				ctx, cancel := context.WithTimeout(context.Background(), test.budget)
				defer cancel()
				slots := make(chan struct{}, 1)
				results := make(chan []serviceProbeOutcome, 2)
				for _, family := range []int{4, 6} {
					go func() {
						results <- collector.collectDefaultServiceUnlocksWithSemaphore(ctx, []string{"youtube-premium"}, family, slots)
					}()
				}
				success := 0
				for range 2 {
					result := (<-results)[0].Result
					if result.Status == "unlocked" && result.Region == "US" {
						success++
					} else if result.Status != "unknown" || result.ErrorCode != "timeout" {
						t.Fatalf("unexpected queued probe result: %+v", result)
					}
				}
				if success != test.success {
					t.Fatalf("successful family probes = %d, want %d", success, test.success)
				}
			})
		})
	}
}
func restrictedServiceFixture(service string, req *http.Request) string {
	switch service {
	case "netflix":
		return `<html><meta property="al:android:app_name" content="Netflix"><body>Oh no!</body></html>`
	case "chatgpt":
		if req.URL.Hostname() == "api.openai.com" {
			return `{"error":{"code":"unsupported_country_region_territory"}}`
		}
		return `<html><title>ChatGPT</title><body>Please disable your VPN</body></html>`
	case "youtube-premium":
		return `<html><body>Premium is not available in your country</body><script>var ytInitialData = {};</script></html>`
	case "amazon-prime-video":
		return `<html><title>Prime Video</title><script>{"isServiceRestricted":true}</script></html>`
	case "disney-plus":
		return `{"error_description":"forbidden-location"}`
	case "tiktok":
		return `<html><title>TikTok</title><body>Not available in your region</body></html>`
	case "reddit":
		return `<html><body>You've been blocked by network security</body></html>`
	default:
		return ""
	}
}

var anonymousTestServices = []string{"netflix", "chatgpt", "youtube-premium", "amazon-prime-video", "disney-plus", "tiktok", "reddit"}

func TestAnonymousServiceProtocolMatrices(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		expected string
		body     func(string, *http.Request) string
	}{
		{"positive", 200, "unlocked", func(_ string, r *http.Request) string { return anonymousServiceFixture(r) }},
		{"restricted", 403, "blocked", restrictedServiceFixture},
		{"challenge", 200, "unknown", func(_ string, r *http.Request) string {
			return `<html><title>Just a moment...</title><div class="g-recaptcha">Verify you are human</div></html>`
		}},
		{"generic_html", 200, "unknown", func(_ string, r *http.Request) string { return `<html><title>Welcome</title><body>Hello</body></html>` }},
		{"empty_json", 200, "unknown", func(_ string, r *http.Request) string { return `{}` }},
		{"malformed_json", 200, "unknown", func(_ string, r *http.Request) string { return `{"region":` }},
		{"empty_body", 200, "unknown", func(_ string, r *http.Request) string { return `` }},
		{"429_even_with_restriction", 429, "unknown", restrictedServiceFixture},
		{"500_even_with_positive", 500, "unknown", func(_ string, r *http.Request) string { return anonymousServiceFixture(r) }},
		{"500_even_with_restriction", 500, "unknown", restrictedServiceFixture},
		{"401_generic", 401, "unknown", func(_ string, r *http.Request) string { return `<html>Unauthorized</html>` }},
		{"403_generic", 403, "unknown", func(_ string, r *http.Request) string { return `<html>Forbidden</html>` }},
	}
	for _, service := range anonymousTestServices {
		for _, test := range cases {
			t.Run(service+"/"+test.name, func(t *testing.T) {
				collector := serviceTestCollector(func(req *http.Request) (*http.Response, error) {
					if req.Header.Get("User-Agent") != serviceUserAgent || req.Header.Get("Accept-Language") != "en-US,en;q=0.9" {
						t.Fatal("common headers missing")
					}
					return serviceTestResponse(req, test.status, test.body(service, req)), nil
				})
				outcome := collector.probeService(context.Background(), service, 4)
				expected := test.expected
				if service == "netflix" && test.name == "restricted" {
					expected = "partial"
				}
				if outcome.Result.Status != expected {
					t.Fatalf("result = %+v, want %s", outcome.Result, expected)
				}
				probeStatus := sourceStatusSuccess
				if expected == "unknown" {
					probeStatus = sourceStatusFailure
				}
				if outcome.Result.ProbeStatus != probeStatus || outcome.Result.LatencyMS == nil {
					t.Fatalf("missing completion facts: %+v", outcome.Result)
				}
				if expected == "unknown" && (outcome.Result.Region != "" || outcome.Result.UnlockType != "") {
					t.Fatalf("unknown leaked facts: %+v", outcome.Result)
				}
				if test.name == "challenge" && outcome.Result.ErrorCode != "challenge_detected" {
					t.Fatalf("challenge result = %+v", outcome.Result)
				}
				var evidence map[string]any
				if json.Unmarshal(outcome.Raw, &evidence) != nil || evidence["probe_revision"] != float64(3) || evidence["evidence_kind"] != "anonymous_service_probe" {
					t.Fatalf("invalid evidence: %s", outcome.Raw)
				}
			})
		}
	}
}

func TestAnonymousServiceSpecificBoundaries(t *testing.T) {
	cases := []struct {
		name, service, wantStatus, wantScope string
		handler                              serviceTestTransport
	}{
		{"netflix_one_unavailable", "netflix", "unlocked", "catalog_sample", func(r *http.Request) (*http.Response, error) {
			body := anonymousServiceFixture(r)
			if strings.HasSuffix(r.URL.Path, "70143836") {
				body = restrictedServiceFixture("netflix", r)
			}
			return serviceTestResponse(r, 200, body), nil
		}},
		{"netflix_missing_title_meta", "netflix", "unknown", "", func(r *http.Request) (*http.Response, error) {
			return serviceTestResponse(r, 200, `<meta property="al:android:app_name" content="Netflix"><h1>Breaking Bad</h1>`), nil
		}},
		{"netflix_wrong_title_id", "netflix", "unknown", "", func(r *http.Request) (*http.Response, error) {
			return serviceTestResponse(r, 200, strings.ReplaceAll(anonymousServiceFixture(r), "/title/81280792", "/title/000")), nil
		}},
		{"netflix_region_conflict", "netflix", "unknown", "", func(r *http.Request) (*http.Response, error) {
			body := anonymousServiceFixture(r)
			if strings.HasSuffix(r.URL.Path, "70143836") {
				body = strings.ReplaceAll(body, `"US"`, `"JP"`)
			}
			return serviceTestResponse(r, 200, body), nil
		}},
		{"netflix_hidden_h1", "netflix", "unknown", "", func(r *http.Request) (*http.Response, error) {
			return serviceTestResponse(r, 200, strings.ReplaceAll(anonymousServiceFixture(r), "<h1>", "<h1 hidden>")), nil
		}},
		{"chatgpt_web_positive_dc_unknown", "chatgpt", "partial", "web_verified_app_unknown", func(r *http.Request) (*http.Response, error) {
			if r.URL.Hostname() == "ios.chat.openai.com" {
				return serviceTestResponse(r, 403, `{"cf_details":"Request is not allowed. Please try again later.","type":"dc"}`), nil
			}
			return serviceTestResponse(r, 200, anonymousServiceFixture(r)), nil
		}},
		{"chatgpt_dc_without_positive", "chatgpt", "unknown", "", func(r *http.Request) (*http.Response, error) {
			return serviceTestResponse(r, 403, `{"cf_details":"Request is not allowed. Please try again later.","type":"dc"}`), nil
		}},
		{"chatgpt_web_only", "chatgpt", "partial", "web_only", func(r *http.Request) (*http.Response, error) {
			if r.URL.Hostname() == "api.openai.com" {
				return serviceTestResponse(r, 200, anonymousServiceFixture(r)), nil
			}
			return serviceTestResponse(r, 403, restrictedServiceFixture("chatgpt", r)), nil
		}},
		{"chatgpt_app_only", "chatgpt", "partial", "app_only", func(r *http.Request) (*http.Response, error) {
			if r.URL.Hostname() == "ios.chat.openai.com" {
				return serviceTestResponse(r, 200, anonymousServiceFixture(r)), nil
			}
			return serviceTestResponse(r, 403, restrictedServiceFixture("chatgpt", r)), nil
		}},
		{"chatgpt_app_web_unknown", "chatgpt", "partial", "app_verified_web_unknown", func(r *http.Request) (*http.Response, error) {
			if r.URL.Hostname() == "ios.chat.openai.com" {
				return serviceTestResponse(r, 200, anonymousServiceFixture(r)), nil
			}
			return serviceTestResponse(r, 200, `{}`), nil
		}},
		{"chatgpt_vpn_word_not_restriction", "chatgpt", "partial", "app_verified_web_unknown", func(r *http.Request) (*http.Response, error) {
			if r.URL.Hostname() == "ios.chat.openai.com" {
				return serviceTestResponse(r, 200, `<html><title>ChatGPT</title><body>Information about VPN</body></html>`), nil
			}
			return serviceTestResponse(r, 200, `{}`), nil
		}},
		{"youtube_missing_region", "youtube-premium", "unknown", "", func(r *http.Request) (*http.Response, error) {
			return serviceTestResponse(r, 200, `<html><body>ad-free</body><script>var ytInitialData = {};</script></html>`), nil
		}},
		{"youtube_context_region", "youtube-premium", "unlocked", "premium_offer", func(r *http.Request) (*http.Response, error) {
			return serviceTestResponse(r, 200, `<html><body>ad-free</body><script>ytcfg.set({"INNERTUBE_CONTEXT_GL":"jp"});</script></html>`), nil
		}},
		{"youtube_marketing_only", "youtube-premium", "unknown", "", func(r *http.Request) (*http.Response, error) {
			return serviceTestResponse(r, 200, `<html><body>ad-free</body><script>{"contentRegion":"US"}</script></html>`), nil
		}},
		{"prime_false", "amazon-prime-video", "unlocked", "territory_offer", func(r *http.Request) (*http.Response, error) {
			return serviceTestResponse(r, 200, anonymousServiceFixture(r)), nil
		}},
		{"prime_string_false", "amazon-prime-video", "unknown", "", func(r *http.Request) (*http.Response, error) {
			return serviceTestResponse(r, 200, `<title>Prime Video</title><script>{"currentTerritory":"US","isServiceRestricted":"false"}</script>`), nil
		}},
		{"prime_wrong_brand", "amazon-prime-video", "unknown", "", func(r *http.Request) (*http.Response, error) {
			return serviceTestResponse(r, 200, `<title>Other</title><script>{"currentTerritory":"US"}</script>`), nil
		}},
		{"tiktok_no_brand", "tiktok", "unknown", "", func(r *http.Request) (*http.Response, error) {
			return serviceTestResponse(r, 200, `<script>{"region":"US"}</script>`), nil
		}},
		{"tiktok_invalid_region", "tiktok", "unknown", "", func(r *http.Request) (*http.Response, error) {
			return serviceTestResponse(r, 200, `<title>TikTok</title><script>{"region":"UNKNOWN"}</script>`), nil
		}},
		{"reddit_country_without_component", "reddit", "unknown", "", func(r *http.Request) (*http.Response, error) {
			return serviceTestResponse(r, 200, `<div country="US">Reddit</div>`), nil
		}},
		{"reddit_hidden_component", "reddit", "unknown", "", func(r *http.Request) (*http.Response, error) {
			return serviceTestResponse(r, 200, `<div hidden><shreddit-app country="US"></shreddit-app></div>`), nil
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			outcome := serviceTestCollector(test.handler).probeService(context.Background(), test.service, 4)
			if outcome.Result.Status != test.wantStatus || outcome.Result.UnlockType != test.wantScope {
				t.Fatalf("result = %+v", outcome.Result)
			}
		})
	}
}

func TestTikTokExploreFallbackIsBounded(t *testing.T) {
	for _, challenge := range []bool{false, true} {
		t.Run(fmt.Sprint(challenge), func(t *testing.T) {
			var paths []string
			collector := serviceTestCollector(func(r *http.Request) (*http.Response, error) {
				paths = append(paths, r.URL.Path)
				if r.URL.Path == "/" {
					return serviceTestResponse(r, 200, `<title>TikTok</title><body>Please wait...</body>`), nil
				}
				if challenge {
					return serviceTestResponse(r, 200, `<title>TikTok</title><body>Just a moment...</body>`), nil
				}
				return serviceTestResponse(r, 200, anonymousServiceFixture(r)), nil
			})
			outcome := collector.probeService(context.Background(), "tiktok", 4)
			want := "unlocked"
			if challenge {
				want = "unknown"
			}
			if outcome.Result.Status != want || strings.Join(paths, ",") != "/,/explore" {
				t.Fatalf("result %+v requests %v", outcome.Result, paths)
			}
		})
	}
}

func TestDisneyAnonymousExchangeAndNoCredentialLeak(t *testing.T) {
	var paths []string
	collector := serviceTestCollector(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		if req.Method != http.MethodPost {
			t.Fatal("Disney method must be POST")
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		switch req.URL.Path {
		case "/devices":
			if req.Header.Get("Authorization") != "Bearer "+disneyBrowserClient || string(body) != `{"deviceFamily":"browser","applicationRuntime":"chrome","deviceProfile":"windows","attributes":{}}` {
				t.Fatalf("incorrect devices request %s", body)
			}
		case "/token":
			form, err := url.ParseQuery(string(body))
			if err != nil {
				t.Fatal(err)
			}
			want := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:token-exchange"}, "latitude": {"0"}, "longitude": {"0"}, "platform": {"browser"}, "subject_token": {"test-device-assertion"}, "subject_token_type": {"urn:bamtech:params:oauth:token-type:device"}}
			if form.Encode() != want.Encode() || req.Header.Get("Content-Type") != "application/x-www-form-urlencoded" || req.Header.Get("Authorization") != "Bearer "+disneyBrowserClient {
				t.Fatalf("incorrect token request %s", body)
			}
		case "/graph/v1/device/graphql":
			var value map[string]any
			if json.Unmarshal(body, &value) != nil {
				t.Fatal("bad graphql JSON")
			}
			variables := value["variables"].(map[string]any)
			input := variables["input"].(map[string]any)
			if input["refreshToken"] != "test-refresh-token" || value["query"] != "mutation refreshToken($input: RefreshTokenInput!) { refreshToken(refreshToken: $input) { activeSession { sessionId } } }" || req.Header.Get("Authorization") != disneyBrowserClient {
				t.Fatalf("incorrect graphql request %s", body)
			}
		default:
			t.Fatalf("unexpected endpoint %s", req.URL.Path)
		}
		response := serviceTestResponse(req, 200, anonymousServiceFixture(req))
		response.Header.Set("Set-Cookie", "private-cookie=value")
		return response, nil
	})
	outcome := collector.probeService(context.Background(), "disney-plus", 6)
	if outcome.Result.Status != "unlocked" || len(paths) != 3 {
		t.Fatalf("result %+v paths %v", outcome.Result, paths)
	}
	payload, _ := json.Marshal(outcome)
	for _, forbidden := range []string{"test-device-assertion", "test-refresh-token", "private-cookie", disneyBrowserClient, "sessionId", "Authorization", "Set-Cookie"} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatalf("credential leak: %s", payload)
		}
	}
}

func TestDisneyEarlyAbortAndGraphBoundaries(t *testing.T) {
	cases := []struct {
		name, path, body, want string
		status, calls          int
	}{
		{"missing_assertion", "/devices", `{}`, "unknown", 200, 1},
		{"empty_assertion", "/devices", `{"assertion":" "}`, "unknown", 200, 1},
		{"missing_token", "/token", `{}`, "unknown", 200, 2},
		{"empty_token", "/token", `{"refresh_token":""}`, "unknown", 200, 2},
		{"token_denied", "/token", `{"error":"invalid_client"}`, "unknown", 401, 2},
		{"token_location_denied", "/token", `{"error_description":"forbidden-location"}`, "blocked", 403, 2},
		{"graph_errors", "/graph/v1/device/graphql", `{"errors":[{"message":"secret failure"}]}`, "unknown", 200, 3},
		{"graph_missing_bool", "/graph/v1/device/graphql", `{"extensions":{"sdk":{"session":{"location":{"countryCode":"US"}}}}}`, "unknown", 200, 3},
		{"graph_invalid_country", "/graph/v1/device/graphql", `{"extensions":{"sdk":{"session":{"location":{"countryCode":"UNKNOWN"},"inSupportedLocation":true}}}}`, "unknown", 200, 3},
		{"graph_unsupported", "/graph/v1/device/graphql", `{"extensions":{"sdk":{"session":{"location":{"countryCode":"JP"},"inSupportedLocation":false}}}}`, "blocked", 200, 3},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			collector := serviceTestCollector(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Path == test.path {
					return serviceTestResponse(r, test.status, test.body), nil
				}
				return serviceTestResponse(r, 200, anonymousServiceFixture(r)), nil
			})
			outcome := collector.probeService(context.Background(), "disney-plus", 4)
			if calls != test.calls || outcome.Result.Status != test.want {
				t.Fatalf("calls=%d result=%+v", calls, outcome.Result)
			}
		})
	}
}

func TestServiceFamilyClientDispatch(t *testing.T) {
	collector := serviceTestCollector(func(r *http.Request) (*http.Response, error) { t.Fatal("fallback client used"); return nil, nil })
	var calls4, calls6 int
	collector.serviceClient4 = &http.Client{Transport: serviceTestTransport(func(r *http.Request) (*http.Response, error) {
		calls4++
		return serviceTestResponse(r, 200, anonymousServiceFixture(r)), nil
	})}
	collector.serviceClient6 = &http.Client{Transport: serviceTestTransport(func(r *http.Request) (*http.Response, error) {
		calls6++
		return serviceTestResponse(r, 200, anonymousServiceFixture(r)), nil
	})}
	for _, family := range []int{6, 4} {
		result := collector.probeService(context.Background(), "reddit", family)
		if result.Result.Status != "unlocked" {
			t.Fatalf("family %d result %+v", family, result.Result)
		}
	}
	if calls4 != 1 || calls6 != 1 {
		t.Fatalf("dispatch calls v4=%d v6=%d", calls4, calls6)
	}
}

type serviceBarrierBody struct {
	ctx     context.Context
	entered chan<- struct{}
	closed  chan<- struct{}
}

func (b *serviceBarrierBody) Read(_ []byte) (int, error) {
	b.entered <- struct{}{}
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}
func (b *serviceBarrierBody) Close() error { b.closed <- struct{}{}; return nil }
func TestServiceWorkersJoinCanceledBodyReads(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{}, 7)
	closed := make(chan struct{}, 7)
	collector := serviceTestCollector(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: &serviceBarrierBody{ctx: r.Context(), entered: entered, closed: closed}, Request: r}, nil
	})
	done := make(chan []serviceProbeOutcome, 1)
	go func() { done <- collector.collectDefaultServiceUnlocks(ctx, anonymousTestServices, 6) }()
	for range 7 {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("worker failed to enter body read")
		}
	}
	cancel()
	var outcomes []serviceProbeOutcome
	select {
	case outcomes = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("workers did not join")
	}
	if len(closed) != 7 {
		t.Fatalf("returned before closing all bodies: %d", len(closed))
	}
	for _, outcome := range outcomes {
		if outcome.Result.Status != "unknown" || outcome.Result.ProbeStatus != sourceStatusFailure {
			t.Fatalf("cancel produced facts: %+v", outcome.Result)
		}
	}
	before, _ := json.Marshal(outcomes)
	for range 7 {
		<-closed
	}
	after, _ := json.Marshal(outcomes)
	if string(before) != string(after) {
		t.Fatal("outcomes mutated after return")
	}
}

func TestServiceWorkersDoNotSerializeIndependentRequests(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan string, 7)
	release := make(chan struct{})
	collector := serviceTestCollector(func(r *http.Request) (*http.Response, error) {
		entered <- r.URL.Hostname()
		select {
		case <-release:
			return serviceTestResponse(r, 200, anonymousServiceFixture(r)), nil
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	})
	done := make(chan []serviceProbeOutcome, 1)
	go func() { done <- collector.collectDefaultServiceUnlocks(ctx, anonymousTestServices, 4) }()
	for range 7 {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("requests serialized")
		}
	}
	close(release)
	select {
	case outcomes := <-done:
		for _, outcome := range outcomes {
			if outcome.Result.Status != "unlocked" {
				t.Fatalf("result %+v", outcome.Result)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("workers did not finish")
	}
}

func TestServiceUnsupportedEmptyAndPreCanceledHaveNoIO(t *testing.T) {
	collector := serviceTestCollector(func(r *http.Request) (*http.Response, error) { t.Fatal("unexpected IO"); return nil, nil })
	if result := collector.collectDefaultServiceUnlocks(context.Background(), nil, 4); len(result) != 0 {
		t.Fatal(result)
	}
	if result := collector.probeService(context.Background(), "unsupported", 4); result.Result.ErrorCode != "unsupported_service" {
		t.Fatal(result)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, service := range anonymousTestServices {
		if result := collector.probeService(ctx, service, 4); result.Result.ProbeStatus != sourceStatusFailure {
			t.Fatal(result)
		}
	}
}

func TestHTMLScriptTextDoesNotProvideVisibleEvidence(t *testing.T) {
	body := `<html><title>TikTok</title><script>{"region":"u\u0073","marketing":"not available in your country captcha"}</script><style>.x {content:"Verify you are human"}</style></html>`
	page := parseServicePage([]byte(body))
	region, ok := serviceRegion(page.objects, "region")
	if !ok || region != "US" || page.challenge || strings.Contains(page.text, "not available") {
		t.Fatalf("page %+v region %s", page, region)
	}
}

// Concurrent calls must not mutate the shared injected client's configuration.
func TestAnonymousServiceClientPolicyIsRequestLocal(t *testing.T) {
	client := &http.Client{Transport: serviceTestTransport(func(r *http.Request) (*http.Response, error) {
		return serviceTestResponse(r, 200, anonymousServiceFixture(r)), nil
	})}
	originalRedirect := func(*http.Request, []*http.Request) error { return fmt.Errorf("original policy") }
	client.CheckRedirect = originalRedirect
	collector := NewHTTPCollector(HTTPCollectorOptions{Client: client})
	var workers sync.WaitGroup
	workers.Add(2)
	for range 2 {
		go func() { defer workers.Done(); collector.probeService(context.Background(), "reddit", 4) }()
	}
	workers.Wait()
	if client.CheckRedirect == nil || client.CheckRedirect(nil, nil).Error() != "original policy" {
		t.Fatal("shared policy mutated")
	}
}

func TestCountryOnlyAndChallengeOverridesAreUnknown(t *testing.T) {
	for _, service := range anonymousTestServices {
		for _, challenge := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/challenge=%t", service, challenge), func(t *testing.T) {
				collector := serviceTestCollector(func(r *http.Request) (*http.Response, error) {
					body := `{"country":"US","region":"US"}`
					if challenge {
						body = anonymousServiceFixture(r) + `<div class="hcaptcha">Verify you are human</div>`
					}
					return serviceTestResponse(r, 200, body), nil
				})
				result := collector.probeService(context.Background(), service, 4)
				if result.Result.Status != "unknown" {
					t.Fatalf("result %+v", result.Result)
				}
			})
		}
	}
}

func TestServiceWorkersShareDeadlineAcrossProtocolSteps(t *testing.T) {
	for _, service := range []string{"netflix", "chatgpt", "disney-plus", "tiktok"} {
		t.Run(service, func(t *testing.T) {
			var deadline time.Time
			calls := 0
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			collector := serviceTestCollector(func(r *http.Request) (*http.Response, error) {
				current, ok := r.Context().Deadline()
				if !ok {
					t.Fatal("request has no deadline")
				}
				if calls == 0 {
					deadline = current
				} else if !deadline.Equal(current) {
					t.Fatal("protocol step reset deadline")
				}
				parent, _ := ctx.Deadline()
				if !current.Equal(parent) {
					t.Fatal("service exceeded parent deadline")
				}
				calls++
				body := anonymousServiceFixture(r)
				if service == "tiktok" && r.URL.Path == "/" {
					body = `<title>TikTok</title><body>Please wait...</body>`
				}
				return serviceTestResponse(r, 200, body), nil
			})
			results := collector.collectDefaultServiceUnlocks(ctx, []string{service}, 6)
			if len(results) != 1 || results[0].Result.Status != "unlocked" || calls < 2 {
				t.Fatalf("results %+v calls %d", results, calls)
			}
		})
	}
}

func TestAnonymousProbeEgressAmbiguityHasNoFacts(t *testing.T) {
	results := ambiguousServiceOutcomes(anonymousTestServices, 6)
	for _, outcome := range results {
		if outcome.Result.ErrorCode != "egress_ambiguous" || outcome.Result.ErrorSummary != "service egress is ambiguous" || outcome.Result.Status != "unknown" || outcome.Result.ProbeStatus != sourceStatusFailure || outcome.Result.Region != "" || outcome.Result.UnlockType != "" {
			t.Fatalf("result %+v", outcome.Result)
		}
		var evidence serviceEvidence
		if json.Unmarshal(outcome.Raw, &evidence) != nil || evidence.ProbeRevision != 3 || evidence.IPVersion != 6 || evidence.ObservedRegion != "" || len(evidence.Endpoints) != 0 {
			t.Fatalf("ambiguous egress leaked service evidence: %s", outcome.Raw)
		}
	}
}

func TestServiceScriptQuotedAndCommentedLookalikesAreUnknown(t *testing.T) {
	wrappers := []struct {
		name string
		wrap func(string) string
	}{
		{"single_quoted", func(payload string) string { return "console.log('" + payload + "');" }},
		{"double_quoted", func(payload string) string { return fmt.Sprintf("console.log(%q);", payload) }},
		{"single_quoted_escape", func(payload string) string { return `console.log('prefix \' ` + payload + `');` }},
		{"template_string", func(payload string) string { return "console.log(`" + payload + "`);" }},
		{"template_interpolation", func(payload string) string { return "console.log(`${" + payload + "}`);" }},
		{"line_comment", func(payload string) string { return "// " + payload + "\nconsole.log('done');" }},
		{"block_comment", func(payload string) string { return "/* " + payload + " */ console.log('done');" }},
		{"unterminated_block_comment", func(payload string) string { return "/* " + payload }},
		{"regex_after_unary", func(payload string) string { return "const value = !/" + payload + "/.test('x');" }},
		{"regex_after_logical", func(payload string) string { return "const value = enabled && /" + payload + "/;" }},
	}
	for _, service := range []string{"amazon-prime-video", "tiktok"} {
		title, payload := "Prime Video", `{"isServiceRestricted":true}`
		if service == "tiktok" {
			title, payload = "TikTok", `{"region":"US"}`
		}
		for _, wrapper := range wrappers {
			t.Run(service+"/"+wrapper.name, func(t *testing.T) {
				collector := serviceTestCollector(func(req *http.Request) (*http.Response, error) {
					return serviceTestResponse(req, 200, "<title>"+title+"</title><script>"+wrapper.wrap(payload)+"</script>"), nil
				})
				result := collector.probeService(context.Background(), service, 4)
				if result.Result.Status != "unknown" || result.Result.ProbeStatus != sourceStatusFailure || result.Result.Region != "" || result.Result.UnlockType != "" {
					t.Fatalf("lookalike provided business evidence: %+v", result.Result)
				}
			})
		}
	}
}

func TestServiceScriptActualJSONSurvivesWrapperLexing(t *testing.T) {
	cases := []struct{ service, title, script, status, region string }{
		{"amazon-prime-video", "Prime Video", `{"isServiceRestricted":true}`, "blocked", ""},
		{"amazon-prime-video", "Prime Video", `window.state = {"currentTerritory":"US","isServiceRestricted":false};`, "unlocked", "US"},
		{"tiktok", "TikTok", `{"region":"u\u0073","note":"escaped \" brace { and slash /"}`, "unlocked", "US"},
		{"tiktok", "TikTok", `window.state = {"region":"US"};`, "unlocked", "US"},
		{"youtube-premium", "YouTube", `ytcfg.set({"INNERTUBE_CONTEXT_GL":"US"});`, "unlocked", "US"},
	}
	prefixes := []string{
		"",
		`console.log('{"isServiceRestricted":true,"region":"JP"}');`,
		"// {\"isServiceRestricted\":true,\"region\":\"JP\"}\n",
		`/* {"isServiceRestricted":true,"region":"JP"} */`,
		"console.log(`{\"isServiceRestricted\":true,\"region\":\"JP\"}`);",
		`const ratio = 8 / 2;`,
		`const ratio = numerator / denominator;`,
		`const ratio = (numerator / denominator) / scale;`,
		`const ratio = value() / 2;`,
		`const ratio = "8" / 2;`,
		`const ratio = numerator / /* units */ denominator;`,
		`const ratio = numerator++ / denominator;`,
		`const ratio = numerator-- / denominator;`,
		`const ratio = source.return / denominator;`,
		`const ratio = source?.return / denominator;`,
		`const ratio = source.returnValue / denominator;`,
		`const ratio = numerator / /"/.source.length;`,
		`const quote = /"/;`,
		`const quote = /\"/;`,
		`const quote = /\"\/"/;`,
		`const quote = /["/]/;`,
		`const quote = /[\"\/]/;`,
		`const pattern = /{"isServiceRestricted":true,"region":"JP"}/;`,
		`const quote = /* quote matcher */ /"/;`,
		`const noQuote = !/"/.test('x');`,
		`const quote = enabled && /"/;`,
		`const quote = enabled || /"/;`,
		`const quote = enabled ?? /"/;`,
		`const quote = +/"/.test('x');`,
		`const quote = -/"/.test('x');`,
		`const quote = ~/"/.test('x');`,
		`const quote = typeof /"/;`,
		`const quote = void /"/;`,
		`const quote = value instanceof /"/;`,
		`const quote = !/* matcher */ /{"isServiceRestricted":true,"region":"JP"}/;`,
		`function matcher() { return /"/; }`,
		`function matcher() { throw /"/; }`,
		`function* matcher() { yield /"/; }`,
		`async function matcher() { await /"/; }`,
		`const patterns = [/"/, /{"region":"JP"}/];`,
		`const helpers = { matcher: /"/ };`,
		`const ratio = source. /* field */ return / denominator;`,
	}
	for index, prefix := range prefixes {
		for _, test := range cases {
			t.Run(fmt.Sprintf("%s/prefix%d/%s", test.service, index, test.status), func(t *testing.T) {
				collector := serviceTestCollector(func(req *http.Request) (*http.Response, error) {
					return serviceTestResponse(req, 200, "<title>"+test.title+"</title><body>ad-free</body><script>"+prefix+test.script+"</script>"), nil
				})
				result := collector.probeService(context.Background(), test.service, 4)
				if result.Result.Status != test.status || result.Result.ProbeStatus != sourceStatusSuccess || result.Result.Region != test.region {
					t.Fatalf("actual JSON lost or contaminated: %+v", result.Result)
				}
			})
		}
	}
}

func TestRegexQuoteDoesNotHideLaterServiceJSON(t *testing.T) {
	cases := []struct{ service, body string }{
		{"tiktok", `<title>TikTok</title><script>const quote = /"/; window.state = {"region":"US"};</script>`},
		{"amazon-prime-video", `<title>Prime Video</title><script>const quote = /"/; window.state = {"currentTerritory":"US","isServiceRestricted":false};</script>`},
	}
	for _, test := range cases {
		t.Run(test.service, func(t *testing.T) {
			collector := serviceTestCollector(func(req *http.Request) (*http.Response, error) {
				return serviceTestResponse(req, 200, test.body), nil
			})
			result := collector.probeService(context.Background(), test.service, 4)
			if result.Result.Status != "unlocked" || result.Result.ProbeStatus != sourceStatusSuccess || result.Result.Region != "US" {
				t.Fatalf("regex quote hid genuine service JSON: %+v", result.Result)
			}
		})
	}
}

func TestYouTubePageRegionDoesNotEstablishPremiumAvailability(t *testing.T) {
	tests := []struct {
		name, body, status, code, region, observed, evidence string
		httpStatus                                           int
	}{
		{"cn_no_offer", `<body>Welcome to YouTube</body><script>var ytInitialData = {"contentRegion":"cn"};</script>`, "unknown", "invalid_response", "", "CN", "youtube_content_region", 200},
		{"cn_offer", `<body>ad-free</body><script>var ytInitialData = {"contentRegion":"CN"};</script>`, "unlocked", "", "CN", "CN", "youtube_content_region", 200},
		{"context_no_offer", `<script>ytcfg.set({"INNERTUBE_CONTEXT_GL":"jp"});</script>`, "unknown", "invalid_response", "", "JP", "youtube_context_gl", 200},
		{"refusal_with_region", `<body>Premium is not available in your country</body><script>var ytInitialData = {"contentRegion":"CN"};</script>`, "blocked", "", "CN", "CN", "youtube_content_region", 200},
		{"refusal_without_region", `<body>Premium is not available in your country</body><script>var ytInitialData = {};</script>`, "blocked", "", "", "", "", 200},
		{"403_refusal_without_region", `<body>Premium is not available in your country</body><script>var ytInitialData = {};</script>`, "blocked", "", "", "", "", 403},
		{"403_refusal_with_untrusted_region", `<body>Premium is not available in your country</body><script>var ytInitialData = {"contentRegion":"CN"};</script>`, "blocked", "", "", "", "", 403},
		{"conflict_no_fallback", `<body>ad-free</body><script>var ytInitialData = {"contentRegion":"CN"}; ytcfg.set({"contentRegion":"US","INNERTUBE_CONTEXT_GL":"JP"});</script>`, "unknown", "invalid_response", "", "", "", 200},
		{"invalid_primary_no_fallback", `<body>ad-free</body><script>ytcfg.set({"contentRegion":"China","INNERTUBE_CONTEXT_GL":"US"});</script>`, "unknown", "invalid_response", "", "", "", 200},
		{"refusal_survives_conflict", `<body>Premium is not available in your country</body><script>var ytInitialData = {"contentRegion":"CN"}; ytcfg.set({"contentRegion":"US"});</script>`, "blocked", "", "", "", "", 200},
		{"challenge", `<body>Verify you are human; ad-free</body><script>var ytInitialData = {"contentRegion":"CN"};</script>`, "unknown", "challenge_detected", "", "", "", 200},
		{"login", `<form action="/login"><input type="password"></form><body>ad-free</body><script>ytcfg.set({"contentRegion":"CN"});</script>`, "unknown", "invalid_response", "", "", "", 200},
		{"malformed_config", `<body>Premium is not available in your country</body><script>var ytInitialData = {"contentRegion":;</script>`, "unknown", "invalid_response", "", "", "", 200},
		{"unrelated_script_region", `<body>ad-free</body><script>ytcfg.set({});</script><script>{"contentRegion":"CN"}</script>`, "unknown", "invalid_response", "", "", "", 200},
		{"same_script_unrelated_region", `<body>ad-free</body><script>ytcfg.set({}); const unrelated = {"contentRegion":"CN"};</script>`, "unknown", "invalid_response", "", "", "", 200},
		{"same_script_unrelated_does_not_conflict", `<body>ad-free</body><script>ytcfg.set({"contentRegion":"US"}); const unrelated = {"contentRegion":"CN"};</script>`, "unlocked", "", "US", "US", "youtube_content_region", 200},
		{"comment_marker", `<body>ad-free</body><script>/* ytcfg.set */ const unrelated = {"contentRegion":"CN"};</script>`, "unknown", "invalid_response", "", "", "", 200},
		{"string_marker", `<body>ad-free</body><script>const marker = "ytInitialData"; const unrelated = {"contentRegion":"CN"};</script>`, "unknown", "invalid_response", "", "", "", 200},
		{"regex_marker", `<body>ad-free</body><script>const marker = /ytcfg/; const unrelated = {"contentRegion":"CN"};</script>`, "unknown", "invalid_response", "", "", "", 200},
		{"different_receiver", `<body>ad-free</body><script>unrelated.ytcfg.set({"contentRegion":"CN"});</script>`, "unknown", "invalid_response", "", "", "", 200},
		{"unrelated_property", `<body>ad-free</body><script>unrelated.ytInitialData = {"contentRegion":"CN"};</script>`, "unknown", "invalid_response", "", "", "", 200},
		{"comment_between_wrapper_tokens", `<body>ad-free</body><script>ytcfg /* config */ . set ( {"contentRegion":"US"} );</script>`, "unlocked", "", "US", "US", "youtube_content_region", 200},
		{"window_assignment", `<body>ad-free</body><script>window.ytInitialData = {"contentRegion":"US"};</script>`, "unlocked", "", "US", "US", "youtube_content_region", 200},
		{"fatal_refusal", `<body>Premium is not available in your country</body><script>ytcfg.set({"contentRegion":"CN"});</script>`, "unknown", "http_status", "", "", "", 503},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			collector := serviceTestCollector(func(req *http.Request) (*http.Response, error) {
				return serviceTestResponse(req, test.httpStatus, test.body), nil
			})
			outcome := collector.probeService(context.Background(), "youtube-premium", 6)
			if outcome.Result.Status != test.status || outcome.Result.ErrorCode != test.code || outcome.Result.Region != test.region {
				t.Fatalf("business conclusion = %+v", outcome.Result)
			}
			if test.status == "unknown" && outcome.Result.UnlockType != "" {
				t.Fatalf("unknown retained normalized scope: %+v", outcome.Result)
			}
			var evidence serviceEvidence
			if err := json.Unmarshal(outcome.Raw, &evidence); err != nil {
				t.Fatal(err)
			}
			if evidence.ProbeRevision != 3 || evidence.IPVersion != 6 || evidence.ObservedRegion != test.observed || evidence.RegionEvidence != test.evidence || evidence.Region != test.region {
				t.Fatalf("page evidence = %+v", evidence)
			}
			if len(evidence.Endpoints) != 1 || evidence.Endpoints[0].HTTPStatus != test.httpStatus {
				t.Fatalf("endpoint evidence = %+v", evidence.Endpoints)
			}
			endpoint := evidence.Endpoints[0]
			wantOutcome := "unknown"
			if test.status == "unlocked" {
				wantOutcome = "available"
			} else if test.status == "blocked" {
				wantOutcome = "restricted"
			}
			if endpoint.Outcome != wantOutcome || endpoint.ErrorCode != test.code {
				t.Fatalf("endpoint conclusion = %+v", endpoint)
			}
		})
	}
}

func TestChatGPTBooleanConsentAndIndependentEndpointFailures(t *testing.T) {
	for _, consent := range []bool{false, true} {
		t.Run(fmt.Sprint(consent), func(t *testing.T) {
			collector := serviceTestCollector(func(req *http.Request) (*http.Response, error) {
				if req.URL.Hostname() == "ios.chat.openai.com" {
					return nil, &net.OpError{Op: "dial", Net: "tcp6", Err: syscall.ENETUNREACH}
				}
				return serviceTestResponse(req, 200, fmt.Sprintf(`{"cookie_consent_required":%t,"private":"response-secret"}`, consent)), nil
			})
			outcome := collector.probeService(context.Background(), "chatgpt", 6)
			if outcome.Result.Status != "partial" || outcome.Result.ProbeStatus != sourceStatusSuccess || outcome.Result.UnlockType != "web_verified_app_unknown" || outcome.Result.Region != "" {
				t.Fatalf("endpoint failure obscured verified Web result: %+v", outcome.Result)
			}
			var evidence serviceEvidence
			if err := json.Unmarshal(outcome.Raw, &evidence); err != nil {
				t.Fatal(err)
			}
			if len(evidence.Endpoints) != 2 {
				t.Fatalf("endpoints = %+v", evidence.Endpoints)
			}
			web, app := evidence.Endpoints[0], evidence.Endpoints[1]
			if web.Endpoint != "web_cookie_requirements" || web.Outcome != "available" || web.HTTPStatus != 200 || web.ErrorCode != "" || web.MatchedRule == "" {
				t.Fatalf("Web evidence = %+v", web)
			}
			if app.Endpoint != "app_home" || app.Outcome != "unknown" || app.ErrorCode != "network_unreachable" || app.HTTPStatus != 0 || app.MatchedRule != "" {
				t.Fatalf("App evidence = %+v", app)
			}
			if strings.Contains(string(outcome.Raw), "response-secret") || strings.Contains(string(outcome.Raw), "https://") {
				t.Fatalf("unsafe endpoint evidence: %s", outcome.Raw)
			}
		})
	}
}

func TestServiceTransportDiagnosticsNeverPersistErrorText(t *testing.T) {
	tests := []struct {
		name, code string
		err        error
	}{
		{"dns", "dns_failed", &net.DNSError{Err: "secret-dns-message", Name: "secret.example"}},
		{"refused", "connection_refused", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}},
		{"deadline", "timeout", context.DeadlineExceeded},
		{"unknown", "request_failed", fmt.Errorf("secret-token http status 403 timeout non_json_response")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			collector := serviceTestCollector(func(req *http.Request) (*http.Response, error) {
				return nil, test.err
			})
			outcome := collector.probeService(context.Background(), "reddit", 4)
			if outcome.Result.Status != "unknown" || outcome.Result.ErrorCode != test.code || outcome.Result.Region != "" || outcome.Result.UnlockType != "" {
				t.Fatalf("transport error established service facts: %+v", outcome.Result)
			}
			var evidence serviceEvidence
			if err := json.Unmarshal(outcome.Raw, &evidence); err != nil {
				t.Fatal(err)
			}
			if len(evidence.Endpoints) != 1 || evidence.Endpoints[0].ErrorCode != test.code || evidence.Endpoints[0].Outcome != "unknown" {
				t.Fatalf("missing safe transport classification: %+v", evidence)
			}
			payload, _ := json.Marshal(outcome)
			for _, forbidden := range []string{"secret", "https://", "secret.example"} {
				if strings.Contains(string(payload), forbidden) {
					t.Fatalf("persisted transport detail: %s", payload)
				}
			}
		})
	}
}

func TestServiceSharedSemaphoreHonorsCancellationBeforeTransport(t *testing.T) {
	slots := make(chan struct{}, 1)
	slots <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	collector := serviceTestCollector(func(req *http.Request) (*http.Response, error) {
		t.Error("request escaped occupied shared service pool")
		return serviceTestResponse(req, 200, anonymousServiceFixture(req)), nil
	})
	done := make(chan []serviceProbeOutcome, 1)
	go func() {
		done <- collector.collectDefaultServiceUnlocksWithSemaphore(ctx, []string{"reddit", "youtube-premium"}, 6, slots)
	}()
	cancel()
	select {
	case outcomes := <-done:
		for _, outcome := range outcomes {
			if outcome.Result.Status != "unknown" || outcome.Result.ProbeStatus != sourceStatusFailure {
				t.Fatalf("canceled waiter produced service facts: %+v", outcome.Result)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled service waiters did not join")
	}
	if len(slots) != 1 {
		t.Fatal("canceled waiter released a slot it did not own")
	}
}

func TestServiceSemaphoreIsSharedAcrossFamilyCollectors(t *testing.T) {
	slots := make(chan struct{}, 1)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	ctx4, cancel4 := context.WithCancel(context.Background())
	defer cancel4()
	collector := serviceTestCollector(func(req *http.Request) (*http.Response, error) {
		select {
		case entered <- struct{}{}:
		default:
			t.Error("second family bypassed occupied round-wide service slot")
		}
		select {
		case <-release:
			return serviceTestResponse(req, 200, anonymousServiceFixture(req)), nil
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	})
	done4 := make(chan []serviceProbeOutcome, 1)
	go func() {
		done4 <- collector.collectDefaultServiceUnlocksWithSemaphore(ctx4, []string{"reddit"}, 4, slots)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first family did not acquire slot")
	}
	ctx6, cancel6 := context.WithCancel(context.Background())
	defer cancel6()
	done6 := make(chan []serviceProbeOutcome, 1)
	go func() {
		done6 <- collector.collectDefaultServiceUnlocksWithSemaphore(ctx6, []string{"reddit"}, 6, slots)
	}()
	select {
	case <-entered:
		t.Fatal("second family bypassed occupied round-wide service slot")
	case <-time.After(50 * time.Millisecond):
	}
	cancel6()
	select {
	case outcomes := <-done6:
		if len(outcomes) != 1 || outcomes[0].Result.Status != "unknown" || outcomes[0].Result.ProbeStatus != sourceStatusFailure {
			t.Fatalf("waiting family produced facts: %+v", outcomes)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiting family failed to cancel")
	}
	if len(slots) != 1 || len(entered) != 0 {
		t.Fatal("waiting family disturbed active family slot or started transport")
	}
	close(release)
	select {
	case outcomes := <-done4:
		if len(outcomes) != 1 || outcomes[0].Result.Status != "unlocked" || len(slots) != 0 {
			t.Fatalf("active family did not finish independently: %+v", outcomes)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("active family failed to join")
	}
}

func TestYouTubeConsentAndTransportFailuresHaveNoPageRegion(t *testing.T) {
	for _, failure := range []string{"consent", "transport"} {
		t.Run(failure, func(t *testing.T) {
			collector := serviceTestCollector(func(req *http.Request) (*http.Response, error) {
				if failure == "transport" {
					return nil, &net.DNSError{Err: "private-dns-detail", Name: "private.example"}
				}
				response := serviceTestResponse(req, 200, `<body>ad-free</body><script>ytcfg.set({"contentRegion":"CN"});</script>`)
				finalRequest := req.Clone(req.Context())
				finalRequest.URL, _ = url.Parse("https://consent.youtube.com/?token=private-token")
				response.Request = finalRequest
				return response, nil
			})
			outcome := collector.probeService(context.Background(), "youtube-premium", 4)
			wantCode := "invalid_response"
			if failure == "transport" {
				wantCode = "dns_failed"
			}
			var evidence serviceEvidence
			if err := json.Unmarshal(outcome.Raw, &evidence); err != nil {
				t.Fatal(err)
			}
			if outcome.Result.Status != "unknown" || outcome.Result.ErrorCode != wantCode || outcome.Result.Region != "" || outcome.Result.UnlockType != "" || evidence.ObservedRegion != "" || evidence.RegionEvidence != "" {
				t.Fatalf("untrusted response established page geography: %+v evidence %+v", outcome.Result, evidence)
			}
			if len(evidence.Endpoints) != 1 || evidence.Endpoints[0].Outcome != "unknown" || evidence.Endpoints[0].ErrorCode != wantCode {
				t.Fatalf("failure evidence = %+v", evidence.Endpoints)
			}
			if strings.Contains(string(outcome.Raw), "private") || strings.Contains(string(outcome.Raw), "consent.youtube.com") {
				t.Fatalf("persisted unsafe redirect/transport detail: %s", outcome.Raw)
			}
		})
	}
}
