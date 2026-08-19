package records

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
)

// A draft list page is only complete once the caller can ask for the next one.
// Without a cursor the author's older drafts are unreachable, which is the same
// dead end the records list already solved.
func TestListDraftsReturnsCursorWhenMoreDraftsRemain(t *testing.T) {
	fixture := newDraftListFixture(t, 3)
	result, err := fixture.service.ListDrafts(context.Background(), DraftListRequest{
		Actor: fixture.actor, Limit: 2,
	})
	if err != nil {
		t.Fatalf("ListDrafts() error = %v", err)
	}
	if got := draftListIDs(result.Drafts); !equalStrings(got, []string{"rdf_page0", "rdf_page1"}) {
		t.Fatalf("ListDrafts() drafts = %v, want the first two", got)
	}
	if result.NextCursor == nil {
		t.Fatal("ListDrafts() NextCursor = nil, want a cursor for the withheld draft")
	}
	// The cursor has to name the last returned draft, not the first withheld one,
	// so a draft that changes between pages cannot be skipped.
	if result.NextCursor.DraftID != "rdf_page1" ||
		!result.NextCursor.UpdatedAt.Equal(fixture.updatedAt("rdf_page1")) {
		t.Fatalf("ListDrafts() NextCursor = %#v, want the last returned draft", result.NextCursor)
	}

	next, err := fixture.service.ListDrafts(context.Background(), DraftListRequest{
		Actor: fixture.actor, After: result.NextCursor, Limit: 2,
	})
	if err != nil {
		t.Fatalf("ListDrafts(after) error = %v", err)
	}
	if got := draftListIDs(next.Drafts); !equalStrings(got, []string{"rdf_page2"}) {
		t.Fatalf("ListDrafts(after) drafts = %v, want the remaining draft", got)
	}
	if next.NextCursor != nil {
		t.Fatalf("ListDrafts(after) NextCursor = %#v, want nil on the last page", next.NextCursor)
	}
}

// A full page that happens to exhaust the drafts must not advertise a next page,
// or the client pages forever into an empty result.
func TestListDraftsOmitsCursorWhenPageExactlyExhaustsDrafts(t *testing.T) {
	fixture := newDraftListFixture(t, 2)
	result, err := fixture.service.ListDrafts(context.Background(), DraftListRequest{
		Actor: fixture.actor, Limit: 2,
	})
	if err != nil {
		t.Fatalf("ListDrafts() error = %v", err)
	}
	if len(result.Drafts) != 2 || result.NextCursor != nil {
		t.Fatalf("ListDrafts() = %d drafts, cursor %#v, want 2 drafts and no cursor",
			len(result.Drafts), result.NextCursor)
	}
}

// Drafts dropped because their record no longer resolves, or because the draft
// itself vanished, still consumed a candidate slot. The page must be filled from
// further candidates rather than returned short, and the cursor must name a draft
// the caller actually saw.
func TestListDraftsFillsPagePastSkippedCandidates(t *testing.T) {
	fixture := newDraftListFixture(t, 6)
	fixture.store.attachToUnresolvableRecord("rdf_page1")
	fixture.store.missing["rdf_page2"] = true
	result, err := fixture.service.ListDrafts(context.Background(), DraftListRequest{
		Actor: fixture.actor, Limit: 2,
	})
	if err != nil {
		t.Fatalf("ListDrafts() error = %v", err)
	}
	if got := draftListIDs(result.Drafts); !equalStrings(got, []string{"rdf_page0", "rdf_page3"}) {
		t.Fatalf("ListDrafts() drafts = %v, want the page filled past the skipped drafts", got)
	}
	if result.NextCursor == nil || result.NextCursor.DraftID != "rdf_page3" {
		t.Fatalf("ListDrafts() NextCursor = %#v, want the last visible draft", result.NextCursor)
	}
}

// A cursor is a position, not a filter, so the store must receive it verbatim.
// Re-deriving the position in the service would let the two disagree.
func TestListDraftsPassesCursorAndBoundedBatchToStore(t *testing.T) {
	fixture := newDraftListFixture(t, 1)
	after := &DraftCursor{
		UpdatedAt: time.Date(2026, time.August, 4, 9, 30, 0, 0, time.UTC), DraftID: "rdf_resume",
	}
	if _, err := fixture.service.ListDrafts(context.Background(), DraftListRequest{
		Actor: fixture.actor, After: after, Limit: 5,
	}); err != nil {
		t.Fatalf("ListDrafts() error = %v", err)
	}
	if len(fixture.store.pages) != 1 {
		t.Fatalf("store pages = %d, want 1", len(fixture.store.pages))
	}
	page := fixture.store.pages[0]
	if page.After == nil || *page.After != *after {
		t.Fatalf("store page After = %#v, want %#v", page.After, after)
	}
	if page.Limit == 0 || page.Limit < 5 {
		t.Fatalf("store page Limit = %d, want a batch at least as large as the requested page", page.Limit)
	}
	// A caller-supplied cursor must not be reachable from the store's copy.
	after.DraftID = "rdf_mutated"
	if fixture.store.pages[0].After.DraftID != "rdf_resume" {
		t.Fatal("store page cursor aliases the caller's cursor")
	}
}

func TestListDraftsRejectsUnusableRequests(t *testing.T) {
	fixture := newDraftListFixture(t, 1)
	for _, testCase := range []struct {
		name    string
		request DraftListRequest
	}{
		{name: "zero limit", request: DraftListRequest{Actor: fixture.actor}},
		{name: "limit above bound", request: DraftListRequest{Actor: fixture.actor, Limit: 101}},
		{name: "unusable actor", request: DraftListRequest{Limit: 10}},
		{
			name: "cursor without draft",
			request: DraftListRequest{
				Actor: fixture.actor, Limit: 10,
				After: &DraftCursor{UpdatedAt: time.Now().UTC()},
			},
		},
		{
			name: "cursor without timestamp",
			request: DraftListRequest{
				Actor: fixture.actor, Limit: 10, After: &DraftCursor{DraftID: "rdf_page0"},
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := fixture.service.ListDrafts(context.Background(), testCase.request); !errors.Is(err, ErrInvalidDraftCommand) {
				t.Fatalf("ListDrafts(%s) error = %v, want ErrInvalidDraftCommand", testCase.name, err)
			}
		})
	}
}

type draftListFixture struct {
	service *DraftService
	store   *draftListStoreStub
	actor   recordauth.ActorScope
	drafts  []Draft
}

func (fixture draftListFixture) updatedAt(draftID string) time.Time {
	for _, draft := range fixture.drafts {
		if draft.DraftID == draftID {
			return draft.UpdatedAt
		}
	}
	return time.Time{}
}

func newDraftListFixture(t *testing.T, drafts int) draftListFixture {
	t.Helper()
	actor := recordauth.ActorScope{
		UserID:    "usr_000000000000000000000dfa",
		ProjectID: recordauth.ProjectIDDefault,
		Role:      recordauth.RoleProjectAdmin,
	}
	payload, err := NewDraftPayload([]byte(`{"title":"Draft"}`))
	if err != nil {
		t.Fatalf("NewDraftPayload() error = %v", err)
	}
	// Descending activity order, so index 0 is the newest and paging walks down.
	base := time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
	seeded := make([]Draft, 0, drafts)
	for index := 0; index < drafts; index++ {
		draftID := draftListID(index)
		etag, err := NewDraftETag(draftID, actor.UserID, 1, payload)
		if err != nil {
			t.Fatalf("NewDraftETag(%s) error = %v", draftID, err)
		}
		updatedAt := base.Add(-time.Duration(index) * time.Minute)
		seeded = append(seeded, Draft{
			DraftID:   draftID,
			ProjectID: actor.ProjectID,
			AuthorID:  actor.UserID,
			Payload:   payload,
			Version:   1,
			ETag:      etag,
			WarningAt: updatedAt.Add(time.Hour),
			CreatedAt: updatedAt,
			UpdatedAt: updatedAt,
			ExpiresAt: updatedAt.Add(2 * time.Hour),
		})
	}
	store := &draftListStoreStub{drafts: seeded, missing: map[string]bool{}}
	service, err := NewDraftService(store, store)
	if err != nil {
		t.Fatalf("NewDraftService() error = %v", err)
	}
	return draftListFixture{service: service, store: store, actor: actor, drafts: seeded}
}

func draftListID(index int) string {
	return "rdf_page" + strconv.Itoa(index)
}

func draftListIDs(drafts []Draft) []string {
	ids := make([]string, 0, len(drafts))
	for _, draft := range drafts {
		ids = append(ids, draft.DraftID)
	}
	return ids
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

// draftListStoreStub serves a fixed descending corpus and records the pages it
// was asked for, so the tests can assert both the visible result and the reads
// that produced it.
type draftListStoreStub struct {
	drafts  []Draft
	pages   []DraftCandidatePage
	missing map[string]bool
}

// attachToUnresolvableRecord points a draft at a record the read side cannot
// resolve, which is how a draft outlives its record.
func (store *draftListStoreStub) attachToUnresolvableRecord(draftID string) {
	for index := range store.drafts {
		if store.drafts[index].DraftID == draftID {
			store.drafts[index].RecordID = "rec_draftlistgone"
			store.drafts[index].BaseRevisionID = "rrv_draftlistgone"
		}
	}
}

func (store *draftListStoreStub) ListDraftRoutings(
	_ context.Context,
	authorID string,
	page DraftCandidatePage,
) ([]DraftRouting, error) {
	store.pages = append(store.pages, page)
	routings := make([]DraftRouting, 0, page.Limit)
	for _, draft := range store.drafts {
		if draft.AuthorID != authorID {
			continue
		}
		if page.After != nil {
			if draft.UpdatedAt.After(page.After.UpdatedAt) {
				continue
			}
			if draft.UpdatedAt.Equal(page.After.UpdatedAt) && draft.DraftID >= page.After.DraftID {
				continue
			}
		}
		routings = append(routings, DraftRoutingFromDraft(draft))
		if uint64(len(routings)) == page.Limit {
			break
		}
	}
	return routings, nil
}

func (store *draftListStoreStub) GetDraft(_ context.Context, draftID, authorID string) (Draft, error) {
	if store.missing[draftID] {
		return Draft{}, ErrDraftNotFound
	}
	for _, draft := range store.drafts {
		if draft.DraftID == draftID && draft.AuthorID == authorID {
			return draft, nil
		}
	}
	return Draft{}, ErrDraftNotFound
}

func (store *draftListStoreStub) GetDraftRouting(_ context.Context, draftID, authorID string) (DraftRouting, error) {
	for _, draft := range store.drafts {
		if draft.DraftID == draftID && draft.AuthorID == authorID {
			return DraftRoutingFromDraft(draft), nil
		}
	}
	return DraftRouting{}, ErrDraftNotFound
}

func (store *draftListStoreStub) CreateDraft(_ context.Context, _ DraftCreateCommand) (Draft, error) {
	return Draft{}, ErrDraftNotFound
}

func (store *draftListStoreStub) PatchDraft(_ context.Context, _ DraftPatchCommand) (Draft, error) {
	return Draft{}, ErrDraftNotFound
}

func (store *draftListStoreStub) DeleteDraft(_ context.Context, _ DraftDeleteCommand) error {
	return ErrDraftNotFound
}

// ResolveCurrentRecordAuthorization backs the denial cases: a draft attached to a
// record the actor may not read must drop out of the list.
func (store *draftListStoreStub) ResolveCurrentRecordAuthorization(
	_ context.Context,
	_ recordauth.ActorScope,
	recordID string,
) (CurrentRecordAuthorization, error) {
	return CurrentRecordAuthorization{}, ErrRecordNotFound
}
