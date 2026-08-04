package records

import (
	"errors"
	"slices"
	"testing"
	"time"
)

func TestLifecycleRegistryIsClosed(t *testing.T) {
	t.Parallel()

	for _, lifecycle := range []Lifecycle{LifecycleActive, LifecycleArchived} {
		if err := ValidateLifecycle(lifecycle); err != nil {
			t.Fatalf("ValidateLifecycle(%q) error = %v", lifecycle, err)
		}
	}

	for _, lifecycle := range []Lifecycle{"", "ACTIVE", "working_draft", "deleted"} {
		if err := ValidateLifecycle(lifecycle); !errors.Is(err, ErrInvalidLifecycle) {
			t.Fatalf("ValidateLifecycle(%q) error = %v, want ErrInvalidLifecycle", lifecycle, err)
		}
	}
}

func TestBuiltinRecordTypeRegistryIsClosed(t *testing.T) {
	t.Parallel()

	want := []RecordType{
		RecordTypeTroubleshooting,
		RecordTypeMaintenance,
		RecordTypeMigration,
		RecordTypeProviderCommunication,
		RecordTypeBilling,
		RecordTypeImportantFinding,
		RecordTypeNote,
	}
	if got := BuiltinRecordTypes(); !slices.Equal(got, want) {
		t.Fatalf("BuiltinRecordTypes() = %#v, want %#v", got, want)
	}

	got := BuiltinRecordTypes()
	got[0] = RecordType("mutated")
	if again := BuiltinRecordTypes(); !slices.Equal(again, want) {
		t.Fatalf("BuiltinRecordTypes() changed through returned slice mutation: %#v", again)
	}

	for _, recordType := range want {
		if err := ValidateRecordType(recordType); err != nil {
			t.Fatalf("ValidateRecordType(%q) error = %v", recordType, err)
		}
	}
	for _, recordType := range []RecordType{"", "Troubleshooting", "custom"} {
		if err := ValidateRecordType(recordType); !errors.Is(err, ErrInvalidRecordType) {
			t.Fatalf("ValidateRecordType(%q) error = %v, want ErrInvalidRecordType", recordType, err)
		}
	}
}

func TestNormalizeCompleteRevisionInputKeepsFieldsImmutableAndUTC(t *testing.T) {
	t.Parallel()

	values := validCompleteRevisionValues(t)
	wantOccurredAt := values.OccurredAt.UTC()
	wantCompletedAt := values.CompletedAt.UTC()
	wantFollowUpAt := values.FollowUpAt.UTC()

	got, err := NormalizeCompleteRevisionInput(values)
	if err != nil {
		t.Fatalf("NormalizeCompleteRevisionInput() error = %v", err)
	}
	if got.Title() != "Provider packet loss" {
		t.Fatalf("Title() = %q", got.Title())
	}
	if got.BodyMarkdown() != "  packet loss details\n" {
		t.Fatalf("BodyMarkdown() = %q", got.BodyMarkdown())
	}
	if got.MarkdownDialectVersion() != MarkdownDialectVersionV1 ||
		got.RecordType() != RecordTypeTroubleshooting ||
		got.BusinessStatus() != StatusResolved ||
		got.StatusGroup() != StatusGroupCompleted ||
		got.ImpactLevel() != ImpactLevel("high") {
		t.Fatalf("normalized scalar fields = dialect:%d type:%q status:%q group:%q impact:%q",
			got.MarkdownDialectVersion(), got.RecordType(), got.BusinessStatus(), got.StatusGroup(), got.ImpactLevel())
	}
	if occurredAt := got.OccurredAt(); occurredAt == nil || !occurredAt.Equal(wantOccurredAt) || occurredAt.Location() != time.UTC {
		t.Fatalf("OccurredAt() = %#v, want UTC %v", occurredAt, wantOccurredAt)
	}
	if completedAt := got.CompletedAt(); completedAt == nil || !completedAt.Equal(wantCompletedAt) || completedAt.Location() != time.UTC {
		t.Fatalf("CompletedAt() = %#v, want UTC %v", completedAt, wantCompletedAt)
	}
	if followUpAt := got.FollowUpAt(); followUpAt == nil || !followUpAt.Equal(wantFollowUpAt) || followUpAt.Location() != time.UTC {
		t.Fatalf("FollowUpAt() = %#v, want UTC %v", followUpAt, wantFollowUpAt)
	}
	if got.OwnerID() != testRecordOwnerID || got.AuthorID() != testRecordAuthorID || got.SaveReason() != "provider confirmed resolution" {
		t.Fatalf("normalized ownership = owner:%q author:%q reason:%q", got.OwnerID(), got.AuthorID(), got.SaveReason())
	}
	if template := got.Template(); template == nil || *template != (TemplateProvenance{ID: "troubleshooting_default", Version: 2}) {
		t.Fatalf("Template() = %#v", template)
	}
	if tags := got.Tags(); !slices.Equal(tags, []string{"network", "provider"}) {
		t.Fatalf("Tags() = %#v", tags)
	}

	values.Subjects[0].IdentitySnapshot["display_name"] = "input mutation"
	values.Subjects[0].CaptureAuthorization.CurrentScope.AllowedGroupIDs[0] = "rag_mutated"
	values.Tags[0] = "input mutation"
	values.Participants[0].IdentitySnapshot["display_name"] = "input mutation"
	values.VisibilityScope.AllowedGroupIDs[0] = "rag_mutated"
	values.Template.ID = "input_mutation"
	*values.OccurredAt = values.OccurredAt.Add(24 * time.Hour)

	subjects := got.Subjects()
	if subjects[0].IdentitySnapshot["display_name"] != "VPS Alpha" ||
		subjects[0].CaptureAuthorization.CurrentScope.AllowedGroupIDs[0] != testRecordGroupID {
		t.Fatalf("Subjects() changed through constructor input mutation: %#v", subjects)
	}
	participants := got.Participants()
	if participants[0].IdentitySnapshot["display_name"] != "Operator Two" {
		t.Fatalf("Participants() changed through constructor input mutation: %#v", participants)
	}
	if visibility := got.VisibilityScope(); visibility.AllowedGroupIDs[0] != testRecordGroupID {
		t.Fatalf("VisibilityScope() changed through constructor input mutation: %#v", visibility)
	}

	subjects[0].IdentitySnapshot["display_name"] = "return mutation"
	subjects[0].CaptureAuthorization.CurrentScope.AllowedGroupIDs[0] = "rag_returned"
	participants[0].IdentitySnapshot["display_name"] = "return mutation"
	tags := got.Tags()
	tags[0] = "return mutation"
	visibility := got.VisibilityScope()
	visibility.AllowedGroupIDs[0] = "rag_returned"
	template := got.Template()
	template.ID = "return_mutation"
	returnedOccurredAt := got.OccurredAt()
	*returnedOccurredAt = returnedOccurredAt.Add(24 * time.Hour)

	if again := got.Subjects(); again[0].IdentitySnapshot["display_name"] != "VPS Alpha" ||
		again[0].CaptureAuthorization.CurrentScope.AllowedGroupIDs[0] != testRecordGroupID {
		t.Fatalf("stored subjects changed through returned copy mutation: %#v", again)
	}
	if again := got.Participants(); again[0].IdentitySnapshot["display_name"] != "Operator Two" {
		t.Fatalf("stored participants changed through returned copy mutation: %#v", again)
	}
	if again := got.Tags(); !slices.Equal(again, []string{"network", "provider"}) {
		t.Fatalf("stored tags changed through returned copy mutation: %#v", again)
	}
	if again := got.VisibilityScope(); again.AllowedGroupIDs[0] != testRecordGroupID {
		t.Fatalf("stored visibility changed through returned copy mutation: %#v", again)
	}
	if again := got.Template(); again.ID != "troubleshooting_default" {
		t.Fatalf("stored template changed through returned copy mutation: %#v", again)
	}
	if again := got.OccurredAt(); !again.Equal(wantOccurredAt) {
		t.Fatalf("stored occurred time changed through returned pointer mutation: %v", again)
	}
}

func TestCompleteRevisionCanonicalHashIsDeterministicAndContentScoped(t *testing.T) {
	t.Parallel()

	baseValues := validCompleteRevisionValues(t)
	base := mustCompleteRevisionInput(t, baseValues)

	equivalentValues := validCompleteRevisionValues(t)
	utcOccurredAt := equivalentValues.OccurredAt.UTC()
	utcCompletedAt := equivalentValues.CompletedAt.UTC()
	utcFollowUpAt := equivalentValues.FollowUpAt.UTC()
	equivalentValues.OccurredAt = &utcOccurredAt
	equivalentValues.CompletedAt = &utcCompletedAt
	equivalentValues.FollowUpAt = &utcFollowUpAt
	equivalentValues.Subjects[0].IdentitySnapshot = map[string]string{
		"provider":     "Example Cloud",
		"display_name": "VPS Alpha",
	}
	equivalent := mustCompleteRevisionInput(t, equivalentValues)
	if base.CanonicalHash() != equivalent.CanonicalHash() {
		t.Fatalf("equivalent inputs have different hashes: %x != %x", base.CanonicalHash(), equivalent.CanonicalHash())
	}

	metadataOnlyValues := validCompleteRevisionValues(t)
	metadataOnlyValues.AuthorID = "usr_aaaaaaaaaaaaaaaaaaaaaaaa"
	metadataOnlyValues.SaveReason = "different immutable commit metadata"
	metadataOnly := mustCompleteRevisionInput(t, metadataOnlyValues)
	if base.CanonicalHash() != metadataOnly.CanonicalHash() {
		t.Fatalf("author/save reason changed content hash: %x != %x", base.CanonicalHash(), metadataOnly.CanonicalHash())
	}

	contentMutations := []struct {
		name   string
		mutate func(*CompleteRevisionValues)
	}{
		{name: "title", mutate: func(values *CompleteRevisionValues) { values.Title = "Different title" }},
		{name: "markdown", mutate: func(values *CompleteRevisionValues) { values.BodyMarkdown += "changed" }},
		{name: "business status", mutate: func(values *CompleteRevisionValues) { values.BusinessStatus = StatusClosed }},
		{name: "tag order", mutate: func(values *CompleteRevisionValues) { values.Tags[0], values.Tags[1] = values.Tags[1], values.Tags[0] }},
		{name: "subject snapshot", mutate: func(values *CompleteRevisionValues) { values.Subjects[0].IdentitySnapshot["provider"] = "Other Cloud" }},
		{name: "participant order", mutate: func(values *CompleteRevisionValues) {
			values.Participants = append(values.Participants, RevisionParticipantSnapshot{ParticipantID: "usr_bbbbbbbbbbbbbbbbbbbbbbbb", IdentitySnapshot: map[string]string{"display_name": "Operator Three"}})
		}},
		{name: "template", mutate: func(values *CompleteRevisionValues) { values.Template.Version++ }},
	}
	for _, tt := range contentMutations {
		t.Run(tt.name, func(t *testing.T) {
			values := validCompleteRevisionValues(t)
			tt.mutate(&values)
			mutated := mustCompleteRevisionInput(t, values)
			if base.CanonicalHash() == mutated.CanonicalHash() {
				t.Fatalf("%s mutation did not change canonical hash %x", tt.name, base.CanonicalHash())
			}
		})
	}
}
