package store

import (
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/records"
)

func recordDomainTestSubjects() []activity.SubjectSnapshot {
	return []activity.SubjectSnapshot{{
		Kind:     records.SubjectKindVPS,
		SourceID: "vps_7c2a4e18b09d5f31",
		Role:     records.RelationRoleAffected,
		Primary:  true,
		Identity: map[string]string{"display_name": "hk-edge-01"},
	}}
}

func recordDomainTestRow() recordDomainActivityRow {
	saved := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	return recordDomainActivityRow{
		activityID:    "rac_0f3c9d",
		recordID:      "rec_9a1b2c",
		revisionID:    "rev_5d6e7f",
		eventKind:     string(activity.EventKindRecordCreated),
		sourceVersion: 1,
		actorID:       "usr_000000000000000000000001",
		eventAt:       saved,
		recordedAt:    saved,
		revisionNo:    1,
	}
}

// The projected vocabulary has to match what the record writers actually write.
// A kind in one set and not the other is either a silently empty timeline or a
// batch that fails in production on a row nobody tested.
func TestRecordDomainSourceCoversExactlyTheRecordEventVocabulary(t *testing.T) {
	projected := make(map[activity.EventKind]bool)
	for _, kind := range RecordDomainEventKinds() {
		projected[kind] = true
	}

	// These are the kinds the five record-domain writers emit, taken from the
	// records, comments and actions domains.
	written := []activity.EventKind{
		activity.EventKindRecordCreated,
		activity.EventKindRecordRevised,
		activity.EventKindRecordRestored,
		activity.EventKindRecordArchived,
		activity.EventKindRecordUnarchived,
		activity.EventKindRecordOwnerChanged,
		activity.EventKindRecordParticipantChanged,
		activity.EventKindRecordFollowUpChanged,
		activity.EventKindCommentCreated,
		activity.EventKindCommentEdited,
		activity.EventKindCommentRedacted,
		activity.EventKindActionCreated,
		activity.EventKindActionUpdated,
		activity.EventKindActionCompleted,
		activity.EventKindActionCancelled,
		activity.EventKindActionReopened,
	}
	for _, kind := range written {
		if !projected[kind] {
			t.Errorf("writers emit %q but the source cannot project it", kind)
		}
		delete(projected, kind)
	}
	for kind := range projected {
		t.Errorf("source projects %q but no record writer emits it", kind)
	}

	// The four system sources own the rest of the vocabulary; this source must not
	// claim them.
	for _, foreign := range []activity.EventKind{
		activity.EventKindEvidenceCaptured,
		activity.EventKindAssetFactChanged,
		activity.EventKindMonitoringStateChanged,
		activity.EventKindCommandExecuted,
	} {
		for _, kind := range RecordDomainEventKinds() {
			if kind == foreign {
				t.Errorf("record domain must not project %q, which belongs to another source", foreign)
			}
		}
	}
}

func TestBuildRecordDomainCandidateDerivesAStableIdentity(t *testing.T) {
	namespace := activityTestNamespace()
	row := recordDomainTestRow()

	first, err := buildRecordDomainCandidate(namespace, row, recordDomainTestSubjects())
	if err != nil {
		t.Fatalf("build candidate: %v", err)
	}
	second, err := buildRecordDomainCandidate(namespace, row, recordDomainTestSubjects())
	if err != nil {
		t.Fatalf("rebuild candidate: %v", err)
	}

	if first.ActivityID != second.ActivityID || first.CanonicalHash != second.CanonicalHash {
		t.Fatalf("the same row produced two identities, so a retry would insert twice")
	}
	// The event's own primary key is the coordinate, not the upstream
	// source_event_id, whose shape differs across the five writers.
	if first.Source.EventID != row.activityID {
		t.Fatalf("source event id = %q, want the row's own key %q", first.Source.EventID, row.activityID)
	}
	if first.Source.Kind != activity.SourceKindRecordDomain {
		t.Fatalf("source kind = %q, want record_domain", first.Source.Kind)
	}
	if first.CanonicalHash != first.ComputeCanonicalHash() {
		t.Fatalf("candidate ships a hash that does not cover its own content")
	}
}

func TestBuildRecordDomainCandidateSeparatesEveryCoordinate(t *testing.T) {
	namespace := activityTestNamespace()
	base, err := buildRecordDomainCandidate(namespace, recordDomainTestRow(), recordDomainTestSubjects())
	if err != nil {
		t.Fatalf("build baseline: %v", err)
	}

	for name, mutate := range map[string]func(*recordDomainActivityRow){
		"another event":  func(row *recordDomainActivityRow) { row.activityID = "rac_0f3c9e" },
		"another kind":   func(row *recordDomainActivityRow) { row.eventKind = string(activity.EventKindRecordArchived) },
		"later revision": func(row *recordDomainActivityRow) { row.sourceVersion = 2 },
	} {
		t.Run(name, func(t *testing.T) {
			row := recordDomainTestRow()
			mutate(&row)
			candidate, err := buildRecordDomainCandidate(namespace, row, recordDomainTestSubjects())
			if err != nil {
				t.Fatalf("build candidate: %v", err)
			}
			if candidate.ActivityID == base.ActivityID {
				t.Fatalf("%s reuses the baseline identity, so one event would overwrite the other", name)
			}
		})
	}
}

// A title assembled from the record would put record text, comment text or
// command output onto a timeline row that is readable under a much wider
// authorization than the record itself.
func TestBuildRecordDomainCandidateCarriesNoRecordContent(t *testing.T) {
	namespace := activityTestNamespace()
	candidate, err := buildRecordDomainCandidate(namespace, recordDomainTestRow(), recordDomainTestSubjects())
	if err != nil {
		t.Fatalf("build candidate: %v", err)
	}

	if candidate.Presentation.Title != recordDomainEventTitles[activity.EventKindRecordCreated] {
		t.Fatalf("title = %q, want the fixed per-kind label", candidate.Presentation.Title)
	}
	if candidate.Presentation.Summary != "" {
		t.Fatalf("summary = %q, want it empty: there is no record-free summary to put there", candidate.Presentation.Summary)
	}
	if err := activity.ValidatePresentation(candidate.Presentation); err != nil {
		t.Fatalf("presentation is not a registered shape: %v", err)
	}
	for _, forbidden := range []string{candidate.RecordID, candidate.RevisionID} {
		if forbidden != "" && candidate.Presentation.Title == forbidden {
			t.Fatalf("title leaks an identifier: %q", candidate.Presentation.Title)
		}
	}
}

func TestBuildRecordDomainCandidateRejectsRowsItCannotProject(t *testing.T) {
	namespace := activityTestNamespace()
	for name, test := range map[string]struct {
		mutate   func(*recordDomainActivityRow)
		subjects []activity.SubjectSnapshot
		want     error
	}{
		"a kind no writer emits": {
			mutate:   func(row *recordDomainActivityRow) { row.eventKind = "record_teleported" },
			subjects: recordDomainTestSubjects(),
			want:     activity.ErrInvalidEventKind,
		},
		"a kind owned by another source": {
			mutate:   func(row *recordDomainActivityRow) { row.eventKind = string(activity.EventKindCommandExecuted) },
			subjects: recordDomainTestSubjects(),
			want:     activity.ErrInvalidEventKind,
		},
		"no version to order corrections by": {
			mutate:   func(row *recordDomainActivityRow) { row.sourceVersion = 0 },
			subjects: recordDomainTestSubjects(),
			want:     activity.ErrInvalidSourceIdentity,
		},
		"no revision to take subjects from": {
			mutate:   func(row *recordDomainActivityRow) { row.revisionID = "" },
			subjects: nil,
			want:     activity.ErrUnreachableCandidate,
		},
		"a subject kind no route resolves": {
			mutate: func(row *recordDomainActivityRow) {},
			subjects: []activity.SubjectSnapshot{{
				Kind:     "datacenter",
				SourceID: "dc_1",
				Role:     records.RelationRoleAffected,
			}},
			want: activity.ErrUnreachableCandidate,
		},
	} {
		t.Run(name, func(t *testing.T) {
			row := recordDomainTestRow()
			test.mutate(&row)
			if _, err := buildRecordDomainCandidate(namespace, row, test.subjects); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

// The event-time rules are frozen in the activity package. This source must route
// through them rather than picking its own columns, or the same act would sort
// differently depending on which source projected it.
func TestBuildRecordDomainCandidateAppliesTheFrozenEventTimeRules(t *testing.T) {
	namespace := activityTestNamespace()
	occurred := time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC)
	saved := occurred.Add(2 * time.Minute)

	firstRevision := recordDomainTestRow()
	firstRevision.eventKind = string(activity.EventKindRecordCreated)
	firstRevision.revisionNo = 1
	firstRevision.sourceVersion = 1
	firstRevision.eventAt = occurred
	firstRevision.recordedAt = saved

	candidate, err := buildRecordDomainCandidate(namespace, firstRevision, recordDomainTestSubjects())
	if err != nil {
		t.Fatalf("build first revision: %v", err)
	}
	if !candidate.EventAt.Equal(occurred) {
		t.Fatalf("first revision event time = %s, want the occurrence %s", candidate.EventAt, occurred)
	}
	if !candidate.RecordedAt.Equal(saved) {
		t.Fatalf("recorded time = %s, want the save time %s", candidate.RecordedAt, saved)
	}

	laterRevision := firstRevision
	laterRevision.eventKind = string(activity.EventKindRecordRevised)
	laterRevision.revisionNo = 4
	laterRevision.sourceVersion = 4

	later, err := buildRecordDomainCandidate(namespace, laterRevision, recordDomainTestSubjects())
	if err != nil {
		t.Fatalf("build later revision: %v", err)
	}
	// A later revision is an edit made at save time; it describes now, not the
	// original occurrence.
	if !later.EventAt.Equal(saved) {
		t.Fatalf("later revision event time = %s, want the save time %s", later.EventAt, saved)
	}
	if later.Backfilled {
		t.Fatalf("an ordinary save delay is not a backfill")
	}
}

func TestNewRecordDomainActivitySourceRejectsUnusableConfiguration(t *testing.T) {
	if _, err := NewRecordDomainActivitySource(nil, activityTestNamespace()); err == nil {
		t.Fatalf("a source without a pool must be rejected")
	}
}
