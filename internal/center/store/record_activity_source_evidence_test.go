package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

const testEvidenceVPSSourceID = "vps_7c2a4e18b09d5f31"

func evidenceActivityTestRow() evidenceActivityRow {
	observedEnd := time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC)
	return evidenceActivityRow{
		snapshotID:     "evs_hostwindow01",
		recordID:       "rec_evidencesrc",
		kind:           "monitoring.host.v1",
		schemaVersion:  2,
		sourceKind:     string(recordauth.SourceKindVPS),
		sourceID:       testEvidenceVPSSourceID,
		displayName:    "hk-edge-01",
		actualEndedAt:  observedEnd,
		createdAt:      observedEnd.Add(90 * time.Second),
		referencedAt:   observedEnd.Add(2 * time.Minute),
		sensitivityLvl: "normal",
	}
}

// Every source kind the authorization model can produce must project. A kind it
// admits but this source drops would be evidence that silently never appears.
func TestEvidenceSourceCoversEverySubjectKindTheWriterCanProduce(t *testing.T) {
	for _, sourceKind := range []recordauth.SourceKind{
		recordauth.SourceKindVPS,
		recordauth.SourceKindMonitoringInstance,
		recordauth.SourceKindTarget,
	} {
		if _, projectable := evidenceSubjectKind(string(sourceKind)); !projectable {
			t.Errorf("writers can capture from %q but the source cannot project it", sourceKind)
		}
	}
	// The remaining check-constraint values are schema headroom. They must not
	// resolve to a subject, because there is no timeline they belong on.
	for _, headroom := range []string{"subscription", "monitoring_event", "command_audit", "record_revision"} {
		if _, projectable := evidenceSubjectKind(headroom); projectable {
			t.Errorf("%q is not a subject and must not resolve to one", headroom)
		}
	}
}

// Evidence is timed by the end of the window it observed. A capture of last
// week's data belongs last week, not at the moment it was written.
func TestBuildEvidenceCandidateTimesItByTheObservedWindowEnd(t *testing.T) {
	row := evidenceActivityTestRow()

	candidate, err := buildEvidenceCandidate(activityTestNamespace(), row)
	if err != nil {
		t.Fatalf("build candidate: %v", err)
	}
	if !candidate.EventAt.Equal(row.actualEndedAt) {
		t.Fatalf("event time = %s, want the observed window end %s", candidate.EventAt, row.actualEndedAt)
	}
	if !candidate.RecordedAt.Equal(row.createdAt) {
		t.Fatalf("recorded time = %s, want the write time %s", candidate.RecordedAt, row.createdAt)
	}
	if candidate.EventKind != activity.EventKindEvidenceCaptured {
		t.Fatalf("event kind = %q, want evidence_captured", candidate.EventKind)
	}
}

// A wide gap between the observed window and the write is not evidence of
// lateness. Backfilled means a producer said the fact arrived late, and
// evidence_snapshots has no column that says it: a capture summarizing an older
// window is the normal case, not a late arrival. Inferring it from the gap is
// exactly the guess the event-time rules forbid.
func TestBuildEvidenceCandidateDoesNotInferLatenessFromTheGap(t *testing.T) {
	row := evidenceActivityTestRow()
	row.actualEndedAt = row.createdAt.Add(-30 * 24 * time.Hour)

	candidate, err := buildEvidenceCandidate(activityTestNamespace(), row)
	if err != nil {
		t.Fatalf("build candidate: %v", err)
	}
	if candidate.Backfilled {
		t.Fatal("lateness must come from the producer, not from comparing timestamps")
	}
	// It still sorts by the window it observed, which is what keeps a capture of
	// old data out of today's position on the timeline.
	if !candidate.EventAt.Equal(row.actualEndedAt) {
		t.Fatalf("event time = %s, want the observed window end", candidate.EventAt)
	}
	if !candidate.RecordedAt.After(candidate.EventAt) {
		t.Fatal("the write time must stay distinguishable from the observed time")
	}
}

// The schema version is part of the coordinate: a re-capture under a new schema
// is a different fact about the same window, not an edit of the old one.
func TestBuildEvidenceCandidateSeparatesSchemaVersions(t *testing.T) {
	namespace := activityTestNamespace()
	first := evidenceActivityTestRow()
	second := evidenceActivityTestRow()
	second.schemaVersion = 3

	firstCandidate, err := buildEvidenceCandidate(namespace, first)
	if err != nil {
		t.Fatalf("build first: %v", err)
	}
	secondCandidate, err := buildEvidenceCandidate(namespace, second)
	if err != nil {
		t.Fatalf("build second: %v", err)
	}
	if firstCandidate.ActivityID == secondCandidate.ActivityID {
		t.Fatal("two schema versions of one snapshot must not collapse onto one activity")
	}
}

func TestBuildEvidenceCandidateUsesTheCapturedSubjectIdentity(t *testing.T) {
	row := evidenceActivityTestRow()

	candidate, err := buildEvidenceCandidate(activityTestNamespace(), row)
	if err != nil {
		t.Fatalf("build candidate: %v", err)
	}
	subject := candidate.Subjects[0]
	if subject.Kind != records.SubjectKindVPS || subject.SourceID != testEvidenceVPSSourceID {
		t.Fatalf("subject = %+v", subject)
	}
	if subject.Identity["display_name"] != "hk-edge-01" {
		t.Fatalf("identity = %v, want the identity captured with the snapshot", subject.Identity)
	}
	// Evidence observes a subject rather than changing it, so it is context on
	// that subject's timeline instead of a change to it.
	if subject.Role != records.RelationRoleContext {
		t.Fatalf("role = %q, want context", subject.Role)
	}
	if !subject.Primary {
		t.Fatalf("the observed subject must be primary")
	}
}

func TestBuildEvidenceCandidateProjectsNoCapturedContent(t *testing.T) {
	candidate, err := buildEvidenceCandidate(activityTestNamespace(), evidenceActivityTestRow())
	if err != nil {
		t.Fatalf("build candidate: %v", err)
	}
	if candidate.Presentation.Title != evidenceActivityTitle {
		t.Fatalf("title = %q, want the fixed label", candidate.Presentation.Title)
	}
	// The evidence kind is a registered machine identifier; it names what was
	// measured without copying any of the measurement.
	if candidate.Presentation.Summary != "monitoring.host.v1" {
		t.Fatalf("summary = %q, want the evidence kind", candidate.Presentation.Summary)
	}
	if err := activity.ValidatePresentation(candidate.Presentation); err != nil {
		t.Fatalf("presentation is not a registered shape: %v", err)
	}
}

// The payload and its surrounding capture detail are exactly what must not reach
// a timeline, so the scan must not even read those columns.
func TestEvidenceSourceNeverReadsCapturedPayload(t *testing.T) {
	for _, forbidden := range []string{
		"payload_digest", "quality", "redaction", "unit_semantics",
		"capture_authorization", "source_digest",
	} {
		if strings.Contains(evidenceActivityScanSQL, forbidden) {
			t.Errorf("the scan must not read %q", forbidden)
		}
	}
}

// A snapshot's record is mutable: its subjects can change after the capture.
// Resolving through it would let today's record membership rewrite what an old
// capture was about.
func TestEvidenceSourceResolvesSubjectsWithoutTouchingTheRecord(t *testing.T) {
	for _, forbidden := range []string{"record_revisions", "record_revision_subjects", "join"} {
		if strings.Contains(strings.ToLower(evidenceActivityScanSQL), forbidden) {
			t.Errorf("the scan must not reach through %q to find a subject", forbidden)
		}
	}
}

func TestBuildEvidenceCandidateRejectsRowsItCannotProject(t *testing.T) {
	for name, test := range map[string]struct {
		mutate func(*evidenceActivityRow)
		want   error
	}{
		"a source kind that is not a subject": {
			mutate: func(row *evidenceActivityRow) { row.sourceKind = "subscription" },
			want:   activity.ErrUnreachableCandidate,
		},
		"a source id no route resolves": {
			mutate: func(row *evidenceActivityRow) { row.sourceID = "vps_nope" },
			want:   activity.ErrUnreachableCandidate,
		},
		"a version the schema forbids": {
			mutate: func(row *evidenceActivityRow) { row.schemaVersion = 0 },
			want:   activity.ErrInvalidSourceIdentity,
		},
		"a capture with no observed window": {
			mutate: func(row *evidenceActivityRow) { row.actualEndedAt = time.Time{} },
			want:   activity.ErrInvalidEventTime,
		},
	} {
		t.Run(name, func(t *testing.T) {
			row := evidenceActivityTestRow()
			test.mutate(&row)
			if _, err := buildEvidenceCandidate(activityTestNamespace(), row); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestNewEvidenceActivitySourceRejectsUnusableConfiguration(t *testing.T) {
	if _, err := NewEvidenceActivitySource(nil, activityTestNamespace()); err == nil {
		t.Fatal("a source without a pool must be rejected")
	}
	if _, err := NewEvidenceActivitySource(&pgxpool.Pool{}, activity.Namespace{}); err == nil {
		t.Fatal("a source without a project namespace must be rejected")
	}
}
