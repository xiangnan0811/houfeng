package store

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
)

func TestPostgresIntegrationRecordWatchLifecycleReplayCASAndSourcePreservation(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "rec_pgwatchflow", "watch-parent-key")
	repository := NewPostgresRecordWatchRepository(
		fixture.openDirectRuntimePool(t, ctx, "record-watch-lifecycle", 3),
		allowRecordPlatformAdmissionGate,
		NewPostgresCollaborationMembershipReader(),
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
	defaultWithSource := postgresWatchCommand(t, parent, 2, recordcollaboration.FollowerPreferenceDefault, "watch-flow-default-source", 0x13)
	status, err = repository.SetWatch(ctx, defaultWithSource)
	if err != nil || status.Version != 3 || status.Preference != recordcollaboration.FollowerPreferenceDefault || !status.Sources.Owner {
		t.Fatalf("SetWatch(default with source) = (%#v, %v), want preserved row/source", status, err)
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
	if err != nil || status.Version != 0 || status.Preference != recordcollaboration.FollowerPreferenceDefault || status.Sources.Any() || !status.UpdatedAt.IsZero() {
		t.Fatalf("SetWatch(unwatch empty) = (%#v, %v), want absent default", status, err)
	}
	if replay, err = repository.SetWatch(ctx, unwatch); err != nil || replay != status {
		t.Fatalf("SetWatch(unwatch replay) = (%#v, %v), want %#v", replay, err, status)
	}

	stale := postgresWatchCommand(t, parent, 4, recordcollaboration.FollowerPreferenceWatching, "watch-flow-stale", 0x16)
	if got, err := repository.SetWatch(ctx, stale); !errors.Is(err, recordcollaboration.ErrWatchConflict) || got != (recordcollaboration.WatchStatus{}) {
		t.Fatalf("SetWatch(stale) = (%#v, %v), want ErrWatchConflict", got, err)
	}
	assertPostgresWatchRowAndKeyCounts(t, ctx, fixture, parent.RecordID, stale.Idempotency.Key.Key, 0, 0)

	read, err := repository.GetWatch(ctx, postgresWatchReadCommand(t, parent))
	if err != nil || read != status {
		t.Fatalf("GetWatch() = (%#v, %v), want %#v", read, err, status)
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
	repository := NewPostgresRecordWatchRepository(
		fixture.openDirectRuntimePool(t, ctx, "record-watch-rollback", 1), gate,
		NewPostgresCollaborationMembershipReader(),
	)
	command := postgresWatchCommand(t, parent, 0, recordcollaboration.FollowerPreferenceWatching, "watch-rollback-key", 0x21)

	if got, err := repository.SetWatch(ctx, command); !errors.Is(err, cutPoint) || got != (recordcollaboration.WatchStatus{}) {
		t.Fatalf("SetWatch(cut point) = (%#v, %v), want rollback cut point", got, err)
	}
	if admissions.Load() != 3 {
		t.Fatalf("admission calls = %d, want write then completion cut point 3", admissions.Load())
	}
	assertPostgresWatchRowAndKeyCounts(t, ctx, fixture, parent.RecordID, command.Idempotency.Key.Key, 0, 0)
}

func TestPostgresIntegrationRecordWatchConcurrentSameCASHasOneWinner(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "rec_pgwatchrace", "watch-race-parent")
	seedRepository := NewPostgresRecordWatchRepository(
		fixture.openDirectRuntimePool(t, ctx, "record-watch-race-seed", 1),
		allowRecordPlatformAdmissionGate, NewPostgresCollaborationMembershipReader(),
	)
	seed := postgresWatchCommand(t, parent, 0, recordcollaboration.FollowerPreferenceWatching, "watch-race-seed", 0x31)
	if _, err := seedRepository.SetWatch(ctx, seed); err != nil {
		t.Fatalf("SetWatch(seed) error = %v", err)
	}

	commands := []recordcollaboration.WatchCommand{
		postgresWatchCommand(t, parent, 1, recordcollaboration.FollowerPreferenceMuted, "watch-race-muted", 0x32),
		postgresWatchCommand(t, parent, 1, recordcollaboration.FollowerPreferenceWatching, "watch-race-watching", 0x33),
	}
	type outcome struct {
		status recordcollaboration.WatchStatus
		err    error
	}
	results := make(chan outcome, len(commands))
	start := make(chan struct{})
	for index, command := range commands {
		repository := NewPostgresRecordWatchRepository(
			fixture.openDirectRuntimePool(t, ctx, fmt.Sprintf("record-watch-race-%d", index), 1),
			allowRecordPlatformAdmissionGate, NewPostgresCollaborationMembershipReader(),
		)
		command := command
		go func() {
			<-start
			status, err := repository.SetWatch(context.Background(), command)
			results <- outcome{status: status, err: err}
		}()
	}
	close(start)
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

func postgresWatchCommand(t *testing.T, parent records.RevisionCommitResult, expected uint64, preference recordcollaboration.FollowerPreference, key string, fingerprintByte byte) recordcollaboration.WatchCommand {
	t.Helper()
	_, evidence := storeActionAuthorization(t)
	actor := collaborationActor(t, "usr_aaaaaaaaaaaaaaaaaaaaaaaa", nil)
	operation := recordplatform.OperationKindRecordWatchPreference
	return recordcollaboration.WatchCommand{
		Actor: actor, RecordID: parent.RecordID, CurrentRevisionID: parent.RevisionID,
		RecordLockVersion: parent.LockVersion, AuthorizationEpoch: parent.AuthorizationEpoch,
		AuthorizationEvidence: evidence, ExpectedVersion: expected, Preference: preference,
		ResultFingerprint: mustStoreActionFingerprint(t, operation, fingerprintByte+0x40),
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
