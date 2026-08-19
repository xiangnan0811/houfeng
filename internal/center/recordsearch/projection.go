package recordsearch

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

// ErrInvalidProjection reports facts that cannot be projected into the index.
// The projector runs inside the transaction that wrote the record, so refusing
// here is what keeps a malformed derived row from aborting an otherwise valid
// record write.
var ErrInvalidProjection = errors.New("invalid record search projection")

const (
	maxProjectionTitleRunes  int = 512
	maxProjectionTags        int = 64
	maxProjectionParticipant int = 512
	maxProjectionSubjects    int = 256
)

// DocumentSubject is one indexed subject edge. Primary marks the single subject
// the records registry treats as the record's own subject; every other edge is
// related context.
type DocumentSubject struct {
	Kind     records.SubjectKind
	Role     records.RelationRole
	SourceID string
	Primary  bool
}

// DocumentFactValues is the mutable input the write path assembles.
type DocumentFactValues struct {
	RecordID           string
	CurrentRevisionID  string
	LockVersion        uint64
	AuthorizationEpoch uint64
	FenceEpoch         uint64
	Lifecycle          records.Lifecycle
	RecordType         records.RecordType
	BusinessStatus     records.BusinessStatus
	ImpactLevel        records.ImpactLevel
	OwnerID            string
	Title              string
	Text               string
	Tags               []string
	ParticipantIDs     []string
	VisibilityKind     string
	VisibilityDigest   [sha256.Size]byte
	OccurredAt         *time.Time
	CompletedAt        *time.Time
	FollowUpAt         *time.Time
	RecordCreatedAt    time.Time
	RecordUpdatedAt    time.Time
	Subjects           []DocumentSubject
}

// DocumentFacts is a normalized, immutable projection of one record. Its digest
// covers the projected content of one revision, which is what lets a rebuild
// verify coverage without re-deriving the body text of every record.
//
// The digest deliberately excludes the mutable control columns: lifecycle, lock
// version, authorization epoch, fence epoch, and the timestamps. Those are
// stored plainly and compared column by column, which is what allows a lifecycle
// move to update the index in place instead of re-deriving a document whose
// content did not change.
type DocumentFacts struct {
	values      DocumentFactValues
	statusGroup records.StatusGroup
	digest      [sha256.Size]byte
}

func NormalizeDocumentFacts(values DocumentFactValues) (DocumentFacts, error) {
	if !records.ValidRecordRootID(values.RecordID) {
		return DocumentFacts{}, fmt.Errorf("%w: record id", ErrInvalidProjection)
	}
	if !records.ValidRevisionID(values.CurrentRevisionID) {
		return DocumentFacts{}, fmt.Errorf("%w: revision id", ErrInvalidProjection)
	}
	if records.ValidateLifecycle(values.Lifecycle) != nil {
		return DocumentFacts{}, fmt.Errorf("%w: lifecycle", ErrInvalidProjection)
	}
	if records.ValidateRecordType(values.RecordType) != nil {
		return DocumentFacts{}, fmt.Errorf("%w: record type", ErrInvalidProjection)
	}
	// The status group is derived rather than accepted, because it is a function
	// of the type and status. Taking it as input would let the projector index a
	// group the record never had.
	statusGroup, err := records.StatusGroupFor(values.RecordType, values.BusinessStatus)
	if err != nil {
		return DocumentFacts{}, fmt.Errorf("%w: business status", ErrInvalidProjection)
	}
	if !validProjectionToken(string(values.ImpactLevel)) {
		return DocumentFacts{}, fmt.Errorf("%w: impact level", ErrInvalidProjection)
	}
	if values.OwnerID != "" && !validProjectionUserID(values.OwnerID) {
		return DocumentFacts{}, fmt.Errorf("%w: owner id", ErrInvalidProjection)
	}
	title, err := normalizeProjectionText(values.Title, maxProjectionTitleRunes)
	if err != nil || title == "" {
		return DocumentFacts{}, fmt.Errorf("%w: title", ErrInvalidProjection)
	}
	if values.VisibilityKind != "project" && values.VisibilityKind != "restricted" {
		return DocumentFacts{}, fmt.Errorf("%w: visibility kind", ErrInvalidProjection)
	}
	if values.VisibilityDigest == ([sha256.Size]byte{}) {
		return DocumentFacts{}, fmt.Errorf("%w: visibility digest", ErrInvalidProjection)
	}
	if values.RecordCreatedAt.IsZero() || values.RecordUpdatedAt.IsZero() ||
		values.RecordUpdatedAt.Before(values.RecordCreatedAt) {
		return DocumentFacts{}, fmt.Errorf("%w: record timestamps", ErrInvalidProjection)
	}

	normalized := DocumentFactValues{
		RecordID:           values.RecordID,
		CurrentRevisionID:  values.CurrentRevisionID,
		LockVersion:        values.LockVersion,
		AuthorizationEpoch: values.AuthorizationEpoch,
		FenceEpoch:         values.FenceEpoch,
		Lifecycle:          values.Lifecycle,
		RecordType:         values.RecordType,
		BusinessStatus:     values.BusinessStatus,
		ImpactLevel:        values.ImpactLevel,
		OwnerID:            values.OwnerID,
		Title:              title,
		VisibilityKind:     values.VisibilityKind,
		VisibilityDigest:   values.VisibilityDigest,
		OccurredAt:         normalizeProjectionInstant(values.OccurredAt),
		CompletedAt:        normalizeProjectionInstant(values.CompletedAt),
		FollowUpAt:         normalizeProjectionInstant(values.FollowUpAt),
		RecordCreatedAt:    values.RecordCreatedAt.UTC(),
		RecordUpdatedAt:    values.RecordUpdatedAt.UTC(),
	}
	if normalized.Text, err = normalizeProjectionText(values.Text, 0); err != nil {
		return DocumentFacts{}, fmt.Errorf("%w: text", ErrInvalidProjection)
	}
	if len(normalized.Text) > MaxDocumentTextBytes {
		normalized.Text = truncateDocumentTextOnRuneBoundary(normalized.Text)
	}
	if normalized.Tags, err = normalizeProjectionTokens(values.Tags, maxProjectionTags, maxTagRunes); err != nil {
		return DocumentFacts{}, fmt.Errorf("%w: tags", ErrInvalidProjection)
	}
	if normalized.ParticipantIDs, err = normalizeProjectionUserIDs(values.ParticipantIDs); err != nil {
		return DocumentFacts{}, fmt.Errorf("%w: participants", ErrInvalidProjection)
	}
	if normalized.Subjects, err = normalizeProjectionSubjects(values.Subjects); err != nil {
		return DocumentFacts{}, err
	}

	facts := DocumentFacts{values: normalized, statusGroup: statusGroup}
	facts.digest = digestDocumentFacts(normalized, statusGroup)
	return facts, nil
}

func (facts DocumentFacts) RecordID() string                 { return facts.values.RecordID }
func (facts DocumentFacts) CurrentRevisionID() string        { return facts.values.CurrentRevisionID }
func (facts DocumentFacts) LockVersion() uint64              { return facts.values.LockVersion }
func (facts DocumentFacts) AuthorizationEpoch() uint64       { return facts.values.AuthorizationEpoch }
func (facts DocumentFacts) FenceEpoch() uint64               { return facts.values.FenceEpoch }
func (facts DocumentFacts) Lifecycle() records.Lifecycle     { return facts.values.Lifecycle }
func (facts DocumentFacts) RecordType() records.RecordType   { return facts.values.RecordType }
func (facts DocumentFacts) StatusGroup() records.StatusGroup { return facts.statusGroup }
func (facts DocumentFacts) ImpactLevel() records.ImpactLevel { return facts.values.ImpactLevel }
func (facts DocumentFacts) OwnerID() string                  { return facts.values.OwnerID }
func (facts DocumentFacts) Title() string                    { return facts.values.Title }
func (facts DocumentFacts) Text() string                     { return facts.values.Text }
func (facts DocumentFacts) VisibilityKind() string           { return facts.values.VisibilityKind }
func (facts DocumentFacts) RecordCreatedAt() time.Time       { return facts.values.RecordCreatedAt }
func (facts DocumentFacts) RecordUpdatedAt() time.Time       { return facts.values.RecordUpdatedAt }
func (facts DocumentFacts) Digest() [sha256.Size]byte        { return facts.digest }
func (facts DocumentFacts) BusinessStatus() records.BusinessStatus {
	return facts.values.BusinessStatus
}

func (facts DocumentFacts) VisibilityDigest() [sha256.Size]byte {
	return facts.values.VisibilityDigest
}

func (facts DocumentFacts) OccurredAt() *time.Time {
	return cloneProjectionInstant(facts.values.OccurredAt)
}
func (facts DocumentFacts) CompletedAt() *time.Time {
	return cloneProjectionInstant(facts.values.CompletedAt)
}
func (facts DocumentFacts) FollowUpAt() *time.Time {
	return cloneProjectionInstant(facts.values.FollowUpAt)
}

func (facts DocumentFacts) Tags() []string {
	return append([]string(nil), facts.values.Tags...)
}

func (facts DocumentFacts) ParticipantIDs() []string {
	return append([]string(nil), facts.values.ParticipantIDs...)
}

func (facts DocumentFacts) Subjects() []DocumentSubject {
	return append([]DocumentSubject(nil), facts.values.Subjects...)
}

func digestDocumentFacts(values DocumentFactValues, statusGroup records.StatusGroup) [sha256.Size]byte {
	encoder := &canonicalEncoder{bytes: make([]byte, 0, 1024)}
	encoder.string("houfeng.record-search.document.v1")
	encoder.uint64(QueryVersionV1)
	encoder.string(values.RecordID)
	encoder.string(values.CurrentRevisionID)
	encoder.string(string(values.RecordType))
	encoder.string(string(statusGroup))
	encoder.string(string(values.BusinessStatus))
	encoder.string(string(values.ImpactLevel))
	encoder.string(values.OwnerID)
	encoder.string(values.Title)
	encoder.string(values.Text)
	encoder.length(len(values.Tags))
	for _, tag := range values.Tags {
		encoder.string(tag)
	}
	encoder.length(len(values.ParticipantIDs))
	for _, participant := range values.ParticipantIDs {
		encoder.string(participant)
	}
	encoder.string(values.VisibilityKind)
	encoder.bytes = append(encoder.bytes, values.VisibilityDigest[:]...)
	encoder.optionalTime(values.OccurredAt)
	encoder.optionalTime(values.CompletedAt)
	encoder.optionalTime(values.FollowUpAt)
	encoder.length(len(values.Subjects))
	for _, subject := range values.Subjects {
		encoder.string(string(subject.Kind))
		encoder.string(string(subject.Role))
		encoder.string(subject.SourceID)
		if subject.Primary {
			encoder.byte(1)
		} else {
			encoder.byte(0)
		}
	}
	return sha256.Sum256(encoder.bytes)
}

func normalizeProjectionSubjects(subjects []DocumentSubject) ([]DocumentSubject, error) {
	if len(subjects) > maxProjectionSubjects {
		return nil, fmt.Errorf("%w: subject count", ErrInvalidProjection)
	}
	seen := make(map[DocumentSubject]struct{}, len(subjects))
	normalized := make([]DocumentSubject, 0, len(subjects))
	primaries := 0
	for _, subject := range subjects {
		if !records.ValidSubjectKind(subject.Kind) || !records.ValidRelationRole(subject.Role) ||
			!records.ValidSubjectSourceID(subject.Kind, subject.SourceID) {
			return nil, fmt.Errorf("%w: subject", ErrInvalidProjection)
		}
		if _, duplicate := seen[subject]; duplicate {
			continue
		}
		seen[subject] = struct{}{}
		if subject.Primary {
			primaries++
		}
		normalized = append(normalized, subject)
	}
	// The records registry allows at most one primary subject per revision, so a
	// second one here means the projection disagrees with its own source.
	if primaries > 1 {
		return nil, fmt.Errorf("%w: multiple primary subjects", ErrInvalidProjection)
	}
	sort.Slice(normalized, func(left, right int) bool {
		if normalized[left].Kind != normalized[right].Kind {
			return normalized[left].Kind < normalized[right].Kind
		}
		if normalized[left].SourceID != normalized[right].SourceID {
			return normalized[left].SourceID < normalized[right].SourceID
		}
		return normalized[left].Role < normalized[right].Role
	})
	return normalized, nil
}

func normalizeProjectionUserIDs(values []string) ([]string, error) {
	if len(values) > maxProjectionParticipant {
		return nil, fmt.Errorf("%w: count", ErrInvalidProjection)
	}
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		if !validProjectionUserID(value) {
			return nil, fmt.Errorf("%w: user id", ErrInvalidProjection)
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func normalizeProjectionText(value string, maxRunes int) (string, error) {
	folded, ok := foldSearchText(value)
	if !ok {
		return "", ErrInvalidProjection
	}
	if maxRunes > 0 && utf8.RuneCountInString(folded) > maxRunes {
		// A title longer than the column bound is truncated rather than
		// refused: losing the tail of a long title is better than keeping the
		// record out of its own index.
		return string([]rune(folded)[:maxRunes]), nil
	}
	return folded, nil
}

// normalizeProjectionTokens folds derived repeated values. Unlike a query
// filter, a duplicate here is not a caller mistake to reject but source data to
// absorb, so equal documents keep equal digests.
func normalizeProjectionTokens(values []string, maxCount int, maxRunes int) ([]string, error) {
	if len(values) > maxCount {
		return nil, ErrInvalidProjection
	}
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		folded := strings.ToLower(strings.TrimSpace(value))
		if folded == "" || !utf8.ValidString(folded) || utf8.RuneCountInString(folded) > maxRunes {
			return nil, ErrInvalidProjection
		}
		if _, duplicate := seen[folded]; duplicate {
			continue
		}
		seen[folded] = struct{}{}
		normalized = append(normalized, folded)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func validProjectionToken(value string) bool {
	if len(value) < 1 || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if character != '_' && (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func validProjectionUserID(value string) bool {
	return recordauth.ValidateActorUserID(value) == nil
}

func cloneProjectionInstant(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}

func normalizeProjectionInstant(value *time.Time) *time.Time {
	return cloneProjectionInstant(value)
}
