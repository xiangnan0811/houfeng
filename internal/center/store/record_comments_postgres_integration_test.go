package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

func TestPostgresIntegrationRecordCommentRedactionActivityFailureRollsBackAllContentChanges(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "rec_pgcommentrollback", "comments-rollback-parent")
	repository := NewPostgresRecordCommentRepository(
		fixture.openDirectRuntimePool(t, ctx, "record-comments-rollback", 1),
		allowRecordPlatformAdmissionGate,
		NewPostgresCollaborationMembershipReader(),
	)
	create := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationCreate,
		"rcm_pgrollback", 0, "Content must survive rollback.", "",
		[]string{"usr_bbbbbbbbbbbbbbbbbbbbbbbb"}, "comments-rollback-create")
	if _, err := repository.CommitComment(ctx, create); err != nil {
		t.Fatalf("CommitComment(create) error = %v", err)
	}

	redact := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationRedact,
		create.CommentID, 1, "", "", nil, "comments-rollback-redact")
	conflictingActivityID := recordCommentActivityID(recordCommentTombstoneID(create.CommentID, 2))
	if _, err := fixture.db.Exec(ctx, `
		insert into public.record_domain_activities (
			activity_id, project_id, record_id, revision_id, event_kind, source_event_id,
			source_version, actor_id, authorization_epoch, record_lock_version, event_at
		) values ($1, 'default', $2, $3, 'comment_rollback_block', 'comment_rollback_block',
			1, $4, $5, $6, transaction_timestamp())`, conflictingActivityID, parent.RecordID,
		parent.RevisionID, create.Actor.UserID, int64(parent.AuthorizationEpoch), int64(parent.LockVersion)); err != nil {
		t.Fatalf("seed conflicting comment activity: %v", err)
	}

	if result, err := repository.CommitComment(ctx, redact); err == nil || result != (recordcollaboration.CommentMutationResult{}) {
		t.Fatalf("CommitComment(redact with activity failure) result=%#v error=%v", result, err)
	}

	var (
		state, body, contractVersion, renderModel                                                  string
		version, revisionCount, activeRevisionCount, tombstoneCount, outboxCount, idempotencyCount int
		bodyDigest                                                                                 []byte
		redactedAt                                                                                 *time.Time
	)
	if err := fixture.db.QueryRow(ctx, `
		select comment_state, comment_version::int, body_markdown, render_contract_version, render_model::text,
		       body_digest, redacted_at,
		       (select count(*)::int from public.record_comment_revisions where comment_id = $1),
		       (select count(*)::int from public.record_comment_revisions where comment_id = $1
		          and body_markdown is not null and render_model is not null and body_digest is not null and redacted_at is null),
		       (select count(*)::int from public.record_comment_tombstones where comment_id = $1),
		       (select count(*)::int from public.record_outbox where subject_kind = 'comment' and subject_id = $1),
		       (select count(*)::int from public.record_idempotency_keys where idempotency_key = 'comments-rollback-redact')
		from public.record_comments where comment_id = $1`, create.CommentID).Scan(
		&state, &version, &body, &contractVersion, &renderModel, &bodyDigest, &redactedAt,
		&revisionCount, &activeRevisionCount, &tombstoneCount, &outboxCount, &idempotencyCount,
	); err != nil {
		t.Fatalf("read redaction rollback state: %v", err)
	}
	if state != string(recordcollaboration.CommentStateActive) || version != 1 || body != create.Content.Source() ||
		contractVersion != recordcollaboration.CommentRenderContractVersionV1 || renderModel == "" || len(bodyDigest) != 32 ||
		redactedAt != nil || revisionCount != 1 || activeRevisionCount != 1 || tombstoneCount != 0 || outboxCount != 2 || idempotencyCount != 0 {
		t.Fatalf("redaction rollback state=%q/%d/%q/%q model=%q digest=%x redacted=%v revisions=%d/%d tombstone/outbox/key=%d/%d/%d",
			state, version, body, contractVersion, renderModel, bodyDigest, redactedAt,
			revisionCount, activeRevisionCount, tombstoneCount, outboxCount, idempotencyCount)
	}
}

func TestPostgresIntegrationRecordCommentsConcurrentSameVersionHasOneWinner(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "rec_pgcommentrace", "comments-race-parent")
	seedRepository := NewPostgresRecordCommentRepository(
		fixture.openDirectRuntimePool(t, ctx, "record-comments-race-seed", 1),
		allowRecordPlatformAdmissionGate,
		NewPostgresCollaborationMembershipReader(),
	)
	create := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationCreate,
		"rcm_pgrace", 0, "Race source.", "", nil, "comments-race-create")
	if _, err := seedRepository.CommitComment(ctx, create); err != nil {
		t.Fatalf("CommitComment(create) error = %v", err)
	}
	rootBefore := readPostgresActionRoot(t, ctx, fixture, parent.RecordID)

	const holdLock int64 = 917_004_002
	if _, err := fixture.db.Exec(ctx, fmt.Sprintf(`
		create function public.houfeng_test_hold_record_comment_race() returns trigger
		language plpgsql
		set search_path = pg_catalog
		as $function$
		begin
		  perform pg_catalog.pg_advisory_xact_lock(%d);
		  return new;
		end
		$function$`, holdLock)); err != nil {
		t.Fatalf("create comment race hold function: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := fixture.db.Exec(cleanupCtx, `drop trigger if exists houfeng_test_hold_record_comment_race on public.record_comment_revisions`); err != nil {
			t.Errorf("drop comment race hold trigger: %v", err)
		}
		if _, err := fixture.db.Exec(cleanupCtx, `drop function if exists public.houfeng_test_hold_record_comment_race()`); err != nil {
			t.Errorf("drop comment race hold function: %v", err)
		}
	})
	if _, err := fixture.db.Exec(ctx, `
		create trigger houfeng_test_hold_record_comment_race
		after insert on public.record_comment_revisions
		for each row
		when (new.comment_id = 'rcm_pgrace' and new.comment_version = 2 and new.body_markdown = 'Winner A.')
		execute function public.houfeng_test_hold_record_comment_race()`); err != nil {
		t.Fatalf("create comment race hold trigger: %v", err)
	}

	blocker, err := fixture.db.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire comment race hold connection: %v", err)
	}
	holdReleased := false
	defer func() {
		if !holdReleased {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cleanupCancel()
			_, _ = blocker.Exec(cleanupCtx, `select pg_catalog.pg_advisory_unlock($1)`, holdLock)
		}
		blocker.Release()
	}()
	if _, err := blocker.Exec(ctx, `select pg_catalog.pg_advisory_lock($1)`, holdLock); err != nil {
		t.Fatalf("acquire comment race hold lock: %v", err)
	}
	var blockerPID int32
	if err := blocker.QueryRow(ctx, `select pg_backend_pid()`).Scan(&blockerPID); err != nil {
		t.Fatalf("read comment race hold backend PID: %v", err)
	}

	firstPool := fixture.openDirectRuntimePool(t, ctx, "record-comments-race-first", 1)
	secondPool := fixture.openDirectRuntimePool(t, ctx, "record-comments-race-second", 1)
	firstPID := postgresCollaborationBackendPID(t, ctx, firstPool)
	secondPID := postgresCollaborationBackendPID(t, ctx, secondPool)
	firstRepository := NewPostgresRecordCommentRepository(firstPool, allowRecordPlatformAdmissionGate, NewPostgresCollaborationMembershipReader())
	secondRepository := NewPostgresRecordCommentRepository(secondPool, allowRecordPlatformAdmissionGate, NewPostgresCollaborationMembershipReader())
	firstCommand := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationEdit,
		create.CommentID, 1, "Winner A.", "", nil, "comments-race-a")
	secondCommand := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationEdit,
		create.CommentID, 1, "Winner B.", "", nil, "comments-race-b")
	type outcome struct {
		result recordcollaboration.CommentMutationResult
		err    error
	}
	firstResult := make(chan outcome, 1)
	go func() {
		result, err := firstRepository.CommitComment(ctx, firstCommand)
		firstResult <- outcome{result: result, err: err}
	}()
	firstBlockers := waitForPostgresCollaborationBlocker(t, ctx, fixture, firstPID, blockerPID)
	if !slices.Contains(firstBlockers, blockerPID) {
		t.Fatalf("first comment backend blockers = %#v, want hold backend %d", firstBlockers, blockerPID)
	}

	secondResult := make(chan outcome, 1)
	secondStarted := make(chan struct{})
	go func() {
		close(secondStarted)
		result, err := secondRepository.CommitComment(ctx, secondCommand)
		secondResult <- outcome{result: result, err: err}
	}()
	select {
	case <-secondStarted:
	case <-ctx.Done():
		t.Fatalf("second comment edit did not start: %v", ctx.Err())
	}
	secondBlockers := waitForPostgresCollaborationBlocker(t, ctx, fixture, secondPID, firstPID)
	if !slices.Contains(secondBlockers, firstPID) {
		t.Fatalf("second comment backend blockers = %#v, want first backend %d", secondBlockers, firstPID)
	}
	select {
	case got := <-secondResult:
		t.Fatalf("second comment edit completed before first release: %#v", got)
	default:
	}
	if _, err := blocker.Exec(ctx, `select pg_catalog.pg_advisory_unlock($1)`, holdLock); err != nil {
		t.Fatalf("release comment race hold lock: %v", err)
	}
	holdReleased = true

	var first, second outcome
	select {
	case first = <-firstResult:
	case <-ctx.Done():
		t.Fatalf("first comment edit did not finish: %v", ctx.Err())
	}
	select {
	case second = <-secondResult:
	case <-ctx.Done():
		t.Fatalf("second comment edit did not finish: %v", ctx.Err())
	}
	if first.err != nil || first.result.Version != 2 || first.result.State != recordcollaboration.CommentStateActive {
		t.Fatalf("first comment edit result/error = %#v/%v", first.result, first.err)
	}
	if !errors.Is(second.err, recordcollaboration.ErrCommentConflict) || second.result != (recordcollaboration.CommentMutationResult{}) {
		t.Fatalf("second comment edit result/error = %#v/%v, want conflict", second.result, second.err)
	}
	var version, revisionCount, activityCount, outboxCount, idempotencyCount int
	if err := fixture.db.QueryRow(ctx, `
		select comment_version::int,
		       (select count(*)::int from public.record_comment_revisions where comment_id = $1),
		       (select count(*)::int from public.record_domain_activities where record_id = $2
		          and event_kind in ('comment_created', 'comment_edited')),
		       (select count(*)::int from public.record_outbox where subject_kind = 'comment' and subject_id = $1),
		       (select count(*)::int from public.record_idempotency_keys where idempotency_key in ('comments-race-a', 'comments-race-b'))
		from public.record_comments where comment_id = $1`, create.CommentID, parent.RecordID).Scan(
		&version, &revisionCount, &activityCount, &outboxCount, &idempotencyCount,
	); err != nil {
		t.Fatalf("read concurrent comment state: %v", err)
	}
	if version != 2 || revisionCount != 2 || activityCount != 2 || outboxCount != 2 || idempotencyCount != 1 {
		t.Fatalf("concurrent durable state version/revisions/activity/outbox/keys=%d/%d/%d/%d/%d",
			version, revisionCount, activityCount, outboxCount, idempotencyCount)
	}
	if rootAfter := readPostgresActionRoot(t, ctx, fixture, parent.RecordID); !reflect.DeepEqual(rootAfter, rootBefore) {
		t.Fatalf("concurrent comments mutated root: before=%#v after=%#v", rootBefore, rootAfter)
	}
}

func TestPostgresIntegrationRecordCommentsReplyMentionAndModeratorPolicies(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "rec_pgcommentpolicy", "comments-policy-parent")
	repository := NewPostgresRecordCommentRepository(
		fixture.openDirectRuntimePool(t, ctx, "record-comments-policy", 2),
		allowRecordPlatformAdmissionGate,
		NewPostgresCollaborationMembershipReader(),
	)

	root := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationCreate,
		"rcm_pgpolicyroot", 0, "Policy root.", "", nil, "comments-policy-root")
	if _, err := repository.CommitComment(ctx, root); err != nil {
		t.Fatalf("CommitComment(root) error = %v", err)
	}
	reply := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationCreate,
		"rcm_pgpolicyreply", 0, "Flat reply.", root.CommentID,
		[]string{"usr_bbbbbbbbbbbbbbbbbbbbbbbb"}, "comments-policy-reply")
	if _, err := repository.CommitComment(ctx, reply); err != nil {
		t.Fatalf("CommitComment(reply) error = %v", err)
	}

	nested := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationCreate,
		"rcm_pgpolicynested", 0, "Nested reply.", reply.CommentID, nil, "comments-policy-nested")
	if result, err := repository.CommitComment(ctx, nested); !errors.Is(err, recordcollaboration.ErrInvalidCommentContent) || result != (recordcollaboration.CommentMutationResult{}) {
		t.Fatalf("CommitComment(nested reply) result=%#v error=%v", result, err)
	}
	missingMention := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationCreate,
		"rcm_pgpolicymissing", 0, "Missing mention.", "",
		[]string{"usr_eeeeeeeeeeeeeeeeeeeeeeee"}, "comments-policy-missing")
	if result, err := repository.CommitComment(ctx, missingMention); !errors.Is(err, recordcollaboration.ErrMembershipDenied) || result != (recordcollaboration.CommentMutationResult{}) {
		t.Fatalf("CommitComment(missing member) result=%#v error=%v", result, err)
	}
	if _, err := fixture.db.Exec(ctx, `update public.users set role = 'user' where user_id = $1`, "usr_cccccccccccccccccccccccc"); err != nil {
		t.Fatalf("seed non-member role: %v", err)
	}
	otherRoleMention := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationCreate,
		"rcm_pgpolicyrole", 0, "Other-role mention.", "",
		[]string{"usr_cccccccccccccccccccccccc"}, "comments-policy-role")
	if result, err := repository.CommitComment(ctx, otherRoleMention); !errors.Is(err, recordcollaboration.ErrMembershipDenied) || result != (recordcollaboration.CommentMutationResult{}) {
		t.Fatalf("CommitComment(other-role member) result=%#v error=%v", result, err)
	}

	otherAuthorEdit := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationEdit,
		root.CommentID, 1, "Unauthorized edit.", "", nil, "comments-policy-other-edit")
	otherAuthorEdit.Actor = mustPostgresCommentActor(t, "usr_bbbbbbbbbbbbbbbbbbbbbbbb", recordauth.RoleProjectAdmin)
	if result, err := repository.CommitComment(ctx, otherAuthorEdit); !errors.Is(err, recordcollaboration.ErrCommentPolicyDenied) || result != (recordcollaboration.CommentMutationResult{}) {
		t.Fatalf("CommitComment(other-author edit) result=%#v error=%v", result, err)
	}

	moderatorRedact := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationRedact,
		root.CommentID, 1, "", "", nil, "comments-policy-moderator-redact")
	moderatorRedact.Actor = mustPostgresCommentActor(t, "usr_bbbbbbbbbbbbbbbbbbbbbbbb", recordauth.RoleProjectAdmin)
	result, err := repository.CommitComment(ctx, moderatorRedact)
	if err != nil || result.State != recordcollaboration.CommentStateRedacted || result.Version != 2 {
		t.Fatalf("CommitComment(moderator redact) result=%#v error=%v", result, err)
	}

	for _, key := range []string{
		"comments-policy-nested", "comments-policy-missing", "comments-policy-role", "comments-policy-other-edit",
	} {
		assertPostgresCollaborationIdempotencyAbsent(t, ctx, fixture, key)
	}
	var nestedCount, missingCount, roleCount, rootTombstones, replyMentions int
	if err := fixture.db.QueryRow(ctx, `
		select
		  (select count(*)::int from public.record_comments where comment_id = 'rcm_pgpolicynested'),
		  (select count(*)::int from public.record_comments where comment_id = 'rcm_pgpolicymissing'),
		  (select count(*)::int from public.record_comments where comment_id = 'rcm_pgpolicyrole'),
		  (select count(*)::int from public.record_comment_tombstones where comment_id = $1),
		  (select count(*)::int from public.record_comment_mentions where comment_id = $2 and mentioned_user_id = $3)`,
		root.CommentID, reply.CommentID, "usr_bbbbbbbbbbbbbbbbbbbbbbbb").Scan(
		&nestedCount, &missingCount, &roleCount, &rootTombstones, &replyMentions,
	); err != nil {
		t.Fatalf("read comment policy durable state: %v", err)
	}
	if nestedCount != 0 || missingCount != 0 || roleCount != 0 || rootTombstones != 1 || replyMentions != 1 {
		t.Fatalf("comment policy durable counts nested/missing/role/tombstones/mentions=%d/%d/%d/%d/%d",
			nestedCount, missingCount, roleCount, rootTombstones, replyMentions)
	}
}

func TestPostgresIntegrationRecordCommentsRejectStaleSessionModeratorAfterPersistedDemotion(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "rec_pgcommentdemotion", "comments-demotion-parent")
	repository := NewPostgresRecordCommentRepository(
		fixture.openDirectRuntimePool(t, ctx, "record-comments-demotion", 1),
		allowRecordPlatformAdmissionGate,
		NewPostgresCollaborationMembershipReader(),
	)
	create := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationCreate,
		"rcm_pgdemotion", 0, "Must remain active.", "", nil, "comments-demotion-create")
	if _, err := repository.CommitComment(ctx, create); err != nil {
		t.Fatalf("CommitComment(create) error = %v", err)
	}
	if _, err := fixture.db.Exec(ctx, `update public.users set role = 'user' where user_id = $1`, "usr_bbbbbbbbbbbbbbbbbbbbbbbb"); err != nil {
		t.Fatalf("demote moderator: %v", err)
	}
	redact := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationRedact,
		create.CommentID, 1, "", "", nil, "comments-demotion-redact")
	redact.Actor = mustPostgresCommentActor(t, "usr_bbbbbbbbbbbbbbbbbbbbbbbb", recordauth.RoleProjectAdmin)
	if result, err := repository.CommitComment(ctx, redact); !errors.Is(err, recordcollaboration.ErrMembershipDenied) || result != (recordcollaboration.CommentMutationResult{}) {
		t.Fatalf("CommitComment(stale-session moderator) result=%#v error=%v", result, err)
	}
	var state string
	var version, tombstones, idempotency int
	if err := fixture.db.QueryRow(ctx, `
		select comment_state, comment_version::int,
		       (select count(*)::int from public.record_comment_tombstones where comment_id = $1),
		       (select count(*)::int from public.record_idempotency_keys where idempotency_key = 'comments-demotion-redact')
		from public.record_comments where comment_id = $1`, create.CommentID).Scan(&state, &version, &tombstones, &idempotency); err != nil {
		t.Fatalf("read demotion state: %v", err)
	}
	if state != string(recordcollaboration.CommentStateActive) || version != 1 || tombstones != 0 || idempotency != 0 {
		t.Fatalf("demotion durable state=%q/%d tombstones/key=%d/%d", state, version, tombstones, idempotency)
	}
}

func TestPostgresIntegrationRecordCommentsRequireAvailablePersistedActorMembershipForEveryMutation(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "rec_pgcommentactor", "comments-actor-parent")
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-comments-actor", 1)

	if _, err := fixture.db.Exec(ctx, `delete from public.users where user_id = $1`, "usr_dddddddddddddddddddddddd"); err != nil {
		t.Fatalf("remove actor membership row: %v", err)
	}
	missingActorRepository := NewPostgresRecordCommentRepository(
		runtimePool, allowRecordPlatformAdmissionGate, NewPostgresCollaborationMembershipReader(),
	)
	missingActor := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationCreate,
		"rcm_pgmissingactor", 0, "Missing actor.", "", nil, "comments-missing-actor")
	missingActor.Actor = mustPostgresCommentActor(t, "usr_dddddddddddddddddddddddd", recordauth.RoleProjectAdmin)
	if result, err := missingActorRepository.CommitComment(ctx, missingActor); !errors.Is(err, recordcollaboration.ErrMembershipDenied) || result != (recordcollaboration.CommentMutationResult{}) {
		t.Fatalf("CommitComment(missing actor) result=%#v error=%v", result, err)
	}

	unavailable := &unavailableCommentMembershipReader{err: recordcollaboration.ErrMembershipUnavailable}
	unavailableRepository := NewPostgresRecordCommentRepository(runtimePool, allowRecordPlatformAdmissionGate, unavailable)
	unavailableCreate := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationCreate,
		"rcm_pgunavailableactor", 0, "Unavailable actor.", "", nil, "comments-unavailable-actor")
	if result, err := unavailableRepository.CommitComment(ctx, unavailableCreate); !errors.Is(err, recordcollaboration.ErrMembershipUnavailable) || result != (recordcollaboration.CommentMutationResult{}) {
		t.Fatalf("CommitComment(unavailable actor) result=%#v error=%v", result, err)
	}
	if unavailable.calls != 1 || unavailable.tx == nil {
		t.Fatalf("membership calls/tx = %d/%T, want one caller-owned tx", unavailable.calls, unavailable.tx)
	}

	for _, key := range []string{"comments-missing-actor", "comments-unavailable-actor"} {
		assertPostgresCollaborationIdempotencyAbsent(t, ctx, fixture, key)
	}
	var commentCount int
	if err := fixture.db.QueryRow(ctx, `
		select count(*)::int from public.record_comments
		where comment_id in ('rcm_pgmissingactor', 'rcm_pgunavailableactor')`).Scan(&commentCount); err != nil {
		t.Fatalf("count failed actor comments: %v", err)
	}
	if commentCount != 0 {
		t.Fatalf("failed actor comments = %d, want zero", commentCount)
	}
}

func TestPostgresIntegrationRecordCommentsRejectCrossRecordReplyAndStaleRootAuthorization(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	first := seedPostgresActionParent(t, ctx, fixture, "rec_pgcommentfirst", "comments-first-parent")
	recordRepository := newRecordsPostgresRepository(t, fixture.openDirectRuntimePool(t, ctx, "record-comments-records", 1))
	second, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordCreate, "rec_pgcommentsecond", "", 0, 0,
		recordsPostgresCompleteRevisionInput(t, "Second comment parent"), "comments-second-parent",
	))
	if err != nil {
		t.Fatalf("CommitRevision(second parent) error = %v", err)
	}
	repository := NewPostgresRecordCommentRepository(
		fixture.openDirectRuntimePool(t, ctx, "record-comments-cross-record", 1),
		allowRecordPlatformAdmissionGate,
		NewPostgresCollaborationMembershipReader(),
	)
	secondComment := postgresCommentCommand(t, second, recordcollaboration.CommentMutationCreate,
		"rcm_pgsecond", 0, "Second record.", "", nil, "comments-second-create")
	if _, err := repository.CommitComment(ctx, secondComment); err != nil {
		t.Fatalf("CommitComment(second record) error = %v", err)
	}
	crossRecordReply := postgresCommentCommand(t, first, recordcollaboration.CommentMutationCreate,
		"rcm_pgcrossrecord", 0, "Cross-record reply.", secondComment.CommentID, nil, "comments-cross-record")
	if result, err := repository.CommitComment(ctx, crossRecordReply); !errors.Is(err, recordcollaboration.ErrInvalidCommentContent) || result != (recordcollaboration.CommentMutationResult{}) {
		t.Fatalf("CommitComment(cross-record reply) result=%#v error=%v", result, err)
	}

	staleRoot := postgresCommentCommand(t, first, recordcollaboration.CommentMutationCreate,
		"rcm_pgstaleroot", 0, "Stale root.", "", nil, "comments-stale-root")
	if _, err := recordRepository.CommitRevision(ctx, recordsPostgresRevisionCommand(
		t, recordplatform.OperationKindRecordUpdate, first.RecordID, first.RevisionID, first.LockVersion,
		first.AuthorizationEpoch, recordsPostgresCompleteRevisionInput(t, "Authorization drift"), "comments-root-drift",
	)); err != nil {
		t.Fatalf("CommitRevision(root drift) error = %v", err)
	}
	if result, err := repository.CommitComment(ctx, staleRoot); !errors.Is(err, recordcollaboration.ErrCommentConflict) || result != (recordcollaboration.CommentMutationResult{}) {
		t.Fatalf("CommitComment(stale root) result=%#v error=%v", result, err)
	}
	for _, key := range []string{"comments-cross-record", "comments-stale-root"} {
		assertPostgresCollaborationIdempotencyAbsent(t, ctx, fixture, key)
	}
}

func TestPostgresIntegrationRecordCommentsRejectCommittedDeletionReservationBeforeWrites(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "rec_pgcommentfenced", "comments-fenced-parent")
	if _, err := fixture.db.Exec(ctx, `
		insert into public.deletion_reservations (
			reservation_id, project_id, object_kind, object_id,
			deletion_token_commitment, request_fingerprint,
			actor_scope_digest, preview_binding_digest,
			preview_current_revision_id, preview_lock_version,
			preview_authorization_epoch, preview_content_delivery_epoch,
			preview_dependency_graph_digest, preview_backup_inventory_digest,
			preview_processor_inventory_digest, adapter_readiness_digest,
			adapter_preview_digest, preview_witness_sequence,
			preview_witness_entry_hash, state, expires_at, completed_at
		) values (
			'drs_commentfenced', 'default', 'record', $1,
			decode(repeat('31', 32), 'hex'), decode(repeat('32', 32), 'hex'),
			decode(repeat('33', 32), 'hex'), decode(repeat('34', 32), 'hex'),
			$2, $3, $4, 0,
			decode(repeat('35', 32), 'hex'), decode(repeat('36', 32), 'hex'),
			decode(repeat('37', 32), 'hex'), decode(repeat('38', 32), 'hex'),
			decode(repeat('39', 32), 'hex'), 1,
			decode(repeat('3a', 32), 'hex'), 'committed',
			transaction_timestamp() + interval '5 minutes', transaction_timestamp()
		)`, parent.RecordID, parent.RevisionID, parent.LockVersion, parent.AuthorizationEpoch); err != nil {
		t.Fatalf("seed committed deletion reservation: %v", err)
	}
	repository := NewPostgresRecordCommentRepository(
		fixture.openDirectRuntimePool(t, ctx, "record-comments-fenced", 1),
		allowRecordPlatformAdmissionGate,
		NewPostgresCollaborationMembershipReader(),
	)
	command := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationCreate,
		"rcm_pgfenced", 0, "Fenced comment.", "", nil, "comments-fenced-create")
	if result, err := repository.CommitComment(ctx, command); !errors.Is(err, records.ErrRecordDeletionReserved) || result != (recordcollaboration.CommentMutationResult{}) {
		t.Fatalf("CommitComment(fenced record) result=%#v error=%v", result, err)
	}
	assertPostgresCollaborationIdempotencyAbsent(t, ctx, fixture, command.Idempotency.Key.Key)
	var count int
	if err := fixture.db.QueryRow(ctx, `select count(*)::int from public.record_comments where comment_id = $1`, command.CommentID).Scan(&count); err != nil {
		t.Fatalf("count fenced comment: %v", err)
	}
	if count != 0 {
		t.Fatalf("fenced comment rows = %d, want zero", count)
	}
}

type unavailableCommentMembershipReader struct {
	err   error
	calls int
	tx    pgx.Tx
}

func (reader *unavailableCommentMembershipReader) ReadMemberActor(_ context.Context, tx pgx.Tx, _ recordauth.ProjectID, _ string) (recordauth.ActorScope, error) {
	reader.calls++
	reader.tx = tx
	return recordauth.ActorScope{}, reader.err
}

func TestPostgresIntegrationRecordCommentsLifecycleReplayAndOneWayRedaction(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "rec_pgcomments", "comments-parent-key")
	repository := NewPostgresRecordCommentRepository(
		fixture.openDirectRuntimePool(t, ctx, "record-comments-lifecycle", 3),
		allowRecordPlatformAdmissionGate,
		NewPostgresCollaborationMembershipReader(),
	)
	rootBefore := readPostgresActionRoot(t, ctx, fixture, parent.RecordID)

	createParent := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationCreate,
		"rcm_pgparent", 0, "Parent **comment**.", "", nil, "comments-create-parent")
	createReply := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationCreate,
		"rcm_pgreply", 0, "Reply with [safe link](https://example.com/path).", createParent.CommentID,
		[]string{"usr_bbbbbbbbbbbbbbbbbbbbbbbb"}, "comments-create-reply")
	editReply := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationEdit,
		createReply.CommentID, 1, "Edited reply.", "", []string{"usr_cccccccccccccccccccccccc"}, "comments-edit-reply")
	redactReply := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationRedact,
		createReply.CommentID, 2, "", "", nil, "comments-redact-reply")
	commands := []recordcollaboration.CommentCommand{createParent, createReply, editReply, redactReply}

	for index, command := range commands {
		result, err := repository.CommitComment(ctx, command)
		if err != nil {
			t.Fatalf("CommitComment(%s) error = %v", command.Kind, err)
		}
		wantVersion := []uint64{1, 1, 2, 3}[index]
		wantState := recordcollaboration.CommentStateActive
		if command.Kind == recordcollaboration.CommentMutationRedact {
			wantState = recordcollaboration.CommentStateRedacted
		}
		if result.Replayed || result.Version != wantVersion || result.State != wantState || result.EventKind != command.Kind {
			t.Fatalf("CommitComment(%s) result = %#v", command.Kind, result)
		}
	}
	for index, command := range commands {
		result, err := repository.CommitComment(ctx, command)
		if err != nil {
			t.Fatalf("CommitComment(%s) replay error = %v", command.Kind, err)
		}
		if !result.Replayed || result.Version != []uint64{1, 1, 2, 3}[index] {
			t.Fatalf("CommitComment(%s) replay = %#v", command.Kind, result)
		}
	}

	comments, err := repository.ListComments(ctx, postgresCommentReadCommand(t, parent, 100))
	if err != nil {
		t.Fatalf("ListComments() error = %v", err)
	}
	if len(comments) != 2 || comments[0].CommentID != createParent.CommentID || comments[1].CommentID != createReply.CommentID {
		t.Fatalf("ListComments() = %#v", comments)
	}
	redacted := comments[1]
	if redacted.State != recordcollaboration.CommentStateRedacted || redacted.Version != 3 ||
		redacted.BodyMarkdown != "" || redacted.RenderModel.Version != "" || len(redacted.MentionUserIDs) != 0 ||
		redacted.ReplyToCommentID != createParent.CommentID || redacted.RedactedAt == nil {
		t.Fatalf("redacted comment = %#v", redacted)
	}

	staleEdit := editReply
	staleEdit.Idempotency.Key.Key = "comments-stale-edit"
	staleEdit.Idempotency.RequestFingerprint = mustStoreActionFingerprint(t, recordplatform.OperationKindRecordCommentEdit, 0x61)
	staleEdit.ResultFingerprint = mustStoreActionFingerprint(t, recordplatform.OperationKindRecordCommentEdit, 0x62)
	if result, err := repository.CommitComment(ctx, staleEdit); !errors.Is(err, recordcollaboration.ErrCommentConflict) || result != (recordcollaboration.CommentMutationResult{}) {
		t.Fatalf("CommitComment(stale edit) result=%#v error=%v", result, err)
	}

	var (
		state, replyTo                                               string
		version, revisionCount, clearedRevisionCount, tombstoneCount int
		activityCount, outboxCount, idempotencyCount                 int
		body, contractVersion, renderModel                           *string
		bodyDigest                                                   []byte
	)
	if err := fixture.db.QueryRow(ctx, `
		select comment.comment_state, comment.comment_version::int,
		       comment.body_markdown, comment.render_contract_version, comment.render_model::text, comment.body_digest,
		       reply.parent_comment_id,
		       (select count(*)::int from public.record_comment_revisions where comment_id = comment.comment_id),
		       (select count(*)::int from public.record_comment_revisions where comment_id = comment.comment_id
		          and body_markdown is null and render_contract_version is null and render_model is null and body_digest is null and redacted_at is not null),
		       (select count(*)::int from public.record_comment_tombstones where comment_id = comment.comment_id),
		       (select count(*)::int from public.record_domain_activities where record_id = comment.record_id
		          and event_kind in ('comment_created', 'comment_edited', 'comment_redacted')),
		       (select count(*)::int from public.record_outbox where subject_kind = 'comment'),
		       (select count(*)::int from public.record_idempotency_keys where operation_kind like 'record_comment_%')
		from public.record_comments comment
		join public.record_comment_replies reply on reply.record_id = comment.record_id and reply.child_comment_id = comment.comment_id
		where comment.comment_id = $1`, createReply.CommentID).Scan(
		&state, &version, &body, &contractVersion, &renderModel, &bodyDigest, &replyTo,
		&revisionCount, &clearedRevisionCount, &tombstoneCount, &activityCount, &outboxCount, &idempotencyCount,
	); err != nil {
		t.Fatalf("read redacted durable state: %v", err)
	}
	if state != string(recordcollaboration.CommentStateRedacted) || version != 3 || body != nil || contractVersion != nil ||
		renderModel != nil || bodyDigest != nil || replyTo != createParent.CommentID || revisionCount != 2 ||
		clearedRevisionCount != 2 || tombstoneCount != 1 || activityCount != 4 || outboxCount != 7 || idempotencyCount != 4 {
		t.Fatalf("redacted durable state=%q/%d/%v/%v/%v/%x reply=%q revisions=%d/%d tombstones=%d activity/outbox/keys=%d/%d/%d",
			state, version, body, contractVersion, renderModel, bodyDigest, replyTo, revisionCount, clearedRevisionCount,
			tombstoneCount, activityCount, outboxCount, idempotencyCount)
	}
	if rootAfter := readPostgresActionRoot(t, ctx, fixture, parent.RecordID); !reflect.DeepEqual(rootAfter, rootBefore) {
		t.Fatalf("comment lifecycle mutated root: before=%#v after=%#v", rootBefore, rootAfter)
	}
}

func postgresCommentCommand(
	t *testing.T,
	parent records.RevisionCommitResult,
	kind recordcollaboration.CommentMutationKind,
	commentID string,
	expectedVersion uint64,
	body, replyTo string,
	mentions []string,
	idempotencyKey string,
) recordcollaboration.CommentCommand {
	t.Helper()
	actor, evidence := storeActionAuthorization(t)
	actor = mustPostgresCommentActor(t, "usr_aaaaaaaaaaaaaaaaaaaaaaaa", actor.Role)
	operation := map[recordcollaboration.CommentMutationKind]recordplatform.OperationKind{
		recordcollaboration.CommentMutationCreate: recordplatform.OperationKindRecordCommentCreate,
		recordcollaboration.CommentMutationEdit:   recordplatform.OperationKindRecordCommentEdit,
		recordcollaboration.CommentMutationRedact: recordplatform.OperationKindRecordCommentRedact,
	}[kind]
	command := recordcollaboration.CommentCommand{
		Kind: kind, Actor: actor, RecordID: parent.RecordID, CommentID: commentID, ExpectedVersion: expectedVersion,
		CurrentRevisionID: parent.RevisionID, RecordLockVersion: parent.LockVersion, AuthorizationEpoch: parent.AuthorizationEpoch,
		AuthorizationEvidence: evidence, ReplyToCommentID: replyTo, MentionUserIDs: append([]string(nil), mentions...),
		Idempotency: recordplatform.IdempotencyClaimInputV1{
			Key:                recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: operation, Key: idempotencyKey},
			RequestFingerprint: mustStoreActionFingerprint(t, operation, 0x51), OwnerID: "records_comments_api",
			OwnerLeaseDuration: time.Minute, RecordTTL: 24 * time.Hour,
		},
		ResultFingerprint: mustStoreActionFingerprint(t, operation, 0x52), OutboxTTL: 24 * time.Hour,
	}
	if kind != recordcollaboration.CommentMutationRedact {
		content, err := recordcollaboration.NewCommentContent(body)
		if err != nil {
			t.Fatal(err)
		}
		command.Content = content
	}
	if err := command.Validate(); err != nil {
		t.Fatalf("comment command invalid: %v", err)
	}
	return command
}

func postgresCommentReadCommand(t *testing.T, parent records.RevisionCommitResult, limit uint64) recordcollaboration.CommentReadCommand {
	t.Helper()
	_, evidence := storeActionAuthorization(t)
	return recordcollaboration.CommentReadCommand{
		Actor:    mustPostgresCommentActor(t, "usr_aaaaaaaaaaaaaaaaaaaaaaaa", recordauth.RoleProjectAdmin),
		RecordID: parent.RecordID, CurrentRevisionID: parent.RevisionID, RecordLockVersion: parent.LockVersion,
		AuthorizationEpoch: parent.AuthorizationEpoch, AuthorizationEvidence: evidence, Limit: limit,
	}
}

func mustPostgresCommentActor(t *testing.T, userID string, role recordauth.Role) recordauth.ActorScope {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{UserID: userID, Role: role, ProjectID: recordauth.ProjectIDDefault})
	if err != nil {
		t.Fatal(err)
	}
	return actor
}
