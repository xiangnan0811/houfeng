package evidence

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"strings"
	"testing"

	"houfeng/internal/center/recordauth"
)

const (
	testPreparedReferenceRecordID  = "rec_reference1"
	testPreparedReferenceSnapshot1 = "evs_0123456789abcdef"
	testPreparedReferenceSnapshot2 = "evs_fedcba9876543210"
)

func TestPrepareExistingSnapshotReferenceReauthorizesWithoutRecaptureOrPayloadCopy(t *testing.T) {
	actor := testPreparedReferenceActor(t)
	state := testExistingSnapshotReferenceState(t, testPreparedReferenceSnapshot1)
	source := &existingSnapshotReferenceSourceStub{state: state}

	prepared, err := PrepareExistingSnapshotReference(
		context.Background(), source, actor, state.RecordID, state.SnapshotID,
	)
	if err != nil {
		t.Fatalf("PrepareExistingSnapshotReference() error = %v", err)
	}
	if err := prepared.Validate(); err != nil {
		t.Fatalf("PreparedReference.Validate() error = %v", err)
	}
	if source.reauthorizeCalls != 1 || source.recaptureCalls != 0 || source.payloadCopyCalls != 0 {
		t.Fatalf("source calls = reauthorize:%d recapture:%d payload_copy:%d, want 1/0/0",
			source.reauthorizeCalls, source.recaptureCalls, source.payloadCopyCalls)
	}
	if source.recordID != state.RecordID || source.snapshotID != state.SnapshotID ||
		source.actor.CanonicalHash() != actor.CanonicalHash() {
		t.Fatalf("reauthorization input = (%q, %q, %x), want (%q, %q, %x)",
			source.recordID, source.snapshotID, source.actor.CanonicalHash(),
			state.RecordID, state.SnapshotID, actor.CanonicalHash())
	}
	if prepared.RecordID() != state.RecordID || prepared.SnapshotID() != state.SnapshotID ||
		prepared.Key() != state.Key || prepared.SourceType() != state.SourceType ||
		prepared.SourceID() != state.SourceID || prepared.PayloadDigest() != state.PayloadDigest ||
		prepared.CaptureAuthorizationDigest() != state.CaptureAuthorizationDigest ||
		prepared.ActorScopeDigest() != actor.CanonicalHash() {
		t.Fatal("prepared reference identity did not preserve the reauthorized snapshot")
	}

	wantAuthorization := prepared.Authorization()
	state.SourceType = "changed"
	state.SourceID = "tg_changed"
	state.Authorization.CurrentScope.PolicyRevision++
	source.state.Authorization.CurrentScope.PolicyRevision++
	source.state.PayloadDigest[0] ^= 0xff
	returnedAuthorization := prepared.Authorization()
	returnedAuthorization.CurrentScope.PolicyRevision++

	if prepared.SourceType() != string(wantAuthorization.Kind) || prepared.SourceID() != wantAuthorization.SourceID ||
		!reflect.DeepEqual(prepared.Authorization(), wantAuthorization) ||
		prepared.PayloadDigest() != testExistingSnapshotReferenceState(t, testPreparedReferenceSnapshot1).PayloadDigest {
		t.Fatal("prepared reference changed through constructor or accessor mutation")
	}
}

func TestPrepareExistingSnapshotReferenceRejectsInvalidOrDriftingIdentity(t *testing.T) {
	actor := testPreparedReferenceActor(t)
	tests := []struct {
		name       string
		recordID   string
		snapshotID string
		mutate     func(*ExistingSnapshotReferenceState)
	}{
		{name: "empty requested record", recordID: "", snapshotID: testPreparedReferenceSnapshot1},
		{name: "invalid requested record", recordID: "rec_INVALID", snapshotID: testPreparedReferenceSnapshot1},
		{name: "empty requested snapshot", recordID: testPreparedReferenceRecordID, snapshotID: ""},
		{name: "snapshot prefix only", recordID: testPreparedReferenceRecordID, snapshotID: "evs_"},
		{name: "snapshot uppercase", recordID: testPreparedReferenceRecordID, snapshotID: "evs_INVALID"},
		{name: "snapshot punctuation", recordID: testPreparedReferenceRecordID, snapshotID: "evs_invalid-id"},
		{name: "snapshot too long", recordID: testPreparedReferenceRecordID, snapshotID: "evs_" + strings.Repeat("a", 65)},
		{name: "returned record drift", recordID: testPreparedReferenceRecordID, snapshotID: testPreparedReferenceSnapshot1, mutate: func(state *ExistingSnapshotReferenceState) {
			state.RecordID = "rec_reference2"
		}},
		{name: "returned snapshot drift", recordID: testPreparedReferenceRecordID, snapshotID: testPreparedReferenceSnapshot1, mutate: func(state *ExistingSnapshotReferenceState) {
			state.SnapshotID = testPreparedReferenceSnapshot2
		}},
		{name: "unknown key", recordID: testPreparedReferenceRecordID, snapshotID: testPreparedReferenceSnapshot1, mutate: func(state *ExistingSnapshotReferenceState) {
			state.Key = KindKey{Kind: "unknown", SchemaVersion: 1}
		}},
		{name: "source identity drift", recordID: testPreparedReferenceRecordID, snapshotID: testPreparedReferenceSnapshot1, mutate: func(state *ExistingSnapshotReferenceState) {
			state.SourceID = "tg_1111111111111111"
		}},
		{name: "authorization digest drift", recordID: testPreparedReferenceRecordID, snapshotID: testPreparedReferenceSnapshot1, mutate: func(state *ExistingSnapshotReferenceState) {
			state.Authorization.Digest[0] ^= 0xff
		}},
		{name: "zero capture authorization digest", recordID: testPreparedReferenceRecordID, snapshotID: testPreparedReferenceSnapshot1, mutate: func(state *ExistingSnapshotReferenceState) {
			state.CaptureAuthorizationDigest = [sha256.Size]byte{}
		}},
		{name: "zero payload digest", recordID: testPreparedReferenceRecordID, snapshotID: testPreparedReferenceSnapshot1, mutate: func(state *ExistingSnapshotReferenceState) {
			state.PayloadDigest = [sha256.Size]byte{}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := testExistingSnapshotReferenceState(t, testPreparedReferenceSnapshot1)
			if tt.mutate != nil {
				tt.mutate(&state)
			}
			source := &existingSnapshotReferenceSourceStub{state: state}
			if _, err := PrepareExistingSnapshotReference(
				context.Background(), source, actor, tt.recordID, tt.snapshotID,
			); !errors.Is(err, ErrInvalidPreparedReference) {
				t.Fatalf("PrepareExistingSnapshotReference() error = %v, want ErrInvalidPreparedReference", err)
			}
		})
	}
}

func TestRevisionPreparationIsImmutableAndPreservesOrderedMixedReferences(t *testing.T) {
	actor := testPreparedReferenceActor(t)
	first := mustPreparedExistingReference(t, actor, testPreparedReferenceSnapshot1)
	second := mustPreparedExistingReference(t, actor, testPreparedReferenceSnapshot2)
	captureInputs := newPreparedCaptureTestInputs(t)
	captureInputs.recordID = testPreparedReferenceRecordID
	captureInputs.snapshotID = "evs_capture1"
	capture, err := prepareCaptureFromTestInputs(captureInputs)
	if err != nil {
		t.Fatalf("PrepareCapture() error = %v", err)
	}
	ordered := []string{second.SnapshotID(), capture.SnapshotID(), first.SnapshotID()}
	values := RevisionPreparationValues{
		Captures:           []PreparedCapture{capture},
		References:         []PreparedReference{first, second},
		OrderedSnapshotIDs: ordered,
	}

	prepared, err := NewRevisionPreparation(testPreparedReferenceRecordID, values)
	if err != nil {
		t.Fatalf("NewRevisionPreparation() error = %v", err)
	}
	if err := prepared.ValidateForRecord(testPreparedReferenceRecordID); err != nil {
		t.Fatalf("RevisionPreparation.ValidateForRecord() error = %v", err)
	}
	if err := prepared.ValidateReferencesForActor(actor); err != nil {
		t.Fatalf("RevisionPreparation.ValidateReferencesForActor() error = %v", err)
	}

	ordered[0] = testPreparedReferenceSnapshot1
	values.OrderedSnapshotIDs[2] = testPreparedReferenceSnapshot2
	values.Captures[0] = PreparedCapture{}
	values.References[0] = PreparedReference{}
	returnedIDs := prepared.SnapshotIDs()
	returnedIDs[0] = testPreparedReferenceSnapshot1
	returnedReferences := prepared.References()
	returnedReferences[0] = PreparedReference{}

	returnedCaptures := prepared.Captures()
	returnedCaptures[0] = PreparedCapture{}

	want := []string{testPreparedReferenceSnapshot2, capture.SnapshotID(), testPreparedReferenceSnapshot1}
	if got := prepared.SnapshotIDs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("SnapshotIDs() = %#v, want %#v", got, want)
	}
	if got := prepared.References(); len(got) != 2 || got[0].SnapshotID() != testPreparedReferenceSnapshot1 ||
		got[1].SnapshotID() != testPreparedReferenceSnapshot2 {
		t.Fatalf("References() = %#v, want immutable reference order", got)
	}
	if got := prepared.Captures(); len(got) != 1 || got[0].SnapshotID() != capture.SnapshotID() {
		t.Fatalf("Captures() = %#v, want immutable prepared capture", got)
	}
}

func TestRevisionPreparationRejectsInvalidDuplicateMissingAndWrongActorReferences(t *testing.T) {
	actor := testPreparedReferenceActor(t)
	first := mustPreparedExistingReference(t, actor, testPreparedReferenceSnapshot1)
	second := mustPreparedExistingReference(t, actor, testPreparedReferenceSnapshot2)
	tests := []struct {
		name   string
		values RevisionPreparationValues
	}{
		{name: "duplicate ordered identity", values: RevisionPreparationValues{
			References: []PreparedReference{first, second}, OrderedSnapshotIDs: []string{first.SnapshotID(), first.SnapshotID()},
		}},
		{name: "missing ordered identity", values: RevisionPreparationValues{
			References: []PreparedReference{first, second}, OrderedSnapshotIDs: []string{first.SnapshotID()},
		}},
		{name: "unknown ordered identity", values: RevisionPreparationValues{
			References: []PreparedReference{first}, OrderedSnapshotIDs: []string{first.SnapshotID(), second.SnapshotID()},
		}},
		{name: "duplicate reference", values: RevisionPreparationValues{
			References: []PreparedReference{first, first}, OrderedSnapshotIDs: []string{first.SnapshotID()},
		}},
		{name: "zero reference", values: RevisionPreparationValues{
			References: []PreparedReference{{}}, OrderedSnapshotIDs: []string{first.SnapshotID()},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewRevisionPreparation(testPreparedReferenceRecordID, tt.values); !errors.Is(err, ErrInvalidRevisionPreparation) {
				t.Fatalf("NewRevisionPreparation() error = %v, want ErrInvalidRevisionPreparation", err)
			}
		})
	}

	otherActor := actor.Clone()
	otherActor.UserID = "usr_bbbbbbbbbbbbbbbbbbbbbbbb"
	otherActor, err := recordauth.NormalizeActorScope(otherActor)
	if err != nil {
		t.Fatalf("NormalizeActorScope(other) error = %v", err)
	}
	prepared, err := NewRevisionPreparation(testPreparedReferenceRecordID, RevisionPreparationValues{
		References: []PreparedReference{first}, OrderedSnapshotIDs: []string{first.SnapshotID()},
	})
	if err != nil {
		t.Fatalf("NewRevisionPreparation() error = %v", err)
	}
	if err := prepared.ValidateReferencesForActor(otherActor); !errors.Is(err, ErrInvalidRevisionPreparation) {
		t.Fatalf("ValidateReferencesForActor(other) error = %v, want ErrInvalidRevisionPreparation", err)
	}
}

func testPreparedReferenceActor(t *testing.T) ActorScope {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID:    "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
		Role:      recordauth.RoleProjectAdmin,
		ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	return actor
}

func testExistingSnapshotReferenceState(t *testing.T, snapshotID string) ExistingSnapshotReferenceState {
	t.Helper()
	authorization := testAuthorization(t)
	return ExistingSnapshotReferenceState{
		RecordID:                   testPreparedReferenceRecordID,
		SnapshotID:                 snapshotID,
		Key:                        MonitoringProbeV2Key(),
		SourceType:                 string(authorization.Kind),
		SourceID:                   authorization.SourceID,
		CaptureAuthorizationDigest: authorization.Digest,
		PayloadDigest:              sha256.Sum256([]byte("payload:" + snapshotID)),
		Authorization:              authorization,
	}
}

func mustPreparedExistingReference(t *testing.T, actor ActorScope, snapshotID string) PreparedReference {
	t.Helper()
	state := testExistingSnapshotReferenceState(t, snapshotID)
	prepared, err := PrepareExistingSnapshotReference(
		context.Background(), &existingSnapshotReferenceSourceStub{state: state}, actor, state.RecordID, snapshotID,
	)
	if err != nil {
		t.Fatalf("PrepareExistingSnapshotReference() error = %v", err)
	}
	return prepared
}

type existingSnapshotReferenceSourceStub struct {
	state            ExistingSnapshotReferenceState
	err              error
	reauthorizeCalls int
	recaptureCalls   int
	payloadCopyCalls int
	actor            ActorScope
	recordID         string
	snapshotID       string
}

func (source *existingSnapshotReferenceSourceStub) ReauthorizeExistingSnapshot(
	_ context.Context,
	actor ActorScope,
	recordID string,
	snapshotID string,
) (ExistingSnapshotReferenceState, error) {
	source.reauthorizeCalls++
	source.actor = actor.Clone()
	source.recordID = recordID
	source.snapshotID = snapshotID
	return source.state, source.err
}

// These methods deliberately are not part of ExistingSnapshotReferenceSource.
func (source *existingSnapshotReferenceSourceStub) RecaptureSource() { source.recaptureCalls++ }
func (source *existingSnapshotReferenceSourceStub) CopyPayload()     { source.payloadCopyCalls++ }
