package store

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

func TestPostgresIntegrationRecordWatchLifecycleReplayCASAndSourcePreservation(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "rec_pgwatchflow", "watch-parent-key")
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-watch-lifecycle", 3)
	repository := NewPostgresRecordWatchRepository(
		runtimePool, allowRecordPlatformAdmissionGate, NewPostgresCollaborationMembershipReader(),
		newPostgresWatchAuthorizationSource(t, runtimePool),
	)

	watch := postgresWatchCommand(t, parent, 0, recordcollaboration.FollowerPreferenceWatching, "watch-flow-watch", 0x11)
	status, err := repository.SetWatch(ctx, watch)
	if err != nil || status.Version != 1 || status.Preference != recordcollaboration.FollowerPreferenceWatching || status.Sources.Any() {
		t.Fatalf("SetWatch(watching) = (%#v, %v)", status, err)
	}
	replay, err := repository.SetWatch(ctx, watch)
	if err != nil || replay != status {
		t.Fatalf("SetWatch(replay) = (%#v, %v), want %#v", replay, err, status)
	}

	if _, err := fixture.db.Exec(ctx, `
		update public.record_followers
		set follows_owner = true
		where record_id = $1 and user_id = $2`, parent.RecordID, watch.Actor.UserID); err != nil {
		t.Fatalf("seed automatic owner source: %v", err)
	}
	mute := postgresWatchCommand(t, parent, 1, recordcollaboration.FollowerPreferenceMuted, "watch-flow-mute", 0x12)
	status, err = repository.SetWatch(ctx, mute)
	if err != nil || status.Version != 2 || status.Preference != recordcollaboration.FollowerPreferenceMuted || !status.Sources.Owner {
		t.Fatalf("SetWatch(muted) = (%#v, %v), want preserved owner source", status, err)
	}
	if replay, err = repository.SetWatch(ctx, watch); !errors.Is(err, recordplatform.ErrIdempotencyConflictState) || replay != (recordcollaboration.WatchStatus{}) {
		t.Fatalf("SetWatch(first key after later mutation) = (%#v, %v), want content-free fail-closed replay", replay, err)
	}
	defaultWithSource := postgresWatchCommand(t, parent, 2, recordcollaboration.FollowerPreferenceDefault, "watch-flow-default-source", 0x13)
	status, err = repository.SetWatch(ctx, defaultWithSource)
	if err != nil || status.Version != 3 || status.Preference != recordcollaboration.FollowerPreferenceDefault || !status.Sources.Owner {
		t.Fatalf("SetWatch(default with source) = (%#v, %v), want preserved row/source", status, err)
	}
	if replay, err = repository.SetWatch(ctx, defaultWithSource); err != nil || replay != status {
		t.Fatalf("SetWatch(default with source replay) = (%#v, %v), want %#v", replay, err, status)
	}
	watchAgain := postgresWatchCommand(t, parent, 3, recordcollaboration.FollowerPreferenceWatching, "watch-flow-watch-again", 0x14)
	status, err = repository.SetWatch(ctx, watchAgain)
	if err != nil || status.Version != 4 || status.Preference != recordcollaboration.FollowerPreferenceWatching || !status.Sources.Owner {
		t.Fatalf("SetWatch(watching again) = (%#v, %v)", status, err)
	}
	if _, err := fixture.db.Exec(ctx, `
		update public.record_followers
		set follows_owner = false
		where record_id = $1 and user_id = $2`, parent.RecordID, watch.Actor.UserID); err != nil {
		t.Fatalf("remove automatic owner source: %v", err)
	}
	unwatch := postgresWatchCommand(t, parent, 4, recordcollaboration.FollowerPreferenceDefault, "watch-flow-unwatch", 0x15)
	status, err = repository.SetWatch(ctx, unwatch)
	if err != nil || status.Version != 5 || status.Preference != recordcollaboration.FollowerPreferenceDefault || status.Sources.Any() || status.UpdatedAt.IsZero() {
		t.Fatalf("SetWatch(unwatch empty) = (%#v, %v), want retained versioned default", status, err)
	}
	if replay, err = repository.SetWatch(ctx, unwatch); err != nil || replay != status {
		t.Fatalf("SetWatch(unwatch replay) = (%#v, %v), want %#v", replay, err, status)
	}

	stale := postgresWatchCommand(t, parent, 4, recordcollaboration.FollowerPreferenceWatching, "watch-flow-stale", 0x16)
	if got, err := repository.SetWatch(ctx, stale); !errors.Is(err, recordcollaboration.ErrWatchConflict) || got != (recordcollaboration.WatchStatus{}) {
		t.Fatalf("SetWatch(stale) = (%#v, %v), want ErrWatchConflict", got, err)
	}
	assertPostgresWatchRowAndKeyCounts(t, ctx, fixture, parent.RecordID, stale.Idempotency.Key.Key, 1, 0)

	read, err := repository.GetWatch(ctx, postgresWatchReadCommand(t, parent))
	if err != nil || read != status {
		t.Fatalf("GetWatch() = (%#v, %v), want %#v", read, err, status)
	}
}

func TestPostgresIntegrationRecordWatchInitialDefaultCreatesMonotonicReplayAnchor(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "rec_pgwatchinitial", "watch-initial-parent")
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-watch-initial-default", 2)
	repository := NewPostgresRecordWatchRepository(
		runtimePool, allowRecordPlatformAdmissionGate, NewPostgresCollaborationMembershipReader(),
		newPostgresWatchAuthorizationSource(t, runtimePool),
	)
	initial := postgresWatchCommand(t, parent, 0, recordcollaboration.FollowerPreferenceDefault, "watch-initial-default", 0x17)
	status, err := repository.SetWatch(ctx, initial)
	if err != nil || status.Version != 1 || status.Preference != recordcollaboration.FollowerPreferenceDefault || status.Sources.Any() || status.UpdatedAt.IsZero() {
		t.Fatalf("SetWatch(initial default) = (%#v, %v), want versioned replay anchor", status, err)
	}
	var markerBytes int
	if err := fixture.db.QueryRow(ctx, `
		select octet_length(preference_result_fingerprint)
		from public.record_followers
		where record_id = $1 and user_id = $2`, parent.RecordID, initial.Actor.UserID,
	).Scan(&markerBytes); err != nil || markerBytes != 32 {
		t.Fatalf("initial default marker = (%d, %v), want 32 bytes", markerBytes, err)
	}
	if replay, err := repository.SetWatch(ctx, initial); err != nil || replay != status {
		t.Fatalf("SetWatch(initial default replay) = (%#v, %v), want %#v", replay, err, status)
	}

	if _, err := runtimePool.Exec(ctx, `
		update public.record_followers
		set follower_version = follower_version + 1,
		    follows_owner = true,
		    updated_at = transaction_timestamp()
		where record_id = $1 and user_id = $2`, parent.RecordID, initial.Actor.UserID); err != nil {
		t.Fatalf("add automatic source: %v", err)
	}
	if _, err := runtimePool.Exec(ctx, `
		update public.record_followers
		set follower_version = follower_version + 1,
		    follows_owner = false,
		    updated_at = transaction_timestamp()
		where record_id = $1 and user_id = $2`, parent.RecordID, initial.Actor.UserID); err != nil {
		t.Fatalf("remove automatic source: %v", err)
	}
	if replay, err := repository.SetWatch(ctx, initial); !errors.Is(err, recordplatform.ErrIdempotencyConflictState) || replay != (recordcollaboration.WatchStatus{}) {
		t.Fatalf("SetWatch(initial replay after evolution) = (%#v, %v), want fail closed", replay, err)
	}
}

func TestPostgresIntegrationRecordWatchRollsBackMutationWhenCompletionAdmissionFails(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "rec_pgwatchrollback", "watch-rollback-parent")
	cutPoint := errors.New("watch completion admission cut point")
	var admissions atomic.Int32
	gate := AdmissionGateFunc(func(context.Context, pgx.Tx) error {
		if admissions.Add(1) == 3 {
			return cutPoint
		}
		return nil
	})
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-watch-rollback", 1)
	repository := NewPostgresRecordWatchRepository(runtimePool, gate, NewPostgresCollaborationMembershipReader(),
		newPostgresWatchAuthorizationSource(t, runtimePool))
	command := postgresWatchCommand(t, parent, 0, recordcollaboration.FollowerPreferenceWatching, "watch-rollback-key", 0x21)

	if got, err := repository.SetWatch(ctx, command); !errors.Is(err, cutPoint) || got != (recordcollaboration.WatchStatus{}) {
		t.Fatalf("SetWatch(cut point) = (%#v, %v), want rollback cut point", got, err)
	}
	if admissions.Load() != 3 {
		t.Fatalf("admission calls = %d, want write then completion cut point 3", admissions.Load())
	}
	assertPostgresWatchRowAndKeyCounts(t, ctx, fixture, parent.RecordID, command.Idempotency.Key.Key, 0, 0)
}

func TestPostgresIntegrationRecordWatchReplayFailsClosedAfterAutomaticSourceMutation(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "rec_pgwatchsource", "watch-source-parent")
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-watch-source", 2)
	repository := NewPostgresRecordWatchRepository(
		runtimePool, allowRecordPlatformAdmissionGate, NewPostgresCollaborationMembershipReader(),
		newPostgresWatchAuthorizationSource(t, runtimePool),
	)
	watch := postgresWatchCommand(t, parent, 0, recordcollaboration.FollowerPreferenceWatching, "watch-source-watch", 0x29)
	if status, err := repository.SetWatch(ctx, watch); err != nil || status.Version != 1 || status.Sources.Any() {
		t.Fatalf("SetWatch(watching) = (%#v, %v)", status, err)
	}
	action := postgresActionCommand(t, parent, recordcollaboration.ActionMutationCreate, "ract_pgwatchsource", 0,
		mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{Title: "Automatic watch source"}), "watch-source-action")
	action.Actor = watch.Actor
	if _, err := newPostgresActionRepositoryForTest(t, runtimePool, allowRecordPlatformAdmissionGate).CommitAction(ctx, action); err != nil {
		t.Fatalf("CommitAction() error = %v", err)
	}
	var version, keyCount int
	var followsAction bool
	var updatedAt time.Time
	if err := fixture.db.QueryRow(ctx, `
		select follower_version::int, follows_action, updated_at,
		       (select count(*)::int from public.record_idempotency_keys where idempotency_key = $3)
		from public.record_followers
		where record_id = $1 and user_id = $2`, parent.RecordID, watch.Actor.UserID, watch.Idempotency.Key.Key).Scan(
		&version, &followsAction, &updatedAt, &keyCount,
	); err != nil {
		t.Fatalf("read automatic source mutation: %v", err)
	}
	if version != 2 || !followsAction || keyCount != 1 {
		t.Fatalf("automatic source version/action/keys = %d/%v/%d, want 2/true/1", version, followsAction, keyCount)
	}
	if replay, err := repository.SetWatch(ctx, watch); !errors.Is(err, recordplatform.ErrIdempotencyConflictState) || replay != (recordcollaboration.WatchStatus{}) {
		t.Fatalf("SetWatch(replay after automatic source) = (%#v, %v), want fail closed", replay, err)
	}
	var afterVersion, afterKeyCount int
	var afterAction bool
	var afterUpdatedAt time.Time
	if err := fixture.db.QueryRow(ctx, `
		select follower_version::int, follows_action, updated_at,
		       (select count(*)::int from public.record_idempotency_keys where idempotency_key = $3)
		from public.record_followers
		where record_id = $1 and user_id = $2`, parent.RecordID, watch.Actor.UserID, watch.Idempotency.Key.Key).Scan(
		&afterVersion, &afterAction, &afterUpdatedAt, &afterKeyCount,
	); err != nil {
		t.Fatalf("read state after failed replay: %v", err)
	}
	if afterVersion != version || afterAction != followsAction || !afterUpdatedAt.Equal(updatedAt) || afterKeyCount != keyCount {
		t.Fatalf("failed replay changed follower/key state: before=%d/%v/%s/%d after=%d/%v/%s/%d",
			version, followsAction, updatedAt, keyCount, afterVersion, afterAction, afterUpdatedAt, afterKeyCount)
	}
}

func TestPostgresIntegrationRecordWatchReplayFailsClosedAfterAutomaticSourcesEvolveRetainedDefault(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "rec_pgwatchreuse", "watch-reuse-parent")
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-watch-reuse", 2)
	repository := NewPostgresRecordWatchRepository(
		runtimePool, allowRecordPlatformAdmissionGate, NewPostgresCollaborationMembershipReader(),
		newPostgresWatchAuthorizationSource(t, runtimePool),
	)

	watch := postgresWatchCommand(t, parent, 0, recordcollaboration.FollowerPreferenceWatching, "watch-reuse-watch", 0x2a)
	if status, err := repository.SetWatch(ctx, watch); err != nil || status.Version != 1 || status.Sources.Any() {
		t.Fatalf("SetWatch(watching) = (%#v, %v)", status, err)
	}
	unwatch := postgresWatchCommand(t, parent, 1, recordcollaboration.FollowerPreferenceDefault, "watch-reuse-unwatch", 0x2b)
	if status, err := repository.SetWatch(ctx, unwatch); err != nil || status.Version != 2 || status.Sources.Any() {
		t.Fatalf("SetWatch(default) = (%#v, %v)", status, err)
	}

	action := postgresActionCommand(t, parent, recordcollaboration.ActionMutationCreate, "ract_pgwatchreuse", 0,
		mustPostgresActionFields(t, recordcollaboration.ActionFieldValues{Title: "Recreate automatic source"}), "watch-reuse-action")
	action.Actor = watch.Actor
	if _, err := newPostgresActionRepositoryForTest(t, runtimePool, allowRecordPlatformAdmissionGate).CommitAction(ctx, action); err != nil {
		t.Fatalf("CommitAction() error = %v", err)
	}
	comment := postgresCommentCommand(t, parent, recordcollaboration.CommentMutationCreate, "rcm_pgwatchreuse", 0,
		"Second automatic source", "", nil, "watch-reuse-comment")
	comment.Actor = watch.Actor
	if _, err := newPostgresCommentRepositoryForTest(t, runtimePool, allowRecordPlatformAdmissionGate).CommitComment(ctx, comment); err != nil {
		t.Fatalf("CommitComment() error = %v", err)
	}

	var version, keyCount int
	var preference string
	var followsAction, followsComment bool
	var updatedAt time.Time
	if err := fixture.db.QueryRow(ctx, `
		select follower_version::int, manual_preference, follows_action, follows_comment, updated_at,
		       (select count(*)::int from public.record_idempotency_keys where idempotency_key = $3)
		from public.record_followers
		where record_id = $1 and user_id = $2`, parent.RecordID, watch.Actor.UserID, unwatch.Idempotency.Key.Key).Scan(
		&version, &preference, &followsAction, &followsComment, &updatedAt, &keyCount,
	); err != nil {
		t.Fatalf("read rebuilt automatic sources: %v", err)
	}
	if version != 4 || preference != string(recordcollaboration.FollowerPreferenceDefault) || !followsAction || !followsComment || keyCount != 1 {
		t.Fatalf("evolved follower version/preference/action/comment/keys = %d/%q/%v/%v/%d, want 4/default/true/true/1",
			version, preference, followsAction, followsComment, keyCount)
	}

	if replay, err := repository.SetWatch(ctx, unwatch); !errors.Is(err, recordplatform.ErrIdempotencyConflictState) || replay != (recordcollaboration.WatchStatus{}) {
		t.Fatalf("SetWatch(replay after row reuse) = (%#v, %v), want fail closed", replay, err)
	}
	var afterVersion, afterKeyCount int
	var afterPreference string
	var afterAction, afterComment bool
	var afterUpdatedAt time.Time
	if err := fixture.db.QueryRow(ctx, `
		select follower_version::int, manual_preference, follows_action, follows_comment, updated_at,
		       (select count(*)::int from public.record_idempotency_keys where idempotency_key = $3)
		from public.record_followers
		where record_id = $1 and user_id = $2`, parent.RecordID, watch.Actor.UserID, unwatch.Idempotency.Key.Key).Scan(
		&afterVersion, &afterPreference, &afterAction, &afterComment, &afterUpdatedAt, &afterKeyCount,
	); err != nil {
		t.Fatalf("read state after failed replay: %v", err)
	}
	if afterVersion != version || afterPreference != preference || afterAction != followsAction || afterComment != followsComment ||
		!afterUpdatedAt.Equal(updatedAt) || afterKeyCount != keyCount {
		t.Fatalf("failed replay changed rebuilt follower/key state: before=%d/%q/%v/%v/%s/%d after=%d/%q/%v/%v/%s/%d",
			version, preference, followsAction, followsComment, updatedAt, keyCount,
			afterVersion, afterPreference, afterAction, afterComment, afterUpdatedAt, afterKeyCount)
	}
}

func TestPostgresIntegrationRecordWatchReplayKeepsVersionsMonotonicAcrossUnwatch(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "rec_pgwatchsame", "watch-same-parent")
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-watch-same", 2)
	repository := NewPostgresRecordWatchRepository(
		runtimePool, allowRecordPlatformAdmissionGate, NewPostgresCollaborationMembershipReader(),
		newPostgresWatchAuthorizationSource(t, runtimePool),
	)

	first := postgresWatchCommand(t, parent, 0, recordcollaboration.FollowerPreferenceWatching, "watch-same-first", 0x2c)
	if status, err := repository.SetWatch(ctx, first); err != nil || status.Version != 1 || status.Preference != recordcollaboration.FollowerPreferenceWatching {
		t.Fatalf("SetWatch(first) = (%#v, %v)", status, err)
	}
	remove := postgresWatchCommand(t, parent, 1, recordcollaboration.FollowerPreferenceDefault, "watch-same-remove", 0x2d)
	if status, err := repository.SetWatch(ctx, remove); err != nil || status.Version != 2 {
		t.Fatalf("SetWatch(remove) = (%#v, %v)", status, err)
	}
	second := postgresWatchCommand(t, parent, 2, recordcollaboration.FollowerPreferenceWatching, "watch-same-second", 0x2e)
	secondStatus, err := repository.SetWatch(ctx, second)
	if err != nil || secondStatus.Version != 3 || secondStatus.Preference != recordcollaboration.FollowerPreferenceWatching {
		t.Fatalf("SetWatch(second) = (%#v, %v)", secondStatus, err)
	}

	if replay, err := repository.SetWatch(ctx, first); !errors.Is(err, recordplatform.ErrIdempotencyConflictState) || replay != (recordcollaboration.WatchStatus{}) {
		t.Fatalf("SetWatch(first replay after identical recreation) = (%#v, %v), want fail closed", replay, err)
	}
	current, err := repository.GetWatch(ctx, postgresWatchReadCommand(t, parent))
	if err != nil || current != secondStatus {
		t.Fatalf("GetWatch() = (%#v, %v), want second state %#v", current, err, secondStatus)
	}
}

func TestPostgresIntegrationRecordWatchReplayRejectsRetainedDefaultAfterLaterPrune(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "rec_pgwatchprune", "watch-prune-parent")
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-watch-prune", 2)
	repository := NewPostgresRecordWatchRepository(
		runtimePool, allowRecordPlatformAdmissionGate, NewPostgresCollaborationMembershipReader(),
		newPostgresWatchAuthorizationSource(t, runtimePool),
	)

	watch := postgresWatchCommand(t, parent, 0, recordcollaboration.FollowerPreferenceWatching, "watch-prune-watch", 0x2f)
	if status, err := repository.SetWatch(ctx, watch); err != nil || status.Version != 1 {
		t.Fatalf("SetWatch(watching) = (%#v, %v)", status, err)
	}
	if _, err := fixture.db.Exec(ctx, `
		update public.record_followers
		set follows_owner = true
		where record_id = $1 and user_id = $2`, parent.RecordID, watch.Actor.UserID); err != nil {
		t.Fatalf("seed revision follower source: %v", err)
	}
	retained := postgresWatchCommand(t, parent, 1, recordcollaboration.FollowerPreferenceDefault, "watch-prune-default", 0x30)
	retainedStatus, err := repository.SetWatch(ctx, retained)
	if err != nil || retainedStatus.Version != 2 || retainedStatus.Preference != recordcollaboration.FollowerPreferenceDefault || !retainedStatus.Sources.Owner {
		t.Fatalf("SetWatch(retained default) = (%#v, %v)", retainedStatus, err)
	}
	if replay, err := repository.SetWatch(ctx, retained); err != nil || replay != retainedStatus {
		t.Fatalf("SetWatch(retained default replay) = (%#v, %v), want %#v", replay, err, retainedStatus)
	}
	encoded, err := encodeRecordPurgeCommand(collaborationPruneRevisionFollowersFunctionCommand{
		RecordID: parent.RecordID, KeepUserIDs: []string{}, FenceEpoch: 0,
	})
	if err != nil {
		t.Fatalf("encode prune command: %v", err)
	}
	var removed int64
	if err := runtimePool.QueryRow(ctx, `select public.record_collaboration_prune_revision_followers($1)`, encoded).Scan(&removed); err != nil {
		t.Fatalf("prune retained default row: %v", err)
	}
	if removed != 0 {
		t.Fatalf("pruned rows = %d, want watch replay anchor retained", removed)
	}
	if _, err := runtimePool.Exec(ctx, `
		update public.record_followers
		set follower_version = follower_version + 1,
		    follows_owner = false,
		    updated_at = transaction_timestamp()
		where record_id = $1 and user_id = $2`, parent.RecordID, watch.Actor.UserID); err != nil {
		t.Fatalf("clear stale revision source: %v", err)
	}

	if replay, err := repository.SetWatch(ctx, retained); !errors.Is(err, recordplatform.ErrIdempotencyConflictState) || replay != (recordcollaboration.WatchStatus{}) {
		t.Fatalf("SetWatch(retained replay after prune) = (%#v, %v), want fail closed", replay, err)
	}
}

func TestPostgresIntegrationRecordWatchRefreshesAuthorizationInsideTransactionForWriteReadAndReplay(t *testing.T) {
	for _, failure := range []struct {
		name    string
		step    func(*testing.T, records.CompleteRevisionInput) watchSubjectResolutionStep
		wantErr error
	}{
		{
			name: "revoked",
			step: func(t *testing.T, input records.CompleteRevisionInput) watchSubjectResolutionStep {
				capture := input.Subjects()[0].CaptureAuthorization
				denied := collaborationSourceAuthorization(t, capture.CaptureScope,
					collaborationVisibility(t, recordauth.VisibilityKindRestricted, nil), recordauth.SourceStateLive)
				return watchSubjectResolutionStep{resolved: watchResolvedSubject(t, input, denied)}
			},
			wantErr: recordauth.ErrDenied,
		},
		{
			name: "unavailable",
			step: func(*testing.T, records.CompleteRevisionInput) watchSubjectResolutionStep {
				return watchSubjectResolutionStep{err: ErrRecordSubjectUnavailable}
			},
			wantErr: ErrRecordSubjectUnavailable,
		},
	} {
		for _, operation := range []string{"write", "read", "replay"} {
			t.Run(failure.name+"/"+operation, func(t *testing.T) {
				ctx := context.Background()
				fixture := newRecordsPostgresFixture(t, ctx)
				seedCollaborationRevisionUsers(t, ctx, fixture)
				input := collaborationRevisionInput(t, collaborationRevisionInputValues{})
				runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-watch-auth-"+failure.name+"-"+operation, 2)
				parent, err := newRecordsPostgresRepository(t, runtimePool).CommitRevision(ctx, recordsPostgresRevisionCommand(
					t, recordplatform.OperationKindRecordCreate, "rec_pgwatchauth"+failure.name+operation, "", 0, 0,
					input, "watch-auth-parent-"+failure.name+"-"+operation,
				))
				if err != nil {
					t.Fatalf("CommitRevision(parent) error = %v", err)
				}
				allowed := watchSubjectResolutionStep{resolved: watchResolvedSubject(t, input, input.Subjects()[0].CaptureAuthorization)}
				steps := []watchSubjectResolutionStep{allowed, failure.step(t, input)}
				if operation == "replay" {
					steps = []watchSubjectResolutionStep{allowed, allowed, allowed, failure.step(t, input)}
				}
				resolver := &sequencedWatchSubjectResolver{steps: steps}
				authorizations := newPostgresCurrentRecordAuthorizationSource(runtimePool, resolver, allowRecordPlatformAdmissionGate)
				repository := NewPostgresRecordWatchRepository(
					runtimePool, allowRecordPlatformAdmissionGate, NewPostgresCollaborationMembershipReader(), authorizations,
				)
				service, err := recordcollaboration.NewWatchService(authorizations, repository)
				if err != nil {
					t.Fatalf("NewWatchService() error = %v", err)
				}
				actor := collaborationActor(t, "usr_aaaaaaaaaaaaaaaaaaaaaaaa", nil)
				setRequest := recordcollaboration.WatchSetRequest{
					Actor: actor, RecordID: parent.RecordID, ExpectedVersion: 0,
					Preference: recordcollaboration.FollowerPreferenceWatching, IdempotencyKey: "watch-auth-" + failure.name + "-" + operation,
					IdempotencyOwnerID: "record_watches_api", OwnerLeaseDuration: time.Minute, IdempotencyTTL: 24 * time.Hour,
				}
				if operation == "replay" {
					if seeded, err := service.SetWatch(ctx, setRequest); err != nil || seeded.Version != 1 {
						t.Fatalf("seed SetWatch() = (%#v, %v)", seeded, err)
					}
				}
				var operationErr error
				switch operation {
				case "read":
					_, operationErr = service.GetWatch(ctx, recordcollaboration.WatchReadRequest{Actor: actor, RecordID: parent.RecordID})
				default:
					_, operationErr = service.SetWatch(ctx, setRequest)
				}
				if !errors.Is(operationErr, failure.wantErr) {
					t.Fatalf("%s authorization race error = %v, want %v", operation, operationErr, failure.wantErr)
				}
				wantCalls := 2
				wantRows, wantKeys := 0, 0
				if operation == "replay" {
					wantCalls, wantRows, wantKeys = 4, 1, 1
				}
				if resolver.calls != wantCalls {
					t.Fatalf("%s resolver calls = %d, want outside+inside %d", operation, resolver.calls, wantCalls)
				}
				assertPostgresWatchRowAndKeyCounts(t, ctx, fixture, parent.RecordID, setRequest.IdempotencyKey, wantRows, wantKeys)
			})
		}
	}
}

func TestPostgresIntegrationRecordWatchActorMembershipDenialIsOpaque(t *testing.T) {
	for _, membership := range []string{"missing", "demoted"} {
		for _, operation := range []string{"write", "read"} {
			t.Run(membership+"/"+operation, func(t *testing.T) {
				ctx := context.Background()
				fixture := newRecordsPostgresFixture(t, ctx)
				seedCollaborationRevisionUsers(t, ctx, fixture)
				parent := seedPostgresActionParent(t, ctx, fixture,
					"rec_pgwatchmember"+membership+operation, "watch-member-parent-"+membership+"-"+operation)
				runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-watch-member-"+membership+"-"+operation, 2)
				repository := NewPostgresRecordWatchRepository(
					runtimePool, allowRecordPlatformAdmissionGate, NewPostgresCollaborationMembershipReader(),
					newPostgresWatchAuthorizationSource(t, runtimePool),
				)
				actor := collaborationActor(t, "usr_aaaaaaaaaaaaaaaaaaaaaaaa", nil)
				if membership == "missing" {
					actor = collaborationActor(t, "usr_eeeeeeeeeeeeeeeeeeeeeeee", nil)
				} else if _, err := fixture.db.Exec(ctx, `update public.users set role = 'viewer' where user_id = $1`, actor.UserID); err != nil {
					t.Fatalf("demote collaboration actor: %v", err)
				}
				key := "watch-member-" + membership + "-" + operation
				var operationErr error
				if operation == "write" {
					command := postgresWatchCommand(t, parent, 0, recordcollaboration.FollowerPreferenceWatching, key, 0x3a)
					command.Actor = actor
					_, operationErr = repository.SetWatch(ctx, command)
				} else {
					command := postgresWatchReadCommand(t, parent)
					command.Actor = actor
					_, operationErr = repository.GetWatch(ctx, command)
				}
				if !errors.Is(operationErr, recordauth.ErrDenied) || errors.Is(operationErr, recordcollaboration.ErrMembershipDenied) {
					t.Fatalf("%s %s actor error = %v, want opaque recordauth.ErrDenied", membership, operation, operationErr)
				}
				assertPostgresWatchRowAndKeyCounts(t, ctx, fixture, parent.RecordID, key, 0, 0)
			})
		}
	}
}

func TestPostgresIntegrationRecordWatchConcurrentSameCASHasOneWinner(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "rec_pgwatchrace", "watch-race-parent")
	seedPool := fixture.openDirectRuntimePool(t, ctx, "record-watch-race-seed", 1)
	seedRepository := NewPostgresRecordWatchRepository(
		seedPool, allowRecordPlatformAdmissionGate, NewPostgresCollaborationMembershipReader(),
		newPostgresWatchAuthorizationSource(t, seedPool),
	)
	seed := postgresWatchCommand(t, parent, 0, recordcollaboration.FollowerPreferenceWatching, "watch-race-seed", 0x31)
	if _, err := seedRepository.SetWatch(ctx, seed); err != nil {
		t.Fatalf("SetWatch(seed) error = %v", err)
	}

	commands := []recordcollaboration.WatchCommand{
		postgresWatchCommand(t, parent, 1, recordcollaboration.FollowerPreferenceMuted, "watch-race-muted", 0x32),
		postgresWatchCommand(t, parent, 1, recordcollaboration.FollowerPreferenceWatching, "watch-race-watching", 0x33),
	}
	for _, statement := range []string{
		`create table public.test_record_watch_overlap_latch (latch_id integer primary key)`,
		`insert into public.test_record_watch_overlap_latch (latch_id) values (1)`,
		`create function record_platform_internal.test_record_watch_overlap() returns trigger
		 language plpgsql security definer set search_path = pg_catalog as $$
		 begin
		   perform latch_id from public.test_record_watch_overlap_latch where latch_id = 1 for update;
		   return new;
		 end
		 $$`,
		`create trigger test_record_watch_overlap after update on public.record_followers
		 for each row when (new.record_id = 'rec_pgwatchrace')
		 execute function record_platform_internal.test_record_watch_overlap()`,
	} {
		if _, err := fixture.db.Exec(ctx, statement); err != nil {
			t.Fatalf("install watch overlap latch: %v", err)
		}
	}
	controlTx, err := fixture.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = controlTx.Rollback(context.Background()) }()
	var controlPID, latchID int
	if err := controlTx.QueryRow(ctx, `select pg_backend_pid()`).Scan(&controlPID); err != nil {
		t.Fatal(err)
	}
	if err := controlTx.QueryRow(ctx, `select latch_id from public.test_record_watch_overlap_latch where latch_id = 1 for update`).Scan(&latchID); err != nil || latchID != 1 {
		t.Fatalf("lock watch overlap latch = %d/%v", latchID, err)
	}
	type outcome struct {
		status recordcollaboration.WatchStatus
		err    error
	}
	results := make(chan outcome, len(commands))
	repositories := make([]*PostgresRecordWatchRepository, 0, len(commands))
	workerPIDs := make([]int, 0, len(commands))
	for index := range commands {
		pool := fixture.openDirectRuntimePool(t, ctx, fmt.Sprintf("record-watch-race-%d", index), 1)
		var pid int
		if err := pool.QueryRow(ctx, `select pg_backend_pid()`).Scan(&pid); err != nil {
			t.Fatal(err)
		}
		workerPIDs = append(workerPIDs, pid)
		repositories = append(repositories, NewPostgresRecordWatchRepository(
			pool, allowRecordPlatformAdmissionGate, NewPostgresCollaborationMembershipReader(),
			newPostgresWatchAuthorizationSource(t, pool),
		))
	}
	raceCtx, cancelRace := context.WithTimeout(ctx, 10*time.Second)
	defer cancelRace()
	for index := range commands {
		index := index
		go func() {
			status, err := repositories[index].SetWatch(raceCtx, commands[index])
			results <- outcome{status: status, err: err}
		}()
		if index == 0 {
			waitForPostgresBlockingPID(t, raceCtx, fixture.db, workerPIDs[0], controlPID)
		} else {
			waitForPostgresBlockingPID(t, raceCtx, fixture.db, workerPIDs[1], workerPIDs[0])
		}
	}
	if err := controlTx.Commit(ctx); err != nil {
		t.Fatalf("release watch overlap latch: %v", err)
	}
	winners, conflicts := 0, 0
	for range commands {
		result := <-results
		switch {
		case result.err == nil && result.status.Version == 2:
			winners++
		case errors.Is(result.err, recordcollaboration.ErrWatchConflict) && result.status == (recordcollaboration.WatchStatus{}):
			conflicts++
		default:
			t.Fatalf("concurrent SetWatch() = (%#v, %v)", result.status, result.err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("concurrent outcomes winners/conflicts = %d/%d, want 1/1", winners, conflicts)
	}
	var version, keyCount int
	if err := fixture.db.QueryRow(ctx, `
		select follower_version::int,
		       (select count(*)::int from public.record_idempotency_keys
		        where idempotency_key in ('watch-race-muted', 'watch-race-watching'))
		from public.record_followers
		where record_id = $1 and user_id = $2`, parent.RecordID, seed.Actor.UserID).Scan(&version, &keyCount); err != nil {
		t.Fatalf("read concurrent watch state: %v", err)
	}
	if version != 2 || keyCount != 1 {
		t.Fatalf("concurrent durable version/keys = %d/%d, want 2/1", version, keyCount)
	}
}

type watchSubjectResolutionStep struct {
	resolved records.ResolvedSubject
	err      error
}

type sequencedWatchSubjectResolver struct {
	steps []watchSubjectResolutionStep
	calls int
}

func (resolver *sequencedWatchSubjectResolver) Resolve(
	_ context.Context,
	_ recordauth.ActorScope,
	_ RecordSubjectReadInput,
) (records.ResolvedSubject, error) {
	resolver.calls++
	if resolver.calls > len(resolver.steps) {
		return records.ResolvedSubject{}, ErrRecordSubjectUnavailable
	}
	step := resolver.steps[resolver.calls-1]
	return step.resolved, step.err
}

func watchResolvedSubject(t *testing.T, input records.CompleteRevisionInput, authorization recordauth.SourceAuthorization) records.ResolvedSubject {
	t.Helper()
	subject := input.Subjects()[0]
	identity, err := records.NewSubjectIdentitySnapshot(subject.Kind, subject.IdentitySnapshot)
	if err != nil {
		t.Fatalf("NewSubjectIdentitySnapshot() error = %v", err)
	}
	return records.ResolvedSubject{
		ProjectID: recordauth.ProjectIDDefault, StableID: subject.SourceID,
		IdentitySnapshot: identity, LiveRoute: "/vps/" + subject.SourceID,
		CaptureAuthorization: authorization,
	}
}

func newPostgresWatchAuthorizationSource(t *testing.T, pool *pgxpool.Pool) *PostgresCurrentRecordAuthorizationSource {
	t.Helper()
	_, evidence := storeActionAuthorization(t)
	identity, err := records.NewSubjectIdentitySnapshot(records.SubjectKindVPS, map[string]string{"display_name": "VPS"})
	if err != nil {
		t.Fatalf("NewSubjectIdentitySnapshot() error = %v", err)
	}
	resolver := &fakeCurrentRecordSubjectResolver{resolved: records.ResolvedSubject{
		ProjectID: recordauth.ProjectIDDefault, StableID: testStoreRecordVPSID,
		IdentitySnapshot: identity, LiveRoute: "/vps/" + testStoreRecordVPSID,
		CaptureAuthorization: evidence.Sources[0],
	}}
	return newPostgresCurrentRecordAuthorizationSource(pool, resolver, allowRecordPlatformAdmissionGate)
}

func postgresWatchCommand(t *testing.T, parent records.RevisionCommitResult, expected uint64, preference recordcollaboration.FollowerPreference, key string, fingerprintByte byte) recordcollaboration.WatchCommand {
	t.Helper()
	_, evidence := storeActionAuthorization(t)
	actor := collaborationActor(t, "usr_aaaaaaaaaaaaaaaaaaaaaaaa", nil)
	operation := recordplatform.OperationKindRecordWatchPreference
	return recordcollaboration.WatchCommand{
		Actor: actor, RecordID: parent.RecordID, CurrentRevisionID: parent.RevisionID,
		RecordLockVersion: parent.LockVersion, AuthorizationEpoch: parent.AuthorizationEpoch,
		AuthorizationEvidence: evidence, ExpectedVersion: expected, Preference: preference,
		Idempotency: recordplatform.IdempotencyClaimInputV1{
			Key:                recordplatform.IdempotencyKey{ProjectID: recordplatform.ProjectIDDefault, OperationKind: operation, Key: key},
			RequestFingerprint: mustStoreActionFingerprint(t, operation, fingerprintByte),
			OwnerID:            "record_watch_api", OwnerLeaseDuration: time.Minute, RecordTTL: 24 * time.Hour,
		},
	}
}

func postgresWatchReadCommand(t *testing.T, parent records.RevisionCommitResult) recordcollaboration.WatchReadCommand {
	t.Helper()
	_, evidence := storeActionAuthorization(t)
	actor := collaborationActor(t, "usr_aaaaaaaaaaaaaaaaaaaaaaaa", nil)
	return recordcollaboration.WatchReadCommand{
		Actor: actor, RecordID: parent.RecordID, CurrentRevisionID: parent.RevisionID,
		RecordLockVersion: parent.LockVersion, AuthorizationEpoch: parent.AuthorizationEpoch,
		AuthorizationEvidence: evidence,
	}
}

func assertPostgresWatchRowAndKeyCounts(t *testing.T, ctx context.Context, fixture recordPlatformPostgresFixture, recordID, key string, wantRows, wantKeys int) {
	t.Helper()
	var rows, keys int
	if err := fixture.db.QueryRow(ctx, `
		select (select count(*)::int from public.record_followers where record_id = $1),
		       (select count(*)::int from public.record_idempotency_keys where idempotency_key = $2)`,
		recordID, key,
	).Scan(&rows, &keys); err != nil {
		t.Fatalf("count watch rows and idempotency keys: %v", err)
	}
	if rows != wantRows || keys != wantKeys {
		t.Fatalf("watch rows/keys = %d/%d, want %d/%d", rows, keys, wantRows, wantKeys)
	}
}

type postgresBlockingPIDQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func waitForPostgresBlockingPID(t *testing.T, ctx context.Context, db postgresBlockingPIDQueryer, blockedPID, blockerPID int) {
	t.Helper()
	for {
		var observed bool
		err := db.QueryRow(ctx, `select $2::integer = any(pg_blocking_pids($1::integer))`, blockedPID, blockerPID).Scan(&observed)
		if err != nil {
			t.Fatalf("observe PostgreSQL blocker %d <- %d: %v", blockedPID, blockerPID, err)
		}
		if observed {
			return
		}
		if err := ctx.Err(); err != nil {
			t.Fatalf("PostgreSQL blocker %d <- %d not observed: %v", blockedPID, blockerPID, err)
		}
	}
}
