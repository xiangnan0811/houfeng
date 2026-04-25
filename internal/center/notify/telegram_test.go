package notify_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestTelegramNotifierReturnsErrorForBadStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream unavailable"))
	}))
	defer ts.Close()

	notifier := notify.NewTelegramNotifierWithBaseURL("bot-token", "chat-001", ts.URL, ts.Client())
	if err := notifier.Send(context.Background(), "incident started"); err == nil {
		t.Fatal("Send() error = nil, want non-nil")
	}
}
