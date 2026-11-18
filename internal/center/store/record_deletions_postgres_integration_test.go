package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/attachments"
	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/recordsearch"
	storemigrate "houfeng/internal/center/store/migrate"
)

func TestPostgresIntegrationRecordDeletionPreviewReserveAndExpiredReplay(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-deletion-preview-reserve", 2)
	birthFixture := ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}
	recordRepository := newRevisionMutationAdmittedRepository(t, birthFixture)
	created, err := recordRepository.CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, birthFixture,
		recordsPostgresCompleteRevisionInput(t, "Deletion preview record"), "record-deletion-preview-create"))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}

	deploymentID := recordsPostgresActiveDeletionDeployment(t, ctx, fixture)
	createCommand, token := recordsPostgresDeletionPreviewCommand(t, created, deploymentID)
	repository := NewPostgresRecordDeletionRepository(runtimePool, fixture.admissionGate)
	recordsPostgresAssertV1IssuanceClosed(t, ctx, fixture, repository, createCommand)
	preview := recordsPostgresSeedV1Preview(t, ctx, fixture, repository, createCommand, token, "drs_pgdeletepreview")
	resolved, err := repository.ResolvePreview(ctx, recorddeletion.PreviewLookup{
		ReservationID: preview.ReservationID,
		Object:        preview.Object,
		Token:         token,
	})
	if err != nil {
		t.Fatalf("ResolvePreview() before reserve error = %v", err)
	}
	if resolved.Operation != nil || resolved.ReservationID != preview.ReservationID ||
		resolved.TokenCommitment != createCommand.TokenCommitment ||
		!createCommand.RequestFingerprint.MatchesPersisted(resolved.RequestFingerprint) {
		t.Fatalf("ResolvePreview() before reserve = %#v", resolved)
	}
	platformRepository := NewPostgresRecordPlatformRepository(runtimePool, fixture.admissionGate)
	serving, err := platformRepository.AcquireServingLease(ctx, createCommand.Object, recordplatform.LeaseClaimInputV1{
		OwnerID:       "attachment_delivery_pgdeletepreview",
		LeaseDuration: time.Second,
	})
	if err != nil {
		t.Fatalf("AcquireServingLease() before reserve error = %v", err)
	}

	reserveCommand := recordsPostgresDeletionReserveCommand(createCommand, resolved, deploymentID)
	// V1 reservation issuance is closed; the fixture represents a reservation
	// fenced before migration. V2's live-serving-lease refusal is exercised in
	// record_deletion_issuer_postgres_test.go:129.
	// The fixture preserves the historical audit; current V1 issuance remains closed.
	operation := recordsPostgresSeedV1Fence(t, ctx, fixture, repository, reserveCommand, "rpo_pgdeletepreview")
	workerRepository := recordsPostgresV1DeletionWorker(t, ctx, fixture)
	claimInput := recorddeletion.DeletionWorkClaimInput{
		OwnerID:            reserveCommand.OwnerID,
		OwnerLeaseDuration: reserveCommand.OwnerLeaseDuration,
	}
	claim, err := workerRepository.ClaimDeletionWork(ctx, claimInput)
	if err != nil {
		t.Fatalf("ClaimDeletionWork() while content lease is live error = %v", err)
	}
	if claim != nil {
		t.Fatalf("ClaimDeletionWork() while content lease is live = %#v, want nil drain wait", claim)
	}
	if _, err := platformRepository.RenewServingLease(ctx, serving, time.Second); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		t.Fatalf("RenewServingLease() after provisional fence error = %v, want ErrLostOwnerLease", err)
	}
	if err := platformRepository.ReleaseObjectContentLease(ctx, serving.Object, serving.Owner); err != nil {
		t.Fatalf("ReleaseObjectContentLease() drain error = %v", err)
	}
	claim, err = workerRepository.ClaimDeletionWork(ctx, claimInput)
	if err != nil {
		t.Fatalf("ClaimDeletionWork() after content drain error = %v", err)
	}
	if claim == nil || claim.Stage != recorddeletion.DeletionWorkAppendDeleteCommit || claim.Operation != operation {
		t.Fatalf("ClaimDeletionWork() after content drain = %#v, want append-delete claim for %#v", claim, operation)
	}
	status, err := repository.ResolveOperationStatus(ctx, recordplatform.ProjectIDDefault, operation.OperationID)
	if err != nil {
		t.Fatalf("ResolveOperationStatus() error = %v", err)
	}
	if status.Operation != operation || status.InitiatorActorID != reserveCommand.ActorID || status.Validate() != nil {
		t.Fatalf("ResolveOperationStatus() = %#v, want operation %#v actor %q", status, operation, reserveCommand.ActorID)
	}
	if _, err := repository.ResolveOperationStatus(ctx, recordplatform.ProjectIDDefault, "rpo_pgdeletemissing"); !errors.Is(err, recorddeletion.ErrDeletionStatusUnavailable) {
		t.Fatalf("ResolveOperationStatus(missing) error = %v, want ErrDeletionStatusUnavailable", err)
	}

	if _, err := fixture.db.Exec(ctx, `
		update public.deletion_reservations
		set expires_at = transaction_timestamp()
		where reservation_id = $1`, preview.ReservationID); err != nil {
		t.Fatalf("expire reserved preview TTL: %v", err)
	}
	replayed, err := repository.ResolvePreview(ctx, recorddeletion.PreviewLookup{
		ReservationID: preview.ReservationID,
		Object:        preview.Object,
		Token:         token,
	})
	if err != nil {
		t.Fatalf("ResolvePreview() durable replay error = %v", err)
	}
	if replayed.Operation == nil || *replayed.Operation != operation {
		t.Fatalf("ResolvePreview() durable replay = %#v, want %#v", replayed, operation)
	}

	var (
		reservationState string
		reservationEpoch int64
		operationState   string
		operationOwner   string
		operationGen     int64
		fenceOwner       string
		fenceGen         int64
		deliveryEpoch    int64
	)
	if err := fixture.db.QueryRow(ctx, `
		select reservation.state,
		       reservation.fence_epoch,
		       operation.operation_state,
		       operation.owner_id,
		       operation.owner_generation,
		       fence.owner_id,
		       fence.owner_generation,
		       epoch.delivery_epoch
		from public.deletion_reservations reservation
		join public.record_purge_operations operation
		  on operation.reservation_id = reservation.reservation_id
		join public.deletion_fence_leases fence
		  on fence.project_id = reservation.project_id
		 and fence.object_kind = reservation.object_kind
		 and fence.object_id = reservation.object_id
		join public.content_delivery_epochs epoch
		  on epoch.project_id = reservation.project_id
		 and epoch.object_kind = reservation.object_kind
		 and epoch.object_id = reservation.object_id
		where reservation.reservation_id = $1`, preview.ReservationID).Scan(
		&reservationState,
		&reservationEpoch,
		&operationState,
		&operationOwner,
		&operationGen,
		&fenceOwner,
		&fenceGen,
		&deliveryEpoch,
	); err != nil {
		t.Fatalf("read reserved deletion operation: %v", err)
	}
	if reservationState != "fenced" || reservationEpoch != 1 ||
		operationState != string(recorddeletion.DeletionStateProvisionalFenced) ||
		operationOwner != reserveCommand.OwnerID || operationGen != 1 ||
		fenceOwner != operationOwner || fenceGen != operationGen || deliveryEpoch != 1 {
		t.Fatalf("reserved deletion state = reservation %q/%d operation %q/%q/%d fence %q/%d epoch %d",
			reservationState, reservationEpoch, operationState, operationOwner, operationGen,
			fenceOwner, fenceGen, deliveryEpoch)
	}
	var fencedAudits int
	if err := fixture.db.QueryRow(ctx, `select count(*) from public.record_deletion_audits where operation_id=$1 and event_kind='fenced'`, operation.OperationID).Scan(&fencedAudits); err != nil || fencedAudits != 1 {
		t.Fatalf("historical fenced audit count = %d, %v", fencedAudits, err)
	}
}

func TestPostgresIntegrationRecordDeletionReserveCASDriftRollsBackFenceAndOperation(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-deletion-cas-drift", 2)
	birthFixture := ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}
	recordRepository := newRevisionMutationAdmittedRepository(t, birthFixture)
	created, err := recordRepository.CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, birthFixture,
		recordsPostgresCompleteRevisionInput(t, "Deletion CAS record"), "record-deletion-cas-create"))
	if err != nil {
		t.Fatalf("CommitRevision(create) error = %v", err)
	}

	deploymentID := recordsPostgresActiveDeletionDeployment(t, ctx, fixture)
	createCommand, token := recordsPostgresDeletionPreviewCommand(t, created, deploymentID)
	repository := NewPostgresRecordDeletionRepository(runtimePool, fixture.admissionGate)
	recordsPostgresAssertV1IssuanceClosed(t, ctx, fixture, repository, createCommand)
	preview := recordsPostgresSeedV1Preview(t, ctx, fixture, repository, createCommand, token, "drs_pgdeletecasdrift")

	advanced, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordUpdate,
		created.RecordID,
		created.RevisionID,
		created.LockVersion,
		created.AuthorizationEpoch,
		recordsPostgresCompleteRevisionInput(t, "Deletion CAS advanced record"),
		"record-deletion-cas-advance",
	))
	if err != nil {
		t.Fatalf("CommitRevision(advance) error = %v", err)
	}
	if advanced.RevisionID == created.RevisionID || advanced.LockVersion == created.LockVersion {
		t.Fatalf("CommitRevision(advance) = %#v", advanced)
	}

	_, err = repository.ReservePreview(ctx, recordsPostgresDeletionReserveCommand(createCommand, preview, deploymentID))
	// V1 issuance remains closed; the V2 issuer separately exercises revision
	// drift and a post-issue transaction rollback against real writes.
	if !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) {
		t.Fatalf("ReservePreview() drift error = %v, want ErrDeletionSafetyUnavailable", err)
	}
	var (
		reservationState string
		fenceEpoch       int64
		ownerID          string
		ownerGeneration  int64
		operationCount   int
		auditCount       int
		fenceCount       int
		deliveryEpoch    int64
	)
	if err := fixture.db.QueryRow(ctx, `
		select reservation.state,
		       reservation.fence_epoch,
		       reservation.owner_id,
		       reservation.owner_generation,
		       (select count(*)::int from public.record_purge_operations operation
		         where operation.reservation_id = reservation.reservation_id),
		       (select count(*)::int from public.record_deletion_audits audit
		         where audit.operation_id = 'rpo_pgdeletecasdrift'),
		       (select count(*)::int from public.deletion_fence_leases fence
		         where fence.project_id = reservation.project_id
		           and fence.object_kind = reservation.object_kind
		           and fence.object_id = reservation.object_id),
		       epoch.delivery_epoch
		from public.deletion_reservations reservation
		join public.content_delivery_epochs epoch
		  on epoch.project_id = reservation.project_id
		 and epoch.object_kind = reservation.object_kind
		 and epoch.object_id = reservation.object_id
		where reservation.reservation_id = $1`, preview.ReservationID).Scan(
		&reservationState,
		&fenceEpoch,
		&ownerID,
		&ownerGeneration,
		&operationCount,
		&auditCount,
		&fenceCount,
		&deliveryEpoch,
	); err != nil {
		t.Fatalf("read stale deletion preview: %v", err)
	}
	if reservationState != "previewed" || fenceEpoch != 0 || ownerID != "" || ownerGeneration != 0 ||
		operationCount != 0 || auditCount != 0 || fenceCount != 0 || deliveryEpoch != 0 {
		t.Fatalf("stale reserve residue = state %q fence %d owner %q/%d operation/audit/lease %d/%d/%d epoch %d",
			reservationState, fenceEpoch, ownerID, ownerGeneration,
			operationCount, auditCount, fenceCount, deliveryEpoch)
	}
}

func RunV1DeletionDeleteCommit(t *testing.T, newPortabilityAdapter func(*PostgresRecordDeletionRepository) (recorddeletion.Adapter, error)) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	// The processor fixture admits a real namespace and leaves a second record
	// available to prove that the continuation does not affect its neighbor.
	local, neighborID, _, _, _, _, measured, _ := seedV2ProcessedWorkspace(t, ctx, fixture)
	runtimePool := local.pool
	birthFixture := ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}
	recordRepository := newRevisionMutationAdmittedRepository(t, birthFixture)
	draftRepository := newAdmittedCoreDraftRepository(t, runtimePool, fixture.admissionGate)
	created, err := recordRepository.CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, birthFixture,
		recordsPostgresCompleteRevisionInput(t, "Deletion committed record"), "record-deletion-commit-create"))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}
	draft, err := draftRepository.CreateDraft(ctx, records.DraftCreateCommand{
		DraftID:        "rdf_pgdeletecommit",
		ProjectID:      recordauth.ProjectIDDefault,
		RecordID:       created.RecordID,
		BaseRevisionID: created.RevisionID,
		AuthorID:       recordsPostgresDraftAuthorID,
		Payload:        recordsPostgresDraftPayload(t, "Deletion committed draft"),
		Policy:         records.DefaultDraftRetentionPolicy(),
	})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if _, err := draftRepository.PatchDraft(ctx, records.DraftPatchCommand{
		DraftID:  draft.DraftID,
		AuthorID: draft.AuthorID,
		IfMatch:  draft.ETag,
		Payload:  recordsPostgresDraftPayload(t, "Deletion committed draft changed"),
		Policy:   records.DefaultDraftRetentionPolicy(),
	}); err != nil {
		t.Fatalf("PatchDraft() error = %v", err)
	}
	var checkpointCount int
	if err := fixture.db.QueryRow(ctx, `
		select count(*)::int
		from public.record_draft_checkpoints
		where draft_id = $1`, draft.DraftID).Scan(&checkpointCount); err != nil {
		t.Fatalf("count draft checkpoints before deletion: %v", err)
	}
	if checkpointCount != 1 {
		t.Fatalf("checkpoint count before deletion = %d, want 1", checkpointCount)
	}
	archive := seedV2ExportArtifact(t, ctx, fixture, local, created.RecordID)
	if _, err := local.blob.Stat(ctx, archive.Version); err != nil {
		t.Fatalf("published export bytes unavailable before deletion: %v", err)
	}
	// These are real rows on each client/collaboration surface, with matching
	// neighbor rows to detect an accidentally broad purge.
	if _, err := fixture.db.Exec(ctx, `insert into public.markdown_client_draft_buffers
		(user_id,browser_id,record_id,project_id,session_generation,cleanup_generation)
		values ('usr_records1','v1nine',$1,'default',1,0),('usr_records1','v1nine',$2,'default',1,0)`, created.RecordID, neighborID); err != nil {
		t.Fatalf("seed markdown buffers: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.comparison_client_workbenches
		(user_id,browser_id,workbench_digest,destination_record_id,source_record_ids,generation)
		values ('usr_records1','v1nine',decode(repeat('ab',32),'hex'),$1,array[$1],1),
		       ('usr_records1','v1neighbor',decode(repeat('cd',32),'hex'),$2,array[$2],1)`, created.RecordID, neighborID); err != nil {
		t.Fatalf("seed comparison workbenches: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.comparison_client_save_attempts
		(user_id,browser_id,save_digest,destination_record_id,source_record_ids,idempotency_key,generation)
		values ('usr_records1','v1nine',decode(repeat('ab',32),'hex'),$1,array[$1],'v1nine',1),
		       ('usr_records1','v1neighbor',decode(repeat('cd',32),'hex'),$2,array[$2],'v1neighbor',1)`, created.RecordID, neighborID); err != nil {
		t.Fatalf("seed comparison saves: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.record_actions
		(action_id,record_id,action_version,title,status,created_by,updated_by,record_fence_epoch)
		values ('ract_v1nine',$1,1,'Action','open','usr_records1','usr_records1',0),
		       ('ract_v1neighbor',$2,1,'Neighbor','open','usr_records1','usr_records1',0)`, created.RecordID, neighborID); err != nil {
		t.Fatalf("seed collaboration actions: %v", err)
	}
	var targetOutbox, neighborOutbox int64
	if err := fixture.db.QueryRow(ctx, `with inserted as (
		insert into public.record_outbox(project_id,event_kind,subject_kind,subject_id,authorization_epoch,expires_at)
		values ('default','record_action_created','action','ract_v1nine',0,clock_timestamp()+interval '1 hour'),
		       ('default','record_action_created','action','ract_v1neighbor',0,clock_timestamp()+interval '1 hour')
		returning subject_id,outbox_row_id)
		select max(outbox_row_id) filter(where subject_id='ract_v1nine'),
		       max(outbox_row_id) filter(where subject_id='ract_v1neighbor') from inserted`).Scan(&targetOutbox, &neighborOutbox); err != nil {
		t.Fatalf("seed routed outbox: %v", err)
	}
	if _, err := runtimePool.Exec(ctx, `insert into public.browser_record_cleanup_targets(user_id,browser_id,record_id,project_id)
		values ('usr_aaaaaaaaaaaaaaaaaaaaaaaa','v1nine',$1,'default'),('usr_aaaaaaaaaaaaaaaaaaaaaaaa','v1nine',$2,'default')`, created.RecordID, neighborID); err != nil {
		t.Fatalf("register cleanup targets: %v", err)
	}

	deploymentID := recordsPostgresActiveDeletionDeployment(t, ctx, fixture)
	createCommand, token := recordsPostgresDeletionPreviewCommand(t, created, deploymentID)
	repository := NewPostgresRecordDeletionRepository(runtimePool, fixture.admissionGate)
	recordsPostgresAssertV1IssuanceClosed(t, ctx, fixture, repository, createCommand)
	preview := recordsPostgresSeedV1Preview(t, ctx, fixture, repository, createCommand, token, "drs_pgdeletecommit")
	reserveCommand := recordsPostgresDeletionReserveCommand(createCommand, preview, deploymentID)
	reserveCommand.OwnerID = "deletion_commit_worker"
	operation := recordsPostgresSeedV1Fence(t, ctx, fixture, repository, reserveCommand, "rpo_pgdeletecommit")
	workerPool := openV2LaunchBoundPool(t, ctx, fixture, fixture.deletionWorker, "legacy-deletion-purge", measured)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, workerPool); err != nil {
		t.Fatal(err)
	}
	workerRepository := NewPostgresRecordDeletionRepository(workerPool, allowRecordPlatformAdmissionGate)
	claim, err := workerRepository.ClaimDeletionWork(ctx, recorddeletion.DeletionWorkClaimInput{OwnerID: reserveCommand.OwnerID, OwnerLeaseDuration: 2 * time.Minute})
	if err != nil || claim == nil {
		t.Fatalf("ClaimDeletionWork() = %#v, %v", claim, err)
	}
	verifiedLedger := newRecordsPostgresRecoveryLedger(t, ctx, ImportAttachmentGCFixture{platform: fixture})
	entry, proof := verifiedLedger.record(t, ctx, claim.Request)
	coordinator, err := attachments.NewLocalCleanupCoordinator(local.blob, NewPostgresLocalBlobRepository(workerPool, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := workerRepository.ConfigurePortabilityDeletionLocalCleanup(coordinator); err != nil {
		t.Fatal(err)
	}
	attachmentRepository := NewPostgresAttachmentRepository(workerPool)
	if err := attachmentRepository.ConfigureAttachmentDeletionLocalCleanup(coordinator); err != nil {
		t.Fatal(err)
	}
	workerEvidence := NewPostgresEvidenceRepository(workerPool, allowRecordPlatformAdmissionGate, nil)
	runtimeEvidence := NewPostgresEvidenceRepository(runtimePool, allowRecordPlatformAdmissionGate, nil)
	comparison, err := NewPostgresComparisonDeletionStore(runtimeEvidence, workerEvidence)
	if err != nil {
		t.Fatal(err)
	}
	var adapters []recorddeletion.Adapter
	add := func(adapter recorddeletion.Adapter, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		adapters = append(adapters, adapter)
	}
	core, err := recorddeletion.NewCoreAdapter(workerRepository)
	if err != nil {
		t.Fatal(err)
	}
	coreCalled := false
	add(v2CoreHookAdapter{DeletionPurgeAdapter: core, hook: func(target recorddeletion.PurgeTarget) {
		coreCalled = true
		var ready bool
		if err := fixture.db.QueryRow(ctx, `select
			exists(select 1 from public.records where record_id=$2)
			and (select count(*) from public.attachment_purge_receipts where operation_id=$1)=3
			and exists(select 1 from public.evidence_purge_receipts where operation_id=$1 and surface_kind='record_evidence')
			and exists(select 1 from public.record_markdown_client_purge_receipts where operation_id=$1)
			and exists(select 1 from public.record_search_purge_receipts where operation_id=$1)
			and exists(select 1 from public.record_activity_purge_receipts where operation_id=$1)
			and exists(select 1 from public.record_comparison_purge_receipts where operation_id=$1)
			and exists(select 1 from public.record_collaboration_purge_receipts where operation_id=$1)
			and exists(select 1 from public.record_portability_purge_receipts where operation_id=$1)`, target.Operation.OperationID, created.RecordID).Scan(&ready); err != nil || !ready {
			t.Fatalf("core purge preceded other receipts or root disappeared: %t, %v", ready, err)
		}
	}}, nil)
	add(attachments.NewDeletionAdapter(attachmentRepository))
	add(evidence.NewDeletionAdapter(workerEvidence))
	add(recorddeletion.NewMarkdownClientAdapter(workerRepository))
	add(recordsearch.NewDeletionAdapter(workerRepository))
	add(activity.NewDeletionAdapter(workerRepository))
	add(recorddeletion.NewComparisonAdapter(comparison))
	add(recordcollaboration.NewDeletionAdapter(workerRepository))
	add(newPortabilityAdapter(workerRepository))
	registry, err := recorddeletion.NewRegistry(adapters)
	if err != nil {
		t.Fatal(err)
	}
	ledger := &recordsPostgresDeletionLedger{sequence: entry.Sequence, entryHash: entry.EntryHash}
	witness := &recordsPostgresDeletionWitness{proofDigest: proof}
	purger := &recordsPostgresRegistryDeletionPurger{registry: recorddeletion.NewRegistryPurger(registry)}
	worker := recorddeletion.NewDeletionWorker(workerRepository, ledger, witness, purger, recorddeletion.DeletionWorkerOptions{
		OwnerID:            reserveCommand.OwnerID,
		OwnerLeaseDuration: 2 * time.Minute,
		PollInterval:       time.Second,
	})
	expectedStates := []recorddeletion.DeletionState{
		recorddeletion.DeletionStateWitnessPending,
		recorddeletion.DeletionStateDeleteRequested,
		recorddeletion.DeletionStateFencePropagating,
		recorddeletion.DeletionStateReadFenced,
		recorddeletion.DeletionStateOnlinePurging,
	}
	for pass, expected := range expectedStates {
		if expected == recorddeletion.DeletionStateReadFenced {
			probe, err := workerRepository.ClaimDeletionWork(ctx, recorddeletion.DeletionWorkClaimInput{OwnerID: reserveCommand.OwnerID, OwnerLeaseDuration: 2 * time.Minute})
			if err != nil || probe == nil {
				t.Fatalf("ClaimDeletionWork(propagation) = %#v, %v", probe, err)
			}
			if ready, err := workerRepository.PermanentFenceApplied(ctx, *probe); err != nil || !ready {
				t.Fatalf("PermanentFenceApplied() = %t, %v", ready, err)
			}
		}
		if err := worker.RunOnce(ctx); err != nil {
			t.Fatalf("RunOnce() pass %d error = %v", pass, err)
		}
		var state string
		if err := fixture.db.QueryRow(ctx, `
			select operation_state
			from public.record_purge_operations
			where operation_id = $1`, operation.OperationID).Scan(&state); err != nil {
			t.Fatalf("read operation state after pass %d: %v", pass, err)
		}
		if state != string(expected) {
			t.Fatalf("operation state after pass %d = %q, want %q (purge error: %v)", pass, state, expected, purger.lastErr)
		}
	}
	claim, err = workerRepository.ClaimDeletionWork(ctx, recorddeletion.DeletionWorkClaimInput{OwnerID: reserveCommand.OwnerID, OwnerLeaseDuration: 2 * time.Minute})
	if err != nil || claim == nil || claim.Stage != recorddeletion.DeletionWorkPurgeOnline {
		t.Fatalf("ClaimDeletionWork(purge) = %#v, %v", claim, err)
	}
	var denied *pgconn.PgError
	bogus := recorddeletion.OnlinePurgeReceipt{OperationID: operation.OperationID, ReceiptDigest: testStoreRecordPlatformDigest(0x71)}
	if err := workerRepository.CompleteOnlinePurge(ctx, *claim, bogus); !errors.As(err, &denied) || denied.Code != "42501" {
		t.Fatalf("CompleteOnlinePurge(missing receipts) = %v, want 42501", err)
	}
	var currentState, currentOwner string
	var currentGeneration, currentEpoch int64
	var currentExpiry time.Time
	if err := fixture.db.QueryRow(ctx, `select o.operation_state,o.owner_id,o.owner_generation,o.owner_expires_at,e.delivery_epoch from public.record_purge_operations o join public.content_delivery_epochs e on e.project_id=o.project_id and e.object_kind='record' and e.object_id=$2 where o.operation_id=$1`, operation.OperationID, operation.Object.ObjectID).Scan(&currentState, &currentOwner, &currentGeneration, &currentExpiry, &currentEpoch); err != nil || currentState != "online_purging" || currentOwner != claim.Owner.OwnerID || currentGeneration != int64(claim.Owner.Generation) || !currentExpiry.Equal(claim.Owner.ExpiresAt) || currentEpoch != int64(operation.FenceEpoch) {
		t.Fatalf("rejected completion changed operation/owner/epoch: %s %s %d %s %d: %v", currentState, currentOwner, currentGeneration, currentExpiry, currentEpoch, err)
	}
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce(full purge): %v", err)
	}
	if purger.lastErr != nil || purger.receipt.ReceiptDigest == ([32]byte{}) {
		t.Fatalf("registry purge receipt = %#v, %v", purger.receipt, purger.lastErr)
	}

	var (
		rootCount        int
		revisionCount    int
		subjectCount     int
		tagCount         int
		participantCount int
		draftCount       int
		checkpointRows   int
		activityCount    int
		deliveryCount    int
		receiptCount     int
		removedRows      int64
		reservationState string
	)
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.records where record_id = $1),
		       (select count(*)::int from public.record_revisions where record_id = $1),
		       (select count(*)::int from public.record_revision_subjects where revision_id=$4),
		       (select count(*)::int from public.record_revision_tags where revision_id=$4),
		       (select count(*)::int from public.record_revision_participants where revision_id=$4),
		       (select count(*)::int from public.record_drafts where record_id = $1),
		       (select count(*)::int from public.record_draft_checkpoints where draft_id=$5),
		       (select count(*)::int from public.record_domain_activities where record_id = $1),
		       (select count(*)::int
		          from public.content_delivery_epochs
		         where project_id = 'default' and object_kind = 'record' and object_id = $1),
		       (select count(*)::int
		          from public.record_core_purge_receipts
		         where operation_id = $2),
		       (select removed_row_count
		          from public.record_core_purge_receipts
		         where operation_id = $2),
		       (select state
		          from public.deletion_reservations
		         where reservation_id = $3)`,
		created.RecordID,
		operation.OperationID,
		operation.ReservationID,
		created.RevisionID,
		draft.DraftID,
	).Scan(
		&rootCount,
		&revisionCount,
		&subjectCount,
		&tagCount,
		&participantCount,
		&draftCount,
		&checkpointRows,
		&activityCount,
		&deliveryCount,
		&receiptCount,
		&removedRows,
		&reservationState,
	); err != nil {
		t.Fatalf("read committed deletion result: %v", err)
	}
	if rootCount != 0 || revisionCount != 0 || subjectCount != 0 || tagCount != 0 || participantCount != 0 ||
		draftCount != 0 || checkpointRows != 0 || activityCount != 0 || deliveryCount != 0 ||
		receiptCount != 1 || removedRows < 1 || reservationState != "committed" {
		t.Fatalf("committed deletion residue roots/revisions/subjects/tags/participants/drafts/checkpoints/activities/epochs=%d/%d/%d/%d/%d/%d/%d/%d/%d receipt=%d rows=%d reservation=%q",
			rootCount, revisionCount, subjectCount, tagCount, participantCount,
			draftCount, checkpointRows, activityCount, deliveryCount,
			receiptCount, removedRows, reservationState)
	}
	if !coreCalled || ledger.appendCalls != 1 || ledger.resolveCalls != 0 || witness.calls != 1 || purger.calls != 1 {
		t.Fatalf("external cut points append/resolve/witness/purge=%d/%d/%d/%d, want 1/0/1/1",
			ledger.appendCalls, ledger.resolveCalls, witness.calls, purger.calls)
	}
	var terminalState, terminalOwner string
	var terminalGeneration int64
	var terminalExpiry *time.Time
	var terminalDigest []byte
	if err := fixture.db.QueryRow(ctx, `select operation_state,owner_id,owner_generation,owner_expires_at,receipt_digest from public.record_purge_operations where operation_id=$1`, operation.OperationID).Scan(&terminalState, &terminalOwner, &terminalGeneration, &terminalExpiry, &terminalDigest); err != nil || terminalState != "online_purged" || terminalOwner != "" || terminalGeneration != 0 || terminalExpiry != nil || !bytes.Equal(terminalDigest, purger.receipt.ReceiptDigest[:]) {
		t.Fatalf("terminal operation = %s %s %d %v digest=%x: %v", terminalState, terminalOwner, terminalGeneration, terminalExpiry, terminalDigest, err)
	}
	for _, table := range []string{"record_core_purge_receipts", "record_markdown_client_purge_receipts", "record_search_purge_receipts", "record_activity_purge_receipts", "record_comparison_purge_receipts", "record_collaboration_purge_receipts", "record_portability_purge_receipts"} {
		var count int
		if err := fixture.db.QueryRow(ctx, `select count(*) from public.`+table+` where operation_id=$1`, operation.OperationID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s receipt count = %d: %v", table, count, err)
		}
	}
	var attachmentReceipts, evidenceReceipts int
	if err := fixture.db.QueryRow(ctx, `select (select count(distinct surface_kind) from public.attachment_purge_receipts where operation_id=$1 and surface_kind in ('logical_attachment','upload_part','blob_object')), (select count(*) from public.evidence_purge_receipts where operation_id=$1 and surface_kind='record_evidence')`, operation.OperationID).Scan(&attachmentReceipts, &evidenceReceipts); err != nil || attachmentReceipts != 3 || evidenceReceipts != 1 {
		t.Fatalf("attachment/evidence receipts = %d/%d: %v", attachmentReceipts, evidenceReceipts, err)
	}
	if _, err := local.blob.Stat(ctx, archive.Version); !errors.Is(err, attachments.ErrBlobNotFound) {
		t.Fatalf("published export still exists after purge: %v", err)
	}
	var physicalProof bool
	if err := fixture.db.QueryRow(ctx, `select exists(
		select 1 from public.record_portability_v1_artifacts a
		join public.record_portability_v1_targets t on (t.operation_id,t.target_id)=(a.operation_id,a.target_id)
		join public.blob_gc_deletions g on g.deletion_id=t.local_deletion_id
		join public.record_portability_v1_obligations captured on (captured.operation_id,captured.target_id,captured.obligation_id)=(t.operation_id,t.target_id,$3)
		join public.record_write_obligations o on o.obligation_id=captured.obligation_id
		where a.operation_id=$1 and a.source_kind='export' and a.artifact_id='rxa_v2exclusive'
		and t.disposition='local' and t.blob_key=$2 and t.object_version_id=$4
		and g.operation_id=$1 and g.local_contract_version=2 and g.deletion_state='completed'
		and g.physical_delete_result='deleted' and g.final_identity_bound and g.final_was_present
		and g.object_version=$4 and g.final_device is not null and g.final_inode is not null
		and octet_length(g.inventory_digest)=32 and octet_length(g.receipt_digest)=32
		and g.completion_command is not null and o.cleanup_completed_at is not null)
		and not exists(select 1 from public.record_export_artifacts where export_artifact_id='rxa_v2exclusive')`,
		operation.OperationID, archive.Version.Key, archive.ObligationID, archive.Version.VersionID).Scan(&physicalProof); err != nil || !physicalProof {
		t.Fatalf("V1 export physical proof missing: %t, %v", physicalProof, err)
	}
	var surfacesValid bool
	if err := fixture.db.QueryRow(ctx, `select
		not exists(select 1 from public.markdown_client_draft_buffers where record_id=$2)
		and not exists(select 1 from public.comparison_client_workbenches where destination_record_id=$2 or $2=any(source_record_ids))
		and not exists(select 1 from public.comparison_client_save_attempts where destination_record_id=$2 or $2=any(source_record_ids))
		and not exists(select 1 from public.record_actions where record_id=$2)
		and exists(select 1 from public.record_outbox where outbox_row_id=$4 and record_id=$2 and status='cancelled')
		and exists(select 1 from public.records where record_id=$3)
		and exists(select 1 from public.markdown_client_draft_buffers where record_id=$3 and browser_id='v1nine')
		and exists(select 1 from public.comparison_client_workbenches where destination_record_id=$3 and browser_id='v1neighbor')
		and exists(select 1 from public.comparison_client_save_attempts where destination_record_id=$3 and browser_id='v1neighbor')
		and exists(select 1 from public.record_actions where record_id=$3 and action_id='ract_v1neighbor')
		and exists(select 1 from public.record_outbox where outbox_row_id=$5 and status='pending')
		and exists(select 1 from public.browser_record_cleanup_events where operation_id=$1 and project_id='default' and record_id=$2
			and cleanup_epoch=$6 and scopes=array['markdown_draft_buffer','markdown_workspace','comparison_workbench','comparison_save_attempt']::text[])
		and exists(select 1 from public.browser_record_cleanup_targets where user_id='usr_aaaaaaaaaaaaaaaaaaaaaaaa' and browser_id='v1nine' and record_id=$2)
		and exists(select 1 from public.browser_record_cleanup_targets where user_id='usr_aaaaaaaaaaaaaaaaaaaaaaaa' and browser_id='v1nine' and record_id=$3)
		and not exists(select 1 from public.record_deletion_gates where operation_id=$1)
		and (select removed_row_count from public.record_markdown_client_purge_receipts where operation_id=$1)>0
		and (select removed_row_count from public.record_comparison_purge_receipts where operation_id=$1)>0
		and (select removed_row_count from public.record_collaboration_purge_receipts where operation_id=$1)>0`,
		operation.OperationID, created.RecordID, neighborID, targetOutbox, neighborOutbox, int64(operation.FenceEpoch)).Scan(&surfacesValid); err != nil || !surfacesValid {
		t.Fatalf("V1 client, collaboration, outbox or neighbor result invalid: %t, %v", surfacesValid, err)
	}
	cleanup := NewPostgresRecordClientCleanupRepository(runtimePool, fixture.admissionGate, newRecordsPostgresAuthorization(t, runtimePool, fixture.admissionGate))
	actor := recordauth.ActorScope{UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", ProjectID: recordauth.ProjectIDDefault, Role: recordauth.RoleProjectAdmin}
	event, err := cleanup.Next(ctx, actor, "v1nine")
	if err != nil || event == nil || event.OperationID != operation.OperationID || event.RecordID != created.RecordID {
		t.Fatalf("root-independent cleanup event = %#v, %v", event, err)
	}
	if err := cleanup.Acknowledge(ctx, actor, "v1nine", event.Acknowledgement()); err != nil {
		t.Fatalf("acknowledge root-independent cleanup: %v", err)
	}
	if next, err := cleanup.Next(ctx, actor, "v1nine"); err != nil || next != nil {
		t.Fatalf("acknowledged cleanup event replay = %#v, %v", next, err)
	}
	var ackPersisted bool
	if err := fixture.db.QueryRow(ctx, `select exists(select 1 from public.browser_record_cleanup_receipts
		where user_id='usr_aaaaaaaaaaaaaaaaaaaaaaaa' and browser_id='v1nine' and operation_id=$1 and record_id=$2 and cleanup_epoch=$3)`, operation.OperationID, created.RecordID, int64(operation.FenceEpoch)).Scan(&ackPersisted); err != nil || !ackPersisted {
		t.Fatalf("root-independent cleanup acknowledgement missing: %t, %v", ackPersisted, err)
	}
	replayed, err := repository.ResolvePreview(ctx, recorddeletion.PreviewLookup{ReservationID: preview.ReservationID, Object: preview.Object, Token: token})
	if err != nil || replayed.Operation == nil || replayed.Operation.State != recorddeletion.DeletionStateOnlinePurged || replayed.Operation.ReceiptDigest != purger.receipt.ReceiptDigest {
		t.Fatalf("terminal replay = %#v: %v", replayed, err)
	}
	if err := worker.RunOnce(ctx); err != nil || purger.calls != 1 {
		t.Fatalf("terminal worker replay invoked purge or failed: calls=%d error=%v", purger.calls, err)
	}
	var replayState, replayOwner string
	var replayGeneration int64
	var replayExpiry *time.Time
	var replayDigest []byte
	if err := fixture.db.QueryRow(ctx, `select operation_state,owner_id,owner_generation,owner_expires_at,receipt_digest
		from public.record_purge_operations where operation_id=$1`, operation.OperationID).Scan(&replayState, &replayOwner, &replayGeneration, &replayExpiry, &replayDigest); err != nil ||
		replayState != terminalState || replayOwner != terminalOwner || replayGeneration != terminalGeneration || replayExpiry != nil || !bytes.Equal(replayDigest, terminalDigest) {
		t.Fatalf("terminal state changed after worker replay: %s %s %d %v digest=%x: %v", replayState, replayOwner, replayGeneration, replayExpiry, replayDigest, err)
	}
}

func TestPostgresIntegrationRecordDeletionV1PermanentFenceAuthority(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	runtime := fixture.openDirectRuntimePool(t, ctx, "v1-fence-authority", 2)
	created, err := newRevisionMutationAdmittedRepository(t, ImportAttachmentGCFixture{Runtime: runtime, Gate: fixture.admissionGate}).CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtime, Gate: fixture.admissionGate}, recordsPostgresCompleteRevisionInput(t, "V1 fence authority"), "v1-fence-authority"))
	if err != nil {
		t.Fatal(err)
	}
	deployment := recordsPostgresActiveDeletionDeployment(t, ctx, fixture)
	create, token := recordsPostgresDeletionPreviewCommand(t, created, deployment)
	runtimeRepository := NewPostgresRecordDeletionRepository(runtime, fixture.admissionGate)
	recordsPostgresAssertV1IssuanceClosed(t, ctx, fixture, runtimeRepository, create)
	preview := recordsPostgresSeedV1Preview(t, ctx, fixture, runtimeRepository, create, token, "drs_v1fenceauthority")
	reserve := recordsPostgresDeletionReserveCommand(create, preview, deployment)
	reserve.OwnerID = "v1_fence_authority"
	operation := recordsPostgresSeedV1Fence(t, ctx, fixture, runtimeRepository, reserve, "rpo_v1fenceauthority")
	workerPool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "v1-fence-authority-worker", 2)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, workerPool); err != nil {
		t.Fatal(err)
	}
	workerRepository := NewPostgresRecordDeletionRepository(workerPool, allowRecordPlatformAdmissionGate)
	input := recorddeletion.DeletionWorkClaimInput{OwnerID: reserve.OwnerID, OwnerLeaseDuration: 2 * time.Minute}
	claim, err := workerRepository.ClaimDeletionWork(ctx, input)
	if err != nil || claim == nil {
		t.Fatalf("claim historical operation: %#v %v", claim, err)
	}
	verified := newRecordsPostgresRecoveryLedger(t, ctx, ImportAttachmentGCFixture{platform: fixture})
	entry, proof := verified.record(t, ctx, claim.Request)
	worker := recorddeletion.NewDeletionWorker(workerRepository, &recordsPostgresDeletionLedger{sequence: entry.Sequence, entryHash: entry.EntryHash}, &recordsPostgresDeletionWitness{proofDigest: proof}, recordsPostgresUnexpectedDeletionPurger{}, recorddeletion.DeletionWorkerOptions{OwnerID: reserve.OwnerID, OwnerLeaseDuration: 2 * time.Minute})
	for i := range 3 {
		if err := worker.RunOnce(ctx); err != nil {
			t.Fatalf("advance to propagation %d: %v", i, err)
		}
	}
	claim, err = workerRepository.ClaimDeletionWork(ctx, input)
	if err != nil || claim == nil || claim.Stage != recorddeletion.DeletionWorkPropagatePermanentFence {
		t.Fatalf("claim propagation: %#v %v", claim, err)
	}
	snapshot := func() string {
		t.Helper()
		var state string
		err := fixture.db.QueryRow(ctx, `select jsonb_build_array((select to_jsonb(o) from public.record_purge_operations o where o.operation_id=$1),(select to_jsonb(r) from public.deletion_reservations r where r.reservation_id=$2),(select to_jsonb(f) from public.deletion_fence_leases f where f.object_id=$3),(select to_jsonb(e) from public.content_delivery_epochs e where e.object_id=$3),(select coalesce(jsonb_agg(to_jsonb(a) order by a.audit_id),'[]'::jsonb) from public.record_deletion_audits a where a.operation_id=$1),(select to_jsonb(c) from public.object_content_leases c where c.object_id=$3))::text`, operation.OperationID, operation.ReservationID, operation.Object.ObjectID).Scan(&state)
		if err != nil {
			t.Fatal(err)
		}
		return state
	}
	check := func(label string, candidate recorddeletion.ClaimedDeletionWork, want bool, denied bool) {
		t.Helper()
		before := snapshot()
		ready, err := workerRepository.PermanentFenceApplied(ctx, candidate)
		var pgerr *pgconn.PgError
		if denied {
			if !errors.As(err, &pgerr) || pgerr.Code != "42501" {
				t.Fatalf("%s = %t, %v; want 42501", label, ready, err)
			}
		} else if err != nil || ready != want {
			t.Fatalf("%s = %t, %v; want %t, nil", label, ready, err, want)
		}
		if after := snapshot(); after != before {
			t.Fatalf("%s changed business rows: before=%s after=%s", label, before, after)
		}
	}
	check("drained", *claim, true, false)
	if _, err := fixture.db.Exec(ctx, `update public.deletion_fence_leases set expires_at=clock_timestamp()+interval '1 minute' where object_id=$1`, operation.Object.ObjectID); err != nil {
		t.Fatal(err)
	}
	check("live deletion fence", *claim, false, false)
	if _, err := fixture.db.Exec(ctx, `update public.deletion_fence_leases set created_at=clock_timestamp()-interval '2 minutes',expires_at=clock_timestamp()-interval '1 minute' where object_id=$1`, operation.Object.ObjectID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.object_content_leases(project_id,object_kind,object_id,owner_id,owner_generation,expires_at) values('default','record',$1,'v1_test_content',1,clock_timestamp()+interval '1 minute')`, operation.Object.ObjectID); err != nil {
		t.Fatal(err)
	}
	check("live content lease", *claim, false, false)
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatalf("worker pending propagation: %v", err)
	}
	claim, err = workerRepository.ClaimDeletionWork(ctx, input)
	if err != nil || claim == nil || claim.Stage != recorddeletion.DeletionWorkPropagatePermanentFence || claim.Operation.State != recorddeletion.DeletionStateFencePropagating {
		t.Fatalf("pending propagation changed state or entered retry: %#v %v", claim, err)
	}
	if _, err := fixture.db.Exec(ctx, `delete from public.object_content_leases where object_id=$1`, operation.Object.ObjectID); err != nil {
		t.Fatal(err)
	}
	check("content drained", *claim, true, false)
	for name, mutate := range map[string]func(*recorddeletion.ClaimedDeletionWork){
		"operation": func(c *recorddeletion.ClaimedDeletionWork) {
			c.Operation.OperationID = "rpo_wrong"
			c.Request.OperationID = c.Operation.OperationID
		},
		"reservation": func(c *recorddeletion.ClaimedDeletionWork) { c.Operation.ReservationID = "drs_wrong" },
		"record": func(c *recorddeletion.ClaimedDeletionWork) {
			c.Operation.Object.ObjectID = "rec_wrong"
			c.Request.Object = c.Operation.Object
		},
		"epoch":      func(c *recorddeletion.ClaimedDeletionWork) { c.Operation.FenceEpoch++ },
		"hash":       func(c *recorddeletion.ClaimedDeletionWork) { c.Operation.LedgerEntryHash[0] ^= 1 },
		"owner":      func(c *recorddeletion.ClaimedDeletionWork) { c.Owner.OwnerID = "wrong_owner" },
		"generation": func(c *recorddeletion.ClaimedDeletionWork) { c.Owner.Generation++ },
		"expiry":     func(c *recorddeletion.ClaimedDeletionWork) { c.Owner.ExpiresAt = c.Owner.ExpiresAt.Add(-time.Hour) },
	} {
		candidate := *claim
		mutate(&candidate)
		check(name, candidate, false, true)
	}
	var expired time.Time
	if err := fixture.db.QueryRow(ctx, `update public.record_purge_operations set owner_expires_at=clock_timestamp()-interval '1 minute' where operation_id=$1 returning owner_expires_at`, operation.OperationID).Scan(&expired); err != nil {
		t.Fatal(err)
	}
	expiredClaim := *claim
	expiredClaim.Owner.ExpiresAt = expired
	check("expired persisted owner", expiredClaim, false, true)
	takeover, err := workerRepository.ClaimDeletionWork(ctx, recorddeletion.DeletionWorkClaimInput{OwnerID: "v1_fence_takeover", OwnerLeaseDuration: 2 * time.Minute})
	if err != nil || takeover == nil || takeover.Owner.Generation != claim.Owner.Generation+1 {
		t.Fatalf("claim takeover = %#v, %v", takeover, err)
	}
	check("prior owner after takeover", *claim, false, true)
	claim = takeover
	var workerExecute, runtimeExecute, adminExecute, publicExecute, workerSelect bool
	if err := fixture.db.QueryRow(ctx, `select has_function_privilege($1,'public.record_deletion_permanent_fence_applied_v1(bytea)','execute'),has_function_privilege($2,'public.record_deletion_permanent_fence_applied_v1(bytea)','execute'),has_function_privilege($3,'public.record_deletion_permanent_fence_applied_v1(bytea)','execute'),exists(select 1 from pg_proc p cross join lateral aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a where p.oid='public.record_deletion_permanent_fence_applied_v1(bytea)'::regprocedure and a.grantee=0 and a.privilege_type='EXECUTE'),has_table_privilege($1,'public.record_purge_operations','select')`, fixture.deletionWorker, fixture.runtime, fixture.admin).Scan(&workerExecute, &runtimeExecute, &adminExecute, &publicExecute, &workerSelect); err != nil || !workerExecute || runtimeExecute || adminExecute || publicExecute || workerSelect {
		t.Fatalf("fence ACL worker/runtime/admin/public/select=%t/%t/%t/%t/%t: %v", workerExecute, runtimeExecute, adminExecute, publicExecute, workerSelect, err)
	}
	ledgerEnvelope, err := deletionV1OperationLedger(*claim)
	if err != nil {
		t.Fatal(err)
	}
	body, err := encodeRecordPurgeCommand(ledgerEnvelope)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{fixture.runtime, fixture.admin} {
		pool := fixture.openDirectRolePool(t, ctx, role, "v1-fence-denied", 1)
		before := snapshot()
		var denied *pgconn.PgError
		if _, err := pool.Exec(ctx, `select public.record_deletion_permanent_fence_applied_v1($1)`, body); !errors.As(err, &denied) || denied.Code != "42501" {
			t.Fatalf("role %s fence wrapper = %v, want 42501", role, err)
		}
		if after := snapshot(); after != before {
			t.Fatalf("role %s denial changed business rows", role)
		}
	}
	var denied *pgconn.PgError
	if _, err := workerPool.Exec(ctx, `select 1 from public.record_purge_operations limit 1`); !errors.As(err, &denied) || denied.Code != "42501" {
		t.Fatalf("worker direct operation select = %v, want 42501", err)
	}
	if err := workerRepository.MarkReadFenced(ctx, *claim); err != nil {
		t.Fatalf("mark drained fence propagated: %v", err)
	}
	check("already read fenced", *claim, false, true)
}

func TestPostgresIntegrationRecordDeletionNotCommittedTakeoverRejectsStaleOwnerAndRestoresRead(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-deletion-not-committed", 2)
	birthFixture := ImportAttachmentGCFixture{Runtime: runtimePool, Gate: fixture.admissionGate}
	recordRepository := newRevisionMutationAdmittedRepository(t, birthFixture)
	created, err := recordRepository.CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, birthFixture,
		recordsPostgresCompleteRevisionInput(t, "Deletion not committed record"), "record-deletion-not-committed-create"))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}

	deploymentID := recordsPostgresActiveDeletionDeployment(t, ctx, fixture)
	createCommand, token := recordsPostgresDeletionPreviewCommand(t, created, deploymentID)
	repository := NewPostgresRecordDeletionRepository(runtimePool, fixture.admissionGate)
	recordsPostgresAssertV1IssuanceClosed(t, ctx, fixture, repository, createCommand)
	preview := recordsPostgresSeedV1Preview(t, ctx, fixture, repository, createCommand, token, "drs_pgdeletenotcommitted")
	reserveCommand := recordsPostgresDeletionReserveCommand(createCommand, preview, deploymentID)
	reserveCommand.OwnerID = "deletion_original_worker"
	operation := recordsPostgresSeedV1Fence(t, ctx, fixture, repository, reserveCommand, "rpo_pgdeletenotcommitted")
	workerRepository := recordsPostgresV1DeletionWorker(t, ctx, fixture)

	initialClaim, err := workerRepository.ClaimDeletionWork(ctx, recorddeletion.DeletionWorkClaimInput{
		OwnerID: reserveCommand.OwnerID, OwnerLeaseDuration: 2 * time.Minute,
	})
	if err != nil || initialClaim == nil {
		t.Fatalf("ClaimDeletionWork(initial) = %#v, %v", initialClaim, err)
	}
	if err := workerRepository.MarkDeleteCommitUnknown(ctx, *initialClaim); err != nil {
		t.Fatalf("MarkDeleteCommitUnknown() error = %v", err)
	}
	staleClaim, err := workerRepository.ClaimDeletionWork(ctx, recorddeletion.DeletionWorkClaimInput{
		OwnerID: reserveCommand.OwnerID, OwnerLeaseDuration: 2 * time.Minute,
	})
	if err != nil || staleClaim == nil || staleClaim.Stage != recorddeletion.DeletionWorkResolveDeleteCommit {
		t.Fatalf("ClaimDeletionWork(stale token) = %#v, %v", staleClaim, err)
	}
	var expiredOperationCount int
	var expiredReservationCount int
	var expiredFenceCount int
	if err := fixture.db.QueryRow(ctx, `
		with expiry as materialized (
			select transaction_timestamp() - interval '1 second' as expired_at
		), expired_operation as (
			update public.record_purge_operations operation
			set owner_expires_at = expiry.expired_at
			from expiry
			where operation.operation_id = $1
			returning 1
		), expired_reservation as (
			update public.deletion_reservations reservation
			set owner_expires_at = expiry.expired_at
			from expiry
			where reservation.reservation_id = $2
			returning 1
		), expired_fence as (
			update public.deletion_fence_leases fence
			set created_at = least(fence.created_at, expiry.expired_at - interval '1 second'),
			    expires_at = expiry.expired_at
			from expiry
			where fence.project_id = 'default'
			  and fence.object_kind = 'record'
			  and fence.object_id = $3
			returning 1
		)
		select (select count(*)::int from expired_operation),
		       (select count(*)::int from expired_reservation),
		       (select count(*)::int from expired_fence)`,
		operation.OperationID,
		operation.ReservationID,
		operation.Object.ObjectID,
	).Scan(&expiredOperationCount, &expiredReservationCount, &expiredFenceCount); err != nil {
		t.Fatalf("expire deletion owner lease: %v", err)
	}
	if expiredOperationCount != 1 || expiredReservationCount != 1 || expiredFenceCount != 1 {
		t.Fatalf("expired owner rows operation/reservation/fence=%d/%d/%d, want 1/1/1",
			expiredOperationCount, expiredReservationCount, expiredFenceCount)
	}

	takeoverOwner := "deletion_takeover_worker"
	takeoverClaim, err := workerRepository.ClaimDeletionWork(ctx, recorddeletion.DeletionWorkClaimInput{
		OwnerID: takeoverOwner, OwnerLeaseDuration: 2 * time.Minute,
	})
	if err != nil || takeoverClaim == nil || takeoverClaim.Stage != recorddeletion.DeletionWorkResolveDeleteCommit {
		t.Fatalf("ClaimDeletionWork(takeover) = %#v, %v", takeoverClaim, err)
	}
	if takeoverClaim.Owner.Generation != staleClaim.Owner.Generation+1 {
		t.Fatalf("takeover generation = %d, want %d", takeoverClaim.Owner.Generation, staleClaim.Owner.Generation+1)
	}
	staleEntry := recorddeletion.DeletionLedgerEntry{
		Request:   staleClaim.Request,
		Sequence:  201,
		EntryHash: testStoreRecordPlatformDigest(0x91),
	}
	var staleError *pgconn.PgError
	if err := workerRepository.RecordDeleteEntry(ctx, *staleClaim, staleEntry); !errors.As(err, &staleError) || staleError.Code != "42501" {
		t.Fatalf("RecordDeleteEntry(stale owner) error = %v, want permission denied", err)
	}

	// The ledger resolution is a test double; it does not prove source absence.
	absenceProof, err := recorddeletion.NewDeletionAbsenceProof(1, testStoreRecordPlatformDigest(0x92))
	if err != nil {
		t.Fatalf("NewDeletionAbsenceProof() error = %v", err)
	}
	verifiedLedger := newRecordsPostgresRecoveryLedger(t, ctx, ImportAttachmentGCFixture{platform: fixture})
	entry, proof := verifiedLedger.record(t, ctx, takeoverClaim.Request.AttemptNotCommitted(1))
	ledger := &recordsPostgresDeletionLedger{
		sequence:   entry.Sequence,
		entryHash:  entry.EntryHash,
		resolution: recorddeletion.NewAbsenceProvenLedgerResolution(absenceProof),
	}
	witness := &recordsPostgresDeletionWitness{proofDigest: proof}
	worker := recorddeletion.NewDeletionWorker(
		workerRepository,
		ledger,
		witness,
		recordsPostgresUnexpectedDeletionPurger{},
		recorddeletion.DeletionWorkerOptions{
			OwnerID: takeoverOwner, OwnerLeaseDuration: 2 * time.Minute, PollInterval: time.Second,
		},
	)
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce(outcome append) error = %v", err)
	}

	var (
		pendingOperationState   string
		pendingReservationState string
		pendingFenceCount       int
		pendingRootCount        int
		pendingDeliveryCount    int
	)
	if err := fixture.db.QueryRow(ctx, `
		select operation.operation_state,
		       reservation.state,
		       (select count(*)::int
		          from public.deletion_fence_leases fence
		         where fence.project_id = reservation.project_id
		           and fence.object_kind = reservation.object_kind
		           and fence.object_id = reservation.object_id),
		       (select count(*)::int from public.records where record_id = reservation.object_id),
		       (select count(*)::int
		          from public.content_delivery_epochs epoch
		         where epoch.project_id = reservation.project_id
		           and epoch.object_kind = reservation.object_kind
		           and epoch.object_id = reservation.object_id)
		from public.record_purge_operations operation
		join public.deletion_reservations reservation
		  on reservation.reservation_id = operation.reservation_id
		where operation.operation_id = $1`, operation.OperationID).Scan(
		&pendingOperationState,
		&pendingReservationState,
		&pendingFenceCount,
		&pendingRootCount,
		&pendingDeliveryCount,
	); err != nil {
		t.Fatalf("read release-pending state: %v", err)
	}
	if pendingOperationState != string(recorddeletion.DeletionStateReleasePending) || pendingReservationState != "fenced" ||
		pendingFenceCount != 1 || pendingRootCount != 1 || pendingDeliveryCount != 1 {
		t.Fatalf("release-pending state operation/reservation=%q/%q fence/root/epoch=%d/%d/%d",
			pendingOperationState, pendingReservationState, pendingFenceCount, pendingRootCount, pendingDeliveryCount)
	}
	if _, err := recordRepository.ReadRecordRevision(ctx, records.StoredRecordRevisionRequest{
		RecordID:           created.RecordID,
		RevisionID:         created.RevisionID,
		CurrentRevisionID:  created.RevisionID,
		LockVersion:        created.LockVersion,
		AuthorizationEpoch: created.AuthorizationEpoch,
	}); !errors.Is(err, records.ErrRecordDeletionReserved) {
		t.Fatalf("ReadRecordRevision(release pending) error = %v, want ErrRecordDeletionReserved", err)
	}

	if err := worker.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce(outcome witness) error = %v", err)
	}
	var (
		terminalOperationState   string
		terminalReservationState string
		operationReleaseEpoch    int64
		reservationReleaseEpoch  int64
		terminalActiveFenceCount int
		terminalRootCount        int
		terminalDeliveryCount    int
	)
	if err := fixture.db.QueryRow(ctx, `
		select operation.operation_state,
		       reservation.state,
		       operation.release_epoch,
		       reservation.release_epoch,
		       (select count(*)::int
		          from public.deletion_fence_leases fence
		         where fence.project_id = reservation.project_id
		           and fence.object_kind = reservation.object_kind
		           and fence.object_id = reservation.object_id
		           and fence.expires_at > transaction_timestamp()),
		       (select count(*)::int from public.records where record_id = reservation.object_id),
		       (select count(*)::int
		          from public.content_delivery_epochs epoch
		         where epoch.project_id = reservation.project_id
		           and epoch.object_kind = reservation.object_kind
		           and epoch.object_id = reservation.object_id)
		from public.record_purge_operations operation
		join public.deletion_reservations reservation
		  on reservation.reservation_id = operation.reservation_id
		where operation.operation_id = $1`, operation.OperationID).Scan(
		&terminalOperationState,
		&terminalReservationState,
		&operationReleaseEpoch,
		&reservationReleaseEpoch,
		&terminalActiveFenceCount,
		&terminalRootCount,
		&terminalDeliveryCount,
	); err != nil {
		t.Fatalf("read not-committed state: %v", err)
	}
	if terminalOperationState != string(recorddeletion.DeletionStateNotCommitted) || terminalReservationState != "not_committed" ||
		operationReleaseEpoch != 1 || reservationReleaseEpoch != 1 || terminalActiveFenceCount != 0 ||
		terminalRootCount != 1 || terminalDeliveryCount != 1 {
		t.Fatalf("not-committed state operation/reservation=%q/%q release=%d/%d fence/root/epoch=%d/%d/%d",
			terminalOperationState, terminalReservationState, operationReleaseEpoch, reservationReleaseEpoch,
			terminalActiveFenceCount, terminalRootCount, terminalDeliveryCount)
	}
	if _, err := recordRepository.ReadRecordRevision(ctx, records.StoredRecordRevisionRequest{
		RecordID:           created.RecordID,
		RevisionID:         created.RevisionID,
		CurrentRevisionID:  created.RevisionID,
		LockVersion:        created.LockVersion,
		AuthorizationEpoch: created.AuthorizationEpoch,
	}); err != nil {
		t.Fatalf("ReadRecordRevision(not committed) error = %v", err)
	}
	replayed, err := repository.ResolvePreview(ctx, recorddeletion.PreviewLookup{
		ReservationID: preview.ReservationID,
		Object:        preview.Object,
		Token:         token,
	})
	if err != nil || replayed.Operation == nil || replayed.Operation.State != recorddeletion.DeletionStateNotCommitted {
		t.Fatalf("ResolvePreview(not committed) = %#v, %v", replayed, err)
	}
	if ledger.resolveCalls != 1 || ledger.appendCalls != 1 || witness.calls != 1 {
		t.Fatalf("outcome cut points resolve/append/witness=%d/%d/%d, want 1/1/1",
			ledger.resolveCalls, ledger.appendCalls, witness.calls)
	}
}

func recordsPostgresV1DeletionWorker(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture) *PostgresRecordDeletionRepository {
	t.Helper()
	pool := fixture.openDirectRolePool(t, ctx, fixture.deletionWorker, "legacy-deletion-worker", 2)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
		t.Fatalf("admit deletion worker: %v", err)
	}
	return NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
}

// These fixtures install predecessor state for the current continuation paths.
func recordsPostgresAssertV1IssuanceClosed(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, repository *PostgresRecordDeletionRepository, command recorddeletion.CreatePreviewCommand) {
	t.Helper()
	preview, err := repository.CreatePreview(ctx, command)
	if !errors.Is(err, recorddeletion.ErrDeletionSafetyUnavailable) || preview != (recorddeletion.StoredPreview{}) {
		t.Fatalf("CreatePreview() = %#v, %v, want empty and ErrDeletionSafetyUnavailable", preview, err)
	}
	var reservations, operations, fences, audits int
	var epoch int64
	if err := fixture.db.QueryRow(ctx, `select
		(select count(*) from public.deletion_reservations where project_id=$1 and object_kind=$2 and object_id=$3),
		(select count(*) from public.record_purge_operations),
		(select count(*) from public.deletion_fence_leases where project_id=$1 and object_kind=$2 and object_id=$3),
		(select count(*) from public.record_deletion_audits),
		(select delivery_epoch from public.content_delivery_epochs where project_id=$1 and object_kind=$2 and object_id=$3)`,
		command.Object.ProjectID, command.Object.ObjectKind, command.Object.ObjectID).Scan(&reservations, &operations, &fences, &audits, &epoch); err != nil {
		t.Fatalf("read rejected issuance state: %v", err)
	}
	if reservations != 0 || operations != 0 || fences != 0 || audits != 0 || epoch != 0 {
		t.Fatalf("rejected issuance wrote reservation/operation/fence/audit/epoch = %d/%d/%d/%d/%d", reservations, operations, fences, audits, epoch)
	}
}

func recordsPostgresSeedV1Preview(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, repository *PostgresRecordDeletionRepository, command recorddeletion.CreatePreviewCommand, token recordplatform.DeletionRequestTokenTransportV1, reservationID string) recorddeletion.StoredPreview {
	t.Helper()
	fingerprint, err := command.RequestFingerprint.PersistedBytes()
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.db.Exec(ctx, `insert into public.deletion_reservations (
		reservation_id, project_id, object_kind, object_id, deletion_token_commitment, request_fingerprint,
		state, fence_epoch, owner_id, owner_generation, expires_at, actor_scope_digest, preview_binding_digest,
		preview_current_revision_id, preview_lock_version, preview_authorization_epoch, preview_content_delivery_epoch,
		preview_dependency_graph_digest, preview_backup_inventory_digest, preview_processor_inventory_digest,
		adapter_readiness_digest, adapter_preview_digest, preview_witness_sequence, preview_witness_entry_hash, deletion_contract_version
	) values ($1,$2,$3,$4,$5,$6,'previewed',0,'',0,transaction_timestamp()+interval '5 minutes',
		$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,1)`,
		reservationID, command.Object.ProjectID, command.Object.ObjectKind, command.Object.ObjectID,
		command.TokenCommitment[:], fingerprint[:], command.ActorScopeDigest[:], command.BindingDigest[:],
		command.Record.CurrentRevisionID, int64(command.Record.LockVersion), int64(command.Record.AuthorizationEpoch),
		int64(command.Record.ContentDeliveryEpoch), command.Record.DependencyGraphDigest[:], command.Record.BackupInventoryDigest[:],
		command.Record.ProcessorInventoryDigest[:], command.AdapterReadinessDigest[:], command.AdapterPreviewDigest[:],
		int64(command.WitnessHead.Sequence), command.WitnessHead.EntryHash[:])
	if err != nil {
		t.Fatalf("seed historical V1 preview: %v", err)
	}
	preview, err := repository.ResolvePreview(ctx, recorddeletion.PreviewLookup{ReservationID: reservationID, Object: command.Object, Token: token})
	if err != nil {
		t.Fatalf("resolve historical V1 preview: %v", err)
	}
	return preview
}

func recordsPostgresSeedV1Fence(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, repository *PostgresRecordDeletionRepository, command recorddeletion.ReservePreviewCommand, operationID string) recorddeletion.DeletionOperation {
	t.Helper()
	tx, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var expires time.Time
	if err := tx.QueryRow(ctx, `select transaction_timestamp()+interval '5 minutes'`).Scan(&expires); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `update public.deletion_reservations set state='fenced',fence_epoch=1,owner_id=$2,owner_generation=1,owner_expires_at=$3 where reservation_id=$1`, command.Preview.ReservationID, command.OwnerID, expires); err != nil {
		t.Fatalf("seed historical V1 fenced reservation: %v", err)
	}
	if _, err := tx.Exec(ctx, `insert into public.deletion_fence_leases(project_id,object_kind,object_id,owner_id,owner_generation,expires_at) values($1,$2,$3,$4,1,$5)`, command.Preview.Object.ProjectID, command.Preview.Object.ObjectKind, command.Preview.Object.ObjectID, command.OwnerID, expires); err != nil {
		t.Fatalf("seed historical V1 fence: %v", err)
	}
	if _, err := tx.Exec(ctx, `update public.content_delivery_epochs set delivery_epoch=1 where project_id=$1 and object_kind=$2 and object_id=$3`, command.Preview.Object.ProjectID, command.Preview.Object.ObjectKind, command.Preview.Object.ObjectID); err != nil {
		t.Fatalf("seed historical V1 delivery epoch: %v", err)
	}
	if _, err := tx.Exec(ctx, `insert into public.record_purge_operations(operation_id,reservation_id,project_id,operation_state,deployment_id,actor_id,reason_code,deletion_contract_version,owner_id,owner_generation,owner_expires_at) values($1,$2,$3,'provisional_fenced',$4,$5,$6,1,$7,1,$8)`, operationID, command.Preview.ReservationID, command.Preview.Object.ProjectID, string(command.DeploymentID), command.ActorID, string(command.ReasonCode), command.OwnerID, expires); err != nil {
		t.Fatalf("seed historical V1 operation: %v", err)
	}
	if _, err := tx.Exec(ctx, `insert into public.record_deletion_audits(audit_id,operation_id,project_id,event_kind,reason_code) values('rda_' || substring($1 from 5),$1,$2,'fenced',$3)`, operationID, command.Preview.Object.ProjectID, string(command.ReasonCode)); err != nil {
		t.Fatalf("seed historical V1 fenced audit: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit historical V1 operation: %v", err)
	}
	operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1, OperationID: operationID, ReservationID: command.Preview.ReservationID, Object: command.Preview.Object, ReasonCode: command.ReasonCode, State: recorddeletion.DeletionStateProvisionalFenced, FenceEpoch: 1}
	if err := operation.Validate(); err != nil {
		t.Fatalf("invalid historical V1 operation: %v", err)
	}
	return operation
}

type recordsPostgresDeletionLedger struct {
	sequence     uint64
	entryHash    [32]byte
	resolution   recorddeletion.LedgerResolution
	appendErr    error
	appendCalls  int
	resolveCalls int
}

func (ledger *recordsPostgresDeletionLedger) AppendDeletionEntry(
	_ context.Context,
	request recorddeletion.LedgerAppendRequest,
) (recorddeletion.DeletionLedgerEntry, error) {
	ledger.appendCalls++
	if ledger.appendErr != nil {
		return recorddeletion.DeletionLedgerEntry{}, ledger.appendErr
	}
	return recorddeletion.DeletionLedgerEntry{
		Request: request, Sequence: ledger.sequence, EntryHash: ledger.entryHash,
	}, nil
}

func (ledger *recordsPostgresDeletionLedger) ResolveDeletionEntry(
	context.Context,
	recorddeletion.LedgerAppendRequest,
) (recorddeletion.LedgerResolution, error) {
	ledger.resolveCalls++
	return ledger.resolution, nil
}

type recordsPostgresDeletionWitness struct {
	proofDigest [32]byte
	calls       int
}

func (witness *recordsPostgresDeletionWitness) ConfirmDeletionEntry(
	_ context.Context,
	entry recorddeletion.DeletionLedgerEntry,
) (recorddeletion.DeletionWitnessReceipt, error) {
	witness.calls++
	return recorddeletion.DeletionWitnessReceipt{
		Sequence: entry.Sequence, EntryHash: entry.EntryHash, ProofDigest: witness.proofDigest,
	}, nil
}

type recordsPostgresRegistryDeletionPurger struct {
	registry *recorddeletion.RegistryPurger
	calls    int
	lastErr  error
	receipt  recorddeletion.OnlinePurgeReceipt
}

func (purger *recordsPostgresRegistryDeletionPurger) PurgeOnline(ctx context.Context, target recorddeletion.PurgeTarget) (recorddeletion.OnlinePurgeReceipt, error) {
	purger.calls++
	purger.receipt, purger.lastErr = purger.registry.PurgeOnline(ctx, target)
	return purger.receipt, purger.lastErr
}

type recordsPostgresUnexpectedDeletionPurger struct{}

func (recordsPostgresUnexpectedDeletionPurger) PurgeOnline(
	context.Context,
	recorddeletion.PurgeTarget,
) (recorddeletion.OnlinePurgeReceipt, error) {
	return recorddeletion.OnlinePurgeReceipt{}, errors.New("unexpected online purge")
}

func recordsPostgresActiveDeletionDeployment(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture) recordplatform.DeploymentID {
	t.Helper()
	var deploymentID recordplatform.DeploymentID
	if err := fixture.db.QueryRow(ctx, `select deployment_id from public.deployment_contract_state where project_id = $1`,
		recordplatform.ProjectIDDefault).Scan(&deploymentID); err != nil {
		t.Fatalf("read admitted deletion deployment: %v", err)
	}
	if err := recordplatform.ValidateDeploymentID(deploymentID); err != nil {
		t.Fatalf("invalid admitted deletion deployment: %v", err)
	}
	return deploymentID
}

func recordsPostgresDeletionPreviewCommand(
	t *testing.T,
	record records.RevisionCommitResult,
	deploymentID recordplatform.DeploymentID,
) (recorddeletion.CreatePreviewCommand, recordplatform.DeletionRequestTokenTransportV1) {
	t.Helper()
	issued, err := recordplatform.NewIssuedDeletionRequestTokenV1()
	if err != nil {
		t.Fatalf("NewIssuedDeletionRequestTokenV1() error = %v", err)
	}
	token, err := recordplatform.ParseDeletionRequestTokenTransportV1(issued.Transport())
	if err != nil {
		t.Fatalf("ParseDeletionRequestTokenTransportV1() error = %v", err)
	}
	commitment, err := issued.Commitment(deploymentID, recordplatform.ProjectIDDefault)
	if err != nil {
		t.Fatalf("DeletionRequestToken.Commitment() error = %v", err)
	}
	bindingDigest := testStoreRecordPlatformDigest(0x75)
	fingerprint, err := recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version:            recordplatform.RequestFingerprintVersionV1,
		OperationKind:      recordplatform.OperationKindRecordPermanentDelete,
		ProjectID:          recordplatform.ProjectIDDefault,
		ActorScopeDigest:   testStoreRecordPlatformDigest(0x71),
		RequestScopeDigest: testStoreRecordPlatformDigest(0x72),
		PayloadDigest:      bindingDigest,
	})
	if err != nil {
		t.Fatalf("FingerprintRequestV1() error = %v", err)
	}
	return recorddeletion.CreatePreviewCommand{
		Object: recordplatform.ObjectRef{
			ProjectID:  string(recordplatform.ProjectIDDefault),
			ObjectKind: "record",
			ObjectID:   record.RecordID,
		},
		ActorScopeDigest:   testStoreRecordPlatformDigest(0x71),
		TokenCommitment:    commitment,
		RequestFingerprint: fingerprint,
		BindingDigest:      bindingDigest,
		Record: recorddeletion.DeletionRecordSnapshot{
			RecordID:                 record.RecordID,
			CurrentRevisionID:        record.RevisionID,
			LockVersion:              record.LockVersion,
			AuthorizationEpoch:       record.AuthorizationEpoch,
			ContentDeliveryEpoch:     0,
			Authorization:            recordauth.ResourceScope{Version: recordauth.ResourceScopeVersionV1, ProjectID: recordauth.ProjectIDDefault},
			DependencyGraphDigest:    testStoreRecordPlatformDigest(0x73),
			BackupInventoryDigest:    testStoreRecordPlatformDigest(0x74),
			ProcessorInventoryDigest: testStoreRecordPlatformDigest(0x76),
		},
		WitnessHead:            recorddeletion.WitnessHead{Sequence: 7, EntryHash: testStoreRecordPlatformDigest(0x77)},
		AdapterReadinessDigest: testStoreRecordPlatformDigest(0x78),
		AdapterPreviewDigest:   testStoreRecordPlatformDigest(0x79),
		TTL:                    recorddeletion.DeletionPreviewTTL,
	}, token
}

func recordsPostgresDeletionReserveCommand(
	create recorddeletion.CreatePreviewCommand,
	preview recorddeletion.StoredPreview,
	deploymentID recordplatform.DeploymentID,
) recorddeletion.ReservePreviewCommand {
	return recorddeletion.ReservePreviewCommand{
		DeploymentID:            deploymentID,
		ActorID:                 "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
		DeletionContractVersion: recorddeletion.RecordDeletionContractVersionV1,
		Preview:                 preview,
		Record:                  create.Record,
		ExpectedBindingDigest:   create.BindingDigest,
		RequestFingerprint:      create.RequestFingerprint,
		ObservedWitnessHead:     recorddeletion.WitnessHead{Sequence: 9, EntryHash: testStoreRecordPlatformDigest(0x7a)},
		OwnerID:                 "deletion_pg_worker",
		OwnerLeaseDuration:      2 * time.Minute,
		ReasonCode:              recorddeletion.DeletionReasonUserConfirmed,
	}
}

// runV1AdapterAuthorityMatrix exercises the restricted commands against a
// real V1 delete-commit and a managed local export. All rejected commands are
// compared with the complete relevant database state, including owner leases.
func runV1AdapterAuthorityMatrix(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	local, _, _, _, _, _, measured, _ := seedV2ProcessedWorkspace(t, ctx, fixture)
	birth := ImportAttachmentGCFixture{Runtime: local.pool, Gate: fixture.admissionGate}
	record, err := newRevisionMutationAdmittedRepository(t, birth).CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, birth,
		recordsPostgresCompleteRevisionInput(t, "V1 authority matrix"), "v1-authority-matrix"))
	if err != nil {
		t.Fatal(err)
	}
	written := seedV2ExportArtifact(t, ctx, fixture, local, record.RecordID)
	if _, err := local.blob.Stat(ctx, written.Version); err != nil {
		t.Fatalf("published source: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.markdown_client_draft_buffers
		(user_id,browser_id,record_id,project_id,session_generation,cleanup_generation)
		values('usr_records1','v1authority',$1,'default',1,0)`, record.RecordID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.comparison_client_workbenches
		(user_id,browser_id,workbench_digest,destination_record_id,source_record_ids,generation)
		values('usr_records1','v1authority',decode(repeat('ab',32),'hex'),$1,array[$1],1)`, record.RecordID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.record_actions
		(action_id,record_id,action_version,title,status,created_by,updated_by,record_fence_epoch)
		values('ract_v1authority',$1,1,'Authority','open','usr_records1','usr_records1',0)`, record.RecordID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.record_outbox(project_id,event_kind,subject_kind,subject_id,authorization_epoch,expires_at)
		values('default','record_action_created','action','ract_v1authority',0,clock_timestamp()+interval '1 hour')`); err != nil {
		t.Fatal(err)
	}
	op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1,
		OperationID: "rpo_v1authoritymatrix", ReservationID: "drs_v1authoritymatrix",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
	target := seedVerifiedS3AttachmentDeletionOperation(t, ctx, fixture, record, op)
	pool := openV2LaunchBoundPool(t, ctx, fixture, fixture.deletionWorker, "v1-authority-matrix", measured)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, pool); err != nil {
		t.Fatal(err)
	}
	worker := NewPostgresRecordDeletionRepository(pool, allowRecordPlatformAdmissionGate)
	coordinator, err := attachments.NewLocalCleanupCoordinator(local.blob, NewPostgresLocalBlobRepository(pool, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.ConfigurePortabilityDeletionLocalCleanup(coordinator); err != nil {
		t.Fatal(err)
	}
	activeTest := t
	authority := func(surface [32]byte) []byte {
		t.Helper()
		var body []byte
		err := worker.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, tx *RecordPlatformTransaction) error {
			var e error
			body, e = legacyAdapterPurgeEnvelope(ctx, tx.tx, target, surface)
			return e
		})
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	markdown := authority(recorddeletion.RecordMarkdownClientSurfaceDigest())
	comparison := authority(recorddeletion.RecordComparisonSurfaceDigest())
	collaboration := authority(recorddeletion.RecordCollaborationSurfaceDigest())
	portability := authority(recorddeletion.RecordPortabilitySurfaceDigest())
	wrap := func(auth []byte, extra map[string]any) []byte {
		t := activeTest
		t.Helper()
		m := make(map[string]any, len(extra)+1)
		for k, v := range extra {
			m[k] = v
		}
		m["authority"] = json.RawMessage(auth)
		body, e := json.Marshal(m)
		if e != nil {
			t.Fatal(e)
		}
		return body
	}
	prepareBody := wrap(portability, map[string]any{"storage_binding": nil})
	// Full saved rows detect changes to source, quota, owner, epoch, dispatch,
	// manifests, receipts and client/outbox effects, not just row counts.
	snapshot := func() string {
		t := activeTest
		t.Helper()
		var parts []string
		for _, table := range []string{"records", "record_purge_operations", "deletion_reservations", "content_delivery_epochs", "deletion_fence_leases", "object_content_leases", "record_deletion_audits",
			"markdown_client_draft_buffers", "comparison_client_workbenches", "record_actions", "record_outbox",
			"browser_record_cleanup_events", "record_markdown_client_purge_receipts", "record_comparison_purge_receipts",
			"record_collaboration_purge_receipts", "record_portability_purge_receipts", "record_export_jobs", "record_export_artifacts",
			"record_write_obligations", "attachment_quota_accounts", "blob_gc_deletions", "local_blob_gc_obligations",
			"record_portability_v1_preparations", "record_portability_v1_targets", "record_portability_v1_artifacts", "record_portability_v1_obligations"} {
			parts = append(parts, `(select coalesce(jsonb_agg(to_jsonb(x) order by to_jsonb(x)::text),'[]'::jsonb) from public.`+pgx.Identifier{table}.Sanitize()+` x)`)
		}
		var state string
		if err := fixture.db.QueryRow(ctx, `select jsonb_build_array(`+strings.Join(parts, ",")+`)::text`).Scan(&state); err != nil {
			t.Fatalf("snapshot state: %v", err)
		}
		return state
	}
	denied := func(label, statement string, body []byte) {
		t := activeTest
		t.Helper()
		before := snapshot()
		_, e := pool.Exec(ctx, statement, body)
		var pgerr *pgconn.PgError
		// An unknown, syntactically valid operation or verified entry is read
		// with SELECT INTO STRICT and yields no_data_found rather than 42501.
		if !errors.As(e, &pgerr) || (pgerr.Code != "42501" && pgerr.Code != "P0002") {
			t.Fatalf("%s: got %v, want authority denial", label, e)
		}
		if after := snapshot(); after != before {
			t.Fatalf("%s changed state on denial", label)
		}
	}
	controlRollback := func(statement string, body []byte) {
		t := activeTest
		t.Helper()
		before := snapshot()
		tx, e := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if e != nil {
			t.Fatal(e)
		}
		_, e = tx.Exec(ctx, statement, body)
		rollback := tx.Rollback(ctx)
		if e != nil || rollback != nil {
			t.Fatalf("valid control %s: %v; rollback: %v", statement, e, rollback)
		}
		if snapshot() != before {
			t.Fatalf("rolled-back control changed state: %s", statement)
		}
	}
	wrong := func(statement string, body []byte, nested bool) {
		t := activeTest
		t.Helper()
		for _, tc := range []struct {
			field string
			value any
		}{
			{"operation_id", "rpo_wrongowner"}, {"reservation_id", "drs_wrongowner"}, {"record_id", "rec_wrongowner"},
			{"fence_epoch", 13}, {"owner_id", "wrong_owner"}, {"owner_generation", 2},
			{"owner_expires_at", target.OwnerClaim.Owner.ExpiresAt.Add(-time.Second).UTC().Format(time.RFC3339Nano)},
			{"ledger_domain", strings.Repeat("11", 32)}, {"ledger_sequence", 999},
			{"ledger_entry_hash", strings.Repeat("22", 32)}, {"witness_proof_digest", strings.Repeat("33", 32)},
		} {
			var object map[string]any
			if e := json.Unmarshal(body, &object); e != nil {
				t.Fatal(e)
			}
			fields := object
			if nested {
				fields = object["authority"].(map[string]any)
			}
			fields[tc.field] = tc.value
			mutated, e := json.Marshal(object)
			if e != nil {
				t.Fatal(e)
			}
			denied(statement+"/"+tc.field, statement, mutated)
		}
	}

	t.Run("all_wrapper_acl", func(t *testing.T) {
		activeTest = t
		wrappers := []string{"record_deletion_permanent_fence_applied_v1", "record_markdown_client_purge_v1", "record_markdown_client_purge_verify_v1",
			"record_comparison_purge_v1", "record_comparison_purge_verify_v1", "record_collaboration_purge_v1", "record_collaboration_purge_verify_v1",
			"record_outbox_drain_v1", "record_portability_purge_prepare_v1", "record_portability_purge_prepare_resolve_v1",
			"record_portability_purge_v1", "record_portability_purge_verify_v1", "record_portability_local_prepare_v1", "record_portability_local_claim_v1",
			"record_portability_local_resolve_v1", "record_portability_local_bind_final_v1", "record_portability_local_complete_v1",
			"record_portability_gc_claim_v1", "record_portability_gc_claim_resolve_v1", "record_portability_gc_complete_v1",
			"record_portability_gc_retry_v1", "record_portability_gc_resolve_v1"}
		role := "v1_matrix_" + strings.ReplaceAll(localBlobFixtureUUID(t), "-", "")
		quoted := pgx.Identifier{role}.Sanitize()
		if _, err := fixture.db.Exec(ctx, `create role `+quoted+` login noinherit nosuperuser nocreatedb nocreaterole noreplication nobypassrls password 'v1_matrix_fixture_password'`); err != nil {
			t.Fatal(err)
		}
		fixture.dropRole(t, role)
		if _, err := fixture.db.Exec(ctx, `grant connect on database `+pgx.Identifier{fixture.databaseName}.Sanitize()+` to `+quoted); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.db.Exec(ctx, `grant usage on schema public to `+quoted); err != nil {
			t.Fatal(err)
		}
		fixture.passwords[role] = "v1_matrix_fixture_password"
		unrelated := fixture.openDirectRolePool(t, ctx, role, "v1-matrix-unrelated", 1)
		runtime := openV2LaunchBoundPool(t, ctx, fixture, fixture.runtime, "v1-matrix-runtime", measured)
		admin := fixture.openDirectRolePool(t, ctx, fixture.admin, "v1-matrix-admin", 1)
		for _, name := range wrappers {
			var workerExec, runtimeExec, adminExec, publicExec, unrelatedExec bool
			identity := "public." + name + "(bytea)"
			err := fixture.db.QueryRow(ctx, `select has_function_privilege($1,$5,'execute'),has_function_privilege($2,$5,'execute'),
				has_function_privilege($3,$5,'execute'),exists(select 1 from pg_proc p cross join lateral
				aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a where p.oid=$5::regprocedure and a.grantee=0 and a.privilege_type='EXECUTE'),
				has_function_privilege($4,$5,'execute')`, fixture.deletionWorker, fixture.runtime, fixture.admin, role, identity).
				Scan(&workerExec, &runtimeExec, &adminExec, &publicExec, &unrelatedExec)
			if err != nil || !workerExec || runtimeExec || adminExec || publicExec || unrelatedExec {
				t.Fatalf("%s ACL worker/runtime/admin/PUBLIC/unrelated=%t/%t/%t/%t/%t: %v", identity, workerExec, runtimeExec, adminExec, publicExec, unrelatedExec, err)
			}
			for label, db := range map[string]func(context.Context, string, ...any) error{
				"runtime": func(ctx context.Context, q string, args ...any) error {
					_, e := runtime.Exec(ctx, q, args...)
					return e
				},
				"admin": func(ctx context.Context, q string, args ...any) error { _, e := admin.Exec(ctx, q, args...); return e },
				"unrelated": func(ctx context.Context, q string, args ...any) error {
					_, e := unrelated.Exec(ctx, q, args...)
					return e
				},
			} {
				before := snapshot()
				var pgerr *pgconn.PgError
				if e := db(ctx, `select public.`+pgx.Identifier{name}.Sanitize()+`($1)`, []byte(`{}`)); !errors.As(e, &pgerr) || pgerr.Code != "42501" {
					t.Fatalf("%s %s = %v, want 42501", label, name, e)
				}
				if snapshot() != before {
					t.Fatalf("%s %s changed state", label, name)
				}
			}
		}
		for _, table := range []string{"record_purge_operations", "record_portability_v1_preparations", "record_portability_v1_targets",
			"record_portability_v1_artifacts", "record_portability_v1_obligations", "blob_gc_deletions",
			"attachment_quota_accounts", "record_markdown_client_purge_receipts", "record_comparison_purge_receipts",
			"record_collaboration_purge_receipts", "record_portability_purge_receipts", "browser_record_cleanup_events"} {
			for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE"} {
				var allowed bool
				if err := fixture.db.QueryRow(ctx, `select has_table_privilege($1,$2,$3)`, fixture.deletionWorker, "public."+table, privilege).Scan(&allowed); err != nil || allowed {
					t.Fatalf("worker %s %s allowed=%t: %v", privilege, table, allowed, err)
				}
			}
			var pgerr *pgconn.PgError
			if _, err := pool.Exec(ctx, `select 1 from public.`+pgx.Identifier{table}.Sanitize()+` limit 1`); !errors.As(err, &pgerr) || pgerr.Code != "42501" {
				t.Fatalf("worker raw %s select = %v", table, err)
			}
		}
		// The five pre-existing recovery-table reads are shared by runtime,
		// admin and worker; preserve them while rejecting worker raw writes.
		for _, table := range []string{"local_blob_namespaces", "local_blob_namespace_incarnations", "local_blob_key_anchors", "record_write_obligations", "record_write_obligation_targets"} {
			for _, subject := range []string{fixture.runtime, fixture.admin, fixture.deletionWorker} {
				var allowed bool
				if err := fixture.db.QueryRow(ctx, `select has_table_privilege($1,$2,'SELECT')`, subject, "public."+table).Scan(&allowed); err != nil || !allowed {
					t.Fatalf("baseline %s select %s = %t: %v", subject, table, allowed, err)
				}
			}
			for _, privilege := range []string{"INSERT", "UPDATE", "DELETE", "TRUNCATE"} {
				var allowed bool
				if err := fixture.db.QueryRow(ctx, `select has_table_privilege($1,$2,$3)`, fixture.deletionWorker, "public."+table, privilege).Scan(&allowed); err != nil || allowed {
					t.Fatalf("worker %s %s allowed=%t: %v", privilege, table, allowed, err)
				}
			}
			if _, err := pool.Exec(ctx, `select 1 from public.`+pgx.Identifier{table}.Sanitize()+` limit 1`); err != nil {
				t.Fatalf("existing worker %s read: %v", table, err)
			}
		}
		for _, table := range []string{"record_portability_v1_preparations", "record_portability_v1_targets", "record_portability_v1_artifacts", "record_portability_v1_obligations"} {
			for _, subject := range []string{fixture.runtime, fixture.admin, role} {
				for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE"} {
					var allowed bool
					if err := fixture.db.QueryRow(ctx, `select has_table_privilege($1,$2,$3)`, subject, "public."+table, privilege).Scan(&allowed); err != nil || allowed {
						t.Fatalf("%s %s %s allowed=%t: %v", subject, privilege, table, allowed, err)
					}
				}
			}
			for label, db := range map[string]func(context.Context, string, ...any) error{
				"runtime": func(ctx context.Context, q string, args ...any) error {
					_, e := runtime.Exec(ctx, q, args...)
					return e
				},
				"admin": func(ctx context.Context, q string, args ...any) error { _, e := admin.Exec(ctx, q, args...); return e },
				"unrelated": func(ctx context.Context, q string, args ...any) error {
					_, e := unrelated.Exec(ctx, q, args...)
					return e
				},
			} {
				var pgerr *pgconn.PgError
				if err := db(ctx, `select 1 from public.`+pgx.Identifier{table}.Sanitize()+` limit 1`); !errors.As(err, &pgerr) || pgerr.Code != "42501" {
					t.Fatalf("%s raw %s select = %v", label, table, err)
				}
			}
		}
	})

	t.Run("verify_readonly_missing_proof", func(t *testing.T) {
		activeTest = t
		for _, tc := range []struct {
			name string
			body []byte
		}{
			{"record_markdown_client_purge_verify_v1", markdown}, {"record_comparison_purge_verify_v1", comparison},
			{"record_collaboration_purge_verify_v1", collaboration}, {"record_portability_purge_verify_v1", portability},
			{"record_portability_purge_prepare_resolve_v1", prepareBody},
		} {
			before := snapshot()
			_, err := pool.Exec(ctx, `select public.`+pgx.Identifier{tc.name}.Sanitize()+`($1)`, tc.body)
			var pgerr *pgconn.PgError
			if !errors.As(err, &pgerr) || pgerr.Code != "55000" {
				t.Fatalf("missing %s = %v, want 55000", tc.name, err)
			}
			if snapshot() != before {
				t.Fatalf("%s created missing proof", tc.name)
			}
		}
	})

	t.Run("all_wrong_authority_no_effects", func(t *testing.T) {
		activeTest = t
		// Each mutating control executes in a real worker transaction and rolls
		// back before its malformed-authority variants are submitted.
		for _, tc := range []struct {
			name   string
			body   []byte
			nested bool
		}{
			{"record_markdown_client_purge_v1", markdown, false}, {"record_comparison_purge_v1", comparison, false},
			{"record_outbox_drain_v1", collaboration, false}, {"record_portability_purge_prepare_v1", prepareBody, true},
		} {
			statement := `select public.` + pgx.Identifier{tc.name}.Sanitize() + `($1)`
			controlRollback(statement, tc.body)
			wrong(statement, tc.body, tc.nested)
		}
		// Commit the real drain and metadata effects so all verify controls have
		// actual receipts and the collaboration control has a drained outbox.
		for _, tc := range []struct {
			name string
			body []byte
		}{
			{"record_outbox_drain_v1", collaboration}, {"record_markdown_client_purge_v1", markdown},
			{"record_comparison_purge_v1", comparison}, {"record_collaboration_purge_v1", collaboration},
		} {
			if _, err := pool.Exec(ctx, `select public.`+pgx.Identifier{tc.name}.Sanitize()+`($1)`, tc.body); err != nil {
				t.Fatalf("valid %s: %v", tc.name, err)
			}
		}
		for _, tc := range []struct {
			name string
			body []byte
		}{
			{"record_collaboration_purge_v1", collaboration}, {"record_markdown_client_purge_verify_v1", markdown},
			{"record_comparison_purge_verify_v1", comparison}, {"record_collaboration_purge_verify_v1", collaboration},
		} {
			statement := `select public.` + pgx.Identifier{tc.name}.Sanitize() + `($1)`
			controlRollback(statement, tc.body)
			wrong(statement, tc.body, false)
			if strings.Contains(tc.name, "verify") {
				before := snapshot()
				if _, err := pool.Exec(ctx, statement, tc.body); err != nil {
					t.Fatalf("valid verify %s: %v", tc.name, err)
				}
				if snapshot() != before {
					t.Fatalf("verify %s changed state", tc.name)
				}
			}
		}
		// The fence command has its own ten-field continuation envelope; its
		// project is explicit, whereas the twelve-field adapter binds default
		// project through the independently verified reservation and entry.
		fenceRecord, err := newRevisionMutationAdmittedRepository(t, birth).CommitRevision(ctx, revisionBirthBatchCommand(t, ctx, birth,
			recordsPostgresCompleteRevisionInput(t, "V1 matrix fence"), "v1-matrix-fence"))
		if err != nil {
			t.Fatal(err)
		}
		deployment := recordsPostgresActiveDeletionDeployment(t, ctx, fixture)
		create, token := recordsPostgresDeletionPreviewCommand(t, fenceRecord, deployment)
		runtimeRepo := NewPostgresRecordDeletionRepository(local.pool, fixture.admissionGate)
		preview := recordsPostgresSeedV1Preview(t, ctx, fixture, runtimeRepo, create, token, "drs_v1matrixfence")
		reserve := recordsPostgresDeletionReserveCommand(create, preview, deployment)
		reserve.OwnerID = "v1_matrix_fence"
		recordsPostgresSeedV1Fence(t, ctx, fixture, runtimeRepo, reserve, "rpo_v1matrixfence")
		claimInput := recorddeletion.DeletionWorkClaimInput{OwnerID: reserve.OwnerID, OwnerLeaseDuration: 2 * time.Minute}
		claim, err := worker.ClaimDeletionWork(ctx, claimInput)
		if err != nil || claim == nil {
			t.Fatalf("fence claim: %#v %v", claim, err)
		}
		ledger := newRecordsPostgresRecoveryLedger(t, ctx, ImportAttachmentGCFixture{platform: fixture})
		entry, proof := ledger.record(t, ctx, claim.Request)
		fenceWorker := recorddeletion.NewDeletionWorker(worker, &recordsPostgresDeletionLedger{sequence: entry.Sequence, entryHash: entry.EntryHash},
			&recordsPostgresDeletionWitness{proofDigest: proof}, recordsPostgresUnexpectedDeletionPurger{},
			recorddeletion.DeletionWorkerOptions{OwnerID: reserve.OwnerID, OwnerLeaseDuration: 2 * time.Minute})
		for i := 0; i < 3; i++ {
			if err := fenceWorker.RunOnce(ctx); err != nil {
				t.Fatalf("advance fence %d: %v", i, err)
			}
		}
		claim, err = worker.ClaimDeletionWork(ctx, claimInput)
		if err != nil || claim == nil || claim.Stage != recorddeletion.DeletionWorkPropagatePermanentFence {
			t.Fatalf("fence propagation claim: %#v %v", claim, err)
		}
		fenceEnvelope, err := deletionV1OperationLedger(*claim)
		if err != nil {
			t.Fatal(err)
		}
		fenceBody, err := encodeRecordPurgeCommand(fenceEnvelope)
		if err != nil {
			t.Fatal(err)
		}
		var ready bool
		before := snapshot()
		if err := pool.QueryRow(ctx, `select public.record_deletion_permanent_fence_applied_v1($1)`, fenceBody).Scan(&ready); err != nil || !ready {
			t.Fatalf("ten-field valid fence = %t, %v", ready, err)
		}
		if snapshot() != before {
			t.Fatal("fence read changed state")
		}
		for _, tc := range []struct {
			field string
			value any
			code  string
		}{
			{"operation_id", "rpo_wrongowner", "42501"}, {"reservation_id", "drs_wrongowner", "42501"},
			{"project_id", "other", "22023"}, {"record_id", "rec_wrongowner", "42501"}, {"fence_epoch", 13, "42501"},
			{"owner_id", "wrong_owner", "42501"}, {"owner_generation", 2, "42501"},
			{"owner_expires_at", claim.Owner.ExpiresAt.Add(-time.Second).UTC().Format(time.RFC3339Nano), "42501"},
			{"ledger_sequence", 999, "42501"}, {"ledger_entry_hash", strings.Repeat("22", 32), "42501"},
		} {
			var object map[string]any
			if err := json.Unmarshal(fenceBody, &object); err != nil {
				t.Fatal(err)
			}
			object[tc.field] = tc.value
			body, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			before := snapshot()
			_, err = pool.Exec(ctx, `select public.record_deletion_permanent_fence_applied_v1($1)`, body)
			var pgerr *pgconn.PgError
			if !errors.As(err, &pgerr) || pgerr.Code != tc.code {
				t.Fatalf("fence %s = %v, want %s", tc.field, err, tc.code)
			}
			if snapshot() != before {
				t.Fatalf("fence %s changed state", tc.field)
			}
		}
	})

	t.Run("local_claim_tuple_tamper", func(t *testing.T) {
		activeTest = t
		prep, err := worker.preparePortabilityV1(ctx, target, recorddeletion.RecordPortabilitySurfaceDigest())
		if err != nil || prep.UnresolvedCount != 0 || len(prep.LocalTargets) != 1 {
			t.Fatalf("local preparation = %#v, %v", prep, err)
		}
		item := prep.LocalTargets[0]
		generation := int64(target.OwnerClaim.Owner.Generation)
		owner := target.OwnerClaim.Owner
		opID := target.Operation.OperationID
		opFields := attachments.LocalCleanupOperation{OperationID: &opID, OwnerID: &owner.OwnerID, Generation: &generation, ExpiresAt: &owner.ExpiresAt}
		version := item.ExpectedVersion
		request := attachments.LocalCleanupPreparation{LocalCleanupOperation: opFields, DeletionID: item.DeletionID,
			NamespaceID: item.NamespaceID, IncarnationID: item.IncarnationID, Key: item.Key, ExpectedVersion: &version}
		v1 := &portabilityV1LocalCleanupRepository{repository: worker, target: target, surface: recorddeletion.RecordPortabilitySurfaceDigest()}
		if _, err := v1.PrepareLocalCleanup(ctx, request); err != nil {
			t.Fatalf("local prepare replay: %v", err)
		}
		requestBody := func(v any) []byte { return wrap(portability, map[string]any{"request": v}) }
		prepare := requestBody(request)
		controlRollback(`select public.record_portability_local_prepare_v1($1)`, prepare)
		wrong(`select public.record_portability_local_prepare_v1($1)`, prepare, true)
		beforeMissing := snapshot()
		_, missingErr := pool.Exec(ctx, `select public.record_portability_purge_verify_v1($1)`, portability)
		var missingPG *pgconn.PgError
		if !errors.As(missingErr, &missingPG) || missingPG.Code != "55000" {
			t.Fatalf("verify prepared without physical proof: %v", missingErr)
		}
		if snapshot() != beforeMissing {
			t.Fatal("verify created physical proof or receipt")
		}
		if err := local.blob.WithKeyGuard(ctx, item.Key, func(ctx context.Context, _ *attachments.LocalKeyGuard) error {
			var anchorInode uint64
			var anchorNonce string
			if err := fixture.db.QueryRow(ctx, `select lock_inode,lock_nonce::text from public.local_blob_key_anchors
				where namespace_id=$1 and incarnation_id=$2 and canonical_key=$3`, item.NamespaceID, item.IncarnationID, item.Key).Scan(&anchorInode, &anchorNonce); err != nil {
				return err
			}
			identity := attachments.LocalCleanupIdentity{LocalCleanupOperation: opFields, DeletionID: item.DeletionID,
				NamespaceID: item.NamespaceID, IncarnationID: item.IncarnationID, OwnerID: owner.OwnerID, DispatchID: localBlobFixtureUUID(t)}
			beforeMissing := snapshot()
			if _, err := v1.ResolveLocalCleanup(ctx, identity); err == nil {
				t.Fatal("resolve issued a missing dispatch")
			}
			if snapshot() != beforeMissing {
				t.Fatal("resolve created a missing dispatch")
			}
			claim := attachments.LocalCleanupClaim{LocalCleanupIdentity: identity, AnchorInode: anchorInode, AnchorNonce: anchorNonce}
			claimBody := requestBody(claim)
			controlRollback(`select public.record_portability_local_claim_v1($1)`, claimBody)
			wrong(`select public.record_portability_local_claim_v1($1)`, claimBody, true)
			claimed, err := v1.ClaimLocalCleanup(ctx, claim)
			if err != nil || claimed.Task.Generation == nil || claimed.Task.ExpiresAt == nil {
				t.Fatalf("actual local claim = %#v, %v", claimed, err)
			}
			// Claim compares the caller's observed generation and expiry before
			// issuing a replacement dispatch. A valid replay is rolled back so
			// it cannot extend the lease while testing altered tuples.
			currentClaim := claim
			currentClaim.Generation, currentClaim.ExpiresAt = *claimed.Task.Generation, claimed.Task.ExpiresAt
			currentBody := requestBody(currentClaim)
			controlRollback(`select public.record_portability_local_claim_v1($1)`, currentBody)
			for _, tc := range []struct {
				field string
				value any
				code  string
			}{
				{"anchor_inode", anchorInode + 1, "42501"}, {"anchor_nonce", localBlobFixtureUUID(t), "42501"},
				{"cleanup_owner_id", "wrong_owner", "55000"}, {"cleanup_generation", *claimed.Task.Generation + 1, "55000"},
				{"cleanup_expires_at", claimed.Task.ExpiresAt.Add(-time.Second).UTC().Format(time.RFC3339Nano), "55000"},
				{"operation_id", "rpo_wrongowner", "42501"}, {"operation_owner_id", "wrong_owner", "42501"},
				{"operation_generation", generation + 1, "42501"}, {"operation_expires_at", owner.ExpiresAt.Add(-time.Second).UTC().Format(time.RFC3339Nano), "42501"},
			} {
				var object map[string]any
				if err := json.Unmarshal(currentBody, &object); err != nil {
					t.Fatal(err)
				}
				object["request"].(map[string]any)[tc.field] = tc.value
				body, err := json.Marshal(object)
				if err != nil {
					t.Fatal(err)
				}
				before := snapshot()
				_, err = pool.Exec(ctx, `select public.record_portability_local_claim_v1($1)`, body)
				var pgerr *pgconn.PgError
				if !errors.As(err, &pgerr) || pgerr.Code != tc.code {
					t.Fatalf("claim %s = %v, want %s", tc.field, err, tc.code)
				}
				if snapshot() != before {
					t.Fatalf("claim %s changed state", tc.field)
				}
			}
			resolveBody := requestBody(identity)
			before := snapshot()
			if _, err := v1.ResolveLocalCleanup(ctx, identity); err != nil {
				t.Fatalf("resolve actual dispatch: %v", err)
			}
			if snapshot() != before {
				t.Fatal("resolve changed claim, lease or state")
			}
			wrong(`select public.record_portability_local_resolve_v1($1)`, resolveBody, true)
			for _, tc := range []struct {
				field string
				value any
			}{
				{"deletion_id", "bgd_" + strings.Repeat("a", 64)}, {"namespace_id", localBlobFixtureUUID(t)},
				{"incarnation_id", localBlobFixtureUUID(t)}, {"cleanup_owner_id", "wrong_owner"}, {"cleanup_dispatch_id", localBlobFixtureUUID(t)},
			} {
				var m map[string]any
				if err := json.Unmarshal(resolveBody, &m); err != nil {
					t.Fatal(err)
				}
				m["request"].(map[string]any)[tc.field] = tc.value
				body, err := json.Marshal(m)
				if err != nil {
					t.Fatal(err)
				}
				if tc.field == "namespace_id" || tc.field == "incarnation_id" {
					before := snapshot()
					_, err := pool.Exec(ctx, `select public.record_portability_local_resolve_v1($1)`, body)
					var pgerr *pgconn.PgError
					if !errors.As(err, &pgerr) || pgerr.Code != "55000" || !strings.Contains(pgerr.Message, "local namespace admission is closed or stale") {
						t.Fatalf("resolve namespace admission = %v, want 55000 local namespace admission is closed or stale", err)
					}
					if snapshot() != before {
						t.Fatalf("resolve wrong %s changed state or lease", tc.field)
					}
					continue
				}
				denied("resolve/"+tc.field, `select public.record_portability_local_resolve_v1($1)`, body)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		// The coordinator reclaims the same real task under the key guard and
		// performs the original inode-bound physical cleanup.
		if _, err := worker.PurgeRecordPortability(ctx, target, recorddeletion.RecordPortabilitySurfaceDigest()); err != nil {
			t.Fatalf("real local cleanup: %v", err)
		}
		if _, err := local.blob.Stat(ctx, written.Version); !errors.Is(err, attachments.ErrBlobNotFound) {
			t.Fatalf("original version after cleanup = %v", err)
		}
		var cleanupOwner, dispatch, inventory, outcome, anchorNonce string
		var cleanupGen, anchorInode, device, inode int64
		var expiry time.Time
		var finalVersion *string
		var completionCommand []byte
		if err := fixture.db.QueryRow(ctx, `select g.owner_id,g.cleanup_dispatch_id::text,g.owner_generation,g.lease_expires_at,
			encode(g.inventory_digest,'hex'),g.physical_delete_result,g.object_version,g.final_device,g.final_inode,a.lock_inode,a.lock_nonce::text,g.completion_command
			from public.blob_gc_deletions g join public.local_blob_key_anchors a on a.namespace_id=g.namespace_id
			and a.incarnation_id=g.incarnation_id and a.canonical_key=g.blob_key where g.deletion_id=$1 and g.deletion_state='completed'`, item.DeletionID).
			Scan(&cleanupOwner, &dispatch, &cleanupGen, &expiry, &inventory, &outcome, &finalVersion, &device, &inode, &anchorInode, &anchorNonce, &completionCommand); err != nil {
			t.Fatal(err)
		}
		if !json.Valid(completionCommand) {
			t.Fatal("completed task has no valid saved completion command")
		}
		identity := attachments.LocalCleanupIdentity{LocalCleanupOperation: opFields, DeletionID: item.DeletionID, NamespaceID: item.NamespaceID,
			IncarnationID: item.IncarnationID, OwnerID: cleanupOwner, DispatchID: dispatch}
		current := attachments.LocalCleanupCurrent{LocalCleanupIdentity: identity, Generation: cleanupGen, ExpiresAt: &expiry}
		for _, tc := range []struct {
			name    string
			request any
		}{
			{"record_portability_local_resolve_v1", identity},
			{"record_portability_local_bind_final_v1", attachments.LocalCleanupFinalBinding{LocalCleanupCurrent: current, Version: finalVersion, Device: uint64(device), Inode: uint64(inode), InventoryDigest: inventory}},
			{"record_portability_local_complete_v1", nil},
		} {
			statement := `select public.` + pgx.Identifier{tc.name}.Sanitize() + `($1)`
			var body []byte
			if tc.name == "record_portability_local_complete_v1" {
				// Keep the saved request byte-for-byte; re-marshalling changes the
				// completion command that the physical receipt authenticated.
				body = append([]byte(`{"authority":`), portability...)
				body = append(body, `,"request":`...)
				body = append(body, completionCommand...)
				body = append(body, '}')
			} else {
				body = requestBody(tc.request)
			}
			controlRollback(statement, body)
			wrong(statement, body, true)
			for _, change := range []struct {
				field string
				value any
			}{
				{"operation_id", "rpo_wrongowner"}, {"operation_owner_id", "wrong_owner"},
				{"operation_generation", generation + 1}, {"operation_expires_at", owner.ExpiresAt.Add(-time.Second).UTC().Format(time.RFC3339Nano)},
			} {
				var object map[string]any
				if err := json.Unmarshal(body, &object); err != nil {
					t.Fatal(err)
				}
				object["request"].(map[string]any)[change.field] = change.value
				changed, err := json.Marshal(object)
				if err != nil {
					t.Fatal(err)
				}
				denied(tc.name+"/"+change.field, statement, changed)
			}
			for _, field := range []string{"cleanup_generation", "cleanup_expires_at", "cleanup_dispatch_id"} {
				if tc.name == "record_portability_local_resolve_v1" && field != "cleanup_dispatch_id" {
					continue
				}
				var m map[string]any
				if err := json.Unmarshal(body, &m); err != nil {
					t.Fatal(err)
				}
				value := any(localBlobFixtureUUID(t))
				if field == "cleanup_generation" {
					value = cleanupGen + 1
				}
				if field == "cleanup_expires_at" {
					value = expiry.Add(-time.Second).UTC().Format(time.RFC3339Nano)
				}
				m["request"].(map[string]any)[field] = value
				changed, err := json.Marshal(m)
				if err != nil {
					t.Fatal(err)
				}
				denied(tc.name+"/"+field, statement, changed)
			}
		}
		for _, tc := range []struct {
			name   string
			body   []byte
			nested bool
		}{
			{"record_portability_purge_prepare_resolve_v1", prepareBody, true}, {"record_portability_purge_v1", portability, false},
			{"record_portability_purge_verify_v1", portability, false},
		} {
			statement := `select public.` + pgx.Identifier{tc.name}.Sanitize() + `($1)`
			controlRollback(statement, tc.body)
			wrong(statement, tc.body, tc.nested)
			if tc.name != "record_portability_purge_v1" {
				before := snapshot()
				if _, err := pool.Exec(ctx, statement, tc.body); err != nil {
					t.Fatalf("valid readonly %s: %v", tc.name, err)
				}
				if snapshot() != before {
					t.Fatalf("%s changed saved state", tc.name)
				}
			}
		}
		// Expire the persisted operation, then require a real worker takeover.
		// Completed physical proof retains its original cleanup tuple; neither
		// an expired nor a superseded caller may adopt the new database owner.
		var expired time.Time
		if err := fixture.db.QueryRow(ctx, `update public.record_purge_operations set owner_expires_at=clock_timestamp()-interval '1 minute'
			where operation_id=$1 returning owner_expires_at`, opID).Scan(&expired); err != nil {
			t.Fatal(err)
		}
		var expiredFields map[string]any
		if err := json.Unmarshal(portability, &expiredFields); err != nil {
			t.Fatal(err)
		}
		expiredFields["owner_expires_at"] = expired.UTC().Format(time.RFC3339Nano)
		expiredBody, err := json.Marshal(expiredFields)
		if err != nil {
			t.Fatal(err)
		}
		denied("expired exact owner", `select public.record_portability_purge_verify_v1($1)`, expiredBody)
		takeover, err := worker.ClaimDeletionWork(ctx, recorddeletion.DeletionWorkClaimInput{OwnerID: "v1_matrix_takeover", OwnerLeaseDuration: 2 * time.Minute})
		if err != nil || takeover == nil || takeover.Operation.OperationID != opID || takeover.Owner.Generation != owner.Generation+1 {
			t.Fatalf("operation takeover = %#v, %v", takeover, err)
		}
		denied("previous owner after takeover", `select public.record_portability_purge_verify_v1($1)`, portability)
		newTarget := target
		newTarget.OwnerClaim.Owner = takeover.Owner
		var newBody []byte
		if err := worker.platform.RunRecordPlatformTransaction(ctx, func(ctx context.Context, tx *RecordPlatformTransaction) error {
			var err error
			newBody, err = legacyAdapterPurgeEnvelope(ctx, tx.tx, newTarget, recorddeletion.RecordPortabilitySurfaceDigest())
			return err
		}); err != nil {
			t.Fatal(err)
		}
		before := snapshot()
		if _, err := pool.Exec(ctx, `select public.record_portability_purge_verify_v1($1)`, newBody); err != nil {
			t.Fatalf("takeover verifies original physical proof: %v", err)
		}
		if snapshot() != before {
			t.Fatal("takeover verify changed physical proof or receipt")
		}
	})
}

// v1EpochFixture creates a real import archive, plan and imported root before
// installing historical V1 authority for that root. The real archive and
// writer registration remain available to the serialization tests.
type v1EpochFixture struct {
	fixture     ImportAttachmentGCFixture
	imports     *PostgresRecordPortabilityRepository
	actor       recordauth.ActorScope
	destination records.SubjectReference
	content     []byte
	digest      [32]byte
	expires     time.Time
	written     attachments.WrittenBlob
	target      recorddeletion.PurgeTarget
	worker      *PostgresRecordDeletionRepository
}

func v1EpochPlanInput(t *testing.T, ctx context.Context, f *v1EpochFixture, job RecordImportJob, source string) (SaveRecordImportPlanInput, recordbirth.Allocation) {
	t.Helper()
	birth, err := recordbirth.NewCoordinator(NewPostgresRecordBirthRepository(f.fixture.Runtime, f.fixture.Gate, nil), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	allocation := reserveImportWriteFixture(t, ctx, birth, f.actor, job, source)
	planDigest := sha256.Sum256([]byte("v1-epoch-plan-" + source))
	return SaveRecordImportPlanInput{ImportJobID: job.ImportJobID, Actor: f.actor, ArchiveDigest: f.digest,
		PlanDigest: planDigest, ObjectCount: 1, RemapCount: 1, Allocations: []recordbirth.Allocation{allocation},
		Remaps:             []ImportRemap{{EntityKind: "record", SourceID: source, TargetID: allocation.RecordID, BirthAllocationID: allocation.AllocationID}},
		Documents:          []ImportDocumentPlan{{SourceID: source, TargetID: allocation.RecordID, Title: "Epoch import", Body: "Body"}},
		DestinationSubject: f.destination, ExpiresAt: f.expires}, allocation
}

func v1EpochClaimJob(t *testing.T, ctx context.Context, f *v1EpochFixture, key string) RecordImportJob {
	t.Helper()
	job, err := f.imports.ClaimImportJob(ctx, ClaimRecordImportJobInput{ActorID: f.actor.UserID, IdempotencyKey: key,
		ArchiveDigest: f.digest, DestinationSubject: f.destination, ExpiresAt: f.expires})
	if err != nil {
		t.Fatalf("claim import job %s: %v", key, err)
	}
	return job
}

func v1EpochWriteArchive(t *testing.T, ctx context.Context, f *v1EpochFixture, job RecordImportJob, artifactID string) attachments.WrittenBlob {
	t.Helper()
	digest := hex.EncodeToString(f.digest[:])
	written, err := f.fixture.Writer.Put(ctx, attachments.WriteRequest{ProjectID: "default",
		Origin:  attachments.WriteOrigin{Kind: "import_archive", ID: job.ImportJobID, Generation: int64(job.LockVersion), ArchiveDigest: &digest},
		Content: attachments.PutRequest{ExpectedSHA256: f.digest, ExpectedSizeBytes: int64(len(f.content))}}, bytes.NewReader(f.content))
	if err != nil {
		t.Fatalf("write import archive %s: %v", artifactID, err)
	}
	tx, err := f.fixture.Runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var expiresAt time.Time
	if err := tx.QueryRow(ctx, `select expires_at from public.record_import_jobs where import_job_id=$1`, job.ImportJobID).Scan(&expiresAt); err != nil {
		t.Fatalf("read import job expiry: %v", err)
	}
	namespace := f.fixture.LocalCleanup.NamespaceID()
	body, err := json.Marshal(map[string]any{"artifact_id": artifactID, "job_id": job.ImportJobID, "kind": "archive",
		"backend_kind": "local", "namespace_id": namespace, "key": written.Version.Key, "version": written.Version.VersionID,
		"sha256": digest, "size": len(f.content), "expires_at": expiresAt, "obligation_id": written.ObligationID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `select public.record_import_artifact_publish($1)`, body); err != nil {
		t.Fatalf("publish import archive: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit import artifact publication: %v", err)
	}
	return written
}

func v1EpochNewFixture(t *testing.T, ctx context.Context, repeatableRead bool) *v1EpochFixture {
	t.Helper()
	f := &v1EpochFixture{fixture: NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, repeatableRead),
		actor: storeEvidenceParticipantActor(t), content: []byte("v1 epoch shared import archive\n"), expires: time.Now().UTC().Add(time.Hour)}
	f.digest = sha256.Sum256(f.content)
	f.destination = records.SubjectReference{RegistryVersion: records.SubjectRegistryVersionV1, Kind: records.SubjectKindVPS,
		Role: records.RelationRoleAffected, SourceID: testStoreRecordVPSID, Primary: true}
	f.imports = NewPostgresRecordPortabilityRepository(f.fixture.Runtime, f.fixture.Gate, localBlobFixtureWitness(t, f.fixture.Runtime), nil)
	job := v1EpochClaimJob(t, ctx, f, "v1-epoch-original")
	f.written = v1EpochWriteArchive(t, ctx, f, job, "ria_v1epochoriginal")
	planInput, allocation := v1EpochPlanInput(t, ctx, f, job, "rec_v1epochoriginal")
	if _, err := f.imports.SaveImportPlan(ctx, planInput); err != nil {
		t.Fatalf("save original import plan: %v", err)
	}
	f.fixture.RefreshHeartbeat()
	if err := f.fixture.CommitImported(ctx, "CommitRevision", [][]attachments.ImportedAvailableAttachment{{}}, []recordbirth.Allocation{allocation}); err != nil {
		t.Fatalf("commit original imported record: %v", err)
	}
	if _, err := f.fixture.platform.db.Exec(ctx, `update public.record_import_jobs set job_state='applied' where import_job_id=$1`, job.ImportJobID); err != nil {
		t.Fatal(err)
	}
	record := records.RevisionCommitResult{RecordID: allocation.RecordID}
	var lockVersion, authorizationEpoch int64
	if err := f.fixture.platform.db.QueryRow(ctx, `select current_revision_id,lock_version,authorization_epoch from public.records where record_id=$1`, record.RecordID).
		Scan(&record.RevisionID, &lockVersion, &authorizationEpoch); err != nil {
		t.Fatal(err)
	}
	record.LockVersion, record.AuthorizationEpoch = uint64(lockVersion), uint64(authorizationEpoch)
	op := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1,
		OperationID: "rpo_v1epochoriginal", ReservationID: "drs_v1epochoriginal",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: record.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging, FenceEpoch: 12}
	f.target = seedVerifiedS3AttachmentDeletionOperation(t, ctx, f.fixture.platform, record, op)
	workerPool := f.fixture.platform.openDirectRolePool(t, ctx, f.fixture.platform.deletionWorker, "v1-epoch-worker", 4)
	if err := storemigrate.AdmitAppACLCurrentDeletionWorker(ctx, workerPool); err != nil {
		t.Fatal(err)
	}
	f.worker = NewPostgresRecordDeletionRepository(workerPool, allowRecordPlatformAdmissionGate)
	if err := f.worker.ConfigurePortabilityDeletionLocalCleanup(f.fixture.LocalCleanup); err != nil {
		t.Fatal(err)
	}
	return f
}

func v1EpochSnapshot(t *testing.T, ctx context.Context, f *v1EpochFixture) string {
	t.Helper()
	var result string
	err := f.fixture.platform.db.QueryRow(ctx, `select jsonb_build_array(
		(select coalesce(jsonb_agg(to_jsonb(x) order by x.import_job_id),'[]'::jsonb) from public.record_import_jobs x),
		(select coalesce(jsonb_agg(to_jsonb(x) order by x.import_plan_id),'[]'::jsonb) from public.record_import_plans x),
		(select coalesce(jsonb_agg(to_jsonb(x) order by x.import_artifact_id),'[]'::jsonb) from public.record_import_artifacts x),
		(select coalesce(jsonb_agg(to_jsonb(x) order by to_jsonb(x)::text),'[]'::jsonb) from public.record_import_entity_mappings x),
		(select coalesce(jsonb_agg(to_jsonb(x) order by x.obligation_id),'[]'::jsonb) from public.record_write_obligations x),
		(select coalesce(jsonb_agg(to_jsonb(x) order by to_jsonb(x)::text),'[]'::jsonb) from public.record_write_obligation_targets x),
		(select coalesce(jsonb_agg(to_jsonb(x) order by x.record_id),'[]'::jsonb) from public.records x),
		(select coalesce(jsonb_agg(to_jsonb(x) order by to_jsonb(x)::text),'[]'::jsonb) from public.content_delivery_epochs x),
		(select coalesce(jsonb_agg(to_jsonb(x) order by x.deletion_id),'[]'::jsonb) from public.blob_gc_deletions x),
		(select coalesce(jsonb_agg(to_jsonb(x) order by x.target_id),'[]'::jsonb) from public.record_portability_v1_targets x),
		(select coalesce(jsonb_agg(to_jsonb(x) order by x.operation_id),'[]'::jsonb) from public.record_portability_v1_preparations x),
		(select coalesce(jsonb_agg(to_jsonb(x) order by to_jsonb(x)::text),'[]'::jsonb) from public.record_portability_v1_artifacts x),
		(select coalesce(jsonb_agg(to_jsonb(x) order by to_jsonb(x)::text),'[]'::jsonb) from public.record_portability_v1_obligations x),
		(select coalesce(jsonb_agg(to_jsonb(x) order by x.operation_id),'[]'::jsonb) from public.record_portability_purge_receipts x),
		(select to_jsonb(x) from public.attachment_quota_accounts x where project_id='default'),
		(select to_jsonb(x) from public.record_purge_operations x where operation_id=$1))::text`, f.target.Operation.OperationID).Scan(&result)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func runV1EpochSerialization(t *testing.T) {
	t.Run("no_manifest_default_repeatable_read", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		f := v1EpochNewFixture(t, ctx, true)
		var isolation string
		var root, plan, manifest bool
		if err := f.fixture.Runtime.QueryRow(ctx, `select current_setting('default_transaction_isolation'),
			exists(select 1 from public.records where record_id=$1),
			exists(select 1 from public.record_import_entity_mappings where entity_kind='record' and target_id=$1)`,
			f.target.Operation.Object.ObjectID).Scan(&isolation, &root, &plan); err != nil {
			t.Fatal(err)
		}
		if err := f.fixture.platform.db.QueryRow(ctx, `select exists(select 1 from public.record_portability_v1_preparations where operation_id=$1)`,
			f.target.Operation.OperationID).Scan(&manifest); err != nil {
			t.Fatal(err)
		}
		if isolation != "repeatable read" || !root || !plan || manifest {
			t.Fatalf("default RR real import isolation=%q root=%t plan=%t manifest=%t", isolation, root, plan, manifest)
		}
		if _, err := f.fixture.Blobs.Stat(ctx, f.written.Version); err != nil {
			t.Fatalf("import bytes: %v", err)
		}
	})

	t.Run("snapshot_before_prepare_repeatable_read", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		f := v1EpochNewFixture(t, ctx, true)
		writer, err := f.fixture.Runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = writer.Rollback(context.Background()) }()
		var isolation, snapshot string
		if err := writer.QueryRow(ctx, `select current_setting('transaction_isolation'),txid_current_snapshot()::text`).Scan(&isolation, &snapshot); err != nil {
			t.Fatal(err)
		}
		if isolation != "repeatable read" || snapshot == "" {
			t.Fatalf("writer snapshot isolation=%q snapshot=%q", isolation, snapshot)
		}
		prep, err := f.worker.preparePortabilityV1(ctx, f.target, recorddeletion.RecordPortabilitySurfaceDigest())
		if err != nil || prep.UnresolvedCount != 0 || len(prep.LocalTargets) != 1 {
			t.Fatalf("real V1 prepare = %#v, %v", prep, err)
		}
		before := v1EpochSnapshot(t, ctx, f)
		f.imports.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return writer, nil }
		_, err = f.imports.ClaimImportJob(ctx, ClaimRecordImportJobInput{ActorID: f.actor.UserID, IdempotencyKey: "v1-epoch-stale-rr",
			ArchiveDigest: f.digest, DestinationSubject: f.destination, ExpiresAt: f.expires})
		var pgerr *pgconn.PgError
		if !errors.As(err, &pgerr) || pgerr.Code != "40001" {
			t.Fatalf("snapshot-before-prepare writer = %v, want 40001", err)
		}
		if after := v1EpochSnapshot(t, ctx, f); after != before {
			t.Fatal("stale RR writer changed jobs, source, manifest, quota or owner")
		}
		if _, err := f.fixture.Blobs.Stat(ctx, f.written.Version); err != nil {
			t.Fatalf("stale writer changed bytes: %v", err)
		}
	})

	t.Run("snapshot_before_prepare_read_committed", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		f := v1EpochNewFixture(t, ctx, false)
		writer, err := f.fixture.Runtime.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = writer.Rollback(context.Background()) }()
		var isolation, snapshot string
		if err := writer.QueryRow(ctx, `select current_setting('transaction_isolation'),txid_current_snapshot()::text`).Scan(&isolation, &snapshot); err != nil {
			t.Fatal(err)
		}
		if isolation != "read committed" || snapshot == "" {
			t.Fatalf("writer snapshot isolation=%q snapshot=%q", isolation, snapshot)
		}
		prep, err := f.worker.preparePortabilityV1(ctx, f.target, recorddeletion.RecordPortabilitySurfaceDigest())
		if err != nil || prep.UnresolvedCount != 0 || len(prep.LocalTargets) != 1 {
			t.Fatalf("real V1 prepare = %#v, %v", prep, err)
		}
		var before string
		if err := f.fixture.platform.db.QueryRow(ctx, `select jsonb_build_array(to_jsonb(p),to_jsonb(t),to_jsonb(g),to_jsonb(o))::text
			from public.record_portability_v1_preparations p join public.record_portability_v1_targets t using(operation_id)
			join public.blob_gc_deletions g on g.deletion_id=t.local_deletion_id
			join public.record_purge_operations o on o.operation_id=p.operation_id where p.operation_id=$1`, f.target.Operation.OperationID).Scan(&before); err != nil {
			t.Fatal(err)
		}
		f.imports.platform.beginTx = func(context.Context, pgx.TxOptions) (pgx.Tx, error) { return writer, nil }
		job, err := f.imports.ClaimImportJob(ctx, ClaimRecordImportJobInput{ActorID: f.actor.UserID, IdempotencyKey: "v1-epoch-fresh-rc",
			ArchiveDigest: f.digest, DestinationSubject: f.destination, ExpiresAt: f.expires})
		if err != nil || job.ImportJobID == "" {
			t.Fatalf("fresh RC unrelated job = %#v, %v", job, err)
		}
		var after string
		if err := f.fixture.platform.db.QueryRow(ctx, `select jsonb_build_array(to_jsonb(p),to_jsonb(t),to_jsonb(g),to_jsonb(o))::text
			from public.record_portability_v1_preparations p join public.record_portability_v1_targets t using(operation_id)
			join public.blob_gc_deletions g on g.deletion_id=t.local_deletion_id
			join public.record_purge_operations o on o.operation_id=p.operation_id where p.operation_id=$1`, f.target.Operation.OperationID).Scan(&after); err != nil {
			t.Fatal(err)
		}
		if after != before {
			t.Fatal("fresh RC unrelated job changed V1 manifest, task or owner")
		}
		if _, err := f.fixture.Blobs.Stat(ctx, f.written.Version); err != nil {
			t.Fatalf("fresh RC job changed bytes: %v", err)
		}
	})

	for _, rr := range []bool{false, true} {
		name := "default_read_committed"
		if rr {
			name = "default_repeatable_read"
		}
		t.Run("prepare_before_writer/"+name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			f := v1EpochNewFixture(t, ctx, rr)
			job := v1EpochClaimJob(t, ctx, f, "v1-epoch-late-writer")
			prep, err := f.worker.preparePortabilityV1(ctx, f.target, recorddeletion.RecordPortabilitySurfaceDigest())
			if err != nil || prep.UnresolvedCount != 0 || len(prep.LocalTargets) != 1 {
				t.Fatalf("prepare first = %#v, %v", prep, err)
			}
			before := v1EpochSnapshot(t, ctx, f)
			digest := hex.EncodeToString(f.digest[:])
			_, err = f.fixture.Writer.Put(ctx, attachments.WriteRequest{ProjectID: "default",
				Origin:  attachments.WriteOrigin{Kind: "import_archive", ID: job.ImportJobID, Generation: int64(job.LockVersion), ArchiveDigest: &digest},
				Content: attachments.PutRequest{ExpectedSHA256: f.digest, ExpectedSizeBytes: int64(len(f.content))}}, bytes.NewReader(f.content))
			var pgerr *pgconn.PgError
			if !errors.As(err, &pgerr) || pgerr.Code != "55000" {
				t.Fatalf("late real managed writer = %v, want 55000", err)
			}
			if after := v1EpochSnapshot(t, ctx, f); after != before {
				t.Fatal("late writer changed source, manifest, task, quota or owner")
			}
			if _, err := f.fixture.Blobs.Stat(ctx, f.written.Version); err != nil {
				t.Fatalf("late writer changed original bytes: %v", err)
			}
		})

		t.Run("writer_before_prepare/"+name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			f := v1EpochNewFixture(t, ctx, rr)
			job := v1EpochClaimJob(t, ctx, f, "v1-epoch-survivor")
			written := v1EpochWriteArchive(t, ctx, f, job, "ria_v1epochsurvivor")
			if written.ObligationID == f.written.ObligationID || written.Version.Key != f.written.Version.Key || written.Version.VersionID != f.written.Version.VersionID {
				t.Fatalf("independent writer identity original=%#v survivor=%#v", f.written, written)
			}
			planInput, allocation := v1EpochPlanInput(t, ctx, f, job, "rec_v1epochsurvivor")
			workerConn, err := f.worker.pool.Acquire(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer workerConn.Release()
			workerTx, err := workerConn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = workerTx.Rollback(context.Background()) }()
			var workerPID int32
			if err := workerTx.QueryRow(ctx, `select pg_backend_pid()`).Scan(&workerPID); err != nil {
				t.Fatal(err)
			}
			authority, err := legacyAdapterPurgeEnvelope(ctx, workerTx, f.target, recorddeletion.RecordPortabilitySurfaceDigest())
			if err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(struct {
				Authority      json.RawMessage `json:"authority"`
				StorageBinding *string         `json:"storage_binding"`
			}{authority, nil})
			if err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			t.Cleanup(func() {
				select {
				case <-release:
				default:
					close(release)
				}
			})
			writerPID := make(chan int32, 1)
			begin := f.imports.platform.beginTx
			attempt := 0
			f.imports.platform.beginTx = func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
				tx, err := begin(ctx, options)
				if err != nil {
					return nil, err
				}
				attempt++
				if attempt != 1 {
					return tx, nil
				}
				var pid int32
				var isolation string
				if err := tx.QueryRow(ctx, `select pg_backend_pid(),current_setting('transaction_isolation')`).Scan(&pid, &isolation); err != nil {
					_ = tx.Rollback(ctx)
					return nil, err
				}
				if isolation != "read committed" {
					_ = tx.Rollback(ctx)
					return nil, errors.New("real SaveImportPlan transaction is not read committed")
				}
				writerPID <- pid
				return &importAttachmentCommitTx{Tx: tx, entered: entered, release: release}, nil
			}
			planDone := make(chan error, 1)
			go func() { _, err := f.imports.SaveImportPlan(ctx, planInput); planDone <- err }()
			select {
			case <-entered:
			case err := <-planDone:
				t.Fatalf("writer did not reach commit barrier: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			pid := <-writerPID
			type v1EpochPrepareResult struct {
				projection []byte
				err        error
			}
			prepareDone := make(chan v1EpochPrepareResult, 1)
			go func() {
				var raw []byte
				err := workerTx.QueryRow(ctx, `select public.record_portability_purge_prepare_v1($1)`, body).Scan(&raw)
				if err == nil {
					err = workerTx.Commit(ctx)
				} else {
					_ = workerTx.Rollback(ctx)
				}
				prepareDone <- v1EpochPrepareResult{projection: raw, err: err}
			}()
			waitCtx, stopWait := context.WithTimeout(ctx, 15*time.Second)
			waitForPostgresCollaborationBlocker(t, waitCtx, f.fixture.platform, workerPID, pid)
			stopWait()
			close(release)
			select {
			case err := <-planDone:
				if err != nil {
					t.Fatalf("real writer plan commit: %v", err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			var outcome v1EpochPrepareResult
			select {
			case outcome = <-prepareDone:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if outcome.err != nil {
				t.Fatalf("prepare after writer: %v", outcome.err)
			}
			prep, err := decodePortabilityV1Preparation(outcome.projection)
			if err != nil || prep.UnresolvedCount != 0 || len(prep.LocalTargets) != 0 {
				t.Fatalf("writer-first retained projection = %#v, %v", prep, err)
			}
			assertV1SavedInventoryDigest(t, ctx, f.fixture.platform, f.target.Operation.OperationID, prep.InventoryDigest)
			var captured, planned, unassigned bool
			if err := f.fixture.platform.db.QueryRow(ctx, `select
				exists(select 1 from public.record_portability_v1_targets x, jsonb_array_elements(x.retained_evidence) e(value)
				  where x.operation_id=$1 and x.disposition='retained_shared' and e.value->>0='import_artifact' and e.value->>1='ria_v1epochsurvivor'),
				exists(select 1 from public.record_import_plans p join public.record_import_entity_mappings m using(import_plan_id)
				  where p.import_job_id=$2 and m.entity_kind='record' and m.target_id=$3),
				exists(select 1 from public.record_write_obligations where obligation_id=$4 and operation_id is null and cleanup_completed_at is null)`,
				f.target.Operation.OperationID, job.ImportJobID, allocation.RecordID, written.ObligationID).Scan(&captured, &planned, &unassigned); err != nil || !captured || !planned || !unassigned {
				t.Fatalf("writer-first manifest=%t plan=%t owner-unassigned=%t: %v", captured, planned, unassigned, err)
			}
			if _, err := f.fixture.Blobs.Stat(ctx, f.written.Version); err != nil {
				t.Fatalf("retained bytes: %v", err)
			}
		})

		t.Run("ordinary_gc_unrelated_waits/"+name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			f := v1EpochNewFixture(t, ctx, rr)
			prep, err := f.worker.preparePortabilityV1(ctx, f.target, recorddeletion.RecordPortabilitySurfaceDigest())
			if err != nil || prep.UnresolvedCount != 0 || len(prep.LocalTargets) != 1 {
				t.Fatalf("V1 preparation = %#v, %v", prep, err)
			}
			orphan := f.fixture.SeedBlob([]byte("unrelated ordinary GC epoch lock\n"))
			f.fixture.SeedOrphan(orphan.Version)
			var unrelated bool
			if err := f.fixture.platform.db.QueryRow(ctx, `select not exists(select 1 from public.record_portability_v1_targets where blob_key=$1)`, orphan.Version.Key).Scan(&unrelated); err != nil || !unrelated {
				t.Fatalf("ordinary identity unrelated=%t: %v", unrelated, err)
			}
			holder, err := f.fixture.platform.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = holder.Rollback(context.Background()) }()
			var holderPID int32
			if err := holder.QueryRow(ctx, `select pg_backend_pid()`).Scan(&holderPID); err != nil {
				t.Fatal(err)
			}
			if _, err := holder.Exec(ctx, `select 1 from public.attachment_quota_accounts where project_id='default' for update`); err != nil {
				t.Fatal(err)
			}
			gc, err := attachments.NewBlobGCWorker(f.fixture.Repository, f.fixture.Deleter, attachments.BlobGCWorkerOptions{BackendKind: attachments.BackendKindLocal, LocalCleanup: f.fixture.LocalCleanup})
			if err != nil {
				t.Fatal(err)
			}
			type v1EpochGCResult struct {
				worked bool
				err    error
			}
			done := make(chan v1EpochGCResult, 1)
			go func() { worked, err := gc.RunOnce(ctx); done <- v1EpochGCResult{worked: worked, err: err} }()
			waitCtx, stopWait := context.WithTimeout(ctx, 15*time.Second)
			for {
				var waiting bool
				if err := f.fixture.platform.db.QueryRow(waitCtx, `select exists(select 1 from pg_catalog.pg_stat_activity a
					where a.datname=current_database() and $1=any(pg_blocking_pids(a.pid)) and a.wait_event_type='Lock'
					and exists(select 1 from pg_catalog.pg_locks l where l.pid=a.pid and not l.granted))`, holderPID).Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if waiting {
					break
				}
				select {
				case got := <-done:
					t.Fatalf("ordinary GC did not wait: %#v", got)
				case <-waitCtx.Done():
					t.Fatal(waitCtx.Err())
				default:
				}
			}
			stopWait()
			if err := holder.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case got := <-done:
				if !got.worked || got.err != nil {
					t.Fatalf("ordinary GC after lock release: %#v", got)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if _, err := f.fixture.Blobs.Stat(ctx, orphan.Version); !errors.Is(err, attachments.ErrBlobNotFound) {
				t.Fatalf("ordinary GC unrelated bytes = %v", err)
			}
			if _, err := f.fixture.Blobs.Stat(ctx, f.written.Version); err != nil {
				t.Fatalf("V1 target changed by unrelated GC: %v", err)
			}
		})
	}
}
