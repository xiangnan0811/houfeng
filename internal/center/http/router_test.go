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
	"houfeng/internal/center/store"
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
	handler := centerhttp.New(centerhttp.RouterOptions{
		Version:    "dev",
		WebDistDir: dir,
		Nodes: &fakeNodeRepository{listNodesResult: []store.NodeRecord{{
			NodeID:              "nd_001",
			DisplayName:         "Tokyo Edge",
			Region:              "ap-northeast-1",
			City:                "Tokyo",
			Provider:            "Vultr",
			LifecycleStatus:     "待接入",
			MonitoringStatus:    "启用",
			BindingStatus:       "未绑定",
			CurrentHealthStatus: "正常",
			CreatedAt:           now,
			UpdatedAt:           now,
		}}},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/nodes", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	var body []store.NodeRecord
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}

	if len(body) != 1 || body[0].NodeID != "nd_001" {
		t.Fatalf("expected nodes API response, got %#v", body)
	}
}

type fakeNodeRepository struct {
	listNodesResult  []store.NodeRecord
	getNodeResult    store.NodeRecord
	createNodeResult store.NodeRecord
}

func (f *fakeNodeRepository) ListNodes(context.Context) ([]store.NodeRecord, error) {
	return f.listNodesResult, nil
}

func (f *fakeNodeRepository) GetNode(context.Context, string) (store.NodeRecord, error) {
	return f.getNodeResult, nil
}

func (f *fakeNodeRepository) CreateNode(context.Context, store.CreateNodeInput) (store.NodeRecord, error) {
	return f.createNodeResult, nil
}
