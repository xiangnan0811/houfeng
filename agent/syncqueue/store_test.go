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
	if backfilled.Heartbeats[0].SyncBatchID != "sync_retry" {
		t.Fatalf("heartbeat SyncBatchID = %q, want %q", backfilled.Heartbeats[0].SyncBatchID, "sync_retry")
	}
	if !backfilled.Heartbeats[0].IsBackfilled {
		t.Fatal("heartbeat IsBackfilled = false, want true")
	}
	if !backfilled.HostSamples[0].IsBackfilled {
		t.Fatal("host sample IsBackfilled = false, want true")
	}
	if !backfilled.ProbeObservations[0].IsBackfilled {
		t.Fatal("probe observation IsBackfilled = false, want true")
	}

	entries[0].Request.HostSamples[0].IsBackfilled = false
	entries[0].Request.ProbeObservations[0].IsBackfilled = false
	entries[0].Request.Heartbeats[0].IsBackfilled = false
	if backfilled.Heartbeats[0].IsBackfilled != true ||
		backfilled.HostSamples[0].IsBackfilled != true ||
		backfilled.ProbeObservations[0].IsBackfilled != true {
		t.Fatal("WithBackfilledFacts() did not deep-copy payload slices")
	}
}

func TestFileStoreFallbackEntryIDUsesCurrentTime(t *testing.T) {
	t.Parallel()
	path := t.TempDir() + "/sync-buffer.json"
	ctx := context.Background()
	now := time.Date(2026, time.April, 28, 8, 0, 0, 0, time.UTC)
	store := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour})
	store.SetNowForTest(func() time.Time { return now })

	if _, err := store.Enqueue(ctx, syncRequest("sync_existing", false)); err != nil {
		t.Fatalf("Enqueue(existing) error = %v", err)
	}

	fallbackID, err := store.Enqueue(ctx, agentapi.SyncRequest{NodeID: "nd_001", SyncToken: "sync-token-001"})
	if err != nil {
		t.Fatalf("Enqueue(fallback) error = %v", err)
	}
	if fallbackID != now.Format(time.RFC3339Nano) {
		t.Fatalf("fallbackID = %q, want current time %q", fallbackID, now.Format(time.RFC3339Nano))
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
			ObservedAt:   observedAt,
			AgentVersion: "dev",
			Fingerprint:  "fp-001",
			SyncBatchID:  batchID,
			IsBackfilled: backfilled,
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
