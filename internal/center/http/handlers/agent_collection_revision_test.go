package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"houfeng/internal/center/http/handlers"
	"houfeng/internal/contracts/agentapi"
)

func TestAgentSyncCollectionRevisionHeaderIsTransientStrictAndLiveOnly(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers []string
		live    bool
		want    int
	}{
		{"capable", []string{"5"}, true, 5},
		{"missing", nil, true, 0},
		{"duplicate", []string{"5", "5"}, true, 0},
		{"combined", []string{"5,5"}, true, 0},
		{"old", []string{"4"}, true, 0},
		{"future", []string{"6"}, true, 0},
		{"whitespace", []string{" 5 "}, true, 0},
		{"invalid", []string{"true"}, true, 0},
		{"backfill", []string{"5"}, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{` + testSyncBatchFields + `"monitoring_instance_id":"mi_1","session_id":"mas_1","heartbeats":[{"observed_at":"2026-10-01T00:00:00Z","agent_version":"test","fingerprint":"fp_1","sync_batch_id":"c393d63b-3128-489f-89ca-109be0de137d:1"}]`
			if tc.live {
				body += `,"live_signal":{"sequence":1,"fingerprint":"fp_1"}`
			}
			body += `}`
			req := httptest.NewRequest(http.MethodPost, agentapi.SyncPath, strings.NewReader(body))
			setSyncAuth(req)
			for _, header := range tc.headers {
				req.Header.Add(agentapi.IPQualityCollectionRevisionHeader, header)
			}
			svc := &fakeAgentSyncService{}
			recorder := httptest.NewRecorder()
			handlers.AgentSync(svc).ServeHTTP(recorder, req)
			if recorder.Code != http.StatusOK {
				t.Fatalf("capability must not reject unrelated monitoring: %d %s", recorder.Code, recorder.Body.String())
			}
			if svc.syncBatch.IPQualityCollectionRevision != tc.want {
				t.Fatalf("revision=%d want=%d", svc.syncBatch.IPQualityCollectionRevision, tc.want)
			}
		})
	}
}
