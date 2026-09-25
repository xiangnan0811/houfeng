package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"houfeng/internal/center/assetlifecycle"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/store"
)

type fakeMonitoringInstanceBatchRepository struct {
	setMaintenanceErr    error
	setMaintenanceCalled []string
	pauseErr             error
	pauseCalled          []string
	pauseExpectedDigests map[string]string
	resumeErr            error
	resumeCalled         []string
}

func (f *fakeMonitoringInstanceBatchRepository) SetMonitoringInstanceMonitoringMaintenance(_ context.Context, monitoringInstanceID string) (monitoringinstances.Record, error) {
	f.setMaintenanceCalled = append(f.setMaintenanceCalled, monitoringInstanceID)
	if f.setMaintenanceErr != nil {
		return monitoringinstances.Record{}, f.setMaintenanceErr
	}
	return monitoringinstances.Record{MonitoringInstanceID: monitoringInstanceID, MonitoringStatus: "维护中"}, nil
}

func (f *fakeMonitoringInstanceBatchRepository) PauseMonitoringInstanceMonitoring(_ context.Context, monitoringInstanceID string, inputs ...monitoringinstances.RuntimeControlInput) (monitoringinstances.Record, error) {
	f.pauseCalled = append(f.pauseCalled, monitoringInstanceID)
	if f.pauseErr != nil {
		return monitoringinstances.Record{}, f.pauseErr
	}
	if expectedDigest, ok := f.pauseExpectedDigests[monitoringInstanceID]; ok {
		if len(inputs) != 1 || inputs[0].PreviewDigest != expectedDigest || !inputs[0].ConfirmSharedImpact {
			return monitoringinstances.Record{}, assetlifecycle.ErrSharedImpactConfirmationRequired
		}
	}
	if len(inputs) > 1 {
		return monitoringinstances.Record{}, monitoringinstances.ErrInvalidManagementInput
	}
	return monitoringinstances.Record{MonitoringInstanceID: monitoringInstanceID, MonitoringStatus: "暂停"}, nil
}

func (f *fakeMonitoringInstanceBatchRepository) ResumeMonitoringInstanceMonitoring(_ context.Context, monitoringInstanceID string) (monitoringinstances.Record, error) {
	f.resumeCalled = append(f.resumeCalled, monitoringInstanceID)
	if f.resumeErr != nil {
		return monitoringinstances.Record{}, f.resumeErr
	}
	return monitoringinstances.Record{MonitoringInstanceID: monitoringInstanceID, MonitoringStatus: "启用"}, nil
}

func TestMonitoringInstanceBatchSuccess(t *testing.T) {
	t.Parallel()

	repo := &fakeMonitoringInstanceBatchRepository{}
	handler := handlers.MonitoringInstanceBatch(repo)

	body := strings.NewReader(`{"monitoring_instance_ids":["mi_001","mi_002"],"action":"enter-maintenance"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/batch", body)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var resp struct {
		Results []struct {
			MonitoringInstanceID string `json:"monitoring_instance_id"`
			OK                   bool   `json:"ok"`
			Error                string `json:"error,omitempty"`
		} `json:"results"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if len(resp.Results) != 2 {
		t.Fatalf("results length = %d, want 2", len(resp.Results))
	}
	if !resp.Results[0].OK || resp.Results[0].MonitoringInstanceID != "mi_001" {
		t.Fatalf("result[0] = {monitoring_instance_id:%s ok:%t}, want {monitoring_instance_id:mi_001 ok:true}", resp.Results[0].MonitoringInstanceID, resp.Results[0].OK)
	}
	if !resp.Results[1].OK || resp.Results[1].MonitoringInstanceID != "mi_002" {
		t.Fatalf("result[1] = {monitoring_instance_id:%s ok:%t}, want {monitoring_instance_id:mi_002 ok:true}", resp.Results[1].MonitoringInstanceID, resp.Results[1].OK)
	}
}

func TestMonitoringInstanceBatchSingleFailureDoesNotBlockOthers(t *testing.T) {
	t.Parallel()

	repo := &fakeMonitoringInstanceBatchRepository{
		pauseErr: store.ErrInvalidMonitoringInstanceRuntimeTransition,
	}
	handler := handlers.MonitoringInstanceBatch(repo)

	body := strings.NewReader(`{"monitoring_instance_ids":["mi_001","mi_002","mi_003"],"action":"pause"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/batch", body)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var resp struct {
		Results []struct {
			MonitoringInstanceID string `json:"monitoring_instance_id"`
			OK                   bool   `json:"ok"`
			Error                string `json:"error,omitempty"`
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
func TestMonitoringInstanceBatchUsesPerInstancePauseConfirmation(t *testing.T) {
	t.Parallel()

	repo := &fakeMonitoringInstanceBatchRepository{
		pauseExpectedDigests: map[string]string{
			"mi_001": "review-001",
			"mi_002": "review-002",
		},
	}
	handler := handlers.MonitoringInstanceBatch(repo)
	body := strings.NewReader(`{"monitoring_instance_ids":["mi_001","mi_002"],"action":"pause","confirmations":{"mi_001":{"preview_digest":"review-001","confirm_shared_impact":true}}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/batch", body)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var response struct {
		Results []struct {
			MonitoringInstanceID string `json:"monitoring_instance_id"`
			OK                   bool   `json:"ok"`
			Error                string `json:"error,omitempty"`
		} `json:"results"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(response.Results) != 2 ||
		response.Results[0].MonitoringInstanceID != "mi_001" ||
		!response.Results[0].OK ||
		response.Results[1].MonitoringInstanceID != "mi_002" ||
		response.Results[1].OK ||
		response.Results[1].Error != "shared impact confirmation required" {
		t.Fatalf("batch results = %#v, want confirmed mi_001 success and unconfirmed mi_002 failure", response.Results)
	}
}

func TestMonitoringInstanceBatchMapsArchivedMonitoringInstance(t *testing.T) {
	t.Parallel()

	repo := &fakeMonitoringInstanceBatchRepository{
		resumeErr: monitoringinstances.ErrArchivedMonitoringInstance,
	}
	handler := handlers.MonitoringInstanceBatch(repo)

	body := strings.NewReader(`{"monitoring_instance_ids":["mi_archived"],"action":"resume"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/batch", body)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var resp struct {
		Results []struct {
			MonitoringInstanceID string `json:"monitoring_instance_id"`
			OK                   bool   `json:"ok"`
			Error                string `json:"error,omitempty"`
		} `json:"results"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("results length = %d, want 1", len(resp.Results))
	}
	if resp.Results[0].OK {
		t.Fatal("result ok = true, want false")
	}
	if resp.Results[0].Error != "archived monitoring instance" {
		t.Fatalf("error = %q, want archived monitoring instance", resp.Results[0].Error)
	}
}

func TestMonitoringInstanceBatchRejectsEmptyMonitoringInstanceIDs(t *testing.T) {
	t.Parallel()

	handler := handlers.MonitoringInstanceBatch(&fakeMonitoringInstanceBatchRepository{})

	body := strings.NewReader(`{"monitoring_instance_ids":[],"action":"pause"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/batch", body)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestMonitoringInstanceBatchRejectsInvalidAction(t *testing.T) {
	t.Parallel()

	handler := handlers.MonitoringInstanceBatch(&fakeMonitoringInstanceBatchRepository{})

	body := strings.NewReader(`{"monitoring_instance_ids":["mi_001"],"action":"retire"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/batch", body)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestMonitoringInstanceBatchRejectsWrongMethod(t *testing.T) {
	t.Parallel()

	handler := handlers.MonitoringInstanceBatch(&fakeMonitoringInstanceBatchRepository{})
	req := httptest.NewRequest(http.MethodGet, "/api/monitoring-instances/batch", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}

func TestMonitoringInstanceBatchMonitoringInstanceNotFound(t *testing.T) {
	t.Parallel()

	repo := &fakeMonitoringInstanceBatchRepository{
		setMaintenanceErr: monitoringinstances.ErrMonitoringInstanceNotFound,
	}
	handler := handlers.MonitoringInstanceBatch(repo)

	body := strings.NewReader(`{"monitoring_instance_ids":["mi_missing"],"action":"enter-maintenance"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/monitoring-instances/batch", body)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var resp struct {
		Results []struct {
			MonitoringInstanceID string `json:"monitoring_instance_id"`
			OK                   bool   `json:"ok"`
			Error                string `json:"error,omitempty"`
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
	if resp.Results[0].Error != "monitoring instance not found" {
		t.Fatalf("result error = %q, want %q", resp.Results[0].Error, "monitoring instance not found")
	}
}
