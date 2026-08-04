package store

import (
	"context"
	"errors"
	"testing"

	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

func TestPostgresIntegrationRecordDeletionRecoveryDeleteCommitIsAtomicIdempotentAndContinuous(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-deletion-recovery-delete", 2)
	recordRepository := newRecordsPostgresRepository(t, runtimePool)
	draftRepository := newRecordsPostgresDraftRepository(runtimePool)
	created, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		"rec_pgrecoverydelete",
		"",
		0,
		0,
		recordsPostgresCompleteRevisionInput(t, "Recovery delete record"),
		"record-recovery-delete-create",
	))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}
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

	cursor := recorddeletion.RecoveryReplayCursor{
		Sequence:  30,
		EntryHash: testStoreRecordPlatformDigest(0xa1),
	}
	seedRecordsPostgresRecoveryCursor(t, ctx, fixture, cursor)
	entry, fingerprintBytes := recordsPostgresRecoveryLedgerEntry(
		t,
		created,
		"rpo_pgrecoverydelete",
		recordplatform.DeploymentID("dp-eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"),
		cursor.Sequence+1,
		recorddeletion.LedgerEntryDeleteCommit,
		0,
	)
	replayRequest := recordsPostgresRecoveryReplayRequest(entry, cursor, fingerprintBytes)
	repository := NewPostgresRecordDeletionRepository(runtimePool, allowRecordPlatformAdmissionGate)
	adapter, err := recorddeletion.NewRecoveryAdapter(repository)
	if err != nil {
		t.Fatalf("NewRecoveryAdapter() error = %v", err)
	}

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
		         where receipt.operation_id = operation.operation_id
		           and receipt.record_id = reservation.object_id),
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
		!deletionStoreDigestEqual(witnessDigest, replayRequest.WitnessProofDigest) || len(receiptDigest) != 32 ||
		auditCount != 1 || coreReceiptCount != 1 || cursorSequence != int64(entry.Sequence) ||
		!deletionStoreDigestEqual(cursorHash, entry.EntryHash) {
		t.Fatalf("recovery terminal projection replayed/state/fence/null/op/type/sequence/audit/receipt/cursor = %t/%q/%d/%t/%q/%q/%d/%d/%d/%d",
			recoveryReplayed, reservationState, fenceEpoch, previewTupleNull, operationState,
			entryType, ledgerSequence, auditCount, coreReceiptCount, cursorSequence)
	}

	staleCursor := recorddeletion.RecoveryReplayCursor{
		Sequence:  entry.Sequence + 1,
		EntryHash: testStoreRecordPlatformDigest(0xa2),
	}
	staleEntry := entry
	staleEntry.Request.OperationID = "rpo_pgrecoverystale"
	staleEntry.Sequence = staleCursor.Sequence + 1
	staleEntry.EntryHash = testStoreRecordPlatformDigest(0xa3)
	if err := staleEntry.Validate(); err != nil {
		t.Fatalf("stale recovery entry invalid: %v", err)
	}
	_, err = adapter.Replay(ctx, recordsPostgresRecoveryReplayRequest(staleEntry, staleCursor, fingerprintBytes))
	if !errors.Is(err, recorddeletion.ErrRecoveryContractUnavailable) {
		t.Fatalf("Replay(stale cursor) error = %v, want ErrRecoveryContractUnavailable", err)
	}
	var staleOperationCount int
	if err := fixture.db.QueryRow(ctx, `
		select count(*)::int from public.record_purge_operations where operation_id = $1`,
		staleEntry.Request.OperationID,
	).Scan(&staleOperationCount); err != nil {
		t.Fatalf("count stale recovery operation: %v", err)
	}
	if staleOperationCount != 0 {
		t.Fatalf("stale recovery operation count = %d, want 0", staleOperationCount)
	}

	if _, err := fixture.db.Exec(ctx, `
		insert into public.deletion_fence_leases (
			project_id, object_kind, object_id, owner_id, owner_generation, expires_at, created_at
		) values ('default', 'record', $1, 'recovery_corrupt_fence', 1,
			transaction_timestamp() + interval '1 minute', transaction_timestamp())`, created.RecordID); err != nil {
		t.Fatalf("insert active fence for idempotent verification: %v", err)
	}
	if _, err := adapter.Replay(ctx, replayRequest); !errors.Is(err, recorddeletion.ErrRecoveryContractUnavailable) {
		t.Fatalf("Replay(idempotent with active fence) error = %v, want ErrRecoveryContractUnavailable", err)
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
	if _, err := adapter.Replay(ctx, replayRequest); !errors.Is(err, recorddeletion.ErrRecoveryContractUnavailable) {
		t.Fatalf("Replay(idempotent with missing audit) error = %v, want ErrRecoveryContractUnavailable", err)
	}
}

func TestPostgresIntegrationRecordDeletionRecoveryDeleteCommitReusesPreviewReservationWithoutOperation(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-deletion-recovery-preview", 2)
	recordRepository := newRecordsPostgresRepository(t, runtimePool)
	created, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		"rec_pgrecoverypreview",
		"",
		0,
		0,
		recordsPostgresCompleteRevisionInput(t, "Recovery preview record"),
		"record-recovery-preview-create",
	))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}

	deploymentID := recordplatform.DeploymentID("dp-dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")
	createCommand, _ := recordsPostgresDeletionPreviewCommand(t, created, deploymentID)
	repository := NewPostgresRecordDeletionRepository(runtimePool, allowRecordPlatformAdmissionGate)
	repository.newReservationID = func() (string, error) { return "drs_pgrecoverypreview", nil }
	preview, err := repository.CreatePreview(ctx, createCommand)
	if err != nil {
		t.Fatalf("CreatePreview() error = %v", err)
	}
	fingerprintBytes, err := createCommand.RequestFingerprint.PersistedBytes()
	if err != nil {
		t.Fatalf("PersistedBytes() error = %v", err)
	}

	cursor := recorddeletion.RecoveryReplayCursor{
		Sequence:  35,
		EntryHash: testStoreRecordPlatformDigest(0xaa),
	}
	seedRecordsPostgresRecoveryCursor(t, ctx, fixture, cursor)
	request := recorddeletion.LedgerAppendRequest{
		EntryType:               recorddeletion.LedgerEntryDeleteCommit,
		DeploymentID:            deploymentID,
		ProjectID:               recordplatform.ProjectIDDefault,
		OperationID:             "rpo_pgrecoverypreview",
		ActorID:                 "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
		Object:                  preview.Object,
		TokenCommitment:         preview.TokenCommitment,
		RequestFingerprint:      preview.RequestFingerprint,
		ReasonCode:              recorddeletion.DeletionReasonUserConfirmed,
		DeletionContractVersion: recorddeletion.RecordDeletionContractVersionV1,
	}
	entry := recorddeletion.DeletionLedgerEntry{
		Request:   request,
		Sequence:  cursor.Sequence + 1,
		EntryHash: testStoreRecordPlatformDigest(0xab),
	}
	if err := entry.Validate(); err != nil {
		t.Fatalf("recovery preview entry invalid: %v", err)
	}
	adapter, err := recorddeletion.NewRecoveryAdapter(repository)
	if err != nil {
		t.Fatalf("NewRecoveryAdapter() error = %v", err)
	}

	if _, err := adapter.Replay(ctx, recordsPostgresRecoveryReplayRequest(entry, cursor, fingerprintBytes)); err != nil {
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
	if reservationID != preview.ReservationID || recoveryReplayed || reservationCount != 1 || rootCount != 0 {
		t.Fatalf("preview-only recovery reservation/replayed/count/root = %q/%t/%d/%d, want %q/false/1/0",
			reservationID, recoveryReplayed, reservationCount, rootCount, preview.ReservationID)
	}
}

func TestPostgresIntegrationRecordDeletionRecoveryNotCommittedReleasesExistingFenceWithoutPurging(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-deletion-recovery-outcome", 2)
	recordRepository := newRecordsPostgresRepository(t, runtimePool)
	created, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		"rec_pgrecoveryoutcome",
		"",
		0,
		0,
		recordsPostgresCompleteRevisionInput(t, "Recovery outcome record"),
		"record-recovery-outcome-create",
	))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}

	deploymentID := recordplatform.DeploymentID("dp-ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	createCommand, _ := recordsPostgresDeletionPreviewCommand(t, created, deploymentID)
	repository := NewPostgresRecordDeletionRepository(runtimePool, allowRecordPlatformAdmissionGate)
	repository.newReservationID = func() (string, error) { return "drs_pgrecoveryoutcome", nil }
	repository.newOperationID = func() (string, error) { return "rpo_pgrecoveryoutcome", nil }
	repository.newAuditID = func() (string, error) { return "rda_pgrecoveryoutcome", nil }
	preview, err := repository.CreatePreview(ctx, createCommand)
	if err != nil {
		t.Fatalf("CreatePreview() error = %v", err)
	}
	reserveCommand := recordsPostgresDeletionReserveCommand(createCommand, preview, deploymentID)
	reserveCommand.OwnerID = "recovery_outcome_owner"
	operation, err := repository.ReservePreview(ctx, reserveCommand)
	if err != nil {
		t.Fatalf("ReservePreview() error = %v", err)
	}

	fingerprintBytes, err := createCommand.RequestFingerprint.PersistedBytes()
	if err != nil {
		t.Fatalf("PersistedBytes() error = %v", err)
	}
	request := recorddeletion.LedgerAppendRequest{
		EntryType:               recorddeletion.LedgerEntryDeleteCommit,
		DeploymentID:            deploymentID,
		ProjectID:               recordplatform.ProjectIDDefault,
		OperationID:             operation.OperationID,
		ActorID:                 reserveCommand.ActorID,
		Object:                  operation.Object,
		TokenCommitment:         preview.TokenCommitment,
		RequestFingerprint:      preview.RequestFingerprint,
		ReasonCode:              operation.ReasonCode,
		DeletionContractVersion: recorddeletion.RecordDeletionContractVersionV1,
	}.AttemptNotCommitted(7)
	cursor := recorddeletion.RecoveryReplayCursor{
		Sequence:  40,
		EntryHash: testStoreRecordPlatformDigest(0xb1),
	}
	seedRecordsPostgresRecoveryCursor(t, ctx, fixture, cursor)
	entry := recorddeletion.DeletionLedgerEntry{
		Request:   request,
		Sequence:  cursor.Sequence + 1,
		EntryHash: testStoreRecordPlatformDigest(0xb2),
	}
	if err := entry.Validate(); err != nil {
		t.Fatalf("recovery outcome entry invalid: %v", err)
	}
	replayRequest := recordsPostgresRecoveryReplayRequest(entry, cursor, fingerprintBytes)
	adapter, err := recorddeletion.NewRecoveryAdapter(repository)
	if err != nil {
		t.Fatalf("NewRecoveryAdapter() error = %v", err)
	}
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
		where operation.operation_id = $1`, operation.OperationID).Scan(
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
	if _, err := recordRepository.ReadRecordRevision(ctx, records.StoredRecordRevisionRequest{
		RecordID:           created.RecordID,
		RevisionID:         created.RevisionID,
		CurrentRevisionID:  created.RevisionID,
		LockVersion:        created.LockVersion,
		AuthorizationEpoch: created.AuthorizationEpoch,
	}); err != nil {
		t.Fatalf("ReadRecordRevision() after recovery outcome error = %v", err)
	}
}

func TestPostgresIntegrationRecordDeletionRecoveryNotCommittedCreatesSyntheticTerminalProjectionWithoutPurging(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-deletion-recovery-synthetic-outcome", 2)
	recordRepository := newRecordsPostgresRepository(t, runtimePool)
	created, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		"rec_pgrecoverysynthetic",
		"",
		0,
		0,
		recordsPostgresCompleteRevisionInput(t, "Recovery synthetic outcome record"),
		"record-recovery-synthetic-outcome-create",
	))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}

	cursor := recorddeletion.RecoveryReplayCursor{
		Sequence:  45,
		EntryHash: testStoreRecordPlatformDigest(0xb5),
	}
	seedRecordsPostgresRecoveryCursor(t, ctx, fixture, cursor)
	entry, fingerprintBytes := recordsPostgresRecoveryLedgerEntry(
		t,
		created,
		"rpo_pgrecoverysynthetic",
		recordplatform.DeploymentID("dp-2222222222222222222222222222222222222222222222222222222222222222"),
		cursor.Sequence+1,
		recorddeletion.LedgerEntryAttemptNotCommitted,
		9,
	)
	replayRequest := recordsPostgresRecoveryReplayRequest(entry, cursor, fingerprintBytes)
	repository := NewPostgresRecordDeletionRepository(runtimePool, allowRecordPlatformAdmissionGate)
	adapter, err := recorddeletion.NewRecoveryAdapter(repository)
	if err != nil {
		t.Fatalf("NewRecoveryAdapter() error = %v", err)
	}

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
		!deletionStoreDigestEqual(witnessDigest, replayRequest.WitnessProofDigest) || !receiptIsNull ||
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
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-deletion-recovery-rollback", 2)
	recordRepository := newRecordsPostgresRepository(t, runtimePool)
	created, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		"rec_pgrecoveryrollback",
		"",
		0,
		0,
		recordsPostgresCompleteRevisionInput(t, "Recovery rollback record"),
		"record-recovery-rollback-create",
	))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}

	cursor := recorddeletion.RecoveryReplayCursor{
		Sequence:  50,
		EntryHash: testStoreRecordPlatformDigest(0xc1),
	}
	seedRecordsPostgresRecoveryCursor(t, ctx, fixture, cursor)
	entry, fingerprintBytes := recordsPostgresRecoveryLedgerEntry(
		t,
		created,
		"rpo_pgrecoveryrollback",
		recordplatform.DeploymentID("dp-1111111111111111111111111111111111111111111111111111111111111111"),
		cursor.Sequence+1,
		recorddeletion.LedgerEntryDeleteCommit,
		0,
	)
	const failureConstraint = "record_core_purge_receipts_recovery_failure_test"
	if _, err := fixture.db.Exec(ctx, `alter table public.record_core_purge_receipts add constraint `+failureConstraint+` check (false) not valid`); err != nil {
		t.Fatalf("add recovery receipt failure constraint: %v", err)
	}
	t.Cleanup(func() {
		_, _ = fixture.db.Exec(context.Background(), `alter table public.record_core_purge_receipts drop constraint if exists `+failureConstraint)
	})

	repository := NewPostgresRecordDeletionRepository(runtimePool, allowRecordPlatformAdmissionGate)
	adapter, err := recorddeletion.NewRecoveryAdapter(repository)
	if err != nil {
		t.Fatalf("NewRecoveryAdapter() error = %v", err)
	}
	if _, err := adapter.Replay(ctx, recordsPostgresRecoveryReplayRequest(entry, cursor, fingerprintBytes)); err == nil {
		t.Fatal("Replay(delete commit with receipt failure) error = nil")
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

func recordsPostgresRecoveryLedgerEntry(
	t *testing.T,
	record records.RevisionCommitResult,
	operationID string,
	deploymentID recordplatform.DeploymentID,
	sequence uint64,
	entryType recorddeletion.LedgerEntryType,
	releaseEpoch uint64,
) (recorddeletion.DeletionLedgerEntry, [32]byte) {
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
	entry := recorddeletion.DeletionLedgerEntry{
		Request:   request,
		Sequence:  sequence,
		EntryHash: testStoreRecordPlatformDigest(byte(sequence)),
	}
	if err := entry.Validate(); err != nil {
		t.Fatalf("recovery ledger entry invalid: %v", err)
	}
	return entry, fingerprintBytes
}

func recordsPostgresRecoveryReplayRequest(
	entry recorddeletion.DeletionLedgerEntry,
	cursor recorddeletion.RecoveryReplayCursor,
	fingerprintBytes [32]byte,
) recorddeletion.RecoveryReplayRequest {
	return recorddeletion.RecoveryReplayRequest{
		Cursor:                  cursor,
		Entry:                   entry,
		PreviousHash:            cursor.EntryHash,
		WitnessProofDigest:      testStoreRecordPlatformDigest(0xd1),
		RequestFingerprintBytes: fingerprintBytes,
	}
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
