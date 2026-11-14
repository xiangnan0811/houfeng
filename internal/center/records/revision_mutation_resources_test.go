package records

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
)

func TestRevisionMutationResourcesDeterministicIdentityAndBindings(t *testing.T) {
	first := RevisionMutationResources{
		Records:          []RevisionMutationRecord{{ProjectID: "default", RecordID: "rec_z"}, {ProjectID: "default", RecordID: "rec_a"}},
		Sources:          []RevisionMutationSource{{ProjectID: "default", SourceKind: SubjectKindTarget, SourceID: "tg_0123456789abcdef"}},
		RecordGateScopes: []RevisionMutationRecordGateScope{{ProjectID: "default", RecordID: "rec_z"}, {ProjectID: "default", RecordID: "rec_a"}},
		Idempotency: []RevisionMutationIdempotency{
			{Key: recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: recordplatform.OperationKindRecordCreate, Key: "same-client-key"}},
			{Key: recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: recordplatform.OperationKindRecordUpdate, Key: "same-client-key"}},
		},
		Dependencies: []RevisionMutationDependency{
			{ResultSnapshotID: "evs_result", SourceRecordID: "rec_source", SourceRevisionID: "rrv_source"},
			{ResultSnapshotID: "evs_result", SourceRecordID: "rec_source", SourceSnapshotID: "evs_source"},
		},
		NewRevisionEffects: []RevisionMutationNewRevisionEffect{{ProjectID: "default", RecordID: "rec_a", RevisionID: "rrv_new", Kind: RevisionMutationNewEvidenceReference, SnapshotID: "evs_source", Ordinal: 0}},
	}
	second := RevisionMutationResources{
		Records:          []RevisionMutationRecord{{ProjectID: "default", RecordID: "rec_a"}},
		RecordGateScopes: []RevisionMutationRecordGateScope{{ProjectID: "default", RecordID: "rec_a"}},
		Idempotency:      []RevisionMutationIdempotency{{Key: first.Idempotency[0].Key}},
	}
	forward, err := CombineRevisionMutationResources(first, second)
	if err != nil {
		t.Fatal(err)
	}
	reverse, err := CombineRevisionMutationResources(second, first)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(forward, reverse) {
		t.Fatalf("order-dependent declarations: %#v / %#v", forward, reverse)
	}
	if len(forward.Records) != 2 || forward.Records[0].RecordID != "rec_a" || len(forward.Dependencies) != 2 {
		t.Fatalf("identities collapsed incorrectly: %#v", forward)
	}
	if len(forward.RecordGateScopes) != 2 || forward.RecordGateScopes[0].RecordID != "rec_a" || len(forward.Idempotency) != 2 {
		t.Fatalf("gate/key identity scopes collapsed incorrectly: %#v", forward)
	}
	forward.Records[0].RecordID = "rec_changed"
	if first.Records[1].RecordID != "rec_a" {
		t.Fatal("normalization retained caller-owned slice")
	}
}

func TestRevisionMutationResourcesRejectMalformedOrContradictoryIdentity(t *testing.T) {
	validSnapshot := RevisionMutationSnapshot{SnapshotID: "evs_snapshot", RecordID: "rec_owner", PayloadDigest: [32]byte{1}, SourceKind: SubjectKindTarget, SourceID: "tg_0123456789abcdef", AuthorizationDigest: [32]byte{2}}
	wrongOwner := validSnapshot
	wrongOwner.RecordID = "rec_other"
	for _, test := range []struct {
		name      string
		resources RevisionMutationResources
	}{
		{"unknown project", RevisionMutationResources{Records: []RevisionMutationRecord{{ProjectID: "other", RecordID: "rec_a"}}}},
		{"malformed record", RevisionMutationResources{Records: []RevisionMutationRecord{{ProjectID: "default", RecordID: "rec_A"}}}},
		{"malformed gate scope", RevisionMutationResources{RecordGateScopes: []RevisionMutationRecordGateScope{{ProjectID: "default", RecordID: "not_a_record"}}}},
		{"incomplete idempotency key", RevisionMutationResources{Idempotency: []RevisionMutationIdempotency{{Key: recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: recordplatform.OperationKindRecordCreate}}}}},
		{"unknown idempotency operation", RevisionMutationResources{Idempotency: []RevisionMutationIdempotency{{Key: recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: "unknown", Key: "client-key"}}}}},
		{"unknown source", RevisionMutationResources{Sources: []RevisionMutationSource{{ProjectID: "default", SourceKind: "unknown", SourceID: "tg_0123456789abcdef"}}}},
		{"wrong source grammar", RevisionMutationResources{Sources: []RevisionMutationSource{{ProjectID: "default", SourceKind: SubjectKindVPS, SourceID: "tg_0123456789abcdef"}}}},
		{"missing snapshot digest", RevisionMutationResources{Payloads: []RevisionMutationPayload{{}}}},
		{"same snapshot different owner", RevisionMutationResources{Snapshots: []RevisionMutationSnapshot{validSnapshot, wrongOwner}}},
		{"unsupported table", RevisionMutationResources{BlobTables: []RevisionMutationBlobTable{"users"}}},
		{"ambiguous attachment route", RevisionMutationResources{Attachments: []RevisionMutationAttachment{{ProjectID: "default", AttachmentID: "att_a", RecordID: "rec_a", DraftID: "rdr_a", State: "available", LogicalSizeBytes: 1}}}},
		{"missing attachment route", RevisionMutationResources{Attachments: []RevisionMutationAttachment{{ProjectID: "default", AttachmentID: "att_a", State: "available", LogicalSizeBytes: 1}}}},
		{"missing new revision identity", RevisionMutationResources{NewRevisionEffects: []RevisionMutationNewRevisionEffect{{ProjectID: "default", RecordID: "rec_a", Kind: RevisionMutationNewEvidenceReference, SnapshotID: "evs_a"}}}},
		{"new revision scope invented binding", RevisionMutationResources{NewRevisionEffects: []RevisionMutationNewRevisionEffect{{ProjectID: "default", RecordID: "rec_a", RevisionID: "rrv_new", Kind: RevisionMutationNewCollaborationActivity, ActivityID: "rac_activity", EventKind: RevisionMutationEventOwnerChanged, SnapshotID: "evs_snapshot"}}}},
		{"same ordinal different snapshot", RevisionMutationResources{NewRevisionEffects: []RevisionMutationNewRevisionEffect{{ProjectID: "default", RecordID: "rec_a", RevisionID: "rrv_new", Kind: RevisionMutationNewEvidenceReference, SnapshotID: "evs_a"}, {ProjectID: "default", RecordID: "rec_a", RevisionID: "rrv_new", Kind: RevisionMutationNewEvidenceReference, SnapshotID: "evs_b"}}}},
		{"same revision different owner record", RevisionMutationResources{NewRevisionEffects: []RevisionMutationNewRevisionEffect{{ProjectID: "default", RecordID: "rec_a", RevisionID: "rrv_new", Kind: RevisionMutationNewEvidenceReference, SnapshotID: "evs_a"}, {ProjectID: "default", RecordID: "rec_b", RevisionID: "rrv_new", Kind: RevisionMutationNewEvidenceReference, SnapshotID: "evs_a"}}}},
		{"core activity not command derived", RevisionMutationResources{NewRevisionEffects: []RevisionMutationNewRevisionEffect{{ProjectID: "default", RecordID: "rec_a", RevisionID: "rrv_new", Kind: RevisionMutationNewRevisionRows, ActivityID: "rac_other", EventKind: RevisionMutationEventKind(DomainActivityRecordCreated)}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := CombineRevisionMutationResources(test.resources); !errors.Is(err, ErrInvalidRevisionMutationResources) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestRevisionMutationResourcesPreserveOriginalImportObservation(t *testing.T) {
	object := attachments.ObjectVersion{Key: "sha256/0100000000000000000000000000000000000000000000000000000000000000", VersionID: "version-one", SHA256: [32]byte{1}, SizeBytes: 1}
	observation, err := attachments.NewImportBlobGCObservation(object.SHA256, 7)
	if err != nil {
		t.Fatal(err)
	}
	item := attachments.ImportedAvailableAttachment{AttachmentID: "att_import", DisplayName: "a.txt", MediaType: "text/plain", LogicalSizeBytes: 1, Object: object, BackendKind: attachments.BackendKindS3, CreatedBy: "usr_0123456789abcdef01234567", GCObservation: observation}
	resources, err := CombineRevisionMutationResources(RevisionMutationResources{Imports: []attachments.ImportedAvailableAttachment{item, item}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resources.Imports) != 1 || resources.Imports[0].GCObservation != observation {
		t.Fatalf("original observation changed: %#v", resources.Imports)
	}
	changed := item
	changed.GCObservation, err = attachments.NewImportBlobGCObservation(object.SHA256, 8)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CombineRevisionMutationResources(RevisionMutationResources{Imports: []attachments.ImportedAvailableAttachment{item, changed}}); !errors.Is(err, ErrInvalidRevisionMutationResources) {
		t.Fatalf("different observations accepted: %v", err)
	}
}

func TestRevisionMutationWriteObligationObservations(t *testing.T) {
	first := RevisionMutationWriteObligation{
		ProjectID: "default", ObligationID: "rwo_00000000000000000000000000000001",
		OriginKind: "upload", OriginID: "upl_source", OriginGeneration: 1,
		OriginOwnerID: "owner", OriginOwnerGeneration: 2,
		OriginOwnerExpiresAt: time.Date(2026, 1, 1, 4, 0, 0, 0, time.FixedZone("offset", 3600)),
		BackendKind:          attachments.BackendKindS3,
		CanonicalKey:         "sha256/0100000000000000000000000000000000000000000000000000000000000000",
		SHA256:               [32]byte{1}, SizeBytes: 1, Outcome: "registered",
	}
	second := first
	second.ObligationID = "rwo_00000000000000000000000000000002"
	equivalent := first
	equivalent.OriginOwnerExpiresAt = first.OriginOwnerExpiresAt.UTC()
	combined, err := CombineRevisionMutationResources(RevisionMutationResources{WriteObligations: []RevisionMutationWriteObligation{second, first, equivalent}})
	if err != nil {
		t.Fatal(err)
	}
	if len(combined.WriteObligations) != 2 || combined.WriteObligations[0].ObligationID != first.ObligationID || !combined.WriteObligations[0].OriginOwnerExpiresAt.Equal(first.OriginOwnerExpiresAt) {
		t.Fatalf("unexpected obligation observations: %#v", combined.WriteObligations)
	}
	changed := first
	changed.CleanupGeneration++
	if _, err := CombineRevisionMutationResources(RevisionMutationResources{WriteObligations: []RevisionMutationWriteObligation{first, changed}}); !errors.Is(err, ErrInvalidRevisionMutationResources) {
		t.Fatalf("changed cleanup claim was accepted: %v", err)
	}
	changed = first
	changed.TemporaryInode = 9
	if _, err := CombineRevisionMutationResources(RevisionMutationResources{WriteObligations: []RevisionMutationWriteObligation{first, changed}}); !errors.Is(err, ErrInvalidRevisionMutationResources) {
		t.Fatalf("changed temporary target was accepted: %v", err)
	}
}

func TestRevisionParticipantRegistryCollectsOnExactTransaction(t *testing.T) {
	input := mustCompleteRevisionInput(t, validCompleteRevisionValues(t))
	command := RevisionCommitCommand{
		Actor:    recordauth.ActorScope{UserID: input.AuthorID(), Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault},
		RecordID: "rec_discovery", Input: input, ActivityKind: DomainActivityRecordCreated, OutboxTTL: time.Hour,
		Idempotency: revisionCommandIdempotency(mustRevisionCommandFingerprint(t, recordplatform.OperationKindRecordCreate, input.CanonicalHash()), recordplatform.OperationKindRecordCreate),
	}
	ctx := context.Background()
	tx := &revisionParticipantTxStub{}
	var called []string
	participant := func(name, recordID string) *revisionParticipantStub {
		return &revisionParticipantStub{name: name, resources: func(gotCtx context.Context, gotTx pgx.Tx, got RevisionCommitCommand) (RevisionMutationResources, error) {
			if gotCtx != ctx || gotTx != tx || !reflect.DeepEqual(got, command) {
				t.Fatal("discovery changed transaction or command")
			}
			called = append(called, name)
			return RevisionMutationResources{Records: []RevisionMutationRecord{{ProjectID: "default", RecordID: recordID}}}, nil
		}}
	}
	registry, err := NewRevisionParticipantRegistry([]RevisionParticipant{participant("z", "rec_z"), participant("a", "rec_a")})
	if err != nil {
		t.Fatal(err)
	}
	result, err := registry.MutationResources(ctx, tx, command)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(called, []string{"a", "z"}) || len(result.Records) != 2 || result.Records[0].RecordID != "rec_a" {
		t.Fatalf("calls=%v resources=%#v", called, result)
	}
	if _, err := registry.MutationResources(ctx, nil, command); !errors.Is(err, ErrInvalidRevisionCommand) {
		t.Fatalf("nil tx: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := registry.MutationResources(cancelled, tx, command); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}

	sentinel := errors.New("resource read failed")
	failing, err := NewRevisionParticipantRegistry([]RevisionParticipant{&revisionParticipantStub{name: "failing", resources: func(context.Context, pgx.Tx, RevisionCommitCommand) (RevisionMutationResources, error) {
		return RevisionMutationResources{}, sentinel
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := failing.MutationResources(ctx, tx, command); !errors.Is(err, sentinel) {
		t.Fatalf("lost participant error: %v", err)
	}
	invalid, err := NewRevisionParticipantRegistry([]RevisionParticipant{&revisionParticipantStub{name: "invalid", resources: func(context.Context, pgx.Tx, RevisionCommitCommand) (RevisionMutationResources, error) {
		return RevisionMutationResources{Records: []RevisionMutationRecord{{ProjectID: "default", RecordID: "wrong"}}}, nil
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := invalid.MutationResources(ctx, tx, command); !errors.Is(err, ErrInvalidRevisionMutationResources) {
		t.Fatalf("invalid participant declarations: %v", err)
	}
	revisionID, err := command.RevisionID()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, recordID, revisionID string
		invalid                    bool
	}{
		{"exact command", command.RecordID, revisionID, false},
		{"other target record", "rec_other", revisionID, true},
		{"other new revision", command.RecordID, "rrv_other", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			bound, err := NewRevisionParticipantRegistry([]RevisionParticipant{&revisionParticipantStub{name: "bound", resources: func(context.Context, pgx.Tx, RevisionCommitCommand) (RevisionMutationResources, error) {
				return RevisionMutationResources{NewRevisionEffects: []RevisionMutationNewRevisionEffect{{ProjectID: "default", RecordID: test.recordID, RevisionID: test.revisionID, Kind: RevisionMutationNewEvidenceReference, SnapshotID: "evs_bound"}}}, nil
			}}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = bound.MutationResources(ctx, tx, command)
			if test.invalid && !errors.Is(err, ErrInvalidRevisionMutationResources) || !test.invalid && err != nil {
				t.Fatalf("command binding error=%v invalid=%v", err, test.invalid)
			}
		})
	}
}

func TestRevisionMutationNewEffectsKeepExactRevisionAndEventIdentities(t *testing.T) {
	resources, err := CombineRevisionMutationResources(RevisionMutationResources{NewRevisionEffects: []RevisionMutationNewRevisionEffect{
		{ProjectID: "default", RecordID: "rec_a", RevisionID: "rrv_one", Kind: RevisionMutationNewEvidenceReference, SnapshotID: "evs_a"},
		{ProjectID: "default", RecordID: "rec_a", RevisionID: "rrv_two", Kind: RevisionMutationNewEvidenceReference, SnapshotID: "evs_a"},
		{ProjectID: "default", RecordID: "rec_a", RevisionID: "rrv_two", Kind: RevisionMutationNewCollaborationOutbox, EventKind: RevisionMutationEventOwnerChanged},
		{ProjectID: "default", RecordID: "rec_a", RevisionID: "rrv_two", Kind: RevisionMutationNewCollaborationOutbox, EventKind: RevisionMutationEventParticipantChanged},
		{ProjectID: "default", RecordID: "rec_a", RevisionID: "rrv_two", Kind: RevisionMutationNewRevisionRows, ActivityID: "rac_two", EventKind: RevisionMutationEventKind(DomainActivityRecordCreated)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(resources.NewRevisionEffects) != 5 {
		t.Fatalf("revision/event identities collapsed: %#v", resources.NewRevisionEffects)
	}
}
