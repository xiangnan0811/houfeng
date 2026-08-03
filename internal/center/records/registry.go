package records

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

var builtinRecordTypes = [...]RecordType{
	RecordTypeTroubleshooting,
	RecordTypeMaintenance,
	RecordTypeMigration,
	RecordTypeProviderCommunication,
	RecordTypeBilling,
	RecordTypeImportantFinding,
	RecordTypeNote,
}

var canonicalStatusGroups = [...]StatusGroup{
	StatusGroupPending,
	StatusGroupInProgress,
	StatusGroupWaiting,
	StatusGroupVerification,
	StatusGroupCompleted,
	StatusGroupCancelled,
}

type StatusDefinition struct {
	Status BusinessStatus
	Group  StatusGroup
}

type RecordTypeDefinition struct {
	Type                   RecordType
	SupportsBusinessStatus bool
	DefaultStatus          BusinessStatus
	Statuses               []StatusDefinition
}

type recordTypeSpec struct {
	definition      RecordTypeDefinition
	recommendedNext map[BusinessStatus][]BusinessStatus
}

type TemplateDefinition struct {
	Provenance       TemplateProvenance
	RecordType       RecordType
	Markdown         string
	FieldSuggestions map[string]string
}

type TemplateDiff struct {
	Provenance       TemplateProvenance
	RecordType       RecordType
	CurrentMarkdown  string
	TemplateMarkdown string
	FieldSuggestions map[string]string
}

type templateKey struct {
	id      string
	version uint64
}

type TemplateRegistry struct {
	definitions map[templateKey]TemplateDefinition
}

var recordTypeSpecs = [...]recordTypeSpec{
	{
		definition: RecordTypeDefinition{
			Type:                   RecordTypeTroubleshooting,
			SupportsBusinessStatus: true,
			DefaultStatus:          StatusPendingInvestigation,
			Statuses: []StatusDefinition{
				{Status: StatusPendingInvestigation, Group: StatusGroupPending},
				{Status: StatusInvestigating, Group: StatusGroupInProgress},
				{Status: StatusVerifying, Group: StatusGroupVerification},
				{Status: StatusResolved, Group: StatusGroupCompleted},
				{Status: StatusClosed, Group: StatusGroupCompleted},
				{Status: StatusCancelled, Group: StatusGroupCancelled},
			},
		},
		recommendedNext: map[BusinessStatus][]BusinessStatus{
			StatusPendingInvestigation: {StatusInvestigating},
			StatusInvestigating:        {StatusVerifying},
			StatusVerifying:            {StatusResolved, StatusClosed},
		},
	},
	{
		definition: workflowRecordTypeDefinition(RecordTypeMaintenance),
		recommendedNext: map[BusinessStatus][]BusinessStatus{
			StatusPlanned:   {StatusExecuting},
			StatusExecuting: {StatusVerifying},
			StatusVerifying: {StatusCompleted},
		},
	},
	{
		definition: workflowRecordTypeDefinition(RecordTypeMigration),
		recommendedNext: map[BusinessStatus][]BusinessStatus{
			StatusPlanned:   {StatusExecuting},
			StatusExecuting: {StatusVerifying},
			StatusVerifying: {StatusCompleted},
		},
	},
	{
		definition: RecordTypeDefinition{
			Type:                   RecordTypeProviderCommunication,
			SupportsBusinessStatus: true,
			DefaultStatus:          StatusPendingContact,
			Statuses: []StatusDefinition{
				{Status: StatusPendingContact, Group: StatusGroupPending},
				{Status: StatusWaitingProvider, Group: StatusGroupWaiting},
				{Status: StatusWaitingInternal, Group: StatusGroupInProgress},
				{Status: StatusResolved, Group: StatusGroupCompleted},
				{Status: StatusClosed, Group: StatusGroupCompleted},
				{Status: StatusCancelled, Group: StatusGroupCancelled},
			},
		},
		recommendedNext: map[BusinessStatus][]BusinessStatus{
			StatusPendingContact:  {StatusWaitingProvider, StatusWaitingInternal},
			StatusWaitingProvider: {StatusWaitingInternal, StatusResolved, StatusClosed},
			StatusWaitingInternal: {StatusWaitingProvider, StatusResolved, StatusClosed},
		},
	},
	{
		definition: RecordTypeDefinition{
			Type:                   RecordTypeBilling,
			SupportsBusinessStatus: true,
			DefaultStatus:          StatusPendingReview,
			Statuses: []StatusDefinition{
				{Status: StatusPendingReview, Group: StatusGroupPending},
				{Status: StatusProcessing, Group: StatusGroupInProgress},
				{Status: StatusResolved, Group: StatusGroupCompleted},
				{Status: StatusClosed, Group: StatusGroupCompleted},
				{Status: StatusCancelled, Group: StatusGroupCancelled},
			},
		},
		recommendedNext: map[BusinessStatus][]BusinessStatus{
			StatusPendingReview: {StatusProcessing},
			StatusProcessing:    {StatusResolved, StatusClosed},
		},
	},
	{definition: RecordTypeDefinition{Type: RecordTypeImportantFinding}},
	{definition: RecordTypeDefinition{Type: RecordTypeNote}},
}

func BuiltinRecordTypes() []RecordType {
	return append([]RecordType(nil), builtinRecordTypes[:]...)
}

func CanonicalStatusGroups() []StatusGroup {
	return append([]StatusGroup(nil), canonicalStatusGroups[:]...)
}

func ValidateRecordType(recordType RecordType) error {
	for _, candidate := range builtinRecordTypes {
		if recordType == candidate {
			return nil
		}
	}
	return fmt.Errorf("%w: %q", ErrInvalidRecordType, recordType)
}

func LookupRecordTypeDefinition(recordType RecordType) (RecordTypeDefinition, bool) {
	spec, ok := lookupRecordTypeSpec(recordType)
	if !ok {
		return RecordTypeDefinition{}, false
	}
	definition := spec.definition
	definition.Statuses = append([]StatusDefinition(nil), spec.definition.Statuses...)
	return definition, true
}

func StatusGroupFor(recordType RecordType, status BusinessStatus) (StatusGroup, error) {
	spec, ok := lookupRecordTypeSpec(recordType)
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrInvalidRecordType, recordType)
	}
	if !spec.definition.SupportsBusinessStatus {
		if status == "" {
			return "", nil
		}
		return "", fmt.Errorf("%w: %q for %q", ErrInvalidBusinessStatus, status, recordType)
	}
	for _, definition := range spec.definition.Statuses {
		if definition.Status == status {
			return definition.Group, nil
		}
	}
	return "", fmt.Errorf("%w: %q for %q", ErrInvalidBusinessStatus, status, recordType)
}

func ValidateStatusTransition(recordType RecordType, from, to BusinessStatus, reason string) error {
	spec, ok := lookupRecordTypeSpec(recordType)
	if !ok {
		return fmt.Errorf("%w: %q", ErrInvalidRecordType, recordType)
	}
	if !spec.definition.SupportsBusinessStatus {
		if from == "" && to == "" {
			return nil
		}
		return fmt.Errorf("%w: status on %q", ErrInvalidBusinessStatus, recordType)
	}
	if from != "" {
		if _, err := StatusGroupFor(recordType, from); err != nil {
			return err
		}
	}
	if _, err := StatusGroupFor(recordType, to); err != nil {
		return err
	}
	if from == to {
		return nil
	}

	reasonRequired := to == StatusCancelled
	if from == "" {
		reasonRequired = reasonRequired || to != spec.definition.DefaultStatus
	} else if !containsBusinessStatus(spec.recommendedNext[from], to) {
		reasonRequired = true
	}
	if reasonRequired && strings.TrimSpace(reason) == "" {
		return fmt.Errorf("%w: %q to %q", ErrStatusTransitionReasonRequired, from, to)
	}
	return nil
}

func NewTemplateRegistry(definitions []TemplateDefinition) (TemplateRegistry, error) {
	registry := TemplateRegistry{definitions: make(map[templateKey]TemplateDefinition, len(definitions))}
	for _, definition := range definitions {
		if err := validateTemplateDefinition(definition); err != nil {
			return TemplateRegistry{}, err
		}
		key := templateKey{id: definition.Provenance.ID, version: definition.Provenance.Version}
		if _, exists := registry.definitions[key]; exists {
			return TemplateRegistry{}, fmt.Errorf("%w: duplicate %q version %d", ErrInvalidTemplate, key.id, key.version)
		}
		definition.FieldSuggestions = cloneStringMap(definition.FieldSuggestions)
		registry.definitions[key] = definition
	}
	return registry, nil
}

func (registry TemplateRegistry) Lookup(provenance TemplateProvenance) (TemplateDefinition, bool) {
	definition, ok := registry.definitions[templateKey{id: provenance.ID, version: provenance.Version}]
	if !ok {
		return TemplateDefinition{}, false
	}
	definition.FieldSuggestions = cloneStringMap(definition.FieldSuggestions)
	return definition, true
}

func (registry TemplateRegistry) Diff(recordType RecordType, provenance TemplateProvenance, currentMarkdown string) (TemplateDiff, error) {
	definition, ok := registry.Lookup(provenance)
	if !ok {
		return TemplateDiff{}, fmt.Errorf("%w: %q version %d", ErrTemplateNotFound, provenance.ID, provenance.Version)
	}
	if definition.RecordType != recordType {
		return TemplateDiff{}, fmt.Errorf("%w: template %q version %d is for %q", ErrInvalidTemplate, provenance.ID, provenance.Version, definition.RecordType)
	}
	return TemplateDiff{
		Provenance:       definition.Provenance,
		RecordType:       definition.RecordType,
		CurrentMarkdown:  currentMarkdown,
		TemplateMarkdown: definition.Markdown,
		FieldSuggestions: cloneStringMap(definition.FieldSuggestions),
	}, nil
}

func workflowRecordTypeDefinition(recordType RecordType) RecordTypeDefinition {
	return RecordTypeDefinition{
		Type:                   recordType,
		SupportsBusinessStatus: true,
		DefaultStatus:          StatusPlanned,
		Statuses: []StatusDefinition{
			{Status: StatusPlanned, Group: StatusGroupPending},
			{Status: StatusExecuting, Group: StatusGroupInProgress},
			{Status: StatusVerifying, Group: StatusGroupVerification},
			{Status: StatusCompleted, Group: StatusGroupCompleted},
			{Status: StatusCancelled, Group: StatusGroupCancelled},
		},
	}
}

func lookupRecordTypeSpec(recordType RecordType) (recordTypeSpec, bool) {
	for _, spec := range recordTypeSpecs {
		if spec.definition.Type == recordType {
			return spec, true
		}
	}
	return recordTypeSpec{}, false
}

func containsBusinessStatus(values []BusinessStatus, value BusinessStatus) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func validateTemplateDefinition(definition TemplateDefinition) error {
	if !validRegistryToken(definition.Provenance.ID, 64) || definition.Provenance.Version == 0 {
		return fmt.Errorf("%w: provenance", ErrInvalidTemplate)
	}
	if err := ValidateRecordType(definition.RecordType); err != nil {
		return fmt.Errorf("%w: record type", ErrInvalidTemplate)
	}
	if definition.Markdown == "" || !utf8.ValidString(definition.Markdown) {
		return fmt.Errorf("%w: markdown", ErrInvalidTemplate)
	}
	for key := range definition.FieldSuggestions {
		if !validRegistryToken(key, 64) {
			return fmt.Errorf("%w: field suggestion key", ErrInvalidTemplate)
		}
	}
	return nil
}

func validRegistryToken(value string, maxLength int) bool {
	if value == "" || len(value) > maxLength {
		return false
	}
	for index := range len(value) {
		character := value[index]
		if (character < 'a' || character > 'z') &&
			(character < '0' || character > '9') &&
			character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}
