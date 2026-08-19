package recordsearch

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestRebuildWorkerDrainsBatchesThenPublishesOnce(t *testing.T) {
	t.Parallel()

	store := &fakeRebuildStore{
		needed: true,
		lease:  RebuildLease{JobID: "rsj_drain", Generation: 4},
		batches: []RebuildBatchResult{
			{Projected: 200, ResumeAfter: "rec_0200"},
			{Projected: 200, ResumeAfter: "rec_0400"},
			{Projected: 17, ResumeAfter: "rec_0417", Drained: true},
		},
		coverage: RebuildCoverage{DocumentCount: 417, CoverageDigest: testRebuildDigest(3)},
	}
	worker, err := NewRebuildWorker(store, RebuildWorkerOptions{
		OwnerID: "record_search_rebuilder", OwnerLeaseDuration: time.Minute,
		BatchSize: 200, PollInterval: time.Second,
	})
	if err != nil {
		t.Fatalf("NewRebuildWorker() error = %v", err)
	}

	rebuilt, err := worker.RunOnce(context.Background())
	if err != nil || !rebuilt {
		t.Fatalf("RunOnce() = %t, %v, want true, nil", rebuilt, err)
	}
	if store.claims != 1 || store.batchCalls != 3 || store.publishes != 1 || store.failures != 0 {
		t.Fatalf("store calls claim=%d batch=%d publish=%d fail=%d",
			store.claims, store.batchCalls, store.publishes, store.failures)
	}
	// Every batch must resume from where the previous one stopped, or the rebuild
	// would either reprocess rows forever or skip a stretch of records.
	wantResume := []string{"", "rec_0200", "rec_0400"}
	if len(store.resumeSeen) != len(wantResume) {
		t.Fatalf("resume checkpoints = %#v, want %#v", store.resumeSeen, wantResume)
	}
	for index, want := range wantResume {
		if store.resumeSeen[index] != want {
			t.Fatalf("resume checkpoint %d = %q, want %q", index, store.resumeSeen[index], want)
		}
	}
	if store.published.Generation != 4 || store.published.JobID != "rsj_drain" ||
		store.published.Projected != 417 {
		t.Fatalf("publish request = %#v", store.published)
	}
}

func TestRebuildWorkerSkipsWorkWhenNoRebuildIsNeeded(t *testing.T) {
	t.Parallel()

	store := &fakeRebuildStore{needed: false}
	worker := mustRebuildWorker(t, store)
	rebuilt, err := worker.RunOnce(context.Background())
	if err != nil || rebuilt {
		t.Fatalf("RunOnce() = %t, %v, want false, nil", rebuilt, err)
	}
	if store.claims != 0 || store.batchCalls != 0 || store.publishes != 0 {
		t.Fatalf("store touched without a pending rebuild: %#v", store)
	}
}

// A batch failure must leave the generation unpublished and the job marked
// failed. Publishing a partially built generation would silently drop records
// from every search that follows.
func TestRebuildWorkerFailsJobWithoutPublishingPartialGeneration(t *testing.T) {
	t.Parallel()

	batchFailure := errors.New("batch read failed")
	store := &fakeRebuildStore{
		needed:  true,
		lease:   RebuildLease{JobID: "rsj_partial", Generation: 9},
		batches: []RebuildBatchResult{{Projected: 200, ResumeAfter: "rec_0200"}},
		batchErr: map[int]error{
			1: batchFailure,
		},
	}
	worker := mustRebuildWorker(t, store)
	if _, err := worker.RunOnce(context.Background()); !errors.Is(err, batchFailure) {
		t.Fatalf("RunOnce() error = %v, want %v", err, batchFailure)
	}
	if store.publishes != 0 || store.failures != 1 {
		t.Fatalf("publish/fail calls = %d/%d, want 0/1", store.publishes, store.failures)
	}
	if store.failed.JobID != "rsj_partial" || store.failed.Generation != 9 || store.failed.Reason == "" {
		t.Fatalf("failure request = %#v", store.failed)
	}
}

// A rebuild that stops making progress would spin forever holding the building
// generation, so a batch that reports neither progress nor completion is a fault.
func TestRebuildWorkerRejectsBatchThatNeitherProgressesNorFinishes(t *testing.T) {
	t.Parallel()

	store := &fakeRebuildStore{
		needed:  true,
		lease:   RebuildLease{JobID: "rsj_stall", Generation: 2},
		batches: []RebuildBatchResult{{Projected: 0, ResumeAfter: ""}},
	}
	worker := mustRebuildWorker(t, store)
	if _, err := worker.RunOnce(context.Background()); !errors.Is(err, ErrRebuildStalled) {
		t.Fatalf("RunOnce() error = %v, want ErrRebuildStalled", err)
	}
	if store.publishes != 0 || store.failures != 1 {
		t.Fatalf("publish/fail calls = %d/%d, want 0/1", store.publishes, store.failures)
	}
}

// A resumed job carries a checkpoint and a running count, so the published
// coverage must include the records an earlier attempt already projected.
func TestRebuildWorkerResumesFromPersistedCheckpoint(t *testing.T) {
	t.Parallel()

	store := &fakeRebuildStore{
		needed:  true,
		lease:   RebuildLease{JobID: "rsj_resume", Generation: 5, ResumeAfter: "rec_0300", Projected: 300},
		batches: []RebuildBatchResult{{Projected: 42, ResumeAfter: "rec_0342", Drained: true}},
	}
	worker := mustRebuildWorker(t, store)
	if _, err := worker.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if len(store.resumeSeen) != 1 || store.resumeSeen[0] != "rec_0300" {
		t.Fatalf("resume checkpoints = %#v, want [rec_0300]", store.resumeSeen)
	}
	if store.published.Projected != 342 {
		t.Fatalf("published projected count = %d, want 342 including the earlier attempt", store.published.Projected)
	}
}

func TestRebuildWorkerRejectsInvalidConstruction(t *testing.T) {
	t.Parallel()

	valid := RebuildWorkerOptions{
		OwnerID: "record_search_rebuilder", OwnerLeaseDuration: time.Minute,
		BatchSize: 200, PollInterval: time.Second,
	}
	var typedNil *fakeRebuildStore
	if _, err := NewRebuildWorker(typedNil, valid); !errors.Is(err, ErrInvalidRebuild) {
		t.Fatalf("NewRebuildWorker(typed nil) error = %v", err)
	}
	for name, mutate := range map[string]func(*RebuildWorkerOptions){
		"owner":     func(options *RebuildWorkerOptions) { options.OwnerID = "" },
		"owner set": func(options *RebuildWorkerOptions) { options.OwnerID = "Record Search" },
		"lease":     func(options *RebuildWorkerOptions) { options.OwnerLeaseDuration = 0 },
		"batch":     func(options *RebuildWorkerOptions) { options.BatchSize = 0 },
		"batch max": func(options *RebuildWorkerOptions) { options.BatchSize = maxRebuildBatchSize + 1 },
		"poll":      func(options *RebuildWorkerOptions) { options.PollInterval = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			options := valid
			mutate(&options)
			if _, err := NewRebuildWorker(&fakeRebuildStore{}, options); !errors.Is(err, ErrInvalidRebuild) {
				t.Fatalf("NewRebuildWorker(%s) error = %v, want ErrInvalidRebuild", name, err)
			}
		})
	}
}

func TestRebuildWorkerRunStopsOnCancelledContext(t *testing.T) {
	t.Parallel()

	worker := mustRebuildWorker(t, &fakeRebuildStore{needed: false})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := worker.Run(ctx); err != nil {
		t.Fatalf("Run(cancelled) error = %v", err)
	}
}

func mustRebuildWorker(t *testing.T, store RebuildStore) *RebuildWorker {
	t.Helper()
	worker, err := NewRebuildWorker(store, RebuildWorkerOptions{
		OwnerID: "record_search_rebuilder", OwnerLeaseDuration: time.Minute,
		BatchSize: 200, PollInterval: time.Second,
	})
	if err != nil {
		t.Fatalf("NewRebuildWorker() error = %v", err)
	}
	return worker
}

type fakeRebuildStore struct {
	needed     bool
	lease      RebuildLease
	batches    []RebuildBatchResult
	batchErr   map[int]error
	coverage   RebuildCoverage
	claims     int
	batchCalls int
	publishes  int
	failures   int
	resumeSeen []string
	published  RebuildPublish
	failed     RebuildFailure
}

func (store *fakeRebuildStore) RecordSearchRebuildNeeded(context.Context) (bool, error) {
	return store.needed, nil
}

func (store *fakeRebuildStore) ClaimRecordSearchRebuild(_ context.Context, _ RebuildClaim) (RebuildLease, error) {
	store.claims++
	return store.lease, nil
}

func (store *fakeRebuildStore) ProjectRecordSearchRebuildBatch(
	_ context.Context,
	batch RebuildBatch,
) (RebuildBatchResult, error) {
	index := store.batchCalls
	store.batchCalls++
	store.resumeSeen = append(store.resumeSeen, batch.ResumeAfter)
	if err, found := store.batchErr[index]; found {
		return RebuildBatchResult{}, err
	}
	if index >= len(store.batches) {
		return RebuildBatchResult{}, fmt.Errorf("unexpected batch call %d", index)
	}
	return store.batches[index], nil
}

func (store *fakeRebuildStore) PublishRecordSearchRebuild(
	_ context.Context,
	publish RebuildPublish,
) (RebuildCoverage, error) {
	store.publishes++
	store.published = publish
	return store.coverage, nil
}

func (store *fakeRebuildStore) FailRecordSearchRebuild(_ context.Context, failure RebuildFailure) error {
	store.failures++
	store.failed = failure
	return nil
}

func testRebuildDigest(value byte) [32]byte {
	var digest [32]byte
	for index := range digest {
		digest[index] = value
	}
	return digest
}
