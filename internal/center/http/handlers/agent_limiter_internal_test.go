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

func TestAgentRateLimitOptionsNormalizeEachField(t *testing.T) {
	for _, endpoint := range []struct {
		name     string
		defaults AgentRateLimitOptions
	}{
		{"enrollment", AgentRateLimitOptions{MaxRequestsByIP: 120, MaxRequestsGlobal: 1000, MaxTrackedKeys: 10000, Window: time.Minute, SweepInterval: time.Minute, MaxSyncInflight: 32}},
		{"sync", AgentRateLimitOptions{MaxRequestsByIP: 2400, MaxRequestsGlobal: 2400, MaxTrackedKeys: 10000, Window: time.Minute, SweepInterval: time.Minute, MaxSyncInflight: 32}},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			for _, opts := range []AgentRateLimitOptions{
				{},
				{MaxRequestsByIP: -1, MaxRequestsGlobal: -1, MaxTrackedKeys: -1, Window: -1, SweepInterval: -1, MaxSyncInflight: -1},
			} {
				if got := normalizeAgentRateLimitOptions(opts, endpoint.defaults); got != endpoint.defaults {
					t.Fatalf("nonpositive defaults = %+v, want %+v", got, endpoint.defaults)
				}
			}
			for field := range 6 {
				var opts AgentRateLimitOptions
				want := endpoint.defaults
				switch field {
				case 0:
					opts.MaxRequestsByIP, want.MaxRequestsByIP = 3, 3
				case 1:
					opts.MaxRequestsGlobal, want.MaxRequestsGlobal = 7, 7
				case 2:
					opts.MaxTrackedKeys, want.MaxTrackedKeys = 11, 11
				case 3:
					opts.Window, want.Window = 2*time.Minute, 2*time.Minute
				case 4:
					opts.SweepInterval, want.SweepInterval = 3*time.Second, 3*time.Second
				case 5:
					opts.MaxSyncInflight, want.MaxSyncInflight = 2, 2
				}
				if got := normalizeAgentRateLimitOptions(opts, endpoint.defaults); got != want {
					t.Fatalf("field %d overrides = %+v, want %+v", field, got, want)
				}
			}
		})
	}
}

func TestAgentSyncDefaultInflightGateRemainsBounded(t *testing.T) {
	gate := newAgentSyncInflightGate(defaultAgentSyncRateLimitOptions().MaxSyncInflight)
	for i := range 32 {
		if !gate.acquire() {
			t.Fatalf("inflight %d rejected before default capacity", i+1)
		}
	}
	if gate.acquire() {
		t.Fatal("33rd inflight request accepted")
	}
	gate.release()
	if !gate.acquire() {
		t.Fatal("released capacity was not reusable")
	}
}
