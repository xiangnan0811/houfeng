package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	if err := runContentProcessorCommand(); err != nil && !errors.Is(err, context.Canceled) {
		logContentProcessorFailure("content processor stopped", err)
		os.Exit(1)
	}
}

func runContentProcessorCommand() error {
	if len(os.Args) >= 2 && os.Args[1] == renderDocumentPDFCommand {
		return runRenderDocumentPDF()
	}
	slog.Info("content processor starting")
	defer slog.Info("content processor stopped")
	config, err := loadContentProcessorConfig()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return runContentProcessor(ctx, config, processorBootstrapDeps{})
}

func runContentProcessor(
	ctx context.Context,
	config contentProcessorConfig,
	dependencies processorBootstrapDeps,
) error {
	runtime, cleanup, err := bootstrapContentProcessor(ctx, config, dependencies)
	if err != nil {
		return err
	}
	defer cleanup()
	return runtime.Run(ctx)
}

func logContentProcessorFailure(message string, err error) {
	// Bootstrap and dependency errors may wrap configuration, endpoint, or
	// scanner details.  Log only a closed category; never emit the raw error.
	slog.Error(message, "class", safeProcessorErrorClass(err))
}
