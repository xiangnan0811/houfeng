package http_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	centerhttp "houfeng/internal/center/http"
)

func TestRouterRegistersDeploymentSettingsBeforeSPAFallback(t *testing.T) {
	router := newTestRouter(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		DeploymentSettingsHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items":[{"key":"HOUFENG_HTTP_ADDR"}]}`))
		}),
	})

	request := httptest.NewRequest(http.MethodGet, "/api/settings/deployment", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), spaShell) {
		t.Fatalf("deployment route fell through to SPA: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "HOUFENG_HTTP_ADDR") {
		t.Fatalf("body = %s, want deployment payload", response.Body.String())
	}
}

func TestRouterDeploymentSettingsProtectsGetAndKeepsMethodBoundary(t *testing.T) {
	authCalls := 0
	router := centerhttp.New(centerhttp.RouterOptions{
		Version: "dev",
		DeploymentSettingsHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		AuthMiddleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				authCalls++
				w.WriteHeader(http.StatusUnauthorized)
			})
		},
	})

	postRequest := httptest.NewRequest(http.MethodPost, "/api/settings/deployment", nil)
	postResponse := httptest.NewRecorder()
	router.ServeHTTP(postResponse, postRequest)
	if postResponse.Code != http.StatusMethodNotAllowed || authCalls != 0 {
		t.Fatalf("POST status/auth calls = %d/%d, want 405/0", postResponse.Code, authCalls)
	}
	if got := postResponse.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("POST Cache-Control = %q, want private, no-store", got)
	}

	getRequest := httptest.NewRequest(http.MethodGet, "/api/settings/deployment", nil)
	getResponse := httptest.NewRecorder()
	router.ServeHTTP(getResponse, getRequest)
	if getResponse.Code != http.StatusUnauthorized || authCalls != 1 {
		t.Fatalf("GET status/auth calls = %d/%d, want 401/1", getResponse.Code, authCalls)
	}
	if got := getResponse.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("GET Cache-Control = %q, want private, no-store", got)
	}
}
