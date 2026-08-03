package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store"
)

func TestRecordDraftsHandlerListsOnlyTrustedActorsPrivateDrafts(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	application := &recordDraftHandlerApplicationStub{
		listDrafts: func(_ context.Context, request records.DraftListRequest) ([]records.Draft, error) {
			if !reflect.DeepEqual(request.Actor, actor) {
				t.Fatalf("ListDrafts() actor = %#v, want trusted context actor %#v", request.Actor, actor)
			}
			if request.Limit != 25 {
				t.Fatalf("ListDrafts() request = %#v", request)
			}
			return make([]records.Draft, 0), nil
		},
	}
	handler := RecordDraftsWithOptions(application, RecordDraftHandlerOptions{
		NewDraftID: func() (string, error) { return "rdf_httpcontract", nil },
	})
	request := httptest.NewRequest(http.MethodGet, "/api/record-drafts?limit=25", nil)
	request = request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var response struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Items == nil || len(response.Items) != 0 {
		t.Fatalf("items = %#v, want non-nil empty array", response.Items)
	}
	if reflect.TypeOf(recordDraftResponse{}).PkgPath() != reflect.TypeOf(recordDraftListResponse{}).PkgPath() {
		t.Fatal("draft response is not handler-owned")
	}
}

func TestRecordDraftsHandlerCreatesReadsPatchesAndDiscardsPrivateDraft(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	base := mustRecordsHandlerDraft(t, actor, "", "")
	application := &recordDraftHandlerApplicationStub{
		createDraft: func(_ context.Context, request records.DraftCreateRequest) (records.Draft, error) {
			if request.DraftID != base.DraftID || request.RecordID != "" || request.BaseRevisionID != "" ||
				request.Payload.Hash() != base.Payload.Hash() || !reflect.DeepEqual(request.Actor, actor) {
				t.Fatalf("CreateDraft() request = %#v", request)
			}
			return base, nil
		},
		readDraft: func(_ context.Context, request records.DraftReadRequest) (records.Draft, error) {
			if request.DraftID != base.DraftID || !reflect.DeepEqual(request.Actor, actor) {
				t.Fatalf("ReadDraft() request = %#v", request)
			}
			return base, nil
		},
		patchDraft: func(_ context.Context, request records.DraftPatchRequest) (records.Draft, error) {
			if request.DraftID != base.DraftID || request.IfMatch != base.ETag ||
				request.Payload.Hash() != base.Payload.Hash() || !reflect.DeepEqual(request.Actor, actor) {
				t.Fatalf("PatchDraft() request = %#v", request)
			}
			return base, nil
		},
		discardDraft: func(_ context.Context, request records.DraftDiscardRequest) error {
			if request.DraftID != base.DraftID || !reflect.DeepEqual(request.Actor, actor) {
				t.Fatalf("DiscardDraft() request = %#v", request)
			}
			return nil
		},
	}
	handler := RecordDraftsWithOptions(application, RecordDraftHandlerOptions{
		NewDraftID: func() (string, error) { return base.DraftID, nil },
	})
	payload := string(base.Payload.JSON())

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		ifMatch    string
		wantStatus int
		wantETag   bool
	}{
		{name: "create", method: http.MethodPost, path: "/api/record-drafts", body: `{"payload":` + payload + `}`, wantStatus: http.StatusCreated, wantETag: true},
		{name: "read", method: http.MethodGet, path: "/api/record-drafts/" + base.DraftID, wantStatus: http.StatusOK, wantETag: true},
		{name: "patch", method: http.MethodPatch, path: "/api/record-drafts/" + base.DraftID, body: `{"payload":` + payload + `}`, ifMatch: base.ETag.String(), wantStatus: http.StatusOK, wantETag: true},
		{name: "discard", method: http.MethodDelete, path: "/api/record-drafts/" + base.DraftID, wantStatus: http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := recordsHandlerRequest(t, actor, tt.method, tt.path, tt.body)
			if tt.ifMatch != "" {
				request.Header.Set("If-Match", tt.ifMatch)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
			if tt.wantETag && recorder.Header().Get("ETag") != base.ETag.String() {
				t.Fatalf("ETag = %q, want %q", recorder.Header().Get("ETag"), base.ETag.String())
			}
			if tt.wantETag {
				body := recorder.Body.String()
				if strings.Contains(body, "project_id") || strings.Contains(body, "author_id") ||
					strings.Contains(body, "payload_hash") || strings.Contains(body, "etag_digest") {
					t.Fatalf("draft response leaked internal authority: %s", body)
				}
			}
		})
	}
}

func TestRecordDraftsHandlerMapsConflictsWithAllowlistedRecovery(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	server := mustRecordsHandlerDraft(t, actor, "rec_httpcontract", "rrv_httpbase")
	localPayload, err := records.NewDraftPayload([]byte(`{
		"title":"local edit",
		"body_markdown":"# Local edit",
		"markdown_dialect_version":1,
		"record_type":"note",
		"business_status":"",
		"impact_level":"high",
		"visibility":{"kind":"project","allowed_roles":[],"allowed_group_ids":[]},
		"subjects":[{"registry_version":1,"kind":"vps","role":"affected","source_id":"vps_0123456789abcdef","primary":true}],
		"tags":[],
		"owner_id":"",
		"participant_ids":[],
		"save_reason":"local edit"
	}`))
	if err != nil {
		t.Fatalf("NewDraftPayload() error = %v", err)
	}
	application := &recordDraftHandlerApplicationStub{
		patchDraft: func(_ context.Context, request records.DraftPatchRequest) (records.Draft, error) {
			return records.Draft{}, &records.DraftConflictError{Server: server, LocalPayload: request.Payload}
		},
	}
	handler := RecordDraftsWithOptions(application, RecordDraftHandlerOptions{
		NewDraftID: func() (string, error) { return "rdf_unused", nil },
	})
	request := recordsHandlerRequest(
		t,
		actor,
		http.MethodPatch,
		"/api/record-drafts/"+server.DraftID,
		`{"payload":`+string(localPayload.JSON())+`}`,
	)
	request.Header.Set("If-Match", server.ETag.String())
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	assertRecordsHandlerError(t, recorder, http.StatusConflict, "draft_conflict")
	var response struct {
		Recovery struct {
			ServerDraft struct {
				DraftID string `json:"draft_id"`
			} `json:"server_draft"`
			LocalPayload json.RawMessage `json:"local_payload"`
		} `json:"recovery"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode conflict response: %v", err)
	}
	if response.Recovery.ServerDraft.DraftID != server.DraftID || len(response.Recovery.LocalPayload) == 0 ||
		strings.Contains(recorder.Body.String(), "project_id") || strings.Contains(recorder.Body.String(), "author_id") {
		t.Fatalf("conflict recovery = %#v; body=%s", response.Recovery, recorder.Body.String())
	}
}

func TestRecordDraftsHandlerRejectsUntrustedPayloadAndMapsNoLeakErrors(t *testing.T) {
	actor := mustRecordsHandlerActor(t)

	for _, tt := range []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "denied", err: recordauth.ErrDenied, wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "not found", err: records.ErrDraftNotFound, wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "reserved", err: records.ErrRecordDeletionReserved, wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "base advanced", err: records.ErrDraftRevisionConflict, wantStatus: http.StatusConflict, wantCode: "record_revision_conflict"},
		{name: "unavailable", err: store.ErrRecordPlatformAdmissionUnavailable, wantStatus: http.StatusServiceUnavailable, wantCode: "record_service_unavailable"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			handler := RecordDraftsWithOptions(&recordDraftHandlerApplicationStub{
				readDraft: func(context.Context, records.DraftReadRequest) (records.Draft, error) {
					return records.Draft{}, tt.err
				},
			}, RecordDraftHandlerOptions{NewDraftID: func() (string, error) { return "rdf_unused", nil }})
			request := recordsHandlerRequest(t, actor, http.MethodGet, "/api/record-drafts/rdf_httpcontract", "")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			assertRecordsHandlerError(t, recorder, tt.wantStatus, tt.wantCode)
		})
	}

	t.Run("nested trusted field", func(t *testing.T) {
		calls := 0
		handler := RecordDraftsWithOptions(&recordDraftHandlerApplicationStub{
			createDraft: func(context.Context, records.DraftCreateRequest) (records.Draft, error) {
				calls++
				return records.Draft{}, nil
			},
		}, RecordDraftHandlerOptions{NewDraftID: func() (string, error) { return "rdf_unused", nil }})
		request := recordsHandlerRequest(t, actor, http.MethodPost, "/api/record-drafts", `{"payload":{"title":"draft","trusted_project":"other"}}`)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		assertRecordsHandlerError(t, recorder, http.StatusBadRequest, "invalid_json")
		if calls != 0 {
			t.Fatalf("CreateDraft() calls = %d, want 0", calls)
		}
	})

	t.Run("null payload arrays", func(t *testing.T) {
		for _, tt := range []struct {
			name    string
			payload string
		}{
			{name: "allowed roles", payload: `{"title":"draft","visibility":{"kind":"project","allowed_roles":null,"allowed_group_ids":[]},"subjects":[],"tags":[],"participant_ids":[]}`},
			{name: "allowed group IDs", payload: `{"title":"draft","visibility":{"kind":"project","allowed_roles":[],"allowed_group_ids":null},"subjects":[],"tags":[],"participant_ids":[]}`},
			{name: "subjects", payload: `{"title":"draft","visibility":{"kind":"project","allowed_roles":[],"allowed_group_ids":[]},"subjects":null,"tags":[],"participant_ids":[]}`},
			{name: "tags", payload: `{"title":"draft","visibility":{"kind":"project","allowed_roles":[],"allowed_group_ids":[]},"subjects":[],"tags":null,"participant_ids":[]}`},
			{name: "participant IDs", payload: `{"title":"draft","visibility":{"kind":"project","allowed_roles":[],"allowed_group_ids":[]},"subjects":[],"tags":[],"participant_ids":null}`},
		} {
			t.Run(tt.name, func(t *testing.T) {
				calls := 0
				handler := RecordDraftsWithOptions(&recordDraftHandlerApplicationStub{
					createDraft: func(context.Context, records.DraftCreateRequest) (records.Draft, error) {
						calls++
						return records.Draft{}, nil
					},
				}, RecordDraftHandlerOptions{NewDraftID: func() (string, error) { return "rdf_unused", nil }})
				request := recordsHandlerRequest(t, actor, http.MethodPost, "/api/record-drafts", `{"payload":`+tt.payload+`}`)
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, request)
				assertRecordsHandlerError(t, recorder, http.StatusBadRequest, "invalid_json")
				if calls != 0 {
					t.Fatalf("CreateDraft() calls = %d, want 0", calls)
				}
			})
		}
	})

	t.Run("oversized", func(t *testing.T) {
		handler := RecordDraftsWithOptions(&recordDraftHandlerApplicationStub{}, RecordDraftHandlerOptions{
			NewDraftID: func() (string, error) { return "rdf_unused", nil },
		})
		request := recordsHandlerRequest(t, actor, http.MethodPost, "/api/record-drafts", `{"payload":{"title":"`+strings.Repeat("x", DefaultJSONBodyLimit)+`"}}`)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		assertRecordsHandlerError(t, recorder, http.StatusRequestEntityTooLarge, "request_too_large")
	})

	t.Run("missing actor", func(t *testing.T) {
		handler := RecordDraftsWithOptions(&recordDraftHandlerApplicationStub{}, RecordDraftHandlerOptions{
			NewDraftID: func() (string, error) { return "rdf_unused", nil },
		})
		request := httptest.NewRequest(http.MethodGet, "/api/record-drafts", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		assertRecordsHandlerError(t, recorder, http.StatusServiceUnavailable, "authorization_unavailable")
	})
}

func TestRecordDraftResponsePreservesCanonicalPayloadAndUTCMetadata(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	draft := mustRecordsHandlerDraft(t, actor, "", "")
	response, err := newRecordDraftResponse(draft)
	if err != nil {
		t.Fatalf("newRecordDraftResponse() error = %v", err)
	}
	if response.DraftID != draft.DraftID || !reflect.DeepEqual([]byte(response.Payload), draft.Payload.JSON()) ||
		response.UpdatedAt.Location() != time.UTC || response.ExpiresAt.Location() != time.UTC {
		t.Fatalf("newRecordDraftResponse() = %#v", response)
	}
}

func TestRecordDraftHandlersFailClosedOnNonAllowlistedPersistedPayloads(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	valid := mustRecordsHandlerDraft(t, actor, "rec_httpcontract", "rrv_httpbase")
	unknown := recordsHandlerDraftWithRawPayload(t, valid, `{"secret_internal":"must-not-leak"}`)
	patchBody := `{"payload":` + string(valid.Payload.JSON()) + `}`

	tests := []struct {
		name    string
		handler http.Handler
		request *http.Request
	}{
		{
			name: "read response",
			handler: RecordDraftsWithOptions(&recordDraftHandlerApplicationStub{
				readDraft: func(context.Context, records.DraftReadRequest) (records.Draft, error) {
					return unknown, nil
				},
			}, RecordDraftHandlerOptions{NewDraftID: func() (string, error) { return "rdf_unused", nil }}),
			request: recordsHandlerRequest(t, actor, http.MethodGet, "/api/record-drafts/"+valid.DraftID, ""),
		},
		{
			name: "list response",
			handler: RecordDraftsWithOptions(&recordDraftHandlerApplicationStub{
				listDrafts: func(context.Context, records.DraftListRequest) ([]records.Draft, error) {
					return []records.Draft{unknown}, nil
				},
			}, RecordDraftHandlerOptions{NewDraftID: func() (string, error) { return "rdf_unused", nil }}),
			request: recordsHandlerRequest(t, actor, http.MethodGet, "/api/record-drafts", ""),
		},
		{
			name: "conflict server payload",
			handler: RecordDraftsWithOptions(&recordDraftHandlerApplicationStub{
				patchDraft: func(context.Context, records.DraftPatchRequest) (records.Draft, error) {
					return records.Draft{}, &records.DraftConflictError{Server: unknown, LocalPayload: valid.Payload}
				},
			}, RecordDraftHandlerOptions{NewDraftID: func() (string, error) { return "rdf_unused", nil }}),
			request: recordsHandlerRequest(t, actor, http.MethodPatch, "/api/record-drafts/"+valid.DraftID, patchBody),
		},
		{
			name: "conflict local payload",
			handler: RecordDraftsWithOptions(&recordDraftHandlerApplicationStub{
				patchDraft: func(context.Context, records.DraftPatchRequest) (records.Draft, error) {
					return records.Draft{}, &records.DraftConflictError{Server: valid, LocalPayload: unknown.Payload}
				},
			}, RecordDraftHandlerOptions{NewDraftID: func() (string, error) { return "rdf_unused", nil }}),
			request: recordsHandlerRequest(t, actor, http.MethodPatch, "/api/record-drafts/"+valid.DraftID, patchBody),
		},
		{
			name: "revision conflict draft payload",
			handler: RecordDraftsWithOptions(&recordDraftHandlerApplicationStub{
				readDraft: func(context.Context, records.DraftReadRequest) (records.Draft, error) {
					return records.Draft{}, &records.DraftRevisionConflictError{
						ServerRevisionID: "rrv_httpserver", ServerLockVersion: 8,
						ServerAuthorizationEpoch: 6, Draft: unknown,
					}
				},
			}, RecordDraftHandlerOptions{NewDraftID: func() (string, error) { return "rdf_unused", nil }}),
			request: recordsHandlerRequest(t, actor, http.MethodGet, "/api/record-drafts/"+valid.DraftID, ""),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.request.Method == http.MethodPatch {
				test.request.Header.Set("If-Match", valid.ETag.String())
			}
			recorder := httptest.NewRecorder()
			test.handler.ServeHTTP(recorder, test.request)
			assertRecordsHandlerError(t, recorder, http.StatusInternalServerError, "internal_error")
			if strings.Contains(recorder.Body.String(), "secret_internal") || strings.Contains(recorder.Body.String(), "must-not-leak") {
				t.Fatalf("response leaked non-allowlisted payload: %s", recorder.Body.String())
			}
		})
	}
}

func TestRecordDraftsHandlerMapsBaseRevisionConflictWithAllowlistedRecovery(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	draft := mustRecordsHandlerDraft(t, actor, "rec_httpcontract", "rrv_httpbase")
	handler := RecordDraftsWithOptions(&recordDraftHandlerApplicationStub{
		readDraft: func(context.Context, records.DraftReadRequest) (records.Draft, error) {
			return records.Draft{}, &records.DraftRevisionConflictError{
				ServerRevisionID:         "rrv_httpserver",
				ServerLockVersion:        8,
				ServerAuthorizationEpoch: 6,
				Draft:                    draft,
			}
		},
	}, RecordDraftHandlerOptions{NewDraftID: func() (string, error) { return "rdf_unused", nil }})
	request := recordsHandlerRequest(t, actor, http.MethodGet, "/api/record-drafts/"+draft.DraftID, "")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	assertRecordsHandlerError(t, recorder, http.StatusConflict, "record_revision_conflict")
	var response struct {
		Recovery struct {
			ServerRevisionID         string `json:"server_revision_id"`
			ServerLockVersion        uint64 `json:"server_lock_version"`
			ServerAuthorizationEpoch uint64 `json:"server_authorization_epoch"`
			Draft                    struct {
				DraftID string `json:"draft_id"`
			} `json:"draft"`
		} `json:"recovery"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode conflict response: %v", err)
	}
	if response.Recovery.ServerRevisionID != "rrv_httpserver" || response.Recovery.ServerLockVersion != 8 ||
		response.Recovery.ServerAuthorizationEpoch != 6 || response.Recovery.Draft.DraftID != draft.DraftID ||
		strings.Contains(recorder.Body.String(), "project_id") || strings.Contains(recorder.Body.String(), "author_id") {
		t.Fatalf("conflict recovery = %#v; body=%s", response.Recovery, recorder.Body.String())
	}
}

func TestRecordDraftsHandlerRequiresExactIfMatchAndImmutableRouting(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	draft := mustRecordsHandlerDraft(t, actor, "", "")
	patchCalls := 0
	handler := RecordDraftsWithOptions(&recordDraftHandlerApplicationStub{
		patchDraft: func(context.Context, records.DraftPatchRequest) (records.Draft, error) {
			patchCalls++
			return draft, nil
		},
	}, RecordDraftHandlerOptions{NewDraftID: func() (string, error) { return "rdf_unused", nil }})
	payload := string(draft.Payload.JSON())

	for _, tt := range []struct {
		name      string
		ifMatches []string
		body      string
	}{
		{name: "missing", body: `{"payload":` + payload + `}`},
		{name: "malformed", ifMatches: []string{`draft-v1-bad`}, body: `{"payload":` + payload + `}`},
		{name: "multiple", ifMatches: []string{draft.ETag.String(), draft.ETag.String()}, body: `{"payload":` + payload + `}`},
		{name: "routing mutation", ifMatches: []string{draft.ETag.String()}, body: `{"record_id":"rec_httpcontract","base_revision_id":"rrv_httpbase","payload":` + payload + `}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := recordsHandlerRequest(t, actor, http.MethodPatch, "/api/record-drafts/"+draft.DraftID, tt.body)
			for _, value := range tt.ifMatches {
				request.Header.Add("If-Match", value)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			assertRecordsHandlerError(t, recorder, http.StatusBadRequest, "invalid_request")
		})
	}
	if patchCalls != 0 {
		t.Fatalf("PatchDraft() calls = %d, want 0", patchCalls)
	}
}

func TestRecordDraftsHandlerUsesExactSubtreesAndMethodBoundaries(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	handler := RecordDraftsWithOptions(&recordDraftHandlerApplicationStub{}, RecordDraftHandlerOptions{
		NewDraftID: func() (string, error) { return "rdf_unused", nil },
	})
	for _, tt := range []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantCode   string
	}{
		{name: "unknown subtree", method: http.MethodGet, path: "/api/record-drafts/rdf_httpcontract/unknown", wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "trailing slash", method: http.MethodGet, path: "/api/record-drafts/rdf_httpcontract/", wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "unsupported collection method", method: http.MethodPut, path: "/api/record-drafts", wantStatus: http.StatusMethodNotAllowed, wantCode: "method_not_allowed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := recordsHandlerRequest(t, actor, tt.method, tt.path, "")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			assertRecordsHandlerError(t, recorder, tt.wantStatus, tt.wantCode)
		})
	}
}

func recordsHandlerDraftWithRawPayload(t *testing.T, draft records.Draft, raw string) records.Draft {
	t.Helper()
	payload, err := records.NewDraftPayload([]byte(raw))
	if err != nil {
		t.Fatalf("NewDraftPayload() error = %v", err)
	}
	etag, err := records.NewDraftETag(draft.DraftID, draft.AuthorID, draft.Version, payload)
	if err != nil {
		t.Fatalf("NewDraftETag() error = %v", err)
	}
	draft.Payload = payload
	draft.ETag = etag
	return draft
}

type recordDraftHandlerApplicationStub struct {
	readDraft      func(context.Context, records.DraftReadRequest) (records.Draft, error)
	listDrafts     func(context.Context, records.DraftListRequest) ([]records.Draft, error)
	createDraft    func(context.Context, records.DraftCreateRequest) (records.Draft, error)
	patchDraft     func(context.Context, records.DraftPatchRequest) (records.Draft, error)
	discardDraft   func(context.Context, records.DraftDiscardRequest) error
	preparePublish func(context.Context, records.DraftPublishRequest) (records.Draft, error)
}

func (stub *recordDraftHandlerApplicationStub) ReadDraft(ctx context.Context, request records.DraftReadRequest) (records.Draft, error) {
	if stub.readDraft == nil {
		return records.Draft{}, errors.New("unexpected ReadDraft call")
	}
	return stub.readDraft(ctx, request)
}

func (stub *recordDraftHandlerApplicationStub) ListDrafts(ctx context.Context, request records.DraftListRequest) ([]records.Draft, error) {
	if stub.listDrafts == nil {
		return nil, errors.New("unexpected ListDrafts call")
	}
	return stub.listDrafts(ctx, request)
}

func (stub *recordDraftHandlerApplicationStub) CreateDraft(ctx context.Context, request records.DraftCreateRequest) (records.Draft, error) {
	if stub.createDraft == nil {
		return records.Draft{}, errors.New("unexpected CreateDraft call")
	}
	return stub.createDraft(ctx, request)
}

func (stub *recordDraftHandlerApplicationStub) PatchDraft(ctx context.Context, request records.DraftPatchRequest) (records.Draft, error) {
	if stub.patchDraft == nil {
		return records.Draft{}, errors.New("unexpected PatchDraft call")
	}
	return stub.patchDraft(ctx, request)
}

func (stub *recordDraftHandlerApplicationStub) DiscardDraft(ctx context.Context, request records.DraftDiscardRequest) error {
	if stub.discardDraft == nil {
		return errors.New("unexpected DiscardDraft call")
	}
	return stub.discardDraft(ctx, request)
}

func (stub *recordDraftHandlerApplicationStub) PreparePublish(ctx context.Context, request records.DraftPublishRequest) (records.Draft, error) {
	if stub.preparePublish == nil {
		return records.Draft{}, errors.New("unexpected PreparePublish call")
	}
	return stub.preparePublish(ctx, request)
}
