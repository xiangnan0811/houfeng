package syncqueue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"houfeng/internal/contracts/agentapi"
)

func newInternalStore(t *testing.T) *FileStore {
	t.Helper()
	store := NewFileStore(filepath.Join(t.TempDir(), "agent", "queue.json"), Options{})
	if err := store.OpenSession(context.Background(), "session"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func internalRequest() agentapi.SyncRequest {
	return agentapi.SyncRequest{SessionID: "session", MonitoringInstanceID: "mi", SyncToken: "token", Heartbeats: []agentapi.MonitoringInstanceHeartbeat{{Fingerprint: "fp", ObservedAt: time.Now().UTC()}}}
}

func internalEnqueue(t *testing.T, store *FileStore) string {
	t.Helper()
	id, err := store.Enqueue(context.Background(), internalRequest())
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func internalBytes(t *testing.T, store *FileStore) []byte {
	t.Helper()
	payload, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func internalMutations(store *FileStore, id string) map[string]func() error {
	ctx := context.Background()
	return map[string]func() error{
		"enqueue":     func() error { _, err := store.Enqueue(ctx, internalRequest()); return err },
		"delete":      func() error { return store.Delete(ctx, id) },
		"delete_many": func() error { return store.DeleteMany(ctx, []string{id}) },
		"attempt":     func() error { return store.MarkAttempt(ctx, id) },
		"prune":       func() error { return store.Prune(ctx) },
		"reserve":     func() error { _, _, err := store.ReserveLiveSequences(ctx, 1024); return err },
	}
}

func TestFileStorePreRenameFailuresPreserveBytesAndAllocation(t *testing.T) {
	for _, stage := range []string{"file_sync", "rename"} {
		for _, operation := range []string{"enqueue", "delete", "delete_many", "attempt", "prune", "reserve"} {
			t.Run(stage+"/"+operation, func(t *testing.T) {
				store := newInternalStore(t)
				id := internalEnqueue(t, store)
				original := internalBytes(t, store)
				fault := errors.New("injected before rename")
				if stage == "file_sync" {
					store.syncFile = func(*os.File) error { return fault }
				} else {
					store.rename = func(string, string) error { return fault }
				}
				if err := internalMutations(store, id)[operation](); !errors.Is(err, fault) || errors.Is(err, ErrAmbiguousDurability) {
					t.Fatalf("wrong failure classification: %v", err)
				}
				if !bytes.Equal(original, internalBytes(t, store)) {
					t.Fatal("pre-rename failure changed bytes")
				}
				store.syncFile, store.rename = (*os.File).Sync, os.Rename
				internalEnqueue(t, store)
				entries, err := store.List(context.Background())
				if err != nil || len(entries) != 2 || entries[1].Request.BatchSequence != 2 {
					t.Fatal("failed mutation consumed sequence or altered queue", err)
				}
				matches, err := filepath.Glob(filepath.Join(filepath.Dir(store.path), ".sync-buffer-*.tmp"))
				if err != nil || len(matches) != 0 {
					t.Fatal("temporary files leaked")
				}
			})
		}
	}
}

func TestFileStoreAmbiguousDurabilityPoisonsEveryOperation(t *testing.T) {
	for _, operation := range []string{"enqueue", "delete", "delete_many", "attempt", "prune", "reserve"} {
		t.Run(operation, func(t *testing.T) {
			store := newInternalStore(t)
			id := internalEnqueue(t, store)
			store.syncDir = func(string) error { return errors.New("injected directory fsync failure") }
			if err := internalMutations(store, id)[operation](); !errors.Is(err, ErrAmbiguousDurability) {
				t.Fatal("missing ambiguous durability error", err)
			}
			visible := internalBytes(t, store)
			for name, mutation := range internalMutations(store, id) {
				if err := mutation(); !errors.Is(err, ErrAmbiguousDurability) {
					t.Fatalf("poisoned %s returned %v", name, err)
				}
			}
			if _, err := store.List(context.Background()); !errors.Is(err, ErrAmbiguousDurability) {
				t.Fatal("poisoned list succeeded", err)
			}
			if _, err := store.ReplayState(context.Background()); !errors.Is(err, ErrAmbiguousDurability) {
				t.Fatal("poisoned ReplayState succeeded", err)
			}
			if err := store.DeleteMany(context.Background(), nil); !errors.Is(err, ErrAmbiguousDurability) {
				t.Fatal("empty deletion bypassed poison")
			}
			if err := store.OpenSession(context.Background(), "session"); !errors.Is(err, ErrAmbiguousDurability) {
				t.Fatal("same store reopened poisoned state")
			}
			if !bytes.Equal(visible, internalBytes(t, store)) {
				t.Fatal("poisoned operations rewrote visible commit")
			}
			if err := store.Close(); !errors.Is(err, ErrAmbiguousDurability) {
				t.Fatal("close lost poison", err)
			}
			if err := store.OpenSession(context.Background(), "session"); !errors.Is(err, ErrAmbiguousDurability) {
				t.Fatal("close cleared poison")
			}
			fresh := NewFileStore(store.path, Options{})
			if err := fresh.OpenSession(context.Background(), "session"); err != nil {
				t.Fatal("fresh store could not recover visible state", err)
			}
			defer fresh.Close()
			state, err := decodeEnvelope(visible)
			if err != nil {
				t.Fatal(err)
			}
			first, last, err := fresh.ReserveLiveSequences(context.Background(), 1024)
			if err != nil || first != state.LiveReservedThrough+1 || last != state.LiveReservedThrough+1024 {
				t.Fatal("recovery reused reserved live sequences", first, last, err)
			}
			internalEnqueue(t, fresh)
			entries, err := fresh.List(context.Background())
			if err != nil || entries[len(entries)-1].Request.BatchSequence != state.LastAllocatedBatchSeq+1 {
				t.Fatal("recovery reused visible batch sequence", err)
			}
		})
	}
}

func TestFileStoreBootstrapAmbiguityReleasesLockAndRequiresFreshStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	store := NewFileStore(path, Options{})
	store.syncDir = func(string) error { return errors.New("bootstrap sync failed") }
	if err := store.OpenSession(context.Background(), "session"); !errors.Is(err, ErrAmbiguousDurability) {
		t.Fatal(err)
	}
	visible := internalBytes(t, store)
	fresh := NewFileStore(path, Options{})
	if err := fresh.OpenSession(context.Background(), "session"); err != nil {
		t.Fatal("failed open leaked flock", err)
	}
	defer fresh.Close()
	if !bytes.Equal(visible, internalBytes(t, fresh)) {
		t.Fatal("recovery replaced committed bootstrap authority")
	}
}

func TestFileStoreExplicitSessionChangeDurablyRetiresBeforeInitializing(t *testing.T) {
	for _, failAt := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			store := newInternalStore(t)
			internalEnqueue(t, store)
			before, err := store.ReplayState(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			original := internalBytes(t, store)
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			rollover := NewFileStore(store.path, Options{})
			calls := 0
			rollover.rename = func(from, to string) error {
				calls++
				if calls == failAt {
					return errors.New("injected rollover rename failure")
				}
				if calls == 2 {
					retired, err := decodeEnvelope(internalBytes(t, rollover))
					if err != nil || retired.SessionID != "session" || len(retired.Entries) != 0 || retired.RetiredBatchFloor != 1 {
						t.Fatal("new authority preceded durable old authority cleanup", err)
					}
				}
				return os.Rename(from, to)
			}
			err = rollover.OpenSession(context.Background(), "new-session")
			if failAt == 0 {
				if err != nil {
					t.Fatal(err)
				}
				state, err := rollover.ReplayState(context.Background())
				if err != nil || state.SessionID != "new-session" || state.ProducerID == before.ProducerID || state.AllocatedThrough != 0 || state.RetiredThrough != 0 {
					t.Fatal("new authority was not initialized", err)
				}
				_ = rollover.Close()
				return
			}
			if err == nil {
				t.Fatal("injected rollover failure ignored")
			}
			if failAt == 1 && !bytes.Equal(original, internalBytes(t, rollover)) {
				t.Fatal("failed cleanup changed old bytes")
			}
			if failAt == 2 {
				retired, err := decodeEnvelope(internalBytes(t, rollover))
				if err != nil || retired.SessionID != "session" || len(retired.Entries) != 0 || retired.RetiredBatchFloor != 1 {
					t.Fatal("failed new init did not leave retired old authority", err)
				}
			}
			fresh := NewFileStore(store.path, Options{})
			if err := fresh.OpenSession(context.Background(), "new-session"); err != nil {
				t.Fatal(err)
			}
			defer fresh.Close()
			entries, err := fresh.List(context.Background())
			if err != nil || len(entries) != 0 {
				t.Fatal("rollover renumbered old facts", err)
			}
		})
	}
}

func TestFileStoreLiveReservationsAndInt64Overflow(t *testing.T) {
	store := newInternalStore(t)
	ctx := context.Background()
	for _, want := range []int64{1, 1025} {
		first, last, err := store.ReserveLiveSequences(ctx, 1024)
		if err != nil || first != want || last != want+1023 {
			t.Fatal(first, last, err)
		}
	}
	before, err := store.ReplayState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if before.AllocatedThrough != 0 {
		t.Fatal("live reservation affected batch window")
	}
	if _, _, err := store.ReserveLiveSequences(ctx, 1); err == nil {
		t.Fatal("accepted partial reservation")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	fresh := NewFileStore(store.path, Options{})
	if err := fresh.OpenSession(ctx, "session"); err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	if first, _, err := fresh.ReserveLiveSequences(ctx, 1024); err != nil || first != 2049 {
		t.Fatal("restart reused reserved range", first, err)
	}
	state, err := fresh.readEnvelope()
	if err != nil {
		t.Fatal(err)
	}
	state.LiveReservedThrough = math.MaxInt64 - 1024
	state.LastAllocatedBatchSeq = math.MaxInt64 - 1
	state.RetiredBatchFloor = math.MaxInt64 - 1
	if err := fresh.writeEnvelope(ctx, state); err != nil {
		t.Fatal(err)
	}
	first, last, err := fresh.ReserveLiveSequences(ctx, 1024)
	if err != nil || first != math.MaxInt64-1023 || last != math.MaxInt64 {
		t.Fatal("last full live reservation failed", first, last, err)
	}
	internalEnqueue(t, fresh)
	original := internalBytes(t, fresh)
	if _, _, err := fresh.ReserveLiveSequences(ctx, 1024); err == nil {
		t.Fatal("live sequence wrapped")
	}
	if _, err := fresh.Enqueue(ctx, internalRequest()); err == nil {
		t.Fatal("batch sequence wrapped")
	}
	if !bytes.Equal(original, internalBytes(t, fresh)) {
		t.Fatal("overflow changed durable state")
	}
}

func TestFileStoreRejectsInvalidEnvelopesWithoutRepair(t *testing.T) {
	store := newInternalStore(t)
	internalEnqueue(t, store)
	original := internalBytes(t, store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	tests := map[string]func(map[string]any){
		"version":                func(m map[string]any) { m["version"] = 1 },
		"producer":               func(m map[string]any) { m["producer_id"] = "invalid" },
		"session":                func(m map[string]any) { m["session_id"] = "" },
		"negative_allocated":     func(m map[string]any) { m["last_allocated_batch_seq"] = -1 },
		"floor_above_allocated":  func(m map[string]any) { m["retired_batch_floor"] = 2 },
		"negative_live":          func(m map[string]any) { m["live_reserved_through"] = -1 },
		"null_entries":           func(m map[string]any) { m["entries"] = nil },
		"unknown":                func(m map[string]any) { m["legacy"] = true },
		"wrong_floor":            func(m map[string]any) { m["retired_batch_floor"] = 1 },
		"wrong_id":               func(m map[string]any) { firstEntry(m)["id"] = "old" },
		"missing_attempts":       func(m map[string]any) { delete(firstEntry(m), "attempts") },
		"negative_attempts":      func(m map[string]any) { firstEntry(m)["attempts"] = -1 },
		"missing_time":           func(m map[string]any) { delete(firstEntry(m), "created_at") },
		"duplicate_entry":        func(m map[string]any) { m["entries"] = append(m["entries"].([]any), firstEntry(m)) },
		"wrong_request_session":  func(m map[string]any) { firstRequest(m)["session_id"] = "different" },
		"wrong_request_producer": func(m map[string]any) { firstRequest(m)["producer_id"] = "00000000-0000-0000-0000-000000000000" },
		"wrong_request_revision": func(m map[string]any) { firstRequest(m)["protocol_revision"] = 1 },
		"zero_sequence":          func(m map[string]any) { firstRequest(m)["batch_sequence"] = 0 },
		"entry_above_allocated":  func(m map[string]any) { firstRequest(m)["batch_sequence"] = 2 },
		"retired_entry":          func(m map[string]any) { firstRequest(m)["retired_through"] = 1 },
		"live_in_queue": func(m map[string]any) {
			firstRequest(m)["live_signal"] = map[string]any{"sequence": 1, "fingerprint": "fp"}
		},
		"wrong_fact_identity": func(m map[string]any) {
			firstRequest(m)["heartbeats"].([]any)[0].(map[string]any)["sync_batch_id"] = "old-id"
		},
	}
	for _, field := range []string{"version", "session_id", "producer_id", "last_allocated_batch_seq", "retired_batch_floor", "live_reserved_through", "entries"} {
		field := field
		tests["missing_"+field] = func(m map[string]any) { delete(m, field) }
		tests["null_"+field] = func(m map[string]any) { m[field] = nil }
	}
	for name, corrupt := range tests {
		t.Run(name, func(t *testing.T) {
			var m map[string]any
			if err := json.Unmarshal(original, &m); err != nil {
				t.Fatal(err)
			}
			corrupt(m)
			payload, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(store.path, payload, 0o600); err != nil {
				t.Fatal(err)
			}
			fresh := NewFileStore(store.path, Options{})
			if err := fresh.OpenSession(context.Background(), "session"); err == nil {
				_ = fresh.Close()
				t.Fatal("invalid queue accepted")
			}
			if !bytes.Equal(payload, internalBytes(t, fresh)) {
				t.Fatal("invalid queue repaired itself")
			}
		})
	}
	duplicate := bytes.Replace(original, []byte(`"version":2`), []byte(`"version":1,"version":2`), 1)
	if _, err := decodeEnvelope(duplicate); err == nil {
		t.Fatal("duplicate replay authority key accepted")
	}
}

func firstEntry(m map[string]any) map[string]any   { return m["entries"].([]any)[0].(map[string]any) }
func firstRequest(m map[string]any) map[string]any { return firstEntry(m)["request"].(map[string]any) }

func TestFileStoreDefaultBoundsAndEnvelopeMinimum(t *testing.T) {
	store := newInternalStore(t)
	if store.opts.MaxEntries != 65536 || store.opts.MaxAge != 72*time.Hour || store.opts.MaxBytes != 64*1024*1024 {
		t.Fatal("default bounds changed")
	}
	original := internalBytes(t, store)
	store.opts.MaxBytes = len(original) - 1
	if err := store.Prune(context.Background()); !errors.Is(err, errEntryExceedsCapacity) {
		t.Fatal("metadata did not count against capacity", err)
	}
	if !bytes.Equal(original, internalBytes(t, store)) {
		t.Fatal("oversized metadata rewrote queue")
	}
}

func TestFileStorePrunesLargeOversizedPersistedBacklogInLinearPass(t *testing.T) {
	store := newInternalStore(t)
	state, err := store.readEnvelope()
	if err != nil {
		t.Fatal(err)
	}
	const total = 32768
	state.LastAllocatedBatchSeq = total
	state.Entries = make([]Entry, total)
	now := time.Now().UTC()
	for i := range state.Entries {
		r := internalRequest()
		r.ProtocolRevision, r.ProducerID, r.BatchSequence, r.AllocatedThrough = 2, state.ProducerID, int64(i+1), total
		id := state.ProducerID + ":" + strconv.Itoa(i+1)
		setBatchIdentity(&r, id)
		state.Entries[i] = Entry{ID: id, CreatedAt: now.Add(time.Duration(i)), Request: r}
	}
	if err := store.writeEnvelope(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	store.opts.MaxBytes = len(internalBytes(t, store)) / 2
	id := internalEnqueue(t, store)
	entries, err := store.List(context.Background())
	if err != nil || len(entries) == 0 || entries[len(entries)-1].ID != id || len(internalBytes(t, store)) > store.opts.MaxBytes {
		t.Fatal("large backlog pruning failed", err)
	}
	got, err := store.ReplayState(context.Background())
	if err != nil || got.RetiredThrough != entries[0].Request.BatchSequence-1 {
		t.Fatal("large backlog floor incorrect", err)
	}
}

func TestFileStoreEnvelopeBytesCanRejectEntryThatBareArrayWouldFit(t *testing.T) {
	store := newInternalStore(t)
	internalEnqueue(t, store)
	state, err := store.readEnvelope()
	if err != nil {
		t.Fatal(err)
	}
	bare, err := json.Marshal(state.Entries)
	if err != nil {
		t.Fatal(err)
	}
	store.opts.MaxBytes = len(bare) + 64
	original := internalBytes(t, store)
	request := state.Entries[0].Request
	candidate := state.Entries[0]
	candidate.Request = request
	candidate.Request.BatchSequence = 2
	candidate.Request.AllocatedThrough = 2
	candidate.ID = state.ProducerID + ":2"
	setBatchIdentity(&candidate.Request, candidate.ID)
	candidate.CreatedAt = candidate.CreatedAt.Add(time.Second)
	bareCandidate, err := json.Marshal([]Entry{candidate})
	if err != nil || len(bareCandidate) > store.opts.MaxBytes {
		t.Fatal("bare candidate must fit for this test to exercise envelope overhead", err)
	}
	store.SetNowForTest(func() time.Time { return candidate.CreatedAt })
	if _, err := store.Enqueue(context.Background(), request); err == nil {
		t.Fatal("envelope byte bound ignored")
	}
	if !bytes.Equal(original, internalBytes(t, store)) {
		t.Fatal("failed enqueue changed durable authority")
	}
}

func TestFileStoreSyncsCreatedDirectoryParentsAndTarget(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "agent", "state", "queue.json")
	store := NewFileStore(path, Options{})
	var synced []string
	store.syncDir = func(dir string) error {
		synced = append(synced, filepath.Clean(dir))
		return syncDirectory(dir)
	}
	if err := store.OpenSession(context.Background(), "session"); err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	want := []string{root, filepath.Join(root, "agent"), filepath.Join(root, "agent", "state")}
	if !reflect.DeepEqual(synced, want) {
		t.Fatalf("synced directories = %#v, want %#v", synced, want)
	}
}

func TestFileStoreAmbiguousSessionCleanupDoesNotInitializeNewAuthority(t *testing.T) {
	store := newInternalStore(t)
	internalEnqueue(t, store)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	rollover := NewFileStore(store.path, Options{})
	rollover.syncDir = func(string) error { return errors.New("cleanup directory sync failed") }
	if err := rollover.OpenSession(context.Background(), "new-session"); !errors.Is(err, ErrAmbiguousDurability) {
		t.Fatal(err)
	}
	visible, err := decodeEnvelope(internalBytes(t, rollover))
	if err != nil || visible.SessionID != "session" || len(visible.Entries) != 0 || visible.RetiredBatchFloor != 1 {
		t.Fatal("ambiguous cleanup initialized or retained old payload", err)
	}
	fresh := NewFileStore(store.path, Options{})
	if err := fresh.OpenSession(context.Background(), "new-session"); err != nil {
		t.Fatal("failed OpenSession leaked its lock", err)
	}
	defer fresh.Close()
	state, err := fresh.ReplayState(context.Background())
	if err != nil || state.SessionID != "new-session" || state.AllocatedThrough != 0 {
		t.Fatal("fresh rollover recovery failed", err)
	}
}
