package activity

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func testNamespace() Namespace {
	return Namespace{ProjectID: "default"}
}

func testSource() SourceIdentity {
	return SourceIdentity{
		Kind:    SourceKindRecordDomain,
		EventID: "rac_0hz2k9",
		Version: 3,
	}
}

// The projector may run twice, crash mid-batch, or rebuild the whole projection
// from empty. All three have to land on the same identifier, otherwise a
// rebuild silently renumbers history and every stored cursor points somewhere
// else.
func TestActivityIDIsDeterministicForTheSameSourceIdentity(t *testing.T) {
	first, err := NewActivityID(testNamespace(), testSource(), EventKindRecordRevised)
	if err != nil {
		t.Fatalf("mint activity id: %v", err)
	}
	second, err := NewActivityID(testNamespace(), testSource(), EventKindRecordRevised)
	if err != nil {
		t.Fatalf("mint activity id again: %v", err)
	}
	if first != second {
		t.Fatalf("activity id is not deterministic: %q then %q", first, second)
	}
	if !ValidActivityID(first) {
		t.Fatalf("minted activity id %q does not satisfy the stored contract", first)
	}
	if !strings.HasPrefix(first, "act_") {
		t.Fatalf("activity id %q must carry the act_ prefix", first)
	}
}

// Every field of the unique key participates in the hash. If two of them
// collapsed onto one identifier the projector would treat distinct source
// events as a retry of each other and drop one.
func TestActivityIDSeparatesEveryComponentOfTheSourceKey(t *testing.T) {
	base, err := NewActivityID(testNamespace(), testSource(), EventKindRecordRevised)
	if err != nil {
		t.Fatalf("mint baseline activity id: %v", err)
	}

	variants := map[string]struct {
		namespace Namespace
		source    SourceIdentity
		kind      EventKind
	}{
		"different project": {
			namespace: Namespace{ProjectID: "other"},
			source:    testSource(),
			kind:      EventKindRecordRevised,
		},
		"different source kind": {
			namespace: testNamespace(),
			source:    SourceIdentity{Kind: SourceKindCommandAudit, EventID: "rac_0hz2k9", Version: 3},
			kind:      EventKindRecordRevised,
		},
		"different source event": {
			namespace: testNamespace(),
			source:    SourceIdentity{Kind: SourceKindRecordDomain, EventID: "rac_0hz2ka", Version: 3},
			kind:      EventKindRecordRevised,
		},
		"different source version": {
			namespace: testNamespace(),
			source:    SourceIdentity{Kind: SourceKindRecordDomain, EventID: "rac_0hz2k9", Version: 4},
			kind:      EventKindRecordRevised,
		},
		"different event kind": {
			namespace: testNamespace(),
			source:    testSource(),
			kind:      EventKindRecordArchived,
		},
	}
	for name, variant := range variants {
		t.Run(name, func(t *testing.T) {
			got, err := NewActivityID(variant.namespace, variant.source, variant.kind)
			if err != nil {
				t.Fatalf("mint activity id: %v", err)
			}
			if got == base {
				t.Fatalf("%s collides with the baseline identifier %q", name, base)
			}
		})
	}
}

// Length-prefixed canonical bytes are what stop "ab" + "c" from hashing like
// "a" + "bc". Without the prefixes an event id ending in a digit could collide
// with a different id plus a different version.
func TestActivityIDResistsFieldBoundaryAmbiguity(t *testing.T) {
	left, err := NewActivityID(
		testNamespace(),
		SourceIdentity{Kind: SourceKindRecordDomain, EventID: "rac_ab", Version: 1},
		EventKindRecordRevised,
	)
	if err != nil {
		t.Fatalf("mint left activity id: %v", err)
	}
	right, err := NewActivityID(
		testNamespace(),
		SourceIdentity{Kind: SourceKindRecordDomain, EventID: "rac_a", Version: 1},
		EventKindRecordRevised,
	)
	if err != nil {
		t.Fatalf("mint right activity id: %v", err)
	}
	if left == right {
		t.Fatal("adjacent field values hash to the same identifier")
	}
}

func TestNewActivityIDRejectsIncompleteIdentity(t *testing.T) {
	cases := map[string]struct {
		namespace Namespace
		source    SourceIdentity
		kind      EventKind
	}{
		"empty project":       {Namespace{}, testSource(), EventKindRecordRevised},
		"empty source kind":   {testNamespace(), SourceIdentity{EventID: "rac_0hz2k9", Version: 1}, EventKindRecordRevised},
		"unknown source kind": {testNamespace(), SourceIdentity{Kind: "guesswork", EventID: "rac_0hz2k9", Version: 1}, EventKindRecordRevised},
		"empty source event":  {testNamespace(), SourceIdentity{Kind: SourceKindRecordDomain, Version: 1}, EventKindRecordRevised},
		"zero source version": {testNamespace(), SourceIdentity{Kind: SourceKindRecordDomain, EventID: "rac_0hz2k9"}, EventKindRecordRevised},
		"empty event kind":    {testNamespace(), testSource(), ""},
		"unknown event kind":  {testNamespace(), testSource(), "invented_kind"},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewActivityID(testCase.namespace, testCase.source, testCase.kind); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}
}

// The event kinds have to match what the writers actually emit. A kind that
// nothing writes produces an empty timeline with no error to explain it.
func TestEventKindsCoverTheKindsWritersEmit(t *testing.T) {
	for _, kind := range []EventKind{
		EventKindRecordCreated,
		EventKindRecordRevised,
		EventKindRecordRestored,
		EventKindRecordArchived,
		EventKindRecordUnarchived,
		EventKindRecordOwnerChanged,
		EventKindRecordParticipantChanged,
		EventKindRecordFollowUpChanged,
		EventKindCommentCreated,
		EventKindCommentEdited,
		EventKindCommentRedacted,
		EventKindActionCreated,
		EventKindActionUpdated,
		EventKindActionCompleted,
		EventKindActionCancelled,
		EventKindActionReopened,
	} {
		if !ValidEventKind(kind) {
			t.Fatalf("event kind %q is not registered", kind)
		}
	}
	for _, absent := range []EventKind{
		"record_revision",
		"record_state_changed",
		"record_visibility_changed",
	} {
		if ValidEventKind(absent) {
			t.Fatalf("event kind %q is registered but no writer emits it", absent)
		}
	}
}

// The records view is a server-side predicate over kinds. It must select the
// human-authored record events and leave collaboration chatter out, or the
// view stops meaning "records".
func TestRecordsViewSelectsHumanRecordEventsOnly(t *testing.T) {
	included := RecordsViewEventKinds()
	if len(included) == 0 {
		t.Fatal("records view selects no event kinds")
	}
	selected := make(map[EventKind]bool, len(included))
	for _, kind := range included {
		if !ValidEventKind(kind) {
			t.Fatalf("records view selects unregistered kind %q", kind)
		}
		selected[kind] = true
	}
	for _, want := range []EventKind{
		EventKindRecordCreated,
		EventKindRecordRevised,
		EventKindRecordArchived,
		EventKindRecordUnarchived,
		EventKindRecordRestored,
	} {
		if !selected[want] {
			t.Fatalf("records view omits %q", want)
		}
	}
	for _, unwanted := range []EventKind{
		EventKindCommentCreated,
		EventKindActionCreated,
	} {
		if selected[unwanted] {
			t.Fatalf("records view includes collaboration kind %q", unwanted)
		}
	}
}

// Presentation is a registered, versioned shape. Accepting arbitrary maps is
// how raw command output and Markdown bodies leak into a system fact.
func TestPresentationRejectsUnregisteredShapes(t *testing.T) {
	valid := Presentation{
		Version: PresentationVersionV1,
		Title:   "记录已修订",
		Summary: "状态由处理中变为已完成",
	}
	if err := ValidatePresentation(valid); err != nil {
		t.Fatalf("valid presentation rejected: %v", err)
	}
	cases := map[string]Presentation{
		"missing version": {Title: "记录已修订"},
		"unknown version": {Version: 99, Title: "记录已修订"},
		"empty title":     {Version: PresentationVersionV1},
		"oversized title": {Version: PresentationVersionV1, Title: strings.Repeat("标", MaxPresentationTitleRunes+1)},
	}
	for name, presentation := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidatePresentation(presentation); err == nil {
				t.Fatalf("%s was accepted", name)
			}
		})
	}
}

// Go slices marshal to null when nil. A timeline that answers null items forces
// every caller to special-case it, and the web contract says otherwise.
func TestEventMarshalsEmptyCollectionsAsArrays(t *testing.T) {
	payload, err := json.Marshal(Event{
		ActivityID: "act_2z4a",
		EventKind:  EventKindRecordRevised,
		EventAt:    time.Now().UTC(),
		RecordedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	if strings.Contains(string(payload), "null") {
		t.Fatalf("event JSON contains null: %s", payload)
	}
	if !strings.Contains(string(payload), `"subjects":[]`) {
		t.Fatalf("event JSON omits an empty subjects array: %s", payload)
	}
}

// The global watermark and the projection generation are server-side facts. If
// they reach a response body, one authorization scope learns that activity it
// cannot see is advancing.
func TestEventJSONNeverCarriesGlobalProjectionState(t *testing.T) {
	payload, err := json.Marshal(Event{
		ActivityID:     "act_2z4a",
		EventKind:      EventKindRecordRevised,
		EventAt:        time.Now().UTC(),
		RecordedAt:     time.Now().UTC(),
		IngestSequence: 4211,
	})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	for _, forbidden := range []string{
		"ingest_sequence",
		"projection_generation",
		"as_of",
		"checkpoint",
		"4211",
	} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatalf("event JSON leaks %q: %s", forbidden, payload)
		}
	}
}
