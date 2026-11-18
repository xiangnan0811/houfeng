package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

func TestPostgresIntegrationRecordEvidenceRevisionParticipantCapturesAndReusesExistingSnapshot(t *testing.T) {
	ctx := context.Background()
	signed := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	seedRevisionMutationDiscoveryUsers(t, ctx, signed)
	evidenceBirthTestSource(t, ctx, signed.platform.db)
	fixture := signed.platform
	runtimePool := signed.Runtime
	evidenceRepository := NewPostgresEvidenceRepository(runtimePool, signed.Gate, nil)
	recordRepository := newRevisionMutationAdmittedRepository(t, signed, NewRecordEvidenceRevisionParticipant())
	now := recordEvidenceParticipantDatabaseNow(t, ctx, fixture)
	allocation := reserveRevisionBirth(t, ctx, signed, storeEvidenceParticipantActor(t), "record-evidence-first", recordbirth.PurposeEvidence)

	firstCapture := storePreparedEvidenceCapture(
		t, allocation.RecordID, "evs_pgevidencefirst", "evi_111111111111111111111111", now,
	)
	persistRecordEvidenceParticipantPayloadAndIntent(t, ctx, evidenceRepository, firstCapture, allocation.AllocationID)
	firstPreparation := storeEvidenceRevisionPreparation(
		t, firstCapture.RecordID(), []evidence.PreparedCapture{firstCapture}, nil, []string{firstCapture.SnapshotID()},
	)
	firstCommand := recordEvidenceParticipantCommand(
		t, recordplatform.OperationKindRecordCreate, firstCapture.RecordID(), "", 0, 0,
		"First evidence revision", "record-evidence-first", firstPreparation,
	)
	firstCommand = prepareRevisionBirthCommand(t, ctx, signed, allocation, firstCommand)
	first, err := recordRepository.CommitRevision(ctx, firstCommand)
	if err != nil {
		t.Fatalf("CommitRevision(first capture) error = %v", err)
	}
	assertRecordEvidenceParticipantCounts(t, ctx, fixture, first.RecordID, 1, 1, 1, 0)
	assertPersistedEvidenceSnapshotMatchesCapture(t, ctx, fixture, firstCapture)

	secondCapture := storePreparedEvidenceCapture(
		t, first.RecordID, "evs_pgevidencesecond", "evi_222222222222222222222222", now.Add(time.Second),
	)
	persistRecordEvidenceParticipantPayloadAndIntent(t, ctx, evidenceRepository, secondCapture)
	actor := storeEvidenceParticipantActor(t)
	firstEnvelope := firstCapture.Snapshot().Envelope()
	firstReference, err := evidence.PrepareExistingSnapshotReference(
		ctx,
		storeEvidenceReferenceSource{state: evidence.ExistingSnapshotReferenceState{
			RecordID: first.RecordID, SnapshotID: firstCapture.SnapshotID(), Key: firstEnvelope.Key,
			SourceType: firstEnvelope.Source.Type, SourceID: firstEnvelope.Source.ID,
			CaptureAuthorizationDigest: firstEnvelope.Authorization.Digest,
			PayloadDigest:              firstCapture.Snapshot().Hash(),
			Authorization:              firstEnvelope.Authorization,
		}},
		actor,
		first.RecordID,
		firstCapture.SnapshotID(),
	)
	if err != nil {
		t.Fatalf("PrepareExistingSnapshotReference() error = %v", err)
	}
	secondPreparation := storeEvidenceRevisionPreparation(
		t,
		first.RecordID,
		[]evidence.PreparedCapture{secondCapture},
		[]evidence.PreparedReference{firstReference},
		[]string{firstReference.SnapshotID(), secondCapture.SnapshotID()},
	)
	secondCommand := recordEvidenceParticipantCommand(
		t, recordplatform.OperationKindRecordUpdate, first.RecordID, first.RevisionID,
		first.LockVersion, first.AuthorizationEpoch, "Mixed evidence revision", "record-evidence-mixed", secondPreparation,
	)
	second, err := recordRepository.CommitRevision(ctx, secondCommand)
	if err != nil {
		t.Fatalf("CommitRevision(mixed reference) error = %v", err)
	}

	var ordered []string
	if err := fixture.db.QueryRow(ctx, `
		select array_agg(snapshot_id order by ordinal)
		from public.record_revision_evidence
		where record_id = $1 and revision_id = $2`,
		second.RecordID, second.RevisionID,
	).Scan(&ordered); err != nil {
		t.Fatalf("read ordered revision evidence: %v", err)
	}
	if !reflect.DeepEqual(ordered, secondPreparation.SnapshotIDs()) {
		t.Fatalf("ordered revision evidence = %#v, want %#v", ordered, secondPreparation.SnapshotIDs())
	}
	var payloadCount, snapshotCount int
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.evidence_payloads),
		       (select count(*)::int from public.evidence_snapshots where record_id = $1)`,
		second.RecordID,
	).Scan(&payloadCount, &snapshotCount); err != nil {
		t.Fatalf("count reused evidence storage: %v", err)
	}
	if payloadCount != 2 || snapshotCount != 2 {
		t.Fatalf("reused evidence storage = payloads:%d snapshots:%d, want 2/2", payloadCount, snapshotCount)
	}
}

func TestPostgresIntegrationRecordEvidenceRevisionParticipantNormalizesOffsetIntentBeforeConsumption(t *testing.T) {
	ctx := context.Background()
	signed := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	seedRevisionMutationDiscoveryUsers(t, ctx, signed)
	evidenceBirthTestSource(t, ctx, signed.platform.db)
	fixture := signed.platform
	runtimePool := signed.Runtime
	evidenceRepository := NewPostgresEvidenceRepository(runtimePool, signed.Gate, nil)
	recordRepository := newRevisionMutationAdmittedRepository(t, signed, NewRecordEvidenceRevisionParticipant())
	now := recordEvidenceParticipantDatabaseNow(t, ctx, fixture)
	allocation := reserveRevisionBirth(t, ctx, signed, storeEvidenceParticipantActor(t), "record-evidence-offset-intent", recordbirth.PurposeEvidence)
	capture := storePreparedEvidenceCapture(
		t, allocation.RecordID, "evs_pgevidenceoffset", "evi_999999999999999999999999", now,
	)
	intent := capture.Intent()
	preview := capture.Preview()
	offset := time.FixedZone("capture-offset", 5*60*60+30*60)
	intent.Selection.RequestedWindow.Start = intent.Selection.RequestedWindow.Start.In(offset)
	intent.Selection.RequestedWindow.End = intent.Selection.RequestedWindow.End.In(offset)
	intent.ValidUntil = intent.ValidUntil.In(offset)
	preview.Selection = intent.Selection
	preview.RequestedWindow.Start = preview.RequestedWindow.Start.In(offset)
	preview.RequestedWindow.End = preview.RequestedWindow.End.In(offset)
	preview.ActualWindow.Start = preview.ActualWindow.Start.In(offset)
	preview.ActualWindow.End = preview.ActualWindow.End.In(offset)
	preview.ObservedAt = preview.ObservedAt.In(offset)
	preview.PreviewedAt = preview.PreviewedAt.In(offset)
	preview.ValidUntil = preview.ValidUntil.In(offset)
	if err := persistEvidenceCaptureIntentFixture(ctx, evidenceRepository, capture.RecordID(), capture.SnapshotID(), intent, preview, allocation.AllocationID); err != nil {
		t.Fatalf("PersistCaptureIntent(offset values) error = %v", err)
	}
	if _, err := persistEvidencePayloadFixture(ctx, evidenceRepository, capture.Snapshot(), evidenceCaptureFixtureRoute(capture, allocation.AllocationID)); err != nil {
		t.Fatalf("PersistPayload() error = %v", err)
	}
	preparation := storeEvidenceRevisionPreparation(
		t, capture.RecordID(), []evidence.PreparedCapture{capture}, nil, []string{capture.SnapshotID()},
	)
	command := recordEvidenceParticipantCommand(
		t, recordplatform.OperationKindRecordCreate, capture.RecordID(), "", 0, 0,
		"Offset intent evidence revision", "record-evidence-offset-intent", preparation,
	)
	command = prepareRevisionBirthCommand(t, ctx, signed, allocation, command)

	if _, err := recordRepository.CommitRevision(ctx, command); err != nil {
		t.Fatalf("CommitRevision(offset intent) error = %v", err)
	}
	assertRecordEvidenceParticipantCounts(t, ctx, fixture, capture.RecordID(), 1, 1, 1, 0)
}

func TestPostgresIntegrationRecordEvidenceRevisionParticipantRollbackRestoresIntent(t *testing.T) {
	ctx := context.Background()
	signed := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	seedRevisionMutationDiscoveryUsers(t, ctx, signed)
	evidenceBirthTestSource(t, ctx, signed.platform.db)
	fixture := signed.platform
	runtimePool := signed.Runtime
	evidenceRepository := NewPostgresEvidenceRepository(runtimePool, signed.Gate, nil)
	now := recordEvidenceParticipantDatabaseNow(t, ctx, fixture)
	allocation := reserveRevisionBirth(t, ctx, signed, storeEvidenceParticipantActor(t), "record-evidence-rollback", recordbirth.PurposeEvidence)
	capture := storePreparedEvidenceCapture(
		t, allocation.RecordID, "evs_pgevidencerollback", "evi_333333333333333333333333", now,
	)
	persistRecordEvidenceParticipantPayloadAndIntent(t, ctx, evidenceRepository, capture, allocation.AllocationID)
	preparation := storeEvidenceRevisionPreparation(
		t, capture.RecordID(), []evidence.PreparedCapture{capture}, nil, []string{capture.SnapshotID()},
	)
	command := recordEvidenceParticipantCommand(
		t, recordplatform.OperationKindRecordCreate, capture.RecordID(), "", 0, 0,
		"Evidence rollback", "record-evidence-rollback", preparation,
	)
	command = prepareRevisionBirthCommand(t, ctx, signed, allocation, command)
	afterEvidenceErr := errors.New("later participant failed")
	failingParticipant := &storeRevisionParticipantStub{
		name: "zz_after_evidence",
		resources: func(context.Context, pgx.Tx, records.RevisionCommitCommand) (records.RevisionMutationResources, error) {
			return records.RevisionMutationResources{}, nil
		},
		apply: func(context.Context, pgx.Tx, records.RevisionCommitted) error {
			return afterEvidenceErr
		},
	}
	recordRepository := newRevisionMutationAdmittedRepository(t, signed, NewRecordEvidenceRevisionParticipant(), failingParticipant)

	if result, err := recordRepository.CommitRevision(ctx, command); !errors.Is(err, afterEvidenceErr) {
		t.Fatalf("CommitRevision(later failure) = (%#v, %v), want later participant failure", result, err)
	}
	assertRecordEvidenceParticipantCounts(t, ctx, fixture, capture.RecordID(), 0, 0, 0, 1)
	assertEvidencePayloadExists(t, ctx, fixture, capture.Snapshot().Hash(), true)

	retryRepository := newRevisionMutationAdmittedRepository(t, signed, NewRecordEvidenceRevisionParticipant())
	if _, err := retryRepository.CommitRevision(ctx, command); err != nil {
		t.Fatalf("CommitRevision(after rollback restored intent) error = %v", err)
	}
	assertRecordEvidenceParticipantCounts(t, ctx, fixture, capture.RecordID(), 1, 1, 1, 0)
}

func TestPostgresIntegrationRecordEvidenceRevisionParticipantIntentFailuresFailClosed(t *testing.T) {
	ctx := context.Background()
	signed := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	seedRevisionMutationDiscoveryUsers(t, ctx, signed)
	evidenceBirthTestSource(t, ctx, signed.platform.db)
	fixture := signed.platform
	runtimePool := signed.Runtime
	evidenceRepository := NewPostgresEvidenceRepository(runtimePool, signed.Gate, nil)
	recordRepository := newRevisionMutationAdmittedRepository(t, signed, NewRecordEvidenceRevisionParticipant())
	now := recordEvidenceParticipantDatabaseNow(t, ctx, fixture)

	t.Run("missing", func(t *testing.T) {
		allocation := reserveRevisionBirth(t, ctx, signed, storeEvidenceParticipantActor(t), "record-evidence-missing", recordbirth.PurposeEvidence)
		capture := storePreparedEvidenceCapture(
			t, allocation.RecordID, "evs_pgevidencemissing", "evi_444444444444444444444444", now,
		)
		persistRecordEvidenceParticipantPayloadAndIntent(t, ctx, evidenceRepository, capture, allocation.AllocationID)
		if _, err := runtimePool.Exec(ctx, `delete from public.evidence_capture_intents where intent_id=$1`, capture.Intent().ID); err != nil {
			t.Fatalf("remove capture intent: %v", err)
		}
		preparation := storeEvidenceRevisionPreparation(
			t, capture.RecordID(), []evidence.PreparedCapture{capture}, nil, []string{capture.SnapshotID()},
		)
		command := recordEvidenceParticipantCommand(
			t, recordplatform.OperationKindRecordCreate, capture.RecordID(), "", 0, 0,
			"Missing evidence intent", "record-evidence-missing", preparation,
		)
		command = prepareRevisionBirthCommand(t, ctx, signed, allocation, command)
		if result, err := recordRepository.CommitRevision(ctx, command); !errors.Is(err, ErrEvidencePersistenceConflict) {
			t.Fatalf("CommitRevision(missing intent) = (%#v, %v), want conflict", result, err)
		}
		assertRecordEvidenceParticipantCounts(t, ctx, fixture, capture.RecordID(), 0, 0, 0, 0)
		assertEvidencePayloadExists(t, ctx, fixture, capture.Snapshot().Hash(), true)
	})

	t.Run("expired", func(t *testing.T) {
		allocation := reserveRevisionBirth(t, ctx, signed, storeEvidenceParticipantActor(t), "record-evidence-expired", recordbirth.PurposeEvidence)
		capture := storePreparedEvidenceCapture(
			t, allocation.RecordID, "evs_pgevidenceexpired", "evi_555555555555555555555555",
			now,
		)
		persistRecordEvidenceParticipantPayloadAndIntent(t, ctx, evidenceRepository, capture, allocation.AllocationID)
		if _, err := fixture.db.Exec(ctx, `update public.evidence_capture_intents set created_at=transaction_timestamp()-interval '16 minutes', valid_until=transaction_timestamp()-interval '1 minute' where intent_id=$1`, capture.Intent().ID); err != nil {
			t.Fatalf("expire capture intent: %v", err)
		}
		preparation := storeEvidenceRevisionPreparation(
			t, capture.RecordID(), []evidence.PreparedCapture{capture}, nil, []string{capture.SnapshotID()},
		)
		command := recordEvidenceParticipantCommand(
			t, recordplatform.OperationKindRecordCreate, capture.RecordID(), "", 0, 0,
			"Expired evidence intent", "record-evidence-expired", preparation,
		)
		command = prepareRevisionBirthCommand(t, ctx, signed, allocation, command)
		if result, err := recordRepository.CommitRevision(ctx, command); !errors.Is(err, ErrEvidencePersistenceConflict) {
			t.Fatalf("CommitRevision(expired intent) = (%#v, %v), want conflict", result, err)
		}
		assertRecordEvidenceParticipantCounts(t, ctx, fixture, capture.RecordID(), 0, 0, 0, 1)
	})

	t.Run("persisted drift", func(t *testing.T) {
		allocation := reserveRevisionBirth(t, ctx, signed, storeEvidenceParticipantActor(t), "record-evidence-drift", recordbirth.PurposeEvidence)
		capture := storePreparedEvidenceCapture(
			t, allocation.RecordID, "evs_pgevidencedrift", "evi_888888888888888888888888", now,
		)
		persistRecordEvidenceParticipantPayloadAndIntent(t, ctx, evidenceRepository, capture, allocation.AllocationID)
		if _, err := fixture.db.Exec(ctx, `
			update public.evidence_capture_intents
			set source_digest = decode(repeat('ff', 32), 'hex')
			where intent_id = $1`, capture.Intent().ID); err != nil {
			t.Fatalf("drift persisted evidence intent: %v", err)
		}
		preparation := storeEvidenceRevisionPreparation(
			t, capture.RecordID(), []evidence.PreparedCapture{capture}, nil, []string{capture.SnapshotID()},
		)
		command := recordEvidenceParticipantCommand(
			t, recordplatform.OperationKindRecordCreate, capture.RecordID(), "", 0, 0,
			"Drifted evidence intent", "record-evidence-drift", preparation,
		)
		command = prepareRevisionBirthCommand(t, ctx, signed, allocation, command)
		if result, err := recordRepository.CommitRevision(ctx, command); !errors.Is(err, ErrEvidencePersistenceConflict) {
			t.Fatalf("CommitRevision(drifted intent) = (%#v, %v), want conflict", result, err)
		}
		assertRecordEvidenceParticipantCounts(t, ctx, fixture, capture.RecordID(), 0, 0, 0, 1)
		assertEvidencePayloadExists(t, ctx, fixture, capture.Snapshot().Hash(), true)
	})

	t.Run("replayed", func(t *testing.T) {
		allocation := reserveRevisionBirth(t, ctx, signed, storeEvidenceParticipantActor(t), "record-evidence-first-use", recordbirth.PurposeEvidence)
		capture := storePreparedEvidenceCapture(
			t, allocation.RecordID, "evs_pgevidencereplayed", "evi_666666666666666666666666", now,
		)
		persistRecordEvidenceParticipantPayloadAndIntent(t, ctx, evidenceRepository, capture, allocation.AllocationID)
		preparation := storeEvidenceRevisionPreparation(
			t, capture.RecordID(), []evidence.PreparedCapture{capture}, nil, []string{capture.SnapshotID()},
		)
		firstCommand := recordEvidenceParticipantCommand(
			t, recordplatform.OperationKindRecordCreate, capture.RecordID(), "", 0, 0,
			"First intent use", "record-evidence-first-use", preparation,
		)
		firstCommand = prepareRevisionBirthCommand(t, ctx, signed, allocation, firstCommand)
		first, err := recordRepository.CommitRevision(ctx, firstCommand)
		if err != nil {
			t.Fatalf("CommitRevision(first use) error = %v", err)
		}
		replayCommand := recordEvidenceParticipantCommand(
			t, recordplatform.OperationKindRecordUpdate, first.RecordID, first.RevisionID,
			first.LockVersion, first.AuthorizationEpoch, "Replayed intent use", "record-evidence-replay", preparation,
		)
		if result, err := recordRepository.CommitRevision(ctx, replayCommand); !errors.Is(err, ErrEvidencePersistenceConflict) {
			t.Fatalf("CommitRevision(replayed intent) = (%#v, %v), want conflict", result, err)
		}
		assertRecordEvidenceParticipantCounts(t, ctx, fixture, capture.RecordID(), 1, 1, 1, 0)
	})

	t.Run("double consumed in one transaction", func(t *testing.T) {
		allocation := reserveRevisionBirth(t, ctx, signed, storeEvidenceParticipantActor(t), "record-evidence-double", recordbirth.PurposeEvidence)
		capture := storePreparedEvidenceCapture(
			t, allocation.RecordID, "evs_pgevidencedouble", "evi_777777777777777777777777", now,
		)
		persistRecordEvidenceParticipantPayloadAndIntent(t, ctx, evidenceRepository, capture, allocation.AllocationID)
		preparation := storeEvidenceRevisionPreparation(
			t, capture.RecordID(), []evidence.PreparedCapture{capture}, nil, []string{capture.SnapshotID()},
		)
		command := recordEvidenceParticipantCommand(
			t, recordplatform.OperationKindRecordCreate, capture.RecordID(), "", 0, 0,
			"Double intent consume", "record-evidence-double", preparation,
		)
		command = prepareRevisionBirthCommand(t, ctx, signed, allocation, command)
		doubleRepository := newRevisionMutationAdmittedRepository(t, signed, doubleRecordEvidenceParticipant{
			delegate: NewRecordEvidenceRevisionParticipant(),
		})
		if result, err := doubleRepository.CommitRevision(ctx, command); !errors.Is(err, ErrEvidencePersistenceConflict) {
			t.Fatalf("CommitRevision(double consume) = (%#v, %v), want conflict", result, err)
		}
		assertRecordEvidenceParticipantCounts(t, ctx, fixture, capture.RecordID(), 0, 0, 0, 1)
	})
}

type doubleRecordEvidenceParticipant struct {
	delegate records.RevisionParticipant
}

func (doubleRecordEvidenceParticipant) Name() string { return "evidence_double_apply" }

func (participant doubleRecordEvidenceParticipant) MutationResources(ctx context.Context, tx pgx.Tx, command records.RevisionCommitCommand) (records.RevisionMutationResources, error) {
	return participant.delegate.MutationResources(ctx, tx, command)
}

func (participant doubleRecordEvidenceParticipant) ApplyRevision(
	ctx context.Context,
	tx pgx.Tx,
	committed records.RevisionCommitted,
) error {
	if err := participant.delegate.ApplyRevision(ctx, tx, committed); err != nil {
		return err
	}
	return participant.delegate.ApplyRevision(ctx, tx, committed)
}

func recordEvidenceParticipantCommand(
	t *testing.T,
	operation recordplatform.OperationKind,
	recordID string,
	baseRevisionID string,
	lockVersion uint64,
	authorizationEpoch uint64,
	title string,
	idempotencyKey string,
	preparation evidence.RevisionPreparation,
) records.RevisionCommitCommand {
	t.Helper()
	command := recordsPostgresRevisionCommand(
		t,
		operation,
		recordID,
		baseRevisionID,
		lockVersion,
		authorizationEpoch,
		recordsPostgresCompleteRevisionInputWithEvidence(t, title, preparation.SnapshotIDs()),
		idempotencyKey,
	)
	command.EvidencePreparation = preparation
	return command
}

func evidenceCaptureFixtureRoute(capture evidence.PreparedCapture, allocationID string) evidencePayloadCommandRoute {
	actor := recordauth.ActorScope{UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault}
	return evidencePayloadCommandRoute{
		Route: "capture", ActorID: actor.UserID, ActorScopeDigest: fmt.Sprintf("%x", actor.CanonicalHash()),
		AllocationID: allocationID, RecordID: capture.RecordID(), SnapshotID: capture.SnapshotID(), IntentID: capture.Intent().ID,
	}
}

func persistRecordEvidenceParticipantPayloadAndIntent(t *testing.T, ctx context.Context,
	repository *PostgresEvidenceRepository, capture evidence.PreparedCapture, allocationIDs ...string) {
	t.Helper()
	allocationID := ""
	if len(allocationIDs) != 0 {
		allocationID = allocationIDs[0]
	}
	if err := persistEvidenceCaptureIntentFixture(ctx, repository, capture.RecordID(), capture.SnapshotID(), capture.Intent(), capture.Preview(), allocationID); err != nil {
		t.Fatalf("PersistCaptureIntent() error = %v", err)
	}
	if _, err := persistEvidencePayloadFixture(ctx, repository, capture.Snapshot(), evidenceCaptureFixtureRoute(capture, allocationID)); err != nil {
		t.Fatalf("PersistPayload() error = %v", err)
	}
}

func recordEvidenceParticipantDatabaseNow(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
) time.Time {
	t.Helper()
	var now time.Time
	if err := fixture.db.QueryRow(ctx, `select transaction_timestamp()`).Scan(&now); err != nil {
		t.Fatalf("read database time: %v", err)
	}
	return now.UTC()
}

func assertRecordEvidenceParticipantCounts(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	recordID string,
	wantRevisions int,
	wantSnapshots int,
	wantReferences int,
	wantIntents int,
) {
	t.Helper()
	var revisions, snapshots, references, intents int
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.record_revisions where record_id = $1),
		       (select count(*)::int from public.evidence_snapshots where record_id = $1),
		       (select count(*)::int from public.record_revision_evidence where record_id = $1),
		       (select count(*)::int from public.evidence_capture_intents where record_id = $1)`,
		recordID,
	).Scan(&revisions, &snapshots, &references, &intents); err != nil {
		t.Fatalf("count record evidence participant state: %v", err)
	}
	if revisions != wantRevisions || snapshots != wantSnapshots || references != wantReferences || intents != wantIntents {
		t.Fatalf(
			"record evidence state = revisions:%d snapshots:%d references:%d intents:%d, want %d/%d/%d/%d",
			revisions, snapshots, references, intents,
			wantRevisions, wantSnapshots, wantReferences, wantIntents,
		)
	}
}

func assertPersistedEvidenceSnapshotMatchesCapture(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	capture evidence.PreparedCapture,
) {
	t.Helper()
	envelope := capture.Snapshot().Envelope()
	var (
		kind, sourceKind, sourceID, sourceRevision, sourceWatermark string
		producerVersion, calculationVersion, sensitivity            string
		schemaVersion, logicalSize                                  int64
		subjectJSON, sourceJSON, authorizationJSON                  []byte
		authorizationDigest, sourceDigest                           []byte
		actualPrecisionJSON, bucketWidthJSON, unitsJSON             []byte
		qualityJSON, quotaJSON, retentionJSON, redactionJSON        []byte
		canonicalHash, payloadDigest                                []byte
		requestedStart, requestedEnd, actualStart, actualEnd        time.Time
		observedAt, capturedAt, referencedAt                        time.Time
	)
	if err := fixture.db.QueryRow(ctx, `
		select kind, schema_version, source_kind, source_id,
		       subject_identity_snapshot, source_identity_snapshot,
		       capture_authorization, capture_authorization_digest,
		       requested_started_at, requested_ended_at,
		       actual_started_at, actual_ended_at,
		       observed_at, captured_at, referenced_at,
		       source_revision, source_watermark, source_digest,
		       producer_version, calculation_version,
		       actual_precision, bucket_width, unit_semantics, quality,
		       quota_outcome, retention, sensitivity_level, redaction,
		       canonical_hash, logical_size_bytes, payload_digest
		from public.evidence_snapshots
		where record_id = $1 and snapshot_id = $2`, capture.RecordID(), capture.SnapshotID(),
	).Scan(
		&kind, &schemaVersion, &sourceKind, &sourceID,
		&subjectJSON, &sourceJSON, &authorizationJSON, &authorizationDigest,
		&requestedStart, &requestedEnd, &actualStart, &actualEnd,
		&observedAt, &capturedAt, &referencedAt,
		&sourceRevision, &sourceWatermark, &sourceDigest,
		&producerVersion, &calculationVersion,
		&actualPrecisionJSON, &bucketWidthJSON, &unitsJSON, &qualityJSON,
		&quotaJSON, &retentionJSON, &sensitivity, &redactionJSON,
		&canonicalHash, &logicalSize, &payloadDigest,
	); err != nil {
		t.Fatalf("read persisted evidence snapshot: %v", err)
	}
	digest := capture.Snapshot().Hash()
	if kind != string(envelope.Key.Kind) || schemaVersion != int64(envelope.Key.SchemaVersion) ||
		sourceKind != envelope.Source.Type || sourceID != envelope.Source.ID ||
		!equalEvidenceJSON(subjectJSON, envelope.Subject) || !equalEvidenceJSON(sourceJSON, envelope.Source) ||
		!equalEvidenceJSON(authorizationJSON, envelope.Authorization) ||
		!bytes.Equal(authorizationDigest, envelope.Authorization.Digest[:]) ||
		!requestedStart.Equal(envelope.RequestedWindow.Start) || !requestedEnd.Equal(envelope.RequestedWindow.End) ||
		!actualStart.Equal(envelope.ActualWindow.Start) || !actualEnd.Equal(envelope.ActualWindow.End) ||
		!observedAt.Equal(envelope.ObservedAt) || !capturedAt.Equal(envelope.CapturedAt) ||
		!referencedAt.Equal(envelope.ReferencedAt) || sourceRevision != envelope.SourceRevision ||
		sourceWatermark != envelope.SourceWatermark || !bytes.Equal(sourceDigest, envelope.SourceDigest[:]) ||
		producerVersion != envelope.ProducerVersion || calculationVersion != envelope.CalculationVersion ||
		!equalEvidenceJSON(actualPrecisionJSON, envelope.ActualPrecision) ||
		!equalEvidenceJSON(bucketWidthJSON, envelope.BucketWidth) || !equalEvidenceJSON(unitsJSON, envelope.Units) ||
		!equalEvidenceJSON(qualityJSON, envelope.Quality) || !equalEvidenceJSON(quotaJSON, envelope.QuotaOutcome) ||
		!equalEvidenceJSON(retentionJSON, envelope.Retention) || sensitivity != string(envelope.Sensitivity) ||
		!equalEvidenceJSON(redactionJSON, envelope.Redaction) || !bytes.Equal(canonicalHash, digest[:]) ||
		logicalSize != int64(capture.Snapshot().Size()) || !bytes.Equal(payloadDigest, digest[:]) {
		t.Fatal("persisted evidence snapshot does not match prepared capture")
	}
}
