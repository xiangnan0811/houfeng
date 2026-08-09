package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

func TestPostgresIntegrationRecordDeletionPreviewReserveAndExpiredReplay(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-deletion-preview-reserve", 2)
	recordRepository := newRecordsPostgresRepository(t, runtimePool)
	created, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		"rec_pgdeletepreview",
		"",
		0,
		0,
		recordsPostgresCompleteRevisionInput(t, "Deletion preview record"),
		"record-deletion-preview-create",
	))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}

	deploymentID := recordplatform.DeploymentID("dp-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	createCommand, token := recordsPostgresDeletionPreviewCommand(t, created, deploymentID)
	repository := NewPostgresRecordDeletionRepository(runtimePool, allowRecordPlatformAdmissionGate)
	repository.newReservationID = func() (string, error) { return "drs_pgdeletepreview", nil }
	repository.newOperationID = func() (string, error) { return "rpo_pgdeletepreview", nil }
	repository.newAuditID = func() (string, error) { return "rda_pgdeletepreview", nil }

	preview, err := repository.CreatePreview(ctx, createCommand)
	if err != nil {
		t.Fatalf("CreatePreview() error = %v", err)
	}
	if preview.ReservationID != "drs_pgdeletepreview" || preview.Object != createCommand.Object ||
		preview.Operation != nil || preview.ExpiresAt.IsZero() || preview.Validate() != nil {
		t.Fatalf("CreatePreview() = %#v", preview)
	}
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
	platformRepository := NewPostgresRecordPlatformRepository(runtimePool, allowRecordPlatformAdmissionGate)
	serving, err := platformRepository.AcquireServingLease(ctx, createCommand.Object, recordplatform.LeaseClaimInputV1{
		OwnerID:       "attachment_delivery_pgdeletepreview",
		LeaseDuration: time.Second,
	})
	if err != nil {
		t.Fatalf("AcquireServingLease() before reserve error = %v", err)
	}

	reserveCommand := recordsPostgresDeletionReserveCommand(createCommand, resolved, deploymentID)
	operation, err := repository.ReservePreview(ctx, reserveCommand)
	if err != nil {
		t.Fatalf("ReservePreview() error = %v", err)
	}
	if operation.OperationID != "rpo_pgdeletepreview" || operation.ReservationID != preview.ReservationID ||
		operation.State != recorddeletion.DeletionStateProvisionalFenced || operation.FenceEpoch != 1 ||
		operation.Validate() != nil {
		t.Fatalf("ReservePreview() = %#v", operation)
	}
	claimInput := recorddeletion.DeletionWorkClaimInput{
		OwnerID:            reserveCommand.OwnerID,
		OwnerLeaseDuration: reserveCommand.OwnerLeaseDuration,
	}
	claim, err := repository.ClaimDeletionWork(ctx, claimInput)
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
	claim, err = repository.ClaimDeletionWork(ctx, claimInput)
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
		auditCount       int
	)
	if err := fixture.db.QueryRow(ctx, `
		select reservation.state,
		       reservation.fence_epoch,
		       operation.operation_state,
		       operation.owner_id,
		       operation.owner_generation,
		       fence.owner_id,
		       fence.owner_generation,
		       epoch.delivery_epoch,
		       (select count(*)::int
		          from public.record_deletion_audits audit
		         where audit.operation_id = operation.operation_id
		           and audit.event_kind = 'fenced')
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
		&auditCount,
	); err != nil {
		t.Fatalf("read reserved deletion operation: %v", err)
	}
	if reservationState != "fenced" || reservationEpoch != 1 ||
		operationState != string(recorddeletion.DeletionStateProvisionalFenced) ||
		operationOwner != reserveCommand.OwnerID || operationGen != 1 ||
		fenceOwner != operationOwner || fenceGen != operationGen || deliveryEpoch != 1 || auditCount != 1 {
		t.Fatalf("reserved deletion state = reservation %q/%d operation %q/%q/%d fence %q/%d epoch %d audits %d",
			reservationState, reservationEpoch, operationState, operationOwner, operationGen,
			fenceOwner, fenceGen, deliveryEpoch, auditCount)
	}
}

func TestPostgresIntegrationRecordDeletionReserveCASDriftRollsBackFenceAndOperation(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-deletion-cas-drift", 2)
	recordRepository := newRecordsPostgresRepository(t, runtimePool)
	created, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		"rec_pgdeletecasdrift",
		"",
		0,
		0,
		recordsPostgresCompleteRevisionInput(t, "Deletion CAS record"),
		"record-deletion-cas-create",
	))
	if err != nil {
		t.Fatalf("CommitRevision(create) error = %v", err)
	}

	deploymentID := recordplatform.DeploymentID("dp-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	createCommand, _ := recordsPostgresDeletionPreviewCommand(t, created, deploymentID)
	repository := NewPostgresRecordDeletionRepository(runtimePool, allowRecordPlatformAdmissionGate)
	repository.newReservationID = func() (string, error) { return "drs_pgdeletecasdrift", nil }
	repository.newOperationID = func() (string, error) { return "rpo_pgdeletecasdrift", nil }
	repository.newAuditID = func() (string, error) { return "rda_pgdeletecasdrift", nil }
	preview, err := repository.CreatePreview(ctx, createCommand)
	if err != nil {
		t.Fatalf("CreatePreview() error = %v", err)
	}

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
	if !errors.Is(err, recorddeletion.ErrDeletionPreviewStale) {
		t.Fatalf("ReservePreview() drift error = %v, want ErrDeletionPreviewStale", err)
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

func TestPostgresIntegrationRecordDeletionDeleteCommitPurgesCoreAndDeliveryEpoch(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-deletion-delete-commit", 2)
	recordRepository := newRecordsPostgresRepository(t, runtimePool)
	draftRepository := newRecordsPostgresDraftRepository(runtimePool)
	created, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		"rec_pgdeletecommit",
		"",
		0,
		0,
		recordsPostgresCompleteRevisionInput(t, "Deletion committed record"),
		"record-deletion-commit-create",
	))
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

	deploymentID := recordplatform.DeploymentID("dp-cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	createCommand, _ := recordsPostgresDeletionPreviewCommand(t, created, deploymentID)
	repository := NewPostgresRecordDeletionRepository(runtimePool, allowRecordPlatformAdmissionGate)
	preview, err := repository.CreatePreview(ctx, createCommand)
	if err != nil {
		t.Fatalf("CreatePreview() error = %v", err)
	}
	reserveCommand := recordsPostgresDeletionReserveCommand(createCommand, preview, deploymentID)
	reserveCommand.OwnerID = "deletion_commit_worker"
	operation, err := repository.ReservePreview(ctx, reserveCommand)
	if err != nil {
		t.Fatalf("ReservePreview() error = %v", err)
	}

	coreAdapter, err := recorddeletion.NewCoreAdapter(repository)
	if err != nil {
		t.Fatalf("NewCoreAdapter() error = %v", err)
	}
	ledger := &recordsPostgresDeletionLedger{
		sequence:  101,
		entryHash: testStoreRecordPlatformDigest(0x81),
	}
	witness := &recordsPostgresDeletionWitness{proofDigest: testStoreRecordPlatformDigest(0x82)}
	purger := &recordsPostgresCoreDeletionPurger{adapter: coreAdapter}
	worker := recorddeletion.NewDeletionWorker(repository, ledger, witness, purger, recorddeletion.DeletionWorkerOptions{
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
		recorddeletion.DeletionStateOnlinePurged,
	}
	for pass, expected := range expectedStates {
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
		       (select count(*)::int from public.record_revision_subjects),
		       (select count(*)::int from public.record_revision_tags),
		       (select count(*)::int from public.record_revision_participants),
		       (select count(*)::int from public.record_drafts where record_id = $1),
		       (select count(*)::int from public.record_draft_checkpoints),
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
	if ledger.appendCalls != 1 || ledger.resolveCalls != 0 || witness.calls != 1 || purger.calls != 1 {
		t.Fatalf("external cut points append/resolve/witness/purge=%d/%d/%d/%d, want 1/0/1/1",
			ledger.appendCalls, ledger.resolveCalls, witness.calls, purger.calls)
	}
}

func TestPostgresIntegrationRecordDeletionNotCommittedTakeoverRejectsStaleOwnerAndRestoresRead(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-deletion-not-committed", 2)
	recordRepository := newRecordsPostgresRepository(t, runtimePool)
	created, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		"rec_pgdeletenotcommitted",
		"",
		0,
		0,
		recordsPostgresCompleteRevisionInput(t, "Deletion not committed record"),
		"record-deletion-not-committed-create",
	))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}

	deploymentID := recordplatform.DeploymentID("dp-dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")
	createCommand, token := recordsPostgresDeletionPreviewCommand(t, created, deploymentID)
	repository := NewPostgresRecordDeletionRepository(runtimePool, allowRecordPlatformAdmissionGate)
	preview, err := repository.CreatePreview(ctx, createCommand)
	if err != nil {
		t.Fatalf("CreatePreview() error = %v", err)
	}
	reserveCommand := recordsPostgresDeletionReserveCommand(createCommand, preview, deploymentID)
	reserveCommand.OwnerID = "deletion_original_worker"
	operation, err := repository.ReservePreview(ctx, reserveCommand)
	if err != nil {
		t.Fatalf("ReservePreview() error = %v", err)
	}

	initialClaim, err := repository.ClaimDeletionWork(ctx, recorddeletion.DeletionWorkClaimInput{
		OwnerID: reserveCommand.OwnerID, OwnerLeaseDuration: 2 * time.Minute,
	})
	if err != nil || initialClaim == nil {
		t.Fatalf("ClaimDeletionWork(initial) = %#v, %v", initialClaim, err)
	}
	if err := repository.MarkDeleteCommitUnknown(ctx, *initialClaim); err != nil {
		t.Fatalf("MarkDeleteCommitUnknown() error = %v", err)
	}
	staleClaim, err := repository.ClaimDeletionWork(ctx, recorddeletion.DeletionWorkClaimInput{
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
	takeoverClaim, err := repository.ClaimDeletionWork(ctx, recorddeletion.DeletionWorkClaimInput{
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
	if err := repository.RecordDeleteEntry(ctx, *staleClaim, staleEntry); !errors.Is(err, recordplatform.ErrLostOwnerLease) {
		t.Fatalf("RecordDeleteEntry(stale owner) error = %v, want ErrLostOwnerLease", err)
	}

	absenceProof, err := recorddeletion.NewDeletionAbsenceProof(1, testStoreRecordPlatformDigest(0x92))
	if err != nil {
		t.Fatalf("NewDeletionAbsenceProof() error = %v", err)
	}
	ledger := &recordsPostgresDeletionLedger{
		sequence:   202,
		entryHash:  testStoreRecordPlatformDigest(0x93),
		resolution: recorddeletion.NewAbsenceProvenLedgerResolution(absenceProof),
	}
	witness := &recordsPostgresDeletionWitness{proofDigest: testStoreRecordPlatformDigest(0x94)}
	worker := recorddeletion.NewDeletionWorker(
		repository,
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

type recordsPostgresCoreDeletionPurger struct {
	adapter *recorddeletion.CoreAdapter
	calls   int
	lastErr error
}

func (purger *recordsPostgresCoreDeletionPurger) PurgeOnline(
	ctx context.Context,
	operation recorddeletion.DeletionOperation,
) (recorddeletion.OnlinePurgeReceipt, error) {
	purger.calls++
	target := recorddeletion.PurgeTarget{Operation: operation}
	receipt, err := purger.adapter.PurgeDeletion(ctx, target)
	if err != nil {
		purger.lastErr = err
		return recorddeletion.OnlinePurgeReceipt{}, err
	}
	if err := purger.adapter.VerifyDeletion(ctx, target, receipt); err != nil {
		purger.lastErr = err
		return recorddeletion.OnlinePurgeReceipt{}, err
	}
	return recorddeletion.OnlinePurgeReceipt{
		OperationID: operation.OperationID, ReceiptDigest: receipt.ReceiptDigest,
	}, nil
}

type recordsPostgresUnexpectedDeletionPurger struct{}

func (recordsPostgresUnexpectedDeletionPurger) PurgeOnline(
	context.Context,
	recorddeletion.DeletionOperation,
) (recorddeletion.OnlinePurgeReceipt, error) {
	return recorddeletion.OnlinePurgeReceipt{}, errors.New("unexpected online purge")
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
