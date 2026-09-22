package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/recordauth"
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
	authScope, err := activity.ProjectAuthScope(recordauth.ProjectIDDefault)
	if err != nil {
		t.Fatalf("ProjectAuthScope() error = %v", err)
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
		AuthScope: authScope,
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

// A page fixed at as-of must keep the same membership when later rows are
// published, including rows that would sort into the middle of the timeline.
func TestPostgresIntegrationRecordActivitySubjectPageKeepsFixedWatermark(t *testing.T) {
	ctx := context.Background()
	pool := openActivityTestPool(t, ctx)
	repository, err := NewActivityProjectionRepository(pool)
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}

	base := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	first := []activity.CandidateEvent{
		newActivityCandidate(t, "rac_wm1", base),
		newActivityCandidate(t, "rac_wm2", base.Add(2*time.Minute)),
		newActivityCandidate(t, "rac_wm3", base.Add(4*time.Minute)),
	}
	if _, err := PublishActivityBatch(ctx, pool, 1, first); err != nil {
		t.Fatalf("publish first page: %v", err)
	}
	head, err := repository.LoadPublishedHead(ctx)
	if err != nil {
		t.Fatalf("load head: %v", err)
	}
	asOf := head.PublishedIngestSequence

	query, err := activity.NormalizeQuery(activity.Query{
		Subject: activity.SubjectRef{
			Kind: records.SubjectKindVPS, SourceID: "vps_7c2a4e18b09d5f31",
		},
		View:  activity.ViewActivity,
		Limit: 50,
	})
	if err != nil {
		t.Fatalf("normalize query: %v", err)
	}
	pageRequest := activity.SubjectPageRequest{
		Query:            query,
		Generation:       head.Generation,
		AsOf:             asOf,
		Limit:            10,
		AuthUnrestricted: true,
	}
	pageOne, err := repository.ListSubjectPage(ctx, pageRequest)
	if err != nil {
		t.Fatalf("list page one: %v", err)
	}
	if !pageOne.SubjectKnown || len(pageOne.Events) != 3 {
		t.Fatalf("page one = known=%v events=%d, want known with 3", pageOne.SubjectKnown, len(pageOne.Events))
	}
	firstIDs := []string{pageOne.Events[0].ActivityID, pageOne.Events[1].ActivityID, pageOne.Events[2].ActivityID}

	// Insert a live row that sorts between the first two events, and a backfilled
	// older row. Neither may appear on a page still bound to the old as-of.
	late := newActivityCandidate(t, "rac_wm_late", base.Add(time.Minute))
	backfill := newActivityCandidate(t, "rac_wm_old", base.Add(-time.Hour))
	backfill.Backfilled = true
	backfill.CanonicalHash = backfill.ComputeCanonicalHash()
	if _, err := PublishActivityBatch(ctx, pool, 1, []activity.CandidateEvent{late, backfill}); err != nil {
		t.Fatalf("publish after watermark: %v", err)
	}

	pageAgain, err := repository.ListSubjectPage(ctx, pageRequest)
	if err != nil {
		t.Fatalf("list at frozen as-of: %v", err)
	}
	if len(pageAgain.Events) != 3 {
		t.Fatalf("frozen page length = %d, want 3", len(pageAgain.Events))
	}
	for i, event := range pageAgain.Events {
		if event.ActivityID != firstIDs[i] {
			t.Fatalf("frozen page[%d] = %s, want %s", i, event.ActivityID, firstIDs[i])
		}
	}

	newerHead, err := repository.LoadPublishedHead(ctx)
	if err != nil {
		t.Fatalf("reload head: %v", err)
	}
	hasNewer, err := repository.HasNewerAuthorized(ctx, activity.SubjectPageRequest{
		Query:            query,
		Generation:       newerHead.Generation,
		AsOf:             newerHead.PublishedIngestSequence,
		AuthUnrestricted: true,
	}, asOf)
	if err != nil {
		t.Fatalf("HasNewerAuthorized: %v", err)
	}
	if !hasNewer {
		t.Fatal("published rows after as-of must set HasNewerAuthorized")
	}

	refreshed := pageRequest
	refreshed.AsOf = newerHead.PublishedIngestSequence
	pageFresh, err := repository.ListSubjectPage(ctx, refreshed)
	if err != nil {
		t.Fatalf("list refreshed: %v", err)
	}
	if len(pageFresh.Events) != 5 {
		t.Fatalf("refreshed page length = %d, want 5", len(pageFresh.Events))
	}
}

func TestPostgresIntegrationActivityListHydratesPersistedRouteReferences(t *testing.T) {
	ctx := context.Background()
	pool := openActivityTestPool(t, ctx)
	base := time.Date(2026, 8, 19, 14, 0, 0, 0, time.UTC)

	projectScope, err := activity.ProjectAuthScope(recordauth.ProjectIDDefault)
	if err != nil {
		t.Fatalf("ProjectAuthScope() error = %v", err)
	}
	subject := activity.SubjectSnapshot{
		Kind:     records.SubjectKindVPS,
		SourceID: "vps_7c2a4e18b09d5f31",
		Role:     records.RelationRoleAffected,
		Primary:  true,
		Identity: map[string]string{"display_name": "hk-edge-01"},
	}

	recordSource := activity.SourceIdentity{
		Kind:    activity.SourceKindRecordDomain,
		EventID: "rac_refs_record",
		Version: 1,
	}
	recordActivityID, err := activity.NewActivityID(
		activityTestNamespace(), recordSource, activity.EventKindRecordRevised,
	)
	if err != nil {
		t.Fatalf("mint record activity id: %v", err)
	}
	recordCandidate := activity.CandidateEvent{
		ActivityID: recordActivityID,
		Source:     recordSource,
		EventKind:  activity.EventKindRecordRevised,
		EventAt:    base,
		RecordedAt: base,
		Subjects:   []activity.SubjectSnapshot{subject},
		Presentation: activity.Presentation{
			Version: activity.PresentationVersionV1,
			Title:   "记录已修订",
		},
		Severity:   "info",
		RecordID:   "rec_activityrefs",
		RevisionID: "rrv_activityrefs",
		AuthScope:  projectScope,
	}
	recordCandidate.CanonicalHash = recordCandidate.ComputeCanonicalHash()

	evidenceSource := activity.SourceIdentity{
		Kind:    activity.SourceKindEvidenceSnapshot,
		EventID: "evs_refs_evidence",
		Version: 1,
	}
	evidenceActivityID, err := activity.NewActivityID(
		activityTestNamespace(), evidenceSource, activity.EventKindEvidenceCaptured,
	)
	if err != nil {
		t.Fatalf("mint evidence activity id: %v", err)
	}
	evidenceCandidate := activity.CandidateEvent{
		ActivityID: evidenceActivityID,
		Source:     evidenceSource,
		EventKind:  activity.EventKindEvidenceCaptured,
		EventAt:    base.Add(time.Minute),
		RecordedAt: base.Add(time.Minute),
		Subjects:   []activity.SubjectSnapshot{subject},
		Presentation: activity.Presentation{
			Version: activity.PresentationVersionV1,
			Title:   "证据已捕获",
			Summary: "monitoring.host.v1",
		},
		Severity:   "info",
		EvidenceID: "evs_activityrefs",
		AuthScope:  projectScope,
	}
	evidenceCandidate.CanonicalHash = evidenceCandidate.ComputeCanonicalHash()

	if _, err := PublishActivityBatch(ctx, pool, 1, []activity.CandidateEvent{
		recordCandidate, evidenceCandidate,
	}); err != nil {
		t.Fatalf("publish route-reference candidates: %v", err)
	}
	repository, err := NewActivityProjectionRepository(pool)
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	head, err := repository.LoadPublishedHead(ctx)
	if err != nil {
		t.Fatalf("load published head: %v", err)
	}
	query, err := activity.NormalizeQuery(activity.Query{
		Subject: activity.SubjectRef{
			Kind: records.SubjectKindVPS, SourceID: subject.SourceID,
		},
		View:  activity.ViewActivity,
		Limit: 50,
	})
	if err != nil {
		t.Fatalf("normalize query: %v", err)
	}
	page, err := repository.ListSubjectPage(ctx, activity.SubjectPageRequest{
		Query:            query,
		Generation:       head.Generation,
		AsOf:             head.PublishedIngestSequence,
		Limit:            50,
		AuthUnrestricted: true,
	})
	if err != nil {
		t.Fatalf("list subject page: %v", err)
	}
	if len(page.Events) != 2 {
		t.Fatalf("listed %d events, want 2: %+v", len(page.Events), page.Events)
	}

	byKind := make(map[activity.EventKind]activity.Event, len(page.Events))
	for _, event := range page.Events {
		byKind[event.EventKind] = event
	}
	recordEvent, ok := byKind[activity.EventKindRecordRevised]
	if !ok {
		t.Fatalf("record revision event missing from page: %+v", page.Events)
	}
	if recordEvent.RecordID != recordCandidate.RecordID ||
		recordEvent.RevisionID != recordCandidate.RevisionID ||
		recordEvent.EvidenceID != "" {
		t.Fatalf("record route refs = record %q revision %q evidence %q, want record/revision only",
			recordEvent.RecordID, recordEvent.RevisionID, recordEvent.EvidenceID)
	}
	evidenceEvent, ok := byKind[activity.EventKindEvidenceCaptured]
	if !ok {
		t.Fatalf("evidence event missing from page: %+v", page.Events)
	}
	if evidenceEvent.RecordID != "" ||
		evidenceEvent.RevisionID != "" ||
		evidenceEvent.EvidenceID != evidenceCandidate.EvidenceID {
		t.Fatalf("evidence route refs = record %q revision %q evidence %q, want evidence only",
			evidenceEvent.RecordID, evidenceEvent.RevisionID, evidenceEvent.EvidenceID)
	}

	wire, err := json.Marshal(page.Events)
	if err != nil {
		t.Fatalf("marshal listed events: %v", err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatalf("decode listed events: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("decoded %d events, want 2", len(decoded))
	}
	for _, item := range decoded {
		switch item["event_kind"] {
		case string(activity.EventKindRecordRevised):
			if item["record_id"] != recordCandidate.RecordID ||
				item["revision_id"] != recordCandidate.RevisionID {
				t.Fatalf("record JSON refs = %#v, want persisted refs", item)
			}
			if _, present := item["evidence_snapshot_id"]; present {
				t.Fatalf("record JSON unexpectedly carries evidence ref: %#v", item)
			}
		case string(activity.EventKindEvidenceCaptured):
			if item["evidence_snapshot_id"] != evidenceCandidate.EvidenceID {
				t.Fatalf("evidence JSON ref = %#v, want %q", item["evidence_snapshot_id"], evidenceCandidate.EvidenceID)
			}
			if _, present := item["record_id"]; present {
				t.Fatalf("evidence JSON unexpectedly carries record ref: %#v", item)
			}
		default:
			t.Fatalf("unexpected JSON event: %#v", item)
		}
	}
}
