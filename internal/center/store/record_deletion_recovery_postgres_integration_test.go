package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

func TestPostgresIntegrationRecordDeletionRecoveryDeleteCommitIsAtomicIdempotentAndContinuous(t *testing.T) {
	ctx := context.Background()
	legacy, created := newRecordsPostgresLegacyDeletionFixture(t, ctx,
		"rec_pgrecoverydelete", "Recovery delete record", "record-recovery-delete-create")
	fixture, runtimePool := legacy.platform, legacy.Runtime
	draftRepository := newAdmittedCoreDraftRepository(t, runtimePool, legacy.Gate)
	draft, err := draftRepository.CreateDraft(ctx, records.DraftCreateCommand{
		DraftID:        "rdf_pgrecoverydelete",
		ProjectID:      "default",
		RecordID:       created.RecordID,
		BaseRevisionID: created.RevisionID,
		AuthorID:       recordsPostgresDraftAuthorID,
		Payload:        recordsPostgresDraftPayload(t, "Recovery delete draft"),
		Policy:         records.DefaultDraftRetentionPolicy(),
	})
	if err != nil {
		t.Fatalf("CreateDraft() error = %v", err)
	}
	if _, err := draftRepository.PatchDraft(ctx, records.DraftPatchCommand{
		DraftID:  draft.DraftID,
		AuthorID: draft.AuthorID,
		IfMatch:  draft.ETag,
		Payload:  recordsPostgresDraftPayload(t, "Recovery delete draft changed"),
		Policy:   records.DefaultDraftRetentionPolicy(),
	}); err != nil {
		t.Fatalf("PatchDraft() error = %v", err)
	}

	deploymentID := recordsPostgresActiveDeletionDeployment(t, ctx, fixture)
	ledger := newRecordsPostgresRecoveryLedger(t, ctx, legacy)
	cursor := ledger.prior(t, ctx, deploymentID)
	seedRecordsPostgresRecoveryCursor(t, ctx, fixture, cursor)
	entry, witnessProof := ledger.record(t, ctx, recordsPostgresRecoveryRequest(
		t, created, "rpo_pgrecoverydelete", deploymentID, recorddeletion.LedgerEntryDeleteCommit, 0))
	replayRequest := ledger.replayRequest(cursor, entry)
	adapter := ledger.adapter

	first, err := adapter.Replay(ctx, replayRequest)
	if err != nil {
		t.Fatalf("Replay(delete commit) error = %v", err)
	}
	second, err := adapter.Replay(ctx, replayRequest)
	if err != nil {
		t.Fatalf("Replay(delete commit idempotent) error = %v", err)
	}
	if first != second || first.Sequence != entry.Sequence || first.EntryHash != entry.EntryHash || !first.ContentPurged {
		t.Fatalf("recovery receipts first=%#v second=%#v", first, second)
	}

	var (
		rootCount          int
		revisionCount      int
		subjectCount       int
		tagCount           int
		participantCount   int
		draftCount         int
		checkpointCount    int
		activityCount      int
		deliveryEpochCount int
	)
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.records where record_id = $1),
		       (select count(*)::int from public.record_revisions where record_id = $1),
		       (select count(*)::int
		          from public.record_revision_subjects subject
		          join public.record_revisions revision on revision.revision_id = subject.revision_id
		         where revision.record_id = $1),
		       (select count(*)::int
		          from public.record_revision_tags tag
		          join public.record_revisions revision on revision.revision_id = tag.revision_id
		         where revision.record_id = $1),
		       (select count(*)::int
		          from public.record_revision_participants participant
		          join public.record_revisions revision on revision.revision_id = participant.revision_id
		         where revision.record_id = $1),
		       (select count(*)::int from public.record_drafts where record_id = $1),
		       (select count(*)::int from public.record_draft_checkpoints where draft_id = $2),
		       (select count(*)::int from public.record_domain_activities where record_id = $1),
		       (select count(*)::int
		          from public.content_delivery_epochs
		         where project_id = 'default' and object_kind = 'record' and object_id = $1)`,
		created.RecordID,
		draft.DraftID,
	).Scan(
		&rootCount,
		&revisionCount,
		&subjectCount,
		&tagCount,
		&participantCount,
		&draftCount,
		&checkpointCount,
		&activityCount,
		&deliveryEpochCount,
	); err != nil {
		t.Fatalf("read recovery-deleted content: %v", err)
	}
	if rootCount != 0 || revisionCount != 0 || subjectCount != 0 || tagCount != 0 || participantCount != 0 ||
		draftCount != 0 || checkpointCount != 0 || activityCount != 0 || deliveryEpochCount != 0 {
		t.Fatalf("recovery delete residue root/revision/subject/tag/participant/draft/checkpoint/activity/epoch = %d/%d/%d/%d/%d/%d/%d/%d/%d",
			rootCount, revisionCount, subjectCount, tagCount, participantCount,
			draftCount, checkpointCount, activityCount, deliveryEpochCount)
	}

	var (
		recoveryReplayed bool
		reservationState string
		fenceEpoch       int64
		previewTupleNull bool
		operationState   string
		entryType        string
		ledgerSequence   int64
		ledgerHash       []byte
		witnessDigest    []byte
		receiptDigest    []byte
		auditCount       int
		coreReceiptCount int
		cursorSequence   int64
		cursorHash       []byte
	)
	if err := fixture.db.QueryRow(ctx, `
		select reservation.recovery_replayed,
		       reservation.state,
		       reservation.fence_epoch,
		       reservation.actor_scope_digest is null
		         and reservation.preview_binding_digest is null
		         and reservation.preview_current_revision_id is null
		         and reservation.preview_lock_version is null
		         and reservation.preview_authorization_epoch is null
		         and reservation.preview_content_delivery_epoch is null
		         and reservation.preview_dependency_graph_digest is null
		         and reservation.preview_backup_inventory_digest is null
		         and reservation.preview_processor_inventory_digest is null
		         and reservation.adapter_readiness_digest is null
		         and reservation.adapter_preview_digest is null
		         and reservation.preview_witness_sequence is null
		         and reservation.preview_witness_entry_hash is null,
		       operation.operation_state,
		       operation.ledger_entry_type,
		       operation.ledger_sequence,
		       operation.ledger_entry_hash,
		       operation.witness_proof_digest,
		       operation.receipt_digest,
		       (select count(*)::int
		          from public.record_deletion_audits audit
		         where audit.operation_id = operation.operation_id
		           and audit.event_kind = 'committed'),
		       (select count(*)::int
		          from public.record_core_purge_receipts receipt
		         where receipt.operation_id = operation.operation_id),
		       replay.applied_ledger_sequence,
		       replay.applied_ledger_hash
		from public.record_purge_operations operation
		join public.deletion_reservations reservation
		  on reservation.reservation_id = operation.reservation_id
		join public.deletion_replay_state replay
		  on replay.project_id = operation.project_id
		where operation.operation_id = $1`, entry.Request.OperationID).Scan(
		&recoveryReplayed,
		&reservationState,
		&fenceEpoch,
		&previewTupleNull,
		&operationState,
		&entryType,
		&ledgerSequence,
		&ledgerHash,
		&witnessDigest,
		&receiptDigest,
		&auditCount,
		&coreReceiptCount,
		&cursorSequence,
		&cursorHash,
	); err != nil {
		t.Fatalf("read recovery terminal projection: %v", err)
	}
	if !recoveryReplayed || reservationState != "committed" || fenceEpoch != 1 || !previewTupleNull ||
		operationState != string(recorddeletion.DeletionStateOnlinePurged) || entryType != string(recorddeletion.LedgerEntryDeleteCommit) ||
		ledgerSequence != int64(entry.Sequence) || !deletionStoreDigestEqual(ledgerHash, entry.EntryHash) ||
		!deletionStoreDigestEqual(witnessDigest, witnessProof) || len(receiptDigest) != 32 ||
		auditCount != 1 || coreReceiptCount != 1 || cursorSequence != int64(entry.Sequence) ||
		!deletionStoreDigestEqual(cursorHash, entry.EntryHash) {
		t.Fatalf("recovery terminal projection replayed/state/fence/null/op/type/sequence/audit/receipt/cursor = %t/%q/%d/%t/%q/%q/%d/%d/%d/%d",
			recoveryReplayed, reservationState, fenceEpoch, previewTupleNull, operationState,
			entryType, ledgerSequence, auditCount, coreReceiptCount, cursorSequence)
	}

	// A ledger-consistent cursor ahead of APP's applied entry is rejected by
	// the replay command before any reservation, operation or cursor effect.
	gap, _ := ledger.record(t, ctx, recordsPostgresRecoveryRequest(
		t, created, "rpo_pgrecoverygap", deploymentID, recorddeletion.LedgerEntryDeleteCommit, 0))
	staleEntry, _ := ledger.record(t, ctx, recordsPostgresRecoveryRequest(
		t, created, "rpo_pgrecoverystale", deploymentID, recorddeletion.LedgerEntryDeleteCommit, 0))
	staleCursor := recorddeletion.RecoveryReplayCursor{Sequence: gap.Sequence, EntryHash: gap.EntryHash}
	_, err = adapter.Replay(ctx, ledger.replayRequest(staleCursor, staleEntry))
	var stalePgErr *pgconn.PgError
	if !errors.As(err, &stalePgErr) || stalePgErr.Code != "40001" || stalePgErr.Message != "record recovery predecessor changed" {
		t.Fatalf("Replay(stale cursor) error = %v, want predecessor change rejection", err)
	}
	var staleOperationCount int
	if err := fixture.db.QueryRow(ctx, `
		select count(*)::int from public.record_purge_operations where operation_id in ($1, $2)`,
		gap.Request.OperationID, staleEntry.Request.OperationID,
	).Scan(&staleOperationCount); err != nil {
		t.Fatalf("count stale recovery operation: %v", err)
	}
	if staleOperationCount != 0 {
		t.Fatalf("stale recovery operation count = %d, want 0", staleOperationCount)
	}

	var (
		activeFenceOwner      string
		activeFenceGeneration int64
		activeFenceExpiresAt  time.Time
	)
	if err := fixture.db.QueryRow(ctx, `
		insert into public.deletion_fence_leases (
			project_id, object_kind, object_id, owner_id, owner_generation, expires_at, created_at
		) values ('default', 'record', $1, 'recovery_corrupt_fence', 1,
			transaction_timestamp() + interval '1 minute', transaction_timestamp())
		returning owner_id, owner_generation, expires_at`, created.RecordID).Scan(
		&activeFenceOwner,
		&activeFenceGeneration,
		&activeFenceExpiresAt,
	); err != nil {
		t.Fatalf("insert active fence for idempotent verification: %v", err)
	}
	if _, err := adapter.Replay(ctx, replayRequest); err != nil {
		t.Fatalf("Replay(idempotent with unrelated active fence) error = %v", err)
	}
	var (
		preservedFenceOwner      string
		preservedFenceGeneration int64
		preservedFenceExpiresAt  time.Time
	)
	if err := fixture.db.QueryRow(ctx, `
		select owner_id, owner_generation, expires_at
		from public.deletion_fence_leases
		where project_id = 'default' and object_kind = 'record' and object_id = $1`,
		created.RecordID,
	).Scan(&preservedFenceOwner, &preservedFenceGeneration, &preservedFenceExpiresAt); err != nil {
		t.Fatalf("read active fence after idempotent replay: %v", err)
	}
	if preservedFenceOwner != activeFenceOwner || preservedFenceGeneration != activeFenceGeneration ||
		!preservedFenceExpiresAt.Equal(activeFenceExpiresAt) {
		t.Fatalf("active fence changed across idempotent replay: got %q/%d/%s, want %q/%d/%s",
			preservedFenceOwner, preservedFenceGeneration, preservedFenceExpiresAt,
			activeFenceOwner, activeFenceGeneration, activeFenceExpiresAt)
	}
	if _, err := fixture.db.Exec(ctx, `
		delete from public.deletion_fence_leases
		where project_id = 'default' and object_kind = 'record' and object_id = $1`, created.RecordID); err != nil {
		t.Fatalf("remove active fence after idempotent verification: %v", err)
	}

	if _, err := fixture.db.Exec(ctx, `
		delete from public.record_deletion_audits
		where operation_id = $1
		  and event_kind = 'committed'`, entry.Request.OperationID); err != nil {
		t.Fatalf("remove recovery audit for idempotent verification: %v", err)
	}
	_, err = adapter.Replay(ctx, replayRequest)
	var auditPgErr *pgconn.PgError
	if !errors.As(err, &auditPgErr) || auditPgErr.Code != "42501" || auditPgErr.Message != "record recovery audit mismatch" {
		t.Fatalf("Replay(idempotent with missing audit) error = %v, want audit mismatch rejection", err)
	}
}

func TestPostgresIntegrationRecordDeletionRecoveryDeleteCommitReusesPreviewReservationWithoutOperation(t *testing.T) {
	ctx := context.Background()
	var request recorddeletion.LedgerAppendRequest
	legacy, _ := newRecordsPostgresLegacyDeletionFixture(t, ctx,
		"rec_pgrecoverypreview", "Recovery preview record", "record-recovery-preview-create",
		func(ctx context.Context, conn *pgx.Conn, created records.RevisionCommitResult, deploymentID recordplatform.DeploymentID) error {
			request = recordsPostgresRecoveryRequest(t, created, "rpo_pgrecoverypreview", deploymentID, recorddeletion.LedgerEntryDeleteCommit, 0)
			return seedRecordsPostgresLegacyDeletionHistory(ctx, conn, created, request, "drs_pgrecoverypreview", false)
		})
	fixture := legacy.platform
	deploymentID := recordsPostgresActiveDeletionDeployment(t, ctx, fixture)
	ledger := newRecordsPostgresRecoveryLedger(t, ctx, legacy)
	cursor := ledger.prior(t, ctx, deploymentID)
	seedRecordsPostgresRecoveryCursor(t, ctx, fixture, cursor)
	if request.DeploymentID != deploymentID {
		t.Fatal("historical preview deployment differs from activated deployment")
	}
	entry, _ := ledger.record(t, ctx, request)
	if _, err := ledger.adapter.Replay(ctx, ledger.replayRequest(cursor, entry)); err != nil {
		t.Fatalf("Replay(delete commit from preview-only backup) error = %v", err)
	}

	var (
		reservationID    string
		recoveryReplayed bool
		reservationCount int
		rootCount        int
	)
	if err := fixture.db.QueryRow(ctx, `
		select operation.reservation_id,
		       reservation.recovery_replayed,
		       (select count(*)::int
		          from public.deletion_reservations candidate
		         where candidate.deletion_token_commitment = reservation.deletion_token_commitment),
		       (select count(*)::int from public.records where record_id = reservation.object_id)
		from public.record_purge_operations operation
		join public.deletion_reservations reservation
		  on reservation.reservation_id = operation.reservation_id
		where operation.operation_id = $1`, entry.Request.OperationID).Scan(
		&reservationID,
		&recoveryReplayed,
		&reservationCount,
		&rootCount,
	); err != nil {
		t.Fatalf("read preview-only recovery projection: %v", err)
	}
	if reservationID != "drs_pgrecoverypreview" || recoveryReplayed || reservationCount != 1 || rootCount != 0 {
		t.Fatalf("preview-only recovery reservation/replayed/count/root = %q/%t/%d/%d, want drs_pgrecoverypreview/false/1/0",
			reservationID, recoveryReplayed, reservationCount, rootCount)
	}
}

func TestPostgresIntegrationRecordDeletionRecoveryNotCommittedReleasesExistingFenceWithoutPurging(t *testing.T) {
	ctx := context.Background()
	var request recorddeletion.LedgerAppendRequest
	legacy, created := newRecordsPostgresLegacyDeletionFixture(t, ctx,
		"rec_pgrecoveryoutcome", "Recovery outcome record", "record-recovery-outcome-create",
		func(ctx context.Context, conn *pgx.Conn, created records.RevisionCommitResult, deploymentID recordplatform.DeploymentID) error {
			request = recordsPostgresRecoveryRequest(t, created, "rpo_pgrecoveryoutcome", deploymentID, recorddeletion.LedgerEntryDeleteCommit, 0)
			return seedRecordsPostgresLegacyDeletionHistory(ctx, conn, created, request, "drs_pgrecoveryoutcome", true)
		})
	fixture := legacy.platform
	recordRepository := newRevisionMutationAdmittedRepository(t, legacy)
	deploymentID := recordsPostgresActiveDeletionDeployment(t, ctx, fixture)
	if request.DeploymentID != deploymentID {
		t.Fatal("historical operation deployment differs from activated deployment")
	}
	request = request.AttemptNotCommitted(7)
	ledger := newRecordsPostgresRecoveryLedger(t, ctx, legacy)
	cursor := ledger.prior(t, ctx, deploymentID)
	seedRecordsPostgresRecoveryCursor(t, ctx, fixture, cursor)
	entry, _ := ledger.record(t, ctx, request)
	replayRequest := ledger.replayRequest(cursor, entry)
	adapter := ledger.adapter
	receipt, err := adapter.Replay(ctx, replayRequest)
	if err != nil {
		t.Fatalf("Replay(attempt not committed) error = %v", err)
	}
	if receipt.ContentPurged || receipt.Sequence != entry.Sequence || receipt.EntryHash != entry.EntryHash {
		t.Fatalf("Replay(attempt not committed) = %#v", receipt)
	}

	var (
		recoveryReplayed bool
		reservationState string
		reservationEpoch int64
		operationState   string
		operationEpoch   int64
		activeFenceCount int
		rootCount        int
		revisionCount    int
		deliveryCount    int
		coreReceiptCount int
		auditCount       int
		cursorSequence   int64
		cursorHash       []byte
	)
	if err := fixture.db.QueryRow(ctx, `
		select reservation.recovery_replayed,
		       reservation.state,
		       reservation.release_epoch,
		       operation.operation_state,
		       operation.release_epoch,
		       (select count(*)::int
		          from public.deletion_fence_leases fence
		         where fence.project_id = reservation.project_id
		           and fence.object_kind = reservation.object_kind
		           and fence.object_id = reservation.object_id
		           and fence.expires_at > transaction_timestamp()),
		       (select count(*)::int from public.records where record_id = reservation.object_id),
		       (select count(*)::int from public.record_revisions where record_id = reservation.object_id),
		       (select count(*)::int
		          from public.content_delivery_epochs epoch
		         where epoch.project_id = reservation.project_id
		           and epoch.object_kind = reservation.object_kind
		           and epoch.object_id = reservation.object_id),
		       (select count(*)::int
		          from public.record_core_purge_receipts receipt
		         where receipt.operation_id = operation.operation_id),
		       (select count(*)::int
		          from public.record_deletion_audits audit
		         where audit.operation_id = operation.operation_id
		           and audit.event_kind = 'not_committed'),
		       replay.applied_ledger_sequence,
		       replay.applied_ledger_hash
		from public.record_purge_operations operation
		join public.deletion_reservations reservation
		  on reservation.reservation_id = operation.reservation_id
		join public.deletion_replay_state replay
		  on replay.project_id = operation.project_id
		where operation.operation_id = $1`, request.OperationID).Scan(
		&recoveryReplayed,
		&reservationState,
		&reservationEpoch,
		&operationState,
		&operationEpoch,
		&activeFenceCount,
		&rootCount,
		&revisionCount,
		&deliveryCount,
		&coreReceiptCount,
		&auditCount,
		&cursorSequence,
		&cursorHash,
	); err != nil {
		t.Fatalf("read recovery outcome projection: %v", err)
	}
	if recoveryReplayed || reservationState != "not_committed" || reservationEpoch != 7 ||
		operationState != string(recorddeletion.DeletionStateNotCommitted) || operationEpoch != 7 ||
		activeFenceCount != 0 || rootCount != 1 || revisionCount != 1 || deliveryCount != 1 ||
		coreReceiptCount != 0 || auditCount != 1 || cursorSequence != int64(entry.Sequence) ||
		!deletionStoreDigestEqual(cursorHash, entry.EntryHash) {
		t.Fatalf("recovery outcome replayed/state/release/op/release/fence/root/revision/epoch/receipt/audit/cursor = %t/%q/%d/%q/%d/%d/%d/%d/%d/%d/%d/%d",
			recoveryReplayed, reservationState, reservationEpoch, operationState, operationEpoch,
			activeFenceCount, rootCount, revisionCount, deliveryCount, coreReceiptCount, auditCount, cursorSequence)
	}

	var currentDeliveryEpoch int64
	if err := fixture.db.QueryRow(ctx, `
		select delivery_epoch
		from public.content_delivery_epochs
		where project_id = $1 and object_kind = $2 and object_id = $3`,
		recordplatform.ProjectIDDefault,
		request.Object.ObjectKind,
		request.Object.ObjectID,
	).Scan(&currentDeliveryEpoch); err != nil {
		t.Fatalf("read current content delivery epoch: %v", err)
	}
	// A delayed V1 outcome must preserve an independently acquired newer lease.
	// V1 preview/reserve is closed after C81, so install only the live lease.
	if result, err := fixture.db.Exec(ctx, `
		insert into public.deletion_fence_leases
			(project_id, object_kind, object_id, owner_id, owner_generation, expires_at)
		values ($1, $2, $3, 'recovery_newer_owner', 2, clock_timestamp() + interval '5 minutes')
		on conflict (project_id, object_kind, object_id) do update
		set owner_id=excluded.owner_id, owner_generation=excluded.owner_generation, expires_at=excluded.expires_at
		where public.deletion_fence_leases.expires_at <= clock_timestamp()`,
		request.Object.ProjectID, request.Object.ObjectKind, request.Object.ObjectID); err != nil || result.RowsAffected() != 1 {
		t.Fatalf("seed newer independent fence: rows=%d error=%v", result.RowsAffected(), err)
	}

	var (
		newFenceOwner      string
		newFenceGeneration int64
		newFenceExpiresAt  time.Time
	)
	if err := fixture.db.QueryRow(ctx, `
		select owner_id, owner_generation, expires_at
		from public.deletion_fence_leases
		where project_id = $1 and object_kind = $2 and object_id = $3`,
		request.Object.ProjectID,
		request.Object.ObjectKind,
		request.Object.ObjectID,
	).Scan(&newFenceOwner, &newFenceGeneration, &newFenceExpiresAt); err != nil {
		t.Fatalf("read newer deletion fence: %v", err)
	}

	if _, err := adapter.Replay(ctx, replayRequest); err != nil {
		t.Fatalf("Replay(applied outcome with newer fence) error = %v", err)
	}

	if _, err := fixture.db.Exec(ctx, `
		update public.deletion_replay_state
		set applied_ledger_sequence = $2, applied_ledger_hash = $3
		where project_id = $1`,
		recordplatform.ProjectIDDefault,
		int64(cursor.Sequence),
		cursor.EntryHash[:],
	); err != nil {
		t.Fatalf("rewind recovery cursor before delayed outcome replay: %v", err)
	}
	if _, err := adapter.Replay(ctx, replayRequest); err != nil {
		t.Fatalf("Replay(delayed outcome with newer fence) error = %v", err)
	}

	var (
		preservedOwner      string
		preservedGeneration int64
		preservedExpiresAt  time.Time
	)
	if err := fixture.db.QueryRow(ctx, `
		select owner_id, owner_generation, expires_at
		from public.deletion_fence_leases
		where project_id = $1 and object_kind = $2 and object_id = $3`,
		request.Object.ProjectID,
		request.Object.ObjectKind,
		request.Object.ObjectID,
	).Scan(&preservedOwner, &preservedGeneration, &preservedExpiresAt); err != nil {
		t.Fatalf("read newer deletion fence after delayed replay: %v", err)
	}
	if preservedOwner != newFenceOwner || preservedGeneration != newFenceGeneration ||
		!preservedExpiresAt.Equal(newFenceExpiresAt) {
		t.Fatalf("newer deletion fence changed across old outcome replay: got %q/%d/%s, want %q/%d/%s",
			preservedOwner, preservedGeneration, preservedExpiresAt,
			newFenceOwner, newFenceGeneration, newFenceExpiresAt)
	}
	if _, err := recordRepository.ReadRecordRevision(ctx, records.StoredRecordRevisionRequest{
		RecordID:           created.RecordID,
		RevisionID:         created.RevisionID,
		CurrentRevisionID:  created.RevisionID,
		LockVersion:        created.LockVersion,
		AuthorizationEpoch: created.AuthorizationEpoch,
	}); !errors.Is(err, records.ErrRecordDeletionReserved) {
		t.Fatalf("ReadRecordRevision() with preserved newer fence error = %v, want ErrRecordDeletionReserved", err)
	}
}

func TestPostgresIntegrationRecordDeletionRecoveryNotCommittedCreatesSyntheticTerminalProjectionWithoutPurging(t *testing.T) {
	ctx := context.Background()
	legacy, created := newRecordsPostgresLegacyDeletionFixture(t, ctx,
		"rec_pgrecoverysynthetic", "Recovery synthetic outcome record", "record-recovery-synthetic-outcome-create")
	fixture := legacy.platform
	recordRepository := newRevisionMutationAdmittedRepository(t, legacy)

	deploymentID := recordsPostgresActiveDeletionDeployment(t, ctx, fixture)
	ledger := newRecordsPostgresRecoveryLedger(t, ctx, legacy)
	cursor := ledger.prior(t, ctx, deploymentID)
	seedRecordsPostgresRecoveryCursor(t, ctx, fixture, cursor)
	entry, witnessProof := ledger.record(t, ctx, recordsPostgresRecoveryRequest(
		t, created, "rpo_pgrecoverysynthetic", deploymentID, recorddeletion.LedgerEntryAttemptNotCommitted, 9))
	replayRequest := ledger.replayRequest(cursor, entry)
	adapter := ledger.adapter

	first, err := adapter.Replay(ctx, replayRequest)
	if err != nil {
		t.Fatalf("Replay(synthetic attempt not committed) error = %v", err)
	}
	second, err := adapter.Replay(ctx, replayRequest)
	if err != nil {
		t.Fatalf("Replay(synthetic attempt not committed idempotent) error = %v", err)
	}
	if first != second || first.ContentPurged || first.Sequence != entry.Sequence || first.EntryHash != entry.EntryHash {
		t.Fatalf("synthetic recovery receipts first=%#v second=%#v", first, second)
	}

	var (
		reservationID    string
		recoveryReplayed bool
		reservationState string
		reservationEpoch int64
		previewTupleNull bool
		operationState   string
		operationEpoch   int64
		entryType        string
		witnessDigest    []byte
		receiptIsNull    bool
		activeFenceCount int
		rootCount        int
		revisionCount    int
		deliveryCount    int
		coreReceiptCount int
		auditCount       int
		cursorSequence   int64
		cursorHash       []byte
	)
	if err := fixture.db.QueryRow(ctx, `
		select reservation.reservation_id,
		       reservation.recovery_replayed,
		       reservation.state,
		       reservation.release_epoch,
		       reservation.actor_scope_digest is null
		         and reservation.preview_binding_digest is null
		         and reservation.preview_current_revision_id is null
		         and reservation.preview_lock_version is null
		         and reservation.preview_authorization_epoch is null
		         and reservation.preview_content_delivery_epoch is null
		         and reservation.preview_dependency_graph_digest is null
		         and reservation.preview_backup_inventory_digest is null
		         and reservation.preview_processor_inventory_digest is null
		         and reservation.adapter_readiness_digest is null
		         and reservation.adapter_preview_digest is null
		         and reservation.preview_witness_sequence is null
		         and reservation.preview_witness_entry_hash is null,
		       operation.operation_state,
		       operation.release_epoch,
		       operation.ledger_entry_type,
		       operation.witness_proof_digest,
		       operation.receipt_digest is null,
		       (select count(*)::int
		          from public.deletion_fence_leases fence
		         where fence.project_id = reservation.project_id
		           and fence.object_kind = reservation.object_kind
		           and fence.object_id = reservation.object_id
		           and fence.expires_at > transaction_timestamp()),
		       (select count(*)::int from public.records where record_id = reservation.object_id),
		       (select count(*)::int from public.record_revisions where record_id = reservation.object_id),
		       (select count(*)::int
		          from public.content_delivery_epochs epoch
		         where epoch.project_id = reservation.project_id
		           and epoch.object_kind = reservation.object_kind
		           and epoch.object_id = reservation.object_id),
		       (select count(*)::int
		          from public.record_core_purge_receipts receipt
		         where receipt.operation_id = operation.operation_id),
		       (select count(*)::int
		          from public.record_deletion_audits audit
		         where audit.operation_id = operation.operation_id
		           and audit.event_kind = 'not_committed'),
		       replay.applied_ledger_sequence,
		       replay.applied_ledger_hash
		from public.record_purge_operations operation
		join public.deletion_reservations reservation
		  on reservation.reservation_id = operation.reservation_id
		join public.deletion_replay_state replay
		  on replay.project_id = operation.project_id
		where operation.operation_id = $1`, entry.Request.OperationID).Scan(
		&reservationID,
		&recoveryReplayed,
		&reservationState,
		&reservationEpoch,
		&previewTupleNull,
		&operationState,
		&operationEpoch,
		&entryType,
		&witnessDigest,
		&receiptIsNull,
		&activeFenceCount,
		&rootCount,
		&revisionCount,
		&deliveryCount,
		&coreReceiptCount,
		&auditCount,
		&cursorSequence,
		&cursorHash,
	); err != nil {
		t.Fatalf("read synthetic recovery outcome projection: %v", err)
	}
	if !validStoredRecordIdentity(reservationID, "drs_") || !recoveryReplayed ||
		reservationState != "not_committed" || reservationEpoch != 9 || !previewTupleNull ||
		operationState != string(recorddeletion.DeletionStateNotCommitted) || operationEpoch != 9 ||
		entryType != string(recorddeletion.LedgerEntryAttemptNotCommitted) ||
		!deletionStoreDigestEqual(witnessDigest, witnessProof) || !receiptIsNull ||
		activeFenceCount != 0 || rootCount != 1 || revisionCount != 1 || deliveryCount != 1 ||
		coreReceiptCount != 0 || auditCount != 1 || cursorSequence != int64(entry.Sequence) ||
		!deletionStoreDigestEqual(cursorHash, entry.EntryHash) {
		t.Fatalf("synthetic recovery outcome reservation/replayed/state/release/null/op/release/type/receipt/fence/root/revision/epoch/receipt/audit/cursor = %q/%t/%q/%d/%t/%q/%d/%q/%t/%d/%d/%d/%d/%d/%d/%d",
			reservationID, recoveryReplayed, reservationState, reservationEpoch, previewTupleNull,
			operationState, operationEpoch, entryType, receiptIsNull, activeFenceCount,
			rootCount, revisionCount, deliveryCount, coreReceiptCount, auditCount, cursorSequence)
	}
	if _, err := recordRepository.ReadRecordRevision(ctx, records.StoredRecordRevisionRequest{
		RecordID:           created.RecordID,
		RevisionID:         created.RevisionID,
		CurrentRevisionID:  created.RevisionID,
		LockVersion:        created.LockVersion,
		AuthorizationEpoch: created.AuthorizationEpoch,
	}); err != nil {
		t.Fatalf("ReadRecordRevision() after synthetic recovery outcome error = %v", err)
	}
}

func TestPostgresIntegrationRecordDeletionRecoveryReceiptFailureRollsBackContentProjectionAndCursor(t *testing.T) {
	ctx := context.Background()
	legacy, created := newRecordsPostgresLegacyDeletionFixture(t, ctx,
		"rec_pgrecoveryrollback", "Recovery rollback record", "record-recovery-rollback-create")
	fixture := legacy.platform

	deploymentID := recordsPostgresActiveDeletionDeployment(t, ctx, fixture)
	ledger := newRecordsPostgresRecoveryLedger(t, ctx, legacy)
	cursor := ledger.prior(t, ctx, deploymentID)
	seedRecordsPostgresRecoveryCursor(t, ctx, fixture, cursor)
	entry, _ := ledger.record(t, ctx, recordsPostgresRecoveryRequest(
		t, created, "rpo_pgrecoveryrollback", deploymentID, recorddeletion.LedgerEntryDeleteCommit, 0))
	const failureConstraint = "record_core_purge_receipts_recovery_failure_test"
	if _, err := fixture.db.Exec(ctx, `alter table public.record_core_purge_receipts add constraint `+failureConstraint+` check (false) not valid`); err != nil {
		t.Fatalf("add recovery receipt failure constraint: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fixture.db.Exec(context.Background(), `alter table public.record_core_purge_receipts drop constraint if exists `+failureConstraint)
	})

	_, err := ledger.adapter.Replay(ctx, ledger.replayRequest(cursor, entry))
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != failureConstraint {
		t.Fatalf("Replay(delete commit with receipt failure) error = %v, want violation of %s", err, failureConstraint)
	}

	var (
		rootCount        int
		revisionCount    int
		deliveryCount    int
		reservationCount int
		operationCount   int
		auditCount       int
		receiptCount     int
		cursorSequence   int64
		cursorHash       []byte
	)
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.records where record_id = $1),
		       (select count(*)::int from public.record_revisions where record_id = $1),
		       (select count(*)::int
		          from public.content_delivery_epochs
		         where project_id = 'default' and object_kind = 'record' and object_id = $1),
		       (select count(*)::int
		          from public.deletion_reservations
		         where project_id = 'default' and object_kind = 'record' and object_id = $1),
		       (select count(*)::int from public.record_purge_operations where operation_id = $2),
		       (select count(*)::int from public.record_deletion_audits where operation_id = $2),
		       (select count(*)::int from public.record_core_purge_receipts where operation_id = $2),
		       replay.applied_ledger_sequence,
		       replay.applied_ledger_hash
		from public.deletion_replay_state replay
		where replay.project_id = 'default'`, created.RecordID, entry.Request.OperationID).Scan(
		&rootCount,
		&revisionCount,
		&deliveryCount,
		&reservationCount,
		&operationCount,
		&auditCount,
		&receiptCount,
		&cursorSequence,
		&cursorHash,
	); err != nil {
		t.Fatalf("read recovery rollback state: %v", err)
	}
	if rootCount != 1 || revisionCount != 1 || deliveryCount != 1 || reservationCount != 0 ||
		operationCount != 0 || auditCount != 0 || receiptCount != 0 || cursorSequence != int64(cursor.Sequence) ||
		!deletionStoreDigestEqual(cursorHash, cursor.EntryHash) {
		t.Fatalf("recovery rollback root/revision/epoch/reservation/operation/audit/receipt/cursor = %d/%d/%d/%d/%d/%d/%d/%d",
			rootCount, revisionCount, deliveryCount, reservationCount,
			operationCount, auditCount, receiptCount, cursorSequence)
	}
}

// Recovery replays V1 history, so its target and optional deletion history
// must exist before C81. This seeds backup content, not replay authority;
// the real ledger verifier and held fence still authorize every replay.
func newRecordsPostgresLegacyDeletionFixture(
	t *testing.T, ctx context.Context, recordID, title, key string,
	history ...func(context.Context, *pgx.Conn, records.RevisionCommitResult, recordplatform.DeploymentID) error,
) (ImportAttachmentGCFixture, records.RevisionCommitResult) {
	t.Helper()
	input := recordsPostgresCompleteRevisionInput(t, title)
	command := recordsPostgresRevisionCommand(t, recordplatform.OperationKindRecordCreate,
		recordID, "", 0, 0, input, key)
	prepared, err := prepareRecordRevision(command)
	if err != nil {
		t.Fatalf("prepare predecessor record: %v", err)
	}
	created := records.RevisionCommitResult{
		RecordID: recordID, RevisionID: prepared.revisionID, RevisionNo: 1,
		LockVersion: 1, AuthorizationEpoch: 1, Lifecycle: records.LifecycleActive, Created: true,
	}
	legacy := newImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false,
		func(ctx context.Context, conn *pgx.Conn, deploymentID recordplatform.DeploymentID) error {
			var beforeC81 bool
			if err := conn.QueryRow(ctx, `select to_regclass('public.record_deletion_gates') is null`).Scan(&beforeC81); err != nil || !beforeC81 {
				return fmt.Errorf("predecessor record must be seeded before C81: before=%t: %v", beforeC81, err)
			}
			if _, err := conn.Exec(ctx, `insert into public.records (record_id) values ($1)`, recordID); err != nil {
				return fmt.Errorf("seed predecessor root: %w", err)
			}
			if err := conn.QueryRow(ctx, `
				insert into public.record_revisions (
					revision_id, record_id, revision_no, title, body_markdown,
					markdown_dialect_version, record_type, impact_level, visibility_scope,
					visibility_digest, owner_id, author_id, canonical_hash
				) values ($1, $2, 1, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
				returning created_at`, prepared.revisionID, recordID, input.Title(), input.BodyMarkdown(),
				int64(input.MarkdownDialectVersion()), string(input.RecordType()), string(input.ImpactLevel()),
				prepared.visibilityJSON, prepared.visibilityHash[:], input.OwnerID(), input.AuthorID(),
				prepared.canonicalHash[:]).Scan(&created.CommittedAt); err != nil {
				return fmt.Errorf("seed predecessor revision: %w", err)
			}
			for ordinal, subject := range prepared.subjects {
				if _, err := conn.Exec(ctx, `
					insert into public.record_revision_subjects (
						revision_id, ordinal, registry_version, subject_kind, relation_role,
						source_id, is_primary, identity_snapshot, capture_authorization, capture_authorization_digest
					) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, prepared.revisionID, int64(ordinal),
					int64(subject.subject.RegistryVersion), string(subject.subject.Kind), string(subject.subject.Role),
					subject.subject.SourceID, subject.subject.Primary, subject.identityJSON, subject.authorizationJSON,
					subject.subject.CaptureAuthorization.Digest[:]); err != nil {
					return fmt.Errorf("seed predecessor subject: %w", err)
				}
			}
			for ordinal, tag := range prepared.tags {
				if _, err := conn.Exec(ctx, `insert into public.record_revision_tags (revision_id, ordinal, tag_value) values ($1,$2,$3)`,
					prepared.revisionID, int64(ordinal), tag); err != nil {
					return fmt.Errorf("seed predecessor tag: %w", err)
				}
			}
			for ordinal, participant := range prepared.participants {
				if _, err := conn.Exec(ctx, `insert into public.record_revision_participants (revision_id, ordinal, participant_id, identity_snapshot) values ($1,$2,$3,$4)`,
					prepared.revisionID, int64(ordinal), participant.participant.ParticipantID, participant.identityJSON); err != nil {
					return fmt.Errorf("seed predecessor participant: %w", err)
				}
			}
			if _, err := conn.Exec(ctx, `
				update public.records set current_revision_id=$2, current_title=$3, current_record_type=$4,
					current_impact_level=$5, current_visibility_scope=$6, current_visibility_digest=$7,
					current_owner_id=$8, lock_version=1, authorization_epoch=1 where record_id=$1`,
				recordID, prepared.revisionID, input.Title(), string(input.RecordType()), string(input.ImpactLevel()),
				prepared.visibilityJSON, prepared.visibilityHash[:], input.OwnerID()); err != nil {
				return fmt.Errorf("seed predecessor current projection: %w", err)
			}
			if _, err := conn.Exec(ctx, `
				insert into public.record_domain_activities (activity_id, project_id, record_id, revision_id,
					event_kind, source_event_id, source_version, actor_id, authorization_epoch, record_lock_version, event_at)
				values ($1, 'default', $2, $3, 'record_created', $3, 1, $4, 1, 1, transaction_timestamp())`,
				prepared.activityID, recordID, prepared.revisionID, input.AuthorID()); err != nil {
				return fmt.Errorf("seed predecessor activity: %w", err)
			}
			if _, err := conn.Exec(ctx, `insert into public.content_delivery_epochs (project_id, object_kind, object_id, delivery_epoch) values ('default','record',$1,0)`, recordID); err != nil {
				return fmt.Errorf("seed predecessor delivery epoch: %w", err)
			}
			for _, seed := range history {
				if err := seed(ctx, conn, created, deploymentID); err != nil {
					return err
				}
			}
			return nil
		})
	return legacy, created
}

// Seed the pre-C81 backup state that replay consumes. This never creates a
// verified entry or a recovery fence, and cannot authorize replay by itself.
func seedRecordsPostgresLegacyDeletionHistory(
	ctx context.Context, conn *pgx.Conn, created records.RevisionCommitResult,
	request recorddeletion.LedgerAppendRequest, reservationID string, fenced bool,
) error {
	fingerprint, err := request.RequestFingerprint.PersistedBytes()
	if err != nil {
		return err
	}
	state, owner, generation := "previewed", "", int64(0)
	if fenced {
		state, owner, generation = "fenced", "recovery_outcome_owner", 1
	}
	if _, err := conn.Exec(ctx, `
		insert into public.deletion_reservations (
			reservation_id, project_id, object_kind, object_id,
			deletion_token_commitment, request_fingerprint, state, fence_epoch,
			owner_id, owner_generation, owner_expires_at, expires_at,
			actor_scope_digest, preview_binding_digest, preview_current_revision_id,
			preview_lock_version, preview_authorization_epoch, preview_content_delivery_epoch,
			preview_dependency_graph_digest, preview_backup_inventory_digest,
			preview_processor_inventory_digest, adapter_readiness_digest, adapter_preview_digest,
			preview_witness_sequence, preview_witness_entry_hash
		) values ($1,'default','record',$2,$3,$4,$5,$6,$7,$6,
			case when $6::bigint > 0 then transaction_timestamp()+interval '5 minutes' else null end,
			transaction_timestamp()+interval '5 minutes',
			decode(repeat('71',32),'hex'),decode(repeat('75',32),'hex'),$8,$9,$10,0,
			decode(repeat('73',32),'hex'),decode(repeat('74',32),'hex'),
			decode(repeat('76',32),'hex'),decode(repeat('78',32),'hex'),decode(repeat('79',32),'hex'),
			7,decode(repeat('77',32),'hex'))`,
		reservationID, created.RecordID, request.TokenCommitment[:], fingerprint[:], state,
		generation, owner, created.RevisionID, int64(created.LockVersion), int64(created.AuthorizationEpoch)); err != nil {
		return fmt.Errorf("seed predecessor preview: %w", err)
	}
	if !fenced {
		return nil
	}
	if _, err := conn.Exec(ctx, `
		insert into public.record_purge_operations (
			operation_id, reservation_id, project_id, operation_state, deployment_id,
			actor_id, reason_code, deletion_contract_version, owner_id, owner_generation, owner_expires_at
		) values ($1,$2,'default','provisional_fenced',$3,$4,$5,1,$6,1,transaction_timestamp()+interval '5 minutes')`,
		request.OperationID, reservationID, string(request.DeploymentID), request.ActorID, string(request.ReasonCode), owner); err != nil {
		return fmt.Errorf("seed predecessor operation: %w", err)
	}
	if _, err := conn.Exec(ctx, `
		insert into public.deletion_fence_leases
			(project_id, object_kind, object_id, owner_id, owner_generation, expires_at)
		values ('default','record',$1,$2,1,transaction_timestamp()+interval '5 minutes')`, created.RecordID, owner); err != nil {
		return fmt.Errorf("seed predecessor fence: %w", err)
	}
	if _, err := conn.Exec(ctx, `update public.content_delivery_epochs set delivery_epoch=1
		where project_id='default' and object_kind='record' and object_id=$1`, created.RecordID); err != nil {
		return fmt.Errorf("seed predecessor fenced delivery epoch: %w", err)
	}
	return nil
}

func recordsPostgresRecoveryRequest(
	t *testing.T,
	record records.RevisionCommitResult,
	operationID string,
	deploymentID recordplatform.DeploymentID,
	entryType recorddeletion.LedgerEntryType,
	releaseEpoch uint64,
) recorddeletion.LedgerAppendRequest {
	t.Helper()
	preview, _ := recordsPostgresDeletionPreviewCommand(t, record, deploymentID)
	fingerprintBytes, err := preview.RequestFingerprint.PersistedBytes()
	if err != nil {
		t.Fatalf("PersistedBytes() error = %v", err)
	}
	persistedFingerprint, err := recordplatform.ParseTrustedPersistedRequestFingerprintV1(fingerprintBytes[:])
	if err != nil {
		t.Fatalf("ParseTrustedPersistedRequestFingerprintV1() error = %v", err)
	}
	request := recorddeletion.LedgerAppendRequest{
		EntryType:               recorddeletion.LedgerEntryDeleteCommit,
		DeploymentID:            deploymentID,
		ProjectID:               recordplatform.ProjectIDDefault,
		OperationID:             operationID,
		ActorID:                 "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
		Object:                  preview.Object,
		TokenCommitment:         preview.TokenCommitment,
		RequestFingerprint:      persistedFingerprint,
		ReasonCode:              recorddeletion.DeletionReasonUserConfirmed,
		DeletionContractVersion: recorddeletion.RecordDeletionContractVersionV1,
	}
	if entryType == recorddeletion.LedgerEntryAttemptNotCommitted {
		request = request.AttemptNotCommitted(releaseEpoch)
	}
	if err := request.Validate(); err != nil {
		t.Fatalf("recovery ledger request invalid: %v", err)
	}
	return request
}

func seedRecordsPostgresRecoveryCursor(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	cursor recorddeletion.RecoveryReplayCursor,
) {
	t.Helper()
	if _, err := fixture.db.Exec(ctx, `
		insert into public.deletion_replay_state (
			project_id, applied_ledger_sequence, applied_ledger_hash, updated_at
		) values ('default', $1, $2, transaction_timestamp())
		on conflict (project_id) do update
		set applied_ledger_sequence = excluded.applied_ledger_sequence,
		    applied_ledger_hash = excluded.applied_ledger_hash,
		    updated_at = transaction_timestamp()`, int64(cursor.Sequence), cursor.EntryHash[:]); err != nil {
		t.Fatalf("seed recovery cursor: %v", err)
	}
}
