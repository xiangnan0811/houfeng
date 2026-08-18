package recordsearch

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

// QueryValues is the caller-supplied search question. Values inside one
// repeated field are OR-ed; distinct fields are AND-ed.
type QueryValues struct {
	Text           string
	Types          []records.RecordType
	Statuses       []records.BusinessStatus
	StatusGroups   []records.StatusGroup
	Lifecycles     []records.Lifecycle
	Subjects       []SubjectFilter
	OwnerIDs       []string
	ParticipantIDs []string
	Tags           []string
	FollowUp       FollowUpState
	Action         ActionState
	Occurred       TimeRange
	Updated        TimeRange
	Sort           Sort
	PageSize       uint32
}

// Query is an immutable normalized search question. Construct it through
// NormalizeQuery, which is the only path that produces a digest, and therefore
// the only path that can mint or accept a cursor.
type Query struct {
	text           string
	recordTypes    []records.RecordType
	statuses       []records.BusinessStatus
	statusGroups   []records.StatusGroup
	lifecycles     []records.Lifecycle
	subjects       []SubjectFilter
	ownerIDs       []string
	participantIDs []string
	tags           []string
	followUp       FollowUpState
	action         ActionState
	occurred       TimeRange
	updated        TimeRange
	sortOrder      Sort
	pageSize       uint32

	canonicalBytes []byte
	digest         [sha256.Size]byte
}

// NormalizeQuery canonicalizes one search question. Two logically equal
// questions always produce equal canonical bytes, and two different questions
// never do; the cursor contract depends on both halves of that property.
func NormalizeQuery(values QueryValues) (Query, error) {
	text, err := normalizeQueryText(values.Text)
	if err != nil {
		return Query{}, err
	}
	recordTypes, err := normalizeRecordTypes(values.Types)
	if err != nil {
		return Query{}, err
	}
	statuses, err := normalizeStatuses(values.Statuses)
	if err != nil {
		return Query{}, err
	}
	statusGroups, err := normalizeStatusGroups(values.StatusGroups)
	if err != nil {
		return Query{}, err
	}
	lifecycles, err := normalizeLifecycles(values.Lifecycles)
	if err != nil {
		return Query{}, err
	}
	subjects, err := normalizeSubjectFilters(values.Subjects)
	if err != nil {
		return Query{}, err
	}
	ownerIDs, err := normalizeActorIDs(values.OwnerIDs, "owner id")
	if err != nil {
		return Query{}, err
	}
	participantIDs, err := normalizeActorIDs(values.ParticipantIDs, "participant id")
	if err != nil {
		return Query{}, err
	}
	tags, err := normalizeTags(values.Tags)
	if err != nil {
		return Query{}, err
	}
	if !validFollowUpState(values.FollowUp) {
		return Query{}, invalidQuery("follow up state")
	}
	if !validActionState(values.Action) {
		return Query{}, invalidQuery("action state")
	}
	occurred, err := normalizeTimeRange(values.Occurred, "occurred range")
	if err != nil {
		return Query{}, err
	}
	updated, err := normalizeTimeRange(values.Updated, "updated range")
	if err != nil {
		return Query{}, err
	}
	sortOrder := values.Sort
	if sortOrder == "" {
		sortOrder = SortUpdatedDesc
	}
	if !validSort(sortOrder) {
		return Query{}, invalidQuery("sort")
	}
	// Relevance is only defined against a text term. Allowing it without one
	// would order every record by a constant rank and make the cursor's
	// relevance component meaningless.
	if sortOrder == SortRelevanceDesc && text == "" {
		return Query{}, invalidQuery("relevance sort without text")
	}
	pageSize := values.PageSize
	if pageSize == 0 {
		pageSize = DefaultPageSize
	}
	if pageSize > MaxPageSize {
		return Query{}, invalidQuery("page size")
	}

	query := Query{
		text:           text,
		recordTypes:    recordTypes,
		statuses:       statuses,
		statusGroups:   statusGroups,
		lifecycles:     lifecycles,
		subjects:       subjects,
		ownerIDs:       ownerIDs,
		participantIDs: participantIDs,
		tags:           tags,
		followUp:       values.FollowUp,
		action:         values.Action,
		occurred:       occurred,
		updated:        updated,
		sortOrder:      sortOrder,
		pageSize:       pageSize,
	}
	query.canonicalBytes = canonicalQueryBytes(query)
	query.digest = sha256.Sum256(query.canonicalBytes)
	return query, nil
}

func (query Query) Text() string {
	return query.text
}

func (query Query) Types() []records.RecordType {
	return append([]records.RecordType(nil), query.recordTypes...)
}

func (query Query) Statuses() []records.BusinessStatus {
	return append([]records.BusinessStatus(nil), query.statuses...)
}

func (query Query) StatusGroups() []records.StatusGroup {
	return append([]records.StatusGroup(nil), query.statusGroups...)
}

func (query Query) Lifecycles() []records.Lifecycle {
	return append([]records.Lifecycle(nil), query.lifecycles...)
}

func (query Query) Subjects() []SubjectFilter {
	return append([]SubjectFilter(nil), query.subjects...)
}

func (query Query) OwnerIDs() []string {
	return append([]string(nil), query.ownerIDs...)
}

func (query Query) ParticipantIDs() []string {
	return append([]string(nil), query.participantIDs...)
}

func (query Query) Tags() []string {
	return append([]string(nil), query.tags...)
}

func (query Query) FollowUp() FollowUpState {
	return query.followUp
}

func (query Query) Action() ActionState {
	return query.action
}

func (query Query) Occurred() TimeRange {
	return cloneTimeRange(query.occurred)
}

func (query Query) Updated() TimeRange {
	return cloneTimeRange(query.updated)
}

func (query Query) Sort() Sort {
	return query.sortOrder
}

func (query Query) PageSize() uint32 {
	return query.pageSize
}

// CanonicalBytes returns a copy of the canonical encoding. It is evidence for
// the digest, not a second wire format.
func (query Query) CanonicalBytes() []byte {
	return append([]byte(nil), query.canonicalBytes...)
}

// Digest identifies the question. A cursor is only valid for the digest it was
// minted under.
func (query Query) Digest() [sha256.Size]byte {
	return query.digest
}

func (query Query) normalized() bool {
	return len(query.canonicalBytes) > 0
}

func invalidQuery(field string) error {
	return fmt.Errorf("%w: %s", ErrInvalidQuery, field)
}

// normalizeQueryText folds the operator's term to one shape: NFC composition,
// no surrounding whitespace, and single spaces between words. Without this a
// retyped term produces a different digest and silently invalidates a cursor.
func normalizeQueryText(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if !utf8.ValidString(value) {
		return "", invalidQuery("text encoding")
	}
	composed := norm.NFC.String(value)
	var builder strings.Builder
	pendingSpace := false
	for _, character := range composed {
		switch {
		case unicode.IsSpace(character):
			pendingSpace = builder.Len() > 0
		case character == utf8.RuneError, unicode.IsControl(character):
			return "", invalidQuery("text control character")
		default:
			if pendingSpace {
				builder.WriteRune(' ')
				pendingSpace = false
			}
			builder.WriteRune(character)
		}
	}
	collapsed := builder.String()
	if utf8.RuneCountInString(collapsed) > MaxQueryTextRunes {
		return "", invalidQuery("text length")
	}
	return collapsed, nil
}

func normalizeRecordTypes(values []records.RecordType) ([]records.RecordType, error) {
	normalized, err := normalizeFilterValues(values, "record type", func(value records.RecordType) bool {
		return records.ValidateRecordType(value) == nil
	})
	if err != nil {
		return nil, err
	}
	return normalized, nil
}

func normalizeStatuses(values []records.BusinessStatus) ([]records.BusinessStatus, error) {
	return normalizeFilterValues(values, "business status", records.ValidBusinessStatus)
}

func normalizeStatusGroups(values []records.StatusGroup) ([]records.StatusGroup, error) {
	return normalizeFilterValues(values, "status group", records.ValidStatusGroup)
}

func normalizeLifecycles(values []records.Lifecycle) ([]records.Lifecycle, error) {
	return normalizeFilterValues(values, "lifecycle", func(value records.Lifecycle) bool {
		return records.ValidateLifecycle(value) == nil
	})
}

// normalizeFilterValues sorts and rejects duplicates. Sorting is what makes a
// set of OR-ed values digest-stable regardless of the order the browser sent
// them in; a duplicate is a caller mistake rather than something to absorb.
func normalizeFilterValues[Value ~string](values []Value, field string, valid func(Value) bool) ([]Value, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if len(values) > MaxQueryFilterValues {
		return nil, invalidQuery(field + " count")
	}
	normalized := make([]Value, 0, len(values))
	seen := make(map[Value]struct{}, len(values))
	for _, value := range values {
		if !valid(value) {
			return nil, invalidQuery(field)
		}
		if _, exists := seen[value]; exists {
			return nil, invalidQuery("duplicate " + field)
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	sort.Slice(normalized, func(first, second int) bool { return normalized[first] < normalized[second] })
	return normalized, nil
}

func normalizeActorIDs(values []string, field string) ([]string, error) {
	return normalizeFilterValues(values, field, func(value string) bool {
		return recordauth.ValidateActorUserID(value) == nil
	})
}

func normalizeTags(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if len(values) > MaxQueryFilterValues {
		return nil, invalidQuery("tag count")
	}
	// Tags are stored lowercased and trimmed, so a filter has to fold the same
	// way or a tag typed with capitals would match nothing.
	folded := make([]string, 0, len(values))
	for _, value := range values {
		folded = append(folded, strings.ToLower(strings.TrimSpace(value)))
	}
	return normalizeFilterValues(folded, "tag", func(value string) bool {
		return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= maxTagRunes
	})
}

func normalizeSubjectFilters(values []SubjectFilter) ([]SubjectFilter, error) {
	if len(values) == 0 {
		return nil, nil
	}
	if len(values) > MaxQueryFilterValues {
		return nil, invalidQuery("subject count")
	}
	normalized := make([]SubjectFilter, 0, len(values))
	seen := make(map[SubjectFilter]struct{}, len(values))
	for _, filter := range values {
		if !records.ValidSubjectKind(filter.Kind) {
			return nil, invalidQuery("subject kind")
		}
		if filter.Role != "" && !records.ValidRelationRole(filter.Role) {
			return nil, invalidQuery("subject role")
		}
		if filter.SourceID != "" && !records.ValidSubjectSourceID(filter.Kind, filter.SourceID) {
			return nil, invalidQuery("subject source id")
		}
		if !validSubjectPlacement(filter.Placement) {
			return nil, invalidQuery("subject placement")
		}
		if _, exists := seen[filter]; exists {
			return nil, invalidQuery("duplicate subject")
		}
		seen[filter] = struct{}{}
		normalized = append(normalized, filter)
	}
	sort.Slice(normalized, func(first, second int) bool {
		return subjectFilterOrder(normalized[first]) < subjectFilterOrder(normalized[second])
	})
	return normalized, nil
}

func subjectFilterOrder(filter SubjectFilter) string {
	return strings.Join([]string{
		string(filter.Kind), string(filter.Role), filter.SourceID, string(filter.Placement),
	}, "\x00")
}

// normalizeTimeRange truncates to microseconds because that is the resolution
// PostgreSQL stores. Digesting nanoseconds the database cannot hold would let a
// cursor bind to a bound that no stored row can ever equal.
func normalizeTimeRange(value TimeRange, field string) (TimeRange, error) {
	from, err := normalizeRangeBound(value.From, field)
	if err != nil {
		return TimeRange{}, err
	}
	to, err := normalizeRangeBound(value.To, field)
	if err != nil {
		return TimeRange{}, err
	}
	if from != nil && to != nil && !from.Before(*to) {
		return TimeRange{}, invalidQuery(field)
	}
	return TimeRange{From: from, To: to}, nil
}

func normalizeRangeBound(value *time.Time, field string) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}
	if value.IsZero() {
		return nil, invalidQuery(field)
	}
	normalized := value.UTC().Truncate(time.Microsecond)
	return &normalized, nil
}

func cloneTimeRange(value TimeRange) TimeRange {
	clone := TimeRange{}
	if value.From != nil {
		from := *value.From
		clone.From = &from
	}
	if value.To != nil {
		to := *value.To
		clone.To = &to
	}
	return clone
}

func canonicalQueryBytes(query Query) []byte {
	encoder := canonicalEncoder{}
	encoder.string("recordsearch.query.v1")
	encoder.uint64(QueryVersionV1)
	encoder.string(query.text)
	encoder.length(len(query.recordTypes))
	for _, value := range query.recordTypes {
		encoder.string(string(value))
	}
	encoder.length(len(query.statuses))
	for _, value := range query.statuses {
		encoder.string(string(value))
	}
	encoder.length(len(query.statusGroups))
	for _, value := range query.statusGroups {
		encoder.string(string(value))
	}
	encoder.length(len(query.lifecycles))
	for _, value := range query.lifecycles {
		encoder.string(string(value))
	}
	encoder.length(len(query.subjects))
	for _, value := range query.subjects {
		encoder.string(string(value.Kind))
		encoder.string(string(value.Role))
		encoder.string(value.SourceID)
		encoder.string(string(value.Placement))
	}
	encoder.length(len(query.ownerIDs))
	for _, value := range query.ownerIDs {
		encoder.string(value)
	}
	encoder.length(len(query.participantIDs))
	for _, value := range query.participantIDs {
		encoder.string(value)
	}
	encoder.length(len(query.tags))
	for _, value := range query.tags {
		encoder.string(value)
	}
	encoder.string(string(query.followUp))
	encoder.string(string(query.action))
	encoder.optionalTime(query.occurred.From)
	encoder.optionalTime(query.occurred.To)
	encoder.optionalTime(query.updated.From)
	encoder.optionalTime(query.updated.To)
	encoder.string(string(query.sortOrder))
	encoder.uint64(uint64(query.pageSize))
	return encoder.bytes
}
