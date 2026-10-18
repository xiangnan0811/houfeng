package syncqueue_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"houfeng/agent/syncqueue"
	"houfeng/internal/contracts/agentapi"
)

const testSession = "session-001"

func openStore(t *testing.T, path string, opts syncqueue.Options) *syncqueue.FileStore {
	t.Helper()
	store := syncqueue.NewFileStore(path, opts)
	if err := store.OpenSession(context.Background(), testSession); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func queueEntries(t *testing.T, store *syncqueue.FileStore) []syncqueue.Entry {
	t.Helper()
	entries, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func replayState(t *testing.T, store *syncqueue.FileStore) syncqueue.ReplayState {
	t.Helper()
	state, err := store.ReplayState(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func enqueue(t *testing.T, store *syncqueue.FileStore, request agentapi.SyncRequest) string {
	t.Helper()
	id, err := store.Enqueue(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestFileStorePersistsEntriesAcrossInstancesAndDeletesAckedEntry(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "queue.json")
	store := openStore(t, path, syncqueue.Options{})
	id := enqueue(t, store, syncRequest("ignored-old-id", false))
	before := replayState(t, store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openStore(t, path, syncqueue.Options{})
	if got := replayState(t, reopened); got != before {
		t.Fatalf("reopen changed authority: %+v", got)
	}
	entries := queueEntries(t, reopened)
	if len(entries) != 1 || entries[0].ID != id || entries[0].Request.BatchSequence != 1 {
		t.Fatalf("lost durable entry: %+v", entries)
	}
	if err := reopened.Delete(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if len(queueEntries(t, reopened)) != 0 || replayState(t, reopened).RetiredThrough != 1 {
		t.Fatal("ack did not durably retire entry")
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	third := openStore(t, path, syncqueue.Options{})
	if replayState(t, third).RetiredThrough != 1 {
		t.Fatal("retirement lost on restart")
	}
	if id2 := enqueue(t, third, syncRequest("ignored-old-id", false)); id2 == id || queueEntries(t, third)[0].Request.BatchSequence != 2 {
		t.Fatal("restart reused batch sequence")
	}
}

func TestFileStoreAssignsCanonicalIdentityWithoutMutatingCaller(t *testing.T) {
	t.Parallel()
	store := openStore(t, filepath.Join(t.TempDir(), "queue.json"), syncqueue.Options{})
	r := syncRequest("caller-id", false)
	r.LiveSignal = &agentapi.LiveSignal{Sequence: 11, Fingerprint: "fp"}
	r.ProducerID, r.BatchSequence, r.ProtocolRevision = "caller-producer", 42, 1
	id := enqueue(t, store, r)
	got := queueEntries(t, store)[0].Request
	state := replayState(t, store)
	if got.ProtocolRevision != 2 || got.ProducerID != state.ProducerID || got.BatchSequence != 1 || got.AllocatedThrough != 1 || got.LiveSignal != nil || id != state.ProducerID+":1" {
		t.Fatalf("incorrect assigned identity: %+v", got)
	}
	if got.Heartbeats[0].SyncBatchID != id || got.HostSamples[0].SyncBatchID != id || got.ProbeObservations[0].SyncBatchID != id || got.IPQualityReports[0].SyncBatchID != id {
		t.Fatal("fact identity mismatch")
	}
	if r.BatchSequence != 42 || r.LiveSignal == nil || r.Heartbeats[0].SyncBatchID != "caller-id" || r.IPQualityReports[0].SyncBatchID != "caller-id" {
		t.Fatal("enqueue mutated caller")
	}
	second := enqueue(t, store, r)
	if second != state.ProducerID+":2" {
		t.Fatal("caller IDs affected allocation")
	}
	wrong := r
	wrong.SessionID = "other"
	before := replayState(t, store)
	if _, err := store.Enqueue(context.Background(), wrong); err == nil {
		t.Fatal("accepted wrong session")
	}
	if replayState(t, store) != before {
		t.Fatal("wrong session consumed a sequence")
	}
}

func TestFileStoreDeleteManyPersistsOneAtomicFilteredQueue(t *testing.T) {
	t.Parallel()
	store := openStore(t, filepath.Join(t.TempDir(), "queue.json"), syncqueue.Options{})
	var ids []string
	for range 4 {
		ids = append(ids, enqueue(t, store, syncRequest("same", false)))
	}
	ctx := context.Background()
	if err := store.DeleteMany(ctx, []string{ids[1], ids[3], "missing"}); err != nil {
		t.Fatal(err)
	}
	if replayState(t, store).RetiredThrough != 0 {
		t.Fatal("out-of-order ack retired pending oldest batch")
	}
	if err := store.Delete(ctx, ids[0]); err != nil {
		t.Fatal(err)
	}
	entries := queueEntries(t, store)
	if len(entries) != 1 || entries[0].ID != ids[2] || replayState(t, store).RetiredThrough != 2 {
		t.Fatal("floor did not follow oldest retained sequence")
	}
	if err := store.DeleteMany(ctx, []string{ids[2]}); err != nil {
		t.Fatal(err)
	}
	if replayState(t, store).RetiredThrough != 4 {
		t.Fatal("empty queue did not retire through allocated")
	}
}

func TestFileStoreMarksAttemptsAndBuildsBackfilledRequests(t *testing.T) {
	t.Parallel()
	store := openStore(t, filepath.Join(t.TempDir(), "queue.json"), syncqueue.Options{})
	request := syncRequest("old", false)
	falseValue := false
	request.HostSamples[0].CPURatesValid = &falseValue
	id := enqueue(t, store, request)
	if err := store.MarkAttempt(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	entry := queueEntries(t, store)[0]
	if entry.Attempts != 1 || entry.Request.HostSamples[0].CPURatesValid == nil || *entry.Request.HostSamples[0].CPURatesValid {
		t.Fatal("attempt or explicit false marker lost")
	}
	originalDigest, err := agentapi.CanonicalSyncBatchDigest(entry.Request)
	if err != nil {
		t.Fatal(err)
	}
	backfilled := syncqueue.WithBackfilledFacts(entry.Request, true)
	if !backfilled.Heartbeats[0].IsBackfilled || !backfilled.HostSamples[0].IsBackfilled || !backfilled.ProbeObservations[0].IsBackfilled || !backfilled.IPQualityReports[0].IsBackfilled || backfilled.Heartbeats[0].SyncBatchID != id {
		t.Fatal("backfill changed fact identity")
	}
	backfilledDigest, err := agentapi.CanonicalSyncBatchDigest(backfilled)
	if err != nil || backfilledDigest != originalDigest {
		t.Fatal("backfill changed digest", err)
	}
	if entry.Request.Heartbeats[0].IsBackfilled || entry.Request.IPQualityReports[0].IsBackfilled {
		t.Fatal("backfill mutated queue snapshot")
	}
}

func TestFileStorePrunesByMaxEntriesAndAge(t *testing.T) {
	t.Parallel()
	store := openStore(t, filepath.Join(t.TempDir(), "queue.json"), syncqueue.Options{MaxEntries: 2, MaxAge: time.Hour})
	base := time.Date(2026, 4, 28, 8, 0, 0, 0, time.UTC)
	store.SetNowForTest(func() time.Time { return base })
	for range 3 {
		enqueue(t, store, syncRequest("same", false))
	}
	entries := queueEntries(t, store)
	if len(entries) != 2 || entries[0].Request.BatchSequence != 2 || entries[1].Request.BatchSequence != 3 || replayState(t, store).RetiredThrough != 1 {
		t.Fatal("count pruning broke FIFO/floor")
	}
	store.SetNowForTest(func() time.Time { return base.Add(time.Hour) })
	if err := store.Prune(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(queueEntries(t, store)) != 2 {
		t.Fatal("exact age cutoff removed eligible entries")
	}
	store.SetNowForTest(func() time.Time { return base.Add(2 * time.Hour) })
	if err := store.Prune(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(queueEntries(t, store)) != 0 || replayState(t, store).RetiredThrough != 3 {
		t.Fatal("age pruning did not retire all entries")
	}
}

func TestFileStorePrunesByMaxBytesIncludingEnvelope(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "queue.json")
	store := openStore(t, path, syncqueue.Options{MaxBytes: 2600})
	enqueue(t, store, syncRequestWithOutput("old", 700))
	id := enqueue(t, store, syncRequestWithOutput("new", 700))
	entries := queueEntries(t, store)
	if len(entries) != 1 || entries[0].ID != id || replayState(t, store).RetiredThrough != 1 {
		t.Fatal("byte pruning lost newest or floor")
	}
	if len(readBytes(t, path)) > 2600 {
		t.Fatal("envelope exceeds byte bound")
	}
	original := readBytes(t, path)
	before := replayState(t, store)
	request := syncRequestWithOutput("oversized", 5000)
	if _, err := store.Enqueue(context.Background(), request); err == nil {
		t.Fatal("oversized enqueue succeeded")
	}
	if !bytes.Equal(original, readBytes(t, path)) || replayState(t, store) != before || len(request.CommandResults[0].Stdout) != 5000 {
		t.Fatal("oversized enqueue consumed authority or auxiliary payload")
	}
}

func TestFileStoreBootstrapOnlyMissingOrEmptyLegacyArray(t *testing.T) {
	t.Parallel()
	for _, initial := range []string{"missing", "[]", " \n[ \n ]\t"} {
		t.Run(initial, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "queue.json")
			if initial != "missing" {
				if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			store := openStore(t, path, syncqueue.Options{})
			state := replayState(t, store)
			if state.SessionID != testSession || !agentapi.ValidSyncProducerID(state.ProducerID) || state.AllocatedThrough != 0 || state.RetiredThrough != 0 {
				t.Fatal("invalid bootstrap", state)
			}
		})
	}
}

func TestFileStoreRejectsLegacyEmptyCorruptAndIncompleteBytesUnchanged(t *testing.T) {
	t.Parallel()
	for _, original := range []string{"", " ", "null", "{}", "[", "[{}]", `[{"request":{"node_id":"legacy"}}]`, `{"version":2}`, `{"version":2} garbage`} {
		t.Run(original, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "queue.json")
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			store := syncqueue.NewFileStore(path, syncqueue.Options{})
			if err := store.OpenSession(context.Background(), testSession); err == nil {
				t.Fatal("accepted corrupt/legacy queue")
			}
			if !bytes.Equal(readBytes(t, path), []byte(original)) {
				t.Fatal("rejected bytes were changed")
			}
			// Failed opens must release the process lock.
			if err := os.WriteFile(path, []byte("[]"), 0o600); err != nil {
				t.Fatal(err)
			}
			fresh := openStore(t, path, syncqueue.Options{})
			_ = fresh.Close()
		})
	}
}

func TestFileStoreRequiresOpenSessionAcrossAllOperations(t *testing.T) {
	t.Parallel()
	store := syncqueue.NewFileStore(filepath.Join(t.TempDir(), "queue.json"), syncqueue.Options{})
	for name, operation := range queueOperations(store) {
		t.Run(name, func(t *testing.T) {
			if err := operation(context.Background()); err == nil {
				t.Fatal("operation before OpenSession succeeded")
			}
		})
	}
}

type cancelAfterFirstCheckContext struct{ calls int }

func (*cancelAfterFirstCheckContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (*cancelAfterFirstCheckContext) Done() <-chan struct{}       { return nil }
func (c *cancelAfterFirstCheckContext) Err() error {
	c.calls++
	if c.calls == 1 {
		return nil
	}
	return context.Canceled
}
func (*cancelAfterFirstCheckContext) Value(any) any { return nil }

func queueOperations(store *syncqueue.FileStore) map[string]func(context.Context) error {
	return map[string]func(context.Context) error{
		"enqueue": func(ctx context.Context) error {
			_, err := store.Enqueue(ctx, syncRequest("ignored", false))
			return err
		},
		"list":         func(ctx context.Context) error { _, err := store.List(ctx); return err },
		"delete":       func(ctx context.Context) error { return store.Delete(ctx, "missing") },
		"delete_many":  func(ctx context.Context) error { return store.DeleteMany(ctx, []string{"missing"}) },
		"delete_empty": func(ctx context.Context) error { return store.DeleteMany(ctx, nil) },
		"attempt":      func(ctx context.Context) error { return store.MarkAttempt(ctx, "missing") },
		"prune":        store.Prune,
		"replay_state": func(ctx context.Context) error { _, err := store.ReplayState(ctx); return err },
		"reserve":      func(ctx context.Context) error { _, _, err := store.ReserveLiveSequences(ctx, 1024); return err },
	}
}

func TestFileStoreRechecksCancellationAfterLockAcrossOperations(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "queue.json"), syncqueue.Options{})
	enqueue(t, store, syncRequest("seed", false))
	before := replayState(t, store)
	for name, operation := range queueOperations(store) {
		t.Run(name, func(t *testing.T) {
			canceled, cancel := context.WithCancel(context.Background())
			cancel()
			if err := operation(canceled); !errors.Is(err, context.Canceled) {
				t.Fatal("ignored cancellation", err)
			}
			// Read-only operations need not check twice: their only check is under lock.
			if name != "replay_state" && name != "reserve" {
				if err := operation(&cancelAfterFirstCheckContext{}); !errors.Is(err, context.Canceled) {
					t.Fatal("ignored cancellation at lock boundary", err)
				}
			}
			if replayState(t, store) != before || len(queueEntries(t, store)) != 1 {
				t.Fatal("canceled mutation changed queue")
			}
		})
	}
}

func TestFileStoreSerializesConcurrentMutations(t *testing.T) {
	t.Parallel()
	store := openStore(t, filepath.Join(t.TempDir(), "queue.json"), syncqueue.Options{SkipFsync: true})
	const total = 40
	results := make(chan error, total)
	for range total {
		go func() { _, err := store.Enqueue(context.Background(), syncRequest("same", false)); results <- err }()
	}
	for range total {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	entries := queueEntries(t, store)
	if len(entries) != total {
		t.Fatal("concurrent enqueue lost entries")
	}
	for i, entry := range entries {
		if entry.Request.BatchSequence != int64(i+1) {
			t.Fatal("sequence gap/duplicate")
		}
		entry := entry
		go func() {
			if entry.Request.BatchSequence%2 == 0 {
				results <- store.Delete(context.Background(), entry.ID)
			} else {
				results <- store.MarkAttempt(context.Background(), entry.ID)
			}
		}()
	}
	for range total {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	entries = queueEntries(t, store)
	if len(entries) != total/2 {
		t.Fatal("lost concurrent mutation")
	}
	for _, entry := range entries {
		if entry.Attempts != 1 || entry.Request.BatchSequence%2 == 0 {
			t.Fatal("mutation resurrected/lost an entry")
		}
	}
}

func TestFileStoreProcessLifetimeLockSurvivesRename(t *testing.T) {
	if path := os.Getenv("HOUFENG_QUEUE_LOCK_CHILD"); path != "" {
		store := syncqueue.NewFileStore(path, syncqueue.Options{})
		if err := store.OpenSession(context.Background(), testSession); err == nil {
			_ = store.Close()
			t.Fatal("child acquired locked queue")
		}
		return
	}
	path := filepath.Join(t.TempDir(), "queue.json")
	store := openStore(t, path, syncqueue.Options{})
	second := syncqueue.NewFileStore(path, syncqueue.Options{})
	if err := second.OpenSession(context.Background(), testSession); err == nil {
		t.Fatal("second instance acquired queue")
	}
	for range 2 {
		enqueue(t, store, syncRequest("rename", false))
		command := exec.Command(os.Args[0], "-test.run=^TestFileStoreProcessLifetimeLockSurvivesRename$")
		command.Env = append(os.Environ(), "HOUFENG_QUEUE_LOCK_CHILD="+path)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("child lock check failed: %v %s", err, output)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := second.OpenSession(context.Background(), testSession); err != nil {
		t.Fatal("close did not release flock", err)
	}
	_ = second.Close()
}

func TestFileStoreWritesPrivateQueueAndLock(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "agent", "state", "queue.json")
	store := openStore(t, path, syncqueue.Options{})
	enqueue(t, store, syncRequest("private", false))
	for _, item := range []struct {
		path string
		mode os.FileMode
	}{{filepath.Dir(path), 0o700}, {path, 0o600}, {path + ".lock", 0o600}} {
		info, err := os.Stat(item.path)
		if err != nil || info.Mode().Perm() != item.mode {
			t.Fatalf("wrong permissions on %s: %v", item.path, err)
		}
	}
}

func TestFileStorePersistsAndReplaysDualRoundAsOneBatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dual.json")
	store := openStore(t, path, syncqueue.Options{})
	request := syncRequest("old", false)
	request.IPQualityReports = nil
	observed := time.Now().UTC()
	for index, family := range []int{4, 6} {
		ip, status, availability, code := "198.51.100.10", "success", "available", ""
		if index == 1 {
			ip, status, availability, code = "::", "failure", "unavailable", "address_family_unavailable"
		}
		diagnostics, err := json.Marshal(map[string]any{"collection_revision": 4, "collection_id": "durable-round", "expected_ip_versions": []int{4, 6}, "ip_address_family": family, "family_availability": availability, "collect_request_id": "durable-request"})
		if err != nil {
			t.Fatal(err)
		}
		request.IPQualityReports = append(request.IPQualityReports, agentapi.IPQualityReportPayload{ObservedAt: observed, IPVersion: family, IPAddress: ip, Status: status, ErrorCode: code, DiagnosticsJSON: diagnostics})
	}
	id := enqueue(t, store, request)
	request.IPQualityReports[0].DiagnosticsJSON[0] = 'x'
	request.IPQualityReports[1].IPAddress = "2001:db8::bad"
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openStore(t, path, syncqueue.Options{})
	if err := reopened.MarkAttempt(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	entries := queueEntries(t, reopened)
	if len(entries) != 1 || entries[0].Attempts != 1 || len(entries[0].Request.IPQualityReports) != 2 {
		t.Fatal("split or lost round")
	}
	for index, report := range entries[0].Request.IPQualityReports {
		metadata := agentapi.ParseCollectionMetadata(report.DiagnosticsJSON)
		if report.IPVersion != []int{4, 6}[index] || !metadata.Valid || metadata.CollectionID != "durable-round" || metadata.CollectRequestID != "durable-request" || report.SyncBatchID != id {
			t.Fatal("round identity lost")
		}
	}
	if entries[0].Request.IPQualityReports[1].IPAddress != "::" || entries[0].Request.IPQualityReports[1].ErrorCode != "address_family_unavailable" {
		t.Fatal("terminal family changed")
	}
	if err := reopened.Delete(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if len(queueEntries(t, reopened)) != 0 {
		t.Fatal("ack did not delete whole round")
	}
}

func syncRequest(batchID string, backfilled bool) agentapi.SyncRequest {
	observed := time.Date(2026, 4, 28, 8, 0, 0, 0, time.UTC)
	return agentapi.SyncRequest{
		SessionID: testSession, MonitoringInstanceID: "mi_001", SyncToken: "sync-token-001",
		Heartbeats:        []agentapi.MonitoringInstanceHeartbeat{{ObservedAt: observed, AgentVersion: "dev", Fingerprint: "fp-001", SyncBatchID: batchID, IsBackfilled: backfilled}},
		HostSamples:       []agentapi.HostSamplePayload{{ObservedAt: observed, AgentVersion: "dev", Fingerprint: "fp-001", SyncBatchID: batchID, IsBackfilled: backfilled, CPUUsagePct: 12.5}},
		ProbeObservations: []agentapi.ProbeObservationPayload{{ObservedAt: observed, AgentVersion: "dev", Fingerprint: "fp-001", SyncBatchID: batchID, IsBackfilled: backfilled, TargetID: "tg_001", ProbeItemID: "pb_001", ProbeKind: agentapi.ProbeKindHTTP, ResultKind: agentapi.ProbeResultSuccess}},
		IPQualityReports:  []agentapi.IPQualityReportPayload{{ObservedAt: observed, AgentVersion: "dev", Fingerprint: "fp-001", SyncBatchID: batchID, IsBackfilled: backfilled, IPAddress: "203.0.113.10", IPVersion: 4, Status: agentapi.IPQualityStatusSuccess, ASN: "AS64500", Organization: "Example Network", UseRegionCode: "US", RiskLevel: "low"}},
	}
}

func syncRequestWithOutput(batchID string, outputBytes int) agentapi.SyncRequest {
	return agentapi.SyncRequest{SessionID: testSession, MonitoringInstanceID: "mi_001", SyncToken: "sync-token-001",
		Heartbeats:     []agentapi.MonitoringInstanceHeartbeat{{ObservedAt: time.Now().UTC(), AgentVersion: "dev", Fingerprint: "fp-001", SyncBatchID: batchID}},
		CommandResults: []agentapi.CommandResult{{ActionID: "act_" + batchID, CommandID: "uptime", Stdout: strings.Repeat("x", outputBytes)}},
	}
}
