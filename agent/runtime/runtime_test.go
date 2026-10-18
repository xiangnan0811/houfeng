package runtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	agentconfig "houfeng/agent/config"
	"houfeng/agent/enroll"
	agentruntime "houfeng/agent/runtime"
	"houfeng/agent/syncqueue"
	"houfeng/internal/contracts/agentapi"
)

type fakeClient struct {
	enrollCalls         int
	protocolErr         error
	syncCalls           int
	syncBeforeEnroll    bool
	enrollResponse      agentapi.EnrollmentResponse
	enrollErr           error
	forceEmptySyncToken bool
	syncResponses       []agentapi.SyncResponse
	syncErrs            []error
	syncFunc            func(agentapi.SyncRequest) (*agentapi.SyncResponse, error)
	cancelAfterSyncs    int
	cancel              context.CancelFunc

	lastEnroll   agentapi.EnrollmentRequest
	lastSync     agentapi.SyncRequest
	syncRequests []agentapi.SyncRequest
}

func (f *fakeClient) CheckSyncProtocol(context.Context) error { return f.protocolErr }

func (f *fakeClient) Enroll(_ context.Context, request agentapi.EnrollmentRequest) (*agentapi.EnrollmentResponse, error) {
	f.enrollCalls++
	f.lastEnroll = request
	if f.enrollErr != nil {
		return nil, f.enrollErr
	}
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

func (f *fakeClient) Sync(_ context.Context, request agentapi.SyncRequest) (response *agentapi.SyncResponse, err error) {
	defer func() {
		if err == nil && response != nil {
			digest, digestErr := agentapi.CanonicalSyncBatchDigest(request)
			if digestErr != nil {
				err = digestErr
				return
			}
			response.SessionID = request.SessionID
			response.ProducerID = request.ProducerID
			response.BatchSequence = request.BatchSequence
			response.BatchDigest = fmt.Sprintf("%x", digest)
			response.RetiredThrough = request.RetiredThrough
			if response.Disposition == "" {
				response.Disposition = agentapi.SyncDispositionRecorded
				if request.BatchSequence == 0 {
					response.Disposition = agentapi.SyncDispositionSuppressed
				}
			}
		}
	}()
	if f.enrollCalls == 0 {
		f.syncBeforeEnroll = true
	}
	f.syncCalls++
	f.lastSync = request
	f.syncRequests = append(f.syncRequests, request)
	if f.syncFunc != nil {
		return f.syncFunc(request)
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

func withoutDockerCLI(t *testing.T, requiredBinaries ...string) {
	t.Helper()
	binDir := t.TempDir()
	for _, binary := range requiredBinaries {
		path, err := exec.LookPath(binary)
		if err != nil {
			t.Fatalf("locate required test command %s: %v", binary, err)
		}
		if err := os.Symlink(path, filepath.Join(binDir, binary)); err != nil {
			t.Fatalf("expose required test command %s: %v", binary, err)
		}
	}
	t.Setenv("PATH", binDir)
}

type staticTokenSource struct{}

func (staticTokenSource) Token(context.Context) (string, error) {
	return "plain-token", nil
}

type errorTokenSource struct {
	err error
}

func (s errorTokenSource) Token(context.Context) (string, error) {
	return "", s.err
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

type staticFingerprintValue string

func (value staticFingerprintValue) Fingerprint(context.Context) (string, error) {
	return string(value), nil
}

type errorFingerprintSource struct {
	err error
}

func (s errorFingerprintSource) Fingerprint(context.Context) (string, error) {
	return "", s.err
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

type fileQueueFaults struct {
	store      *runtimeTestStore
	enqueueErr error
	listErr    error
	deleteErr  error
	markErr    error
	pruneErr   error

	entries         []syncqueue.Entry
	deleteCalls     int
	deleteManyCalls int
	markCalls       int
}

type failNthEnqueueQueue struct {
	*fileQueueFaults
	calls  int
	failAt int
	err    error
}

type localEntryIDCollisionQueue struct {
	*fileQueueFaults
	enqueued bool
}

type cancelAfterSuccessfulDeleteQueue struct {
	agentruntime.SyncQueue
	afterDeletes int
	cancel       context.CancelFunc
	deleteCalls  int
}

func (q *failNthEnqueueQueue) Enqueue(ctx context.Context, request agentapi.SyncRequest) (string, error) {
	q.calls++
	if q.failAt > 0 && q.calls == q.failAt {
		return "", q.err
	}
	return q.fileQueueFaults.Enqueue(ctx, request)
}

func (q *localEntryIDCollisionQueue) Enqueue(ctx context.Context, request agentapi.SyncRequest) (string, error) {
	if !q.enqueued {
		q.enqueued = true
		if _, err := q.fileQueueFaults.Enqueue(ctx, request); err != nil {
			return "", err
		}
	}
	return q.fileQueueFaults.Enqueue(ctx, request)
}

func (q *cancelAfterSuccessfulDeleteQueue) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := q.SyncQueue.Delete(ctx, id); err != nil {
		return err
	}
	q.deleteCalls++
	if q.cancel != nil && q.deleteCalls >= q.afterDeletes {
		q.cancel()
		q.cancel = nil
	}
	return nil
}

func (q *cancelAfterSuccessfulDeleteQueue) Enqueue(ctx context.Context, request agentapi.SyncRequest) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return q.SyncQueue.Enqueue(ctx, request)
}

func (q *cancelAfterSuccessfulDeleteQueue) List(ctx context.Context) ([]syncqueue.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return q.SyncQueue.List(ctx)
}

func (q *cancelAfterSuccessfulDeleteQueue) DeleteMany(ctx context.Context, ids []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return q.SyncQueue.DeleteMany(ctx, ids)
}

func (q *cancelAfterSuccessfulDeleteQueue) MarkAttempt(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return q.SyncQueue.MarkAttempt(ctx, id)
}

func (q *cancelAfterSuccessfulDeleteQueue) Prune(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return q.SyncQueue.Prune(ctx)
}

func (f *fileQueueFaults) Enqueue(ctx context.Context, request agentapi.SyncRequest) (string, error) {
	if f.enqueueErr != nil {
		return "", f.enqueueErr
	}
	id, err := f.store.Enqueue(ctx, request)
	if err == nil {
		f.entries, err = f.store.List(ctx)
	}
	return id, err
}

func (f *fileQueueFaults) List(ctx context.Context) ([]syncqueue.Entry, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	entries, err := f.store.List(ctx)
	if err == nil {
		f.entries = entries
	}
	return entries, err
}

func (f *fileQueueFaults) Delete(ctx context.Context, id string) error {
	f.deleteCalls++
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if err := f.store.Delete(ctx, id); err != nil {
		return err
	}
	var err error
	f.entries, err = f.store.List(ctx)
	return err
}

func (f *fileQueueFaults) DeleteMany(ctx context.Context, ids []string) error {
	f.deleteManyCalls++
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if err := f.store.DeleteMany(ctx, ids); err != nil {
		return err
	}
	var err error
	f.entries, err = f.store.List(ctx)
	return err
}

func (f *fileQueueFaults) MarkAttempt(ctx context.Context, id string) error {
	f.markCalls++
	if f.markErr != nil {
		return f.markErr
	}
	if err := f.store.MarkAttempt(ctx, id); err != nil {
		return err
	}
	var err error
	f.entries, err = f.store.List(ctx)
	return err
}

func (f *fileQueueFaults) Prune(ctx context.Context) error {
	if f.pruneErr != nil {
		return f.pruneErr
	}
	return f.store.Prune(ctx)
}

type cancelAfterDeleteQueue struct {
	*runtimeTestStore
	afterDeletes int
	cancel       context.CancelFunc
	deleteCalls  int
	markCalls    int
}

func (q *cancelAfterDeleteQueue) Delete(ctx context.Context, id string) error {
	if err := q.runtimeTestStore.Delete(ctx, id); err != nil {
		return err
	}
	q.deleteCalls++
	if q.cancel != nil && q.deleteCalls >= q.afterDeletes {
		q.cancel()
		q.cancel = nil
	}
	return nil
}

func (q *cancelAfterDeleteQueue) DeleteMany(ctx context.Context, ids []string) error {
	if err := q.runtimeTestStore.DeleteMany(ctx, ids); err != nil {
		return err
	}
	q.deleteCalls += len(ids)
	if q.cancel != nil && q.deleteCalls >= q.afterDeletes {
		q.cancel()
		q.cancel = nil
	}
	return nil
}

func (q *cancelAfterDeleteQueue) MarkAttempt(ctx context.Context, id string) error {
	if err := q.runtimeTestStore.MarkAttempt(ctx, id); err != nil {
		return err
	}
	q.markCalls++
	return nil
}

func TestRuntimeEnrollsBeforeSyncLoop(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := &fakeClient{cancelAfterSyncs: 1, cancel: cancel}
	rt := newRuntimeWithDeps(t, cfg, nil, client, staticTokenSource{}, staticFingerprint{}, 10*time.Millisecond)

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T", err)
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
		t.Fatal("Enroll() did not receive the expected token")
	}
	if client.lastEnroll.Fingerprint != "fp-001" {
		t.Fatal("Enroll() did not receive the expected fingerprint")
	}
	if client.lastSync.MonitoringInstanceID != "monitoringInstance-123" {
		t.Fatal("Sync() did not receive the expected monitoring instance ID")
	}
	if client.lastSync.SyncToken != "sync-token-001" {
		t.Fatal("Sync() did not receive the expected sync token")
	}
	if len(client.lastSync.Heartbeats) == 0 {
		t.Fatal("Sync heartbeats = 0, want > 0")
	}
	if client.lastSync.Heartbeats[0].Fingerprint != "fp-001" {
		t.Fatal("Sync() heartbeat did not carry the expected fingerprint")
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
	rt := newRuntimeWithDeps(t, cfg, nil, client, tokenSource, staticFingerprint{}, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T", err)
	}
	if client.enrollCalls != 0 {
		t.Fatalf("Enroll() calls = %d, want 0 with persisted sync credentials", client.enrollCalls)
	}
	if client.syncCalls == 0 {
		t.Fatal("Sync() was not called")
	}
	if client.lastSync.MonitoringInstanceID != "monitoringInstance-456" {
		t.Fatal("Sync() did not use the persisted monitoring instance ID")
	}
	if client.lastSync.SyncToken != "sync-token-persisted" {
		t.Fatal("Sync() did not use the persisted sync token")
	}
	if tokenSource.saveCalls != 0 {
		t.Fatalf("SaveSyncCredentials() calls = %d, want 0", tokenSource.saveCalls)
	}
}

func TestRuntimePersistsSyncCredentialsAfterEnrollment(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	client := &fakeClient{}
	tokenSource := &syncCredentialTokenSource{enrollmentToken: "enroll-token-001"}
	rt := newRuntimeWithDeps(t, cfg, nil, client, tokenSource, staticFingerprint{}, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T", err)
	}
	if client.enrollCalls != 1 {
		t.Fatalf("Enroll() calls = %d, want 1", client.enrollCalls)
	}
	if client.lastEnroll.Token != "enroll-token-001" {
		t.Fatal("Enroll() did not use the enrollment token")
	}
	if tokenSource.saveCalls != 1 {
		t.Fatalf("SaveSyncCredentials() calls = %d, want 1", tokenSource.saveCalls)
	}
	if tokenSource.monitoringInstanceID != "monitoringInstance-123" {
		t.Fatal("enrollment did not persist the monitoring instance ID")
	}
	if tokenSource.syncToken != "sync-token-001" {
		t.Fatal("enrollment did not persist the sync token")
	}
}

func TestRuntimeReturnsPersistSyncCredentialsErrorBeforeSyncLoop(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	client := &fakeClient{}
	saveErr := errors.New("token file read-only")
	tokenSource := &syncCredentialTokenSource{saveErr: saveErr}
	rt := newRuntimeWithDeps(t, cfg, nil, client, tokenSource, staticFingerprint{}, 10*time.Millisecond)

	err := rt.Run(context.Background())
	if !errors.Is(err, saveErr) {
		t.Fatalf("Run() error type = %T, want save error", err)
	}
	if client.enrollCalls != 1 {
		t.Fatalf("Enroll() calls = %d, want 1", client.enrollCalls)
	}
	if client.syncCalls != 0 {
		t.Fatalf("Sync() calls = %d, want 0", client.syncCalls)
	}
}

func TestRuntimeSanitizesLocalFailuresBeforeSyncLoop(t *testing.T) {
	const localSecret = "raw-local-cause sync-token-secret fp-secret"
	localCause := errors.New(localSecret)
	tests := []struct {
		name              string
		tokenSource       agentruntime.TokenSource
		fingerprintSource agentruntime.FingerprintSource
	}{
		{
			name:              "fingerprint load",
			tokenSource:       staticTokenSource{},
			fingerprintSource: errorFingerprintSource{err: localCause},
		},
		{
			name:              "credential load",
			tokenSource:       &syncCredentialTokenSource{loadErr: localCause},
			fingerprintSource: staticFingerprint{},
		},
		{
			name:              "enrollment token load",
			tokenSource:       errorTokenSource{err: localCause},
			fingerprintSource: staticFingerprint{},
		},
		{
			name:              "credential persistence",
			tokenSource:       &syncCredentialTokenSource{saveErr: localCause},
			fingerprintSource: staticFingerprint{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := newRuntimeWithDeps(t,
				agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"},
				slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
				&fakeClient{},
				tt.tokenSource,
				tt.fingerprintSource,
				10*time.Millisecond,
			)

			err := rt.Run(context.Background())
			if !errors.Is(err, localCause) {
				t.Fatalf("Run() error type = %T, want preserved local cause", err)
			}
			if strings.Contains(err.Error(), localSecret) || strings.Contains(err.Error(), "sync-token-secret") || strings.Contains(err.Error(), "fp-secret") {
				t.Fatal("pre-sync local failure exposed raw cause or secret material")
			}
		})
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
	rt := newRuntimeWithDeps(t, cfg, nil, client, staticTokenSource{}, staticFingerprint{}, 10*time.Millisecond)

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
	rt := newRuntimeWithDeps(t, cfg, nil, client, staticTokenSource{}, staticFingerprint{}, 10*time.Millisecond)

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

func TestRuntimeSanitizesRemoteFailuresOutsideDurableQueue(t *testing.T) {
	const (
		remoteMessage = "untrusted remote body sync-token-secret fp-secret"
		unknownCode   = "unknown-code-secret"
	)

	t.Run("enrollment", func(t *testing.T) {
		remoteErr := &enroll.RemoteError{
			StatusCode: http.StatusUnauthorized,
			Code:       unknownCode,
			Message:    remoteMessage,
		}
		client := &fakeClient{enrollErr: remoteErr}
		rt := newRuntimeWithDeps(t,
			agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"},
			slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
			client,
			staticTokenSource{},
			staticFingerprint{},
			10*time.Millisecond,
		)

		err := rt.Run(context.Background())
		if err == nil {
			t.Fatal("Run() error = nil, want sanitized enrollment failure")
		}
		var gotRemoteErr *enroll.RemoteError
		if !errors.As(err, &gotRemoteErr) || gotRemoteErr != remoteErr {
			t.Fatal("Run() did not preserve the typed remote enrollment cause")
		}
		diagnostic := err.Error()
		for _, wanted := range []string{"enroll", "kind=remote", "status=401"} {
			if !strings.Contains(diagnostic, wanted) {
				t.Fatalf("sanitized enrollment diagnostic missing %q", wanted)
			}
		}
		for _, forbidden := range []string{remoteMessage, unknownCode, "sync-token-secret", "fp-secret"} {
			if strings.Contains(diagnostic, forbidden) {
				t.Fatal("enrollment diagnostic exposed remote free text or secret material")
			}
		}
	})

	t.Run("durable sync", func(t *testing.T) {
		remoteErr := &enroll.RemoteError{
			StatusCode: http.StatusNotFound,
			Code:       agentapi.ErrorCodeMonitoringInstanceNotFound,
			Message:    remoteMessage,
		}
		client := &fakeClient{syncFunc: func(agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
			return nil, remoteErr
		}}
		tokenSource := &syncCredentialTokenSource{
			monitoringInstanceID: "monitoringInstance-current",
			syncToken:            "sync-token-secret",
			hasCredentials:       true,
		}
		rt := newRuntimeWithRuntimeDeps(t,
			agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"},
			slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
			client,
			tokenSource,
			staticFingerprint{},
			&fakeHostSampleProvider{},
			&fakeProbeProvider{},
			10*time.Millisecond,
		)

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		err := rt.Run(ctx)
		if err == nil {
			t.Fatal("Run() error = nil, want sanitized sync failure")
		}
		var gotRemoteErr *enroll.RemoteError
		if !errors.As(err, &gotRemoteErr) || gotRemoteErr != remoteErr {
			t.Fatal("Run() did not preserve the typed remote sync cause")
		}
		diagnostic := err.Error()
		for _, wanted := range []string{"sync", "kind=remote", "status=404", "code=" + agentapi.ErrorCodeMonitoringInstanceNotFound} {
			if !strings.Contains(diagnostic, wanted) {
				t.Fatalf("sanitized sync diagnostic missing %q", wanted)
			}
		}
		for _, forbidden := range []string{remoteMessage, "sync-token-secret", "fp-secret"} {
			if strings.Contains(diagnostic, forbidden) {
				t.Fatal("sync diagnostic exposed remote free text or secret material")
			}
		}
	})
}

func TestRuntimeSanitizesUnexpectedEnrollmentBindingStatus(t *testing.T) {
	const hostileBindingStatus = "unknown-binding sync-token-secret fp-secret"
	client := &fakeClient{enrollResponse: agentapi.EnrollmentResponse{
		MonitoringInstanceID: "monitoringInstance-current",
		Status:               "accepted",
		BindingStatus:        hostileBindingStatus,
	}}
	var logs bytes.Buffer
	rt := newRuntimeWithDeps(t,
		agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"},
		slog.New(slog.NewTextHandler(&logs, nil)),
		client,
		staticTokenSource{},
		staticFingerprint{},
		10*time.Millisecond,
	)

	err := rt.Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want enrollment binding failure")
	}
	var bindingErr *agentruntime.EnrollmentNotBoundError
	if !errors.As(err, &bindingErr) || bindingErr.BindingStatus != hostileBindingStatus {
		t.Fatal("Run() did not preserve the typed enrollment binding cause")
	}
	if strings.Contains(err.Error()+logs.String(), hostileBindingStatus) ||
		strings.Contains(err.Error()+logs.String(), "sync-token-secret") ||
		strings.Contains(err.Error()+logs.String(), "fp-secret") {
		t.Fatal("enrollment binding diagnostics exposed untrusted status content")
	}
	if strings.Contains(logs.String(), "agent enrolled") {
		t.Fatal("runtime logged enrollment success before validating the binding response")
	}
}

func TestRuntimeLogsEnrollmentOnlyAfterCredentialsPersist(t *testing.T) {
	persistErr := errors.New("credential store unavailable")
	tokenSource := &syncCredentialTokenSource{saveErr: persistErr}
	var logs bytes.Buffer
	rt := newRuntimeWithDeps(t,
		agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"},
		slog.New(slog.NewTextHandler(&logs, nil)),
		&fakeClient{},
		tokenSource,
		staticFingerprint{},
		10*time.Millisecond,
	)

	err := rt.Run(context.Background())
	if !errors.Is(err, persistErr) {
		t.Fatal("Run() did not preserve the credential persistence cause")
	}
	if strings.Contains(logs.String(), "agent enrolled") {
		t.Fatal("runtime logged enrollment success before credentials were durably persisted")
	}
}

func TestRuntimeEnrollmentSuccessLogDoesNotExposePersistedIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client := &fakeClient{cancelAfterSyncs: 1, cancel: cancel}
	tokenSource := &syncCredentialTokenSource{enrollmentToken: "enroll-token-secret"}
	var logs bytes.Buffer
	rt := newRuntimeWithDeps(t,
		agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"},
		slog.New(slog.NewTextHandler(&logs, nil)),
		client,
		tokenSource,
		staticFingerprint{},
		10*time.Millisecond,
	)

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T, want nil", err)
	}
	logText := logs.String()
	if !strings.Contains(logText, "agent enrolled") {
		t.Fatal("runtime log missing post-persistence enrollment success")
	}
	for _, forbidden := range []string{
		"monitoringInstance-123",
		"sync-token-001",
		"enroll-token-secret",
		"fp-001",
	} {
		if strings.Contains(logText, forbidden) {
			t.Fatal("enrollment success log exposed persisted identity or credential material")
		}
	}
}

func TestRuntimeDoesNotLogServerURLCredentials(t *testing.T) {
	const serverURL = "https://operator:password-secret@center.example.test/private-path-secret?token=query-secret#fragment-secret"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client := &fakeClient{cancelAfterSyncs: 1, cancel: cancel}
	tokenSource := &syncCredentialTokenSource{
		monitoringInstanceID: "monitoringInstance-current",
		syncToken:            "sync-token-current",
		hasCredentials:       true,
	}
	var logs bytes.Buffer
	rt := newRuntimeWithRuntimeDeps(t,
		agentconfig.AgentConfig{ServerURL: serverURL, TokenFile: "/tmp/token"},
		slog.New(slog.NewTextHandler(&logs, nil)),
		client,
		tokenSource,
		staticFingerprint{},
		&fakeHostSampleProvider{},
		&fakeProbeProvider{},
		10*time.Millisecond,
	)

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T, want nil", err)
	}
	if !strings.Contains(logs.String(), "server_url=https://center.example.test") {
		t.Fatal("runtime log missing the sanitized center origin")
	}
	for _, forbidden := range []string{
		"operator",
		"password-secret",
		"private-path-secret",
		"query-secret",
		"fragment-secret",
	} {
		if strings.Contains(logs.String(), forbidden) {
			t.Fatal("runtime log exposed server URL credential or private component")
		}
	}
}

func TestRuntimeTreatsParentCancellationAcrossBoundariesAsCleanShutdown(t *testing.T) {
	t.Run("fingerprint load", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		rt := newRuntimeWithDeps(t,
			agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"},
			slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
			&fakeClient{},
			staticTokenSource{},
			errorFingerprintSource{err: context.Canceled},
			10*time.Millisecond,
		)
		if err := rt.Run(ctx); err != nil {
			t.Fatalf("Run() error type = %T, want clean cancellation", err)
		}
	})

	t.Run("credential load", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		rt := newRuntimeWithDeps(t,
			agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"},
			slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
			&fakeClient{},
			&syncCredentialTokenSource{loadErr: context.Canceled},
			staticFingerprint{},
			10*time.Millisecond,
		)
		if err := rt.Run(ctx); err != nil {
			t.Fatalf("Run() error type = %T, want clean cancellation", err)
		}
	})

	t.Run("enrollment", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		rt := newRuntimeWithDeps(t,
			agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"},
			slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
			&fakeClient{enrollErr: context.Canceled},
			staticTokenSource{},
			staticFingerprint{},
			10*time.Millisecond,
		)
		if err := rt.Run(ctx); err != nil {
			t.Fatalf("Run() error type = %T, want clean cancellation", err)
		}
	})

	t.Run("durable sync", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		client := &fakeClient{syncFunc: func(agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
			cancel()
			return nil, context.Canceled
		}}
		tokenSource := &syncCredentialTokenSource{
			monitoringInstanceID: "monitoringInstance-current",
			syncToken:            "sync-token-current",
			hasCredentials:       true,
		}
		rt := newRuntimeWithRuntimeDeps(t,
			agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"},
			slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
			client,
			tokenSource,
			staticFingerprint{},
			&fakeHostSampleProvider{},
			&fakeProbeProvider{},
			10*time.Millisecond,
		)
		if err := rt.Run(ctx); err != nil {
			t.Fatalf("Run() error type = %T, want clean cancellation", err)
		}
	})
}

func TestRuntimeUpdatesPlanAndAttachesDueHostSampleAndProbeObservations(t *testing.T) {
	withoutDockerCLI(t)
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

	rt := newRuntimeWithRuntimeDeps(t, cfg, nil, client, staticTokenSource{}, staticFingerprint{}, hostProvider, probeProvider, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client.cancel = cancel

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T", err)
	}

	if client.syncCalls < 2 {
		t.Fatalf("Sync() calls = %d, want at least 2", client.syncCalls)
	}
	firstSync := client.syncRequests[0]
	if len(firstSync.HostSamples) != 0 || len(firstSync.ProbeObservations) != 0 {
		t.Fatal("first sync should be heartbeat-only before plan arrives")
	}
	secondSync := client.syncRequests[1]
	if len(secondSync.HostSamples) != 1 {
		t.Fatalf("len(secondSync.HostSamples) = %d, want 1", len(secondSync.HostSamples))
	}
	if secondSync.HostSamples[0].AgentVersion != "dev" || secondSync.HostSamples[0].Fingerprint != "fp-001" || secondSync.HostSamples[0].SyncBatchID == "" {
		t.Fatal("host sample metadata was not populated")
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
		t.Fatal("probe observation metadata was not populated")
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

	rt := newRuntimeWithRuntimeDeps(t, cfg, nil, client, staticTokenSource{}, staticFingerprint{}, hostProvider, probeProvider, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client.cancel = cancel

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T", err)
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

	rt := newRuntimeWithRuntimeDeps(t, cfg, nil, client, staticTokenSource{}, staticFingerprint{}, hostProvider, probeProvider, 10*time.Millisecond)

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T", err)
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
	fileStore := newRuntimeTestStore(t, t.TempDir()+"/buffer.json", syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour, SkipFsync: true})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	store := &cancelAfterDeleteQueue{
		runtimeTestStore: fileStore,
		afterDeletes:     3,
		cancel:           cancel,
	}
	hostProvider := &fakeHostSampleProvider{result: agentapi.HostSamplePayload{CPUUsagePct: 12.5}}
	rt := newRuntimeWithRuntimeDeps(t, cfg, nil, client, staticTokenSource{}, staticFingerprint{}, hostProvider, &fakeProbeProvider{}, 10*time.Millisecond, store)

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T", err)
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
		t.Fatalf("sync request count = %d, want one retried backfilled request", len(client.syncRequests))
	}
	if len(retried.Heartbeats) == 0 || !retried.Heartbeats[0].IsBackfilled {
		t.Fatalf("retried heartbeat count = %d, want a backfilled heartbeat", len(retried.Heartbeats))
	}
	if len(retried.HostSamples) == 0 || !retried.HostSamples[0].IsBackfilled {
		t.Fatalf("retried host sample count = %d, want a backfilled host sample", len(retried.HostSamples))
	}
	entries, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("queue List() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("len(queue entries) = %d, want 0 after ack", len(entries))
	}
}

func TestRuntimeRejectedPersistedIdentityDoesNotBlockCurrentCredentialHeartbeat(t *testing.T) {
	withoutDockerCLI(t)

	const (
		oldMonitoringInstanceID = "monitoringInstance-old"
		oldSyncToken            = "sync-token-old"
		currentMonitoringID     = "monitoringInstance-current"
		currentSyncToken        = "sync-token-current"
	)
	path := filepath.Join(t.TempDir(), "buffer.json")
	fileStore := newRuntimeTestStore(t, path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour, SkipFsync: true})
	oldRequest := agentapi.SyncRequest{
		MonitoringInstanceID: oldMonitoringInstanceID,
		SyncToken:            oldSyncToken,
		Heartbeats: []agentapi.MonitoringInstanceHeartbeat{{
			ObservedAt:   time.Now().UTC().Add(-time.Minute),
			AgentVersion: "old-version",
			Fingerprint:  "fp-old",
			SyncBatchID:  "old-batch",
		}},
	}
	if _, err := fileStore.Enqueue(context.Background(), oldRequest); err != nil {
		t.Fatalf("seed Enqueue() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	store := &cancelAfterDeleteQueue{runtimeTestStore: fileStore, afterDeletes: 2, cancel: cancel}
	var logs bytes.Buffer
	client := &fakeClient{}
	client.syncFunc = func(request agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
		switch request.MonitoringInstanceID {
		case oldMonitoringInstanceID:
			return nil, &enroll.RemoteError{
				StatusCode: 401,
				Code:       agentapi.ErrorCodeInvalidSyncToken,
				Message:    "old credential rejected",
			}
		case currentMonitoringID:
			return &agentapi.SyncResponse{AcceptedAt: time.Now().UTC(), Status: "accepted"}, nil
		default:
			t.Fatal("Sync() received an unexpected monitoring authority")
			return nil, nil
		}
	}
	tokenSource := &syncCredentialTokenSource{
		monitoringInstanceID: currentMonitoringID,
		syncToken:            currentSyncToken,
		hasCredentials:       true,
	}
	rt := newRuntimeWithRuntimeDeps(t,
		agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"},
		slog.New(slog.NewTextHandler(&logs, nil)),
		client,
		tokenSource,
		staticFingerprint{},
		&fakeHostSampleProvider{},
		&fakeProbeProvider{},
		10*time.Millisecond,
		store,
	)

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T, want nil", err)
	}

	currentAttempted := false
	for _, request := range client.syncRequests {
		if request.MonitoringInstanceID == currentMonitoringID {
			currentAttempted = true
			break
		}
	}
	if !currentAttempted {
		t.Fatalf("Sync() request count = %d, current heartbeat was blocked behind rejected persisted identity", len(client.syncRequests))
	}
	entries, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("queue List() error = %v", err)
	}
	for _, entry := range entries {
		if entry.Request.MonitoringInstanceID == oldMonitoringInstanceID {
			t.Fatal("queue still contains the stale authority entry")
		}
	}
}

func TestRuntimeDiscardsPersistedQueueEntriesOutsideCurrentAuthority(t *testing.T) {
	withoutDockerCLI(t)

	const (
		currentMonitoringID = "monitoringInstance-current"
		currentSyncToken    = "sync-token-current-secret"
		currentFingerprint  = "fp-001"
	)
	baseRequest := func() agentapi.SyncRequest {
		observedAt := time.Now().UTC().Add(-time.Minute)
		return agentapi.SyncRequest{
			SessionID:            currentSyncToken,
			MonitoringInstanceID: currentMonitoringID,
			SyncToken:            currentSyncToken,
			Heartbeats: []agentapi.MonitoringInstanceHeartbeat{{
				ObservedAt:   observedAt,
				AgentVersion: "old-version",
				Fingerprint:  currentFingerprint,
				SyncBatchID:  "persisted-batch",
			}},
			HostSamples: []agentapi.HostSamplePayload{{
				ObservedAt:   observedAt,
				AgentVersion: "old-version",
				Fingerprint:  currentFingerprint,
				SyncBatchID:  "persisted-batch",
			}},
			ProbeObservations: []agentapi.ProbeObservationPayload{{
				ObservedAt:   observedAt,
				AgentVersion: "old-version",
				Fingerprint:  currentFingerprint,
				SyncBatchID:  "persisted-batch",
			}},
			IPQualityReports: []agentapi.IPQualityReportPayload{{
				ObservedAt:   observedAt,
				AgentVersion: "old-version",
				Fingerprint:  currentFingerprint,
				SyncBatchID:  "persisted-batch",
			}},
		}
	}
	tests := []struct {
		name   string
		reason string
		mutate func(*agentapi.SyncRequest)
	}{
		{name: "monitoring instance mismatch", reason: "monitoring_instance_mismatch", mutate: func(request *agentapi.SyncRequest) {
			request.MonitoringInstanceID = "monitoringInstance-old"
		}},
		{name: "sync token mismatch", reason: "sync_token_mismatch", mutate: func(request *agentapi.SyncRequest) {
			request.SyncToken = "sync-token-old-secret"
		}},
		{name: "missing heartbeat", reason: "missing_heartbeat", mutate: func(request *agentapi.SyncRequest) {
			request.Heartbeats = nil
		}},
		{name: "heartbeat fingerprint mismatch", reason: "heartbeat_fingerprint_mismatch", mutate: func(request *agentapi.SyncRequest) {
			request.Heartbeats[0].Fingerprint = "fp-old-heartbeat"
		}},
		{name: "host sample fingerprint mismatch", reason: "host_sample_fingerprint_mismatch", mutate: func(request *agentapi.SyncRequest) {
			request.HostSamples[0].Fingerprint = "fp-old-host"
		}},
		{name: "probe fingerprint mismatch", reason: "probe_fingerprint_mismatch", mutate: func(request *agentapi.SyncRequest) {
			request.ProbeObservations[0].Fingerprint = "fp-old-probe"
		}},
		{name: "ip quality fingerprint mismatch", reason: "ip_quality_fingerprint_mismatch", mutate: func(request *agentapi.SyncRequest) {
			request.IPQualityReports[0].Fingerprint = "fp-old-ip"
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := baseRequest()
			tt.mutate(&request)

			path := filepath.Join(t.TempDir(), "buffer.json")
			fileStore := newRuntimeTestStore(t, path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour, SkipFsync: true})
			if _, err := fileStore.Enqueue(context.Background(), request); err != nil {
				t.Fatalf("seed Enqueue() error = %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			store := &cancelAfterDeleteQueue{runtimeTestStore: fileStore, afterDeletes: 2, cancel: cancel}
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			client := &fakeClient{syncFunc: func(sent agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
				if sent.MonitoringInstanceID != currentMonitoringID || sent.SyncToken != currentSyncToken {
					t.Fatal("Sync() sent stale authority material")
				}
				return &agentapi.SyncResponse{AcceptedAt: time.Now().UTC(), Status: "accepted"}, nil
			}}
			tokenSource := &syncCredentialTokenSource{
				monitoringInstanceID: currentMonitoringID,
				syncToken:            currentSyncToken,
				hasCredentials:       true,
			}
			rt := newRuntimeWithRuntimeDeps(t,
				agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"},
				logger,
				client,
				tokenSource,
				staticFingerprint{},
				&fakeHostSampleProvider{},
				&fakeProbeProvider{},
				10*time.Millisecond,
				store,
			)

			if err := rt.Run(ctx); err != nil {
				t.Fatalf("Run() error type = %T, want nil", err)
			}
			if len(client.syncRequests) != 1 {
				t.Fatalf("Sync() requests = %d, want only current heartbeat", len(client.syncRequests))
			}
			if store.markCalls != 0 {
				t.Fatalf("MarkAttempt() calls = %d, want 0 for locally discarded stale authority", store.markCalls)
			}
			logText := logs.String()
			for _, wanted := range []string{"discard", tt.reason} {
				if !strings.Contains(logText, wanted) {
					t.Fatalf("logs missing stable diagnostic %q", wanted)
				}
			}
			for _, forbidden := range []string{currentSyncToken, "sync-token-old-secret", currentFingerprint, "fp-old"} {
				if forbidden != "" && strings.Contains(logText, forbidden) {
					t.Fatal("logs contained a forbidden token or fingerprint fixture")
				}
			}
		})
	}
}

func TestRuntimeBulkDiscardsLargeStaleAuthorityBacklog(t *testing.T) {
	const (
		currentMonitoringID = "monitoringInstance-current"
		currentSyncToken    = "sync-token-current"
		staleEntries        = 256
	)
	queue := &fileQueueFaults{}
	for i := 0; i < staleEntries; i++ {
		batchID := "stale-batch-" + strconv.Itoa(i)
		queue.entries = append(queue.entries, syncqueue.Entry{
			ID: batchID,
			Request: validQueueRequest(
				"monitoringInstance-stale",
				"sync-token-stale",
				"fp-stale",
				batchID,
				time.Now().UTC().Add(-time.Duration(staleEntries-i)*time.Second),
			),
		})
	}
	// Persist the large fixture before starting the runtime deadline. This
	// exercises real FileStore allocation without timing fixture construction
	// as part of the single-round bulk-discard behavior.
	queue.prepareTestQueue(t)
	if err := queue.OpenSession(context.Background(), currentSyncToken); err != nil {
		t.Fatalf("seed queue: %v", err)
	}
	if err := queue.Close(); err != nil {
		t.Fatalf("close seeded queue: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client := &fakeClient{cancelAfterSyncs: 1, cancel: cancel}
	var logs bytes.Buffer
	rt := newQueueTestRuntime(t,
		slog.New(slog.NewTextHandler(&logs, nil)),
		client,
		queue,
		currentMonitoringID,
		currentSyncToken,
	)

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T, want nil", err)
	}
	if queue.deleteManyCalls != 1 {
		t.Fatalf("DeleteMany() calls = %d, want one atomic stale-backlog delete", queue.deleteManyCalls)
	}
	if queue.deleteCalls != 1 {
		t.Fatalf("Delete() calls = %d, want only the acknowledged current heartbeat delete", queue.deleteCalls)
	}
	if client.syncCalls != 1 {
		t.Fatalf("Sync() calls = %d, want only the current heartbeat", client.syncCalls)
	}
	if got := strings.Count(logs.String(), "discard stale sync queue entries"); got != 1 {
		t.Fatalf("aggregated stale discard log count = %d, want 1", got)
	}
}

func TestRuntimeCanonicalIDsSeparateStaleAndCurrentFacts(t *testing.T) {
	const (
		currentMonitoringID = "monitoringInstance-current"
		currentSyncToken    = "sync-token-current"
		sharedEntryID       = "shared-entry-id"
	)
	queue := &fileQueueFaults{entries: []syncqueue.Entry{
		{
			ID: sharedEntryID,
			Request: validQueueRequest(
				"monitoringInstance-stale",
				"sync-token-stale",
				"fp-stale",
				"stale-batch",
				time.Now().UTC().Add(-2*time.Minute),
			),
		},
		{
			ID: sharedEntryID,
			Request: validQueueRequest(
				currentMonitoringID,
				currentSyncToken,
				"fp-001",
				"current-backlog-batch",
				time.Now().UTC().Add(-time.Minute),
			),
		},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client := &fakeClient{cancelAfterSyncs: 1, cancel: cancel}
	var logs bytes.Buffer
	rt := newQueueTestRuntime(t,
		slog.New(slog.NewTextHandler(&logs, nil)),
		client,
		queue,
		currentMonitoringID,
		currentSyncToken,
	)

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T, want nil", err)
	}
	if queue.deleteManyCalls != 1 {
		t.Fatalf("DeleteMany() calls = %d, want stale entry retired separately from the canonically distinct current entry", queue.deleteManyCalls)
	}
	if client.syncCalls != 2 || syncBatchID(client.syncRequests[0]) != "current-backlog-batch" {
		t.Fatalf("Sync() sequence match = false (count=%d), want retained backlog then current heartbeat", client.syncCalls)
	}
	for _, request := range client.syncRequests {
		if syncBatchID(request) == "stale-batch" {
			t.Fatal("runtime sent the stale identifier-collision entry")
		}
	}
	if !strings.Contains(logs.String(), "discard stale sync queue entries") {
		t.Fatal("runtime did not report durable removal of stale authority")
	}
}

func TestRuntimeRejectsNonemptyLegacyQueueWithoutChangingFacts(t *testing.T) {
	withoutDockerCLI(t)

	const (
		monitoringInstanceID = "monitoringInstance-current"
		syncToken            = "sync-token-current"
		sharedEntryID        = "shared-entry-id"
	)
	path := filepath.Join(t.TempDir(), "buffer.json")
	createdAt := time.Now().UTC().Add(-2 * time.Minute)
	persisted := []syncqueue.Entry{
		{
			ID:        sharedEntryID,
			CreatedAt: createdAt,
			Request:   validQueueRequest(monitoringInstanceID, syncToken, "fp-001", "current-backlog-one", createdAt),
		},
		{
			ID:        sharedEntryID,
			CreatedAt: createdAt.Add(time.Minute),
			Request:   validQueueRequest(monitoringInstanceID, syncToken, "fp-001", "current-backlog-two", createdAt.Add(time.Minute)),
		},
	}
	payload, err := json.Marshal(persisted)
	if err != nil {
		t.Fatalf("encode duplicate-ID queue fixture: %v", err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write duplicate-ID queue fixture: %v", err)
	}
	store := newRuntimeTestStore(t, path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour, SkipFsync: true})

	client := &fakeClient{}
	rt := newQueueTestRuntime(t, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), client, store, monitoringInstanceID, syncToken)
	if err := rt.Run(context.Background()); err == nil {
		t.Fatal("nonempty legacy queue must block protocol cutover")
	}
	if client.syncCalls != 0 {
		t.Fatal("legacy facts sent with new replay authority")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, payload) {
		t.Fatal("legacy facts modified before explicit drain")
	}
}

func TestRuntimeDiscardsExplicitInvalidQueueEntryAndContinues(t *testing.T) {
	withoutDockerCLI(t)

	const (
		monitoringInstanceID = "monitoringInstance-current"
		syncToken            = "sync-token-current-secret"
		remoteMessage        = "untrusted response body secret"
	)
	for _, code := range []string{agentapi.ErrorCodeInvalidJSON, agentapi.ErrorCodeInvalidRequest} {
		t.Run(code, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "buffer.json")
			fileStore := newRuntimeTestStore(t, path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour, SkipFsync: true})
			poison := validQueueRequest(monitoringInstanceID, syncToken, "fp-001", "poison-batch", time.Now().UTC().Add(-time.Minute))
			if _, err := fileStore.Enqueue(context.Background(), poison); err != nil {
				t.Fatalf("seed Enqueue() error = %v", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			store := &cancelAfterDeleteQueue{runtimeTestStore: fileStore, afterDeletes: 2, cancel: cancel}
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			client := &fakeClient{syncFunc: func(request agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
				if syncBatchID(request) == "poison-batch" {
					return nil, &enroll.RemoteError{StatusCode: http.StatusBadRequest, Code: code, Message: remoteMessage}
				}
				return &agentapi.SyncResponse{AcceptedAt: time.Now().UTC(), Status: "accepted"}, nil
			}}
			rt := newQueueTestRuntime(t, logger, client, store, monitoringInstanceID, syncToken)

			if err := rt.Run(ctx); err != nil {
				t.Fatalf("Run() error type = %T, want nil", err)
			}
			if len(client.syncRequests) != 2 || syncBatchID(client.syncRequests[0]) != "poison-batch" || syncBatchID(client.syncRequests[1]) == "poison-batch" {
				t.Fatalf("Sync() sequence match = false (count=%d), want poison then current heartbeat", len(client.syncRequests))
			}
			if store.markCalls != 1 {
				t.Fatalf("MarkAttempt() calls = %d, want 1 before poison discard", store.markCalls)
			}
			logText := logs.String()
			for _, wanted := range []string{"kind=remote", "action=discard", "status=400", "code=" + code} {
				if !strings.Contains(logText, wanted) {
					t.Fatalf("logs missing stable diagnostic %q", wanted)
				}
			}
			for _, forbidden := range []string{syncToken, "fp-001", remoteMessage} {
				if strings.Contains(logText, forbidden) {
					t.Fatal("logs contained a forbidden token, fingerprint, or remote-message fixture")
				}
			}
		})
	}
}

func TestRuntimeCurrentAuthorityPermanentRemoteErrorsAreTerminalAndSanitized(t *testing.T) {
	withoutDockerCLI(t)

	const (
		monitoringInstanceID = "monitoringInstance-current"
		syncToken            = "sync-token-current-secret"
		remoteMessage        = "untrusted response body secret"
	)
	tests := []struct {
		name               string
		status             int
		code               string
		diagnosticCodeSafe bool
	}{
		{name: "invalid sync token", status: http.StatusUnauthorized, code: agentapi.ErrorCodeInvalidSyncToken, diagnosticCodeSafe: true},
		{name: "missing instance", status: http.StatusNotFound, code: agentapi.ErrorCodeMonitoringInstanceNotFound, diagnosticCodeSafe: true},
		{name: "binding rejected", status: http.StatusConflict, code: agentapi.ErrorCodeBindingNotAccepted, diagnosticCodeSafe: true},
		{name: "method not allowed", status: http.StatusMethodNotAllowed, code: agentapi.ErrorCodeMethodNotAllowed, diagnosticCodeSafe: true},
		{name: "invalid request code on non-bad-request status", status: http.StatusNotFound, code: agentapi.ErrorCodeInvalidRequest, diagnosticCodeSafe: true},
		{name: "bad request with unknown code", status: http.StatusBadRequest, code: "future_bad_request"},
		{name: "other client rejection", status: http.StatusTeapot, code: "future_client_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fileStore := newRuntimeTestStore(t, filepath.Join(t.TempDir(), "buffer.json"), syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour, SkipFsync: true})
			store := &cancelAfterDeleteQueue{runtimeTestStore: fileStore}
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			client := &fakeClient{syncFunc: func(agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
				return nil, &enroll.RemoteError{StatusCode: tt.status, Code: tt.code, Message: remoteMessage}
			}}
			rt := newQueueTestRuntime(t, logger, client, store, monitoringInstanceID, syncToken)
			ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
			defer cancel()

			err := rt.Run(ctx)
			if err == nil {
				t.Fatal("Run() error = nil, want terminal remote error")
			}
			var remoteErr *enroll.RemoteError
			if !errors.As(err, &remoteErr) {
				t.Fatalf("Run() error = %T, want wrapped *enroll.RemoteError", err)
			}
			if remoteErr.StatusCode != tt.status || remoteErr.Code != tt.code {
				t.Fatalf("wrapped remote error fields did not preserve the typed cause (status=%d)", remoteErr.StatusCode)
			}
			if client.syncCalls != 1 {
				t.Fatalf("Sync() calls = %d, want 1 terminal attempt", client.syncCalls)
			}
			if store.markCalls != 1 {
				t.Fatalf("MarkAttempt() calls = %d, want 1", store.markCalls)
			}
			entries, listErr := store.List(context.Background())
			if listErr != nil {
				t.Fatalf("queue List() error = %v", listErr)
			}
			if len(entries) != 1 || entries[0].Attempts != 1 {
				t.Fatalf("queue entry count=%d attempts=%v, want one retained terminal entry with one attempt", len(entries), queueEntryAttempts(entries))
			}
			combined := err.Error() + logs.String()
			wantedDiagnostics := []string{"kind=remote", "action=terminal", "status=" + httpStatus(tt.status)}
			if tt.diagnosticCodeSafe {
				wantedDiagnostics = append(wantedDiagnostics, "code="+tt.code)
			}
			for _, wanted := range wantedDiagnostics {
				if !strings.Contains(combined, wanted) {
					t.Fatalf("diagnostics missing stable field %q", wanted)
				}
			}
			if !tt.diagnosticCodeSafe && strings.Contains(combined, tt.code) {
				t.Fatal("diagnostics contained a non-allowlisted remote error code")
			}
			for _, forbidden := range []string{syncToken, "fp-001", remoteMessage} {
				if strings.Contains(combined, forbidden) {
					t.Fatal("diagnostics contained a forbidden token, fingerprint, or remote-message fixture")
				}
			}
			if strings.Contains(logs.String(), "action=terminal") {
				t.Fatal("runtime logged a terminal error that must be logged only by the process entrypoint")
			}
		})
	}
}

func TestRuntimeLogsDiscardOnlyAfterQueueDeleteSucceeds(t *testing.T) {
	withoutDockerCLI(t)

	const (
		monitoringInstanceID = "monitoringInstance-current"
		syncToken            = "sync-token-current-secret"
		remoteMessage        = "untrusted response body secret"
	)
	deleteErr := errors.New("queue delete denied")
	tests := []struct {
		name       string
		queueEntry *syncqueue.Entry
		syncErr    error
		logMessage string
	}{
		{
			name: "stale authority",
			queueEntry: &syncqueue.Entry{
				ID:      "stale-batch",
				Request: validQueueRequest("monitoringInstance-old", "sync-token-old-secret", "fp-old", "stale-batch", time.Now().UTC().Add(-time.Minute)),
			},
			logMessage: "discard stale sync queue entries",
		},
		{
			name:       "rejected poison",
			syncErr:    &enroll.RemoteError{StatusCode: http.StatusBadRequest, Code: agentapi.ErrorCodeInvalidRequest, Message: remoteMessage},
			logMessage: "discard rejected sync queue entry",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queue := &fileQueueFaults{deleteErr: deleteErr}
			if tt.queueEntry != nil {
				queue.entries = append(queue.entries, *tt.queueEntry)
			}
			client := &fakeClient{}
			if tt.syncErr != nil {
				client.syncFunc = func(agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
					return nil, tt.syncErr
				}
			}
			var logs bytes.Buffer
			rt := newQueueTestRuntime(t, slog.New(slog.NewTextHandler(&logs, nil)), client, queue, monitoringInstanceID, syncToken)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			err := rt.Run(ctx)
			if !errors.Is(err, deleteErr) {
				t.Fatalf("Run() error type = %T, want queue delete error", err)
			}
			if strings.Contains(logs.String(), tt.logMessage) {
				t.Fatalf("logs claimed a discard before queue deletion succeeded: %q", tt.logMessage)
			}
			for _, forbidden := range []string{syncToken, "sync-token-old-secret", "fp-001", "fp-old", remoteMessage} {
				if strings.Contains(logs.String(), forbidden) {
					t.Fatal("logs contained a forbidden token, fingerprint, or remote-message fixture")
				}
			}
		})
	}
}

func TestRuntimeDoesNotExposePersistedQueueIdentifiersOrLocalCauses(t *testing.T) {
	const (
		currentMonitoringID = "monitoringInstance-current-secret"
		currentSyncToken    = "sync-token-current-secret"
		staleEntryID        = "entry-id-secret\nforged_log=true"
		staleMonitoringID   = "monitoringInstance-stale-secret"
		staleSyncToken      = "sync-token-stale-secret"
		staleFingerprint    = "fp-stale-secret"
	)
	staleEntry := syncqueue.Entry{
		ID: staleEntryID,
		Request: validQueueRequest(
			staleMonitoringID,
			staleSyncToken,
			staleFingerprint,
			"stale-batch",
			time.Now().UTC().Add(-time.Minute),
		),
	}

	t.Run("successful stale discard log", func(t *testing.T) {
		queue := &fileQueueFaults{entries: []syncqueue.Entry{staleEntry}}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		client := &fakeClient{cancelAfterSyncs: 1, cancel: cancel}
		var logs bytes.Buffer
		rt := newQueueTestRuntime(t,
			slog.New(slog.NewTextHandler(&logs, nil)),
			client,
			queue,
			currentMonitoringID,
			currentSyncToken,
		)

		if err := rt.Run(ctx); err != nil {
			t.Fatalf("Run() error type = %T, want nil", err)
		}
		if !strings.Contains(logs.String(), "reason=monitoring_instance_mismatch") {
			t.Fatal("stale discard log missing the stable mismatch reason")
		}
		for _, forbidden := range []string{
			staleEntryID,
			"forged_log=true",
			staleMonitoringID,
			staleSyncToken,
			staleFingerprint,
			currentMonitoringID,
			currentSyncToken,
		} {
			if strings.Contains(logs.String(), forbidden) {
				t.Fatal("stale discard log exposed a persisted identifier or secret")
			}
		}
	})

	t.Run("stale delete durability failure", func(t *testing.T) {
		localCause := errors.New("delete failed sync-token-local-cause-secret")
		queue := &fileQueueFaults{
			entries:   []syncqueue.Entry{staleEntry},
			deleteErr: localCause,
		}
		var logs bytes.Buffer
		rt := newQueueTestRuntime(t,
			slog.New(slog.NewTextHandler(&logs, nil)),
			&fakeClient{},
			queue,
			currentMonitoringID,
			currentSyncToken,
		)

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		err := rt.Run(ctx)
		if !errors.Is(err, localCause) {
			t.Fatal("Run() did not preserve the local queue durability cause")
		}
		diagnostic := err.Error() + logs.String()
		for _, forbidden := range []string{
			staleEntryID,
			"forged_log=true",
			staleMonitoringID,
			staleSyncToken,
			staleFingerprint,
			currentMonitoringID,
			currentSyncToken,
			localCause.Error(),
		} {
			if strings.Contains(diagnostic, forbidden) {
				t.Fatal("queue durability diagnostic exposed a persisted identifier, secret, or raw local cause")
			}
		}
	})

	t.Run("remote poison discard log", func(t *testing.T) {
		const poisonEntryID = "poison-entry-secret\nforged_poison=true"
		poison := syncqueue.Entry{
			ID: poisonEntryID,
			Request: validQueueRequest(
				currentMonitoringID,
				currentSyncToken,
				"fp-001",
				"poison-batch",
				time.Now().UTC().Add(-time.Minute),
			),
		}
		queue := &fileQueueFaults{entries: []syncqueue.Entry{poison}}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		client := &fakeClient{syncFunc: func(request agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
			if syncBatchID(request) == "poison-batch" {
				return nil, &enroll.RemoteError{
					StatusCode: http.StatusBadRequest,
					Code:       agentapi.ErrorCodeInvalidRequest,
					Message:    "remote-body-secret",
				}
			}
			cancel()
			return &agentapi.SyncResponse{AcceptedAt: time.Now().UTC(), Status: "accepted"}, nil
		}}
		var logs bytes.Buffer
		rt := newQueueTestRuntime(t,
			slog.New(slog.NewTextHandler(&logs, nil)),
			client,
			queue,
			currentMonitoringID,
			currentSyncToken,
		)

		if err := rt.Run(ctx); err != nil {
			t.Fatalf("Run() error type = %T, want nil", err)
		}
		for _, forbidden := range []string{
			poisonEntryID,
			"forged_poison=true",
			currentMonitoringID,
			currentSyncToken,
			"remote-body-secret",
		} {
			if strings.Contains(logs.String(), forbidden) {
				t.Fatal("remote poison discard log exposed a persisted identifier or secret")
			}
		}
	})
}

func TestRuntimeTransientQueueFailuresRemainRetryableAndBackfilled(t *testing.T) {
	withoutDockerCLI(t)

	tests := []struct {
		name string
		err  error
	}{
		{name: "remote status zero", err: &enroll.RemoteError{StatusCode: 0, Code: "unknown", Message: "missing status secret"}},
		{name: "unexpected success status error", err: &enroll.RemoteError{StatusCode: http.StatusOK, Code: "unexpected", Message: "success status secret"}},
		{name: "redirect status error", err: &enroll.RemoteError{StatusCode: http.StatusFound, Code: "redirect", Message: "redirect detail secret"}},
		{name: "rate limited", err: &enroll.RemoteError{StatusCode: http.StatusTooManyRequests, Code: "rate_limited", Message: "retry after secret"}},
		{name: "service unavailable", err: &enroll.RemoteError{StatusCode: http.StatusServiceUnavailable, Code: agentapi.ErrorCodeInternalError, Message: "database detail secret"}},
		{name: "other server error", err: &enroll.RemoteError{StatusCode: http.StatusBadGateway, Code: agentapi.ErrorCodeInternalError, Message: "proxy detail secret"}},
		{name: "server error with invalid request code", err: &enroll.RemoteError{StatusCode: http.StatusInternalServerError, Code: agentapi.ErrorCodeInvalidRequest, Message: "mismatched status secret"}},
		{name: "wrapped server error", err: fmt.Errorf("stable client wrapper: %w", &enroll.RemoteError{StatusCode: http.StatusServiceUnavailable, Code: agentapi.ErrorCodeInternalError, Message: "wrapped detail secret"})},
		{name: "transport error", err: errors.New("dial center unavailable sync-token-current-secret fp-001")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fileStore := newRuntimeTestStore(t, filepath.Join(t.TempDir(), "buffer.json"), syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour, SkipFsync: true})
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			store := &cancelAfterDeleteQueue{runtimeTestStore: fileStore, afterDeletes: 1, cancel: cancel}
			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			client := &fakeClient{}
			client.syncFunc = func(request agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
				if client.syncCalls == 1 {
					return nil, tt.err
				}
				return &agentapi.SyncResponse{AcceptedAt: time.Now().UTC(), Status: "accepted"}, nil
			}
			logicalNow := time.Now()
			rt := newQueueTestRuntimeWithClock(t, logger, client, store, "monitoringInstance-current", "sync-token-current-secret", func() time.Time {
				// This classification/privacy test advances beyond congestion
				// cooldown; exact cooldown boundaries have separate regressions.
				logicalNow = logicalNow.Add(time.Minute)
				return logicalNow
			})

			if err := rt.Run(ctx); err != nil {
				t.Fatalf("Run() error type = %T, want nil", err)
			}
			if client.syncCalls < 2 {
				t.Fatalf("Sync() calls = %d, want retry", client.syncCalls)
			}
			firstBatch := syncBatchID(client.syncRequests[0])
			if syncBatchID(client.syncRequests[1]) != firstBatch || !client.syncRequests[1].Heartbeats[0].IsBackfilled {
				t.Fatalf("second Sync() retry match = false (backfilled=%t)", client.syncRequests[1].Heartbeats[0].IsBackfilled)
			}
			if store.markCalls != 1 {
				t.Fatalf("MarkAttempt() calls = %d, want 1", store.markCalls)
			}
			logText := logs.String()
			for _, wanted := range []string{
				"sync queue replay progress",
				"state=retrying",
				"error=\"sync queue replay retry pending\"",
				"acked_entries=0",
				"remaining_entries=1",
			} {
				if !strings.Contains(logText, wanted) {
					t.Fatalf("logs missing stable retry diagnostic %q", wanted)
				}
			}
			var remoteErr *enroll.RemoteError
			if errors.As(tt.err, &remoteErr) {
				wantedDiagnostics := []string{"kind=remote", "action=retry"}
				if remoteErr.StatusCode > 0 {
					wantedDiagnostics = append(wantedDiagnostics, "status="+httpStatus(remoteErr.StatusCode))
				}
				for _, wanted := range wantedDiagnostics {
					if !strings.Contains(logText, wanted) {
						t.Fatalf("logs missing stable diagnostic %q", wanted)
					}
				}
				if remoteErr.Code == agentapi.ErrorCodeInternalError && !strings.Contains(logText, "code="+agentapi.ErrorCodeInternalError) {
					t.Fatal("logs missing allowlisted remote error code")
				}
				if remoteErr.Code == "rate_limited" && strings.Contains(logText, remoteErr.Code) {
					t.Fatal("logs contained a non-allowlisted remote error code")
				}
				if strings.Contains(logText, remoteErr.Message) {
					t.Fatal("logs contained a forbidden remote-message fixture")
				}
			} else if !strings.Contains(logText, "kind=transport action=retry") {
				t.Fatal("logs missing stable transport retry diagnostic")
			}
			for _, forbidden := range []string{"sync-token-current-secret", "fp-001"} {
				if strings.Contains(logText, forbidden) {
					t.Fatal("logs contained a forbidden token or fingerprint fixture")
				}
			}
		})
	}
}

func TestRuntimePreservesOldestFirstForValidCurrentAuthorityBacklog(t *testing.T) {
	withoutDockerCLI(t)

	const (
		monitoringInstanceID = "monitoringInstance-current"
		syncToken            = "sync-token-current"
	)
	fileStore := newRuntimeTestStore(t, filepath.Join(t.TempDir(), "buffer.json"), syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour, SkipFsync: true})
	for i, batchID := range []string{"backlog-one", "backlog-two"} {
		request := validQueueRequest(monitoringInstanceID, syncToken, "fp-001", batchID, time.Now().UTC().Add(time.Duration(i-2)*time.Minute))
		if _, err := fileStore.Enqueue(context.Background(), request); err != nil {
			t.Fatalf("seed Enqueue() error type = %T", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	store := &cancelAfterDeleteQueue{runtimeTestStore: fileStore, afterDeletes: 3, cancel: cancel}
	client := &fakeClient{}
	rt := newQueueTestRuntime(t, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), client, store, monitoringInstanceID, syncToken)

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T, want nil", err)
	}
	if len(client.syncRequests) != 3 {
		t.Fatalf("Sync() requests = %d, want two backlog entries then current", len(client.syncRequests))
	}
	for i, batchID := range []string{"backlog-one", "backlog-two"} {
		if got := syncBatchID(client.syncRequests[i]); got != batchID {
			t.Fatalf("Sync() oldest-first entry match = false at index %d", i)
		}
		if !client.syncRequests[i].Heartbeats[0].IsBackfilled {
			t.Fatalf("Sync() batch[%d] IsBackfilled = false, want true", i)
		}
	}
	entries, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("queue List() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("queue entry count = %d, want empty after valid backlog ack", len(entries))
	}
}

func TestRuntimeBoundsReplayBeforeCurrentDurableRequest(t *testing.T) {
	withoutDockerCLI(t)

	const (
		monitoringInstanceID = "monitoringInstance-current"
		syncToken            = "sync-token-current"
	)
	baseQueue := &fileQueueFaults{}
	for i, batchID := range []string{"backlog-one", "backlog-two", "backlog-three", "backlog-four"} {
		baseQueue.entries = append(baseQueue.entries, syncqueue.Entry{
			ID:      batchID,
			Request: validQueueRequest(monitoringInstanceID, syncToken, "fp-001", batchID, time.Now().UTC().Add(time.Duration(i-5)*time.Minute)),
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	queue := &cancelAfterSuccessfulDeleteQueue{
		SyncQueue:    baseQueue,
		afterDeletes: 3,
		cancel:       cancel,
	}
	client := &fakeClient{}
	rt := newQueueTestRuntime(t, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), client, queue, monitoringInstanceID, syncToken)

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T, want nil", err)
	}
	if len(client.syncRequests) != 3 {
		t.Fatalf("Sync() calls = %d, want exactly two backlog attempts and current", len(client.syncRequests))
	}
	if queue.deleteCalls != 3 {
		t.Fatalf("durable Delete() calls = %d, want 3", queue.deleteCalls)
	}
	if syncBatchID(client.syncRequests[0]) != "backlog-one" || syncBatchID(client.syncRequests[1]) != "backlog-two" {
		t.Fatal("backlog FIFO order mismatch in the bounded replay lane")
	}
	current := client.syncRequests[2]
	if batchID := syncBatchID(current); strings.HasPrefix(batchID, "backlog-") {
		t.Fatal("third Sync() was backlog, want current durable request")
	}
	if current.Heartbeats[0].IsBackfilled {
		t.Fatal("current durable request was marked backfilled")
	}
	entries, err := queue.List(context.Background())
	if err != nil {
		t.Fatalf("queue List() error type = %T", err)
	}
	if len(entries) != 2 {
		t.Fatalf("remaining queue count = %d, want 2", len(entries))
	}
	if syncBatchID(entries[0].Request) != "backlog-three" || syncBatchID(entries[1].Request) != "backlog-four" {
		t.Fatal("remaining backlog FIFO order mismatch")
	}
}

func TestRuntimeReplaysBacklogFIFOAcrossFreshInterleaving(t *testing.T) {
	withoutDockerCLI(t)

	const (
		monitoringInstanceID = "monitoringInstance-current"
		syncToken            = "sync-token-current"
	)
	baseQueue := &fileQueueFaults{}
	for i := 1; i <= 5; i++ {
		batchID := "backlog-" + strconv.Itoa(i)
		baseQueue.entries = append(baseQueue.entries, syncqueue.Entry{
			ID:      batchID,
			Request: validQueueRequest(monitoringInstanceID, syncToken, "fp-001", batchID, time.Now().UTC().Add(time.Duration(i-6)*time.Minute)),
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	queue := &cancelAfterSuccessfulDeleteQueue{
		SyncQueue:    baseQueue,
		afterDeletes: 8,
		cancel:       cancel,
	}
	client := &fakeClient{}
	rt := newQueueTestRuntime(t, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), client, queue, monitoringInstanceID, syncToken)

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T, want nil", err)
	}
	if len(client.syncRequests) != 8 {
		t.Fatalf("Sync() calls = %d, want 8 across three bounded rounds", len(client.syncRequests))
	}
	if queue.deleteCalls != 8 {
		t.Fatalf("durable Delete() calls = %d, want 8", queue.deleteCalls)
	}
	wantBacklogAt := map[int]string{0: "backlog-1", 1: "backlog-2", 3: "backlog-3", 4: "backlog-4", 6: "backlog-5"}
	for i, request := range client.syncRequests {
		if batchID, isBacklog := wantBacklogAt[i]; isBacklog {
			if got := syncBatchID(request); got != batchID {
				t.Fatalf("backlog FIFO order mismatch at attempt position %d", i)
			}
			if !request.Heartbeats[0].IsBackfilled {
				t.Fatalf("Sync()[%d] backlog heartbeat IsBackfilled = false", i)
			}
			continue
		}
		if got := syncBatchID(request); strings.HasPrefix(got, "backlog-") {
			t.Fatalf("attempt position %d was backlog, want interleaved current request", i)
		}
		if request.Heartbeats[0].IsBackfilled {
			t.Fatalf("Sync()[%d] current heartbeat IsBackfilled = true", i)
		}
	}
	entries, err := queue.List(context.Background())
	if err != nil {
		t.Fatalf("queue List() error type = %T", err)
	}
	if len(entries) != 0 {
		t.Fatalf("queue count after continuous success = %d, want 0", len(entries))
	}
}

func TestRuntimeRetryableBacklogHeadDoesNotBlockCurrentDurableRequest(t *testing.T) {
	withoutDockerCLI(t)

	tests := []struct {
		name string
		err  error
	}{
		{name: "transport", err: errors.New("center unavailable")},
		{name: "rate limited", err: &enroll.RemoteError{StatusCode: http.StatusTooManyRequests, Code: "rate_limited"}},
		{name: "server error", err: &enroll.RemoteError{StatusCode: http.StatusServiceUnavailable, Code: agentapi.ErrorCodeInternalError}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const (
				monitoringInstanceID = "monitoringInstance-current"
				syncToken            = "sync-token-current"
			)
			baseQueue := &fileQueueFaults{entries: []syncqueue.Entry{
				{ID: "backlog-head", Request: validQueueRequest(monitoringInstanceID, syncToken, "fp-001", "backlog-head", time.Now().UTC().Add(-2*time.Minute))},
				{ID: "backlog-later", Request: validQueueRequest(monitoringInstanceID, syncToken, "fp-001", "backlog-later", time.Now().UTC().Add(-time.Minute))},
			}}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			queue := &cancelAfterSuccessfulDeleteQueue{
				SyncQueue:    baseQueue,
				afterDeletes: 1,
				cancel:       cancel,
			}
			headFailed := false
			client := &fakeClient{}
			client.syncFunc = func(request agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
				batchID := syncBatchID(request)
				if batchID == "backlog-head" && !headFailed {
					headFailed = true
					return nil, tt.err
				}
				return &agentapi.SyncResponse{AcceptedAt: time.Now().UTC(), Status: "accepted"}, nil
			}
			rt := newQueueTestRuntime(t, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), client, queue, monitoringInstanceID, syncToken)

			if err := rt.Run(ctx); err != nil {
				t.Fatalf("Run() error type = %T, want nil", err)
			}
			if len(client.syncRequests) != 2 || syncBatchID(client.syncRequests[0]) != "backlog-head" || strings.HasPrefix(syncBatchID(client.syncRequests[1]), "backlog-") {
				t.Fatalf("Sync() ordering/category mismatch across %d attempts", len(client.syncRequests))
			}
			if queue.deleteCalls != 1 {
				t.Fatalf("durable Delete() calls = %d, want 1", queue.deleteCalls)
			}
			if !client.syncRequests[0].Heartbeats[0].IsBackfilled || client.syncRequests[1].Heartbeats[0].IsBackfilled {
				t.Fatal("retryable backlog/current backfill classification was not preserved")
			}
			entries, err := queue.List(context.Background())
			if err != nil {
				t.Fatalf("queue List() error type = %T", err)
			}
			if len(entries) != 2 {
				t.Fatalf("retained queue count = %d, want 2", len(entries))
			}
			if syncBatchID(entries[0].Request) != "backlog-head" || syncBatchID(entries[1].Request) != "backlog-later" {
				t.Fatal("retained backlog FIFO order mismatch")
			}
			if entries[0].Attempts != 1 {
				t.Fatalf("retryable head attempt count = %d, want 1", entries[0].Attempts)
			}
		})
	}
}

func TestRuntimeCurrentRequestRetryRemainsDurableAndBackfilled(t *testing.T) {
	withoutDockerCLI(t)

	baseQueue := &fileQueueFaults{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	queue := &cancelAfterSuccessfulDeleteQueue{
		SyncQueue:    baseQueue,
		afterDeletes: 2,
		cancel:       cancel,
	}
	var failedBatchID string
	client := &fakeClient{}
	client.syncFunc = func(request agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
		switch client.syncCalls {
		case 1:
			failedBatchID = syncBatchID(request)
			return nil, errors.New("temporary transport failure")
		case 2:
			if syncBatchID(request) != failedBatchID || !request.Heartbeats[0].IsBackfilled {
				t.Fatal("failed current request did not become the next round's backfilled head")
			}
		case 3:
			if syncBatchID(request) == failedBatchID || request.Heartbeats[0].IsBackfilled {
				t.Fatal("new round current request was not kept live")
			}
		}
		return &agentapi.SyncResponse{AcceptedAt: time.Now().UTC(), Status: "accepted"}, nil
	}
	rt := newQueueTestRuntime(t, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), client, queue, "monitoringInstance-current", "sync-token-current")

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T, want nil", err)
	}
	if len(client.syncRequests) != 3 {
		t.Fatalf("Sync() attempt count = %d, want 3", len(client.syncRequests))
	}
	if queue.deleteCalls != 2 {
		t.Fatalf("durable Delete() calls = %d, want 2", queue.deleteCalls)
	}
	if len(baseQueue.entries) != 0 {
		t.Fatalf("queue count after retry success = %d, want 0", len(baseQueue.entries))
	}
}

func TestRuntimeCurrentResponsePlanWinsAfterBoundedReplay(t *testing.T) {
	withoutDockerCLI(t)

	const (
		monitoringInstanceID = "monitoringInstance-current"
		syncToken            = "sync-token-current"
	)
	baseQueue := &fileQueueFaults{}
	for i := 1; i <= 3; i++ {
		batchID := "backlog-" + strconv.Itoa(i)
		baseQueue.entries = append(baseQueue.entries, syncqueue.Entry{
			ID:      batchID,
			Request: validQueueRequest(monitoringInstanceID, syncToken, "fp-001", batchID, time.Now().UTC().Add(time.Duration(i-4)*time.Minute)),
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	queue := &cancelAfterSuccessfulDeleteQueue{
		SyncQueue:    baseQueue,
		afterDeletes: 5,
		cancel:       cancel,
	}
	currentResponses := 0
	client := &fakeClient{syncFunc: func(request agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
		if strings.HasPrefix(syncBatchID(request), "backlog-") {
			return &agentapi.SyncResponse{AcceptedAt: time.Now().UTC(), Status: "exact_duplicate", Plan: &agentapi.SyncPlan{}}, nil
		}
		currentResponses++
		if currentResponses == 2 {
			if len(request.HostSamples) != 1 {
				t.Fatalf("second current request host samples = %d, want 1 from the fresh response plan", len(request.HostSamples))
			}
		}
		return &agentapi.SyncResponse{
			AcceptedAt: time.Now().UTC(),
			Status:     "accepted",
			Plan:       &agentapi.SyncPlan{HostSampleFrequencyTier: agentapi.FrequencyTier5s},
		}, nil
	}}
	hostProvider := &fakeHostSampleProvider{result: agentapi.HostSamplePayload{CPUUsagePct: 12.5}}
	tokenSource := &syncCredentialTokenSource{monitoringInstanceID: monitoringInstanceID, syncToken: syncToken, hasCredentials: true}
	rt := newRuntimeWithRuntimeDeps(t,
		agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"},
		slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		client,
		tokenSource,
		staticFingerprint{},
		hostProvider,
		&fakeProbeProvider{},
		10*time.Millisecond,
		queue,
	)

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T, want nil", err)
	}
	if currentResponses != 2 || hostProvider.calls != 1 {
		t.Fatalf("current responses/host collections = %d/%d, want 2/1", currentResponses, hostProvider.calls)
	}
	if queue.deleteCalls != 5 {
		t.Fatalf("durable Delete() calls = %d, want 5", queue.deleteCalls)
	}
}

func TestRuntimeBoundedReplayPreservesDurableAuxiliaryPayloads(t *testing.T) {
	withoutDockerCLI(t)

	enqueueErr := errors.New("queue temporarily unavailable")
	baseQueue := &failNthEnqueueQueue{fileQueueFaults: &fileQueueFaults{}, failAt: 2, err: enqueueErr}
	queue := &cancelAfterSuccessfulDeleteQueue{SyncQueue: baseQueue}
	ipProvider := &fakeIPQualityProvider{reportsAfterStart: []agentapi.IPQualityReportPayload{{
		IPAddress: "203.0.113.10",
		Status:    agentapi.IPQualityStatusSuccess,
	}}}
	tokenSource := &syncCredentialTokenSource{
		monitoringInstanceID: "monitoringInstance-current",
		syncToken:            "sync-token-current",
		hasCredentials:       true,
	}
	var auxBatchID string
	client := &fakeClient{}
	client.syncFunc = func(request agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
		switch client.syncCalls {
		case 1:
			return &agentapi.SyncResponse{
				AcceptedAt: time.Now().UTC(),
				Status:     "accepted",
				Plan: &agentapi.SyncPlan{
					PendingAction: &agentapi.PendingAction{CommandID: "uptime", ActionID: "act_aux"},
					IPQualityPlan: &agentapi.IPQualityPlan{Enabled: true, FrequencySeconds: 86400},
				},
			}, nil
		case 2:
			auxBatchID = syncBatchID(request)
			assertAuxiliaryPayloads(t, request, false)
			entries, err := queue.List(context.Background())
			if err != nil {
				t.Fatalf("queue List() at send boundary error type = %T", err)
			}
			if len(entries) != 1 {
				t.Fatalf("durable carrier count before send = %d, want 1", len(entries))
			}
			if syncBatchID(entries[0].Request) != auxBatchID {
				t.Fatal("durable carrier identity mismatch before send")
			}
			assertAuxiliaryPayloads(t, entries[0].Request, false)
			return nil, errors.New("temporary transport failure")
		case 3:
			if syncBatchID(request) != auxBatchID {
				t.Fatal("retry carrier identity mismatch")
			}
			assertAuxiliaryPayloads(t, request, true)
		case 4:
			if syncBatchID(request) == auxBatchID || request.Heartbeats[0].IsBackfilled {
				t.Fatal("fresh request after auxiliary retry was not live")
			}
		default:
			t.Fatalf("unexpected Sync() call %d", client.syncCalls)
		}
		return &agentapi.SyncResponse{AcceptedAt: time.Now().UTC(), Status: "accepted"}, nil
	}
	rt := newRuntimeWithRuntimeDeps(t,
		agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"},
		slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		client,
		tokenSource,
		staticFingerprint{},
		&fakeHostSampleProvider{},
		&fakeProbeProvider{},
		time.Millisecond,
		queue,
		ipProvider,
	)

	firstCtx, firstCancel := context.WithTimeout(context.Background(), time.Second)
	if err := rt.Run(firstCtx); !errors.Is(err, enqueueErr) {
		firstCancel()
		t.Fatalf("first Run() error type = %T, want enqueue error", err)
	}
	firstCancel()
	if client.syncCalls != 1 {
		t.Fatalf("first Run() Sync() calls = %d, want one plan response before enqueue failure", client.syncCalls)
	}

	baseQueue.failAt = 0
	secondCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	queue.afterDeletes = queue.deleteCalls + 2
	queue.cancel = cancel
	if err := rt.Run(secondCtx); err != nil {
		t.Fatalf("second Run() error type = %T, want nil", err)
	}
	if client.syncCalls != 4 {
		t.Fatalf("Sync() calls = %d, want plan, failed aux current, backfilled aux retry, fresh current", client.syncCalls)
	}
	if queue.deleteCalls != 3 {
		t.Fatalf("durable Delete() calls = %d, want 3 across both runs", queue.deleteCalls)
	}
	if len(baseQueue.entries) != 0 {
		t.Fatalf("queue count after auxiliary retry = %d, want 0", len(baseQueue.entries))
	}
}

func TestRuntimeLocalEntryIDControlsCurrentAndBackfilledOnBatchIDCollision(t *testing.T) {
	withoutDockerCLI(t)

	baseQueue := &localEntryIDCollisionQueue{fileQueueFaults: &fileQueueFaults{}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	queue := &cancelAfterSuccessfulDeleteQueue{
		SyncQueue:    baseQueue,
		afterDeletes: 2,
		cancel:       cancel,
	}
	client := &fakeClient{}
	rt := newQueueTestRuntime(t, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), client, queue, "monitoringInstance-current", "sync-token-current")

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T, want nil", err)
	}
	if len(client.syncRequests) != 2 {
		t.Fatalf("Sync() calls = %d, want colliding backlog and exact current entry", len(client.syncRequests))
	}
	if queue.deleteCalls != 2 {
		t.Fatalf("durable Delete() calls = %d, want 2", queue.deleteCalls)
	}
	if client.syncRequests[0].BatchSequence == client.syncRequests[1].BatchSequence ||
		client.syncRequests[0].Heartbeats[0].SyncBatchID == client.syncRequests[1].Heartbeats[0].SyncBatchID ||
		!client.syncRequests[0].Heartbeats[0].ObservedAt.Equal(client.syncRequests[1].Heartbeats[0].ObservedAt) {
		t.Fatal("identical local payloads did not receive distinct durable batch identities")
	}
	if !client.syncRequests[0].Heartbeats[0].IsBackfilled {
		t.Fatal("pre-existing local entry with colliding batch ID was not backfilled")
	}
	if client.syncRequests[1].Heartbeats[0].IsBackfilled {
		t.Fatal("exact local entry returned by Enqueue was marked backfilled")
	}
}

func TestRuntimeLogsBoundedReplayProgressAfterDurableAck(t *testing.T) {
	withoutDockerCLI(t)

	const (
		monitoringInstanceID = "monitoringInstance-current"
		syncToken            = "sync-token-current"
	)
	t.Run("successful durable acknowledgements", func(t *testing.T) {
		baseQueue := &fileQueueFaults{}
		for i := 1; i <= 4; i++ {
			batchID := "backlog-" + strconv.Itoa(i)
			baseQueue.entries = append(baseQueue.entries, syncqueue.Entry{
				ID:      batchID,
				Request: validQueueRequest(monitoringInstanceID, syncToken, "fp-001", batchID, time.Now().UTC().Add(time.Duration(i-5)*time.Minute)),
			})
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		queue := &cancelAfterSuccessfulDeleteQueue{SyncQueue: baseQueue, afterDeletes: 3, cancel: cancel}
		var logs bytes.Buffer
		rt := newQueueTestRuntimeWithClock(t,
			slog.New(slog.NewTextHandler(&logs, nil)),
			&fakeClient{},
			queue,
			monitoringInstanceID,
			syncToken,
			func() time.Time { return time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC) },
		)

		if err := rt.Run(ctx); err != nil {
			t.Fatalf("Run() error type = %T, want nil", err)
		}
		lines := replayLogLines(logs.String())
		if len(lines) != 1 {
			t.Fatalf("replay progress log count = %d, want 1", len(lines))
		}
		for _, wanted := range []string{"level=INFO", "state=catching_up", "acked_entries=2", "remaining_entries=2"} {
			if !strings.Contains(lines[0], wanted) {
				t.Fatalf("replay progress log missing stable field %q", wanted)
			}
		}
	})

	t.Run("delete failure is not progress", func(t *testing.T) {
		deleteErr := errors.New("durable delete failed with private local cause")
		queue := &fileQueueFaults{
			deleteErr: deleteErr,
			entries: []syncqueue.Entry{{
				ID:      "backlog-one",
				Request: validQueueRequest(monitoringInstanceID, syncToken, "fp-001", "backlog-one", time.Now().UTC().Add(-time.Minute)),
			}},
		}
		var logs bytes.Buffer
		rt := newQueueTestRuntimeWithClock(t,
			slog.New(slog.NewTextHandler(&logs, nil)),
			&fakeClient{},
			queue,
			monitoringInstanceID,
			syncToken,
			time.Now,
		)

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		err := rt.Run(ctx)
		if !errors.Is(err, deleteErr) {
			t.Fatalf("Run() error type = %T, want durable delete error", err)
		}
		if lines := replayLogLines(logs.String()); len(lines) != 0 {
			t.Fatalf("replay progress log count = %d, want 0 after failed durable delete", len(lines))
		}
	})

	t.Run("discard-only round with backlog remaining has no healthy progress", func(t *testing.T) {
		baseQueue := &fileQueueFaults{}
		for i := 1; i <= 3; i++ {
			batchID := "poison-" + strconv.Itoa(i)
			baseQueue.entries = append(baseQueue.entries, syncqueue.Entry{
				ID:      batchID,
				Request: validQueueRequest(monitoringInstanceID, syncToken, "fp-001", batchID, time.Now().UTC().Add(time.Duration(i-4)*time.Minute)),
			})
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		queue := &cancelAfterSuccessfulDeleteQueue{SyncQueue: baseQueue, afterDeletes: 3, cancel: cancel}
		client := &fakeClient{}
		client.syncFunc = func(request agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
			if strings.HasPrefix(syncBatchID(request), "poison-") {
				return nil, &enroll.RemoteError{
					StatusCode: http.StatusBadRequest,
					Code:       agentapi.ErrorCodeInvalidRequest,
					Message:    "private remote response detail",
				}
			}
			return &agentapi.SyncResponse{AcceptedAt: time.Now().UTC(), Status: "accepted"}, nil
		}
		var logs bytes.Buffer
		rt := newQueueTestRuntimeWithClock(t,
			slog.New(slog.NewTextHandler(&logs, nil)),
			client,
			queue,
			monitoringInstanceID,
			syncToken,
			func() time.Time { return time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC) },
		)

		if err := rt.Run(ctx); err != nil {
			t.Fatalf("Run() error type = %T, want nil", err)
		}
		if got := strings.Count(logs.String(), "discard rejected sync queue entry"); got != 2 {
			t.Fatalf("discard policy log count = %d, want 2", got)
		}
		if lines := replayLogLines(logs.String()); len(lines) != 0 {
			t.Fatalf("healthy replay log count = %d, want 0 for discard-only work", len(lines))
		}
	})

	t.Run("discard-only drain resets replay and keeps later live ticks quiet", func(t *testing.T) {
		baseQueue := &fileQueueFaults{}
		for i := 1; i <= 2; i++ {
			batchID := "poison-" + strconv.Itoa(i)
			baseQueue.entries = append(baseQueue.entries, syncqueue.Entry{
				ID:      batchID,
				Request: validQueueRequest(monitoringInstanceID, syncToken, "fp-001", batchID, time.Now().UTC().Add(time.Duration(i-3)*time.Minute)),
			})
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		queue := &cancelAfterSuccessfulDeleteQueue{SyncQueue: baseQueue, afterDeletes: 5, cancel: cancel}
		client := &fakeClient{}
		client.syncFunc = func(request agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
			if strings.HasPrefix(syncBatchID(request), "poison-") {
				return nil, &enroll.RemoteError{
					StatusCode: http.StatusBadRequest,
					Code:       agentapi.ErrorCodeInvalidRequest,
					Message:    "private remote response detail",
				}
			}
			return &agentapi.SyncResponse{AcceptedAt: time.Now().UTC(), Status: "accepted"}, nil
		}
		var logs bytes.Buffer
		rt := newQueueTestRuntimeWithClock(t,
			slog.New(slog.NewTextHandler(&logs, nil)),
			client,
			queue,
			monitoringInstanceID,
			syncToken,
			func() time.Time { return time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC) },
		)

		if err := rt.Run(ctx); err != nil {
			t.Fatalf("Run() error type = %T, want nil", err)
		}
		if got := strings.Count(logs.String(), "discard rejected sync queue entry"); got != 2 {
			t.Fatalf("discard policy log count = %d, want 2", got)
		}
		if queue.deleteCalls != 5 {
			t.Fatalf("durable delete count = %d, want 5 including later live ticks", queue.deleteCalls)
		}
		if lines := replayLogLines(logs.String()); len(lines) != 0 {
			t.Fatalf("healthy replay log count = %d, want 0 after discard-only drain and live ticks", len(lines))
		}
	})
}

func TestRuntimeLogsInstantSuccessfulReplayDrainAsCaughtUpOnce(t *testing.T) {
	withoutDockerCLI(t)

	const (
		monitoringInstanceID = "monitoringInstance-current"
		syncToken            = "sync-token-current"
	)
	baseQueue := &fileQueueFaults{}
	for i := 1; i <= 2; i++ {
		batchID := "backlog-" + strconv.Itoa(i)
		baseQueue.entries = append(baseQueue.entries, syncqueue.Entry{
			ID:      batchID,
			Request: validQueueRequest(monitoringInstanceID, syncToken, "fp-001", batchID, time.Now().UTC().Add(time.Duration(i-3)*time.Minute)),
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	queue := &cancelAfterSuccessfulDeleteQueue{SyncQueue: baseQueue, afterDeletes: 3, cancel: cancel}
	var logs bytes.Buffer
	rt := newQueueTestRuntimeWithClock(t,
		slog.New(slog.NewTextHandler(&logs, nil)),
		&fakeClient{},
		queue,
		monitoringInstanceID,
		syncToken,
		func() time.Time { return time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC) },
	)

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T, want nil", err)
	}
	lines := replayLogLines(logs.String())
	if len(lines) != 1 {
		t.Fatalf("replay transition log count = %d, want exactly 1", len(lines))
	}
	for _, wanted := range []string{"level=INFO", "state=caught_up", "acked_entries=2", "remaining_entries=0"} {
		if !strings.Contains(lines[0], wanted) {
			t.Fatalf("instant replay drain missing stable field %q", wanted)
		}
	}
	if strings.Contains(lines[0], "state=catching_up") {
		t.Fatal("instant replay drain emitted a preceding catching-up state")
	}
}

func TestRuntimeLogsReplayCaughtUpOnceAndKeepsLiveTicksQuiet(t *testing.T) {
	withoutDockerCLI(t)

	const (
		monitoringInstanceID = "monitoringInstance-current"
		syncToken            = "sync-token-current"
	)
	baseQueue := &fileQueueFaults{}
	for i := 1; i <= 3; i++ {
		batchID := "backlog-" + strconv.Itoa(i)
		baseQueue.entries = append(baseQueue.entries, syncqueue.Entry{
			ID:      batchID,
			Request: validQueueRequest(monitoringInstanceID, syncToken, "fp-001", batchID, time.Now().UTC().Add(time.Duration(i-4)*time.Minute)),
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	queue := &cancelAfterSuccessfulDeleteQueue{SyncQueue: baseQueue, afterDeletes: 7, cancel: cancel}
	var logs bytes.Buffer
	rt := newQueueTestRuntimeWithClock(t,
		slog.New(slog.NewTextHandler(&logs, nil)),
		&fakeClient{},
		queue,
		monitoringInstanceID,
		syncToken,
		func() time.Time { return time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC) },
	)

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T, want nil", err)
	}
	lines := replayLogLines(logs.String())
	if len(lines) != 2 {
		t.Fatalf("replay transition log count = %d, want 2 with later live ticks silent", len(lines))
	}
	for _, wanted := range []string{"state=catching_up", "acked_entries=2", "remaining_entries=1"} {
		if !strings.Contains(lines[0], wanted) {
			t.Fatalf("first replay transition missing stable field %q", wanted)
		}
	}
	for _, wanted := range []string{"state=caught_up", "acked_entries=1", "remaining_entries=0"} {
		if !strings.Contains(lines[1], wanted) {
			t.Fatalf("caught-up transition missing stable field %q", wanted)
		}
	}
	if got := strings.Count(logs.String(), "state=caught_up"); got != 1 {
		t.Fatalf("caught-up transition count = %d, want 1", got)
	}
}

func TestRuntimeThrottlesReplayProgressLogs(t *testing.T) {
	withoutDockerCLI(t)

	const (
		monitoringInstanceID = "monitoringInstance-current"
		syncToken            = "sync-token-current"
	)
	baseQueue := &fileQueueFaults{}
	for i := 1; i <= 8; i++ {
		batchID := "backlog-" + strconv.Itoa(i)
		baseQueue.entries = append(baseQueue.entries, syncqueue.Entry{
			ID:      batchID,
			Request: validQueueRequest(monitoringInstanceID, syncToken, "fp-001", batchID, time.Now().UTC().Add(time.Duration(i-9)*time.Minute)),
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	queue := &cancelAfterSuccessfulDeleteQueue{SyncQueue: baseQueue, afterDeletes: 9, cancel: cancel}
	clockTimes := []time.Time{
		time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC),
		time.Date(2026, time.August, 30, 12, 0, 30, 0, time.UTC),
		time.Date(2026, time.August, 30, 12, 1, 0, 0, time.UTC),
	}
	clockCall := 0
	clock := func() time.Time {
		if clockCall >= len(clockTimes) {
			return clockTimes[len(clockTimes)-1]
		}
		value := clockTimes[clockCall]
		clockCall++
		return value
	}
	var logs bytes.Buffer
	rt := newQueueTestRuntimeWithClock(t,
		slog.New(slog.NewTextHandler(&logs, nil)),
		&fakeClient{},
		queue,
		monitoringInstanceID,
		syncToken,
		clock,
	)

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T, want nil", err)
	}
	lines := replayLogLines(logs.String())
	if len(lines) != 2 {
		t.Fatalf("rate-limited replay progress log count = %d, want 2 across three rounds", len(lines))
	}
	for _, wanted := range []string{"state=catching_up", "acked_entries=2", "remaining_entries=6"} {
		if !strings.Contains(lines[0], wanted) {
			t.Fatalf("first replay progress log missing stable field %q", wanted)
		}
	}
	for _, wanted := range []string{"state=catching_up", "acked_entries=2", "remaining_entries=2"} {
		if !strings.Contains(lines[1], wanted) {
			t.Fatalf("post-throttle replay progress log missing stable field %q", wanted)
		}
	}
}

func TestRuntimeReplayRetryIsFailureStateNotHealthyProgress(t *testing.T) {
	withoutDockerCLI(t)

	tests := []struct {
		name          string
		seedBacklog   int
		failSyncCall  int
		wantAcked     string
		wantRemaining string
		cancelDeletes int
	}{
		{name: "backlog retry after one durable ack", seedBacklog: 2, failSyncCall: 2, wantAcked: "acked_entries=1", wantRemaining: "remaining_entries=1", cancelDeletes: 6},
		{name: "fresh retry becomes durable backlog", seedBacklog: 0, failSyncCall: 1, wantAcked: "acked_entries=0", wantRemaining: "remaining_entries=1", cancelDeletes: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const (
				monitoringInstanceID = "monitoringInstance-current"
				syncToken            = "sync-token-current"
			)
			baseQueue := &fileQueueFaults{}
			for i := 1; i <= tt.seedBacklog; i++ {
				batchID := "backlog-" + strconv.Itoa(i)
				baseQueue.entries = append(baseQueue.entries, syncqueue.Entry{
					ID:      batchID,
					Request: validQueueRequest(monitoringInstanceID, syncToken, "fp-001", batchID, time.Now().UTC().Add(time.Duration(i-tt.seedBacklog-1)*time.Minute)),
				})
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			queue := &cancelAfterSuccessfulDeleteQueue{SyncQueue: baseQueue, afterDeletes: tt.cancelDeletes, cancel: cancel}
			client := &fakeClient{}
			logicalNow := time.Now()
			client.syncFunc = func(agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
				if client.syncCalls == tt.failSyncCall {
					return nil, &enroll.RemoteError{
						StatusCode: http.StatusServiceUnavailable,
						Code:       agentapi.ErrorCodeInternalError,
						Message:    "private remote response detail",
					}
				}
				return &agentapi.SyncResponse{AcceptedAt: time.Now().UTC(), Status: "accepted"}, nil
			}
			var logs bytes.Buffer
			rt := newQueueTestRuntimeWithClock(t,
				slog.New(slog.NewTextHandler(&logs, nil)),
				client,
				queue,
				monitoringInstanceID,
				syncToken,
				func() time.Time {
					logicalNow = logicalNow.Add(time.Minute)
					return logicalNow
				},
			)

			if err := rt.Run(ctx); err != nil {
				t.Fatalf("Run() error type = %T, want nil", err)
			}
			lines := replayLogLines(logs.String())
			if len(lines) != 2 {
				t.Fatalf("replay retry/recovery log count = %d, want exactly 2", len(lines))
			}
			for _, wanted := range []string{
				"level=ERROR",
				"state=retrying",
				"error=\"sync queue replay retry pending\"",
				"kind=remote",
				"action=retry",
				"status=503",
				"code=" + agentapi.ErrorCodeInternalError,
				tt.wantAcked,
				tt.wantRemaining,
			} {
				if !strings.Contains(lines[0], wanted) {
					t.Fatalf("retry state log missing stable field %q", wanted)
				}
			}
			if strings.Contains(lines[0], "state=catching_up") || strings.Contains(lines[0], "state=caught_up") {
				t.Fatal("retry round was logged as healthy replay progress")
			}
			for _, wanted := range []string{"level=INFO", "state=caught_up", "acked_entries=1", "remaining_entries=0"} {
				if !strings.Contains(lines[1], wanted) {
					t.Fatalf("replay recovery transition missing stable field %q", wanted)
				}
			}
			if strings.Contains(lines[1], "state=catching_up") {
				t.Fatal("replay recovery emitted an extra catching-up state")
			}
			if strings.Contains(logs.String(), "sync queue flush failed") {
				t.Fatal("retry round emitted a duplicate legacy queue failure log")
			}
		})
	}
}

func TestRuntimeReplayLogsDoNotExposeSensitiveQueueOrRequestFields(t *testing.T) {
	withoutDockerCLI(t)

	const (
		monitoringInstanceID = "monitoring-private-id-sentinel"
		syncToken            = "sync-private-token-sentinel"
		fingerprint          = "fingerprint-private-sentinel"
		entryID              = "entry-private-id-sentinel"
		batchID              = "batch-private-id-sentinel"
		remoteMessage        = "remote-private-message-sentinel"
	)
	request := validQueueRequest(monitoringInstanceID, syncToken, fingerprint, batchID, time.Now().UTC().Add(-time.Minute))
	request.HostSamples = []agentapi.HostSamplePayload{{
		ObservedAt:  time.Now().UTC().Add(-time.Minute),
		Fingerprint: fingerprint,
		SyncBatchID: batchID,
	}}
	request.ProbeObservations = []agentapi.ProbeObservationPayload{{
		TargetID:     "object-private-id-sentinel",
		ProbeItemID:  "probe-private-id-sentinel",
		Fingerprint:  fingerprint,
		SyncBatchID:  batchID,
		ErrorSummary: "payload-private-sentinel",
	}}
	request.IPQualityReports = []agentapi.IPQualityReportPayload{{
		IPAddress:   "198.51.100.77",
		Fingerprint: fingerprint,
		SyncBatchID: batchID,
	}}
	request.CommandResults = []agentapi.CommandResult{{
		ActionID:  "action-private-id-sentinel",
		CommandID: "command-private-id-sentinel",
		Stdout:    "stdout-private-sentinel",
		Stderr:    "stderr-private-sentinel",
	}}
	baseQueue := &fileQueueFaults{entries: []syncqueue.Entry{{ID: entryID, Request: request}}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	queue := &cancelAfterSuccessfulDeleteQueue{SyncQueue: baseQueue, afterDeletes: 2, cancel: cancel}
	client := &fakeClient{}
	client.syncFunc = func(agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
		if client.syncCalls == 1 {
			return nil, fmt.Errorf("Authorization=private DSN=private request=response private cause: %w", &enroll.RemoteError{
				StatusCode: http.StatusServiceUnavailable,
				Code:       agentapi.ErrorCodeInternalError,
				Message:    remoteMessage,
			})
		}
		return &agentapi.SyncResponse{AcceptedAt: time.Now().UTC(), Status: "accepted"}, nil
	}
	tokenSource := &syncCredentialTokenSource{
		monitoringInstanceID: monitoringInstanceID,
		syncToken:            syncToken,
		hasCredentials:       true,
	}
	var logs bytes.Buffer
	rt := newRuntimeWithRuntimeDeps(t,
		agentconfig.AgentConfig{ServerURL: "https://operator-private@center.example.test/path-private-sentinel?query-private-sentinel", TokenFile: "/tmp/token"},
		slog.New(slog.NewTextHandler(&logs, nil)),
		client,
		tokenSource,
		staticFingerprintValue(fingerprint),
		&fakeHostSampleProvider{},
		&fakeProbeProvider{},
		10*time.Millisecond,
		queue,
		func() time.Time { return time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC) },
	)

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T, want nil", err)
	}
	lines := replayLogLines(logs.String())
	if len(lines) < 1 || !strings.Contains(lines[0], "state=retrying") {
		t.Fatal("privacy fixture did not exercise the replay retry log")
	}
	for _, forbidden := range []string{
		monitoringInstanceID,
		syncToken,
		fingerprint,
		entryID,
		batchID,
		"object-private-id-sentinel",
		"probe-private-id-sentinel",
		"action-private-id-sentinel",
		"command-private-id-sentinel",
		"198.51.100.77",
		"payload-private-sentinel",
		"stdout-private-sentinel",
		"stderr-private-sentinel",
		remoteMessage,
		"Authorization=private",
		"DSN=private",
		"request=response",
		"operator-private",
		"path-private-sentinel",
		"query-private-sentinel",
	} {
		if strings.Contains(logs.String(), forbidden) {
			t.Fatal("runtime replay diagnostics exposed a forbidden sensitive field category")
		}
	}
}

func replayLogLines(logs string) []string {
	lines := make([]string, 0)
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, "sync queue replay progress") {
			lines = append(lines, line)
		}
	}
	return lines
}

func assertAuxiliaryPayloads(t *testing.T, request agentapi.SyncRequest, backfilled bool) {
	t.Helper()
	if len(request.CommandResults) != 1 {
		t.Fatalf("command result count = %d, want 1", len(request.CommandResults))
	}
	if request.CommandResults[0].ActionID != "act_aux" || request.CommandResults[0].CommandID != "uptime" {
		t.Fatal("command result identity category mismatch")
	}
	if len(request.IPQualityReports) != 1 {
		t.Fatalf("IP-quality report count = %d, want 1", len(request.IPQualityReports))
	}
	if request.IPQualityReports[0].IPAddress != "203.0.113.10" {
		t.Fatal("IP-quality report address category mismatch")
	}
	if len(request.Heartbeats) != 1 {
		t.Fatalf("heartbeat count = %d, want 1", len(request.Heartbeats))
	}
	if request.Heartbeats[0].IsBackfilled != backfilled || request.IPQualityReports[0].IsBackfilled != backfilled {
		t.Fatalf("heartbeat/IP backfilled = %t/%t, want %t", request.Heartbeats[0].IsBackfilled, request.IPQualityReports[0].IsBackfilled, backfilled)
	}
}

func validQueueRequest(monitoringInstanceID, syncToken, fingerprint, batchID string, observedAt time.Time) agentapi.SyncRequest {
	return agentapi.SyncRequest{
		MonitoringInstanceID: monitoringInstanceID,
		SyncToken:            syncToken,
		Heartbeats: []agentapi.MonitoringInstanceHeartbeat{{
			ObservedAt:   observedAt,
			AgentVersion: "dev",
			Fingerprint:  fingerprint,
			SyncBatchID:  batchID,
		}},
	}
}

func newQueueTestRuntime(t *testing.T, logger *slog.Logger, client *fakeClient, queue agentruntime.SyncQueue, monitoringInstanceID, syncToken string) *agentruntime.Runtime {
	return newQueueTestRuntimeWithClock(t, logger, client, queue, monitoringInstanceID, syncToken, nil)
}

func newQueueTestRuntimeWithClock(t *testing.T, logger *slog.Logger, client *fakeClient, queue agentruntime.SyncQueue, monitoringInstanceID, syncToken string, clock func() time.Time) *agentruntime.Runtime {
	tokenSource := &syncCredentialTokenSource{
		monitoringInstanceID: monitoringInstanceID,
		syncToken:            syncToken,
		hasCredentials:       true,
	}
	return newRuntimeWithRuntimeDeps(t,
		agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"},
		logger,
		client,
		tokenSource,
		staticFingerprint{},
		&fakeHostSampleProvider{},
		&fakeProbeProvider{},
		10*time.Millisecond,
		queue,
		clock,
	)
}

func syncBatchID(request agentapi.SyncRequest) string {
	if len(request.Heartbeats) == 0 {
		return ""
	}
	if label, ok := fixtureBatchLabels.Load(request.Heartbeats[0].SyncBatchID); ok {
		return label.(string)
	}
	return request.Heartbeats[0].SyncBatchID
}

func queueEntryAttempts(entries []syncqueue.Entry) []int {
	attempts := make([]int, 0, len(entries))
	for _, entry := range entries {
		attempts = append(attempts, entry.Attempts)
	}
	return attempts
}

func httpStatus(status int) string {
	return strconv.Itoa(status)
}

func TestRuntimeReturnsQueuePersistenceErrorWithoutDroppingCurrentBatch(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	client := &fakeClient{}
	enqueueErr := errors.New("queue path is read-only")
	queue := &fileQueueFaults{enqueueErr: enqueueErr}
	rt := newRuntimeWithRuntimeDeps(t, cfg, nil, client, staticTokenSource{}, staticFingerprint{}, &fakeHostSampleProvider{}, &fakeProbeProvider{}, 10*time.Millisecond, queue)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := rt.Run(ctx)
	if !errors.Is(err, enqueueErr) {
		t.Fatalf("Run() error type = %T, want enqueue error", err)
	}
	if client.syncCalls != 0 {
		t.Fatalf("syncCalls = %d, want 0 when enqueue fails before send", client.syncCalls)
	}
}

func TestRuntimeRetainsIPQualityReportUntilQueueEnqueueSucceeds(t *testing.T) {
	enqueueErr := errors.New("queue unavailable")
	queue := &failNthEnqueueQueue{
		fileQueueFaults: &fileQueueFaults{},
		failAt:          1,
		err:             enqueueErr,
	}
	ipProvider := &fakeIPQualityProvider{reports: []agentapi.IPQualityReportPayload{{
		IPAddress: "203.0.113.10",
		Status:    agentapi.IPQualityStatusSuccess,
	}}}
	client := &fakeClient{}
	rt := newRuntimeWithRuntimeDeps(t,
		agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"},
		slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		client,
		staticTokenSource{},
		staticFingerprint{},
		&fakeHostSampleProvider{},
		&fakeProbeProvider{},
		10*time.Millisecond,
		queue,
		ipProvider,
	)

	firstCtx, firstCancel := context.WithTimeout(context.Background(), time.Second)
	defer firstCancel()
	if err := rt.Run(firstCtx); !errors.Is(err, enqueueErr) {
		t.Fatalf("first Run() error type = %T, want enqueue error", err)
	}

	queue.failAt = 0
	secondCtx, secondCancel := context.WithTimeout(context.Background(), time.Second)
	defer secondCancel()
	client.cancelAfterSyncs = 1
	client.cancel = secondCancel
	if err := rt.Run(secondCtx); err != nil {
		t.Fatalf("second Run() error type = %T, want nil", err)
	}
	if len(client.syncRequests) != 1 || len(client.syncRequests[0].IPQualityReports) != 1 {
		t.Fatalf("durable retry request/report count = %d/%d, want 1/1", len(client.syncRequests), firstIPQualityReportCount(client.syncRequests))
	}
}

func firstIPQualityReportCount(requests []agentapi.SyncRequest) int {
	if len(requests) == 0 {
		return 0
	}
	return len(requests[0].IPQualityReports)
}

func TestRuntimeRetainsCommandResultUntilQueueEnqueueSucceeds(t *testing.T) {
	withoutDockerCLI(t)
	enqueueErr := errors.New("queue unavailable")
	queue := &failNthEnqueueQueue{
		fileQueueFaults: &fileQueueFaults{},
		failAt:          2,
		err:             enqueueErr,
	}
	client := &fakeClient{syncResponses: []agentapi.SyncResponse{{
		AcceptedAt: time.Now().UTC(),
		Status:     "accepted",
		Plan: &agentapi.SyncPlan{PendingAction: &agentapi.PendingAction{
			CommandID: "uptime",
			ActionID:  "act_queue_retry",
		}},
	}}}
	rt := newRuntimeWithRuntimeDeps(t,
		agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"},
		slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		client,
		staticTokenSource{},
		staticFingerprint{},
		&fakeHostSampleProvider{},
		&fakeProbeProvider{},
		10*time.Millisecond,
		queue,
	)

	firstCtx, firstCancel := context.WithTimeout(context.Background(), time.Second)
	defer firstCancel()
	if err := rt.Run(firstCtx); !errors.Is(err, enqueueErr) {
		t.Fatalf("first Run() error type = %T, want enqueue error", err)
	}
	if client.syncCalls != 1 {
		t.Fatalf("first Run() Sync() calls = %d, want one accepted plan before enqueue failure", client.syncCalls)
	}

	queue.failAt = 0
	secondCtx, secondCancel := context.WithTimeout(context.Background(), time.Second)
	defer secondCancel()
	client.cancelAfterSyncs = 2
	client.cancel = secondCancel
	if err := rt.Run(secondCtx); err != nil {
		t.Fatalf("second Run() error type = %T, want nil", err)
	}
	if len(client.syncRequests) != 2 || len(client.syncRequests[1].CommandResults) != 1 {
		t.Fatalf("retry request/result count = %d/%d, want 2/1", len(client.syncRequests), secondCommandResultCount(client.syncRequests))
	}
}

func secondCommandResultCount(requests []agentapi.SyncRequest) int {
	if len(requests) < 2 {
		return 0
	}
	return len(requests[1].CommandResults)
}

func TestRuntimeReturnsAckDeleteErrorInsteadOfSilentlyReplaying(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	client := &fakeClient{}
	deleteErr := errors.New("queue delete denied")
	queue := &fileQueueFaults{deleteErr: deleteErr}
	rt := newRuntimeWithRuntimeDeps(t, cfg, nil, client, staticTokenSource{}, staticFingerprint{}, &fakeHostSampleProvider{}, &fakeProbeProvider{}, 10*time.Millisecond, queue)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := rt.Run(ctx)
	if !errors.Is(err, deleteErr) {
		t.Fatalf("Run() error type = %T, want delete error", err)
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
	seedStore := newRuntimeTestStore(t, path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour, SkipFsync: true})
	falseCPU := false
	seeded := agentapi.SyncRequest{
		MonitoringInstanceID: "monitoringInstance-123",
		SyncToken:            "sync-token-001",
		Heartbeats: []agentapi.MonitoringInstanceHeartbeat{{
			ObservedAt:   time.Now().UTC().Add(-time.Minute),
			AgentVersion: "dev",
			Fingerprint:  "fp-001",
			SyncBatchID:  "seeded",
		}},
		HostSamples: []agentapi.HostSamplePayload{{
			ObservedAt:    time.Now().UTC().Add(-time.Minute),
			AgentVersion:  "dev",
			Fingerprint:   "fp-001",
			SyncBatchID:   "seeded",
			CPURatesValid: &falseCPU,
			CPUUsagePct:   0,
			CPUIOWaitPct:  0,
			CPUStealPct:   0,
		}},
	}
	if _, err := seedStore.Enqueue(context.Background(), seeded); err != nil {
		t.Fatalf("seed Enqueue() error = %v", err)
	}

	client := &fakeClient{}
	fileStore := newRuntimeTestStore(t, path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour, SkipFsync: true})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	store := &cancelAfterDeleteQueue{
		runtimeTestStore: fileStore,
		afterDeletes:     1,
		cancel:           cancel,
	}
	rt := newRuntimeWithRuntimeDeps(t, cfg, nil, client, staticTokenSource{}, staticFingerprint{}, &fakeHostSampleProvider{}, &fakeProbeProvider{}, 10*time.Millisecond, store)
	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T", err)
	}
	if len(client.syncRequests) == 0 || syncBatchID(client.syncRequests[0]) != "seeded" || !client.syncRequests[0].Heartbeats[0].IsBackfilled {
		if len(client.syncRequests) == 0 {
			t.Fatal("Sync() requests = 0, want seeded backfilled request")
		}
		t.Fatalf("Sync() seeded-entry match = false (backfilled=%t)", client.syncRequests[0].Heartbeats[0].IsBackfilled)
	}
	if len(client.syncRequests[0].HostSamples) != 1 || client.syncRequests[0].HostSamples[0].CPURatesValid == nil || *client.syncRequests[0].HostSamples[0].CPURatesValid {
		t.Fatalf("Sync() replayed CPURatesValid = %#v, want explicit false", client.syncRequests[0].HostSamples[0].CPURatesValid)
	}
}

func TestRuntimeExecutesPendingActionAndReturnsCommandResult(t *testing.T) {
	withoutDockerCLI(t, "uptime")
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

	rt := newRuntimeWithRuntimeDeps(t, cfg, nil, client, staticTokenSource{}, staticFingerprint{}, &fakeHostSampleProvider{}, &fakeProbeProvider{}, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client.cancel = cancel

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T", err)
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
		t.Fatal("command result did not preserve the action ID")
	}
	if cr.CommandID != commandID {
		t.Fatal("command result did not preserve the command ID")
	}
	if cr.ExitCode != 0 {
		t.Fatalf("CommandResult.ExitCode = %d, want 0 for uptime", cr.ExitCode)
	}
	if cr.Stdout == "" {
		t.Fatal("CommandResult.Stdout is empty, want uptime output")
	}
}

func TestRuntimePendingActionExecutionInheritsRuntimeContext(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	binDir := t.TempDir()
	uptimePath := filepath.Join(binDir, "uptime")
	if err := os.WriteFile(uptimePath, []byte("#!/bin/sh\n/bin/sleep 1\n"), 0o755); err != nil {
		t.Fatalf("write fake uptime: %v", err)
	}
	t.Setenv("PATH", binDir)

	client := &fakeClient{
		cancelAfterSyncs: 1,
		syncResponses: []agentapi.SyncResponse{{
			AcceptedAt: time.Now().UTC(),
			Status:     "accepted",
			Plan: &agentapi.SyncPlan{
				HostSampleFrequencyTier: agentapi.FrequencyTier1m,
				PendingAction: &agentapi.PendingAction{
					CommandID: "uptime",
					ActionID:  "act_cancel",
				},
			},
		}},
	}
	rt := newRuntimeWithRuntimeDeps(t, cfg, nil, client, staticTokenSource{}, staticFingerprint{}, &fakeHostSampleProvider{}, &fakeProbeProvider{}, 10*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	client.cancel = cancel

	startedAt := time.Now()
	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T", err)
	}
	if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
		t.Fatalf("Run() elapsed = %s, want runtime cancellation to stop pending action promptly", elapsed)
	}
}

func TestRuntimeSilentlyIgnoresUnknownPendingActionCommandID(t *testing.T) {
	withoutDockerCLI(t)
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

	rt := newRuntimeWithRuntimeDeps(t, cfg, nil, client, staticTokenSource{}, staticFingerprint{}, &fakeHostSampleProvider{}, &fakeProbeProvider{}, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client.cancel = cancel

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T", err)
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

	rt := newRuntimeWithRuntimeDeps(t, cfg, nil, client, staticTokenSource{}, staticFingerprint{}, &fakeHostSampleProvider{}, &fakeProbeProvider{}, 10*time.Millisecond, nil, ipProvider)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client.cancel = cancel

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T", err)
	}
	if client.syncCalls < 2 {
		t.Fatalf("Sync() calls = %d, want at least 2", client.syncCalls)
	}
	if len(client.syncRequests[0].IPQualityReports) != 0 {
		t.Fatal("first sync included IP-quality reports before a plan arrived")
	}
	if ipProvider.startCalls == 0 {
		t.Fatal("IP quality provider was not started after plan")
	}
	if ipProvider.lastPlan == nil || !ipProvider.lastPlan.Enabled || ipProvider.lastPlan.Services[0] != "netflix" {
		t.Fatal("IP-quality provider did not receive the expected enabled plan")
	}
}

func TestRuntimeDrainsCompletedIPQualityReportIntoNextSync(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	ipProvider := &fakeIPQualityProvider{
		reportsAfterStart: []agentapi.IPQualityReportPayload{{
			ObservedAt:      time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC),
			IPAddress:       "203.0.113.10",
			IPVersion:       4,
			Status:          agentapi.IPQualityStatusSuccess,
			DiagnosticsJSON: json.RawMessage(`{"collection_revision":4,"collection_id":"round-runtime","expected_ip_versions":[4,6],"ip_address_family":4,"family_availability":"available"}`),
		}, {
			ObservedAt: time.Date(2026, time.June, 8, 12, 0, 0, 0, time.UTC),
			IPAddress:  "2001:db8::10", IPVersion: 6, Status: agentapi.IPQualityStatusSuccess,
			DiagnosticsJSON: json.RawMessage(`{"collection_revision":4,"collection_id":"round-runtime","expected_ip_versions":[4,6],"ip_address_family":6,"family_availability":"available"}`),
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

	rt := newRuntimeWithRuntimeDeps(t, cfg, nil, client, staticTokenSource{}, staticFingerprint{}, &fakeHostSampleProvider{}, &fakeProbeProvider{}, 10*time.Millisecond, nil, ipProvider)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client.cancel = cancel

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T", err)
	}
	if client.syncCalls < 3 {
		t.Fatalf("Sync() calls = %d, want at least 3", client.syncCalls)
	}
	secondSync := client.syncRequests[1]
	if len(secondSync.IPQualityReports) != 0 {
		t.Fatal("second sync included an IP-quality report before collection completed")
	}
	thirdSync := client.syncRequests[2]
	if len(thirdSync.IPQualityReports) != 2 {
		t.Fatalf("len(thirdSync.IPQualityReports) = %d, want one complete pair", len(thirdSync.IPQualityReports))
	}
	report := thirdSync.IPQualityReports[0]
	if report.AgentVersion != "dev" || report.Fingerprint != "fp-001" || report.SyncBatchID == "" {
		t.Fatal("IP-quality report metadata was not populated by the runtime")
	}
	if report.IPAddress != "203.0.113.10" || report.Status != agentapi.IPQualityStatusSuccess {
		t.Fatal("completed IP-quality report did not match the provider result")
	}
	for index, family := range []int{4, 6} {
		paired := thirdSync.IPQualityReports[index]
		metadata := agentapi.ParseCollectionMetadata(paired.DiagnosticsJSON)
		if paired.IPVersion != family || !metadata.Valid || metadata.CollectionID != "round-runtime" || paired.Fingerprint != report.Fingerprint || paired.SyncBatchID != report.SyncBatchID || paired.AgentVersion != report.AgentVersion {
			t.Fatalf("dual round lost identity or runtime metadata: %#v", thirdSync.IPQualityReports)
		}
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

	rt := newRuntimeWithRuntimeDeps(t, cfg, nil, client, staticTokenSource{}, staticFingerprint{}, &fakeHostSampleProvider{}, &fakeProbeProvider{}, 10*time.Millisecond, nil, ipProvider)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client.cancel = cancel

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T", err)
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
			Fingerprint:     "fp-001",
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

	rt := newRuntimeWithRuntimeDeps(t, cfg, nil, client, staticTokenSource{}, staticFingerprint{}, &fakeHostSampleProvider{}, &fakeProbeProvider{}, 10*time.Millisecond, nil, ipProvider)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client.cancel = cancel

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T", err)
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
		t.Fatal("IP-quality collection failure was not uploaded")
	}
	if report.AgentVersion != "collector-version" || report.Fingerprint != "fp-001" || report.SyncBatchID != thirdSync.Heartbeats[0].SyncBatchID {
		t.Fatal("report observation metadata or canonical batch identity was lost")
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

	rt := newRuntimeWithRuntimeDeps(t, cfg, nil, client, staticTokenSource{}, staticFingerprint{}, hostProvider, &fakeProbeProvider{}, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	client.cancel = cancel

	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error type = %T", err)
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
