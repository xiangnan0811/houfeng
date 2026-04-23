package enroll_test

import (
	"context"
	"encoding/json"
	"errors"
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
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(agentapi.EnrollmentResponse{
			NodeID:        "nd-local-01",
			BindingStatus: agentapi.BindingStatusPendingConfirmation,
			Status:        "accepted",
		})
	}))
	defer ts.Close()

	client := enroll.NewClient(ts.URL)
	response, err := client.Enroll(context.Background(), agentapi.EnrollmentRequest{NodeName: "nd-local-01"})
	if err != nil {
		t.Fatalf("Enroll() error = %v", err)
	}
	if response == nil {
		t.Fatal("Enroll() response = nil, want non-nil")
	}
	if response.NodeID != "nd-local-01" {
		t.Fatalf("NodeID = %q, want %q", response.NodeID, "nd-local-01")
	}
	if response.BindingStatus != agentapi.BindingStatusPendingConfirmation {
		t.Fatalf("BindingStatus = %q, want %q", response.BindingStatus, agentapi.BindingStatusPendingConfirmation)
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
	_, err := client.Enroll(context.Background(), agentapi.EnrollmentRequest{NodeName: "nd-local-01"})
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
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(agentapi.SyncResponse{AcceptedAt: acceptedAt, Status: "ok"})
	}))
	defer ts.Close()

	client := enroll.NewClient(ts.URL)
	response, err := client.Sync(context.Background(), agentapi.SyncRequest{NodeID: "nd-local-01"})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if response == nil {
		t.Fatal("Sync() response = nil, want non-nil")
	}
	if response.Status != "ok" {
		t.Fatalf("Status = %q, want %q", response.Status, "ok")
	}
}
