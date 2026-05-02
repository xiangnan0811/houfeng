# Houfeng Agent Reliability Closure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add V1 agent-side durable buffering so collected heartbeats, host samples, and probe observations survive temporary center/network failures and are backfilled without noisy notifications.

**Architecture:** Keep this phase narrow and agent-local first. Add a small JSON-file durable sync queue under `agent/syncqueue`, carry `is_backfilled` on heartbeat payloads in the existing agent API contract, and route runtime sync through enqueue/flush/delete semantics. Center ingestion already stores backfilled observation flags and evaluators already suppress backfilled observation noise; this phase makes the agent produce those flags and locks the center heartbeat/write path.

**Tech Stack:** Go standard library, Go agent runtime, existing agent API DTOs, existing center sync/observation ingestion, existing Go test fakes

---

## Scope

This plan implements Phase 2 from `docs/superpowers/specs/2026-04-28-houfeng-v1-completion-sequencing-design.md`.

In scope:

- Agent durable queue for `agentapi.SyncRequest` batches.
- Retry unacknowledged batches until center accepts them.
- Delete accepted batches after successful sync acknowledgement.
- Continue the runtime loop when center sync temporarily fails.
- Mark retried or recovered historical heartbeats, host samples, and probe observations as `is_backfilled=true`.
- Bound queue growth by entry count and entry age.
- Preserve existing sync-plan update behavior from accepted sync responses.

Out of scope:

- Persisting enrollment/session state before first successful bound enrollment.
- Cross-agent deduplication or exactly-once delivery beyond current sync batch IDs.
- Compression, SQLite, or external queue dependencies.
- Retention workers, aggregation, dashboard/events/trends, and visual QA.

## Planned file structure

### Agent durable queue

- Create: `agent/syncqueue/store.go`
  - File-backed JSON queue for `agentapi.SyncRequest` entries.
  - Atomic writes via temp file + rename.
  - FIFO listing, delete-by-ID, attempt increment, and pruning.
- Create: `agent/syncqueue/store_test.go`
  - Locks persistence across store instances, ack deletion, attempt persistence, backfill marking helper, and count/age pruning.

### Agent config/runtime

- Modify: `agent/config/config.go`
  - Add optional buffer settings without adding new required env vars.
- Modify: `agent/config/config_test.go`
  - Lock defaults and env overrides.
- Modify: `agent/runtime/runtime.go`
  - Route each generated sync batch through queue append + flush.
  - Continue after sync errors instead of returning immediately once enrolled.
  - Apply `is_backfilled` to non-current or previously failed queued batches during flush.
- Modify: `agent/runtime/runtime_test.go`
  - Lock center-unavailable behavior, retry, ack deletion, restart persistence via queue, and backfill flags.
- Modify: `cmd/houfeng-agent/main.go`
  - No behavior change expected if `runtime.New` owns queue construction; only touch if constructor shape requires it.

### Contract and center ingestion

- Modify: `internal/contracts/agentapi/types.go`
  - Add `IsBackfilled bool` to `NodeHeartbeat` JSON.
- Modify: `internal/contracts/agentapi/types_test.go`
  - Lock heartbeat JSON round-trip for `is_backfilled`.
- Modify: `internal/center/enrollment/types.go`
  - Add `IsBackfilled` to `HeartbeatPayload`.
- Modify: `internal/center/enrollment/service.go`
  - Preserve heartbeat backfilled flag when using the legacy enrollment sync service path.
- Modify: `internal/center/enrollment/service_test.go`
  - Lock `RecordHeartbeatSync` backfilled persistence.
- Modify: `internal/center/http/handlers/agent.go`
  - Map `NodeHeartbeat.IsBackfilled` into `syncing.HeartbeatPayload`.
- Modify: `internal/center/http/handlers/agent_test.go`
  - Lock handler mapping.
- Modify: `internal/center/store/sync_batches.go`
  - Persist heartbeat `is_backfilled` instead of hard-coded `false`.
- Modify: `internal/center/store/sync_batches_test.go`
  - Lock SQL args include heartbeat backfilled flag.

### Center backfill noise suppression evidence

- Modify: `internal/center/incidents/service_test.go`
  - Add an integration-style service test proving a backfilled failing observation is stored/evaluated as raw history but does not produce a new active incident notification.
  - If existing evaluator coverage already fully locks this behavior, add a narrower `AfterSuccessfulSync` test that proves a backfilled target sync does not send notification.

---

## Task 1: Agent durable sync queue package

**Files:**
- Create: `agent/syncqueue/store.go`
- Create: `agent/syncqueue/store_test.go`

- [ ] **Step 1: Write failing persistence, delete, attempt, backfill, and prune tests**

Create `agent/syncqueue/store_test.go` with tests covering these concrete behaviors:

```go
package syncqueue_test

import (
	"context"
	"testing"
	"time"

	"houfeng/agent/syncqueue"
	"houfeng/internal/contracts/agentapi"
)

func TestFileStorePersistsEntriesAcrossInstancesAndDeletesAckedEntry(t *testing.T) {
	t.Parallel()
	path := t.TempDir() + "/sync-buffer.json"
	ctx := context.Background()
	store := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour})

	entryID, err := store.Enqueue(ctx, syncRequest("sync_001", false))
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	reopened := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour})
	entries, err := reopened.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 1 || entries[0].ID != entryID || entries[0].Request.Heartbeats[0].SyncBatchID != "sync_001" {
		t.Fatalf("entries = %#v, want persisted sync_001 entry", entries)
	}

	if err := reopened.Delete(ctx, entryID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	afterDelete, err := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour}).List(ctx)
	if err != nil {
		t.Fatalf("List() after delete error = %v", err)
	}
	if len(afterDelete) != 0 {
		t.Fatalf("len(afterDelete) = %d, want 0", len(afterDelete))
	}
}

func TestFileStoreMarksAttemptsAndBuildsBackfilledRequests(t *testing.T) {
	t.Parallel()
	path := t.TempDir() + "/sync-buffer.json"
	ctx := context.Background()
	store := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour})

	entryID, err := store.Enqueue(ctx, syncRequest("sync_retry", false))
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if err := store.MarkAttempt(ctx, entryID); err != nil {
		t.Fatalf("MarkAttempt() error = %v", err)
	}

	entries, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 1 || entries[0].Attempts != 1 {
		t.Fatalf("entries = %#v, want one attempted entry", entries)
	}

	backfilled := syncqueue.WithBackfilledFacts(entries[0].Request, true)
	if !backfilled.Heartbeats[0].IsBackfilled {
		t.Fatal("heartbeat IsBackfilled = false, want true")
	}
	if !backfilled.HostSamples[0].IsBackfilled {
		t.Fatal("host sample IsBackfilled = false, want true")
	}
	if !backfilled.ProbeObservations[0].IsBackfilled {
		t.Fatal("probe observation IsBackfilled = false, want true")
	}
}

func TestFileStorePrunesByMaxEntriesAndAge(t *testing.T) {
	t.Parallel()
	path := t.TempDir() + "/sync-buffer.json"
	ctx := context.Background()
	base := time.Date(2026, time.April, 28, 8, 0, 0, 0, time.UTC)
	store := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 2, MaxAge: time.Hour})
	store.SetNowForTest(func() time.Time { return base })
	for _, id := range []string{"sync_old", "sync_mid", "sync_new"} {
		if _, err := store.Enqueue(ctx, syncRequest(id, false)); err != nil {
			t.Fatalf("Enqueue(%s) error = %v", id, err)
		}
	}

	entries, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 2 || entries[0].Request.Heartbeats[0].SyncBatchID != "sync_mid" || entries[1].Request.Heartbeats[0].SyncBatchID != "sync_new" {
		t.Fatalf("entries after max-entry prune = %#v, want sync_mid/sync_new", entries)
	}

	store.SetNowForTest(func() time.Time { return base.Add(2 * time.Hour) })
	if err := store.Prune(ctx); err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	entries, err = store.List(ctx)
	if err != nil {
		t.Fatalf("List() after age prune error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("len(entries) after age prune = %d, want 0", len(entries))
	}
}

func syncRequest(batchID string, backfilled bool) agentapi.SyncRequest {
	observedAt := time.Date(2026, time.April, 28, 8, 0, 0, 0, time.UTC)
	return agentapi.SyncRequest{
		NodeID:    "nd_001",
		SyncToken: "sync-token-001",
		Heartbeats: []agentapi.NodeHeartbeat{{
			ObservedAt:    observedAt,
			AgentVersion:  "dev",
			Fingerprint:   "fp-001",
			SyncBatchID:   batchID,
			IsBackfilled:  backfilled,
		}},
		HostSamples: []agentapi.HostSamplePayload{{
			ObservedAt:   observedAt,
			AgentVersion: "dev",
			Fingerprint:  "fp-001",
			SyncBatchID:  batchID,
			IsBackfilled: backfilled,
			CPUUsagePct:  12.5,
		}},
		ProbeObservations: []agentapi.ProbeObservationPayload{{
			TargetID:     "tg_001",
			ProbeItemID:  "pb_001",
			ProbeKind:    agentapi.ProbeKindHTTP,
			ObservedAt:   observedAt,
			AgentVersion: "dev",
			Fingerprint:  "fp-001",
			SyncBatchID:  batchID,
			ResultKind:   agentapi.ProbeResultSuccess,
			IsBackfilled: backfilled,
		}},
	}
}
```

- [ ] **Step 2: Run tests and confirm failure**

Run:

```bash
go test ./agent/syncqueue -v
```

Expected: FAIL because `agent/syncqueue` does not exist yet.

- [ ] **Step 3: Implement the file-backed queue**

Create `agent/syncqueue/store.go` with these public API names:

```go
package syncqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"houfeng/internal/contracts/agentapi"
)

type Options struct {
	MaxEntries int
	MaxAge     time.Duration
}

type Entry struct {
	ID        string               `json:"id"`
	CreatedAt time.Time            `json:"created_at"`
	Attempts  int                  `json:"attempts"`
	Request   agentapi.SyncRequest `json:"request"`
}

type FileStore struct {
	path string
	opts Options
	now  func() time.Time
}

func NewFileStore(path string, opts Options) *FileStore
func (s *FileStore) SetNowForTest(now func() time.Time)
func (s *FileStore) Enqueue(ctx context.Context, request agentapi.SyncRequest) (string, error)
func (s *FileStore) List(ctx context.Context) ([]Entry, error)
func (s *FileStore) Delete(ctx context.Context, id string) error
func (s *FileStore) MarkAttempt(ctx context.Context, id string) error
func (s *FileStore) Prune(ctx context.Context) error
func WithBackfilledFacts(request agentapi.SyncRequest, backfilled bool) agentapi.SyncRequest
```

Implementation requirements:

- `NewFileStore` must default `MaxEntries` to `2048` and `MaxAge` to `72h` when unset.
- Entry ID should come from the first heartbeat `sync_batch_id` when present, otherwise use `time.Now().UTC().Format(time.RFC3339Nano)`.
- `Enqueue` appends then prunes in one atomic write.
- `List` returns FIFO order by `CreatedAt` then `ID`.
- `Delete` is idempotent.
- `MarkAttempt` increments attempts for the matching entry and persists it.
- `WithBackfilledFacts` must deep-copy slices and set the backfilled flag on all heartbeats, host samples, and probe observations.
- Atomic write pattern: `os.MkdirAll(filepath.Dir(path), 0o755)`, write JSON to `path + ".tmp"`, `Close`, then `os.Rename`.
- If the queue file does not exist, reads return an empty slice.

- [ ] **Step 4: Run queue tests**

Run:

```bash
go test ./agent/syncqueue -v
```

Expected: PASS.

- [ ] **Step 5: Commit queue package**

Run:

```bash
git add agent/syncqueue/store.go agent/syncqueue/store_test.go
git commit -m "Add durable agent sync queue" -m "The V1 agent needs a small local buffer so collected facts survive temporary center outages and process restarts. A JSON file queue keeps this dependency-free and bounded.\n\nConstraint: No new dependencies for V1 agent buffering\nRejected: SQLite queue | unnecessary dependency and migration surface for short-term V1 buffering\nConfidence: high\nScope-risk: moderate\nTested: go test ./agent/syncqueue -v"
```

---

## Task 2: Heartbeat backfill contract and center persistence

**Files:**
- Modify: `internal/contracts/agentapi/types.go`
- Modify: `internal/contracts/agentapi/types_test.go`
- Modify: `internal/center/enrollment/types.go`
- Modify: `internal/center/enrollment/service.go`
- Modify: `internal/center/enrollment/service_test.go`
- Modify: `internal/center/http/handlers/agent.go`
- Modify: `internal/center/http/handlers/agent_test.go`
- Modify: `internal/center/store/sync_batches.go`
- Modify: `internal/center/store/sync_batches_test.go`

- [ ] **Step 1: Add failing contract and handler tests for backfilled heartbeats**

Update `internal/contracts/agentapi/types_test.go` in `TestSyncRequestRoundTrip`:

```go
original := agentapi.SyncRequest{
	NodeID:    "nd-local-01",
	SyncToken: "sync-token-001",
	Heartbeats: []agentapi.NodeHeartbeat{{
		ObservedAt:   observedAt,
		AgentVersion: "dev",
		Fingerprint:  "fp-001",
		SyncBatchID:  "batch-001",
		IsBackfilled: true,
	}},
}
```

Add after existing heartbeat assertions:

```go
if !roundTrip.Heartbeats[0].IsBackfilled {
	t.Fatal("Heartbeats[0].IsBackfilled = false, want true")
}
```

Update `internal/center/http/handlers/agent_test.go` in `TestAgentSyncHandlerReturnsAcceptedAt` request JSON to include heartbeat `"is_backfilled":true`, then add:

```go
if !svc.syncBatch.Heartbeats[0].IsBackfilled {
	t.Fatal("SyncBatch Heartbeats[0].IsBackfilled = false, want true")
}
```

- [ ] **Step 2: Add failing center persistence tests**

In `internal/center/enrollment/service_test.go`, update one accepted heartbeat test to set `IsBackfilled: true` on the input heartbeat and assert the fake repository receives `HeartbeatWrite.IsBackfilled == true`.

In `internal/center/store/sync_batches_test.go`, add a test that sets `testSyncBatch().Heartbeats[0].IsBackfilled = true`, runs `ApplyBatch`, and asserts the heartbeat insert SQL args contain `true` for the backfilled position. If the fake currently does not capture args, extend `fakeSyncBatchTx.Exec` to append `args` alongside `execSQL`:

```go	execArgs [][]any
```

Then assert:

```go
heartbeatArgs := tx.argsForSQL("insert into node_heartbeats")
if got, ok := heartbeatArgs[6].(bool); !ok || !got {
	t.Fatalf("heartbeat is_backfilled arg = %#v, want true", heartbeatArgs[6])
}
```

- [ ] **Step 3: Run focused tests and confirm failure**

Run:

```bash
go test ./internal/contracts/agentapi -run TestSyncRequestRoundTrip -v
go test ./internal/center/http/handlers -run TestAgentSyncHandlerReturnsAcceptedAt -v
go test ./internal/center/enrollment -run 'TestRecordHeartbeat' -v
go test ./internal/center/store -run 'TestPostgresSyncRepository.*Backfilled|TestSyncBatchPlanReturnsAcceptedAtAndDerivedPlan' -v
```

Expected: FAIL because heartbeat backfill is not in the DTO/mapping/persistence path yet.

- [ ] **Step 4: Add heartbeat backfill fields and mappings**

In `internal/contracts/agentapi/types.go`, add to `NodeHeartbeat`:

```go
IsBackfilled bool `json:"is_backfilled,omitempty"`
```

In `internal/center/enrollment/types.go`, add to `HeartbeatPayload`:

```go
IsBackfilled bool
```

In `internal/center/http/handlers/agent.go`, map it in `syncBatchFromRequest`:

```go
IsBackfilled: heartbeat.IsBackfilled,
```

In `internal/center/enrollment/service.go`, map it in `HeartbeatWrite`:

```go
IsBackfilled: heartbeat.IsBackfilled,
```

In `internal/center/store/sync_batches.go`, replace the hard-coded heartbeat insert argument `false` with:

```go
write.IsBackfilled,
```

- [ ] **Step 5: Run focused tests**

Run:

```bash
go test ./internal/contracts/agentapi -run TestSyncRequestRoundTrip -v
go test ./internal/center/http/handlers -run TestAgentSyncHandlerReturnsAcceptedAt -v
go test ./internal/center/enrollment -run 'TestRecordHeartbeat' -v
go test ./internal/center/store -run 'TestPostgresSyncRepository.*Backfilled|TestSyncBatchPlanReturnsAcceptedAtAndDerivedPlan' -v
```

Expected: PASS.

- [ ] **Step 6: Commit heartbeat backfill contract**

Run:

```bash
git add internal/contracts/agentapi/types.go internal/contracts/agentapi/types_test.go internal/center/enrollment/types.go internal/center/enrollment/service.go internal/center/enrollment/service_test.go internal/center/http/handlers/agent.go internal/center/http/handlers/agent_test.go internal/center/store/sync_batches.go internal/center/store/sync_batches_test.go
git commit -m "Carry backfilled heartbeat provenance through sync" -m "Agent retry batches need to distinguish recovered history from live facts. Observations already carry is_backfilled; heartbeat sync must preserve the same provenance through the center write path.\n\nConstraint: Existing heartbeat sync remains the canonical sync carrier\nConfidence: high\nScope-risk: moderate\nTested: go test ./internal/contracts/agentapi -run TestSyncRequestRoundTrip -v\nTested: go test ./internal/center/http/handlers -run TestAgentSyncHandlerReturnsAcceptedAt -v\nTested: go test ./internal/center/enrollment -run 'TestRecordHeartbeat' -v\nTested: go test ./internal/center/store -run 'TestPostgresSyncRepository.*Backfilled|TestSyncBatchPlanReturnsAcceptedAtAndDerivedPlan' -v"
```

---

## Task 3: Runtime enqueue, retry, ack deletion, and config wiring

**Files:**
- Modify: `agent/config/config.go`
- Modify: `agent/config/config_test.go`
- Modify: `agent/runtime/runtime.go`
- Modify: `agent/runtime/runtime_test.go`
- Modify: `cmd/houfeng-agent/main.go` only if needed

- [ ] **Step 1: Add failing config tests**

In `agent/config/config_test.go`, add tests for optional buffer settings:

```go
func TestLoadAgentConfigProvidesDurableBufferDefaults(t *testing.T) {
	t.Setenv("HOUFENG_AGENT_SERVER_URL", "http://center")
	t.Setenv("HOUFENG_AGENT_TOKEN_FILE", "/tmp/token")
	t.Setenv("HOUFENG_AGENT_BUFFER_FILE", "")
	t.Setenv("HOUFENG_AGENT_BUFFER_MAX_ENTRIES", "")
	t.Setenv("HOUFENG_AGENT_BUFFER_MAX_AGE", "")

	cfg, err := agentconfig.LoadAgentConfig()
	if err != nil {
		t.Fatalf("LoadAgentConfig() error = %v", err)
	}
	if cfg.BufferFile != "/var/lib/houfeng-agent/sync-buffer.json" {
		t.Fatalf("BufferFile = %q, want default", cfg.BufferFile)
	}
	if cfg.BufferMaxEntries != 2048 {
		t.Fatalf("BufferMaxEntries = %d, want 2048", cfg.BufferMaxEntries)
	}
	if cfg.BufferMaxAge != 72*time.Hour {
		t.Fatalf("BufferMaxAge = %s, want 72h", cfg.BufferMaxAge)
	}
}

func TestLoadAgentConfigAcceptsDurableBufferOverrides(t *testing.T) {
	t.Setenv("HOUFENG_AGENT_SERVER_URL", "http://center")
	t.Setenv("HOUFENG_AGENT_TOKEN_FILE", "/tmp/token")
	t.Setenv("HOUFENG_AGENT_BUFFER_FILE", "/tmp/houfeng-buffer.json")
	t.Setenv("HOUFENG_AGENT_BUFFER_MAX_ENTRIES", "17")
	t.Setenv("HOUFENG_AGENT_BUFFER_MAX_AGE", "2h")

	cfg, err := agentconfig.LoadAgentConfig()
	if err != nil {
		t.Fatalf("LoadAgentConfig() error = %v", err)
	}
	if cfg.BufferFile != "/tmp/houfeng-buffer.json" || cfg.BufferMaxEntries != 17 || cfg.BufferMaxAge != 2*time.Hour {
		t.Fatalf("config = %#v, want buffer overrides", cfg)
	}
}
```

Add `time` to imports.

- [ ] **Step 2: Add failing runtime queue tests**

In `agent/runtime/runtime_test.go`, extend `fakeClient` with:

```go
syncErrs []error
```

and in `Sync` before response selection:

```go
if len(f.syncErrs) >= f.syncCalls && f.syncErrs[f.syncCalls-1] != nil {
	return nil, f.syncErrs[f.syncCalls-1]
}
```

Add tests:

```go
func TestRuntimeQueuesFailedSyncAndRetriesAsBackfilled(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	client := &fakeClient{
		syncErrs: []error{nil, errors.New("center unavailable"), nil},
		syncResponses: []agentapi.SyncResponse{
			{AcceptedAt: time.Now().UTC(), Status: "accepted", Plan: &agentapi.SyncPlan{HostSampleFrequencyTier: agentapi.FrequencyTier1m}},
			{},
			{AcceptedAt: time.Now().UTC(), Status: "accepted"},
		},
	}
	store := syncqueue.NewFileStore(t.TempDir()+"/buffer.json", syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour})
	hostProvider := &fakeHostSampleProvider{result: agentapi.HostSamplePayload{CPUUsagePct: 12.5}}
	rt := agentruntime.NewWithRuntimeDeps(cfg, nil, client, staticTokenSource{}, staticFingerprint{}, hostProvider, &fakeProbeProvider{}, 10*time.Millisecond, store)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Millisecond)
	defer cancel()
	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if client.syncCalls < 3 {
		t.Fatalf("syncCalls = %d, want at least 3", client.syncCalls)
	}
	last := client.syncRequests[len(client.syncRequests)-1]
	if len(last.Heartbeats) == 0 || !last.Heartbeats[0].IsBackfilled {
		t.Fatalf("retried heartbeat = %#v, want backfilled", last.Heartbeats)
	}
	if len(last.HostSamples) == 0 || !last.HostSamples[0].IsBackfilled {
		t.Fatalf("retried host samples = %#v, want backfilled", last.HostSamples)
	}
	entries, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("queue List() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("len(queue entries) = %d, want 0 after ack", len(entries))
	}
}

func TestRuntimeFlushesPersistedQueueAfterRestart(t *testing.T) {
	cfg := agentconfig.AgentConfig{ServerURL: "http://center", TokenFile: "/tmp/token"}
	path := t.TempDir()+"/buffer.json"
	seedStore := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour})
	seeded := agentapi.SyncRequest{NodeID: "node-123", SyncToken: "sync-token-001", Heartbeats: []agentapi.NodeHeartbeat{{ObservedAt: time.Now().UTC().Add(-time.Minute), AgentVersion: "dev", Fingerprint: "fp-001", SyncBatchID: "seeded"}}}
	if _, err := seedStore.Enqueue(context.Background(), seeded); err != nil {
		t.Fatalf("seed Enqueue() error = %v", err)
	}

	client := &fakeClient{}
	store := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour})
	rt := agentruntime.NewWithRuntimeDeps(cfg, nil, client, staticTokenSource{}, staticFingerprint{}, &fakeHostSampleProvider{}, &fakeProbeProvider{}, 10*time.Millisecond, store)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := rt.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(client.syncRequests) == 0 || client.syncRequests[0].Heartbeats[0].SyncBatchID != "seeded" || !client.syncRequests[0].Heartbeats[0].IsBackfilled {
		t.Fatalf("first sync request = %#v, want seeded backfilled request", client.syncRequests)
	}
}
```

Add `houfeng/agent/syncqueue` to imports.

- [ ] **Step 3: Run focused tests and confirm failure**

Run:

```bash
go test ./agent/config -v
go test ./agent/runtime -run 'TestRuntime(QueuesFailedSyncAndRetriesAsBackfilled|FlushesPersistedQueueAfterRestart)' -v
```

Expected: FAIL because config fields and runtime queue integration are missing.

- [ ] **Step 4: Add config fields and parsing**

In `agent/config/config.go`, add:

```go
import "time"

const (
	DefaultBufferFile       = "/var/lib/houfeng-agent/sync-buffer.json"
	DefaultBufferMaxEntries = 2048
	DefaultBufferMaxAge     = 72 * time.Hour
)

type AgentConfig struct {
	ServerURL        string
	TokenFile        string
	BufferFile       string
	BufferMaxEntries int
	BufferMaxAge     time.Duration
}
```

Add helpers:

```go
func optionalEnv(key, fallback string) string
func optionalPositiveIntEnv(key string, fallback int) (int, error)
func optionalDurationEnv(key string, fallback time.Duration) (time.Duration, error)
```

`LoadAgentConfig` must read:

- `HOUFENG_AGENT_BUFFER_FILE`, default `DefaultBufferFile`
- `HOUFENG_AGENT_BUFFER_MAX_ENTRIES`, default `DefaultBufferMaxEntries`
- `HOUFENG_AGENT_BUFFER_MAX_AGE`, default `DefaultBufferMaxAge`

Invalid max entries or duration should return a descriptive error.

- [ ] **Step 5: Integrate queue into runtime**

In `agent/runtime/runtime.go`:

- Import `houfeng/agent/syncqueue`.
- Add a queue interface:

```go
type SyncQueue interface {
	Enqueue(context.Context, agentapi.SyncRequest) (string, error)
	List(context.Context) ([]syncqueue.Entry, error)
	Delete(context.Context, string) error
	MarkAttempt(context.Context, string) error
	Prune(context.Context) error
}
```

- Add to `Runtime`:

```go
syncQueue SyncQueue
```

- Change `New` to construct a file store:

```go
queue := syncqueue.NewFileStore(cfg.BufferFile, syncqueue.Options{MaxEntries: cfg.BufferMaxEntries, MaxAge: cfg.BufferMaxAge})
return NewWithRuntimeDeps(cfg, logger, enroll.NewClient(cfg.ServerURL), tokenSource, fingerprintSource, hostsample.New(), probe.New(), defaultInterval, queue)
```

- Change `NewWithRuntimeDeps` signature to a variadic optional queue:

```go
func NewWithRuntimeDeps(..., interval time.Duration, queues ...SyncQueue) *Runtime
```

Set `syncQueue` from `queues[0]` when present.

- Extract current request construction into a helper if needed:

```go
func (r *Runtime) buildSyncRequest(... ) agentapi.SyncRequest
```

- Replace direct sync with:

```go
if err := r.enqueueAndFlush(ctx, request, request.Heartbeats[0].SyncBatchID); err != nil {
	r.logger.Error("sync queue flush failed", "error", err)
}
```

- Implement:

```go
func (r *Runtime) enqueueAndFlush(ctx context.Context, request agentapi.SyncRequest, currentBatchID string) error
func (r *Runtime) flushSyncQueue(ctx context.Context, currentBatchID string) error
func (r *Runtime) syncRequest(ctx context.Context, entry syncqueue.Entry, currentBatchID string) (*agentapi.SyncResponse, error)
```

Behavior requirements:

- If `syncQueue` is nil, keep legacy direct sync behavior for unit tests that do not opt in.
- If queue is present, enqueue every generated request before sending.
- Flush FIFO entries.
- A queued entry is sent as backfilled when `entry.Attempts > 0` or `entry.Request.Heartbeats[0].SyncBatchID != currentBatchID`.
- On successful sync: delete that entry and apply response plan if present.
- On failed sync: mark attempt, return error to caller, and continue runtime loop unless context is done.
- `Run` must still return nil on context cancellation.
- `Run` must still return enrollment/token/fingerprint setup errors before entering the loop.

- [ ] **Step 6: Run focused runtime/config tests**

Run:

```bash
go test ./agent/config -v
go test ./agent/runtime -v
go test ./agent/syncqueue -v
```

Expected: PASS.

- [ ] **Step 7: Commit runtime queue integration**

Run:

```bash
git add agent/config/config.go agent/config/config_test.go agent/runtime/runtime.go agent/runtime/runtime_test.go cmd/houfeng-agent/main.go
git commit -m "Buffer agent sync batches across center outages" -m "Once enrolled, the V1 agent should not drop collected facts just because the center is temporarily unavailable. Runtime now persists generated sync batches before send, retries them, marks recovered history as backfilled, and deletes accepted entries.\n\nConstraint: Enrollment state before first bound enrollment is still not persisted in V1\nConfidence: high\nScope-risk: moderate\nTested: go test ./agent/config -v\nTested: go test ./agent/runtime -v\nTested: go test ./agent/syncqueue -v"
```

---

## Task 4: Center backfilled-ingestion notification evidence

**Files:**
- Modify: `internal/center/incidents/service_test.go`
- Modify source only if the new test exposes a real regression.

- [ ] **Step 1: Add a failing/evidence test for backfilled observation suppression after sync**

Add a service-level test in `internal/center/incidents/service_test.go` near existing `AfterSuccessfulSync` tests:

```go
func TestServiceAfterSuccessfulSyncDoesNotNotifyForBackfilledProbeFailure(t *testing.T) {
	now := time.Date(2026, time.April, 28, 9, 0, 0, 0, time.UTC)
	writer := &fakeMutationWriter{}
	notifier := &fakeNotifier{}
	snapshots := &fakeSnapshotReader{
		probeObservationsByTarget: map[string][]runtimefacts.ProbeObservation{
			"tg_001": {
				{ObservedAt: now, TargetID: "tg_001", ProbeItemID: "pb_001", ProbeKind: agentapi.ProbeKindHTTP, NodeID: "nd_001", ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503", IsBackfilled: true},
				{ObservedAt: now.Add(-time.Minute), TargetID: "tg_001", ProbeItemID: "pb_001", ProbeKind: agentapi.ProbeKindHTTP, NodeID: "nd_001", ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503", IsBackfilled: true},
				{ObservedAt: now.Add(-2 * time.Minute), TargetID: "tg_001", ProbeItemID: "pb_001", ProbeKind: agentapi.ProbeKindHTTP, NodeID: "nd_001", ResultKind: agentapi.ProbeResultFailure, ErrorSummary: "503", IsBackfilled: true},
			},
		},
	}
	service := NewService(&fakeNodeRepository{}, nil, snapshots, writer, notifier, slog.Default(), 30*time.Second, time.Minute)
	service.now = func() time.Time { return now }

	err := service.AfterSuccessfulSync(context.Background(), syncing.Batch{
		NodeID: "nd_001",
		Observations: observations.BatchWrite{ProbeObservations: []observations.ProbeObservationWrite{{
			NodeID: "nd_001", TargetID: "tg_001", ProbeItemID: "pb_001", ProbeKind: agentapi.ProbeKindHTTP, IsBackfilled: true,
		}}},
	}, syncing.Result{AcceptedAt: now})
	if err != nil {
		t.Fatalf("AfterSuccessfulSync() error = %v", err)
	}
	if len(notifier.messages) != 0 {
		t.Fatalf("notifier.messages = %#v, want no sends for backfilled failure", notifier.messages)
	}
	if len(writer.notifications) != 0 {
		t.Fatalf("notifications = %#v, want no notification records for skipped backfilled failure", writer.notifications)
	}
	if len(writer.mutations) == 0 {
		t.Fatal("mutations = 0, want evaluation mutation evidence")
	}
}
```

Adjust fake names to match the existing test file. The important assertions are: no sends, no notification records, and evaluation still runs.

- [ ] **Step 2: Run focused incident test**

Run:

```bash
go test ./internal/center/incidents -run 'TestServiceAfterSuccessfulSyncDoesNotNotifyForBackfilledProbeFailure|TestEvaluate.*Backfilled|TestServiceAfterSuccessfulSync' -v
```

Expected: PASS if existing evaluator suppression is sufficient; FAIL only if the service still creates noisy notifications for backfilled submissions.

- [ ] **Step 3: Fix only if the test fails for a real regression**

If needed, keep the fix narrow:

- Do not skip storing facts; storage belongs to sync repository before incident evaluation.
- Do not globally skip target evaluation for mixed live/backfilled batches.
- Prefer relying on existing `IsBackfilled` evaluator suppression.
- If a bug exists, fix at the evaluator/service boundary that incorrectly creates notifications for `TransitionSkipped` or suppressed transitions.

- [ ] **Step 4: Commit backfill suppression evidence**

Run:

```bash
git add internal/center/incidents/service_test.go internal/center/incidents/service.go internal/center/incidents/evaluator.go
git commit -m "Lock backfilled sync noise suppression" -m "Durable agent retry makes historical observations arrive late. They must remain raw facts but must not create fresh noisy incident notifications.\n\nConstraint: Backfilled observations are stored by sync ingestion before incident evaluation\nConfidence: high\nScope-risk: narrow\nTested: go test ./internal/center/incidents -run 'TestServiceAfterSuccessfulSyncDoesNotNotifyForBackfilledProbeFailure|TestEvaluate.*Backfilled|TestServiceAfterSuccessfulSync' -v"
```

Only include source files in the commit if source changes were actually required.

---

## Task 5: Phase-level verification

**Files:**
- No planned source edits unless verification exposes a regression.

- [ ] **Step 1: Run focused agent and center suites**

Run:

```bash
go test ./agent/syncqueue -v
go test ./agent/config -v
go test ./agent/runtime -v
go test ./internal/contracts/agentapi -v
go test ./internal/center/http/handlers -run 'TestAgentSyncHandler' -v
go test ./internal/center/enrollment -v
go test ./internal/center/store -run 'TestPostgresSyncRepository|TestRecordBatch' -v
go test ./internal/center/incidents -run 'TestServiceAfterSuccessfulSync|TestEvaluate.*Backfilled|TestServiceNotificationFlags' -v
```

Expected: PASS.

- [ ] **Step 2: Run full Go tests**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 3: Run repository verification**

Run:

```bash
./scripts/verify.sh
```

Expected: PASS.

- [ ] **Step 4: Inspect git status**

Run:

```bash
git status --short --branch
```

Expected: clean `main` after all phase commits.

---

## Dependency and parallelization notes

Recommended task order:

1. Task 1 and Task 2 can run in parallel: queue internals and heartbeat contract are disjoint.
2. Task 3 depends on Task 1 and Task 2.
3. Task 4 depends on Task 2 and can run after Task 3 starts if file ownership is kept to incident tests only.
4. Task 5 must run after all implementation tasks are merged.

Safe parallel split for `superpowers:subagent-driven-development`:

- Subagent A: Task 1 only (`agent/syncqueue/*`).
- Subagent B: Task 2 only (contract/center heartbeat backfill path).
- Subagent C: Task 4 test-only evidence after Task 2 lands.
- Task 3 should start after Task 1 and Task 2 because it consumes both queue API and heartbeat backfill contract.
- Task 5 stays in the leader session after integration.

## Self-review checklist

- Spec coverage:
  - Durable local queue: Task 1.
  - Retry unacknowledged batches and delete acked batches: Task 3.
  - Preserve observations during center outage: Task 3.
  - Restart persistence: Task 1 and Task 3 tests.
  - Backfilled historical submissions: Task 2 and Task 3.
  - Bounded buffer growth: Task 1.
  - No noisy notifications from backfilled facts: Task 4.
- Out-of-scope coverage:
  - No enrollment persistence before first bound enrollment.
  - No retention/aggregation/dashboard/trend/visual work.
  - No new dependency.
- Verification coverage:
  - Queue unit tests.
  - Runtime retry tests.
  - API/handler/store contract tests.
  - Incident suppression evidence.
  - Full repo verification.
