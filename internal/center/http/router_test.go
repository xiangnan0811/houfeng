package http_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	centerhttp "houfeng/internal/center/http"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/nodes"
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

func TestRouterPrefersNodesAPIOverSPAFallback(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.html")
	if err := os.WriteFile(indexPath, []byte("<html><body>spa</body></html>"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}

	now := time.Date(2026, time.April, 23, 9, 0, 0, 0, time.UTC)
	repo := &fakeNodeRepository{
		listNodesResult: []nodes.Record{{
			NodeID:              "nd_001",
			DisplayName:         "Tokyo Edge",
			Region:              "ap-northeast-1",
			City:                "Tokyo",
			Provider:            "Vultr",
			LifecycleStatus:     nodes.LifecyclePendingEnrollment,
			MonitoringStatus:    nodes.MonitoringEnabled,
			BindingStatus:       nodes.BindingUnbound,
			CurrentHealthStatus: nodes.HealthNormal,
			CreatedAt:           now,
			UpdatedAt:           now,
		}},
	}

	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:                "dev",
		WebDistDir:             dir,
		NodesCollectionHandler: handlers.NodesCollection(repo),
		NodeItemHandler:        handlers.NodeItem(repo),
	})

	req := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	var body []nodes.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}

	if len(body) != 1 || body[0].NodeID != "nd_001" {
		t.Fatalf("expected nodes API response, got %#v", body)
	}
}

func TestRouterDispatchesTargetProbeItemsAPI(t *testing.T) {
	var called string
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version: "dev",
		TargetItemHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "item"
			w.WriteHeader(http.StatusOK)
		}),
		TargetProbeItemsHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = "probe-items"
			w.WriteHeader(http.StatusCreated)
		}),
	})

	req := httptest.NewRequest(http.MethodPost, "/api/targets/tg_001/probe-items", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, recorder.Code)
	}
	if called != "probe-items" {
		t.Fatalf("expected probe items handler, got %q", called)
	}
}

type fakeNodeRepository struct {
	listNodesResult  []nodes.Record
	getNodeResult    nodes.Record
	createNodeResult nodes.Record
}

func (f *fakeNodeRepository) ListNodes(context.Context) ([]nodes.Record, error) {
	return f.listNodesResult, nil
}

func (f *fakeNodeRepository) GetNode(context.Context, string) (nodes.Record, error) {
	return f.getNodeResult, nil
}

func (f *fakeNodeRepository) CreateNode(context.Context, nodes.CreateInput) (nodes.Record, error) {
	return f.createNodeResult, nil
}
