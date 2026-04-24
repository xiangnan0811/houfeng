package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/agentplan"
	"houfeng/internal/center/enrollment"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/observations"
	"houfeng/internal/center/syncing"
	"houfeng/internal/contracts/agentapi"
)

type fakeAgentEnrollmentService struct {
	enrollResult enrollment.EnrollResult
	enrollErr    error
	enrollInput  enrollment.EnrollInput
}

func (f *fakeAgentEnrollmentService) EnrollNode(_ context.Context, input enrollment.EnrollInput) (enrollment.EnrollResult, error) {
	f.enrollInput = input
	if f.enrollErr != nil {
		return enrollment.EnrollResult{}, f.enrollErr
	}
	return f.enrollResult, nil
}

type fakeAgentSyncService struct {
	syncErr    error
	syncBatch  syncing.Batch
	syncResult syncing.Result
}

func (f *fakeAgentSyncService) SyncBatch(_ context.Context, batch syncing.Batch) (syncing.Result, error) {
	f.syncBatch = batch
	return f.syncResult, f.syncErr
}

func TestAgentEnrollHandlerReturnsBindingStatus(t *testing.T) {
	t.Parallel()

	svc := &fakeAgentEnrollmentService{
		enrollResult: enrollment.EnrollResult{
			NodeID:        "nd_001",
			BindingStatus: agentapi.BindingStatusBound,
			SyncToken:     "sync-token-001",
		},
	}

	handler := handlers.AgentEnroll(svc)
	req := httptest.NewRequest(http.MethodPost, agentapi.EnrollPath, strings.NewReader(`{"token":"plain-token","fingerprint":"fp-001"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body agentapi.EnrollmentResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if body.NodeID != "nd_001" {
		t.Fatalf("NodeID = %q, want %q", body.NodeID, "nd_001")
	}
	if body.BindingStatus != agentapi.BindingStatusBound {
		t.Fatalf("BindingStatus = %q, want %q", body.BindingStatus, agentapi.BindingStatusBound)
	}
	if body.Status != "accepted" {
		t.Fatalf("Status = %q, want %q", body.Status, "accepted")
	}
	if body.SyncToken != "sync-token-001" {
		t.Fatalf("SyncToken = %q, want %q", body.SyncToken, "sync-token-001")
	}

	if svc.enrollInput.Token != "plain-token" {
		t.Fatalf("EnrollNode token = %q, want %q", svc.enrollInput.Token, "plain-token")
	}
	if svc.enrollInput.Fingerprint != "fp-001" {
		t.Fatalf("EnrollNode fingerprint = %q, want %q", svc.enrollInput.Fingerprint, "fp-001")
	}
}

func TestAgentEnrollHandlerReturnsInvalidEnrollmentTokenError(t *testing.T) {
	t.Parallel()

	svc := &fakeAgentEnrollmentService{enrollErr: enrollment.ErrInvalidEnrollmentToken}

	handler := handlers.AgentEnroll(svc)
	req := httptest.NewRequest(http.MethodPost, agentapi.EnrollPath, strings.NewReader(`{"token":"missing-token","fingerprint":"fp-001"}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}

	assertErrorResponse(t, recorder, agentapi.ErrorCodeInvalidEnrollmentToken, "invalid enrollment token")
}

func TestAgentEnrollHandlerRejectsEmptyFingerprint(t *testing.T) {
	t.Parallel()

	svc := &fakeAgentEnrollmentService{}

	handler := handlers.AgentEnroll(svc)
	req := httptest.NewRequest(http.MethodPost, agentapi.EnrollPath, strings.NewReader(`{"token":"plain-token","fingerprint":""}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}

	assertErrorResponse(t, recorder, agentapi.ErrorCodeInvalidRequest, "invalid request")
}

func TestAgentEnrollHandlerReturnsMethodNotAllowedError(t *testing.T) {
	t.Parallel()

	handler := handlers.AgentEnroll(&fakeAgentEnrollmentService{})
	req := httptest.NewRequest(http.MethodGet, agentapi.EnrollPath, nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}

	assertErrorResponse(t, recorder, agentapi.ErrorCodeMethodNotAllowed, "method not allowed")
}

func TestAgentSyncHandlerReturnsAcceptedAt(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 23, 8, 30, 0, 0, time.UTC)
	acceptedAt := time.Date(2026, time.April, 23, 8, 30, 5, 0, time.UTC)
	svc := &fakeAgentSyncService{
		syncResult: syncing.Result{
			AcceptedAt: acceptedAt,
			Plan: agentplan.SyncPlan{
				HostSampleFrequencyTier: agentapi.FrequencyTier5m,
				ProbeAssignments: []agentplan.ProbeAssignment{{
					TargetID:           "tg_001",
					TargetHost:         "api.example.test",
					MaintenanceContext: false,
					ProbeItemID:        "pb_001",
					ProbeKind:          agentapi.ProbeKindHTTP,
					FrequencyTier:      agentapi.FrequencyTier1m,
					TimeoutSeconds:     5,
					Config:             json.RawMessage(`{"path":"/healthz"}`),
				}},
			},
		},
	}

	handler := handlers.AgentSync(svc)
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{"node_id":"nd_001","sync_token":"sync-token-001","heartbeats":[{"observed_at":"2026-04-23T08:30:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001"}]}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body agentapi.SyncResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if body.AcceptedAt != acceptedAt {
		t.Fatalf("AcceptedAt = %s, want %s", body.AcceptedAt.Format(time.RFC3339), acceptedAt.Format(time.RFC3339))
	}
	if body.Status != "accepted" {
		t.Fatalf("Status = %q, want %q", body.Status, "accepted")
	}
	if body.Plan == nil {
		t.Fatal("Plan = nil, want non-nil")
	}
	if body.Plan.HostSampleFrequencyTier != agentapi.FrequencyTier5m {
		t.Fatalf("HostSampleFrequencyTier = %q, want %q", body.Plan.HostSampleFrequencyTier, agentapi.FrequencyTier5m)
	}
	if len(body.Plan.ProbeAssignments) != 1 {
		t.Fatalf("len(ProbeAssignments) = %d, want 1", len(body.Plan.ProbeAssignments))
	}
	if body.Plan.ProbeAssignments[0].TargetID != "tg_001" {
		t.Fatalf("TargetID = %q, want %q", body.Plan.ProbeAssignments[0].TargetID, "tg_001")
	}

	if svc.syncBatch.NodeID != "nd_001" {
		t.Fatalf("SyncBatch nodeID = %q, want %q", svc.syncBatch.NodeID, "nd_001")
	}
	if svc.syncBatch.SyncToken != "sync-token-001" {
		t.Fatalf("SyncBatch syncToken = %q, want %q", svc.syncBatch.SyncToken, "sync-token-001")
	}
	if len(svc.syncBatch.Heartbeats) != 1 {
		t.Fatalf("SyncBatch heartbeats = %d, want 1", len(svc.syncBatch.Heartbeats))
	}
	if svc.syncBatch.Heartbeats[0].ObservedAt != observedAt {
		t.Fatalf("ObservedAt = %s, want %s", svc.syncBatch.Heartbeats[0].ObservedAt.Format(time.RFC3339), observedAt.Format(time.RFC3339))
	}
	if svc.syncBatch.Heartbeats[0].AgentVersion != "dev" {
		t.Fatalf("AgentVersion = %q, want %q", svc.syncBatch.Heartbeats[0].AgentVersion, "dev")
	}
	if svc.syncBatch.Heartbeats[0].Fingerprint != "fp-001" {
		t.Fatalf("Fingerprint = %q, want %q", svc.syncBatch.Heartbeats[0].Fingerprint, "fp-001")
	}
	if svc.syncBatch.Heartbeats[0].SyncBatchID != "sync_001" {
		t.Fatalf("SyncBatchID = %q, want %q", svc.syncBatch.Heartbeats[0].SyncBatchID, "sync_001")
	}
}

func TestAgentSyncHandlerWritesObservationBatch(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 23, 9, 0, 0, 0, time.UTC)
	svc := &fakeAgentSyncService{}

	handler := handlers.AgentSync(svc)
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{
		"node_id":"nd_001",
		"sync_token":"sync-token-001",
		"heartbeats":[{"observed_at":"2026-04-23T09:00:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001"}],
		"host_samples":[{"observed_at":"2026-04-23T09:00:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001","cpu_usage_pct":12.5,"load_1":0.2,"load_5":0.3,"load_15":0.4,"mem_used_pct":55.5,"mem_available_bytes":1024,"swap_used_pct":1.5,"disk_used_pct":45.5,"inode_used_pct":5.5,"net_in_bytes_per_sec":120,"net_out_bytes_per_sec":220,"cpu_iowait_pct":0.5,"cpu_steal_pct":0.1,"disk_read_bytes_per_sec":320,"disk_write_bytes_per_sec":420,"disk_busy_pct":3.5,"uptime_seconds":3600}],
		"probe_observations":[{"target_id":"tg_001","probe_item_id":"pb_001","probe_kind":"http","observed_at":"2026-04-23T09:00:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001","result_kind":"success","latency_ms":83,"http_status":200}]
	}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if svc.syncBatch.Observations.NodeID != "nd_001" {
		t.Fatalf("syncBatch.Observations.NodeID = %q, want %q", svc.syncBatch.Observations.NodeID, "nd_001")
	}
	if len(svc.syncBatch.Observations.HostSamples) != 1 {
		t.Fatalf("len(syncBatch.Observations.HostSamples) = %d, want 1", len(svc.syncBatch.Observations.HostSamples))
	}
	if len(svc.syncBatch.Observations.ProbeObservations) != 1 {
		t.Fatalf("len(syncBatch.Observations.ProbeObservations) = %d, want 1", len(svc.syncBatch.Observations.ProbeObservations))
	}
	if svc.syncBatch.Observations.HostSamples[0].NodeID != "nd_001" {
		t.Fatalf("HostSamples[0].NodeID = %q, want %q", svc.syncBatch.Observations.HostSamples[0].NodeID, "nd_001")
	}
	if svc.syncBatch.Observations.HostSamples[0].ObservedAt != observedAt {
		t.Fatalf("HostSamples[0].ObservedAt = %s, want %s", svc.syncBatch.Observations.HostSamples[0].ObservedAt.Format(time.RFC3339), observedAt.Format(time.RFC3339))
	}
	if svc.syncBatch.Observations.HostSamples[0].AgentVersion != "dev" {
		t.Fatalf("HostSamples[0].AgentVersion = %q, want %q", svc.syncBatch.Observations.HostSamples[0].AgentVersion, "dev")
	}
	if svc.syncBatch.Observations.HostSamples[0].Fingerprint != "fp-001" {
		t.Fatalf("HostSamples[0].Fingerprint = %q, want %q", svc.syncBatch.Observations.HostSamples[0].Fingerprint, "fp-001")
	}
	if !svc.syncBatch.Observations.HostSamples[0].ReceivedAt.IsZero() {
		t.Fatal("HostSamples[0].ReceivedAt should remain zero in handler DTO")
	}
	if svc.syncBatch.Observations.ProbeObservations[0].TargetID != "tg_001" {
		t.Fatalf("ProbeObservations[0].TargetID = %q, want %q", svc.syncBatch.Observations.ProbeObservations[0].TargetID, "tg_001")
	}
	if svc.syncBatch.Observations.ProbeObservations[0].ProbeItemID != "pb_001" {
		t.Fatalf("ProbeObservations[0].ProbeItemID = %q, want %q", svc.syncBatch.Observations.ProbeObservations[0].ProbeItemID, "pb_001")
	}
	if svc.syncBatch.Observations.ProbeObservations[0].ProbeKind != "http" {
		t.Fatalf("ProbeObservations[0].ProbeKind = %q, want %q", svc.syncBatch.Observations.ProbeObservations[0].ProbeKind, "http")
	}
	if svc.syncBatch.Observations.ProbeObservations[0].AgentVersion != "dev" {
		t.Fatalf("ProbeObservations[0].AgentVersion = %q, want %q", svc.syncBatch.Observations.ProbeObservations[0].AgentVersion, "dev")
	}
	if svc.syncBatch.Observations.ProbeObservations[0].Fingerprint != "fp-001" {
		t.Fatalf("ProbeObservations[0].Fingerprint = %q, want %q", svc.syncBatch.Observations.ProbeObservations[0].Fingerprint, "fp-001")
	}
	if svc.syncBatch.Observations.ProbeObservations[0].LatencyMS == nil || *svc.syncBatch.Observations.ProbeObservations[0].LatencyMS != 83 {
		t.Fatalf("ProbeObservations[0].LatencyMS = %v, want 83", svc.syncBatch.Observations.ProbeObservations[0].LatencyMS)
	}
	if svc.syncBatch.Observations.ProbeObservations[0].HTTPStatus == nil || *svc.syncBatch.Observations.ProbeObservations[0].HTTPStatus != 200 {
		t.Fatalf("ProbeObservations[0].HTTPStatus = %v, want 200", svc.syncBatch.Observations.ProbeObservations[0].HTTPStatus)
	}
	if !svc.syncBatch.Observations.ProbeObservations[0].ReceivedAt.IsZero() {
		t.Fatal("ProbeObservations[0].ReceivedAt should remain zero in handler DTO")
	}
}

func TestAgentSyncHandlerDoesNotReturn200WhenObservationIngestFails(t *testing.T) {
	t.Parallel()

	svc := &fakeAgentSyncService{syncErr: observations.ErrInvalidProbeObservation}

	handler := handlers.AgentSync(svc)
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{
		"node_id":"nd_001",
		"sync_token":"sync-token-001",
		"heartbeats":[{"observed_at":"2026-04-23T09:00:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001"}],
		"probe_observations":[{"target_id":"tg_bad","probe_item_id":"pb_001","probe_kind":"http","observed_at":"2026-04-23T09:00:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001","result_kind":"success","latency_ms":83,"http_status":200}]
	}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}

	assertErrorResponse(t, recorder, agentapi.ErrorCodeInvalidRequest, "invalid request")
}

func TestAgentSyncHandlerReturnsInvalidSyncTokenError(t *testing.T) {
	t.Parallel()

	svc := &fakeAgentSyncService{syncErr: syncing.ErrInvalidSyncToken}

	handler := handlers.AgentSync(svc)
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{"node_id":"nd_001","sync_token":"bad-token","heartbeats":[{"observed_at":"2026-04-23T08:30:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001"}]}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}

	assertErrorResponse(t, recorder, agentapi.ErrorCodeInvalidSyncToken, "invalid sync token")
}

func TestAgentSyncHandlerReturnsBindingNotAcceptedError(t *testing.T) {
	t.Parallel()

	svc := &fakeAgentSyncService{syncErr: syncing.ErrBindingNotAccepted}

	handler := handlers.AgentSync(svc)
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{"node_id":"nd_001","sync_token":"sync-token-001","heartbeats":[{"observed_at":"2026-04-23T08:30:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001"}]}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}

	assertErrorResponse(t, recorder, agentapi.ErrorCodeBindingNotAccepted, "binding not accepted")
}

func TestAgentSyncHandlerRejectsEmptyNodeID(t *testing.T) {
	t.Parallel()

	handler := handlers.AgentSync(&fakeAgentSyncService{})
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{"node_id":"","sync_token":"sync-token-001","heartbeats":[{"observed_at":"2026-04-23T08:30:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001"}]}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}

	assertErrorResponse(t, recorder, agentapi.ErrorCodeInvalidRequest, "invalid request")
}

func TestAgentSyncHandlerRejectsEmptySyncToken(t *testing.T) {
	t.Parallel()

	handler := handlers.AgentSync(&fakeAgentSyncService{})
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{"node_id":"nd_001","sync_token":"","heartbeats":[{"observed_at":"2026-04-23T08:30:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001"}]}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}

	assertErrorResponse(t, recorder, agentapi.ErrorCodeInvalidRequest, "invalid request")
}

func TestAgentSyncHandlerRejectsHeartbeatMissingSyncBatchID(t *testing.T) {
	t.Parallel()

	handler := handlers.AgentSync(&fakeAgentSyncService{})
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{"node_id":"nd_001","sync_token":"sync-token-001","heartbeats":[{"observed_at":"2026-04-23T08:30:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":""}]}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}

	assertErrorResponse(t, recorder, agentapi.ErrorCodeInvalidRequest, "invalid request")
}

func TestAgentSyncHandlerRejectsHeartbeatWithZeroObservedAt(t *testing.T) {
	t.Parallel()

	handler := handlers.AgentSync(&fakeAgentSyncService{})
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{"node_id":"nd_001","sync_token":"sync-token-001","heartbeats":[{"observed_at":"0001-01-01T00:00:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001"}]}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}

	assertErrorResponse(t, recorder, agentapi.ErrorCodeInvalidRequest, "invalid request")
}

func TestAgentSyncHandlerReturnsMethodNotAllowedError(t *testing.T) {
	t.Parallel()

	handler := handlers.AgentSync(&fakeAgentSyncService{})
	req := httptest.NewRequest(http.MethodGet, agentapi.SyncPath, nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}

	assertErrorResponse(t, recorder, agentapi.ErrorCodeMethodNotAllowed, "method not allowed")
}

func assertErrorResponse(t *testing.T, recorder *httptest.ResponseRecorder, wantCode, wantMessage string) {
	t.Helper()

	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("Content-Type = %q, want %q", contentType, "application/json")
	}

	var body agentapi.ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}

	if body.Code != wantCode {
		t.Fatalf("Code = %q, want %q", body.Code, wantCode)
	}
	if body.Message != wantMessage {
		t.Fatalf("Message = %q, want %q", body.Message, wantMessage)
	}
}
