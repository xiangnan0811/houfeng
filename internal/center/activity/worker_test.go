package activity

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type fakeLeases struct {
	mu       sync.Mutex
	held     map[SourceKind]string
	acquired []SourceKind
	released []SourceKind
	refuse   map[SourceKind]bool
	failWith error
}

func newFakeLeases() *fakeLeases {
	return &fakeLeases{held: map[SourceKind]string{}, refuse: map[SourceKind]bool{}}
}

func (leases *fakeLeases) AcquireSourceLease(
	_ context.Context,
	_ uint64,
	kind SourceKind,
	ownerID string,
	_ time.Duration,
) (bool, error) {
	leases.mu.Lock()
	defer leases.mu.Unlock()
	if leases.failWith != nil {
		return false, leases.failWith
	}
	if leases.refuse[kind] {
		return false, nil
	}
	leases.acquired = append(leases.acquired, kind)
	leases.held[kind] = ownerID
	return true, nil
}

func (leases *fakeLeases) ReleaseSourceLease(
	_ context.Context,
	_ uint64,
	kind SourceKind,
	_ string,
) error {
	leases.mu.Lock()
	defer leases.mu.Unlock()
	leases.released = append(leases.released, kind)
	delete(leases.held, kind)
	return nil
}

func (leases *fakeLeases) snapshot() ([]SourceKind, []SourceKind, map[SourceKind]string) {
	leases.mu.Lock()
	defer leases.mu.Unlock()
	acquired := append([]SourceKind(nil), leases.acquired...)
	released := append([]SourceKind(nil), leases.released...)
	held := map[SourceKind]string{}
	for kind, owner := range leases.held {
		held[kind] = owner
	}
	return acquired, released, held
}

type fakeGenerations struct {
	generation uint64
	err        error
	calls      int
}

func (generations *fakeGenerations) ActiveGeneration(context.Context) (uint64, error) {
	generations.calls++
	return generations.generation, generations.err
}

func newWorkerHarness(t *testing.T, adapters ...SourceAdapter) (*Worker, *fakeLeases, *fakePublisher) {
	t.Helper()
	checkpoints := newFakeCheckpoints()
	publisher := &fakePublisher{}
	projector, err := NewProjector(ProjectorOptions{
		Namespace:   testNamespace(),
		Adapters:    adapters,
		Checkpoints: checkpoints,
		Publisher:   publisher,
		BatchSize:   10,
	})
	if err != nil {
		t.Fatalf("new projector: %v", err)
	}
	leases := newFakeLeases()
	worker, err := NewWorker(WorkerOptions{
		Projector:   projector,
		Leases:      leases,
		Generations: &fakeGenerations{generation: 1},
		OwnerID:     "activity-worker-1",
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Interval:    time.Second,
		LeaseTTL:    time.Minute,
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}
	return worker, leases, publisher
}

func staticAdapter(t *testing.T, kind SourceKind, eventID string) *fakeAdapter {
	t.Helper()
	head := NewSettledSourceHead(kind, time.Now().UTC().Add(-time.Minute), 9000)
	return &fakeAdapter{
		kind: kind,
		head: func() (SourceHead, error) { return head, nil },
		scan: func(window ScanWindow, _ int) ([]CandidateEvent, error) {
			if window.From.IsZero() {
				return []CandidateEvent{testCandidate(t, kind, eventID, head.RecordedThrough.Add(-time.Hour))}, nil
			}
			return nil, nil
		},
	}
}

// A worker must hold a source while it projects it and hand it back afterwards.
// Keeping it would park the source until the lease expired.
func TestWorkerTakesAndReturnsALeasePerSource(t *testing.T) {
	worker, leases, publisher := newWorkerHarness(t,
		staticAdapter(t, SourceKindRecordDomain, "rac_w1"),
		staticAdapter(t, SourceKindEvidenceSnapshot, "evs_w1"),
	)

	worker.runPass(context.Background())

	acquired, released, held := leases.snapshot()
	if len(acquired) != 2 || len(released) != 2 {
		t.Fatalf("acquired %v, released %v, want one of each per source", acquired, released)
	}
	if len(held) != 0 {
		t.Fatalf("still holding %v after the pass", held)
	}
	// Order matters only in that it matches the projector's, so a source is never
	// skipped because the worker iterated differently.
	expected := worker.projector.SourceKinds()
	for index, kind := range expected {
		if acquired[index] != kind {
			t.Fatalf("acquired in %v, want the projector's order %v", acquired, expected)
		}
	}
	if len(publisher.batches) != 2 {
		t.Fatalf("published %d batches, want one per source", len(publisher.batches))
	}
}

// A source another worker holds must be left alone, and must not stop this
// worker from doing the sources it can take.
func TestWorkerSkipsASourceAnotherWorkerHolds(t *testing.T) {
	worker, leases, publisher := newWorkerHarness(t,
		staticAdapter(t, SourceKindRecordDomain, "rac_w2"),
		staticAdapter(t, SourceKindEvidenceSnapshot, "evs_w2"),
	)
	leases.refuse[SourceKindRecordDomain] = true

	worker.runPass(context.Background())

	acquired, _, _ := leases.snapshot()
	if len(acquired) != 1 || acquired[0] != SourceKindEvidenceSnapshot {
		t.Fatalf("acquired %v, want only the unheld source", acquired)
	}
	if len(publisher.batches) != 1 {
		t.Fatalf("published %d batches, want only the source it owns", len(publisher.batches))
	}
}

// A source that failed must be released, so the next worker to come along can
// retry it instead of waiting out the lease.
func TestWorkerReleasesALeaseAfterAFailedSource(t *testing.T) {
	failing := &fakeAdapter{
		kind: SourceKindRecordDomain,
		head: func() (SourceHead, error) { return SourceHead{}, errors.New("source unreachable") },
	}
	worker, leases, _ := newWorkerHarness(t, failing)

	worker.runPass(context.Background())

	acquired, released, held := leases.snapshot()
	if len(acquired) != 1 || len(released) != 1 {
		t.Fatalf("acquired %v released %v, want the failed source released", acquired, released)
	}
	if len(held) != 0 {
		t.Fatalf("a failed source must not stay held: %v", held)
	}
}

// Nothing has been published yet, so there is no generation to write into.
// Waiting is honest; picking one would fill a projection nobody reads.
func TestWorkerWaitsWhenNoGenerationIsActive(t *testing.T) {
	checkpoints := newFakeCheckpoints()
	publisher := &fakePublisher{}
	projector, err := NewProjector(ProjectorOptions{
		Namespace:   testNamespace(),
		Adapters:    []SourceAdapter{staticAdapter(t, SourceKindRecordDomain, "rac_w3")},
		Checkpoints: checkpoints,
		Publisher:   publisher,
		BatchSize:   10,
	})
	if err != nil {
		t.Fatalf("new projector: %v", err)
	}
	leases := newFakeLeases()
	worker, err := NewWorker(WorkerOptions{
		Projector:   projector,
		Leases:      leases,
		Generations: &fakeGenerations{generation: 0},
		OwnerID:     "activity-worker-1",
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Interval:    time.Second,
		LeaseTTL:    time.Minute,
	})
	if err != nil {
		t.Fatalf("new worker: %v", err)
	}

	worker.runPass(context.Background())

	acquired, _, _ := leases.snapshot()
	if len(acquired) != 0 {
		t.Fatalf("acquired %v with no active generation", acquired)
	}
	if len(publisher.batches) != 0 {
		t.Fatalf("published %d batches with no active generation", len(publisher.batches))
	}
}

// A source that cannot be read is a reason to fall behind, not to take the
// center down, so Run keeps ticking and returns cleanly on cancellation.
func TestWorkerRunSurvivesAFailingSourceAndStopsOnCancel(t *testing.T) {
	failing := &fakeAdapter{
		kind: SourceKindRecordDomain,
		head: func() (SourceHead, error) { return SourceHead{}, errors.New("source unreachable") },
	}
	worker, _, _ := newWorkerHarness(t, failing)
	worker.interval = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- worker.Run(ctx) }()

	time.Sleep(30 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil on cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
}

func TestNewWorkerRejectsUnusableConfiguration(t *testing.T) {
	checkpoints := newFakeCheckpoints()
	publisher := &fakePublisher{}
	projector, err := NewProjector(ProjectorOptions{
		Namespace:   testNamespace(),
		Adapters:    []SourceAdapter{staticAdapter(t, SourceKindRecordDomain, "rac_w4")},
		Checkpoints: checkpoints,
		Publisher:   publisher,
		BatchSize:   10,
	})
	if err != nil {
		t.Fatalf("new projector: %v", err)
	}
	valid := WorkerOptions{
		Projector:   projector,
		Leases:      newFakeLeases(),
		Generations: &fakeGenerations{generation: 1},
		OwnerID:     "activity-worker-1",
		Interval:    time.Second,
		LeaseTTL:    time.Minute,
	}

	for name, mutate := range map[string]func(*WorkerOptions){
		"no projector":     func(options *WorkerOptions) { options.Projector = nil },
		"no leases":        func(options *WorkerOptions) { options.Leases = nil },
		"no generations":   func(options *WorkerOptions) { options.Generations = nil },
		"no owner":         func(options *WorkerOptions) { options.OwnerID = "" },
		"unusable owner":   func(options *WorkerOptions) { options.OwnerID = "Owner With Spaces" },
		"lease under tick": func(options *WorkerOptions) { options.LeaseTTL = time.Millisecond },
	} {
		t.Run(name, func(t *testing.T) {
			options := valid
			mutate(&options)
			if _, err := NewWorker(options); err == nil {
				t.Fatalf("NewWorker accepted %s", name)
			}
		})
	}
}

// A lease shorter than the tick would routinely expire between passes and let
// another worker take a source this one is about to work on again.
func TestNewWorkerRefusesALeaseThatCannotOutlastAPass(t *testing.T) {
	checkpoints := newFakeCheckpoints()
	projector, err := NewProjector(ProjectorOptions{
		Namespace:   testNamespace(),
		Adapters:    []SourceAdapter{staticAdapter(t, SourceKindRecordDomain, "rac_w5")},
		Checkpoints: checkpoints,
		Publisher:   &fakePublisher{},
		BatchSize:   10,
	})
	if err != nil {
		t.Fatalf("new projector: %v", err)
	}
	if _, err := NewWorker(WorkerOptions{
		Projector:   projector,
		Leases:      newFakeLeases(),
		Generations: &fakeGenerations{generation: 1},
		OwnerID:     "activity-worker-1",
		Interval:    time.Minute,
		LeaseTTL:    time.Minute,
	}); err == nil {
		t.Fatal("a lease exactly as long as the interval leaves no margin and must be rejected")
	}
}

func TestValidOwnerIDMatchesTheLeaseColumnShape(t *testing.T) {
	for _, valid := range []string{"a", "activity-worker-1", "rso_0123456789abcdef"} {
		if !ValidOwnerID(valid) {
			t.Errorf("ValidOwnerID(%q) = false", valid)
		}
	}
	for _, invalid := range []string{"", "Owner", "owner id", "owner.id", "owner/id"} {
		if ValidOwnerID(invalid) {
			t.Errorf("ValidOwnerID(%q) = true", invalid)
		}
	}
}
