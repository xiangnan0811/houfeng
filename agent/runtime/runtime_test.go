package runtime_test

import (
	"context"
	"errors"
	"testing"
	"time"

	agentconfig "houfeng/agent/config"
	agentruntime "houfeng/agent/runtime"
	"houfeng/internal/contracts/agentapi"
)

type fakeClient struct {
	enrollCalls      int
	syncCalls        int
	syncBeforeEnroll bool
	enrollResponse   agentapi.EnrollmentResponse

	lastEnroll agentapi.EnrollmentRequest
	lastSync   agentapi.SyncRequest
}

func (f *fakeClient) Enroll(_ context.Context, request agentapi.EnrollmentRequest) (*agentapi.EnrollmentResponse, error) {
	f.enrollCalls++
	f.lastEnroll = request
	response := f.enrollResponse
	if response.NodeID == "" {
		response.NodeID = "node-123"
	}
	if response.Status == "" {
		response.Status = "accepted"
	}
	if response.BindingStatus == "" {
		response.BindingStatus = agentapi.BindingStatusBound
	}
	return &response, nil
}

func (f *fakeClient) Sync(_ context.Context, request agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
	if f.enrollCalls == 0 {
		f.syncBeforeEnroll = true
	}
	f.syncCalls++
	f.lastSync = request
	return &agentapi.SyncResponse{AcceptedAt: time.Now().UTC(), Status: "ok"}, nil
}

type staticTokenSource struct{}

func (staticTokenSource) Token(context.Context) (string, error) {
	return "plain-token", nil
}

type staticFingerprint struct{}

func (staticFingerprint) Fingerprint(context.Context) (string, error) {
	return "fp-001", nil
}

func TestRuntimeEnrollsBeforeSyncLoop(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	client := &fakeClient{}
	rt := agentruntime.NewWithDeps(cfg, nil, client, staticTokenSource{}, staticFingerprint{}, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if client.enrollCalls == 0 {
		t.Fatal("Enroll() was not called")
	}
	if client.syncCalls == 0 {
		t.Fatal("Sync() was not called")
	}
	if client.syncBeforeEnroll {
		t.Fatal("Sync() was called before Enroll()")
	}
	if client.lastEnroll.Token != "plain-token" {
		t.Fatalf("Enroll token = %q, want %q", client.lastEnroll.Token, "plain-token")
	}
	if client.lastEnroll.Fingerprint != "fp-001" {
		t.Fatalf("Enroll fingerprint = %q, want %q", client.lastEnroll.Fingerprint, "fp-001")
	}
	if client.lastSync.NodeID != "node-123" {
		t.Fatalf("Sync node_id = %q, want %q", client.lastSync.NodeID, "node-123")
	}
	if len(client.lastSync.Heartbeats) == 0 {
		t.Fatal("Sync heartbeats = 0, want > 0")
	}
	if client.lastSync.Heartbeats[0].Fingerprint != "fp-001" {
		t.Fatalf("Sync fingerprint = %q, want %q", client.lastSync.Heartbeats[0].Fingerprint, "fp-001")
	}
	if client.lastSync.Heartbeats[0].AgentVersion != "dev" {
		t.Fatalf("Sync agent_version = %q, want %q", client.lastSync.Heartbeats[0].AgentVersion, "dev")
	}
}

func TestRuntimeReturnsEnrollmentNotBoundErrorWithoutStartingSyncLoop(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	client := &fakeClient{
		enrollResponse: agentapi.EnrollmentResponse{
			NodeID:        "node-123",
			Status:        "accepted",
			BindingStatus: agentapi.BindingStatusPendingConfirmation,
		},
	}
	rt := agentruntime.NewWithDeps(cfg, nil, client, staticTokenSource{}, staticFingerprint{}, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	err := rt.Run(ctx)
	if err == nil {
		t.Fatal("Run() error = nil, want non-nil")
	}

	var enrollmentErr *agentruntime.EnrollmentNotBoundError
	if !errors.As(err, &enrollmentErr) {
		t.Fatalf("Run() error = %T, want *runtime.EnrollmentNotBoundError", err)
	}
	if enrollmentErr.BindingStatus != agentapi.BindingStatusPendingConfirmation {
		t.Fatalf("BindingStatus = %q, want %q", enrollmentErr.BindingStatus, agentapi.BindingStatusPendingConfirmation)
	}
	if client.enrollCalls != 1 {
		t.Fatalf("Enroll() calls = %d, want %d", client.enrollCalls, 1)
	}
	if client.syncCalls != 0 {
		t.Fatalf("Sync() calls = %d, want %d", client.syncCalls, 0)
	}
}
