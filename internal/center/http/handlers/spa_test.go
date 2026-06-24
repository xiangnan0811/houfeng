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

	req := httptest.NewRequest(http.MethodGet, "/monitoring", nil)
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

func TestSPAHandlerRejectsEncodedTraversal(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.html")
	if err := os.WriteFile(indexPath, []byte("<html><body>houfeng-shell</body></html>"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	parentSecret := filepath.Join(filepath.Dir(dir), "outside-secret.txt")
	if err := os.WriteFile(parentSecret, []byte("outside-secret"), 0o644); err != nil {
		t.Fatalf("write outside secret: %v", err)
	}

	handler := handlers.SPA(dir)
	req := httptest.NewRequest(http.MethodGet, "/%2e%2e/"+filepath.Base(parentSecret), nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "outside-secret") {
		t.Fatalf("traversal response leaked outside file body %q", body)
	}
	if strings.Contains(body, "houfeng-shell") {
		t.Fatalf("expected traversal request to be rejected instead of falling back to SPA shell, got %q", body)
	}
}

func TestSPAHandlerDoesNotServeSymlinkEscapingWebDist(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.html")
	if err := os.WriteFile(indexPath, []byte("<html><body>houfeng-shell</body></html>"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	outsideSecret := filepath.Join(t.TempDir(), "outside-secret.txt")
	if err := os.WriteFile(outsideSecret, []byte("outside-secret"), 0o644); err != nil {
		t.Fatalf("write outside secret: %v", err)
	}
	if err := os.Symlink(outsideSecret, filepath.Join(dir, "linked-secret.txt")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	handler := handlers.SPA(dir)
	req := httptest.NewRequest(http.MethodGet, "/linked-secret.txt", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "outside-secret") {
		t.Fatalf("symlink escape response leaked outside file body %q", body)
	}
	if strings.Contains(body, "houfeng-shell") {
		t.Fatalf("expected symlink escape request to be rejected instead of falling back to SPA shell, got %q", body)
	}
}
