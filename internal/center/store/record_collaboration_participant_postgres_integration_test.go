package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

func TestPostgresIntegrationCollaborationMembershipReaderExactDefaultProjectMatrix(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	if _, err := fixture.db.Exec(ctx, `
		update public.users
		set role = case user_id
			when 'usr_bbbbbbbbbbbbbbbbbbbbbbbb' then 'viewer'
			when 'usr_cccccccccccccccccccccccc' then 'future_role'
			else role
		end`); err != nil {
		t.Fatalf("seed non-admin collaboration roles: %v", err)
	}
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "collaboration-membership-matrix", 1)
	tx, err := runtimePool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin collaboration membership transaction: %v", err)
	}
	reader := NewPostgresCollaborationMembershipReader()
	actor, err := reader.ReadMemberActor(ctx, tx, recordauth.ProjectIDDefault, "usr_aaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatalf("ReadMemberActor(admin) error = %v", err)
	}
	if actor.UserID != "usr_aaaaaaaaaaaaaaaaaaaaaaaa" || actor.Role != recordauth.RoleProjectAdmin {
		t.Fatalf("ReadMemberActor(admin) = %#v", actor)
	}
	for _, candidate := range []struct {
		name      string
		projectID recordauth.ProjectID
		userID    string
	}{
		{name: "missing", projectID: recordauth.ProjectIDDefault, userID: "usr_eeeeeeeeeeeeeeeeeeeeeeee"},
		{name: "other role", projectID: recordauth.ProjectIDDefault, userID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb"},
		{name: "unknown role", projectID: recordauth.ProjectIDDefault, userID: "usr_cccccccccccccccccccccccc"},
		{name: "other project", projectID: "other", userID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa"},
		{name: "malformed id", projectID: recordauth.ProjectIDDefault, userID: "usr_invalid"},
	} {
		if _, err := reader.ReadMemberActor(ctx, tx, candidate.projectID, candidate.userID); !errors.Is(err, recordcollaboration.ErrMembershipDenied) {
			t.Fatalf("ReadMemberActor(%s) error = %v, want ErrMembershipDenied", candidate.name, err)
		}
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback collaboration membership transaction: %v", err)
	}
	if _, err := reader.ReadMemberActor(ctx, tx, recordauth.ProjectIDDefault, "usr_aaaaaaaaaaaaaaaaaaaaaaaa"); !errors.Is(err, recordcollaboration.ErrMembershipUnavailable) {
		t.Fatalf("ReadMemberActor(closed transaction) error = %v, want ErrMembershipUnavailable", err)
	}
}

func TestPostgresIntegrationCollaborationRevisionParticipantKeepsSubmicrosecondFollowUpStable(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	repository := newRecordsPostgresRepository(
		t,
		fixture.openDirectRuntimePool(t, ctx, "collaboration-follow-up-precision", 2),
		NewCollaborationRevisionParticipant(NewPostgresCollaborationMembershipReader()),
	)

	followUp := time.Date(2026, time.August, 19, 8, 0, 0, 123456789, time.FixedZone("UTC+8", 8*60*60))
	firstInput := collaborationRevisionInput(t, collaborationRevisionInputValues{
		ownerID:    "usr_bbbbbbbbbbbbbbbbbbbbbbbb",
		followUpAt: &followUp,
	})
	first, err := repository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordCreate, "rec_pgcollabprecision", "", 0, 0,
		firstInput, "collaboration-follow-up-precision-create",
	))
	if err != nil {
		t.Fatalf("CommitRevision(create precision fixture) error = %v", err)
	}
	secondInput := collaborationRevisionInput(t, collaborationRevisionInputValues{
		ownerID:    "usr_dddddddddddddddddddddddd",
		followUpAt: &followUp,
	})
	second, err := repository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordUpdate, first.RecordID, first.RevisionID,
		first.LockVersion, first.AuthorizationEpoch, secondInput,
		"collaboration-follow-up-precision-revise",
	))
	if err != nil {
		t.Fatalf("CommitRevision(revise precision fixture) error = %v", err)
	}
	assertPostgresCollaborationRevisionFacts(t, ctx, fixture, second, []string{
		"record_owner_changed",
		"record_revised",
	})
	var outboxKinds []string
	if err := fixture.db.QueryRow(ctx, `
		select array_agg(event_kind order by outbox_row_id)
		from public.record_outbox
		where subject_kind = 'record' and subject_id = $1 and authorization_epoch = $2`,
		second.RecordID, int64(second.AuthorizationEpoch),
	).Scan(&outboxKinds); err != nil {
		t.Fatalf("read precision revision outbox facts: %v", err)
	}
	if want := []string{
		recordplatform.OutboxEventKindRecordOwnerChanged,
		recordplatform.OutboxEventKindRecordUpdated,
	}; !reflect.DeepEqual(outboxKinds, want) {
		t.Fatalf("precision revision outbox facts = %#v, want %#v", outboxKinds, want)
	}
}

func TestPostgresIntegrationCollaborationRevisionParticipantCreateAndRestoreFacts(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "collaboration-revision-integration", 4)
	repository := newRecordsPostgresRepository(
		t,
		runtimePool,
		NewCollaborationRevisionParticipant(NewPostgresCollaborationMembershipReader()),
	)

	firstFollowUp := time.Date(2026, time.August, 19, 8, 0, 0, 0, time.UTC)
	firstInput := collaborationRevisionInput(t, collaborationRevisionInputValues{
		ownerID:        "usr_bbbbbbbbbbbbbbbbbbbbbbbb",
		participantIDs: []string{"usr_cccccccccccccccccccccccc"},
		followUpAt:     &firstFollowUp,
	})
	first, err := repository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordCreate,
		"rec_pgcollaboration",
		"",
		0,
		0,
		firstInput,
		"collaboration-create",
	))
	if err != nil {
		t.Fatalf("CommitRevision(create collaboration) error = %v", err)
	}
	assertPostgresCollaborationFollowers(t, ctx, fixture, first.RecordID, map[string][3]bool{
		"usr_aaaaaaaaaaaaaaaaaaaaaaaa": {true, false, false},
		"usr_bbbbbbbbbbbbbbbbbbbbbbbb": {false, true, false},
		"usr_cccccccccccccccccccccccc": {false, false, true},
	})
	assertPostgresCollaborationRevisionFacts(t, ctx, fixture, first, []string{
		"record_created",
		"record_follow_up_changed",
		"record_owner_changed",
		"record_participant_changed",
	})
	assertPostgresCollaborationOutboxOrder(t, ctx, fixture, first.RecordID, []string{
		recordplatform.OutboxEventKindRecordOwnerChanged,
		recordplatform.OutboxEventKindRecordParticipantChanged,
		recordplatform.OutboxEventKindRecordCreated,
	})

	secondFollowUp := firstFollowUp.Add(2 * time.Hour)
	secondInput := collaborationRevisionInput(t, collaborationRevisionInputValues{
		ownerID:        "usr_dddddddddddddddddddddddd",
		participantIDs: []string{"usr_cccccccccccccccccccccccc"},
		followUpAt:     &secondFollowUp,
	})
	second, err := repository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordUpdate,
		first.RecordID,
		first.RevisionID,
		first.LockVersion,
		first.AuthorizationEpoch,
		secondInput,
		"collaboration-revise",
	))
	if err != nil {
		t.Fatalf("CommitRevision(revise collaboration) error = %v", err)
	}

	restoreCommand := recordsPostgresRevisionCommand(
		t,
		recordplatform.OperationKindRecordUpdate,
		second.RecordID,
		second.RevisionID,
		second.LockVersion,
		second.AuthorizationEpoch,
		firstInput,
		"collaboration-restore",
	)
	restoreCommand.ActivityKind = records.DomainActivityRecordRestored
	restored, err := repository.CommitRevision(ctx, restoreCommand)
	if err != nil {
		t.Fatalf("CommitRevision(restore collaboration) error = %v", err)
	}
	assertPostgresCollaborationFollowers(t, ctx, fixture, restored.RecordID, map[string][3]bool{
		"usr_aaaaaaaaaaaaaaaaaaaaaaaa": {true, false, false},
		"usr_bbbbbbbbbbbbbbbbbbbbbbbb": {false, true, false},
		"usr_cccccccccccccccccccccccc": {false, false, true},
	})
	assertPostgresCollaborationRevisionFacts(t, ctx, fixture, restored, []string{
		"record_follow_up_changed",
		"record_owner_changed",
		"record_restored",
	})

	var staleOwnerFollowers int
	if err := fixture.db.QueryRow(ctx, `
		select count(*)::int
		from public.record_followers
		where record_id = $1 and user_id = 'usr_dddddddddddddddddddddddd'`, restored.RecordID).Scan(&staleOwnerFollowers); err != nil {
		t.Fatalf("count stale restored owner followers: %v", err)
	}
	if staleOwnerFollowers != 0 {
		t.Fatalf("stale restored owner followers = %d, want 0", staleOwnerFollowers)
	}
}

func TestPostgresIntegrationCollaborationRevisionParticipantFailureMatrixRollsBackWholeRevision(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "collaboration-revision-rollback", 4)

	projectInput := collaborationRevisionInput(t, collaborationRevisionInputValues{
		ownerID:        "usr_bbbbbbbbbbbbbbbbbbbbbbbb",
		participantIDs: []string{"usr_cccccccccccccccccccccccc"},
	})
	tests := []struct {
		name         string
		recordID     string
		input        records.CompleteRevisionInput
		participants func() []records.RevisionParticipant
		cleanup      func()
	}{
		{
			name:     "membership",
			recordID: "rec_pgcollabmembership",
			input: collaborationRevisionInput(t, collaborationRevisionInputValues{
				ownerID: "usr_eeeeeeeeeeeeeeeeeeeeeeee",
			}),
		},
		{
			name:     "visibility",
			recordID: "rec_pgcollabvisibility",
			input:    collaborationRestrictedRevisionInput(t, recordauth.SourceStateLive),
		},
		{
			name:     "source floor",
			recordID: "rec_pgcollabsourcefloor",
			input:    collaborationRestrictedRevisionInput(t, recordauth.SourceStateTombstoned),
		},
		{
			name:     "fence",
			recordID: "rec_pgcollabfence",
			input:    projectInput,
			participants: func() []records.RevisionParticipant {
				return []records.RevisionParticipant{
					&storeRevisionParticipantStub{name: "aaa_reserve_collaboration_fence", apply: func(ctx context.Context, tx pgx.Tx, committed records.RevisionCommitted) error {
						_, err := tx.Exec(ctx, `
							insert into public.deletion_fence_leases (
								project_id, object_kind, object_id, owner_id, owner_generation, expires_at
							) values ('default', 'record', $1, 'collaboration_test', 1,
								transaction_timestamp() + interval '1 minute')`, committed.Result.RecordID)
						return err
					}},
				}
			},
		},
		{
			name:     "follower",
			recordID: "rec_pgcollabfollower",
			input:    projectInput,
		},
		{
			name:     "activity",
			recordID: "rec_pgcollabactivity",
			input:    projectInput,
		},
		{
			name:     "outbox",
			recordID: "rec_pgcollaboutbox",
			input:    projectInput,
		},
	}

	for index := range tests {
		tt := &tests[index]
		switch tt.name {
		case "follower", "activity", "outbox":
			cleanup := installCollaborationRevisionFailureTrigger(t, ctx, fixture, tt.name)
			tt.cleanup = cleanup
		}
		participants := []records.RevisionParticipant{
			NewCollaborationRevisionParticipant(NewPostgresCollaborationMembershipReader()),
		}
		if tt.participants != nil {
			participants = append(participants, tt.participants()...)
		}
		repository := newRecordsPostgresRepository(t, runtimePool, participants...)
		key := fmt.Sprintf("collaboration-rollback-%d", index)
		command := recordsPostgresRevisionCommand(
			t,
			recordplatform.OperationKindRecordCreate,
			tt.recordID,
			"",
			0,
			0,
			tt.input,
			key,
		)
		if result, err := repository.CommitRevision(ctx, command); err == nil {
			t.Fatalf("CommitRevision(%s) = %#v, want failure", tt.name, result)
		}
		if tt.cleanup != nil {
			tt.cleanup()
		}
		assertPostgresCollaborationRevisionRolledBack(t, ctx, fixture, tt.recordID, key)
	}
}

func TestPostgresIntegrationCollaborationRevisionParticipantSQLCutPointFailuresRollBackCreateReviseAndRestore(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "collaboration-cut-points", 8)

	tests := []struct {
		name    string
		flow    collaborationCutPointFlow
		cut     collaborationSQLCutPoint
		trigger string
	}{
		{name: "previous revision fact scan", flow: collaborationCutPointRevise, cut: collaborationSQLCutPoint{method: collaborationCutQueryRow, contains: []string{"from public.record_revisions"}, occurrence: 1}},
		{name: "previous participant query", flow: collaborationCutPointRevise, cut: collaborationSQLCutPoint{method: collaborationCutQuery, contains: []string{"from public.record_revision_participants"}, occurrence: 1}},
		{name: "previous participant scan", flow: collaborationCutPointRestore, cut: collaborationSQLCutPoint{method: collaborationCutRowsScan, contains: []string{"from public.record_revision_participants"}, occurrence: 1}},
		{name: "previous participant terminal rows error", flow: collaborationCutPointRestore, cut: collaborationSQLCutPoint{method: collaborationCutRowsErr, contains: []string{"from public.record_revision_participants"}, occurrence: 1}},
		{name: "current reservation read", flow: collaborationCutPointCreate, cut: collaborationSQLCutPoint{method: collaborationCutQueryRow, contains: []string{"from public.deletion_reservations", "state in ('previewed', 'fenced', 'committed')"}, occurrence: 1}},
		{name: "current fence epoch initialize", flow: collaborationCutPointCreate, cut: collaborationSQLCutPoint{method: collaborationCutExec, contains: []string{"insert into public.content_delivery_epochs"}, occurrence: 1}},
		{name: "current fence epoch read", flow: collaborationCutPointRevise, cut: collaborationSQLCutPoint{method: collaborationCutQueryRow, contains: []string{"from public.content_delivery_epochs"}, occurrence: 1}},
		{name: "current fence lease read", flow: collaborationCutPointRestore, cut: collaborationSQLCutPoint{method: collaborationCutQueryRow, contains: []string{"from public.deletion_fence_leases"}, occurrence: 1}},
		{name: "current reservation recheck", flow: collaborationCutPointCreate, cut: collaborationSQLCutPoint{method: collaborationCutQueryRow, contains: []string{"from public.deletion_reservations", "state in ('fenced', 'committed')"}, occurrence: 1}},
		{name: "fence binding epoch read", flow: collaborationCutPointRevise, cut: collaborationSQLCutPoint{method: collaborationCutQueryRow, contains: []string{"from public.content_delivery_epochs"}, occurrence: 2}},
		{name: "first follower upsert", flow: collaborationCutPointCreate, cut: collaborationSQLCutPoint{method: collaborationCutExec, contains: []string{"insert into public.record_followers"}, occurrence: 1}},
		{name: "later follower upsert", flow: collaborationCutPointRevise, cut: collaborationSQLCutPoint{method: collaborationCutExec, contains: []string{"insert into public.record_followers"}, occurrence: 3}},
		{name: "stale follower delete", flow: collaborationCutPointRestore, cut: collaborationSQLCutPoint{method: collaborationCutExec, contains: []string{"delete from public.record_followers"}, occurrence: 1}},
		{name: "stale follower update", flow: collaborationCutPointRevise, cut: collaborationSQLCutPoint{method: collaborationCutExec, contains: []string{"update public.record_followers"}, occurrence: 1}},
		{name: "first activity insert", flow: collaborationCutPointCreate, cut: collaborationSQLCutPoint{method: collaborationCutQueryRow, contains: []string{"insert into public.record_domain_activities"}, occurrence: 1}},
		{name: "second activity insert", flow: collaborationCutPointRevise, cut: collaborationSQLCutPoint{method: collaborationCutQueryRow, contains: []string{"insert into public.record_domain_activities"}, occurrence: 2}},
		{name: "third activity insert", flow: collaborationCutPointRestore, cut: collaborationSQLCutPoint{method: collaborationCutQueryRow, contains: []string{"insert into public.record_domain_activities"}, occurrence: 3}},
		{name: "first collaboration outbox insert", flow: collaborationCutPointCreate, trigger: "outbox_owner"},
		{name: "later collaboration outbox insert", flow: collaborationCutPointRestore, trigger: "outbox_participant"},
	}

	for index := range tests {
		tt := &tests[index]
		t.Run(tt.name, func(t *testing.T) {
			recordID := fmt.Sprintf("rec_pgcollabcut%02d", index)
			idempotencyKey := fmt.Sprintf("collaboration-cut-point-%02d", index)
			before, command := preparePostgresCollaborationCutPointFlow(
				t, ctx, fixture, runtimePool, recordID, idempotencyKey, tt.flow,
			)
			if tt.trigger != "" {
				cleanup := installCollaborationRevisionFailureTrigger(t, ctx, fixture, tt.trigger)
				defer cleanup()
			}
			participant := records.RevisionParticipant(
				NewCollaborationRevisionParticipant(NewPostgresCollaborationMembershipReader()),
			)
			if tt.cut.method != "" {
				participant = newCollaborationSQLCutPointParticipant(&tt.cut)
			}
			repository := newRecordsPostgresRepository(t, runtimePool, participant)
			if result, err := repository.CommitRevision(ctx, command); err == nil {
				t.Fatalf("CommitRevision() = %#v, want %s failure", result, tt.name)
			} else if tt.cut.method != "" && !errors.Is(err, errCollaborationSQLCutPoint) {
				t.Fatalf("CommitRevision() error = %v, want injected cut-point error", err)
			} else if tt.trigger != "" && !strings.Contains(err.Error(), "collaboration stage failed") {
				t.Fatalf("CommitRevision() error = %v, want trigger failure", err)
			}
			if tt.cut.method != "" && !tt.cut.triggered {
				t.Fatalf("cut point %#v was not reached", tt.cut)
			}
			if tt.flow == collaborationCutPointCreate {
				assertPostgresCollaborationRevisionRolledBack(t, ctx, fixture, recordID, idempotencyKey)
				return
			}
			after := snapshotPostgresCollaborationRecordState(t, ctx, fixture, recordID)
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("record state changed across failed %s:\nafter=%#v\nbefore=%#v", tt.flow, after, before)
			}
			assertPostgresCollaborationIdempotencyAbsent(t, ctx, fixture, idempotencyKey)
		})
	}
}

func TestPostgresIntegrationCollaborationRevisionParticipantConcurrentSameBaseKeepsWinnerFollowers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	seedRepository := newRecordsPostgresRepository(
		t,
		fixture.openDirectRuntimePool(t, ctx, "collaboration-race-seed", 1),
		NewCollaborationRevisionParticipant(NewPostgresCollaborationMembershipReader()),
	)
	seedInput := collaborationRevisionInput(t, collaborationRevisionInputValues{ownerID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb"})
	seed, err := seedRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordCreate, "rec_pgcollabrace", "", 0, 0, seedInput, "collaboration-race-seed",
	))
	if err != nil {
		t.Fatalf("CommitRevision(seed) error = %v", err)
	}

	firstInput := collaborationRevisionInput(t, collaborationRevisionInputValues{ownerID: "usr_cccccccccccccccccccccccc"})
	secondInput := collaborationRevisionInput(t, collaborationRevisionInputValues{ownerID: "usr_dddddddddddddddddddddddd"})
	firstPool := fixture.openDirectRuntimePool(t, ctx, "collaboration-race-first", 1)
	secondPool := fixture.openDirectRuntimePool(t, ctx, "collaboration-race-second", 1)
	firstPID := postgresCollaborationBackendPID(t, ctx, firstPool)
	secondPID := postgresCollaborationBackendPID(t, ctx, secondPool)

	firstReachedHold := make(chan struct{})
	releaseFirst := make(chan struct{})
	hold := &postgresCollaborationBlockingParticipant{
		reached: firstReachedHold,
		release: releaseFirst,
	}
	firstRepository := newRecordsPostgresRepository(
		t,
		firstPool,
		NewCollaborationRevisionParticipant(NewPostgresCollaborationMembershipReader()),
		hold,
	)
	secondRepository := newRecordsPostgresRepository(
		t,
		secondPool,
		NewCollaborationRevisionParticipant(NewPostgresCollaborationMembershipReader()),
	)
	firstCommand := recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordUpdate, seed.RecordID, seed.RevisionID,
		seed.LockVersion, seed.AuthorizationEpoch, firstInput, "collaboration-race-first",
	)
	secondCommand := recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordUpdate, seed.RecordID, seed.RevisionID,
		seed.LockVersion, seed.AuthorizationEpoch, secondInput, "collaboration-race-second",
	)
	firstResult := make(chan error, 1)
	go func() {
		_, err := firstRepository.CommitRevision(ctx, firstCommand)
		firstResult <- err
	}()
	select {
	case <-firstReachedHold:
	case <-ctx.Done():
		t.Fatalf("first revision did not reach transaction hold: %v", ctx.Err())
	}

	secondStarted := make(chan struct{})
	secondResult := make(chan error, 1)
	go func() {
		close(secondStarted)
		_, err := secondRepository.CommitRevision(ctx, secondCommand)
		secondResult <- err
	}()
	select {
	case <-secondStarted:
	case <-ctx.Done():
		t.Fatalf("second revision did not start: %v", ctx.Err())
	}
	blockers := waitForPostgresCollaborationBlocker(t, ctx, fixture, secondPID, firstPID)
	if !slices.Contains(blockers, firstPID) {
		t.Fatalf("second backend blockers = %#v, want first backend %d", blockers, firstPID)
	}
	select {
	case err := <-secondResult:
		t.Fatalf("second revision completed before first release: %v", err)
	default:
	}
	close(releaseFirst)
	select {
	case err := <-firstResult:
		if err != nil {
			t.Fatalf("first revision error = %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("first revision did not finish after release: %v", ctx.Err())
	}
	select {
	case err := <-secondResult:
		if !errors.Is(err, records.ErrRecordRevisionConflict) {
			t.Fatalf("blocked second revision error = %v, want ErrRecordRevisionConflict", err)
		}
	case <-ctx.Done():
		t.Fatalf("second revision did not finish after first commit: %v", ctx.Err())
	}
	var ownerFollowers []string
	rows, err := fixture.db.Query(ctx, `
		select user_id
		from public.record_followers
		where record_id = $1 and follows_owner
		order by user_id`, seed.RecordID)
	if err != nil {
		t.Fatalf("query concurrent owner followers: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			t.Fatalf("scan concurrent owner follower: %v", err)
		}
		ownerFollowers = append(ownerFollowers, userID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate concurrent owner followers: %v", err)
	}
	if !reflect.DeepEqual(ownerFollowers, []string{firstInput.OwnerID()}) {
		t.Fatalf("concurrent owner followers = %#v, want first winner %q", ownerFollowers, firstInput.OwnerID())
	}
}

type postgresCollaborationBlockingParticipant struct {
	reached     chan struct{}
	release     chan struct{}
	reachedOnce sync.Once
}

func (*postgresCollaborationBlockingParticipant) Name() string { return "zzz_collaboration_test_hold" }

func (participant *postgresCollaborationBlockingParticipant) ApplyRevision(
	ctx context.Context,
	_ pgx.Tx,
	_ records.RevisionCommitted,
) error {
	participant.reachedOnce.Do(func() { close(participant.reached) })
	select {
	case <-participant.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func postgresCollaborationBackendPID(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int32 {
	t.Helper()
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire collaboration backend: %v", err)
	}
	defer connection.Release()
	var pid int32
	if err := connection.QueryRow(ctx, `select pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatalf("read collaboration backend pid: %v", err)
	}
	return pid
}

func waitForPostgresCollaborationBlocker(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	waitingPID int32,
	wantBlocker int32,
) []int32 {
	t.Helper()
	for {
		var blockers []int32
		if err := fixture.db.QueryRow(ctx, `select pg_blocking_pids($1)`, waitingPID).Scan(&blockers); err != nil {
			t.Fatalf("read collaboration backend blockers: %v", err)
		}
		if slices.Contains(blockers, wantBlocker) {
			return blockers
		}
		select {
		case <-ctx.Done():
			t.Fatalf("backend %d did not block on backend %d: %v", waitingPID, wantBlocker, ctx.Err())
		default:
		}
	}
}

type collaborationCutPointFlow string

const (
	collaborationCutPointCreate  collaborationCutPointFlow = "create"
	collaborationCutPointRevise  collaborationCutPointFlow = "revise"
	collaborationCutPointRestore collaborationCutPointFlow = "restore"
)

const (
	collaborationCutQueryRow = "query_row"
	collaborationCutQuery    = "query"
	collaborationCutRowsScan = "rows_scan"
	collaborationCutRowsErr  = "rows_err"
	collaborationCutExec     = "exec"
)

var errCollaborationSQLCutPoint = errors.New("collaboration SQL cut point failed")

type collaborationSQLCutPoint struct {
	method     string
	contains   []string
	occurrence int
	seen       int
	triggered  bool
}

func (cut *collaborationSQLCutPoint) matches(method, sql string) bool {
	if cut == nil || cut.method != method {
		return false
	}
	compact := strings.ToLower(strings.Join(strings.Fields(sql), " "))
	for _, required := range cut.contains {
		if !strings.Contains(compact, required) {
			return false
		}
	}
	cut.seen++
	wantOccurrence := cut.occurrence
	if wantOccurrence == 0 {
		wantOccurrence = 1
	}
	if cut.seen != wantOccurrence {
		return false
	}
	cut.triggered = true
	return true
}

type collaborationSQLCutPointParticipant struct {
	delegate records.RevisionParticipant
	cut      *collaborationSQLCutPoint
}

func newCollaborationSQLCutPointParticipant(cut *collaborationSQLCutPoint) records.RevisionParticipant {
	return &collaborationSQLCutPointParticipant{
		delegate: NewCollaborationRevisionParticipant(NewPostgresCollaborationMembershipReader()),
		cut:      cut,
	}
}

func (*collaborationSQLCutPointParticipant) Name() string { return "collaboration" }

func (participant *collaborationSQLCutPointParticipant) ApplyRevision(
	ctx context.Context,
	tx pgx.Tx,
	committed records.RevisionCommitted,
) error {
	return participant.delegate.ApplyRevision(ctx, &collaborationSQLCutPointTx{Tx: tx, cut: participant.cut}, committed)
}

type collaborationSQLCutPointTx struct {
	pgx.Tx
	cut *collaborationSQLCutPoint
}

func (tx *collaborationSQLCutPointTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if tx.cut.matches(collaborationCutQueryRow, sql) {
		return collaborationSQLCutPointRow{}
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

func (tx *collaborationSQLCutPointTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if tx.cut.matches(collaborationCutQuery, sql) {
		return nil, errCollaborationSQLCutPoint
	}
	rows, err := tx.Tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	if tx.cut.matches(collaborationCutRowsScan, sql) {
		return &collaborationSQLCutPointRows{Rows: rows, failScan: true}, nil
	}
	if tx.cut.matches(collaborationCutRowsErr, sql) {
		return &collaborationSQLCutPointRows{Rows: rows, failErr: true}, nil
	}
	return rows, nil
}

func (tx *collaborationSQLCutPointTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if tx.cut.matches(collaborationCutExec, sql) {
		return pgconn.CommandTag{}, errCollaborationSQLCutPoint
	}
	return tx.Tx.Exec(ctx, sql, args...)
}

type collaborationSQLCutPointRow struct{}

func (collaborationSQLCutPointRow) Scan(...any) error { return errCollaborationSQLCutPoint }

type collaborationSQLCutPointRows struct {
	pgx.Rows
	failScan bool
	failErr  bool
}

func (rows *collaborationSQLCutPointRows) Scan(dest ...any) error {
	if rows.failScan {
		return errCollaborationSQLCutPoint
	}
	return rows.Rows.Scan(dest...)
}

func (rows *collaborationSQLCutPointRows) Err() error {
	if rows.failErr {
		return errCollaborationSQLCutPoint
	}
	return rows.Rows.Err()
}

type postgresCollaborationFollowerState struct {
	UserID           string
	FollowerVersion  int64
	ManualPreference string
	Author           bool
	Owner            bool
	Participant      bool
	Comment          bool
	Mention          bool
	Action           bool
	RecordFenceEpoch int64
}

type postgresCollaborationActivityState struct {
	ActivityID    string
	RevisionID    string
	EventKind     string
	SourceEventID string
	SourceVersion int64
}

type postgresCollaborationOutboxState struct {
	RowID              int64
	EventKind          string
	AuthorizationEpoch int64
}

type postgresCollaborationRecordState struct {
	CurrentRevisionID  string
	CurrentRevisionNo  int64
	LockVersion        int64
	AuthorizationEpoch int64
	Lifecycle          string
	RevisionCount      int
	Followers          []postgresCollaborationFollowerState
	Activities         []postgresCollaborationActivityState
	Outbox             []postgresCollaborationOutboxState
}

func preparePostgresCollaborationCutPointFlow(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	runtimePool *pgxpool.Pool,
	recordID string,
	idempotencyKey string,
	flow collaborationCutPointFlow,
) (postgresCollaborationRecordState, records.RevisionCommitCommand) {
	t.Helper()
	firstFollowUp := time.Date(2026, time.August, 19, 8, 0, 0, 0, time.UTC)
	secondFollowUp := firstFollowUp.Add(2 * time.Hour)
	firstInput := collaborationRevisionInput(t, collaborationRevisionInputValues{
		title:          recordID + " first",
		ownerID:        "usr_bbbbbbbbbbbbbbbbbbbbbbbb",
		participantIDs: []string{"usr_cccccccccccccccccccccccc"},
		followUpAt:     &firstFollowUp,
	})
	secondInput := collaborationRevisionInput(t, collaborationRevisionInputValues{
		title:   recordID + " second",
		ownerID: "usr_dddddddddddddddddddddddd",
		participantIDs: []string{
			"usr_bbbbbbbbbbbbbbbbbbbbbbbb",
			"usr_cccccccccccccccccccccccc",
		},
		followUpAt: &secondFollowUp,
	})
	if flow == collaborationCutPointCreate {
		return postgresCollaborationRecordState{}, recordsPostgresRevisionCommand(
			t, recordplatform.OperationKindRecordCreate, recordID, "", 0, 0,
			secondInput, idempotencyKey,
		)
	}
	goodRepository := newRecordsPostgresRepository(
		t, runtimePool,
		NewCollaborationRevisionParticipant(NewPostgresCollaborationMembershipReader()),
	)
	first, err := goodRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordCreate, recordID, "", 0, 0,
		firstInput, idempotencyKey+"-seed-create",
	))
	if err != nil {
		t.Fatalf("CommitRevision(seed create) error = %v", err)
	}
	if flow == collaborationCutPointRevise {
		seedPostgresCollaborationRetainedStaleFollower(t, ctx, fixture, recordID)
		return snapshotPostgresCollaborationRecordState(t, ctx, fixture, recordID), recordsPostgresRevisionCommand(
			t, recordplatform.OperationKindRecordUpdate, recordID, first.RevisionID,
			first.LockVersion, first.AuthorizationEpoch, secondInput, idempotencyKey,
		)
	}
	second, err := goodRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordUpdate, recordID, first.RevisionID,
		first.LockVersion, first.AuthorizationEpoch, secondInput,
		idempotencyKey+"-seed-revise",
	))
	if err != nil {
		t.Fatalf("CommitRevision(seed revise) error = %v", err)
	}
	seedPostgresCollaborationRetainedStaleFollower(t, ctx, fixture, recordID)
	command := recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordUpdate, recordID, second.RevisionID,
		second.LockVersion, second.AuthorizationEpoch, firstInput, idempotencyKey,
	)
	command.ActivityKind = records.DomainActivityRecordRestored
	return snapshotPostgresCollaborationRecordState(t, ctx, fixture, recordID), command
}

func seedPostgresCollaborationRetainedStaleFollower(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	recordID string,
) {
	t.Helper()
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_followers (
			project_id, record_id, user_id, follower_version,
			follows_owner, follows_comment, record_fence_epoch
		) values ('default', $1, 'usr_eeeeeeeeeeeeeeeeeeeeeeee', 1, true, true, 0)`, recordID); err != nil {
		t.Fatalf("seed retained stale collaboration follower: %v", err)
	}
}

func snapshotPostgresCollaborationRecordState(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	recordID string,
) postgresCollaborationRecordState {
	t.Helper()
	var state postgresCollaborationRecordState
	if err := fixture.db.QueryRow(ctx, `
		select root.current_revision_id, revision.revision_no,
		       root.lock_version, root.authorization_epoch, root.lifecycle
		from public.records as root
		join public.record_revisions as revision
		  on revision.revision_id = root.current_revision_id
		where root.record_id = $1`, recordID).Scan(
		&state.CurrentRevisionID,
		&state.CurrentRevisionNo,
		&state.LockVersion,
		&state.AuthorizationEpoch,
		&state.Lifecycle,
	); err != nil {
		t.Fatalf("read collaboration record state: %v", err)
	}
	if err := fixture.db.QueryRow(ctx, `
		select count(*)::int from public.record_revisions where record_id = $1`, recordID,
	).Scan(&state.RevisionCount); err != nil {
		t.Fatalf("count collaboration revision state: %v", err)
	}
	followerRows, err := fixture.db.Query(ctx, `
		select user_id, follower_version, manual_preference,
		       follows_author, follows_owner, follows_participant,
		       follows_comment, follows_mention, follows_action, record_fence_epoch
		from public.record_followers
		where record_id = $1
		order by user_id`, recordID)
	if err != nil {
		t.Fatalf("query collaboration follower state: %v", err)
	}
	for followerRows.Next() {
		var follower postgresCollaborationFollowerState
		if err := followerRows.Scan(
			&follower.UserID, &follower.FollowerVersion, &follower.ManualPreference,
			&follower.Author, &follower.Owner, &follower.Participant,
			&follower.Comment, &follower.Mention, &follower.Action, &follower.RecordFenceEpoch,
		); err != nil {
			followerRows.Close()
			t.Fatalf("scan collaboration follower state: %v", err)
		}
		state.Followers = append(state.Followers, follower)
	}
	if err := followerRows.Err(); err != nil {
		followerRows.Close()
		t.Fatalf("iterate collaboration follower state: %v", err)
	}
	followerRows.Close()
	activityRows, err := fixture.db.Query(ctx, `
		select activity_id, revision_id, event_kind, source_event_id, source_version
		from public.record_domain_activities
		where record_id = $1
		order by activity_id`, recordID)
	if err != nil {
		t.Fatalf("query collaboration activity state: %v", err)
	}
	for activityRows.Next() {
		var activity postgresCollaborationActivityState
		if err := activityRows.Scan(
			&activity.ActivityID, &activity.RevisionID, &activity.EventKind,
			&activity.SourceEventID, &activity.SourceVersion,
		); err != nil {
			activityRows.Close()
			t.Fatalf("scan collaboration activity state: %v", err)
		}
		state.Activities = append(state.Activities, activity)
	}
	if err := activityRows.Err(); err != nil {
		activityRows.Close()
		t.Fatalf("iterate collaboration activity state: %v", err)
	}
	activityRows.Close()
	outboxRows, err := fixture.db.Query(ctx, `
		select outbox_row_id, event_kind, authorization_epoch
		from public.record_outbox
		where subject_kind = 'record' and subject_id = $1
		order by outbox_row_id`, recordID)
	if err != nil {
		t.Fatalf("query collaboration outbox state: %v", err)
	}
	for outboxRows.Next() {
		var outbox postgresCollaborationOutboxState
		if err := outboxRows.Scan(&outbox.RowID, &outbox.EventKind, &outbox.AuthorizationEpoch); err != nil {
			outboxRows.Close()
			t.Fatalf("scan collaboration outbox state: %v", err)
		}
		state.Outbox = append(state.Outbox, outbox)
	}
	if err := outboxRows.Err(); err != nil {
		outboxRows.Close()
		t.Fatalf("iterate collaboration outbox state: %v", err)
	}
	outboxRows.Close()
	return state
}

func assertPostgresCollaborationIdempotencyAbsent(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	idempotencyKey string,
) {
	t.Helper()
	var count int
	if err := fixture.db.QueryRow(ctx, `
		select count(*)::int
		from public.record_idempotency_keys
		where idempotency_key = $1`, idempotencyKey,
	).Scan(&count); err != nil {
		t.Fatalf("count collaboration idempotency state: %v", err)
	}
	if count != 0 {
		t.Fatalf("collaboration idempotency rows = %d, want 0", count)
	}
}

func seedCollaborationRevisionUsers(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture) {
	t.Helper()
	for index, userID := range []string{
		"usr_aaaaaaaaaaaaaaaaaaaaaaaa",
		"usr_bbbbbbbbbbbbbbbbbbbbbbbb",
		"usr_cccccccccccccccccccccccc",
		"usr_dddddddddddddddddddddddd",
	} {
		if _, err := fixture.db.Exec(ctx, `
			insert into public.users (user_id, username, password_hash, display_name, role)
			values ($1, $2, 'test-hash', $2, 'admin')`, userID, fmt.Sprintf("collaboration-user-%d", index)); err != nil {
			t.Fatalf("seed collaboration user %q: %v", userID, err)
		}
	}
}

func assertPostgresCollaborationFollowers(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	recordID string,
	want map[string][3]bool,
) {
	t.Helper()
	rows, err := fixture.db.Query(ctx, `
		select user_id, follows_author, follows_owner, follows_participant, record_fence_epoch
		from public.record_followers
		where record_id = $1
		order by user_id`, recordID)
	if err != nil {
		t.Fatalf("query collaboration followers: %v", err)
	}
	defer rows.Close()
	got := make(map[string][3]bool)
	for rows.Next() {
		var userID string
		var flags [3]bool
		var fenceEpoch int64
		if err := rows.Scan(&userID, &flags[0], &flags[1], &flags[2], &fenceEpoch); err != nil {
			t.Fatalf("scan collaboration follower: %v", err)
		}
		if fenceEpoch != 0 {
			t.Fatalf("collaboration follower fence epoch = %d, want 0", fenceEpoch)
		}
		got[userID] = flags
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate collaboration followers: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collaboration followers = %#v, want %#v", got, want)
	}
}

func assertPostgresCollaborationRevisionFacts(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	result records.RevisionCommitResult,
	want []string,
) {
	t.Helper()
	var got []string
	if err := fixture.db.QueryRow(ctx, `
		select array_agg(event_kind order by event_kind)
		from public.record_domain_activities
		where record_id = $1 and revision_id = $2`, result.RecordID, result.RevisionID).Scan(&got); err != nil {
		t.Fatalf("read collaboration revision facts: %v", err)
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collaboration revision facts = %#v, want %#v", got, want)
	}
}

func assertPostgresCollaborationOutboxOrder(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	recordID string,
	want []string,
) {
	t.Helper()
	var got []string
	if err := fixture.db.QueryRow(ctx, `
		select array_agg(event_kind order by outbox_row_id)
		from public.record_outbox
		where subject_kind = 'record' and subject_id = $1`, recordID).Scan(&got); err != nil {
		t.Fatalf("read collaboration outbox order: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("collaboration outbox order = %#v, want %#v", got, want)
	}
}

func installCollaborationRevisionFailureTrigger(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	stage string,
) func() {
	t.Helper()
	migratorPool := fixture.openDirectRolePool(t, ctx, fixture.migrator, "collaboration-trigger-"+stage, 1)
	functionName := "record_collaboration_test_fail_" + stage
	tableName := "record_followers"
	condition := "true"
	switch stage {
	case "activity":
		tableName = "record_domain_activities"
		condition = "new.event_kind = 'record_owner_changed'"
	case "outbox":
		tableName = "record_outbox"
		condition = "new.event_kind = 'record_owner_changed'"
	case "outbox_owner":
		tableName = "record_outbox"
		condition = "new.event_kind = 'record_owner_changed'"
	case "outbox_participant":
		tableName = "record_outbox"
		condition = "new.event_kind = 'record_participant_changed'"
	}
	definitions := []string{
		`create function public.` + functionName + `()
			returns trigger language plpgsql as $$
			begin
				if ` + condition + ` then
					raise exception using errcode = '55000', message = 'collaboration stage failed';
				end if;
				return new;
			end
			$$`,
		`create trigger ` + functionName + ` before insert or update on public.` + tableName + `
			for each row execute function public.` + functionName + `()`,
		`grant execute on function public.` + functionName + `() to ` + pgx.Identifier{fixture.runtime}.Sanitize(),
	}
	for _, definition := range definitions {
		if _, err := migratorPool.Exec(ctx, definition); err != nil {
			t.Fatalf("install collaboration %s failure trigger: %v", stage, err)
		}
	}
	return func() {
		if _, err := migratorPool.Exec(ctx, `drop trigger `+functionName+` on public.`+tableName); err != nil {
			t.Fatalf("drop collaboration %s failure trigger: %v", stage, err)
		}
		if _, err := migratorPool.Exec(ctx, `drop function public.`+functionName+`()`); err != nil {
			t.Fatalf("drop collaboration %s failure function: %v", stage, err)
		}
	}
}

func assertPostgresCollaborationRevisionRolledBack(
	t *testing.T,
	ctx context.Context,
	fixture recordPlatformPostgresFixture,
	recordID string,
	idempotencyKey string,
) {
	t.Helper()
	var rootCount, revisionCount, followerCount, activityCount, outboxCount, idempotencyCount int
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.records where record_id = $1),
		       (select count(*)::int from public.record_revisions where record_id = $1),
		       (select count(*)::int from public.record_followers where record_id = $1),
		       (select count(*)::int from public.record_domain_activities where record_id = $1),
		       (select count(*)::int from public.record_outbox where subject_id = $1),
		       (select count(*)::int from public.record_idempotency_keys where idempotency_key = $2)`,
		recordID, idempotencyKey,
	).Scan(&rootCount, &revisionCount, &followerCount, &activityCount, &outboxCount, &idempotencyCount); err != nil {
		t.Fatalf("count collaboration rollback rows: %v", err)
	}
	if rootCount != 0 || revisionCount != 0 || followerCount != 0 || activityCount != 0 || outboxCount != 0 || idempotencyCount != 0 {
		t.Fatalf("collaboration rollback counts = root:%d revision:%d follower:%d activity:%d outbox:%d idempotency:%d, want all zero",
			rootCount, revisionCount, followerCount, activityCount, outboxCount, idempotencyCount)
	}
}
