package records

import (
	"errors"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
)

const (
	testRecordAuthorID      = "usr_0123456789abcdef01234567"
	testRecordOwnerID       = "usr_89abcdef0123456701234567"
	testRecordParticipantID = "usr_fedcba987654321001234567"
	testRecordGroupID       = "rag_records"
	testRecordVPSID         = "vps_0123456789abcdef"
	testRecordAttachmentID1 = "att_0123456789abcdef"
	testRecordAttachmentID2 = "att_fedcba9876543210"
)

func TestNormalizeCompleteRevisionInputRejectsContradictoryOrDuplicateValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*CompleteRevisionValues)
	}{
		{name: "empty title", mutate: func(values *CompleteRevisionValues) { values.Title = " \t\n" }},
		{name: "invalid markdown utf8", mutate: func(values *CompleteRevisionValues) { values.BodyMarkdown = string([]byte{0xff}) }},
		{name: "unknown dialect", mutate: func(values *CompleteRevisionValues) { values.MarkdownDialectVersion = 2 }},
		{name: "unknown type", mutate: func(values *CompleteRevisionValues) { values.RecordType = "custom" }},
		{name: "cross type status", mutate: func(values *CompleteRevisionValues) {
			values.RecordType = RecordTypeBilling
			values.BusinessStatus = StatusInvestigating
		}},
		{name: "state on no-state type", mutate: func(values *CompleteRevisionValues) { values.RecordType = RecordTypeNote }},
		{name: "completion missing time", mutate: func(values *CompleteRevisionValues) { values.CompletedAt = nil }},
		{name: "non-completion carries time", mutate: func(values *CompleteRevisionValues) { values.BusinessStatus = StatusInvestigating }},
		{name: "completion before occurrence", mutate: func(values *CompleteRevisionValues) {
			before := values.OccurredAt.Add(-time.Second)
			values.CompletedAt = &before
		}},
		{name: "cancel without reason", mutate: func(values *CompleteRevisionValues) {
			values.BusinessStatus = StatusCancelled
			values.CompletedAt = nil
			values.SaveReason = ""
		}},
		{name: "invalid impact", mutate: func(values *CompleteRevisionValues) { values.ImpactLevel = "High Risk" }},
		{name: "invalid visibility", mutate: func(values *CompleteRevisionValues) { values.VisibilityScope.PolicyRevision = 0 }},
		{name: "no subjects", mutate: func(values *CompleteRevisionValues) { values.Subjects = nil }},
		{name: "unknown subject registry version", mutate: func(values *CompleteRevisionValues) { values.Subjects[0].RegistryVersion = 2 }},
		{name: "unknown subject relation role", mutate: func(values *CompleteRevisionValues) { values.Subjects[0].Role = "trigger" }},
		{name: "client authorization in subject snapshot", mutate: func(values *CompleteRevisionValues) {
			values.Subjects[0].IdentitySnapshot["allowed_groups"] = testRecordGroupID
		}},
		{name: "tampered subject capture digest", mutate: func(values *CompleteRevisionValues) {
			values.Subjects[0].CaptureAuthorization.Digest[0] ^= 0xff
		}},
		{name: "no primary subject", mutate: func(values *CompleteRevisionValues) { values.Subjects[0].Primary = false }},
		{name: "multiple primary subjects", mutate: func(values *CompleteRevisionValues) {
			related := cloneRevisionSubjectForTest(values.Subjects[0])
			related.SourceID = "vps_fedcba9876543210"
			related.CaptureAuthorization.SourceID = related.SourceID
			values.Subjects = append(values.Subjects, related)
		}},
		{name: "duplicate relation", mutate: func(values *CompleteRevisionValues) {
			duplicate := cloneRevisionSubjectForTest(values.Subjects[0])
			duplicate.Primary = false
			values.Subjects = append(values.Subjects, duplicate)
		}},
		{name: "duplicate normalized tag", mutate: func(values *CompleteRevisionValues) { values.Tags = append(values.Tags, " NETWORK ") }},
		{name: "invalid participant", mutate: func(values *CompleteRevisionValues) { values.Participants[0].ParticipantID = "usr_invalid" }},
		{name: "duplicate participant", mutate: func(values *CompleteRevisionValues) {
			values.Participants = append(values.Participants, cloneRevisionParticipantForTest(values.Participants[0]))
		}},
		{name: "invalid attachment", mutate: func(values *CompleteRevisionValues) { values.AttachmentIDs[0] = "att_INVALID" }},
		{name: "duplicate attachment", mutate: func(values *CompleteRevisionValues) {
			values.AttachmentIDs = append(values.AttachmentIDs, values.AttachmentIDs[0])
		}},
		{name: "invalid owner", mutate: func(values *CompleteRevisionValues) { values.OwnerID = "usr_invalid" }},
		{name: "invalid author", mutate: func(values *CompleteRevisionValues) { values.AuthorID = "usr_invalid" }},
		{name: "invalid template", mutate: func(values *CompleteRevisionValues) { values.Template.Version = 0 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := validCompleteRevisionValues(t)
			tt.mutate(&values)
			if _, err := NormalizeCompleteRevisionInput(values); !errors.Is(err, ErrInvalidRevisionInput) {
				t.Fatalf("NormalizeCompleteRevisionInput() error = %v, want ErrInvalidRevisionInput", err)
			}
		})
	}
}

func TestNormalizeCompleteRevisionInputAllowsNoStateTypesWithoutPlaceholders(t *testing.T) {
	t.Parallel()

	values := validCompleteRevisionValues(t)
	values.RecordType = RecordTypeImportantFinding
	values.BusinessStatus = ""
	values.CompletedAt = nil
	values.SaveReason = ""

	got, err := NormalizeCompleteRevisionInput(values)
	if err != nil {
		t.Fatalf("NormalizeCompleteRevisionInput() error = %v", err)
	}
	if got.BusinessStatus() != "" || got.StatusGroup() != "" {
		t.Fatalf("no-state normalized status = %q/%q, want omitted", got.BusinessStatus(), got.StatusGroup())
	}
}

func TestNormalizeCompleteRevisionInputAcceptsWitnessedTombstonedSubjectAuthorization(t *testing.T) {
	t.Parallel()

	values := validCompleteRevisionValues(t)
	live := values.Subjects[0].CaptureAuthorization
	lastLive := *live.CurrentScope
	floor := lastLive
	tombstoned, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version:       live.Version,
		Kind:          live.Kind,
		SourceID:      live.SourceID,
		State:         recordauth.SourceStateTombstoned,
		CaptureScope:  live.CaptureScope,
		FinalFloor:    &floor,
		LastLiveScope: &lastLive,
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	values.Subjects[0].CaptureAuthorization = tombstoned

	got, err := NormalizeCompleteRevisionInput(values)
	if err != nil {
		t.Fatalf("NormalizeCompleteRevisionInput() error = %v", err)
	}
	source := got.Subjects()[0].CaptureAuthorization
	if source.State != recordauth.SourceStateTombstoned || source.CurrentScope != nil ||
		source.FinalFloor == nil || source.LastLiveScope == nil {
		t.Fatalf("normalized source authorization = %#v, want complete tombstoned union", source)
	}
}

func validCompleteRevisionValues(t *testing.T) CompleteRevisionValues {
	t.Helper()

	location := time.FixedZone("UTC+8", 8*60*60)
	occurredAt := time.Date(2026, time.August, 3, 10, 15, 0, 123, location)
	completedAt := time.Date(2026, time.August, 3, 11, 45, 0, 456, location)
	followUpAt := time.Date(2026, time.August, 10, 9, 0, 0, 789, location)
	visibility := mustRecordVisibility(t)
	capture := mustRecordSourceAuthorization(t, visibility)
	template := TemplateProvenance{ID: "troubleshooting_default", Version: 2}

	return CompleteRevisionValues{
		Title:                  "  Provider packet loss  ",
		BodyMarkdown:           "  packet loss details\n",
		MarkdownDialectVersion: MarkdownDialectVersionV1,
		RecordType:             RecordTypeTroubleshooting,
		BusinessStatus:         StatusResolved,
		ImpactLevel:            ImpactLevel("high"),
		OccurredAt:             &occurredAt,
		CompletedAt:            &completedAt,
		VisibilityScope:        visibility,
		Subjects: []RevisionSubject{
			{
				RegistryVersion:      1,
				Kind:                 SubjectKind("vps"),
				Role:                 RelationRole("affected"),
				SourceID:             testRecordVPSID,
				Primary:              true,
				IdentitySnapshot:     map[string]string{"display_name": "VPS Alpha", "provider": "Example Cloud"},
				CaptureAuthorization: capture,
			},
		},
		Tags:    []string{" Network ", "PROVIDER"},
		OwnerID: testRecordOwnerID,
		Participants: []RevisionParticipantSnapshot{
			{ParticipantID: testRecordParticipantID, IdentitySnapshot: map[string]string{"display_name": "Operator Two"}},
		},
		AttachmentIDs: []string{testRecordAttachmentID1, testRecordAttachmentID2},
		FollowUpAt:    &followUpAt,
		Template:      &template,
		AuthorID:      testRecordAuthorID,
		SaveReason:    "  provider confirmed resolution  ",
	}
}

func mustCompleteRevisionInput(t *testing.T, values CompleteRevisionValues) CompleteRevisionInput {
	t.Helper()
	input, err := NormalizeCompleteRevisionInput(values)
	if err != nil {
		t.Fatalf("NormalizeCompleteRevisionInput() error = %v", err)
	}
	return input
}

func mustRecordVisibility(t *testing.T) recordauth.VisibilityScope {
	t.Helper()
	scope, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version:         recordauth.VisibilityScopeVersionV1,
		Kind:            recordauth.VisibilityKindRestricted,
		ProjectID:       recordauth.ProjectIDDefault,
		AllowedGroupIDs: []string{testRecordGroupID},
		PolicyVersion:   recordauth.PolicyVersionV1,
		PolicyRevision:  7,
	})
	if err != nil {
		t.Fatalf("NormalizeVisibilityScope() error = %v", err)
	}
	return scope
}

func mustRecordSourceAuthorization(t *testing.T, scope recordauth.VisibilityScope) recordauth.SourceAuthorization {
	t.Helper()
	current := scope
	source, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version:      recordauth.SourceAuthorizationVersionV1,
		Kind:         recordauth.SourceKindVPS,
		SourceID:     testRecordVPSID,
		State:        recordauth.SourceStateLive,
		CaptureScope: scope,
		CurrentScope: &current,
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	return source
}

func cloneRevisionSubjectForTest(subject RevisionSubject) RevisionSubject {
	cloned := subject
	cloned.IdentitySnapshot = cloneStringMap(subject.IdentitySnapshot)
	return cloned
}

func cloneRevisionParticipantForTest(participant RevisionParticipantSnapshot) RevisionParticipantSnapshot {
	cloned := participant
	cloned.IdentitySnapshot = cloneStringMap(participant.IdentitySnapshot)
	return cloned
}
