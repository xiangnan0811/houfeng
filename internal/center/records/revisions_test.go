package records

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordplatform"
)

func TestRevisionCommitCommandAcceptsClosedCreateReviseAndRestoreShapes(t *testing.T) {
	t.Parallel()

	input := mustCompleteRevisionInput(t, validCompleteRevisionValues(t))
	createFingerprint := mustRevisionCommandFingerprint(t, recordplatform.OperationKindRecordCreate, input.CanonicalHash())
	updateFingerprint := mustRevisionCommandFingerprint(t, recordplatform.OperationKindRecordUpdate, input.CanonicalHash())
	tests := []struct {
		name    string
		command RevisionCommitCommand
	}{
		{
			name: "create",
			command: RevisionCommitCommand{
				RecordID:     "rec_create1",
				Input:        input,
				ActivityKind: DomainActivityRecordCreated,
				OutboxTTL:    time.Hour,
				Idempotency:  revisionCommandIdempotency(createFingerprint, recordplatform.OperationKindRecordCreate),
			},
		},
		{
			name: "revise",
			command: RevisionCommitCommand{
				RecordID:           "rec_revise1",
				BaseRevisionID:     "rrv_aaaaaaaaaaaaaaaa",
				LockVersion:        7,
				AuthorizationEpoch: 5,
				Input:              input,
				ActivityKind:       DomainActivityRecordRevised,
				OutboxTTL:          time.Hour,
				Idempotency:        revisionCommandIdempotency(updateFingerprint, recordplatform.OperationKindRecordUpdate),
			},
		},
		{
			name: "restore",
			command: RevisionCommitCommand{
				RecordID:           "rec_restore1",
				BaseRevisionID:     "rrv_bbbbbbbbbbbbbbbb",
				LockVersion:        8,
				AuthorizationEpoch: 6,
				Input:              input,
				ActivityKind:       DomainActivityRecordRestored,
				OutboxTTL:          time.Hour,
				Idempotency:        revisionCommandIdempotency(updateFingerprint, recordplatform.OperationKindRecordUpdate),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.command.Validate(); err != nil {
				t.Fatalf("RevisionCommitCommand.Validate() error = %v", err)
			}
			revisionID, err := test.command.RevisionID()
			if err != nil {
				t.Fatalf("RevisionID() error = %v", err)
			}
			again, err := test.command.RevisionID()
			if err != nil || revisionID != again || len(revisionID) != len("rrv_")+64 {
				t.Fatalf("RevisionID() = (%q, %q, %v), want stable 64-hex identity", revisionID, again, err)
			}
			activityID, err := test.command.ActivityID()
			if err != nil || len(activityID) != len("rac_")+64 || activityID[len("rac_"):] != revisionID[len("rrv_"):] {
				t.Fatalf("ActivityID() = %q, want domain-separated prefix over same request identity", activityID)
			}
		})
	}
}

func TestRevisionCommitCommandClosesOptionalPublishedDraftShape(t *testing.T) {
	t.Parallel()

	input := mustCompleteRevisionInput(t, validCompleteRevisionValues(t))
	payload, err := NewDraftPayload([]byte(`{"title":"Publish"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload() error = %v", err)
	}
	etag, err := NewDraftETag("rdf_0123456789abcdef", input.AuthorID(), 4, payload)
	if err != nil {
		t.Fatalf("NewDraftETag() error = %v", err)
	}
	base := RevisionCommitCommand{
		RecordID:     "rec_create1",
		DraftID:      "rdf_0123456789abcdef",
		DraftETag:    etag,
		Input:        input,
		ActivityKind: DomainActivityRecordCreated,
		OutboxTTL:    time.Hour,
		Idempotency: revisionCommandIdempotency(
			mustRevisionCommandFingerprint(t, recordplatform.OperationKindRecordCreate, input.CanonicalHash()),
			recordplatform.OperationKindRecordCreate,
		),
	}
	if err := base.Validate(); err != nil {
		t.Fatalf("RevisionCommitCommand.Validate() error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*RevisionCommitCommand)
	}{
		{name: "draft id without etag", mutate: func(command *RevisionCommitCommand) { command.DraftETag = DraftETag{} }},
		{name: "etag without draft id", mutate: func(command *RevisionCommitCommand) { command.DraftID = "" }},
		{name: "invalid draft id", mutate: func(command *RevisionCommitCommand) { command.DraftID = "draft-1" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			command := base
			tt.mutate(&command)
			if err := command.Validate(); !errors.Is(err, ErrInvalidRevisionCommand) {
				t.Fatalf("RevisionCommitCommand.Validate() error = %v, want ErrInvalidRevisionCommand", err)
			}
		})
	}
}

func TestRevisionCommitCommandRejectsMixedCreateAndUpdateShapes(t *testing.T) {
	t.Parallel()

	input := mustCompleteRevisionInput(t, validCompleteRevisionValues(t))
	createFingerprint := mustRevisionCommandFingerprint(t, recordplatform.OperationKindRecordCreate, input.CanonicalHash())
	updateFingerprint := mustRevisionCommandFingerprint(t, recordplatform.OperationKindRecordUpdate, input.CanonicalHash())
	validCreate := RevisionCommitCommand{
		RecordID:     "rec_shape1",
		Input:        input,
		ActivityKind: DomainActivityRecordCreated,
		OutboxTTL:    time.Hour,
		Idempotency:  revisionCommandIdempotency(createFingerprint, recordplatform.OperationKindRecordCreate),
	}
	validUpdate := RevisionCommitCommand{
		RecordID:           "rec_shape1",
		BaseRevisionID:     "rrv_aaaaaaaaaaaaaaaa",
		LockVersion:        2,
		AuthorizationEpoch: 2,
		Input:              input,
		ActivityKind:       DomainActivityRecordRevised,
		OutboxTTL:          time.Hour,
		Idempotency:        revisionCommandIdempotency(updateFingerprint, recordplatform.OperationKindRecordUpdate),
	}
	tests := []struct {
		name   string
		mutate func(*RevisionCommitCommand)
		base   RevisionCommitCommand
	}{
		{name: "create with base", base: validCreate, mutate: func(command *RevisionCommitCommand) { command.BaseRevisionID = "rrv_aaaaaaaaaaaaaaaa" }},
		{name: "create with lock", base: validCreate, mutate: func(command *RevisionCommitCommand) { command.LockVersion = 1 }},
		{name: "create with update operation", base: validCreate, mutate: func(command *RevisionCommitCommand) { command.Idempotency = validUpdate.Idempotency }},
		{name: "update without base", base: validUpdate, mutate: func(command *RevisionCommitCommand) { command.BaseRevisionID = "" }},
		{name: "update without lock", base: validUpdate, mutate: func(command *RevisionCommitCommand) { command.LockVersion = 0 }},
		{name: "update without authorization epoch", base: validUpdate, mutate: func(command *RevisionCommitCommand) { command.AuthorizationEpoch = 0 }},
		{name: "update with create event", base: validUpdate, mutate: func(command *RevisionCommitCommand) { command.ActivityKind = DomainActivityRecordCreated }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			command := test.base
			test.mutate(&command)
			if err := command.Validate(); !errors.Is(err, ErrInvalidRevisionCommand) {
				t.Fatalf("RevisionCommitCommand.Validate() error = %v, want ErrInvalidRevisionCommand", err)
			}
		})
	}
}

func TestRecordLifecycleCommandAcceptsArchiveAndUnarchiveTransitions(t *testing.T) {
	t.Parallel()

	fingerprint := mustRevisionCommandFingerprint(t, recordplatform.OperationKindRecordUpdate, [32]byte{0x51})
	tests := []struct {
		name         string
		target       Lifecycle
		wantActivity DomainActivityKind
	}{
		{name: "archive", target: LifecycleArchived, wantActivity: DomainActivityRecordArchived},
		{name: "unarchive", target: LifecycleActive, wantActivity: DomainActivityRecordUnarchived},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			command := RecordLifecycleCommand{
				RecordID:           "rec_lifecycle1",
				CurrentRevisionID:  "rrv_current00000001",
				LockVersion:        7,
				AuthorizationEpoch: 5,
				TargetLifecycle:    test.target,
				ActorID:            "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
				OutboxTTL:          time.Hour,
				Idempotency:        revisionCommandIdempotency(fingerprint, recordplatform.OperationKindRecordUpdate),
			}
			if err := command.Validate(); err != nil {
				t.Fatalf("RecordLifecycleCommand.Validate() error = %v", err)
			}
			activityKind, err := command.ActivityKind()
			if err != nil || activityKind != test.wantActivity {
				t.Fatalf("ActivityKind() = (%q, %v), want (%q, nil)", activityKind, err, test.wantActivity)
			}
			activityID, err := command.ActivityID()
			if err != nil || len(activityID) != len("rac_")+64 {
				t.Fatalf("ActivityID() = (%q, %v), want deterministic activity identity", activityID, err)
			}
		})
	}
}

func TestRecordLifecycleCommandRejectsInvalidOrNonCASShapes(t *testing.T) {
	t.Parallel()

	fingerprint := mustRevisionCommandFingerprint(t, recordplatform.OperationKindRecordUpdate, [32]byte{0x52})
	valid := RecordLifecycleCommand{
		RecordID:           "rec_lifecycle1",
		CurrentRevisionID:  "rrv_current00000001",
		LockVersion:        7,
		AuthorizationEpoch: 5,
		TargetLifecycle:    LifecycleArchived,
		ActorID:            "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
		OutboxTTL:          time.Hour,
		Idempotency:        revisionCommandIdempotency(fingerprint, recordplatform.OperationKindRecordUpdate),
	}
	tests := []struct {
		name   string
		mutate func(*RecordLifecycleCommand)
	}{
		{name: "invalid record", mutate: func(command *RecordLifecycleCommand) { command.RecordID = "record-1" }},
		{name: "missing current revision", mutate: func(command *RecordLifecycleCommand) { command.CurrentRevisionID = "" }},
		{name: "zero lock", mutate: func(command *RecordLifecycleCommand) { command.LockVersion = 0 }},
		{name: "zero authorization epoch", mutate: func(command *RecordLifecycleCommand) { command.AuthorizationEpoch = 0 }},
		{name: "unknown lifecycle", mutate: func(command *RecordLifecycleCommand) { command.TargetLifecycle = "deleted" }},
		{name: "invalid actor", mutate: func(command *RecordLifecycleCommand) { command.ActorID = "usr_invalid" }},
		{name: "create operation", mutate: func(command *RecordLifecycleCommand) {
			command.Idempotency = revisionCommandIdempotency(
				mustRevisionCommandFingerprint(t, recordplatform.OperationKindRecordCreate, [32]byte{0x52}),
				recordplatform.OperationKindRecordCreate,
			)
		}},
		{name: "zero outbox ttl", mutate: func(command *RecordLifecycleCommand) { command.OutboxTTL = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			command := valid
			test.mutate(&command)
			if err := command.Validate(); !errors.Is(err, ErrInvalidRecordLifecycleCommand) {
				t.Fatalf("RecordLifecycleCommand.Validate() error = %v, want ErrInvalidRecordLifecycleCommand", err)
			}
		})
	}
}

func mustRevisionCommandFingerprint(t *testing.T, operation recordplatform.OperationKind, payload [32]byte) recordplatform.RequestFingerprintV1 {
	t.Helper()
	fingerprint, err := recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{
		Version:            recordplatform.RequestFingerprintVersionV1,
		OperationKind:      operation,
		ProjectID:          recordplatform.ProjectIDDefault,
		ActorScopeDigest:   [32]byte{1},
		RequestScopeDigest: [32]byte{2},
		PayloadDigest:      payload,
	})
	if err != nil {
		t.Fatalf("FingerprintRequestV1() error = %v", err)
	}
	return fingerprint
}

func revisionCommandIdempotency(fingerprint recordplatform.RequestFingerprintV1, operation recordplatform.OperationKind) recordplatform.IdempotencyClaimInputV1 {
	return recordplatform.IdempotencyClaimInputV1{
		Key: recordplatform.IdempotencyKey{
			ProjectID:     recordplatform.ProjectIDDefault,
			OperationKind: operation,
			Key:           "revision-command-1",
		},
		RequestFingerprint: fingerprint,
		OwnerID:            "records_api_01",
		OwnerLeaseDuration: time.Minute,
		RecordTTL:          time.Hour,
	}
}

func TestRevisionParticipantRegistryAppliesInDeterministicNameOrder(t *testing.T) {
	t.Parallel()

	var applied []string
	var attachmentDraftID string
	registry, err := NewRevisionParticipantRegistry([]RevisionParticipant{
		&revisionParticipantStub{name: "search", apply: func(context.Context, pgx.Tx, RevisionCommitted) error {
			applied = append(applied, "search")
			return nil
		}},
		&revisionParticipantStub{name: "attachments", apply: func(_ context.Context, _ pgx.Tx, committed RevisionCommitted) error {
			applied = append(applied, "attachments")
			attachmentDraftID = committed.DraftID
			return nil
		}},
		&revisionParticipantStub{name: "activity_projection", apply: func(context.Context, pgx.Tx, RevisionCommitted) error {
			applied = append(applied, "activity_projection")
			return nil
		}},
	})
	if err != nil {
		t.Fatalf("NewRevisionParticipantRegistry() error = %v", err)
	}

	if got := registry.Names(); !reflect.DeepEqual(got, []string{"activity_projection", "attachments", "search"}) {
		t.Fatalf("Names() = %#v, want deterministic sorted names", got)
	}
	if err := registry.ApplyRevision(context.Background(), revisionParticipantTxStub{}, RevisionCommitted{DraftID: "rdf_participant"}); err != nil {
		t.Fatalf("ApplyRevision() error = %v", err)
	}
	if !reflect.DeepEqual(applied, []string{"activity_projection", "attachments", "search"}) {
		t.Fatalf("participant order = %#v, want sorted order", applied)
	}
	if attachmentDraftID != "rdf_participant" {
		t.Fatalf("participant DraftID = %q, want published draft identity", attachmentDraftID)
	}

	names := registry.Names()
	names[0] = "mutated"
	if got := registry.Names()[0]; got != "activity_projection" {
		t.Fatalf("Names() changed through returned slice mutation: %q", got)
	}
}

func TestRevisionRequestFingerprintChangesWithOrderedAttachmentIDs(t *testing.T) {
	t.Parallel()

	firstValues := validCompleteRevisionValues(t)
	secondValues := validCompleteRevisionValues(t)
	secondValues.AttachmentIDs[0], secondValues.AttachmentIDs[1] = secondValues.AttachmentIDs[1], secondValues.AttachmentIDs[0]
	firstInput := mustCompleteRevisionInput(t, firstValues)
	secondInput := mustCompleteRevisionInput(t, secondValues)

	first := mustRevisionCommandFingerprint(t, recordplatform.OperationKindRecordUpdate, firstInput.CanonicalHash())
	second := mustRevisionCommandFingerprint(t, recordplatform.OperationKindRecordUpdate, secondInput.CanonicalHash())
	firstBytes, err := first.PersistedBytes()
	if err != nil {
		t.Fatalf("first fingerprint PersistedBytes() error = %v", err)
	}
	secondBytes, err := second.PersistedBytes()
	if err != nil {
		t.Fatalf("second fingerprint PersistedBytes() error = %v", err)
	}
	if reflect.DeepEqual(firstBytes, secondBytes) {
		t.Fatalf("ordered attachment changes produced identical request fingerprints: %x", firstBytes)
	}
}

func TestRevisionParticipantRegistryRejectsInvalidDuplicateAndTypedNilParticipants(t *testing.T) {
	t.Parallel()

	var typedNil *revisionParticipantStub
	tests := []struct {
		name         string
		participants []RevisionParticipant
	}{
		{name: "empty name", participants: []RevisionParticipant{&revisionParticipantStub{}}},
		{name: "invalid name", participants: []RevisionParticipant{&revisionParticipantStub{name: "Search Worker"}}},
		{name: "duplicate name", participants: []RevisionParticipant{
			&revisionParticipantStub{name: "search"},
			&revisionParticipantStub{name: "search"},
		}},
		{name: "typed nil", participants: []RevisionParticipant{typedNil}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewRevisionParticipantRegistry(test.participants); !errors.Is(err, ErrInvalidRevisionParticipant) {
				t.Fatalf("NewRevisionParticipantRegistry() error = %v, want ErrInvalidRevisionParticipant", err)
			}
		})
	}
}

func TestRevisionParticipantRegistryStopsAtFirstFailure(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("participant failed")
	var applied []string
	registry, err := NewRevisionParticipantRegistry([]RevisionParticipant{
		&revisionParticipantStub{name: "first", apply: func(context.Context, pgx.Tx, RevisionCommitted) error {
			applied = append(applied, "first")
			return nil
		}},
		&revisionParticipantStub{name: "second", apply: func(context.Context, pgx.Tx, RevisionCommitted) error {
			applied = append(applied, "second")
			return wantErr
		}},
		&revisionParticipantStub{name: "third", apply: func(context.Context, pgx.Tx, RevisionCommitted) error {
			applied = append(applied, "third")
			return nil
		}},
	})
	if err != nil {
		t.Fatalf("NewRevisionParticipantRegistry() error = %v", err)
	}

	err = registry.ApplyRevision(context.Background(), revisionParticipantTxStub{}, RevisionCommitted{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("ApplyRevision() error = %v, want wrapped participant failure", err)
	}
	if !reflect.DeepEqual(applied, []string{"first", "second"}) {
		t.Fatalf("participant calls = %#v, want stop after first failure", applied)
	}
}

type revisionParticipantStub struct {
	name  string
	apply func(context.Context, pgx.Tx, RevisionCommitted) error
}

func (participant *revisionParticipantStub) Name() string {
	if participant == nil {
		return ""
	}
	return participant.name
}

func (participant *revisionParticipantStub) ApplyRevision(ctx context.Context, tx pgx.Tx, committed RevisionCommitted) error {
	if participant.apply == nil {
		return nil
	}
	return participant.apply(ctx, tx, committed)
}

type revisionParticipantTxStub struct {
	pgx.Tx
}
