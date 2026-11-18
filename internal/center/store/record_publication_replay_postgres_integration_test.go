package store

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

func TestPostgresIntegrationCompletedRecordCreateLookup(t *testing.T) {
	ctx := context.Background()
	fixture := NewImportAttachmentGCBackendFixture(t, ctx, attachments.BackendKindLocal, false)
	seedRevisionMutationDiscoveryUsers(t, ctx, fixture)
	repository := newRevisionMutationAdmittedRepository(t, fixture)
	command := revisionBirthBatchCommand(t, ctx, fixture, recordsPostgresCompleteRevisionInput(t, "Completed replay"), "completed-replay-lookup")

	if candidate, found, err := repository.LookupCompletedRecordCreate(ctx, command.Actor, command.Idempotency.Key.Key); err != nil || found || candidate != (records.CompletedCreateCandidate{}) {
		t.Fatalf("missing claim = %#v, %v, %v", candidate, found, err)
	}
	committed, err := repository.CommitRevision(ctx, command)
	if err != nil {
		t.Fatalf("commit original record: %v", err)
	}
	fingerprint, err := command.Idempotency.RequestFingerprint.PersistedBytes()
	if err != nil {
		t.Fatal(err)
	}
	want := records.CompletedCreateCandidate{RecordID: committed.RecordID, RevisionID: committed.RevisionID, RequestFingerprint: fingerprint}
	assertFound := func() {
		t.Helper()
		candidate, found, err := repository.LookupCompletedRecordCreate(ctx, command.Actor, command.Idempotency.Key.Key)
		if err != nil || !found || candidate != want {
			t.Fatalf("completed claim = %#v, %v, %v; want %#v", candidate, found, err, want)
		}
	}
	assertFound()
	// Simulate a retained, historically expired row. Expiry is not authority
	// to allocate or execute the same create again.
	if _, err := fixture.platform.db.Exec(ctx, `
		update public.record_idempotency_keys
		set created_at = clock_timestamp() - interval '3 days', expires_at = clock_timestamp() - interval '1 day'
		where project_id = 'default' and operation_kind = 'record_create' and idempotency_key = $1`, command.Idempotency.Key.Key); err != nil {
		t.Fatal(err)
	}
	assertFound()

	wrongActor := command.Actor.Clone()
	wrongActor.UserID = "usr_bbbbbbbbbbbbbbbbbbbbbbbb"
	if _, found, err := repository.LookupCompletedRecordCreate(ctx, wrongActor, command.Idempotency.Key.Key); found || !errors.Is(err, recordplatform.ErrIdempotencyKeyReused) {
		t.Fatalf("wrong author = found %v, error %v", found, err)
	}
	pending := command.Idempotency
	pending.Key.Key = "completed-replay-pending"
	if _, err := repository.platform.ClaimIdempotency(ctx, pending); err != nil {
		t.Fatalf("create pending claim: %v", err)
	}
	if _, found, err := repository.LookupCompletedRecordCreate(ctx, command.Actor, pending.Key.Key); found || !errors.Is(err, recordplatform.ErrIdempotencyInProgress) {
		t.Fatalf("pending claim = found %v, error %v", found, err)
	}

	if _, err := fixture.platform.db.Exec(ctx, `
		update public.record_idempotency_keys set result_fingerprint = decode(repeat('ff', 32), 'hex')
		where project_id = 'default' and operation_kind = 'record_create' and idempotency_key = $1`, command.Idempotency.Key.Key); err != nil {
		t.Fatal(err)
	}
	if _, found, err := repository.LookupCompletedRecordCreate(ctx, command.Actor, command.Idempotency.Key.Key); found || !errors.Is(err, recordplatform.ErrInvalidIdempotencyRecord) {
		t.Fatalf("inconsistent result = found %v, error %v", found, err)
	}
	if _, err := fixture.platform.db.Exec(ctx, `
		update public.record_idempotency_keys
		set request_fingerprint = decode(repeat('ff', 32), 'hex'), result_fingerprint = decode(repeat('ff', 32), 'hex')
		where project_id = 'default' and operation_kind = 'record_create' and idempotency_key = $1`, command.Idempotency.Key.Key); err != nil {
		t.Fatal(err)
	}
	if _, found, err := repository.LookupCompletedRecordCreate(ctx, command.Actor, command.Idempotency.Key.Key); found || !errors.Is(err, records.ErrRecordRevisionConflict) {
		t.Fatalf("completed claim without first revision = found %v, error %v", found, err)
	}
	if _, err := fixture.platform.db.Exec(ctx, `
		update public.record_idempotency_keys set status = 'conflict'
		where project_id = 'default' and operation_kind = 'record_create' and idempotency_key = $1`, command.Idempotency.Key.Key); err != nil {
		t.Fatal(err)
	}
	if _, found, err := repository.LookupCompletedRecordCreate(ctx, command.Actor, command.Idempotency.Key.Key); found || !errors.Is(err, recordplatform.ErrIdempotencyConflictState) {
		t.Fatalf("conflict claim = found %v, error %v", found, err)
	}

	denied := &PostgresRecordRepository{platform: NewPostgresRecordPlatformRepository(fixture.Runtime, AdmissionGateFunc(func(context.Context, pgx.Tx) error {
		return errors.New("admission denied")
	}))}
	if _, found, err := denied.LookupCompletedRecordCreate(ctx, command.Actor, "unknown-key"); found || !errors.Is(err, ErrRecordPlatformAdmissionUnavailable) {
		t.Fatalf("unadmitted lookup = found %v, error %v", found, err)
	}
}
