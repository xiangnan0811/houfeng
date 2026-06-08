package runtime_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	agentconfig "houfeng/agent/config"
	agentruntime "houfeng/agent/runtime"
	"houfeng/agent/syncqueue"
	"houfeng/internal/contracts/agentapi"
)

type fakeClient struct {
	enrollCalls         int
	syncCalls           int
	syncBeforeEnroll    bool
	enrollResponse      agentapi.EnrollmentResponse
	forceEmptySyncToken bool
	syncResponses       []agentapi.SyncResponse
	syncErrs            []error
	cancelAfterSyncs    int
	cancel              context.CancelFunc

	lastEnroll   agentapi.EnrollmentRequest
	lastSync     agentapi.SyncRequest
	syncRequests []agentapi.SyncRequest
}

func (f *fakeClient) Enroll(_ context.Context, request agentapi.EnrollmentRequest) (*agentapi.EnrollmentResponse, error) {
	f.enrollCalls++
	f.lastEnroll = request
	response := f.enrollResponse
	if response.MonitoringInstanceID == "" {
		response.MonitoringInstanceID = "monitoringInstance-123"
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
	if f.cancel != nil && f.cancelAfterSyncs > 0 && f.syncCalls >= f.cancelAfterSyncs {
		f.cancel()
		f.cancel = nil
	}
	if len(f.syncErrs) >= f.syncCalls && f.syncErrs[f.syncCalls-1] != nil {
		return nil, f.syncErrs[f.syncCalls-1]
	}
	if len(f.syncResponses) >= f.syncCalls {
		response := f.syncResponses[f.syncCalls-1]
		return &response, nil
	}
	return &agentapi.SyncResponse{AcceptedAt: time.Now().UTC(), Status: "ok"}, nil
}

func withoutDockerCLI(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
}

type staticTokenSource struct{}

func (staticTokenSource) Token(context.Context) (string, error) {
	return "plain-token", nil
}

type syncCredentialTokenSource struct {
	enrollmentToken      string
	monitoringInstanceID string
	syncToken            string
	hasCredentials       bool
	loadErr              error
	saveErr              error
	saveCalls            int
}

func (s *syncCredentialTokenSource) Token(context.Context) (string, error) {
	if s.enrollmentToken == "" {
		return "plain-token", nil
	}
	return s.enrollmentToken, nil
}

func (s *syncCredentialTokenSource) SyncCredentials(context.Context) (string, string, bool, error) {
	if s.loadErr != nil {
		return "", "", false, s.loadErr
	}
	return s.monitoringInstanceID, s.syncToken, s.hasCredentials, nil
}

func (s *syncCredentialTokenSource) SaveSyncCredentials(_ context.Context, monitoringInstanceID, syncToken string) error {
	s.saveCalls++
	if s.saveErr != nil {
		return s.saveErr
	}
	s.monitoringInstanceID = monitoringInstanceID
	s.syncToken = syncToken
	s.hasCredentials = true
	return nil
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

type fakeIPQualityProvider struct {
	startCalls        int
	drainCalls        int
	lastPlan          *agentapi.IPQualityPlan
	startErr          error
	reports           []agentapi.IPQualityReportPayload
	reportsAfterStart []agentapi.IPQualityReportPayload
}

func (f *fakeIPQualityProvider) MaybeStart(ctx context.Context, plan *agentapi.IPQualityPlan, observedAt time.Time) error {
	f.startCalls++
	f.lastPlan = plan
	if f.startErr != nil {
		return f.startErr
	}
	if plan == nil || !plan.Enabled {
		return nil
	}
	if len(f.reportsAfterStart) > 0 {
		f.reports = append(f.reports, f.reportsAfterStart...)
		f.reportsAfterStart = nil
	}
	go func() {
		<-ctx.Done()
	}()
	_ = observedAt
	return nil
}

func (f *fakeIPQualityProvider) DrainReports() []agentapi.IPQualityReportPayload {
	f.drainCalls++
	out := append([]agentapi.IPQualityReportPayload(nil), f.reports...)
	f.reports = nil
	return out
}

type fakeSyncQueue struct {
	enqueueErr error
	listErr    error
	deleteErr  error
	markErr    error
	pruneErr   error

	entries     []syncqueue.Entry
	deleteCalls int
	markCalls   int
}

func (f *fakeSyncQueue) Enqueue(_ context.Context, request agentapi.SyncRequest) (string, error) {
	if f.enqueueErr != nil {
		return "", f.enqueueErr
	}
	id := request.Heartbeats[0].SyncBatchID
	f.entries = append(f.entries, syncqueue.Entry{
		ID:      id,
		Request: request,
	})
	return id, nil
}

func (f *fakeSyncQueue) List(context.Context) ([]syncqueue.Entry, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]syncqueue.Entry, len(f.entries))
	copy(out, f.entries)
	return out, nil
}

func (f *fakeSyncQueue) Delete(_ context.Context, id string) error {
	f.deleteCalls++
	if f.deleteErr != nil {
		return f.deleteErr
	}
	filtered := f.entries[:0]
	for _, entry := range f.entries {
		if entry.ID != id {
			filtered = append(filtered, entry)
		}
	}
	f.entries = filtered
	return nil
}

func (f *fakeSyncQueue) MarkAttempt(_ context.Context, id string) error {
	f.markCalls++
	if f.markErr != nil {
		return f.markErr
	}
	for i := range f.entries {
		if f.entries[i].ID == id {
			f.entries[i].Attempts++
		}
	}
	return nil
}

func (f *fakeSyncQueue) Prune(context.Context) error {
	return f.pruneErr
}

type cancelAfterDeleteQueue struct {
	*syncqueue.FileStore
	afterDeletes int
	cancel       context.CancelFunc
	deleteCalls  int
}

func (q *cancelAfterDeleteQueue) Delete(ctx context.Context, id string) error {
	if err := q.FileStore.Delete(ctx, id); err != nil {
		return err
	}
	q.deleteCalls++
	if q.cancel != nil && q.deleteCalls >= q.afterDeletes {
		q.cancel()
		q.cancel = nil
	}
	return nil
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
	if client.lastSync.MonitoringInstanceID != "monitoringInstance-123" {
		t.Fatalf("Sync monitoring_instance_id = %q, want %q", client.lastSync.MonitoringInstanceID, "monitoringInstance-123")
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

func TestRuntimeUsesPersistedSyncCredentialsWithoutReusingEnrollmentToken(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	client := &fakeClient{}
	tokenSource := &syncCredentialTokenSource{
		monitoringInstanceID: "monitoringInstance-456",
		syncToken:            "sync-token-persisted",
		hasCredentials:       true,
	}
	rt := agentruntime.NewWithDeps(cfg, nil, client, tokenSource, staticFingerprint{}, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if client.enrollCalls != 0 {
		t.Fatalf("Enroll() calls = %d, want 0 with persisted sync credentials", client.enrollCalls)
	}
	if client.syncCalls == 0 {
		t.Fatal("Sync() was not called")
	}
	if client.lastSync.MonitoringInstanceID != "monitoringInstance-456" {
		t.Fatalf("Sync monitoring_instance_id = %q, want %q", client.lastSync.MonitoringInstanceID, "monitoringInstance-456")
	}
	if client.lastSync.SyncToken != "sync-token-persisted" {
		t.Fatalf("Sync sync_token = %q, want %q", client.lastSync.SyncToken, "sync-token-persisted")
	}
	if tokenSource.saveCalls != 0 {
		t.Fatalf("SaveSyncCredentials() calls = %d, want 0", tokenSource.saveCalls)
	}
}

func TestRuntimePersistsSyncCredentialsAfterEnrollment(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	client := &fakeClient{}
	tokenSource := &syncCredentialTokenSource{enrollmentToken: "enroll-token-001"}
	rt := agentruntime.NewWithDeps(cfg, nil, client, tokenSource, staticFingerprint{}, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if client.enrollCalls != 1 {
		t.Fatalf("Enroll() calls = %d, want 1", client.enrollCalls)
	}
	if client.lastEnroll.Token != "enroll-token-001" {
		t.Fatalf("Enroll token = %q, want %q", client.lastEnroll.Token, "enroll-token-001")
	}
	if tokenSource.saveCalls != 1 {
		t.Fatalf("SaveSyncCredentials() calls = %d, want 1", tokenSource.saveCalls)
	}
	if tokenSource.monitoringInstanceID != "monitoringInstance-123" {
		t.Fatalf("persisted monitoringInstanceID = %q, want %q", tokenSource.monitoringInstanceID, "monitoringInstance-123")
	}
	if tokenSource.syncToken != "sync-token-001" {
		t.Fatalf("persisted syncToken = %q, want %q", tokenSource.syncToken, "sync-token-001")
	}
}

func TestRuntimeReturnsPersistSyncCredentialsErrorBeforeSyncLoop(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	client := &fakeClient{}
	saveErr := errors.New("token file read-only")
	tokenSource := &syncCredentialTokenSource{saveErr: saveErr}
	rt := agentruntime.NewWithDeps(cfg, nil, client, tokenSource, staticFingerprint{}, 10*time.Millisecond)

	err := rt.Run(context.Background())
	if !errors.Is(err, saveErr) {
		t.Fatalf("Run() error = %v, want save error", err)
	}
	if client.enrollCalls != 1 {
		t.Fatalf("Enroll() calls = %d, want 1", client.enrollCalls)
	}
	if client.syncCalls != 0 {
		t.Fatalf("Sync() calls = %d, want 0", client.syncCalls)
	}
}

func TestRuntimeReturnsEnrollmentNotBoundErrorWithoutStartingSyncLoop(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	client := &fakeClient{
		enrollResponse: agentapi.EnrollmentResponse{
			MonitoringInstanceID: "monitoringInstance-123",
			Status:               "accepted",
			BindingStatus:        agentapi.BindingStatusPendingConfirmation,
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
			MonitoringInstanceID: "monitoringInstance-123",
			Status:               "accepted",
			BindingStatus:        agentapi.BindingStatusBound,
			SyncToken:            "",
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
		cancelAfterSyncs: 3,
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

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client.cancel = cancel

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
	// Containers field is populated by containersample (nil when Docker unavailable).
	if secondSync.HostSamples[0].Containers != nil {
		t.Logf("containers attached: %d container(s)", len(secondSync.HostSamples[0].Containers))
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
		cancelAfterSyncs: 3,
		syncResponses: []agentapi.SyncResponse{
			{
				AcceptedAt: time.Now().UTC(),
				Status:     "accepted",
				Plan: &agentapi.SyncPlan{
					HostSampleFrequencyTier: agentapi.FrequencyTier1m,
				},
			},
			{AcceptedAt: time.Now().UTC(), Status: "accepted"},
			{AcceptedAt: time.Now().UTC(), Status: "accepted"},
		},
	}
	hostProvider := &fakeHostSampleProvider{err: errors.New("boom")}
	probeProvider := &fakeProbeProvider{}

	rt := agentruntime.NewWithRuntimeDeps(cfg, nil, client, staticTokenSource{}, staticFingerprint{}, hostProvider, probeProvider, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client.cancel = cancel

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
	withoutDockerCLI(t)

	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client := &fakeClient{
		cancelAfterSyncs: 3,
		cancel:           cancel,
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

func TestRuntimeQueuesFailedSyncAndRetriesAsBackfilled(t *testing.T) {
	withoutDockerCLI(t)

	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	client := &fakeClient{
		syncErrs: []error{nil, errors.New("center unavailable"), nil},
		syncResponses: []agentapi.SyncResponse{
			{AcceptedAt: time.Now().UTC(), Status: "accepted", Plan: &agentapi.SyncPlan{HostSampleFrequencyTier: agentapi.FrequencyTier1m}},
			{},
			{AcceptedAt: time.Now().UTC(), Status: "accepted"},
		},
	}
	fileStore := syncqueue.NewFileStore(t.TempDir()+"/buffer.json", syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour, SkipFsync: true})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	store := &cancelAfterDeleteQueue{
		FileStore:    fileStore,
		afterDeletes: 3,
		cancel:       cancel,
	}
	hostProvider := &fakeHostSampleProvider{result: agentapi.HostSamplePayload{CPUUsagePct: 12.5}}
	rt := agentruntime.NewWithRuntimeDeps(cfg, nil, client, staticTokenSource{}, staticFingerprint{}, hostProvider, &fakeProbeProvider{}, 10*time.Millisecond, store)

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if client.syncCalls < 4 {
		t.Fatalf("syncCalls = %d, want at least 4", client.syncCalls)
	}
	var retried *agentapi.SyncRequest
	for i := range client.syncRequests {
		request := &client.syncRequests[i]
		if len(request.Heartbeats) > 0 && request.Heartbeats[0].IsBackfilled {
			retried = request
			break
		}
	}
	if retried == nil {
		t.Fatalf("sync requests = %#v, want one retried backfilled request", client.syncRequests)
	}
	if len(retried.Heartbeats) == 0 || !retried.Heartbeats[0].IsBackfilled {
		t.Fatalf("retried heartbeat = %#v, want backfilled", retried.Heartbeats)
	}
	if len(retried.HostSamples) == 0 || !retried.HostSamples[0].IsBackfilled {
		t.Fatalf("retried host samples = %#v, want backfilled", retried.HostSamples)
	}
	entries, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("queue List() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("len(queue entries) = %d, want 0 after ack", len(entries))
	}
}

func TestRuntimeReturnsQueuePersistenceErrorWithoutDroppingCurrentBatch(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	client := &fakeClient{}
	enqueueErr := errors.New("queue path is read-only")
	queue := &fakeSyncQueue{enqueueErr: enqueueErr}
	rt := agentruntime.NewWithRuntimeDeps(cfg, nil, client, staticTokenSource{}, staticFingerprint{}, &fakeHostSampleProvider{}, &fakeProbeProvider{}, 10*time.Millisecond, queue)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := rt.Run(ctx)
	if !errors.Is(err, enqueueErr) {
		t.Fatalf("Run() error = %v, want enqueue error", err)
	}
	if client.syncCalls != 0 {
		t.Fatalf("syncCalls = %d, want 0 when enqueue fails before send", client.syncCalls)
	}
}

func TestRuntimeReturnsAckDeleteErrorInsteadOfSilentlyReplaying(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	client := &fakeClient{}
	deleteErr := errors.New("queue delete denied")
	queue := &fakeSyncQueue{deleteErr: deleteErr}
	rt := agentruntime.NewWithRuntimeDeps(cfg, nil, client, staticTokenSource{}, staticFingerprint{}, &fakeHostSampleProvider{}, &fakeProbeProvider{}, 10*time.Millisecond, queue)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := rt.Run(ctx)
	if !errors.Is(err, deleteErr) {
		t.Fatalf("Run() error = %v, want delete error", err)
	}
	if client.syncCalls != 1 {
		t.Fatalf("syncCalls = %d, want 1 accepted send before delete failure stops runtime", client.syncCalls)
	}
	if queue.deleteCalls != 1 {
		t.Fatalf("deleteCalls = %d, want 1", queue.deleteCalls)
	}
}

func TestRuntimeFlushesPersistedQueueAfterRestart(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	path := t.TempDir() + "/buffer.json"
	seedStore := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour, SkipFsync: true})
	seeded := agentapi.SyncRequest{
		MonitoringInstanceID: "monitoringInstance-123",
		SyncToken:            "sync-token-001",
		Heartbeats: []agentapi.MonitoringInstanceHeartbeat{{
			ObservedAt:   time.Now().UTC().Add(-time.Minute),
			AgentVersion: "dev",
			Fingerprint:  "fp-001",
			SyncBatchID:  "seeded",
		}},
	}
	if _, err := seedStore.Enqueue(context.Background(), seeded); err != nil {
		t.Fatalf("seed Enqueue() error = %v", err)
	}

	client := &fakeClient{}
	fileStore := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour, SkipFsync: true})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	store := &cancelAfterDeleteQueue{
		FileStore:    fileStore,
		afterDeletes: 1,
		cancel:       cancel,
	}
	rt := agentruntime.NewWithRuntimeDeps(cfg, nil, client, staticTokenSource{}, staticFingerprint{}, &fakeHostSampleProvider{}, &fakeProbeProvider{}, 10*time.Millisecond, store)
	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(client.syncRequests) == 0 || client.syncRequests[0].Heartbeats[0].SyncBatchID != "seeded" || !client.syncRequests[0].Heartbeats[0].IsBackfilled {
		t.Fatalf("first sync request = %#v, want seeded backfilled request", client.syncRequests)
	}
}

func TestRuntimeExecutesPendingActionAndReturnsCommandResult(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	actionID := "act_001"
	commandID := "uptime"
	client := &fakeClient{
		cancelAfterSyncs: 3,
		syncResponses: []agentapi.SyncResponse{
			{
				AcceptedAt: time.Now().UTC(),
				Status:     "accepted",
				Plan: &agentapi.SyncPlan{
					HostSampleFrequencyTier: agentapi.FrequencyTier1m,
					PendingAction: &agentapi.PendingAction{
						CommandID: commandID,
						ActionID:  actionID,
					},
				},
			},
			{AcceptedAt: time.Now().UTC(), Status: "accepted"},
			{AcceptedAt: time.Now().UTC(), Status: "accepted"},
		},
	}

	rt := agentruntime.NewWithRuntimeDeps(cfg, nil, client, staticTokenSource{}, staticFingerprint{}, &fakeHostSampleProvider{}, &fakeProbeProvider{}, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client.cancel = cancel

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if client.syncCalls < 2 {
		t.Fatalf("Sync() calls = %d, want at least 2", client.syncCalls)
	}

	// First sync gets the plan with pending action; result should appear in
	// the second sync request.
	secondSync := client.syncRequests[1]
	if len(secondSync.CommandResults) == 0 {
		t.Fatalf("secondSync.CommandResults empty, want at least 1")
	}
	cr := secondSync.CommandResults[0]
	if cr.ActionID != actionID {
		t.Fatalf("CommandResult.ActionID = %q, want %q", cr.ActionID, actionID)
	}
	if cr.CommandID != commandID {
		t.Fatalf("CommandResult.CommandID = %q, want %q", cr.CommandID, commandID)
	}
	if cr.ExitCode != 0 {
		t.Fatalf("CommandResult.ExitCode = %d, want 0 for uptime", cr.ExitCode)
	}
	if cr.Stdout == "" {
		t.Fatal("CommandResult.Stdout is empty, want uptime output")
	}
}

func TestRuntimeSilentlyIgnoresUnknownPendingActionCommandID(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	client := &fakeClient{
		cancelAfterSyncs: 3,
		syncResponses: []agentapi.SyncResponse{
			{
				AcceptedAt: time.Now().UTC(),
				Status:     "accepted",
				Plan: &agentapi.SyncPlan{
					HostSampleFrequencyTier: agentapi.FrequencyTier1m,
					PendingAction: &agentapi.PendingAction{
						CommandID: "nonexistent_cmd",
						ActionID:  "act_bad",
					},
				},
			},
			{AcceptedAt: time.Now().UTC(), Status: "accepted"},
			{AcceptedAt: time.Now().UTC(), Status: "accepted"},
		},
	}

	rt := agentruntime.NewWithRuntimeDeps(cfg, nil, client, staticTokenSource{}, staticFingerprint{}, &fakeHostSampleProvider{}, &fakeProbeProvider{}, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client.cancel = cancel

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if client.syncCalls < 2 {
		t.Fatalf("Sync() calls = %d, want at least 2", client.syncCalls)
	}

	// Unknown command ID should not produce any command results.
	secondSync := client.syncRequests[1]
	if len(secondSync.CommandResults) != 0 {
		t.Fatalf("secondSync.CommandResults = %d, want 0 for unknown command ID", len(secondSync.CommandResults))
	}
}

func TestRuntimeStartsIPQualityCollectionAfterPlanWithoutBlockingHeartbeat(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	ipProvider := &fakeIPQualityProvider{}
	client := &fakeClient{
		cancelAfterSyncs: 3,
		syncResponses: []agentapi.SyncResponse{
			{
				AcceptedAt: time.Now().UTC(),
				Status:     "accepted",
				Plan: &agentapi.SyncPlan{
					IPQualityPlan: &agentapi.IPQualityPlan{
						Enabled:          true,
						FrequencySeconds: 86400,
						TimeoutSeconds:   15,
						Services:         []string{"netflix"},
					},
				},
			},
			{AcceptedAt: time.Now().UTC(), Status: "accepted"},
			{AcceptedAt: time.Now().UTC(), Status: "accepted"},
		},
	}

	rt := agentruntime.NewWithRuntimeDeps(cfg, nil, client, staticTokenSource{}, staticFingerprint{}, &fakeHostSampleProvider{}, &fakeProbeProvider{}, 10*time.Millisecond, nil, ipProvider)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client.cancel = cancel

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if client.syncCalls < 2 {
		t.Fatalf("Sync() calls = %d, want at least 2", client.syncCalls)
	}
	if len(client.syncRequests[0].IPQualityReports) != 0 {
		t.Fatalf("first sync IPQualityReports = %#v, want none before plan", client.syncRequests[0].IPQualityReports)
	}
	if ipProvider.startCalls == 0 {
		t.Fatal("IP quality provider was not started after plan")
	}
	if ipProvider.lastPlan == nil || !ipProvider.lastPlan.Enabled || ipProvider.lastPlan.Services[0] != "netflix" {
		t.Fatalf("last IP quality plan = %#v, want enabled plan", ipProvider.lastPlan)
	}
}

func TestRuntimeDrainsCompletedIPQualityReportIntoNextSync(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	ipProvider := &fakeIPQualityProvider{
		reportsAfterStart: []agentapi.IPQualityReportPayload{{
			ObservedAt: time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC),
			IPAddress:  "203.0.113.10",
			IPVersion:  4,
			Status:     agentapi.IPQualityStatusSuccess,
		}},
	}
	client := &fakeClient{
		cancelAfterSyncs: 3,
		syncResponses: []agentapi.SyncResponse{
			{
				AcceptedAt: time.Now().UTC(),
				Status:     "accepted",
				Plan: &agentapi.SyncPlan{
					IPQualityPlan: &agentapi.IPQualityPlan{
						Enabled:          true,
						FrequencySeconds: 86400,
						TimeoutSeconds:   15,
					},
				},
			},
			{AcceptedAt: time.Now().UTC(), Status: "accepted"},
			{AcceptedAt: time.Now().UTC(), Status: "accepted"},
		},
	}

	rt := agentruntime.NewWithRuntimeDeps(cfg, nil, client, staticTokenSource{}, staticFingerprint{}, &fakeHostSampleProvider{}, &fakeProbeProvider{}, 10*time.Millisecond, nil, ipProvider)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client.cancel = cancel

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if client.syncCalls < 3 {
		t.Fatalf("Sync() calls = %d, want at least 3", client.syncCalls)
	}
	secondSync := client.syncRequests[1]
	if len(secondSync.IPQualityReports) != 0 {
		t.Fatalf("second sync IPQualityReports = %#v, want collection to start without blocking heartbeat", secondSync.IPQualityReports)
	}
	thirdSync := client.syncRequests[2]
	if len(thirdSync.IPQualityReports) != 1 {
		t.Fatalf("len(thirdSync.IPQualityReports) = %d, want 1", len(thirdSync.IPQualityReports))
	}
	report := thirdSync.IPQualityReports[0]
	if report.AgentVersion != "dev" || report.Fingerprint != "fp-001" || report.SyncBatchID == "" {
		t.Fatalf("report metadata = %#v, want runtime-filled metadata", report)
	}
	if report.IPAddress != "203.0.113.10" || report.Status != agentapi.IPQualityStatusSuccess {
		t.Fatalf("report = %#v, want completed report", report)
	}
}

func TestRuntimeDoesNotStartIPQualityWhenPlanDisabled(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	ipProvider := &fakeIPQualityProvider{}
	client := &fakeClient{
		cancelAfterSyncs: 3,
		syncResponses: []agentapi.SyncResponse{
			{
				AcceptedAt: time.Now().UTC(),
				Status:     "accepted",
				Plan: &agentapi.SyncPlan{
					IPQualityPlan: &agentapi.IPQualityPlan{
						Enabled:          false,
						FrequencySeconds: 86400,
					},
				},
			},
			{AcceptedAt: time.Now().UTC(), Status: "accepted"},
			{AcceptedAt: time.Now().UTC(), Status: "accepted"},
		},
	}

	rt := agentruntime.NewWithRuntimeDeps(cfg, nil, client, staticTokenSource{}, staticFingerprint{}, &fakeHostSampleProvider{}, &fakeProbeProvider{}, 10*time.Millisecond, nil, ipProvider)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client.cancel = cancel

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if ipProvider.startCalls != 0 {
		t.Fatalf("IP quality start calls = %d, want 0 for disabled plan", ipProvider.startCalls)
	}
}

func TestRuntimeUploadsIPQualityFailureReport(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	ipProvider := &fakeIPQualityProvider{
		reportsAfterStart: []agentapi.IPQualityReportPayload{{
			ObservedAt:      time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC),
			IPAddress:       "0.0.0.0",
			IPVersion:       4,
			Status:          agentapi.IPQualityStatusFailure,
			ErrorCode:       "lookup_failed",
			ErrorSummary:    "upstream unavailable",
			AgentVersion:    "collector-version",
			Fingerprint:     "collector-fp",
			SyncBatchID:     "collector-batch",
			IsBackfilled:    false,
			ProviderResults: nil,
		}},
	}
	client := &fakeClient{
		cancelAfterSyncs: 3,
		syncResponses: []agentapi.SyncResponse{
			{
				AcceptedAt: time.Now().UTC(),
				Status:     "accepted",
				Plan: &agentapi.SyncPlan{
					IPQualityPlan: &agentapi.IPQualityPlan{Enabled: true, FrequencySeconds: 86400},
				},
			},
			{AcceptedAt: time.Now().UTC(), Status: "accepted"},
			{AcceptedAt: time.Now().UTC(), Status: "accepted"},
		},
	}

	rt := agentruntime.NewWithRuntimeDeps(cfg, nil, client, staticTokenSource{}, staticFingerprint{}, &fakeHostSampleProvider{}, &fakeProbeProvider{}, 10*time.Millisecond, nil, ipProvider)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client.cancel = cancel

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if client.syncCalls < 3 {
		t.Fatalf("Sync() calls = %d, want at least 3", client.syncCalls)
	}
	thirdSync := client.syncRequests[2]
	if len(thirdSync.IPQualityReports) != 1 {
		t.Fatalf("len(thirdSync.IPQualityReports) = %d, want 1", len(thirdSync.IPQualityReports))
	}
	report := thirdSync.IPQualityReports[0]
	if report.Status != agentapi.IPQualityStatusFailure || report.ErrorCode != "lookup_failed" {
		t.Fatalf("failure report = %#v, want failure uploaded", report)
	}
	if report.AgentVersion != "collector-version" || report.Fingerprint != "collector-fp" || report.SyncBatchID != "collector-batch" {
		t.Fatalf("report metadata overwritten = %#v, want collector metadata preserved when present", report)
	}
}

func TestRuntimeCollectHostSampleAttachesContainers(t *testing.T) {
	// Set up a fake docker so containersample.Collect returns real data.
	dir := t.TempDir()
	script := dir + "/docker"
	psOut := "abc123\tmy-app\tnginx:latest\tUp 2 hours\n"
	statsOut := "0.50%\t1.23%\tmy-app\n"
	content := "#!/bin/sh\ncase \"$1\" in\n  ps) cat <<'DOCKERPS'\n" + psOut + "DOCKERPS\n    ;;\n  stats) cat <<'DOCKERSTATS'\n" + statsOut + "DOCKERSTATS\n    ;;\n  *) exit 1 ;;\nesac\n"
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake docker script: %v", err)
	}
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", dir+":"+origPath)
	defer os.Setenv("PATH", origPath)

	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	client := &fakeClient{
		cancelAfterSyncs: 2,
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
	hostProvider := &fakeHostSampleProvider{
		result: agentapi.HostSamplePayload{CPUUsagePct: 12.5},
	}

	rt := agentruntime.NewWithRuntimeDeps(cfg, nil, client, staticTokenSource{}, staticFingerprint{}, hostProvider, &fakeProbeProvider{}, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client.cancel = cancel

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if client.syncCalls < 2 {
		t.Fatalf("Sync() calls = %d, want at least 2", client.syncCalls)
	}

	secondSync := client.syncRequests[1]
	if len(secondSync.HostSamples) != 1 {
		t.Fatalf("len(secondSync.HostSamples) = %d, want 1", len(secondSync.HostSamples))
	}

	containers := secondSync.HostSamples[0].Containers
	if len(containers) != 1 {
		t.Fatalf("len(Containers) = %d, want 1", len(containers))
	}
	if containers[0].Name != "my-app" {
		t.Errorf("container name = %q, want 'my-app'", containers[0].Name)
	}
	if containers[0].Image != "nginx:latest" {
		t.Errorf("container image = %q, want 'nginx:latest'", containers[0].Image)
	}
	if containers[0].Status != "running" {
		t.Errorf("container status = %q, want 'running'", containers[0].Status)
	}
}

func intPtr(value int) *int { return &value }
