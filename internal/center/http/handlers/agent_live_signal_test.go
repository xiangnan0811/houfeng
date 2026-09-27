package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/syncing"
	"houfeng/internal/contracts/agentapi"
)

func TestAgentSyncMinimalOnlineSignalDoesNotRequirePerformanceOrRawHeartbeat(t *testing.T) {
	svc := &fakeAgentSyncService{syncResult: syncing.Result{StopCollection: true}}
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{"monitoring_instance_id":"mi_1","session_id":"mas_1","live_signal":{"id":"live_1","fingerprint":"fp_1"}}`))
	setSyncAuth(req)
	recorder := httptest.NewRecorder()
	handlers.AgentSync(svc).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if svc.syncBatch.SessionID != "mas_1" || svc.syncBatch.LiveSignal == nil || svc.syncBatch.LiveSignal.ID != "live_1" {
		t.Fatal("handler lost session live identity")
	}
	var response agentapi.SyncResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.StopCollection {
		t.Fatal("handler lost stop_collection instruction")
	}
}

func TestAgentSyncRejectsMissingSessionIdentity(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(`{"monitoring_instance_id":"mi_1","live_signal":{"id":"live_1","fingerprint":"fp_1"}}`))
	setSyncAuth(req)
	recorder := httptest.NewRecorder()
	handlers.AgentSync(&fakeAgentSyncService{}).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", recorder.Code)
	}
}
