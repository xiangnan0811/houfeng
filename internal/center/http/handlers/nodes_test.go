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
	"houfeng/internal/center/nodes"
)

type fakeNodeRepository struct {
	listNodesResult          []nodes.Record
	listNodesErr             error
	getNodeResult            nodes.Record
	getNodeErr               error
	createNodeResult         nodes.Record
	createNodeErr            error
	createNodeInput          nodes.CreateInput
	updateNodeMetadataResult nodes.Record
	updateNodeMetadataErr    error
	updateNodeMetadataID     string
	updateNodeMetadataInput  nodes.UpdateMetadataInput
}

func (f *fakeNodeRepository) ListNodes(context.Context) ([]nodes.Record, error) {
	return f.listNodesResult, f.listNodesErr
}

func (f *fakeNodeRepository) GetNode(context.Context, string) (nodes.Record, error) {
	if f.getNodeErr != nil {
		return nodes.Record{}, f.getNodeErr
	}
	return f.getNodeResult, nil
}

func (f *fakeNodeRepository) CreateNode(_ context.Context, input nodes.CreateInput) (nodes.Record, error) {
	f.createNodeInput = input
	if f.createNodeErr != nil {
		return nodes.Record{}, f.createNodeErr
	}
	return f.createNodeResult, nil
}

func (f *fakeNodeRepository) UpdateNodeMetadata(_ context.Context, nodeID string, input nodes.UpdateMetadataInput) (nodes.Record, error) {
	f.updateNodeMetadataID = nodeID
	f.updateNodeMetadataInput = input
	if f.updateNodeMetadataErr != nil {
		return nodes.Record{}, f.updateNodeMetadataErr
	}
	return f.updateNodeMetadataResult, nil
}

func TestListNodesHandlerReturnsJSON(t *testing.T) {
	now := time.Date(2026, time.April, 23, 9, 0, 0, 0, time.UTC)
	repo := &fakeNodeRepository{
		listNodesResult: []nodes.Record{{
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

	var body []nodes.Record
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
		createNodeResult: nodes.Record{
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

	var body nodes.Record
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

func TestCreateNodeHandlerForcesPendingLifecycleStatus(t *testing.T) {
	now := time.Date(2026, time.April, 23, 9, 0, 0, 0, time.UTC)
	repo := &fakeNodeRepository{
		createNodeResult: nodes.Record{
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
	req := httptest.NewRequest(http.MethodPost, "/api/nodes", strings.NewReader(`{"display_name":"Tokyo Edge","region":"ap-northeast-1","city":"Tokyo","provider":"Vultr","lifecycle_status":"在用"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, recorder.Code)
	}
	if repo.createNodeInput.LifecycleStatus != nodes.LifecyclePendingEnrollment {
		t.Fatalf("create lifecycle_status = %q, want %q", repo.createNodeInput.LifecycleStatus, nodes.LifecyclePendingEnrollment)
	}
}

func TestNodeItemReturnsNotFound(t *testing.T) {
	repo := &fakeNodeRepository{getNodeErr: nodes.ErrNodeNotFound}

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

	if !errors.Is(repo.getNodeErr, nodes.ErrNodeNotFound) {
		t.Fatalf("expected fake repo error to match ErrNodeNotFound")
	}
	if body["error"] != "node not found" {
		t.Fatalf("expected error %q, got %q", "node not found", body["error"])
	}
}

func TestNodeItemRejectsDeeperPaths(t *testing.T) {
	repo := &fakeNodeRepository{}

	handler := handlers.NodeItem(repo)
	req := httptest.NewRequest(http.MethodGet, "/api/nodes/nd_001/runtime-facts", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}
}

func TestNodeItemPatchMetadataReturnsUpdatedRecord(t *testing.T) {
	now := time.Date(2026, time.April, 27, 9, 0, 0, 0, time.UTC)
	repo := &fakeNodeRepository{
		updateNodeMetadataResult: nodes.Record{
			NodeID:              "nd_001",
			DisplayName:         "Tokyo Edge",
			Region:              "ap-northeast-1",
			City:                "Tokyo",
			Provider:            "Vultr",
			LifecycleStatus:     nodes.LifecyclePendingEnrollment,
			MonitoringStatus:    nodes.MonitoringEnabled,
			BindingStatus:       nodes.BindingUnbound,
			Labels:              []string{"edge", "core"},
			Note:                "updated",
			CurrentHealthStatus: nodes.HealthNormal,
			CreatedAt:           now,
			UpdatedAt:           now,
		},
	}

	handler := handlers.NodeItem(repo)
	req := httptest.NewRequest(http.MethodPatch, "/api/nodes/nd_001", strings.NewReader(`{"labels":[" edge ","core","edge"],"note":" updated "}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if repo.updateNodeMetadataID != "nd_001" {
		t.Fatalf("update node id = %q, want %q", repo.updateNodeMetadataID, "nd_001")
	}
	if len(repo.updateNodeMetadataInput.Labels) != 2 || repo.updateNodeMetadataInput.Labels[0] != "edge" || repo.updateNodeMetadataInput.Labels[1] != "core" {
		t.Fatalf("update labels = %#v, want %#v", repo.updateNodeMetadataInput.Labels, []string{"edge", "core"})
	}
	if repo.updateNodeMetadataInput.Note != "updated" {
		t.Fatalf("update note = %q, want %q", repo.updateNodeMetadataInput.Note, "updated")
	}

	var body nodes.Record
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.NodeID != "nd_001" {
		t.Fatalf("response node_id = %q, want %q", body.NodeID, "nd_001")
	}
	if body.Note != "updated" {
		t.Fatalf("response note = %q, want %q", body.Note, "updated")
	}
	if len(body.Labels) != 2 || body.Labels[0] != "edge" || body.Labels[1] != "core" {
		t.Fatalf("response labels = %#v, want %#v", body.Labels, []string{"edge", "core"})
	}
}

func TestNodeItemRejectsInvalidMetadata(t *testing.T) {
	repo := &fakeNodeRepository{}

	handler := handlers.NodeItem(repo)
	req := httptest.NewRequest(http.MethodPatch, "/api/nodes/nd_001", strings.NewReader(`{"labels":["01","02","03","04","05","06","07","08","09","10","11","12","13","14","15","16","17","18","19","20","21"]}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body["error"] != "invalid input" {
		t.Fatalf("expected error %q, got %q", "invalid input", body["error"])
	}
}

func TestNodeItemMapsMetadataNotFound(t *testing.T) {
	repo := &fakeNodeRepository{updateNodeMetadataErr: nodes.ErrNodeNotFound}

	handler := handlers.NodeItem(repo)
	req := httptest.NewRequest(http.MethodPatch, "/api/nodes/nd_missing", strings.NewReader(`{"labels":["edge"],"note":"updated"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, recorder.Code)
	}
}
