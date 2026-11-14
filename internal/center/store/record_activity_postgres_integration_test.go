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
	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store/migrate"
)

func activityTestNamespace() activity.Namespace {
	return activity.Namespace{ProjectID: "default"}
}

// Generic projection tests use a persisted asset-history fact, so they do not
// impersonate a record-domain event without its record and revision route.
func newActivityCandidate(t *testing.T, pool *pgxpool.Pool, eventID string, eventAt time.Time) activity.CandidateEvent {
	t.Helper()
	return newActivityCandidateForVPS(t, pool, eventID, testEvidenceVPSSourceID, eventAt)
}

func newActivityCandidateForVPS(t *testing.T, pool *pgxpool.Pool, eventID, vpsID string, eventAt time.Time) activity.CandidateEvent {
	t.Helper()
	ctx := context.Background()
	eventAt = eventAt.UTC().Truncate(time.Microsecond)
	if _, err := pool.Exec(ctx, `
		insert into public.vps_assets (vps_id, display_name, lifecycle_status, usage_status)
		values ($1, 'hk-edge-01', 'active', 'in_use') on conflict (vps_id) do nothing`, vpsID); err != nil {
		t.Fatalf("seed activity VPS: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		insert into public.renewal_decisions
		  (decision_id, vps_id, from_decision, to_decision, reason, decided_at, created_at)
		values ($1, $2, 'unreviewed', 'keep', 'activity fixture', $3, $3)`, eventID, vpsID, eventAt); err != nil {
		t.Fatalf("seed activity asset-history fact %q: %v", eventID, err)
	}
	source, err := NewAssetHistoryActivitySource(pool, activityTestNamespace())
	if err != nil {
		t.Fatalf("new asset-history source: %v", err)
	}
	candidates, err := source.ScanAfter(ctx, activity.ScanWindow{From: eventAt.Add(-time.Microsecond), Through: eventAt}, 100)
	if err != nil {
		t.Fatalf("scan activity asset-history fact %q: %v", eventID, err)
	}
	for _, candidate := range candidates {
		if candidate.Source.EventID == assetHistoryEventID(assetFactRenewalDecision, eventID) {
			return candidate
		}
	}
	t.Fatalf("asset-history fact %q not returned by source", eventID)
	return activity.CandidateEvent{}
}

// The published watermark is what a reader may page through. Allocating inside
// the head lock is what makes it contiguous, so a first page taken at any moment
// can never contain a gap.
func TestPostgresIntegrationActivityBatchPublishesAContiguousRange(t *testing.T) {
	ctx := context.Background()
	pool := openActivityTestPool(t, ctx)

	base := time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC)
	candidates := []activity.CandidateEvent{
		newActivityCandidate(t, pool, "rac_a1", base),
		newActivityCandidate(t, pool, "rac_a2", base.Add(time.Minute)),
		newActivityCandidate(t, pool, "rac_a3", base.Add(2*time.Minute)),
	}

	result, err := publishActivityTestBatch(t, ctx, pool, 1, candidates)
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
		newActivityCandidate(t, pool, "rac_b1", base),
		newActivityCandidate(t, pool, "rac_b2", base.Add(time.Minute)),
	}
	if _, err := publishActivityTestBatch(t, ctx, pool, 1, first); err != nil {
		t.Fatalf("first publish: %v", err)
	}

	repeat, err := publishActivityTestBatch(t, ctx, pool, 1, first)
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
	partial := append(append([]activity.CandidateEvent{}, first...), newActivityCandidate(t, pool, "rac_b3", base.Add(2*time.Minute)))
	mixed, err := publishActivityTestBatch(t, ctx, pool, 1, partial)
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
	original := newActivityCandidate(t, pool, "rac_c1", base)
	if _, err := publishActivityTestBatch(t, ctx, pool, 1, []activity.CandidateEvent{original}); err != nil {
		t.Fatalf("first publish: %v", err)
	}

	drifted := original
	drifted.Presentation.Title = "标题被改写了"
	drifted.CanonicalHash = drifted.ComputeCanonicalHash()

	_, err := publishActivityTestBatch(t, ctx, pool, 1, []activity.CandidateEvent{drifted})
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
	if storedTitle != original.Presentation.Title {
		t.Fatalf("stored title = %q, want the original %q to be preserved", storedTitle, original.Presentation.Title)
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
	lease := activityLeaseForTest(t, ctx, pool, 1, activity.SourceKindAssetHistory)
	lowRange := []activity.CandidateEvent{
		newActivityCandidate(t, pool, "rac_d1", base),
		newActivityCandidate(t, pool, "rac_d2", base.Add(time.Minute)),
	}
	// Distinct VPS rows avoid blocking on the source lock before the head lock.
	contender := newActivityCandidateForVPS(t, pool, "rac_d3", "vps_0000000000000001", base.Add(2*time.Minute))

	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holding transaction: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()

	held, err := publishActivityBatchInTx(ctx, holder, lease, lowRange)
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
		result, err := publishActivityTestBatch(t, ctx, pool, 1, []activity.CandidateEvent{
			contender,
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
	lease := activityLeaseForTest(t, ctx, pool, 1, activity.SourceKindAssetHistory)

	if _, err := pool.Exec(ctx, `
		update public.record_activity_projection_heads
		set head_state = 'retired', retired_at = now()
		where projection_generation = 1
	`); err != nil {
		t.Fatalf("retire generation: %v", err)
	}

	_, err := PublishActivityBatch(ctx, pool, lease, []activity.CandidateEvent{
		newActivityCandidate(t, pool, "rac_e1", time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC)),
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
		newActivityCandidate(t, pool, "rac_wm1", base),
		newActivityCandidate(t, pool, "rac_wm2", base.Add(2*time.Minute)),
		newActivityCandidate(t, pool, "rac_wm3", base.Add(4*time.Minute)),
	}
	if _, err := publishActivityTestBatch(t, ctx, pool, 1, first); err != nil {
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

	// Insert one live row that sorts in the middle and one older event after the
	// watermark. Neither may appear on a page still bound to the old as-of.
	late := newActivityCandidate(t, pool, "rac_wm_late", base.Add(time.Minute))
	backfill := newActivityCandidate(t, pool, "rac_wm_old", base.Add(-time.Hour))
	if _, err := publishActivityTestBatch(t, ctx, pool, 1, []activity.CandidateEvent{late, backfill}); err != nil {
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
	// Evidence predates C81 in this fixture and is seeded in the real
	// predecessor migration transaction before the successor is applied.
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	legacy := newImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false,
		func(ctx context.Context, conn *pgx.Conn, _ recordplatform.DeploymentID) error {
			return seedEvidenceActivityFixture(ctx, conn, base)
		})
	if _, err := legacy.platform.db.Exec(ctx, `
		insert into public.vps_assets (vps_id, display_name, lifecycle_status, usage_status)
		values ($1, 'Additional VPS', 'active', 'in_use') on conflict (vps_id) do nothing`, testEvidenceVPSSourceID); err != nil {
		t.Fatalf("seed route-reference VPS: %v", err)
	}
	fixture := makeActivityRecordFixture(t, ctx, legacy.platform.db, legacy.Runtime, legacy.Gate)
	pool := fixture.runtime
	results := fixture.commitRevisions(t, ctx, "activity-route-refs",
		activityRecordInput(t, "First", ""), activityRecordInput(t, "Revised", testEvidenceVPSSourceID))
	recordCandidate := fixture.revisionCandidates(t, ctx, results)[1]

	evidenceSource, err := NewEvidenceActivitySource(pool, activityTestNamespace())
	if err != nil {
		t.Fatalf("new evidence source: %v", err)
	}
	evidenceCandidates, err := evidenceSource.ScanAfter(ctx, activity.ScanWindow{Through: base.Add(5 * time.Minute)}, 50)
	if err != nil {
		t.Fatalf("scan evidence source: %v", err)
	}
	var evidenceCandidate activity.CandidateEvent
	for _, candidate := range evidenceCandidates {
		if candidate.EvidenceID == "evs_vpswindow01" {
			evidenceCandidate = candidate
		}
	}
	if evidenceCandidate.EvidenceID == "" || evidenceCandidate.SourceRecordID != "rec_evidencesrc" {
		t.Fatalf("missing predecessor evidence route: %+v", evidenceCandidate)
	}

	if _, err := publishActivityTestBatch(t, ctx, pool, 1, []activity.CandidateEvent{recordCandidate}); err != nil {
		t.Fatalf("publish record route-reference candidate: %v", err)
	}
	if _, err := publishActivityTestBatch(t, ctx, pool, 1, []activity.CandidateEvent{evidenceCandidate}); err != nil {
		t.Fatalf("publish evidence route-reference candidate: %v", err)
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
			Kind: records.SubjectKindVPS, SourceID: testEvidenceVPSSourceID,
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
