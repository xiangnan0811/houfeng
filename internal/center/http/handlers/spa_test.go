package handlers_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"houfeng/internal/center/http/handlers"
)

func TestSPAHandlerServesIndexForUnknownGet(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.html")

	if err := os.WriteFile(indexPath, []byte("<html><body>houfeng-shell</body></html>"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}

	handler := handlers.SPA(dir)

	req := httptest.NewRequest(http.MethodGet, "/nodes", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	body, err := io.ReadAll(recorder.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	if !strings.Contains(string(body), "houfeng-shell") {
		t.Fatalf("expected body to contain %q, got %q", "houfeng-shell", string(body))
	}
}
