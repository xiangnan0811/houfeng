package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/nodes"
	"houfeng/internal/center/store"
)

type fakeNodeBatchRepository struct {
	setMaintenanceErr    error
	setMaintenanceCalled []string
	pauseErr             error
	pauseCalled          []string
	resumeErr            error
	resumeCalled         []string
}

func (f *fakeNodeBatchRepository) SetNodeMonitoringMaintenance(_ context.Context, nodeID string) (nodes.Record, error) {
	f.setMaintenanceCalled = append(f.setMaintenanceCalled, nodeID)
	if f.setMaintenanceErr != nil {
		return nodes.Record{}, f.setMaintenanceErr
	}
	return nodes.Record{NodeID: nodeID, MonitoringStatus: "维护中"}, nil
}

func (f *fakeNodeBatchRepository) PauseNodeMonitoring(_ context.Context, nodeID string) (nodes.Record, error) {
	f.pauseCalled = append(f.pauseCalled, nodeID)
	if f.pauseErr != nil {
		return nodes.Record{}, f.pauseErr
	}
	return nodes.Record{NodeID: nodeID, MonitoringStatus: "暂停"}, nil
}

func (f *fakeNodeBatchRepository) ResumeNodeMonitoring(_ context.Context, nodeID string) (nodes.Record, error) {
	f.resumeCalled = append(f.resumeCalled, nodeID)
	if f.resumeErr != nil {
		return nodes.Record{}, f.resumeErr
	}
	return nodes.Record{NodeID: nodeID, MonitoringStatus: "启用"}, nil
}

func TestNodeBatchSuccess(t *testing.T) {
	t.Parallel()

	repo := &fakeNodeBatchRepository{}
	handler := handlers.NodeBatch(repo)

	body := strings.NewReader(`{"node_ids":["nd_001","nd_002"],"action":"enter-maintenance"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/batch", body)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var resp struct {
		Results []struct {
			NodeID string `json:"node_id"`
			OK     bool   `json:"ok"`
			Error  string `json:"error,omitempty"`
		} `json:"results"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(resp.Results) != 2 {
		t.Fatalf("results length = %d, want 2", len(resp.Results))
	}
	if !resp.Results[0].OK || resp.Results[0].NodeID != "nd_001" {
		t.Fatalf("result[0] = {node_id:%s ok:%t}, want {node_id:nd_001 ok:true}", resp.Results[0].NodeID, resp.Results[0].OK)
	}
	if !resp.Results[1].OK || resp.Results[1].NodeID != "nd_002" {
		t.Fatalf("result[1] = {node_id:%s ok:%t}, want {node_id:nd_002 ok:true}", resp.Results[1].NodeID, resp.Results[1].OK)
	}
}

func TestNodeBatchSingleFailureDoesNotBlockOthers(t *testing.T) {
	t.Parallel()

	repo := &fakeNodeBatchRepository{
		pauseErr: store.ErrInvalidNodeRuntimeTransition,
	}
	handler := handlers.NodeBatch(repo)

	body := strings.NewReader(`{"node_ids":["nd_001","nd_002","nd_003"],"action":"pause"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/batch", body)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var resp struct {
		Results []struct {
			NodeID string `json:"node_id"`
			OK     bool   `json:"ok"`
			Error  string `json:"error,omitempty"`
		} `json:"results"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(resp.Results) != 3 {
		t.Fatalf("results length = %d, want 3", len(resp.Results))
	}

	// With the error, all 3 still fail since the same error is returned.
	// But they should all 3 have results (not blocked).
	for i, r := range resp.Results {
		if r.OK {
			t.Fatalf("result[%d] ok = true, want false (all should fail with invalid transition)", i)
		}
		if r.Error != "invalid runtime transition" {
			t.Fatalf("result[%d] error = %q, want %q", i, r.Error, "invalid runtime transition")
		}
	}

	if len(repo.pauseCalled) != 3 {
		t.Fatalf("pause called %d times, want 3", len(repo.pauseCalled))
	}
}

func TestNodeBatchRejectsEmptyNodeIDs(t *testing.T) {
	t.Parallel()

	handler := handlers.NodeBatch(&fakeNodeBatchRepository{})

	body := strings.NewReader(`{"node_ids":[],"action":"pause"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/batch", body)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestNodeBatchRejectsInvalidAction(t *testing.T) {
	t.Parallel()

	handler := handlers.NodeBatch(&fakeNodeBatchRepository{})

	body := strings.NewReader(`{"node_ids":["nd_001"],"action":"retire"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/batch", body)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestNodeBatchRejectsWrongMethod(t *testing.T) {
	t.Parallel()

	handler := handlers.NodeBatch(&fakeNodeBatchRepository{})
	req := httptest.NewRequest(http.MethodGet, "/api/nodes/batch", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}

func TestNodeBatchNodeNotFound(t *testing.T) {
	t.Parallel()

	repo := &fakeNodeBatchRepository{
		setMaintenanceErr: nodes.ErrNodeNotFound,
	}
	handler := handlers.NodeBatch(repo)

	body := strings.NewReader(`{"node_ids":["nd_missing"],"action":"enter-maintenance"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/nodes/batch", body)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var resp struct {
		Results []struct {
			NodeID string `json:"node_id"`
			OK     bool   `json:"ok"`
			Error  string `json:"error,omitempty"`
		} `json:"results"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(resp.Results) != 1 {
		t.Fatalf("results length = %d, want 1", len(resp.Results))
	}
	if resp.Results[0].OK {
		t.Fatalf("result ok = true, want false")
	}
	if resp.Results[0].Error != "node not found" {
		t.Fatalf("result error = %q, want %q", resp.Results[0].Error, "node not found")
	}
}
