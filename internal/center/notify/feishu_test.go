package notify_test

import (
	"context"
	"errors"
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
		w.WriteHeader(http.StatusOK)
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
