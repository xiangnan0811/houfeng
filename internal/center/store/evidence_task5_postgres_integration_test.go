package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"testing"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
)

func TestPostgresIntegrationEvidenceTask5DeletionPreservesCopyAndPayload(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "evidence-task5-deletion", 2)
	recordRepository := newRecordsPostgresRepository(t, runtimePool, NewRecordEvidenceRevisionParticipant())
	repository := NewPostgresEvidenceRepository(runtimePool, allowRecordPlatformAdmissionGate)
	databaseNow := recordEvidenceParticipantDatabaseNow(t, ctx, fixture)
	targetCapture := storePreparedEvidenceCapture(
		t,
		"rec_evdeletetarget",
		"evs_evdeletetarget",
		"evi_333333333333333333333333",
		databaseNow,
	)
	persistRecordEvidenceParticipantPayloadAndIntent(t, ctx, repository, targetCapture)
	targetPreparation := storeEvidenceRevisionPreparation(
		t,
		targetCapture.RecordID(),
		[]evidence.PreparedCapture{targetCapture},
		nil,
		[]string{targetCapture.SnapshotID()},
	)
	target, err := recordRepository.CommitRevision(ctx, recordEvidenceParticipantCommand(
		t, recordplatform.OperationKindRecordCreate, targetCapture.RecordID(), "", 0, 0,
		"Evidence delete target", "evidence-delete-target", targetPreparation,
	))
	if err != nil {
		t.Fatalf("CommitRevision(target) error = %v", err)
	}
	survivor, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordCreate, "rec_evdeletesurvivor", "", 0, 0,
		recordsPostgresCompleteRevisionInput(t, "Evidence delete survivor"), "evidence-delete-survivor",
	))
	if err != nil {
		t.Fatalf("CommitRevision(survivor) error = %v", err)
	}

	snapshot := targetCapture.Snapshot()
	restoredEnvelope := snapshot.Envelope()
	registry, err := evidence.NewRegistry([]evidence.Kind{
		task5RecoveryEvidenceKind{descriptor: storeEvidenceParticipantDescriptor()},
	})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	recoveryAdapter, err := evidence.NewRecoveryAdapter(registry, repository)
	if err != nil {
		t.Fatalf("NewRecoveryAdapter() error = %v", err)
	}
	if err := recoveryAdapter.Replay(ctx, evidence.EvidenceRecoveryInventory{
		Payloads: []evidence.EvidenceRecoveryPayload{{
			Key: restoredEnvelope.Key, Digest: snapshot.Hash(), CanonicalPayload: snapshot.Bytes(),
		}},
		Snapshots: []evidence.EvidenceRecoverySnapshot{{
			RecordID: survivor.RecordID, SnapshotID: "evs_evdeletesurvivor", Envelope: restoredEnvelope,
			PayloadDigest: snapshot.Hash(),
		}},
		CaptureIntents: []evidence.EvidenceRecoveryCaptureIntent{},
		RevisionReferences: []evidence.EvidenceRecoveryRevisionReference{{
			RecordID: survivor.RecordID, RevisionID: survivor.RevisionID, Ordinal: 0,
			SnapshotID: "evs_evdeletesurvivor", Caption: "explicit comparison copy",
			ReferenceRole: evidence.EvidenceReferenceRoleEvidence,
		}},
		CopyLineage: []evidence.EvidenceRecoveryCopyLineage{{
			SnapshotID: "evs_evdeletesurvivor", CopiedFromSnapshotID: targetCapture.SnapshotID(),
			CopyReason: "explicit comparison copy",
		}},
	}); err != nil {
		t.Fatalf("Replay(explicit surviving copy) error = %v", err)
	}
	intent, preview := targetCapture.Intent(), targetCapture.Preview()
	if err := repository.PersistCaptureIntent(ctx, target.RecordID, "evs_evdeletepending", intent, preview); err != nil {
		t.Fatalf("PersistCaptureIntent() error = %v", err)
	}

	operation := recorddeletion.DeletionOperation{
		OperationID: "rpo_evdeletetask5", ReservationID: "drs_evdeletetask5",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: target.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging,
		FenceEpoch: 1, LedgerSequence: 1, LedgerEntryHash: sha256.Sum256([]byte("task5 deletion ledger")),
	}
	seedAttachmentDeletionOperation(t, ctx, fixture, operation, target.RevisionID)
	adapter, err := evidence.NewDeletionAdapter(repository)
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	receipt, err := adapter.PurgeDeletion(ctx, recorddeletion.PurgeTarget{Operation: operation})
	if err != nil {
		t.Fatalf("PurgeDeletion() error = %v", err)
	}
	if err := adapter.VerifyDeletion(ctx, recorddeletion.PurgeTarget{Operation: operation}, receipt); err != nil {
		t.Fatalf("VerifyDeletion() error = %v", err)
	}
	replayedReceipt, err := adapter.PurgeDeletion(ctx, recorddeletion.PurgeTarget{Operation: operation})
	if err != nil {
		t.Fatalf("PurgeDeletion(idempotent replay) error = %v", err)
	}
	if replayedReceipt != receipt {
		t.Fatalf("PurgeDeletion(idempotent replay) receipt = %#v, want %#v", replayedReceipt, receipt)
	}

	var targetRefs, targetSnapshots, targetIntents, survivorRefs, survivorSnapshots, survivorLineage, payloads int64
	digest := targetCapture.Snapshot().Hash()
	if err := fixture.db.QueryRow(ctx, `
		select
		  (select count(*) from public.record_revision_evidence where record_id = $1),
		  (select count(*) from public.evidence_snapshots where record_id = $1),
		  (select count(*) from public.evidence_capture_intents where record_id = $1),
		  (select count(*) from public.record_revision_evidence where record_id = $2),
		  (select count(*) from public.evidence_snapshots where record_id = $2),
		  (select count(*) from public.evidence_copy_lineage
		    where snapshot_id = 'evs_evdeletesurvivor' and copied_from_snapshot_id = 'evs_evdeletetarget'),
		  (select count(*) from public.evidence_payloads where payload_digest = $3)`,
		target.RecordID, survivor.RecordID, digest[:],
	).Scan(&targetRefs, &targetSnapshots, &targetIntents, &survivorRefs, &survivorSnapshots, &survivorLineage, &payloads); err != nil {
		t.Fatalf("read evidence deletion result: %v", err)
	}
	if targetRefs != 0 || targetSnapshots != 0 || targetIntents != 0 ||
		survivorRefs != 1 || survivorSnapshots != 1 || survivorLineage != 1 || payloads != 1 {
		t.Fatalf("target ref/snapshot/intent survivor ref/snapshot/lineage payload = %d/%d/%d %d/%d/%d %d",
			targetRefs, targetSnapshots, targetIntents, survivorRefs, survivorSnapshots, survivorLineage, payloads)
	}
}

func TestPostgresIntegrationEvidenceTask5RecoveryReplaysCanonicalInventoryThenRunsGlobalGC(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "evidence-task5-recovery", 2)
	recordRepository := newRecordsPostgresRepository(t, runtimePool)
	record, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordCreate, "rec_evrecovery", "", 0, 0,
		recordsPostgresCompleteRevisionInput(t, "Evidence recovery"), "evidence-recovery",
	))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}

	repository := NewPostgresEvidenceRepository(runtimePool, allowRecordPlatformAdmissionGate)
	orphan := storeEvidenceSnapshotFixture(t, "recovery orphan")
	if _, err := repository.PersistPayload(ctx, orphan); err != nil {
		t.Fatalf("PersistPayload(orphan) error = %v", err)
	}
	restored := storeEvidenceSnapshotFixture(t, "recovered canonical payload")
	restoredEnvelope := restored.Envelope()
	intent, preview := storeEvidenceIntentFixture()
	registry, err := evidence.NewRegistry([]evidence.Kind{task5RecoveryEvidenceKind{descriptor: storeEvidenceDescriptor()}})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	adapter, err := evidence.NewRecoveryAdapter(registry, repository)
	if err != nil {
		t.Fatalf("NewRecoveryAdapter() error = %v", err)
	}
	inventory := evidence.EvidenceRecoveryInventory{
		Payloads: []evidence.EvidenceRecoveryPayload{{
			Key: restoredEnvelope.Key, Digest: restored.Hash(), CanonicalPayload: restored.Bytes(),
		}},
		Snapshots: []evidence.EvidenceRecoverySnapshot{{
			RecordID: record.RecordID, SnapshotID: "evs_evrecovery", Envelope: restoredEnvelope,
			PayloadDigest: restored.Hash(),
		}},
		CaptureIntents: []evidence.EvidenceRecoveryCaptureIntent{{
			RecordID: record.RecordID, SnapshotID: "evs_evrecoverypending", Intent: intent, Preview: preview,
		}},
		RevisionReferences: []evidence.EvidenceRecoveryRevisionReference{{
			RecordID: record.RecordID, RevisionID: record.RevisionID, Ordinal: 0, SnapshotID: "evs_evrecovery",
			Caption: "recovered decision", ReferenceRole: evidence.EvidenceReferenceRoleDecisionSupport,
		}},
		CopyLineage: []evidence.EvidenceRecoveryCopyLineage{{
			SnapshotID: "evs_evrecovery", CopiedFromSnapshotID: "evs_evrecoverysource",
			CopyReason: "recovered explicit copy",
		}},
	}
	if err := adapter.Replay(ctx, inventory); err != nil {
		t.Fatalf("Replay() error = %v", err)
	}
	if err := adapter.Replay(ctx, inventory); err != nil {
		t.Fatalf("Replay(idempotent retry) error = %v", err)
	}
	divergent := inventory
	divergent.CopyLineage = append([]evidence.EvidenceRecoveryCopyLineage(nil), inventory.CopyLineage...)
	divergent.CopyLineage[0].CopyReason = "divergent copy reason"
	if err := adapter.Replay(ctx, divergent); !errors.Is(err, evidence.ErrInvalidRecoveryInventory) {
		t.Fatalf("Replay(divergent retry) error = %v, want ErrInvalidRecoveryInventory", err)
	}

	var compressed []byte
	var storedPayloads, orphanPayloads, snapshots, intents, references, gcReceipts int64
	restoredDigest, orphanDigest := restored.Hash(), orphan.Hash()
	if err := fixture.db.QueryRow(ctx, `
		select
		  (select compressed_payload from public.evidence_payloads where payload_digest = $1),
		  (select count(*) from public.evidence_payloads where payload_digest = $1),
		  (select count(*) from public.evidence_payloads where payload_digest = $2),
		  (select count(*) from public.evidence_snapshots
		    where snapshot_id = 'evs_evrecovery' and record_id = $3
		      and capture_authorization_digest = $4),
		  (select count(*) from public.evidence_capture_intents
		    where intent_id = $5 and record_id = $3 and snapshot_id = 'evs_evrecoverypending'),
		  (select count(*) from public.record_revision_evidence
		    where record_id = $3 and revision_id = $6 and ordinal = 0 and snapshot_id = 'evs_evrecovery'
		      and caption = 'recovered decision' and reference_role = 'decision_support'),
		  (select count(*) from public.evidence_payload_gc_receipts)`,
		restoredDigest[:], orphanDigest[:], record.RecordID, restoredEnvelope.Authorization.Digest[:],
		intent.ID, record.RevisionID,
	).Scan(&compressed, &storedPayloads, &orphanPayloads, &snapshots, &intents, &references, &gcReceipts); err != nil {
		t.Fatalf("read evidence recovery result: %v", err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("gzip.NewReader(recovered payload) error = %v", err)
	}
	canonical, err := io.ReadAll(reader)
	if closeErr := reader.Close(); err == nil {
		err = closeErr
	}
	if err != nil || !bytes.Equal(canonical, restored.Bytes()) {
		t.Fatalf("recovered canonical payload mismatch/error = %q/%v", canonical, err)
	}
	if storedPayloads != 1 || orphanPayloads != 0 || snapshots != 1 || intents != 1 || references != 1 || gcReceipts != 1 {
		t.Fatalf("payload/orphan/snapshot/intent/reference/gc = %d/%d/%d/%d/%d/%d, want 1/0/1/1/1/1",
			storedPayloads, orphanPayloads, snapshots, intents, references, gcReceipts)
	}
}

type task5RecoveryEvidenceKind struct {
	descriptor evidence.Descriptor
}

func (kind task5RecoveryEvidenceKind) Descriptor() evidence.Descriptor { return kind.descriptor }
func (task5RecoveryEvidenceKind) ValidateSelection(context.Context, evidence.ActorScope, evidence.Selection) error {
	return nil
}
func (task5RecoveryEvidenceKind) PreviewCapture(context.Context, evidence.ActorScope, evidence.Selection) (evidence.Preview, error) {
	return evidence.Preview{}, nil
}
func (task5RecoveryEvidenceKind) Capture(context.Context, evidence.ActorScope, evidence.Intent) (evidence.CanonicalSnapshot, error) {
	return evidence.CanonicalSnapshot{}, nil
}
func (task5RecoveryEvidenceKind) Authorize(context.Context, evidence.ActorScope, evidence.Selection) (evidence.AuthorizationScope, error) {
	return evidence.AuthorizationScope{}, nil
}
func (task5RecoveryEvidenceKind) Summarize(evidence.CanonicalSnapshot) evidence.Summary {
	return evidence.Summary{}
}
func (task5RecoveryEvidenceKind) Compare(evidence.CanonicalSnapshot, evidence.CanonicalSnapshot, evidence.Alignment) evidence.Comparison {
	return evidence.Comparison{}
}
func (task5RecoveryEvidenceKind) Export(evidence.CanonicalSnapshot, evidence.ExportMode) evidence.ExportMaterial {
	return evidence.ExportMaterial{}
}
