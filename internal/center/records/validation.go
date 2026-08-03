package records

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"houfeng/internal/center/recordauth"
)

const completeRevisionCanonicalDomainV1 = "houfeng.records.complete-revision-content.v1"

func NormalizeCompleteRevisionInput(values CompleteRevisionValues) (CompleteRevisionInput, error) {
	title := strings.TrimSpace(values.Title)
	if title == "" || !utf8.ValidString(title) {
		return CompleteRevisionInput{}, invalidRevisionInput("title")
	}
	if !utf8.ValidString(values.BodyMarkdown) {
		return CompleteRevisionInput{}, invalidRevisionInput("body markdown")
	}
	if values.MarkdownDialectVersion != MarkdownDialectVersionV1 {
		return CompleteRevisionInput{}, invalidRevisionInput("markdown dialect version")
	}
	if err := ValidateRecordType(values.RecordType); err != nil {
		return CompleteRevisionInput{}, invalidRevisionInput("record type")
	}
	statusGroup, err := StatusGroupFor(values.RecordType, values.BusinessStatus)
	if err != nil {
		return CompleteRevisionInput{}, invalidRevisionInput("business status")
	}
	impactLevel := ImpactLevel(strings.TrimSpace(string(values.ImpactLevel)))
	if !validRegistryToken(string(impactLevel), 64) {
		return CompleteRevisionInput{}, invalidRevisionInput("impact level")
	}

	occurredAt := normalizedUTCTime(values.OccurredAt)
	completedAt := normalizedUTCTime(values.CompletedAt)
	if statusGroup == StatusGroupCompleted && completedAt == nil {
		return CompleteRevisionInput{}, invalidRevisionInput("completed status without completed time")
	}
	if statusGroup != StatusGroupCompleted && completedAt != nil {
		return CompleteRevisionInput{}, invalidRevisionInput("completed time without completed status")
	}
	if occurredAt != nil && completedAt != nil && completedAt.Before(*occurredAt) {
		return CompleteRevisionInput{}, invalidRevisionInput("completion before occurrence")
	}

	visibility, err := recordauth.NormalizeVisibilityScope(values.VisibilityScope)
	if err != nil {
		return CompleteRevisionInput{}, invalidRevisionInput("visibility scope")
	}
	subjects, err := normalizeRevisionSubjects(values.Subjects)
	if err != nil {
		return CompleteRevisionInput{}, err
	}
	tags, err := normalizeRevisionTags(values.Tags)
	if err != nil {
		return CompleteRevisionInput{}, err
	}
	if values.OwnerID != "" {
		if err := recordauth.ValidateActorUserID(values.OwnerID); err != nil {
			return CompleteRevisionInput{}, invalidRevisionInput("owner id")
		}
	}
	participants, err := normalizeRevisionParticipants(values.Participants)
	if err != nil {
		return CompleteRevisionInput{}, err
	}
	followUpAt := normalizedUTCTime(values.FollowUpAt)
	template, err := normalizeTemplateProvenance(values.Template)
	if err != nil {
		return CompleteRevisionInput{}, err
	}
	if err := recordauth.ValidateActorUserID(values.AuthorID); err != nil {
		return CompleteRevisionInput{}, invalidRevisionInput("author id")
	}
	saveReason := strings.TrimSpace(values.SaveReason)
	if !utf8.ValidString(saveReason) {
		return CompleteRevisionInput{}, invalidRevisionInput("save reason")
	}
	if statusGroup == StatusGroupCancelled && saveReason == "" {
		return CompleteRevisionInput{}, invalidRevisionInput("cancelled status without reason")
	}

	input := CompleteRevisionInput{
		title:                  title,
		bodyMarkdown:           values.BodyMarkdown,
		markdownDialectVersion: values.MarkdownDialectVersion,
		recordType:             values.RecordType,
		businessStatus:         values.BusinessStatus,
		statusGroup:            statusGroup,
		impactLevel:            impactLevel,
		occurredAt:             occurredAt,
		completedAt:            completedAt,
		visibilityScope:        visibility,
		subjects:               subjects,
		tags:                   tags,
		ownerID:                values.OwnerID,
		participants:           participants,
		followUpAt:             followUpAt,
		template:               template,
		authorID:               values.AuthorID,
		saveReason:             saveReason,
	}
	input.canonicalHash = canonicalRevisionHash(input)
	return input, nil
}

func normalizeRevisionSubjects(values []RevisionSubject) ([]RevisionSubject, error) {
	references := make([]SubjectReference, len(values))
	for index, value := range values {
		references[index] = SubjectReference{
			RegistryVersion: value.RegistryVersion,
			Kind:            value.Kind,
			Role:            value.Role,
			SourceID:        value.SourceID,
			Primary:         value.Primary,
		}
	}
	if _, err := NormalizeSubjectReferences(references); err != nil {
		return nil, invalidRevisionInput("subject references")
	}

	normalized := make([]RevisionSubject, 0, len(values))
	for _, value := range values {
		snapshot, err := NewSubjectIdentitySnapshot(value.Kind, value.IdentitySnapshot)
		if err != nil {
			return nil, invalidRevisionInput("subject identity snapshot")
		}
		authorization, err := recordauth.NormalizeSourceAuthorization(value.CaptureAuthorization)
		expectedSourceKind, sourceKindOK := recordAuthSourceKind(value.Kind)
		if err != nil || authorization.Digest != value.CaptureAuthorization.Digest || !sourceKindOK ||
			authorization.Kind != expectedSourceKind || authorization.SourceID != value.SourceID {
			return nil, invalidRevisionInput("subject capture authorization")
		}
		normalized = append(normalized, RevisionSubject{
			RegistryVersion:      value.RegistryVersion,
			Kind:                 value.Kind,
			Role:                 value.Role,
			SourceID:             value.SourceID,
			Primary:              value.Primary,
			IdentitySnapshot:     snapshot.Fields(),
			CaptureAuthorization: authorization,
		})
	}
	return normalized, nil
}

func normalizeRevisionTags(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	normalized := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 64 {
			return nil, invalidRevisionInput("tag")
		}
		if _, exists := seen[value]; exists {
			return nil, invalidRevisionInput("duplicate tag")
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized, nil
}

func normalizeRevisionParticipants(values []RevisionParticipant) ([]RevisionParticipant, error) {
	if len(values) == 0 {
		return nil, nil
	}
	normalized := make([]RevisionParticipant, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if err := recordauth.ValidateActorUserID(value.ParticipantID); err != nil {
			return nil, invalidRevisionInput("participant id")
		}
		if _, exists := seen[value.ParticipantID]; exists {
			return nil, invalidRevisionInput("duplicate participant")
		}
		seen[value.ParticipantID] = struct{}{}
		snapshot, err := normalizeIdentitySnapshot(value.IdentitySnapshot)
		if err != nil {
			return nil, invalidRevisionInput("participant identity snapshot")
		}
		normalized = append(normalized, RevisionParticipant{
			ParticipantID:    value.ParticipantID,
			IdentitySnapshot: snapshot,
		})
	}
	return normalized, nil
}

func normalizeIdentitySnapshot(values map[string]string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, invalidRevisionInput("empty identity snapshot")
	}
	normalized := make(map[string]string, len(values))
	for key, value := range values {
		if !validRegistryToken(key, 64) || !utf8.ValidString(value) {
			return nil, invalidRevisionInput("identity snapshot field")
		}
		normalized[key] = value
	}
	return normalized, nil
}

func normalizeTemplateProvenance(value *TemplateProvenance) (*TemplateProvenance, error) {
	if value == nil {
		return nil, nil
	}
	if !validRegistryToken(value.ID, 64) || value.Version == 0 {
		return nil, invalidRevisionInput("template provenance")
	}
	normalized := *value
	return &normalized, nil
}

func normalizedUTCTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC()
	return &normalized
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneVisibilityScope(value recordauth.VisibilityScope) recordauth.VisibilityScope {
	cloned, err := recordauth.NormalizeVisibilityScope(value)
	if err != nil {
		return recordauth.VisibilityScope{}
	}
	return cloned
}

func cloneSourceAuthorization(value recordauth.SourceAuthorization) recordauth.SourceAuthorization {
	cloned, err := recordauth.NormalizeSourceAuthorization(value)
	if err != nil {
		return recordauth.SourceAuthorization{}
	}
	return cloned
}

func cloneRevisionSubjects(values []RevisionSubject) []RevisionSubject {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]RevisionSubject, len(values))
	for index, value := range values {
		cloned[index] = value
		cloned[index].IdentitySnapshot = cloneStringMap(value.IdentitySnapshot)
		cloned[index].CaptureAuthorization = cloneSourceAuthorization(value.CaptureAuthorization)
	}
	return cloned
}

func cloneRevisionParticipants(values []RevisionParticipant) []RevisionParticipant {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]RevisionParticipant, len(values))
	for index, value := range values {
		cloned[index] = value
		cloned[index].IdentitySnapshot = cloneStringMap(value.IdentitySnapshot)
	}
	return cloned
}

func canonicalRevisionHash(input CompleteRevisionInput) [sha256.Size]byte {
	encoder := revisionCanonicalEncoder{}
	encoder.string(completeRevisionCanonicalDomainV1)
	encoder.string(input.title)
	encoder.string(input.bodyMarkdown)
	encoder.uint64(uint64(input.markdownDialectVersion))
	encoder.string(string(input.recordType))
	encoder.string(string(input.businessStatus))
	encoder.string(string(input.statusGroup))
	encoder.string(string(input.impactLevel))
	encoder.optionalTime(input.occurredAt)
	encoder.optionalTime(input.completedAt)
	encoder.raw(input.visibilityScope.CanonicalBytes())
	encoder.length(len(input.subjects))
	for _, subject := range input.subjects {
		encoder.uint64(subject.RegistryVersion)
		encoder.string(string(subject.Kind))
		encoder.string(string(subject.Role))
		encoder.string(subject.SourceID)
		encoder.boolean(subject.Primary)
		encoder.stringMap(subject.IdentitySnapshot)
		encoder.raw(subject.CaptureAuthorization.Digest[:])
	}
	encoder.length(len(input.tags))
	for _, tag := range input.tags {
		encoder.string(tag)
	}
	encoder.string(input.ownerID)
	encoder.length(len(input.participants))
	for _, participant := range input.participants {
		encoder.string(participant.ParticipantID)
		encoder.stringMap(participant.IdentitySnapshot)
	}
	encoder.optionalTime(input.followUpAt)
	if input.template == nil {
		encoder.boolean(false)
	} else {
		encoder.boolean(true)
		encoder.string(input.template.ID)
		encoder.uint64(input.template.Version)
	}
	return sha256.Sum256(encoder.bytes)
}

type revisionCanonicalEncoder struct {
	bytes []byte
}

func (encoder *revisionCanonicalEncoder) boolean(value bool) {
	if value {
		encoder.bytes = append(encoder.bytes, 1)
		return
	}
	encoder.bytes = append(encoder.bytes, 0)
}

func (encoder *revisionCanonicalEncoder) uint64(value uint64) {
	encoder.bytes = append(encoder.bytes,
		byte(value>>56), byte(value>>48), byte(value>>40), byte(value>>32),
		byte(value>>24), byte(value>>16), byte(value>>8), byte(value),
	)
}

func (encoder *revisionCanonicalEncoder) length(value int) {
	unsigned := uint32(value)
	encoder.bytes = append(encoder.bytes,
		byte(unsigned>>24), byte(unsigned>>16), byte(unsigned>>8), byte(unsigned),
	)
}

func (encoder *revisionCanonicalEncoder) string(value string) {
	encoder.raw([]byte(value))
}

func (encoder *revisionCanonicalEncoder) raw(value []byte) {
	encoder.length(len(value))
	encoder.bytes = append(encoder.bytes, value...)
}

func (encoder *revisionCanonicalEncoder) optionalTime(value *time.Time) {
	if value == nil {
		encoder.boolean(false)
		return
	}
	encoder.boolean(true)
	encoder.uint64(uint64(value.Unix()))
	encoder.uint64(uint64(value.Nanosecond()))
}

func (encoder *revisionCanonicalEncoder) stringMap(values map[string]string) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	encoder.length(len(keys))
	for _, key := range keys {
		encoder.string(key)
		encoder.string(values[key])
	}
}

func invalidRevisionInput(field string) error {
	return fmt.Errorf("%w: %s", ErrInvalidRevisionInput, field)
}
