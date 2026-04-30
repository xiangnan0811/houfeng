package auth

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

type countingSessions struct {
	*fakeSessions
	calls atomic.Int32
}

func (c *countingSessions) DeleteExpiredBefore(ctx context.Context, cutoff time.Time) (int, error) {
	c.calls.Add(1)
	return c.fakeSessions.DeleteExpiredBefore(ctx, cutoff)
}

func TestSessionCleanupWorkerCallsDeleteExpired(t *testing.T) {
	store := &countingSessions{fakeSessions: newFakeSessions()}
	worker := NewSessionCleanupWorker(store, slog.Default(), 50*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := worker.Run(ctx)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run = %v, want nil or DeadlineExceeded", err)
	}
	if store.calls.Load() < 2 {
		t.Fatalf("DeleteExpiredBefore called %d times, want >= 2", store.calls.Load())
	}
}

func TestNewSessionCleanupWorkerDefaults(t *testing.T) {
	w := NewSessionCleanupWorker(newFakeSessions(), nil, 0)
	if w.interval != DefaultSessionCleanupInterval {
		t.Fatalf("interval = %v, want default %v", w.interval, DefaultSessionCleanupInterval)
	}
	if w.logger == nil {
		t.Fatal("logger must default to slog.Default()")
	}
}
