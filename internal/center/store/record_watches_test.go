package store

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"houfeng/internal/center/recordcollaboration"
	"houfeng/internal/center/records"
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

func TestPostgresRecordWatchRepositoryPrefetchFailurePreventsTransaction(t *testing.T) {
	parent := records.RevisionCommitResult{RecordID: "rec_watchparent1", RevisionID: "rrv_current1", LockVersion: 7, AuthorizationEpoch: 9}
	command := postgresWatchCommand(t, parent, 0, recordcollaboration.FollowerPreferenceWatching, "watch-prefetch", 0x31)
	read := postgresWatchReadCommand(t, parent)
	failure := errors.New("authorization prefetch failed")
	for _, operation := range []string{"set", "get"} {
		t.Run(operation, func(t *testing.T) {
			beginCalls := 0
			prefetch := &recordAuthorizationPrefetchErrorStub{err: failure}
			repository := &PostgresRecordWatchRepository{
				platform: &PostgresRecordPlatformRepository{beginTx: func(context.Context, pgx.TxOptions) (pgx.Tx, error) {
					beginCalls++
					return nil, errors.New("transaction must not begin")
				}},
				members: &commentMembershipReaderStub{}, authorization: prefetch,
			}
			var err error
			if operation == "set" {
				invalid := command
				invalid.RecordID = ""
				_, err = repository.SetWatch(context.Background(), invalid)
			} else {
				invalid := read
				invalid.RecordID = ""
				_, err = repository.GetWatch(context.Background(), invalid)
			}
			if err == nil || prefetch.recordIDs != nil || beginCalls != 0 {
				t.Fatalf("invalid command error=%v prefetch IDs=%v begin calls=%d", err, prefetch.recordIDs, beginCalls)
			}
			if operation == "set" {
				_, err = repository.SetWatch(context.Background(), command)
			} else {
				_, err = repository.GetWatch(context.Background(), read)
			}
			if !errors.Is(err, failure) || !slices.Equal(prefetch.recordIDs, []string{parent.RecordID}) || beginCalls != 0 {
				t.Fatalf("error=%v prefetch IDs=%v begin calls=%d", err, prefetch.recordIDs, beginCalls)
			}
		})
	}
}

func TestNextRecordWatchPreferencePreservesAutomaticSourcesAndNeverResetsExistingVersion(t *testing.T) {
	absent := recordcollaboration.WatchStatus{
		RecordID: "rec_watch1", UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
		Preference: recordcollaboration.FollowerPreferenceDefault,
	}
	next, err := nextRecordWatchPreference(absent, recordcollaboration.FollowerPreferenceWatching, 3)
	if err != nil || next.Version != 1 || next.Preference != recordcollaboration.FollowerPreferenceWatching || next.RecordFenceEpoch != 3 {
		t.Fatalf("absent next/error = %#v/%v", next, err)
	}

	current := recordcollaboration.WatchStatus{
		RecordID: "rec_watch1", UserID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa", Version: 4,
		Preference:       recordcollaboration.FollowerPreferenceMuted,
		Sources:          recordcollaboration.FollowerSources{Author: true, Participant: true, Comment: true},
		RecordFenceEpoch: 2,
		UpdatedAt:        time.Date(2026, 8, 17, 3, 0, 0, 0, time.UTC),
	}
	next, err = nextRecordWatchPreference(current, recordcollaboration.FollowerPreferenceDefault, 3)
	if err != nil || next.Version != 5 || next.Preference != recordcollaboration.FollowerPreferenceDefault ||
		!next.Sources.Author || !next.Sources.Participant || !next.Sources.Comment || next.RecordFenceEpoch != 3 {
		t.Fatalf("next/error = %#v/%v", next, err)
	}

	empty := current
	empty.Sources = recordcollaboration.FollowerSources{}
	empty.Preference = recordcollaboration.FollowerPreferenceWatching
	next, err = nextRecordWatchPreference(empty, recordcollaboration.FollowerPreferenceDefault, 3)
	if err != nil || next.Version != 5 || next.Preference != recordcollaboration.FollowerPreferenceDefault || next.Sources.Any() {
		t.Fatalf("empty default next/error = %#v/%v", next, err)
	}
}
