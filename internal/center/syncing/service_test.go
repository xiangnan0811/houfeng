package syncing

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestMaxBatchItemsMatchesAgentIngressContract(t *testing.T) {
	t.Parallel()

	if MaxBatchItems != 256 {
		t.Fatalf("MaxBatchItems = %d, want existing agent ingress limit 256", MaxBatchItems)
	}
}

func TestServiceExactDuplicateDispositionSkipsPostSyncAndReturnsOriginalResult(t *testing.T) {
	t.Parallel()

	want := Result{
		Disposition: ResultDispositionExactDuplicate,
		AcceptedAt:  time.Date(2026, time.August, 30, 9, 0, 0, 0, time.UTC),
	}
	repo := &fakeSyncRepository{result: want}
	postSync := &fakePostSyncProcessor{}
	service := NewService(repo, postSync)

	got, err := service.SyncBatch(context.Background(), Batch{MonitoringInstanceID: "mi_001"})
	if err != nil {
		t.Fatalf("SyncBatch() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SyncBatch() result = %#v, want original repository result", got)
	}
	if postSync.calls != 0 {
		t.Fatalf("postSync calls = %d, want 0 for exact duplicate", postSync.calls)
	}
}

func TestServiceNonDuplicateDispositionRunsPostSync(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		disposition ResultDisposition
	}{
		{name: "recorded", disposition: ResultDispositionRecorded},
		{name: "suppressed", disposition: ResultDispositionSuppressed},
		{name: "legacy zero", disposition: ResultDisposition("")},
		{name: "unknown", disposition: ResultDisposition("future_value")},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			want := Result{
				Disposition: tt.disposition,
				AcceptedAt:  time.Date(2026, time.August, 30, 9, 1, 0, 0, time.UTC),
			}
			repo := &fakeSyncRepository{result: want}
			postSync := &fakePostSyncProcessor{}
			service := NewService(repo, postSync)

			got, err := service.SyncBatch(context.Background(), Batch{MonitoringInstanceID: "mi_001"})
			if err != nil {
				t.Fatalf("SyncBatch() error = %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("SyncBatch() result = %#v, want original repository result", got)
			}
			if postSync.calls != 1 {
				t.Fatalf("postSync calls = %d, want 1", postSync.calls)
			}
		})
	}
}

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

type fakeSyncRepository struct {
	result Result
	err    error
}

func (f *fakeSyncRepository) ApplyBatch(context.Context, Batch) (Result, error) {
	return f.result, f.err
}
