package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/recordcollaboration"
)

func TestPostgresRecordWatchRepositoryFailsClosedWithoutAdmission(t *testing.T) {
	repository := NewPostgresRecordWatchRepository(nil, nil, NewPostgresCollaborationMembershipReader(), nil)
	if repository == nil {
		t.Fatal("NewPostgresRecordWatchRepository() = nil")
	}
	_, err := repository.SetWatch(context.Background(), recordcollaboration.WatchCommand{})
	if !errors.Is(err, recordcollaboration.ErrInvalidWatchCommand) {
		t.Fatalf("SetWatch(invalid) error = %v", err)
	}
	_, err = repository.GetWatch(context.Background(), recordcollaboration.WatchReadCommand{})
	if !errors.Is(err, recordcollaboration.ErrInvalidWatchCommand) {
		t.Fatalf("GetWatch(invalid) error = %v", err)
	}
}

func TestNextRecordWatchPreferencePreservesAutomaticSourcesAndDeletesOnlyEmptyDefault(t *testing.T) {
	absent := recordcollaboration.WatchStatus{
		RecordID: "rec_watch1", UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
		Preference: recordcollaboration.FollowerPreferenceDefault,
	}
	next, remove, err := nextRecordWatchPreference(absent, recordcollaboration.FollowerPreferenceWatching, 3)
	if err != nil || remove || next.Version != 1 || next.Preference != recordcollaboration.FollowerPreferenceWatching || next.RecordFenceEpoch != 3 {
		t.Fatalf("absent next/remove/error = %#v/%v/%v", next, remove, err)
	}

	current := recordcollaboration.WatchStatus{
		RecordID: "rec_watch1", UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", Version: 4,
		Preference:       recordcollaboration.FollowerPreferenceMuted,
		Sources:          recordcollaboration.FollowerSources{Author: true, Participant: true, Comment: true},
		RecordFenceEpoch: 2,
		UpdatedAt:        time.Date(2026, 8, 17, 3, 0, 0, 0, time.UTC),
	}
	next, remove, err = nextRecordWatchPreference(current, recordcollaboration.FollowerPreferenceDefault, 3)
	if err != nil || remove || next.Version != 5 || next.Preference != recordcollaboration.FollowerPreferenceDefault ||
		!next.Sources.Author || !next.Sources.Participant || !next.Sources.Comment || next.RecordFenceEpoch != 3 {
		t.Fatalf("next/remove/error = %#v/%v/%v", next, remove, err)
	}

	empty := current
	empty.Sources = recordcollaboration.FollowerSources{}
	empty.Preference = recordcollaboration.FollowerPreferenceWatching
	next, remove, err = nextRecordWatchPreference(empty, recordcollaboration.FollowerPreferenceDefault, 3)
	if err != nil || !remove || next.Version != 0 || next.Preference != recordcollaboration.FollowerPreferenceDefault || next.Sources.Any() {
		t.Fatalf("empty default next/remove/error = %#v/%v/%v", next, remove, err)
	}
}
