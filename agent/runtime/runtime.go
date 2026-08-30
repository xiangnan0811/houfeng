package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"time"

	agentconfig "houfeng/agent/config"
	"houfeng/agent/containersample"
	"houfeng/agent/enroll"
	agentexec "houfeng/agent/exec"
	"houfeng/agent/hostsample"
	agentipquality "houfeng/agent/ipquality"
	"houfeng/agent/probe"
	"houfeng/agent/syncqueue"
	"houfeng/internal/contracts/agentapi"
)

const (
	defaultInterval                = 5 * time.Second
	maxBacklogSyncAttemptsPerRound = 2
	replayProgressLogInterval      = time.Minute
	replayProgressLogMessage       = "sync queue replay progress"
)

var agentVersion = "dev"

var ErrMissingSyncToken = errors.New("enrollment bound response missing sync token")

var errRemoteSync = errors.New("remote sync failed")

var errStaleSyncAuthority = errors.New("stale sync queue authority")

var errCurrentSyncQueueEntryMissing = errors.New("current sync queue entry missing")

var errCurrentSyncQueueEntryAmbiguous = errors.New("current sync queue entry ambiguous")

var errSyncQueueReplayRetry = errors.New("sync queue replay retry pending")

type syncQueueAction string

type syncFailureKind string

type syncQueueEntryDisposition string

type syncRoundResult struct {
	ackedEntries     int
	remainingEntries int
}

const (
	syncQueueActionDiscard     syncQueueAction           = "discard"
	syncQueueActionTerminal    syncQueueAction           = "terminal"
	syncQueueActionRetry       syncQueueAction           = "retry"
	syncFailureKindRemote      syncFailureKind           = "remote"
	syncFailureKindTransport   syncFailureKind           = "transport"
	syncQueueEntryAcknowledged syncQueueEntryDisposition = "acknowledged"
	syncQueueEntryDiscarded    syncQueueEntryDisposition = "discarded"
)

type syncAuthority struct {
	monitoringInstanceID string
	syncToken            string
	fingerprint          string
}

type syncPolicyError struct {
	kind       syncFailureKind
	action     syncQueueAction
	statusCode int
	code       string
	cause      error
}

type remoteOperationError struct {
	operation  string
	kind       syncFailureKind
	statusCode int
	code       string
	cause      error
}

func (e *remoteOperationError) Error() string {
	message := fmt.Sprintf("remote operation failed operation=%s kind=%s", e.operation, e.kind)
	if e.statusCode > 0 {
		message += fmt.Sprintf(" status=%d", e.statusCode)
	}
	if e.code != "" {
		message += fmt.Sprintf(" code=%s", e.code)
	}
	return message
}

func (e *remoteOperationError) Unwrap() error {
	return e.cause
}

type syncQueueOperationError struct {
	operation string
	cause     error
}

func (e *syncQueueOperationError) Error() string {
	return fmt.Sprintf("sync queue operation failed operation=%s", e.operation)
}

func (e *syncQueueOperationError) Unwrap() error {
	return e.cause
}

type runtimeOperationError struct {
	operation string
	cause     error
}

func (e *runtimeOperationError) Error() string {
	return fmt.Sprintf("runtime operation failed operation=%s", e.operation)
}

func (e *runtimeOperationError) Unwrap() error {
	return e.cause
}

func (e *syncPolicyError) Error() string {
	message := fmt.Sprintf("sync failure kind=%s action=%s", e.kind, e.action)
	if e.statusCode > 0 {
		message += fmt.Sprintf(" status=%d", e.statusCode)
	}
	if e.code != "" {
		message += fmt.Sprintf(" code=%s", e.code)
	}
	return message
}

func (e *syncPolicyError) Unwrap() error {
	return e.cause
}

func (e *syncPolicyError) Is(target error) bool {
	return e.action == syncQueueActionRetry && target == errRemoteSync
}

type EnrollmentNotBoundError struct {
	BindingStatus string
}

func (e *EnrollmentNotBoundError) Error() string {
	if e.BindingStatus == "" {
		return "enrollment binding_status is not bound"
	}
	switch e.BindingStatus {
	case agentapi.BindingStatusUnbound, agentapi.BindingStatusPendingConfirmation:
		return fmt.Sprintf("enrollment binding_status is %q, want %q", e.BindingStatus, agentapi.BindingStatusBound)
	default:
		return fmt.Sprintf("enrollment binding_status is unrecognized, want %q", agentapi.BindingStatusBound)
	}
}

type Client interface {
	Enroll(context.Context, agentapi.EnrollmentRequest) (*agentapi.EnrollmentResponse, error)
	Sync(context.Context, agentapi.SyncRequest) (*agentapi.SyncResponse, error)
}

type TokenSource interface {
	Token(context.Context) (string, error)
}

type SyncCredentialStore interface {
	SyncCredentials(context.Context) (monitoringInstanceID string, syncToken string, ok bool, err error)
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

type IPQualityProvider interface {
	MaybeStart(context.Context, *agentapi.IPQualityPlan, time.Time) error
	DrainReports() []agentapi.IPQualityReportPayload
}

type SyncQueue interface {
	Enqueue(context.Context, agentapi.SyncRequest) (string, error)
	List(context.Context) ([]syncqueue.Entry, error)
	Delete(context.Context, string) error
	DeleteMany(context.Context, []string) error
	MarkAttempt(context.Context, string) error
	Prune(context.Context) error
}

type Runtime struct {
	cfg                  agentconfig.AgentConfig
	client               Client
	logger               *slog.Logger
	tokenSource          TokenSource
	syncCredentialStore  SyncCredentialStore
	fingerprintSource    FingerprintSource
	hostSampleProvider   HostSampleProvider
	probeProvider        ProbeProvider
	ipQualityProvider    IPQualityProvider
	interval             time.Duration
	syncQueue            SyncQueue
	currentPlan          *agentapi.SyncPlan
	lastHostSampleAt     time.Time
	pendingResults       []agentexec.Result
	pendingIPReports     []agentapi.IPQualityReportPayload
	now                  func() time.Time
	replayActive         bool
	lastReplayProgressAt time.Time
}

func New(cfg agentconfig.AgentConfig, logger *slog.Logger, tokenSource TokenSource, fingerprintSource FingerprintSource) *Runtime {
	queue := syncqueue.NewFileStore(cfg.BufferFile, syncqueue.Options{
		MaxEntries: cfg.BufferMaxEntries,
		MaxAge:     cfg.BufferMaxAge,
		MaxBytes:   cfg.BufferMaxBytes,
	})
	ipStateStore := agentipquality.NewFileStateStore(cfg.IPQualityStateFile)
	ipCollector := agentipquality.NewHTTPCollector(agentipquality.HTTPCollectorOptions{})
	ipProvider := agentipquality.NewManager(ipStateStore, ipCollector)
	return NewWithRuntimeDeps(cfg, logger, enroll.NewClient(cfg.ServerURL), tokenSource, fingerprintSource, hostsample.New(), probe.New(), defaultInterval, queue, ipProvider)
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
	optionalDeps ...any,
) *Runtime {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = defaultInterval
	}
	var queue SyncQueue
	var ipQualityProvider IPQualityProvider
	now := time.Now
	for _, dep := range optionalDeps {
		switch typed := dep.(type) {
		case nil:
			continue
		case SyncQueue:
			if queue == nil {
				queue = typed
			}
		case IPQualityProvider:
			ipQualityProvider = typed
		case func() time.Time:
			if typed != nil {
				now = typed
			}
		}
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
		ipQualityProvider:   ipQualityProvider,
		interval:            interval,
		syncQueue:           queue,
		now:                 now,
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

	serverURL := serverURLForLog(r.cfg.ServerURL)
	r.logger.Info("agent runtime started", "server_url", serverURL)
	defer r.logger.Info("agent runtime stopped", "server_url", serverURL)

	fingerprint, err := r.fingerprintSource.Fingerprint(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return &runtimeOperationError{operation: "load_fingerprint", cause: err}
	}

	monitoringInstanceID, syncToken, ok, err := r.loadSyncCredentials(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return &runtimeOperationError{operation: "load_sync_credentials", cause: err}
	}
	if !ok {
		enrollment, err := r.enroll(ctx, fingerprint)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		monitoringInstanceID = enrollment.MonitoringInstanceID
		syncToken = enrollment.SyncToken
	}
	authority := syncAuthority{
		monitoringInstanceID: monitoringInstanceID,
		syncToken:            syncToken,
		fingerprint:          fingerprint,
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
			request := r.buildSyncRequest(ctx, monitoringInstanceID, syncToken, observedAt, fingerprint, syncBatchID)

			if r.syncQueue == nil {
				response, err := r.client.Sync(ctx, request)
				if err != nil {
					if ctx.Err() != nil {
						return nil
					}
					return fmt.Errorf("sync heartbeat: %w", classifySyncFailure(err))
				}
				r.acknowledgePendingPayloads(len(request.CommandResults), len(request.IPQualityReports))
				r.applySyncPlan(ctx, response)
				continue
			}

			round, err := r.enqueueAndFlush(ctx, request, authority)
			if err != nil {
				if ctx.Err() != nil {
					return nil
				}
				var policyErr *syncPolicyError
				if errors.As(err, &policyErr) && policyErr.action == syncQueueActionTerminal {
					return fmt.Errorf("sync queue terminal failure: %w", err)
				}
				if errors.Is(err, errRemoteSync) {
					r.logSyncQueueReplayRetry(round, policyErr)
					continue
				}
				return fmt.Errorf("sync queue operation failed: %w", err)
			}
			r.logSyncQueueReplayProgress(round)
		}
	}
}

func (r *Runtime) loadSyncCredentials(ctx context.Context) (string, string, bool, error) {
	if r.syncCredentialStore == nil {
		return "", "", false, nil
	}
	monitoringInstanceID, syncToken, ok, err := r.syncCredentialStore.SyncCredentials(ctx)
	if err != nil {
		return "", "", false, fmt.Errorf("load sync credentials: %w", err)
	}
	return monitoringInstanceID, syncToken, ok, nil
}

func (r *Runtime) enroll(ctx context.Context, fingerprint string) (*agentapi.EnrollmentResponse, error) {
	token, err := r.tokenSource.Token(ctx)
	if err != nil {
		return nil, &runtimeOperationError{operation: "load_enrollment_token", cause: err}
	}

	enrollment, err := r.client.Enroll(ctx, agentapi.EnrollmentRequest{
		Token:       token,
		Fingerprint: fingerprint,
	})
	if err != nil {
		return nil, fmt.Errorf("enroll agent: %w", classifyRemoteOperationFailure("enroll", err))
	}

	if enrollment.BindingStatus != agentapi.BindingStatusBound {
		return nil, fmt.Errorf("enroll agent: %w", &EnrollmentNotBoundError{BindingStatus: enrollment.BindingStatus})
	}
	if enrollment.SyncToken == "" {
		return nil, fmt.Errorf("enroll agent: %w", ErrMissingSyncToken)
	}
	if r.syncCredentialStore != nil {
		if err := r.syncCredentialStore.SaveSyncCredentials(ctx, enrollment.MonitoringInstanceID, enrollment.SyncToken); err != nil {
			return nil, &runtimeOperationError{operation: "persist_sync_credentials", cause: err}
		}
	}
	enrollmentStatus := "unrecognized"
	if enrollment.Status == "accepted" {
		enrollmentStatus = enrollment.Status
	}
	r.logger.Info("agent enrolled", "status", enrollmentStatus, "binding_status", enrollment.BindingStatus)
	return enrollment, nil
}

func (r *Runtime) buildSyncRequest(ctx context.Context, monitoringInstanceID, syncToken string, observedAt time.Time, fingerprint, syncBatchID string) agentapi.SyncRequest {
	request := agentapi.SyncRequest{
		MonitoringInstanceID: monitoringInstanceID,
		SyncToken:            syncToken,
		Heartbeats: []agentapi.MonitoringInstanceHeartbeat{{
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

	r.pendingIPReports = append(r.pendingIPReports, r.drainIPQualityReports(observedAt, fingerprint, syncBatchID)...)
	request.IPQualityReports = append(request.IPQualityReports, r.pendingIPReports...)
	r.startIPQualityCollection(ctx, observedAt)

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
	return request
}

func (r *Runtime) enqueueAndFlush(ctx context.Context, request agentapi.SyncRequest, authority syncAuthority) (syncRoundResult, error) {
	currentEntryID, err := r.syncQueue.Enqueue(ctx, request)
	if err != nil {
		return syncRoundResult{}, &syncQueueOperationError{operation: "enqueue", cause: err}
	}
	r.acknowledgePendingPayloads(len(request.CommandResults), len(request.IPQualityReports))
	if err := r.syncQueue.Prune(ctx); err != nil {
		return syncRoundResult{}, &syncQueueOperationError{operation: "prune", cause: err}
	}
	round, err := r.flushSyncQueue(ctx, currentEntryID, authority)
	if err != nil {
		return round, fmt.Errorf("flush sync queue: %w", err)
	}
	return round, nil
}

func (r *Runtime) acknowledgePendingPayloads(commandResults, ipQualityReports int) {
	if commandResults >= len(r.pendingResults) {
		r.pendingResults = nil
	} else if commandResults > 0 {
		r.pendingResults = r.pendingResults[commandResults:]
	}
	if ipQualityReports >= len(r.pendingIPReports) {
		r.pendingIPReports = nil
	} else if ipQualityReports > 0 {
		r.pendingIPReports = r.pendingIPReports[ipQualityReports:]
	}
}

func (r *Runtime) flushSyncQueue(ctx context.Context, currentEntryID string, authority syncAuthority) (syncRoundResult, error) {
	entries, err := r.syncQueue.List(ctx)
	if err != nil {
		return syncRoundResult{}, &syncQueueOperationError{operation: "list", cause: err}
	}
	entries, err = r.discardStaleSyncQueueEntries(ctx, entries, authority)
	if err != nil {
		return syncRoundResult{}, err
	}
	backlog := make([]syncqueue.Entry, 0, len(entries))
	var current *syncqueue.Entry
	for i := range entries {
		entry := entries[i]
		if entry.ID != currentEntryID {
			backlog = append(backlog, entry)
			continue
		}
		if current != nil {
			return syncRoundResult{}, &syncQueueOperationError{operation: "locate_current", cause: errCurrentSyncQueueEntryAmbiguous}
		}
		current = &entry
	}
	if current == nil {
		return syncRoundResult{}, &syncQueueOperationError{operation: "locate_current", cause: errCurrentSyncQueueEntryMissing}
	}
	round := syncRoundResult{remainingEntries: len(backlog)}

	var backlogRetryErr error
	for i := 0; i < len(backlog) && i < maxBacklogSyncAttemptsPerRound; i++ {
		disposition, err := r.flushSyncQueueEntry(ctx, backlog[i], false)
		if err != nil {
			var policyErr *syncPolicyError
			if errors.As(err, &policyErr) && policyErr.action == syncQueueActionRetry {
				backlogRetryErr = err
				break
			}
			return round, err
		}
		round.remainingEntries--
		if disposition == syncQueueEntryAcknowledged {
			round.ackedEntries++
		}
	}

	if _, err := r.flushSyncQueueEntry(ctx, *current, true); err != nil {
		round.remainingEntries++
		return round, err
	}
	if backlogRetryErr != nil {
		return round, backlogRetryErr
	}
	return round, nil
}

func (r *Runtime) flushSyncQueueEntry(ctx context.Context, entry syncqueue.Entry, current bool) (syncQueueEntryDisposition, error) {
	response, err := r.syncRequest(ctx, entry, current)
	if err != nil {
		if markErr := r.syncQueue.MarkAttempt(ctx, entry.ID); markErr != nil {
			return "", &syncQueueOperationError{operation: "mark_attempt", cause: markErr}
		}
		var policyErr *syncPolicyError
		if !errors.As(err, &policyErr) {
			return "", err
		}
		switch policyErr.action {
		case syncQueueActionDiscard:
			if deleteErr := r.syncQueue.Delete(ctx, entry.ID); deleteErr != nil {
				return "", &syncQueueOperationError{operation: "delete_rejected", cause: deleteErr}
			}
			r.logSyncQueuePolicy("discard rejected sync queue entry", policyErr)
			return syncQueueEntryDiscarded, nil
		case syncQueueActionTerminal, syncQueueActionRetry:
			return "", policyErr
		default:
			return "", err
		}
	}
	if err := r.syncQueue.Delete(ctx, entry.ID); err != nil {
		return "", &syncQueueOperationError{operation: "delete_acknowledged", cause: err}
	}
	r.applySyncPlan(ctx, response)
	return syncQueueEntryAcknowledged, nil
}

func (r *Runtime) discardStaleSyncQueueEntries(ctx context.Context, entries []syncqueue.Entry, authority syncAuthority) ([]syncqueue.Entry, error) {
	staleIDs := make([]string, 0)
	retained := make([]syncqueue.Entry, 0, len(entries))
	retainedIDs := make(map[string]struct{})
	reasonCounts := make(map[string]int)
	for _, entry := range entries {
		if _, stale := syncQueueAuthorityMismatch(entry.Request, authority); !stale {
			retained = append(retained, entry)
			retainedIDs[entry.ID] = struct{}{}
		}
	}
	for _, entry := range entries {
		reason, stale := syncQueueAuthorityMismatch(entry.Request, authority)
		if !stale {
			continue
		}
		if _, collidesWithRetained := retainedIDs[entry.ID]; collidesWithRetained {
			continue
		}
		staleIDs = append(staleIDs, entry.ID)
		reasonCounts[reason]++
	}
	if len(staleIDs) == 0 {
		return retained, nil
	}
	if err := r.syncQueue.DeleteMany(ctx, staleIDs); err != nil {
		return nil, &syncQueueOperationError{operation: "delete_stale", cause: err}
	}

	reasons := make([]string, 0, len(reasonCounts))
	for reason := range reasonCounts {
		reasons = append(reasons, reason)
	}
	sort.Strings(reasons)
	for _, reason := range reasons {
		r.logger.Error(
			"discard stale sync queue entries",
			"error", errStaleSyncAuthority,
			"action", syncQueueActionDiscard,
			"reason", reason,
			"discarded", reasonCounts[reason],
		)
	}
	return retained, nil
}

func (r *Runtime) syncRequest(ctx context.Context, entry syncqueue.Entry, current bool) (*agentapi.SyncResponse, error) {
	request := entry.Request
	if !current {
		request = syncqueue.WithBackfilledFacts(entry.Request, true)
	}
	response, err := r.client.Sync(ctx, request)
	if err != nil {
		return nil, classifySyncFailure(err)
	}
	return response, nil
}

func (r *Runtime) logSyncQueuePolicy(message string, policyErr *syncPolicyError) {
	attributes := []any{
		"error", policyErr,
		"kind", policyErr.kind,
		"action", policyErr.action,
	}
	if policyErr.statusCode > 0 {
		attributes = append(attributes, "status", policyErr.statusCode)
	}
	if policyErr.code != "" {
		attributes = append(attributes, "code", policyErr.code)
	}
	r.logger.Error(message, attributes...)
}

func (r *Runtime) logSyncQueueReplayProgress(round syncRoundResult) {
	if round.ackedEntries == 0 {
		if round.remainingEntries > 0 {
			r.replayActive = true
			return
		}
		r.replayActive = false
		r.lastReplayProgressAt = time.Time{}
		return
	}
	if round.remainingEntries > 0 {
		enteringReplay := !r.replayActive
		r.replayActive = true
		now := r.now()
		if !enteringReplay && !r.lastReplayProgressAt.IsZero() && now.Before(r.lastReplayProgressAt.Add(replayProgressLogInterval)) {
			return
		}
		r.lastReplayProgressAt = now
		r.logger.Info(
			replayProgressLogMessage,
			"state", "catching_up",
			"acked_entries", round.ackedEntries,
			"remaining_entries", round.remainingEntries,
		)
		return
	}
	r.replayActive = false
	r.lastReplayProgressAt = time.Time{}
	r.logger.Info(
		replayProgressLogMessage,
		"state", "caught_up",
		"acked_entries", round.ackedEntries,
		"remaining_entries", 0,
	)
}

func (r *Runtime) logSyncQueueReplayRetry(round syncRoundResult, policyErr *syncPolicyError) {
	if round.remainingEntries > 0 {
		r.replayActive = true
	}
	attributes := []any{
		"error", errSyncQueueReplayRetry,
		"state", "retrying",
		"kind", policyErr.kind,
		"action", policyErr.action,
	}
	if policyErr.statusCode > 0 {
		attributes = append(attributes, "status", policyErr.statusCode)
	}
	if policyErr.code != "" {
		attributes = append(attributes, "code", policyErr.code)
	}
	attributes = append(
		attributes,
		"acked_entries", round.ackedEntries,
		"remaining_entries", round.remainingEntries,
	)
	r.logger.Error(replayProgressLogMessage, attributes...)
}

func classifyRemoteOperationFailure(operation string, err error) *remoteOperationError {
	operationErr := &remoteOperationError{
		operation: operation,
		kind:      syncFailureKindTransport,
		cause:     err,
	}
	var remoteErr *enroll.RemoteError
	if !errors.As(err, &remoteErr) || remoteErr == nil {
		return operationErr
	}
	operationErr.kind = syncFailureKindRemote
	operationErr.statusCode = remoteErr.StatusCode
	operationErr.code = allowlistedAgentErrorCode(remoteErr.Code)
	return operationErr
}

func serverURLForLog(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "invalid"
	}
	return parsed.Scheme + "://" + parsed.Host
}

func classifySyncFailure(err error) *syncPolicyError {
	policyErr := &syncPolicyError{
		kind:   syncFailureKindTransport,
		action: syncQueueActionRetry,
		cause:  err,
	}
	var remoteErr *enroll.RemoteError
	if !errors.As(err, &remoteErr) || remoteErr == nil {
		return policyErr
	}

	policyErr.kind = syncFailureKindRemote
	policyErr.statusCode = remoteErr.StatusCode
	policyErr.code = allowlistedAgentErrorCode(remoteErr.Code)
	if remoteErr.StatusCode == 429 || remoteErr.StatusCode >= 500 {
		return policyErr
	}
	if remoteErr.StatusCode == http.StatusBadRequest &&
		(remoteErr.Code == agentapi.ErrorCodeInvalidJSON || remoteErr.Code == agentapi.ErrorCodeInvalidRequest) {
		policyErr.action = syncQueueActionDiscard
		return policyErr
	}
	if remoteErr.StatusCode >= 400 && remoteErr.StatusCode < 500 {
		policyErr.action = syncQueueActionTerminal
	}
	return policyErr
}

func allowlistedAgentErrorCode(code string) string {
	switch code {
	case agentapi.ErrorCodeInvalidRequest,
		agentapi.ErrorCodeInvalidJSON,
		agentapi.ErrorCodeInvalidEnrollmentToken,
		agentapi.ErrorCodeInvalidSyncToken,
		agentapi.ErrorCodeBindingNotAccepted,
		agentapi.ErrorCodeMethodNotAllowed,
		agentapi.ErrorCodeMonitoringInstanceNotFound,
		agentapi.ErrorCodeInternalError:
		return code
	default:
		return ""
	}
}

func syncQueueAuthorityMismatch(request agentapi.SyncRequest, authority syncAuthority) (string, bool) {
	if request.MonitoringInstanceID != authority.monitoringInstanceID {
		return "monitoring_instance_mismatch", true
	}
	if request.SyncToken != authority.syncToken {
		return "sync_token_mismatch", true
	}
	if len(request.Heartbeats) == 0 {
		return "missing_heartbeat", true
	}
	for _, heartbeat := range request.Heartbeats {
		if heartbeat.SyncBatchID == "" {
			return "missing_heartbeat_batch_id", true
		}
		if heartbeat.Fingerprint != authority.fingerprint {
			return "heartbeat_fingerprint_mismatch", true
		}
	}
	for _, sample := range request.HostSamples {
		if sample.Fingerprint != authority.fingerprint {
			return "host_sample_fingerprint_mismatch", true
		}
	}
	for _, observation := range request.ProbeObservations {
		if observation.Fingerprint != authority.fingerprint {
			return "probe_fingerprint_mismatch", true
		}
	}
	for _, report := range request.IPQualityReports {
		if report.Fingerprint != authority.fingerprint {
			return "ip_quality_fingerprint_mismatch", true
		}
	}
	return "", false
}

func (r *Runtime) applySyncPlan(ctx context.Context, response *agentapi.SyncResponse) {
	if response != nil && response.Plan != nil {
		r.currentPlan = cloneSyncPlan(response.Plan)

		// Execute any pending action from the center.
		if response.Plan.PendingAction != nil {
			action := response.Plan.PendingAction
			bin, args, ok := agentexec.Lookup(action.CommandID)
			if ok {
				result := agentexec.Run(ctx, bin, args)
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

func (r *Runtime) drainIPQualityReports(observedAt time.Time, fingerprint, syncBatchID string) []agentapi.IPQualityReportPayload {
	if r.ipQualityProvider == nil {
		return nil
	}
	reports := r.ipQualityProvider.DrainReports()
	for i := range reports {
		if reports[i].ObservedAt.IsZero() {
			reports[i].ObservedAt = observedAt
		}
		if reports[i].AgentVersion == "" {
			reports[i].AgentVersion = agentVersion
		}
		if reports[i].Fingerprint == "" {
			reports[i].Fingerprint = fingerprint
		}
		if reports[i].SyncBatchID == "" {
			reports[i].SyncBatchID = syncBatchID
		}
	}
	return reports
}

func (r *Runtime) startIPQualityCollection(ctx context.Context, observedAt time.Time) {
	if r.ipQualityProvider == nil || r.currentPlan == nil || r.currentPlan.IPQualityPlan == nil || !r.currentPlan.IPQualityPlan.Enabled {
		return
	}
	if err := r.ipQualityProvider.MaybeStart(ctx, r.currentPlan.IPQualityPlan, observedAt); err != nil {
		r.logger.Error("start ip quality collection failed", "error", err)
	}
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
	case agentapi.FrequencyTier5s:
		return 5 * time.Second, true
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
	if plan.IPQualityPlan != nil {
		cloned.IPQualityPlan = &agentapi.IPQualityPlan{
			Enabled:          plan.IPQualityPlan.Enabled,
			FrequencySeconds: plan.IPQualityPlan.FrequencySeconds,
			TimeoutSeconds:   plan.IPQualityPlan.TimeoutSeconds,
			Services:         append([]string(nil), plan.IPQualityPlan.Services...),
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
