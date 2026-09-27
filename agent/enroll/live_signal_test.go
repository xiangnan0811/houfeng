package enroll_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"houfeng/agent/enroll"
	"houfeng/internal/contracts/agentapi"
)

func TestClientTransmitsSessionLiveEvidenceAndDecodesStop(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if _, found := payload["sync_token"]; found {
			t.Error("sync secret leaked into body")
		}
		var session string
		_ = json.Unmarshal(payload["session_id"], &session)
		var signal agentapi.LiveSignal
		_ = json.Unmarshal(payload["live_signal"], &signal)
		if session != "mas_1" || signal.ID != "live_1" || signal.Fingerprint != "fp_1" {
			t.Errorf("transport lost session/live identity: %s", payload)
		}
		if r.Header.Get("Authorization") != "Bearer mas_1.secret" {
			t.Error("missing session authorization")
		}
		_ = json.NewEncoder(w).Encode(agentapi.SyncResponse{Status: "accepted", StopCollection: true})
	}))
	defer server.Close()
	response, err := enroll.NewClient(server.URL).Sync(context.Background(), agentapi.SyncRequest{SessionID: "mas_1", MonitoringInstanceID: "mi_1", SyncToken: "mas_1.secret", LiveSignal: &agentapi.LiveSignal{ID: "live_1", Fingerprint: "fp_1"}})
	if err != nil {
		t.Fatal(err)
	}
	if !response.StopCollection {
		t.Fatal("stop_collection response lost")
	}
}
