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
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

func TestPostgresIntegrationRecordActionsLifecycleReplayAndRootIsolation(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "actions-parent-key")
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-actions-lifecycle", 3)
	repository := NewPostgresRecordActionRepository(
		runtimePool, fixture.admissionGate, NewPostgresCollaborationMembershipReader(),
		newRecordsPostgresAuthorization(t, runtimePool, fixture.admissionGate),
	)
	rootBefore := readPostgresActionRoot(t, ctx, fixture, parent.RecordID)

	dueAt := time.Date(2026, time.August, 21, 9, 30, 0, 123456000, time.UTC)
	createFields := mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{
		Title: "Investigate private symptom", Details: "private diagnostic details", DueAt: &dueAt,
	})
	updateFields := mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{
		Title: "Verify private resolution", Details: "private verification details",
		AssigneeID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", DueAt: &dueAt, SubjectRevisionID: parent.RevisionID,
	})
	commands := []recordcollaboration.ActionCommand{
		postgresActionCommand(t, parent, recordcollaboration.ActionMutationCreate, "ract_pgflow", 0, createFields, "actions-create-key"),
		postgresActionCommand(t, parent, recordcollaboration.ActionMutationComplete, "ract_pgflow", 1, recordcollaboration.ActionFields{}, "actions-complete-key"),
		postgresActionCommand(t, parent, recordcollaboration.ActionMutationUpdate, "ract_pgflow", 2, updateFields, "actions-update-key"),
		postgresActionCommand(t, parent, recordcollaboration.ActionMutationReopen, "ract_pgflow", 3, recordcollaboration.ActionFields{}, "actions-reopen-key"),
		postgresActionCommand(t, parent, recordcollaboration.ActionMutationCancel, "ract_pgflow", 4, recordcollaboration.ActionFields{}, "actions-cancel-key"),
	}
	wantStatuses := []recordcollaboration.ActionStatus{
		recordcollaboration.ActionStatusOpen,
		recordcollaboration.ActionStatusCompleted,
		recordcollaboration.ActionStatusCompleted,
		recordcollaboration.ActionStatusOpen,
		recordcollaboration.ActionStatusCancelled,
	}
	for index, command := range commands {
		result, err := repository.CommitAction(ctx, command)
		if err != nil {
			t.Fatalf("CommitAction(%s) error = %v", command.Kind, err)
		}
		if result.Replayed || result.Version != uint64(index+1) || result.Status != wantStatuses[index] || result.EventKind != command.Kind {
			t.Fatalf("CommitAction(%s) result = %#v", command.Kind, result)
		}
	}

	for index, command := range commands {
		result, err := repository.CommitAction(ctx, command)
		if err != nil {
			t.Fatalf("CommitAction(%s) replay error = %v", command.Kind, err)
		}
		if !result.Replayed || result.Version != uint64(index+1) || result.Status != wantStatuses[index] || result.EventKind != command.Kind {
			t.Fatalf("CommitAction(%s) replay = %#v", command.Kind, result)
		}
	}

	conflict := commands[0]
	conflict.Idempotency.RequestFingerprint = mustStoreActionFingerprint(t, conflict.Idempotency.Key.OperationKind, 0x7f)
	if result, err := repository.CommitAction(ctx, conflict); !errors.Is(err, recordplatform.ErrIdempotencyKeyReused) || result != (recordcollaboration.ActionMutationResult{}) {
		t.Fatalf("same-key different-fingerprint result=%#v error=%v", result, err)
	}
	resultConflict := commands[0]
	resultConflict.ResultFingerprint = mustStoreActionFingerprint(t, resultConflict.Idempotency.Key.OperationKind, 0x7e)
	if result, err := repository.CommitAction(ctx, resultConflict); !errors.Is(err, recordplatform.ErrIdempotencyConflictState) || result != (recordcollaboration.ActionMutationResult{}) {
		t.Fatalf("same request with a different result fingerprint = (%#v, %v)", result, err)
	}

	rootAfter := readPostgresActionRoot(t, ctx, fixture, parent.RecordID)
	if !reflect.DeepEqual(rootAfter, rootBefore) {
		t.Fatalf("record root mutated by action lifecycle: before=%#v after=%#v", rootBefore, rootAfter)
	}
	assertPostgresActionLifecycleState(t, ctx, fixture, parent, dueAt)
	actor, evidence := storeActionAuthorization(t)
	actor = collaborationActor(t, "usr_aaaaaaaaaaaaaaaaaaaaaaaa", nil)
	actions, err := repository.ListActions(ctx, recordcollaboration.ActionReadCommand{
		Actor: actor, RecordID: parent.RecordID, CurrentRevisionID: parent.RevisionID,
		RecordLockVersion: parent.LockVersion, AuthorizationEpoch: parent.AuthorizationEpoch,
		AuthorizationEvidence: evidence, Limit: 25,
	})
	if err != nil {
		t.Fatalf("ListActions() error = %v", err)
	}
	if len(actions) != 1 || actions[0].ActionID != "ract_pgflow" || actions[0].Version != 5 ||
		actions[0].Title != "Verify private resolution" || actions[0].Details != "private verification details" ||
		actions[0].Status != recordcollaboration.ActionStatusCancelled ||
		actions[0].AssigneeID != "usr_bbbbbbbbbbbbbbbbbbbbbbbb" || actions[0].SubjectRevisionID != parent.RevisionID ||
		actions[0].DueAt == nil || !actions[0].DueAt.Equal(dueAt) || actions[0].CompletedAt != nil {
		t.Fatalf("ListActions() = %#v, want bounded current state", actions)
	}
}

func TestPostgresIntegrationRecordActionsRejectInvalidTransitionsAndAssignees(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "actions-policy-parent")
	runtime := fixture.openDirectRuntimePool(t, ctx, "record-actions-policy", 3)
	repository := newPostgresActionRepositoryForTest(t, runtime, fixture.admissionGate)
	create := postgresActionCommand(t, parent, recordcollaboration.ActionMutationCreate, "ract_pgpolicy", 0,
		mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{Title: "Action policy"}), "actions-policy-create")
	if result, err := repository.CommitAction(ctx, create); err != nil || result.Version != 1 {
		t.Fatalf("create action = (%#v, %v)", result, err)
	}
	complete := postgresActionCommand(t, parent, recordcollaboration.ActionMutationComplete, create.ActionID, 1,
		recordcollaboration.ActionFields{}, "actions-policy-complete")
	if result, err := repository.CommitAction(ctx, complete); err != nil || result.Status != recordcollaboration.ActionStatusCompleted || result.Version != 2 {
		t.Fatalf("complete action = (%#v, %v)", result, err)
	}
	assertRejected := func(repository *PostgresRecordActionRepository, command recordcollaboration.ActionCommand, want error) {
		t.Helper()
		before := readPostgresActionAuthorizationCounts(t, ctx, fixture, parent.RecordID, command.Idempotency.Key.Key)
		if result, err := repository.CommitAction(ctx, command); !errors.Is(err, want) || result != (recordcollaboration.ActionMutationResult{}) {
			t.Fatalf("rejected action = (%#v, %v), want %v", result, err, want)
		}
		if after := readPostgresActionAuthorizationCounts(t, ctx, fixture, parent.RecordID, command.Idempotency.Key.Key); after != before {
			t.Fatalf("rejected action changed durable counts: before=%#v after=%#v", before, after)
		}
	}
	assertRejected(repository, postgresActionCommand(t, parent, recordcollaboration.ActionMutationComplete, create.ActionID, 2,
		recordcollaboration.ActionFields{}, "actions-policy-repeat-complete"), recordcollaboration.ErrInvalidActionStateTransition)
	assertRejected(repository, postgresActionCommand(t, parent, recordcollaboration.ActionMutationReopen, create.ActionID, 1,
		recordcollaboration.ActionFields{}, "actions-policy-stale"), recordcollaboration.ErrActionConflict)
	for _, step := range []struct {
		kind    recordcollaboration.ActionMutationKind
		version uint64
		status  recordcollaboration.ActionStatus
		key     string
	}{
		{recordcollaboration.ActionMutationReopen, 2, recordcollaboration.ActionStatusOpen, "actions-policy-reopen-complete"},
		{recordcollaboration.ActionMutationCancel, 3, recordcollaboration.ActionStatusCancelled, "actions-policy-cancel"},
		{recordcollaboration.ActionMutationReopen, 4, recordcollaboration.ActionStatusOpen, "actions-policy-reopen-cancel"},
	} {
		command := postgresActionCommand(t, parent, step.kind, create.ActionID, step.version, recordcollaboration.ActionFields{}, step.key)
		if result, err := repository.CommitAction(ctx, command); err != nil || result.Version != step.version+1 || result.Status != step.status {
			t.Fatalf("transition %s = (%#v, %v)", step.kind, result, err)
		}
	}
	assigneeCommand := func(userID, key string) recordcollaboration.ActionCommand {
		return postgresActionCommand(t, parent, recordcollaboration.ActionMutationUpdate, create.ActionID, 5,
			mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{Title: "Assigned action", AssigneeID: userID}), key)
	}
	assertRejected(repository, assigneeCommand("usr_eeeeeeeeeeeeeeeeeeeeeeee", "actions-policy-missing-assignee"), recordcollaboration.ErrMembershipDenied)
	if _, err := fixture.db.Exec(ctx, `update public.users set role='viewer' where user_id=$1`, "usr_dddddddddddddddddddddddd"); err != nil {
		t.Fatalf("demote assignee: %v", err)
	}
	assertRejected(repository, assigneeCommand("usr_dddddddddddddddddddddddd", "actions-policy-denied-assignee"), recordcollaboration.ErrMembershipDenied)
	unavailable := &selectiveUnavailableActionMembershipReader{
		delegate: NewPostgresCollaborationMembershipReader(), userID: "usr_cccccccccccccccccccccccc",
	}
	unavailableRepository := NewPostgresRecordActionRepository(runtime, fixture.admissionGate, unavailable, newRecordsPostgresAuthorization(t, runtime, fixture.admissionGate))
	assertRejected(unavailableRepository, assigneeCommand(unavailable.userID, "actions-policy-unavailable-assignee"), recordcollaboration.ErrMembershipUnavailable)
	var version int
	var status string
	if err := fixture.db.QueryRow(ctx, `select action_version::int, status from public.record_actions where action_id=$1`, create.ActionID).Scan(&version, &status); err != nil {
		t.Fatalf("read action after rejected updates: %v", err)
	}
	if version != 5 || status != string(recordcollaboration.ActionStatusOpen) {
		t.Fatalf("action after rejected updates version/status = %d/%q, want 5/open", version, status)
	}
}

type selectiveUnavailableActionMembershipReader struct {
	delegate CollaborationMembershipReader
	userID   string
}

func (reader *selectiveUnavailableActionMembershipReader) ReadMemberActor(ctx context.Context, tx pgx.Tx, projectID recordauth.ProjectID, userID string) (recordauth.ActorScope, error) {
	if userID == reader.userID {
		return recordauth.ActorScope{}, recordcollaboration.ErrMembershipUnavailable
	}
	return reader.delegate.ReadMemberActor(ctx, tx, projectID, userID)
}

func TestPostgresIntegrationRecordActionLiveIdempotencyOwnerPreventsEffects(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "actions-progress-parent")
	runtime := fixture.openDirectRuntimePool(t, ctx, "record-actions-in-progress", 3)
	command := postgresActionCommand(t, parent, recordcollaboration.ActionMutationCreate, "ract_pginprogress", 0,
		mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{Title: "Pending action"}), "actions-in-progress")
	platform := NewPostgresRecordPlatformRepository(runtime, fixture.admissionGate)
	if claim, err := platform.ClaimIdempotency(ctx, command.Idempotency); err != nil || claim.Owner == nil {
		t.Fatalf("claim in-progress key = (%#v, %v)", claim, err)
	}
	before := readPostgresActionAuthorizationCounts(t, ctx, fixture, parent.RecordID, command.Idempotency.Key.Key)
	repository := newPostgresActionRepositoryForTest(t, runtime, fixture.admissionGate)
	if result, err := repository.CommitAction(ctx, command); !errors.Is(err, recordplatform.ErrIdempotencyInProgress) || result != (recordcollaboration.ActionMutationResult{}) {
		t.Fatalf("action with live idempotency owner = (%#v, %v)", result, err)
	}
	if after := readPostgresActionAuthorizationCounts(t, ctx, fixture, parent.RecordID, command.Idempotency.Key.Key); after != before {
		t.Fatalf("in-progress action changed durable counts: before=%#v after=%#v", before, after)
	}
}

func TestPostgresIntegrationRecordActionsRefreshSourceAuthorizationInsideTransaction(t *testing.T) {
	for _, operation := range []string{"create", "list", "replay"} {
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			fixture := newAdmittedCoreRecordFixture(t, ctx)
			seedCollaborationRevisionUsers(t, ctx, fixture)
			parent := seedPostgresActionParent(t, ctx, fixture, "actions-auth-parent-"+operation)
			runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-actions-auth-"+operation, 3)
			actor := collaborationActor(t, "usr_aaaaaaaaaaaaaaaaaaaaaaaa", nil)
			request := recordcollaboration.ActionCreateRequest{
				Actor: actor, RecordID: parent.RecordID, Fields: recordcollaboration.ActionFieldValues{Title: "Refresh current source"},
				IdempotencyKey: "actions-auth-" + operation, IdempotencyOwnerID: "records_actions_api",
				OwnerLeaseDuration: time.Minute, IdempotencyTTL: 24 * time.Hour, OutboxTTL: 24 * time.Hour,
			}
			authorization := newRecordsPostgresAuthorization(t, runtimePool, fixture.admissionGate)
			seedRepository := NewPostgresRecordActionRepository(runtimePool, fixture.admissionGate, NewPostgresCollaborationMembershipReader(), authorization)
			if operation == "list" {
				seed := postgresActionCommand(t, parent, recordcollaboration.ActionMutationCreate, "ract_pgauthlist", 0,
					mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{Title: "List seed"}), request.IdempotencyKey+"-seed")
				if _, err := seedRepository.CommitAction(ctx, seed); err != nil {
					t.Fatalf("CommitAction(list seed) error = %v", err)
				}
			} else if operation == "replay" {
				seedService, err := recordcollaboration.NewActionService(authorization, seedRepository)
				if err != nil {
					t.Fatal(err)
				}
				if seeded, err := seedService.CreateAction(ctx, request); err != nil || seeded.Version != 1 {
					t.Fatalf("CreateAction(replay seed) = (%#v, %v)", seeded, err)
				}
			}
			repository := NewPostgresRecordActionRepository(runtimePool, postgresCollaborationSourceRevocationGate(fixture),
				NewPostgresCollaborationMembershipReader(), authorization)
			service, err := recordcollaboration.NewActionService(authorization, repository)
			if err != nil {
				t.Fatal(err)
			}
			before := readPostgresActionAuthorizationCounts(t, ctx, fixture, parent.RecordID, request.IdempotencyKey)
			var operationErr error
			if operation == "list" {
				var result []recordcollaboration.ActionRecord
				result, operationErr = service.ListActions(ctx, recordcollaboration.ActionListRequest{Actor: actor, RecordID: parent.RecordID, Limit: 25})
				if len(result) != 0 {
					t.Fatalf("revoked ListActions returned content: %#v", result)
				}
			} else {
				var result recordcollaboration.ActionMutationResult
				result, operationErr = service.CreateAction(ctx, request)
				if result != (recordcollaboration.ActionMutationResult{}) {
					t.Fatalf("revoked CreateAction returned content: %#v", result)
				}
			}
			if !errors.Is(operationErr, ErrRecordSubjectUnavailable) {
				t.Fatalf("%s source revocation error = %v, want ErrRecordSubjectUnavailable", operation, operationErr)
			}
			assertPostgresCollaborationSourceRevoked(t, ctx, fixture)
			after := readPostgresActionAuthorizationCounts(t, ctx, fixture, parent.RecordID, request.IdempotencyKey)
			if after != before {
				t.Fatalf("%s authorization failure durable counts = %#v, want unchanged %#v", operation, after, before)
			}
		})
	}
}

func TestPostgresIntegrationRecordActionsRequireCurrentActorMembership(t *testing.T) {
	for _, membership := range []string{"missing", "demoted"} {
		for _, operation := range []string{"create", "transition", "list"} {
			t.Run(membership+"/"+operation, func(t *testing.T) {
				ctx := context.Background()
				fixture := newAdmittedCoreRecordFixture(t, ctx)
				seedCollaborationRevisionUsers(t, ctx, fixture)
				parent := seedPostgresActionParent(t, ctx, fixture, "actions-member-parent-"+membership+"-"+operation)
				runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-actions-member-"+membership+"-"+operation, 2)
				repository := NewPostgresRecordActionRepository(
					runtimePool, fixture.admissionGate, NewPostgresCollaborationMembershipReader(),
					newRecordsPostgresAuthorization(t, runtimePool, fixture.admissionGate),
				)
				allowedActor := collaborationActor(t, "usr_aaaaaaaaaaaaaaaaaaaaaaaa", nil)
				actor := allowedActor
				if membership == "missing" {
					actor = collaborationActor(t, "usr_eeeeeeeeeeeeeeeeeeeeeeee", nil)
				} else if _, err := fixture.db.Exec(ctx, `update public.users set role = 'viewer' where user_id = $1`, actor.UserID); err != nil {
					t.Fatalf("demote action actor: %v", err)
				}

				actionID := "ract_pgmember" + membership + operation
				key := "actions-member-" + membership + "-" + operation
				if operation != "create" {
					if membership == "demoted" {
						if _, err := fixture.db.Exec(ctx, `update public.users set role = 'admin' where user_id = $1`, allowedActor.UserID); err != nil {
							t.Fatalf("temporarily restore seed actor: %v", err)
						}
					}
					seed := postgresActionCommand(t, parent, recordcollaboration.ActionMutationCreate, actionID, 0,
						mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{Title: "Membership seed"}), key+"-seed")
					seed.Actor = allowedActor
					if _, err := repository.CommitAction(ctx, seed); err != nil {
						t.Fatalf("CommitAction(seed) error = %v", err)
					}
					if membership == "demoted" {
						if _, err := fixture.db.Exec(ctx, `update public.users set role = 'viewer' where user_id = $1`, allowedActor.UserID); err != nil {
							t.Fatalf("demote seeded action actor: %v", err)
						}
					}
				}

				var operationErr error
				switch operation {
				case "create":
					command := postgresActionCommand(t, parent, recordcollaboration.ActionMutationCreate, actionID, 0,
						mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{Title: "Denied create"}), key)
					command.Actor = actor
					_, operationErr = repository.CommitAction(ctx, command)
				case "transition":
					command := postgresActionCommand(t, parent, recordcollaboration.ActionMutationComplete, actionID, 1,
						recordcollaboration.ActionFields{}, key)
					command.Actor = actor
					_, operationErr = repository.CommitAction(ctx, command)
				case "list":
					_, evidence := storeActionAuthorization(t)
					_, operationErr = repository.ListActions(ctx, recordcollaboration.ActionReadCommand{
						Actor: actor, RecordID: parent.RecordID, CurrentRevisionID: parent.RevisionID,
						RecordLockVersion: parent.LockVersion, AuthorizationEpoch: parent.AuthorizationEpoch,
						AuthorizationEvidence: evidence, Limit: 25,
					})
				}
				if !errors.Is(operationErr, recordauth.ErrDenied) || errors.Is(operationErr, recordcollaboration.ErrMembershipDenied) {
					t.Fatalf("%s %s action actor error = %v, want opaque recordauth.ErrDenied", membership, operation, operationErr)
				}
				var version, keyCount int
				if err := fixture.db.QueryRow(ctx, `
					select coalesce((select action_version::int from public.record_actions where record_id = $1 and action_id = $2), 0),
					       (select count(*)::int from public.record_idempotency_keys where idempotency_key = $3)`,
					parent.RecordID, actionID, key).Scan(&version, &keyCount); err != nil {
					t.Fatalf("read denied action state: %v", err)
				}
				wantVersion := 0
				if operation != "create" {
					wantVersion = 1
				}
				if version != wantVersion || keyCount != 0 {
					t.Fatalf("%s %s durable version/key count = %d/%d, want %d/0", membership, operation, version, keyCount, wantVersion)
				}
			})
		}
	}
}

func TestPostgresIntegrationRecordActionsConcurrentSameVersionHasOneWinner(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "actions-race-parent")
	seedPool := fixture.openDirectRuntimePool(t, ctx, "record-actions-race-seed", 1)
	seedRepository := NewPostgresRecordActionRepository(
		seedPool,
		fixture.admissionGate,
		NewPostgresCollaborationMembershipReader(),
		newRecordsPostgresAuthorization(t, seedPool, fixture.admissionGate),
	)
	create := postgresActionCommand(t, parent, recordcollaboration.ActionMutationCreate, "ract_pgrace", 0,
		mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{Title: "Race"}), "actions-race-create")
	if _, err := seedRepository.CommitAction(ctx, create); err != nil {
		t.Fatalf("CommitAction(create) error = %v", err)
	}
	rootBefore := readPostgresActionRoot(t, ctx, fixture, parent.RecordID)

	commands := []recordcollaboration.ActionCommand{
		postgresActionCommand(t, parent, recordcollaboration.ActionMutationComplete, create.ActionID, 1, recordcollaboration.ActionFields{}, "actions-race-a"),
		postgresActionCommand(t, parent, recordcollaboration.ActionMutationComplete, create.ActionID, 1, recordcollaboration.ActionFields{}, "actions-race-b"),
	}
	type outcome struct {
		result recordcollaboration.ActionMutationResult
		err    error
	}
	outcomes := make(chan outcome, len(commands))
	start := make(chan struct{})
	for index, command := range commands {
		pool := fixture.openDirectRuntimePool(t, ctx, fmt.Sprintf("record-actions-race-%d", index), 1)
		repository := NewPostgresRecordActionRepository(
			pool,
			fixture.admissionGate,
			NewPostgresCollaborationMembershipReader(),
			newRecordsPostgresAuthorization(t, pool, fixture.admissionGate),
		)
		command := command
		go func() {
			<-start
			result, err := repository.CommitAction(context.Background(), command)
			outcomes <- outcome{result: result, err: err}
		}()
	}
	close(start)

	winners, conflicts := 0, 0
	for range commands {
		select {
		case got := <-outcomes:
			switch {
			case got.err == nil:
				winners++
				if got.result.Version != 2 || got.result.Status != recordcollaboration.ActionStatusCompleted {
					t.Fatalf("winner result = %#v", got.result)
				}
			case errors.Is(got.err, recordcollaboration.ErrActionConflict):
				conflicts++
			default:
				t.Fatalf("concurrent CommitAction() result=%#v error=%v", got.result, got.err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("timed out waiting for concurrent action commits")
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("concurrent outcomes winners/conflicts = %d/%d", winners, conflicts)
	}

	var version, eventCount, activityCount, outboxCount, commandKeyCount int
	var status string
	if err := fixture.db.QueryRow(ctx, `
		select action_version::int, status,
		       (select count(*)::int from public.record_action_events where action_id = $1),
		       (select count(*)::int from public.record_domain_activities where record_id = $2 and source_event_id like 'raev_%'),
		       (select count(*)::int from public.record_outbox where subject_kind = 'action' and subject_id = $1),
		       (select count(*)::int from public.record_idempotency_keys where idempotency_key in ('actions-race-a', 'actions-race-b'))
		from public.record_actions where action_id = $1`, create.ActionID, parent.RecordID).Scan(
		&version, &status, &eventCount, &activityCount, &outboxCount, &commandKeyCount,
	); err != nil {
		t.Fatalf("read concurrent action state: %v", err)
	}
	if version != 2 || status != string(recordcollaboration.ActionStatusCompleted) || eventCount != 2 || activityCount != 2 || outboxCount != 2 || commandKeyCount != 1 {
		t.Fatalf("concurrent durable state version/status=%d/%q events/activity/outbox/keys=%d/%d/%d/%d", version, status, eventCount, activityCount, outboxCount, commandKeyCount)
	}
	if rootAfter := readPostgresActionRoot(t, ctx, fixture, parent.RecordID); !reflect.DeepEqual(rootAfter, rootBefore) {
		t.Fatalf("concurrent actions mutated root: before=%#v after=%#v", rootBefore, rootAfter)
	}
}

func TestPostgresIntegrationRecordActionEventFailureRollsBackActionAndPlatformFacts(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "actions-rollback-parent")
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-actions-rollback", 1)
	repository := NewPostgresRecordActionRepository(
		runtimePool,
		fixture.admissionGate,
		NewPostgresCollaborationMembershipReader(),
		newRecordsPostgresAuthorization(t, runtimePool, fixture.admissionGate),
	)
	create := postgresActionCommand(t, parent, recordcollaboration.ActionMutationCreate, "ract_pgrollback", 0,
		mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{Title: "Rollback"}), "actions-rollback-create")
	if _, err := repository.CommitAction(ctx, create); err != nil {
		t.Fatalf("CommitAction(create) error = %v", err)
	}
	rootBefore := readPostgresActionRoot(t, ctx, fixture, parent.RecordID)
	var fenceEpoch int64
	if err := fixture.db.QueryRow(ctx, `select record_fence_epoch from public.record_actions where action_id = $1`, create.ActionID).Scan(&fenceEpoch); err != nil {
		t.Fatalf("read action fence epoch: %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_action_events (
			action_event_id, project_id, record_id, action_id, action_version,
			event_kind, previous_status, current_status, actor_id, record_fence_epoch, occurred_at
		) values ('raev_pgrollbackblock', 'default', $1, $2, 2,
			'completed', 'open', 'completed', $3, $4, transaction_timestamp())`,
		parent.RecordID, create.ActionID, create.Actor.UserID, fenceEpoch); err != nil {
		t.Fatalf("seed conflicting immutable action event: %v", err)
	}

	complete := postgresActionCommand(t, parent, recordcollaboration.ActionMutationComplete, create.ActionID, 1, recordcollaboration.ActionFields{}, "actions-rollback-complete")
	if result, err := repository.CommitAction(ctx, complete); err == nil || result != (recordcollaboration.ActionMutationResult{}) {
		t.Fatalf("CommitAction(complete) result=%#v error=%v, want real PostgreSQL rollback", result, err)
	}

	var version, activityCount, outboxCount, idempotencyCount int
	var status string
	var completedAt *time.Time
	if err := fixture.db.QueryRow(ctx, `
		select action_version::int, status, completed_at,
		       (select count(*)::int from public.record_domain_activities where record_id = $2 and source_event_id like 'raev_%'),
		       (select count(*)::int from public.record_outbox where subject_kind = 'action' and subject_id = $1),
		       (select count(*)::int from public.record_idempotency_keys where idempotency_key = 'actions-rollback-complete')
		from public.record_actions where action_id = $1`, create.ActionID, parent.RecordID).Scan(
		&version, &status, &completedAt, &activityCount, &outboxCount, &idempotencyCount,
	); err != nil {
		t.Fatalf("read rolled-back action state: %v", err)
	}
	if version != 1 || status != string(recordcollaboration.ActionStatusOpen) || completedAt != nil || activityCount != 1 || outboxCount != 1 || idempotencyCount != 0 {
		t.Fatalf("rolled-back durable state version/status/completed/activity/outbox/key=%d/%q/%v/%d/%d/%d", version, status, completedAt, activityCount, outboxCount, idempotencyCount)
	}
	if rootAfter := readPostgresActionRoot(t, ctx, fixture, parent.RecordID); !reflect.DeepEqual(rootAfter, rootBefore) {
		t.Fatalf("failed action mutated root: before=%#v after=%#v", rootBefore, rootAfter)
	}
}

func TestPostgresIntegrationRecordActionMaximumPersistedVersionConflictsWithoutWrites(t *testing.T) {
	ctx := context.Background()
	fixture := newAdmittedCoreRecordFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "actions-maxver-parent")
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-actions-maxver", 1)
	repository := NewPostgresRecordActionRepository(
		runtimePool,
		fixture.admissionGate,
		NewPostgresCollaborationMembershipReader(),
		newRecordsPostgresAuthorization(t, runtimePool, fixture.admissionGate),
	)
	create := postgresActionCommand(t, parent, recordcollaboration.ActionMutationCreate, "ract_pgmaxver", 0,
		mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{Title: "Maximum version"}), "actions-maxver-create")
	if _, err := repository.CommitAction(ctx, create); err != nil {
		t.Fatalf("CommitAction(create) error = %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `
		update public.record_actions
		set action_version = $2
		where action_id = $1`, create.ActionID, int64(recordcollaboration.MaxActionVersion)); err != nil {
		t.Fatalf("seed maximum persisted action version: %v", err)
	}
	rootBefore := readPostgresActionRoot(t, ctx, fixture, parent.RecordID)

	complete := postgresActionCommand(t, parent, recordcollaboration.ActionMutationComplete, create.ActionID,
		recordcollaboration.MaxActionVersion-1, recordcollaboration.ActionFields{}, "actions-maxver-complete")
	if result, err := repository.CommitAction(ctx, complete); !errors.Is(err, recordcollaboration.ErrActionConflict) || result != (recordcollaboration.ActionMutationResult{}) {
		t.Fatalf("CommitAction(maximum persisted version) result=%#v error=%v, want stable conflict", result, err)
	}

	var version int64
	var status string
	var completedAt *time.Time
	var eventCount, activityCount, outboxCount, idempotencyCount int
	if err := fixture.db.QueryRow(ctx, `
		select action_version, status, completed_at,
		       (select count(*)::int from public.record_action_events where action_id = $1),
		       (select count(*)::int from public.record_domain_activities where record_id = $2 and source_event_id like 'raev_%'),
		       (select count(*)::int from public.record_outbox where subject_kind = 'action' and subject_id = $1),
		       (select count(*)::int from public.record_idempotency_keys where idempotency_key = 'actions-maxver-complete')
		from public.record_actions where action_id = $1`, create.ActionID, parent.RecordID).Scan(
		&version, &status, &completedAt, &eventCount, &activityCount, &outboxCount, &idempotencyCount,
	); err != nil {
		t.Fatalf("read maximum-version action state: %v", err)
	}
	if version != int64(recordcollaboration.MaxActionVersion) || status != string(recordcollaboration.ActionStatusOpen) || completedAt != nil ||
		eventCount != 1 || activityCount != 1 || outboxCount != 1 || idempotencyCount != 0 {
		t.Fatalf("maximum-version durable state version/status/completed/events/activity/outbox/key=%d/%q/%v/%d/%d/%d/%d",
			version, status, completedAt, eventCount, activityCount, outboxCount, idempotencyCount)
	}
	if rootAfter := readPostgresActionRoot(t, ctx, fixture, parent.RecordID); !reflect.DeepEqual(rootAfter, rootBefore) {
		t.Fatalf("maximum-version conflict mutated root: before=%#v after=%#v", rootBefore, rootAfter)
	}
}

type postgresActionRootState struct {
	RevisionID         string
	LockVersion        int64
	AuthorizationEpoch int64
	Lifecycle          string
	BusinessStatus     *string
}

func readPostgresActionRoot(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, recordID string) postgresActionRootState {
	t.Helper()
	var state postgresActionRootState
	if err := fixture.db.QueryRow(ctx, `
		select current_revision_id, lock_version, authorization_epoch, lifecycle, current_business_status
		from public.records where record_id = $1`, recordID).Scan(
		&state.RevisionID, &state.LockVersion, &state.AuthorizationEpoch, &state.Lifecycle, &state.BusinessStatus,
	); err != nil {
		t.Fatalf("read parent record root: %v", err)
	}
	return state
}

func assertPostgresActionAuthorizationFailureLeftNoWrites(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	recordID string,
	idempotencyKey string,
) {
	t.Helper()
	var actions, events, activities, outbox, idempotency int
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.record_actions where record_id = $1),
		       (select count(*)::int from public.record_action_events where record_id = $1),
		       (select count(*)::int from public.record_domain_activities where record_id = $1 and source_event_id like 'raev_%'),
		       (select count(*)::int from public.record_outbox where subject_kind = 'action' and subject_id in (
		           select action_id from public.record_actions where record_id = $1)),
		       (select count(*)::int from public.record_idempotency_keys where idempotency_key = $2)`,
		recordID, idempotencyKey,
	).Scan(&actions, &events, &activities, &outbox, &idempotency); err != nil {
		t.Fatalf("read action authorization failure state: %v", err)
	}
	if actions != 0 || events != 0 || activities != 0 || outbox != 0 || idempotency != 0 {
		t.Fatalf("action authorization failure writes actions/events/activities/outbox/idempotency=%d/%d/%d/%d/%d, want zero",
			actions, events, activities, outbox, idempotency)
	}
}

type postgresActionAuthorizationCounts struct {
	actions     int
	events      int
	activities  int
	outbox      int
	idempotency int
}

func readPostgresActionAuthorizationCounts(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	recordID string,
	idempotencyKey string,
) postgresActionAuthorizationCounts {
	t.Helper()
	counts := postgresActionAuthorizationCounts{}
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.record_actions where record_id = $1),
		       (select count(*)::int from public.record_action_events where record_id = $1),
		       (select count(*)::int from public.record_domain_activities where record_id = $1 and source_event_id like 'raev_%'),
		       (select count(*)::int from public.record_outbox where subject_kind = 'action' and subject_id in (
		           select action_id from public.record_actions where record_id = $1)),
		       (select count(*)::int from public.record_idempotency_keys where idempotency_key = $2)`,
		recordID, idempotencyKey,
	).Scan(&counts.actions, &counts.events, &counts.activities, &counts.outbox, &counts.idempotency); err != nil {
		t.Fatalf("read action authorization counts: %v", err)
	}
	return counts
}

func seedPostgresActionParent(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, key string) records.RevisionCommitResult {
	t.Helper()
	seedPostgresCollaborationSource(t, ctx, fixture)
	runtime := fixture.openDirectRuntimePool(t, ctx, key+"-seed", 1)
	repository, err := NewPostgresRecordRepository(runtime, fixture.admissionGate, nil, newRecordsPostgresAuthorization(t, runtime, fixture.admissionGate), nil)
	if err != nil {
		t.Fatal(err)
	}
	command := revisionBirthBatchCommand(t, ctx, ImportAttachmentGCFixture{Runtime: runtime, Gate: fixture.admissionGate},
		recordsPostgresCompleteRevisionInput(t, "Action parent"), key)
	result, err := repository.CommitRevision(ctx, command)
	if err != nil {
		t.Fatalf("CommitRevision(parent) error = %v", err)
	}
	return result
}

// The stored collaboration revision refers to this real source. Bootstrap owns
// fixture setup; runtime is not granted any authority-table write privilege.
func seedPostgresCollaborationSource(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture) {
	t.Helper()
	if _, err := fixture.db.Exec(ctx, `
		insert into public.vps_assets (vps_id, display_name, lifecycle_status)
		values ($1, 'PostgreSQL VPS', 'active')
		on conflict (vps_id) do nothing`, testStoreRecordVPSID); err != nil {
		t.Fatalf("seed collaboration VPS source: %v", err)
	}
}

func postgresActionCommand(
	t *testing.T,
	parent records.RevisionCommitResult,
	kind recordcollaboration.ActionMutationKind,
	actionID string,
	expectedVersion uint64,
	fields recordcollaboration.ActionFields,
	idempotencyKey string,
) recordcollaboration.ActionCommand {
	t.Helper()
	command := testStoreActionCommand(t, kind, expectedVersion, fields)
	command.RecordID = parent.RecordID
	command.ActionID = actionID
	command.CurrentRevisionID = parent.RevisionID
	command.RecordLockVersion = parent.LockVersion
	command.AuthorizationEpoch = parent.AuthorizationEpoch
	command.Idempotency.Key.Key = idempotencyKey
	command.Actor = collaborationActor(t, "usr_aaaaaaaaaaaaaaaaaaaaaaaa", nil)
	return command
}

func mustPostgresActionFields(t *testing.T, values recordcollaboration.ActionFieldValues) recordcollaboration.ActionFields {
	t.Helper()
	fields, err := recordcollaboration.NormalizeActionFields(values)
	if err != nil {
		t.Fatal(err)
	}
	return fields
}

func newPostgresActionRepositoryForTest(
	t *testing.T,
	pool *pgxpool.Pool,
	gate AdmissionGate,
) *PostgresRecordActionRepository {
	t.Helper()
	return NewPostgresRecordActionRepository(
		pool, gate, NewPostgresCollaborationMembershipReader(), newRecordsPostgresAuthorization(t, pool, gate),
	)
}

func assertPostgresActionLifecycleState(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, parent records.RevisionCommitResult, dueAt time.Time) {
	t.Helper()
	var (
		version, actionCount, eventCount, activityCount, outboxCount, idempotencyCount int
		title, details, status, assigneeID, subjectRevisionID                          string
		persistedDueAt                                                                 time.Time
		completedAt                                                                    *time.Time
	)
	if err := fixture.db.QueryRow(ctx, `
		select action_version::int, title, details, status, assignee_id, subject_revision_id, due_at, completed_at,
		       (select count(*)::int from public.record_actions where record_id = $2),
		       (select count(*)::int from public.record_action_events where record_id = $2),
		       (select count(*)::int from public.record_domain_activities where record_id = $2 and source_event_id like 'raev_%'),
		       (select count(*)::int from public.record_outbox where subject_kind = 'action' and subject_id = $1),
		       (select count(*)::int from public.record_idempotency_keys where operation_kind like 'record_action_%')
		from public.record_actions where action_id = $1`, "ract_pgflow", parent.RecordID).Scan(
		&version, &title, &details, &status, &assigneeID, &subjectRevisionID, &persistedDueAt, &completedAt,
		&actionCount, &eventCount, &activityCount, &outboxCount, &idempotencyCount,
	); err != nil {
		t.Fatalf("read action lifecycle state: %v", err)
	}
	if version != 5 || title != "Verify private resolution" || details != "private verification details" ||
		status != string(recordcollaboration.ActionStatusCancelled) || assigneeID != "usr_bbbbbbbbbbbbbbbbbbbbbbbb" ||
		subjectRevisionID != parent.RevisionID || !persistedDueAt.Equal(dueAt) || completedAt != nil ||
		actionCount != 1 || eventCount != 5 || activityCount != 5 || outboxCount != 6 || idempotencyCount != 5 {
		t.Fatalf("action lifecycle state version/title/details/status/assignee/subject/due/completed/counts=%d/%q/%q/%q/%q/%q/%v/%v/%d/%d/%d/%d/%d",
			version, title, details, status, assigneeID, subjectRevisionID, persistedDueAt, completedAt,
			actionCount, eventCount, activityCount, outboxCount, idempotencyCount)
	}

	rows, err := fixture.db.Query(ctx, `
		select events.event_kind, activities.event_kind, outbox.event_kind,
		       idempotency.request_fingerprint, idempotency.result_fingerprint
		from public.record_action_events events
		join public.record_domain_activities activities on activities.source_event_id = events.action_event_id
		join public.record_outbox outbox on outbox.subject_kind = 'action' and outbox.subject_id = events.action_id
		join public.record_idempotency_keys idempotency
		  on idempotency.operation_kind = 'record_action_' || case events.event_kind when 'created' then 'create' when 'updated' then 'update' when 'completed' then 'complete' when 'cancelled' then 'cancel' when 'reopened' then 'reopen' end
		where events.action_id = 'ract_pgflow'
		  and outbox.event_kind = 'record_action_' || events.event_kind
		order by events.action_version`)
	if err != nil {
		t.Fatalf("query ordered action facts: %v", err)
	}
	defer rows.Close()
	wantMutations := []string{"created", "completed", "updated", "reopened", "cancelled"}
	wantActivities := []string{"action_created", "action_completed", "action_updated", "action_reopened", "action_cancelled"}
	index := 0
	for rows.Next() {
		var mutation, activity, outbox string
		var requestFingerprint, resultFingerprint []byte
		if err := rows.Scan(&mutation, &activity, &outbox, &requestFingerprint, &resultFingerprint); err != nil {
			t.Fatalf("scan ordered action facts: %v", err)
		}
		if index >= len(wantMutations) || mutation != wantMutations[index] || activity != wantActivities[index] || outbox != "record_action_"+mutation {
			t.Fatalf("ordered action fact %d = %q/%q/%q", index, mutation, activity, outbox)
		}
		for _, private := range [][]byte{[]byte("private title"), []byte("private details"), []byte("Verify private resolution"), []byte("private verification details")} {
			if bytes.Contains(requestFingerprint, private) || bytes.Contains(resultFingerprint, private) {
				t.Fatalf("durable fingerprint leaked private content at event %d", index)
			}
		}
		index++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate ordered action facts: %v", err)
	}
	if index != len(wantMutations) {
		t.Fatalf("ordered action facts = %d, want %d", index, len(wantMutations))
	}
}
