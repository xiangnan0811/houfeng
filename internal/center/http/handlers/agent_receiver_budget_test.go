package handlers_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/http/handlers"
	"houfeng/internal/contracts/agentapi"
)

func TestAgentSyncSharedIPFleetBudget(t *testing.T) {
	for _, sendsPerTick := range []int{1, 3} {
		t.Run(fmt.Sprintf("%d_requests_per_tick", sendsPerTick), func(t *testing.T) {
			now := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)
			handler := handlers.AgentSyncWithOptions(&fakeAgentSyncService{}, handlers.AgentEndpointOptions{Now: func() time.Time { return now }})
			for tick := range 12 {
				now = now.Add(5 * time.Second)
				for session := range 30 {
					for send := range sendsPerTick {
						sequence := tick*sendsPerTick + send + 1
						body := fmt.Sprintf(`{"protocol_revision":2,"producer_id":"c393d63b-3128-489f-89ca-109be0de137d","batch_sequence":%d,"allocated_through":%d,"retired_through":0,"session_id":"mas_%d","monitoring_instance_id":"mi_%d","heartbeats":[{"observed_at":"2026-10-18T12:00:00Z","agent_version":"dev","fingerprint":"fp_%d","sync_batch_id":"c393d63b-3128-489f-89ca-109be0de137d:%d"}]}`, sequence, sequence, session, session, session, sequence)
						req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(body))
						req.RemoteAddr = "198.51.100.20:12345"
						setSyncAuth(req)
						recorder := httptest.NewRecorder()
						handler.ServeHTTP(recorder, req)
						if recorder.Code != http.StatusOK {
							t.Fatalf("tick=%d session=%d send=%d status=%d", tick, session, send, recorder.Code)
						}
					}
				}
			}
		})
	}
}

func TestAgentReceiverDefaultBudgetsAndWindow(t *testing.T) {
	for _, tt := range []struct {
		name      string
		sync      bool
		uniqueIPs bool
		partial   bool
		budget    int
	}{
		{"sync shared IP ignores forged forwarding", true, false, false, 2400},
		{"sync global", true, true, false, 2400},
		{"sync partial options preserve IP limit", true, false, true, 2400},
		{"sync partial options preserve global limit", true, true, true, 2400},
		{"registration shared IP", false, false, false, 120},
		{"registration global", false, true, false, 1000},
		{"registration partial options preserve IP limit", false, false, true, 120},
		{"registration partial options preserve global limit", false, true, true, 1000},
	} {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)
			opts := handlers.AgentEndpointOptions{Now: func() time.Time { return now }}
			if tt.partial {
				opts.RateLimit.MaxSyncInflight = 1
			}
			var handler http.Handler
			path, accepted := agentapi.EnrollPath, http.StatusBadRequest
			if tt.sync {
				handler = handlers.AgentSyncWithOptions(&fakeAgentSyncService{}, opts)
				path, accepted = agentapi.SyncPath, http.StatusUnauthorized
			} else {
				handler = handlers.AgentEnrollWithOptions(&fakeAgentEnrollmentService{}, opts)
			}
			request := func(i int) *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodPost, path, errReader{})
				req.RemoteAddr = "198.51.100.20:12345"
				if tt.uniqueIPs {
					req.RemoteAddr = fmt.Sprintf("[2001:db8::%x]:12345", i+1)
				}
				req.Header.Set("X-Forwarded-For", fmt.Sprintf("2001:db8:1::%x", i+1))
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, req)
				return recorder
			}
			for i := range tt.budget {
				if got := request(i).Code; got != accepted {
					t.Fatalf("request %d status=%d, want %d before budget exhaustion", i+1, got, accepted)
				}
			}
			limited := request(tt.budget)
			if limited.Code != http.StatusTooManyRequests || limited.Header().Get("Retry-After") != "5" {
				t.Fatalf("exhausted status=%d Retry-After=%q", limited.Code, limited.Header().Get("Retry-After"))
			}
			now = now.Add(time.Minute + time.Nanosecond)
			if got := request(0).Code; got != accepted {
				t.Fatalf("expired window status=%d, want %d", got, accepted)
			}
		})
	}
}
