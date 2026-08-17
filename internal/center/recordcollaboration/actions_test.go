package recordcollaboration

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNormalizeActionFieldsCanonicalizesBoundedContentAndFilterFacts(t *testing.T) {
	due := time.Date(2026, time.August, 18, 9, 30, 0, 123456789, time.FixedZone("UTC+8", 8*60*60))
	fields, err := NormalizeActionFields(ActionFieldValues{
		Title: "  Verify rollback  ", Details: "line one\nline two", AssigneeID: "usr_111111111111111111111111",
		DueAt: &due, SubjectRevisionID: "rrv_revision1",
	})
	if err != nil {
		t.Fatalf("NormalizeActionFields() error = %v", err)
	}
	if fields.Title() != "Verify rollback" || fields.Details() != "line one\nline two" ||
		fields.AssigneeID() != "usr_111111111111111111111111" || fields.SubjectRevisionID() != "rrv_revision1" {
		t.Fatalf("normalized fields = %#v", fields)
	}
	wantDue := due.UTC().Truncate(time.Microsecond)
	if fields.DueAt() == nil || !fields.DueAt().Equal(wantDue) {
		t.Fatalf("DueAt() = %v, want %v", fields.DueAt(), wantDue)
	}
	facts, err := NewActionFilterFacts(ActionStatusOpen, fields.AssigneeID(), fields.DueAt())
	if err != nil {
		t.Fatalf("NewActionFilterFacts() error = %v", err)
	}
	if facts.Status() != ActionStatusOpen || facts.AssigneeID() != "usr_111111111111111111111111" ||
		facts.DueAt() == nil || !facts.DueAt().Equal(wantDue) {
		t.Fatalf("filter facts = %#v", facts)
	}

	copyDue := fields.DueAt()
	*copyDue = copyDue.Add(time.Hour)
	if !fields.DueAt().Equal(wantDue) {
		t.Fatal("DueAt returned mutable internal state")
	}
}

func TestNormalizeActionFieldsRejectsNonCanonicalOrUnsafeValues(t *testing.T) {
	tests := []struct {
		name   string
		values ActionFieldValues
	}{
		{name: "empty title", values: ActionFieldValues{}},
		{name: "title too long", values: ActionFieldValues{Title: strings.Repeat("x", 513)}},
		{name: "details too long", values: ActionFieldValues{Title: "ok", Details: strings.Repeat("x", 4097)}},
		{name: "title control", values: ActionFieldValues{Title: "secret\nline"}},
		{name: "details control", values: ActionFieldValues{Title: "ok", Details: "secret\x00value"}},
		{name: "invalid assignee", values: ActionFieldValues{Title: "ok", AssigneeID: "admin@example.com"}},
		{name: "invalid revision", values: ActionFieldValues{Title: "ok", SubjectRevisionID: "../../revision"}},
		{name: "zero due", values: ActionFieldValues{Title: "ok", DueAt: &time.Time{}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NormalizeActionFields(test.values); !errors.Is(err, ErrInvalidActionFields) {
				t.Fatalf("NormalizeActionFields() error = %v, want ErrInvalidActionFields", err)
			}
		})
	}
}

func TestActionMutationRegistryAndTransitionsAreClosed(t *testing.T) {
	tests := []struct {
		kind ActionMutationKind
		from ActionStatus
		to   ActionStatus
	}{
		{ActionMutationComplete, ActionStatusOpen, ActionStatusCompleted},
		{ActionMutationCancel, ActionStatusOpen, ActionStatusCancelled},
		{ActionMutationReopen, ActionStatusCompleted, ActionStatusOpen},
		{ActionMutationReopen, ActionStatusCancelled, ActionStatusOpen},
	}
	for _, test := range tests {
		if err := ValidateActionMutationTransition(test.kind, test.from, test.to); err != nil {
			t.Fatalf("ValidateActionMutationTransition(%q, %q, %q) error = %v", test.kind, test.from, test.to, err)
		}
	}
	invalid := []struct {
		kind ActionMutationKind
		from ActionStatus
		to   ActionStatus
	}{
		{ActionMutationComplete, ActionStatusCompleted, ActionStatusCompleted},
		{ActionMutationCancel, ActionStatusCancelled, ActionStatusCancelled},
		{ActionMutationReopen, ActionStatusOpen, ActionStatusOpen},
		{ActionMutationKind("delete"), ActionStatusOpen, ActionStatusCancelled},
	}
	for _, test := range invalid {
		if err := ValidateActionMutationTransition(test.kind, test.from, test.to); !errors.Is(err, ErrInvalidActionStateTransition) {
			t.Fatalf("invalid transition (%q, %q, %q) error = %v", test.kind, test.from, test.to, err)
		}
	}
}
