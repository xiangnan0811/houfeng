package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	agentconfig "houfeng/agent/config"
	"houfeng/agent/containersample"
	"houfeng/agent/enroll"
	agentexec "houfeng/agent/exec"
	"houfeng/agent/hostsample"
	"houfeng/agent/probe"
	"houfeng/agent/syncqueue"
	"houfeng/internal/contracts/agentapi"
)

const defaultInterval = 30 * time.Second

var agentVersion = "dev"

var ErrMissingSyncToken = errors.New("enrollment bound response missing sync token")

var errRemoteSync = errors.New("remote sync failed")

type EnrollmentNotBoundError struct {
	BindingStatus string
}

func (e *EnrollmentNotBoundError) Error() string {
	if e.BindingStatus == "" {
		return "enrollment binding_status is not bound"
	}
	return fmt.Sprintf("enrollment binding_status is %q, want %q", e.BindingStatus, agentapi.BindingStatusBound)
}

type Client interface {
	Enroll(context.Context, agentapi.EnrollmentRequest) (*agentapi.EnrollmentResponse, error)
	Sync(context.Context, agentapi.SyncRequest) (*agentapi.SyncResponse, error)
}

type TokenSource interface {
	Token(context.Context) (string, error)
}

type SyncCredentialStore interface {
	SyncCredentials(context.Context) (nodeID string, syncToken string, ok bool, err error)
	SaveSyncCredentials(context.Context, string, string) error
}

type FingerprintSource interface {
	Fingerprint(context.Context) (string, error)
}

type HostSampleProvider interface {
	Collect(time.Time) (agentapi.HostSamplePayload, error)
}

type ProbeProvider interface {
	CollectDue(context.Context, *agentapi.SyncPlan, time.Time) ([]agentapi.ProbeObservationPayload, error)
}

type SyncQueue interface {
	Enqueue(context.Context, agentapi.SyncRequest) (string, error)
	List(context.Context) ([]syncqueue.Entry, error)
	Delete(context.Context, string) error
	MarkAttempt(context.Context, string) error
	Prune(context.Context) error
}

type Runtime struct {
	cfg                 agentconfig.AgentConfig
	client              Client
	logger              *slog.Logger
	tokenSource         TokenSource
	syncCredentialStore SyncCredentialStore
	fingerprintSource   FingerprintSource
	hostSampleProvider  HostSampleProvider
	probeProvider       ProbeProvider
	interval            time.Duration
	syncQueue           SyncQueue
	currentPlan         *agentapi.SyncPlan
	lastHostSampleAt    time.Time
	pendingResults      []agentexec.Result
}

func New(cfg agentconfig.AgentConfig, logger *slog.Logger, tokenSource TokenSource, fingerprintSource FingerprintSource) *Runtime {
	queue := syncqueue.NewFileStore(cfg.BufferFile, syncqueue.Options{
		MaxEntries: cfg.BufferMaxEntries,
		MaxAge:     cfg.BufferMaxAge,
	})
	return NewWithRuntimeDeps(cfg, logger, enroll.NewClient(cfg.ServerURL), tokenSource, fingerprintSource, hostsample.New(), probe.New(), defaultInterval, queue)
}

func NewWithDeps(cfg agentconfig.AgentConfig, logger *slog.Logger, client Client, tokenSource TokenSource, fingerprintSource FingerprintSource, interval time.Duration) *Runtime {
	return NewWithRuntimeDeps(cfg, logger, client, tokenSource, fingerprintSource, hostsample.New(), probe.New(), interval)
}

func NewWithRuntimeDeps(
	cfg agentconfig.AgentConfig,
	logger *slog.Logger,
	client Client,
	tokenSource TokenSource,
	fingerprintSource FingerprintSource,
	hostSampleProvider HostSampleProvider,
	probeProvider ProbeProvider,
	interval time.Duration,
	queues ...SyncQueue,
) *Runtime {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = defaultInterval
	}
	var queue SyncQueue
	if len(queues) > 0 {
		queue = queues[0]
	}
	var syncCredentialStore SyncCredentialStore
	if store, ok := tokenSource.(SyncCredentialStore); ok {
		syncCredentialStore = store
	}

	return &Runtime{
		cfg:                 cfg,
		client:              client,
		logger:              logger,
		tokenSource:         tokenSource,
		syncCredentialStore: syncCredentialStore,
		fingerprintSource:   fingerprintSource,
		hostSampleProvider:  hostSampleProvider,
		probeProvider:       probeProvider,
		interval:            interval,
		syncQueue:           queue,
	}
}

func (r *Runtime) Run(ctx context.Context) error {
	if r.client == nil {
		return fmt.Errorf("runtime client is nil")
	}
	if r.tokenSource == nil {
		return fmt.Errorf("runtime token source is nil")
	}
	if r.fingerprintSource == nil {
		return fmt.Errorf("runtime fingerprint source is nil")
	}

	r.logger.Info("agent runtime started", "server_url", r.cfg.ServerURL)
	defer r.logger.Info("agent runtime stopped", "server_url", r.cfg.ServerURL)

	fingerprint, err := r.fingerprintSource.Fingerprint(ctx)
	if err != nil {
		return fmt.Errorf("load fingerprint: %w", err)
	}

	nodeID, syncToken, ok, err := r.loadSyncCredentials(ctx)
	if err != nil {
		return err
	}
	if !ok {
		enrollment, err := r.enroll(ctx, fingerprint)
		if err != nil {
			return err
		}
		nodeID = enrollment.NodeID
		syncToken = enrollment.SyncToken
	}

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case tick := <-ticker.C:
			observedAt := tick.UTC()
			syncBatchID := observedAt.Format(time.RFC3339Nano)
			request := r.buildSyncRequest(ctx, nodeID, syncToken, observedAt, fingerprint, syncBatchID)

			if r.syncQueue == nil {
				response, err := r.client.Sync(ctx, request)
				if err != nil {
					return fmt.Errorf("sync heartbeat: %w", err)
				}
				r.applySyncPlan(response)
				continue
			}

			if err := r.enqueueAndFlush(ctx, request, syncBatchID); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				if errors.Is(err, errRemoteSync) {
					r.logger.Error("sync queue flush failed", "error", err)
					continue
				}
				return fmt.Errorf("sync queue operation failed: %w", err)
			}
		}
	}
}

func (r *Runtime) loadSyncCredentials(ctx context.Context) (string, string, bool, error) {
	if r.syncCredentialStore == nil {
		return "", "", false, nil
	}
	nodeID, syncToken, ok, err := r.syncCredentialStore.SyncCredentials(ctx)
	if err != nil {
		return "", "", false, fmt.Errorf("load sync credentials: %w", err)
	}
	return nodeID, syncToken, ok, nil
}

func (r *Runtime) enroll(ctx context.Context, fingerprint string) (*agentapi.EnrollmentResponse, error) {
	token, err := r.tokenSource.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("load token: %w", err)
	}

	enrollment, err := r.client.Enroll(ctx, agentapi.EnrollmentRequest{
		Token:       token,
		Fingerprint: fingerprint,
	})
	if err != nil {
		return nil, fmt.Errorf("enroll agent: %w", err)
	}

	r.logger.Info("agent enrolled", "node_id", enrollment.NodeID, "status", enrollment.Status, "binding_status", enrollment.BindingStatus)
	if enrollment.BindingStatus != agentapi.BindingStatusBound {
		return nil, fmt.Errorf("enroll agent: %w", &EnrollmentNotBoundError{BindingStatus: enrollment.BindingStatus})
	}
	if enrollment.SyncToken == "" {
		return nil, fmt.Errorf("enroll agent: %w", ErrMissingSyncToken)
	}
	if r.syncCredentialStore != nil {
		if err := r.syncCredentialStore.SaveSyncCredentials(ctx, enrollment.NodeID, enrollment.SyncToken); err != nil {
			return nil, fmt.Errorf("persist sync credentials: %w", err)
		}
	}
	return enrollment, nil
}

func (r *Runtime) buildSyncRequest(ctx context.Context, nodeID, syncToken string, observedAt time.Time, fingerprint, syncBatchID string) agentapi.SyncRequest {
	request := agentapi.SyncRequest{
		NodeID:    nodeID,
		SyncToken: syncToken,
		Heartbeats: []agentapi.NodeHeartbeat{{
			ObservedAt:   observedAt,
			AgentVersion: agentVersion,
			Fingerprint:  fingerprint,
			SyncBatchID:  syncBatchID,
		}},
	}

	if sample := r.collectHostSample(observedAt, fingerprint, syncBatchID); sample != nil {
		request.HostSamples = append(request.HostSamples, *sample)
	}

	probeObservations, err := r.collectProbeObservations(ctx, observedAt, fingerprint, syncBatchID)
	if err != nil {
		r.logger.Error("collect probe observations failed", "error", err)
	} else {
		request.ProbeObservations = append(request.ProbeObservations, probeObservations...)
	}

	// Flush any pending command results from executed actions.
	for _, pr := range r.pendingResults {
		request.CommandResults = append(request.CommandResults, agentapi.CommandResult{
			ActionID:  pr.ActionID,
			CommandID: pr.CommandID,
			Stdout:    pr.Stdout,
			Stderr:    pr.Stderr,
			ExitCode:  pr.ExitCode,
		})
	}
	r.pendingResults = nil

	return request
}

func (r *Runtime) enqueueAndFlush(ctx context.Context, request agentapi.SyncRequest, currentBatchID string) error {
	if _, err := r.syncQueue.Enqueue(ctx, request); err != nil {
		return fmt.Errorf("enqueue sync request: %w", err)
	}
	if err := r.syncQueue.Prune(ctx); err != nil {
		return fmt.Errorf("prune sync queue: %w", err)
	}
	if err := r.flushSyncQueue(ctx, currentBatchID); err != nil {
		return fmt.Errorf("flush sync queue: %w", err)
	}
	return nil
}

func (r *Runtime) flushSyncQueue(ctx context.Context, currentBatchID string) error {
	entries, err := r.syncQueue.List(ctx)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		response, err := r.syncRequest(ctx, entry, currentBatchID)
		if err != nil {
			if markErr := r.syncQueue.MarkAttempt(ctx, entry.ID); markErr != nil {
				return fmt.Errorf("mark sync attempt for %s: %w", entry.ID, markErr)
			}
			return err
		}
		if err := r.syncQueue.Delete(ctx, entry.ID); err != nil {
			return fmt.Errorf("delete synced queue entry %s: %w", entry.ID, err)
		}
		r.applySyncPlan(response)
	}
	return nil
}

func (r *Runtime) syncRequest(ctx context.Context, entry syncqueue.Entry, currentBatchID string) (*agentapi.SyncResponse, error) {
	request := entry.Request
	if entry.Attempts > 0 || syncBatchIDForRequest(entry.Request) != currentBatchID {
		request = syncqueue.WithBackfilledFacts(entry.Request, true)
	}
	response, err := r.client.Sync(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("%w: sync heartbeat: %w", errRemoteSync, err)
	}
	return response, nil
}

func (r *Runtime) applySyncPlan(response *agentapi.SyncResponse) {
	if response != nil && response.Plan != nil {
		r.currentPlan = cloneSyncPlan(response.Plan)

		// Execute any pending action from the center.
		if response.Plan.PendingAction != nil {
			action := response.Plan.PendingAction
			bin, args, ok := agentexec.Lookup(action.CommandID)
			if ok {
				result := agentexec.Run(context.Background(), bin, args)
				result.ActionID = action.ActionID
				result.CommandID = action.CommandID
				r.pendingResults = append(r.pendingResults, result)
			}
			// Unknown command ID is silently ignored — the center
			// should not send IDs the agent doesn't know, but we
			// never block the sync loop on it.
		}
	} else {
		r.currentPlan = nil
	}
}

func syncBatchIDForRequest(request agentapi.SyncRequest) string {
	if len(request.Heartbeats) == 0 {
		return ""
	}
	return request.Heartbeats[0].SyncBatchID
}

func (r *Runtime) collectHostSample(observedAt time.Time, fingerprint, syncBatchID string) *agentapi.HostSamplePayload {
	if r.hostSampleProvider == nil || r.currentPlan == nil {
		return nil
	}
	if !hostSampleDue(r.currentPlan.HostSampleFrequencyTier, r.lastHostSampleAt, observedAt) {
		return nil
	}

	sample, err := r.hostSampleProvider.Collect(observedAt)
	if err != nil {
		r.logger.Error("collect host sample failed", "error", err)
		return nil
	}
	sample.ObservedAt = observedAt
	sample.AgentVersion = agentVersion
	sample.Fingerprint = fingerprint
	sample.SyncBatchID = syncBatchID
	sample.MaintenanceContext = r.currentPlan.HostSampleMaintenanceContext
	r.lastHostSampleAt = observedAt

	// Attach Docker container info if available. Failure is silent
	// — container collection is best-effort and must not block host
	// sample delivery.
	if containers, ccErr := containersample.Collect(context.Background()); ccErr == nil && len(containers) > 0 {
		sample.Containers = containers
	}

	return &sample
}

func (r *Runtime) collectProbeObservations(ctx context.Context, observedAt time.Time, fingerprint, syncBatchID string) ([]agentapi.ProbeObservationPayload, error) {
	if r.probeProvider == nil || r.currentPlan == nil || len(r.currentPlan.ProbeAssignments) == 0 {
		return nil, nil
	}
	observations, err := r.probeProvider.CollectDue(ctx, r.currentPlan, observedAt)
	if err != nil {
		return nil, err
	}
	for i := range observations {
		observations[i].ObservedAt = observedAt
		observations[i].AgentVersion = agentVersion
		observations[i].Fingerprint = fingerprint
		observations[i].SyncBatchID = syncBatchID
	}
	return observations, nil
}

func hostSampleDue(frequencyTier string, lastAt, observedAt time.Time) bool {
	duration, ok := frequencyTierDuration(frequencyTier)
	if !ok {
		return false
	}
	if lastAt.IsZero() {
		return true
	}
	return !observedAt.Before(lastAt.Add(duration))
}

func frequencyTierDuration(tier string) (time.Duration, bool) {
	switch tier {
	case agentapi.FrequencyTier1m:
		return time.Minute, true
	case agentapi.FrequencyTier5m:
		return 5 * time.Minute, true
	case agentapi.FrequencyTier15m:
		return 15 * time.Minute, true
	case agentapi.FrequencyTier6h:
		return 6 * time.Hour, true
	default:
		return 0, false
	}
}

func cloneSyncPlan(plan *agentapi.SyncPlan) *agentapi.SyncPlan {
	if plan == nil {
		return nil
	}
	cloned := &agentapi.SyncPlan{
		HostSampleFrequencyTier:      plan.HostSampleFrequencyTier,
		HostSampleMaintenanceContext: plan.HostSampleMaintenanceContext,
		ProbeAssignments:             make([]agentapi.ProbeAssignment, 0, len(plan.ProbeAssignments)),
	}
	if plan.PendingAction != nil {
		cloned.PendingAction = &agentapi.PendingAction{
			CommandID: plan.PendingAction.CommandID,
			ActionID:  plan.PendingAction.ActionID,
		}
	}
	for _, assignment := range plan.ProbeAssignments {
		var basePort *int
		if assignment.TargetBasePort != nil {
			value := *assignment.TargetBasePort
			basePort = &value
		}
		cloned.ProbeAssignments = append(cloned.ProbeAssignments, agentapi.ProbeAssignment{
			TargetID:           assignment.TargetID,
			TargetHost:         assignment.TargetHost,
			TargetBasePort:     basePort,
			MaintenanceContext: assignment.MaintenanceContext,
			ProbeItemID:        assignment.ProbeItemID,
			ProbeKind:          assignment.ProbeKind,
			FrequencyTier:      assignment.FrequencyTier,
			TimeoutSeconds:     assignment.TimeoutSeconds,
			Config:             append([]byte(nil), assignment.Config...),
		})
	}
	return cloned
}
