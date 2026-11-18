package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

// The actual issuer composition is not available in this fixture. The live
// deletion lease below exercises the same ranked fence check, without creating
// a synthetic V2 gate or claiming an issued-operation acceptance result.
func TestPostgresIntegrationExportRankedWriterFence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	fixture, admission, runtime := newAdmittedOutboxFixture(t, ctx)
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatal(err)
	}
	birthFixture := ImportAttachmentGCFixture{Runtime: runtime, Gate: admission}
	revisions, err := NewPostgresRecordRepository(runtime, admission, nil, newRecordsPostgresAuthorization(t, runtime, admission), nil)
	if err != nil {
		t.Fatal(err)
	}
	createRecord := func(key string) records.RevisionCommitResult {
		t.Helper()
		command := revisionBirthBatchCommandForActor(t, ctx, birthFixture, recordsPostgresCompleteRevisionInput(t, "Export owner"), key, actor)
		created, err := revisions.CommitRevision(ctx, command)
		if err != nil {
			t.Fatalf("create export record: %v", err)
		}
		return created
	}
	first := createRecord("export-fence-a")
	second := createRecord("export-fence-b")
	worker := fixture.openDirectRuntimePool(t, ctx, "export-ranked-writer", 1)
	// The export mutation must explicitly choose READ COMMITTED even when the
	// direct login's default is REPEATABLE READ.
	if _, err := worker.Exec(ctx, `set default_transaction_isolation = 'repeatable read'`); err != nil {
		t.Fatal(err)
	}
	repository := NewPostgresRecordPortabilityRepository(worker, admission, localBlobFixtureWitness(t, worker), nil)
	newInput := func(parent records.RevisionCommitResult, key string) ClaimRecordExportJobInput {
		return ClaimRecordExportJobInput{
			Actor: actor, IdempotencyKey: key, ExportKind: RecordExportKindMarkdown, ExportMode: RecordExportModeSafe,
			RequestFingerprint: sha256.Sum256([]byte("request:" + key)), InventoryDigest: sha256.Sum256([]byte("inventory:" + key)),
			AuthorizationEpoch: parent.AuthorizationEpoch, RecordLockVersion: parent.LockVersion,
			RecordID: parent.RecordID, RevisionID: parent.RevisionID, ExpiresAt: time.Now().UTC().Add(time.Hour),
		}
	}
	claim := func(input ClaimRecordExportJobInput) RecordExportJob {
		t.Helper()
		job, err := repository.ClaimExportJob(ctx, input)
		if err != nil {
			t.Fatalf("claim %s: %v", input.IdempotencyKey, err)
		}
		return job
	}
	stage := func(job RecordExportJob, lockVersion uint64) RecordExportJob {
		t.Helper()
		if err := repository.AdvanceExportJob(ctx, AdvanceRecordExportJobInput{
			Actor: actor, ExpectedJob: job, RecordLockVersion: lockVersion, JobState: RecordExportJobStateStaging,
		}); err != nil {
			t.Fatalf("stage %s: %v", job.ExportJobID, err)
		}
		current, err := repository.LoadExportJob(ctx, job.ExportJobID)
		if err != nil || current.JobState != RecordExportJobStateStaging || current.LockVersion != job.LockVersion+1 {
			t.Fatalf("staged job = %#v, %v", current, err)
		}
		return current
	}

	t.Run("direct runtime writes are denied", func(t *testing.T) {
		job := claim(newInput(second, "export-direct-owner"))
		statements := []string{
			`update public.record_export_jobs set job_state='staging',lock_version=lock_version+1 where export_job_id=$1`,
			`insert into public.record_export_jobs
			 (export_job_id,project_id,actor_id,idempotency_key,export_kind,export_mode,job_state,request_fingerprint,
			  inventory_digest,authorization_epoch,record_id,revision_id,expires_at)
			 select 'rej_directbypass',project_id,actor_id,'export-direct-bypass',export_kind,export_mode,'previewed',
			  request_fingerprint,inventory_digest,authorization_epoch,record_id,revision_id,expires_at
			 from public.record_export_jobs where export_job_id=$1`,
		}
		for _, statement := range statements {
			_, err := runtime.Exec(ctx, statement, job.ExportJobID)
			var postgresError *pgconn.PgError
			if !errors.As(err, &postgresError) || postgresError.Code != "42501" {
				t.Fatalf("direct runtime export write = %v, want insufficient privilege", err)
			}
		}
		assertExportJobState(t, ctx, fixture.db, job.ExportJobID, RecordExportJobStatePreviewed, job.LockVersion)
		assertExportJobCount(t, ctx, fixture.db, "export-direct-bypass", 0)
	})

	t.Run("lease after material preparation blocks claim and staging", func(t *testing.T) {
		terminal := stage(claim(newInput(first, "export-terminal")), first.LockVersion)
		preview := claim(newInput(first, "export-before-fence"))
		input := newInput(first, "export-after-fence")
		// Prove the material's persisted current route was readable before the
		// competing fence; the worker then waits at the decision fence.
		authorization := newRecordsPostgresAuthorization(t, runtime, admission)
		current, err := authorization.ResolveCurrentRecordAuthorization(ctx, actor, first.RecordID)
		if err != nil || records.AuthorizeRecordResource(actor, recordauth.CapabilityRecordRead, current.Evidence) != nil {
			t.Fatalf("prepare authorized record material: %#v, %v", current, err)
		}
		holder, holderPID := holdExportDecisionFence(t, ctx, fixture)
		defer func() { _ = holder.Rollback(context.Background()) }()
		workerPID := exportWorkerPID(t, ctx, worker)
		type result struct {
			job RecordExportJob
			err error
		}
		finished := make(chan result, 1)
		go func() {
			job, err := repository.ClaimExportJob(ctx, input)
			finished <- result{job: job, err: err}
		}()
		waitForPostgresBlockingPID(t, ctx, fixture.db, workerPID, holderPID)
		object := recordplatform.ObjectRef{ProjectID: string(recordplatform.ProjectIDDefault), ObjectKind: "record", ObjectID: first.RecordID}
		fences := NewPostgresRecordPlatformRepository(runtime, admission)
		lease, err := fences.AcquireDeletionFenceLease(ctx, object, recordplatform.LeaseClaimInputV1{
			OwnerID: "export_deletion_fence", LeaseDuration: time.Minute,
		})
		if err != nil {
			t.Fatalf("acquire real deletion fence: %v", err)
		}
		if err := holder.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		claimed := waitForRecordPlatformResult(t, finished, "fenced export claim")
		if !isExportFenceDenial(claimed.err) || claimed.job.ExportJobID != "" {
			t.Fatalf("claim after deletion fence = %#v, %v", claimed.job, claimed.err)
		}
		assertExportJobCount(t, ctx, fixture.db, input.IdempotencyKey, 0)
		if err := repository.AdvanceExportJob(ctx, AdvanceRecordExportJobInput{
			Actor: actor, ExpectedJob: preview, RecordLockVersion: first.LockVersion, JobState: RecordExportJobStateStaging,
		}); !isExportFenceDenial(err) {
			t.Fatalf("stage after deletion fence = %v", err)
		}
		assertExportJobState(t, ctx, fixture.db, preview.ExportJobID, RecordExportJobStatePreviewed, preview.LockVersion)
		if err := repository.AdvanceExportJob(ctx, AdvanceRecordExportJobInput{
			Actor: actor, ExpectedJob: terminal, RecordLockVersion: first.LockVersion, JobState: RecordExportJobStatePublished,
		}); !isExportFenceDenial(err) {
			t.Fatalf("publish after deletion fence = %v", err)
		}
		assertExportJobState(t, ctx, fixture.db, terminal.ExportJobID, RecordExportJobStateStaging, terminal.LockVersion)
		claimCommand := map[string]string{
			"export_job_id": "rej_directfenced", "project_id": string(actor.ProjectID), "actor_id": actor.UserID,
			"idempotency_key": "export-direct-fenced", "export_kind": input.ExportKind, "export_mode": input.ExportMode,
			"request_fingerprint": hex.EncodeToString(input.RequestFingerprint[:]), "inventory_digest": hex.EncodeToString(input.InventoryDigest[:]),
			"authorization_epoch": strconv.FormatUint(input.AuthorizationEpoch, 10), "record_id": input.RecordID, "revision_id": input.RevisionID,
			"expires_at": input.ExpiresAt.Format(time.RFC3339Nano), "created_at": time.Now().UTC().Format(time.RFC3339Nano),
			"record_lock_version": strconv.FormatUint(input.RecordLockVersion, 10),
		}
		claimCommand["updated_at"] = claimCommand["created_at"]
		assertRestrictedExportDenied(t, ctx, runtime, `select public.record_export_job_claim($1)`, claimCommand)
		assertExportJobCount(t, ctx, fixture.db, "export-direct-fenced", 0)
		advanceCommand := directExportAdvanceCommand(preview, RecordExportJobStateStaging, "", first.LockVersion)
		assertRestrictedExportDenied(t, ctx, runtime, `select public.record_export_job_advance($1)`, advanceCommand)
		assertExportJobState(t, ctx, fixture.db, preview.ExportJobID, RecordExportJobStatePreviewed, preview.LockVersion)
		publishCommand := directExportAdvanceCommand(terminal, RecordExportJobStatePublished, "", first.LockVersion)
		assertRestrictedExportDenied(t, ctx, runtime, `select public.record_export_job_advance($1)`, publishCommand)
		wrongRoute := directExportAdvanceCommand(terminal, RecordExportJobStateFailed, "staging_failed", 0)
		wrongRoute["record_id"] = second.RecordID
		assertRestrictedExportDenied(t, ctx, runtime, `select public.record_export_job_advance($1)`, wrongRoute)
		assertExportJobState(t, ctx, fixture.db, terminal.ExportJobID, RecordExportJobStateStaging, terminal.LockVersion)

		// Report a failed stage using its original job identity after fencing
		// and actor disablement; the stale job version cannot report it twice.
		// The service test separately exercises a simulated writer error.
		if _, err := fixture.db.Exec(ctx, `update public.users set disabled_at=clock_timestamp() where user_id=$1`, actor.UserID); err != nil {
			t.Fatal(err)
		}
		outcome := AdvanceRecordExportJobInput{Actor: actor, ExpectedJob: terminal, JobState: RecordExportJobStateFailed, FailureCode: "staging_failed"}
		if err := repository.AdvanceExportJob(ctx, outcome); err != nil {
			t.Fatalf("record legitimate late staging failure: %v", err)
		}
		assertExportJobState(t, ctx, fixture.db, terminal.ExportJobID, RecordExportJobStateFailed, terminal.LockVersion+1)
		if err := repository.AdvanceExportJob(ctx, outcome); !errors.Is(err, ErrRecordExportJobCASConflict) {
			t.Fatalf("stale terminal outcome = %v", err)
		}
		if _, err := fixture.db.Exec(ctx, `update public.users set disabled_at=null where user_id=$1`, actor.UserID); err != nil {
			t.Fatal(err)
		}
		if err := fences.ReleaseDeletionFenceLease(ctx, object, lease.Owner); err != nil {
			t.Fatalf("release test deletion lease: %v", err)
		}
	})

	t.Run("normal claim replay and stage", func(t *testing.T) {
		input := newInput(second, "export-valid")
		job := claim(input)
		replay, err := repository.ClaimExportJob(ctx, input)
		if err != nil || replay.ExportJobID != job.ExportJobID {
			t.Fatalf("replay = %#v, %v", replay, err)
		}
		assertExportJobCount(t, ctx, fixture.db, input.IdempotencyKey, 1)
		staged := stage(job, second.LockVersion)
		if err := repository.AdvanceExportJob(ctx, AdvanceRecordExportJobInput{
			Actor: actor, ExpectedJob: job, RecordLockVersion: second.LockVersion, JobState: RecordExportJobStateStaging,
		}); !errors.Is(err, ErrRecordExportJobCASConflict) {
			t.Fatalf("stale stage = %v", err)
		}
		assertExportJobState(t, ctx, fixture.db, staged.ExportJobID, RecordExportJobStateStaging, staged.LockVersion)
	})

	t.Run("claim holds record ranks until commit", func(t *testing.T) {
		for _, statement := range []string{
			`create table public.test_export_claim_latch (id integer primary key)`,
			`insert into public.test_export_claim_latch values (1)`,
			`create function record_platform_internal.test_export_claim_latch() returns trigger
			 language plpgsql security definer set search_path=pg_catalog as $$
			 begin perform id from public.test_export_claim_latch where id=1 for update; return new; end $$`,
			`create trigger test_export_claim_latch before insert on public.record_export_jobs for each row
			 when (new.idempotency_key='export-writer-first') execute function record_platform_internal.test_export_claim_latch()`,
		} {
			if _, err := fixture.db.Exec(ctx, statement); err != nil {
				t.Fatalf("install export claim barrier: %v", err)
			}
		}
		holder, err := fixture.db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = holder.Rollback(context.Background()) }()
		var holderPID int
		if err := holder.QueryRow(ctx, `select pg_backend_pid()`).Scan(&holderPID); err != nil {
			t.Fatal(err)
		}
		if _, err := holder.Exec(ctx, `select id from public.test_export_claim_latch where id=1 for update`); err != nil {
			t.Fatal(err)
		}
		fencerPool := fixture.openDirectRuntimePool(t, ctx, "export-fencer-after-writer", 1)
		workerPID, fencerPID := exportWorkerPID(t, ctx, worker), exportWorkerPID(t, ctx, fencerPool)
		writerResult := make(chan error, 1)
		go func() {
			_, err := repository.ClaimExportJob(ctx, newInput(second, "export-writer-first"))
			writerResult <- err
		}()
		waitForPostgresBlockingPID(t, ctx, fixture.db, workerPID, holderPID)
		object := recordplatform.ObjectRef{ProjectID: string(recordplatform.ProjectIDDefault), ObjectKind: "record", ObjectID: second.RecordID}
		type fenceResult struct {
			lease recordplatform.DeletionFenceLeaseV1
			err   error
		}
		fencer := NewPostgresRecordPlatformRepository(fencerPool, admission)
		fenceResults := make(chan fenceResult, 1)
		go func() {
			lease, err := fencer.AcquireDeletionFenceLease(ctx, object, recordplatform.LeaseClaimInputV1{
				OwnerID: "export_fence_second", LeaseDuration: time.Minute,
			})
			fenceResults <- fenceResult{lease: lease, err: err}
		}()
		waitForPostgresBlockingPID(t, ctx, fixture.db, fencerPID, workerPID)
		if err := holder.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := waitForRecordPlatformResult(t, writerResult, "writer-first export claim"); err != nil {
			t.Fatalf("claim before deletion fence: %v", err)
		}
		fenced := waitForRecordPlatformResult(t, fenceResults, "deletion fence after export")
		if fenced.err != nil {
			t.Fatalf("fence after export commit: %v", fenced.err)
		}
		assertExportJobCount(t, ctx, fixture.db, "export-writer-first", 1)
		if err := fencer.ReleaseDeletionFenceLease(ctx, object, fenced.lease.Owner); err != nil {
			t.Fatalf("release writer-first test fence: %v", err)
		}
	})

	t.Run("stale material and authorized historical revision", func(t *testing.T) {
		input := newInput(second, "export-stale-material")
		command := recordsPostgresRevisionCommandForActor(t, recordplatform.OperationKindRecordUpdate,
			second.RecordID, second.RevisionID, second.LockVersion, second.AuthorizationEpoch,
			recordsPostgresCompleteRevisionInput(t, "Revised export owner"), "export-record-update", actor)
		updated, err := revisions.CommitRevision(ctx, command)
		if err != nil {
			t.Fatalf("revise export record: %v", err)
		}
		if _, err := repository.ClaimExportJob(ctx, input); !errors.Is(err, ErrRecordExportJobConflict) {
			t.Fatalf("claim stale prepared material = %v", err)
		}
		assertExportJobCount(t, ctx, fixture.db, input.IdempotencyKey, 0)
		historical := newInput(updated, "export-historical")
		historical.RevisionID = input.RevisionID
		stage(claim(historical), updated.LockVersion)
		second = updated
	})

	t.Run("replay cannot replace a changed job", func(t *testing.T) {
		input := newInput(second, "export-replay-drift")
		job := claim(input)
		holder, holderPID := holdExportDecisionFence(t, ctx, fixture)
		defer func() { _ = holder.Rollback(context.Background()) }()
		workerPID := exportWorkerPID(t, ctx, worker)
		finished := make(chan error, 1)
		go func() {
			_, err := repository.ClaimExportJob(ctx, input)
			finished <- err
		}()
		waitForPostgresBlockingPID(t, ctx, fixture.db, workerPID, holderPID)
		if _, err := fixture.db.Exec(ctx, `update public.record_export_jobs set actor_id=$2 where export_job_id=$1`,
			job.ExportJobID, "usr_bbbbbbbbbbbbbbbbbbbbbbbb"); err != nil {
			t.Fatal(err)
		}
		if err := holder.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := waitForRecordPlatformResult(t, finished, "export replay drift"); !errors.Is(err, ErrRecordExportJobConflict) {
			t.Fatalf("replay after actor route change = %v", err)
		}
		assertExportJobCount(t, ctx, fixture.db, input.IdempotencyKey, 1)
		assertExportJobState(t, ctx, fixture.db, job.ExportJobID, RecordExportJobStatePreviewed, job.LockVersion)
	})

	for _, drift := range []struct {
		name string
		sql  string
		arg  string
	}{
		{name: "record", sql: `update public.record_export_jobs set record_id=$2 where export_job_id=$1`, arg: first.RecordID},
		{name: "actor", sql: `update public.record_export_jobs set actor_id=$2 where export_job_id=$1`, arg: "usr_bbbbbbbbbbbbbbbbbbbbbbbb"},
		{name: "owner", sql: `update public.record_export_jobs set owner_id=$2, owner_generation=1, lease_expires_at=clock_timestamp()+interval '1 hour' where export_job_id=$1`, arg: "other_owner"},
	} {
		t.Run("persisted "+drift.name+" drift", func(t *testing.T) {
			job := claim(newInput(second, "export-drift-"+drift.name))
			holder, holderPID := holdExportDecisionFence(t, ctx, fixture)
			defer func() { _ = holder.Rollback(context.Background()) }()
			workerPID := exportWorkerPID(t, ctx, worker)
			finished := make(chan error, 1)
			go func() {
				finished <- repository.AdvanceExportJob(ctx, AdvanceRecordExportJobInput{
					Actor: actor, ExpectedJob: job, RecordLockVersion: second.LockVersion, JobState: RecordExportJobStateStaging,
				})
			}()
			waitForPostgresBlockingPID(t, ctx, fixture.db, workerPID, holderPID)
			if _, err := fixture.db.Exec(ctx, drift.sql, job.ExportJobID, drift.arg); err != nil {
				t.Fatalf("change persisted %s route: %v", drift.name, err)
			}
			if err := holder.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if err := waitForRecordPlatformResult(t, finished, "export route drift"); !errors.Is(err, ErrRecordExportJobCASConflict) {
				t.Fatalf("advance after %s drift = %v", drift.name, err)
			}
			assertExportJobState(t, ctx, fixture.db, job.ExportJobID, RecordExportJobStatePreviewed, job.LockVersion)
		})
	}

	t.Run("actor authorization changes during wait", func(t *testing.T) {
		job := claim(newInput(second, "export-member-drift"))
		holder, holderPID := holdExportDecisionFence(t, ctx, fixture)
		defer func() { _ = holder.Rollback(context.Background()) }()
		workerPID := exportWorkerPID(t, ctx, worker)
		finished := make(chan error, 1)
		go func() {
			finished <- repository.AdvanceExportJob(ctx, AdvanceRecordExportJobInput{
				Actor: actor, ExpectedJob: job, RecordLockVersion: second.LockVersion, JobState: RecordExportJobStateStaging,
			})
		}()
		waitForPostgresBlockingPID(t, ctx, fixture.db, workerPID, holderPID)
		if _, err := fixture.db.Exec(ctx, `update public.users set disabled_at=clock_timestamp() where user_id=$1`, actor.UserID); err != nil {
			t.Fatal(err)
		}
		if err := holder.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := waitForRecordPlatformResult(t, finished, "export membership drift"); !errors.Is(err, recordauth.ErrDenied) {
			t.Fatalf("advance after actor disabled = %v", err)
		}
		assertExportJobState(t, ctx, fixture.db, job.ExportJobID, RecordExportJobStatePreviewed, job.LockVersion)
	})
}

func holdExportDecisionFence(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture) (pgx.Tx, int) {
	t.Helper()
	tx, err := fixture.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	var pid int
	if err := tx.QueryRow(ctx, `select pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1,$2)`, recordAdvisoryClassID, comparisonDecisionFenceKey); err != nil {
		t.Fatal(err)
	}
	return tx, pid
}

func exportWorkerPID(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var pid int
	if err := pool.QueryRow(ctx, `select pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	return pid
}

func assertExportJobCount(t *testing.T, ctx context.Context, db *pgxpool.Pool, key string, want int) {
	t.Helper()
	var count int
	if err := db.QueryRow(ctx, `select count(*) from public.record_export_jobs where idempotency_key=$1`, key).Scan(&count); err != nil || count != want {
		t.Fatalf("export jobs for %s = %d, %v; want %d", key, count, err, want)
	}
}

func assertExportJobState(t *testing.T, ctx context.Context, db *pgxpool.Pool, jobID, wantState string, wantVersion uint64) {
	t.Helper()
	var state string
	var version int64
	if err := db.QueryRow(ctx, `select job_state,lock_version from public.record_export_jobs where export_job_id=$1`, jobID).Scan(&state, &version); err != nil {
		t.Fatal(err)
	}
	if state != wantState || version != int64(wantVersion) {
		t.Fatalf("export job %s = %s/%d, want %s/%d", jobID, state, version, wantState, wantVersion)
	}
}

func directExportAdvanceCommand(job RecordExportJob, state, failure string, rootVersion uint64) map[string]string {
	return map[string]string{
		"export_job_id": job.ExportJobID, "project_id": string(job.ProjectID), "actor_id": job.ActorID,
		"idempotency_key": job.IdempotencyKey, "export_kind": job.ExportKind, "export_mode": job.ExportMode,
		"request_fingerprint": hex.EncodeToString(job.RequestFingerprint[:]), "inventory_digest": hex.EncodeToString(job.InventoryDigest[:]),
		"authorization_epoch": strconv.FormatUint(job.AuthorizationEpoch, 10), "record_id": job.RecordID, "revision_id": job.RevisionID,
		"expires_at": job.ExpiresAt.UTC().Format(time.RFC3339Nano), "expected_state": job.JobState,
		"expected_lock_version": strconv.FormatUint(job.LockVersion, 10), "new_state": state, "failure_code": failure,
		"record_lock_version": strconv.FormatUint(rootVersion, 10),
	}
}

func assertRestrictedExportDenied(t *testing.T, ctx context.Context, pool *pgxpool.Pool, query string, command map[string]string) {
	t.Helper()
	body, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, query, body)
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) || (postgresError.Code != "55000" && postgresError.Code != "40001") {
		t.Fatalf("restricted export command = %v, want fenced or stale route rejection", err)
	}
}

func isExportFenceDenial(err error) bool {
	if errors.Is(err, records.ErrRecordDeletionReserved) {
		return true
	}
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "55000"
}

// SeedExportWorkflowRecord creates an ordinary record through the real birth
// and revision commands for the external-package portability workflow test.
func SeedExportWorkflowRecord(t *testing.T, ctx context.Context, fixture ImportAttachmentGCFixture) (records.RevisionCommitResult, recordauth.ActorScope) {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatal(err)
	}
	command := revisionBirthBatchCommandForActor(t, ctx, fixture, recordsPostgresCompleteRevisionInput(t, "Export workflow"), "export-workflow-record", actor)
	result, err := newRevisionMutationAdmittedRepository(t, fixture).CommitRevision(ctx, command)
	if err != nil {
		t.Fatalf("create export workflow record: %v", err)
	}
	return result, actor
}
