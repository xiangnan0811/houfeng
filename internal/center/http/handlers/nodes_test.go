package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/store"
)

type fakeNodeRepository struct {
	listNodesResult  []store.NodeRecord
	listNodesErr     error
	getNodeResult    store.NodeRecord
	getNodeErr       error
	createNodeResult store.NodeRecord
	createNodeErr    error
	createNodeInput  store.CreateNodeInput
}

func (f *fakeNodeRepository) ListNodes(context.Context) ([]store.NodeRecord, error) {
	return f.listNodesResult, f.listNodesErr
}

func (f *fakeNodeRepository) GetNode(context.Context, string) (store.NodeRecord, error) {
	if f.getNodeErr != nil {
		return store.NodeRecord{}, f.getNodeErr
	}
	return f.getNodeResult, nil
}

func (f *fakeNodeRepository) CreateNode(_ context.Context, input store.CreateNodeInput) (store.NodeRecord, error) {
	f.createNodeInput = input
	if f.createNodeErr != nil {
		return store.NodeRecord{}, f.createNodeErr
	}
	return f.createNodeResult, nil
}

func TestListNodesHandlerReturnsJSON(t *testing.T) {
	now := time.Date(2026, time.April, 23, 9, 0, 0, 0, time.UTC)
	repo := &fakeNodeRepository{
		listNodesResult: []store.NodeRecord{{
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
		}},
	}

	handler := handlers.NodesCollection(repo)
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

	if len(body) != 1 {
		t.Fatalf("expected 1 node, got %d", len(body))
	}

	if body[0].NodeID != "nd_001" {
		t.Fatalf("expected node_id %q, got %q", "nd_001", body[0].NodeID)
	}
	if body[0].DisplayName != "Tokyo Edge" {
		t.Fatalf("expected display_name %q, got %q", "Tokyo Edge", body[0].DisplayName)
	}
}

func TestCreateNodeHandlerReturnsCreatedRecord(t *testing.T) {
	now := time.Date(2026, time.April, 23, 9, 0, 0, 0, time.UTC)
	repo := &fakeNodeRepository{
		createNodeResult: store.NodeRecord{
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
		},
	}

	handler := handlers.NodesCollection(repo)
	req := httptest.NewRequest(http.MethodPost, "/api/nodes", strings.NewReader(`{"display_name":"Tokyo Edge","region":"ap-northeast-1","city":"Tokyo","provider":"Vultr","lifecycle_status":"待接入"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, recorder.Code)
	}

	var body store.NodeRecord
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}

	if body.NodeID != "nd_001" {
		t.Fatalf("expected node_id %q, got %q", "nd_001", body.NodeID)
	}
	if repo.createNodeInput.DisplayName != "Tokyo Edge" {
		t.Fatalf("expected create input display_name %q, got %q", "Tokyo Edge", repo.createNodeInput.DisplayName)
	}
}

func TestNodeItemReturnsNotFound(t *testing.T) {
	repo := &fakeNodeRepository{getNodeErr: store.ErrNodeNotFound}

	handler := handlers.NodeItem(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/nodes/nd_missing", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}

	if !errors.Is(repo.getNodeErr, store.ErrNodeNotFound) {
		t.Fatalf("expected fake repo error to match ErrNodeNotFound")
	}
	if body["error"] != "node not found" {
		t.Fatalf("expected error %q, got %q", "node not found", body["error"])
	}
}
