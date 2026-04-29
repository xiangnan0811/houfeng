package auth

import (
	"context"
	"log/slog"
	"time"
)

const DefaultSessionCleanupInterval = time.Hour

type SessionCleanupWorker struct {
	sessions SessionRepository
	logger   *slog.Logger
	interval time.Duration
}

func NewSessionCleanupWorker(sessions SessionRepository, logger *slog.Logger, interval time.Duration) *SessionCleanupWorker {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = DefaultSessionCleanupInterval
	}
	return &SessionCleanupWorker{sessions: sessions, logger: logger, interval: interval}
}

func (w *SessionCleanupWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	w.sweep(ctx)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			w.sweep(ctx)
		}
	}
}

func (w *SessionCleanupWorker) sweep(ctx context.Context) {
	n, err := w.sessions.DeleteExpiredBefore(ctx, time.Now().UTC())
	if err != nil {
		w.logger.Error("session sweep", "error", err)
		return
	}
	if n > 0 {
		w.logger.Info("session sweep", "deleted", n)
	}
}
