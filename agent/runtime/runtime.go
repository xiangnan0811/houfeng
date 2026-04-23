package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	agentconfig "houfeng/agent/config"
	"houfeng/agent/enroll"
	"houfeng/internal/contracts/agentapi"
)

const (
	defaultInterval = 30 * time.Second
	agentVersion    = "dev"
)

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

type Runtime struct {
	cfg               agentconfig.AgentConfig
	client            Client
	logger            *slog.Logger
	tokenSource       TokenSource
	fingerprintSource FingerprintSource
	interval          time.Duration
}

func New(cfg agentconfig.AgentConfig, logger *slog.Logger, tokenSource TokenSource, fingerprintSource FingerprintSource) *Runtime {
	return NewWithDeps(cfg, logger, enroll.NewClient(cfg.ServerURL), tokenSource, fingerprintSource, defaultInterval)
}

func NewWithDeps(cfg agentconfig.AgentConfig, logger *slog.Logger, client Client, tokenSource TokenSource, fingerprintSource FingerprintSource, interval time.Duration) *Runtime {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = defaultInterval
	}

	return &Runtime{
		cfg:               cfg,
		client:            client,
		logger:            logger,
		tokenSource:       tokenSource,
		fingerprintSource: fingerprintSource,
		interval:          interval,
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

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case tick := <-ticker.C:
			observedAt := tick.UTC()
			_, err := r.client.Sync(ctx, agentapi.SyncRequest{
				NodeID: enrollment.NodeID,
				Heartbeats: []agentapi.NodeHeartbeat{{
					ObservedAt:   observedAt,
					AgentVersion: agentVersion,
					Fingerprint:  fingerprint,
					SyncBatchID:  observedAt.Format(time.RFC3339Nano),
				}},
			})
			if err != nil {
				return fmt.Errorf("sync heartbeat: %w", err)
			}
		}
	}
}
