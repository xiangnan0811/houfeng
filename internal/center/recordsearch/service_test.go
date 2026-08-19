package recordsearch

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

type fakeCandidateStore struct {
	generation      uint64
	generationErr   error
	pages           [][]Candidate
	pageErr         error
	requestedAfters []*SortKey
	calls           int
}

func (store *fakeCandidateStore) PublishedGeneration(context.Context) (uint64, error) {
	if store.generationErr != nil {
		return 0, store.generationErr
	}
	return store.generation, nil
}

func (store *fakeCandidateStore) ListSearchCandidates(
	_ context.Context,
	page CandidatePage,
) ([]Candidate, error) {
	if store.pageErr != nil {
		return nil, store.pageErr
	}
	store.requestedAfters = append(store.requestedAfters, page.After)
	if store.calls >= len(store.pages) {
		store.calls++
		return nil, nil
	}
	candidates := store.pages[store.calls]
	store.calls++
	return candidates, nil
}

type fakeRecordReader struct {
	denied  map[string]error
	reads   []string
	readErr error
}

func (reader *fakeRecordReader) GetRecord(
	_ context.Context,
	request records.RecordGetRequest,
) (records.Record, error) {
	reader.reads = append(reader.reads, request.RecordID)
	if reader.readErr != nil {
		return records.Record{}, reader.readErr
	}
	if err, blocked := reader.denied[request.RecordID]; blocked {
		return records.Record{}, err
	}
	return records.Record{RecordID: request.RecordID}, nil
}

func testSearchActor(t *testing.T) recordauth.ActorScope {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID:    "usr_000000000000000000000001",
		Role:      recordauth.RoleProjectAdmin,
		ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	return actor
}

func testSearchQuery(t *testing.T, pageSize uint32) Query {
	t.Helper()
	query, err := NormalizeQuery(QueryValues{Text: "磁盘", PageSize: pageSize})
	if err != nil {
		t.Fatalf("NormalizeQuery() error = %v", err)
	}
	return query
}

func testSearchCandidates(count int, base time.Time) []Candidate {
	candidates := make([]Candidate, 0, count)
	for index := range count {
		candidates = append(candidates, Candidate{
			RecordID:  fmt.Sprintf("rec_%024d", index),
			UpdatedAt: base.Add(-time.Duration(index) * time.Minute),
		})
	}
	return candidates
}

func newTestSearchService(
	t *testing.T,
	store *fakeCandidateStore,
	reader *fakeRecordReader,
) *Service {
	t.Helper()
	service, err := NewServiceWithClock(
		store,
		reader,
		func() time.Time { return time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC) },
		time.Hour,
	)
	if err != nil {
		t.Fatalf("NewServiceWithClock() error = %v", err)
	}
	return service
}

// A page has to stop at the requested size and hand back a token, and the token
// has to resume from the last row the caller actually saw rather than from the
// last row the index offered.
func TestSearchServiceReturnsOnePageAndResumableCursor(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	candidates := testSearchCandidates(4, base)
	store := &fakeCandidateStore{generation: 7, pages: [][]Candidate{candidates}}
	reader := &fakeRecordReader{}
	service := newTestSearchService(t, store, reader)
	query := testSearchQuery(t, 2)
	actor := testSearchActor(t)

	result, err := service.Search(context.Background(), SearchRequest{Actor: actor, Query: query})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Records) != 2 || result.Records[0].RecordID != candidates[0].RecordID ||
		result.Records[1].RecordID != candidates[1].RecordID {
		t.Fatalf("Search() records = %#v, want the first two candidates", result.Records)
	}
	if result.Generation != 7 || result.NextCursor == "" {
		t.Fatalf("Search() generation/cursor = %d/%q, want 7 and a token", result.Generation, result.NextCursor)
	}

	cursor, err := BindCursor(result.NextCursor, query, actor, 7, service.now())
	if err != nil {
		t.Fatalf("BindCursor() error = %v", err)
	}
	if cursor.SortKey().RecordID != candidates[1].RecordID {
		t.Fatalf("cursor position = %q, want the last returned record %q",
			cursor.SortKey().RecordID, candidates[1].RecordID)
	}
}

// A full page with nothing beyond it must not offer a cursor, or the caller would
// fetch an empty page to discover it had finished.
func TestSearchServiceOmitsCursorOnFinalPage(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	store := &fakeCandidateStore{generation: 3, pages: [][]Candidate{testSearchCandidates(2, base)}}
	service := newTestSearchService(t, store, &fakeRecordReader{})

	result, err := service.Search(context.Background(), SearchRequest{
		Actor: testSearchActor(t),
		Query: testSearchQuery(t, 2),
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Records) != 2 || result.NextCursor != "" {
		t.Fatalf("Search() = %d records with cursor %q, want a final page of 2",
			len(result.Records), result.NextCursor)
	}
}

// The index cannot decide visibility, so a candidate the actor may not read has
// to drop out of the page silently and must not consume a page slot.
func TestSearchServiceDropsUnauthorizedCandidates(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	candidates := testSearchCandidates(5, base)
	store := &fakeCandidateStore{generation: 2, pages: [][]Candidate{candidates}}
	reader := &fakeRecordReader{denied: map[string]error{
		candidates[0].RecordID: recordauth.ErrDenied,
		candidates[2].RecordID: records.ErrRecordNotFound,
		candidates[3].RecordID: records.ErrRecordRevisionConflict,
	}}
	service := newTestSearchService(t, store, reader)

	result, err := service.Search(context.Background(), SearchRequest{
		Actor: testSearchActor(t),
		Query: testSearchQuery(t, 10),
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Records) != 2 ||
		result.Records[0].RecordID != candidates[1].RecordID ||
		result.Records[1].RecordID != candidates[4].RecordID {
		t.Fatalf("Search() records = %#v, want only the two readable candidates", result.Records)
	}
	if len(reader.reads) != 5 {
		t.Fatalf("hydration attempts = %d, want every candidate authorized", len(reader.reads))
	}
}

// A fault from the read path is not a missing row. Treating it as skippable would
// silently shorten a page whenever the database misbehaved.
func TestSearchServiceSurfacesReadFaults(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	wantErr := errors.New("read path fault")
	store := &fakeCandidateStore{generation: 1, pages: [][]Candidate{testSearchCandidates(1, base)}}
	service := newTestSearchService(t, store, &fakeRecordReader{readErr: wantErr})

	if _, err := service.Search(context.Background(), SearchRequest{
		Actor: testSearchActor(t),
		Query: testSearchQuery(t, 10),
	}); !errors.Is(err, wantErr) {
		t.Fatalf("Search() error = %v, want %v", err, wantErr)
	}
}

// Authorization removes an unknown number of candidates, so the service has to
// read ahead across batches to fill one page.
func TestSearchServiceReadsAheadAcrossBatches(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	first := testSearchCandidates(int(candidateBatchSize), base)
	second := []Candidate{{RecordID: "rec_extra00000000000000001", UpdatedAt: base.Add(-time.Hour)}}
	denied := make(map[string]error, len(first))
	for _, candidate := range first {
		denied[candidate.RecordID] = recordauth.ErrDenied
	}
	store := &fakeCandidateStore{generation: 4, pages: [][]Candidate{first, second}}
	service := newTestSearchService(t, store, &fakeRecordReader{denied: denied})

	result, err := service.Search(context.Background(), SearchRequest{
		Actor: testSearchActor(t),
		Query: testSearchQuery(t, 5),
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Records) != 1 || result.Records[0].RecordID != second[0].RecordID {
		t.Fatalf("Search() records = %#v, want the readable record from the second batch", result.Records)
	}
	if len(store.requestedAfters) < 2 || store.requestedAfters[0] != nil {
		t.Fatalf("requested positions = %#v, want a first unpositioned read then a resumed one",
			store.requestedAfters)
	}
	if store.requestedAfters[1] == nil ||
		store.requestedAfters[1].RecordID != first[len(first)-1].RecordID {
		t.Fatalf("second read resumed at %#v, want after the last candidate of the first batch",
			store.requestedAfters[1])
	}
}

// A query whose matches are nearly all unauthorized must not walk the whole
// index for one page. A short page is the better answer.
func TestSearchServiceBoundsReadAhead(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	pages := make([][]Candidate, 0, maxCandidateBatches+5)
	denied := map[string]error{}
	for batch := range maxCandidateBatches + 5 {
		candidates := make([]Candidate, 0, candidateBatchSize)
		for index := range int(candidateBatchSize) {
			id := fmt.Sprintf("rec_%06d%018d", batch, index)
			candidates = append(candidates, Candidate{
				RecordID:  id,
				UpdatedAt: base.Add(-time.Duration(batch*1000+index) * time.Second),
			})
			denied[id] = recordauth.ErrDenied
		}
		pages = append(pages, candidates)
	}
	store := &fakeCandidateStore{generation: 5, pages: pages}
	service := newTestSearchService(t, store, &fakeRecordReader{denied: denied})

	result, err := service.Search(context.Background(), SearchRequest{
		Actor: testSearchActor(t),
		Query: testSearchQuery(t, 10),
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Records) != 0 || result.NextCursor != "" {
		t.Fatalf("Search() = %d records cursor %q, want an empty bounded page",
			len(result.Records), result.NextCursor)
	}
	if store.calls != maxCandidateBatches {
		t.Fatalf("index reads = %d, want the read-ahead bound %d", store.calls, maxCandidateBatches)
	}
}

// A cursor minted against a generation that has since been republished must not
// silently resume in the new one, and the failure must not read as "no results".
func TestSearchServiceRejectsCursorFromAnotherGeneration(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	store := &fakeCandidateStore{generation: 9, pages: [][]Candidate{testSearchCandidates(3, base)}}
	service := newTestSearchService(t, store, &fakeRecordReader{})
	query := testSearchQuery(t, 2)
	actor := testSearchActor(t)
	stale, err := EncodeCursor(CursorValues{
		Query:      query,
		Actor:      actor,
		Generation: 8,
		ExpiresAt:  service.now().Add(time.Hour),
		SortKey:    SortKey{UpdatedAt: base, RecordID: "rec_000000000000000000000000"},
	})
	if err != nil {
		t.Fatalf("EncodeCursor() error = %v", err)
	}

	if _, err := service.Search(context.Background(), SearchRequest{
		Actor: actor, Query: query, Cursor: stale,
	}); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("Search() error = %v, want %v", err, ErrInvalidCursor)
	}
}

// With no published generation the index is not serving. Reporting an empty page
// would read as "no such records", which is a different and wrong answer.
func TestSearchServiceReportsUnavailableIndex(t *testing.T) {
	t.Parallel()

	service := newTestSearchService(t, &fakeCandidateStore{generation: 0}, &fakeRecordReader{})

	if _, err := service.Search(context.Background(), SearchRequest{
		Actor: testSearchActor(t),
		Query: testSearchQuery(t, 10),
	}); !errors.Is(err, ErrIndexUnavailable) {
		t.Fatalf("Search() error = %v, want %v", err, ErrIndexUnavailable)
	}
}

func TestSearchServiceRejectsUnusableInput(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	store := &fakeCandidateStore{generation: 1, pages: [][]Candidate{testSearchCandidates(1, base)}}
	service := newTestSearchService(t, store, &fakeRecordReader{})

	tests := []struct {
		name    string
		request SearchRequest
	}{
		{name: "unnormalized query", request: SearchRequest{Actor: testSearchActor(t)}},
		{name: "unnormalized actor", request: SearchRequest{Query: testSearchQuery(t, 10)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := service.Search(context.Background(), tt.request); !errors.Is(err, ErrInvalidSearchRequest) {
				t.Fatalf("Search() error = %v, want %v", err, ErrInvalidSearchRequest)
			}
		})
	}
	if _, err := NewService(nil, &fakeRecordReader{}); !errors.Is(err, ErrInvalidSearchRequest) {
		t.Fatalf("NewService(nil store) error = %v, want %v", err, ErrInvalidSearchRequest)
	}
	if _, err := NewService(store, nil); !errors.Is(err, ErrInvalidSearchRequest) {
		t.Fatalf("NewService(nil reader) error = %v, want %v", err, ErrInvalidSearchRequest)
	}
}
