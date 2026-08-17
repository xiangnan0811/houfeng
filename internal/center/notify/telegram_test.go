package notify_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"houfeng/internal/center/notify"
)

func TestTelegramNotifierPostsMessage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botbot-token/sendMessage" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/botbot-token/sendMessage")
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["chat_id"] != "chat-001" {
			t.Fatalf("chat_id = %q, want %q", body["chat_id"], "chat-001")
		}
		if body["text"] != "incident started" {
			t.Fatalf("text = %q, want %q", body["text"], "incident started")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()

	notifier := notify.NewTelegramNotifierWithBaseURL("bot-token", "chat-001", ts.URL, ts.Client())
	if err := notifier.Send(context.Background(), "incident started"); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
}

func TestTelegramNotifierReturnsTypedContentFreeHTTPFailures(t *testing.T) {
	for _, test := range []struct {
		status int
		want   notify.SendFailureClass
	}{
		{status: http.StatusRequestTimeout, want: notify.SendFailureTemporary},
		{status: http.StatusTooEarly, want: notify.SendFailureTemporary},
		{status: http.StatusTooManyRequests, want: notify.SendFailureTemporary},
		{status: http.StatusBadGateway, want: notify.SendFailureTemporary},
		{status: http.StatusBadRequest, want: notify.SendFailurePermanent},
	} {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte("provider response credential=secret"))
			}))
			defer ts.Close()

			notifier := notify.NewTelegramNotifierWithBaseURL("bot-token-secret", "chat-001", ts.URL, ts.Client())
			err := notifier.Send(context.Background(), "incident started")
			assertTelegramSendFailure(t, err, test.want)
		})
	}
}

func TestTelegramNotifierRequiresSingleBoundedVerifiedProviderJSON(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		want *notify.SendFailureClass
	}{
		{name: "verified success", body: `{"ok":true}`},
		{name: "temporary 408", body: `{"ok":false,"error_code":408}`, want: telegramFailureClass(notify.SendFailureTemporary)},
		{name: "temporary 425", body: `{"ok":false,"error_code":425}`, want: telegramFailureClass(notify.SendFailureTemporary)},
		{name: "temporary 429", body: `{"ok":false,"error_code":429}`, want: telegramFailureClass(notify.SendFailureTemporary)},
		{name: "temporary 503", body: `{"ok":false,"error_code":503}`, want: telegramFailureClass(notify.SendFailureTemporary)},
		{name: "permanent 400", body: `{"ok":false,"error_code":400}`, want: telegramFailureClass(notify.SendFailurePermanent)},
		{name: "permanent 499", body: `{"ok":false,"error_code":499}`, want: telegramFailureClass(notify.SendFailurePermanent)},
		{name: "missing ok", body: `{"result":true}`, want: telegramFailureClass(notify.SendFailureUnknown)},
		{name: "false without code", body: `{"ok":false}`, want: telegramFailureClass(notify.SendFailureUnknown)},
		{name: "unknown code", body: `{"ok":false,"error_code":302}`, want: telegramFailureClass(notify.SendFailureUnknown)},
		{name: "invalid code type", body: `{"ok":false,"error_code":"credential=secret"}`, want: telegramFailureClass(notify.SendFailureUnknown)},
		{name: "empty", body: "", want: telegramFailureClass(notify.SendFailureUnknown)},
		{name: "malformed", body: `{"ok":credential=secret}`, want: telegramFailureClass(notify.SendFailureUnknown)},
		{name: "multiple values", body: `{"ok":true}{"ok":true}`, want: telegramFailureClass(notify.SendFailureUnknown)},
		{name: "oversized", body: `{"ok":true,"padding":"` + strings.Repeat("x", 8192) + `"}`, want: telegramFailureClass(notify.SendFailureUnknown)},
	} {
		t.Run(test.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.body))
			}))
			defer ts.Close()

			notifier := notify.NewTelegramNotifierWithBaseURL("bot-token-secret", "chat-001", ts.URL, ts.Client())
			err := notifier.Send(context.Background(), "incident started")
			if test.want == nil {
				if err != nil {
					t.Fatalf("Send() error = %v, want nil", err)
				}
				return
			}
			assertTelegramSendFailure(t, err, *test.want)
		})
	}
}

func TestTelegramNotifierTreatsNonClosedHTTPStatusesAsUnknown(t *testing.T) {
	for _, status := range []int{199, http.StatusFound, 700} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client := &http.Client{Transport: telegramRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: status, Header: make(http.Header), Request: request,
					Body: io.NopCloser(strings.NewReader(`{"ok":true}`)),
				}, nil
			})}
			notifier := notify.NewTelegramNotifierWithBaseURL("bot-token-secret", "chat-001", "https://telegram.invalid", client)
			assertTelegramSendFailure(t, notifier.Send(context.Background(), "incident started"), notify.SendFailureUnknown)
		})
	}
}

func TestTelegramNotifierReturnsContentFreeUnknownForTransportFailure(t *testing.T) {
	client := &http.Client{Transport: telegramRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("transport credential=secret")
	})}
	notifier := notify.NewTelegramNotifierWithBaseURL("bot-token-secret", "chat-001", "https://telegram.invalid", client)
	err := notifier.Send(context.Background(), "incident started")
	assertTelegramSendFailure(t, err, notify.SendFailureUnknown)
}

type telegramRoundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip telegramRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func assertTelegramSendFailure(t *testing.T, err error, want notify.SendFailureClass) {
	t.Helper()
	got, ok := notify.ClassifySendFailure(err)
	if !ok || got != want {
		t.Fatalf("ClassifySendFailure(%v) = (%q, %t), want (%q, true)", err, got, ok, want)
	}
	for _, forbidden := range []string{"secret", "credential", "provider response", "telegram.invalid", "bot-token"} {
		if strings.Contains(strings.ToLower(err.Error()), forbidden) {
			t.Fatalf("typed send failure leaked %q in %q", forbidden, err.Error())
		}
	}
}

func telegramFailureClass(class notify.SendFailureClass) *notify.SendFailureClass {
	return &class
}
