package store_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/http/handlers"
	"houfeng/internal/center/monitoringinstances"
	"houfeng/internal/center/store"
	"houfeng/internal/center/syncing"
	"houfeng/internal/contracts/agentapi"
)

func TestPostgresIntegrationAgentReplayWindow(t *testing.T) {
	store.RunAgentReplayWindowAcceptance(t, func(pool *pgxpool.Pool, original agentapi.SyncRequest) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if _, err := pool.Exec(ctx, `update monitoring_instances set monitoring_status=$2 where monitoring_instance_id=$1`, original.MonitoringInstanceID, monitoringinstances.MonitoringEnabled); err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(handlers.AgentSync(syncing.NewService(store.NewPostgresSyncRepository(pool))))
		defer server.Close()
		post := func(req agentapi.SyncRequest, protocol string, wantStatus int, wantCode string) agentapi.SyncResponse {
			t.Helper()
			payload, err := json.Marshal(req)
			if err != nil {
				t.Fatal(err)
			}
			httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, bytes.NewReader(payload))
			if err != nil {
				t.Fatal(err)
			}
			httpReq.Header.Set("Authorization", "Bearer "+req.SyncToken)
			httpReq.Header.Set("Content-Type", "application/json")
			if protocol != "" {
				httpReq.Header.Set(agentapi.SyncProtocolHeader, protocol)
			}
			response, err := server.Client().Do(httpReq)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != wantStatus {
				t.Fatalf("HTTP status=%d body=%s want %d", response.StatusCode, body, wantStatus)
			}
			if wantCode != "" {
				var decoded agentapi.ErrorResponse
				if err := json.Unmarshal(body, &decoded); err != nil {
					t.Fatal(err)
				}
				if decoded.Code != wantCode {
					t.Fatalf("error code=%q want %q", decoded.Code, wantCode)
				}
				return agentapi.SyncResponse{}
			}
			var ack agentapi.SyncResponse
			if err := json.Unmarshal(body, &ack); err != nil {
				t.Fatal(err)
			}
			digest, err := agentapi.CanonicalSyncBatchDigest(req)
			if err != nil {
				t.Fatal(err)
			}
			if ack.SessionID != req.SessionID || ack.ProducerID != req.ProducerID || ack.BatchSequence != req.BatchSequence || ack.BatchDigest != hex.EncodeToString(digest[:]) {
				t.Fatalf("HTTP ack identity=%#v", ack)
			}
			return ack
		}
		post(original, "", http.StatusUpgradeRequired, agentapi.ErrorCodeSyncProtocolRequired)
		post(original, "1", http.StatusUpgradeRequired, agentapi.ErrorCodeSyncProtocolRequired)
		var high int64
		if err := pool.QueryRow(ctx, `select live_sequence_high from agent_replay_states where session_id=$1`, original.SessionID).Scan(&high); err != nil {
			t.Fatal(err)
		}
		if high != 104 {
			t.Fatalf("old protocol refreshed live high to %d", high)
		}
		ack := post(original, "2", http.StatusOK, "")
		if ack.Disposition != agentapi.SyncDispositionRecorded {
			t.Fatalf("first HTTP disposition=%q", ack.Disposition)
		}
		ack = post(original, "2", http.StatusOK, "")
		if ack.Disposition != agentapi.SyncDispositionExactDuplicate {
			t.Fatalf("duplicate HTTP disposition=%q", ack.Disposition)
		}
		conflict := original
		conflict.Heartbeats = append([]agentapi.MonitoringInstanceHeartbeat(nil), original.Heartbeats...)
		conflict.Heartbeats[0].AgentVersion = "altered-payload"
		post(conflict, "2", http.StatusConflict, agentapi.ErrorCodeSyncBatchIdentityConflict)
		conflict.ProducerID = "99999999-9999-4999-8999-999999999999"
		conflict.Heartbeats[0].SyncBatchID = conflict.ProducerID + ":" + strconv.FormatInt(conflict.BatchSequence, 10)
		post(conflict, "2", http.StatusConflict, agentapi.ErrorCodeSyncProducerMismatch)
		control := original
		control.BatchSequence = 0
		control.Heartbeats = nil
		control.LiveSignal = nil
		control.RetiredThrough = original.BatchSequence
		ack = post(control, "2", http.StatusOK, "")
		if ack.Disposition != agentapi.SyncDispositionSuppressed || ack.RetiredThrough != control.RetiredThrough {
			t.Fatalf("control ack=%#v", ack)
		}
		post(original, "2", http.StatusConflict, agentapi.ErrorCodeSyncBatchRetired)
	})
}
