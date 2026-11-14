package portability

import (
	"context"
	"errors"
	"io"
	"testing"
)

func TestPortabilityLeasedBlobStoreStopsNewBytesAfterRevoke(t *testing.T) {
	t.Parallel()

	inner := newPortabilityMemoryBlob()
	store := NewLeasedBlobStore(inner, &portabilityMemoryWriter{blob: inner, namespace: portabilityTestNamespace})
	payload := []byte("staged-export-bytes")
	if _, err := store.Stage(context.Background(), portabilityStageRequest("export_artifact", "rej_lease1"), payload); err != nil {
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

	inner := newPortabilityMemoryBlob()
	store := NewLeasedBlobStore(inner, &portabilityMemoryWriter{blob: inner, namespace: portabilityTestNamespace})
	payload := []byte("staged-import-bytes")
	version, err := store.StageImport(context.Background(), portabilityStageRequest("import_archive", "rij_lease1"), payload)
	if err != nil {
		t.Fatalf("StageImport() error = %v", err)
	}
	store.dropLease("rij_lease1")
	reader, opened, err := store.OpenPublished(context.Background(), "rij_lease1", version.Version, version.NamespaceID)
	if err != nil {
		t.Fatalf("OpenPublished() error = %v", err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil || string(got) != string(payload) || opened != version.Version {
		t.Fatalf("OpenPublished() = %q %v key=%q", got, err, opened.Key)
	}
}

func TestPortabilityLeasedBlobStoreStageUsesS3TemporaryKeyContract(t *testing.T) {
	t.Parallel()

	inner := newPortabilityMemoryBlob()
	capture := &capturingBlobStore{inner: inner}
	store := NewLeasedBlobStore(capture, &portabilityMemoryWriter{blob: capture, namespace: portabilityTestNamespace})
	if _, err := store.Stage(context.Background(), portabilityStageRequest("export_artifact", "rej_tmp1"), []byte("export-bytes")); err != nil {
		t.Fatalf("Stage() error = %v", err)
	}
	if _, err := store.StageImport(context.Background(), portabilityStageRequest("import_archive", "rij_tmp1"), []byte("import-bytes")); err != nil {
		t.Fatalf("StageImport() error = %v", err)
	}
	if len(capture.puts) != 2 {
		t.Fatalf("Put count = %d, want 2", len(capture.puts))
	}
	requireBlobTemporaryKey(t, capture.puts[0].TemporaryKey)
	requireBlobTemporaryKey(t, capture.puts[1].TemporaryKey)
}
