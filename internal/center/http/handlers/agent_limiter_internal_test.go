package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAgentRequestLimiterCapsIPKeys(t *testing.T) {
	now := time.Date(2026, time.June, 26, 8, 0, 0, 0, time.UTC)
	limiter := newAgentRequestLimiter(AgentEndpointOptions{
		TrustedProxies: []string{"10.0.0.0/8"},
		RateLimit: AgentRateLimitOptions{
			MaxRequestsByIP: 100,
			MaxTrackedKeys:  3,
			Window:          time.Minute,
		},
		Now: func() time.Time { return now },
	})

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/agent/sync", nil)
		req.RemoteAddr = "10.0.0.10:12345"
		req.Header.Set("X-Forwarded-For", "198.51.100."+string(rune('0'+i)))
		if !limiter.allow(req) {
			t.Fatalf("request %d unexpectedly rate limited", i+1)
		}
	}

	if got := len(limiter.byIP); got > 3 {
		t.Fatalf("tracked IP keys = %d, want <= 3", got)
	}
}

func TestAgentRequestLimiterSweepsExpiredKeys(t *testing.T) {
	now := time.Date(2026, time.June, 26, 8, 0, 0, 0, time.UTC)
	limiter := newAgentRequestLimiter(AgentEndpointOptions{
		RateLimit: AgentRateLimitOptions{
			MaxRequestsByIP: 100,
			MaxTrackedKeys:  10,
			SweepInterval:   time.Minute,
			Window:          time.Minute,
		},
		Now: func() time.Time { return now },
	})

	first := httptest.NewRequest(http.MethodPost, "/api/agent/sync", nil)
	first.RemoteAddr = "198.51.100.10:12345"
	limiter.allow(first)

	now = now.Add(2 * time.Minute)
	second := httptest.NewRequest(http.MethodPost, "/api/agent/sync", nil)
	second.RemoteAddr = "198.51.100.11:12345"
	limiter.allow(second)

	if _, ok := limiter.byIP["198.51.100.10"]; ok {
		t.Fatalf("expired IP key was not swept: %#v", limiter.byIP)
	}
	if _, ok := limiter.byIP["198.51.100.11"]; !ok {
		t.Fatalf("active IP key missing after sweep: %#v", limiter.byIP)
	}
}

func TestAgentRequestLimiterAppliesGlobalLimit(t *testing.T) {
	limiter := newAgentRequestLimiter(AgentEndpointOptions{
		RateLimit: AgentRateLimitOptions{
			MaxRequestsByIP:   100,
			MaxRequestsGlobal: 2,
			MaxTrackedKeys:    10,
			Window:            time.Minute,
		},
	})

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/agent/sync", nil)
		req.RemoteAddr = "198.51.100." + string(rune('1'+i)) + ":12345"
		if !limiter.allow(req) {
			t.Fatalf("request %d unexpectedly rate limited", i+1)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/agent/sync", nil)
	req.RemoteAddr = "198.51.100.3:12345"
	if limiter.allow(req) {
		t.Fatal("third request allowed, want global rate limit rejection")
	}
}
