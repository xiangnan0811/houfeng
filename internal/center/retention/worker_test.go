package retention

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	centersettings "houfeng/internal/center/settings"
)

type fakeRepository struct {
	results []Result
	errs    []error
	calls   []Policy
	nows    []time.Time
	block   chan struct{}
}

func (f *fakeRepository) ApplyRetention(ctx context.Context, policy Policy, now time.Time) (Result, error) {
	f.calls = append(f.calls, policy)
	f.nows = append(f.nows, now)
	if f.block != nil {
		<-f.block
		return Result{}, ctx.Err()
	}
	idx := len(f.calls) - 1
	if idx < len(f.errs) && f.errs[idx] != nil {
		return Result{}, f.errs[idx]
	}
	if idx < len(f.results) {
		return f.results[idx], nil
	}
	return Result{}, nil
}

type fakeSettingsRepository struct {
	records []centersettings.CenterSettings
	errs    []error
	calls   int
	block   chan struct{}
}

func (f *fakeSettingsRepository) GetSettings(ctx context.Context) (centersettings.CenterSettings, error) {
	idx := f.calls
	f.calls++
	if f.block != nil {
		<-f.block
		return centersettings.CenterSettings{}, ctx.Err()
	}
	if idx < len(f.errs) && f.errs[idx] != nil {
		return centersettings.CenterSettings{}, f.errs[idx]
	}
	if idx < len(f.records) {
		return f.records[idx], nil
	}
	return centersettings.Default(), nil
}

func TestWorkerRunsRetentionPassOnStartup(t *testing.T) {
	repo := &fakeRepository{}
	settingsRepo := &fakeSettingsRepository{records: []centersettings.CenterSettings{settingsWithRetention(3, 30, 90, 180)}}
	worker := NewWorker(repo, settingsRepo, slog.Default(), time.Hour)
	now := time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC)
	worker.now = func() time.Time { return now }

	ctx, cancel := context.WithCancel(context.Background())
	worker.afterPass = cancel
	if err := worker.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(repo.calls) != 1 {
		t.Fatalf("len(calls) = %d, want 1", len(repo.calls))
	}
	if repo.calls[0].RawLayerDays != 3 || repo.calls[0].AggregateLayerDays != 30 || repo.calls[0].EventLayerDays != 90 || repo.calls[0].NotificationLayerDays != 180 {
		t.Fatalf("policy = %#v, want settings retention policy", repo.calls[0])
	}
	if !repo.nows[0].Equal(now) {
		t.Fatalf("now = %s, want %s", repo.nows[0], now)
	}
}

func TestWorkerContinuesAfterRepositoryFailureAndReloadsSettings(t *testing.T) {
	repo := &fakeRepository{errs: []error{errors.New("retention boom")}}
	settingsRepo := &fakeSettingsRepository{records: []centersettings.CenterSettings{
		settingsWithRetention(7, 30, 90, 180),
		settingsWithRetention(14, 60, 120, 240),
	}}
	worker := NewWorker(repo, settingsRepo, slog.Default(), time.Millisecond)
	calls := 0
	worker.now = func() time.Time {
		calls++
		return time.Date(2026, time.April, 28, 12, 0, calls, 0, time.UTC)
	}
	ctx, cancel := context.WithCancel(context.Background())
	worker.afterPass = func() {
		if len(repo.calls) == 2 {
			cancel()
		}
	}

	if err := worker.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(repo.calls) != 2 {
		t.Fatalf("len(calls) = %d, want 2", len(repo.calls))
	}
	if repo.calls[0].RawLayerDays != 7 || repo.calls[1].RawLayerDays != 14 {
		t.Fatalf("policies = %#v, want latest settings each pass", repo.calls)
	}
}

func TestWorkerStopsOnContextCancellationBeforeFirstPass(t *testing.T) {
	repo := &fakeRepository{}
	worker := NewWorker(repo, &fakeSettingsRepository{}, slog.Default(), time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := worker.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(repo.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0", len(repo.calls))
	}
}

func TestWorkerTreatsCancellationDuringApplyAsNormalShutdown(t *testing.T) {
	repo := &fakeRepository{block: make(chan struct{})}
	settingsRepo := &fakeSettingsRepository{records: []centersettings.CenterSettings{settingsWithRetention(7, 30, 90, 180)}}
	var logs strings.Builder
	worker := NewWorker(repo, settingsRepo, slog.New(slog.NewTextHandler(&logs, nil)), time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- worker.Run(ctx)
	}()

	deadline := time.After(time.Second)
	for len(repo.calls) == 0 {
		select {
		case <-deadline:
			t.Fatal("worker did not enter ApplyRetention")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	close(repo.block)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not return after cancellation during ApplyRetention")
	}
	if strings.Contains(logs.String(), "apply retention failed") {
		t.Fatalf("logs = %q, want no failure log for cancellation", logs.String())
	}
}

func TestWorkerTreatsCancellationDuringSettingsLoadAsNormalShutdown(t *testing.T) {
	repo := &fakeRepository{}
	settingsRepo := &fakeSettingsRepository{block: make(chan struct{})}
	var logs strings.Builder
	worker := NewWorker(repo, settingsRepo, slog.New(slog.NewTextHandler(&logs, nil)), time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- worker.Run(ctx)
	}()

	deadline := time.After(time.Second)
	for settingsRepo.calls == 0 {
		select {
		case <-deadline:
			t.Fatal("worker did not enter GetSettings")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	close(settingsRepo.block)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not return after cancellation during GetSettings")
	}
	if strings.Contains(logs.String(), "load retention settings failed") {
		t.Fatalf("logs = %q, want no failure log for cancellation", logs.String())
	}
}

func TestWorkerStopsWhileSleepingOnTimer(t *testing.T) {
	repo := &fakeRepository{}
	settingsRepo := &fakeSettingsRepository{}
	worker := NewWorker(repo, settingsRepo, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	worker.afterPass = cancel

	done := make(chan error, 1)
	go func() {
		done <- worker.Run(ctx)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop while sleeping on timer")
	}
	if len(repo.calls) != 1 {
		t.Fatalf("len(calls) = %d, want startup pass only", len(repo.calls))
	}
}

func settingsWithRetention(raw, aggregate, event, notification int) centersettings.CenterSettings {
	record := centersettings.Default()
	record.RetentionPolicy = centersettings.RetentionPolicy{
		RawLayerDays:          raw,
		AggregateLayerDays:    aggregate,
		EventLayerDays:        event,
		NotificationLayerDays: notification,
	}
	return record
}
