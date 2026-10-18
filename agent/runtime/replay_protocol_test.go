package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentconfig "houfeng/agent/config"
	"houfeng/agent/enroll"
	agentexec "houfeng/agent/exec"
	"houfeng/agent/syncqueue"
	"houfeng/internal/contracts/agentapi"
)

type replayRuntimeClient struct {
	protocol               func(context.Context) error
	sync                   func(agentapi.SyncRequest) (*agentapi.SyncResponse, error)
	checks, enrolls, sends int
}

func (c *replayRuntimeClient) CheckSyncProtocol(ctx context.Context) error {
	c.checks++
	if c.protocol != nil {
		return c.protocol(ctx)
	}
	return nil
}
func (c *replayRuntimeClient) Enroll(context.Context, agentapi.EnrollmentRequest) (*agentapi.EnrollmentResponse, error) {
	c.enrolls++
	return nil, errors.New("unexpected reenrollment")
}
func (c *replayRuntimeClient) Sync(_ context.Context, request agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
	c.sends++
	return c.sync(request)
}

type replayCredentials struct{}

func (replayCredentials) Token(context.Context) (string, error) { return "unused", nil }
func (replayCredentials) SyncCredentials(context.Context) (string, string, bool, error) {
	return "instance-current", "session-current.secret", true, nil
}
func (replayCredentials) SaveSyncCredentials(context.Context, string, string) error {
	return errors.New("unexpected credential replacement")
}

type replayFingerprint struct{}

func (replayFingerprint) Fingerprint(context.Context) (string, error) { return "fp-current", nil }

func replayRuntime(t *testing.T, client Client, queue SyncQueue) *Runtime {
	t.Helper()
	return NewWithRuntimeDeps(agentconfig.AgentConfig{ServerURL: "http://center"}, slog.New(slog.NewTextHandler(io.Discard, nil)), client, replayCredentials{}, replayFingerprint{}, nil, nil, time.Millisecond, queue)
}

func replayRunContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx, cancel
}

func TestRuntimeProtocolProbePrecedesAllQueueChangesAndSends(t *testing.T) {
	for _, body := range []string{
		`<html>proxy login</html>`,
		`{"name":"houfeng-center","status":"ok"}`,
		`{"name":"houfeng-center","status":"ok","agent_sync_protocol_revision":1,"agent_sync_replay_window":131072}`,
		`{"name":"houfeng-center","status":"ok","agent_sync_protocol_revision":2,"agent_sync_replay_window":42}`,
	} {
		t.Run(body, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/api/healthz" {
					t.Errorf("unexpected send before compatibility admission: %s", r.URL.Path)
				}
				_, _ = io.WriteString(w, body)
			}))
			defer server.Close()
			path := filepath.Join(t.TempDir(), "queue.json")
			before := []byte("[]\n")
			if err := os.WriteFile(path, before, 0o600); err != nil {
				t.Fatal(err)
			}
			queue := syncqueue.NewFileStore(path, syncqueue.Options{})
			rt := replayRuntime(t, enroll.NewClient(server.URL), queue)
			if err := rt.Run(context.Background()); err == nil {
				t.Fatal("incompatible health admitted")
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) || calls != 1 {
				t.Fatal("queue changed or additional request escaped failed probe")
			}
			if _, err := os.Stat(path + ".lock"); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("queue opened before probe: %v", err)
			}
		})
	}
	t.Run("network error", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "queue.json")
		cause := errors.New("unavailable")
		client := &replayRuntimeClient{protocol: func(context.Context) error { return cause }}
		if err := replayRuntime(t, client, syncqueue.NewFileStore(path, syncqueue.Options{})).Run(context.Background()); !errors.Is(err, cause) {
			t.Fatalf("error=%v", err)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("failed probe initialized queue")
		}
		if client.sends != 0 || client.enrolls != 0 {
			t.Fatal("network probe failure fell through")
		}
	})
}

func TestRuntimeRequiresDurableQueue(t *testing.T) {
	client := &replayRuntimeClient{}
	if err := replayRuntime(t, client, nil).Run(context.Background()); err == nil {
		t.Fatal("nil queue admitted")
	}
	if client.checks+client.enrolls+client.sends != 0 {
		t.Fatal("invalid runtime made remote requests")
	}
}

func TestRuntimeTerminalReplayErrorsRetainQueueAndReleaseLifetimeLock(t *testing.T) {
	for _, remote := range []*enroll.RemoteError{
		{StatusCode: 426, Code: agentapi.ErrorCodeSyncProtocolRequired},
		{StatusCode: 409, Code: agentapi.ErrorCodeSyncProducerMismatch},
		{StatusCode: 409, Code: agentapi.ErrorCodeSyncBatchRetired},
		{StatusCode: 409, Code: agentapi.ErrorCodeSyncBatchIdentityConflict},
	} {
		t.Run(remote.Code, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "queue.json")
			queue := syncqueue.NewFileStore(path, syncqueue.Options{})
			client := &replayRuntimeClient{sync: func(request agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
				other := syncqueue.NewFileStore(path, syncqueue.Options{})
				if err := other.OpenSession(context.Background(), request.SessionID); err == nil {
					_ = other.Close()
					t.Fatal("Run did not hold process lifetime lock")
				}
				return nil, remote
			}}
			ctx, _ := replayRunContext(t)
			if err := replayRuntime(t, client, queue).Run(ctx); !errors.Is(err, remote) {
				t.Fatalf("terminal error=%v", err)
			}
			if client.sends != 1 || client.enrolls != 0 {
				t.Fatal("terminal replay error retried or reenrolled")
			}
			other := syncqueue.NewFileStore(path, syncqueue.Options{})
			if err := other.OpenSession(context.Background(), "session-current"); err != nil {
				t.Fatalf("Run leaked queue lock: %v", err)
			}
			defer other.Close()
			entries, err := other.List(context.Background())
			if err != nil || len(entries) != 1 {
				t.Fatalf("retained entries=%d error=%v", len(entries), err)
			}
		})
	}
}

func TestRuntimeRejectsUnboundAcknowledgementsBeforeDeleteAndPlan(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*agentapi.SyncResponse)
	}{
		{"session", func(r *agentapi.SyncResponse) { r.SessionID = "other" }},
		{"producer", func(r *agentapi.SyncResponse) { r.ProducerID = "other" }},
		{"sequence", func(r *agentapi.SyncResponse) { r.BatchSequence++ }},
		{"digest", func(r *agentapi.SyncResponse) { r.BatchDigest = "00" }},
		{"disposition", func(r *agentapi.SyncResponse) { r.Disposition = "accepted" }},
		{"nil", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "queue.json")
			queue := syncqueue.NewFileStore(path, syncqueue.Options{})
			client := &replayRuntimeClient{sync: func(request agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
				if tt.mutate == nil {
					return nil, nil
				}
				ack := replayTestAck(t, request, agentapi.SyncDispositionRecorded)
				ack.Plan = &agentapi.SyncPlan{HostSampleFrequencyTier: "5s"}
				tt.mutate(ack)
				return ack, nil
			}}
			rt := replayRuntime(t, client, queue)
			ctx, _ := replayRunContext(t)
			if err := rt.Run(ctx); err == nil {
				t.Fatal("invalid acknowledgement accepted")
			}
			if rt.currentPlan != nil || client.sends != 1 {
				t.Fatal("invalid acknowledgement applied plan or continued sending")
			}
			if err := queue.OpenSession(context.Background(), "session-current"); err != nil {
				t.Fatal(err)
			}
			defer queue.Close()
			entries, err := queue.List(context.Background())
			if err != nil || len(entries) != 1 {
				t.Fatalf("entries=%d error=%v", len(entries), err)
			}
		})
	}
}

func TestRuntimeDeferredRetainsFactsAndDoesNotApplyPlan(t *testing.T) {
	queue := openReplayTestQueue(t)
	ctx := context.Background()
	client := &replayRuntimeClient{sync: func(request agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
		ack := replayTestAck(t, request, agentapi.SyncDispositionDeferred)
		ack.Plan = &agentapi.SyncPlan{HostSampleFrequencyTier: "5s", PendingAction: &agentapi.PendingAction{CommandID: "uptime"}}
		ack.StopCollection = true
		return ack, nil
	}}
	rt := replayRuntime(t, client, queue)
	authority := syncAuthority{monitoringInstanceID: "instance-current", syncToken: "session-current.secret", fingerprint: "fp-current"}
	if _, err := rt.enqueueAndFlush(ctx, cooldownTestRequest(time.Now()), authority); !errors.Is(err, errRemoteSync) {
		t.Fatalf("deferred error=%v", err)
	}
	if _, err := rt.enqueueAndFlush(ctx, cooldownTestRequest(time.Now()), authority); !errors.Is(err, errRemoteSync) {
		t.Fatalf("second deferred error=%v", err)
	}
	if client.sends != 3 {
		t.Fatalf("deferred backlog blocked current: sends=%d", client.sends)
	}
	if rt.collectionStopped || rt.currentPlan != nil || len(rt.pendingResults) != 0 {
		t.Fatal("deferred applied stop or command plan")
	}
	entries, err := queue.List(ctx)
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries=%d error=%v", len(entries), err)
	}
}

func TestRuntimeRefreshesDurableFloorBeforeEachTransmission(t *testing.T) {
	queue := openReplayTestQueue(t)
	ctx := context.Background()
	for range 3 {
		if _, err := queue.Enqueue(ctx, cooldownTestRequest(time.Now())); err != nil {
			t.Fatal(err)
		}
	}
	var requests []agentapi.SyncRequest
	client := &replayRuntimeClient{sync: func(request agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
		requests = append(requests, request)
		return replayTestAck(t, request, agentapi.SyncDispositionRecorded), nil
	}}
	rt := replayRuntime(t, client, queue)
	authority := syncAuthority{monitoringInstanceID: "instance-current", syncToken: "session-current.secret", fingerprint: "fp-current"}
	if _, err := rt.enqueueAndFlush(ctx, cooldownTestRequest(time.Now()), authority); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 3 {
		t.Fatalf("sends=%d", len(requests))
	}
	for i, request := range requests {
		if request.RetiredThrough != int64(i) || request.AllocatedThrough != 4 {
			t.Fatalf("stale controls on send %d: %#v", i, request)
		}
		if (request.LiveSignal != nil) != (i == 2) {
			t.Fatal("backfill carried live evidence or current lacked it")
		}
	}
	if requests[0].BatchSequence != 1 || requests[1].BatchSequence != 2 || requests[2].BatchSequence != 4 {
		t.Fatal("FIFO two/current interleaving changed")
	}
}

func TestRuntimeStoppedControlDoesNotAllocateBatchAndRetiresSuppressedQueue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	queue := syncqueue.NewFileStore(path, syncqueue.Options{})
	ctx, cancel := replayRunContext(t)
	client := &replayRuntimeClient{}
	client.sync = func(request agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
		if client.sends == 1 {
			ack := replayTestAck(t, request, agentapi.SyncDispositionSuppressed)
			ack.StopCollection = true
			return ack, nil
		}
		if request.BatchSequence != 0 || request.AllocatedThrough != 1 || request.RetiredThrough != 1 ||
			len(request.Heartbeats)+len(request.HostSamples)+len(request.ProbeObservations)+len(request.CommandResults)+len(request.IPQualityReports) != 0 || request.LiveSignal == nil {
			t.Fatalf("stopped control changed replay/fact contract: %#v", request)
		}
		cancel()
		return nil, context.Canceled
	}
	if err := replayRuntime(t, client, queue).Run(ctx); err != nil {
		t.Fatal(err)
	}
	if client.sends != 2 {
		t.Fatalf("sends=%d", client.sends)
	}
	if err := queue.OpenSession(context.Background(), "session-current"); err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	state, err := queue.ReplayState(context.Background())
	if err != nil || state.AllocatedThrough != 1 || state.RetiredThrough != 1 {
		t.Fatalf("state=%#v error=%v", state, err)
	}
}

// Faults decorate actual file mutations. FileStore's own tests inject real
// pre/post-rename failures; this verifies Runtime's handling at every boundary.
type ambiguousRuntimeQueue struct {
	*syncqueue.FileStore
	operation          string
	tripped            bool
	afterFatal, closes int
	cancel             context.CancelFunc
}

func (q *ambiguousRuntimeQueue) enter() {
	if q.tripped {
		q.afterFatal++
	}
}
func (q *ambiguousRuntimeQueue) fail(operation string, err error) error {
	if err != nil {
		return err
	}
	if operation == q.operation {
		q.tripped = true
		if q.cancel != nil {
			q.cancel()
		}
		return fmt.Errorf("injected committed mutation: %w", syncqueue.ErrAmbiguousDurability)
	}
	return nil
}
func (q *ambiguousRuntimeQueue) Enqueue(ctx context.Context, request agentapi.SyncRequest) (string, error) {
	q.enter()
	id, err := q.FileStore.Enqueue(ctx, request)
	return id, q.fail("enqueue", err)
}
func (q *ambiguousRuntimeQueue) Prune(ctx context.Context) error {
	q.enter()
	return q.fail("prune", q.FileStore.Prune(ctx))
}
func (q *ambiguousRuntimeQueue) ReserveLiveSequences(ctx context.Context, count int64) (int64, int64, error) {
	q.enter()
	first, last, err := q.FileStore.ReserveLiveSequences(ctx, count)
	return first, last, q.fail("reserve", err)
}
func (q *ambiguousRuntimeQueue) ReplayState(ctx context.Context) (syncqueue.ReplayState, error) {
	q.enter()
	return q.FileStore.ReplayState(ctx)
}
func (q *ambiguousRuntimeQueue) List(ctx context.Context) ([]syncqueue.Entry, error) {
	q.enter()
	return q.FileStore.List(ctx)
}
func (q *ambiguousRuntimeQueue) MarkAttempt(ctx context.Context, id string) error {
	q.enter()
	return q.fail("attempt", q.FileStore.MarkAttempt(ctx, id))
}
func (q *ambiguousRuntimeQueue) Delete(ctx context.Context, id string) error {
	q.enter()
	return q.fail("delete", q.FileStore.Delete(ctx, id))
}
func (q *ambiguousRuntimeQueue) DeleteMany(ctx context.Context, ids []string) error {
	q.enter()
	return q.fail("delete_many", q.FileStore.DeleteMany(ctx, ids))
}
func (q *ambiguousRuntimeQueue) Close() error { q.closes++; return q.FileStore.Close() }

func TestRuntimeAmbiguousDurabilityStopsBeforeAnyLaterMutationOrSend(t *testing.T) {
	for _, operation := range []string{"enqueue", "prune", "reserve", "attempt", "delete", "delete_many"} {
		for _, cancelTogether := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s_cancel_%t", operation, cancelTogether), func(t *testing.T) {
				ctx, cancel := replayRunContext(t)
				queue := &ambiguousRuntimeQueue{FileStore: syncqueue.NewFileStore(filepath.Join(t.TempDir(), "queue.json"), syncqueue.Options{}), operation: operation}
				if cancelTogether {
					queue.cancel = cancel
				}
				client := &replayRuntimeClient{sync: func(request agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
					if operation == "attempt" {
						return nil, errors.New("lost response")
					}
					ack := replayTestAck(t, request, agentapi.SyncDispositionRecorded)
					ack.StopCollection = operation == "delete_many"
					return ack, nil
				}}
				rt := replayRuntime(t, client, queue)
				rt.pendingResults = []agentexec.Result{{ActionID: "action", CommandID: "uptime", Stdout: "result"}}
				err := rt.Run(ctx)
				if !errors.Is(err, syncqueue.ErrAmbiguousDurability) {
					t.Fatalf("error=%v", err)
				}
				if !queue.tripped || queue.afterFatal != 0 || queue.closes != 1 {
					t.Fatalf("tripped=%t after=%d closes=%d", queue.tripped, queue.afterFatal, queue.closes)
				}
				sends, checks := client.sends, client.checks
				if err := rt.Run(context.Background()); !errors.Is(err, syncqueue.ErrAmbiguousDurability) {
					t.Fatal("same runtime resumed after ambiguous commit")
				}
				if client.sends != sends || client.checks != checks || queue.afterFatal != 0 {
					t.Fatal("poisoned runtime performed later work")
				}
				if (operation == "enqueue" || operation == "prune" || operation == "reserve") && sends != 0 {
					t.Fatal("sent after pre-send durability fault")
				}
			})
		}
	}
}

type deleteFailureReplayQueue struct {
	*syncqueue.FileStore
	err error
}

func (q *deleteFailureReplayQueue) Delete(context.Context, string) error { return q.err }

func TestRuntimeRestartReplaysLostAckWithSameDigestAndSkipsLiveReservation(t *testing.T) {
	for _, lostResponse := range []bool{true, false} {
		t.Run(fmt.Sprintf("lost_response_%t", lostResponse), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "queue.json")
			firstStore := syncqueue.NewFileStore(path, syncqueue.Options{})
			ctx, cancel := replayRunContext(t)
			deleteErr := errors.New("ack delete unavailable")
			var original agentapi.SyncRequest
			firstClient := &replayRuntimeClient{sync: func(request agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
				original = request
				if lostResponse {
					cancel()
					return nil, context.Canceled
				}
				ack := replayTestAck(t, request, agentapi.SyncDispositionRecorded)
				ack.Plan = &agentapi.SyncPlan{HostSampleFrequencyTier: "5s"}
				return ack, nil
			}}
			var firstQueue SyncQueue = firstStore
			if !lostResponse {
				firstQueue = &deleteFailureReplayQueue{FileStore: firstStore, err: deleteErr}
			}
			firstRuntime := replayRuntime(t, firstClient, firstQueue)
			firstRuntime.pendingResults = []agentexec.Result{{ActionID: "action", CommandID: "uptime", Stdout: "preserved"}}
			err := firstRuntime.Run(ctx)
			if lostResponse && err != nil {
				t.Fatal(err)
			}
			if !lostResponse && !errors.Is(err, deleteErr) {
				t.Fatalf("delete failure=%v", err)
			}
			if firstRuntime.currentPlan != nil || firstClient.sends != 1 {
				t.Fatal("uncommitted ack applied plan or continued")
			}

			secondStore := syncqueue.NewFileStore(path, syncqueue.Options{})
			secondCtx, secondCancel := replayRunContext(t)
			secondClient := &replayRuntimeClient{}
			secondClient.sync = func(request agentapi.SyncRequest) (*agentapi.SyncResponse, error) {
				if secondClient.sends == 1 {
					if request.BatchSequence != original.BatchSequence || request.ProducerID != original.ProducerID ||
						request.LiveSignal != nil || !request.Heartbeats[0].IsBackfilled ||
						len(request.CommandResults) != 1 || request.CommandResults[0].Stdout != "preserved" {
						t.Fatal("restart changed durable batch identity/payload")
					}
					before, beforeErr := agentapi.CanonicalSyncBatchDigest(original)
					after, afterErr := agentapi.CanonicalSyncBatchDigest(request)
					if beforeErr != nil || afterErr != nil || before != after {
						t.Fatal("backfill retry changed canonical digest")
					}
					return replayTestAck(t, request, agentapi.SyncDispositionExactDuplicate), nil
				}
				if request.BatchSequence != 2 || request.RetiredThrough != 1 || request.LiveSignal == nil ||
					request.LiveSignal.Sequence != agentapi.LiveSequenceReservation+1 || len(request.CommandResults) != 0 {
					t.Fatal("restart reused reserved live values or pending memory payload")
				}
				entries, listErr := secondStore.List(context.Background())
				if listErr != nil || len(entries) != 1 || entries[0].Request.BatchSequence != 2 {
					t.Fatal("exact duplicate was not durably acknowledged before current send")
				}
				secondCancel()
				return nil, context.Canceled
			}
			if err := replayRuntime(t, secondClient, secondStore).Run(secondCtx); err != nil {
				t.Fatal(err)
			}
			if secondClient.sends != 2 || secondClient.enrolls != 0 {
				t.Fatal("restart did not replay then send current using persisted authority")
			}
		})
	}
}

func TestRuntimeCapacityFailureRetainsPendingAuxiliaryPayloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	queue := syncqueue.NewFileStore(path, syncqueue.Options{MaxBytes: 800})
	if err := queue.OpenSession(context.Background(), "session-current"); err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	client := &replayRuntimeClient{}
	rt := replayRuntime(t, client, queue)
	rt.pendingResults = []agentexec.Result{{ActionID: "action", CommandID: "uptime", Stdout: string(bytes.Repeat([]byte("x"), 2048))}}
	rt.pendingIPReports = []agentapi.IPQualityReportPayload{{IPAddress: "192.0.2.1", Status: agentapi.IPQualityStatusSuccess}}
	request := rt.buildSyncRequest(context.Background(), "instance-current", "session-current.secret", time.Now(), "fp-current", "unallocated")
	authority := syncAuthority{monitoringInstanceID: "instance-current", syncToken: "session-current.secret", fingerprint: "fp-current"}
	if _, err := rt.enqueueAndFlush(context.Background(), request, authority); err == nil {
		t.Fatal("oversize entry admitted")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || client.sends != 0 || len(rt.pendingResults) != 1 || len(rt.pendingIPReports) != 1 {
		t.Fatal("failed enqueue consumed authority or pending payload")
	}
	state, err := queue.ReplayState(context.Background())
	if err != nil || state.AllocatedThrough != 0 {
		t.Fatal("failed enqueue allocated sequence")
	}
}

type canceledSessionReplayQueue struct {
	*syncqueue.FileStore
	cancel    context.CancelFunc
	ambiguous bool
}

func (q *canceledSessionReplayQueue) OpenSession(ctx context.Context, session string) error {
	q.cancel()
	if q.ambiguous {
		return syncqueue.ErrAmbiguousDurability
	}
	return q.FileStore.OpenSession(ctx, session)
}

func TestRuntimeSessionAdmissionCancellationIsCleanUnlessDurabilityIsAmbiguous(t *testing.T) {
	for _, ambiguous := range []bool{false, true} {
		t.Run(fmt.Sprintf("ambiguous_%t", ambiguous), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			queue := &canceledSessionReplayQueue{
				FileStore: syncqueue.NewFileStore(filepath.Join(t.TempDir(), "queue.json"), syncqueue.Options{}),
				cancel:    cancel,
				ambiguous: ambiguous,
			}
			client := &replayRuntimeClient{}
			err := replayRuntime(t, client, queue).Run(ctx)
			if ambiguous {
				if !errors.Is(err, syncqueue.ErrAmbiguousDurability) {
					t.Fatalf("ambiguous admission masked by cancellation: %v", err)
				}
			} else if err != nil {
				t.Fatalf("parent cancellation during session admission: %v", err)
			}
			if client.sends != 0 || client.enrolls != 0 {
				t.Fatal("canceled session admission performed later remote work")
			}
		})
	}
}

func TestRuntimeLegacyQueueDrainGuidanceIsVisibleAndRedacted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "buffer-path-secret.json")
	request := cooldownTestRequest(time.Now().UTC())
	request.SyncToken = "legacy-token-secret"
	request.Heartbeats[0].Fingerprint = "legacy-fingerprint-secret"
	request.Heartbeats[0].SyncBatchID = "legacy-batch-secret"
	request.CommandResults = []agentapi.CommandResult{{ActionID: "legacy-action-secret", Stdout: "legacy-output-secret"}}
	payload, err := json.Marshal([]syncqueue.Entry{{
		ID:        "legacy-entry-secret",
		CreatedAt: time.Now().UTC(),
		Request:   request,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	client := &replayRuntimeClient{}
	queue := syncqueue.NewFileStore(path, syncqueue.Options{})
	rt := replayRuntime(t, client, queue)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	rt.logger = logger
	err = rt.Run(context.Background())
	if !errors.Is(err, syncqueue.ErrLegacyQueueNotDrained) {
		t.Fatalf("legacy drain sentinel missing from runtime error: %v", err)
	}
	// Match cmd/houfeng-agent's structured logging of Run's returned error.
	logger.Error("run agent runtime", "error", err)
	const guidance = "restore previous agent and drain before upgrading"
	if !strings.Contains(err.Error(), guidance) || !strings.Contains(logs.String(), guidance) {
		t.Fatal("operator-visible error or log omitted the safe drain instruction")
	}
	diagnostic := err.Error() + logs.String()
	for _, secret := range []string{
		path, "buffer-path-secret", "legacy-token-secret", "legacy-fingerprint-secret",
		"legacy-batch-secret", "legacy-action-secret", "legacy-output-secret", "legacy-entry-secret",
	} {
		if strings.Contains(diagnostic, secret) {
			t.Fatal("drain diagnostic exposed a queue path, payload or identity")
		}
	}
	if client.sends != 0 || client.enrolls != 0 || client.checks != 1 {
		t.Fatal("legacy admission sent facts, replaced credentials or skipped compatibility admission")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, payload) {
		t.Fatal("legacy queue bytes changed before the explicit old-agent drain")
	}
}

func TestRuntimeQueueErrorGuidanceRequiresLegacySentinel(t *testing.T) {
	const guidance = "restore previous agent and drain before upgrading"
	for _, tt := range []struct {
		name          string
		cause         error
		wantGuidance  bool
		wantAmbiguous bool
	}{
		{name: "generic cause with guidance text", cause: errors.New(guidance + " private-cause-secret")},
		{name: "wrapped legacy", cause: fmt.Errorf("private-cause-secret: %w", syncqueue.ErrLegacyQueueNotDrained), wantGuidance: true},
		{name: "wrapped ambiguous", cause: fmt.Errorf("private-cause-secret: %w", syncqueue.ErrAmbiguousDurability), wantAmbiguous: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := &syncQueueOperationError{operation: "open_session", cause: tt.cause}
			var logs bytes.Buffer
			slog.New(slog.NewTextHandler(&logs, nil)).Error("run agent runtime", "error", err)
			for _, diagnostic := range []string{err.Error(), logs.String()} {
				if strings.Contains(diagnostic, guidance) != tt.wantGuidance {
					t.Fatal("queue diagnostic exposed guidance without its allowlisted sentinel or omitted allowlisted guidance")
				}
				if strings.Contains(diagnostic, "private-cause-secret") {
					t.Fatal("queue diagnostic exposed an underlying cause")
				}
			}
			if errors.Is(err, syncqueue.ErrAmbiguousDurability) != tt.wantAmbiguous {
				t.Fatal("queue diagnostic changed ambiguous-durability error identity")
			}
			if !errors.Is(err, tt.cause) {
				t.Fatal("queue diagnostic lost its typed underlying cause")
			}
		})
	}
}
