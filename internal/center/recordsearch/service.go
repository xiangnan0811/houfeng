package recordsearch

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

var (
	// ErrIndexUnavailable reports that no published generation exists. Bootstrap
	// publishes one, so this is an operational fault rather than caller error and
	// must not be reported as an empty result: an empty page would read as "no
	// such records" when the truth is "the index is not serving".
	ErrIndexUnavailable = errors.New("record search index unavailable")
	// ErrGenerationSuperseded reports that the generation a cursor was minted
	// against is no longer published. The caller may restart from page one.
	ErrGenerationSuperseded = errors.New("record search generation superseded")
	// ErrInvalidSearchRequest reports unusable service input.
	ErrInvalidSearchRequest = errors.New("invalid record search request")
)

const (
	// candidateBatchSize bounds one trip to the index. Candidates are cheap
	// identity rows, and authorization removes an unknown number of them, so the
	// service reads ahead in batches rather than one page at a time.
	candidateBatchSize uint32 = 200
	// maxCandidateBatches bounds the read-ahead. A query whose matches are almost
	// all unauthorized would otherwise walk the whole index for one page; a short
	// page is a better answer than an unbounded scan.
	maxCandidateBatches int = 20
	// DefaultCursorTTL bounds how long a next-page token stays usable. It is
	// short because a cursor is bound to a published generation, and a rebuild
	// invalidates it anyway.
	DefaultCursorTTL time.Duration = 30 * time.Minute
)

// Candidate is one indexed match. The index returns identity and sort position
// only; content comes from the authorized read path.
type Candidate struct {
	RecordID  string
	UpdatedAt time.Time
}

// CandidatePage is one request to the index.
type CandidatePage struct {
	Query      Query
	Generation uint64
	After      *SortKey
	Limit      uint32
}

// CandidateStore is the index side of search. It applies every filter in SQL and
// never decides visibility: the index stores a visibility digest, not the grants
// behind it.
type CandidateStore interface {
	PublishedGeneration(context.Context) (uint64, error)
	ListSearchCandidates(context.Context, CandidatePage) ([]Candidate, error)
}

// RecordReader hydrates and authorizes one candidate. Reusing the record read
// path is what keeps search from growing a second authorization implementation.
type RecordReader interface {
	GetRecord(context.Context, records.RecordGetRequest) (records.Record, error)
}

// SearchRequest is one page request from transport.
type SearchRequest struct {
	Actor  recordauth.ActorScope
	Query  Query
	Cursor string
}

// Result is one authorized page. There is deliberately no total count: a count
// over candidates would report rows the actor may not read, and a count over
// authorized rows would cost a full authorized walk of the index.
type Result struct {
	Records    []records.Record
	NextCursor string
	Generation uint64
}

// Service turns a normalized query into one page of authorized records.
type Service struct {
	candidates CandidateStore
	reader     RecordReader
	now        func() time.Time
	cursorTTL  time.Duration
}

func NewService(candidates CandidateStore, reader RecordReader) (*Service, error) {
	if nilSearchDependency(candidates) || nilSearchDependency(reader) {
		return nil, fmt.Errorf("%w: dependency", ErrInvalidSearchRequest)
	}
	return &Service{
		candidates: candidates,
		reader:     reader,
		now:        func() time.Time { return time.Now().UTC() },
		cursorTTL:  DefaultCursorTTL,
	}, nil
}

// NewServiceWithClock exists for tests that need a fixed cursor expiry.
func NewServiceWithClock(
	candidates CandidateStore,
	reader RecordReader,
	now func() time.Time,
	cursorTTL time.Duration,
) (*Service, error) {
	service, err := NewService(candidates, reader)
	if err != nil {
		return nil, err
	}
	if now == nil || cursorTTL <= 0 {
		return nil, fmt.Errorf("%w: clock", ErrInvalidSearchRequest)
	}
	service.now = now
	service.cursorTTL = cursorTTL
	return service, nil
}

func (service *Service) Search(ctx context.Context, request SearchRequest) (Result, error) {
	if ctx == nil || service == nil || nilSearchDependency(service.candidates) ||
		nilSearchDependency(service.reader) || !request.Query.normalized() {
		return Result{}, ErrInvalidSearchRequest
	}
	actor, err := recordauth.NormalizeActorScope(request.Actor)
	if err != nil {
		return Result{}, fmt.Errorf("%w: actor", ErrInvalidSearchRequest)
	}
	generation, err := service.candidates.PublishedGeneration(ctx)
	if err != nil {
		return Result{}, err
	}
	if generation == 0 {
		return Result{}, ErrIndexUnavailable
	}

	var after *SortKey
	if request.Cursor != "" {
		cursor, err := BindCursor(request.Cursor, request.Query, actor, generation, service.now())
		if err != nil {
			return Result{}, err
		}
		position := cursor.SortKey()
		after = &position
	}

	pageSize := uint64(request.Query.PageSize())
	matched := make([]Candidate, 0, pageSize+1)
	hydrated := make([]records.Record, 0, pageSize+1)
	for batch := 0; batch < maxCandidateBatches && uint64(len(matched)) <= pageSize; batch++ {
		candidates, err := service.candidates.ListSearchCandidates(ctx, CandidatePage{
			Query:      request.Query,
			Generation: generation,
			After:      cloneSortKey(after),
			Limit:      candidateBatchSize,
		})
		if err != nil {
			return Result{}, err
		}
		if len(candidates) == 0 {
			break
		}
		for _, candidate := range candidates {
			record, err := service.reader.GetRecord(ctx, records.RecordGetRequest{
				Actor:    actor.Clone(),
				RecordID: candidate.RecordID,
			})
			if err != nil {
				if skippableSearchCandidateError(err) {
					continue
				}
				return Result{}, err
			}
			matched = append(matched, candidate)
			hydrated = append(hydrated, record)
			if uint64(len(matched)) > pageSize {
				break
			}
		}
		last := candidates[len(candidates)-1]
		after = &SortKey{UpdatedAt: last.UpdatedAt.UTC(), RecordID: last.RecordID}
		if uint32(len(candidates)) < candidateBatchSize {
			break
		}
	}

	result := Result{Generation: generation}
	if uint64(len(matched)) > pageSize {
		matched = matched[:pageSize]
		hydrated = hydrated[:pageSize]
		token, err := EncodeCursor(CursorValues{
			Query:      request.Query,
			Actor:      actor,
			Generation: generation,
			ExpiresAt:  service.now().Add(service.cursorTTL),
			SortKey:    SortKey{UpdatedAt: matched[len(matched)-1].UpdatedAt, RecordID: matched[len(matched)-1].RecordID},
		})
		if err != nil {
			return Result{}, err
		}
		result.NextCursor = token
	}
	result.Records = hydrated
	return result, nil
}

// skippableSearchCandidateError reports whether one candidate can be dropped
// from the page. A denial, a record that vanished, and a record whose revision
// moved under the read all mean the same thing to a search result: this row is
// not this actor's to see right now. Anything else is a fault worth surfacing.
func skippableSearchCandidateError(err error) bool {
	return errors.Is(err, recordauth.ErrDenied) ||
		errors.Is(err, records.ErrRecordNotFound) ||
		errors.Is(err, records.ErrRecordRevisionConflict) ||
		errors.Is(err, records.ErrRecordDeletionReserved)
}

func cloneSortKey(value *SortKey) *SortKey {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.UpdatedAt = cloned.UpdatedAt.UTC()
	return &cloned
}

func nilSearchDependency(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
