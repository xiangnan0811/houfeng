package recordcollaboration

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestNormalizeRevisionFilterFactsSortsDeduplicatesAndCopies(t *testing.T) {
	t.Parallel()

	followUp := time.Date(2026, time.August, 18, 9, 30, 0, 123456000, time.FixedZone("UTC+8", 8*60*60))
	facts, err := NormalizeRevisionFilterFacts(RevisionFilterFactValues{
		OwnerID: "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
		ParticipantIDs: []string{
			"usr_cccccccccccccccccccccccc",
			"usr_bbbbbbbbbbbbbbbbbbbbbbbb",
			"usr_cccccccccccccccccccccccc",
		},
		FollowUpAt: &followUp,
	})
	if err != nil {
		t.Fatalf("NormalizeRevisionFilterFacts() error = %v", err)
	}
	if facts.OwnerID() != "usr_aaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("OwnerID() = %q", facts.OwnerID())
	}
	wantParticipants := []string{
		"usr_bbbbbbbbbbbbbbbbbbbbbbbb",
		"usr_cccccccccccccccccccccccc",
	}
	if got := facts.ParticipantIDs(); !reflect.DeepEqual(got, wantParticipants) {
		t.Fatalf("ParticipantIDs() = %#v, want %#v", got, wantParticipants)
	}
	if got := facts.FollowUpAt(); got == nil || !got.Equal(followUp.UTC()) || got.Location() != time.UTC {
		t.Fatalf("FollowUpAt() = %v, want UTC %v", got, followUp.UTC())
	}

	participants := facts.ParticipantIDs()
	participants[0] = "usr_zzzzzzzzzzzzzzzzzzzzzzzz"
	returnedTime := facts.FollowUpAt()
	*returnedTime = time.Time{}
	if got := facts.ParticipantIDs(); !reflect.DeepEqual(got, wantParticipants) {
		t.Fatalf("ParticipantIDs() mutated through getter: %#v", got)
	}
	if got := facts.FollowUpAt(); got == nil || !got.Equal(followUp.UTC()) {
		t.Fatalf("FollowUpAt() mutated through getter: %v", got)
	}
}

func TestNormalizeRevisionFilterFactsRejectsMalformedOwnerOrParticipant(t *testing.T) {
	t.Parallel()

	tests := []RevisionFilterFactValues{
		{OwnerID: "user-owner"},
		{ParticipantIDs: []string{"usr_invalid"}},
	}
	for _, values := range tests {
		if _, err := NormalizeRevisionFilterFacts(values); !errors.Is(err, ErrInvalidRevisionFilterFacts) {
			t.Fatalf("NormalizeRevisionFilterFacts(%#v) error = %v, want ErrInvalidRevisionFilterFacts", values, err)
		}
	}
}

func TestDiffRevisionFilterFactsUsesClosedDeterministicFieldOrder(t *testing.T) {
	t.Parallel()

	oldFollowUp := time.Date(2026, time.August, 18, 1, 0, 0, 0, time.UTC)
	newFollowUp := oldFollowUp.Add(time.Hour)
	previous, err := NormalizeRevisionFilterFacts(RevisionFilterFactValues{
		OwnerID:        "usr_aaaaaaaaaaaaaaaaaaaaaaaa",
		ParticipantIDs: []string{"usr_bbbbbbbbbbbbbbbbbbbbbbbb"},
		FollowUpAt:     &oldFollowUp,
	})
	if err != nil {
		t.Fatalf("normalize previous facts: %v", err)
	}
	current, err := NormalizeRevisionFilterFacts(RevisionFilterFactValues{
		OwnerID:        "usr_cccccccccccccccccccccccc",
		ParticipantIDs: []string{"usr_dddddddddddddddddddddddd"},
		FollowUpAt:     &newFollowUp,
	})
	if err != nil {
		t.Fatalf("normalize current facts: %v", err)
	}

	got := DiffRevisionFilterFacts(previous, current)
	want := []RevisionFieldKind{
		RevisionFieldOwner,
		RevisionFieldParticipants,
		RevisionFieldFollowUp,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DiffRevisionFilterFacts() = %#v, want %#v", got, want)
	}
	if got := DiffRevisionFilterFacts(current, current); len(got) != 0 {
		t.Fatalf("DiffRevisionFilterFacts(equal) = %#v, want empty", got)
	}
}
