package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/http/sessionctx"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordplatform"
	"houfeng/internal/center/records"
	"houfeng/internal/center/store"
)

func TestRecordsHandlerPreparesOrderedEvidenceBeforeCreate(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	draft := mustRecordsHandlerDraft(t, actor, "", "")
	preparer := &recordEvidencePreparerStub{t: t}
	application := &recordsHandlerApplicationStub{
		preparePublish: func(context.Context, records.DraftPublishRequest) (records.Draft, error) {
			return draft, nil
		},
		createRecord: func(_ context.Context, request records.RecordCreateRequest) (records.RevisionCommitResult, error) {
			if request.RecordID != "rec_previewowned" || request.EvidencePreparation.ValidateForRecord(request.RecordID) != nil {
				t.Fatalf("CreateRecord evidence request = %#v", request)
			}
			if got, want := request.EvidencePreparation.SnapshotIDs(), []string{"evs_prepareda", "evs_httpcontract"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("CreateRecord evidence order = %#v, want %#v", got, want)
			}
			return records.RevisionCommitResult{
				RecordID: request.RecordID, RevisionID: "rrv_httpcontract", RevisionNo: 1,
				LockVersion: 1, AuthorizationEpoch: 1, Lifecycle: records.LifecycleActive,
				Created: true, CommittedAt: time.Date(2026, time.August, 16, 3, 0, 0, 0, time.UTC),
			}, nil
		},
	}
	handler := RecordsWithOptions(application, RecordHandlerOptions{
		NewRecordID:      func() (string, error) { return "rec_shouldnotreplace", nil },
		EvidencePreparer: preparer,
	})
	body := `{"record_id":"rec_previewowned","draft_id":"` + draft.DraftID + `","draft_etag":` + strconvQuote(draft.ETag.String()) + `,"evidence_items":[{"capture_intent_id":"evi_0123456789abcdef01234567"},{"existing_snapshot_id":"evs_httpcontract"}]}`
	request := recordsHandlerRequest(t, actor, http.MethodPost, "/api/records", body)
	request.Header.Set("Idempotency-Key", "create-http-evidence")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s, want 201", recorder.Code, recorder.Body.String())
	}
	if preparer.calls != 1 || preparer.request.RecordID != "rec_previewowned" ||
		!reflect.DeepEqual(preparer.request.Items, []evidence.RevisionPreparationItem{
			{CaptureIntentID: "evi_0123456789abcdef01234567"},
			{ExistingSnapshotID: "evs_httpcontract"},
		}) {
		t.Fatalf("Prepare request/calls = %#v/%d", preparer.request, preparer.calls)
	}
}

func TestRecordsHandlerPreparesEvidenceBeforeReviseAndReauthorizesHistoricalEvidenceBeforeRestore(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	draft := mustRecordsHandlerDraft(t, actor, "rec_httpcontract", "rrv_httpbase")

	t.Run("revise", func(t *testing.T) {
		preparer := &recordEvidencePreparerStub{t: t}
		application := &recordsHandlerApplicationStub{
			preparePublish: func(context.Context, records.DraftPublishRequest) (records.Draft, error) {
				return draft, nil
			},
			createRevision: func(_ context.Context, request records.RecordRevisionCreateRequest) (records.RevisionCommitResult, error) {
				if request.RecordID != "rec_httpcontract" ||
					request.EvidencePreparation.ValidateForRecord(request.RecordID) != nil {
					t.Fatalf("CreateRevision evidence request = %#v", request)
				}
				if got, want := request.EvidencePreparation.SnapshotIDs(), []string{"evs_prepareda"}; !reflect.DeepEqual(got, want) {
					t.Fatalf("CreateRevision evidence order = %#v, want %#v", got, want)
				}
				return records.RevisionCommitResult{RecordID: request.RecordID, RevisionID: "rrv_httpnext", Created: true}, nil
			},
		}
		handler := RecordsWithOptions(application, RecordHandlerOptions{EvidencePreparer: preparer})
		body := `{"base_revision_id":"rrv_httpbase","lock_version":7,"authorization_epoch":5,"draft_id":"` +
			draft.DraftID + `","draft_etag":` + strconvQuote(draft.ETag.String()) +
			`,"evidence_items":[{"capture_intent_id":"evi_0123456789abcdef01234567"}]}`
		request := recordsHandlerRequest(t, actor, http.MethodPost, "/api/records/rec_httpcontract/revisions", body)
		request.Header.Set("Idempotency-Key", "revise-http-evidence")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("status = %d body=%s, want 201", recorder.Code, recorder.Body.String())
		}
		if preparer.calls != 1 || !reflect.DeepEqual(preparer.request.Items, []evidence.RevisionPreparationItem{{
			CaptureIntentID: "evi_0123456789abcdef01234567",
		}}) {
			t.Fatalf("revise Prepare request/calls = %#v/%d", preparer.request, preparer.calls)
		}
	})

	t.Run("restore", func(t *testing.T) {
		preparer := &recordEvidencePreparerStub{t: t}
		historical := mustRecordsHandlerRecord(t, actor).Current
		application := &recordsHandlerApplicationStub{
			getRevision: func(context.Context, records.RecordRevisionGetRequest) (records.RecordRevision, error) {
				return historical, nil
			},
			restoreRevision: func(_ context.Context, request records.RecordRestoreRequest) (records.RevisionCommitResult, error) {
				if request.RecordID != historical.RecordID || request.RevisionID != historical.RevisionID ||
					request.EvidencePreparation.ValidateForRecord(request.RecordID) != nil {
					t.Fatalf("RestoreRevision evidence request = %#v", request)
				}
				if got, want := request.EvidencePreparation.SnapshotIDs(), []string{"evs_httpcontract"}; !reflect.DeepEqual(got, want) {
					t.Fatalf("RestoreRevision evidence order = %#v, want %#v", got, want)
				}
				return records.RevisionCommitResult{RecordID: request.RecordID, RevisionID: "rrv_httprestored", Created: true}, nil
			},
		}
		handler := RecordsWithOptions(application, RecordHandlerOptions{EvidencePreparer: preparer})
		request := recordsHandlerRequest(
			t, actor, http.MethodPost,
			"/api/records/"+historical.RecordID+"/revisions/"+historical.RevisionID+"/restore",
			`{"save_reason":"restore evidence"}`,
		)
		request.Header.Set("Idempotency-Key", "restore-http-evidence")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("status = %d body=%s, want 201", recorder.Code, recorder.Body.String())
		}
		if preparer.calls != 1 || !reflect.DeepEqual(preparer.request.Items, []evidence.RevisionPreparationItem{{
			ExistingSnapshotID: "evs_httpcontract",
		}}) {
			t.Fatalf("restore Prepare request/calls = %#v/%d", preparer.request, preparer.calls)
		}
	})
}

func TestRecordsHandlerEvidenceSaveFailsClosedWhenPreparerUnavailableOrStale(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	draft := mustRecordsHandlerDraft(t, actor, "", "")
	for _, test := range []struct {
		name       string
		preparer   recordEvidencePreparerTestContract
		wantStatus int
		wantCode   string
	}{
		{name: "unavailable", wantStatus: http.StatusServiceUnavailable, wantCode: "record_service_unavailable"},
		{name: "stale", preparer: &recordEvidencePreparerStub{err: evidence.ErrPreviewStale}, wantStatus: http.StatusConflict, wantCode: "evidence_preview_stale"},
	} {
		t.Run(test.name, func(t *testing.T) {
			createCalls := 0
			application := &recordsHandlerApplicationStub{
				preparePublish: func(context.Context, records.DraftPublishRequest) (records.Draft, error) { return draft, nil },
				createRecord: func(context.Context, records.RecordCreateRequest) (records.RevisionCommitResult, error) {
					createCalls++
					return records.RevisionCommitResult{}, nil
				},
			}
			handler := RecordsWithOptions(application, RecordHandlerOptions{EvidencePreparer: test.preparer})
			body := `{"record_id":"rec_previewowned","draft_id":"` + draft.DraftID + `","draft_etag":` + strconvQuote(draft.ETag.String()) + `,"evidence_items":[{"capture_intent_id":"evi_0123456789abcdef01234567"}]}`
			request := recordsHandlerRequest(t, actor, http.MethodPost, "/api/records", body)
			request.Header.Set("Idempotency-Key", "create-http-evidence")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != test.wantStatus || !strings.Contains(recorder.Body.String(), `"code":"`+test.wantCode+`"`) {
				t.Fatalf("status/body = %d %s, want %d/%s", recorder.Code, recorder.Body.String(), test.wantStatus, test.wantCode)
			}
			if createCalls != 0 {
				t.Fatalf("CreateRecord calls = %d, want 0", createCalls)
			}
		})
	}
}

func TestRecordsHandlerRejectsPreparationThatDropsRequestedEvidence(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	draft := mustRecordsHandlerDraft(t, actor, "", "")
	createCalls := 0
	handler := RecordsWithOptions(&recordsHandlerApplicationStub{
		preparePublish: func(context.Context, records.DraftPublishRequest) (records.Draft, error) { return draft, nil },
		createRecord: func(context.Context, records.RecordCreateRequest) (records.RevisionCommitResult, error) {
			createCalls++
			return records.RevisionCommitResult{Created: true}, nil
		},
	}, RecordHandlerOptions{EvidencePreparer: &recordEvidencePreparerStub{}})
	body := `{"record_id":"rec_previewowned","draft_id":"` + draft.DraftID + `","draft_etag":` +
		strconvQuote(draft.ETag.String()) +
		`,"evidence_items":[{"capture_intent_id":"evi_0123456789abcdef01234567"}]}`
	request := recordsHandlerRequest(t, actor, http.MethodPost, "/api/records", body)
	request.Header.Set("Idempotency-Key", "create-http-dropped-evidence")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assertRecordsHandlerError(t, recorder, http.StatusInternalServerError, "internal_error")
	if createCalls != 0 {
		t.Fatalf("CreateRecord calls = %d, want 0", createCalls)
	}
}

type recordEvidencePreparerTestContract interface {
	Prepare(context.Context, evidence.ActorScope, evidence.RevisionPreparationRequest) (evidence.RevisionPreparation, error)
}

type recordEvidencePreparerStub struct {
	t       *testing.T
	calls   int
	request evidence.RevisionPreparationRequest
	err     error
}

func (stub *recordEvidencePreparerStub) Prepare(
	_ context.Context,
	actor evidence.ActorScope,
	request evidence.RevisionPreparationRequest,
) (evidence.RevisionPreparation, error) {
	stub.calls++
	stub.request = request
	if stub.err != nil {
		return evidence.RevisionPreparation{}, stub.err
	}
	if stub.t != nil {
		return mustRecordEvidenceTestPreparation(stub.t, actor, request), nil
	}
	return evidence.NewRevisionPreparation(request.RecordID, evidence.RevisionPreparationValues{})
}

func mustRecordEvidenceTestPreparation(
	t *testing.T,
	actor evidence.ActorScope,
	request evidence.RevisionPreparationRequest,
) evidence.RevisionPreparation {
	t.Helper()
	authorization := mustRecordsHandlerRecord(t, actor).Current.Input.Subjects()[0].CaptureAuthorization
	references := make([]evidence.PreparedReference, 0, len(request.Items))
	ordered := make([]string, 0, len(request.Items))
	for index, item := range request.Items {
		snapshotID := item.ExistingSnapshotID
		if snapshotID == "" {
			snapshotID = "evs_prepared" + strings.Repeat("a", index+1)
		}
		state := evidence.ExistingSnapshotReferenceState{
			RecordID: request.RecordID, SnapshotID: snapshotID, Key: evidence.MonitoringHostV1Key(),
			SourceType: string(authorization.Kind), SourceID: authorization.SourceID,
			CaptureAuthorizationDigest: authorization.Digest,
			PayloadDigest:              sha256.Sum256([]byte("handler evidence " + snapshotID)),
			Authorization:              authorization,
		}
		prepared, err := evidence.PrepareExistingSnapshotReference(
			context.Background(), recordEvidenceReferenceSourceStub{state: state}, actor,
			request.RecordID, snapshotID,
		)
		if err != nil {
			t.Fatalf("PrepareExistingSnapshotReference() error = %v", err)
		}
		references = append(references, prepared)
		ordered = append(ordered, snapshotID)
	}
	prepared, err := evidence.NewRevisionPreparation(request.RecordID, evidence.RevisionPreparationValues{
		References: references, OrderedSnapshotIDs: ordered,
	})
	if err != nil {
		t.Fatalf("NewRevisionPreparation() error = %v", err)
	}
	return prepared
}

type recordEvidenceReferenceSourceStub struct {
	state evidence.ExistingSnapshotReferenceState
}

func (stub recordEvidenceReferenceSourceStub) ReauthorizeExistingSnapshot(
	context.Context,
	evidence.ActorScope,
	string,
	string,
) (evidence.ExistingSnapshotReferenceState, error) {
	return stub.state, nil
}

func TestRecordsHandlerListsThroughTrustedActorAndReturnsOwnedDTO(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	application := &recordsHandlerApplicationStub{
		listRecords: func(_ context.Context, request records.RecordListRequest) (records.RecordListResult, error) {
			if !reflect.DeepEqual(request.Actor, actor) {
				t.Fatalf("ListRecords() actor = %#v, want trusted context actor %#v", request.Actor, actor)
			}
			if request.Limit != 25 || request.Sort != records.RecordSortUpdatedDesc {
				t.Fatalf("ListRecords() request = %#v", request)
			}
			return records.RecordListResult{Records: make([]records.Record, 0)}, nil
		},
	}
	handler := RecordsWithOptions(application, RecordHandlerOptions{
		NewRecordID: func() (string, error) { return "rec_httpcontract", nil },
	})
	request := httptest.NewRequest(http.MethodGet, "/api/records?limit=25&sort=updated_at_desc", nil)
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

	responseType := reflect.TypeOf(recordResponse{})
	if responseType.PkgPath() != reflect.TypeOf(recordListResponse{}).PkgPath() {
		t.Fatalf("record response is not handler-owned: %v", responseType)
	}
}

func TestRecordsHandlerGetsAllowlistedCurrentRecordWithoutAuthorizationEvidence(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	want := mustRecordsHandlerRecord(t, actor)
	application := &recordsHandlerApplicationStub{
		getRecord: func(_ context.Context, request records.RecordGetRequest) (records.Record, error) {
			if request.RecordID != want.RecordID || !reflect.DeepEqual(request.Actor, actor) {
				t.Fatalf("GetRecord() request = %#v", request)
			}
			return want, nil
		},
	}
	handler := RecordsWithOptions(application, RecordHandlerOptions{
		NewRecordID: func() (string, error) { return "rec_httpcontract", nil },
	})
	request := recordsHandlerRequest(t, actor, http.MethodGet, "/api/records/"+want.RecordID, "")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, forbidden := range []string{
		"project_id",
		"capture_authorization",
		"current_scope",
		"final_floor",
		"last_live_scope",
		"canonical_hash",
		"authorization_epoch_digest",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, body)
		}
	}
	var response struct {
		RecordID string `json:"record_id"`
		Current  struct {
			RevisionID          string   `json:"revision_id"`
			Title               string   `json:"title"`
			Tags                []string `json:"tags"`
			AttachmentIDs       []string `json:"attachment_ids"`
			EvidenceSnapshotIDs []string `json:"evidence_snapshot_ids"`
			Subjects            []struct {
				SourceID string `json:"source_id"`
				Identity struct {
					DisplayName string `json:"display_name"`
					Provider    string `json:"provider"`
				} `json:"identity"`
			} `json:"subjects"`
			Participants []json.RawMessage `json:"participants"`
		} `json:"current"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.RecordID != want.RecordID || response.Current.RevisionID != want.CurrentRevisionID ||
		response.Current.Title != "Database outage" || response.Current.Tags == nil ||
		response.Current.Subjects == nil || len(response.Current.Subjects) != 1 ||
		response.Current.Subjects[0].SourceID != "vps_0123456789abcdef" ||
		response.Current.Subjects[0].Identity.DisplayName != "VPS Alpha" ||
		response.Current.Subjects[0].Identity.Provider != "Example Cloud" ||
		response.Current.Participants == nil || response.Current.AttachmentIDs == nil ||
		!reflect.DeepEqual(response.Current.AttachmentIDs, []string{"att_httpfirst", "att_httpsecond"}) ||
		!reflect.DeepEqual(response.Current.EvidenceSnapshotIDs, []string{"evs_httpcontract"}) {
		t.Fatalf("response = %#v", response)
	}
}

func TestRecordsHandlerCreatesRecordFromTrustedDraftPayloadAndHeaders(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	draft := mustRecordsHandlerDraft(t, actor, "", "")
	application := &recordsHandlerApplicationStub{
		preparePublish: func(_ context.Context, request records.DraftPublishRequest) (records.Draft, error) {
			if request.DraftID != draft.DraftID || !reflect.DeepEqual(request.Actor, actor) {
				t.Fatalf("PreparePublish() request = %#v", request)
			}
			return draft, nil
		},
		createRecord: func(_ context.Context, request records.RecordCreateRequest) (records.RevisionCommitResult, error) {
			if request.RecordID != "rec_httpcontract" || request.DraftID != draft.DraftID ||
				request.DraftETag != draft.ETag || request.IdempotencyKey != "create-http-record" ||
				!reflect.DeepEqual(request.Actor, actor) {
				t.Fatalf("CreateRecord() request = %#v", request)
			}
			if request.Values.Title != "Database outage" || request.Values.AuthorID != "" ||
				len(request.Values.Subjects) != 0 || len(request.SubjectReferences) != 1 ||
				request.SubjectReferences[0].SourceID != "vps_0123456789abcdef" ||
				request.Values.VisibilityScope.ProjectID != actor.ProjectID ||
				request.Values.VisibilityScope.CanonicalHashValue() != request.Values.VisibilityScope.CanonicalHash ||
				request.Values.Tags == nil || request.Values.Participants == nil ||
				!reflect.DeepEqual(request.Values.AttachmentIDs, []string{"att_httpfirst", "att_httpsecond"}) {
				t.Fatalf("CreateRecord() mapped values = %#v, subjects %#v", request.Values, request.SubjectReferences)
			}
			return records.RevisionCommitResult{
				RecordID:           request.RecordID,
				RevisionID:         "rrv_httpcontract",
				RevisionNo:         1,
				LockVersion:        1,
				AuthorizationEpoch: 1,
				Lifecycle:          records.LifecycleActive,
				Created:            true,
				CommittedAt:        time.Date(2026, time.August, 3, 15, 0, 0, 0, time.UTC),
			}, nil
		},
	}
	handler := RecordsWithOptions(application, RecordHandlerOptions{
		NewRecordID: func() (string, error) { return "rec_httpcontract", nil },
	})
	body := `{"draft_id":"` + draft.DraftID + `","draft_etag":` + strconvQuote(draft.ETag.String()) + `}`
	request := recordsHandlerRequest(t, actor, http.MethodPost, "/api/records", body)
	request.Header.Set("Idempotency-Key", "create-http-record")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	var response struct {
		RecordID   string `json:"record_id"`
		RevisionID string `json:"revision_id"`
		Created    bool   `json:"created"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.RecordID != "rec_httpcontract" || response.RevisionID != "rrv_httpcontract" || !response.Created {
		t.Fatalf("response = %#v", response)
	}
}

func TestRecordDraftPayloadRequiresAndMapsOrderedAttachmentIDs(t *testing.T) {
	t.Parallel()

	actor := mustRecordsHandlerActor(t)
	raw := []byte(`{
		"title":"Attachment order",
		"body_markdown":"body",
		"markdown_dialect_version":1,
		"record_type":"note",
		"business_status":"",
		"impact_level":"high",
		"visibility":{"kind":"project","allowed_roles":[],"allowed_group_ids":[]},
		"subjects":[],
		"tags":[],
		"owner_id":"",
		"participant_ids":[],
		"attachment_ids":["att_httpfirst","att_httpsecond"],
		"save_reason":"ordered attachments"
	}`)
	decoded, err := decodeRecordDraftPayload(raw)
	if err != nil {
		t.Fatalf("decodeRecordDraftPayload() error = %v", err)
	}
	values, _, err := decoded.toDomain(actor)
	if err != nil {
		t.Fatalf("toDomain() error = %v", err)
	}
	if !reflect.DeepEqual(values.AttachmentIDs, []string{"att_httpfirst", "att_httpsecond"}) {
		t.Fatalf("toDomain().AttachmentIDs = %#v", values.AttachmentIDs)
	}

	for _, invalid := range [][]byte{
		[]byte(strings.ReplaceAll(string(raw), `"attachment_ids":["att_httpfirst","att_httpsecond"],`, "")),
		[]byte(strings.ReplaceAll(string(raw), `"attachment_ids":["att_httpfirst","att_httpsecond"]`, `"attachment_ids":null`)),
	} {
		if _, err := decodeRecordDraftPayload(invalid); err == nil {
			t.Fatalf("decodeRecordDraftPayload(%s) error = nil", invalid)
		}
	}
}

func TestRecordsHandlerRoutesRevisionRestoreAndLifecycleActions(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	draft := mustRecordsHandlerDraft(t, actor, "rec_httpcontract", "rrv_httpbase")
	application := &recordsHandlerApplicationStub{
		listRevisions: func(_ context.Context, request records.RecordRevisionListRequest) ([]records.RecordRevision, error) {
			if request.RecordID != "rec_httpcontract" || request.Limit != 20 || !reflect.DeepEqual(request.Actor, actor) {
				t.Fatalf("ListRevisions() request = %#v", request)
			}
			return make([]records.RecordRevision, 0), nil
		},
		getRevision: func(_ context.Context, request records.RecordRevisionGetRequest) (records.RecordRevision, error) {
			if request.RecordID != "rec_httpcontract" || request.RevisionID != "rrv_httpbase" ||
				!reflect.DeepEqual(request.Actor, actor) {
				t.Fatalf("GetRevision() request = %#v", request)
			}
			return records.RecordRevision{RecordID: request.RecordID, RevisionID: request.RevisionID}, nil
		},
		preparePublish: func(_ context.Context, request records.DraftPublishRequest) (records.Draft, error) {
			return draft, nil
		},
		createRevision: func(_ context.Context, request records.RecordRevisionCreateRequest) (records.RevisionCommitResult, error) {
			if request.RecordID != "rec_httpcontract" || request.BaseRevisionID != "rrv_httpbase" ||
				request.LockVersion != 7 || request.AuthorizationEpoch != 5 || request.DraftID != draft.DraftID ||
				request.DraftETag != draft.ETag || request.IdempotencyKey != "revise-http-record" ||
				!reflect.DeepEqual(request.Actor, actor) {
				t.Fatalf("CreateRevision() request = %#v", request)
			}
			return records.RevisionCommitResult{RecordID: request.RecordID, RevisionID: "rrv_httpnext", Created: true}, nil
		},
		restoreRevision: func(_ context.Context, request records.RecordRestoreRequest) (records.RevisionCommitResult, error) {
			if request.RecordID != "rec_httpcontract" || request.RevisionID != "rrv_httpbase" ||
				request.SaveReason != "restore known good" || request.IdempotencyKey != "restore-http-record" ||
				!reflect.DeepEqual(request.Actor, actor) {
				t.Fatalf("RestoreRevision() request = %#v", request)
			}
			return records.RevisionCommitResult{RecordID: request.RecordID, RevisionID: "rrv_httprestored", Created: true}, nil
		},
		changeLifecycle: func(_ context.Context, request records.RecordLifecycleChangeRequest) (records.RecordLifecycleResult, error) {
			if request.RecordID != "rec_httpcontract" || request.IdempotencyKey == "" || !reflect.DeepEqual(request.Actor, actor) {
				t.Fatalf("ChangeLifecycle() request = %#v", request)
			}
			return records.RecordLifecycleResult{RecordID: request.RecordID, Lifecycle: request.TargetLifecycle}, nil
		},
	}
	handler := RecordsWithOptions(application, RecordHandlerOptions{
		NewRecordID: func() (string, error) { return "rec_unused", nil },
	})

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		key        string
		wantStatus int
	}{
		{name: "revision list", method: http.MethodGet, path: "/api/records/rec_httpcontract/revisions?limit=20", wantStatus: http.StatusOK},
		{name: "revision get", method: http.MethodGet, path: "/api/records/rec_httpcontract/revisions/rrv_httpbase", wantStatus: http.StatusOK},
		{
			name:   "revision create",
			method: http.MethodPost,
			path:   "/api/records/rec_httpcontract/revisions",
			body: `{"base_revision_id":"rrv_httpbase","lock_version":7,"authorization_epoch":5,"draft_id":"` +
				draft.DraftID + `","draft_etag":` + strconvQuote(draft.ETag.String()) + `}`,
			key:        "revise-http-record",
			wantStatus: http.StatusCreated,
		},
		{
			name:       "historical restore",
			method:     http.MethodPost,
			path:       "/api/records/rec_httpcontract/revisions/rrv_httpbase/restore",
			body:       `{"save_reason":"restore known good"}`,
			key:        "restore-http-record",
			wantStatus: http.StatusCreated,
		},
		{name: "archive", method: http.MethodPost, path: "/api/records/rec_httpcontract/archive", key: "archive-http-record", wantStatus: http.StatusOK},
		{name: "restore lifecycle", method: http.MethodPost, path: "/api/records/rec_httpcontract/restore", key: "unarchive-http-record", wantStatus: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := recordsHandlerRequest(t, actor, tt.method, tt.path, tt.body)
			if tt.key != "" {
				request.Header.Set("Idempotency-Key", tt.key)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
			if strings.Contains(tt.name, "list") {
				var response struct {
					Items []json.RawMessage `json:"items"`
				}
				if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || response.Items == nil {
					t.Fatalf("revision list response = %#v, error %v", response, err)
				}
			}
		})
	}
}

func TestRecordsHandlerMapsStableErrorsAndRejectsUntrustedOrOversizedInput(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "denied", err: recordauth.ErrDenied, wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "not found", err: records.ErrRecordNotFound, wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "reserved", err: records.ErrRecordDeletionReserved, wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "revision conflict", err: records.ErrRecordRevisionConflict, wantStatus: http.StatusConflict, wantCode: "record_revision_conflict"},
		{name: "idempotency reused", err: recordplatform.ErrIdempotencyKeyReused, wantStatus: http.StatusConflict, wantCode: "idempotency_key_reused"},
		{name: "semantic validation", err: records.ErrInvalidRevisionInput, wantStatus: http.StatusUnprocessableEntity, wantCode: "record_invalid"},
		{name: "status reason required", err: records.ErrStatusTransitionReasonRequired, wantStatus: http.StatusUnprocessableEntity, wantCode: "record_invalid"},
		{name: "lifecycle request invalid", err: records.ErrInvalidRecordLifecycleRequest, wantStatus: http.StatusUnprocessableEntity, wantCode: "record_invalid"},
		{name: "platform unavailable", err: store.ErrRecordPlatformAdmissionUnavailable, wantStatus: http.StatusServiceUnavailable, wantCode: "record_service_unavailable"},
		{name: "source unavailable", err: store.ErrRecordSubjectUnavailable, wantStatus: http.StatusServiceUnavailable, wantCode: "record_service_unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := RecordsWithOptions(&recordsHandlerApplicationStub{
				getRecord: func(context.Context, records.RecordGetRequest) (records.Record, error) {
					return records.Record{}, tt.err
				},
			}, RecordHandlerOptions{NewRecordID: func() (string, error) { return "rec_unused", nil }})
			request := recordsHandlerRequest(t, actor, http.MethodGet, "/api/records/rec_httpcontract", "")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			assertRecordsHandlerError(t, recorder, tt.wantStatus, tt.wantCode)
		})
	}

	t.Run("missing typed actor", func(t *testing.T) {
		calls := 0
		handler := RecordsWithOptions(&recordsHandlerApplicationStub{
			listRecords: func(context.Context, records.RecordListRequest) (records.RecordListResult, error) {
				calls++
				return records.RecordListResult{}, nil
			},
		}, RecordHandlerOptions{NewRecordID: func() (string, error) { return "rec_unused", nil }})
		request := httptest.NewRequest(http.MethodGet, "/api/records", nil)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		assertRecordsHandlerError(t, recorder, http.StatusServiceUnavailable, "authorization_unavailable")
		if calls != 0 {
			t.Fatalf("application calls = %d, want 0", calls)
		}
	})

	t.Run("oversized body", func(t *testing.T) {
		handler := RecordsWithOptions(&recordsHandlerApplicationStub{}, RecordHandlerOptions{
			NewRecordID: func() (string, error) { return "rec_unused", nil },
		})
		request := recordsHandlerRequest(t, actor, http.MethodPost, "/api/records", `{"draft_id":"rdf_http","padding":"`+strings.Repeat("x", DefaultJSONBodyLimit)+`"}`)
		request.Header.Set("Idempotency-Key", "oversized-http-record")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		assertRecordsHandlerError(t, recorder, http.StatusRequestEntityTooLarge, "request_too_large")
	})

	t.Run("unknown field", func(t *testing.T) {
		handler := RecordsWithOptions(&recordsHandlerApplicationStub{}, RecordHandlerOptions{
			NewRecordID: func() (string, error) { return "rec_unused", nil },
		})
		request := recordsHandlerRequest(t, actor, http.MethodPost, "/api/records", `{"draft_id":"rdf_http","draft_etag":"bad","trusted_project":"other"}`)
		request.Header.Set("Idempotency-Key", "unknown-http-record")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		assertRecordsHandlerError(t, recorder, http.StatusBadRequest, "invalid_json")
	})
}

func TestRecordHandlersSetPrivateNoStoreForAllResponseClasses(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	tests := []struct {
		name    string
		handler http.Handler
		request *http.Request
	}{
		{
			name: "records success",
			handler: RecordsWithOptions(&recordsHandlerApplicationStub{
				listRecords: func(context.Context, records.RecordListRequest) (records.RecordListResult, error) {
					return records.RecordListResult{Records: make([]records.Record, 0)}, nil
				},
			}, RecordHandlerOptions{NewRecordID: func() (string, error) { return "rec_unused", nil }}),
			request: recordsHandlerRequest(t, actor, http.MethodGet, "/api/records", ""),
		},
		{
			name: "records not found",
			handler: RecordsWithOptions(&recordsHandlerApplicationStub{}, RecordHandlerOptions{
				NewRecordID: func() (string, error) { return "rec_unused", nil },
			}),
			request: recordsHandlerRequest(t, actor, http.MethodGet, "/api/records/rec_httpcontract/unknown", ""),
		},
		{
			name: "records conflict",
			handler: RecordsWithOptions(&recordsHandlerApplicationStub{
				getRecord: func(context.Context, records.RecordGetRequest) (records.Record, error) {
					return records.Record{}, records.ErrRecordRevisionConflict
				},
			}, RecordHandlerOptions{NewRecordID: func() (string, error) { return "rec_unused", nil }}),
			request: recordsHandlerRequest(t, actor, http.MethodGet, "/api/records/rec_httpcontract", ""),
		},
		{
			name: "records unavailable",
			handler: RecordsWithOptions(nil, RecordHandlerOptions{
				NewRecordID: func() (string, error) { return "rec_unused", nil },
			}),
			request: recordsHandlerRequest(t, actor, http.MethodGet, "/api/records", ""),
		},
		{
			name: "draft no content",
			handler: RecordDraftsWithOptions(&recordDraftHandlerApplicationStub{
				discardDraft: func(context.Context, records.DraftDiscardRequest) error { return nil },
			}, RecordDraftHandlerOptions{NewDraftID: func() (string, error) { return "rdf_unused", nil }}),
			request: recordsHandlerRequest(t, actor, http.MethodDelete, "/api/record-drafts/rdf_httpcontract", ""),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			test.handler.ServeHTTP(recorder, test.request)
			if got := recorder.Header().Get("Cache-Control"); got != recordPrivateCacheControl {
				t.Fatalf("Cache-Control = %q, want %s; status=%d body=%s", got, recordPrivateCacheControl, recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestRecordsHandlerTreatsNonAllowlistedPersistedDraftPayloadAsInternalError(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	draft := recordsHandlerDraftWithRawPayload(
		t,
		mustRecordsHandlerDraft(t, actor, "", ""),
		`{"secret_internal":"must-not-leak"}`,
	)
	createCalls := 0
	handler := RecordsWithOptions(&recordsHandlerApplicationStub{
		preparePublish: func(context.Context, records.DraftPublishRequest) (records.Draft, error) {
			return draft, nil
		},
		createRecord: func(context.Context, records.RecordCreateRequest) (records.RevisionCommitResult, error) {
			createCalls++
			return records.RevisionCommitResult{}, nil
		},
	}, RecordHandlerOptions{NewRecordID: func() (string, error) { return "rec_unused", nil }})
	request := recordsHandlerRequest(
		t,
		actor,
		http.MethodPost,
		"/api/records",
		`{"draft_id":"`+draft.DraftID+`","draft_etag":`+strconvQuote(draft.ETag.String())+`}`,
	)
	request.Header.Set("Idempotency-Key", "unknown-persisted-draft")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	assertRecordsHandlerError(t, recorder, http.StatusInternalServerError, "internal_error")
	if createCalls != 0 || strings.Contains(recorder.Body.String(), "secret_internal") || strings.Contains(recorder.Body.String(), "must-not-leak") {
		t.Fatalf("persisted draft handling calls=%d body=%s", createCalls, recorder.Body.String())
	}
}

func TestRecordsHandlerRejectsTrailingSlashItemPathBeforeApplication(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	calls := 0
	handler := RecordsWithOptions(&recordsHandlerApplicationStub{
		getRecord: func(context.Context, records.RecordGetRequest) (records.Record, error) {
			calls++
			return records.Record{}, nil
		},
	}, RecordHandlerOptions{NewRecordID: func() (string, error) { return "rec_unused", nil }})
	request := recordsHandlerRequest(t, actor, http.MethodGet, "/api/records/rec_httpcontract/", "")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	assertRecordsHandlerError(t, recorder, http.StatusNotFound, "resource_not_found")
	if calls != 0 {
		t.Fatalf("GetRecord() calls = %d, want 0", calls)
	}
}

func TestRecordsHandlerRoundTripsOpaqueCursorAndRejectsInvalidCursor(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	wantCursor := records.RecordCursor{
		UpdatedAt: time.Date(2026, time.August, 3, 16, 0, 0, 123, time.FixedZone("offset", 8*60*60)),
		RecordID:  "rec_cursorcontract",
	}
	calls := 0
	handler := RecordsWithOptions(&recordsHandlerApplicationStub{
		listRecords: func(_ context.Context, request records.RecordListRequest) (records.RecordListResult, error) {
			calls++
			switch calls {
			case 1:
				if request.After != nil {
					t.Fatalf("first ListRecords() cursor = %#v, want nil", request.After)
				}
				return records.RecordListResult{Records: make([]records.Record, 0), NextCursor: &wantCursor}, nil
			case 2:
				if request.After == nil || request.After.RecordID != wantCursor.RecordID ||
					!request.After.UpdatedAt.Equal(wantCursor.UpdatedAt) || request.After.UpdatedAt.Location() != time.UTC {
					t.Fatalf("second ListRecords() cursor = %#v, want UTC %#v", request.After, wantCursor)
				}
				return records.RecordListResult{Records: make([]records.Record, 0)}, nil
			default:
				t.Fatalf("ListRecords() calls = %d, want at most 2", calls)
				return records.RecordListResult{}, nil
			}
		},
	}, RecordHandlerOptions{NewRecordID: func() (string, error) { return "rec_unused", nil }})

	first := recordsHandlerRequest(t, actor, http.MethodGet, "/api/records", "")
	firstRecorder := httptest.NewRecorder()
	handler.ServeHTTP(firstRecorder, first)
	if firstRecorder.Code != http.StatusOK {
		t.Fatalf("first status = %d; body=%s", firstRecorder.Code, firstRecorder.Body.String())
	}
	var firstResponse struct {
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal(firstRecorder.Body.Bytes(), &firstResponse); err != nil || firstResponse.NextCursor == "" {
		t.Fatalf("first response = %#v, error %v", firstResponse, err)
	}

	second := recordsHandlerRequest(t, actor, http.MethodGet, "/api/records?cursor="+firstResponse.NextCursor, "")
	secondRecorder := httptest.NewRecorder()
	handler.ServeHTTP(secondRecorder, second)
	if secondRecorder.Code != http.StatusOK {
		t.Fatalf("second status = %d; body=%s", secondRecorder.Code, secondRecorder.Body.String())
	}

	invalid := recordsHandlerRequest(t, actor, http.MethodGet, "/api/records?cursor=not-a-cursor", "")
	invalidRecorder := httptest.NewRecorder()
	handler.ServeHTTP(invalidRecorder, invalid)
	assertRecordsHandlerError(t, invalidRecorder, http.StatusBadRequest, "cursor_invalid")
	if calls != 2 {
		t.Fatalf("ListRecords() calls = %d, want 2", calls)
	}
}

func TestRecordsHandlerRequiresIdempotencyKeyBeforeFormalMutations(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	handler := RecordsWithOptions(&recordsHandlerApplicationStub{}, RecordHandlerOptions{
		NewRecordID: func() (string, error) { return "rec_unused", nil },
	})
	for _, tt := range []struct {
		name string
		path string
		body string
	}{
		{name: "create record", path: "/api/records", body: `{}`},
		{name: "create revision", path: "/api/records/rec_httpcontract/revisions", body: `{}`},
		{name: "restore revision", path: "/api/records/rec_httpcontract/revisions/rrv_httpbase/restore", body: `{}`},
		{name: "archive", path: "/api/records/rec_httpcontract/archive"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := recordsHandlerRequest(t, actor, http.MethodPost, tt.path, tt.body)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			assertRecordsHandlerError(t, recorder, http.StatusBadRequest, "invalid_request")
		})
	}
}

func TestRecordsHandlerRejectsDraftRoutingMismatchBeforeRevisionWrite(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	draft := mustRecordsHandlerDraft(t, actor, "rec_otherrecord", "rrv_httpbase")
	createCalls := 0
	handler := RecordsWithOptions(&recordsHandlerApplicationStub{
		preparePublish: func(context.Context, records.DraftPublishRequest) (records.Draft, error) {
			return draft, nil
		},
		createRevision: func(context.Context, records.RecordRevisionCreateRequest) (records.RevisionCommitResult, error) {
			createCalls++
			return records.RevisionCommitResult{}, nil
		},
	}, RecordHandlerOptions{NewRecordID: func() (string, error) { return "rec_unused", nil }})
	body := `{"base_revision_id":"rrv_httpbase","lock_version":7,"authorization_epoch":5,"draft_id":"` +
		draft.DraftID + `","draft_etag":` + strconvQuote(draft.ETag.String()) + `}`
	request := recordsHandlerRequest(t, actor, http.MethodPost, "/api/records/rec_httpcontract/revisions", body)
	request.Header.Set("Idempotency-Key", "mismatched-draft-routing")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	assertRecordsHandlerError(t, recorder, http.StatusConflict, "record_revision_conflict")
	if createCalls != 0 {
		t.Fatalf("CreateRevision() calls = %d, want 0", createCalls)
	}
}

func TestRecordsHandlerUsesExactSubtreesAndMethodBoundaries(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	handler := RecordsWithOptions(&recordsHandlerApplicationStub{}, RecordHandlerOptions{
		NewRecordID: func() (string, error) { return "rec_unused", nil },
	})
	for _, tt := range []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantCode   string
	}{
		{name: "unknown subtree", method: http.MethodGet, path: "/api/records/rec_httpcontract/unknown", wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "deeper subtree", method: http.MethodGet, path: "/api/records/rec_httpcontract/revisions/rrv_httpbase/unknown", wantStatus: http.StatusNotFound, wantCode: "resource_not_found"},
		{name: "unsupported item method", method: http.MethodDelete, path: "/api/records/rec_httpcontract", wantStatus: http.StatusMethodNotAllowed, wantCode: "method_not_allowed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := recordsHandlerRequest(t, actor, tt.method, tt.path, "")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			assertRecordsHandlerError(t, recorder, tt.wantStatus, tt.wantCode)
		})
	}
}

type recordsHandlerApplicationStub struct {
	getRecord       func(context.Context, records.RecordGetRequest) (records.Record, error)
	listRecords     func(context.Context, records.RecordListRequest) (records.RecordListResult, error)
	getRevision     func(context.Context, records.RecordRevisionGetRequest) (records.RecordRevision, error)
	listRevisions   func(context.Context, records.RecordRevisionListRequest) ([]records.RecordRevision, error)
	createRecord    func(context.Context, records.RecordCreateRequest) (records.RevisionCommitResult, error)
	createRevision  func(context.Context, records.RecordRevisionCreateRequest) (records.RevisionCommitResult, error)
	restoreRevision func(context.Context, records.RecordRestoreRequest) (records.RevisionCommitResult, error)
	changeLifecycle func(context.Context, records.RecordLifecycleChangeRequest) (records.RecordLifecycleResult, error)
	preparePublish  func(context.Context, records.DraftPublishRequest) (records.Draft, error)
}

func (stub *recordsHandlerApplicationStub) GetRecord(ctx context.Context, request records.RecordGetRequest) (records.Record, error) {
	if stub.getRecord == nil {
		return records.Record{}, errors.New("unexpected GetRecord call")
	}
	return stub.getRecord(ctx, request)
}

func (stub *recordsHandlerApplicationStub) ListRecords(ctx context.Context, request records.RecordListRequest) (records.RecordListResult, error) {
	if stub.listRecords == nil {
		return records.RecordListResult{}, errors.New("unexpected ListRecords call")
	}
	return stub.listRecords(ctx, request)
}

func (stub *recordsHandlerApplicationStub) GetRevision(ctx context.Context, request records.RecordRevisionGetRequest) (records.RecordRevision, error) {
	if stub.getRevision == nil {
		return records.RecordRevision{}, errors.New("unexpected GetRevision call")
	}
	return stub.getRevision(ctx, request)
}

func (stub *recordsHandlerApplicationStub) ListRevisions(ctx context.Context, request records.RecordRevisionListRequest) ([]records.RecordRevision, error) {
	if stub.listRevisions == nil {
		return nil, errors.New("unexpected ListRevisions call")
	}
	return stub.listRevisions(ctx, request)
}

func (stub *recordsHandlerApplicationStub) CreateRecord(ctx context.Context, request records.RecordCreateRequest) (records.RevisionCommitResult, error) {
	if stub.createRecord == nil {
		return records.RevisionCommitResult{}, errors.New("unexpected CreateRecord call")
	}
	return stub.createRecord(ctx, request)
}

func (stub *recordsHandlerApplicationStub) CreateRevision(ctx context.Context, request records.RecordRevisionCreateRequest) (records.RevisionCommitResult, error) {
	if stub.createRevision == nil {
		return records.RevisionCommitResult{}, errors.New("unexpected CreateRevision call")
	}
	return stub.createRevision(ctx, request)
}

func (stub *recordsHandlerApplicationStub) RestoreRevision(ctx context.Context, request records.RecordRestoreRequest) (records.RevisionCommitResult, error) {
	if stub.restoreRevision == nil {
		return records.RevisionCommitResult{}, errors.New("unexpected RestoreRevision call")
	}
	return stub.restoreRevision(ctx, request)
}

func (stub *recordsHandlerApplicationStub) ChangeLifecycle(ctx context.Context, request records.RecordLifecycleChangeRequest) (records.RecordLifecycleResult, error) {
	if stub.changeLifecycle == nil {
		return records.RecordLifecycleResult{}, errors.New("unexpected ChangeLifecycle call")
	}
	return stub.changeLifecycle(ctx, request)
}

func (stub *recordsHandlerApplicationStub) PreparePublish(ctx context.Context, request records.DraftPublishRequest) (records.Draft, error) {
	if stub.preparePublish == nil {
		return records.Draft{}, errors.New("unexpected PreparePublish call")
	}
	return stub.preparePublish(ctx, request)
}

func recordsHandlerRequest(
	t *testing.T,
	actor recordauth.ActorScope,
	method string,
	path string,
	body string,
) *http.Request {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	return request.WithContext(sessionctx.WithActorScope(request.Context(), actor))
}

func assertRecordsHandlerError(t *testing.T, recorder *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if recorder.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, wantStatus, recorder.Body.String())
	}
	var response struct {
		Code        string            `json:"code"`
		Message     string            `json:"message"`
		FieldErrors []json.RawMessage `json:"field_errors"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if response.Code != wantCode || response.Message == "" || response.FieldErrors == nil {
		t.Fatalf("error response = %#v", response)
	}
}

func mustRecordsHandlerRecord(t *testing.T, actor recordauth.ActorScope) records.Record {
	t.Helper()
	visibility, err := recordauth.NormalizeVisibilityScope(recordauth.VisibilityScope{
		Version:        recordauth.VisibilityScopeVersionV1,
		Kind:           recordauth.VisibilityKindProject,
		ProjectID:      actor.ProjectID,
		PolicyVersion:  recordauth.PolicyVersionV1,
		PolicyRevision: 1,
	})
	if err != nil {
		t.Fatalf("NormalizeVisibilityScope() error = %v", err)
	}
	currentScope := visibility
	authorization, err := recordauth.NormalizeSourceAuthorization(recordauth.SourceAuthorization{
		Version:      recordauth.SourceAuthorizationVersionV1,
		Kind:         recordauth.SourceKindVPS,
		SourceID:     "vps_0123456789abcdef",
		State:        recordauth.SourceStateLive,
		CaptureScope: visibility,
		CurrentScope: &currentScope,
	})
	if err != nil {
		t.Fatalf("NormalizeSourceAuthorization() error = %v", err)
	}
	input, err := records.NormalizeCompleteRevisionInput(records.CompleteRevisionValues{
		Title:                  "Database outage",
		BodyMarkdown:           "# Details\nRecovered.",
		MarkdownDialectVersion: records.MarkdownDialectVersionV1,
		RecordType:             records.RecordTypeNote,
		ImpactLevel:            "high",
		VisibilityScope:        visibility,
		Subjects: []records.RevisionSubject{{
			RegistryVersion:      records.SubjectRegistryVersionV1,
			Kind:                 records.SubjectKindVPS,
			Role:                 records.RelationRoleAffected,
			SourceID:             "vps_0123456789abcdef",
			Primary:              true,
			IdentitySnapshot:     map[string]string{"display_name": "VPS Alpha", "provider": "Example Cloud"},
			CaptureAuthorization: authorization,
		}},
		Tags:                make([]string, 0),
		Participants:        make([]records.RevisionParticipantSnapshot, 0),
		AttachmentIDs:       []string{"att_httpfirst", "att_httpsecond"},
		EvidenceSnapshotIDs: []string{"evs_httpcontract"},
		AuthorID:            actor.UserID,
		SaveReason:          "initial record",
	})
	if err != nil {
		t.Fatalf("NormalizeCompleteRevisionInput() error = %v", err)
	}
	createdAt := time.Date(2026, time.August, 3, 14, 0, 0, 0, time.UTC)
	return records.Record{
		RecordID:           "rec_httpcontract",
		Lifecycle:          records.LifecycleActive,
		CurrentRevisionID:  "rrv_httpcontract",
		LockVersion:        7,
		AuthorizationEpoch: 5,
		Current: records.RecordRevision{
			RecordID:   "rec_httpcontract",
			RevisionID: "rrv_httpcontract",
			RevisionNo: 1,
			Input:      input,
			CreatedAt:  createdAt,
		},
		Capabilities: records.RecordCapabilities{Read: true, Update: true, Archive: true, Draft: true},
		CreatedAt:    createdAt,
		UpdatedAt:    createdAt,
	}
}

func mustRecordsHandlerDraft(
	t *testing.T,
	actor recordauth.ActorScope,
	recordID string,
	baseRevisionID string,
) records.Draft {
	t.Helper()
	payload, err := records.NewDraftPayload([]byte(`{
		"title":"Database outage",
		"body_markdown":"# Details\nRecovered.",
		"markdown_dialect_version":1,
		"record_type":"note",
		"business_status":"",
		"impact_level":"high",
		"visibility":{"kind":"project","allowed_roles":[],"allowed_group_ids":[]},
		"subjects":[{"registry_version":1,"kind":"vps","role":"affected","source_id":"vps_0123456789abcdef","primary":true}],
		"tags":[],
		"owner_id":"",
		"participant_ids":[],
		"attachment_ids":["att_httpfirst","att_httpsecond"],
		"save_reason":"initial record"
	}`))
	if err != nil {
		t.Fatalf("NewDraftPayload() error = %v", err)
	}
	etag, err := records.NewDraftETag("rdf_httpcontract", actor.UserID, 1, payload)
	if err != nil {
		t.Fatalf("NewDraftETag() error = %v", err)
	}
	now := time.Date(2026, time.August, 3, 14, 30, 0, 0, time.UTC)
	return records.Draft{
		DraftID:        "rdf_httpcontract",
		ProjectID:      actor.ProjectID,
		RecordID:       recordID,
		BaseRevisionID: baseRevisionID,
		AuthorID:       actor.UserID,
		Payload:        payload,
		Version:        1,
		ETag:           etag,
		CreatedAt:      now,
		UpdatedAt:      now,
		WarningAt:      now.Add(83 * 24 * time.Hour),
		ExpiresAt:      now.Add(90 * 24 * time.Hour),
	}
}

func strconvQuote(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func mustRecordsHandlerActor(t *testing.T) recordauth.ActorScope {
	t.Helper()
	actor, err := recordauth.NormalizeActorScope(recordauth.ActorScope{
		UserID:    "usr_0123456789abcdef01234567",
		Role:      recordauth.RoleProjectAdmin,
		ProjectID: recordauth.ProjectIDDefault,
		GroupIDs:  []string{"rag_ops"},
	})
	if err != nil {
		t.Fatalf("NormalizeActorScope() error = %v", err)
	}
	return actor
}
