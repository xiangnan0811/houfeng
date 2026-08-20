package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/activity"
	"houfeng/internal/center/records"
	"houfeng/internal/contracts/agentapi"
)

func commandAuditTestRow() commandAuditActivityRow {
	return commandAuditActivityRow{
		auditID:              "cmda_0001",
		monitoringInstanceID: testMonitoringInstanceSourceID,
		instanceNameSnapshot: "hk-edge-01",
		liveInstanceName:     "hk-edge-01-renamed",
		commandID:            "df_h",
		sensitivity:          "standard",
		eventType:            "completed",
		actorUserID:          "usr_000000000000000000000001",
		actorDisplayName:     "Alan",
		occurredAt:           time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC),
	}
}

func exitCode(code int32) *int32 { return &code }

// Every phase the schema allows must have a label. A phase without one would be
// dropped, and a command dispatched but never completed is precisely the case an
// operator needs the timeline to show.
func TestCommandAuditSourceLabelsEveryPhaseTheSchemaAllows(t *testing.T) {
	for _, phase := range []string{"queued", "dispatched", "completed"} {
		if _, known := commandAuditPhaseTitles[phase]; !known {
			t.Errorf("schema allows phase %q but the source has no label for it", phase)
		}
	}
	if got, want := len(commandAuditPhaseTitles), 3; got != want {
		t.Fatalf("source labels %d phases, want the schema's %d", got, want)
	}
}

func TestBuildCommandAuditCandidateProjectsMetadataOnly(t *testing.T) {
	row := commandAuditTestRow()

	candidate, err := buildCommandAuditCandidate(activityTestNamespace(), row)
	if err != nil {
		t.Fatalf("build candidate: %v", err)
	}

	if candidate.EventKind != activity.EventKindCommandExecuted {
		t.Fatalf("event kind = %q, want command_executed", candidate.EventKind)
	}
	// The audit row is written in the same transaction as the moment it records,
	// so there is no separate save time to distinguish.
	if !candidate.EventAt.Equal(row.occurredAt) || !candidate.RecordedAt.Equal(row.occurredAt) {
		t.Fatalf("times = %s / %s, want both at %s", candidate.EventAt, candidate.RecordedAt, row.occurredAt)
	}
	if candidate.Presentation.Summary != "df_h" {
		t.Fatalf("summary = %q, want the catalog command id", candidate.Presentation.Summary)
	}
	if !agentapi.IsKnownCommandID(candidate.Presentation.Summary) {
		t.Fatalf("the projected summary must be a catalog id, got %q", candidate.Presentation.Summary)
	}
	if candidate.Actor == nil || candidate.Actor.ActorID != row.actorUserID {
		t.Fatalf("actor = %+v, want the operator who ran it", candidate.Actor)
	}
	if candidate.Actor.DisplayName != "Alan" {
		t.Fatalf("actor display name = %q", candidate.Actor.DisplayName)
	}
	subject := candidate.Subjects[0]
	if subject.Kind != records.SubjectKindMonitoringInstance || !subject.Primary {
		t.Fatalf("subject = %+v", subject)
	}
	// The snapshot is what the instance was called when the command ran; a later
	// rename must not rewrite history.
	if subject.Identity["display_name"] != "hk-edge-01" {
		t.Fatalf("identity = %v, want the captured name rather than the current one", subject.Identity)
	}
}

func TestBuildCommandAuditCandidateFallsBackToTheLiveNameOnlyWhenNothingWasCaptured(t *testing.T) {
	row := commandAuditTestRow()
	row.instanceNameSnapshot = ""

	candidate, err := buildCommandAuditCandidate(activityTestNamespace(), row)
	if err != nil {
		t.Fatalf("build candidate: %v", err)
	}
	if candidate.Subjects[0].Identity["display_name"] != "hk-edge-01-renamed" {
		t.Fatalf("identity = %v, want the live name as a fallback", candidate.Subjects[0].Identity)
	}
}

// Each phase is its own row with its own time, so each must project to its own
// activity rather than collapsing onto one id per action.
func TestBuildCommandAuditCandidateGivesEachPhaseItsOwnIdentity(t *testing.T) {
	namespace := activityTestNamespace()
	seen := map[string]string{}
	for _, phase := range []string{"queued", "dispatched", "completed"} {
		row := commandAuditTestRow()
		row.eventType = phase
		row.auditID = "cmda_" + phase

		candidate, err := buildCommandAuditCandidate(namespace, row)
		if err != nil {
			t.Fatalf("build %s: %v", phase, err)
		}
		if previous, clash := seen[candidate.ActivityID]; clash {
			t.Fatalf("phase %q collides with %q on id %s", phase, previous, candidate.ActivityID)
		}
		seen[candidate.ActivityID] = phase
		if candidate.Presentation.Title != commandAuditPhaseTitles[phase] {
			t.Fatalf("phase %q title = %q", phase, candidate.Presentation.Title)
		}
	}
}

// A failed diagnostic is the row an operator is scanning for, and the exit code
// is metadata rather than output, so it may raise severity.
func TestBuildCommandAuditCandidateRaisesAFailedCommand(t *testing.T) {
	for name, test := range map[string]struct {
		phase string
		code  *int32
		want  string
	}{
		"a command that exited non-zero":  {phase: "completed", code: exitCode(1), want: "warning"},
		"a command that exited cleanly":   {phase: "completed", code: exitCode(0), want: "info"},
		"a completed row with no code":    {phase: "completed", code: nil, want: "info"},
		"a queued row cannot have exited": {phase: "queued", code: exitCode(1), want: "info"},
	} {
		t.Run(name, func(t *testing.T) {
			row := commandAuditTestRow()
			row.eventType = test.phase
			row.exitCode = test.code

			candidate, err := buildCommandAuditCandidate(activityTestNamespace(), row)
			if err != nil {
				t.Fatalf("build candidate: %v", err)
			}
			if candidate.Severity != test.want {
				t.Fatalf("severity = %q, want %q", candidate.Severity, test.want)
			}
		})
	}
}

// An agent-initiated phase has no user behind it. Leaving the actor unset says
// so; inventing one would attribute the command to somebody who did not run it.
func TestBuildCommandAuditCandidateLeavesAnAgentPhaseUnattributed(t *testing.T) {
	row := commandAuditTestRow()
	row.actorUserID = ""
	row.actorDisplayName = ""

	candidate, err := buildCommandAuditCandidate(activityTestNamespace(), row)
	if err != nil {
		t.Fatalf("build candidate: %v", err)
	}
	if candidate.Actor != nil {
		t.Fatalf("actor = %+v, want none", candidate.Actor)
	}
}

func TestBuildCommandAuditCandidateRejectsRowsItCannotProject(t *testing.T) {
	for name, test := range map[string]struct {
		mutate func(*commandAuditActivityRow)
		want   error
	}{
		"a phase the schema does not allow": {
			mutate: func(row *commandAuditActivityRow) { row.eventType = "abandoned" },
			want:   activity.ErrInvalidEventKind,
		},
		"a command outside the catalog": {
			mutate: func(row *commandAuditActivityRow) { row.commandID = "rm_rf_slash" },
			want:   activity.ErrInvalidEventKind,
		},
		"an instance id no route resolves": {
			mutate: func(row *commandAuditActivityRow) { row.monitoringInstanceID = "mi_nope" },
			want:   activity.ErrUnreachableCandidate,
		},
	} {
		t.Run(name, func(t *testing.T) {
			row := commandAuditTestRow()
			test.mutate(&row)
			if _, err := buildCommandAuditCandidate(activityTestNamespace(), row); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

// The audit's details payload holds command context and must never be read: a
// timeline is metadata about what ran, not a copy of what it printed.
func TestCommandAuditSourceNeverReadsCommandOutput(t *testing.T) {
	for _, forbidden := range []string{"details", "stdout", "stderr", "output"} {
		if strings.Contains(commandAuditActivityScanSQL, forbidden) {
			t.Errorf("the scan must not read %q", forbidden)
		}
	}
}

func TestNewCommandAuditActivitySourceRejectsUnusableConfiguration(t *testing.T) {
	if _, err := NewCommandAuditActivitySource(nil, activityTestNamespace()); err == nil {
		t.Fatal("a source without a pool must be rejected")
	}
	if _, err := NewCommandAuditActivitySource(&pgxpool.Pool{}, activity.Namespace{}); err == nil {
		t.Fatal("a source without a project namespace must be rejected")
	}
}
