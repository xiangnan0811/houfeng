package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"houfeng/agent/enroll"
	"houfeng/agent/syncqueue"
	"houfeng/internal/contracts/agentapi"
)

type cooldownTestClient struct {
	Client
	requests []agentapi.SyncRequest
	sync     func(agentapi.SyncRequest) (*agentapi.SyncResponse, error)
}

func (client *cooldownTestClient) Sync(_ context.Context, request agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
	client.requests = append(client.requests, request)
	return client.sync(request)
}

func cooldownTestRequest(now time.Time) agentapi.SyncRequest {
	return agentapi.SyncRequest{
		SessionID: "session-current", MonitoringInstanceID: "instance-current", SyncToken: "session-current.secret",
		Heartbeats: []agentapi.MonitoringInstanceHeartbeat{{ObservedAt: now, Fingerprint: "fp-current", AgentVersion: "dev"}},
	}
}

func replayTestAck(t *testing.T, request agentapi.SyncRequest, disposition string) *agentapi.SyncResponse {
	t.Helper()
	digest, err := agentapi.CanonicalSyncBatchDigest(request)
	if err != nil {
		t.Fatal(err)
	}
	return &agentapi.SyncResponse{Status: "accepted", SessionID: request.SessionID, ProducerID: request.ProducerID,
		BatchSequence: request.BatchSequence, BatchDigest: fmt.Sprintf("%x", digest), RetiredThrough: request.RetiredThrough, Disposition: disposition}
}

func openReplayTestQueue(t *testing.T) *syncqueue.FileStore {
	t.Helper()
	queue := syncqueue.NewFileStore(filepath.Join(t.TempDir(), "queue.json"), syncqueue.Options{})
	if err := queue.OpenSession(context.Background(), "session-current"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.Close() })
	return queue
}

func TestRuntimeCongestionCooldownSkipsOnlyBacklog(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		for _, targetCurrent := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d_current_%t", status, targetCurrent), func(t *testing.T) {
				ctx := context.Background()
				now := time.Now().UTC()
				start := now
				queue := openReplayTestQueue(t)
				for range 3 {
					if _, err := queue.Enqueue(ctx, cooldownTestRequest(now.Add(-time.Minute))); err != nil {
						t.Fatal(err)
					}
				}
				failedSequence := int64(1)
				if targetCurrent {
					failedSequence = 4
				}
				failed := false
				client := &cooldownTestClient{}
				client.sync = func(request agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
					if !failed && request.BatchSequence == failedSequence {
						failed = true
						return nil, fmt.Errorf("wrapped response: %w", &enroll.RemoteError{StatusCode: status, RetryAfter: 12 * time.Second})
					}
					return replayTestAck(t, request, agentapi.SyncDispositionRecorded), nil
				}
				rt := &Runtime{client: client, syncQueue: queue, now: func() time.Time { return now }, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
				authority := syncAuthority{monitoringInstanceID: "instance-current", syncToken: "session-current.secret", fingerprint: "fp-current"}
				if _, err := rt.enqueueAndFlush(ctx, cooldownTestRequest(now), authority); !errors.Is(err, errRemoteSync) {
					t.Fatalf("initial error=%v", err)
				}
				if !rt.backlogRetryNotBefore.Equal(start.Add(12 * time.Second)) {
					t.Fatalf("deadline=%v", rt.backlogRetryNotBefore)
				}
				if client.requests[len(client.requests)-1].BatchSequence != 4 {
					t.Fatal("backlog rejection blocked current")
				}
				for i, elapsed := range []time.Duration{5 * time.Second, 10 * time.Second} {
					now = start.Add(elapsed)
					before := len(client.requests)
					if _, err := rt.enqueueAndFlush(ctx, cooldownTestRequest(now), authority); err != nil {
						t.Fatal(err)
					}
					if len(client.requests) != before+1 || client.requests[before].BatchSequence != int64(i+5) {
						t.Fatal("cooldown sent backlog or suppressed current")
					}
				}
				entries, err := queue.List(ctx)
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, entry := range entries {
					if entry.Request.BatchSequence == failedSequence {
						found = true
						if entry.Attempts != 1 {
							t.Fatalf("attempts=%d", entry.Attempts)
						}
					}
				}
				if !found {
					t.Fatal("rejected batch was lost")
				}
				now = start.Add(12 * time.Second)
				before := len(client.requests)
				if _, err := rt.enqueueAndFlush(ctx, cooldownTestRequest(now), authority); err != nil {
					t.Fatal(err)
				}
				want := []int64{1, 2, 7}
				if targetCurrent {
					want = []int64{3, 4, 7}
				}
				if len(client.requests) != before+3 {
					t.Fatal("recovery did not send FIFO two plus current")
				}
				for i, sequence := range want {
					request := client.requests[before+i]
					if request.BatchSequence != sequence || request.Heartbeats[0].IsBackfilled != (i < 2) || (request.LiveSignal != nil) != (i == 2) {
						t.Fatalf("invalid recovery request at %d: %#v", i, request)
					}
				}
			})
		}
	}
}

func TestRuntimeCooldownConsumesOnly429And503(t *testing.T) {
	for _, tt := range []struct {
		status      int
		retry, want time.Duration
	}{
		{429, 13 * time.Second, 13 * time.Second}, {503, time.Minute, time.Minute}, {429, 0, 5 * time.Second},
		{400, 13 * time.Second, 0}, {401, 13 * time.Second, 0}, {500, 13 * time.Second, 0}, {502, 13 * time.Second, 0},
	} {
		t.Run(fmt.Sprintf("%d_%s", tt.status, tt.retry), func(t *testing.T) {
			now := time.Now()
			queue := openReplayTestQueue(t)
			if _, err := queue.Enqueue(context.Background(), cooldownTestRequest(now)); err != nil {
				t.Fatal(err)
			}
			entries, err := queue.List(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			client := &cooldownTestClient{sync: func(agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
				return nil, &enroll.RemoteError{StatusCode: tt.status, RetryAfter: tt.retry}
			}}
			rt := &Runtime{client: client, syncQueue: queue, now: func() time.Time { return now }}
			_, _ = rt.syncRequest(context.Background(), entries[0], true)
			if tt.want == 0 {
				if !rt.backlogRetryNotBefore.IsZero() {
					t.Fatal("non-congestion error enabled cooldown")
				}
			} else if !rt.backlogRetryNotBefore.Equal(now.Add(tt.want)) {
				t.Fatalf("deadline=%v", rt.backlogRetryNotBefore)
			}
		})
	}
}
