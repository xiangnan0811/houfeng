package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

func TestPostgresIntegrationRecordReadRoundTripsCurrentAndHistoricalRevisions(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-read-round-trip", 2)
	repository := newRecordsPostgresRepository(t, runtimePool)
	recordID := "rec_readroundtrip"

	firstInput := recordsPostgresCompleteRevisionInput(t, "Readable revision one")
	first, err := repository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		recordID,
		"",
		0,
		0,
		firstInput,
		"record-read-create",
	))
	if err != nil {
		t.Fatalf("CommitRevision(first) error = %v", err)
	}

	secondInput := recordsPostgresCompleteRevisionInput(t, "Readable revision two")
	second, err := repository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordUpdate,
		recordID,
		first.RevisionID,
		first.LockVersion,
		first.AuthorizationEpoch,
		secondInput,
		"record-read-update",
	))
	if err != nil {
		t.Fatalf("CommitRevision(second) error = %v", err)
	}

	candidates, err := repository.ListRecordCandidates(ctx, records.RecordCandidatePage{
		Sort:  records.RecordSortUpdatedDesc,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListRecordCandidates() error = %v", err)
	}
	if len(candidates) != 1 || candidates[0].RecordID != recordID || candidates[0].UpdatedAt.IsZero() {
		t.Fatalf("ListRecordCandidates() = %#v", candidates)
	}

	revisions, err := repository.ListRevisionCandidates(ctx, records.RecordRevisionCandidatePage{
		RecordID:           recordID,
		CurrentRevisionID:  second.RevisionID,
		LockVersion:        second.LockVersion,
		AuthorizationEpoch: second.AuthorizationEpoch,
		Limit:              10,
	})
	if err != nil {
		t.Fatalf("ListRevisionCandidates() error = %v", err)
	}
	if len(revisions) != 2 || revisions[0].RevisionID != second.RevisionID || revisions[0].RevisionNo != 2 ||
		revisions[1].RevisionID != first.RevisionID || revisions[1].RevisionNo != 1 {
		t.Fatalf("ListRevisionCandidates() = %#v", revisions)
	}

	tests := []struct {
		name       string
		revisionID string
		wantInput  records.CompleteRevisionInput
		wantNo     uint64
		wantBase   string
	}{
		{name: "current", revisionID: second.RevisionID, wantInput: secondInput, wantNo: 2, wantBase: first.RevisionID},
		{name: "historical", revisionID: first.RevisionID, wantInput: firstInput, wantNo: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored, err := repository.ReadRecordRevision(ctx, records.StoredRecordRevisionRequest{
				RecordID:           recordID,
				RevisionID:         tt.revisionID,
				CurrentRevisionID:  second.RevisionID,
				LockVersion:        second.LockVersion,
				AuthorizationEpoch: second.AuthorizationEpoch,
			})
			if err != nil {
				t.Fatalf("ReadRecordRevision() error = %v", err)
			}
			if stored.RecordID != recordID || stored.RevisionID != tt.revisionID ||
				stored.RevisionNo != tt.wantNo || stored.BaseRevisionID != tt.wantBase ||
				stored.LockVersion != second.LockVersion || stored.AuthorizationEpoch != second.AuthorizationEpoch ||
				stored.Lifecycle != records.LifecycleActive || stored.CreatedAt.IsZero() ||
				stored.RecordCreatedAt.IsZero() || stored.RecordUpdatedAt.IsZero() || stored.ArchivedAt != nil {
				t.Fatalf("ReadRecordRevision() = %#v", stored)
			}
			if !reflect.DeepEqual(stored.Input, tt.wantInput) {
				t.Fatalf("ReadRecordRevision().Input = %#v, want %#v", stored.Input, tt.wantInput)
			}
		})
	}
}

func TestPostgresIntegrationRecordReadRoundTripsOrderedEvidenceSnapshotIDs(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-read-evidence", 2)
	repository := newRecordsPostgresRepository(t, runtimePool, recordReadEvidenceParticipant{})
	recordID := "rec_readevidence"

	first, err := repository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		recordID,
		"",
		0,
		0,
		recordsPostgresCompleteRevisionInput(t, "Evidence revision base"),
		"record-read-evidence-create",
	))
	if err != nil {
		t.Fatalf("CommitRevision(first) error = %v", err)
	}

	var databaseNow time.Time
	if err := fixture.db.QueryRow(ctx, `select transaction_timestamp()`).Scan(&databaseNow); err != nil {
		t.Fatalf("read database time: %v", err)
	}
	snapshot := storeEvidenceSnapshotFixture(t, "record revision evidence read")
	seedEvidencePayloadAt(t, ctx, fixture, snapshot, databaseNow)
	seedEvidenceSnapshotReference(t, ctx, fixture, recordID, snapshot, databaseNow)

	actor := mustStoreRecordActor(t)
	visibility := recordsPostgresCompleteRevisionInput(t, "authorization fixture").VisibilityScope()
	authorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version:      recordauth.SourceAuthorizationVersionV1,
		Kind:         recordauth.SourceKindMonitoringInstance,
		SourceID:     "mi_0123456789abcdef",
		State:        recordauth.SourceStateLive,
		CaptureScope: visibility,
		CurrentScope: &visibility,
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	const snapshotID = "evs_evidencegc"
	var captureAuthorizationDigest [32]byte
	for index := range captureAuthorizationDigest {
		captureAuthorizationDigest[index] = 0x11
	}
	preparedReference, err := evidence.PrepareExistingSnapshotReference(
		ctx,
		recordReadExistingSnapshotSource{state: evidence.ExistingSnapshotReferenceState{
			RecordID:                   recordID,
			SnapshotID:                 snapshotID,
			Key:                        evidence.MonitoringHostV1Key(),
			SourceType:                 string(authorization.Kind),
			SourceID:                   authorization.SourceID,
			CaptureAuthorizationDigest: captureAuthorizationDigest,
			PayloadDigest:              snapshot.Hash(),
			Authorization:              authorization,
		}},
		actor,
		recordID,
		snapshotID,
	)
	if err != nil {
		t.Fatalf("PrepareExistingSnapshotReference() error = %v", err)
	}
	preparation, err := evidence.NewRevisionPreparation(recordID, evidence.RevisionPreparationValues{
		References:         []evidence.PreparedReference{preparedReference},
		OrderedSnapshotIDs: []string{snapshotID},
	})
	if err != nil {
		t.Fatalf("NewRevisionPreparation() error = %v", err)
	}
	secondInput := recordsPostgresCompleteRevisionInputWithEvidence(
		t, "Evidence revision", preparation.SnapshotIDs(),
	)
	command := recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordUpdate,
		recordID,
		first.RevisionID,
		first.LockVersion,
		first.AuthorizationEpoch,
		secondInput,
		"record-read-evidence-update",
	)
	command.EvidencePreparation = preparation
	second, err := repository.CommitRevision(ctx, command)
	if err != nil {
		t.Fatalf("CommitRevision(second) error = %v", err)
	}

	stored, err := repository.ReadRecordRevision(ctx, records.StoredRecordRevisionRequest{
		RecordID:           recordID,
		RevisionID:         second.RevisionID,
		CurrentRevisionID:  second.RevisionID,
		LockVersion:        second.LockVersion,
		AuthorizationEpoch: second.AuthorizationEpoch,
	})
	if err != nil {
		t.Fatalf("ReadRecordRevision() error = %v", err)
	}
	if !reflect.DeepEqual(stored.Input, secondInput) ||
		!reflect.DeepEqual(stored.Input.EvidenceSnapshotIDs(), []string{snapshotID}) {
		t.Fatalf("ReadRecordRevision().Input = %#v, want ordered evidence input %#v", stored.Input, secondInput)
	}
}

type recordReadEvidenceParticipant struct{}

func (recordReadEvidenceParticipant) Name() string { return "evidence" }

func (recordReadEvidenceParticipant) ApplyRevision(
	ctx context.Context,
	tx pgx.Tx,
	committed records.RevisionCommitted,
) error {
	for ordinal, snapshotID := range committed.EvidencePreparation.SnapshotIDs() {
		if _, err := tx.Exec(ctx, `
			insert into public.record_revision_evidence (
				record_id, revision_id, ordinal, snapshot_id
			) values ($1, $2, $3, $4)`,
			committed.Result.RecordID,
			committed.Result.RevisionID,
			int64(ordinal),
			snapshotID,
		); err != nil {
			return fmt.Errorf("insert test revision evidence reference: %w", err)
		}
	}
	return nil
}

type recordReadExistingSnapshotSource struct {
	state evidence.ExistingSnapshotReferenceState
}

func (source recordReadExistingSnapshotSource) ReauthorizeExistingSnapshot(
	context.Context,
	evidence.ActorScope,
	string,
	string,
) (evidence.ExistingSnapshotReferenceState, error) {
	return source.state, nil
}

func TestPostgresIntegrationRecordReadFenceBlocksRevisionAndHistoryContent(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-read-fence", 2)
	repository := newRecordsPostgresRepository(t, runtimePool)
	recordID := "rec_readfenced"

	created, err := repository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		recordID,
		"",
		0,
		0,
		recordsPostgresCompleteRevisionInput(t, "Fenced record"),
		"record-read-fence-create",
	))
	if err != nil {
		t.Fatalf("CommitRevision() error = %v", err)
	}

	if _, err := runtimePool.Exec(ctx, `
		insert into public.deletion_reservations (
			reservation_id, project_id, object_kind, object_id,
			deletion_token_commitment, request_fingerprint,
			actor_scope_digest, preview_binding_digest,
			preview_current_revision_id, preview_lock_version,
			preview_authorization_epoch, preview_content_delivery_epoch,
			preview_dependency_graph_digest, preview_backup_inventory_digest,
			preview_processor_inventory_digest, adapter_readiness_digest,
			adapter_preview_digest, preview_witness_sequence,
			preview_witness_entry_hash, state, expires_at, completed_at
		) values (
			'drs_readfenced', 'default', 'record', $1,
			decode(repeat('31', 32), 'hex'), decode(repeat('32', 32), 'hex'),
			decode(repeat('33', 32), 'hex'), decode(repeat('34', 32), 'hex'),
			$2, $3, $4, 0,
			decode(repeat('35', 32), 'hex'), decode(repeat('36', 32), 'hex'),
			decode(repeat('37', 32), 'hex'), decode(repeat('38', 32), 'hex'),
			decode(repeat('39', 32), 'hex'), 1,
			decode(repeat('3a', 32), 'hex'), 'committed',
			transaction_timestamp() + interval '5 minutes', transaction_timestamp()
		)`, recordID, created.RevisionID, created.LockVersion, created.AuthorizationEpoch); err != nil {
		t.Fatalf("seed committed deletion reservation: %v", err)
	}

	candidates, err := repository.ListRecordCandidates(ctx, records.RecordCandidatePage{
		Sort: records.RecordSortUpdatedDesc, Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListRecordCandidates() error = %v", err)
	}
	for _, candidate := range candidates {
		if candidate.RecordID == recordID {
			t.Fatalf("ListRecordCandidates() returned reserved record %#v", candidate)
		}
	}

	resolver := &fakeCurrentRecordSubjectResolver{}
	authorizations := newPostgresCurrentRecordAuthorizationSource(
		runtimePool,
		resolver,
		allowRecordPlatformAdmissionGate,
	)
	if _, err := authorizations.ResolveCurrentRecordAuthorization(
		ctx,
		mustStoreRecordActor(t),
		recordID,
	); !errors.Is(err, records.ErrRecordDeletionReserved) {
		t.Fatalf("ResolveCurrentRecordAuthorization() error = %v, want ErrRecordDeletionReserved", err)
	}
	if _, err := authorizations.ResolveRecordRevisionAuthorization(
		ctx,
		mustStoreRecordActor(t),
		recordID,
		created.RevisionID,
	); !errors.Is(err, records.ErrRecordDeletionReserved) {
		t.Fatalf("ResolveRecordRevisionAuthorization() error = %v, want ErrRecordDeletionReserved", err)
	}
	if resolver.calls != 0 {
		t.Fatalf("reserved authorization resolver calls = %d, want 0", resolver.calls)
	}

	_, err = repository.ListRevisionCandidates(ctx, records.RecordRevisionCandidatePage{
		RecordID:           recordID,
		CurrentRevisionID:  created.RevisionID,
		LockVersion:        created.LockVersion,
		AuthorizationEpoch: created.AuthorizationEpoch,
		Limit:              10,
	})
	if !errors.Is(err, records.ErrRecordDeletionReserved) {
		t.Fatalf("ListRevisionCandidates() error = %v, want ErrRecordDeletionReserved", err)
	}

	_, err = repository.ReadRecordRevision(ctx, records.StoredRecordRevisionRequest{
		RecordID:           recordID,
		RevisionID:         created.RevisionID,
		CurrentRevisionID:  created.RevisionID,
		LockVersion:        created.LockVersion,
		AuthorizationEpoch: created.AuthorizationEpoch,
	})
	if !errors.Is(err, records.ErrRecordDeletionReserved) {
		t.Fatalf("ReadRecordRevision() error = %v, want ErrRecordDeletionReserved", err)
	}
}
