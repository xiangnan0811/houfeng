package recordcollaboration

import (
	"context"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
)

func TestWatchServiceSetsIndependentCASPreferenceWithContentFreeIdempotency(t *testing.T) {
	actor, current := testActionAuthorization(t, recordauth.RoleProjectAdmin)
	store := &watchStoreStub{setResult: WatchStatus{
		RecordID: current.RecordID, UserID: actor.UserID, Version: 3,
		Preference: FollowerPreferenceMuted, Sources: FollowerSources{Owner: true},
		RecordFenceEpoch: 2, UpdatedAt: time.Date(2026, 8, 17, 3, 0, 0, 0, time.UTC),
	}}
	service, err := NewWatchService(&actionCurrentSourceStub{result: current}, store)
	if err != nil {
		t.Fatalf("NewWatchService() error = %v", err)
	}
	result, err := service.SetWatch(context.Background(), WatchSetRequest{
		Actor: actor, RecordID: current.RecordID, ExpectedVersion: 2,
		Preference: FollowerPreferenceMuted, IdempotencyKey: "watch-mute-1",
		IdempotencyOwnerID: "record_watches_api", OwnerLeaseDuration: time.Minute, IdempotencyTTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("SetWatch() error = %v", err)
	}
	if result != store.setResult || store.setCalls != 1 {
		t.Fatalf("result/calls = %#v/%d", result, store.setCalls)
	}
	command := store.command
	if command.RecordID != current.RecordID || command.Actor.UserID != actor.UserID ||
		command.CurrentRevisionID != current.CurrentRevisionID || command.RecordLockVersion != current.LockVersion ||
		command.AuthorizationEpoch != current.AuthorizationEpoch || command.ExpectedVersion != 2 ||
		command.Preference != FollowerPreferenceMuted || command.Idempotency.Key.OperationKind != recordplatform.OperationKindRecordWatchPreference ||
		command.Idempotency.Key.Key != "watch-mute-1" || command.Idempotency.RequestFingerprint.Validate() != nil {
		t.Fatalf("command = %#v", command)
	}
}

func TestWatchServiceReadsFreshAuthorizedStatusAndReturnsDefensiveFacts(t *testing.T) {
	actor, current := testActionAuthorization(t, recordauth.RoleProjectAdmin)
	store := &watchStoreStub{getResult: WatchStatus{
		RecordID: current.RecordID, UserID: actor.UserID, Version: 1,
		Preference: FollowerPreferenceDefault, Sources: FollowerSources{Participant: true},
		RecordFenceEpoch: 2, UpdatedAt: time.Date(2026, 8, 17, 3, 0, 0, 0, time.UTC),
	}}
	service, err := NewWatchService(&actionCurrentSourceStub{result: current}, store)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.GetWatch(context.Background(), WatchReadRequest{Actor: actor, RecordID: current.RecordID})
	if err != nil {
		t.Fatalf("GetWatch() error = %v", err)
	}
	if result != store.getResult || store.getCalls != 1 || store.read.Actor.UserID != actor.UserID ||
		store.read.AuthorizationEpoch != current.AuthorizationEpoch {
		t.Fatalf("result/read/calls = %#v/%#v/%d", result, store.read, store.getCalls)
	}
}

func TestWatchStatusResultFingerprintBindsExactStateAndIdempotencyKey(t *testing.T) {
	status := WatchStatus{
		RecordID: "rec_watchfingerprint", UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", Version: 2,
		Preference: FollowerPreferenceDefault, Sources: FollowerSources{Action: true},
		RecordFenceEpoch: 7, UpdatedAt: time.Date(2026, 8, 17, 3, 4, 5, 678000000, time.UTC),
	}
	firstKey := recordplatform.IdempotencyKey{
		ProjectID: recordplatform.ProjectIDDefault, OperationKind: recordplatform.OperationKindRecordWatchPreference,
		Key: "watch-result-first",
	}
	secondKey := firstKey
	secondKey.Key = "watch-result-second"
	first, err := status.ResultFingerprint(firstKey)
	if err != nil {
		t.Fatalf("ResultFingerprint(first) error = %v", err)
	}
	if repeated, err := status.ResultFingerprint(firstKey); err != nil || !first.Equal(repeated) {
		t.Fatalf("ResultFingerprint(repeated) = (%#v, %v), want stable", repeated, err)
	}
	second, err := status.ResultFingerprint(secondKey)
	if err != nil {
		t.Fatalf("ResultFingerprint(second) error = %v", err)
	}
	if first.Equal(second) {
		t.Fatal("ResultFingerprint() did not bind the idempotency key")
	}
	changed := status
	changed.Sources.Comment = true
	changedFingerprint, err := changed.ResultFingerprint(firstKey)
	if err != nil {
		t.Fatalf("ResultFingerprint(changed) error = %v", err)
	}
	if first.Equal(changedFingerprint) {
		t.Fatal("ResultFingerprint() did not bind the exact current status")
	}
}

func TestWatchStatusAllowsVersionedDefaultWithoutSourcesAsReplayAnchor(t *testing.T) {
	status := WatchStatus{
		RecordID: "rec_watchanchor", UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", Version: 2,
		Preference: FollowerPreferenceDefault, RecordFenceEpoch: 7,
		UpdatedAt: time.Date(2026, 8, 17, 3, 4, 5, 678000000, time.UTC),
	}
	if err := status.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

type watchStoreStub struct {
	command   WatchCommand
	read      WatchReadCommand
	setResult WatchStatus
	getResult WatchStatus
	setErr    error
	getErr    error
	setCalls  int
	getCalls  int
}

func (store *watchStoreStub) SetWatch(_ context.Context, command WatchCommand) (WatchStatus, error) {
	store.setCalls++
	store.command = command
	return store.setResult, store.setErr
}

func (store *watchStoreStub) GetWatch(_ context.Context, command WatchReadCommand) (WatchStatus, error) {
	store.getCalls++
	store.read = command
	return store.getResult, store.getErr
}
