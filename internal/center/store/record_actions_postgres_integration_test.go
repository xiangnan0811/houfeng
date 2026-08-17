package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

func TestPostgresIntegrationRecordActionsLifecycleReplayAndRootIsolation(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "rec_pgactions", "actions-parent-key")
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-actions-lifecycle", 3)
	repository := NewPostgresRecordActionRepository(runtimePool, allowRecordPlatformAdmissionGate, NewPostgresCollaborationMembershipReader())
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

	rootAfter := readPostgresActionRoot(t, ctx, fixture, parent.RecordID)
	if !reflect.DeepEqual(rootAfter, rootBefore) {
		t.Fatalf("record root mutated by action lifecycle: before=%#v after=%#v", rootBefore, rootAfter)
	}
	assertPostgresActionLifecycleState(t, ctx, fixture, parent, dueAt)
}

func TestPostgresIntegrationRecordActionsConcurrentSameVersionHasOneWinner(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	parent := seedPostgresActionParent(t, ctx, fixture, "rec_pgactionrace", "actions-race-parent")
	seedRepository := NewPostgresRecordActionRepository(
		fixture.openDirectRuntimePool(t, ctx, "record-actions-race-seed", 1),
		allowRecordPlatformAdmissionGate,
		NewPostgresCollaborationMembershipReader(),
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
		repository := NewPostgresRecordActionRepository(
			fixture.openDirectRuntimePool(t, ctx, fmt.Sprintf("record-actions-race-%d", index), 1),
			allowRecordPlatformAdmissionGate,
			NewPostgresCollaborationMembershipReader(),
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
	fixture := newRecordsPostgresFixture(t, ctx)
	parent := seedPostgresActionParent(t, ctx, fixture, "rec_pgactionrollback", "actions-rollback-parent")
	repository := NewPostgresRecordActionRepository(
		fixture.openDirectRuntimePool(t, ctx, "record-actions-rollback", 1),
		allowRecordPlatformAdmissionGate,
		NewPostgresCollaborationMembershipReader(),
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
	fixture := newRecordsPostgresFixture(t, ctx)
	parent := seedPostgresActionParent(t, ctx, fixture, "rec_pgactionmaxver", "actions-maxver-parent")
	repository := NewPostgresRecordActionRepository(
		fixture.openDirectRuntimePool(t, ctx, "record-actions-maxver", 1),
		allowRecordPlatformAdmissionGate,
		NewPostgresCollaborationMembershipReader(),
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

func seedPostgresActionParent(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, recordID, key string) records.RevisionCommitResult {
	t.Helper()
	repository := newRecordsPostgresRepository(t, fixture.openDirectRuntimePool(t, ctx, key+"-seed", 1))
	result, err := repository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordCreate, recordID, "", 0, 0,
		recordsPostgresCompleteRevisionInput(t, "Action parent"), key,
	))
	if err != nil {
		t.Fatalf("CommitRevision(parent) error = %v", err)
	}
	return result
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
