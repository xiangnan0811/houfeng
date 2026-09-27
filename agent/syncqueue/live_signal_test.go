package syncqueue

import (
	"context"
	"path/filepath"
	"testing"

	"houfeng/internal/contracts/agentapi"
)

func TestLiveSignalCannotEnterPersistentQueueOrBackfill(t *testing.T) {
	request := agentapi.SyncRequest{LiveSignal: &agentapi.LiveSignal{ID: "live", Fingerprint: "fp"}, Heartbeats: []agentapi.MonitoringInstanceHeartbeat{{SyncBatchID: "batch"}}}
	store := NewFileStore(filepath.Join(t.TempDir(), "queue.json"), Options{})
	if _, err := store.Enqueue(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	entries, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Request.LiveSignal != nil {
		t.Fatal("live evidence was persisted")
	}
	if WithBackfilledFacts(request, true).LiveSignal != nil {
		t.Fatal("backfill retains live evidence")
	}
}
