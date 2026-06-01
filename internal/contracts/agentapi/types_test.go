package agentapi_test

import (
	"encoding/json"
	"testing"
	"time"

	"houfeng/internal/contracts/agentapi"
)

func TestEnrollmentResponseRoundTrip(t *testing.T) {
	original := agentapi.EnrollmentResponse{
		MonitoringInstanceID: "nd-local-01",
		BindingStatus:        agentapi.BindingStatusPendingConfirmation,
		Status:               "accepted",
		SyncToken:            "sync-token-001",
	}

	payload, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal enrollment response: %v", err)
	}

	var roundTrip agentapi.EnrollmentResponse
	if err := json.Unmarshal(payload, &roundTrip); err != nil {
		t.Fatalf("unmarshal enrollment response: %v", err)
	}

	if roundTrip.MonitoringInstanceID != "nd-local-01" {
		t.Fatalf("MonitoringInstanceID = %q, want %q", roundTrip.MonitoringInstanceID, "nd-local-01")
	}

	if roundTrip.BindingStatus != agentapi.BindingStatusPendingConfirmation {
		t.Fatalf("BindingStatus = %q, want %q", roundTrip.BindingStatus, agentapi.BindingStatusPendingConfirmation)
	}

	if roundTrip.Status != "accepted" {
		t.Fatalf("Status = %q, want %q", roundTrip.Status, "accepted")
	}
	if roundTrip.SyncToken != "sync-token-001" {
		t.Fatalf("SyncToken = %q, want %q", roundTrip.SyncToken, "sync-token-001")
	}
}

func TestEnrollmentResponseOmitsEmptySyncToken(t *testing.T) {
	payload, err := json.Marshal(agentapi.EnrollmentResponse{MonitoringInstanceID: "nd-local-01", Status: "accepted"})
	if err != nil {
		t.Fatalf("marshal enrollment response: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal enrollment response payload: %v", err)
	}

	if _, ok := got["sync_token"]; ok {
		t.Fatalf("payload unexpectedly included sync_token: %s", payload)
	}
}

func TestErrorResponseRoundTrip(t *testing.T) {
	original := agentapi.ErrorResponse{
		Code:    agentapi.ErrorCodeInvalidSyncToken,
		Message: "invalid sync token",
	}

	payload, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal error response: %v", err)
	}

	var roundTrip agentapi.ErrorResponse
	if err := json.Unmarshal(payload, &roundTrip); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}

	if roundTrip.Code != agentapi.ErrorCodeInvalidSyncToken {
		t.Fatalf("Code = %q, want %q", roundTrip.Code, agentapi.ErrorCodeInvalidSyncToken)
	}
	if roundTrip.Message != "invalid sync token" {
		t.Fatalf("Message = %q, want %q", roundTrip.Message, "invalid sync token")
	}
}

func TestEnrollmentRequestOmitsDeprecatedTransportFields(t *testing.T) {
	original := agentapi.EnrollmentRequest{
		Token:       "plain-token",
		Fingerprint: "fp-001",
	}

	payload, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal enrollment request: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if got["token"] != "plain-token" {
		t.Fatalf("token = %v, want %q", got["token"], "plain-token")
	}
	if got["fingerprint"] != "fp-001" {
		t.Fatalf("fingerprint = %v, want %q", got["fingerprint"], "fp-001")
	}
	if _, ok := got["node_name"]; ok {
		t.Fatalf("payload unexpectedly included node_name: %s", payload)
	}
	if _, ok := got["agent_version"]; ok {
		t.Fatalf("payload unexpectedly included agent_version: %s", payload)
	}
}

func TestSyncRequestRoundTrip(t *testing.T) {
	observedAt, err := time.Parse(time.RFC3339, "2026-04-22T12:00:00Z")
	if err != nil {
		t.Fatalf("parse observedAt: %v", err)
	}

	original := agentapi.SyncRequest{
		MonitoringInstanceID: "nd-local-01",
		SyncToken:            "sync-token-001",
		Heartbeats: []agentapi.MonitoringInstanceHeartbeat{{
			ObservedAt:   observedAt,
			AgentVersion: "dev",
			Fingerprint:  "fp-001",
			SyncBatchID:  "batch-001",
			IsBackfilled: true,
		}},
	}

	payload, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal sync request: %v", err)
	}

	var roundTrip agentapi.SyncRequest
	if err := json.Unmarshal(payload, &roundTrip); err != nil {
		t.Fatalf("unmarshal sync request: %v", err)
	}

	if roundTrip.MonitoringInstanceID != "nd-local-01" {
		t.Fatalf("MonitoringInstanceID = %q, want %q", roundTrip.MonitoringInstanceID, "nd-local-01")
	}
	if roundTrip.SyncToken != "sync-token-001" {
		t.Fatalf("SyncToken = %q, want %q", roundTrip.SyncToken, "sync-token-001")
	}

	if len(roundTrip.Heartbeats) != 1 {
		t.Fatalf("len(Heartbeats) = %d, want %d", len(roundTrip.Heartbeats), 1)
	}

	if roundTrip.Heartbeats[0].Fingerprint != "fp-001" {
		t.Fatalf("Fingerprint = %q, want %q", roundTrip.Heartbeats[0].Fingerprint, "fp-001")
	}
	if !roundTrip.Heartbeats[0].IsBackfilled {
		t.Fatal("Heartbeats[0].IsBackfilled = false, want true")
	}
}

func TestSyncRequestOmitsEmptyHeartbeats(t *testing.T) {
	payload, err := json.Marshal(agentapi.SyncRequest{MonitoringInstanceID: "nd-local-01", SyncToken: "sync-token-001"})
	if err != nil {
		t.Fatalf("marshal sync request: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if got["sync_token"] != "sync-token-001" {
		t.Fatalf("sync_token = %v, want %q", got["sync_token"], "sync-token-001")
	}
	if _, ok := got["heartbeats"]; ok {
		t.Fatalf("payload unexpectedly included heartbeats: %s", payload)
	}
}
