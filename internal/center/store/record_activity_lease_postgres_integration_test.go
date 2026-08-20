package store

import (
	"context"
	"testing"
	"time"

	"houfeng/internal/center/activity"
)

func TestPostgresIntegrationActivitySourceLeaseExcludesASecondWorker(t *testing.T) {
	ctx := context.Background()
	pool := openActivityTestPool(t, ctx)
	repository, err := NewActivityProjectionRepository(pool)
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}

	claimed, err := repository.AcquireSourceLease(ctx, 1, activity.SourceKindRecordDomain, "worker-a", time.Minute)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if !claimed {
		t.Fatal("an unheld source must be claimable")
	}

	// A live lease held by someone else is a refusal, not an error: with more than
	// one process this is the normal outcome on most ticks.
	claimed, err = repository.AcquireSourceLease(ctx, 1, activity.SourceKindRecordDomain, "worker-b", time.Minute)
	if err != nil {
		t.Fatalf("competing acquire: %v", err)
	}
	if claimed {
		t.Fatal("a source under a live lease must not be handed to a second worker")
	}

	// Re-acquiring our own extends it, which is how a long pass keeps it alive.
	claimed, err = repository.AcquireSourceLease(ctx, 1, activity.SourceKindRecordDomain, "worker-a", time.Minute)
	if err != nil {
		t.Fatalf("re-acquire: %v", err)
	}
	if !claimed {
		t.Fatal("the holder must be able to extend its own lease")
	}

	if err := repository.ReleaseSourceLease(ctx, 1, activity.SourceKindRecordDomain, "worker-a"); err != nil {
		t.Fatalf("release: %v", err)
	}
	claimed, err = repository.AcquireSourceLease(ctx, 1, activity.SourceKindRecordDomain, "worker-b", time.Minute)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	if !claimed {
		t.Fatal("a released source must be claimable")
	}
}

// A worker that died holding a lease must not park its source until someone
// notices, so an expired lease is reclaimable.
func TestPostgresIntegrationActivitySourceLeaseIsReclaimableOnceExpired(t *testing.T) {
	ctx := context.Background()
	pool := openActivityTestPool(t, ctx)
	repository, err := NewActivityProjectionRepository(pool)
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}

	if _, err := repository.AcquireSourceLease(
		ctx, 1, activity.SourceKindMonitoringEvent, "worker-dead", time.Millisecond,
	); err != nil {
		t.Fatalf("acquire short lease: %v", err)
	}
	// Move the expiry into the past rather than sleeping, so the test does not
	// depend on wall-clock timing.
	if _, err := pool.Exec(ctx, `
		update public.record_activity_projection_checkpoints
		set lease_expires_at = now() - interval '1 minute'
		where source_kind = $1`, string(activity.SourceKindMonitoringEvent),
	); err != nil {
		t.Fatalf("expire the lease: %v", err)
	}

	claimed, err := repository.AcquireSourceLease(ctx, 1, activity.SourceKindMonitoringEvent, "worker-live", time.Minute)
	if err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	if !claimed {
		t.Fatal("an expired lease must be reclaimable")
	}

	// The dead worker coming back must not be able to release the new holder's
	// lease out from under it.
	if err := repository.ReleaseSourceLease(ctx, 1, activity.SourceKindMonitoringEvent, "worker-dead"); err != nil {
		t.Fatalf("stale release: %v", err)
	}
	claimed, err = repository.AcquireSourceLease(ctx, 1, activity.SourceKindMonitoringEvent, "worker-third", time.Minute)
	if err != nil {
		t.Fatalf("acquire after stale release: %v", err)
	}
	if claimed {
		t.Fatal("a stale release must not free a lease the new holder still owns")
	}
}

// Leases are per source: taking one must not block the others, which is what
// lets several workers share the set.
func TestPostgresIntegrationActivitySourceLeasesAreIndependentPerSource(t *testing.T) {
	ctx := context.Background()
	pool := openActivityTestPool(t, ctx)
	repository, err := NewActivityProjectionRepository(pool)
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}

	if _, err := repository.AcquireSourceLease(ctx, 1, activity.SourceKindRecordDomain, "worker-a", time.Minute); err != nil {
		t.Fatalf("acquire record domain: %v", err)
	}
	for _, kind := range []activity.SourceKind{
		activity.SourceKindEvidenceSnapshot,
		activity.SourceKindAssetHistory,
		activity.SourceKindMonitoringEvent,
		activity.SourceKindCommandAudit,
	} {
		claimed, err := repository.AcquireSourceLease(ctx, 1, kind, "worker-b", time.Minute)
		if err != nil {
			t.Fatalf("acquire %s: %v", kind, err)
		}
		if !claimed {
			t.Fatalf("%s must be claimable while another source is held", kind)
		}
	}
}

// A lease must not disturb the position, and a position must not disturb the
// lease: they share a row but answer different questions.
func TestPostgresIntegrationActivityLeaseAndCheckpointDoNotClobberEachOther(t *testing.T) {
	ctx := context.Background()
	pool := openActivityTestPool(t, ctx)
	repository, err := NewActivityProjectionRepository(pool)
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}

	position := time.Now().UTC().Add(-time.Hour).Truncate(time.Microsecond)
	if _, err := repository.AcquireSourceLease(ctx, 1, activity.SourceKindCommandAudit, "worker-a", time.Minute); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if err := repository.SaveCheckpoint(ctx, 1, activity.SourceCheckpoint{
		Kind:            activity.SourceKindCommandAudit,
		RecordedThrough: position,
		CaughtUp:        true,
	}); err != nil {
		t.Fatalf("save checkpoint: %v", err)
	}

	// Saving a position must leave the lease held.
	claimed, err := repository.AcquireSourceLease(ctx, 1, activity.SourceKindCommandAudit, "worker-b", time.Minute)
	if err != nil {
		t.Fatalf("competing acquire: %v", err)
	}
	if claimed {
		t.Fatal("saving a checkpoint must not drop the lease")
	}

	// Releasing the lease must leave the position intact.
	if err := repository.ReleaseSourceLease(ctx, 1, activity.SourceKindCommandAudit, "worker-a"); err != nil {
		t.Fatalf("release: %v", err)
	}
	checkpoint, err := repository.LoadCheckpoint(ctx, 1, activity.SourceKindCommandAudit)
	if err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	if !checkpoint.RecordedThrough.Equal(position) {
		t.Fatalf("position = %s, want %s: releasing a lease must not move it", checkpoint.RecordedThrough, position)
	}
	if !checkpoint.CaughtUp {
		t.Fatal("releasing a lease must not change the caught-up state")
	}
}

func TestPostgresIntegrationActivityActiveGenerationReadsTheHead(t *testing.T) {
	ctx := context.Background()
	pool := openActivityTestPool(t, ctx)
	repository, err := NewActivityProjectionRepository(pool)
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}

	generation, err := repository.ActiveGeneration(ctx)
	if err != nil {
		t.Fatalf("active generation: %v", err)
	}
	if generation != 1 {
		t.Fatalf("active generation = %d, want the seeded 1", generation)
	}

	// A disaster rebuild increments the generation, and the worker must follow it
	// rather than keep filling a projection nobody reads.
	if _, err := pool.Exec(ctx, `
		update public.record_activity_projection_heads
		set projection_generation = 2, published_ingest_sequence = 0, allocated_ingest_sequence = 0`,
	); err != nil {
		t.Fatalf("bump generation: %v", err)
	}
	generation, err = repository.ActiveGeneration(ctx)
	if err != nil {
		t.Fatalf("active generation after rebuild: %v", err)
	}
	if generation != 2 {
		t.Fatalf("active generation = %d, want 2 after a rebuild", generation)
	}
}

// Nothing published yet is a deployment state, not a failure: the worker waits
// rather than inventing a generation.
func TestPostgresIntegrationActivityActiveGenerationIsZeroBeforeAnyHead(t *testing.T) {
	ctx := context.Background()
	pool := openActivityTestPool(t, ctx)
	repository, err := NewActivityProjectionRepository(pool)
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	if _, err := pool.Exec(ctx, `delete from public.record_activity_projection_heads`); err != nil {
		t.Fatalf("clear head: %v", err)
	}

	generation, err := repository.ActiveGeneration(ctx)
	if err != nil {
		t.Fatalf("active generation: %v", err)
	}
	if generation != 0 {
		t.Fatalf("active generation = %d, want 0 with no head", generation)
	}
}
