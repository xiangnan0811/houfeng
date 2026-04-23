package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/enrollment"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/contracts/agentapi"
)

type fakeAgentEnrollmentService struct {
	enrollResult enrollment.EnrollResult
	enrollErr    error
	enrollInput  enrollment.EnrollInput

	syncErr   error
	syncInput enrollment.SyncInput
}

func (f *fakeAgentEnrollmentService) EnrollNode(_ context.Context, input enrollment.EnrollInput) (enrollment.EnrollResult, error) {
	f.enrollInput = input
	if f.enrollErr != nil {
		return enrollment.EnrollResult{}, f.enrollErr
	}
	return f.enrollResult, nil
}

func (f *fakeAgentEnrollmentService) RecordHeartbeatSync(_ context.Context, input enrollment.SyncInput) error {
	f.syncInput = input
	return f.syncErr
}

func TestAgentEnrollHandlerReturnsBindingStatus(t *testing.T) {
	t.Parallel()

	svc := &fakeAgentEnrollmentService{
		enrollResult: enrollment.EnrollResult{
			NodeID:        "nd_001",
			BindingStatus: agentapi.BindingStatusBound,
		},
	}

	handler := handlers.AgentEnroll(svc)
	req := httptest.NewRequest(http.MethodPost, agentapi.EnrollPath, strings.NewReader(`{"node_name":"nd-local-01","token":"plain-token","fingerprint":"fp-001","agent_version":"dev"}`))
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

func TestAgentSyncHandlerReturnsAcceptedAt(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.April, 23, 8, 30, 0, 0, time.UTC)
	svc := &fakeAgentEnrollmentService{}

	handler := handlers.AgentSync(svc)
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{"node_id":"nd_001","heartbeats":[{"observed_at":"2026-04-23T08:30:00Z","agent_version":"dev","fingerprint":"fp-001","sync_batch_id":"sync_001"}]}`))
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

	if body.AcceptedAt.IsZero() {
		t.Fatal("AcceptedAt is zero, want non-zero")
	}
	if body.Status != "accepted" {
		t.Fatalf("Status = %q, want %q", body.Status, "accepted")
	}

	if svc.syncInput.NodeID != "nd_001" {
		t.Fatalf("RecordHeartbeatSync nodeID = %q, want %q", svc.syncInput.NodeID, "nd_001")
	}
	if len(svc.syncInput.Heartbeats) != 1 {
		t.Fatalf("RecordHeartbeatSync heartbeats = %d, want 1", len(svc.syncInput.Heartbeats))
	}
	if svc.syncInput.Heartbeats[0].ObservedAt != observedAt {
		t.Fatalf("ObservedAt = %s, want %s", svc.syncInput.Heartbeats[0].ObservedAt.Format(time.RFC3339), observedAt.Format(time.RFC3339))
	}
	if svc.syncInput.Heartbeats[0].AgentVersion != "dev" {
		t.Fatalf("AgentVersion = %q, want %q", svc.syncInput.Heartbeats[0].AgentVersion, "dev")
	}
	if svc.syncInput.Heartbeats[0].Fingerprint != "fp-001" {
		t.Fatalf("Fingerprint = %q, want %q", svc.syncInput.Heartbeats[0].Fingerprint, "fp-001")
	}
	if svc.syncInput.Heartbeats[0].SyncBatchID != "sync_001" {
		t.Fatalf("SyncBatchID = %q, want %q", svc.syncInput.Heartbeats[0].SyncBatchID, "sync_001")
	}
}

func TestAgentSyncHandlerReturnsBindingNotAcceptedError(t *testing.T) {
	t.Parallel()

	svc := &fakeAgentEnrollmentService{syncErr: enrollment.ErrBindingNotAccepted}

	handler := handlers.AgentSync(svc)
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{"node_id":"nd_001","heartbeats":[]}`))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}

	assertErrorResponse(t, recorder, agentapi.ErrorCodeBindingNotAccepted, "binding not accepted")
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
