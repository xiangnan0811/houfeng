package enroll_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"houfeng/agent/enroll"
	"houfeng/internal/contracts/agentapi"
)

func TestClientEnrollReturnsDecodedPointer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != agentapi.EnrollPath {
			t.Fatalf("path = %q, want %q", r.URL.Path, agentapi.EnrollPath)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal request body: %v", err)
		}
		if payload["token"] != "plain-token" {
			t.Fatalf("token = %v, want %q", payload["token"], "plain-token")
		}
		if payload["fingerprint"] != "fp-001" {
			t.Fatalf("fingerprint = %v, want %q", payload["fingerprint"], "fp-001")
		}
		if _, ok := payload["node_name"]; ok {
			t.Fatalf("request unexpectedly included node_name: %s", body)
		}
		if _, ok := payload["agent_version"]; ok {
			t.Fatalf("request unexpectedly included agent_version: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(agentapi.EnrollmentResponse{
			MonitoringInstanceID: "nd-local-01",
			BindingStatus:        agentapi.BindingStatusPendingConfirmation,
			Status:               "accepted",
			SyncToken:            "sync-token-001",
		})
	}))
	defer ts.Close()

	client := enroll.NewClient(ts.URL)
	response, err := client.Enroll(context.Background(), agentapi.EnrollmentRequest{
		Token:       "plain-token",
		Fingerprint: "fp-001",
	})
	if err != nil {
		t.Fatalf("Enroll() error = %v", err)
	}
	if response == nil {
		t.Fatal("Enroll() response = nil, want non-nil")
	}
	if response.MonitoringInstanceID != "nd-local-01" {
		t.Fatalf("MonitoringInstanceID = %q, want %q", response.MonitoringInstanceID, "nd-local-01")
	}
	if response.BindingStatus != agentapi.BindingStatusPendingConfirmation {
		t.Fatalf("BindingStatus = %q, want %q", response.BindingStatus, agentapi.BindingStatusPendingConfirmation)
	}
	if response.SyncToken != "sync-token-001" {
		t.Fatalf("SyncToken = %q, want %q", response.SyncToken, "sync-token-001")
	}
}

func TestClientEnrollReturnsRemoteErrorWithCode(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != agentapi.EnrollPath {
			t.Fatalf("path = %q, want %q", r.URL.Path, agentapi.EnrollPath)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(agentapi.ErrorResponse{
			Code:    agentapi.ErrorCodeInvalidEnrollmentToken,
			Message: "invalid enrollment token",
		})
	}))
	defer ts.Close()

	client := enroll.NewClient(ts.URL)
	_, err := client.Enroll(context.Background(), agentapi.EnrollmentRequest{
		Token:       "plain-token",
		Fingerprint: "fp-001",
	})
	if err == nil {
		t.Fatal("Enroll() error = nil, want non-nil")
	}

	var remoteErr *enroll.RemoteError
	if !errors.As(err, &remoteErr) {
		t.Fatalf("Enroll() error = %T, want *enroll.RemoteError", err)
	}
	if remoteErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("StatusCode = %d, want %d", remoteErr.StatusCode, http.StatusUnauthorized)
	}
	if remoteErr.Code != agentapi.ErrorCodeInvalidEnrollmentToken {
		t.Fatalf("Code = %q, want %q", remoteErr.Code, agentapi.ErrorCodeInvalidEnrollmentToken)
	}
	if remoteErr.Message != "invalid enrollment token" {
		t.Fatalf("Message = %q, want %q", remoteErr.Message, "invalid enrollment token")
	}
}

func TestClientSyncReturnsDecodedPointer(t *testing.T) {
	acceptedAt, err := time.Parse(time.RFC3339, "2026-04-23T00:00:00Z")
	if err != nil {
		t.Fatalf("parse acceptedAt: %v", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != agentapi.SyncPath {
			t.Fatalf("path = %q, want %q", r.URL.Path, agentapi.SyncPath)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sync-token-001" {
			t.Fatalf("Authorization = %q, want bearer sync token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(agentapi.SyncResponse{
			AcceptedAt: acceptedAt,
			Status:     "ok",
			Plan: &agentapi.SyncPlan{
				HostSampleFrequencyTier: agentapi.FrequencyTier5m,
				ProbeAssignments: []agentapi.ProbeAssignment{{
					TargetID:       "tg_001",
					TargetHost:     "api.example.test",
					ProbeItemID:    "pb_001",
					ProbeKind:      agentapi.ProbeKindHTTP,
					FrequencyTier:  agentapi.FrequencyTier1m,
					TimeoutSeconds: 5,
					Config:         json.RawMessage(`{"path":"/healthz","method":"GET","expected_status_range":[200,299]}`),
				}},
			},
		})
	}))
	defer ts.Close()

	client := enroll.NewClient(ts.URL)
	response, err := client.Sync(context.Background(), agentapi.SyncRequest{MonitoringInstanceID: "nd-local-01", SyncToken: "sync-token-001"})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if response == nil {
		t.Fatal("Sync() response = nil, want non-nil")
	}
	if response.Status != "ok" {
		t.Fatalf("Status = %q, want %q", response.Status, "ok")
	}
	if response.Plan == nil {
		t.Fatal("Plan = nil, want non-nil")
	}
	if response.Plan.HostSampleFrequencyTier != agentapi.FrequencyTier5m {
		t.Fatalf("HostSampleFrequencyTier = %q, want %q", response.Plan.HostSampleFrequencyTier, agentapi.FrequencyTier5m)
	}
	if len(response.Plan.ProbeAssignments) != 1 {
		t.Fatalf("len(ProbeAssignments) = %d, want 1", len(response.Plan.ProbeAssignments))
	}
}
