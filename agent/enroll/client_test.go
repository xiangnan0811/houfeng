package enroll_test

import (
	"context"
	"encoding/json"
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
