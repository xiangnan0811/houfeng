package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	agentconfig "houfeng/agent/config"
	agentruntime "houfeng/agent/runtime"
)

func main() {
	cfg, err := agentconfig.LoadAgentConfig()
	if err != nil {
		slog.Error("load agent config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	if err := agentruntime.New(cfg, logger).Run(ctx); err != nil {
		logger.Error("run agent runtime", "error", err)
		os.Exit(1)
	}
}
