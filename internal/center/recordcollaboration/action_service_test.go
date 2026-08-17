package recordcollaboration

import (
	"context"
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

func TestActionServiceCreateBuildsAuthorizedContentBoundCommand(t *testing.T) {
	actor, current := testActionAuthorization(t, recordauth.RoleProjectAdmin)
	currentSource := &actionCurrentSourceStub{result: current}
	store := &actionCommandStoreStub{result: ActionMutationResult{
		ActionID: "ract_result1", RecordID: current.RecordID, Version: 1, Status: ActionStatusOpen,
		EventKind: ActionMutationCreate, ChangedAt: time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC),
	}}
	service, err := NewActionService(currentSource, store)
	if err != nil {
		t.Fatalf("NewActionService() error = %v", err)
	}

	request := testActionCreateRequest(actor, current.RecordID)
	result, err := service.CreateAction(context.Background(), request)
	if err != nil {
		t.Fatalf("CreateAction() error = %v", err)
	}
	if result != store.result || currentSource.calls != 1 || store.calls != 1 {
		t.Fatalf("result/calls = %#v current=%d store=%d", result, currentSource.calls, store.calls)
	}
	command := store.command
	if command.Kind != ActionMutationCreate || command.ActionID == "" || ValidateActionID(command.ActionID) != nil ||
		command.RecordID != current.RecordID || command.ExpectedVersion != 0 ||
		command.CurrentRevisionID != current.CurrentRevisionID || command.RecordLockVersion != current.LockVersion ||
		command.AuthorizationEpoch != current.AuthorizationEpoch || command.Actor.UserID != actor.UserID ||
		command.Fields.Title() != "Investigate timeout" || command.Fields.Details() != "Check the bounded trace" ||
		command.Idempotency.Key.OperationKind != recordplatform.OperationKindRecordActionCreate ||
		command.Idempotency.Key.Key != request.IdempotencyKey {
		t.Fatalf("command = %#v", command)
	}
	if command.ResultFingerprint.Validate() != nil || command.Idempotency.RequestFingerprint.Validate() != nil {
		t.Fatal("command fingerprints are not sealed")
	}
}

func TestActionServiceListsBoundedCurrentStateThroughReadAuthorization(t *testing.T) {
	actor, current := testActionAuthorization(t, recordauth.RoleProjectAdmin)
	createdAt := time.Date(2026, 8, 17, 10, 0, 0, 0, time.UTC)
	store := &actionCommandStoreStub{list: []ActionRecord{{
		ActionID: "ract_result1", RecordID: current.RecordID, Version: 1, Status: ActionStatusOpen,
		Title: "Review", CreatedAt: createdAt, UpdatedAt: createdAt,
	}}}
	service, err := NewActionService(&actionCurrentSourceStub{result: current}, store)
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.ListActions(context.Background(), ActionListRequest{
		Actor: actor, RecordID: current.RecordID, Limit: 25,
	})
	if err != nil {
		t.Fatalf("ListActions() error = %v", err)
	}
	if len(result) != 1 || result[0].Title != "Review" || store.readCommand.Limit != 25 ||
		store.readCommand.CurrentRevisionID != current.CurrentRevisionID ||
		store.readCommand.RecordLockVersion != current.LockVersion ||
		store.readCommand.AuthorizationEpoch != current.AuthorizationEpoch {
		t.Fatalf("ListActions() result=%#v command=%#v", result, store.readCommand)
	}
	store.list[0].Title = "mutated"
	if result[0].Title != "Review" {
		t.Fatal("ListActions() returned mutable store storage")
	}
}

func TestActionServiceFingerprintBindsExactCommandWithoutContentResult(t *testing.T) {
	actor, current := testActionAuthorization(t, recordauth.RoleProjectAdmin)
	makeCommand := func(request ActionCreateRequest) ActionCommand {
		store := &actionCommandStoreStub{}
		service, err := NewActionService(&actionCurrentSourceStub{result: current}, store)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.CreateAction(context.Background(), request); err != nil {
			t.Fatal(err)
		}
		return store.command
	}
	base := testActionCreateRequest(actor, current.RecordID)
	first := makeCommand(base)
	replay := makeCommand(base)
	if !first.Idempotency.RequestFingerprint.Equal(replay.Idempotency.RequestFingerprint) ||
		!first.ResultFingerprint.Equal(replay.ResultFingerprint) || first.ActionID != replay.ActionID {
		t.Fatal("exact command did not produce stable request/result identities")
	}

	changed := base
	changed.Fields.Details = "different protected content"
	second := makeCommand(changed)
	if first.Idempotency.RequestFingerprint.Equal(second.Idempotency.RequestFingerprint) || first.ActionID == second.ActionID {
		t.Fatal("payload change did not change request identity")
	}
	if first.ResultFingerprint.Equal(second.ResultFingerprint) {
		t.Fatal("different action identity produced the same result identity")
	}
	resultType := ActionMutationResult{}
	if resultType.ActionID != "" || resultType.RecordID != "" {
		t.Fatal("unexpected nonzero result")
	}
}

func TestActionServiceUpdateResultFingerprintContainsIdentityNotContent(t *testing.T) {
	actor, current := testActionAuthorization(t, recordauth.RoleProjectAdmin)
	makeCommand := func(fields ActionFieldValues) ActionCommand {
		store := &actionCommandStoreStub{}
		service, err := NewActionService(&actionCurrentSourceStub{result: current}, store)
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.UpdateAction(context.Background(), ActionUpdateRequest{
			ActionCommandRequest: testActionCommandRequest(actor, current.RecordID, "ract_action1", 4, "key-update"),
			Fields:               fields,
		})
		if err != nil {
			t.Fatal(err)
		}
		return store.command
	}
	first := makeCommand(ActionFieldValues{Title: "First private title", Details: "first private details"})
	second := makeCommand(ActionFieldValues{Title: "Second private title", Details: "second private details"})
	if first.Idempotency.RequestFingerprint.Equal(second.Idempotency.RequestFingerprint) {
		t.Fatal("different update content produced the same request fingerprint")
	}
	if !first.ResultFingerprint.Equal(second.ResultFingerprint) {
		t.Fatal("update result fingerprint changed with content despite identical result identity")
	}
	if first.ActionID != second.ActionID || first.ExpectedVersion != second.ExpectedVersion {
		t.Fatal("test did not hold result identity constant")
	}
}

func TestActionServiceCreateIdentityIsStablePerIdempotencyKeyAndDistinctAcrossKeys(t *testing.T) {
	actor, current := testActionAuthorization(t, recordauth.RoleProjectAdmin)
	makeCommand := func(key string) ActionCommand {
		store := &actionCommandStoreStub{}
		service, err := NewActionService(&actionCurrentSourceStub{result: current}, store)
		if err != nil {
			t.Fatal(err)
		}
		request := testActionCreateRequest(actor, current.RecordID)
		request.IdempotencyKey = key
		if _, err := service.CreateAction(context.Background(), request); err != nil {
			t.Fatal(err)
		}
		return store.command
	}
	first := makeCommand("create-key-a")
	retry := makeCommand("create-key-a")
	second := makeCommand("create-key-b")
	if first.ActionID != retry.ActionID {
		t.Fatal("same key and exact request did not produce a stable action identity")
	}
	if first.ActionID == second.ActionID {
		t.Fatal("different idempotency keys for identical creates collided on one action identity")
	}
	if !first.Idempotency.RequestFingerprint.Equal(second.Idempotency.RequestFingerprint) {
		t.Fatal("transport idempotency key changed the canonical request fingerprint")
	}
}

func TestActionServiceBuildsUpdateAndClosedTransitionCommands(t *testing.T) {
	actor, current := testActionAuthorization(t, recordauth.RoleProjectAdmin)
	tests := []struct {
		name      string
		kind      ActionMutationKind
		operation recordplatform.OperationKind
		call      func(*ActionService) error
	}{
		{name: "update", kind: ActionMutationUpdate, operation: recordplatform.OperationKindRecordActionUpdate, call: func(service *ActionService) error {
			_, err := service.UpdateAction(context.Background(), ActionUpdateRequest{
				ActionCommandRequest: testActionCommandRequest(actor, current.RecordID, "ract_action1", 4, "key-update"),
				Fields:               ActionFieldValues{Title: "Updated", Details: "safe", AssigneeID: "usr_111111111111111111111111"},
			})
			return err
		}},
		{name: "complete", kind: ActionMutationComplete, operation: recordplatform.OperationKindRecordActionComplete, call: func(service *ActionService) error {
			_, err := service.CompleteAction(context.Background(), testActionCommandRequest(actor, current.RecordID, "ract_action1", 4, "key-complete"))
			return err
		}},
		{name: "cancel", kind: ActionMutationCancel, operation: recordplatform.OperationKindRecordActionCancel, call: func(service *ActionService) error {
			_, err := service.CancelAction(context.Background(), testActionCommandRequest(actor, current.RecordID, "ract_action1", 4, "key-cancel"))
			return err
		}},
		{name: "reopen", kind: ActionMutationReopen, operation: recordplatform.OperationKindRecordActionReopen, call: func(service *ActionService) error {
			_, err := service.ReopenAction(context.Background(), testActionCommandRequest(actor, current.RecordID, "ract_action1", 4, "key-reopen"))
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &actionCommandStoreStub{}
			service, err := NewActionService(&actionCurrentSourceStub{result: current}, store)
			if err != nil {
				t.Fatal(err)
			}
			if err := test.call(service); err != nil {
				t.Fatalf("action call error = %v", err)
			}
			if store.command.Kind != test.kind || store.command.Idempotency.Key.OperationKind != test.operation ||
				store.command.ExpectedVersion != 4 || store.command.ActionID != "ract_action1" {
				t.Fatalf("command = %#v", store.command)
			}
		})
	}
}

func TestActionCommandAndServiceRejectNonIncrementableExpectedVersion(t *testing.T) {
	actor, current := testActionAuthorization(t, recordauth.RoleProjectAdmin)
	validStore := &actionCommandStoreStub{}
	service, err := NewActionService(&actionCurrentSourceStub{result: current}, validStore)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CompleteAction(context.Background(), testActionCommandRequest(
		actor, current.RecordID, "ract_action1", MaxActionVersion-1, "key-maximum-incrementable",
	)); err != nil {
		t.Fatalf("maximum incrementable service request error = %v", err)
	}
	command := validStore.command
	if err := command.Validate(); err != nil {
		t.Fatalf("maximum incrementable command error = %v", err)
	}
	for _, test := range []struct {
		name    string
		version uint64
	}{
		{name: "maximum bigint", version: MaxActionVersion},
		{name: "above maximum bigint", version: MaxActionVersion + 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			invalid := command
			invalid.ExpectedVersion = test.version
			if err := invalid.Validate(); !errors.Is(err, ErrInvalidActionCommand) {
				t.Fatalf("ActionCommand.Validate(%d) error = %v, want ErrInvalidActionCommand", test.version, err)
			}

			currentSource := &actionCurrentSourceStub{result: current}
			rejectedStore := &actionCommandStoreStub{}
			service, err := NewActionService(currentSource, rejectedStore)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.CompleteAction(context.Background(), testActionCommandRequest(
				actor, current.RecordID, "ract_action1", test.version, "key-non-incrementable",
			)); !errors.Is(err, ErrInvalidActionRequest) {
				t.Fatalf("service request version %d error = %v, want ErrInvalidActionRequest", test.version, err)
			}
			if currentSource.calls != 0 || rejectedStore.calls != 0 {
				t.Fatalf("non-incrementable request reached dependencies: current=%d store=%d", currentSource.calls, rejectedStore.calls)
			}
		})
	}
}

func TestActionServiceFailsClosedBeforeStore(t *testing.T) {
	admin, current := testActionAuthorization(t, recordauth.RoleProjectAdmin)
	viewer, _ := testActionAuthorization(t, recordauth.RoleViewer)
	tests := []struct {
		name       string
		actor      recordauth.ActorScope
		current    records.CurrentRecordAuthorization
		currentErr error
		request    ActionCreateRequest
		wantErr    error
	}{
		{name: "semantic content", actor: admin, current: current, request: func() ActionCreateRequest {
			r := testActionCreateRequest(admin, current.RecordID)
			r.Fields.Title = "\x00"
			return r
		}(), wantErr: ErrInvalidActionFields},
		{name: "viewer denied", actor: viewer, current: current, request: testActionCreateRequest(viewer, current.RecordID), wantErr: recordauth.ErrDenied},
		{name: "record missing", actor: admin, current: current, currentErr: records.ErrRecordNotFound, request: testActionCreateRequest(admin, current.RecordID), wantErr: records.ErrRecordNotFound},
		{name: "archived", actor: admin, current: func() records.CurrentRecordAuthorization {
			c := current
			c.Lifecycle = records.LifecycleArchived
			return c
		}(), request: testActionCreateRequest(admin, current.RecordID), wantErr: ErrActionConflict},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			currentSource := &actionCurrentSourceStub{result: test.current, err: test.currentErr}
			store := &actionCommandStoreStub{}
			service, err := NewActionService(currentSource, store)
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.CreateAction(context.Background(), test.request)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("CreateAction() error = %v, want %v", err, test.wantErr)
			}
			if store.calls != 0 {
				t.Fatalf("invalid request reached store %d times", store.calls)
			}
		})
	}
}

type actionCurrentSourceStub struct {
	result records.CurrentRecordAuthorization
	err    error
	calls  int
}

func (source *actionCurrentSourceStub) ResolveCurrentRecordAuthorization(context.Context, recordauth.ActorScope, string) (records.CurrentRecordAuthorization, error) {
	source.calls++
	return source.result, source.err
}

type actionCommandStoreStub struct {
	command     ActionCommand
	readCommand ActionReadCommand
	result      ActionMutationResult
	list        []ActionRecord
	err         error
	calls       int
}

func (store *actionCommandStoreStub) CommitAction(_ context.Context, command ActionCommand) (ActionMutationResult, error) {
	store.calls++
	store.command = command
	return store.result, store.err
}

func (store *actionCommandStoreStub) ListActions(_ context.Context, command ActionReadCommand) ([]ActionRecord, error) {
	store.calls++
	store.readCommand = command
	return store.list, store.err
}

func testActionCreateRequest(actor recordauth.ActorScope, recordID string) ActionCreateRequest {
	return ActionCreateRequest{
		Actor: actor, RecordID: recordID,
		Fields:         ActionFieldValues{Title: "Investigate timeout", Details: "Check the bounded trace"},
		IdempotencyKey: "action-create-1", IdempotencyOwnerID: "records_actions_api",
		OwnerLeaseDuration: time.Minute, IdempotencyTTL: 24 * time.Hour, OutboxTTL: 24 * time.Hour,
	}
}

func testActionCommandRequest(actor recordauth.ActorScope, recordID, actionID string, version uint64, key string) ActionCommandRequest {
	return ActionCommandRequest{
		Actor: actor, RecordID: recordID, ActionID: actionID, ExpectedVersion: version,
		IdempotencyKey: key, IdempotencyOwnerID: "records_actions_api",
		OwnerLeaseDuration: time.Minute, IdempotencyTTL: 24 * time.Hour, OutboxTTL: 24 * time.Hour,
	}
}

func testActionAuthorization(t *testing.T, role recordauth.Role) (recordauth.ActorScope, records.CurrentRecordAuthorization) {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID: "usr_0123456789abcdef01234567", Role: role, ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatal(err)
	}
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version: recordauth.VisibilityScopeVersionV1, Kind: recordauth.VisibilityKindProject,
		ProjectID: recordauth.ProjectIDDefault, PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	source, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version: recordauth.SourceAuthorizationVersionV1, Kind: recordauth.SourceKindVPS,
		SourceID: "vps_0123456789abcdef", State: recordauth.SourceStateLive,
		CaptureScope: visibility, CurrentScope: &visibility,
	})
	if err != nil {
		t.Fatal(err)
	}
	return actor, records.CurrentRecordAuthorization{
		RecordID: "rec_actionparent1", CurrentRevisionID: "rrv_current1", LockVersion: 7,
		AuthorizationEpoch: 9, Lifecycle: records.LifecycleActive,
		Evidence: records.RecordAuthorizationEvidence{ProjectID: recordauth.ProjectIDDefault, Visibility: visibility, Sources: []recordauth.SourceAuthorization{source}},
	}
}
