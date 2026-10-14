package ipquality

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestServiceHTTPBodyLimitAndDecompression(t *testing.T) {
	for _, compressed := range []bool{false, true} {
		for _, size := range []int{serviceBodyLimit, serviceBodyLimit + 1} {
			name := "plain"
			if compressed {
				name = "gzip"
			}
			if size > serviceBodyLimit {
				name += "/exceeds"
			} else {
				name += "/exact"
			}
			t.Run(name, func(t *testing.T) {
				body := bytes.Repeat([]byte("a"), size)
				if compressed {
					var buffer bytes.Buffer
					writer := gzip.NewWriter(&buffer)
					if _, err := writer.Write(body); err != nil {
						t.Fatal(err)
					}
					if err := writer.Close(); err != nil {
						t.Fatal(err)
					}
					body = buffer.Bytes()
				}
				closed := false
				collector := serviceTestCollector(func(r *http.Request) (*http.Response, error) {
					response := serviceTestResponse(r, 200, "")
					response.Body = &serviceTestCloseBody{Reader: bytes.NewReader(body), closed: &closed}
					if compressed {
						response.Header.Set("Content-Encoding", "gzip")
					}
					return response, nil
				})
				response, err := collector.doServiceRequest(context.Background(), 4, "reddit", http.MethodGet, "https://www.reddit.com/svc/shreddit/reddit-chat", nil, nil)
				if size > serviceBodyLimit {
					if !errors.Is(err, errServiceBodyLimit) || response.Body != nil {
						t.Fatalf("oversize result err=%v body=%d", err, len(response.Body))
					}
				} else {
					if err != nil || len(response.Body) != size {
						t.Fatalf("exact limit err=%v body=%d", err, len(response.Body))
					}
				}
				if !closed {
					t.Fatal("body not closed")
				}
			})
		}
	}
}

type serviceTestCloseBody struct {
	io.Reader
	closed *bool
}

func (b *serviceTestCloseBody) Close() error { *b.closed = true; return nil }

func TestServiceHTTPReadFailuresDoNotBecomeSuccess(t *testing.T) {
	collector := serviceTestCollector(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(serviceTestErrorReader{}), Request: r}, nil
	})
	outcome := collector.probeService(context.Background(), "reddit", 4)
	if outcome.Result.Status != "unknown" || outcome.Result.ErrorSummary != "service request failed" {
		t.Fatalf("result %+v", outcome.Result)
	}
}

type serviceTestErrorReader struct{}

func (serviceTestErrorReader) Read([]byte) (int, error) {
	return 0, errors.New("private response body read failure")
}

func TestServiceHTTPRedirectPolicy(t *testing.T) {
	cases := []struct {
		name, service, initial, target string
		allowed                        bool
	}{
		{"netflix_official", "netflix", "https://www.netflix.com/title/81280792", "https://netflix.com/title/81280792", true},
		{"chatgpt_official", "chatgpt", "https://api.openai.com/compliance/cookie_requirements", "https://ios.chat.openai.com/", true},
		{"youtube_consent", "youtube-premium", "https://www.youtube.com/premium", "https://consent.google.com/", true},
		{"prime_official", "amazon-prime-video", "https://www.primevideo.com/", "https://primevideo.com/", true},
		{"tiktok_official", "tiktok", "https://www.tiktok.com/", "https://tiktok.com/", true},
		{"reddit_official", "reddit", "https://www.reddit.com/svc/shreddit/reddit-chat", "https://reddit.com/svc/shreddit/reddit-chat", true},
		{"external_host", "reddit", "https://www.reddit.com/", "https://attacker.invalid/", false},
		{"suffix_host", "reddit", "https://www.reddit.com/", "https://www.reddit.com.attacker.invalid/", false},
		{"downgrade", "reddit", "https://www.reddit.com/", "http://www.reddit.com/", false},
		{"userinfo", "reddit", "https://www.reddit.com/", "https://user@www.reddit.com/", false},
		{"alternate_port", "reddit", "https://www.reddit.com/", "https://www.reddit.com:8443/", false},
		{"disney_post_never_redirects", "disney-plus", "https://disney.api.edge.bamgrid.com/devices", "https://disney.api.edge.bamgrid.com/token", false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			collector := serviceTestCollector(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					response := serviceTestResponse(r, 302, "")
					response.Header.Set("Location", test.target)
					return response, nil
				}
				if r.Header.Get("Authorization") != "" {
					t.Fatal("authorization forwarded across hosts")
				}
				return serviceTestResponse(r, 200, "final"), nil
			})
			method := http.MethodGet
			if test.service == "disney-plus" {
				method = http.MethodPost
			}
			response, err := collector.doServiceRequest(context.Background(), 4, test.service, method, test.initial, http.Header{"Authorization": {"private-test-header"}}, nil)
			if test.allowed {
				if err != nil || calls != 2 || string(response.Body) != "final" {
					t.Fatalf("calls %d response %+v err %v", calls, response, err)
				}
			} else {
				if !errors.Is(err, errServiceRedirect) || calls != 1 {
					t.Fatalf("calls %d err %v", calls, err)
				}
			}
		})
	}
}

func TestServiceHTTPRedirectCountLimit(t *testing.T) {
	for _, redirects := range []int{5, 6} {
		t.Run(strings.Repeat("r", redirects), func(t *testing.T) {
			calls := 0
			collector := serviceTestCollector(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls <= redirects {
					response := serviceTestResponse(r, 302, "")
					response.Header.Set("Location", "https://www.reddit.com/next")
					return response, nil
				}
				return serviceTestResponse(r, 200, "done"), nil
			})
			_, err := collector.doServiceRequest(context.Background(), 4, "reddit", http.MethodGet, "https://www.reddit.com/", nil, nil)
			if redirects == 5 {
				if err != nil || calls != 6 {
					t.Fatalf("calls %d err %v", calls, err)
				}
			} else {
				if !errors.Is(err, errServiceRedirect) || calls != 6 {
					t.Fatalf("calls %d err %v", calls, err)
				}
			}
		})
	}
}

func TestServiceHTTPCookiesAreNeitherReadNorPersisted(t *testing.T) {
	jar, _ := cookiejar.New(nil)
	target, _ := url.Parse("https://www.reddit.com/")
	jar.SetCookies(target, []*http.Cookie{{Name: "existing-cookie", Value: "private-existing", Path: "/"}})
	requests := 0
	client := &http.Client{Jar: jar, Transport: serviceTestTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		if r.Header.Get("Cookie") != "" {
			t.Fatal("cookie sent")
		}
		response := serviceTestResponse(r, 200, anonymousServiceFixture(r))
		response.Header.Set("Set-Cookie", "private-cookie=value; Path=/")
		return response, nil
	})}
	collector := NewHTTPCollector(HTTPCollectorOptions{Client: client})
	for range 2 {
		if result := collector.probeService(context.Background(), "reddit", 4); result.Result.Status != "unlocked" {
			t.Fatal(result)
		}
	}
	if requests != 2 || client.Jar != jar {
		t.Fatal("shared jar changed")
	}
	cookies := jar.Cookies(target)
	if len(cookies) != 1 || cookies[0].Name != "existing-cookie" {
		t.Fatal("service persisted response cookies")
	}
}

func TestServiceProtocolRedirectLoginAndConsentAreUnknown(t *testing.T) {
	for _, service := range []string{"netflix", "chatgpt", "youtube-premium", "amazon-prime-video", "tiktok", "reddit"} {
		t.Run(service, func(t *testing.T) {
			collector := serviceTestCollector(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/login" {
					response := serviceTestResponse(r, 302, "")
					target := "https://" + r.URL.Host + "/login"
					if service == "youtube-premium" {
						target = "https://consent.youtube.com/login"
					}
					response.Header.Set("Location", target)
					return response, nil
				}
				return serviceTestResponse(r, 200, anonymousServiceFixture(r)), nil
			})
			result := collector.probeService(context.Background(), service, 4)
			if result.Result.Status != "unknown" {
				t.Fatalf("result %+v", result.Result)
			}
		})
	}
}

func TestServiceHTTPParentDeadlineCancelsRequestWithoutRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	exited := make(chan struct{})
	calls := 0
	collector := serviceTestCollector(func(r *http.Request) (*http.Response, error) {
		calls++
		close(entered)
		<-r.Context().Done()
		close(exited)
		return nil, r.Context().Err()
	})
	done := make(chan serviceProbeOutcome, 1)
	go func() { done <- collector.probeService(ctx, "tiktok", 6) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case result := <-done:
		if result.Result.Status != "unknown" || calls != 1 {
			t.Fatalf("result %+v calls %d", result.Result, calls)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request did not join")
	}
	select {
	case <-exited:
	default:
		t.Fatal("returned before request exited")
	}
}

func TestServiceErrorSummariesAndSafeEvidence(t *testing.T) {
	cases := []struct {
		name, code, summary string
		handler             serviceTestTransport
	}{
		{"oversize", "response_too_large", "service response exceeds size limit", func(r *http.Request) (*http.Response, error) {
			return serviceTestResponse(r, 200, strings.Repeat("x", serviceBodyLimit+1)), nil
		}},
		{"redirect", "unexpected_redirect", "service redirect is not allowed", func(r *http.Request) (*http.Response, error) {
			response := serviceTestResponse(r, 302, "secret-html")
			response.Header.Set("Location", "https://private.example/?access_token=secret-token")
			return response, nil
		}},
		{"challenge", "challenge_detected", "service challenge detected", func(r *http.Request) (*http.Response, error) {
			return serviceTestResponse(r, 200, `<div id="cf-chl-test">secret-html</div>`), nil
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result := serviceTestCollector(test.handler).probeService(context.Background(), "reddit", 4)
			if result.Result.ErrorCode != test.code || result.Result.ErrorSummary != test.summary {
				t.Fatalf("result %+v", result.Result)
			}
			if strings.Contains(string(result.Raw), "secret") || strings.Contains(string(result.Result.ExtraJSON), "private.example") {
				t.Fatalf("raw leak %s", result.Raw)
			}
		})
	}
}

func TestLoginFormsOverrideServiceHTMLFacts(t *testing.T) {
	for _, service := range []string{"netflix", "chatgpt", "youtube-premium", "amazon-prime-video", "tiktok", "reddit"} {
		t.Run(service, func(t *testing.T) {
			collector := serviceTestCollector(func(r *http.Request) (*http.Response, error) {
				body := anonymousServiceFixture(r)
				if service == "chatgpt" && r.URL.Hostname() == "api.openai.com" {
					body = `{}`
				}
				return serviceTestResponse(r, 200, body+`<form action="/signin"><input type="password"></form>`), nil
			})
			result := collector.probeService(context.Background(), service, 4)
			if result.Result.Status != "unknown" {
				t.Fatalf("result %+v", result.Result)
			}
		})
	}
}

func TestJSONTextIsNotAnHTMLRestrictionPage(t *testing.T) {
	for _, service := range []string{"chatgpt", "reddit"} {
		collector := serviceTestCollector(func(r *http.Request) (*http.Response, error) {
			return serviceTestResponse(r, 403, `{"message":"ChatGPT disable your VPN; blocked by network security"}`), nil
		})
		result := collector.probeService(context.Background(), service, 4)
		if result.Result.Status != "unknown" {
			t.Fatalf("%s result %+v", service, result.Result)
		}
	}
}
