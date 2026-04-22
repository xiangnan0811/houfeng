package http_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	centerhttp "houfeng/internal/center/http"
)

func TestRouterHealthz(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{Version: "dev"})

	req := httptest.NewRequest(http.MethodGet, "/api/healthz", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	var body struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Status  string `json:"status"`
	}

	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}

	if body.Name != "houfeng-center" {
		t.Fatalf("expected name %q, got %q", "houfeng-center", body.Name)
	}

	if body.Version != "dev" {
		t.Fatalf("expected version %q, got %q", "dev", body.Version)
	}

	if body.Status != "ok" {
		t.Fatalf("expected status %q, got %q", "ok", body.Status)
	}
}
