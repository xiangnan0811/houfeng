package activity

import (
	"context"
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/records"
)

// fakeAdapter scripts one source. Windows are recorded so a test can assert what
// the projector asked for, which is the only way to prove it never advanced past
// ground it did not read.
type fakeAdapter struct {
	kind    SourceKind
	head    func() (SourceHead, error)
	scan    func(window ScanWindow, limit int) ([]CandidateEvent, error)
	windows []ScanWindow
}

func (adapter *fakeAdapter) Kind() SourceKind { return adapter.kind }

func (adapter *fakeAdapter) IncrementalHead(context.Context) (SourceHead, error) {
	if adapter.head == nil {
		return NewIncrementalSourceHead(adapter.kind, time.Now(), DefaultSourceSafetyLag), nil
	}
	return adapter.head()
}

func (adapter *fakeAdapter) ScanAfter(_ context.Context, window ScanWindow, limit int) ([]CandidateEvent, error) {
	adapter.windows = append(adapter.windows, window)
	if adapter.scan == nil {
		return nil, nil
	}
	return adapter.scan(window, limit)
}

type fakeCheckpoints struct {
	stored  map[SourceKind]SourceCheckpoint
	saved   []SourceCheckpoint
	loadErr error
	saveErr error
}

func newFakeCheckpoints() *fakeCheckpoints {
	return &fakeCheckpoints{stored: map[SourceKind]SourceCheckpoint{}}
}

func (store *fakeCheckpoints) LoadCheckpoint(_ context.Context, _ uint64, kind SourceKind) (SourceCheckpoint, error) {
	if store.loadErr != nil {
		return SourceCheckpoint{}, store.loadErr
	}
	checkpoint, ok := store.stored[kind]
	if !ok {
		return SourceCheckpoint{Kind: kind}, nil
	}
	return checkpoint, nil
}

func (store *fakeCheckpoints) SaveCheckpoint(_ context.Context, _ uint64, checkpoint SourceCheckpoint) error {
	if store.saveErr != nil {
		return store.saveErr
	}
	store.saved = append(store.saved, checkpoint)
	store.stored[checkpoint.Kind] = checkpoint
	return nil
}

type fakePublisher struct {
	batches [][]CandidateEvent
	err     error
}

func (publisher *fakePublisher) PublishBatch(_ context.Context, _ uint64, candidates []CandidateEvent) (PublishOutcome, error) {
	if publisher.err != nil {
		return PublishOutcome{}, publisher.err
	}
	batch := make([]CandidateEvent, len(candidates))
	copy(batch, candidates)
	publisher.batches = append(publisher.batches, batch)
	return PublishOutcome{Inserted: len(candidates)}, nil
}

func (publisher *fakePublisher) published() []CandidateEvent {
	all := make([]CandidateEvent, 0)
	for _, batch := range publisher.batches {
		all = append(all, batch...)
	}
	return all
}

func testCandidate(t *testing.T, kind SourceKind, eventID string, recordedAt time.Time) CandidateEvent {
	t.Helper()
	source := SourceIdentity{Kind: kind, EventID: eventID, Version: 1}
	activityID, err := NewActivityID(testNamespace(), source, EventKindRecordCreated)
	if err != nil {
		t.Fatalf("derive activity id: %v", err)
	}
	candidate := CandidateEvent{
		ActivityID: activityID,
		Source:     source,
		EventKind:  EventKindRecordCreated,
		EventAt:    recordedAt.UTC(),
		RecordedAt: recordedAt.UTC(),
		Severity:   "info",
		Presentation: Presentation{
			Version: PresentationVersionV1,
			Title:   "记录已创建",
		},
		Subjects: []SubjectSnapshot{{
			Kind:     records.SubjectKindVPS,
			SourceID: testVPSSourceID,
			Role:     records.RelationRoleAffected,
			Primary:  true,
			Identity: map[string]string{"display_name": "hk-edge-01"},
		}},
	}
	candidate.CanonicalHash = candidate.ComputeCanonicalHash()
	return candidate
}

type projectorHarness struct {
	projector   *Projector
	adapter     *fakeAdapter
	checkpoints *fakeCheckpoints
	publisher   *fakePublisher
}

func newProjectorHarness(t *testing.T, adapter *fakeAdapter, batchSize int) projectorHarness {
	t.Helper()
	checkpoints := newFakeCheckpoints()
	publisher := &fakePublisher{}
	projector, err := NewProjector(ProjectorOptions{
		Namespace:          testNamespace(),
		Adapters:           []SourceAdapter{adapter},
		Checkpoints:        checkpoints,
		Publisher:          publisher,
		BatchSize:          batchSize,
		ReprojectionWindow: DefaultReprojectionWindow,
	})
	if err != nil {
		t.Fatalf("new projector: %v", err)
	}
	return projectorHarness{projector: projector, adapter: adapter, checkpoints: checkpoints, publisher: publisher}
}

func (harness projectorHarness) projectOnce(t *testing.T) SourceOutcome {
	t.Helper()
	report, err := harness.projector.ProjectOnce(context.Background(), 1)
	if err != nil {
		t.Fatalf("project once: %v", err)
	}
	outcome, ok := report.Source(harness.adapter.kind)
	if !ok {
		t.Fatalf("report has no outcome for %s", harness.adapter.kind)
	}
	return outcome
}

func TestProjectorAdvancesToTheHeadWhenItReadsTheWholeWindow(t *testing.T) {
	head := NewSettledSourceHead(SourceKindRecordDomain, time.Now().UTC().Add(-time.Minute), 9000)
	adapter := &fakeAdapter{
		kind: SourceKindRecordDomain,
		head: func() (SourceHead, error) { return head, nil },
		scan: func(window ScanWindow, _ int) ([]CandidateEvent, error) {
			return []CandidateEvent{testCandidate(t, SourceKindRecordDomain, "rac_a1", head.RecordedThrough.Add(-time.Hour))}, nil
		},
	}
	harness := newProjectorHarness(t, adapter, 10)

	outcome := harness.projectOnce(t)

	if outcome.Err != nil {
		t.Fatalf("unexpected outcome error: %v", outcome.Err)
	}
	if !outcome.CaughtUp {
		t.Fatalf("a fully read window must leave the source caught up")
	}
	if !outcome.CheckpointThrough.Equal(head.RecordedThrough) {
		t.Fatalf("checkpoint = %s, want head %s", outcome.CheckpointThrough, head.RecordedThrough)
	}
	if outcome.Inserted != 1 {
		t.Fatalf("inserted = %d, want 1", outcome.Inserted)
	}
}

// A page that came back full means the window may hold more rows. Advancing to
// the head here would mark ground as read that was never scanned, and every row
// left behind would be lost until a rebuild.
func TestProjectorTruncatedPageStopsAtTheLastRowItActuallyRead(t *testing.T) {
	head := NewIncrementalSourceHead(SourceKindRecordDomain, time.Now().UTC(), DefaultSourceSafetyLag)
	first := head.RecordedThrough.Add(-2 * time.Hour)
	second := head.RecordedThrough.Add(-90 * time.Minute)
	pages := 0
	adapter := &fakeAdapter{
		kind: SourceKindRecordDomain,
		head: func() (SourceHead, error) { return head, nil },
		scan: func(window ScanWindow, limit int) ([]CandidateEvent, error) {
			pages++
			if pages > 1 {
				// Second page is empty so the drain stops; only the first page's
				// rows were ever read.
				return nil, nil
			}
			return []CandidateEvent{
				testCandidate(t, SourceKindRecordDomain, "rac_a1", first),
				testCandidate(t, SourceKindRecordDomain, "rac_a2", second),
			}, nil
		},
	}
	harness := newProjectorHarness(t, adapter, 2)

	outcome := harness.projectOnce(t)

	if outcome.Err != nil {
		t.Fatalf("unexpected outcome error: %v", outcome.Err)
	}
	if len(harness.adapter.windows) < 2 {
		t.Fatalf("a full page must be followed by another read, got %d", len(harness.adapter.windows))
	}
	if !harness.adapter.windows[1].From.Equal(second) {
		t.Fatalf("second read started at %s, want the last row read %s", harness.adapter.windows[1].From, second)
	}
}

func TestProjectorTruncatedPageBoundedByPagesDoesNotClaimTheHead(t *testing.T) {
	head := NewIncrementalSourceHead(SourceKindRecordDomain, time.Now().UTC(), DefaultSourceSafetyLag)
	base := head.RecordedThrough.Add(-10 * time.Hour)
	page := 0
	adapter := &fakeAdapter{
		kind: SourceKindRecordDomain,
		head: func() (SourceHead, error) { return head, nil },
		scan: func(window ScanWindow, limit int) ([]CandidateEvent, error) {
			// Always full: the source holds more than one pass can drain.
			page++
			return []CandidateEvent{
				testCandidate(t, SourceKindRecordDomain, sequentialEventID(page*2-1), base.Add(time.Duration(page*2-1)*time.Minute)),
				testCandidate(t, SourceKindRecordDomain, sequentialEventID(page*2), base.Add(time.Duration(page*2)*time.Minute)),
			}, nil
		},
	}
	harness := newProjectorHarness(t, adapter, 2)

	outcome := harness.projectOnce(t)

	if outcome.Err != nil {
		t.Fatalf("unexpected outcome error: %v", outcome.Err)
	}
	if outcome.CaughtUp {
		t.Fatalf("a source still holding unread rows must not report caught up")
	}
	if !outcome.CheckpointThrough.Before(head.RecordedThrough) {
		t.Fatalf("checkpoint %s must stay behind the head %s", outcome.CheckpointThrough, head.RecordedThrough)
	}
}

// The trailing re-scan is the whole reason a late commit is not lost. None of the
// five sources orders rows by commit, so a row can appear below the checkpoint
// after the forward scan has already moved past it.
func TestProjectorRereadsTheTrailingWindowSoALateCommitIsStillProjected(t *testing.T) {
	now := time.Now().UTC()
	head := NewIncrementalSourceHead(SourceKindRecordDomain, now, DefaultSourceSafetyLag)
	checkpointAt := head.RecordedThrough.Add(-time.Minute)
	lateRow := testCandidate(t, SourceKindRecordDomain, "rac_late", checkpointAt.Add(-2*time.Minute))

	adapter := &fakeAdapter{
		kind: SourceKindRecordDomain,
		head: func() (SourceHead, error) { return head, nil },
		scan: func(window ScanWindow, _ int) ([]CandidateEvent, error) {
			// Only the backward window can see it: it committed under the old
			// watermark and its recorded time is below the checkpoint.
			if !window.From.After(lateRow.RecordedAt) && !window.Through.Before(lateRow.RecordedAt) {
				return []CandidateEvent{lateRow}, nil
			}
			return nil, nil
		},
	}
	harness := newProjectorHarness(t, adapter, 10)
	harness.checkpoints.stored[SourceKindRecordDomain] = SourceCheckpoint{
		Kind:            SourceKindRecordDomain,
		RecordedThrough: checkpointAt,
		CaughtUp:        true,
	}

	outcome := harness.projectOnce(t)

	if outcome.Err != nil {
		t.Fatalf("unexpected outcome error: %v", outcome.Err)
	}
	published := harness.publisher.published()
	if len(published) != 1 || published[0].ActivityID != lateRow.ActivityID {
		t.Fatalf("late row was not projected, published %d rows", len(published))
	}
}

func TestProjectorLeavesLatenessExactlyAsTheSourceDeclaredIt(t *testing.T) {
	head := NewIncrementalSourceHead(SourceKindMonitoringEvent, time.Now().UTC(), DefaultSourceSafetyLag)
	late := testCandidate(t, SourceKindMonitoringEvent, "mev_1", head.RecordedThrough.Add(-time.Hour))
	late.Backfilled = true
	late.CanonicalHash = late.ComputeCanonicalHash()

	adapter := &fakeAdapter{
		kind: SourceKindMonitoringEvent,
		head: func() (SourceHead, error) { return head, nil },
		scan: func(ScanWindow, int) ([]CandidateEvent, error) { return []CandidateEvent{late}, nil },
	}
	harness := newProjectorHarness(t, adapter, 10)

	if outcome := harness.projectOnce(t); outcome.Err != nil {
		t.Fatalf("unexpected outcome error: %v", outcome.Err)
	}
	published := harness.publisher.published()
	if len(published) != 1 {
		t.Fatalf("published %d rows, want 1", len(published))
	}
	if !published[0].Backfilled {
		t.Fatalf("projector must not clear a source-declared backfill flag")
	}
	if !published[0].EventAt.Equal(late.EventAt) {
		t.Fatalf("event time = %s, want the source's %s", published[0].EventAt, late.EventAt)
	}
}

func TestProjectorKeepsTheCheckpointWhenWorkFails(t *testing.T) {
	head := NewIncrementalSourceHead(SourceKindRecordDomain, time.Now().UTC(), DefaultSourceSafetyLag)
	checkpointAt := head.RecordedThrough.Add(-time.Hour)
	scanFailure := errors.New("source unavailable")

	for name, test := range map[string]struct {
		scanErr    error
		publishErr error
	}{
		"scan fails":    {scanErr: scanFailure},
		"publish fails": {publishErr: errors.New("publish rejected")},
	} {
		t.Run(name, func(t *testing.T) {
			adapter := &fakeAdapter{
				kind: SourceKindRecordDomain,
				head: func() (SourceHead, error) { return head, nil },
				scan: func(ScanWindow, int) ([]CandidateEvent, error) {
					if test.scanErr != nil {
						return nil, test.scanErr
					}
					return []CandidateEvent{testCandidate(t, SourceKindRecordDomain, "rac_a1", checkpointAt.Add(time.Minute))}, nil
				},
			}
			harness := newProjectorHarness(t, adapter, 10)
			harness.publisher.err = test.publishErr
			harness.checkpoints.stored[SourceKindRecordDomain] = SourceCheckpoint{
				Kind:            SourceKindRecordDomain,
				RecordedThrough: checkpointAt,
				CaughtUp:        true,
			}

			outcome := harness.projectOnce(t)

			if outcome.Err == nil {
				t.Fatalf("a failed pass must report an error")
			}
			if !outcome.CheckpointThrough.Equal(checkpointAt) {
				t.Fatalf("checkpoint moved to %s during a failure, want %s", outcome.CheckpointThrough, checkpointAt)
			}
			if outcome.CaughtUp {
				t.Fatalf("a failed pass must not report caught up")
			}
			saved := harness.checkpoints.stored[SourceKindRecordDomain]
			if saved.Attempt == 0 || saved.LastErrorCode == "" {
				t.Fatalf("a failure must be recorded on the checkpoint, got %+v", saved)
			}
			if !saved.RecordedThrough.Equal(checkpointAt) {
				t.Fatalf("persisted checkpoint moved to %s, want %s", saved.RecordedThrough, checkpointAt)
			}
		})
	}
}

func TestProjectorRefusesAHeadThatMovedBackwards(t *testing.T) {
	now := time.Now().UTC()
	checkpointAt := now.Add(-time.Hour)
	adapter := &fakeAdapter{
		kind: SourceKindRecordDomain,
		head: func() (SourceHead, error) {
			return NewSettledSourceHead(SourceKindRecordDomain, checkpointAt.Add(-time.Minute), 10), nil
		},
		scan: func(ScanWindow, int) ([]CandidateEvent, error) {
			t.Fatalf("a rejected head must not be scanned")
			return nil, nil
		},
	}
	harness := newProjectorHarness(t, adapter, 10)
	harness.checkpoints.stored[SourceKindRecordDomain] = SourceCheckpoint{
		Kind:            SourceKindRecordDomain,
		RecordedThrough: checkpointAt,
	}

	outcome := harness.projectOnce(t)

	if !errors.Is(outcome.Err, ErrInvalidSourceHead) {
		t.Fatalf("error = %v, want ErrInvalidSourceHead", outcome.Err)
	}
	if !outcome.CheckpointThrough.Equal(checkpointAt) {
		t.Fatalf("checkpoint moved to %s, want %s", outcome.CheckpointThrough, checkpointAt)
	}
}

// Adapter output is not trusted. Each case below is a way one bad adapter could
// corrupt the projection for everyone else, so the pass must fail before
// publishing and must leave the checkpoint where it was.
func TestProjectorRejectsUntrustworthyCandidates(t *testing.T) {
	head := NewIncrementalSourceHead(SourceKindRecordDomain, time.Now().UTC(), DefaultSourceSafetyLag)
	recordedAt := head.RecordedThrough.Add(-time.Hour)

	for name, test := range map[string]struct {
		mutate func(*testing.T, *CandidateEvent)
		want   error
	}{
		"recorded after the window it was asked for": {
			mutate: func(t *testing.T, candidate *CandidateEvent) {
				candidate.RecordedAt = head.RecordedThrough.Add(time.Minute)
				candidate.CanonicalHash = candidate.ComputeCanonicalHash()
			},
			want: ErrCandidateOutsideWindow,
		},
		"minted under another source's kind": {
			mutate: func(t *testing.T, candidate *CandidateEvent) {
				candidate.Source.Kind = SourceKindCommandAudit
				candidate.CanonicalHash = candidate.ComputeCanonicalHash()
			},
			want: ErrForeignSourceCandidate,
		},
		"id not derived from its source identity": {
			mutate: func(t *testing.T, candidate *CandidateEvent) {
				candidate.ActivityID = "act_deadbeefdeadbeef"
				candidate.CanonicalHash = candidate.ComputeCanonicalHash()
			},
			want: ErrUndeterminedActivityID,
		},
		"hash does not cover its own content": {
			mutate: func(t *testing.T, candidate *CandidateEvent) {
				candidate.Presentation.Title = "改写后的标题"
			},
			want: ErrCandidateHashMismatch,
		},
		"no subject to reach it by": {
			mutate: func(t *testing.T, candidate *CandidateEvent) {
				candidate.Subjects = nil
				candidate.CanonicalHash = candidate.ComputeCanonicalHash()
			},
			want: ErrUnreachableCandidate,
		},
		"subject id no live route could resolve": {
			mutate: func(t *testing.T, candidate *CandidateEvent) {
				candidate.Subjects[0].SourceID = "not-a-vps-id"
				candidate.CanonicalHash = candidate.ComputeCanonicalHash()
			},
			want: ErrUnreachableCandidate,
		},
		"unregistered presentation version": {
			mutate: func(t *testing.T, candidate *CandidateEvent) {
				candidate.Presentation.Version = 99
				candidate.CanonicalHash = candidate.ComputeCanonicalHash()
			},
			want: ErrInvalidPresentation,
		},
		"source identity without a version": {
			mutate: func(t *testing.T, candidate *CandidateEvent) {
				candidate.Source.Version = 0
				candidate.CanonicalHash = candidate.ComputeCanonicalHash()
			},
			want: ErrInvalidSourceIdentity,
		},
		// A currency claim decides what versions=current answers at a watermark, so
		// an unidentifiable one must not reach publication.
		"claims a revision it does not name": {
			mutate: func(t *testing.T, candidate *CandidateEvent) {
				candidate.OpensRevision = true
				candidate.RecordID = "rec_9a1b2c"
				candidate.RevisionNo = 2
				candidate.CanonicalHash = candidate.ComputeCanonicalHash()
			},
			want: ErrIncompleteRevisionClaim,
		},
		"claims a revision with no order": {
			mutate: func(t *testing.T, candidate *CandidateEvent) {
				candidate.OpensRevision = true
				candidate.RecordID = "rec_9a1b2c"
				candidate.RevisionID = "rrv_5d6e7f"
				candidate.CanonicalHash = candidate.ComputeCanonicalHash()
			},
			want: ErrIncompleteRevisionClaim,
		},
		"claims a revision for no record": {
			mutate: func(t *testing.T, candidate *CandidateEvent) {
				candidate.OpensRevision = true
				candidate.RevisionID = "rrv_5d6e7f"
				candidate.RevisionNo = 2
				candidate.CanonicalHash = candidate.ComputeCanonicalHash()
			},
			want: ErrIncompleteRevisionClaim,
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := testCandidate(t, SourceKindRecordDomain, "rac_a1", recordedAt)
			test.mutate(t, &candidate)
			adapter := &fakeAdapter{
				kind: SourceKindRecordDomain,
				head: func() (SourceHead, error) { return head, nil },
				scan: func(ScanWindow, int) ([]CandidateEvent, error) {
					return []CandidateEvent{candidate}, nil
				},
			}
			harness := newProjectorHarness(t, adapter, 10)

			outcome := harness.projectOnce(t)

			if !errors.Is(outcome.Err, test.want) {
				t.Fatalf("error = %v, want %v", outcome.Err, test.want)
			}
			if len(harness.publisher.batches) != 0 {
				t.Fatalf("a rejected batch must not be published")
			}
			if !outcome.CheckpointThrough.IsZero() {
				t.Fatalf("checkpoint advanced to %s after a rejected batch", outcome.CheckpointThrough)
			}
		})
	}
}

// A full page of rows that share one recorded_at must still advance via the
// exclusive AfterEventID keyset. Stalling forever (or skipping the rest) would
// leave the projection permanently incomplete.
func TestProjectorPagesPastAFullPageOfSameInstantRows(t *testing.T) {
	head := NewIncrementalSourceHead(SourceKindRecordDomain, time.Now().UTC(), DefaultSourceSafetyLag)
	sameInstant := head.RecordedThrough.Add(-time.Hour)
	ids := []string{"rac_a1", "rac_a2", "rac_a3", "rac_a4"}
	reads := 0
	adapter := &fakeAdapter{
		kind: SourceKindRecordDomain,
		head: func() (SourceHead, error) { return head, nil },
		scan: func(window ScanWindow, limit int) ([]CandidateEvent, error) {
			reads++
			out := make([]CandidateEvent, 0, limit)
			for _, id := range ids {
				if !window.From.IsZero() && sameInstant.Before(window.From) {
					continue
				}
				if !window.From.IsZero() && !sameInstant.After(window.From) &&
					window.AfterEventID != "" && id <= window.AfterEventID {
					continue
				}
				if !window.Through.IsZero() && sameInstant.After(window.Through) {
					continue
				}
				out = append(out, testCandidate(t, SourceKindRecordDomain, id, sameInstant))
				if len(out) == limit {
					break
				}
			}
			return out, nil
		},
	}
	harness := newProjectorHarness(t, adapter, 2)

	outcome := harness.projectOnce(t)

	if !outcome.CaughtUp {
		t.Fatalf("same-instant pages must catch up once the keyset drains them")
	}
	if outcome.Stalled {
		t.Fatalf("keyset paging must not report a stall")
	}
	if outcome.Inserted != len(ids) {
		t.Fatalf("inserted %d, want %d", outcome.Inserted, len(ids))
	}
	if reads != 3 {
		t.Fatalf("projector read %d pages, want 2 full pages then an empty one", reads)
	}
}

// A buggy adapter that ignores AfterEventID and returns the same page forever
// must fail closed rather than catch up or spin.
func TestProjectorRejectsAPageThatDoesNotRespectKeyset(t *testing.T) {
	head := NewIncrementalSourceHead(SourceKindRecordDomain, time.Now().UTC(), DefaultSourceSafetyLag)
	sameInstant := head.RecordedThrough.Add(-time.Hour)
	reads := 0
	adapter := &fakeAdapter{
		kind: SourceKindRecordDomain,
		head: func() (SourceHead, error) { return head, nil },
		scan: func(ScanWindow, int) ([]CandidateEvent, error) {
			reads++
			return []CandidateEvent{
				testCandidate(t, SourceKindRecordDomain, "rac_a1", sameInstant),
				testCandidate(t, SourceKindRecordDomain, "rac_a2", sameInstant),
			}, nil
		},
	}
	harness := newProjectorHarness(t, adapter, 2)

	outcome := harness.projectOnce(t)

	if outcome.CaughtUp {
		t.Fatalf("a keyset-violating source must not report caught up")
	}
	if outcome.Err == nil {
		t.Fatalf("outcome must surface the invalid page")
	}
	if !errors.Is(outcome.Err, ErrCandidateOutsideWindow) {
		t.Fatalf("outcome err = %v, want %v", outcome.Err, ErrCandidateOutsideWindow)
	}
	if reads != 2 {
		t.Fatalf("projector read %d pages, want one publish then one rejected re-read", reads)
	}
}

func TestProjectorProjectsEveryOtherSourceWhenOneFails(t *testing.T) {
	head := func(kind SourceKind) SourceHead {
		return NewIncrementalSourceHead(kind, time.Now().UTC(), DefaultSourceSafetyLag)
	}
	broken := &fakeAdapter{
		kind: SourceKindRecordDomain,
		head: func() (SourceHead, error) { return SourceHead{}, errors.New("source down") },
	}
	healthy := &fakeAdapter{
		kind: SourceKindAssetHistory,
		head: func() (SourceHead, error) { return head(SourceKindAssetHistory), nil },
		scan: func(window ScanWindow, _ int) ([]CandidateEvent, error) {
			return []CandidateEvent{testCandidate(t, SourceKindAssetHistory, "ahi_1", window.Through.Add(-time.Hour))}, nil
		},
	}
	checkpoints := newFakeCheckpoints()
	publisher := &fakePublisher{}
	projector, err := NewProjector(ProjectorOptions{
		Namespace:   testNamespace(),
		Adapters:    []SourceAdapter{broken, healthy},
		Checkpoints: checkpoints,
		Publisher:   publisher,
	})
	if err != nil {
		t.Fatalf("new projector: %v", err)
	}

	report, err := projector.ProjectOnce(context.Background(), 1)
	if err != nil {
		t.Fatalf("project once: %v", err)
	}

	if report.Err() == nil {
		t.Fatalf("a pass with a failing source must surface an error")
	}
	assetOutcome, ok := report.Source(SourceKindAssetHistory)
	if !ok || assetOutcome.Err != nil {
		t.Fatalf("healthy source outcome = %+v, want no error", assetOutcome)
	}
	if assetOutcome.Inserted != 1 {
		t.Fatalf("healthy source inserted %d rows, want 1", assetOutcome.Inserted)
	}
}

func TestProjectorVisitsSourcesInAStableOrder(t *testing.T) {
	kinds := []SourceKind{SourceKindMonitoringEvent, SourceKindRecordDomain, SourceKindCommandAudit}
	adapters := make([]SourceAdapter, 0, len(kinds))
	for _, kind := range kinds {
		adapters = append(adapters, &fakeAdapter{kind: kind})
	}
	projector, err := NewProjector(ProjectorOptions{
		Namespace:   testNamespace(),
		Adapters:    adapters,
		Checkpoints: newFakeCheckpoints(),
		Publisher:   &fakePublisher{},
	})
	if err != nil {
		t.Fatalf("new projector: %v", err)
	}

	report, err := projector.ProjectOnce(context.Background(), 1)
	if err != nil {
		t.Fatalf("project once: %v", err)
	}

	visited := make([]SourceKind, 0, len(report.Sources))
	for _, outcome := range report.Sources {
		visited = append(visited, outcome.Kind)
	}
	want := []SourceKind{SourceKindCommandAudit, SourceKindMonitoringEvent, SourceKindRecordDomain}
	for index := range want {
		if visited[index] != want[index] {
			t.Fatalf("visited %v, want %v", visited, want)
		}
	}
}

func TestProjectorSkipsPublishingWhenThereIsNothingToRead(t *testing.T) {
	head := NewSettledSourceHead(SourceKindRecordDomain, time.Now().UTC().Add(-time.Minute), 7)
	adapter := &fakeAdapter{
		kind: SourceKindRecordDomain,
		head: func() (SourceHead, error) { return head, nil },
	}
	harness := newProjectorHarness(t, adapter, 10)

	outcome := harness.projectOnce(t)

	if outcome.Err != nil {
		t.Fatalf("unexpected outcome error: %v", outcome.Err)
	}
	if len(harness.publisher.batches) != 0 {
		t.Fatalf("an empty scan must not take the publication lock")
	}
	if !outcome.CaughtUp || !outcome.CheckpointThrough.Equal(head.RecordedThrough) {
		t.Fatalf("outcome = %+v, want caught up at %s", outcome, head.RecordedThrough)
	}
}

func TestNewProjectorRejectsUnusableConfiguration(t *testing.T) {
	valid := func() ProjectorOptions {
		return ProjectorOptions{
			Namespace:   testNamespace(),
			Adapters:    []SourceAdapter{&fakeAdapter{kind: SourceKindRecordDomain}},
			Checkpoints: newFakeCheckpoints(),
			Publisher:   &fakePublisher{},
		}
	}
	for name, mutate := range map[string]func(*ProjectorOptions){
		"no namespace":   func(options *ProjectorOptions) { options.Namespace = Namespace{} },
		"no adapters":    func(options *ProjectorOptions) { options.Adapters = nil },
		"no checkpoints": func(options *ProjectorOptions) { options.Checkpoints = nil },
		"no publisher":   func(options *ProjectorOptions) { options.Publisher = nil },
		"unknown source kind": func(options *ProjectorOptions) {
			options.Adapters = []SourceAdapter{&fakeAdapter{kind: "ledger"}}
		},
		"two adapters for one source": func(options *ProjectorOptions) {
			options.Adapters = []SourceAdapter{
				&fakeAdapter{kind: SourceKindRecordDomain},
				&fakeAdapter{kind: SourceKindRecordDomain},
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			options := valid()
			mutate(&options)
			if _, err := NewProjector(options); err == nil {
				t.Fatalf("expected %s to be rejected", name)
			}
		})
	}
}

func TestProjectOnceRefusesAnInactiveGeneration(t *testing.T) {
	harness := newProjectorHarness(t, &fakeAdapter{kind: SourceKindRecordDomain}, 10)
	if _, err := harness.projector.ProjectOnce(context.Background(), 0); !errors.Is(err, ErrInactiveGeneration) {
		t.Fatalf("error = %v, want ErrInactiveGeneration", err)
	}
}

func sequentialEventID(index int) string {
	return "rac_seq" + string(rune('a'+index%26)) + string(rune('a'+index/26%26))
}
