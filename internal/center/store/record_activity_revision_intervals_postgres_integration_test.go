package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
)

const (
	intervalTestRecordID = "rec_9a1b2c"
	intervalTestOtherID  = "rec_7f8e9d"
)

// newRevisionCommitCandidate is a record-domain event that committed a revision,
// which is the only kind of event that moves a record's current pointer.
func newRevisionCommitCandidate(
	t *testing.T,
	eventID string,
	recordID string,
	revisionID string,
	revisionNo uint64,
	eventAt time.Time,
) activity.CandidateEvent {
	t.Helper()
	candidate := newActivityCandidate(t, eventID, eventAt)
	candidate.RecordID = recordID
	candidate.RevisionID = revisionID
	candidate.RevisionNo = revisionNo
	candidate.OpensRevision = true
	candidate.CanonicalHash = candidate.ComputeCanonicalHash()
	return candidate
}

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
	pool := openActivityTestPool(t, ctx)

	base := time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC)
	first, err := PublishActivityBatch(ctx, pool, 1, []activity.CandidateEvent{
		newRevisionCommitCandidate(t, "rac_r1", intervalTestRecordID, "rrv_000001", 1, base),
	})
	if err != nil {
		t.Fatalf("publish first revision: %v", err)
	}
	if first.SupersededRevisions != 0 {
		t.Fatalf("superseded = %d, want 0 for a record's first revision", first.SupersededRevisions)
	}

	second, err := PublishActivityBatch(ctx, pool, 1, []activity.CandidateEvent{
		newRevisionCommitCandidate(t, "rac_r2", intervalTestRecordID, "rrv_000002", 2, base.Add(time.Hour)),
	})
	if err != nil {
		t.Fatalf("publish second revision: %v", err)
	}

	intervals := loadRevisionIntervals(t, ctx, pool, 1, intervalTestRecordID)
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
	current := currentRevisionAt(t, ctx, pool, 1, intervalTestRecordID, second.AssignedFrom)
	if current != "rrv_000002" {
		t.Fatalf("current at the handover sequence = %q, want the new revision", current)
	}
	current = currentRevisionAt(t, ctx, pool, 1, intervalTestRecordID, second.AssignedFrom-1)
	if current != "rrv_000001" {
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
	pool := openActivityTestPool(t, ctx)

	base := time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC)
	result, err := PublishActivityBatch(ctx, pool, 1, []activity.CandidateEvent{
		newRevisionCommitCandidate(t, "rac_b1", intervalTestRecordID, "rrv_000001", 1, base),
		newRevisionCommitCandidate(t, "rac_b2", intervalTestRecordID, "rrv_000002", 2, base.Add(time.Minute)),
		newRevisionCommitCandidate(t, "rac_b3", intervalTestRecordID, "rrv_000003", 3, base.Add(2*time.Minute)),
	})
	if err != nil {
		t.Fatalf("publish batch: %v", err)
	}

	intervals := loadRevisionIntervals(t, ctx, pool, 1, intervalTestRecordID)
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
		if currentRevisionAt(t, ctx, pool, 1, intervalTestRecordID, sequence) == "" {
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
	pool := openActivityTestPool(t, ctx)

	base := time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC)
	if _, err := PublishActivityBatch(ctx, pool, 1, []activity.CandidateEvent{
		newRevisionCommitCandidate(t, "rac_l3", intervalTestRecordID, "rrv_000003", 3, base.Add(2*time.Hour)),
	}); err != nil {
		t.Fatalf("publish the newer revision: %v", err)
	}

	late, err := PublishActivityBatch(ctx, pool, 1, []activity.CandidateEvent{
		newRevisionCommitCandidate(t, "rac_l2", intervalTestRecordID, "rrv_000002", 2, base.Add(time.Hour)),
	})
	if err != nil {
		t.Fatalf("publish the late revision: %v", err)
	}
	if late.Inserted != 1 {
		t.Fatalf("inserted %d, want the late event still projected", late.Inserted)
	}
	if late.SupersededRevisions != 1 {
		t.Fatalf("superseded = %d, want 1 so the skipped pointer move is visible", late.SupersededRevisions)
	}

	intervals := loadRevisionIntervals(t, ctx, pool, 1, intervalTestRecordID)
	if len(intervals) != 1 || intervals[0].revisionID != "rrv_000003" {
		t.Fatalf("intervals = %+v, want only the newer revision to hold the pointer", intervals)
	}
	if intervals[0].validTo != nil {
		t.Fatal("the newer revision must stay open after a late lower one arrives")
	}
	if current := currentRevisionAt(t, ctx, pool, 1, intervalTestRecordID, late.AssignedThrough); current != "rrv_000003" {
		t.Fatalf("current = %q, want the newer revision to keep the pointer", current)
	}
}

// Two different events each claiming to have created one revision is a source
// contradiction, and it must stop the batch rather than pick one.
func TestPostgresIntegrationActivityRefusesTwoEventsOpeningOneRevision(t *testing.T) {
	ctx := context.Background()
	pool := openActivityTestPool(t, ctx)

	base := time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC)
	if _, err := PublishActivityBatch(ctx, pool, 1, []activity.CandidateEvent{
		newRevisionCommitCandidate(t, "rac_d1", intervalTestRecordID, "rrv_000001", 1, base),
	}); err != nil {
		t.Fatalf("publish first: %v", err)
	}

	_, err := PublishActivityBatch(ctx, pool, 1, []activity.CandidateEvent{
		newRevisionCommitCandidate(t, "rac_d2", intervalTestRecordID, "rrv_000001", 1, base.Add(time.Minute)),
	})
	if !errors.Is(err, ErrActivityRevisionIntervalConflict) {
		t.Fatalf("error = %v, want a revision interval conflict", err)
	}

	// The refusal must leave nothing behind, including the event row.
	intervals := loadRevisionIntervals(t, ctx, pool, 1, intervalTestRecordID)
	if len(intervals) != 1 {
		t.Fatalf("intervals = %+v, want the rejected batch rolled back", intervals)
	}
	assertActivitySequencesAreContiguous(t, ctx, pool, 1, 1)
}

// Events that carry a revision without having created it — comments, archives —
// must leave the pointer alone.
func TestPostgresIntegrationActivityNonCommitEventsLeaveIntervalsAlone(t *testing.T) {
	ctx := context.Background()
	pool := openActivityTestPool(t, ctx)

	base := time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC)
	commit := newRevisionCommitCandidate(t, "rac_n1", intervalTestRecordID, "rrv_000001", 1, base)

	comment := newActivityCandidate(t, "rac_n2", base.Add(time.Minute))
	comment.RecordID = intervalTestRecordID
	comment.RevisionID = "rrv_000001"
	comment.RevisionNo = 1
	comment.CanonicalHash = comment.ComputeCanonicalHash()

	if _, err := PublishActivityBatch(ctx, pool, 1, []activity.CandidateEvent{commit, comment}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	intervals := loadRevisionIntervals(t, ctx, pool, 1, intervalTestRecordID)
	if len(intervals) != 1 {
		t.Fatalf("intervals = %+v, want only the commit to open one", intervals)
	}
	if intervals[0].validTo != nil {
		t.Fatal("a comment must not close the revision it was written against")
	}
}

// Records are independent: one record's revisions must not close another's.
func TestPostgresIntegrationActivityRevisionIntervalsAreScopedPerRecord(t *testing.T) {
	ctx := context.Background()
	pool := openActivityTestPool(t, ctx)

	base := time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC)
	if _, err := PublishActivityBatch(ctx, pool, 1, []activity.CandidateEvent{
		newRevisionCommitCandidate(t, "rac_s1", intervalTestRecordID, "rrv_000001", 1, base),
		newRevisionCommitCandidate(t, "rac_s2", intervalTestOtherID, "rrv_000101", 1, base.Add(time.Minute)),
		newRevisionCommitCandidate(t, "rac_s3", intervalTestOtherID, "rrv_000102", 2, base.Add(2*time.Minute)),
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}

	first := loadRevisionIntervals(t, ctx, pool, 1, intervalTestRecordID)
	if len(first) != 1 || first[0].validTo != nil {
		t.Fatalf("intervals for the untouched record = %+v, want one open", first)
	}
	second := loadRevisionIntervals(t, ctx, pool, 1, intervalTestOtherID)
	if len(second) != 2 {
		t.Fatalf("intervals for the revised record = %+v, want two", second)
	}
	if second[0].validTo == nil || second[1].validTo != nil {
		t.Fatalf("intervals = %+v, want the older closed and the newer open", second)
	}
}

// A rebuild opens a new generation while the retired one's rows are still there.
// The one-open-interval rule is per generation, so the old open row must not
// stop the new generation from establishing its own pointer.
func TestPostgresIntegrationActivityRetiredGenerationIntervalDoesNotBlockTheNext(t *testing.T) {
	ctx := context.Background()
	pool := openActivityTestPool(t, ctx)

	base := time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC)
	if _, err := PublishActivityBatch(ctx, pool, 1, []activity.CandidateEvent{
		newRevisionCommitCandidate(t, "rac_g1", intervalTestRecordID, "rrv_000001", 1, base),
	}); err != nil {
		t.Fatalf("publish into generation 1: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		update public.record_activity_projection_heads
		set head_state = 'retired', retired_at = now()
		where projection_generation = 1`); err != nil {
		t.Fatalf("retire generation 1: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into public.record_activity_projection_heads
		  (project_id, projection_generation, published_ingest_sequence, allocated_ingest_sequence)
		values ('default', 2, 0, 0)`); err != nil {
		t.Fatalf("open generation 2: %v", err)
	}

	// The rebuilt generation re-derives the record's history; here it reaches the
	// same revision through a different source event, which is enough to show the
	// two generations hold independent pointers.
	if _, err := PublishActivityBatch(ctx, pool, 2, []activity.CandidateEvent{
		newRevisionCommitCandidate(t, "rac_g2", intervalTestRecordID, "rrv_000001", 1, base),
	}); err != nil {
		t.Fatalf("publish into generation 2: %v", err)
	}

	for _, generation := range []uint64{1, 2} {
		intervals := loadRevisionIntervals(t, ctx, pool, generation, intervalTestRecordID)
		if len(intervals) != 1 || intervals[0].validTo != nil {
			t.Fatalf("generation %d intervals = %+v, want its own open row", generation, intervals)
		}
	}
}
