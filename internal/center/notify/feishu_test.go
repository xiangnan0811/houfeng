package notify_test

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestFeishuNotifierReturnsErrorForBadStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid webhook"))
	}))
	defer ts.Close()

	notifier := notify.NewFeishuNotifierWithClient(ts.URL, ts.Client())
	if err := notifier.Send(context.Background(), "incident started"); err == nil {
		t.Fatal("Send() error = nil, want non-nil")
	}
}

func TestFeishuNotifierReturnsErrorOnConnectionFailure(t *testing.T) {
	// Use a server that closes immediately to simulate connection failure.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Do nothing; the client will time out or get a connection reset.
	}))
	ts.Close() // close to cause connection refused

	notifier := notify.NewFeishuNotifierWithClient(ts.URL, ts.Client())
	if err := notifier.Send(context.Background(), "incident started"); err == nil {
		t.Fatal("Send() error = nil, want non-nil (connection refused)")
	}
}
