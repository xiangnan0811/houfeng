package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

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

func TestPostgresIntegrationCollaborationRevisionParticipantConcurrentSameBaseKeepsWinnerFollowers(t *testing.T) {
	ctx := context.Background()
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

	inputs := []records.CompleteRevisionInput{
		collaborationRevisionInput(t, collaborationRevisionInputValues{ownerID: "usr_cccccccccccccccccccccccc"}),
		collaborationRevisionInput(t, collaborationRevisionInputValues{ownerID: "usr_dddddddddddddddddddddddd"}),
	}
	type outcome struct {
		owner string
		err   error
	}
	outcomes := make(chan outcome, len(inputs))
	start := make(chan struct{})
	for index, input := range inputs {
		index := index
		repository := newRecordsPostgresRepository(
			t,
			fixture.openDirectRuntimePool(t, ctx, fmt.Sprintf("collaboration-race-%d", index), 1),
			NewCollaborationRevisionParticipant(NewPostgresCollaborationMembershipReader()),
		)
		command := recordsPostgresRevisionCommand(
			t, recordplatform.OperationKindRecordUpdate, seed.RecordID, seed.RevisionID,
			seed.LockVersion, seed.AuthorizationEpoch, input, fmt.Sprintf("collaboration-race-%d", index),
		)
		go func() {
			<-start
			_, err := repository.CommitRevision(context.Background(), command)
			outcomes <- outcome{owner: input.OwnerID(), err: err}
		}()
	}
	close(start)

	winnerOwner := ""
	conflicts := 0
	for range inputs {
		result := <-outcomes
		if result.err == nil {
			winnerOwner = result.owner
			continue
		}
		if errors.Is(result.err, records.ErrRecordRevisionConflict) {
			conflicts++
			continue
		}
		t.Fatalf("concurrent collaboration revision error = %v", result.err)
	}
	if winnerOwner == "" || conflicts != 1 {
		t.Fatalf("concurrent collaboration winner/conflicts = %q/%d", winnerOwner, conflicts)
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
	if !reflect.DeepEqual(ownerFollowers, []string{winnerOwner}) {
		t.Fatalf("concurrent owner followers = %#v, want winner %q", ownerFollowers, winnerOwner)
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
