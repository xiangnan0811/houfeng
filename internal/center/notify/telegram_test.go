package notify_test

import (
	"context"
	"encoding/json"
	"errors"
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
		w.WriteHeader(http.StatusOK)
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
