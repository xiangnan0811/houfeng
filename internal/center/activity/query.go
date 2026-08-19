package activity

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sort"
	"time"

	"houfeng/internal/center/records"
)

var ErrInvalidQuery = errors.New("invalid activity query")

// View is the partial projection of one subject's timeline. All three read the
// same canonical rows through different server-side predicates; none of them is
// a second list built somewhere else.
type View string

const (
	ViewActivity View = "activity"
	ViewRecords  View = "records"
	ViewEvidence View = "evidence"
)

// VersionScope decides whether superseded revisions stay on the timeline.
type VersionScope string

const (
	// VersionsHistory is the default because the activity timeline is a log of
	// what happened. Hiding superseded revisions by default would quietly drop
	// events the operator performed.
	VersionsHistory VersionScope = "history"
	VersionsCurrent VersionScope = "current"
)

// SubjectRef addresses one observable subject. The kind is validated against
// the records subject registry, so a URL cannot invent a subject namespace.
type SubjectRef struct {
	Kind     records.SubjectKind
	SourceID string
}

// Query is the normalized question behind one page. The cursor binds to its
// digest, so this type is the single definition of "the same question" shared
// by the handler, the domain and the store.
type Query struct {
	Subject    SubjectRef
	View       View
	Sources    []SourceKind
	EventKinds []EventKind
	From       time.Time
	To         time.Time
	Versions   VersionScope
	Limit      int

	normalizedFlag bool
}

func (query Query) normalized() bool {
	return query.normalizedFlag
}

// NormalizeQuery produces the canonical form. Sorting and deduplicating the
// repeated filters is what makes ?source=a&source=b and ?source=b&source=a one
// query rather than two, which in turn makes a cursor from either usable on the
// other.
func NormalizeQuery(input Query) (Query, error) {
	if !records.ValidSubjectKind(input.Subject.Kind) {
		return Query{}, ErrInvalidQuery
	}
	if !records.ValidSubjectSourceID(input.Subject.Kind, input.Subject.SourceID) {
		return Query{}, ErrInvalidQuery
	}

	view := input.View
	if view == "" {
		view = ViewActivity
	}
	switch view {
	case ViewActivity, ViewRecords, ViewEvidence:
	default:
		return Query{}, ErrInvalidQuery
	}

	versions := input.Versions
	if versions == "" {
		versions = VersionsHistory
	}
	switch versions {
	case VersionsHistory, VersionsCurrent:
	default:
		return Query{}, ErrInvalidQuery
	}

	limit := input.Limit
	if limit == 0 {
		limit = DefaultPageSize
	}
	if limit < 1 || limit > MaxPageSize {
		return Query{}, ErrInvalidQuery
	}

	sources := make([]SourceKind, 0, len(input.Sources))
	seenSource := make(map[SourceKind]bool, len(input.Sources))
	for _, source := range input.Sources {
		if !ValidSourceKind(source) {
			return Query{}, ErrInvalidQuery
		}
		if seenSource[source] {
			continue
		}
		seenSource[source] = true
		sources = append(sources, source)
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i] < sources[j] })

	kinds := make([]EventKind, 0, len(input.EventKinds))
	seenKind := make(map[EventKind]bool, len(input.EventKinds))
	for _, kind := range input.EventKinds {
		if !ValidEventKind(kind) {
			return Query{}, ErrInvalidQuery
		}
		if seenKind[kind] {
			continue
		}
		seenKind[kind] = true
		kinds = append(kinds, kind)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })

	from := normalizeQueryBound(input.From)
	to := normalizeQueryBound(input.To)
	if !from.IsZero() && !to.IsZero() && to.Before(from) {
		return Query{}, ErrInvalidQuery
	}

	return Query{
		Subject:        input.Subject,
		View:           view,
		Sources:        sources,
		EventKinds:     kinds,
		From:           from,
		To:             to,
		Versions:       versions,
		Limit:          limit,
		normalizedFlag: true,
	}, nil
}

func normalizeQueryBound(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	return value.UTC().Truncate(time.Microsecond)
}

// ResolvedEventKinds folds the view predicate and the explicit filter into the
// one set the store may return. Doing it here keeps the handler and the SQL from
// growing two different ideas of which events count as a record.
func (query Query) ResolvedEventKinds() []EventKind {
	viewKinds := query.viewEventKinds()
	if len(query.EventKinds) == 0 {
		return viewKinds
	}
	if viewKinds == nil {
		kinds := make([]EventKind, len(query.EventKinds))
		copy(kinds, query.EventKinds)
		return kinds
	}
	allowed := make(map[EventKind]bool, len(viewKinds))
	for _, kind := range viewKinds {
		allowed[kind] = true
	}
	intersection := make([]EventKind, 0, len(query.EventKinds))
	for _, kind := range query.EventKinds {
		if allowed[kind] {
			intersection = append(intersection, kind)
		}
	}
	return intersection
}

// viewEventKinds returns nil when the view does not constrain kinds, which is
// different from returning an empty slice: nil means "no kind predicate", empty
// means "nothing can match".
func (query Query) viewEventKinds() []EventKind {
	switch query.View {
	case ViewRecords:
		return RecordsViewEventKinds()
	case ViewEvidence:
		return []EventKind{EventKindEvidenceCaptured}
	default:
		return nil
	}
}

// Digest is the binding value carried inside a cursor. Components are
// length-prefixed so two different filter lists cannot flatten into the same
// bytes.
func (query Query) Digest() [sha256.Size]byte {
	digest := sha256.New()
	digest.Write([]byte("houfeng.record-activity.query.v1"))
	writeDigestString := func(value string) {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(value)))
		digest.Write(length[:])
		digest.Write([]byte(value))
	}
	writeDigestString(string(query.Subject.Kind))
	writeDigestString(query.Subject.SourceID)
	writeDigestString(string(query.View))
	writeDigestString(string(query.Versions))

	var count [8]byte
	binary.BigEndian.PutUint64(count[:], uint64(len(query.Sources)))
	digest.Write(count[:])
	for _, source := range query.Sources {
		writeDigestString(string(source))
	}
	binary.BigEndian.PutUint64(count[:], uint64(len(query.EventKinds)))
	digest.Write(count[:])
	for _, kind := range query.EventKinds {
		writeDigestString(string(kind))
	}

	var bound [8]byte
	binary.BigEndian.PutUint64(bound[:], uint64(query.From.UnixMicro()))
	digest.Write(bound[:])
	binary.BigEndian.PutUint64(bound[:], uint64(query.To.UnixMicro()))
	digest.Write(bound[:])
	binary.BigEndian.PutUint64(bound[:], uint64(query.Limit))
	digest.Write(bound[:])

	var sum [sha256.Size]byte
	copy(sum[:], digest.Sum(nil))
	return sum
}
