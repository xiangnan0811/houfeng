package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

func TestPostgresRecordActionRepositoryCommitsOrderedIdentityOnlyFactsWithoutMutatingRecordRoot(t *testing.T) {
	command := testStoreActionCommand(t, recordcollaboration.ActionMutationCreate, 0, recordcollaboration.ActionFields{})
	command.Fields = mustStoreActionFields(t, "Verify rollback", "protected details", "")
	tx := newFakeRecordActionTx(command)
	repository := newRecordActionTestRepository(tx, &recordActionMembershipStub{})

	result, err := repository.CommitAction(context.Background(), command)
	if err != nil {
		t.Fatalf("CommitAction() error = %v", err)
	}
	if result.Validate() != nil || result.ActionID != command.ActionID || result.Version != 1 ||
		result.Status != recordcollaboration.ActionStatusOpen || result.EventKind != recordcollaboration.ActionMutationCreate {
		t.Fatalf("result = %#v", result)
	}
	want := []string{"begin", "admission", "admission", "idempotency_lock", "idempotency_claim", "reservation_lock", "epoch_init", "epoch_lock", "fence_lock", "reservation_recheck", "epoch_lock", "root_lock", "action_create", "action_event", "activity", "follower_source", "outbox", "admission", "idempotency_complete", "commit", "rollback_cleanup"}
	if !reflect.DeepEqual(tx.steps, want) {
		t.Fatalf("steps = %#v, want %#v", tx.steps, want)
	}
	if tx.recordRootUpdates != 0 {
		t.Fatalf("record root updates = %d, want zero", tx.recordRootUpdates)
	}
	if tx.persistedTitle != "Verify rollback" || tx.persistedDetails != "protected details" {
		t.Fatalf("persisted action content mismatch")
	}
	if tx.outboxSubjectKind != recordplatform.OutboxSubjectKindAction || tx.outboxSubjectID != command.ActionID ||
		tx.activityKind != string(recordcollaboration.ActionActivityCreated) {
		t.Fatalf("typed facts: subject=%q/%q activity=%q", tx.outboxSubjectKind, tx.outboxSubjectID, tx.activityKind)
	}
}

func TestPostgresRecordActionRepositoryEnforcesTransitionsCASAndTxBoundAssignee(t *testing.T) {
	tests := []struct {
		name         string
		kind         recordcollaboration.ActionMutationKind
		from         recordcollaboration.ActionStatus
		persisted    uint64
		want         recordcollaboration.ActionStatus
		assignee     string
		memberDenied bool
		memberErr    error
		wantErr      error
	}{
		{name: "complete", kind: recordcollaboration.ActionMutationComplete, from: recordcollaboration.ActionStatusOpen, want: recordcollaboration.ActionStatusCompleted},
		{name: "cancel", kind: recordcollaboration.ActionMutationCancel, from: recordcollaboration.ActionStatusOpen, want: recordcollaboration.ActionStatusCancelled},
		{name: "reopen completed", kind: recordcollaboration.ActionMutationReopen, from: recordcollaboration.ActionStatusCompleted, want: recordcollaboration.ActionStatusOpen},
		{name: "reopen cancelled", kind: recordcollaboration.ActionMutationReopen, from: recordcollaboration.ActionStatusCancelled, want: recordcollaboration.ActionStatusOpen},
		{name: "invalid complete", kind: recordcollaboration.ActionMutationComplete, from: recordcollaboration.ActionStatusCompleted, wantErr: recordcollaboration.ErrInvalidActionStateTransition},
		{name: "stale cas", kind: recordcollaboration.ActionMutationComplete, from: recordcollaboration.ActionStatusOpen, persisted: 3, wantErr: recordcollaboration.ErrActionConflict},
		{name: "member allowed in caller transaction", kind: recordcollaboration.ActionMutationUpdate, from: recordcollaboration.ActionStatusOpen, assignee: "usr_111111111111111111111111", want: recordcollaboration.ActionStatusOpen},
		{name: "member cannot read parent", kind: recordcollaboration.ActionMutationUpdate, from: recordcollaboration.ActionStatusOpen, assignee: "usr_111111111111111111111111", memberDenied: true, wantErr: recordcollaboration.ErrMembershipDenied},
		{name: "member unavailable", kind: recordcollaboration.ActionMutationUpdate, from: recordcollaboration.ActionStatusOpen, assignee: "usr_111111111111111111111111", memberErr: recordcollaboration.ErrMembershipUnavailable, wantErr: recordcollaboration.ErrMembershipUnavailable},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := testStoreActionCommand(t, test.kind, 4, recordcollaboration.ActionFields{})
			if test.kind == recordcollaboration.ActionMutationUpdate {
				command.Fields = mustStoreActionFields(t, "Updated", "safe", test.assignee)
			}
			tx := newFakeRecordActionTx(command)
			tx.currentStatus = test.from
			tx.persistedVersion = test.persisted
			memberActor, _ := storeActionAuthorization(t)
			if test.assignee != "" {
				memberActor = collaborationActor(t, test.assignee, nil)
			}
			if test.memberDenied {
				memberActor = recordauth.ActorScope{}
			}
			member := &recordActionMembershipStub{actor: memberActor, err: test.memberErr}
			repository := newRecordActionTestRepository(tx, member)
			result, err := repository.CommitAction(context.Background(), command)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("CommitAction() error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr != nil {
				if !tx.rolledBack || tx.committed {
					t.Fatalf("failure transaction committed=%v rolledBack=%v", tx.committed, tx.rolledBack)
				}
				return
			}
			if result.Status != test.want || result.Version != 5 {
				t.Fatalf("result = %#v, want status %q version 5", result, test.want)
			}
			if test.assignee != "" && (member.targetCalls != 1 || member.tx != tx) {
				t.Fatalf("target membership calls=%d tx=%T, want exact caller tx", member.targetCalls, member.tx)
			}
		})
	}
}

func TestPostgresRecordActionRepositoryExactReplayIsReadOnlyAndContentFree(t *testing.T) {
	command := testStoreActionCommand(t, recordcollaboration.ActionMutationCreate, 0, mustStoreActionFields(t, "private title", "private details", ""))
	tx := newFakeRecordActionTx(command)
	tx.idempotencyRequestFingerprint = persistedActionFingerprint(t, command.Idempotency.RequestFingerprint)
	tx.idempotencyResultFingerprint = persistedActionFingerprint(t, command.ResultFingerprint)
	repository := newRecordActionTestRepository(tx, &recordActionMembershipStub{})

	result, err := repository.CommitAction(context.Background(), command)
	if err != nil {
		t.Fatalf("CommitAction() replay error = %v", err)
	}
	if !result.Replayed || result.ActionID != command.ActionID || result.RecordID != command.RecordID || result.Version != 1 ||
		result.Status != recordcollaboration.ActionStatusOpen || result.EventKind != recordcollaboration.ActionMutationCreate {
		t.Fatalf("replay result = %#v", result)
	}
	want := []string{"begin", "admission", "admission", "idempotency_lock", "reservation_lock", "epoch_init", "epoch_lock", "fence_lock", "reservation_recheck", "epoch_lock", "root_lock", "action_replay", "commit", "rollback_cleanup"}
	if !reflect.DeepEqual(tx.steps, want) {
		t.Fatalf("replay steps = %#v, want read-only %#v", tx.steps, want)
	}
	if tx.persistedTitle != "" || tx.persistedDetails != "" || tx.outboxSubjectID != "" || tx.activityKind != "" || tx.recordRootUpdates != 0 {
		t.Fatalf("replay performed a business write: %#v", tx)
	}
}

func TestPostgresRecordActionRepositoryIdempotencyConflictAndInProgressStopBeforeBusinessWrites(t *testing.T) {
	tests := []struct {
		name       string
		configure  func(*testing.T, *fakeRecordActionTx, *recordcollaboration.ActionCommand)
		wantErr    error
		wantPrefix []string
	}{
		{
			name: "same key different request fingerprint",
			configure: func(t *testing.T, tx *fakeRecordActionTx, command *recordcollaboration.ActionCommand) {
				tx.idempotencyRequestFingerprint = persistedActionFingerprint(t, command.Idempotency.RequestFingerprint)
				tx.idempotencyResultFingerprint = persistedActionFingerprint(t, command.ResultFingerprint)
				command.Idempotency.RequestFingerprint = mustStoreActionFingerprint(t, command.Idempotency.Key.OperationKind, 0x77)
			},
			wantErr:    recordplatform.ErrIdempotencyKeyReused,
			wantPrefix: []string{"begin", "admission", "admission", "idempotency_lock", "rollback_cleanup"},
		},
		{
			name: "same request mismatched result fingerprint",
			configure: func(t *testing.T, tx *fakeRecordActionTx, command *recordcollaboration.ActionCommand) {
				tx.idempotencyRequestFingerprint = persistedActionFingerprint(t, command.Idempotency.RequestFingerprint)
				tx.idempotencyResultFingerprint = persistedActionFingerprint(t, mustStoreActionFingerprint(t, command.Idempotency.Key.OperationKind, 0x66))
			},
			wantErr:    recordplatform.ErrIdempotencyConflictState,
			wantPrefix: []string{"begin", "admission", "admission", "idempotency_lock", "reservation_lock", "epoch_init", "epoch_lock", "fence_lock", "reservation_recheck", "epoch_lock", "root_lock", "rollback_cleanup"},
		},
		{
			name: "live owner in progress",
			configure: func(t *testing.T, tx *fakeRecordActionTx, command *recordcollaboration.ActionCommand) {
				tx.idempotencyRequestFingerprint = persistedActionFingerprint(t, command.Idempotency.RequestFingerprint)
				tx.idempotencyStatus = "in_progress"
			},
			wantErr:    recordplatform.ErrIdempotencyInProgress,
			wantPrefix: []string{"begin", "admission", "admission", "idempotency_lock", "rollback_cleanup"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := testStoreActionCommand(t, recordcollaboration.ActionMutationCreate, 0, mustStoreActionFields(t, "private title", "private details", ""))
			tx := newFakeRecordActionTx(command)
			test.configure(t, tx, &command)
			repository := newRecordActionTestRepository(tx, &recordActionMembershipStub{})
			result, err := repository.CommitAction(context.Background(), command)
			if !errors.Is(err, test.wantErr) || result != (recordcollaboration.ActionMutationResult{}) {
				t.Fatalf("CommitAction() result=%#v error=%v, want %v", result, err, test.wantErr)
			}
			if !reflect.DeepEqual(tx.steps, test.wantPrefix) || !tx.rolledBack || tx.committed || tx.persistedTitle != "" || tx.persistedDetails != "" {
				t.Fatalf("failure transaction = steps:%#v committed:%v rolledBack:%v", tx.steps, tx.committed, tx.rolledBack)
			}
		})
	}
}

func TestPostgresRecordActionRepositoryPersistedMaximumVersionConflictsBeforeWrites(t *testing.T) {
	command := testStoreActionCommand(t, recordcollaboration.ActionMutationComplete, recordcollaboration.MaxActionVersion-1, recordcollaboration.ActionFields{})
	tx := newFakeRecordActionTx(command)
	tx.persistedVersion = recordcollaboration.MaxActionVersion
	repository := newRecordActionTestRepository(tx, &recordActionMembershipStub{})

	result, err := repository.CommitAction(context.Background(), command)
	if !errors.Is(err, recordcollaboration.ErrActionConflict) || result != (recordcollaboration.ActionMutationResult{}) {
		t.Fatalf("CommitAction() result=%#v error=%v, want stable version conflict", result, err)
	}
	want := []string{
		"begin", "admission", "admission", "idempotency_lock", "idempotency_claim",
		"reservation_lock", "epoch_init", "epoch_lock", "fence_lock", "reservation_recheck",
		"epoch_lock", "root_lock", "action_lock", "rollback_cleanup",
	}
	if !reflect.DeepEqual(tx.steps, want) || tx.committed || !tx.rolledBack || tx.recordRootUpdates != 0 {
		t.Fatalf("maximum-version conflict transaction steps=%#v committed=%v rolledBack=%v rootUpdates=%d", tx.steps, tx.committed, tx.rolledBack, tx.recordRootUpdates)
	}
	for _, forbidden := range []string{"action_update", "action_event", "activity", "outbox", "idempotency_complete"} {
		if slices.Contains(tx.steps, forbidden) {
			t.Fatalf("maximum-version conflict reached forbidden step %q: %#v", forbidden, tx.steps)
		}
	}
}

func TestPostgresRecordActionRepositoryRollsBackEveryPostClaimFailure(t *testing.T) {
	cutPoints := []string{"reservation_lock", "epoch_init", "epoch_lock", "fence_lock", "reservation_recheck", "root_lock", "action_create", "action_event", "activity", "outbox", "idempotency_complete", "commit"}
	for _, cutPoint := range cutPoints {
		t.Run(cutPoint, func(t *testing.T) {
			command := testStoreActionCommand(t, recordcollaboration.ActionMutationCreate, 0, mustStoreActionFields(t, "Rollback", "safe", ""))
			tx := newFakeRecordActionTx(command)
			tx.failAt = cutPoint
			repository := newRecordActionTestRepository(tx, &recordActionMembershipStub{})
			result, err := repository.CommitAction(context.Background(), command)
			if !errors.Is(err, errRecordActionCutPoint) {
				t.Fatalf("CommitAction() error = %v, want cut point", err)
			}
			if result != (recordcollaboration.ActionMutationResult{}) || !tx.rolledBack {
				t.Fatalf("result=%#v committed=%v rolledBack=%v", result, tx.committed, tx.rolledBack)
			}
		})
	}
}

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

type recordActionAuthorizationStub struct {
	command recordcollaboration.ActionCommand
}

func (stub *recordActionAuthorizationStub) resolveCurrentAuthorizationInTransaction(
	_ context.Context,
	_ pgx.Tx,
	_ recordauth.ActorScope,
	_ string,
) (records.CurrentRecordAuthorization, error) {
	return records.CurrentRecordAuthorization{
		RecordID: stub.command.RecordID, CurrentRevisionID: stub.command.CurrentRevisionID,
		LockVersion: stub.command.RecordLockVersion, AuthorizationEpoch: stub.command.AuthorizationEpoch,
		Lifecycle: records.LifecycleActive, Evidence: stub.command.AuthorizationEvidence,
	}, nil
}

func newRecordActionTestRepository(tx *fakeRecordActionTx, members CollaborationMembershipReader) *PostgresRecordActionRepository {
	if stub, ok := members.(*recordActionMembershipStub); ok {
		stub.caller = tx.command.Actor.Clone()
	}
	return &PostgresRecordActionRepository{
		platform: &PostgresRecordPlatformRepository{
			beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
				tx.steps = append(tx.steps, "begin")
				return tx, nil
			},
			gate: AdmissionGateFunc(func(context.Context, pgx.Tx) error { tx.steps = append(tx.steps, "admission"); return nil }),
		},
		members: members, authorization: &recordActionAuthorizationStub{command: tx.command},
	}
}

type fakeRecordActionTx struct {
	pgx.Tx
	command                       recordcollaboration.ActionCommand
	now                           time.Time
	steps                         []string
	failAt                        string
	failed                        bool
	rolledBack                    bool
	committed                     bool
	currentStatus                 recordcollaboration.ActionStatus
	persistedVersion              uint64
	idempotencyRequestFingerprint []byte
	idempotencyResultFingerprint  []byte
	idempotencyStatus             string
	persistedTitle                string
	persistedDetails              string
	activityKind                  string
	actionEventAssignee           string
	outboxSubjectKind             string
	outboxSubjectID               string
	outboxEvents                  []recordplatform.OutboxEvent
	recordRootUpdates             int
}

func newFakeRecordActionTx(command recordcollaboration.ActionCommand) *fakeRecordActionTx {
	return &fakeRecordActionTx{command: command, now: time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC), currentStatus: recordcollaboration.ActionStatusOpen}
}

func (tx *fakeRecordActionTx) Commit(context.Context) error {
	tx.steps = append(tx.steps, "commit")
	if tx.shouldFail("commit") {
		return errRecordActionCutPoint
	}
	tx.committed = true
	return nil
}

func (tx *fakeRecordActionTx) Rollback(context.Context) error {
	tx.steps = append(tx.steps, "rollback_cleanup")
	tx.rolledBack = true
	return nil
}

func (tx *fakeRecordActionTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	step := recordActionSQLStep(sql)
	tx.steps = append(tx.steps, step)
	if tx.shouldFail(step) {
		return pgconn.CommandTag{}, errRecordActionCutPoint
	}
	if strings.Contains(strings.ToLower(sql), "update public.records") {
		tx.recordRootUpdates++
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (tx *fakeRecordActionTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	step := recordActionSQLStep(sql)
	tx.steps = append(tx.steps, step)
	if tx.shouldFail(step) {
		return fakeRecordRevisionRow{err: errRecordActionCutPoint}
	}
	switch step {
	case "idempotency_lock":
		if len(tx.idempotencyRequestFingerprint) == 0 {
			return fakeRecordRevisionRow{err: pgx.ErrNoRows}
		}
		status := tx.idempotencyStatus
		if status == "" {
			status = "completed"
		}
		var ownerID string
		var ownerGeneration int64
		var ownerExpiresAt any
		if status == "in_progress" {
			ownerID = "records_actions_api"
			ownerGeneration = 1
			expiresAt := tx.now.Add(time.Minute)
			ownerExpiresAt = &expiresAt
		}
		return fakeRecordRevisionRow{values: []any{tx.idempotencyRequestFingerprint, tx.idempotencyResultFingerprint, status, ownerID, ownerGeneration, ownerExpiresAt, tx.now.Add(24 * time.Hour), tx.now}}
	case "reservation_lock", "fence_lock", "reservation_recheck":
		return fakeRecordRevisionRow{err: pgx.ErrNoRows}
	case "idempotency_claim":
		return fakeRecordRevisionRow{values: []any{"records_actions_api", int64(1), tx.now.Add(time.Minute)}}
	case "epoch_lock":
		return fakeRecordRevisionRow{values: []any{int64(0)}}
	case "root_lock":
		revisionNo := int64(3)
		revisionAt := tx.now.Add(-time.Hour)
		return fakeRecordRevisionRow{values: []any{tx.command.RecordID, "default", "active", &tx.command.CurrentRevisionID, int64(tx.command.RecordLockVersion), int64(tx.command.AuthorizationEpoch), make([]byte, 32), &revisionNo, &revisionAt}}
	case "action_create":
		tx.persistedTitle = fmt.Sprint(args[4])
		tx.persistedDetails = fmt.Sprint(args[5])
		return fakeRecordRevisionRow{values: []any{tx.now, tx.now}}
	case "action_lock":
		var assignee *string
		var due *time.Time
		var subject *string
		version := tx.persistedVersion
		if version == 0 {
			version = tx.command.ExpectedVersion
		}
		return fakeRecordRevisionRow{values: []any{tx.command.RecordID, int64(version), string(tx.currentStatus), "Old", "old", assignee, due, subject, tx.now.Add(-time.Hour), tx.now.Add(-time.Minute)}}
	case "action_replay":
		version := tx.command.ExpectedVersion + 1
		if tx.command.Kind == recordcollaboration.ActionMutationCreate {
			version = 1
		}
		return fakeRecordRevisionRow{values: []any{string(tx.command.Kind), string(actionStatusForStoreTest(tx.command.Kind, tx.currentStatus)), tx.now.Add(time.Duration(version) * time.Second)}}
	case "action_update":
		return fakeRecordRevisionRow{values: []any{tx.now}}
	case "action_event":
		tx.actionEventAssignee = fmt.Sprint(args[9])
		return fakeRecordRevisionRow{values: []any{tx.now}}
	case "activity":
		tx.activityKind = fmt.Sprint(args[4])
		return fakeRecordRevisionRow{values: []any{tx.now}}
	case "outbox":
		tx.outboxSubjectKind, tx.outboxSubjectID = fmt.Sprint(args[2]), fmt.Sprint(args[3])
		tx.outboxEvents = append(tx.outboxEvents, recordplatform.OutboxEvent{
			ProjectID: fmt.Sprint(args[0]), EventKind: fmt.Sprint(args[1]),
			SubjectKind: fmt.Sprint(args[2]), SubjectID: fmt.Sprint(args[3]),
			SourceVersion: args[4].(uint64), AuthorizationEpoch: args[5].(uint64), RecordFenceEpoch: args[6].(uint64),
		})
		return fakeRecordRevisionRow{values: []any{int64(51)}}
	default:
		return fakeRecordRevisionRow{err: fmt.Errorf("unexpected row SQL: %s", step)}
	}
}

func (tx *fakeRecordActionTx) shouldFail(step string) bool {
	if tx.failed || tx.failAt != step {
		return false
	}
	tx.failed = true
	return true
}

func recordActionSQLStep(sql string) string {
	compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
	switch {
	case strings.Contains(compact, "select request_fingerprint"):
		return "idempotency_lock"
	case strings.Contains(compact, "insert into public.record_idempotency_keys"):
		return "idempotency_claim"
	case strings.Contains(compact, "from public.deletion_reservations") && strings.Contains(compact, "for update"):
		return "reservation_lock"
	case strings.Contains(compact, "insert into public.content_delivery_epochs"):
		return "epoch_init"
	case strings.Contains(compact, "from public.content_delivery_epochs"):
		return "epoch_lock"
	case strings.Contains(compact, "from public.deletion_fence_leases"):
		return "fence_lock"
	case strings.Contains(compact, "from public.deletion_reservations"):
		return "reservation_recheck"
	case strings.Contains(compact, "from public.records") && strings.Contains(compact, "for update"):
		return "root_lock"
	case strings.Contains(compact, "insert into public.record_actions"):
		return "action_create"
	case strings.Contains(compact, "from public.record_actions") && strings.Contains(compact, "for update"):
		return "action_lock"
	case strings.Contains(compact, "update public.record_actions"):
		return "action_update"
	case strings.Contains(compact, "insert into public.record_action_events"):
		return "action_event"
	case strings.Contains(compact, "from public.record_action_events"):
		return "action_replay"
	case strings.Contains(compact, "insert into public.record_domain_activities"):
		return "activity"
	case strings.Contains(compact, "insert into public.record_followers"):
		return "follower_source"
	case strings.Contains(compact, "insert into public.record_outbox"):
		return "outbox"
	case strings.Contains(compact, "update public.record_idempotency_keys"):
		return "idempotency_complete"
	default:
		return "unexpected_sql"
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

func persistedActionFingerprint(t *testing.T, fingerprint recordplatform.RequestFingerprintV1) []byte {
	t.Helper()
	persisted, err := fingerprint.PersistedBytes()
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte(nil), persisted[:]...)
}

func actionStatusForStoreTest(kind recordcollaboration.ActionMutationKind, current recordcollaboration.ActionStatus) recordcollaboration.ActionStatus {
	switch kind {
	case recordcollaboration.ActionMutationCreate:
		return recordcollaboration.ActionStatusOpen
	case recordcollaboration.ActionMutationComplete:
		return recordcollaboration.ActionStatusCompleted
	case recordcollaboration.ActionMutationCancel:
		return recordcollaboration.ActionStatusCancelled
	case recordcollaboration.ActionMutationReopen:
		return recordcollaboration.ActionStatusOpen
	default:
		return current
	}
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

var errRecordActionCutPoint = errors.New("record action cut point")

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

func TestPostgresRecordActionRepositoryPersistsRawSubjectAndExactTypedSourceVersion(t *testing.T) {
	actor, _ := storeActionAuthorization(t)
	command := testStoreActionCommand(t, recordcollaboration.ActionMutationCreate, 0, mustStoreActionFields(t, "Assign", "safe", actor.UserID))
	tx := newFakeRecordActionTx(command)
	repository := newRecordActionTestRepository(tx, &recordActionMembershipStub{actor: actor})

	result, err := repository.CommitAction(context.Background(), command)
	if err != nil {
		t.Fatalf("CommitAction() error = %v", err)
	}
	if result.Version != 1 {
		t.Fatalf("CommitAction() version = %d, want 1", result.Version)
	}
	want := []recordplatform.OutboxEvent{
		{ProjectID: "default", EventKind: recordplatform.OutboxEventKindRecordActionCreated, SubjectKind: "action", SubjectID: command.ActionID, AuthorizationEpoch: command.AuthorizationEpoch},
		{ProjectID: "default", EventKind: recordplatform.OutboxEventKindRecordActionAssigned, SubjectKind: "action", SubjectID: command.ActionID, SourceVersion: 1, AuthorizationEpoch: command.AuthorizationEpoch, RecordFenceEpoch: 0},
	}
	if !reflect.DeepEqual(tx.outboxEvents, want) {
		t.Fatalf("outbox events = %#v, want %#v", tx.outboxEvents, want)
	}
	if tx.actionEventAssignee != actor.UserID {
		t.Fatalf("action event assignee = %q, want %q", tx.actionEventAssignee, actor.UserID)
	}
}
