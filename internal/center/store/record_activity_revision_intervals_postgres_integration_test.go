package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
)

type revisionInterval struct {
	revisionID string
	revisionNo int64
	validFrom  int64
	validTo    *int64
}

func loadRevisionIntervals(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	generation uint64,
	recordID string,
) []revisionInterval {
	t.Helper()
	rows, err := pool.Query(ctx, `
		select revision_id, revision_no, valid_from_ingest_sequence, valid_to_ingest_sequence
		from public.record_activity_revision_intervals
		where projection_generation = $1 and record_id = $2
		order by valid_from_ingest_sequence`,
		generation, recordID,
	)
	if err != nil {
		t.Fatalf("load revision intervals: %v", err)
	}
	defer rows.Close()

	loaded := make([]revisionInterval, 0, 4)
	for rows.Next() {
		var interval revisionInterval
		if err := rows.Scan(
			&interval.revisionID, &interval.revisionNo, &interval.validFrom, &interval.validTo,
		); err != nil {
			t.Fatalf("scan revision interval: %v", err)
		}
		loaded = append(loaded, interval)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("load revision intervals: %v", err)
	}
	return loaded
}

// A page fixed at a watermark has to resolve one current revision and keep
// resolving the same one while the reader pages. That is what these intervals
// are: closed behind the watermark where the next revision arrived, open at the
// head. Half-open is the point — the commit that opens an interval is itself
// visible at the sequence where the previous one closes.
func TestPostgresIntegrationActivityRevisionIntervalsChainAtPublishedSequences(t *testing.T) {
	ctx := context.Background()
	fixture := newActivityRecordFixture(t, ctx)
	pool := fixture.runtime
	results := fixture.commitRevisions(t, ctx, "interval-chain", activityRecordInput(t, "First", ""), activityRecordInput(t, "Second", ""))
	candidates := fixture.revisionCandidates(t, ctx, results)
	recordID := results[0].RecordID
	first, err := publishActivityTestBatch(t, ctx, pool, 1, candidates[:1])
	if err != nil {
		t.Fatalf("publish first revision: %v", err)
	}
	if first.SupersededRevisions != 0 {
		t.Fatalf("superseded = %d, want 0 for a record's first revision", first.SupersededRevisions)
	}

	second, err := publishActivityTestBatch(t, ctx, pool, 1, candidates[1:])
	if err != nil {
		t.Fatalf("publish second revision: %v", err)
	}

	intervals := loadRevisionIntervals(t, ctx, pool, 1, recordID)
	if len(intervals) != 2 {
		t.Fatalf("intervals = %+v, want one per revision", intervals)
	}
	if intervals[0].validTo == nil {
		t.Fatal("the superseded revision must be closed, or two revisions would look current at once")
	}
	if *intervals[0].validTo != int64(second.AssignedFrom) {
		t.Fatalf("closed at %d, want the sequence %d where its successor became current",
			*intervals[0].validTo, second.AssignedFrom)
	}
	if intervals[1].validFrom != int64(second.AssignedFrom) {
		t.Fatalf("opened at %d, want %d", intervals[1].validFrom, second.AssignedFrom)
	}
	if intervals[1].validTo != nil {
		t.Fatal("the newest revision must stay open")
	}
	if intervals[0].validFrom != int64(first.AssignedFrom) {
		t.Fatalf("first opened at %d, want %d", intervals[0].validFrom, first.AssignedFrom)
	}

	// The watermark where one closes and the next opens must resolve to exactly
	// one revision, which is what a half-open interval gives.
	current := currentRevisionAt(t, ctx, pool, 1, recordID, second.AssignedFrom)
	if current != results[1].RevisionID {
		t.Fatalf("current at the handover sequence = %q, want the new revision", current)
	}
	current = currentRevisionAt(t, ctx, pool, 1, recordID, second.AssignedFrom-1)
	if current != results[0].RevisionID {
		t.Fatalf("current just below the handover = %q, want the old revision", current)
	}
}

// currentRevisionAt is the resolution the read path performs: at watermark S, one
// revision, or none.
func currentRevisionAt(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	generation uint64,
	recordID string,
	sequence uint64,
) string {
	t.Helper()
	rows, err := pool.Query(ctx, `
		select revision_id
		from public.record_activity_revision_intervals
		where projection_generation = $1
		  and record_id = $2
		  and valid_from_ingest_sequence <= $3
		  and (valid_to_ingest_sequence is null or valid_to_ingest_sequence > $3)`,
		generation, recordID, int64(sequence),
	)
	if err != nil {
		t.Fatalf("resolve current revision: %v", err)
	}
	defer rows.Close()

	resolved := make([]string, 0, 2)
	for rows.Next() {
		var revisionID string
		if err := rows.Scan(&revisionID); err != nil {
			t.Fatalf("scan current revision: %v", err)
		}
		resolved = append(resolved, revisionID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("resolve current revision: %v", err)
	}
	if len(resolved) > 1 {
		t.Fatalf("watermark %d resolves to %v, and a page cannot show two current revisions", sequence, resolved)
	}
	if len(resolved) == 0 {
		return ""
	}
	return resolved[0]
}

// Several revisions of one record in a single batch must still chain, because a
// batch is numbered in one transaction and every commit inside it still happened
// in an order.
func TestPostgresIntegrationActivityRevisionIntervalsChainWithinOneBatch(t *testing.T) {
	ctx := context.Background()
	fixture := newActivityRecordFixture(t, ctx)
	pool := fixture.runtime
	results := fixture.commitRevisions(t, ctx, "interval-batch",
		activityRecordInput(t, "First", ""), activityRecordInput(t, "Second", ""), activityRecordInput(t, "Third", ""))
	recordID := results[0].RecordID
	result, err := publishActivityTestBatch(t, ctx, pool, 1, fixture.revisionCandidates(t, ctx, results))
	if err != nil {
		t.Fatalf("publish batch: %v", err)
	}

	intervals := loadRevisionIntervals(t, ctx, pool, 1, recordID)
	if len(intervals) != 3 {
		t.Fatalf("intervals = %+v, want three", intervals)
	}
	for index := 0; index < len(intervals)-1; index++ {
		if intervals[index].validTo == nil {
			t.Fatalf("revision %s stayed open behind a later one", intervals[index].revisionID)
		}
		if *intervals[index].validTo != intervals[index+1].validFrom {
			t.Fatalf("gap between %s and %s: closed at %d, next opens at %d",
				intervals[index].revisionID, intervals[index+1].revisionID,
				*intervals[index].validTo, intervals[index+1].validFrom)
		}
	}
	if intervals[len(intervals)-1].validTo != nil {
		t.Fatal("the last revision in the batch must be the open one")
	}
	for sequence := result.AssignedFrom; sequence <= result.AssignedThrough; sequence++ {
		if currentRevisionAt(t, ctx, pool, 1, recordID, sequence) == "" {
			t.Fatalf("no revision resolves at sequence %d", sequence)
		}
	}
}

// A revision that arrives after a later one already took the pointer never held
// it: there is no watermark at which it was current. Its event is still
// projected, and the caller is told the pointer did not move, so the skip cannot
// be mistaken for a lost write.
func TestPostgresIntegrationActivityLateLowerRevisionDoesNotTakeThePointer(t *testing.T) {
	ctx := context.Background()
	fixture := newActivityRecordFixture(t, ctx)
	pool := fixture.runtime
	results := fixture.commitRevisions(t, ctx, "interval-late",
		activityRecordInput(t, "First", ""), activityRecordInput(t, "Second", ""), activityRecordInput(t, "Third", ""))
	candidates := fixture.revisionCandidates(t, ctx, results)
	recordID := results[0].RecordID
	if _, err := publishActivityTestBatch(t, ctx, pool, 1, candidates[2:]); err != nil {
		t.Fatalf("publish the newer revision: %v", err)
	}

	late, err := publishActivityTestBatch(t, ctx, pool, 1, candidates[1:2])
	if err != nil {
		t.Fatalf("publish the late revision: %v", err)
	}
	if late.Inserted != 1 {
		t.Fatalf("inserted %d, want the late event still projected", late.Inserted)
	}
	if late.SupersededRevisions != 1 {
		t.Fatalf("superseded = %d, want 1 so the skipped pointer move is visible", late.SupersededRevisions)
	}

	intervals := loadRevisionIntervals(t, ctx, pool, 1, recordID)
	if len(intervals) != 1 || intervals[0].revisionID != results[2].RevisionID {
		t.Fatalf("intervals = %+v, want only the newer revision to hold the pointer", intervals)
	}
	if intervals[0].validTo != nil {
		t.Fatal("the newer revision must stay open after a late lower one arrives")
	}
	if current := currentRevisionAt(t, ctx, pool, 1, recordID, late.AssignedThrough); current != results[2].RevisionID {
		t.Fatalf("current = %q, want the newer revision to keep the pointer", current)
	}
}

// Two different events each claiming to have created one revision is a source
// contradiction, and it must stop the batch rather than pick one.
func TestPostgresIntegrationActivityRefusesTwoEventsOpeningOneRevision(t *testing.T) {
	ctx := context.Background()
	fixture := newActivityRecordFixture(t, ctx)
	pool := fixture.runtime
	results := fixture.commitRevisions(t, ctx, "interval-duplicate", activityRecordInput(t, "First", ""))
	original := fixture.revisionCandidates(t, ctx, results)[0]
	if _, err := publishActivityTestBatch(t, ctx, pool, 1, []activity.CandidateEvent{original}); err != nil {
		t.Fatalf("publish first: %v", err)
	}

	// The source event is a real committed action. Simulate a faulty adapter
	// claiming that this distinct event also opened the existing revision.
	duplicate := fixture.createAction(t, ctx, results[0], "interval-duplicate-action")
	if duplicate.RevisionID != original.RevisionID || duplicate.OpensRevision {
		t.Fatalf("action did not refer to the existing revision: %+v", duplicate)
	}
	duplicate.OpensRevision = true
	duplicate.CanonicalHash = duplicate.ComputeCanonicalHash()
	_, err := publishActivityTestBatch(t, ctx, pool, 1, []activity.CandidateEvent{duplicate})
	if !errors.Is(err, ErrActivityRevisionIntervalConflict) {
		t.Fatalf("error = %v, want a revision interval conflict", err)
	}

	// The refusal must leave nothing behind, including the event row.
	intervals := loadRevisionIntervals(t, ctx, pool, 1, results[0].RecordID)
	if len(intervals) != 1 {
		t.Fatalf("intervals = %+v, want the rejected batch rolled back", intervals)
	}
	assertActivitySequencesAreContiguous(t, ctx, pool, 1, 1)
}

// An action can refer to a revision without creating it; it must leave the
// pointer alone.
func TestPostgresIntegrationActivityNonCommitEventsLeaveIntervalsAlone(t *testing.T) {
	ctx := context.Background()
	fixture := newActivityRecordFixture(t, ctx)
	pool := fixture.runtime
	results := fixture.commitRevisions(t, ctx, "interval-action", activityRecordInput(t, "First", ""))
	commit := fixture.revisionCandidates(t, ctx, results)[0]
	action := fixture.createAction(t, ctx, results[0], "interval-action-create")
	if action.RevisionID != commit.RevisionID || action.OpensRevision {
		t.Fatalf("action revision route = %+v, want the current revision without a commit", action)
	}
	if _, err := publishActivityTestBatch(t, ctx, pool, 1, []activity.CandidateEvent{commit, action}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	intervals := loadRevisionIntervals(t, ctx, pool, 1, results[0].RecordID)
	if len(intervals) != 1 {
		t.Fatalf("intervals = %+v, want only the commit to open one", intervals)
	}
	if intervals[0].validTo != nil {
		t.Fatal("an action must not close the revision it was written against")
	}
}

// Records are independent: one record's revisions must not close another's.
func TestPostgresIntegrationActivityRevisionIntervalsAreScopedPerRecord(t *testing.T) {
	ctx := context.Background()
	fixture := newActivityRecordFixture(t, ctx)
	pool := fixture.runtime
	firstRecord := fixture.commitRevisions(t, ctx, "interval-scoped-a", activityRecordInput(t, "First", ""))
	secondRecord := fixture.commitRevisions(t, ctx, "interval-scoped-b", activityRecordInput(t, "First", ""), activityRecordInput(t, "Second", ""))
	candidates := append(fixture.revisionCandidates(t, ctx, firstRecord), fixture.revisionCandidates(t, ctx, secondRecord)...)
	if _, err := publishActivityTestBatch(t, ctx, pool, 1, candidates); err != nil {
		t.Fatalf("publish: %v", err)
	}

	first := loadRevisionIntervals(t, ctx, pool, 1, firstRecord[0].RecordID)
	if len(first) != 1 || first[0].validTo != nil {
		t.Fatalf("intervals for the untouched record = %+v, want one open", first)
	}
	second := loadRevisionIntervals(t, ctx, pool, 1, secondRecord[0].RecordID)
	if len(second) != 2 {
		t.Fatalf("intervals for the revised record = %+v, want two", second)
	}
	if second[0].validTo == nil || second[1].validTo != nil {
		t.Fatalf("intervals = %+v, want the older closed and the newer open", second)
	}
}

// Rebuilding clears the retired generation and permits the same committed
// source event to establish its revision pointer in the new generation.
func TestPostgresIntegrationActivityRetiredGenerationIntervalDoesNotBlockTheNext(t *testing.T) {
	ctx := context.Background()
	fixture := newActivityRecordFixture(t, ctx)
	pool := fixture.runtime
	results := fixture.commitRevisions(t, ctx, "interval-generation", activityRecordInput(t, "First", ""))
	candidate := fixture.revisionCandidates(t, ctx, results)[0]
	if _, err := publishActivityTestBatch(t, ctx, pool, 1, []activity.CandidateEvent{candidate}); err != nil {
		t.Fatalf("publish into generation 1: %v", err)
	}
	assertActivitySequencesAreContiguous(t, ctx, pool, 1, 1)
	repository, err := NewActivityProjectionRepository(pool)
	if err != nil {
		t.Fatalf("new activity repository: %v", err)
	}
	rebuilt, err := repository.RebuildActivityProjection(ctx)
	if err != nil || rebuilt.RetiredGeneration != 1 || rebuilt.ActiveGeneration != 2 {
		t.Fatalf("rebuild activity projection = %+v, error %v", rebuilt, err)
	}
	if old := loadRevisionIntervals(t, ctx, pool, 1, results[0].RecordID); len(old) != 0 {
		t.Fatalf("retired generation retained intervals after rebuild: %+v", old)
	}
	head, err := repository.LoadPublishedHead(ctx)
	if err != nil || head.Generation != 2 || head.PublishedIngestSequence != 0 {
		t.Fatalf("rebuilt activity head = %+v, error %v; want generation 2 at watermark 0", head, err)
	}
	if _, err := publishActivityTestBatch(t, ctx, pool, 2, []activity.CandidateEvent{candidate}); err != nil {
		t.Fatalf("publish into generation 2: %v", err)
	}
	assertActivitySequencesAreContiguous(t, ctx, pool, 2, 1)
	intervals := loadRevisionIntervals(t, ctx, pool, 2, results[0].RecordID)
	if len(intervals) != 1 || intervals[0].revisionID != results[0].RevisionID || intervals[0].validTo != nil {
		t.Fatalf("rebuilt intervals = %+v, want the committed revision open", intervals)
	}
}

func TestPostgresIntegrationActivityRecordRouteRecheckRejectsDrift(t *testing.T) {
	ctx := context.Background()
	fixture := newActivityRecordFixture(t, ctx)
	results := fixture.commitRevisions(t, ctx, "activity-route-recheck", activityRecordInput(t, "First", ""), activityRecordInput(t, "Second", ""))
	original := fixture.revisionCandidates(t, ctx, results)[0]
	tests := []struct {
		name       string
		mutate     func(*activity.CandidateEvent)
		incomplete bool
	}{
		{"missing source record", func(candidate *activity.CandidateEvent) { candidate.SourceRecordID = "" }, true},
		{"different record", func(candidate *activity.CandidateEvent) { candidate.RecordID = "rec_different" }, true},
		{"different source version", func(candidate *activity.CandidateEvent) { candidate.Source.Version++ }, false},
		{"different revision", func(candidate *activity.CandidateEvent) { candidate.RevisionID = results[1].RevisionID }, false},
		{"different actor", func(candidate *activity.CandidateEvent) {
			actor := *candidate.Actor
			actor.ActorID = "usr_bbbbbbbbbbbbbbbbbbbbbbbb"
			candidate.Actor = &actor
		}, false},
		{"different recorded time", func(candidate *activity.CandidateEvent) {
			candidate.RecordedAt = candidate.RecordedAt.Add(time.Microsecond)
		}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := original
			test.mutate(&candidate)
			var err error
			candidate.ActivityID, err = activity.NewActivityID(activityTestNamespace(), candidate.Source, candidate.EventKind)
			if err != nil {
				t.Fatal(err)
			}
			candidate.CanonicalHash = candidate.ComputeCanonicalHash()
			_, err = publishActivityTestBatch(t, ctx, fixture.runtime, 1, []activity.CandidateEvent{candidate})
			if test.incomplete {
				if !errors.Is(err, activity.ErrIncompleteRevisionClaim) {
					t.Fatalf("publish changed route error = %v, want incomplete revision claim", err)
				}
			} else {
				var postgresError *pgconn.PgError
				if !errors.As(err, &postgresError) || postgresError.Code != "40001" {
					t.Fatalf("publish changed route error = %v, want source route retry", err)
				}
			}
			var rows, watermark uint64
			if err := fixture.owner.QueryRow(ctx, `select
				(select count(*) from public.record_activity_projection),
				(select published_ingest_sequence from public.record_activity_projection_heads where projection_generation=1)`).Scan(&rows, &watermark); err != nil {
				t.Fatal(err)
			}
			if rows != 0 || watermark != 0 {
				t.Fatalf("rejected route left %d rows and watermark %d", rows, watermark)
			}
		})
	}
	if _, err := publishActivityTestBatch(t, ctx, fixture.runtime, 1, []activity.CandidateEvent{original}); err != nil {
		t.Fatalf("publish unchanged committed route: %v", err)
	}
	assertActivitySequencesAreContiguous(t, ctx, fixture.runtime, 1, 1)
}
