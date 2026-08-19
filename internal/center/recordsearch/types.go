// Package recordsearch owns the query vocabulary and pagination cursor of the
// records search projection. It depends only on the standard library, the
// records registry, and recordauth so the HTTP layer, the store, and the
// projector share one normalization and one cursor contract.
package recordsearch

import (
	"errors"
	"time"

	"houfeng/internal/center/records"
)

const (
	// QueryVersionV1 tags the canonical query encoding. A change to the
	// encoding needs a new version, because every live cursor is bound to a
	// digest produced by the current one.
	QueryVersionV1 uint64 = 1
	// CursorVersionV1 tags the opaque cursor envelope.
	CursorVersionV1 uint64 = 1
)

const (
	// DefaultPageSize matches the record list default so the two surfaces
	// page at the same rate.
	DefaultPageSize uint32 = 50
	// MaxPageSize bounds one page of authorized rows.
	MaxPageSize uint32 = 100
	// MaxQueryTextRunes bounds the free-text term before any index work.
	MaxQueryTextRunes int = 256
	// MaxQueryFilterValues bounds each repeated filter field. Values inside
	// one field are OR-ed, so an unbounded field is an unbounded query.
	MaxQueryFilterValues int = 32
	// maxTagRunes mirrors the revision tag rule in the records package. A
	// filter value longer than a storable tag can never match.
	maxTagRunes int = 64
)

var (
	// ErrInvalidQuery reports caller input that cannot be normalized. Its
	// wrapped detail names the offending field, which is safe because the
	// caller supplied it.
	ErrInvalidQuery = errors.New("invalid record search query")
	// ErrInvalidCursor reports an unusable cursor. It is deliberately
	// detail-free: the reason separates tampering from a republished
	// generation from a changed authorization namespace, and no caller has a
	// legitimate use for that difference.
	ErrInvalidCursor = errors.New("invalid record search cursor")
)

// Sort is the closed ordering vocabulary. Every ordering ends with the record
// identifier so one page boundary is a total order.
//
// There is deliberately no relevance ordering. The trigram extension lives in
// record_platform_internal, where no app role has USAGE, and the APP ACL
// grammar only grants functions shaped public.name(bytea), so a per-row
// similarity scalar could not be reached without widening a frozen contract.
// The index still earns its place by making the text filter indexed.
type Sort string

const (
	SortUpdatedDesc Sort = "updated_at_desc"
	SortUpdatedAsc  Sort = "updated_at_asc"
)

// FollowUpState filters on the follow-up time carried by the current revision.
type FollowUpState string

const (
	FollowUpAny       FollowUpState = ""
	FollowUpNone      FollowUpState = "none"
	FollowUpScheduled FollowUpState = "scheduled"
	FollowUpOverdue   FollowUpState = "overdue"
)

// ActionState filters on collaboration actions. Matching is existential: a
// record with three open actions is still one result.
type ActionState string

const (
	ActionAny     ActionState = ""
	ActionNone    ActionState = "none"
	ActionOpen    ActionState = "open"
	ActionOverdue ActionState = "overdue"
)

// SubjectPlacement narrows a subject filter to the primary subject or to the
// remaining related ones.
type SubjectPlacement string

const (
	SubjectPlacementAny     SubjectPlacement = ""
	SubjectPlacementPrimary SubjectPlacement = "primary"
	SubjectPlacementRelated SubjectPlacement = "related"
)

// TimeRange is a half-open instant range: From is inclusive, To is exclusive.
// A nil bound is unbounded on that side.
type TimeRange struct {
	From *time.Time
	To   *time.Time
}

// SubjectFilter selects records by subject. An empty Role, SourceID, or
// Placement means "any", so a filter can ask for one VPS or for every record
// that touches any VPS.
type SubjectFilter struct {
	Kind      records.SubjectKind
	Role      records.RelationRole
	SourceID  string
	Placement SubjectPlacement
}

func validSort(sort Sort) bool {
	switch sort {
	case SortUpdatedDesc, SortUpdatedAsc:
		return true
	default:
		return false
	}
}

func validFollowUpState(state FollowUpState) bool {
	switch state {
	case FollowUpAny, FollowUpNone, FollowUpScheduled, FollowUpOverdue:
		return true
	default:
		return false
	}
}

func validActionState(state ActionState) bool {
	switch state {
	case ActionAny, ActionNone, ActionOpen, ActionOverdue:
		return true
	default:
		return false
	}
}

func validSubjectPlacement(placement SubjectPlacement) bool {
	switch placement {
	case SubjectPlacementAny, SubjectPlacementPrimary, SubjectPlacementRelated:
		return true
	default:
		return false
	}
}

// canonicalEncoder writes length-prefixed fields so two different queries
// cannot produce the same bytes by shifting a boundary between values.
type canonicalEncoder struct {
	bytes []byte
}

func (encoder *canonicalEncoder) byte(value byte) {
	encoder.bytes = append(encoder.bytes, value)
}

func (encoder *canonicalEncoder) uint64(value uint64) {
	encoder.bytes = append(encoder.bytes,
		byte(value>>56), byte(value>>48), byte(value>>40), byte(value>>32),
		byte(value>>24), byte(value>>16), byte(value>>8), byte(value),
	)
}

func (encoder *canonicalEncoder) length(value int) {
	unsigned := uint32(value)
	encoder.bytes = append(encoder.bytes,
		byte(unsigned>>24), byte(unsigned>>16), byte(unsigned>>8), byte(unsigned),
	)
}

func (encoder *canonicalEncoder) string(value string) {
	encoder.length(len(value))
	encoder.bytes = append(encoder.bytes, value...)
}

func (encoder *canonicalEncoder) optionalTime(value *time.Time) {
	if value == nil {
		encoder.byte(0)
		return
	}
	encoder.byte(1)
	encoder.uint64(uint64(value.UnixMicro()))
}
