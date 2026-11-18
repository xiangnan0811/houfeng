package store

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

type recordActionMembershipStub struct {
	caller      recordauth.ActorScope
	actor       recordauth.ActorScope
	err         error
	calls       int
	targetCalls int
	tx          pgx.Tx
}

func (member *recordActionMembershipStub) ReadMemberActor(_ context.Context, tx pgx.Tx, _ recordauth.ProjectID, userID string) (recordauth.ActorScope, error) {
	member.calls++
	member.tx = tx
	if userID == member.caller.UserID {
		return member.caller.Clone(), nil
	}
	member.targetCalls++
	if member.err != nil {
		return recordauth.ActorScope{}, member.err
	}
	return member.actor, nil
}

type recordAuthorizationPrefetchErrorStub struct {
	recordIDs []string
	err       error
}

func (stub *recordAuthorizationPrefetchErrorStub) prepareCurrentAuthorization(
	_ context.Context,
	recordIDs []string,
) (currentRecordAuthorizationTransactionResolver, error) {
	stub.recordIDs = append([]string(nil), recordIDs...)
	return nil, stub.err
}

func TestPostgresRecordActionRepositoryPrefetchFailurePreventsTransaction(t *testing.T) {
	command := testStoreActionCommand(t, recordcollaboration.ActionMutationCreate, 0, mustStoreActionFields(t, "Action", "safe", ""))
	read := recordcollaboration.ActionReadCommand{
		Actor: command.Actor, RecordID: command.RecordID, CurrentRevisionID: command.CurrentRevisionID,
		RecordLockVersion: command.RecordLockVersion, AuthorizationEpoch: command.AuthorizationEpoch,
		AuthorizationEvidence: command.AuthorizationEvidence, Limit: 25,
	}
	failure := errors.New("authorization prefetch failed")
	for _, operation := range []string{"commit", "list"} {
		t.Run(operation, func(t *testing.T) {
			beginCalls := 0
			prefetch := &recordAuthorizationPrefetchErrorStub{err: failure}
			repository := &PostgresRecordActionRepository{
				platform: &PostgresRecordPlatformRepository{beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
					beginCalls++
					return nil, errors.New("unexpected transaction")
				}},
				members: &recordActionMembershipStub{}, authorization: prefetch,
			}
			var err error
			if operation == "commit" {
				invalid := command
				invalid.RecordID = ""
				_, err = repository.CommitAction(context.Background(), invalid)
			} else {
				invalid := read
				invalid.RecordID = ""
				_, err = repository.ListActions(context.Background(), invalid)
			}
			if err == nil || prefetch.recordIDs != nil || beginCalls != 0 {
				t.Fatalf("invalid command error=%v prefetch IDs=%v transaction begins=%d", err, prefetch.recordIDs, beginCalls)
			}
			if operation == "commit" {
				_, err = repository.CommitAction(context.Background(), command)
			} else {
				_, err = repository.ListActions(context.Background(), read)
			}
			if !errors.Is(err, failure) || !slices.Equal(prefetch.recordIDs, []string{command.RecordID}) || beginCalls != 0 {
				t.Fatalf("error=%v prefetch IDs=%v transaction begins=%d", err, prefetch.recordIDs, beginCalls)
			}
		})
	}
}

func testStoreActionCommand(t *testing.T, kind recordcollaboration.ActionMutationKind, expected uint64, fields recordcollaboration.ActionFields) recordcollaboration.ActionCommand {
	t.Helper()
	actor, evidence := storeActionAuthorization(t)
	operation := map[recordcollaboration.ActionMutationKind]recordplatform.OperationKind{
		recordcollaboration.ActionMutationCreate:   recordplatform.OperationKindRecordActionCreate,
		recordcollaboration.ActionMutationUpdate:   recordplatform.OperationKindRecordActionUpdate,
		recordcollaboration.ActionMutationComplete: recordplatform.OperationKindRecordActionComplete,
		recordcollaboration.ActionMutationCancel:   recordplatform.OperationKindRecordActionCancel,
		recordcollaboration.ActionMutationReopen:   recordplatform.OperationKindRecordActionReopen,
	}[kind]
	requestFingerprint := mustStoreActionFingerprint(t, operation, 0x31)
	resultFingerprint := mustStoreActionFingerprint(t, operation, 0x41)
	return recordcollaboration.ActionCommand{
		Kind: kind, Actor: actor, RecordID: "rec_actionparent1", ActionID: "ract_action1", ExpectedVersion: expected,
		CurrentRevisionID: "rrv_current1", RecordLockVersion: 7, AuthorizationEpoch: 9,
		AuthorizationEvidence: evidence, Fields: fields, ResultFingerprint: resultFingerprint, OutboxTTL: 24 * time.Hour,
		Idempotency: recordplatform.IdempotencyClaimInputV1{Key: recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: operation, Key: "action-key-1"}, RequestFingerprint: requestFingerprint, OwnerID: "records_actions_api", OwnerLeaseDuration: time.Minute, RecordTTL: 24 * time.Hour},
	}
}

func mustStoreActionFingerprint(t *testing.T, operation recordplatform.OperationKind, value byte) recordplatform.RequestFingerprintV1 {
	t.Helper()
	digest := [32]byte{}
	for i := range digest {
		digest[i] = value
	}
	fingerprint, err := recordplatform.FingerprintRequestV1(recordplatform.RequestFingerprintInputV1{Version: recordplatform.RequestFingerprintVersionV1, OperationKind: operation, ProjectID: recordplatform.ProjectIDDefault, ActorScopeDigest: digest, RequestScopeDigest: digest, PayloadDigest: digest})
	if err != nil {
		t.Fatal(err)
	}
	return fingerprint
}

func mustStoreActionFields(t *testing.T, title, details, assignee string) recordcollaboration.ActionFields {
	t.Helper()
	fields, err := recordcollaboration.NormalizeActionFields(recordcollaboration.ActionFieldValues{Title: title, Details: details, AssigneeID: assignee})
	if err != nil {
		t.Fatal(err)
	}
	return fields
}

func storeActionAuthorization(t *testing.T) (recordauth.ActorScope, records.RecordAuthorizationEvidence) {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{UserID: "usr_0123456789abcdef01234567", Role: recordauth.RoleProjectAdmin, ProjectID: recordauth.ProjectIDDefault})
	if err != nil {
		t.Fatal(err)
	}
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{Version: recordauth.VisibilityScopeVersionV1, Kind: recordauth.VisibilityKindProject, ProjectID: recordauth.ProjectIDDefault, PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	source, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{Version: recordauth.SourceAuthorizationVersionV1, Kind: recordauth.SourceKindVPS, SourceID: "vps_0123456789abcdef", State: recordauth.SourceStateLive, CaptureScope: visibility, CurrentScope: &visibility})
	if err != nil {
		t.Fatal(err)
	}
	return actor, records.RecordAuthorizationEvidence{ProjectID: recordauth.ProjectIDDefault, Visibility: visibility, Sources: []recordauth.SourceAuthorization{source}}
}

func TestRecordActionNotificationOutboxKindsEmitAssignmentOnlyForNewAssignee(t *testing.T) {
	tests := []struct {
		name, previous, current string
		kind                    recordcollaboration.ActionMutationKind
		want                    []string
	}{
		{name: "create without assignee", kind: recordcollaboration.ActionMutationCreate, want: []string{recordplatform.OutboxEventKindRecordActionCreated}},
		{name: "create with assignee", kind: recordcollaboration.ActionMutationCreate, current: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", want: []string{recordplatform.OutboxEventKindRecordActionCreated, recordplatform.OutboxEventKindRecordActionAssigned}},
		{name: "unchanged assignee edit", kind: recordcollaboration.ActionMutationUpdate, previous: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", current: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", want: []string{recordplatform.OutboxEventKindRecordActionUpdated}},
		{name: "changed assignee edit", kind: recordcollaboration.ActionMutationUpdate, previous: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", current: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", want: []string{recordplatform.OutboxEventKindRecordActionUpdated, recordplatform.OutboxEventKindRecordActionAssigned}},
		{name: "cleared assignee edit", kind: recordcollaboration.ActionMutationUpdate, previous: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", want: []string{recordplatform.OutboxEventKindRecordActionUpdated}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := recordActionNotificationOutboxKinds(tt.kind, tt.previous, tt.current); !slices.Equal(got, tt.want) {
				t.Fatalf("recordActionNotificationOutboxKinds() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
