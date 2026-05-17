package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"

	"houfeng/internal/center/config"
)

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}

func setupLogging(cfg config.CenterConfig) (func(), error) {
	if cfg.LogFile == "" {
		logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
		slog.SetDefault(logger)
		return func() {}, nil
	}

	logFile, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open log file %s: %w", cfg.LogFile, err)
	}
	if err := logFile.Chmod(0o644); err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("set log file permissions %s: %w", cfg.LogFile, err)
	}

	writer := &lockedWriter{w: io.MultiWriter(os.Stdout, logFile)}
	logger := slog.New(slog.NewTextHandler(writer, nil))
	slog.SetDefault(logger)

	return func() {
		_ = logFile.Close()
	}, nil
}
