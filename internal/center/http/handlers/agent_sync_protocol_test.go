package handlers_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/agentplan"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/syncing"
	"houfeng/internal/contracts/agentapi"
)

func protocolSyncRequest() agentapi.SyncRequest {
	return agentapi.SyncRequest{
		ProtocolRevision: agentapi.SyncProtocolRevision,
		ProducerID:       testSyncProducerID,
		SessionID:        "mas_protocol", MonitoringInstanceID: "mi_protocol",
		BatchSequence: 1, AllocatedThrough: 1,
		Heartbeats: []agentapi.MonitoringInstanceHeartbeat{{
			ObservedAt:   time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC),
			AgentVersion: "test", Fingerprint: "fp_protocol", SyncBatchID: testSyncBatchID,
		}},
	}
}

func serveProtocolSync(t *testing.T, svc *fakeAgentSyncService, payload agentapi.SyncRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, bytes.NewReader(body))
	setSyncAuth(req)
	recorder := httptest.NewRecorder()
	handlers.AgentSync(svc).ServeHTTP(recorder, req)
	return recorder
}

func TestAgentSyncProtocolHeaderRejectedBeforeBodyRead(t *testing.T) {
	for _, headers := range [][]string{nil, {"1"}, {"3"}, {"2", "2"}, {"2,2"}, {" 2 "}, {"02"}, {""}} {
		t.Run(fmt.Sprint(headers), func(t *testing.T) {
			svc := &fakeAgentSyncService{}
			req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, errReader{})
			setSyncAuth(req)
			req.Header.Del(agentapi.SyncProtocolHeader)
			for _, value := range headers {
				req.Header.Add(agentapi.SyncProtocolHeader, value)
			}
			recorder := httptest.NewRecorder()
			handlers.AgentSync(svc).ServeHTTP(recorder, req)
			if recorder.Code != http.StatusUpgradeRequired {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			assertErrorResponse(t, recorder, agentapi.ErrorCodeSyncProtocolRequired, "sync protocol required")
			if svc.syncBatch.SessionID != "" {
				t.Fatal("old protocol reached service")
			}
		})
	}
}

func TestAgentSyncProtocolBodyRevisionRequired(t *testing.T) {
	for _, revision := range []int{0, 1, 3} {
		t.Run(fmt.Sprint(revision), func(t *testing.T) {
			payload := protocolSyncRequest()
			payload.ProtocolRevision = revision
			svc := &fakeAgentSyncService{}
			recorder := serveProtocolSync(t, svc, payload)
			if recorder.Code != http.StatusUpgradeRequired {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			assertErrorResponse(t, recorder, agentapi.ErrorCodeSyncProtocolRequired, "sync protocol required")
			if svc.syncBatch.SessionID != "" {
				t.Fatal("old body revision reached service")
			}
		})
	}
}

func TestAgentSyncProtocolRejectsInvalidReplayControlsAndFactIdentities(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*agentapi.SyncRequest)
	}{
		{"missing producer", func(r *agentapi.SyncRequest) { r.ProducerID = "" }},
		{"opaque producer", func(r *agentapi.SyncRequest) { r.ProducerID = "producer" }},
		{"negative allocated", func(r *agentapi.SyncRequest) { r.AllocatedThrough = -1 }},
		{"negative retired", func(r *agentapi.SyncRequest) { r.RetiredThrough = -1 }},
		{"floor above allocated", func(r *agentapi.SyncRequest) { r.RetiredThrough = 2 }},
		{"negative sequence", func(r *agentapi.SyncRequest) { r.BatchSequence = -1 }},
		{"unallocated sequence", func(r *agentapi.SyncRequest) { r.AllocatedThrough = 0 }},
		{"zero sequence with facts", func(r *agentapi.SyncRequest) { r.BatchSequence = 0 }},
		{"positive sequence without facts", func(r *agentapi.SyncRequest) { r.Heartbeats = nil }},
		{"zero live sequence", func(r *agentapi.SyncRequest) { r.LiveSignal = &agentapi.LiveSignal{Fingerprint: "fp"} }},
		{"negative live sequence", func(r *agentapi.SyncRequest) { r.LiveSignal = &agentapi.LiveSignal{Sequence: -1, Fingerprint: "fp"} }},
		{"missing live fingerprint", func(r *agentapi.SyncRequest) { r.LiveSignal = &agentapi.LiveSignal{Sequence: 1} }},
		{"heartbeat opaque ID", func(r *agentapi.SyncRequest) { r.Heartbeats[0].SyncBatchID = "opaque" }},
		{"heartbeat wrong sequence", func(r *agentapi.SyncRequest) { r.Heartbeats[0].SyncBatchID = testSyncProducerID + ":2" }},
		{"host different ID", func(r *agentapi.SyncRequest) {
			h := r.Heartbeats[0]
			r.HostSamples = []agentapi.HostSamplePayload{{ObservedAt: h.ObservedAt, AgentVersion: h.AgentVersion, Fingerprint: h.Fingerprint, SyncBatchID: "different"}}
		}},
		{"probe different ID", func(r *agentapi.SyncRequest) {
			h := r.Heartbeats[0]
			r.ProbeObservations = []agentapi.ProbeObservationPayload{{ObservedAt: h.ObservedAt, AgentVersion: h.AgentVersion, Fingerprint: h.Fingerprint, SyncBatchID: "different", TargetID: "target", ProbeItemID: "probe", ProbeKind: "tcp", ResultKind: "success"}}
		}},
		{"IP report different ID", func(r *agentapi.SyncRequest) {
			h := r.Heartbeats[0]
			r.IPQualityReports = []agentapi.IPQualityReportPayload{{ObservedAt: h.ObservedAt, AgentVersion: h.AgentVersion, Fingerprint: h.Fingerprint, SyncBatchID: "different", IPAddress: "198.51.100.1", IPVersion: 4, Status: "success"}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := protocolSyncRequest()
			tc.change(&payload)
			svc := &fakeAgentSyncService{}
			recorder := serveProtocolSync(t, svc, payload)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			assertErrorResponse(t, recorder, agentapi.ErrorCodeInvalidRequest, "invalid request")
			if svc.syncBatch.SessionID != "" {
				t.Fatal("invalid replay controls reached service")
			}
		})
	}
}

func TestAgentSyncProtocolRetiredRequestIsTerminalConflict(t *testing.T) {
	payload := protocolSyncRequest()
	payload.RetiredThrough = payload.BatchSequence
	svc := &fakeAgentSyncService{}
	recorder := serveProtocolSync(t, svc, payload)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	assertErrorResponse(t, recorder, agentapi.ErrorCodeSyncBatchRetired, "sync batch retired")
	if svc.syncBatch.SessionID != "" {
		t.Fatal("retired batch reached service")
	}
}

func TestAgentSyncProtocolRejectsOverflowAndOldLiveSignalField(t *testing.T) {
	for _, body := range []string{
		`{"protocol_revision":2,"producer_id":"c393d63b-3128-489f-89ca-109be0de137d","session_id":"mas_1","monitoring_instance_id":"mi_1","allocated_through":9223372036854775808}`,
		`{"protocol_revision":2,"producer_id":"c393d63b-3128-489f-89ca-109be0de137d","session_id":"mas_1","monitoring_instance_id":"mi_1","live_signal":{"id":"old","fingerprint":"fp"}}`,
	} {
		svc := &fakeAgentSyncService{}
		req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(body))
		setSyncAuth(req)
		recorder := httptest.NewRecorder()
		handlers.AgentSync(svc).ServeHTTP(recorder, req)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		assertErrorResponse(t, recorder, agentapi.ErrorCodeInvalidJSON, "invalid json")
		if svc.syncBatch.SessionID != "" {
			t.Fatal("invalid JSON reached service")
		}
	}
}

func TestAgentSyncProtocolMapsDomainErrors(t *testing.T) {
	for _, tc := range []struct {
		err           error
		status        int
		code, message string
	}{
		{syncing.ErrSyncProtocolRequired, http.StatusUpgradeRequired, agentapi.ErrorCodeSyncProtocolRequired, "sync protocol required"},
		{syncing.ErrSyncProducerMismatch, http.StatusConflict, agentapi.ErrorCodeSyncProducerMismatch, "sync producer mismatch"},
		{syncing.ErrSyncBatchRetired, http.StatusConflict, agentapi.ErrorCodeSyncBatchRetired, "sync batch retired"},
		{syncing.ErrSyncBatchIdentityConflict, http.StatusConflict, agentapi.ErrorCodeSyncBatchIdentityConflict, "sync batch identity conflict"},
		{syncing.ErrInvalidReplayState, http.StatusBadRequest, agentapi.ErrorCodeInvalidRequest, "invalid request"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			svc := &fakeAgentSyncService{syncErr: fmt.Errorf("private detail: %w", tc.err)}
			recorder := serveProtocolSync(t, svc, protocolSyncRequest())
			if recorder.Code != tc.status {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			assertErrorResponse(t, recorder, tc.code, tc.message)
		})
	}
}

func TestAgentSyncProtocolMapsComputedDigestAndAcknowledgement(t *testing.T) {
	for _, disposition := range []syncing.ResultDisposition{syncing.ResultDispositionRecorded, syncing.ResultDispositionExactDuplicate, syncing.ResultDispositionSuppressed, syncing.ResultDispositionDeferred} {
		t.Run(string(disposition), func(t *testing.T) {
			payload := protocolSyncRequest()
			payload.BatchSequence, payload.AllocatedThrough = agentapi.SyncReplayWindow+1, agentapi.SyncReplayWindow+1
			payload.Heartbeats[0].SyncBatchID = fmt.Sprintf("%s:%d", testSyncProducerID, payload.BatchSequence)
			payload.LiveSignal = &agentapi.LiveSignal{Sequence: math.MaxInt64, Fingerprint: "fp_protocol"}
			digest, err := agentapi.CanonicalSyncBatchDigest(payload)
			if err != nil {
				t.Fatal(err)
			}
			svc := &fakeAgentSyncService{syncResult: syncing.Result{
				SessionID: payload.SessionID, ProducerID: payload.ProducerID,
				BatchSequence: payload.BatchSequence, BatchDigest: hex.EncodeToString(digest[:]),
				RetiredThrough: 0, Disposition: disposition,
				Plan: agentplan.SyncPlan{PendingAction: &agentplan.PendingAction{ActionID: "action", CommandID: "uptime"}},
			}}
			recorder := serveProtocolSync(t, svc, payload)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if svc.syncBatch.PayloadDigest != digest || svc.syncBatch.ProtocolRevision != payload.ProtocolRevision ||
				svc.syncBatch.ProducerID != payload.ProducerID || svc.syncBatch.BatchSequence != payload.BatchSequence ||
				svc.syncBatch.AllocatedThrough != payload.AllocatedThrough || svc.syncBatch.RetiredThrough != payload.RetiredThrough {
				t.Fatalf("handler lost protocol identity or canonical digest: %#v", svc.syncBatch)
			}
			var response agentapi.SyncResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.SessionID != payload.SessionID || response.ProducerID != payload.ProducerID || response.BatchSequence != payload.BatchSequence ||
				response.BatchDigest != hex.EncodeToString(digest[:]) || response.Disposition != string(disposition) || response.RetiredThrough != 0 {
				t.Fatalf("lost acknowledgement: %#v", response)
			}
			if disposition == syncing.ResultDispositionDeferred && response.Plan != nil {
				t.Fatal("deferred batch exposed a plan")
			}
		})
	}
}

func TestAgentSyncProtocolControlRequestCanRetireWithoutLiveSignal(t *testing.T) {
	payload := protocolSyncRequest()
	payload.Heartbeats = nil
	payload.BatchSequence = 0
	payload.AllocatedThrough, payload.RetiredThrough = math.MaxInt64, math.MaxInt64
	svc := &fakeAgentSyncService{}
	recorder := serveProtocolSync(t, svc, payload)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if svc.syncBatch.RetiredThrough != math.MaxInt64 || svc.syncBatch.BatchSequence != 0 {
		t.Fatal("control request lost retirement boundary")
	}
}
