package enroll_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"houfeng/agent/enroll"
	"houfeng/internal/center/http/handlers"
	"houfeng/internal/contracts/agentapi"
)

func TestClientCheckSyncProtocolReadsActualHealth(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/api/healthz" {
			t.Errorf("unexpected health request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("health check sent sync credentials")
		}
		handlers.Healthz("test")(w, r)
	}))
	defer server.Close()
	if err := enroll.NewClient(server.URL).CheckSyncProtocol(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("health requests=%d, want 1", got)
	}
}

func TestClientCheckSyncProtocolRejectsIncompatibleHealth(t *testing.T) {
	valid := `{"name":"houfeng-center","status":"ok","agent_sync_protocol_revision":2,"agent_sync_replay_window":131072}`
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"old center", 200, `{"name":"houfeng-center","status":"ok","version":"old"}`},
		{"HTML proxy", 200, `<!doctype html><html>login</html>`},
		{"empty", 200, ""},
		{"null", 200, "null"},
		{"array", 200, "[]"},
		{"missing revision", 200, `{"name":"houfeng-center","status":"ok","agent_sync_replay_window":131072}`},
		{"missing window", 200, `{"name":"houfeng-center","status":"ok","agent_sync_protocol_revision":2}`},
		{"old revision", 200, strings.Replace(valid, `revision":2`, `revision":1`, 1)},
		{"future revision", 200, strings.Replace(valid, `revision":2`, `revision":3`, 1)},
		{"wrong window", 200, strings.Replace(valid, "131072", "131071", 1)},
		{"string revision", 200, strings.Replace(valid, `revision":2`, `revision":"2"`, 1)},
		{"fractional revision", 200, strings.Replace(valid, `revision":2`, `revision":2.0`, 1)},
		{"wrong service", 200, strings.Replace(valid, "houfeng-center", "other-service", 1)},
		{"missing service", 200, strings.Replace(valid, `"name":"houfeng-center",`, "", 1)},
		{"unhealthy", 200, strings.Replace(valid, `"ok"`, `"failed"`, 1)},
		{"trailing JSON", 200, valid + `{}`},
		{"trailing HTML", 200, valid + `<html>`},
		{"truncated", 200, valid[:len(valid)-1]},
		{"oversized", 200, valid + strings.Repeat(" ", 64<<10)},
		{"health unavailable", 503, valid},
		{"not found", 404, valid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			if err := enroll.NewClient(server.URL).CheckSyncProtocol(context.Background()); err == nil {
				t.Fatalf("accepted incompatible health: %s", tc.body)
			}
		})
	}
}

func TestClientCheckSyncProtocolPropagatesTransportAndContextErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(handlers.Healthz("test")))
	url := server.URL
	server.Close()
	if err := enroll.NewClient(url).CheckSyncProtocol(context.Background()); err == nil {
		t.Fatal("accepted failed network request")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := enroll.NewClient(url).CheckSyncProtocol(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled health check error=%v", err)
	}
	if err := enroll.NewClient("://invalid").CheckSyncProtocol(context.Background()); err == nil {
		t.Fatal("accepted invalid health URL")
	}
}

func TestClientSyncTransmitsV2Int64IdentityAndDecodesAck(t *testing.T) {
	request := agentapi.SyncRequest{
		ProtocolRevision: agentapi.SyncProtocolRevision,
		SessionID:        "mas_v2", ProducerID: "c393d63b-3128-489f-89ca-109be0de137d",
		MonitoringInstanceID: "mi_v2", SyncToken: "mas_v2.secret",
		BatchSequence: math.MaxInt64, AllocatedThrough: math.MaxInt64, RetiredThrough: math.MaxInt64 - 1,
		LiveSignal: &agentapi.LiveSignal{Sequence: math.MaxInt64, Fingerprint: "fp_v2"},
		Heartbeats: []agentapi.MonitoringInstanceHeartbeat{{
			ObservedAt:   time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC),
			AgentVersion: "test", Fingerprint: "fp_v2", SyncBatchID: "c393d63b-3128-489f-89ca-109be0de137d:9223372036854775807",
		}},
	}
	digest, err := agentapi.CanonicalSyncBatchDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, disposition := range []string{agentapi.SyncDispositionRecorded, agentapi.SyncDispositionExactDuplicate, agentapi.SyncDispositionSuppressed, agentapi.SyncDispositionDeferred} {
		t.Run(disposition, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get(agentapi.SyncProtocolHeader) != "2" {
					t.Error("missing v2 header")
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if strings.Contains(string(body), `"sync_token"`) || strings.Contains(string(body), `"id"`) {
					t.Errorf("secret or old live ID leaked: %s", body)
				}
				var wire agentapi.SyncRequest
				if err := json.Unmarshal(body, &wire); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				if wire.ProtocolRevision != request.ProtocolRevision || wire.SessionID != request.SessionID || wire.ProducerID != request.ProducerID ||
					wire.BatchSequence != request.BatchSequence || wire.AllocatedThrough != request.AllocatedThrough || wire.RetiredThrough != request.RetiredThrough ||
					wire.LiveSignal == nil || wire.LiveSignal.Sequence != math.MaxInt64 {
					t.Errorf("wire lost replay identity: %#v", wire)
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(agentapi.SyncResponse{
					SessionID: request.SessionID, ProducerID: request.ProducerID,
					BatchSequence: request.BatchSequence, BatchDigest: hex.EncodeToString(digest[:]),
					RetiredThrough: request.RetiredThrough, Disposition: disposition, Status: "accepted",
				})
			}))
			defer server.Close()
			response, err := enroll.NewClient(server.URL).Sync(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			if response.SessionID != request.SessionID || response.ProducerID != request.ProducerID || response.BatchSequence != request.BatchSequence ||
				response.BatchDigest != hex.EncodeToString(digest[:]) || response.RetiredThrough != request.RetiredThrough || response.Disposition != disposition {
				t.Fatalf("lost replay acknowledgement: %#v", response)
			}
		})
	}
}

func TestClientSyncPreservesTerminalProtocolErrors(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{
		{http.StatusUpgradeRequired, agentapi.ErrorCodeSyncProtocolRequired},
		{http.StatusConflict, agentapi.ErrorCodeSyncProducerMismatch},
		{http.StatusConflict, agentapi.ErrorCodeSyncBatchRetired},
		{http.StatusConflict, agentapi.ErrorCodeSyncBatchIdentityConflict},
	} {
		t.Run(tc.code, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "30")
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(agentapi.ErrorResponse{Code: tc.code, Message: "terminal"})
			}))
			defer server.Close()
			_, err := enroll.NewClient(server.URL).Sync(context.Background(), agentapi.SyncRequest{ProtocolRevision: agentapi.SyncProtocolRevision, ProducerID: "c393d63b-3128-489f-89ca-109be0de137d", SessionID: "mas_v2", MonitoringInstanceID: "mi_v2", SyncToken: "mas_v2.secret"})
			var remote *enroll.RemoteError
			if !errors.As(err, &remote) || remote.StatusCode != tc.status || remote.Code != tc.code || remote.RetryAfter != 0 {
				t.Fatalf("terminal error changed classification: %#v", err)
			}
		})
	}
}
