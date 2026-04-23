package runtime_test

import (
	"context"
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

	lastEnroll agentapi.EnrollmentRequest
	lastSync   agentapi.SyncRequest
}

func (f *fakeClient) Enroll(_ context.Context, request agentapi.EnrollmentRequest) (*agentapi.EnrollmentResponse, error) {
	f.enrollCalls++
	f.lastEnroll = request
	return &agentapi.EnrollmentResponse{NodeID: "node-123", Status: "accepted"}, nil
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
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token", NodeName: "nd-local-01"}
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
}
