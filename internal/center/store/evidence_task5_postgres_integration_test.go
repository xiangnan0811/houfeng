package store

import (
	"context"
	"crypto/sha256"
	"testing"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
)

func TestPostgresIntegrationEvidenceTask5DeletionPreservesCopyAndPayload(t *testing.T) {
	ctx := context.Background()
	signed := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	seedRevisionMutationDiscoveryUsers(t, ctx, signed)
	evidenceBirthTestSource(t, ctx, signed.platform.db)
	fixture, runtimePool := signed.platform, signed.Runtime
	recordRepository := newRevisionMutationAdmittedRepository(t, signed, NewRecordEvidenceRevisionParticipant())
	repository := NewPostgresEvidenceRepository(runtimePool, signed.Gate, nil)
	targetAllocation := reserveRevisionBirth(t, ctx, signed, storeEvidenceParticipantActor(t), "evidence-delete-target", recordbirth.PurposeEvidence)
	databaseNow := recordEvidenceParticipantDatabaseNow(t, ctx, fixture)
	targetCapture := storePreparedEvidenceCapture(
		t,
		targetAllocation.RecordID,
		"evs_evdeletetarget",
		"evi_333333333333333333333333",
		databaseNow,
	)
	persistRecordEvidenceParticipantPayloadAndIntent(t, ctx, repository, targetCapture, targetAllocation.AllocationID)
	targetPreparation := storeEvidenceRevisionPreparation(
		t,
		targetCapture.RecordID(),
		[]evidence.PreparedCapture{targetCapture},
		nil,
		[]string{targetCapture.SnapshotID()},
	)
	targetCommand := recordEvidenceParticipantCommand(t, recordplatform.OperationKindRecordCreate, targetCapture.RecordID(), "", 0, 0,
		"Evidence delete target", "evidence-delete-target", targetPreparation)
	targetCommand = prepareRevisionBirthCommand(t, ctx, signed, targetAllocation, targetCommand)
	target, err := recordRepository.CommitRevision(ctx, targetCommand)
	if err != nil {
		t.Fatalf("CommitRevision(target) error = %v", err)
	}
	survivorAllocation := reserveRevisionBirth(t, ctx, signed, storeEvidenceParticipantActor(t), "evidence-delete-survivor", recordbirth.PurposeCreate)
	survivorCommand := recordsPostgresRevisionCommand(t, recordplatform.OperationKindRecordCreate, survivorAllocation.RecordID, "", 0, 0,
		recordsPostgresCompleteRevisionInput(t, "Evidence delete survivor"), "evidence-delete-survivor")
	survivorCommand = prepareRevisionBirthCommand(t, ctx, signed, survivorAllocation, survivorCommand)
	survivor, err := recordRepository.CommitRevision(ctx, survivorCommand)
	if err != nil {
		t.Fatalf("CommitRevision(survivor) error = %v", err)
	}

	// Seed the retained copy as fixture data; this deletion test does not claim
	// that ordinary runtime principals can restore an arbitrary inventory.
	if _, err := fixture.db.Exec(ctx, `
		insert into public.evidence_snapshots
		select (jsonb_populate_record(null::public.evidence_snapshots,
		  to_jsonb(s) || jsonb_build_object('snapshot_id','evs_evdeletesurvivor','record_id',$2::text))).*
		from public.evidence_snapshots s where s.snapshot_id=$1`, targetCapture.SnapshotID(), survivor.RecordID); err != nil {
		t.Fatalf("seed surviving comparison snapshot: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.record_revision_evidence(record_id,revision_id,ordinal,snapshot_id,caption,reference_role)
		values($1,$2,0,'evs_evdeletesurvivor','explicit comparison copy','evidence')`, survivor.RecordID, survivor.RevisionID); err != nil {
		t.Fatalf("seed surviving comparison reference: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `insert into public.evidence_copy_lineage(snapshot_id,copied_from_snapshot_id,copy_reason)
		values('evs_evdeletesurvivor',$1,'explicit comparison copy')`, targetCapture.SnapshotID()); err != nil {
		t.Fatalf("seed surviving comparison lineage: %v", err)
	}
	intent, preview := targetCapture.Intent(), targetCapture.Preview()
	if err := persistEvidenceCaptureIntentFixture(ctx, repository, target.RecordID, "evs_evdeletepending", intent, preview); err != nil {
		t.Fatalf("PersistCaptureIntent() error = %v", err)
	}

	operation := recorddeletion.DeletionOperation{ContractVersion: recorddeletion.DeletionContractV1, OperationID: "rpo_evdeletetask5", ReservationID: "drs_evdeletetask5",
		Object:     recordplatform.ObjectRef{ProjectID: "default", ObjectKind: "record", ObjectID: target.RecordID},
		ReasonCode: recorddeletion.DeletionReasonUserConfirmed, State: recorddeletion.DeletionStateOnlinePurging,
		FenceEpoch: 1, LedgerSequence: 1, LedgerEntryHash: sha256.Sum256([]byte("task5 deletion ledger"))}
	seedAttachmentDeletionOperation(t, ctx, fixture, operation, target.RevisionID)
	if _, err := fixture.db.Exec(ctx, `update public.content_delivery_epochs set delivery_epoch=$2
		where project_id='default' and object_kind='record' and object_id=$1`, target.RecordID, int64(operation.FenceEpoch)); err != nil {
		t.Fatalf("set fixture cleanup epoch: %v", err)
	}
	adapter, err := evidence.NewDeletionAdapter(repository)
	if err != nil {
		t.Fatalf("NewDeletionAdapter() error = %v", err)
	}
	digest := targetCapture.Snapshot().Hash()
	connection, err := fixture.db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var originalCompressed []byte
	if err := connection.QueryRow(ctx, `select compressed_payload from public.evidence_payloads where payload_digest=$1`, digest[:]).Scan(&originalCompressed); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec(ctx, `set session_replication_role=replica`); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec(ctx, `update public.evidence_payloads set compressed_payload=set_byte(compressed_payload,0,0) where payload_digest=$1`, digest[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.PurgeDeletion(ctx, recorddeletion.PurgeTarget{Operation: operation}); err == nil {
		t.Fatal("corrupt evidence produced a purge receipt")
	}
	if _, err := connection.Exec(ctx, `update public.evidence_payloads set compressed_payload=$2 where payload_digest=$1`, digest[:], originalCompressed); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Exec(ctx, `set session_replication_role=origin`); err != nil {
		t.Fatal(err)
	}
	connection.Release()
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
