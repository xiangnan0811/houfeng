package activity

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
)

type fakePublishedHeadStore struct {
	head PublishedHead
	err  error
}

func (store *fakePublishedHeadStore) LoadPublishedHead(context.Context) (PublishedHead, error) {
	if store.err != nil {
		return PublishedHead{}, store.err
	}
	return store.head, nil
}

type fakeSubjectPageStore struct {
	pageResult          SubjectPageResult
	pageErr             error
	lastPageRequest     SubjectPageRequest
	newerAvailable      bool
	newerErr            error
	lastNewerRequest    SubjectPageRequest
	lastAfterSequence   uint64
	maxObservedAt       *time.Time
	maxObservedErr      error
	lastObservedRequest SubjectPageRequest
	sourceStatuses      []SourceStatus
	sourceStatusesErr   error
}

func (store *fakeSubjectPageStore) ListSubjectPage(
	_ context.Context,
	req SubjectPageRequest,
) (SubjectPageResult, error) {
	store.lastPageRequest = req
	if store.pageErr != nil {
		return SubjectPageResult{}, store.pageErr
	}
	return store.pageResult, nil
}

func (store *fakeSubjectPageStore) HasNewerAuthorized(
	_ context.Context,
	req SubjectPageRequest,
	afterSequence uint64,
) (bool, error) {
	store.lastNewerRequest = req
	store.lastAfterSequence = afterSequence
	if store.newerErr != nil {
		return false, store.newerErr
	}
	return store.newerAvailable, nil
}

func (store *fakeSubjectPageStore) MaxVisibleObservedAt(
	_ context.Context,
	req SubjectPageRequest,
) (*time.Time, error) {
	store.lastObservedRequest = req
	if store.maxObservedErr != nil {
		return nil, store.maxObservedErr
	}
	return store.maxObservedAt, nil
}

func (store *fakeSubjectPageStore) LoadSourceStatuses(context.Context, uint64) ([]SourceStatus, error) {
	if store.sourceStatusesErr != nil {
		return nil, store.sourceStatusesErr
	}
	return store.sourceStatuses, nil
}

type fakeLiveSubjectResolver struct {
	header SubjectHeader
	err    error
	calls  int
}

func (resolver *fakeLiveSubjectResolver) ResolveLive(
	context.Context,
	recordauth.ActorScope,
	SubjectRef,
) (SubjectHeader, error) {
	resolver.calls++
	if resolver.err != nil {
		return SubjectHeader{}, resolver.err
	}
	return resolver.header, nil
}

func testListCodec(t *testing.T) *CursorCodec {
	t.Helper()
	codec, err := NewCursorCodec([]byte("houfeng-test-session-hmac-key-material"))
	if err != nil {
		t.Fatalf("build cursor codec: %v", err)
	}
	return codec
}

func testListActor(t *testing.T, role recordauth.Role) recordauth.ActorScope {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID:    "usr_000000000000000000000001",
		Role:      role,
		ProjectID: recordauth.ProjectIDDefault,
	})
	if err != nil {
		t.Fatalf("normalize actor: %v", err)
	}
	return actor
}

func testListService(t *testing.T, head PublishedHead, pages *fakeSubjectPageStore, live *fakeLiveSubjectResolver) *Service {
	t.Helper()
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	service, err := NewServiceWithClock(
		&fakePublishedHeadStore{head: head},
		pages,
		live,
		testListCodec(t),
		func() time.Time { return now },
		DefaultCursorTTL,
	)
	if err != nil {
		t.Fatalf("NewServiceWithClock() error = %v", err)
	}
	return service
}

func testSampleEvent(index int, ingest uint64) Event {
	eventAt := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC).Add(time.Duration(index) * time.Minute)
	recordedAt := eventAt.Add(5 * time.Second)
	return Event{
		ActivityID:     "act_gv2xq4hb7zsm3nkfr6wcyd5ple",
		EventKind:      EventKindRecordCreated,
		EventAt:        eventAt,
		RecordedAt:     recordedAt,
		IngestSequence: ingest,
		SourceKind:     SourceKindRecordDomain,
		Source: SourceIdentity{
			Kind:    SourceKindRecordDomain,
			EventID: "evt_sample",
			Version: 1,
		},
		Subjects:     []SubjectSnapshot{},
		Presentation: Presentation{Version: PresentationVersionV1, Title: "sample"},
	}
}

func TestListSubjectActivityNormalizesQueryDefaults(t *testing.T) {
	pages := &fakeSubjectPageStore{pageResult: SubjectPageResult{Events: []Event{}}}
	live := &fakeLiveSubjectResolver{
		header: SubjectHeader{
			Kind: testSubject().Kind, SourceID: testSubject().SourceID,
			Identity: map[string]string{"name": "edge-1"}, LiveRoute: "/vps/" + testVPSSourceID,
		},
	}
	service := testListService(t, PublishedHead{Generation: 3, PublishedIngestSequence: 900}, pages, live)

	result, err := service.List(context.Background(), ListRequest{
		Actor: testListActor(t, recordauth.RoleProjectAdmin),
		Query: Query{Subject: testSubject()},
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if result.View != ViewActivity {
		t.Fatalf("view = %q, want %q", result.View, ViewActivity)
	}
	if pages.lastPageRequest.Limit != DefaultPageSize+1 {
		t.Fatalf("store limit = %d, want %d", pages.lastPageRequest.Limit, DefaultPageSize+1)
	}
	if pages.lastPageRequest.Query.Versions != VersionsHistory {
		t.Fatalf("versions = %q, want %q", pages.lastPageRequest.Query.Versions, VersionsHistory)
	}
}

func TestListSubjectActivityFixesAsOfFromPublishedHeadOnFirstPage(t *testing.T) {
	pages := &fakeSubjectPageStore{pageResult: SubjectPageResult{Events: []Event{}}}
	live := &fakeLiveSubjectResolver{
		header: SubjectHeader{
			Kind: testSubject().Kind, SourceID: testSubject().SourceID,
			Identity: map[string]string{}, LiveRoute: "/vps/" + testVPSSourceID,
		},
	}
	head := PublishedHead{Generation: 5, PublishedIngestSequence: 1200}
	service := testListService(t, head, pages, live)

	if _, err := service.List(context.Background(), ListRequest{
		Actor: testListActor(t, recordauth.RoleProjectAdmin),
		Query: Query{Subject: testSubject(), View: ViewActivity, Limit: 10},
	}); err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if pages.lastPageRequest.AsOf != head.PublishedIngestSequence {
		t.Fatalf("as-of = %d, want published head %d", pages.lastPageRequest.AsOf, head.PublishedIngestSequence)
	}
	if pages.lastPageRequest.Generation != head.Generation {
		t.Fatalf("generation = %d, want %d", pages.lastPageRequest.Generation, head.Generation)
	}
}

func TestListSubjectActivityCursorPreservesFixedAsOfWhenHeadAdvances(t *testing.T) {
	codec := testListCodec(t)
	actor := testListActor(t, recordauth.RoleProjectAdmin)
	query, err := NormalizeQuery(Query{Subject: testSubject(), View: ViewActivity, Limit: 10})
	if err != nil {
		t.Fatalf("NormalizeQuery() error = %v", err)
	}
	fixedAsOf := uint64(800)
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	token, err := codec.Encode(CursorValues{
		Query: query, Actor: actor, Generation: 4, AsOf: fixedAsOf,
		ExpiresAt: now.Add(DefaultCursorTTL),
		SortKey: SortKey{
			EventAt:    time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC),
			RecordedAt: time.Date(2026, 8, 18, 9, 0, 5, 0, time.UTC),
			SourceKind: SourceKindRecordDomain,
			ActivityID: "act_gv2xq4hb7zsm3nkfr6wcyd5ple",
		},
	})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	pages := &fakeSubjectPageStore{pageResult: SubjectPageResult{Events: []Event{}}}
	live := &fakeLiveSubjectResolver{
		header: SubjectHeader{
			Kind: testSubject().Kind, SourceID: testSubject().SourceID,
			Identity: map[string]string{}, LiveRoute: "/vps/" + testVPSSourceID,
		},
	}
	service, err := NewServiceWithClock(
		&fakePublishedHeadStore{head: PublishedHead{Generation: 4, PublishedIngestSequence: 1500}},
		pages,
		live,
		codec,
		func() time.Time { return now },
		DefaultCursorTTL,
	)
	if err != nil {
		t.Fatalf("NewServiceWithClock() error = %v", err)
	}

	if _, err := service.List(context.Background(), ListRequest{
		Actor: actor, Query: query, Cursor: token,
	}); err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if pages.lastPageRequest.AsOf != fixedAsOf {
		t.Fatalf("as-of = %d, want fixed cursor watermark %d", pages.lastPageRequest.AsOf, fixedAsOf)
	}
	if pages.lastNewerRequest.AsOf != 1500 {
		t.Fatalf("newer check as-of = %d, want current head 1500", pages.lastNewerRequest.AsOf)
	}
}

func TestListSubjectActivityCursorErrors(t *testing.T) {
	actor := testListActor(t, recordauth.RoleProjectAdmin)
	query, err := NormalizeQuery(Query{Subject: testSubject(), View: ViewActivity})
	if err != nil {
		t.Fatalf("NormalizeQuery() error = %v", err)
	}
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	codec := testListCodec(t)

	validToken, err := codec.Encode(CursorValues{
		Query: query, Actor: actor, Generation: 2, AsOf: 100,
		ExpiresAt: now.Add(DefaultCursorTTL),
	})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	tests := []struct {
		name    string
		head    PublishedHead
		cursor  string
		wantErr error
	}{
		{
			name:    "invalid token",
			head:    PublishedHead{Generation: 2, PublishedIngestSequence: 100},
			cursor:  "not-a-valid-token",
			wantErr: ErrCursorInvalid,
		},
		{
			name:    "generation change",
			head:    PublishedHead{Generation: 3, PublishedIngestSequence: 100},
			cursor:  validToken,
			wantErr: ErrCursorExpired,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pages := &fakeSubjectPageStore{}
			live := &fakeLiveSubjectResolver{
				header: SubjectHeader{
					Kind: testSubject().Kind, SourceID: testSubject().SourceID,
					Identity: map[string]string{}, LiveRoute: "/vps/" + testVPSSourceID,
				},
			}
			service, err := NewServiceWithClock(
				&fakePublishedHeadStore{head: tt.head},
				pages, live, codec,
				func() time.Time { return now },
				DefaultCursorTTL,
			)
			if err != nil {
				t.Fatalf("NewServiceWithClock() error = %v", err)
			}
			_, err = service.List(context.Background(), ListRequest{
				Actor: actor, Query: query, Cursor: tt.cursor,
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("List() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestListSubjectActivityLiveSubjectHeader(t *testing.T) {
	pages := &fakeSubjectPageStore{
		pageResult: SubjectPageResult{Events: []Event{testSampleEvent(0, 10)}},
	}
	live := &fakeLiveSubjectResolver{
		header: SubjectHeader{
			Kind: records.SubjectKindVPS, SourceID: testVPSSourceID,
			Identity:  map[string]string{"display_name": "edge-1"},
			LiveRoute: "/vps/" + testVPSSourceID,
		},
	}
	service := testListService(t, PublishedHead{Generation: 1, PublishedIngestSequence: 50}, pages, live)

	result, err := service.List(context.Background(), ListRequest{
		Actor: testListActor(t, recordauth.RoleProjectAdmin),
		Query: Query{Subject: testSubject(), View: ViewActivity, Limit: 10},
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if result.Subject.Status != SubjectStatusLive {
		t.Fatalf("status = %q, want live", result.Subject.Status)
	}
	if result.Subject.LiveRoute != live.header.LiveRoute {
		t.Fatalf("live route = %q, want %q", result.Subject.LiveRoute, live.header.LiveRoute)
	}
	if result.Subject.Identity["display_name"] != "edge-1" {
		t.Fatalf("identity = %#v, want display_name edge-1", result.Subject.Identity)
	}
}

func TestListSubjectActivityTombstoneWhenLiveMissingButSubjectKnown(t *testing.T) {
	pages := &fakeSubjectPageStore{
		pageResult: SubjectPageResult{
			SubjectKnown: true,
			TombstoneIdentity: map[string]string{
				"display_name": "retired-edge",
			},
			Events: []Event{testSampleEvent(0, 10)},
		},
	}
	live := &fakeLiveSubjectResolver{err: ErrSubjectNotFound}
	service := testListService(t, PublishedHead{Generation: 1, PublishedIngestSequence: 50}, pages, live)

	result, err := service.List(context.Background(), ListRequest{
		Actor: testListActor(t, recordauth.RoleProjectAdmin),
		Query: Query{Subject: testSubject(), View: ViewActivity, Limit: 10},
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if result.Subject.Status != SubjectStatusTombstoned {
		t.Fatalf("status = %q, want tombstoned", result.Subject.Status)
	}
	if result.Subject.LiveRoute != "" {
		t.Fatalf("live route = %q, want empty", result.Subject.LiveRoute)
	}
	if result.Subject.Identity["display_name"] != "retired-edge" {
		t.Fatalf("identity = %#v, want retired-edge", result.Subject.Identity)
	}
}

func TestListSubjectNotFoundWhenNeitherLiveNorProjection(t *testing.T) {
	pages := &fakeSubjectPageStore{
		pageResult: SubjectPageResult{SubjectKnown: false, Events: []Event{}},
	}
	live := &fakeLiveSubjectResolver{err: ErrSubjectNotFound}
	service := testListService(t, PublishedHead{Generation: 1, PublishedIngestSequence: 50}, pages, live)

	_, err := service.List(context.Background(), ListRequest{
		Actor: testListActor(t, recordauth.RoleProjectAdmin),
		Query: Query{Subject: testSubject(), View: ViewActivity, Limit: 10},
	})
	if !errors.Is(err, ErrSubjectNotFound) {
		t.Fatalf("List() error = %v, want ErrSubjectNotFound", err)
	}
}

func TestListSubjectActivityProjectionUnavailableWhenHeadMissing(t *testing.T) {
	pages := &fakeSubjectPageStore{}
	live := &fakeLiveSubjectResolver{}
	service, err := NewService(
		&fakePublishedHeadStore{err: ErrProjectionUnavailable},
		pages,
		live,
		testListCodec(t),
	)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	_, err = service.List(context.Background(), ListRequest{
		Actor: testListActor(t, recordauth.RoleProjectAdmin),
		Query: Query{Subject: testSubject(), View: ViewActivity, Limit: 10},
	})
	if !errors.Is(err, ErrProjectionUnavailable) {
		t.Fatalf("List() error = %v, want ErrProjectionUnavailable", err)
	}
}

func TestListSubjectActivityNewItemsAvailableFollowsStoreSignal(t *testing.T) {
	tests := []struct {
		name  string
		newer bool
		want  bool
	}{
		{name: "true when store reports newer", newer: true, want: true},
		{name: "false when store reports none", newer: false, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pages := &fakeSubjectPageStore{
				pageResult:     SubjectPageResult{Events: []Event{}},
				newerAvailable: tt.newer,
			}
			live := &fakeLiveSubjectResolver{
				header: SubjectHeader{
					Kind: testSubject().Kind, SourceID: testSubject().SourceID,
					Identity: map[string]string{}, LiveRoute: "/vps/" + testVPSSourceID,
				},
			}
			head := PublishedHead{Generation: 2, PublishedIngestSequence: 900}
			service := testListService(t, head, pages, live)

			result, err := service.List(context.Background(), ListRequest{
				Actor: testListActor(t, recordauth.RoleProjectAdmin),
				Query: Query{Subject: testSubject(), View: ViewActivity, Limit: 10},
			})
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if result.Freshness.NewItemsAvailable != tt.want {
				t.Fatalf("new_items_available = %v, want %v", result.Freshness.NewItemsAvailable, tt.want)
			}
			if pages.lastAfterSequence != head.PublishedIngestSequence {
				t.Fatalf("after sequence = %d, want page as-of %d", pages.lastAfterSequence, head.PublishedIngestSequence)
			}
		})
	}
}

func TestListSubjectActivityResultJSONNeverNullCollectionsOrDeniedFields(t *testing.T) {
	pages := &fakeSubjectPageStore{pageResult: SubjectPageResult{Events: nil}}
	live := &fakeLiveSubjectResolver{
		header: SubjectHeader{
			Kind: testSubject().Kind, SourceID: testSubject().SourceID,
			Identity: nil, LiveRoute: "/vps/" + testVPSSourceID,
		},
	}
	service := testListService(t, PublishedHead{Generation: 1, PublishedIngestSequence: 10}, pages, live)

	result, err := service.List(context.Background(), ListRequest{
		Actor: testListActor(t, recordauth.RoleProjectAdmin),
		Query: Query{Subject: testSubject(), View: ViewActivity, Limit: 10},
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if result.Items == nil {
		t.Fatal("items slice is nil before marshal")
	}

	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	text := string(payload)
	for _, forbidden := range []string{
		`"items":null`,
		`"source_statuses":null`,
		`"identity":null`,
		"projection_generation",
		"as_of_ingest_sequence",
		"current_ingest_sequence",
		"ingest_sequence",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("json payload contains forbidden %q: %s", forbidden, text)
		}
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	items, ok := decoded["items"].([]any)
	if !ok {
		t.Fatalf("items = %#v, want array", decoded["items"])
	}
	if len(items) != 0 {
		t.Fatalf("items length = %d, want empty array", len(items))
	}
}

func TestListSubjectActivityAuthFilterForAdminAndViewer(t *testing.T) {
	head := PublishedHead{Generation: 1, PublishedIngestSequence: 100}
	live := &fakeLiveSubjectResolver{
		header: SubjectHeader{
			Kind: testSubject().Kind, SourceID: testSubject().SourceID,
			Identity: map[string]string{}, LiveRoute: "/vps/" + testVPSSourceID,
		},
	}

	t.Run("project admin unrestricted", func(t *testing.T) {
		store := &fakeSubjectPageStore{pageResult: SubjectPageResult{Events: []Event{}}}
		service := testListService(t, head, store, live)
		if _, err := service.List(context.Background(), ListRequest{
			Actor: testListActor(t, recordauth.RoleProjectAdmin),
			Query: Query{Subject: testSubject(), View: ViewActivity, Limit: 10},
		}); err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if !store.lastPageRequest.AuthUnrestricted {
			t.Fatal("project admin request was not auth unrestricted")
		}
		if len(store.lastPageRequest.AllowedAuthDigests) != 0 {
			t.Fatalf("allowed digests = %d, want none", len(store.lastPageRequest.AllowedAuthDigests))
		}
	})

	t.Run("viewer uses digest allowlist", func(t *testing.T) {
		store := &fakeSubjectPageStore{pageResult: SubjectPageResult{Events: []Event{}}}
		service := testListService(t, head, store, live)
		if _, err := service.List(context.Background(), ListRequest{
			Actor: testListActor(t, recordauth.RoleViewer),
			Query: Query{Subject: testSubject(), View: ViewActivity, Limit: 10},
		}); err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if store.lastPageRequest.AuthUnrestricted {
			t.Fatal("viewer request was auth unrestricted")
		}
		project, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
			Version: recordauth.VisibilityScopeVersionV1, Kind: recordauth.VisibilityKindProject,
			ProjectID: recordauth.ProjectIDDefault, PolicyVersion: recordauth.PolicyVersionV1, PolicyRevision: 1,
		})
		if err != nil {
			t.Fatalf("NormalizeVisibilityScope(project) error = %v", err)
		}
		want := [][32]byte{
			project.CanonicalHash,
		}
		if !reflect.DeepEqual(store.lastPageRequest.AllowedAuthDigests, want) {
			t.Fatalf("allowed digests = %x, want %x", store.lastPageRequest.AllowedAuthDigests, want)
		}
	})
}

func TestListSubjectActivityFreshnessIgnoresGlobalSourceHealth(t *testing.T) {
	pages := &fakeSubjectPageStore{
		pageResult: SubjectPageResult{Events: []Event{}},
		sourceStatuses: []SourceStatus{{
			SourceKind: SourceKindRecordDomain,
			State:      "unavailable",
			ReasonCode: "projector_stalled",
		}},
	}
	live := &fakeLiveSubjectResolver{
		header: SubjectHeader{
			Kind: testSubject().Kind, SourceID: testSubject().SourceID,
			Identity: map[string]string{}, LiveRoute: "/vps/" + testVPSSourceID,
		},
	}
	service := testListService(t, PublishedHead{Generation: 1, PublishedIngestSequence: 10}, pages, live)

	result, err := service.List(context.Background(), ListRequest{
		Actor: testListActor(t, recordauth.RoleViewer),
		Query: Query{Subject: testSubject(), View: ViewActivity, Limit: 10},
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if result.Freshness.State != "ready" {
		t.Fatalf("freshness state = %q, want ready (hidden-scope projector health must not flip it)", result.Freshness.State)
	}
	if result.Freshness.ReasonCode != "" {
		t.Fatalf("reason = %q, want empty", result.Freshness.ReasonCode)
	}
	if len(result.SourceStatuses) != 1 || result.SourceStatuses[0].State != "unavailable" {
		t.Fatalf("source_statuses should still expose diagnostics: %#v", result.SourceStatuses)
	}
}

func TestListSubjectActivityTrimsPageAndEncodesNextCursor(t *testing.T) {
	events := []Event{
		testSampleEvent(0, 1),
		testSampleEvent(1, 2),
		testSampleEvent(2, 3),
	}
	pages := &fakeSubjectPageStore{
		pageResult: SubjectPageResult{Events: events},
	}
	live := &fakeLiveSubjectResolver{
		header: SubjectHeader{
			Kind: testSubject().Kind, SourceID: testSubject().SourceID,
			Identity: map[string]string{}, LiveRoute: "/vps/" + testVPSSourceID,
		},
	}
	service := testListService(t, PublishedHead{Generation: 6, PublishedIngestSequence: 300}, pages, live)

	result, err := service.List(context.Background(), ListRequest{
		Actor: testListActor(t, recordauth.RoleProjectAdmin),
		Query: Query{Subject: testSubject(), View: ViewActivity, Limit: 2},
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(result.Items) != 2 {
		t.Fatalf("items length = %d, want 2", len(result.Items))
	}
	if result.NextCursor == "" {
		t.Fatal("next cursor is empty, want token")
	}
	if result.SnapshotCursor == "" {
		t.Fatal("snapshot cursor is empty, want watermark token")
	}
}
