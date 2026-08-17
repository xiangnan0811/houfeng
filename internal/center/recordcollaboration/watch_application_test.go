package recordcollaboration

import (
	"context"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
)

func TestWatchApplicationInjectsBoundedIdempotencyOptions(t *testing.T) {
	actor, current := testActionAuthorization(t, recordauth.RoleProjectAdmin)
	store := &watchStoreStub{setResult: WatchStatus{RecordID: current.RecordID, UserID: actor.UserID, Version: 1,
		Preference: FollowerPreferenceWatching, RecordFenceEpoch: 2, UpdatedAt: time.Date(2026, 8, 17, 3, 0, 0, 0, time.UTC)}}
	service, err := NewWatchService(&actionCurrentSourceStub{result: current}, store)
	if err != nil {
		t.Fatal(err)
	}
	application, err := NewWatchApplication(service, WatchApplicationOptions{
		IdempotencyOwnerID: "record_watches_api", OwnerLeaseDuration: time.Minute, IdempotencyTTL: 24 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = application.SetWatch(context.Background(), WatchSetApplicationRequest{
		Actor: actor, RecordID: current.RecordID, ExpectedVersion: 0,
		Preference: FollowerPreferenceWatching, IdempotencyKey: "watch-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if store.command.Idempotency.OwnerID != "record_watches_api" || store.command.Idempotency.OwnerLeaseDuration != time.Minute ||
		store.command.Idempotency.RecordTTL != 24*time.Hour {
		t.Fatalf("idempotency = %#v", store.command.Idempotency)
	}
}
