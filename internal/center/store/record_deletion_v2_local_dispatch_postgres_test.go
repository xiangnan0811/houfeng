//go:build linux

package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/sys/unix"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recorddeletion"
	storemigrate "houfeng/internal/center/store/migrate"
)

// v2DispatchBarrier stops only after the selected, operation-bound SQL command
// has returned successfully. The coordinator still owns its key guard while the
// transaction awaits COMMIT and while the committed result is held back.
type v2DispatchBarrier struct {
	*PostgresLocalBlobRepository
	command       string
	entered       chan attachments.LocalCleanupSnapshot
	commit        chan struct{}
	committed     chan error
	resume        chan struct{}
	bindError     chan error
	completeError chan error
	resolveError  chan error
}

func (b *v2DispatchBarrier) execute(ctx context.Context, statement string, request any) (attachments.LocalCleanupSnapshot, error) {
	var snapshot attachments.LocalCleanupSnapshot
	body, err := json.Marshal(request)
	if err != nil {
		return snapshot, err
	}
	tx, err := b.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return snapshot, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var encoded []byte
	if err = tx.QueryRow(ctx, statement, body).Scan(&encoded); err != nil {
		return snapshot, err
	}
	if err = json.Unmarshal(encoded, &snapshot); err != nil {
		return snapshot, err
	}
	if err = normalizeLocalCleanupDigests(&snapshot.Task); err != nil {
		return snapshot, err
	}
	select {
	case b.entered <- snapshot:
	case <-ctx.Done():
		return snapshot, ctx.Err()
	}
	select {
	case <-b.commit:
	case <-ctx.Done():
		return snapshot, ctx.Err()
	}
	err = tx.Commit(ctx)
	select {
	case b.committed <- err:
	case <-ctx.Done():
		return snapshot, ctx.Err()
	}
	if err != nil {
		return snapshot, err
	}
	select {
	case <-b.resume:
	case <-ctx.Done():
		return snapshot, ctx.Err()
	}
	return snapshot, nil
}

func (b *v2DispatchBarrier) ClaimLocalCleanup(ctx context.Context, request attachments.LocalCleanupClaim) (attachments.LocalCleanupSnapshot, error) {
	if b.command == "claim" && request.OperationID != nil {
		return b.execute(ctx, `select public.local_blob_purge_task_claim($1)`, request)
	}
	return b.PostgresLocalBlobRepository.ClaimLocalCleanup(ctx, request)
}

func (b *v2DispatchBarrier) BindLocalCleanupFinal(ctx context.Context, request attachments.LocalCleanupFinalBinding) (attachments.LocalCleanupSnapshot, error) {
	if b.command == "bind" && request.OperationID != nil {
		return b.execute(ctx, `select public.local_blob_purge_bind_final($1)`, request)
	}
	result, err := b.PostgresLocalBlobRepository.BindLocalCleanupFinal(ctx, request)
	b.bindError <- err
	return result, err
}

func (b *v2DispatchBarrier) CompleteLocalCleanup(ctx context.Context, request attachments.LocalCleanupCompletion) (attachments.LocalCleanupSnapshot, error) {
	result, err := b.PostgresLocalBlobRepository.CompleteLocalCleanup(ctx, request)
	b.completeError <- err
	return result, err
}

func (b *v2DispatchBarrier) ResolveLocalCleanup(ctx context.Context, request attachments.LocalCleanupIdentity) (attachments.LocalCleanupSnapshot, error) {
	result, err := b.PostgresLocalBlobRepository.ResolveLocalCleanup(ctx, request)
	b.resolveError <- err
	return result, err
}

var errV2DispatchSeedResponse = errors.New("seed binding response unavailable")

// Only the seed loses its response. Both claim and binding are committed by the
// real repository before the error, and the coordinator has not unlinked bytes.
type v2DispatchBoundSeed struct {
	*PostgresLocalBlobRepository
	bound bool
}

func (s *v2DispatchBoundSeed) BindLocalCleanupFinal(ctx context.Context, request attachments.LocalCleanupFinalBinding) (attachments.LocalCleanupSnapshot, error) {
	result, err := s.PostgresLocalBlobRepository.BindLocalCleanupFinal(ctx, request)
	if err != nil {
		return result, err
	}
	s.bound = true
	return attachments.LocalCleanupSnapshot{}, errV2DispatchSeedResponse
}

func (s *v2DispatchBoundSeed) ResolveLocalCleanup(ctx context.Context, request attachments.LocalCleanupIdentity) (attachments.LocalCleanupSnapshot, error) {
	if s.bound {
		return attachments.LocalCleanupSnapshot{}, errV2DispatchSeedResponse
	}
	return s.PostgresLocalBlobRepository.ResolveLocalCleanup(ctx, request)
}

// Pause after the recovery coordinator acquires the key guard, before it can
// change the first owner's durable or physical results.
type v2DispatchRecoveryBarrier struct {
	*PostgresLocalBlobRepository
	entered chan struct{}
	resume  chan struct{}
}

func (b *v2DispatchRecoveryBarrier) PrepareLocalCleanup(ctx context.Context, request attachments.LocalCleanupPreparation) (attachments.LocalCleanupTask, error) {
	close(b.entered)
	select {
	case <-b.resume:
		return b.PostgresLocalBlobRepository.PrepareLocalCleanup(ctx, request)
	case <-ctx.Done():
		return attachments.LocalCleanupTask{}, ctx.Err()
	}
}

func TestPostgresIntegrationRecordDeletionV2LocalDispatch(t *testing.T) {
	if os.Getenv("HOUFENG_POSTGRES_INTEGRATION") != "1" {
		t.Skip("HOUFENG_POSTGRES_INTEGRATION=1 is required")
	}
	// The existing process fixture stops a genuine managed writer after its
	// temporary file has been synced, leaving its exact durable identity behind.
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "attachments-process.test")
	build := exec.Command("go", "test", "-c", "-o", binary, "./internal/center/attachments")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build process fixture: %v\n%s", err, output)
	}

	for _, name := range []string{"bound_claim_commit_after_expiry", "bind_commit_after_expiry", "unbound_claim_requires_fresh_bind"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
			defer cancel()
			fixture := newAdmittedCoreRecordFixture(t, ctx)
			runtime := fixture.openDirectRuntimePool(t, ctx, "v2-dispatch-runtime", 4)
			worker := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v2-dispatch-worker", 6)
			if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, worker); err != nil {
				t.Fatal(err)
			}
			blob, err := newPostgresLocalBlobStore(t, ctx, fixture, filepath.Join(t.TempDir(), "blob-root"))
			if err != nil {
				t.Fatal(err)
			}
			target := seedAdmittedOutboxRecord(t, ctx, runtime, fixture.admissionGate, "v2-dispatch-target")
			survivor := seedAdmittedOutboxRecord(t, ctx, runtime, fixture.admissionGate, "v2-dispatch-survivor")
			exclusiveBytes := []byte("v2 dispatch exclusive bytes")
			sharedBytes := []byte("v2 dispatch shared bytes")
			exclusive := seedPostgresManagedLocalBlob(t, ctx, fixture, blob, exclusiveBytes).Version
			shared := seedPostgresManagedLocalBlob(t, ctx, fixture, blob, sharedBytes).Version
			for _, object := range []attachments.ObjectVersion{exclusive, shared} {
				if _, err := fixture.db.Exec(ctx, `insert into public.blob_objects(blob_key,sha256_digest,object_version,size_bytes,backend_kind,namespace_id) values($1,$2,$3,$4,'local',$5)`, object.Key, object.SHA256[:], object.VersionID, object.SizeBytes, blob.Namespace().NamespaceID); err != nil {
					t.Fatal(err)
				}
			}
			for _, row := range []struct {
				id, record string
				object     attachments.ObjectVersion
			}{
				{"att_v2dispatchexclusive", target.RecordID, exclusive},
				{"att_v2dispatchshared", target.RecordID, shared},
				{"att_v2dispatchsurvivor", survivor.RecordID, shared},
			} {
				if _, err := fixture.db.Exec(ctx, `insert into public.record_attachments(attachment_id,project_id,record_id,attachment_state,display_name,media_type,logical_size_bytes,blob_key,blob_object_version,created_by) values($1,'default',$2,'available',$1,'text/plain',$3,$4,$5,'usr_records1')`, row.id, row.record, row.object.SizeBytes, row.object.Key, row.object.VersionID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := fixture.db.Exec(ctx, `insert into public.attachment_quota_accounts(project_id,logical_bytes,reserved_bytes,physical_bytes) values('default',$1,0,$2) on conflict(project_id) do update set logical_bytes=excluded.logical_bytes,reserved_bytes=0,physical_bytes=excluded.physical_bytes`, exclusive.SizeBytes+2*shared.SizeBytes, exclusive.SizeBytes+shared.SizeBytes); err != nil {
				t.Fatal(err)
			}

			// A second genuine attempt for the same key is killed while its synced
			// temporary file and original final file both still exist.
			digest := sha256.Sum256(exclusiveBytes)
			job := "rij_v2dispatch"
			if _, err := fixture.db.Exec(ctx, `insert into public.record_import_jobs(import_job_id,actor_id,idempotency_key,job_state,identity_classification,archive_digest,destination_subject_kind,destination_subject_source_id,expires_at) values($1,'usr_aaaaaaaaaaaaaaaaaaaaaaaa',$1,'uploaded','unknown',$2,'vps',$3,clock_timestamp()+interval '1 hour')`, job, digest[:], testStoreRecordVPSID); err != nil {
				t.Fatal(err)
			}
			process := startLocalProtocolProcess(t, binary, []string{
				"HOUFENG_LOCAL_PROCESS_ROOT=" + blob.Namespace().RootPath,
				"HOUFENG_LOCAL_PROCESS_DATABASE_URL=" + contentProcessorCrashDatabaseURL(fixture.directRoleConfig(t, fixture.runtime, "v2-dispatch-temp")),
				"HOUFENG_LOCAL_PROCESS_MODE=put", "HOUFENG_LOCAL_PROCESS_JOB=" + job,
				"HOUFENG_LOCAL_PROCESS_CONTENT=" + string(exclusiveBytes), "HOUFENG_LOCAL_PROCESS_CUTPOINT=after-file-sync",
			})
			process.await(t, 'B')
			facts := readLocalProcessWriteFacts(t, ctx, fixture, job)
			if !facts.dispatched || !facts.created || facts.returned || facts.inode <= 0 {
				t.Fatalf("temporary write facts: %#v", facts)
			}
			tempPath := filepath.Join(blob.Namespace().RootPath, facts.temporary)
			info, err := os.Stat(tempPath)
			if err != nil || info.Size() != int64(len(exclusiveBytes)) {
				t.Fatalf("synced temporary: %v %v", info, err)
			}
			tempBytes, err := os.ReadFile(tempPath)
			if err != nil || !bytes.Equal(tempBytes, exclusiveBytes) {
				t.Fatalf("synced temporary bytes: %q %v", tempBytes, err)
			}
			var temporaryStat, finalStat unix.Stat_t
			if err := unix.Lstat(tempPath, &temporaryStat); err != nil || int64(temporaryStat.Dev) != facts.device || int64(temporaryStat.Ino) != facts.inode {
				t.Fatalf("temporary inode differs from durable identity: %#v %v", temporaryStat, err)
			}
			if err := unix.Lstat(filepath.Join(blob.Namespace().RootPath, exclusive.Key), &finalStat); err != nil {
				t.Fatalf("stat exclusive final: %v", err)
			}
			if temporaryStat.Ino == finalStat.Ino && temporaryStat.Dev == finalStat.Dev {
				t.Fatal("seed temporary unexpectedly aliases the preexisting final")
			}
			process.kill(t)
			if _, err := fixture.db.Exec(ctx, `delete from public.record_import_jobs where import_job_id=$1`, job); err != nil {
				t.Fatal(err)
			}

			issuerExpiry := issueV2WorkerFixture(t, ctx, fixture, runtime, target.RecordID, "dispatch")
			v2DispatchWaitDeadline(t, ctx, fixture, issuerExpiry)
			repository := NewPostgresRecordDeletionRepository(worker, allowRecordPlatformAdmissionGate)
			claim, err := repository.ClaimV2DeletionWork(ctx, recorddeletion.DeletionWorkClaimInput{OwnerID: "v2_dispatch_original", OwnerLeaseDuration: 3 * time.Minute})
			if err != nil || claim == nil {
				t.Fatalf("claim operation: %v %v", claim, err)
			}
			local := NewPostgresLocalBlobRepository(worker, nil, nil)
			coordinator, err := attachments.NewLocalCleanupCoordinator(blob, local)
			if err != nil {
				t.Fatal(err)
			}
			attachmentRepository := NewPostgresAttachmentRepository(worker)
			if err := attachmentRepository.ConfigureAttachmentDeletionLocalCleanup(coordinator); err != nil {
				t.Fatal(err)
			}
			command := attachments.AttachmentDeletionCommand{PurgeTarget: recorddeletion.PurgeTarget{Operation: claim.Operation, OwnerClaim: recorddeletion.OperationOwnerClaim{OperationID: claim.Operation.OperationID, Owner: claim.Owner}}, SurfaceDigest: recorddeletion.RecordAttachmentsSurfaceDigest()}
			_, operation, body, err := attachmentRepository.localAttachmentPurgeBinding(command)
			if err != nil {
				t.Fatal(err)
			}
			var preparation struct {
				Targets []struct {
					DeletionID string `json:"deletion_id"`
					Key        string `json:"key"`
				} `json:"targets"`
			}
			if err := attachmentRepository.localAttachmentPurgeCommand(ctx, `select public.local_attachment_purge_prepare($1)`, body, &preparation); err != nil {
				t.Fatal(err)
			}
			var deletionID string
			for _, item := range preparation.Targets {
				if item.Key == exclusive.Key {
					deletionID = item.DeletionID
				}
			}
			if deletionID == "" {
				t.Fatalf("no exclusive target: %#v", preparation.Targets)
			}
			request := attachments.LocalCleanupPreparation{LocalCleanupOperation: operation.LocalCleanupOperation, DeletionID: deletionID, NamespaceID: operation.NamespaceID, IncarnationID: operation.IncarnationID, Key: exclusive.Key}

			if name == "bound_claim_commit_after_expiry" {
				seed := &v2DispatchBoundSeed{PostgresLocalBlobRepository: local}
				seedCoordinator, err := attachments.NewLocalCleanupCoordinator(blob, seed)
				if err != nil {
					t.Fatal(err)
				}
				_, err = seedCoordinator.Cleanup(ctx, request, "v2_seed_cleanup")
				if !seed.bound || !errors.Is(err, errV2DispatchSeedResponse) {
					t.Fatalf("bound seed: %t %v", seed.bound, err)
				}
				var expires time.Time
				var bound bool
				if err := fixture.db.QueryRow(ctx, `select final_identity_bound and final_was_present and final_device=$2 and final_inode=$3 and object_version=$4 and octet_length(inventory_digest)=32 and deletion_state='claimed' and receipt_digest is null,lease_expires_at from public.blob_gc_deletions where deletion_id=$1`, deletionID, int64(finalStat.Dev), int64(finalStat.Ino), exclusive.VersionID).Scan(&bound, &expires); err != nil || !bound {
					t.Fatalf("durable seed binding: %t %v", bound, err)
				}
				if _, err := blob.Stat(ctx, exclusive); err != nil {
					t.Fatalf("seed removed final: %v", err)
				}
				if _, err := os.Stat(tempPath); err != nil {
					t.Fatalf("seed removed temporary: %v", err)
				}
				v2DispatchWaitDeadline(t, ctx, fixture, expires)
			}
			short, err := repository.RenewV2DeletionWork(ctx, *claim, 8*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			claim = &short
			generation := int64(claim.Owner.Generation)
			request.LocalCleanupOperation = attachments.LocalCleanupOperation{OperationID: &claim.Operation.OperationID, OwnerID: &claim.Owner.OwnerID, Generation: &generation, ExpiresAt: &claim.Owner.ExpiresAt}
			beforeQuota := v2DispatchQuota(t, ctx, fixture)
			barrier := &v2DispatchBarrier{PostgresLocalBlobRepository: local, command: "claim", entered: make(chan attachments.LocalCleanupSnapshot, 1), commit: make(chan struct{}), committed: make(chan error, 1), resume: make(chan struct{}), bindError: make(chan error, 1), completeError: make(chan error, 1), resolveError: make(chan error, 1)}
			if name == "bind_commit_after_expiry" {
				barrier.command = "bind"
			}
			coordinator, err = attachments.NewLocalCleanupCoordinator(blob, barrier)
			if err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() { _, err := coordinator.Cleanup(ctx, request, "v2_formal_cleanup"); result <- err }()
			var snapshot attachments.LocalCleanupSnapshot
			select {
			case snapshot = <-barrier.entered:
			case err := <-result:
				t.Fatalf("command failed before barrier: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if snapshot.Task.ExpiresAt == nil || snapshot.Task.Generation == nil || snapshot.Task.DispatchID == nil || (name == "bound_claim_commit_after_expiry" && !snapshot.Task.FinalIdentityBound) || (name == "bind_commit_after_expiry" && !snapshot.Task.FinalIdentityBound) || (name == "unbound_claim_requires_fresh_bind" && snapshot.Task.FinalIdentityBound) {
				t.Fatalf("unexpected successful command result: %#v", snapshot.Task)
			}
			if snapshot.Task.FinalIdentityBound && (snapshot.Task.FinalWasPresent == nil || !*snapshot.Task.FinalWasPresent || snapshot.Task.FinalDevice == nil || *snapshot.Task.FinalDevice != finalStat.Dev || snapshot.Task.FinalInode == nil || *snapshot.Task.FinalInode != finalStat.Ino || snapshot.Task.Version == nil || *snapshot.Task.Version != exclusive.VersionID || snapshot.Task.InventoryDigest == nil) {
				t.Fatalf("binding omitted exact final identity: %#v", snapshot.Task)
			}
			var live bool
			if err := fixture.db.QueryRow(ctx, `select clock_timestamp()<$1`, claim.Owner.ExpiresAt).Scan(&live); err != nil || !live {
				t.Fatalf("command was not live: %t %v", live, err)
			}
			waitTx, err := fixture.db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			waitV2DeadlineInTransaction(t, ctx, fixture, waitTx, claim.Owner.ExpiresAt)
			if err := waitTx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			close(barrier.commit)
			if err := <-barrier.committed; err != nil {
				t.Fatalf("late commit: %v", err)
			}

			fresh, err := repository.ClaimV2DeletionWork(ctx, recorddeletion.DeletionWorkClaimInput{OwnerID: "v2_dispatch_fresh", OwnerLeaseDuration: 3 * time.Minute})
			if err != nil || fresh == nil || fresh.Owner.Generation <= claim.Owner.Generation {
				t.Fatalf("take over operation: %#v %v", fresh, err)
			}
			// An independent open-file description must fail to lock while the
			// original synchronous coordinator still owns the guard.
			lockPath := filepath.Join(blob.Namespace().RootPath, ".locks", filepath.Base(exclusive.Key)+".lock")
			lock, err := os.OpenFile(lockPath, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); !errors.Is(err, unix.EWOULDBLOCK) {
				lock.Close()
				t.Fatalf("same-key lock was not held: %v", err)
			}
			lock.Close()
			waiter := startLocalProtocolProcess(t, binary, []string{
				"HOUFENG_LOCAL_PROCESS_ROOT=" + blob.Namespace().RootPath,
				"HOUFENG_LOCAL_PROCESS_DATABASE_URL=" + contentProcessorCrashDatabaseURL(fixture.directRoleConfig(t, fixture.runtime, "v2-dispatch-waiter")),
				"HOUFENG_LOCAL_PROCESS_MODE=cancel-wait", "HOUFENG_LOCAL_PROCESS_KEY=" + exclusive.Key,
			})
			waiter.await(t, 'W')
			waiter.release(t)
			v2DispatchWaitDeadline(t, ctx, fixture, *snapshot.Task.ExpiresAt)
			freshGeneration := int64(fresh.Owner.Generation)
			freshRequest := request
			freshRequest.LocalCleanupOperation = attachments.LocalCleanupOperation{OperationID: &fresh.Operation.OperationID, OwnerID: &fresh.Owner.OwnerID, Generation: &freshGeneration, ExpiresAt: &fresh.Owner.ExpiresAt}
			recoveryBarrier := &v2DispatchRecoveryBarrier{PostgresLocalBlobRepository: local, entered: make(chan struct{}), resume: make(chan struct{})}
			freshCoordinator, err := attachments.NewLocalCleanupCoordinator(blob, recoveryBarrier)
			if err != nil {
				t.Fatal(err)
			}
			type cleanupResult struct {
				task attachments.LocalCleanupTask
				err  error
			}
			freshResult := make(chan cleanupResult, 1)
			started := make(chan struct{})
			go func() {
				close(started)
				task, err := freshCoordinator.Cleanup(ctx, freshRequest, "v2_fresh_cleanup")
				freshResult <- cleanupResult{task, err}
			}()
			<-started
			select {
			case got := <-freshResult:
				t.Fatalf("fresh cleanup bypassed held guard: %#v", got)
			default:
			}
			select {
			case <-recoveryBarrier.entered:
				t.Fatal("fresh owner entered held guard")
			default:
			}
			close(barrier.resume)
			oldErr := <-result
			var pgErr *pgconn.PgError
			if !errors.Is(oldErr, attachments.ErrWriteOutcomeUnknown) || !errors.As(oldErr, &pgErr) || pgErr.Code != "42501" {
				t.Fatalf("expired old completion/binding: %v", oldErr)
			}
			var commandErr error
			if name == "unbound_claim_requires_fresh_bind" {
				commandErr = <-barrier.bindError
			} else {
				commandErr = <-barrier.completeError
			}
			resolveErr := <-barrier.resolveError
			for _, observed := range []error{commandErr, resolveErr} {
				var cause *pgconn.PgError
				if !errors.As(observed, &cause) || cause.Code != "42501" {
					t.Fatalf("actual SQL rejection: %v", observed)
				}
			}
			sharedBeforeRecovery, err := readAttachmentPublicationCrashBytes(ctx, blob, shared)
			if err != nil || !bytes.Equal(sharedBeforeRecovery, sharedBytes) {
				t.Fatalf("shared bytes after old dispatch: %q %v", sharedBeforeRecovery, err)
			}
			select {
			case <-recoveryBarrier.entered:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			var pending bool
			if err := fixture.db.QueryRow(ctx, `select deletion_state='claimed' and receipt_digest is null and completed_at is null and cleanup_dispatch_id=$2::uuid from public.blob_gc_deletions where deletion_id=$1`, deletionID, *snapshot.Task.DispatchID).Scan(&pending); err != nil || !pending {
				t.Fatalf("old dispatch did not remain pending: %t %v", pending, err)
			}
			if current := v2DispatchQuota(t, ctx, fixture); current != beforeQuota {
				t.Fatalf("expired command settled quota: before=%#v after=%#v", beforeQuota, current)
			}
			if name == "unbound_claim_requires_fresh_bind" {
				if _, err := blob.Stat(ctx, exclusive); err != nil {
					t.Fatalf("unbound claim removed final: %v", err)
				}
				if _, err := os.Stat(tempPath); err != nil {
					t.Fatalf("unbound claim removed temporary: %v", err)
				}
			} else {
				if _, err := blob.Stat(ctx, exclusive); !errors.Is(err, attachments.ErrBlobNotFound) {
					t.Fatalf("bound dispatch did not remove final: %v", err)
				}
				if _, err := os.Stat(tempPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("bound dispatch did not remove temporary: %v", err)
				}
			}
			close(recoveryBarrier.resume)
			// The fresh coordinator can run only after the old guard has ended.
			got := <-freshResult
			if got.err != nil || got.task.State != "completed" || got.task.ReceiptDigest == nil {
				t.Fatalf("fresh recovery: %#v %v", got.task, got.err)
			}
			wantOutcome := "already_absent"
			if name == "unbound_claim_requires_fresh_bind" {
				wantOutcome = "deleted"
			}
			if got.task.Outcome == nil || *got.task.Outcome != wantOutcome || got.task.Generation == nil || *got.task.Generation <= *snapshot.Task.Generation || got.task.DispatchID == nil || *got.task.DispatchID == *snapshot.Task.DispatchID {
				t.Fatalf("fresh recovery did not claim a new dispatch: %#v", got.task)
			}
			if _, err := blob.Stat(ctx, exclusive); !errors.Is(err, attachments.ErrBlobNotFound) {
				t.Fatalf("exclusive final remains: %v", err)
			}
			if _, err := os.Stat(tempPath); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("temporary remains: %v", err)
			}
			sharedActual, err := readAttachmentPublicationCrashBytes(ctx, blob, shared)
			if err != nil || !bytes.Equal(sharedActual, sharedBytes) {
				t.Fatalf("shared bytes: %q %v", sharedActual, err)
			}
			after := v2DispatchQuota(t, ctx, fixture)
			if after.physical != beforeQuota.physical-exclusive.SizeBytes {
				t.Fatalf("physical quota before=%#v after=%#v", beforeQuota, after)
			}
			replayCoordinator, err := attachments.NewLocalCleanupCoordinator(blob, local)
			if err != nil {
				t.Fatal(err)
			}
			replay, err := replayCoordinator.Cleanup(ctx, freshRequest, "v2_fresh_cleanup")
			if err != nil || replay.ReceiptDigest == nil || *replay.ReceiptDigest != *got.task.ReceiptDigest || v2DispatchQuota(t, ctx, fixture) != after {
				t.Fatalf("recovery replay changed receipt or quota: %#v %v", replay, err)
			}
			oldIdentity := attachments.LocalCleanupIdentity{LocalCleanupOperation: request.LocalCleanupOperation, DeletionID: deletionID, NamespaceID: request.NamespaceID, IncarnationID: request.IncarnationID, OwnerID: "v2_formal_cleanup", DispatchID: *snapshot.Task.DispatchID}
			if _, err := local.ResolveLocalCleanup(ctx, oldIdentity); !errors.As(err, &pgErr) || pgErr.Code != "42501" {
				t.Fatalf("stale dispatch resolved after recovery: %v", err)
			}
			if _, err := coordinator.Cleanup(ctx, request, "v2_formal_cleanup"); !errors.As(err, &pgErr) || pgErr.Code != "42501" {
				t.Fatalf("expired dispatch restarted cleanup: %v", err)
			}
		})
	}
}

type v2DispatchQuotaSnapshot struct{ logical, reserved, physical, version int64 }

func v2DispatchQuota(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture) v2DispatchQuotaSnapshot {
	t.Helper()
	var q v2DispatchQuotaSnapshot
	if err := fixture.db.QueryRow(ctx, `select logical_bytes,reserved_bytes,physical_bytes,quota_version from public.attachment_quota_accounts where project_id='default'`).Scan(&q.logical, &q.reserved, &q.physical, &q.version); err != nil {
		t.Fatal(err)
	}
	return q
}

func v2DispatchWaitDeadline(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, expiry time.Time) {
	t.Helper()
	tx, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	waitV2DeadlineInTransaction(t, ctx, fixture, tx, expiry)
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}
