package store

import (
	"context"
	"reflect"
	"testing"

	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/recordplatform"
)

func TestPostgresIntegrationRecordWatchVersionedDefaultAnchorRoundTripsThroughPortability(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	seedCollaborationRevisionUsers(t, ctx, fixture)
	parent := seedPostgresActionParent(t, ctx, fixture, "rec_pgwatchportable", "watch-portable-parent")
	runtimePool := fixture.openDirectRuntimePool(t, ctx, "record-watch-portability", 2)
	watches := NewPostgresRecordWatchRepository(
		runtimePool, allowRecordPlatformAdmissionGate, NewPostgresCollaborationMembershipReader(),
		newPostgresWatchAuthorizationSource(t, runtimePool),
	)
	watch := postgresWatchCommand(t, parent, 0, recordcollaboration.FollowerPreferenceWatching, "watch-portable-watch", 0x61)
	if status, err := watches.SetWatch(ctx, watch); err != nil || status.Version != 1 {
		t.Fatalf("SetWatch(watching) = (%#v, %v)", status, err)
	}
	unwatch := postgresWatchCommand(t, parent, 1, recordcollaboration.FollowerPreferenceDefault, "watch-portable-default", 0x62)
	if status, err := watches.SetWatch(ctx, unwatch); err != nil || status.Version != 2 || status.Sources.Any() {
		t.Fatalf("SetWatch(default anchor) = (%#v, %v)", status, err)
	}

	binding, err := recordcollaboration.NewRecordFenceBinding(recordplatform.ProjectIDDefault, parent.RecordID, 0)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := recordcollaboration.NewPortabilityAdapter(NewPostgresRecordCollaborationProvider())
	if err != nil {
		t.Fatal(err)
	}
	tx, err := runtimePool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := adapter.Backup(ctx, tx, binding)
	if err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("Backup() error = %v", err)
	}
	if len(snapshot.Followers) != 1 || !snapshot.Followers[0].WatchReplayAnchor ||
		snapshot.Followers[0].Version != 2 || snapshot.Followers[0].Preference != recordcollaboration.FollowerPreferenceDefault ||
		snapshot.Followers[0].Sources.Any() {
		_ = tx.Rollback(ctx)
		t.Fatalf("Backup() followers = %#v", snapshot.Followers)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.db.Exec(ctx, `delete from public.record_followers where record_id = $1`, parent.RecordID); err != nil {
		t.Fatalf("clear portable watch anchor: %v", err)
	}

	tx, err = runtimePool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := adapter.Restore(ctx, tx, binding, snapshot); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	restored, err := adapter.Backup(ctx, tx, binding)
	if err != nil {
		t.Fatalf("Backup(restored) error = %v", err)
	}
	if !reflect.DeepEqual(restored, snapshot) {
		t.Fatalf("restored snapshot = %#v, want %#v", restored, snapshot)
	}
}
