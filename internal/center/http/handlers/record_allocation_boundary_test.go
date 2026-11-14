package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"houfeng/internal/center/evidence"
	"houfeng/internal/center/recordauth"
	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordidentity"
	"houfeng/internal/center/records"
)

func TestRecordAllocationHTTPRequiresCapabilityAndKeepsRequestBinding(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	options, memory := newHandlerBirthFixture(t)
	handler := RecordsWithOptions(&recordsHandlerApplicationStub{}, options)
	body := `{"purpose":"record_create","request_digest":"1111111111111111111111111111111111111111111111111111111111111111"}`
	post := func(actor recordauth.ActorScope, key, body string) *httptest.ResponseRecorder {
		request := recordsHandlerRequest(t, actor, http.MethodPost, "/api/records/allocations", body)
		request.Header.Set("Idempotency-Key", key)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	viewer := actor.Clone()
	viewer.Role = recordauth.RoleViewer
	denied := post(viewer, "denied-allocation", body)
	assertRecordsHandlerError(t, denied, http.StatusNotFound, "resource_not_found")
	if memory.begins != 0 {
		t.Fatal("denied actor caused an allocation")
	}
	first := post(actor, "allocation-key", body)
	second := post(actor, "allocation-key", body)
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("allocation/replay status = %d/%d; bodies %s / %s", first.Code, second.Code, first.Body.String(), second.Body.String())
	}
	var original, replay recordAllocationResponse
	if err := json.Unmarshal(first.Body.Bytes(), &original); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(second.Body.Bytes(), &replay); err != nil {
		t.Fatal(err)
	}
	if original != replay || recordidentity.Classify(original.RecordID) != recordidentity.Ordinary || memory.begins != 1 {
		t.Fatalf("allocation/replay = %#v/%#v, allocation attempts %d", original, replay, memory.begins)
	}
	changed := strings.Replace(body, strings.Repeat("1", 64), strings.Repeat("2", 64), 1)
	assertRecordsHandlerError(t, post(actor, "allocation-key", changed), http.StatusConflict, "idempotency_conflict")
	if memory.begins != 1 {
		t.Fatal("changed allocation request created a second identity")
	}
}

func TestRecordPublicationHTTPRequiresExactAllocation(t *testing.T) {
	for _, test := range []struct {
		name    string
		purpose recordbirth.Purpose
		modify  func(*recordauth.ActorScope, *recordAllocationResponse, *string, *bool)
		want    int
	}{
		{name: "matching reference", purpose: recordbirth.PurposeEvidence, want: http.StatusCreated},
		{name: "different actor", purpose: recordbirth.PurposeEvidence, modify: func(actor *recordauth.ActorScope, _ *recordAllocationResponse, _ *string, _ *bool) {
			actor.UserID = "usr_1123456789abcdef01234567"
		}, want: http.StatusServiceUnavailable},
		{name: "different scope", purpose: recordbirth.PurposeEvidence, modify: func(actor *recordauth.ActorScope, _ *recordAllocationResponse, _ *string, _ *bool) {
			actor.GroupIDs = []string{"rag_other"}
		}, want: http.StatusConflict},
		{name: "different purpose", purpose: recordbirth.PurposeComparison, want: http.StatusConflict},
		{name: "different record", purpose: recordbirth.PurposeEvidence, modify: func(_ *recordauth.ActorScope, _ *recordAllocationResponse, recordID *string, _ *bool) {
			*recordID = "rec2_00000000000000000000000000000002"
		}, want: http.StatusConflict},
		{name: "tampered reservation", purpose: recordbirth.PurposeEvidence, modify: func(_ *recordauth.ActorScope, ref *recordAllocationResponse, _ *string, _ *bool) {
			ref.ReservationID = "rba_00000000000000000000000000000002"
		}, want: http.StatusConflict},
		{name: "tampered digest", purpose: recordbirth.PurposeEvidence, modify: func(_ *recordauth.ActorScope, ref *recordAllocationResponse, _ *string, _ *bool) {
			ref.RequestDigest = strings.Repeat("2", 64)
		}, want: http.StatusConflict},
		{name: "tampered generation", purpose: recordbirth.PurposeEvidence, modify: func(_ *recordauth.ActorScope, ref *recordAllocationResponse, _ *string, _ *bool) { ref.Generation++ }, want: http.StatusConflict},
		{name: "tampered allocation key", purpose: recordbirth.PurposeEvidence, modify: func(_ *recordauth.ActorScope, ref *recordAllocationResponse, _ *string, _ *bool) {
			ref.IdempotencyKey = "another-allocation"
		}, want: http.StatusConflict},
		{name: "missing allocation", purpose: recordbirth.PurposeEvidence, modify: func(_ *recordauth.ActorScope, _ *recordAllocationResponse, _ *string, include *bool) {
			*include = false
		}, want: http.StatusBadRequest},
		{name: "unallocated legacy target", purpose: recordbirth.PurposeEvidence, modify: func(_ *recordauth.ActorScope, _ *recordAllocationResponse, recordID *string, include *bool) {
			*recordID, *include = "rec_oldunallocated", false
		}, want: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			actor := mustRecordsHandlerActor(t)
			draft := mustRecordsHandlerDraft(t, actor, "", "")
			options, memory := newHandlerBirthFixture(t)
			preparer := &recordEvidencePreparerStub{t: t}
			options.EvidencePreparer = preparer
			creates, drafts := 0, 0
			handler := RecordsWithOptions(&recordsHandlerApplicationStub{
				preparePublish: func(context.Context, records.DraftPublishRequest) (records.Draft, error) { drafts++; return draft, nil },
				createRecord: func(_ context.Context, request records.RecordCreateRequest) (records.RevisionCommitResult, error) {
					creates++
					return records.RevisionCommitResult{RecordID: request.RecordID, RevisionID: "rrv_test", RevisionNo: 1, Created: true}, nil
				},
			}, options)
			allocation := mustHandlerAllocation(t, handler, actor, test.purpose, "allocation-key")
			recordID, include := allocation.RecordID, true
			if test.modify != nil {
				test.modify(&actor, &allocation, &recordID, &include)
			}
			body := `{"record_id":` + strconvQuote(recordID) + `,"draft_id":` + strconvQuote(draft.DraftID) + `,"draft_etag":` + strconvQuote(draft.ETag.String()) + `,"evidence_items":[{"capture_intent_id":"evi_0123456789abcdef01234567"}]`
			if include {
				body += `,"allocation":` + handlerAllocationJSON(t, allocation)
			}
			body += `}`
			request := recordsHandlerRequest(t, actor, http.MethodPost, "/api/records", body)
			request.Header.Set("Idempotency-Key", "publish-key")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != test.want {
				t.Fatalf("status = %d body=%s, want %d", recorder.Code, recorder.Body.String(), test.want)
			}
			if test.want == http.StatusCreated {
				if creates != 1 || drafts != 1 || preparer.calls != 1 {
					t.Fatalf("matching reference reached create/draft/evidence %d/%d/%d times", creates, drafts, preparer.calls)
				}
			} else if creates != 0 || drafts != 0 || preparer.calls != 0 || memory.begins != 1 {
				t.Fatalf("rejected reference produced create/draft/evidence/allocation %d/%d/%d/%d", creates, drafts, preparer.calls, memory.begins)
			}
		})
	}
}

func TestRecordPublicationHTTPReplaysCompletedResultBeforeDraftAndEvidence(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	draft := mustRecordsHandlerDraft(t, actor, "", "")
	options, memory := newHandlerBirthFixture(t)
	preparer := &recordEvidencePreparerStub{t: t}
	options.EvidencePreparer = preparer
	var revision records.RecordRevision
	var readErr error
	creates, drafts := 0, 0
	handler := RecordsWithOptions(&recordsHandlerApplicationStub{
		preparePublish: func(context.Context, records.DraftPublishRequest) (records.Draft, error) {
			drafts++
			if drafts > 1 {
				return records.Draft{}, records.ErrDraftNotFound
			}
			return draft, nil
		},
		createRecord: func(_ context.Context, request records.RecordCreateRequest) (records.RevisionCommitResult, error) {
			creates++
			revision = mustRecordsHandlerRecord(t, actor).Current
			revision.RecordID = request.RecordID
			base := revision.Input
			input, err := records.NormalizeCompleteRevisionInput(records.CompleteRevisionValues{
				Title: base.Title(), BodyMarkdown: base.BodyMarkdown(), MarkdownDialectVersion: base.MarkdownDialectVersion(),
				RecordType: base.RecordType(), BusinessStatus: base.BusinessStatus(), ImpactLevel: base.ImpactLevel(),
				OccurredAt: base.OccurredAt(), CompletedAt: base.CompletedAt(), VisibilityScope: base.VisibilityScope(),
				Subjects: base.Subjects(), Tags: base.Tags(), OwnerID: base.OwnerID(), Participants: base.Participants(),
				AttachmentIDs: base.AttachmentIDs(), EvidenceSnapshotIDs: request.EvidencePreparation.SnapshotIDs(),
				FollowUpAt: base.FollowUpAt(), Template: base.Template(), AuthorID: base.AuthorID(), SaveReason: base.SaveReason(),
			})
			if err != nil {
				t.Fatal(err)
			}
			revision.Input = input
			return records.RevisionCommitResult{RecordID: request.RecordID, RevisionID: revision.RevisionID, RevisionNo: 1, Created: true, CommittedAt: revision.CreatedAt}, nil
		},
		getRevision: func(_ context.Context, request records.RecordRevisionGetRequest) (records.RecordRevision, error) {
			if readErr != nil {
				return records.RecordRevision{}, readErr
			}
			if request.Actor.CanonicalHash() != actor.CanonicalHash() {
				return records.RecordRevision{}, recordauth.ErrDenied
			}
			if request.RecordID != revision.RecordID || request.RevisionID != revision.RevisionID {
				t.Fatalf("wrong replay identity: %#v", request)
			}
			return revision, nil
		},
	}, options)
	allocation := mustHandlerAllocation(t, handler, actor, recordbirth.PurposeEvidence, "preview-allocation")
	body := `{"record_id":` + strconvQuote(allocation.RecordID) + `,"allocation":` + handlerAllocationJSON(t, allocation) + `,"draft_id":` + strconvQuote(draft.DraftID) + `,"draft_etag":` + strconvQuote(draft.ETag.String()) + `,"evidence_items":[{"capture_intent_id":"evi_0123456789abcdef01234567"}]}`
	post := func(body string) *httptest.ResponseRecorder {
		request := recordsHandlerRequest(t, actor, http.MethodPost, "/api/records", body)
		request.Header.Set("Idempotency-Key", "publication-key")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	first := post(body)
	if first.Code != http.StatusCreated {
		t.Fatalf("first status = %d body=%s", first.Code, first.Body.String())
	}
	memory.finishPublication(t, actor, "publication-key", revision)
	preparer.err = evidence.ErrPreviewStale
	replay := post(body)
	if replay.Code != http.StatusOK {
		t.Fatalf("replay status = %d body=%s", replay.Code, replay.Body.String())
	}
	var result struct {
		RecordID   string `json:"record_id"`
		RevisionID string `json:"revision_id"`
		Replayed   bool   `json:"replayed"`
	}
	if err := json.Unmarshal(replay.Body.Bytes(), &result); err != nil || result.RecordID != revision.RecordID || result.RevisionID != revision.RevisionID || !result.Replayed {
		t.Fatalf("replay response = %#v error %v", result, err)
	}
	changed := strings.Replace(body, "evi_0123456789abcdef01234567", "evi_1123456789abcdef01234567", 1)
	assertRecordsHandlerError(t, post(changed), http.StatusConflict, "idempotency_conflict")
	otherScope := actor.Clone()
	otherScope.GroupIDs = []string{"rag_other"}
	request := recordsHandlerRequest(t, otherScope, http.MethodPost, "/api/records", body)
	request.Header.Set("Idempotency-Key", "publication-key")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assertRecordsHandlerError(t, recorder, http.StatusNotFound, "resource_not_found")
	for _, err := range []error{recordauth.ErrDenied, records.ErrRecordDeletionReserved} {
		readErr = err
		assertRecordsHandlerError(t, post(body), http.StatusNotFound, "resource_not_found")
	}
	if creates != 1 || drafts != 1 || preparer.calls != 1 || memory.begins != 1 {
		t.Fatalf("replay effects create/draft/evidence/allocation = %d/%d/%d/%d", creates, drafts, preparer.calls, memory.begins)
	}
}

func TestRecordPublicationHTTPDeniedBeforeAllocation(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	draft := mustRecordsHandlerDraft(t, actor, "", "")
	actor.Role = recordauth.RoleViewer
	options, memory := newHandlerBirthFixture(t)
	contentCalls := 0
	handler := RecordsWithOptions(&recordsHandlerApplicationStub{
		preparePublish: func(context.Context, records.DraftPublishRequest) (records.Draft, error) {
			contentCalls++
			return draft, nil
		},
		createRecord: func(context.Context, records.RecordCreateRequest) (records.RevisionCommitResult, error) {
			contentCalls++
			return records.RevisionCommitResult{}, nil
		},
	}, options)
	body := `{"draft_id":` + strconvQuote(draft.DraftID) + `,"draft_etag":` + strconvQuote(draft.ETag.String()) + `}`
	request := recordsHandlerRequest(t, actor, http.MethodPost, "/api/records", body)
	request.Header.Set("Idempotency-Key", "denied-publication")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assertRecordsHandlerError(t, recorder, http.StatusNotFound, "resource_not_found")
	if memory.begins != 0 || contentCalls != 0 {
		t.Fatalf("denied publication performed allocation/content work %d/%d times", memory.begins, contentCalls)
	}
}
