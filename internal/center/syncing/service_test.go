package syncing

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCompositePostSyncProcessorRunsBestEffortAndReturnsPrimaryError(t *testing.T) {
	t.Parallel()

	primaryErr := errors.New("incident failed")
	primary := &fakePostSyncProcessor{err: primaryErr}
	bestEffort := &fakePostSyncProcessor{}
	processor := NewCompositePostSyncProcessor(primary, bestEffort)

	batch := Batch{MonitoringInstanceID: "mi_001"}
	result := Result{AcceptedAt: time.Date(2026, time.April, 24, 9, 0, 0, 0, time.UTC)}
	err := processor.AfterSuccessfulSync(context.Background(), batch, result)

	if !errors.Is(err, primaryErr) {
		t.Fatalf("AfterSuccessfulSync() error = %v, want primary error", err)
	}
	if primary.calls != 1 {
		t.Fatalf("primary calls = %d, want 1", primary.calls)
	}
	if bestEffort.calls != 1 {
		t.Fatalf("bestEffort calls = %d, want 1", bestEffort.calls)
	}
}

func TestCompositePostSyncProcessorIgnoresBestEffortErrors(t *testing.T) {
	t.Parallel()

	bestEffort := &fakePostSyncProcessor{err: errors.New("stream failed")}
	processor := NewCompositePostSyncProcessor(nil, bestEffort)

	if err := processor.AfterSuccessfulSync(context.Background(), Batch{}, Result{}); err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v, want nil", err)
	}
	if bestEffort.calls != 1 {
		t.Fatalf("bestEffort calls = %d, want 1", bestEffort.calls)
	}
}

type fakePostSyncProcessor struct {
	calls int
	err   error
}

func (f *fakePostSyncProcessor) AfterSuccessfulSync(context.Context, Batch, Result) error {
	f.calls++
	return f.err
}
