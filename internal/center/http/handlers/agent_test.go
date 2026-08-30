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

	"houfeng/internal/center/agentplan"
	"houfeng/internal/center/enrollment"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/observations"
	"houfeng/internal/center/syncing"
	"houfeng/internal/contracts/agentapi"
)

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, errors.New("body should not be read")
}

func setSyncAuth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer sync-token-001")
}

type fakeAgentEnrollmentService struct {
	enrollResult enrollment.EnrollResult
	enrollErr    error
	enrollInput  enrollment.EnrollInput
}

func (f *fakeAgentEnrollmentService) EnrollMonitoringInstance(_ context.Context, input enrollment.EnrollInput) (enrollment.EnrollResult, error) {
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

type blockingAgentSyncService struct {
	entered chan struct{}
	release chan struct{}
}

func (f *blockingAgentSyncService) SyncBatch(_ context.Context, _ syncing.Batch) (syncing.Result, error) {
	close(f.entered)
	<-f.release
	return syncing.Result{}, nil
}

func TestAgentEnrollHandlerReturnsBindingStatus(t *testing.T) {
	t.Parallel()

	svc := &fakeAgentEnrollmentService{
		enrollResult: enrollment.EnrollResult{
			MonitoringInstanceID: "mi_001",
			BindingStatus:        agentapi.BindingStatusBound,
			SyncToken:            "sync-token-001",
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

	if body.MonitoringInstanceID != "mi_001" {
		t.Fatalf("MonitoringInstanceID = %q, want %q", body.MonitoringInstanceID, "mi_001")
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
		t.Fatalf("EnrollMonitoringInstance token = %q, want %q", svc.enrollInput.Token, "plain-token")
	}
	if svc.enrollInput.Fingerprint != "fp-001" {
		t.Fatalf("EnrollMonitoringInstance fingerprint = %q, want %q", svc.enrollInput.Fingerprint, "fp-001")
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

func TestAgentEnrollHandlerRejectsOversizedBodyBeforeService(t *testing.T) {
	t.Parallel()

	svc := &fakeAgentEnrollmentService{}
	handler := handlers.AgentEnroll(svc)
	body := `{"token":"` + strings.Repeat("x", handlers.AgentEnrollBodyLimit) + `","fingerprint":"fp-001"}`
	req := httptest.NewRequest(http.MethodPost, agentapi.EnrollPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	assertErrorResponse(t, recorder, agentapi.ErrorCodeInvalidJSON, "invalid json")
	if svc.enrollInput.Token != "" || svc.enrollInput.Fingerprint != "" {
		t.Fatalf("service was called for oversized body: %#v", svc.enrollInput)
	}
}

func TestAgentEnrollHandlerRateLimitsByClientIP(t *testing.T) {
	t.Parallel()

	svc := &fakeAgentEnrollmentService{
		enrollResult: enrollment.EnrollResult{
			MonitoringInstanceID: "mi_001",
			BindingStatus:        agentapi.BindingStatusBound,
			SyncToken:            "sync-token-001",
		},
	}
	handler := handlers.AgentEnrollWithOptions(svc, handlers.AgentEndpointOptions{
		RateLimit: handlers.AgentRateLimitOptions{
			MaxRequestsByIP: 2,
			Window:          time.Minute,
		},
	})

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, agentapi.EnrollPath, strings.NewReader(`{"token":"plain-token","fingerprint":"fp-001"}`))
		req.RemoteAddr = "198.51.100.10:12345"
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("attempt %d status = %d, want %d", i+1, recorder.Code, http.StatusOK)
		}
	}

	req := httptest.NewRequest(http.MethodPost, agentapi.EnrollPath, strings.NewReader(`{"token":"plain-token","fingerprint":"fp-001"}`))
	req.RemoteAddr = "198.51.100.10:12345"
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("limited attempt status = %d, want %d", recorder.Code, http.StatusTooManyRequests)
	}
	assertErrorResponse(t, recorder, agentapi.ErrorCodeInvalidRequest, "too many requests")
}

func TestAgentEnrollHandlerRateLimitIgnoresForgedForwardedForFromUntrustedClient(t *testing.T) {
	t.Parallel()

	svc := &fakeAgentEnrollmentService{
		enrollResult: enrollment.EnrollResult{
			MonitoringInstanceID: "mi_001",
			BindingStatus:        agentapi.BindingStatusBound,
			SyncToken:            "sync-token-001",
		},
	}
	handler := handlers.AgentEnrollWithOptions(svc, handlers.AgentEndpointOptions{
		RateLimit: handlers.AgentRateLimitOptions{
			MaxRequestsByIP: 1,
			Window:          time.Minute,
		},
	})

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, agentapi.EnrollPath, strings.NewReader(`{"token":"plain-token","fingerprint":"fp-001"}`))
		req.RemoteAddr = "198.51.100.10:12345"
		req.Header.Set("X-Forwarded-For", "203.0.113.77")
		req.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()

		handler.ServeHTTP(recorder, req)

		if i == 0 && recorder.Code != http.StatusOK {
			t.Fatalf("first attempt status = %d, want %d", recorder.Code, http.StatusOK)
		}
		if i == 1 && recorder.Code != http.StatusTooManyRequests {
			t.Fatalf("second attempt status = %d, want %d", recorder.Code, http.StatusTooManyRequests)
		}
	}
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
				HostSampleFrequencyTier:      agentapi.FrequencyTier5m,
				HostSampleMaintenanceContext: true,
				IPQualityPlan: &agentplan.IPQualityPlan{
					Enabled:          true,
					FrequencySeconds: 86400,
					TimeoutSeconds:   15,
					Services:         []string{"netflix", "chatgpt"},
				},
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
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{"monitoring_instance_id":"mi_001","sync_token":"sync-token-001","heartbeats":[{"observed_at":"2026-04-23T08:30:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001","is_backfilled":true}],"command_results":[{"action_id":"act_001","command_id":"uptime","stdout":"up 1 day","stderr":"","exit_code":0}]}`))
	setSyncAuth(req)
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
	if body.Plan.IPQualityPlan == nil {
		t.Fatal("IPQualityPlan = nil, want non-nil")
	}
	if body.Plan.IPQualityPlan.FrequencySeconds != 86400 || body.Plan.IPQualityPlan.Services[1] != "chatgpt" {
		t.Fatalf("IPQualityPlan = %#v, want frequency/services preserved", body.Plan.IPQualityPlan)
	}
	if !body.Plan.HostSampleMaintenanceContext {
		t.Fatal("HostSampleMaintenanceContext = false, want true")
	}
	if len(body.Plan.ProbeAssignments) != 1 {
		t.Fatalf("len(ProbeAssignments) = %d, want 1", len(body.Plan.ProbeAssignments))
	}
	if body.Plan.ProbeAssignments[0].TargetID != "tg_001" {
		t.Fatalf("TargetID = %q, want %q", body.Plan.ProbeAssignments[0].TargetID, "tg_001")
	}

	if svc.syncBatch.MonitoringInstanceID != "mi_001" {
		t.Fatalf("SyncBatch monitoringInstanceID = %q, want %q", svc.syncBatch.MonitoringInstanceID, "mi_001")
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
	if !svc.syncBatch.Heartbeats[0].IsBackfilled {
		t.Fatal("SyncBatch Heartbeats[0].IsBackfilled = false, want true")
	}
	if len(svc.syncBatch.CommandResults) != 1 {
		t.Fatalf("len(SyncBatch.CommandResults) = %d, want 1", len(svc.syncBatch.CommandResults))
	}
	if svc.syncBatch.CommandResults[0].ActionID != "act_001" {
		t.Fatalf("CommandResults[0].ActionID = %q, want act_001", svc.syncBatch.CommandResults[0].ActionID)
	}
	if svc.syncBatch.CommandResults[0].CommandID != "uptime" {
		t.Fatalf("CommandResults[0].CommandID = %q, want uptime", svc.syncBatch.CommandResults[0].CommandID)
	}
}

func TestAgentSyncHandlerAcceptsHeaderTokenWithoutJSONSyncToken(t *testing.T) {
	t.Parallel()

	svc := &fakeAgentSyncService{}
	handler := handlers.AgentSync(svc)
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{"monitoring_instance_id":"mi_001","heartbeats":[{"observed_at":"2026-04-23T08:30:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001"}]}`))
	req.Header.Set("Authorization", "Bearer sync-token-001")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if svc.syncBatch.SyncToken != "sync-token-001" {
		t.Fatalf("SyncBatch syncToken = %q, want header token", svc.syncBatch.SyncToken)
	}
	if svc.syncBatch.MonitoringInstanceID != "mi_001" {
		t.Fatalf("SyncBatch monitoringInstanceID = %q, want mi_001", svc.syncBatch.MonitoringInstanceID)
	}
}

func TestAgentSyncHandlerRejectsMissingAuthorizationBeforeBodyRead(t *testing.T) {
	t.Parallel()

	svc := &fakeAgentSyncService{}
	handler := handlers.AgentSync(svc)
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, errReader{})
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}
	assertErrorResponse(t, recorder, agentapi.ErrorCodeInvalidSyncToken, "invalid sync token")
	if svc.syncBatch.MonitoringInstanceID != "" {
		t.Fatalf("service was called without authorization: %#v", svc.syncBatch)
	}
}

func TestAgentSyncHandlerRejectsJSONOnlySyncToken(t *testing.T) {
	t.Parallel()

	svc := &fakeAgentSyncService{}
	handler := handlers.AgentSync(svc)
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{"monitoring_instance_id":"mi_001","sync_token":"sync-token-001","heartbeats":[{"observed_at":"2026-04-23T08:30:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001"}]}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}
	assertErrorResponse(t, recorder, agentapi.ErrorCodeInvalidSyncToken, "invalid sync token")
	if svc.syncBatch.MonitoringInstanceID != "" {
		t.Fatalf("service was called for json-only sync token: %#v", svc.syncBatch)
	}
}

func TestAgentSyncHandlerWritesObservationBatch(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 23, 9, 0, 0, 0, time.UTC)
	svc := &fakeAgentSyncService{}

	handler := handlers.AgentSync(svc)
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{
		"monitoring_instance_id":"mi_001",
		"sync_token":"sync-token-001",
		"heartbeats":[{"observed_at":"2026-04-23T09:00:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001"}],
		"host_samples":[{"observed_at":"2026-04-23T09:00:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001","cpu_usage_pct":12.5,"load_1":0.2,"load_5":0.3,"load_15":0.4,"mem_used_pct":55.5,"mem_available_bytes":1024,"mem_total_bytes":2048,"swap_used_pct":1.5,"disk_used_pct":45.5,"disk_total_bytes":4096,"inode_used_pct":5.5,"net_in_bytes_per_sec":120,"net_out_bytes_per_sec":220,"cpu_iowait_pct":0.5,"cpu_steal_pct":0.1,"disk_read_bytes_per_sec":320,"disk_write_bytes_per_sec":420,"disk_busy_pct":3.5,"uptime_seconds":3600}],
		"probe_observations":[{"target_id":"tg_001","probe_item_id":"pb_001","probe_kind":"http","observed_at":"2026-04-23T09:00:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001","result_kind":"success","latency_ms":83,"http_status":200}]
	}`))
	setSyncAuth(req)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if svc.syncBatch.Observations.MonitoringInstanceID != "mi_001" {
		t.Fatalf("syncBatch.Observations.MonitoringInstanceID = %q, want %q", svc.syncBatch.Observations.MonitoringInstanceID, "mi_001")
	}
	if len(svc.syncBatch.Observations.HostSamples) != 1 {
		t.Fatalf("len(syncBatch.Observations.HostSamples) = %d, want 1", len(svc.syncBatch.Observations.HostSamples))
	}
	if len(svc.syncBatch.Observations.ProbeObservations) != 1 {
		t.Fatalf("len(syncBatch.Observations.ProbeObservations) = %d, want 1", len(svc.syncBatch.Observations.ProbeObservations))
	}
	if svc.syncBatch.Observations.HostSamples[0].MonitoringInstanceID != "mi_001" {
		t.Fatalf("HostSamples[0].MonitoringInstanceID = %q, want %q", svc.syncBatch.Observations.HostSamples[0].MonitoringInstanceID, "mi_001")
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
	if svc.syncBatch.Observations.HostSamples[0].MemTotalBytes != 2048 {
		t.Fatalf("HostSamples[0].MemTotalBytes = %d, want 2048", svc.syncBatch.Observations.HostSamples[0].MemTotalBytes)
	}
	if svc.syncBatch.Observations.HostSamples[0].DiskTotalBytes != 4096 {
		t.Fatalf("HostSamples[0].DiskTotalBytes = %d, want 4096", svc.syncBatch.Observations.HostSamples[0].DiskTotalBytes)
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

func TestAgentSyncHandlerWritesIPQualityReports(t *testing.T) {
	t.Parallel()

	svc := &fakeAgentSyncService{}

	handler := handlers.AgentSync(svc)
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{
		"monitoring_instance_id":"mi_001",
		"sync_token":"sync-token-001",
		"heartbeats":[{"observed_at":"2026-04-23T09:00:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001"}],
		"ip_quality_reports":[{
			"observed_at":"2026-04-23T09:00:01Z",
			"agent_version":"dev",
			"fingerprint":"fp-001",
			"sync_batch_id":"sync_001",
			"ip_address":"203.0.113.10",
			"ip_version":4,
			"status":"success",
			"asn":"AS64500",
			"organization":"Example Network",
			"use_region_code":"US",
			"risk_level":"low",
			"raw_json":{"Info":{"ASN":"AS64500"},"token":"secret-token"},
			"coverage":{"expected_provider_count":2,"successful_provider_count":1,"failed_provider_count":1,"expected_service_count":1,"successful_service_count":1},
			"diagnostics_json":{"source_version":"v2","secret":"diagnostic-secret"},
			"provider_results":[{"provider":"ipinfo","status":"success","source_type":"default","latency_ms":73,"usage_type":"hosting","company_type":"hosting","risk_level":"low","is_server":true,"is_vpn":false,"extra_json":{"risk":{"score":12},"api_key":"provider-secret"}}],
			"service_unlocks":[{"service":"netflix","source":"netflix_title_probe","status":"unlocked","probe_status":"success","latency_ms":211,"region":"US","unlock_type":"full","extra_json":{"title_probe":"full_catalog","token":"service-secret"}}]
		}]
	}`))
	setSyncAuth(req)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if len(svc.syncBatch.IPQualityReports) != 1 {
		t.Fatalf("len(IPQualityReports) = %d, want 1", len(svc.syncBatch.IPQualityReports))
	}
	report := svc.syncBatch.IPQualityReports[0]
	if report.MonitoringInstanceID != "mi_001" {
		t.Fatalf("MonitoringInstanceID = %q, want mi_001", report.MonitoringInstanceID)
	}
	if report.IPAddress != "203.0.113.10" || report.Status != agentapi.IPQualityStatusSuccess {
		t.Fatalf("report identity = %#v, want ip/status preserved", report)
	}
	if len(report.ProviderResults) != 1 || report.ProviderResults[0].Provider != "ipinfo" {
		t.Fatalf("ProviderResults = %#v, want ipinfo result", report.ProviderResults)
	}
	if report.CoverageJSON == nil || !strings.Contains(string(report.CoverageJSON), `"expected_provider_count":2`) {
		t.Fatalf("CoverageJSON = %s, want coverage preserved", report.CoverageJSON)
	}
	if strings.Contains(string(report.DiagnosticsJSON), "diagnostic-secret") || !strings.Contains(string(report.DiagnosticsJSON), `"secret":"[redacted]"`) {
		t.Fatalf("DiagnosticsJSON = %s, want sanitized diagnostics", report.DiagnosticsJSON)
	}
	if report.ProviderResults[0].Status != "success" || report.ProviderResults[0].SourceType != "default" ||
		report.ProviderResults[0].LatencyMS == nil || *report.ProviderResults[0].LatencyMS != 73 {
		t.Fatalf("ProviderResults[0] source fields = %#v, want source metadata", report.ProviderResults[0])
	}
	if report.ProviderResults[0].IsVPN == nil || *report.ProviderResults[0].IsVPN {
		t.Fatalf("ProviderResults[0].IsVPN = %#v, want false pointer", report.ProviderResults[0].IsVPN)
	}
	if strings.Contains(string(report.ProviderResults[0].ExtraJSON), "provider-secret") ||
		!strings.Contains(string(report.ProviderResults[0].ExtraJSON), `"api_key":"[redacted]"`) {
		t.Fatalf("ProviderResults[0].ExtraJSON = %s, want sanitized extra JSON", report.ProviderResults[0].ExtraJSON)
	}
	if len(report.ServiceUnlocks) != 1 || report.ServiceUnlocks[0].Service != "netflix" {
		t.Fatalf("ServiceUnlocks = %#v, want netflix result", report.ServiceUnlocks)
	}
	if report.ServiceUnlocks[0].Source != "netflix_title_probe" || report.ServiceUnlocks[0].ProbeStatus != "success" ||
		report.ServiceUnlocks[0].LatencyMS == nil || *report.ServiceUnlocks[0].LatencyMS != 211 {
		t.Fatalf("ServiceUnlocks[0] source fields = %#v, want source metadata", report.ServiceUnlocks[0])
	}
	if strings.Contains(string(report.ServiceUnlocks[0].ExtraJSON), "service-secret") ||
		!strings.Contains(string(report.ServiceUnlocks[0].ExtraJSON), `"token":"[redacted]"`) {
		t.Fatalf("ServiceUnlocks[0].ExtraJSON = %s, want sanitized extra JSON", report.ServiceUnlocks[0].ExtraJSON)
	}
	rawJSON := string(report.RawJSON)
	if strings.Contains(rawJSON, "secret-token") {
		t.Fatalf("RawJSON leaked secret: %s", rawJSON)
	}
	if !strings.Contains(rawJSON, `"token":"[redacted]"`) || !strings.Contains(rawJSON, `"Info":{"ASN":"AS64500"}`) {
		t.Fatalf("RawJSON = %s, want sanitized raw JSON preserved for store layer", report.RawJSON)
	}
}

func TestAgentSyncHandlerRejectsInvalidIPQualityReport(t *testing.T) {
	t.Parallel()

	handler := handlers.AgentSync(&fakeAgentSyncService{})
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{
		"monitoring_instance_id":"mi_001",
		"sync_token":"sync-token-001",
		"heartbeats":[{"observed_at":"2026-04-23T09:00:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001"}],
		"ip_quality_reports":[{"observed_at":"2026-04-23T09:00:01Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001","ip_address":"","ip_version":4,"status":"success"}]
	}`))
	setSyncAuth(req)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	assertErrorResponse(t, recorder, agentapi.ErrorCodeInvalidRequest, "invalid request")
}

func TestAgentSyncHandlerRejectsInvalidIPQualityReportEnums(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		report string
	}{
		{
			name:   "report status",
			report: `"status":"done","provider_results":[{"provider":"ipinfo"}]`,
		},
		{
			name:   "provider status",
			report: `"status":"success","provider_results":[{"provider":"ipinfo","status":"done"}]`,
		},
		{
			name:   "provider source type",
			report: `"status":"success","provider_results":[{"provider":"ipinfo","source_type":"demo"}]`,
		},
		{
			name:   "service status",
			report: `"status":"success","service_unlocks":[{"service":"netflix","status":"allowed"}]`,
		},
		{
			name:   "service probe status",
			report: `"status":"success","service_unlocks":[{"service":"netflix","status":"unknown","probe_status":"done"}]`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := handlers.AgentSync(&fakeAgentSyncService{})
			body := `{
				"monitoring_instance_id":"mi_001",
				"sync_token":"sync-token-001",
				"heartbeats":[{"observed_at":"2026-04-23T09:00:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001"}],
				"ip_quality_reports":[{
					"observed_at":"2026-04-23T09:00:01Z",
					"agent_version":"dev",
					"fingerprint":"fp-001",
					"sync_batch_id":"sync_001",
					"ip_address":"203.0.113.10",
					"ip_version":4,
					` + tc.report + `
				}]
			}`
			req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(body))
			setSyncAuth(req)
			req.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
			}
			assertErrorResponse(t, recorder, agentapi.ErrorCodeInvalidRequest, "invalid request")
		})
	}
}

func TestAgentSyncHandlerDoesNotReturn200WhenObservationIngestFails(t *testing.T) {
	t.Parallel()

	svc := &fakeAgentSyncService{syncErr: observations.ErrInvalidProbeObservation}

	handler := handlers.AgentSync(svc)
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{
		"monitoring_instance_id":"mi_001",
		"sync_token":"sync-token-001",
		"heartbeats":[{"observed_at":"2026-04-23T09:00:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001"}],
		"probe_observations":[{"target_id":"tg_bad","probe_item_id":"pb_001","probe_kind":"http","observed_at":"2026-04-23T09:00:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001","result_kind":"success","latency_ms":83,"http_status":200}]
	}`))
	setSyncAuth(req)
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
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{"monitoring_instance_id":"mi_001","sync_token":"bad-token","heartbeats":[{"observed_at":"2026-04-23T08:30:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001"}]}`))
	req.Header.Set("Authorization", "Bearer bad-token")
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
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{"monitoring_instance_id":"mi_001","sync_token":"sync-token-001","heartbeats":[{"observed_at":"2026-04-23T08:30:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001"}]}`))
	setSyncAuth(req)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}

	assertErrorResponse(t, recorder, agentapi.ErrorCodeBindingNotAccepted, "binding not accepted")
}

func TestAgentSyncHandlerReturnsStableInternalErrorWithoutStoreDetails(t *testing.T) {
	t.Parallel()

	const privateStoreDetail = "permission denied for table agent_sync_batches; sync-token-fixture; raw-fingerprint-fixture"
	svc := &fakeAgentSyncService{syncErr: errors.New(privateStoreDetail)}
	handler := handlers.AgentSync(svc)
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{"monitoring_instance_id":"mi_001","heartbeats":[{"observed_at":"2026-04-23T08:30:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001"}]}`))
	setSyncAuth(req)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	assertErrorResponse(t, recorder, agentapi.ErrorCodeInternalError, "internal server error")
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal internal error envelope: %v", err)
	}
	if len(envelope) != 2 || envelope["code"] == nil || envelope["message"] == nil {
		t.Fatalf("internal error envelope keys = %#v, want only code and message", envelope)
	}
	responseSurface := recorder.Body.String()
	for name, values := range recorder.Header() {
		responseSurface += name + ":" + strings.Join(values, ",") + "\n"
	}
	for _, privateFragment := range []string{
		privateStoreDetail,
		"permission denied for table agent_sync_batches",
		"sync-token-fixture",
		"raw-fingerprint-fixture",
		"sync-token-001",
		"fp-001",
	} {
		if strings.Contains(responseSurface, privateFragment) {
			t.Fatalf("agent sync error response exposed private store detail %q", privateFragment)
		}
	}
}

func TestAgentSyncHandlerRejectsEmptyMonitoringInstanceID(t *testing.T) {
	t.Parallel()

	handler := handlers.AgentSync(&fakeAgentSyncService{})
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{"monitoring_instance_id":"","sync_token":"sync-token-001","heartbeats":[{"observed_at":"2026-04-23T08:30:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001"}]}`))
	setSyncAuth(req)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}

	assertErrorResponse(t, recorder, agentapi.ErrorCodeInvalidRequest, "invalid request")
}

func TestAgentSyncHandlerRejectsOversizedBodyBeforeService(t *testing.T) {
	t.Parallel()

	svc := &fakeAgentSyncService{}
	handler := handlers.AgentSync(svc)
	body := `{"monitoring_instance_id":"mi_001","sync_token":"sync-token-001","heartbeats":[{"observed_at":"2026-04-23T08:30:00Z","agent_version":"` + strings.Repeat("x", handlers.AgentSyncBodyLimit) + `","fingerprint":"fp-001","sync_batch_id":"sync_001"}]}`
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(body))
	setSyncAuth(req)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	assertErrorResponse(t, recorder, agentapi.ErrorCodeInvalidJSON, "invalid json")
	if svc.syncBatch.MonitoringInstanceID != "" {
		t.Fatalf("service was called for oversized body: %#v", svc.syncBatch)
	}
}

func TestAgentSyncHandlerRateLimitsByClientIP(t *testing.T) {
	t.Parallel()

	svc := &fakeAgentSyncService{}
	handler := handlers.AgentSyncWithOptions(svc, handlers.AgentEndpointOptions{
		RateLimit: handlers.AgentRateLimitOptions{
			MaxRequestsByIP: 1,
			Window:          time.Minute,
		},
	})

	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{"monitoring_instance_id":"mi_001","sync_token":"sync-token-001","heartbeats":[{"observed_at":"2026-04-23T08:30:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001"}]}`))
	req.RemoteAddr = "198.51.100.20:12345"
	setSyncAuth(req)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("first attempt status = %d, want %d", recorder.Code, http.StatusOK)
	}

	req = httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{"monitoring_instance_id":"mi_001","sync_token":"sync-token-001","heartbeats":[{"observed_at":"2026-04-23T08:31:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_002"}]}`))
	req.RemoteAddr = "198.51.100.20:12345"
	setSyncAuth(req)
	req.Header.Set("Content-Type", "application/json")
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("second attempt status = %d, want %d", recorder.Code, http.StatusTooManyRequests)
	}
	assertErrorResponse(t, recorder, agentapi.ErrorCodeInvalidRequest, "too many requests")
}

func TestAgentSyncHandlerRejectsMalformedHeaderTokenBeforeBodyRead(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		authorization string
	}{
		{name: "token contains spaces", authorization: "Bearer bad token with spaces"},
		{name: "token has leading whitespace", authorization: "Bearer  sync-token-001"},
		{name: "token has trailing whitespace", authorization: "Bearer sync-token-001 "},
		{name: "oversized token", authorization: "Bearer " + strings.Repeat("x", 513)},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := handlers.AgentSync(&fakeAgentSyncService{})
			req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, errReader{})
			req.Header.Set("Authorization", tt.authorization)
			recorder := httptest.NewRecorder()

			handler.ServeHTTP(recorder, req)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
			}
			assertErrorResponse(t, recorder, agentapi.ErrorCodeInvalidSyncToken, "invalid sync token")
		})
	}
}

func TestAgentSyncHandlerLimitsInflightRequestsBeforeBodyRead(t *testing.T) {
	t.Parallel()

	svc := &blockingAgentSyncService{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	handler := handlers.AgentSyncWithOptions(svc, handlers.AgentEndpointOptions{
		RateLimit: handlers.AgentRateLimitOptions{
			MaxRequestsByIP:   100,
			MaxRequestsGlobal: 100,
			MaxSyncInflight:   1,
			Window:            time.Minute,
		},
	})
	firstReq := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{"monitoring_instance_id":"mi_001","sync_token":"sync-token-001","heartbeats":[{"observed_at":"2026-04-23T08:30:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001"}]}`))
	setSyncAuth(firstReq)
	firstReq.Header.Set("Content-Type", "application/json")
	firstRecorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(firstRecorder, firstReq)
		close(done)
	}()
	<-svc.entered

	secondReq := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, errReader{})
	setSyncAuth(secondReq)
	secondReq.Header.Set("Content-Type", "application/json")
	secondRecorder := httptest.NewRecorder()
	handler.ServeHTTP(secondRecorder, secondReq)

	close(svc.release)
	<-done

	if secondRecorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", secondRecorder.Code, http.StatusServiceUnavailable)
	}
	assertErrorResponse(t, secondRecorder, agentapi.ErrorCodeInvalidRequest, "service unavailable")
}

func TestAgentSyncHandlerRejectsTooManyHeartbeatsBeforeService(t *testing.T) {
	t.Parallel()

	svc := &fakeAgentSyncService{}
	handler := handlers.AgentSync(svc)
	heartbeats := make([]string, 0, 257)
	for i := 0; i < 257; i++ {
		heartbeats = append(heartbeats, `{"observed_at":"2026-04-23T08:30:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001"}`)
	}
	body := `{"monitoring_instance_id":"mi_001","sync_token":"sync-token-001","heartbeats":[` + strings.Join(heartbeats, ",") + `]}`
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(body))
	setSyncAuth(req)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	assertErrorResponse(t, recorder, agentapi.ErrorCodeInvalidRequest, "invalid request")
	if svc.syncBatch.MonitoringInstanceID != "" {
		t.Fatalf("service was called for oversized batch: %#v", svc.syncBatch)
	}
}

func TestAgentSyncHandlerRejectsOverlongIdentityStringBeforeService(t *testing.T) {
	t.Parallel()

	svc := &fakeAgentSyncService{}
	handler := handlers.AgentSync(svc)
	body := `{"monitoring_instance_id":"mi_001","sync_token":"sync-token-001","heartbeats":[{"observed_at":"2026-04-23T08:30:00Z","agent_version":"dev","fingerprint":"` + strings.Repeat("x", 257) + `","sync_batch_id":"sync_001"}]}`
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(body))
	setSyncAuth(req)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	assertErrorResponse(t, recorder, agentapi.ErrorCodeInvalidRequest, "invalid request")
	if svc.syncBatch.MonitoringInstanceID != "" {
		t.Fatalf("service was called for overlong string: %#v", svc.syncBatch)
	}
}

func TestAgentSyncHandlerRejectsEmptyBearerToken(t *testing.T) {
	t.Parallel()

	handler := handlers.AgentSync(&fakeAgentSyncService{})
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, errReader{})
	req.Header.Set("Authorization", "Bearer ")
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}

	assertErrorResponse(t, recorder, agentapi.ErrorCodeInvalidSyncToken, "invalid sync token")
}

func TestAgentSyncHandlerRejectsHeartbeatMissingSyncBatchID(t *testing.T) {
	t.Parallel()

	handler := handlers.AgentSync(&fakeAgentSyncService{})
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{"monitoring_instance_id":"mi_001","sync_token":"sync-token-001","heartbeats":[{"observed_at":"2026-04-23T08:30:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":""}]}`))
	setSyncAuth(req)
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
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{"monitoring_instance_id":"mi_001","sync_token":"sync-token-001","heartbeats":[{"observed_at":"0001-01-01T00:00:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001"}]}`))
	setSyncAuth(req)
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
