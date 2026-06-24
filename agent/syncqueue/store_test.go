package syncqueue_test

import (
	"context"
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
		t.Fatalf("MonitoringInstanceID = %q, want legacy node id", entries[0].Request.MonitoringInstanceID)
	}
	if entries[0].Request.SyncToken != "sync-token-legacy" {
		t.Fatalf("SyncToken = %q, want legacy sync token", entries[0].Request.SyncToken)
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
		t.Fatalf("fallbackID = %q, want current time %q", fallbackID, now.Format(time.RFC3339Nano))
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
		t.Fatalf("firstID = %q, want current time", firstID)
	}
	if secondID != now.Format(time.RFC3339Nano)+"-1" {
		t.Fatalf("secondID = %q, want suffixed collision ID", secondID)
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
			t.Fatalf("duplicate entry ID %q in %#v", entry.ID, entries)
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
			t.Fatalf("deleted ID %q was resurrected in %#v", id, entries)
		}
	}
	for _, id := range markIDs {
		entry, ok := byID[id]
		if !ok {
			t.Fatalf("marked ID %q was lost in %#v", id, entries)
		}
		if entry.Attempts != 1 {
			t.Fatalf("entry %q Attempts = %d, want 1", id, entry.Attempts)
		}
	}
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("new_%03d", i)
		if _, ok := byID[id]; !ok {
			t.Fatalf("new ID %q was lost in %#v", id, entries)
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
		t.Fatalf("remaining SyncBatchID = %q, want newest entry", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(queue file) error = %v", err)
	}
	if info.Size() > maxBytes {
		t.Fatalf("queue file size = %d, want <= %d", info.Size(), maxBytes)
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
