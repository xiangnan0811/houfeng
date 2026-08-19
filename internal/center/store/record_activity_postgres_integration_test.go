package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store/migrate"
)

func activityTestNamespace() activity.Namespace {
	return activity.Namespace{ProjectID: "default"}
}

func newActivityCandidate(t *testing.T, eventID string, eventAt time.Time) activity.CandidateEvent {
	t.Helper()
	source := activity.SourceIdentity{
		Kind:    activity.SourceKindRecordDomain,
		EventID: eventID,
		Version: 1,
	}
	activityID, err := activity.NewActivityID(activityTestNamespace(), source, activity.EventKindRecordRevised)
	if err != nil {
		t.Fatalf("mint activity id for %q: %v", eventID, err)
	}
	candidate := activity.CandidateEvent{
		ActivityID: activityID,
		Source:     source,
		EventKind:  activity.EventKindRecordRevised,
		EventAt:    eventAt.UTC(),
		RecordedAt: eventAt.UTC(),
		Severity:   "info",
		Presentation: activity.Presentation{
			Version: activity.PresentationVersionV1,
			Title:   "记录已修订",
		},
		Subjects: []activity.SubjectSnapshot{{
			Kind:     records.SubjectKindVPS,
			SourceID: "vps_7c2a4e18b09d5f31",
			Role:     records.RelationRoleAffected,
			Primary:  true,
			Identity: map[string]string{"display_name": "hk-edge-01"},
		}},
	}
	candidate.CanonicalHash = candidate.ComputeCanonicalHash()
	return candidate
}

// The published watermark is what a reader may page through. Allocating inside
// the head lock is what makes it contiguous, so a first page taken at any moment
// can never contain a gap.
func TestPostgresIntegrationActivityBatchPublishesAContiguousRange(t *testing.T) {
	ctx := context.Background()
	pool := openActivityTestPool(t, ctx)

	base := time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC)
	candidates := []activity.CandidateEvent{
		newActivityCandidate(t, "rac_a1", base),
		newActivityCandidate(t, "rac_a2", base.Add(time.Minute)),
		newActivityCandidate(t, "rac_a3", base.Add(2*time.Minute)),
	}

	result, err := PublishActivityBatch(ctx, pool, 1, candidates)
	if err != nil {
		t.Fatalf("PublishActivityBatch() error = %v", err)
	}
	if result.Inserted != 3 || result.AlreadyPresent != 0 {
		t.Fatalf("result = %+v, want 3 inserted and 0 already present", result)
	}
	if result.AssignedFrom != 1 || result.AssignedThrough != 3 || result.PublishedThrough != 3 {
		t.Fatalf("assigned range = [%d,%d] published through %d, want [1,3] and 3",
			result.AssignedFrom, result.AssignedThrough, result.PublishedThrough)
	}

	assertActivitySequencesAreContiguous(t, ctx, pool, 1, 3)
}

// A retry must not consume sequence numbers. If a fully duplicate batch
// allocated a new range, the watermark would advance past numbers no row holds
// and the projection would look like it had holes.
func TestPostgresIntegrationActivityRetryConsumesNoSequenceNumbers(t *testing.T) {
	ctx := context.Background()
	pool := openActivityTestPool(t, ctx)

	base := time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC)
	first := []activity.CandidateEvent{
		newActivityCandidate(t, "rac_b1", base),
		newActivityCandidate(t, "rac_b2", base.Add(time.Minute)),
	}
	if _, err := PublishActivityBatch(ctx, pool, 1, first); err != nil {
		t.Fatalf("first publish: %v", err)
	}

	repeat, err := PublishActivityBatch(ctx, pool, 1, first)
	if err != nil {
		t.Fatalf("exact repeat publish: %v", err)
	}
	if repeat.Inserted != 0 || repeat.AlreadyPresent != 2 {
		t.Fatalf("repeat result = %+v, want 0 inserted and 2 already present", repeat)
	}
	if repeat.PublishedThrough != 2 {
		t.Fatalf("repeat advanced the watermark to %d, want it to stay at 2", repeat.PublishedThrough)
	}

	// A partly overlapping batch numbers only what is genuinely new.
	partial := append(append([]activity.CandidateEvent{}, first...), newActivityCandidate(t, "rac_b3", base.Add(2*time.Minute)))
	mixed, err := PublishActivityBatch(ctx, pool, 1, partial)
	if err != nil {
		t.Fatalf("partial repeat publish: %v", err)
	}
	if mixed.Inserted != 1 || mixed.AlreadyPresent != 2 {
		t.Fatalf("partial result = %+v, want 1 inserted and 2 already present", mixed)
	}
	if mixed.AssignedFrom != 3 || mixed.AssignedThrough != 3 {
		t.Fatalf("partial assigned range = [%d,%d], want [3,3]", mixed.AssignedFrom, mixed.AssignedThrough)
	}
	assertActivitySequencesAreContiguous(t, ctx, pool, 1, 3)
}

// The same source event arriving with different canonical bytes is a source
// contract violation, not a retry. Overwriting the stored row would let the
// projector quietly rewrite history.
func TestPostgresIntegrationActivityRefusesADifferentHashForTheSameSourceEvent(t *testing.T) {
	ctx := context.Background()
	pool := openActivityTestPool(t, ctx)

	base := time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC)
	original := newActivityCandidate(t, "rac_c1", base)
	if _, err := PublishActivityBatch(ctx, pool, 1, []activity.CandidateEvent{original}); err != nil {
		t.Fatalf("first publish: %v", err)
	}

	drifted := original
	drifted.Presentation.Title = "标题被改写了"
	drifted.CanonicalHash = drifted.ComputeCanonicalHash()

	_, err := PublishActivityBatch(ctx, pool, 1, []activity.CandidateEvent{drifted})
	if !errors.Is(err, ErrActivitySourceHashMismatch) {
		t.Fatalf("publish with drifted hash error = %v, want ErrActivitySourceHashMismatch", err)
	}

	// The stored row must be untouched.
	var storedTitle string
	if err := pool.QueryRow(ctx, `
		select presentation_json->>'title'
		from public.record_activity_projection
		where activity_id = $1
	`, original.ActivityID).Scan(&storedTitle); err != nil {
		t.Fatalf("read stored presentation: %v", err)
	}
	if storedTitle != "记录已修订" {
		t.Fatalf("stored title = %q, want the original to be preserved", storedTitle)
	}
}

// This is the ordering guarantee the fixed watermark rests on. While one worker
// holds the head lock with an uncommitted low range, a second worker must not be
// able to publish a higher range and expose a hole. When the first rolls back,
// the second must get the numbers it gave up rather than skipping them.
func TestPostgresIntegrationActivityHeadLockKeepsPublishedRangeContiguousUnderContention(t *testing.T) {
	ctx := context.Background()
	pool := openActivityTestPool(t, ctx)
	base := time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC)

	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holding transaction: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()

	lowRange := []activity.CandidateEvent{
		newActivityCandidate(t, "rac_d1", base),
		newActivityCandidate(t, "rac_d2", base.Add(time.Minute)),
	}
	held, err := publishActivityBatchInTx(ctx, holder, 1, lowRange)
	if err != nil {
		t.Fatalf("publish inside holding transaction: %v", err)
	}
	if held.AssignedFrom != 1 || held.AssignedThrough != 2 {
		t.Fatalf("held range = [%d,%d], want [1,2]", held.AssignedFrom, held.AssignedThrough)
	}

	// The contender must block on the head row rather than racing ahead.
	type publishOutcome struct {
		result ActivityPublishResult
		err    error
	}
	outcomes := make(chan publishOutcome, 1)
	go func() {
		result, err := PublishActivityBatch(ctx, pool, 1, []activity.CandidateEvent{
			newActivityCandidate(t, "rac_d3", base.Add(2*time.Minute)),
		})
		outcomes <- publishOutcome{result: result, err: err}
	}()

	select {
	case outcome := <-outcomes:
		t.Fatalf("contender published while the head lock was held: %+v (err %v)", outcome.result, outcome.err)
	case <-time.After(750 * time.Millisecond):
	}

	// Rolling back must release the numbers, not leave a hole at 1 and 2.
	if err := holder.Rollback(ctx); err != nil {
		t.Fatalf("rollback holding transaction: %v", err)
	}

	select {
	case outcome := <-outcomes:
		if outcome.err != nil {
			t.Fatalf("contender publish after rollback: %v", outcome.err)
		}
		if outcome.result.AssignedFrom != 1 || outcome.result.AssignedThrough != 1 {
			t.Fatalf("contender range = [%d,%d], want the released [1,1]",
				outcome.result.AssignedFrom, outcome.result.AssignedThrough)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("contender never completed after the head lock was released")
	}

	assertActivitySequencesAreContiguous(t, ctx, pool, 1, 1)
}

// A generation that is no longer active must refuse publication outright, so a
// stale worker cannot resurrect rows into a retired projection.
func TestPostgresIntegrationActivityRefusesPublishingIntoARetiredGeneration(t *testing.T) {
	ctx := context.Background()
	pool := openActivityTestPool(t, ctx)

	if _, err := pool.Exec(ctx, `
		update public.record_activity_projection_heads
		set head_state = 'retired', retired_at = now()
		where projection_generation = 1
	`); err != nil {
		t.Fatalf("retire generation: %v", err)
	}

	_, err := PublishActivityBatch(ctx, pool, 1, []activity.CandidateEvent{
		newActivityCandidate(t, "rac_e1", time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC)),
	})
	if !errors.Is(err, ErrActivityGenerationInactive) {
		t.Fatalf("publish into retired generation error = %v, want ErrActivityGenerationInactive", err)
	}
}

func assertActivitySequencesAreContiguous(t *testing.T, ctx context.Context, pool pgxPublisher, generation uint64, through uint64) {
	t.Helper()
	var count, minimum, maximum uint64
	if err := pool.QueryRow(ctx, `
		select count(*), coalesce(min(ingest_sequence), 0), coalesce(max(ingest_sequence), 0)
		from public.record_activity_projection
		where projection_generation = $1
	`, generation).Scan(&count, &minimum, &maximum); err != nil {
		t.Fatalf("read sequence range: %v", err)
	}
	if count != through || minimum != 1 || maximum != through {
		t.Fatalf("generation %d holds %d rows spanning [%d,%d], want %d rows spanning [1,%d]",
			generation, count, minimum, maximum, through, through)
	}

	var publishedThrough uint64
	if err := pool.QueryRow(ctx, `
		select published_ingest_sequence
		from public.record_activity_projection_heads
		where projection_generation = $1
	`, generation).Scan(&publishedThrough); err != nil {
		t.Fatalf("read published head: %v", err)
	}
	if publishedThrough != through {
		t.Fatalf("published head = %d, want %d", publishedThrough, through)
	}
}

// pgxPublisher is the small surface these assertions need, so they work against
// both a pool and a transaction.
type pgxPublisher interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// openActivityTestPool gives each test its own migrated database and seeds the
// one active generation the projection rows hang off.
func openActivityTestPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	pool := openRecordPlatformTemporaryPostgresDatabase(t, ctx)
	if err := migrate.Apply(ctx, pool); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into public.record_activity_projection_heads
		  (project_id, projection_generation, published_ingest_sequence, allocated_ingest_sequence)
		values ('default', 1, 0, 0)
	`); err != nil {
		t.Fatalf("seed active generation: %v", err)
	}
	return pool
}
