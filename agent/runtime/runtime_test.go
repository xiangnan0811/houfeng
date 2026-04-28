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
	enrollCalls         int
	syncCalls           int
	syncBeforeEnroll    bool
	enrollResponse      agentapi.EnrollmentResponse
	forceEmptySyncToken bool
	syncResponses       []agentapi.SyncResponse

	lastEnroll   agentapi.EnrollmentRequest
	lastSync     agentapi.SyncRequest
	syncRequests []agentapi.SyncRequest
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
	if !f.forceEmptySyncToken && response.SyncToken == "" && response.BindingStatus == agentapi.BindingStatusBound {
		response.SyncToken = "sync-token-001"
	}
	return &response, nil
}

func (f *fakeClient) Sync(_ context.Context, request agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
	if f.enrollCalls == 0 {
		f.syncBeforeEnroll = true
	}
	f.syncCalls++
	f.lastSync = request
	f.syncRequests = append(f.syncRequests, request)
	if len(f.syncResponses) >= f.syncCalls {
		response := f.syncResponses[f.syncCalls-1]
		return &response, nil
	}
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

type fakeHostSampleProvider struct {
	calls  int
	result agentapi.HostSamplePayload
	err    error
}

func (f *fakeHostSampleProvider) Collect(observedAt time.Time) (agentapi.HostSamplePayload, error) {
	f.calls++
	if f.err != nil {
		return agentapi.HostSamplePayload{}, f.err
	}
	sample := f.result
	sample.ObservedAt = observedAt
	return sample, nil
}

type fakeProbeProvider struct {
	calls    int
	lastPlan *agentapi.SyncPlan
	result   []agentapi.ProbeObservationPayload
	err      error
}

func (f *fakeProbeProvider) CollectDue(_ context.Context, plan *agentapi.SyncPlan, observedAt time.Time) ([]agentapi.ProbeObservationPayload, error) {
	f.calls++
	f.lastPlan = plan
	if f.err != nil {
		return nil, f.err
	}
	out := make([]agentapi.ProbeObservationPayload, 0, len(f.result))
	for _, observation := range f.result {
		observation.ObservedAt = observedAt
		out = append(out, observation)
	}
	return out, nil
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
	if client.lastSync.SyncToken != "sync-token-001" {
		t.Fatalf("Sync sync_token = %q, want %q", client.lastSync.SyncToken, "sync-token-001")
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

func TestRuntimeReturnsMissingSyncTokenErrorForBoundEnrollment(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	client := &fakeClient{
		forceEmptySyncToken: true,
		enrollResponse: agentapi.EnrollmentResponse{
			NodeID:        "node-123",
			Status:        "accepted",
			BindingStatus: agentapi.BindingStatusBound,
			SyncToken:     "",
		},
	}
	rt := agentruntime.NewWithDeps(cfg, nil, client, staticTokenSource{}, staticFingerprint{}, 10*time.Millisecond)

	err := rt.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want non-nil")
	}
	if err.Error() != "enroll agent: enrollment bound response missing sync token" {
		t.Fatalf("Run() error = %q, want %q", err.Error(), "enroll agent: enrollment bound response missing sync token")
	}
	if client.syncCalls != 0 {
		t.Fatalf("Sync() calls = %d, want 0", client.syncCalls)
	}
}

func TestRuntimeUpdatesPlanAndAttachesDueHostSampleAndProbeObservations(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	client := &fakeClient{
		syncResponses: []agentapi.SyncResponse{
			{
				AcceptedAt: time.Now().UTC(),
				Status:     "accepted",
				Plan: &agentapi.SyncPlan{
					HostSampleFrequencyTier:      agentapi.FrequencyTier1m,
					HostSampleMaintenanceContext: true,
					ProbeAssignments: []agentapi.ProbeAssignment{{
						TargetID:       "tg_001",
						TargetHost:     "api.example.test",
						ProbeItemID:    "pb_001",
						ProbeKind:      agentapi.ProbeKindHTTP,
						FrequencyTier:  agentapi.FrequencyTier1m,
						TimeoutSeconds: 5,
						Config:         []byte(`{"path":"/healthz","method":"GET","expected_status_range":[200,299]}`),
					}},
				},
			},
			{AcceptedAt: time.Now().UTC(), Status: "accepted"},
		},
	}
	hostProvider := &fakeHostSampleProvider{
		result: agentapi.HostSamplePayload{
			CPUUsagePct: 12.5,
		},
	}
	probeProvider := &fakeProbeProvider{
		result: []agentapi.ProbeObservationPayload{{
			TargetID:    "tg_001",
			ProbeItemID: "pb_001",
			ProbeKind:   agentapi.ProbeKindHTTP,
			ResultKind:  agentapi.ProbeResultSuccess,
			LatencyMS:   intPtr(83),
			HTTPStatus:  intPtr(200),
		}},
	}

	rt := agentruntime.NewWithRuntimeDeps(cfg, nil, client, staticTokenSource{}, staticFingerprint{}, hostProvider, probeProvider, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Millisecond)
	defer cancel()

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if client.syncCalls < 2 {
		t.Fatalf("Sync() calls = %d, want at least 2", client.syncCalls)
	}
	firstSync := client.syncRequests[0]
	if len(firstSync.HostSamples) != 0 || len(firstSync.ProbeObservations) != 0 {
		t.Fatalf("first sync should be heartbeat-only before plan arrives: %#v", firstSync)
	}
	secondSync := client.syncRequests[1]
	if len(secondSync.HostSamples) != 1 {
		t.Fatalf("len(secondSync.HostSamples) = %d, want 1", len(secondSync.HostSamples))
	}
	if secondSync.HostSamples[0].AgentVersion != "dev" || secondSync.HostSamples[0].Fingerprint != "fp-001" || secondSync.HostSamples[0].SyncBatchID == "" {
		t.Fatalf("host sample metadata not populated: %#v", secondSync.HostSamples[0])
	}
	if !secondSync.HostSamples[0].MaintenanceContext {
		t.Fatal("HostSamples[0].MaintenanceContext = false, want true")
	}
	if len(secondSync.ProbeObservations) != 1 {
		t.Fatalf("len(secondSync.ProbeObservations) = %d, want 1", len(secondSync.ProbeObservations))
	}
	if secondSync.ProbeObservations[0].AgentVersion != "dev" || secondSync.ProbeObservations[0].Fingerprint != "fp-001" || secondSync.ProbeObservations[0].SyncBatchID == "" {
		t.Fatalf("probe observation metadata not populated: %#v", secondSync.ProbeObservations[0])
	}
	if hostProvider.calls == 0 {
		t.Fatal("host provider was not called")
	}
	if probeProvider.calls == 0 || probeProvider.lastPlan == nil {
		t.Fatal("probe provider did not receive current plan")
	}
}

func TestRuntimeLogsHostSampleFailureAndContinuesHeartbeatSync(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	client := &fakeClient{
		syncResponses: []agentapi.SyncResponse{
			{
				AcceptedAt: time.Now().UTC(),
				Status:     "accepted",
				Plan: &agentapi.SyncPlan{
					HostSampleFrequencyTier: agentapi.FrequencyTier1m,
				},
			},
			{AcceptedAt: time.Now().UTC(), Status: "accepted"},
		},
	}
	hostProvider := &fakeHostSampleProvider{err: errors.New("boom")}
	probeProvider := &fakeProbeProvider{}

	rt := agentruntime.NewWithRuntimeDeps(cfg, nil, client, staticTokenSource{}, staticFingerprint{}, hostProvider, probeProvider, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Millisecond)
	defer cancel()

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if client.syncCalls < 2 {
		t.Fatalf("Sync() calls = %d, want at least 2", client.syncCalls)
	}
	if len(client.syncRequests[1].HostSamples) != 0 {
		t.Fatalf("len(HostSamples) = %d, want 0 when collection fails", len(client.syncRequests[1].HostSamples))
	}
}

func TestRuntimeReplacesCurrentPlanWithExplicitEmptyPlan(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	client := &fakeClient{
		syncResponses: []agentapi.SyncResponse{
			{
				AcceptedAt: time.Now().UTC(),
				Status:     "accepted",
				Plan: &agentapi.SyncPlan{
					HostSampleFrequencyTier: agentapi.FrequencyTier1m,
					ProbeAssignments: []agentapi.ProbeAssignment{{
						TargetID:       "tg_001",
						TargetHost:     "api.example.test",
						ProbeItemID:    "pb_001",
						ProbeKind:      agentapi.ProbeKindHTTP,
						FrequencyTier:  agentapi.FrequencyTier1m,
						TimeoutSeconds: 5,
						Config:         []byte(`{"path":"/healthz","method":"GET","expected_status_range":[200,299]}`),
					}},
				},
			},
			{
				AcceptedAt: time.Now().UTC(),
				Status:     "accepted",
				Plan: &agentapi.SyncPlan{
					HostSampleFrequencyTier: agentapi.FrequencyTier1m,
				},
			},
			{
				AcceptedAt: time.Now().UTC(),
				Status:     "accepted",
				Plan: &agentapi.SyncPlan{
					HostSampleFrequencyTier: agentapi.FrequencyTier1m,
				},
			},
		},
	}
	hostProvider := &fakeHostSampleProvider{
		result: agentapi.HostSamplePayload{CPUUsagePct: 12.5},
	}
	probeProvider := &fakeProbeProvider{
		result: []agentapi.ProbeObservationPayload{{
			TargetID:    "tg_001",
			ProbeItemID: "pb_001",
			ProbeKind:   agentapi.ProbeKindHTTP,
			ResultKind:  agentapi.ProbeResultSuccess,
		}},
	}

	rt := agentruntime.NewWithRuntimeDeps(cfg, nil, client, staticTokenSource{}, staticFingerprint{}, hostProvider, probeProvider, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Millisecond)
	defer cancel()

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if client.syncCalls < 3 {
		t.Fatalf("Sync() calls = %d, want at least 3", client.syncCalls)
	}
	if probeProvider.calls != 1 {
		t.Fatalf("probeProvider.calls = %d, want 1 after plan replaced with explicit empty plan", probeProvider.calls)
	}
	if len(client.syncRequests[2].ProbeObservations) != 0 {
		t.Fatalf("len(thirdSync.ProbeObservations) = %d, want 0 after explicit empty plan", len(client.syncRequests[2].ProbeObservations))
	}
}

func intPtr(value int) *int { return &value }
