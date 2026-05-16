package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"houfeng/internal/center/http/handlers"
)

func TestInstallerScriptHandlerServesShellScript(t *testing.T) {
	t.Parallel()

	handler := handlers.InstallerScript("#!/bin/sh\necho install\n")
	req := httptest.NewRequest(http.MethodGet, "/api/agent/install.sh", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/x-shellscript; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want shell script content type", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := recorder.Body.String(); got != "#!/bin/sh\necho install\n" {
		t.Fatalf("body = %q, want embedded script", got)
	}
}

func TestInstallerScriptHandlerRejectsWrongMethod(t *testing.T) {
	t.Parallel()

	handler := handlers.InstallerScript("#!/bin/sh\n")
	req := httptest.NewRequest(http.MethodPost, "/api/agent/install.sh", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}
