package store

import (
	"context"
	"errors"
	"reflect"
	"testing"

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
			deletion_token_commitment, request_fingerprint, state, expires_at, completed_at
		) values (
			'drs_readfenced', 'default', 'record', $1,
			decode(repeat('31', 32), 'hex'), decode(repeat('32', 32), 'hex'), 'committed',
			transaction_timestamp() + interval '5 minutes', transaction_timestamp()
		)`, recordID); err != nil {
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
