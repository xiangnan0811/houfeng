package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	agentconfig "houfeng/agent/config"
	"houfeng/agent/enroll"
	"houfeng/agent/hostsample"
	"houfeng/agent/probe"
	"houfeng/internal/contracts/agentapi"
)

const (
	defaultInterval = 30 * time.Second
	agentVersion    = "dev"
)

var ErrMissingSyncToken = errors.New("enrollment bound response missing sync token")

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

type FingerprintSource interface {
	Fingerprint(context.Context) (string, error)
}

type HostSampleProvider interface {
	Collect(time.Time) (agentapi.HostSamplePayload, error)
}

type ProbeProvider interface {
	CollectDue(context.Context, *agentapi.SyncPlan, time.Time) ([]agentapi.ProbeObservationPayload, error)
}

type Runtime struct {
	cfg                agentconfig.AgentConfig
	client             Client
	logger             *slog.Logger
	tokenSource        TokenSource
	fingerprintSource  FingerprintSource
	hostSampleProvider HostSampleProvider
	probeProvider      ProbeProvider
	interval           time.Duration
	currentPlan        *agentapi.SyncPlan
	lastHostSampleAt   time.Time
}

func New(cfg agentconfig.AgentConfig, logger *slog.Logger, tokenSource TokenSource, fingerprintSource FingerprintSource) *Runtime {
	return NewWithDeps(cfg, logger, enroll.NewClient(cfg.ServerURL), tokenSource, fingerprintSource, defaultInterval)
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
) *Runtime {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = defaultInterval
	}

	return &Runtime{
		cfg:                cfg,
		client:             client,
		logger:             logger,
		tokenSource:        tokenSource,
		fingerprintSource:  fingerprintSource,
		hostSampleProvider: hostSampleProvider,
		probeProvider:      probeProvider,
		interval:           interval,
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

	token, err := r.tokenSource.Token(ctx)
	if err != nil {
		return fmt.Errorf("load token: %w", err)
	}

	fingerprint, err := r.fingerprintSource.Fingerprint(ctx)
	if err != nil {
		return fmt.Errorf("load fingerprint: %w", err)
	}

	enrollment, err := r.client.Enroll(ctx, agentapi.EnrollmentRequest{
		Token:       token,
		Fingerprint: fingerprint,
	})
	if err != nil {
		return fmt.Errorf("enroll agent: %w", err)
	}

	r.logger.Info("agent enrolled", "node_id", enrollment.NodeID, "status", enrollment.Status, "binding_status", enrollment.BindingStatus)
	if enrollment.BindingStatus != agentapi.BindingStatusBound {
		return fmt.Errorf("enroll agent: %w", &EnrollmentNotBoundError{BindingStatus: enrollment.BindingStatus})
	}
	if enrollment.SyncToken == "" {
		return fmt.Errorf("enroll agent: %w", ErrMissingSyncToken)
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
			request := agentapi.SyncRequest{
				NodeID:    enrollment.NodeID,
				SyncToken: enrollment.SyncToken,
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

			response, err := r.client.Sync(ctx, request)
			if err != nil {
				return fmt.Errorf("sync heartbeat: %w", err)
			}
			if response != nil && response.Plan != nil {
				r.currentPlan = cloneSyncPlan(response.Plan)
			}
		}
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
