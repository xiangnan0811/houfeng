package notify_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"houfeng/internal/center/notify"
)

func TestFeishuNotifierPostsMessage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, want POST", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer ts.Close()

	notifier := notify.NewFeishuNotifierWithClient(ts.URL, ts.Client())
	if err := notifier.Send(context.Background(), "incident started"); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
}

func TestFeishuNotifierReturnsTypedContentFreeHTTPFailures(t *testing.T) {
	for _, test := range []struct {
		status int
		want   notify.SendFailureClass
	}{
		{status: http.StatusRequestTimeout, want: notify.SendFailureTemporary},
		{status: http.StatusTooEarly, want: notify.SendFailureTemporary},
		{status: http.StatusTooManyRequests, want: notify.SendFailureTemporary},
		{status: http.StatusServiceUnavailable, want: notify.SendFailureTemporary},
		{status: http.StatusForbidden, want: notify.SendFailurePermanent},
	} {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte("provider response credential=secret"))
			}))
			defer ts.Close()

			notifier := notify.NewFeishuNotifierWithClient(ts.URL+"/credential-secret", ts.Client())
			err := notifier.Send(context.Background(), "incident started")
			assertFeishuSendFailure(t, err, test.want)
		})
	}
}

func TestFeishuNotifierRequiresSingleBoundedVerifiedProviderJSON(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want *notify.SendFailureClass
	}{
		{name: "verified success", body: `{"code":0}`},
		{name: "nonzero code", body: `{"code":400}`, want: feishuFailureClass(notify.SendFailureUnknown)},
		{name: "missing code", body: `{"ok":true}`, want: feishuFailureClass(notify.SendFailureUnknown)},
		{name: "invalid code type", body: `{"code":"credential=secret"}`, want: feishuFailureClass(notify.SendFailureUnknown)},
		{name: "empty", body: "", want: feishuFailureClass(notify.SendFailureUnknown)},
		{name: "malformed", body: `{"code":credential=secret}`, want: feishuFailureClass(notify.SendFailureUnknown)},
		{name: "multiple values", body: `{"code":0}{"code":0}`, want: feishuFailureClass(notify.SendFailureUnknown)},
		{name: "oversized", body: `{"code":0,"padding":"` + strings.Repeat("x", 8192) + `"}`, want: feishuFailureClass(notify.SendFailureUnknown)},
	} {
		t.Run(test.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.body))
			}))
			defer ts.Close()

			notifier := notify.NewFeishuNotifierWithClient(ts.URL+"/credential-secret", ts.Client())
			err := notifier.Send(context.Background(), "incident started")
			if test.want == nil {
				if err != nil {
					t.Fatalf("Send() error = %v, want nil", err)
				}
				return
			}
			assertFeishuSendFailure(t, err, *test.want)
		})
	}
}

func TestFeishuNotifierTreatsNonClosedHTTPStatusesAsUnknown(t *testing.T) {
	for _, status := range []int{199, http.StatusFound, 700} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client := &http.Client{Transport: feishuRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: status, Header: make(http.Header), Request: request,
					Body: io.NopCloser(strings.NewReader(`{"code":0}`)),
				}, nil
			})}
			notifier := notify.NewFeishuNotifierWithClient("https://feishu.invalid/credential-secret", client)
			assertFeishuSendFailure(t, notifier.Send(context.Background(), "incident started"), notify.SendFailureUnknown)
		})
	}
}

func TestFeishuNotifierReturnsContentFreeUnknownForTransportFailure(t *testing.T) {
	client := &http.Client{Transport: feishuRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("transport credential=secret")
	})}
	notifier := notify.NewFeishuNotifierWithClient("https://feishu.invalid/credential-secret", client)
	err := notifier.Send(context.Background(), "incident started")
	assertFeishuSendFailure(t, err, notify.SendFailureUnknown)
}

type feishuRoundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip feishuRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func assertFeishuSendFailure(t *testing.T, err error, want notify.SendFailureClass) {
	t.Helper()
	got, ok := notify.ClassifySendFailure(err)
	if !ok || got != want {
		t.Fatalf("ClassifySendFailure(%v) = (%q, %t), want (%q, true)", err, got, ok, want)
	}
	for _, forbidden := range []string{"secret", "credential", "provider response", "feishu.invalid"} {
		if strings.Contains(strings.ToLower(err.Error()), forbidden) {
			t.Fatalf("typed send failure leaked %q in %q", forbidden, err.Error())
		}
	}
}

func feishuFailureClass(class notify.SendFailureClass) *notify.SendFailureClass {
	return &class
}
