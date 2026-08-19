package activity

import (
	"testing"
	"time"
)

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return parsed
}

// A new record and its first revision are one thing that happened, and the
// operator already told us when: the confirmed occurrence time. Using the save
// time here would file last week's incident under today.
func TestFirstRevisionUsesTheConfirmedOccurrenceTime(t *testing.T) {
	occurred := mustTime(t, "2026-08-10T02:15:00Z")
	saved := mustTime(t, "2026-08-19T09:00:00Z")

	resolved, err := ResolveEventTime(EventTimeInput{
		Kind:       EventKindRecordCreated,
		RevisionNo: 1,
		OccurredAt: occurred,
		SavedAt:    saved,
	})
	if err != nil {
		t.Fatalf("resolve first revision: %v", err)
	}
	if !resolved.EventAt.Equal(occurred) {
		t.Fatalf("event time = %s, want the confirmed occurrence %s", resolved.EventAt, occurred)
	}
	if !resolved.RecordedAt.Equal(saved) {
		t.Fatalf("recorded time = %s, want the save time %s", resolved.RecordedAt, saved)
	}
}

// Later revisions are edits made now. They carry the save time, otherwise every
// revision of an old incident would stack on the original date and the timeline
// would never show that someone touched it today.
func TestLaterRevisionsUseTheSaveTime(t *testing.T) {
	occurred := mustTime(t, "2026-08-10T02:15:00Z")
	saved := mustTime(t, "2026-08-19T09:00:00Z")

	resolved, err := ResolveEventTime(EventTimeInput{
		Kind:       EventKindRecordRevised,
		RevisionNo: 4,
		OccurredAt: occurred,
		SavedAt:    saved,
	})
	if err != nil {
		t.Fatalf("resolve later revision: %v", err)
	}
	if !resolved.EventAt.Equal(saved) {
		t.Fatalf("event time = %s, want the save time %s", resolved.EventAt, saved)
	}
}

// A late system fact keeps the time it really happened and admits it arrived
// late. Rewriting its time to arrival would reorder the timeline into a lie
// about when the outage occurred.
func TestLateSystemFactKeepsItsOccurrenceAndIsMarkedBackfilled(t *testing.T) {
	occurred := mustTime(t, "2026-08-12T22:41:00Z")
	recorded := mustTime(t, "2026-08-19T09:00:00Z")

	resolved, err := ResolveEventTime(EventTimeInput{
		Kind:          EventKindMonitoringStateChanged,
		OccurredAt:    occurred,
		SavedAt:       recorded,
		SourceIsLate:  true,
		Authoritative: true,
	})
	if err != nil {
		t.Fatalf("resolve late system fact: %v", err)
	}
	if !resolved.EventAt.Equal(occurred) {
		t.Fatalf("event time = %s, want the authoritative occurrence %s", resolved.EventAt, occurred)
	}
	if !resolved.Backfilled {
		t.Fatal("a late authoritative fact must be marked backfilled")
	}
}

// An ordinary save is slower than the clock tick it describes. Inferring
// "backfilled" from that gap alone would mark most normal saves as late.
func TestOrdinarySaveLatencyIsNotInferredAsBackfill(t *testing.T) {
	occurred := mustTime(t, "2026-08-19T08:59:58Z")
	saved := mustTime(t, "2026-08-19T09:00:00Z")

	resolved, err := ResolveEventTime(EventTimeInput{
		Kind:          EventKindMonitoringStateChanged,
		OccurredAt:    occurred,
		SavedAt:       saved,
		Authoritative: true,
	})
	if err != nil {
		t.Fatalf("resolve ordinary save: %v", err)
	}
	if resolved.Backfilled {
		t.Fatal("save latency alone must not mark an event backfilled")
	}
}

// Evidence is filed at the end of what it observed, not when the capture job
// finished writing it. Both times are kept because the operator needs to know
// the observation window and when we learned about it.
func TestEvidenceUsesObservationEndAndKeepsCaptureTime(t *testing.T) {
	observedEnd := mustTime(t, "2026-08-19T07:00:00Z")
	captured := mustTime(t, "2026-08-19T07:04:30Z")

	resolved, err := ResolveEventTime(EventTimeInput{
		Kind:           EventKindEvidenceCaptured,
		ObservationEnd: observedEnd,
		SavedAt:        captured,
		Authoritative:  true,
		CaptureIsLate:  false,
	})
	if err != nil {
		t.Fatalf("resolve evidence: %v", err)
	}
	if !resolved.EventAt.Equal(observedEnd) {
		t.Fatalf("event time = %s, want the observation end %s", resolved.EventAt, observedEnd)
	}
	if !resolved.RecordedAt.Equal(captured) {
		t.Fatalf("recorded time = %s, want the capture time %s", resolved.RecordedAt, captured)
	}
}

// Everything is stored in UTC. Leaving a zone on the row would make two events
// that happened in the same instant sort differently depending on who saved
// them.
func TestResolvedTimesAreNormalizedToUTC(t *testing.T) {
	zone := time.FixedZone("CST", 8*60*60)
	occurred := time.Date(2026, 8, 19, 17, 0, 0, 0, zone)
	saved := time.Date(2026, 8, 19, 17, 0, 5, 0, zone)

	resolved, err := ResolveEventTime(EventTimeInput{
		Kind:       EventKindRecordCreated,
		RevisionNo: 1,
		OccurredAt: occurred,
		SavedAt:    saved,
	})
	if err != nil {
		t.Fatalf("resolve zoned input: %v", err)
	}
	if resolved.EventAt.Location() != time.UTC || resolved.RecordedAt.Location() != time.UTC {
		t.Fatalf("resolved times are not UTC: %s / %s", resolved.EventAt, resolved.RecordedAt)
	}
	if !resolved.EventAt.Equal(occurred) {
		t.Fatalf("UTC normalization changed the instant: %s vs %s", resolved.EventAt, occurred)
	}
}

func TestResolveEventTimeRejectsUnusableInput(t *testing.T) {
	saved := mustTime(t, "2026-08-19T09:00:00Z")
	cases := map[string]EventTimeInput{
		"unknown kind": {
			Kind:       "invented_kind",
			RevisionNo: 1,
			OccurredAt: saved,
			SavedAt:    saved,
		},
		"missing save time": {
			Kind:       EventKindRecordRevised,
			RevisionNo: 2,
			OccurredAt: saved,
		},
		"first revision without occurrence": {
			Kind:       EventKindRecordCreated,
			RevisionNo: 1,
			SavedAt:    saved,
		},
		"evidence without observation end": {
			Kind:          EventKindEvidenceCaptured,
			SavedAt:       saved,
			Authoritative: true,
		},
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ResolveEventTime(input); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}
}
