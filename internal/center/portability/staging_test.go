package portability

import (
	"context"
	"errors"
	"io"
	"testing"

	"houfeng/internal/center/attachments"
)

func TestPortabilityLeasedBlobStoreStopsNewBytesAfterRevoke(t *testing.T) {
	t.Parallel()

	inner, err := attachments.NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalBlobStore() error = %v", err)
	}
	store := NewLeasedBlobStore(inner)
	payload := []byte("staged-export-bytes")
	if _, err := store.Stage(context.Background(), "rej_lease1", payload); err != nil {
		t.Fatalf("Stage() error = %v", err)
	}

	reader, _, err := store.OpenLeased(context.Background(), "rej_lease1")
	if err != nil {
		t.Fatalf("OpenLeased() error = %v", err)
	}
	defer reader.Close()
	first := make([]byte, 6)
	if _, err := io.ReadFull(reader, first); err != nil {
		t.Fatalf("first Read() error = %v", err)
	}
	store.Revoke("rej_lease1")
	rest, err := io.ReadAll(reader)
	if !errors.Is(err, ErrExportLeaseRevoked) {
		t.Fatalf("revoked Read() = %q, %v, want ErrExportLeaseRevoked", rest, err)
	}

	if _, _, err := store.OpenLeased(context.Background(), "rej_lease1"); !errors.Is(err, ErrExportLeaseRevoked) {
		t.Fatalf("OpenLeased after revoke error = %v, want ErrExportLeaseRevoked", err)
	}
}

func TestPortabilityLeasedBlobStoreStageImportOpensAfterLeaseDrop(t *testing.T) {
	t.Parallel()

	inner, err := attachments.NewLocalBlobStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalBlobStore() error = %v", err)
	}
	store := NewLeasedBlobStore(inner)
	payload := []byte("staged-import-bytes")
	version, err := store.StageImport(context.Background(), "rij_lease1", payload)
	if err != nil {
		t.Fatalf("StageImport() error = %v", err)
	}
	store.dropLease("rij_lease1")
	reader, opened, err := store.OpenPublished(context.Background(), "rij_lease1", version)
	if err != nil {
		t.Fatalf("OpenPublished() error = %v", err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil || string(got) != string(payload) || opened.Key != version.Key {
		t.Fatalf("OpenPublished() = %q %v key=%q", got, err, opened.Key)
	}
}
