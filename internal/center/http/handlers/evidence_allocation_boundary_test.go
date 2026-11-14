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
)

const handlerPreviewBody = `{"kind":"monitoring.host","schema_version":1,"source_type":"monitoring_instance","source_id":"mi_0123456789abcdef","requested_window":{"start":"2026-08-16T01:00:00Z","end":"2026-08-16T02:00:00Z"},"metrics":["cpu_usage_pct"],"precision_seconds":60,"sensitive_topology_fields":[]}`

func TestEvidencePreviewHTTPAllocatesAndReturnsReference(t *testing.T) {
	actor := mustRecordsHandlerActor(t)
	options, memory := newHandlerBirthFixture(t)
	var captures []evidence.CapturePreviewRequest
	handler := EvidenceWithOptions(&evidenceHandlerApplicationStub{
		capture: func(_ context.Context, request evidence.CapturePreviewRequest) (evidence.CapturePreviewResult, error) {
			captures = append(captures, request)
			return evidence.CapturePreviewResult{
				RecordID: request.RecordID, SnapshotID: request.SnapshotID,
				Preview: evidence.Preview{IntentID: "evi_0123456789abcdef01234567", Key: request.Selection.Key, Selection: request.Selection},
			}, nil
		},
	}, EvidenceHandlerOptions{Birth: options.Birth, Allocations: options.Allocations, NewSnapshotID: func() (string, error) { return "evs_httpcontract", nil }})
	post := func(actor recordauth.ActorScope, body, key string) *httptest.ResponseRecorder {
		request := evidenceHandlerRequest(t, actor, http.MethodPost, "/api/evidence/capture-previews", strings.NewReader(body))
		if key != "" {
			request.Header.Set("Idempotency-Key", key)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	assertRecordsHandlerError(t, post(actor, handlerPreviewBody, ""), http.StatusBadRequest, "invalid_request")
	viewer := actor.Clone()
	viewer.Role = recordauth.RoleViewer
	assertRecordsHandlerError(t, post(viewer, handlerPreviewBody, "denied-preview"), http.StatusNotFound, "resource_not_found")
	if memory.begins != 0 || len(captures) != 0 {
		t.Fatalf("missing key or denied actor allocated/captured %d/%d times", memory.begins, len(captures))
	}
	first := post(actor, handlerPreviewBody, "preview-key")
	if first.Code != http.StatusCreated {
		t.Fatalf("preview status = %d body=%s", first.Code, first.Body.String())
	}
	var response struct {
		RecordID   string                    `json:"record_id"`
		Allocation *recordAllocationResponse `json:"allocation"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &response); err != nil || response.Allocation == nil {
		t.Fatalf("preview allocation response = %#v error %v", response, err)
	}
	if response.RecordID != response.Allocation.RecordID || recordidentity.Classify(response.RecordID) != recordidentity.Ordinary || response.Allocation.Purpose != recordbirth.PurposeEvidence || len(captures) != 1 || captures[0].AllocationID != response.Allocation.AllocationID {
		t.Fatalf("preview allocation = %#v capture = %#v", response, captures)
	}
	bodyWithAllocation := `{"record_id":` + strconvQuote(response.RecordID) + `,"allocation":` + handlerAllocationJSON(t, *response.Allocation) + `,` + handlerPreviewBody[1:]
	second := post(actor, bodyWithAllocation, "")
	if second.Code != http.StatusCreated || memory.begins != 1 || len(captures) != 2 || captures[1].AllocationID != response.Allocation.AllocationID {
		t.Fatalf("reference preview status=%d body=%s allocations=%d captures=%#v", second.Code, second.Body.String(), memory.begins, captures)
	}
	changed := strings.Replace(handlerPreviewBody, "mi_0123456789abcdef", "mi_1123456789abcdef", 1)
	assertRecordsHandlerError(t, post(actor, changed, "preview-key"), http.StatusConflict, "idempotency_conflict")
	if memory.begins != 1 || len(captures) != 2 {
		t.Fatalf("changed preview allocated/captured %d/%d times", memory.begins, len(captures))
	}
}

func TestEvidencePreviewHTTPRejectsMismatchedAllocation(t *testing.T) {
	for _, test := range []struct {
		name    string
		purpose recordbirth.Purpose
		modify  func(*recordauth.ActorScope, *recordAllocationResponse, *string)
	}{
		{name: "other actor", purpose: recordbirth.PurposeEvidence, modify: func(actor *recordauth.ActorScope, _ *recordAllocationResponse, _ *string) {
			actor.UserID = "usr_1123456789abcdef01234567"
		}},
		{name: "other scope", purpose: recordbirth.PurposeEvidence, modify: func(actor *recordauth.ActorScope, _ *recordAllocationResponse, _ *string) {
			actor.GroupIDs = []string{"rag_other"}
		}},
		{name: "other purpose", purpose: recordbirth.PurposeComparison},
		{name: "other record", purpose: recordbirth.PurposeEvidence, modify: func(_ *recordauth.ActorScope, _ *recordAllocationResponse, id *string) {
			*id = "rec2_00000000000000000000000000000002"
		}},
		{name: "altered reference", purpose: recordbirth.PurposeEvidence, modify: func(_ *recordauth.ActorScope, allocation *recordAllocationResponse, _ *string) {
			allocation.RequestDigest = strings.Repeat("2", 64)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			actor := mustRecordsHandlerActor(t)
			options, memory := newHandlerBirthFixture(t)
			allocation := mustHandlerAllocation(t, RecordsWithOptions(&recordsHandlerApplicationStub{}, options), actor, test.purpose, "preview-allocation")
			recordID := allocation.RecordID
			if test.modify != nil {
				test.modify(&actor, &allocation, &recordID)
			}
			captures := 0
			handler := EvidenceWithOptions(&evidenceHandlerApplicationStub{
				capture: func(context.Context, evidence.CapturePreviewRequest) (evidence.CapturePreviewResult, error) {
					captures++
					return evidence.CapturePreviewResult{}, nil
				},
			}, EvidenceHandlerOptions{Birth: options.Birth, Allocations: options.Allocations})
			body := `{"record_id":` + strconvQuote(recordID) + `,"allocation":` + handlerAllocationJSON(t, allocation) + `,` + handlerPreviewBody[1:]
			request := evidenceHandlerRequest(t, actor, http.MethodPost, "/api/evidence/capture-previews", strings.NewReader(body))
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code < 400 || captures != 0 || memory.begins != 1 {
				t.Fatalf("mismatched preview status=%d body=%s captures=%d allocations=%d", recorder.Code, recorder.Body.String(), captures, memory.begins)
			}
		})
	}
}
