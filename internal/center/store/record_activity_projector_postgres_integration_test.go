package store

import (
	"context"
	"sort"
	"testing"
	"time"

	"houfeng/internal/center/activity"
)

// sliceSourceAdapter stands in for one real source. It behaves the way a SQL
// adapter must: it honours both window bounds, orders by recorded time, and
// respects the page limit. The five concrete adapters replace it later; what is
// under test here is the projector against real PostgreSQL.
type sliceSourceAdapter struct {
	kind activity.SourceKind
	rows []activity.CandidateEvent
	head time.Time
}

func (adapter *sliceSourceAdapter) Kind() activity.SourceKind { return adapter.kind }

func (adapter *sliceSourceAdapter) IncrementalHead(context.Context) (activity.SourceHead, error) {
	return activity.NewSettledSourceHead(adapter.kind, adapter.head, 1), nil
}

func (adapter *sliceSourceAdapter) ScanAfter(
	_ context.Context,
	window activity.ScanWindow,
	limit int,
) ([]activity.CandidateEvent, error) {
	matched := make([]activity.CandidateEvent, 0, len(adapter.rows))
	for _, row := range adapter.rows {
		if !window.From.IsZero() && row.RecordedAt.Before(window.From) {
			continue
		}
		if !window.Through.IsZero() && row.RecordedAt.After(window.Through) {
			continue
		}
		matched = append(matched, row)
	}
	sort.Slice(matched, func(i, j int) bool {
		if !matched[i].RecordedAt.Equal(matched[j].RecordedAt) {
			return matched[i].RecordedAt.Before(matched[j].RecordedAt)
		}
		return matched[i].ActivityID < matched[j].ActivityID
	})
	if limit > 0 && len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, nil
}

func newProjectorAgainstPostgres(
	t *testing.T,
	ctx context.Context,
	adapter activity.SourceAdapter,
	batchSize int,
) (*activity.Projector, *ActivityProjectionRepository) {
	t.Helper()
	repository := newActivityCheckpointRepository(t, ctx)
	projector, err := activity.NewProjector(activity.ProjectorOptions{
		Namespace:          activityTestNamespace(),
		Adapters:           []activity.SourceAdapter{adapter},
		Checkpoints:        repository,
		Publisher:          repository,
		BatchSize:          batchSize,
		ReprojectionWindow: time.Hour,
	})
	if err != nil {
		t.Fatalf("new projector: %v", err)
	}
	return projector, repository
}

type projectedRow struct {
	activityID string
	sequence   uint64
	eventAt    time.Time
	backfilled bool
}

func readProjectedRows(t *testing.T, ctx context.Context, repository *ActivityProjectionRepository) []projectedRow {
	t.Helper()
	rows, err := repository.pool.Query(ctx, `
		select activity_id, ingest_sequence, event_at, backfilled
		from public.record_activity_projection
		order by ingest_sequence`)
	if err != nil {
		t.Fatalf("read projection: %v", err)
	}
	defer rows.Close()

	projected := make([]projectedRow, 0)
	for rows.Next() {
		var row projectedRow
		if err := rows.Scan(&row.activityID, &row.sequence, &row.eventAt, &row.backfilled); err != nil {
			t.Fatalf("scan projection row: %v", err)
		}
		projected = append(projected, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read projection: %v", err)
	}
	return projected
}

func assertContiguousFromOne(t *testing.T, projected []projectedRow) {
	t.Helper()
	for index, row := range projected {
		if want := uint64(index + 1); row.sequence != want {
			t.Fatalf("row %d has sequence %d, want %d: a gap makes a reader page over unpublished ground",
				index, row.sequence, want)
		}
	}
}

func projectOnceAgainstPostgres(
	t *testing.T,
	ctx context.Context,
	projector *activity.Projector,
	kind activity.SourceKind,
) activity.SourceOutcome {
	t.Helper()
	report, err := projector.ProjectOnce(ctx, 1)
	if err != nil {
		t.Fatalf("project once: %v", err)
	}
	if err := report.Err(); err != nil {
		t.Fatalf("pass reported failures: %v", err)
	}
	outcome, ok := report.Source(kind)
	if !ok {
		t.Fatalf("report has no outcome for %s", kind)
	}
	return outcome
}

// This is the guarantee the whole read path rests on: after a pass the projection
// is a gap-free prefix, and running the same pass again neither adds rows nor
// consumes sequence numbers.
func TestPostgresIntegrationProjectorPublishesAGapFreePrefixAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	head := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	adapter := &sliceSourceAdapter{
		kind: activity.SourceKindRecordDomain,
		head: head,
		rows: []activity.CandidateEvent{
			newActivityCandidate(t, "rac_p1", head.Add(-30*time.Minute)),
			newActivityCandidate(t, "rac_p2", head.Add(-20*time.Minute)),
			newActivityCandidate(t, "rac_p3", head.Add(-10*time.Minute)),
		},
	}
	projector, repository := newProjectorAgainstPostgres(t, ctx, adapter, 2)

	first := projectOnceAgainstPostgres(t, ctx, projector, adapter.kind)
	if first.Inserted != 3 {
		t.Fatalf("first pass inserted %d rows, want 3", first.Inserted)
	}
	if !first.CaughtUp {
		t.Fatalf("first pass must finish caught up, got %+v", first)
	}
	projected := readProjectedRows(t, ctx, repository)
	if len(projected) != 3 {
		t.Fatalf("projected %d rows, want 3", len(projected))
	}
	assertContiguousFromOne(t, projected)

	second := projectOnceAgainstPostgres(t, ctx, projector, adapter.kind)
	if second.Inserted != 0 {
		t.Fatalf("second pass inserted %d rows, want 0", second.Inserted)
	}
	if again := readProjectedRows(t, ctx, repository); len(again) != 3 {
		t.Fatalf("second pass changed the projection to %d rows", len(again))
	}
}

// A row that commits after the watermark has already passed it is the failure
// mode the trailing re-scan exists for. It has to arrive with a later sequence,
// because publication order is what it is, while keeping its real event time so
// the timeline shows it where it belongs rather than at the end.
func TestPostgresIntegrationProjectorPicksUpALateCommitInItsTruePosition(t *testing.T) {
	ctx := context.Background()
	head := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	adapter := &sliceSourceAdapter{
		kind: activity.SourceKindRecordDomain,
		head: head,
		rows: []activity.CandidateEvent{
			newActivityCandidate(t, "rac_e1", head.Add(-30*time.Minute)),
			newActivityCandidate(t, "rac_e2", head.Add(-10*time.Minute)),
		},
	}
	projector, repository := newProjectorAgainstPostgres(t, ctx, adapter, 10)

	if outcome := projectOnceAgainstPostgres(t, ctx, projector, adapter.kind); outcome.Inserted != 2 {
		t.Fatalf("first pass inserted %d rows, want 2", outcome.Inserted)
	}

	// Only visible now, and recorded below the position the pass already reached.
	late := newActivityCandidate(t, "rac_late", head.Add(-20*time.Minute))
	late.Backfilled = true
	late.CanonicalHash = late.ComputeCanonicalHash()
	adapter.rows = append(adapter.rows, late)

	second := projectOnceAgainstPostgres(t, ctx, projector, adapter.kind)
	if second.Inserted != 1 {
		t.Fatalf("second pass inserted %d rows, want the late row", second.Inserted)
	}

	projected := readProjectedRows(t, ctx, repository)
	if len(projected) != 3 {
		t.Fatalf("projected %d rows, want 3", len(projected))
	}
	assertContiguousFromOne(t, projected)

	var lateRow projectedRow
	for _, row := range projected {
		if row.activityID == late.ActivityID {
			lateRow = row
		}
	}
	if lateRow.sequence != 3 {
		t.Fatalf("late row got sequence %d, want 3: it was published last", lateRow.sequence)
	}
	if !lateRow.backfilled {
		t.Fatalf("a late row must stay visibly late")
	}
	if !lateRow.eventAt.UTC().Equal(late.EventAt) {
		t.Fatalf("late row event time = %s, want its real %s", lateRow.eventAt.UTC(), late.EventAt)
	}
	// The timeline orders by event time, so the late row must land in the middle
	// even though it was published last.
	byEventTime := append([]projectedRow(nil), projected...)
	sort.Slice(byEventTime, func(i, j int) bool { return byEventTime[i].eventAt.Before(byEventTime[j].eventAt) })
	if byEventTime[1].activityID != late.ActivityID {
		t.Fatalf("late row sorted to position %v by event time, want the middle",
			[]string{byEventTime[0].activityID, byEventTime[1].activityID, byEventTime[2].activityID})
	}
}

// A source holding more than one pass can drain must still make progress without
// ever claiming ground it did not read.
func TestPostgresIntegrationProjectorDrainsABacklogAcrossPasses(t *testing.T) {
	ctx := context.Background()
	head := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	rows := make([]activity.CandidateEvent, 0, 7)
	for index := 0; index < 7; index++ {
		rows = append(rows, newActivityCandidate(
			t,
			"rac_b"+string(rune('a'+index)),
			head.Add(-time.Duration(70-index*10)*time.Minute),
		))
	}
	adapter := &sliceSourceAdapter{kind: activity.SourceKindRecordDomain, head: head, rows: rows}
	projector, repository := newProjectorAgainstPostgres(t, ctx, adapter, 3)

	outcome := projectOnceAgainstPostgres(t, ctx, projector, adapter.kind)
	if outcome.Inserted != 7 {
		t.Fatalf("pass inserted %d rows, want the whole backlog of 7", outcome.Inserted)
	}
	if !outcome.CaughtUp {
		t.Fatalf("a drained backlog must end caught up, got %+v", outcome)
	}
	projected := readProjectedRows(t, ctx, repository)
	if len(projected) != 7 {
		t.Fatalf("projected %d rows, want 7", len(projected))
	}
	assertContiguousFromOne(t, projected)
}
