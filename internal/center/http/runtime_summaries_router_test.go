package http_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	centerhttp "houfeng/internal/center/http"
)

func TestRuntimeSummariesRouteRequiresAuthMiddleware(t *testing.T) {
	called := false
	router := centerhttp.New(centerhttp.RouterOptions{
		MonitoringInstanceRuntimeSummariesHandler: runtimeSummaryTestHandler(&called),
		AuthMiddleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Test-Session") != "ok" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				next.ServeHTTP(w, r)
			})
		},
	})

	unauthorized := httptest.NewRecorder()
	router.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/runtime-summaries", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}
	if called {
		t.Fatal("runtime summaries handler ran for unauthorized request")
	}

	authorizedRequest := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/runtime-summaries", nil)
	authorizedRequest.Header.Set("X-Test-Session", "ok")
	authorized := httptest.NewRecorder()
	router.ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusOK {
		t.Fatalf("authorized status = %d, want %d; body=%s", authorized.Code, http.StatusOK, authorized.Body.String())
	}
	if !called {
		t.Fatal("runtime summaries handler did not run for authorized request")
	}
}

func runtimeSummaryTestHandler(called *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*called = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"read_at":"2026-04-24T10:00:00Z","monitoring_instances":{}}`))
	})
}
