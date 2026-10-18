package enroll

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"houfeng/internal/contracts/agentapi"
)

func TestRetryAfterBoundsAndFallback(t *testing.T) {
	now := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"missing", "", 5 * time.Second},
		{"invalid", "later", 5 * time.Second},
		{"zero", "0", 5 * time.Second},
		{"negative", "-3", 5 * time.Second},
		{"signed", "+3", 5 * time.Second},
		{"fraction", "1.5", 5 * time.Second},
		{"overflow", "18446744073709551616", 5 * time.Second},
		{"one second", "1", time.Second},
		{"seconds", " 17 ", 17 * time.Second},
		{"maximum", "60", time.Minute},
		{"clamped seconds", "999", time.Minute},
		{"date", now.Add(19 * time.Second).Format(http.TimeFormat), 19 * time.Second},
		{"clamped future date", now.Add(time.Hour).Format(http.TimeFormat), time.Minute},
		{"past date", now.Add(-time.Minute).Format(http.TimeFormat), time.Second},
		{"current date", now.Format(http.TimeFormat), time.Second},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseRetryAfter(tt.value, now); got != tt.want {
				t.Fatalf("Retry-After %q = %s, want %s", tt.value, got, tt.want)
			}
		})
	}
}

func TestClientConsumesRetryAfterOnlyForCongestion(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusBadRequest, http.StatusUnauthorized, http.StatusInternalServerError} {
		for _, body := range []string{`{"code":"invalid_request","message":"rejected"}`, "proxy unavailable"} {
			t.Run(http.StatusText(status)+body, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Header().Set("Retry-After", "23")
					w.WriteHeader(status)
					_, _ = w.Write([]byte(body))
				}))
				defer server.Close()
				_, err := NewClient(server.URL).Sync(context.Background(), agentapi.SyncRequest{ProtocolRevision: agentapi.SyncProtocolRevision, ProducerID: "c393d63b-3128-489f-89ca-109be0de137d", SessionID: "mas_retry", MonitoringInstanceID: "mi_retry", SyncToken: "mas_retry.secret"})
				var remote *RemoteError
				if !errors.As(err, &remote) {
					t.Fatalf("Sync() error = %T, want RemoteError", err)
				}
				want := time.Duration(0)
				if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
					want = 23 * time.Second
				}
				if remote.StatusCode != status || remote.RetryAfter != want {
					t.Fatalf("remote status=%d retry=%s, want %d/%s", remote.StatusCode, remote.RetryAfter, status, want)
				}
			})
		}
	}
}
