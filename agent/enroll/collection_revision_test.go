package enroll_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"houfeng/agent/enroll"
	"houfeng/internal/contracts/agentapi"
)

func TestSyncAdvertisesCollectionRevisionOnlyForLiveTransmission(t *testing.T) {
	for _, live := range []bool{false, true} {
		t.Run(map[bool]string{false: "backfill", true: "live"}[live], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				want := ""
				if live {
					want = "5"
				}
				if r.Header.Get(agentapi.IPQualityCollectionRevisionHeader) != want {
					t.Errorf("header=%q want=%q", r.Header.Get(agentapi.IPQualityCollectionRevisionHeader), want)
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				if strings.Contains(string(body), "collection_revision") || strings.Contains(string(body), "collect_ip_versions") {
					t.Errorf("capability entered strict sync wire: %s", body)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"status":"accepted"}`)
			}))
			defer server.Close()
			request := agentapi.SyncRequest{SessionID: "mas_1", MonitoringInstanceID: "mi_1", SyncToken: "secret"}
			if live {
				request.LiveSignal = &agentapi.LiveSignal{ID: "live", Fingerprint: "fp"}
			}
			stored, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(stored), "collection_revision") {
				t.Fatal("capability persisted in request JSON")
			}
			if _, err := enroll.NewClient(server.URL).Sync(context.Background(), request); err != nil {
				t.Fatal(err)
			}
		})
	}
}
