//go:build linux

package deploy

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestOrdinaryRootMarkerBootstrapRecovery(t *testing.T) {
	base := os.Getenv("HOUFENG_ORDINARY_GUARD_FIXTURE_ROOT")
	if base == "" || os.Geteuid() != 0 {
		t.Skip("requires root and HOUFENG_ORDINARY_GUARD_FIXTURE_ROOT")
	}
	parent, err := os.MkdirTemp(base, "ordinary-marker-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(parent) })
	if err := os.Chmod(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "ordinary")
	if err := os.Mkdir(root, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(root, composeLaunchPublisherUID, composeLaunchRuntimeGID); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := ProvisionOrdinaryComposeGuard(root); err != nil {
		t.Fatal(err)
	}
	parentFD, err := unix.Open(parent, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(parentFD)
	marker, exists, err := ordinaryReadRootMarker(parentFD)
	if err != nil || !exists {
		t.Fatalf("read marker: %v", err)
	}
	guardPath := filepath.Join(root, ordinaryGuardFile)
	before, err := os.Stat(guardPath)
	if err != nil {
		t.Fatal(err)
	}
	// Model a crash after PREPARED was recorded and before the lock rename.
	marker.Phase = "PREPARED"
	if err := ordinaryWriteRootMarker(parentFD, marker, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, ordinaryDispositionFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(guardPath, filepath.Join(parent, marker.BootstrapName)); err != nil {
		t.Fatal(err)
	}
	if err := WithOrdinaryComposeGuard(context.Background(), root, false, func() error { t.Error("prepared marker admitted reader"); return nil }); err == nil {
		t.Fatal("prepared marker admitted reader")
	}
	if err := ProvisionOrdinaryComposeGuard(root); err != nil {
		t.Fatalf("resume prepared anchor: %v", err)
	}
	after, err := os.Stat(guardPath)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("bootstrap recovery changed guard inode")
	}
	// Model a cut after linking the new name but before dropping the root name.
	marker.Phase = "PREPARED"
	if err := ordinaryWriteRootMarker(parentFD, marker, false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, ordinaryDispositionFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(guardPath, filepath.Join(parent, marker.BootstrapName)); err != nil {
		t.Fatal(err)
	}
	if err := ProvisionOrdinaryComposeGuard(root); err != nil {
		t.Fatalf("resume linked anchor: %v", err)
	}
	if after, err := os.Stat(guardPath); err != nil || !os.SameFile(before, after) {
		t.Fatal("link recovery changed guard inode")
	}
	// Model a cut during CLEAN record creation after the lock rename.
	marker.Phase = "PREPARED"
	if err := ordinaryWriteRootMarker(parentFD, marker, false); err != nil {
		t.Fatal(err)
	}
	disposition := filepath.Join(root, ordinaryDispositionFile)
	body, err := os.ReadFile(disposition)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(disposition, body[:len(body)/2], 0o640); err != nil {
		t.Fatal(err)
	}
	if err := ProvisionOrdinaryComposeGuard(root); err != nil {
		t.Fatalf("resume partial CLEAN record: %v", err)
	}
	if err := WithOrdinaryComposeGuard(context.Background(), root, false, func() error { return nil }); err != nil {
		t.Fatalf("read after recovered bootstrap: %v", err)
	}
}
