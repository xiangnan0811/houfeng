package activity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

var (
	// ErrProjectionUnavailable means no published generation is serving reads.
	// Callers map this to HTTP 503 rather than an empty timeline, which would
	// read as "nothing happened" when the truth is "the projection is down".
	ErrProjectionUnavailable = errors.New("activity projection unavailable")
	// ErrSubjectNotFound unifies missing subjects and authorization denials so
	// callers cannot probe which case occurred.
	ErrSubjectNotFound = errors.New("activity subject not found")
	// ErrInvalidListRequest reports unusable service input.
	ErrInvalidListRequest = errors.New("invalid activity list request")
)

// SubjectStatus reports whether the registry still resolves a live route.
type SubjectStatus string

const (
	SubjectStatusLive       SubjectStatus = "live"
	SubjectStatusTombstoned SubjectStatus = "tombstoned"
)

// ListRequest is one page request from transport.
type ListRequest struct {
	Actor  recordauth.ActorScope
	Query  Query
	Cursor string
}

// SubjectHeader is the subject identity bar for one timeline page.
type SubjectHeader struct {
	Kind      records.SubjectKind `json:"kind"`
	SourceID  string              `json:"source_id"`
	Identity  map[string]string   `json:"identity"`
	LiveRoute string              `json:"live_route,omitempty"`
	Status    SubjectStatus       `json:"status"`
}

// Freshness summarizes how current this fixed-watermark page is.
type Freshness struct {
	State             string     `json:"state"`
	VisibleObservedAt *time.Time `json:"visible_observed_at"`
	NewItemsAvailable bool       `json:"new_items_available"`
	ReasonCode        string     `json:"reason_code"`
}

// SourceStatus is the safe per-source readiness exposed to browsers.
type SourceStatus struct {
	SourceKind SourceKind `json:"source_kind"`
	State      string     `json:"state"`
	ReasonCode string     `json:"reason_code"`
}

// ListResult is one authorized subject-activity page.
type ListResult struct {
	Subject        SubjectHeader  `json:"subject"`
	View           View           `json:"view"`
	SnapshotCursor string         `json:"snapshot_cursor"`
	Freshness      Freshness      `json:"freshness"`
	Items          []Event        `json:"items"`
	SourceStatuses []SourceStatus `json:"source_statuses"`
	NextCursor     string         `json:"next_cursor,omitempty"`
}

// PublishedHead is the committed contiguous projection watermark.
type PublishedHead struct {
	Generation              uint64
	PublishedIngestSequence uint64
}

// PublishedHeadStore reads the generation currently serving list queries.
type PublishedHeadStore interface {
	LoadPublishedHead(ctx context.Context) (PublishedHead, error)
}

// SubjectPageRequest is the normalized store question for one page.
type SubjectPageRequest struct {
	Query              Query
	Generation         uint64
	AsOf               uint64
	After              *SortKey
	Limit              int
	AuthUnrestricted   bool
	AllowedAuthDigests [][32]byte
}

// SubjectPageResult is one store page before cursor encoding.
type SubjectPageResult struct {
	Events             []Event
	SubjectKnown       bool
	TombstoneIdentity  map[string]string
	TombstoneLiveRoute string
}

// SubjectPageStore applies every predicate before ORDER/LIMIT.
type SubjectPageStore interface {
	ListSubjectPage(ctx context.Context, req SubjectPageRequest) (SubjectPageResult, error)
	HasNewerAuthorized(ctx context.Context, req SubjectPageRequest, afterSequence uint64) (bool, error)
	MaxVisibleObservedAt(ctx context.Context, req SubjectPageRequest) (*time.Time, error)
	LoadSourceStatuses(ctx context.Context, generation uint64) ([]SourceStatus, error)
}

// LiveSubjectResolver resolves a live subject header from the registry.
type LiveSubjectResolver interface {
	ResolveLive(ctx context.Context, actor recordauth.ActorScope, ref SubjectRef) (SubjectHeader, error)
}

// Service turns one list request into a fixed-watermark page.
type Service struct {
	headStore PublishedHeadStore
	pages     SubjectPageStore
	live      LiveSubjectResolver
	codec     *CursorCodec
	now       func() time.Time
	cursorTTL time.Duration
}

// NewService wires the activity list path. Every dependency is required so
// bootstrap cannot accidentally serve pages without auth or cursors.
func NewService(
	headStore PublishedHeadStore,
	pages SubjectPageStore,
	live LiveSubjectResolver,
	codec *CursorCodec,
) (*Service, error) {
	if nilActivityDependency(headStore) || nilActivityDependency(pages) ||
		nilActivityDependency(live) || codec == nil {
		return nil, fmt.Errorf("%w: dependency", ErrInvalidListRequest)
	}
	return &Service{
		headStore: headStore,
		pages:     pages,
		live:      live,
		codec:     codec,
		now:       func() time.Time { return time.Now().UTC() },
		cursorTTL: DefaultCursorTTL,
	}, nil
}

// NewServiceWithClock exists for tests that need a fixed cursor expiry.
func NewServiceWithClock(
	headStore PublishedHeadStore,
	pages SubjectPageStore,
	live LiveSubjectResolver,
	codec *CursorCodec,
	now func() time.Time,
	cursorTTL time.Duration,
) (*Service, error) {
	service, err := NewService(headStore, pages, live, codec)
	if err != nil {
		return nil, err
	}
	if now == nil || cursorTTL <= 0 {
		return nil, fmt.Errorf("%w: clock", ErrInvalidListRequest)
	}
	service.now = now
	service.cursorTTL = cursorTTL
	return service, nil
}

// List returns one authorized page at a fixed ingest watermark.
func (service *Service) List(ctx context.Context, request ListRequest) (ListResult, error) {
	if ctx == nil || service == nil || nilActivityDependency(service.headStore) ||
		nilActivityDependency(service.pages) || nilActivityDependency(service.live) ||
		service.codec == nil {
		return ListResult{}, ErrInvalidListRequest
	}

	actor, err := recordauth.NormalizeActorScope(request.Actor)
	if err != nil {
		return ListResult{}, fmt.Errorf("%w: actor", ErrInvalidListRequest)
	}
	query, err := NormalizeQuery(request.Query)
	if err != nil {
		return ListResult{}, fmt.Errorf("%w: query", ErrInvalidListRequest)
	}

	head, err := service.headStore.LoadPublishedHead(ctx)
	if err != nil {
		if errors.Is(err, ErrProjectionUnavailable) {
			return ListResult{}, ErrProjectionUnavailable
		}
		return ListResult{}, err
	}
	if head.Generation == 0 {
		return ListResult{}, ErrProjectionUnavailable
	}

	auth, err := authFilterForActor(actor)
	if err != nil {
		return ListResult{}, fmt.Errorf("%w: auth filter", ErrInvalidListRequest)
	}

	asOf := head.PublishedIngestSequence
	var after *SortKey
	if request.Cursor != "" {
		cursor, err := service.codec.Bind(
			request.Cursor,
			query,
			actor,
			head.Generation,
			service.now(),
		)
		if err != nil {
			return ListResult{}, err
		}
		asOf = cursor.AsOf()
		if !cursor.FirstPage() {
			position := cursor.SortKey()
			after = &position
		}
	}

	pageRequest := SubjectPageRequest{
		Query:              query,
		Generation:         head.Generation,
		AsOf:               asOf,
		After:              after,
		Limit:              query.Limit + 1,
		AuthUnrestricted:   auth.Unrestricted,
		AllowedAuthDigests: auth.AllowedAuthDigests,
	}

	liveHeader, liveErr := service.live.ResolveLive(ctx, actor, query.Subject)
	liveFound := liveErr == nil
	if liveErr != nil && !errors.Is(liveErr, ErrSubjectNotFound) {
		return ListResult{}, liveErr
	}

	page, err := service.pages.ListSubjectPage(ctx, pageRequest)
	if err != nil {
		return ListResult{}, err
	}
	if !liveFound && !page.SubjectKnown {
		return ListResult{}, ErrSubjectNotFound
	}

	subject, err := service.subjectHeader(query.Subject, liveHeader, liveFound, page)
	if err != nil {
		return ListResult{}, err
	}

	items := page.Events
	if items == nil {
		items = []Event{}
	}
	nextCursor := ""
	if len(items) > query.Limit {
		items = items[:query.Limit]
		token, err := service.codec.Encode(CursorValues{
			Query:      query,
			Actor:      actor,
			Generation: head.Generation,
			AsOf:       asOf,
			ExpiresAt:  service.now().Add(service.cursorTTL),
			SortKey:    eventSortKey(items[len(items)-1]),
		})
		if err != nil {
			return ListResult{}, err
		}
		nextCursor = token
	}

	snapshotCursor, err := service.codec.Encode(CursorValues{
		Query:      query,
		Actor:      actor,
		Generation: head.Generation,
		AsOf:       asOf,
		ExpiresAt:  service.now().Add(service.cursorTTL),
	})
	if err != nil {
		return ListResult{}, err
	}

	newerRequest := pageRequest
	newerRequest.AsOf = head.PublishedIngestSequence
	newerAvailable, err := service.pages.HasNewerAuthorized(ctx, newerRequest, asOf)
	if err != nil {
		return ListResult{}, err
	}

	visibleObservedAt, err := service.pages.MaxVisibleObservedAt(ctx, pageRequest)
	if err != nil {
		return ListResult{}, err
	}

	sourceStatuses, err := service.pages.LoadSourceStatuses(ctx, head.Generation)
	if err != nil {
		return ListResult{}, err
	}
	if sourceStatuses == nil {
		sourceStatuses = []SourceStatus{}
	}

	return ListResult{
		Subject:        subject,
		View:           query.View,
		SnapshotCursor: snapshotCursor,
		Freshness:      buildFreshness(sourceStatuses, visibleObservedAt, newerAvailable),
		Items:          items,
		SourceStatuses: sourceStatuses,
		NextCursor:     nextCursor,
	}, nil
}

func (service *Service) subjectHeader(
	subject SubjectRef,
	live SubjectHeader,
	liveFound bool,
	page SubjectPageResult,
) (SubjectHeader, error) {
	if liveFound {
		if live.Identity == nil {
			live.Identity = map[string]string{}
		}
		live.Status = SubjectStatusLive
		return live, nil
	}
	identity := page.TombstoneIdentity
	if identity == nil {
		identity = map[string]string{}
	}
	return SubjectHeader{
		Kind:     subject.Kind,
		SourceID: subject.SourceID,
		Identity: identity,
		Status:   SubjectStatusTombstoned,
	}, nil
}

// buildFreshness summarizes how current a fixed-watermark page is for the
// caller. State is derived only from authorized-visible signals: global
// projector source health must not flip freshness for hidden scopes the actor
// cannot see. SourceStatuses remain on ListResult for diagnostics.
func buildFreshness(
	_ []SourceStatus,
	visibleObservedAt *time.Time,
	newItemsAvailable bool,
) Freshness {
	return Freshness{
		State:             "ready",
		VisibleObservedAt: visibleObservedAt,
		NewItemsAvailable: newItemsAvailable,
		ReasonCode:        "",
	}
}

func eventSortKey(event Event) SortKey {
	sourceKind := event.SourceKind
	if sourceKind == "" {
		sourceKind = event.Source.Kind
	}
	return SortKey{
		EventAt:    event.EventAt.UTC(),
		RecordedAt: event.RecordedAt.UTC(),
		SourceKind: sourceKind,
		ActivityID: event.ActivityID,
	}
}

func nilActivityDependency(value any) bool {
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

// MarshalJSON keeps collection fields as arrays and never leaks server-side
// watermarks into the browser payload.
func (result ListResult) MarshalJSON() ([]byte, error) {
	type wire ListResult
	clone := wire(result)
	if clone.Items == nil {
		clone.Items = []Event{}
	}
	if clone.SourceStatuses == nil {
		clone.SourceStatuses = []SourceStatus{}
	}
	if clone.Subject.Identity == nil {
		clone.Subject.Identity = map[string]string{}
	}
	return json.Marshal(clone)
}

// MarshalJSON keeps subject identity as an object even when empty.
func (header SubjectHeader) MarshalJSON() ([]byte, error) {
	type wire SubjectHeader
	clone := wire(header)
	if clone.Identity == nil {
		clone.Identity = map[string]string{}
	}
	return json.Marshal(clone)
}
