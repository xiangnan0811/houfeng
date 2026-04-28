package syncqueue

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"houfeng/internal/contracts/agentapi"
)

func TestFileStoreSyncsCreatedDirectoryParentsAndTarget(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "agent", "state", "sync-buffer.json")
	oldSyncDir := syncDir
	var synced []string
	syncDir = func(dir string) error {
		synced = append(synced, filepath.Clean(dir))
		return nil
	}
	defer func() { syncDir = oldSyncDir }()

	store := NewFileStore(path, Options{MaxEntries: 10, MaxAge: time.Hour})
	if _, err := store.Enqueue(context.Background(), agentapi.SyncRequest{NodeID: "nd_001", SyncToken: "sync-token-001"}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	want := []string{
		filepath.Clean(root),
		filepath.Join(root, "agent"),
		filepath.Join(root, "agent", "state"),
	}
	if !reflect.DeepEqual(synced, want) {
		t.Fatalf("synced dirs = %#v, want %#v", synced, want)
	}
}
