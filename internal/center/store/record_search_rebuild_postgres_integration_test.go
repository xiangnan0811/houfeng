package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/recordsearch"
)

// A record that predates the index has no document, which is exactly the case a
// rebuild exists for: the projector only writes commits, so backfill is the only
// way those records become searchable.
func TestPostgresIntegrationRecordSearchRebuildBackfillsAndPublishesCoverage(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	first := seedRecordWithoutSearchProjection(t, ctx, fixture, "rec_rebuildone", "Rebuild one")
	second := seedRecordWithoutSearchProjection(t, ctx, fixture, "rec_rebuildtwo", "Rebuild two")
	seedPublishedSearchGeneration(t, ctx, fixture, 1)

	store := NewPostgresRecordSearchRebuildStore(
		fixture.openDirectRuntimePool(t, ctx, "record-search-rebuild", 2),
		allowRecordPlatformAdmissionGate,
	)
	needed, err := store.RecordSearchRebuildNeeded(ctx)
	if err != nil || !needed {
		t.Fatalf("RecordSearchRebuildNeeded() = %t, %v, want true, nil", needed, err)
	}
	worker := mustSearchRebuildWorker(t, store)
	rebuilt, err := worker.RunOnce(ctx)
	if err != nil || !rebuilt {
		t.Fatalf("RunOnce() = %t, %v, want true, nil", rebuilt, err)
	}

	var published int64
	var documents int64
	var count int64
	var digestLength int
	if err := fixture.db.QueryRow(ctx, `
		select generation.generation, generation.document_count,
		       (select count(*) from public.record_search_documents where generation = generation.generation),
		       octet_length(generation.coverage_digest)
		from public.record_search_generations as generation
		where generation.generation_state = 'published'`,
	).Scan(&published, &count, &documents, &digestLength); err != nil {
		t.Fatalf("read published generation: %v", err)
	}
	if published != 2 || documents != 2 || count != 2 || digestLength != 32 {
		t.Fatalf("published generation = %d documents=%d count=%d digest=%d bytes",
			published, documents, count, digestLength)
	}
	// The generation that was serving reads is superseded rather than deleted, so
	// a cursor minted against it fails loudly instead of silently changing pages.
	var supersededState string
	if err := fixture.db.QueryRow(ctx, `
		select generation_state from public.record_search_generations where generation = 1`,
	).Scan(&supersededState); err != nil {
		t.Fatalf("read previous generation: %v", err)
	}
	if supersededState != "superseded" {
		t.Fatalf("previous generation state = %q, want superseded", supersededState)
	}
	for _, record := range []string{first.RecordID, second.RecordID} {
		var subjects int64
		if err := fixture.db.QueryRow(ctx, `
			select count(*) from public.record_search_subjects
			where generation = $1 and record_id = $2`, published, record,
		).Scan(&subjects); err != nil {
			t.Fatalf("read rebuilt subjects for %s: %v", record, err)
		}
		if subjects == 0 {
			t.Fatalf("rebuilt record %s has no subject edges", record)
		}
	}
	// Coverage is now complete, so a second pass must find nothing to do.
	needed, err = store.RecordSearchRebuildNeeded(ctx)
	if err != nil || needed {
		t.Fatalf("RecordSearchRebuildNeeded(after publish) = %t, %v, want false, nil", needed, err)
	}
}

// A crashed rebuild leaves a running job with an expired lease. The replacement
// worker must continue from the persisted checkpoint, not restart the scan.
func TestPostgresIntegrationRecordSearchRebuildResumesExpiredLeaseFromCheckpoint(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedRecordWithoutSearchProjection(t, ctx, fixture, "rec_resumeone", "Resume one")
	second := seedRecordWithoutSearchProjection(t, ctx, fixture, "rec_resumetwo", "Resume two")
	seedPublishedSearchGeneration(t, ctx, fixture, 1)
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_search_generations (generation, generation_state) values (2, 'building')`,
	); err != nil {
		t.Fatalf("seed abandoned building generation: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_search_rebuild_jobs (
			job_id, generation, job_state, owner_id, lease_expires_at,
			resume_after_record_id, processed_count
		) values ('rsj_crashed', 2, 'running', 'crashed-owner',
			transaction_timestamp() - interval '1 hour', 'rec_resumeone', 1)`,
	); err != nil {
		t.Fatalf("seed crashed rebuild job: %v", err)
	}

	store := NewPostgresRecordSearchRebuildStore(
		fixture.openDirectRuntimePool(t, ctx, "record-search-rebuild-resume", 2),
		allowRecordPlatformAdmissionGate,
	)
	lease, err := store.ClaimRecordSearchRebuild(ctx, recordsearch.RebuildClaim{
		OwnerID: "record_search_rebuilder", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("ClaimRecordSearchRebuild() error = %v", err)
	}
	if lease.JobID != "rsj_crashed" || lease.Generation != 2 ||
		lease.ResumeAfter != "rec_resumeone" || lease.Projected != 1 {
		t.Fatalf("resumed lease = %#v", lease)
	}

	batch, err := store.ProjectRecordSearchRebuildBatch(ctx, recordsearch.RebuildBatch{
		JobID: lease.JobID, Generation: lease.Generation, OwnerID: "record_search_rebuilder",
		ResumeAfter: lease.ResumeAfter, Limit: 200, LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("ProjectRecordSearchRebuildBatch() error = %v", err)
	}
	if batch.Projected != 1 || batch.ResumeAfter != second.RecordID || !batch.Drained {
		t.Fatalf("resumed batch = %#v, want only the record after the checkpoint", batch)
	}
	// The skipped record stays absent because the checkpoint said it was done, so
	// resume is only safe if the checkpoint is trustworthy.
	var rebuilt int64
	if err := fixture.db.QueryRow(ctx, `
		select count(*) from public.record_search_documents where generation = 2`,
	).Scan(&rebuilt); err != nil {
		t.Fatalf("read resumed documents: %v", err)
	}
	if rebuilt != 1 {
		t.Fatalf("resumed document count = %d, want 1", rebuilt)
	}
}

// Two workers must not write one generation. The loser has to be told, not
// allowed to interleave writes into a generation another worker will publish.
func TestPostgresIntegrationRecordSearchRebuildRefusesSecondOwnerOnLiveLease(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedRecordWithoutSearchProjection(t, ctx, fixture, "rec_leaseheld", "Lease held")
	seedPublishedSearchGeneration(t, ctx, fixture, 1)
	store := NewPostgresRecordSearchRebuildStore(
		fixture.openDirectRuntimePool(t, ctx, "record-search-rebuild-lease", 3),
		allowRecordPlatformAdmissionGate,
	)
	held, err := store.ClaimRecordSearchRebuild(ctx, recordsearch.RebuildClaim{
		OwnerID: "owner-one", LeaseDuration: time.Hour,
	})
	if err != nil {
		t.Fatalf("ClaimRecordSearchRebuild(first) error = %v", err)
	}
	if _, err := store.ClaimRecordSearchRebuild(ctx, recordsearch.RebuildClaim{
		OwnerID: "owner-two", LeaseDuration: time.Hour,
	}); !errors.Is(err, ErrRebuildLeaseHeld) {
		t.Fatalf("ClaimRecordSearchRebuild(second) error = %v, want ErrRebuildLeaseHeld", err)
	}
	if _, err := store.ProjectRecordSearchRebuildBatch(ctx, recordsearch.RebuildBatch{
		JobID: held.JobID, Generation: held.Generation, OwnerID: "owner-two",
		Limit: 200, LeaseDuration: time.Minute,
	}); !errors.Is(err, ErrRebuildLeaseHeld) {
		t.Fatalf("ProjectRecordSearchRebuildBatch(foreign owner) error = %v, want ErrRebuildLeaseHeld", err)
	}
}

// Publishing is only valid from the building state. A second publish of the same
// generation would supersede the index it just installed and leave nothing live.
func TestPostgresIntegrationRecordSearchRebuildRejectsPublishingNonBuildingGeneration(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedRecordWithoutSearchProjection(t, ctx, fixture, "rec_publishcas", "Publish CAS")
	seedPublishedSearchGeneration(t, ctx, fixture, 1)
	store := NewPostgresRecordSearchRebuildStore(
		fixture.openDirectRuntimePool(t, ctx, "record-search-rebuild-cas", 2),
		allowRecordPlatformAdmissionGate,
	)
	worker := mustSearchRebuildWorker(t, store)
	if _, err := worker.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	var jobID string
	if err := fixture.db.QueryRow(ctx, `
		select job_id from public.record_search_rebuild_jobs where generation = 2`,
	).Scan(&jobID); err != nil {
		t.Fatalf("read completed rebuild job: %v", err)
	}
	if _, err := store.PublishRecordSearchRebuild(ctx, recordsearch.RebuildPublish{
		JobID: jobID, Generation: 2, OwnerID: "record_search_rebuilder", Projected: 1,
	}); !errors.Is(err, ErrRebuildLeaseHeld) && !errors.Is(err, recordsearch.ErrInvalidRebuild) {
		t.Fatalf("PublishRecordSearchRebuild(replay) error = %v, want a closed-door refusal", err)
	}
	var live int64
	if err := fixture.db.QueryRow(ctx, `
		select count(*) from public.record_search_generations where generation_state = 'published'`,
	).Scan(&live); err != nil {
		t.Fatalf("read live generation count: %v", err)
	}
	if live != 1 {
		t.Fatalf("published generation count = %d, want exactly 1", live)
	}
}

// A commit that lands while the rebuild is running writes the building
// generation too. The rebuild replays an older snapshot, so the fence must keep
// the newer projection rather than overwrite it.
func TestPostgresIntegrationRecordSearchRebuildDoesNotOverwriteNewerLiveCommit(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	created := seedRecordWithoutSearchProjection(t, ctx, fixture, "rec_rebuildrace", "Race original")
	seedPublishedSearchGeneration(t, ctx, fixture, 1)
	store := NewPostgresRecordSearchRebuildStore(
		fixture.openDirectRuntimePool(t, ctx, "record-search-rebuild-race", 3),
		allowRecordPlatformAdmissionGate,
	)
	lease, err := store.ClaimRecordSearchRebuild(ctx, recordsearch.RebuildClaim{
		OwnerID: "record_search_rebuilder", LeaseDuration: time.Hour,
	})
	if err != nil {
		t.Fatalf("ClaimRecordSearchRebuild() error = %v", err)
	}

	// A live update after the generation opened. The projector writes both the
	// published and the building generation.
	repository := newRecordsPostgresRepository(t,
		fixture.openDirectRuntimePool(t, ctx, "record-search-rebuild-race-commit", 1),
		NewRecordSearchRevisionParticipant(),
	)
	updated, err := repository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordUpdate, created.RecordID, created.RevisionID,
		created.LockVersion, created.AuthorizationEpoch,
		recordsPostgresCompleteRevisionInput(t, "Race newer title"), "rebuild-race-update",
	))
	if err != nil {
		t.Fatalf("CommitRevision(update) error = %v", err)
	}

	if _, err := store.ProjectRecordSearchRebuildBatch(ctx, recordsearch.RebuildBatch{
		JobID: lease.JobID, Generation: lease.Generation, OwnerID: "record_search_rebuilder",
		Limit: 200, LeaseDuration: time.Minute,
	}); err != nil {
		t.Fatalf("ProjectRecordSearchRebuildBatch() error = %v", err)
	}
	var title string
	var lockVersion int64
	if err := fixture.db.QueryRow(ctx, `
		select title, record_lock_version from public.record_search_documents
		where generation = $1 and record_id = $2`, int64(lease.Generation), created.RecordID,
	).Scan(&title, &lockVersion); err != nil {
		t.Fatalf("read raced document: %v", err)
	}
	if title != "Race newer title" || uint64(lockVersion) != updated.LockVersion {
		t.Fatalf("raced document = title %q lock %d, want the newer commit at lock %d",
			title, lockVersion, updated.LockVersion)
	}
}

func mustSearchRebuildWorker(t *testing.T, store recordsearch.RebuildStore) *recordsearch.RebuildWorker {
	t.Helper()
	worker, err := recordsearch.NewRebuildWorker(store, recordsearch.RebuildWorkerOptions{
		OwnerID: "record_search_rebuilder", OwnerLeaseDuration: time.Minute,
		BatchSize: 200, PollInterval: time.Second,
	})
	if err != nil {
		t.Fatalf("NewRebuildWorker() error = %v", err)
	}
	return worker
}

func seedPublishedSearchGeneration(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, generation int64) {
	t.Helper()
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_search_generations (generation, generation_state, published_at)
		values ($1, 'published', transaction_timestamp())`, generation,
	); err != nil {
		t.Fatalf("seed published search generation %d: %v", generation, err)
	}
}
