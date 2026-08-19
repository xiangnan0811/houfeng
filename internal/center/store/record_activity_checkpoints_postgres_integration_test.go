package store

import (
	"context"
	"testing"
	"time"

	"houfeng/internal/center/activity"
)

func newActivityCheckpointRepository(t *testing.T, ctx context.Context) *ActivityProjectionRepository {
	t.Helper()
	pool := openActivityTestPool(t, ctx)
	repository, err := NewActivityProjectionRepository(pool)
	if err != nil {
		t.Fatalf("new activity projection repository: %v", err)
	}
	return repository
}

// An unseen source must come back with no position at all. Returning "now" or an
// error instead would make the first pass skip every event that already exists.
func TestPostgresIntegrationActivityCheckpointStartsWithNoPosition(t *testing.T) {
	ctx := context.Background()
	repository := newActivityCheckpointRepository(t, ctx)

	checkpoint, err := repository.LoadCheckpoint(ctx, 1, activity.SourceKindRecordDomain)
	if err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	if checkpoint.Kind != activity.SourceKindRecordDomain {
		t.Fatalf("kind = %q, want %q", checkpoint.Kind, activity.SourceKindRecordDomain)
	}
	if !checkpoint.RecordedThrough.IsZero() {
		t.Fatalf("recorded through = %s, want the zero time", checkpoint.RecordedThrough)
	}
	if checkpoint.CaughtUp {
		t.Fatalf("a source that never ran must not claim to be caught up")
	}
	if window := checkpoint.FrontierWindow(
		activity.NewSettledSourceHead(activity.SourceKindRecordDomain, time.Now().UTC(), 1),
	); !window.From.IsZero() {
		t.Fatalf("first window starts at %s, want all history", window.From)
	}
}

func TestPostgresIntegrationActivityCheckpointRoundTripsAPosition(t *testing.T) {
	ctx := context.Background()
	repository := newActivityCheckpointRepository(t, ctx)
	position := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)

	if err := repository.SaveCheckpoint(ctx, 1, activity.SourceCheckpoint{
		Kind:            activity.SourceKindRecordDomain,
		RecordedThrough: position,
		CaughtUp:        true,
	}); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}

	checkpoint, err := repository.LoadCheckpoint(ctx, 1, activity.SourceKindRecordDomain)
	if err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	if !checkpoint.RecordedThrough.Equal(position) {
		t.Fatalf("recorded through = %s, want %s", checkpoint.RecordedThrough, position)
	}
	if !checkpoint.CaughtUp || checkpoint.Attempt != 0 || checkpoint.LastErrorCode != "" {
		t.Fatalf("checkpoint = %+v, want a clean caught-up position", checkpoint)
	}
}

// A stale worker writing an old position would open a window that no later pass
// re-reads, because every future scan starts from whatever is stored.
func TestPostgresIntegrationActivityCheckpointRefusesToMoveBackwards(t *testing.T) {
	ctx := context.Background()
	repository := newActivityCheckpointRepository(t, ctx)
	ahead := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	behind := ahead.Add(-time.Hour)

	if err := repository.SaveCheckpoint(ctx, 1, activity.SourceCheckpoint{
		Kind:            activity.SourceKindRecordDomain,
		RecordedThrough: ahead,
		CaughtUp:        true,
	}); err != nil {
		t.Fatalf("save leading checkpoint: %v", err)
	}
	if err := repository.SaveCheckpoint(ctx, 1, activity.SourceCheckpoint{
		Kind:            activity.SourceKindRecordDomain,
		RecordedThrough: behind,
	}); err != nil {
		t.Fatalf("save trailing checkpoint: %v", err)
	}

	checkpoint, err := repository.LoadCheckpoint(ctx, 1, activity.SourceKindRecordDomain)
	if err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	if !checkpoint.RecordedThrough.Equal(ahead) {
		t.Fatalf("recorded through = %s, want it to stay at %s", checkpoint.RecordedThrough, ahead)
	}
}

// A pass that fails before reading anything has no position to write. It must not
// erase the one already stored, or the next pass would re-read all history.
func TestPostgresIntegrationActivityCheckpointFailureKeepsThePosition(t *testing.T) {
	ctx := context.Background()
	repository := newActivityCheckpointRepository(t, ctx)
	position := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)

	if err := repository.SaveCheckpoint(ctx, 1, activity.SourceCheckpoint{
		Kind:            activity.SourceKindRecordDomain,
		RecordedThrough: position,
		CaughtUp:        true,
	}); err != nil {
		t.Fatalf("save initial checkpoint: %v", err)
	}
	if err := repository.SaveCheckpoint(ctx, 1, activity.SourceCheckpoint{
		Kind:          activity.SourceKindRecordDomain,
		Attempt:       3,
		LastErrorCode: "source_unavailable",
	}); err != nil {
		t.Fatalf("save failed checkpoint: %v", err)
	}

	checkpoint, err := repository.LoadCheckpoint(ctx, 1, activity.SourceKindRecordDomain)
	if err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	if !checkpoint.RecordedThrough.Equal(position) {
		t.Fatalf("recorded through = %s, want it preserved at %s", checkpoint.RecordedThrough, position)
	}
	if checkpoint.Attempt != 3 || checkpoint.LastErrorCode != "source_unavailable" {
		t.Fatalf("checkpoint = %+v, want the failure recorded", checkpoint)
	}
	if checkpoint.CaughtUp {
		t.Fatalf("a failed pass must not leave the source claiming to be caught up")
	}
}

func TestPostgresIntegrationActivityCheckpointKeepsSourcesIndependent(t *testing.T) {
	ctx := context.Background()
	repository := newActivityCheckpointRepository(t, ctx)
	recordPosition := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	assetPosition := recordPosition.Add(-3 * time.Hour)

	for kind, position := range map[activity.SourceKind]time.Time{
		activity.SourceKindRecordDomain: recordPosition,
		activity.SourceKindAssetHistory: assetPosition,
	} {
		if err := repository.SaveCheckpoint(ctx, 1, activity.SourceCheckpoint{
			Kind:            kind,
			RecordedThrough: position,
			CaughtUp:        true,
		}); err != nil {
			t.Fatalf("save %s checkpoint: %v", kind, err)
		}
	}

	for kind, want := range map[activity.SourceKind]time.Time{
		activity.SourceKindRecordDomain: recordPosition,
		activity.SourceKindAssetHistory: assetPosition,
	} {
		checkpoint, err := repository.LoadCheckpoint(ctx, 1, kind)
		if err != nil {
			t.Fatalf("load %s checkpoint: %v", kind, err)
		}
		if !checkpoint.RecordedThrough.Equal(want) {
			t.Fatalf("%s recorded through = %s, want %s", kind, checkpoint.RecordedThrough, want)
		}
	}
	// A source with no row of its own must not inherit another source's position.
	unseen, err := repository.LoadCheckpoint(ctx, 1, activity.SourceKindCommandAudit)
	if err != nil {
		t.Fatalf("load unseen checkpoint: %v", err)
	}
	if !unseen.RecordedThrough.IsZero() {
		t.Fatalf("unseen source position = %s, want the zero time", unseen.RecordedThrough)
	}
}
