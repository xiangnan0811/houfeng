package http_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	centerhttp "houfeng/internal/center/http"
)

const spaShell = "<!doctype html><title>houfeng-spa</title>"

func TestRouterKeepsAPINodesOutOfSPAFallback(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		NodesCollectionHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"node_id":"nd_001"}]`))
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	body, err := io.ReadAll(recorder.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	if strings.TrimSpace(string(body)) == spaShell {
		t.Fatalf("expected API response, got SPA fallback body %q", string(body))
	}

	if !strings.Contains(string(body), `"node_id":"nd_001"`) {
		t.Fatalf("expected node payload, got %q", string(body))
	}
}

func TestRouterStillFallsBackForWebPath(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
	})

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

	if strings.TrimSpace(string(body)) != spaShell {
		t.Fatalf("expected SPA body %q, got %q", spaShell, string(body))
	}
}

func TestRouterDoesNotFallBackToSPAForUnknownAPIPath(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/unknown", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}

	body, err := io.ReadAll(recorder.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	if strings.TrimSpace(string(body)) == spaShell {
		t.Fatalf("expected API 404 body, got SPA fallback body %q", string(body))
	}
}

func TestRouterDoesNotFallBackToSPAForUnknownTargetSubtree(t *testing.T) {
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: "testdata/web",
		TargetItemHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		TargetProbeItemsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/targets/tg_001/unknown", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}

	body, err := io.ReadAll(recorder.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	if strings.TrimSpace(string(body)) == spaShell {
		t.Fatalf("expected target subtree 404 body, got SPA fallback body %q", string(body))
	}
}
