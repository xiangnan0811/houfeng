package records

import (
	"errors"
	"slices"
	"testing"
)

func TestStatusRegistryMapsBuiltinStatusesToCanonicalGroups(t *testing.T) {
	t.Parallel()

	tests := []struct {
		recordType RecordType
		status     BusinessStatus
		group      StatusGroup
	}{
		{RecordTypeTroubleshooting, StatusPendingInvestigation, StatusGroupPending},
		{RecordTypeTroubleshooting, StatusInvestigating, StatusGroupInProgress},
		{RecordTypeTroubleshooting, StatusVerifying, StatusGroupVerification},
		{RecordTypeTroubleshooting, StatusResolved, StatusGroupCompleted},
		{RecordTypeTroubleshooting, StatusClosed, StatusGroupCompleted},
		{RecordTypeTroubleshooting, StatusCancelled, StatusGroupCancelled},
		{RecordTypeMaintenance, StatusPlanned, StatusGroupPending},
		{RecordTypeMaintenance, StatusExecuting, StatusGroupInProgress},
		{RecordTypeMaintenance, StatusVerifying, StatusGroupVerification},
		{RecordTypeMaintenance, StatusCompleted, StatusGroupCompleted},
		{RecordTypeMaintenance, StatusCancelled, StatusGroupCancelled},
		{RecordTypeMigration, StatusPlanned, StatusGroupPending},
		{RecordTypeMigration, StatusExecuting, StatusGroupInProgress},
		{RecordTypeMigration, StatusVerifying, StatusGroupVerification},
		{RecordTypeMigration, StatusCompleted, StatusGroupCompleted},
		{RecordTypeMigration, StatusCancelled, StatusGroupCancelled},
		{RecordTypeProviderCommunication, StatusPendingContact, StatusGroupPending},
		{RecordTypeProviderCommunication, StatusWaitingProvider, StatusGroupWaiting},
		{RecordTypeProviderCommunication, StatusWaitingInternal, StatusGroupInProgress},
		{RecordTypeProviderCommunication, StatusResolved, StatusGroupCompleted},
		{RecordTypeProviderCommunication, StatusClosed, StatusGroupCompleted},
		{RecordTypeProviderCommunication, StatusCancelled, StatusGroupCancelled},
		{RecordTypeBilling, StatusPendingReview, StatusGroupPending},
		{RecordTypeBilling, StatusProcessing, StatusGroupInProgress},
		{RecordTypeBilling, StatusResolved, StatusGroupCompleted},
		{RecordTypeBilling, StatusClosed, StatusGroupCompleted},
		{RecordTypeBilling, StatusCancelled, StatusGroupCancelled},
	}

	for _, tt := range tests {
		t.Run(string(tt.recordType)+"/"+string(tt.status), func(t *testing.T) {
			group, err := StatusGroupFor(tt.recordType, tt.status)
			if err != nil {
				t.Fatalf("StatusGroupFor(%q, %q) error = %v", tt.recordType, tt.status, err)
			}
			if group != tt.group {
				t.Fatalf("StatusGroupFor(%q, %q) = %q, want %q", tt.recordType, tt.status, group, tt.group)
			}
		})
	}

	wantGroups := []StatusGroup{
		StatusGroupPending,
		StatusGroupInProgress,
		StatusGroupWaiting,
		StatusGroupVerification,
		StatusGroupCompleted,
		StatusGroupCancelled,
	}
	if got := CanonicalStatusGroups(); !slices.Equal(got, wantGroups) {
		t.Fatalf("CanonicalStatusGroups() = %#v, want %#v", got, wantGroups)
	}
	mutated := CanonicalStatusGroups()
	mutated[0] = StatusGroup("mutated")
	if got := CanonicalStatusGroups(); !slices.Equal(got, wantGroups) {
		t.Fatalf("CanonicalStatusGroups() changed through returned slice mutation: %#v", got)
	}
}

func TestNoStateRecordTypesOmitBusinessStatus(t *testing.T) {
	t.Parallel()

	for _, recordType := range []RecordType{RecordTypeImportantFinding, RecordTypeNote} {
		definition, ok := LookupRecordTypeDefinition(recordType)
		if !ok {
			t.Fatalf("LookupRecordTypeDefinition(%q) missing", recordType)
		}
		if definition.SupportsBusinessStatus || definition.DefaultStatus != "" || len(definition.Statuses) != 0 {
			t.Fatalf("definition for %q exposes an empty business status: %#v", recordType, definition)
		}
		group, err := StatusGroupFor(recordType, "")
		if err != nil || group != "" {
			t.Fatalf("StatusGroupFor(%q, empty) = %q, %v; want empty, nil", recordType, group, err)
		}
		if _, err := StatusGroupFor(recordType, StatusPlanned); !errors.Is(err, ErrInvalidBusinessStatus) {
			t.Fatalf("StatusGroupFor(%q, planned) error = %v, want ErrInvalidBusinessStatus", recordType, err)
		}
	}
}

func TestStatusRegistryRejectsUnknownTypeAndStatus(t *testing.T) {
	t.Parallel()

	if _, err := StatusGroupFor(RecordType("custom"), StatusPlanned); !errors.Is(err, ErrInvalidRecordType) {
		t.Fatalf("unknown type error = %v, want ErrInvalidRecordType", err)
	}
	if _, err := StatusGroupFor(RecordTypeBilling, StatusInvestigating); !errors.Is(err, ErrInvalidBusinessStatus) {
		t.Fatalf("cross-type status error = %v, want ErrInvalidBusinessStatus", err)
	}
	if _, err := StatusGroupFor(RecordTypeBilling, BusinessStatus(" Processing ")); !errors.Is(err, ErrInvalidBusinessStatus) {
		t.Fatalf("normalized unknown status error = %v, want ErrInvalidBusinessStatus", err)
	}
}

func TestStatusTransitionRequiresReasonOutsideRecommendedFlow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		recordType RecordType
		from       BusinessStatus
		to         BusinessStatus
		reason     string
		wantReason bool
	}{
		{name: "initial default", recordType: RecordTypeTroubleshooting, to: StatusPendingInvestigation},
		{name: "recommended next", recordType: RecordTypeTroubleshooting, from: StatusPendingInvestigation, to: StatusInvestigating},
		{name: "same status", recordType: RecordTypeTroubleshooting, from: StatusInvestigating, to: StatusInvestigating},
		{name: "skip without reason", recordType: RecordTypeTroubleshooting, from: StatusPendingInvestigation, to: StatusResolved, wantReason: true},
		{name: "skip with reason", recordType: RecordTypeTroubleshooting, from: StatusPendingInvestigation, to: StatusResolved, reason: "incident already resolved"},
		{name: "backward without reason", recordType: RecordTypeMaintenance, from: StatusVerifying, to: StatusExecuting, wantReason: true},
		{name: "reopen terminal without reason", recordType: RecordTypeBilling, from: StatusResolved, to: StatusProcessing, wantReason: true},
		{name: "reopen terminal with reason", recordType: RecordTypeBilling, from: StatusResolved, to: StatusProcessing, reason: "invoice changed"},
		{name: "cancel without reason", recordType: RecordTypeMigration, from: StatusExecuting, to: StatusCancelled, wantReason: true},
		{name: "cancel with reason", recordType: RecordTypeMigration, from: StatusExecuting, to: StatusCancelled, reason: "provider blocked migration"},
		{name: "provider wait handoff", recordType: RecordTypeProviderCommunication, from: StatusWaitingProvider, to: StatusWaitingInternal},
		{name: "provider resolves", recordType: RecordTypeProviderCommunication, from: StatusWaitingProvider, to: StatusResolved},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStatusTransition(tt.recordType, tt.from, tt.to, tt.reason)
			if tt.wantReason {
				if !errors.Is(err, ErrStatusTransitionReasonRequired) {
					t.Fatalf("ValidateStatusTransition() error = %v, want ErrStatusTransitionReasonRequired", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ValidateStatusTransition() error = %v", err)
			}
		})
	}
}

func TestTemplateRegistryReturnsDiffWithoutMutatingMarkdown(t *testing.T) {
	t.Parallel()

	fields := map[string]string{"impact_level": "high", "owner_id": "usr_0123456789abcdef01234567"}
	provenance := TemplateProvenance{ID: "troubleshooting_default", Version: 1}
	registry, err := NewTemplateRegistry([]TemplateDefinition{
		{
			Provenance:       provenance,
			RecordType:       RecordTypeTroubleshooting,
			Markdown:         "## Observation\n\n## Resolution\n",
			FieldSuggestions: fields,
		},
	})
	if err != nil {
		t.Fatalf("NewTemplateRegistry() error = %v", err)
	}

	fields["impact_level"] = "mutated"
	definition, ok := registry.Lookup(provenance)
	if !ok {
		t.Fatal("Lookup() missing registered template")
	}
	if got := definition.FieldSuggestions["impact_level"]; got != "high" {
		t.Fatalf("stored field suggestion = %q, want high", got)
	}
	definition.FieldSuggestions["impact_level"] = "returned mutation"
	if again, _ := registry.Lookup(provenance); again.FieldSuggestions["impact_level"] != "high" {
		t.Fatalf("registry changed through Lookup() result mutation: %#v", again.FieldSuggestions)
	}

	current := "Operator-authored body that must remain authoritative."
	diff, err := registry.Diff(RecordTypeTroubleshooting, provenance, current)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if diff.CurrentMarkdown != current {
		t.Fatalf("CurrentMarkdown = %q, want untouched %q", diff.CurrentMarkdown, current)
	}
	if diff.TemplateMarkdown != "## Observation\n\n## Resolution\n" {
		t.Fatalf("TemplateMarkdown = %q", diff.TemplateMarkdown)
	}
	if diff.CurrentMarkdown == diff.TemplateMarkdown {
		t.Fatal("Diff() silently replaced current Markdown")
	}
	diff.FieldSuggestions["impact_level"] = "diff mutation"
	if again, _ := registry.Lookup(provenance); again.FieldSuggestions["impact_level"] != "high" {
		t.Fatalf("registry changed through Diff() result mutation: %#v", again.FieldSuggestions)
	}
}

func TestTemplateRegistryRejectsInvalidDuplicateAndMismatchedTemplates(t *testing.T) {
	t.Parallel()

	valid := TemplateDefinition{
		Provenance: TemplateProvenance{ID: "maintenance_default", Version: 2},
		RecordType: RecordTypeMaintenance,
		Markdown:   "## Plan\n",
	}
	tests := []struct {
		name        string
		definitions []TemplateDefinition
	}{
		{name: "empty id", definitions: []TemplateDefinition{{Provenance: TemplateProvenance{Version: 1}, RecordType: RecordTypeMaintenance, Markdown: "body"}}},
		{name: "zero version", definitions: []TemplateDefinition{{Provenance: TemplateProvenance{ID: "maintenance_default"}, RecordType: RecordTypeMaintenance, Markdown: "body"}}},
		{name: "unknown type", definitions: []TemplateDefinition{{Provenance: TemplateProvenance{ID: "maintenance_default", Version: 1}, RecordType: "custom", Markdown: "body"}}},
		{name: "empty markdown", definitions: []TemplateDefinition{{Provenance: TemplateProvenance{ID: "maintenance_default", Version: 1}, RecordType: RecordTypeMaintenance}}},
		{name: "invalid markdown utf8", definitions: []TemplateDefinition{{Provenance: TemplateProvenance{ID: "maintenance_default", Version: 1}, RecordType: RecordTypeMaintenance, Markdown: string([]byte{0xff})}}},
		{name: "duplicate id and version", definitions: []TemplateDefinition{valid, valid}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewTemplateRegistry(tt.definitions); !errors.Is(err, ErrInvalidTemplate) {
				t.Fatalf("NewTemplateRegistry() error = %v, want ErrInvalidTemplate", err)
			}
		})
	}

	registry, err := NewTemplateRegistry([]TemplateDefinition{valid})
	if err != nil {
		t.Fatalf("NewTemplateRegistry(valid) error = %v", err)
	}
	if _, err := registry.Diff(RecordTypeMigration, valid.Provenance, "current"); !errors.Is(err, ErrInvalidTemplate) {
		t.Fatalf("Diff(mismatched type) error = %v, want ErrInvalidTemplate", err)
	}
	if _, err := registry.Diff(RecordTypeMaintenance, TemplateProvenance{ID: "maintenance_default", Version: 3}, "current"); !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("Diff(unknown version) error = %v, want ErrTemplateNotFound", err)
	}
}
