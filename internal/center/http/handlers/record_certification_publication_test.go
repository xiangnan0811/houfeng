package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"houfeng/internal/center/recordbirth"
	"houfeng/internal/center/recordidentity"
	"houfeng/internal/center/records"
)

func TestCompletedCertifiedPublicationHTTPReturnsPendingWhenConfirmationUnavailable(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	draft := mustRecordsHandlerDraft(t, actor, "", "")
	options, memory := newHandlerBirthFixture(t)
	allocationResponse := mustHandlerAllocation(t, RecordsWithOptions(&recordsHandlerApplicationStub{}, options), actor, recordbirth.PurposeCreate, "certificate-allocation")
	allocation := memory.allocations[allocationResponse.AllocationID]
	allocation.Domain = recordidentity.Certifiable
	allocation.RecordID = "rec2_80000000000000000000000000000001"
	allocation.Binding = recordbirth.Binding{
		AuthorityID: "rd-" + strings.Repeat("1", 64), LineageID: "bl-" + strings.Repeat("2", 64),
		DeploymentID: "dp-" + strings.Repeat("3", 64), ProjectID: string(actor.ProjectID),
		Generation: 1, KeyID: "bk-" + strings.Repeat("4", 64), CutoverDigest: sha256.Sum256([]byte("cutover")),
	}
	revision := mustRecordsHandlerRecord(t, actor).Current
	revision.RecordID = allocation.RecordID
	allocation.State = recordbirth.StateFinished
	allocation.Creation = recordbirth.InitialCreation{
		ReservationID: allocation.ReservationID, RecordID: allocation.RecordID, RevisionID: revision.RevisionID,
		AuthorID: actor.UserID, CanonicalHash: revision.Input.CanonicalHash(), RequestFingerprint: sha256.Sum256([]byte("committed-request")),
	}
	commitment, err := allocation.Creation.Commitment()
	if err != nil {
		t.Fatal(err)
	}
	encodeReceipt := func(event recordbirth.Event) []byte {
		t.Helper()
		body, err := recordbirth.EncodeEvent(event)
		if err != nil {
			t.Fatal(err)
		}
		mac := hmac.New(sha256.New, []byte("fixture-receipt-key-32-bytes!!!!"))
		_, _ = mac.Write(body)
		return append(body, mac.Sum(nil)...)
	}
	event := recordbirth.Event{
		Binding: allocation.Binding, Sequence: 1, Kind: recordbirth.EventReserved,
		ReservationID: allocation.ReservationID, RecordID: allocation.RecordID, Request: allocation.Request,
	}
	allocation.ReserveReceipt = encodeReceipt(event)
	event.Sequence, event.Kind = 2, recordbirth.EventPrepared
	event.PreviousHash = sha256.Sum256(allocation.ReserveReceipt)
	event.InitialCommitment = commitment
	allocation.PrepareReceipt = encodeReceipt(event)
	if err := allocation.Validate(); err != nil {
		t.Fatalf("certified projection fixture: %v", err)
	}
	memory.allocations[allocation.AllocationID] = allocation
	memory.requests[handlerBirthRequestKey(allocation.Request)] = allocation
	input := recordPublishRequest{DraftID: draft.DraftID, DraftETag: draft.ETag.String()}
	digest, err := recordPublicationDigest(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := memory.BindPublication(context.Background(), actor, allocation.AllocationID, "finished-publication", digest); err != nil {
		t.Fatal(err)
	}
	handler := RecordsWithOptions(&recordsHandlerApplicationStub{
		getRevision: func(context.Context, records.RecordRevisionGetRequest) (records.RecordRevision, error) {
			return revision, nil
		},
		preparePublish: func(context.Context, records.DraftPublishRequest) (records.Draft, error) {
			t.Fatal("completed publication read draft")
			return records.Draft{}, nil
		},
		createRecord: func(context.Context, records.RecordCreateRequest) (records.RevisionCommitResult, error) {
			t.Fatal("completed publication created again")
			return records.RevisionCommitResult{}, nil
		},
	}, options)
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	request := recordsHandlerRequest(t, actor, http.MethodPost, "/api/records", string(body))
	request.Header.Set("Idempotency-Key", "finished-publication")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	var response struct {
		RecordID             string `json:"record_id"`
		RevisionID           string `json:"revision_id"`
		Replayed             bool   `json:"replayed"`
		CertificationPending bool   `json:"certification_pending"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); recorder.Code != http.StatusOK || err != nil || response.RecordID != allocation.RecordID || response.RevisionID != revision.RevisionID || !response.Replayed || !response.CertificationPending {
		t.Fatalf("pending confirmation response status=%d body=%s parsed=%#v error=%v", recorder.Code, recorder.Body.String(), response, err)
	}
}
