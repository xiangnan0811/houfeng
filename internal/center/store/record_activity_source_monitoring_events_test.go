package store

import (
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/incidents"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/records"
	"houfeng/internal/center/targets"
)

const testMonitoringInstanceSourceID = "mi_3b7d1e9a04c6f285"

func monitoringEventTestRow() monitoringEventActivityRow {
	occurred := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	return monitoringEventActivityRow{
		eventID:         "evt_incident0001",
		objectType:      string(incidents.ObjectTypeMonitoringInstance),
		objectID:        testMonitoringInstanceSourceID,
		displayName:     "hk-edge-01",
		eventType:       string(incidents.EventIncidentStarted),
		severity:        string(incidents.SeverityAlert),
		eventAt:         occurred,
		recordedAt:      occurred.Add(3 * time.Second),
		provenance:      incidents.MonitoringEventProvenanceCenter,
		producerVersion: incidents.MonitoringEventProducerVersion,
		ruleVersion:     incidents.MonitoringEventIncidentRuleVersion,
		priorState:      "normal",
		resultingState:  "alert",
	}
}

// The projected vocabulary must match the closed contract the writers validate
// against. A type in one and not the other is either an event that can never be
// projected or a label for something nothing writes.
func TestMonitoringEventSourceCoversTheWriterContract(t *testing.T) {
	for _, eventType := range []incidents.EventType{
		incidents.EventIncidentStarted,
		incidents.EventIncidentEscalated,
		incidents.EventIncidentRecovered,
		incidents.EventMonitoringInstanceBindingRebindConfirmed,
		incidents.EventMonitoringInstanceBindingPendingRejected,
		incidents.EventMonitoringInstanceBindingReset,
		incidents.EventMonitoringInstanceMonitoringMaintenanceEntered,
		incidents.EventMonitoringInstanceMonitoringMaintenanceExited,
		incidents.EventMonitoringInstanceMonitoringPaused,
		incidents.EventMonitoringInstanceMonitoringResumed,
		incidents.EventMonitoringInstanceLifecycleUpdated,
		incidents.EventMonitoringInstanceRetired,
		incidents.EventMonitoringInstanceRestoredToObserving,
		incidents.EventTargetMaintenanceEntered,
		incidents.EventTargetMaintenanceExited,
		incidents.EventTargetPaused,
		incidents.EventTargetResumed,
		incidents.EventTargetArchived,
		incidents.EventTargetRestoredToPaused,
		incidents.EventCorrected,
	} {
		if _, known := monitoringEventTitles[eventType]; !known {
			t.Errorf("writers emit %q but the source has no label for it", eventType)
		}
	}
	if got, want := len(MonitoringEventActivityTypes()), 20; got != want {
		t.Fatalf("source projects %d event types, want the contract's %d", got, want)
	}

	// Every severity the incident scale can carry must map onto the projection's,
	// which is a different vocabulary shared across all five sources.
	for _, severity := range []incidents.Severity{
		incidents.SeverityNormal,
		incidents.SeverityNotice,
		incidents.SeverityAlert,
		incidents.SeverityCritical,
	} {
		mapped, known := monitoringEventSeverities[severity]
		if !known {
			t.Errorf("severity %q has no projected equivalent", severity)
			continue
		}
		switch mapped {
		case "info", "notice", "warning", "critical":
		default:
			t.Errorf("severity %q maps to %q, which the projection does not accept", severity, mapped)
		}
	}
}

func TestBuildMonitoringEventCandidateProjectsAnIncident(t *testing.T) {
	namespace := activityTestNamespace()
	row := monitoringEventTestRow()

	candidate, err := buildMonitoringEventCandidate(namespace, row)
	if err != nil {
		t.Fatalf("build candidate: %v", err)
	}

	if candidate.EventKind != activity.EventKindMonitoringStateChanged {
		t.Fatalf("event kind = %q, want monitoring_state_changed", candidate.EventKind)
	}
	// The producer owns the occurrence time for a system fact, so the event sorts
	// by when the incident began rather than by when the row was written.
	if !candidate.EventAt.Equal(row.eventAt) {
		t.Fatalf("event time = %s, want the payload's %s", candidate.EventAt, row.eventAt)
	}
	if !candidate.RecordedAt.Equal(row.recordedAt) {
		t.Fatalf("recorded time = %s, want %s", candidate.RecordedAt, row.recordedAt)
	}
	if candidate.Severity != "warning" {
		t.Fatalf("severity = %q, want the projected equivalent of 告警", candidate.Severity)
	}
	if len(candidate.Subjects) != 1 {
		t.Fatalf("subjects = %d, want 1", len(candidate.Subjects))
	}
	subject := candidate.Subjects[0]
	if subject.Kind != records.SubjectKindMonitoringInstance || subject.SourceID != testMonitoringInstanceSourceID {
		t.Fatalf("subject = %+v", subject)
	}
	if !subject.Primary || subject.Role != records.RelationRoleAffected {
		t.Fatalf("the object an event is about must be its primary affected subject: %+v", subject)
	}
	if subject.Identity["display_name"] != "hk-edge-01" {
		t.Fatalf("identity snapshot = %v, want the live display name", subject.Identity)
	}
	if candidate.Corrects != "" {
		t.Fatalf("an ordinary event must not claim to correct anything")
	}
	if candidate.CanonicalHash != candidate.ComputeCanonicalHash() {
		t.Fatalf("candidate ships a hash that does not cover its content")
	}
}

// A correction has to point at the projected identity of the event it corrects.
// Storing the raw source id would leave the projection unable to join a
// correction to its original without knowing how ids are minted.
func TestBuildMonitoringEventCandidateResolvesACorrectionToAProjectedIdentity(t *testing.T) {
	namespace := activityTestNamespace()
	original := monitoringEventTestRow()
	originalCandidate, err := buildMonitoringEventCandidate(namespace, original)
	if err != nil {
		t.Fatalf("build original: %v", err)
	}

	correction := monitoringEventTestRow()
	correction.eventID = "evt_correction001"
	correction.eventType = string(incidents.EventCorrected)
	correction.provenance = incidents.MonitoringEventProvenanceManualCorrection
	correction.correctionOfEventID = original.eventID
	correction.severity = string(incidents.SeverityAlert)
	correction.resultingState = "alert"

	corrected, err := buildMonitoringEventCandidate(namespace, correction)
	if err != nil {
		t.Fatalf("build correction: %v", err)
	}
	if corrected.Corrects != originalCandidate.ActivityID {
		t.Fatalf("corrects = %q, want the original's projected id %q", corrected.Corrects, originalCandidate.ActivityID)
	}
	if corrected.ActivityID == originalCandidate.ActivityID {
		t.Fatalf("a correction must be its own row, not an overwrite of the original")
	}
}

func TestBuildMonitoringEventCandidateRejectsASelfCorrection(t *testing.T) {
	row := monitoringEventTestRow()
	row.eventType = string(incidents.EventCorrected)
	row.provenance = incidents.MonitoringEventProvenanceManualCorrection
	row.correctionOfEventID = row.eventID
	row.resultingState = "alert"

	if _, err := buildMonitoringEventCandidate(activityTestNamespace(), row); !errors.Is(err, activity.ErrInvalidSourceIdentity) {
		t.Fatalf("error = %v, want ErrInvalidSourceIdentity", err)
	}
}

// A backfilled event keeps its real occurrence time so it lands where it belongs
// on the timeline, while staying visibly late so it never triggers a
// notification as though it had just happened.
func TestBuildMonitoringEventCandidateKeepsABackfilledEventVisiblyLate(t *testing.T) {
	row := monitoringEventTestRow()
	row.backfilled = true
	row.provenance = incidents.MonitoringEventProvenanceRetentionBackfill
	row.recordedAt = row.eventAt.Add(6 * time.Hour)

	candidate, err := buildMonitoringEventCandidate(activityTestNamespace(), row)
	if err != nil {
		t.Fatalf("build candidate: %v", err)
	}
	if !candidate.Backfilled {
		t.Fatalf("a backfilled event must stay marked as late")
	}
	if !candidate.EventAt.Equal(row.eventAt) {
		t.Fatalf("event time = %s, want its real occurrence %s", candidate.EventAt, row.eventAt)
	}
	if !candidate.RecordedAt.After(candidate.EventAt) {
		t.Fatalf("a late event must record after it occurred")
	}
}

func TestBuildMonitoringEventCandidateCarriesNoFreeTextSummary(t *testing.T) {
	candidate, err := buildMonitoringEventCandidate(activityTestNamespace(), monitoringEventTestRow())
	if err != nil {
		t.Fatalf("build candidate: %v", err)
	}
	if candidate.Presentation.Title != monitoringEventTitles[incidents.EventIncidentStarted] {
		t.Fatalf("title = %q, want the fixed label", candidate.Presentation.Title)
	}
	if candidate.Presentation.Summary != "" {
		t.Fatalf("summary = %q: the writer's summary is composed from live host detail and must not be projected", candidate.Presentation.Summary)
	}
	if err := activity.ValidatePresentation(candidate.Presentation); err != nil {
		t.Fatalf("presentation is not a registered shape: %v", err)
	}
}

func TestBuildMonitoringEventCandidateRejectsRowsItCannotProject(t *testing.T) {
	for name, test := range map[string]struct {
		mutate func(*monitoringEventActivityRow)
		want   error
	}{
		"a payload claiming provenance no writer uses": {
			mutate: func(row *monitoringEventActivityRow) { row.provenance = "somewhere_else" },
			want:   activity.ErrInvalidEventKind,
		},
		"a state transition the contract forbids": {
			// incident_started must leave normal; claiming it started from alert is
			// not a transition any rule admits.
			mutate: func(row *monitoringEventActivityRow) { row.priorState = "alert" },
			want:   activity.ErrInvalidEventKind,
		},
		"an event type no writer emits": {
			mutate: func(row *monitoringEventActivityRow) { row.eventType = "incident_teleported" },
			want:   activity.ErrInvalidEventKind,
		},
		"a rule version outside the contract": {
			mutate: func(row *monitoringEventActivityRow) { row.ruleVersion = "incident-rules/v99" },
			want:   activity.ErrInvalidEventKind,
		},
		"a correction without correction provenance": {
			mutate: func(row *monitoringEventActivityRow) {
				row.eventType = string(incidents.EventCorrected)
				row.correctionOfEventID = "evt_incident0002"
			},
			want: activity.ErrInvalidEventKind,
		},
		"an object type that is not a subject": {
			mutate: func(row *monitoringEventActivityRow) {
				row.objectType = string(incidents.ObjectTypeSubscription)
			},
			want: activity.ErrInvalidEventKind,
		},
		"an object id no route resolves": {
			mutate: func(row *monitoringEventActivityRow) { row.objectID = "mi_short" },
			want:   activity.ErrUnreachableCandidate,
		},
	} {
		t.Run(name, func(t *testing.T) {
			row := monitoringEventTestRow()
			test.mutate(&row)
			if _, err := buildMonitoringEventCandidate(activityTestNamespace(), row); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

// Target events share the table and must project against the target subject
// kind, not be silently attributed to a monitoring instance.
func TestBuildMonitoringEventCandidateProjectsTargetEventsAgainstTargets(t *testing.T) {
	row := monitoringEventTestRow()
	row.objectType = string(incidents.ObjectTypeTarget)
	row.objectID = "tg_9f8e7d6c5b4a3210"
	row.eventType = string(incidents.EventTargetPaused)
	row.ruleVersion = incidents.MonitoringEventTargetRuleVersion
	row.severity = ""
	row.priorState = targets.RunStatusEnabled
	row.resultingState = targets.RunStatusPaused
	row.displayName = "edge-https"

	candidate, err := buildMonitoringEventCandidate(activityTestNamespace(), row)
	if err != nil {
		t.Fatalf("build candidate: %v", err)
	}
	if candidate.Subjects[0].Kind != records.SubjectKindTarget {
		t.Fatalf("subject kind = %q, want target", candidate.Subjects[0].Kind)
	}
	// Non-incident events carry no incident severity, so they project at the
	// baseline rather than inheriting one.
	if candidate.Severity != "info" {
		t.Fatalf("severity = %q, want info for an event with no incident severity", candidate.Severity)
	}
}

func TestBuildMonitoringEventCandidateProjectsRuntimeStateChanges(t *testing.T) {
	row := monitoringEventTestRow()
	row.eventType = string(incidents.EventMonitoringInstanceMonitoringPaused)
	row.ruleVersion = incidents.MonitoringEventRuntimeRuleVersion
	row.severity = ""
	row.priorState = monitoringinstances.MonitoringEnabled
	row.resultingState = monitoringinstances.MonitoringPaused

	candidate, err := buildMonitoringEventCandidate(activityTestNamespace(), row)
	if err != nil {
		t.Fatalf("build candidate: %v", err)
	}
	if candidate.Presentation.Title != monitoringEventTitles[incidents.EventMonitoringInstanceMonitoringPaused] {
		t.Fatalf("title = %q", candidate.Presentation.Title)
	}
}

func TestNewMonitoringEventActivitySourceRejectsUnusableConfiguration(t *testing.T) {
	if _, err := NewMonitoringEventActivitySource(nil, activityTestNamespace()); err == nil {
		t.Fatalf("a source without a pool must be rejected")
	}
}
