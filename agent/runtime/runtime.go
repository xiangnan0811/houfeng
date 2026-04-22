package runtime

import (
	"context"
	"log/slog"
	"time"

	agentconfig "houfeng/agent/config"
	"houfeng/agent/enroll"
)

type Runtime struct {
	cfg    agentconfig.AgentConfig
	client *enroll.Client
	logger *slog.Logger
}

func New(cfg agentconfig.AgentConfig, logger *slog.Logger) *Runtime {
	if logger == nil {
		logger = slog.Default()
	}

	return &Runtime{
		cfg:    cfg,
		client: enroll.NewClient(cfg.ServerURL),
		logger: logger,
	}
}

func (r *Runtime) Run(ctx context.Context) error {
	r.logger.Info("agent runtime started", "node_name", r.cfg.NodeName, "server_url", r.cfg.ServerURL)
	defer r.logger.Info("agent runtime stopped", "node_name", r.cfg.NodeName)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			r.logger.Debug("agent runtime tick", "node_name", r.cfg.NodeName)
		}
	}
}
