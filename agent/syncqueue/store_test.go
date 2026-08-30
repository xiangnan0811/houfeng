package syncqueue_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"houfeng/agent/syncqueue"
	"houfeng/internal/contracts/agentapi"
)

type cancelAfterFirstCheckContext struct {
	done  chan struct{}
	calls int
}

func newCancelAfterFirstCheckContext() *cancelAfterFirstCheckContext {
	return &cancelAfterFirstCheckContext{done: make(chan struct{})}
}

func (*cancelAfterFirstCheckContext) Deadline() (time.Time, bool) { return time.Time{}, false }

func (c *cancelAfterFirstCheckContext) Done() <-chan struct{} { return c.done }

func (c *cancelAfterFirstCheckContext) Err() error {
	c.calls++
	if c.calls == 1 {
		close(c.done)
		return nil
	}
	return context.Canceled
}

func (*cancelAfterFirstCheckContext) Value(any) any { return nil }

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
		t.Fatalf("persisted entry match = false (count=%d), want one expected entry", len(entries))
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

func TestFileStoreDeleteManyPersistsOneAtomicFilteredQueue(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sync-buffer.json")
	ctx := context.Background()
	store := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour, SkipFsync: true})
	ids := make([]string, 0, 3)
	for _, batchID := range []string{"sync_bulk_one", "sync_bulk_two", "sync_bulk_three"} {
		id, err := store.Enqueue(ctx, syncRequest(batchID, false))
		if err != nil {
			t.Fatalf("Enqueue() error = %v", err)
		}
		ids = append(ids, id)
	}

	if err := store.DeleteMany(ctx, []string{ids[0], ids[2], "missing-id"}); err != nil {
		t.Fatalf("DeleteMany() error = %v", err)
	}
	entries, err := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour, SkipFsync: true}).List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 1 || entries[0].ID != ids[1] {
		t.Fatalf("remaining entry match = false (count=%d), want only the middle entry", len(entries))
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.DeleteMany(canceled, []string{ids[1]}); !errors.Is(err, context.Canceled) {
		t.Fatalf("DeleteMany() canceled error = %v, want context.Canceled", err)
	}
	entries, err = store.List(ctx)
	if err != nil {
		t.Fatalf("List() after canceled delete error = %v", err)
	}
	if len(entries) != 1 || entries[0].ID != ids[1] {
		t.Fatal("canceled DeleteMany() changed durable queue contents")
	}
}

func TestFileStoreDeleteManyRechecksCancellationBeforeMutation(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sync-buffer.json")
	store := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour, SkipFsync: true})
	id, err := store.Enqueue(context.Background(), syncRequest("sync_cancel_after_entry", false))
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	ctx := newCancelAfterFirstCheckContext()
	if err := store.DeleteMany(ctx, []string{id}); !errors.Is(err, context.Canceled) {
		t.Fatalf("DeleteMany() error type = %T, want context cancellation", err)
	}
	entries, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entry count = %d, want canceled batch delete to preserve the queue", len(entries))
	}
}

func TestFileStoreRechecksCancellationAfterLockAcrossOperations(t *testing.T) {
	tests := []struct {
		name      string
		operation func(context.Context, *syncqueue.FileStore, string) error
	}{
		{
			name: "enqueue",
			operation: func(ctx context.Context, store *syncqueue.FileStore, _ string) error {
				_, err := store.Enqueue(ctx, syncRequest("sync_canceled_enqueue", false))
				return err
			},
		},
		{
			name: "list",
			operation: func(ctx context.Context, store *syncqueue.FileStore, _ string) error {
				_, err := store.List(ctx)
				return err
			},
		},
		{name: "delete", operation: func(ctx context.Context, store *syncqueue.FileStore, id string) error {
			return store.Delete(ctx, id)
		}},
		{name: "delete many", operation: func(ctx context.Context, store *syncqueue.FileStore, id string) error {
			return store.DeleteMany(ctx, []string{id})
		}},
		{name: "mark attempt", operation: func(ctx context.Context, store *syncqueue.FileStore, id string) error {
			return store.MarkAttempt(ctx, id)
		}},
		{name: "prune", operation: func(ctx context.Context, store *syncqueue.FileStore, _ string) error {
			return store.Prune(ctx)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sync-buffer.json")
			store := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour, SkipFsync: true})
			id, err := store.Enqueue(context.Background(), syncRequest("sync_preserved", false))
			if err != nil {
				t.Fatalf("seed Enqueue() error type = %T", err)
			}

			ctx := newCancelAfterFirstCheckContext()
			if err := tt.operation(ctx, store, id); !errors.Is(err, context.Canceled) {
				t.Fatalf("operation error type = %T, want context cancellation", err)
			}
			entries, err := store.List(context.Background())
			if err != nil {
				t.Fatalf("List() after canceled operation error type = %T", err)
			}
			if len(entries) != 1 || entries[0].ID != id || entries[0].Attempts != 0 {
				t.Fatalf("canceled operation changed durable queue state (count=%d attempts=%d)", len(entries), firstEntryAttempts(entries))
			}
		})
	}
}

func firstEntryAttempts(entries []syncqueue.Entry) int {
	if len(entries) == 0 {
		return -1
	}
	return entries[0].Attempts
}

func TestFileStoreReadsLegacyNodeIDBufferedRequests(t *testing.T) {
	t.Parallel()
	path := t.TempDir() + "/sync-buffer.json"
	payload := `[
		{
			"id":"sync_legacy",
			"created_at":"2026-04-28T08:00:00Z",
			"attempts":1,
			"request":{
				"node_id":"node-legacy-001",
				"sync_token":"sync-token-legacy",
				"heartbeats":[{"observed_at":"2026-04-28T08:00:00Z","agent_version":"v0.24.1","fingerprint":"fp-001","sync_batch_id":"sync_legacy"}]
			}
		}
	]`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("write legacy queue file: %v", err)
	}

	entries, err := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour}).List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if entries[0].Request.MonitoringInstanceID != "node-legacy-001" {
		t.Fatal("legacy monitoring instance ID was not restored")
	}
	if entries[0].Request.SyncToken != "sync-token-legacy" {
		t.Fatal("legacy sync token was not restored")
	}
	if entries[0].Attempts != 1 {
		t.Fatalf("Attempts = %d, want 1", entries[0].Attempts)
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
		t.Fatalf("attempted entry match = false (count=%d), want one entry with one attempt", len(entries))
	}

	backfilled := syncqueue.WithBackfilledFacts(entries[0].Request, true)
	if backfilled.Heartbeats[0].SyncBatchID != "sync_retry" {
		t.Fatal("heartbeat sync batch ID changed while marking facts as backfilled")
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
	if !backfilled.IPQualityReports[0].IsBackfilled {
		t.Fatal("ip quality report IsBackfilled = false, want true")
	}

	entries[0].Request.HostSamples[0].IsBackfilled = false
	entries[0].Request.ProbeObservations[0].IsBackfilled = false
	entries[0].Request.IPQualityReports[0].IsBackfilled = false
	entries[0].Request.Heartbeats[0].IsBackfilled = false
	if backfilled.Heartbeats[0].IsBackfilled != true ||
		backfilled.HostSamples[0].IsBackfilled != true ||
		backfilled.ProbeObservations[0].IsBackfilled != true ||
		backfilled.IPQualityReports[0].IsBackfilled != true {
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

	fallbackID, err := store.Enqueue(ctx, agentapi.SyncRequest{MonitoringInstanceID: "mi_001", SyncToken: "sync-token-001"})
	if err != nil {
		t.Fatalf("Enqueue(fallback) error = %v", err)
	}
	if fallbackID != now.Format(time.RFC3339Nano) {
		t.Fatal("fallback entry ID did not use the current timestamp")
	}
}

func TestFileStoreFallbackEntryIDAddsSuffixOnCollision(t *testing.T) {
	t.Parallel()
	path := t.TempDir() + "/sync-buffer.json"
	ctx := context.Background()
	now := time.Date(2026, time.April, 28, 8, 0, 0, 0, time.UTC)
	store := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour})
	store.SetNowForTest(func() time.Time { return now })

	firstID, err := store.Enqueue(ctx, agentapi.SyncRequest{MonitoringInstanceID: "mi_001", SyncToken: "sync-token-001"})
	if err != nil {
		t.Fatalf("first Enqueue() error = %v", err)
	}
	secondID, err := store.Enqueue(ctx, agentapi.SyncRequest{MonitoringInstanceID: "mi_001", SyncToken: "sync-token-001"})
	if err != nil {
		t.Fatalf("second Enqueue() error = %v", err)
	}

	if firstID != now.Format(time.RFC3339Nano) {
		t.Fatal("first fallback entry ID did not use the current timestamp")
	}
	if secondID != now.Format(time.RFC3339Nano)+"-1" {
		t.Fatal("second fallback entry ID did not use the collision suffix")
	}
}

func TestFileStoreEntryIDAddsSuffixOnHeartbeatBatchCollision(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sync-buffer.json")
	ctx := context.Background()
	store := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour, SkipFsync: true})

	firstID, err := store.Enqueue(ctx, syncRequest("sync_duplicate", false))
	if err != nil {
		t.Fatalf("first Enqueue() error = %v", err)
	}
	secondID, err := store.Enqueue(ctx, syncRequest("sync_duplicate", false))
	if err != nil {
		t.Fatalf("second Enqueue() error = %v", err)
	}
	if firstID == secondID {
		t.Fatal("duplicate heartbeat batch IDs produced the same persisted entry ID")
	}

	if err := store.Delete(ctx, firstID); err != nil {
		t.Fatalf("Delete(first) error = %v", err)
	}
	entries, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 1 || entries[0].ID != secondID {
		t.Fatalf("remaining duplicate entry match = false (count=%d), want the independently addressable second entry", len(entries))
	}
}

func TestFileStoreEntryIDScalesAcrossDenseHeartbeatBatchSuffixes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync-buffer.json")
	const total = 32768
	createdAt := time.Date(2026, time.April, 28, 8, 0, 0, 0, time.UTC)
	persisted := make([]syncqueue.Entry, total)
	for i := range persisted {
		id := "dense-batch"
		if i > 0 {
			id = fmt.Sprintf("dense-batch-%d", i)
		}
		persisted[i] = syncqueue.Entry{
			ID:        id,
			CreatedAt: createdAt.Add(time.Duration(i)),
		}
	}
	payload, err := json.Marshal(persisted)
	if err != nil {
		t.Fatalf("encode dense suffix backlog fixture error type = %T", err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write dense suffix backlog fixture error type = %T", err)
	}

	store := syncqueue.NewFileStore(path, syncqueue.Options{
		MaxEntries: total + 1,
		MaxAge:     time.Hour,
		SkipFsync:  true,
	})
	store.SetNowForTest(func() time.Time { return createdAt.Add(time.Minute) })
	id, err := store.Enqueue(context.Background(), syncRequest("dense-batch", false))
	if err != nil {
		t.Fatalf("Enqueue() error type = %T", err)
	}
	if id != fmt.Sprintf("dense-batch-%d", total) {
		t.Fatalf("Enqueue() ID = %q, want the first suffix beyond the dense backlog", id)
	}
}

func TestFileStorePrunesLargeOversizedPersistedBacklogInLinearPass(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync-buffer.json")
	const total = 32768
	createdAt := time.Date(2026, time.April, 28, 8, 0, 0, 0, time.UTC)
	persisted := make([]syncqueue.Entry, total)
	for i := range persisted {
		persisted[i] = syncqueue.Entry{
			ID:        fmt.Sprintf("entry-%d", i),
			CreatedAt: createdAt.Add(time.Duration(i)),
		}
	}
	payload, err := json.Marshal(persisted)
	if err != nil {
		t.Fatalf("encode oversized backlog fixture error type = %T", err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write oversized backlog fixture error type = %T", err)
	}
	maxBytes := len(payload) / 2

	store := syncqueue.NewFileStore(path, syncqueue.Options{
		MaxEntries: total + 1,
		MaxAge:     time.Hour,
		MaxBytes:   maxBytes,
		SkipFsync:  true,
	})
	store.SetNowForTest(func() time.Time { return createdAt.Add(time.Minute) })
	id, err := store.Enqueue(context.Background(), syncRequest("newest-batch", false))
	if err != nil {
		t.Fatalf("Enqueue() error type = %T", err)
	}
	entries, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List() error type = %T", err)
	}
	if len(entries) == 0 || entries[len(entries)-1].ID != id {
		t.Fatal("byte pruning did not retain the newly enqueued entry")
	}
	storedPayload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pruned backlog error type = %T", err)
	}
	if len(storedPayload) > maxBytes {
		t.Fatalf("pruned queue size = %d, want <= %d", len(storedPayload), maxBytes)
	}
}

func TestFileStoreNormalizesPersistedDuplicateIDsBeforeDelete(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sync-buffer.json")
	createdAt := time.Date(2026, time.April, 28, 8, 0, 0, 0, time.UTC)
	persisted := []syncqueue.Entry{
		{ID: "shared-entry", CreatedAt: createdAt, Request: syncRequest("sync_first", false)},
		{ID: "shared-entry", CreatedAt: createdAt.Add(time.Nanosecond), Request: syncRequest("sync_second", false)},
	}
	payload, err := json.Marshal(persisted)
	if err != nil {
		t.Fatalf("encode duplicate-ID fixture: %v", err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write duplicate-ID fixture: %v", err)
	}

	store := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour, SkipFsync: true})
	entries, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entry count = %d, want two retained facts", len(entries))
	}
	if entries[0].ID == entries[1].ID {
		t.Fatal("List() returned duplicate persisted IDs that cannot be acknowledged independently")
	}

	if err := store.Delete(context.Background(), entries[0].ID); err != nil {
		t.Fatalf("Delete(first) error = %v", err)
	}
	remaining, err := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour, SkipFsync: true}).List(context.Background())
	if err != nil {
		t.Fatalf("List() after Delete error = %v", err)
	}
	if len(remaining) != 1 || remaining[0].Request.Heartbeats[0].SyncBatchID != "sync_second" {
		t.Fatalf("remaining fact match = false (count=%d), want the unsent second fact preserved", len(remaining))
	}
}

func TestFileStoreNormalizesLargePersistedDuplicateIDBacklog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync-buffer.json")
	const total = 32768
	createdAt := time.Date(2026, time.April, 28, 8, 0, 0, 0, time.UTC)
	persisted := make([]syncqueue.Entry, total)
	for i := range persisted {
		persisted[i] = syncqueue.Entry{
			ID:        "shared-entry",
			CreatedAt: createdAt.Add(time.Duration(i)),
		}
	}
	payload, err := json.Marshal(persisted)
	if err != nil {
		t.Fatalf("encode duplicate-ID backlog fixture error type = %T", err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write duplicate-ID backlog fixture error type = %T", err)
	}

	entries, err := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: total, MaxAge: time.Hour, SkipFsync: true}).List(context.Background())
	if err != nil {
		t.Fatalf("List() error type = %T", err)
	}
	if len(entries) != total {
		t.Fatalf("entry count = %d, want %d", len(entries), total)
	}
	seen := make(map[string]struct{}, total)
	for _, entry := range entries {
		if _, duplicate := seen[entry.ID]; duplicate {
			t.Fatal("large persisted backlog still contained a duplicate entry ID")
		}
		seen[entry.ID] = struct{}{}
	}
}

func TestFileStoreSerializesConcurrentEnqueues(t *testing.T) {
	t.Parallel()
	path := t.TempDir() + "/sync-buffer.json"
	ctx := context.Background()
	store := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 100, MaxAge: time.Hour})

	const total = 25
	done := make(chan error, total)
	for i := 0; i < total; i++ {
		i := i
		go func() {
			_, err := store.Enqueue(ctx, syncRequest(fmt.Sprintf("sync_%03d", i), false))
			done <- err
		}()
	}
	for i := 0; i < total; i++ {
		if err := <-done; err != nil {
			t.Fatalf("concurrent Enqueue() error = %v", err)
		}
	}

	entries, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != total {
		t.Fatalf("len(entries) = %d, want %d", len(entries), total)
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		if seen[entry.ID] {
			t.Fatal("concurrent enqueue produced a duplicate persisted entry ID")
		}
		seen[entry.ID] = true
	}
}

func TestFileStoreSerializesMutatorsAcrossInstances(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sync-buffer.json")
	ctx := context.Background()
	first := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 100, MaxAge: time.Hour})
	second := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 100, MaxAge: time.Hour})

	var deleteIDs []string
	var markIDs []string
	for i := 0; i < 40; i++ {
		id, err := first.Enqueue(ctx, syncRequest(fmt.Sprintf("seed_%03d", i), false))
		if err != nil {
			t.Fatalf("seed Enqueue(%d) error = %v", i, err)
		}
		if i%2 == 0 {
			deleteIDs = append(deleteIDs, id)
		} else {
			markIDs = append(markIDs, id)
		}
	}

	done := make(chan error, len(deleteIDs)+len(markIDs)+20)
	for _, id := range deleteIDs {
		id := id
		go func() {
			done <- first.Delete(ctx, id)
		}()
	}
	for _, id := range markIDs {
		id := id
		go func() {
			done <- second.MarkAttempt(ctx, id)
		}()
	}
	for i := 0; i < 20; i++ {
		i := i
		go func() {
			_, err := second.Enqueue(ctx, syncRequest(fmt.Sprintf("new_%03d", i), false))
			done <- err
		}()
	}
	for i := 0; i < cap(done); i++ {
		if err := <-done; err != nil {
			t.Fatalf("concurrent mutator error = %v", err)
		}
	}

	entries, err := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 100, MaxAge: time.Hour}).List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	byID := map[string]syncqueue.Entry{}
	for _, entry := range entries {
		byID[entry.ID] = entry
	}
	for _, id := range deleteIDs {
		if _, ok := byID[id]; ok {
			t.Fatal("a deleted queue entry was resurrected")
		}
	}
	for _, id := range markIDs {
		entry, ok := byID[id]
		if !ok {
			t.Fatal("an attempted queue entry was lost")
		}
		if entry.Attempts != 1 {
			t.Fatalf("attempted entry Attempts = %d, want 1", entry.Attempts)
		}
	}
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("new_%03d", i)
		if _, ok := byID[id]; !ok {
			t.Fatal("a concurrently enqueued entry was lost")
		}
	}
}

func TestFileStoreWritesPrivateQueueFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not portable on windows")
	}
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "state", "sync-buffer.json")
	store := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour})

	if _, err := store.Enqueue(context.Background(), syncRequest("sync_private", false)); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat(queue dir) error = %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("queue dir mode = %#o, want 0700", got)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(queue file) error = %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("queue file mode = %#o, want 0600", got)
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
			t.Fatalf("Enqueue() error type = %T", err)
		}
	}

	entries, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 2 || entries[0].Request.Heartbeats[0].SyncBatchID != "sync_mid" || entries[1].Request.Heartbeats[0].SyncBatchID != "sync_new" {
		t.Fatalf("pruned entry match = false (count=%d), want the two newest entries", len(entries))
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

func TestFileStorePrunesByMaxBytes(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "sync-buffer.json")
	ctx := context.Background()
	const maxBytes = 1800
	store := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour, MaxBytes: maxBytes, SkipFsync: true})

	if _, err := store.Enqueue(ctx, syncRequestWithOutput("sync_large_old", 700)); err != nil {
		t.Fatalf("Enqueue(old) error = %v", err)
	}
	if _, err := store.Enqueue(ctx, syncRequestWithOutput("sync_large_new", 700)); err != nil {
		t.Fatalf("Enqueue(new) error = %v", err)
	}

	entries, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1 after byte pruning", len(entries))
	}
	if got := entries[0].Request.Heartbeats[0].SyncBatchID; got != "sync_large_new" {
		t.Fatal("byte pruning did not retain the newest entry")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(queue file) error = %v", err)
	}
	if info.Size() > maxBytes {
		t.Fatalf("queue file size = %d, want <= %d", info.Size(), maxBytes)
	}
}

func TestFileStoreEnqueueFailsWithoutMutatingQueueWhenNewestEntryExceedsMaxBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync-buffer.json")
	ctx := context.Background()
	store := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 10, MaxAge: time.Hour, MaxBytes: 1000, SkipFsync: true})
	seed := agentapi.SyncRequest{
		MonitoringInstanceID: "mi_001",
		SyncToken:            "sync-token-001",
		Heartbeats: []agentapi.MonitoringInstanceHeartbeat{{
			ObservedAt:   time.Date(2026, time.April, 28, 8, 0, 0, 0, time.UTC),
			AgentVersion: "dev",
			Fingerprint:  "fp-001",
			SyncBatchID:  "sync_seed",
		}},
	}
	if _, err := store.Enqueue(ctx, seed); err != nil {
		t.Fatalf("seed Enqueue() error type = %T", err)
	}

	if _, err := store.Enqueue(ctx, syncRequestWithOutput("sync_oversized", 5000)); err == nil {
		t.Fatal("oversized Enqueue() error = nil, want local durability failure")
	}
	entries, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List() error type = %T", err)
	}
	if len(entries) != 1 || entries[0].Request.Heartbeats[0].SyncBatchID != "sync_seed" {
		t.Fatalf("queue state changed after rejected oversized enqueue (count=%d)", len(entries))
	}
}

func TestFileStorePruneDoesNotWriteAnEmptyQueueBeyondMaxBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync-buffer.json")
	original := []byte(`[{"id":"legacy"}]`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("write queue fixture error type = %T", err)
	}
	store := syncqueue.NewFileStore(path, syncqueue.Options{MaxEntries: 1, MaxAge: time.Nanosecond, MaxBytes: 1, SkipFsync: true})
	if err := store.Prune(context.Background()); err == nil {
		t.Fatal("Prune() error = nil, want durability failure when even [] exceeds MaxBytes")
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read queue after rejected Prune error type = %T", err)
	}
	if !bytes.Equal(stored, original) {
		t.Fatal("rejected Prune rewrote the prior queue beyond MaxBytes")
	}
}

func syncRequest(batchID string, backfilled bool) agentapi.SyncRequest {
	observedAt := time.Date(2026, time.April, 28, 8, 0, 0, 0, time.UTC)
	return agentapi.SyncRequest{
		MonitoringInstanceID: "mi_001",
		SyncToken:            "sync-token-001",
		Heartbeats: []agentapi.MonitoringInstanceHeartbeat{{
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
		IPQualityReports: []agentapi.IPQualityReportPayload{{
			ObservedAt:    observedAt,
			AgentVersion:  "dev",
			Fingerprint:   "fp-001",
			SyncBatchID:   batchID,
			IPAddress:     "203.0.113.10",
			IPVersion:     4,
			Status:        agentapi.IPQualityStatusSuccess,
			ASN:           "AS64500",
			Organization:  "Example Network",
			UseRegionCode: "US",
			RiskLevel:     "low",
			IsBackfilled:  backfilled,
		}},
	}
}

func syncRequestWithOutput(batchID string, outputBytes int) agentapi.SyncRequest {
	observedAt := time.Date(2026, time.April, 28, 8, 0, 0, 0, time.UTC)
	request := agentapi.SyncRequest{
		MonitoringInstanceID: "mi_001",
		SyncToken:            "sync-token-001",
		Heartbeats: []agentapi.MonitoringInstanceHeartbeat{{
			ObservedAt:   observedAt,
			AgentVersion: "dev",
			Fingerprint:  "fp-001",
			SyncBatchID:  batchID,
		}},
	}
	request.CommandResults = []agentapi.CommandResult{{
		ActionID:  "act_" + batchID,
		CommandID: "uptime",
		Stdout:    strings.Repeat("x", outputBytes),
		ExitCode:  0,
	}}
	return request
}
