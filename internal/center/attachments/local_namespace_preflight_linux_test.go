//go:build linux

package attachments

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPreflightLocalNamespacePreservesFreshAndExistingStates(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "blobs")
	request := LocalProvisionRequest{Root: root, NamespaceID: "11111111-1111-1111-1111-111111111111", IncarnationID: "22222222-2222-2222-2222-222222222222", RequestID: "33333333-3333-3333-3333-333333333333", OwnerUID: uint32(os.Geteuid()), OwnerGID: uint32(os.Getegid())}
	if ColdReleasedLocalNamespaceRoot(root, request.OwnerUID, request.OwnerGID) {
		t.Fatal("cold path accepted missing root")
	}
	if err := PreflightLocalNamespace(ctx, request, nil); err != nil {
		t.Fatalf("absent root rejected: %v", err)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatal("preflight created the root")
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if ColdReleasedLocalNamespaceRoot(root, request.OwnerUID, request.OwnerGID) {
		t.Fatal("cold path accepted empty root")
	}
	if err := os.Mkdir(filepath.Join(root, "sha256"), 0o700); err != nil {
		t.Fatal(err)
	}
	if !ColdReleasedLocalNamespaceRoot(root, request.OwnerUID, request.OwnerGID) {
		t.Fatal("cold path rejected released skeleton")
	}
	if err := PreflightLocalNamespace(ctx, request, nil); err != nil {
		t.Fatalf("empty released skeleton rejected: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "stray"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ColdReleasedLocalNamespaceRoot(root, request.OwnerUID, request.OwnerGID) {
		t.Fatal("cold path accepted stray root entry")
	}
	if err := PreflightLocalNamespace(ctx, request, nil); err == nil {
		t.Fatal("stray root file accepted")
	}
	if err := os.Remove(filepath.Join(root, "stray")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sha256", "blob"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ColdReleasedLocalNamespaceRoot(root, request.OwnerUID, request.OwnerGID) {
		t.Fatal("cold path accepted populated skeleton")
	}
	if err := PreflightLocalNamespace(ctx, request, nil); err == nil {
		t.Fatal("populated skeleton accepted")
	}
	if err := os.Remove(filepath.Join(root, "sha256", "blob")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "sha256"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := PreflightLocalNamespace(ctx, request, nil); err == nil {
		t.Fatal("wrong skeleton mode accepted")
	}
	if err := os.Chmod(filepath.Join(root, "sha256"), 0o700); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(root, localProvisionLockName)
	if err := os.WriteFile(lockPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := PreflightLocalNamespace(ctx, request, nil); err != nil {
		t.Fatalf("recoverable empty lock rejected: %v", err)
	}
	if stat, err := os.Stat(lockPath); err != nil || stat.Size() != 0 {
		t.Fatal("preflight changed the interrupted lock")
	}
	repository := &localBlobTestRepository{}
	namespace, err := ProvisionLocalNamespace(ctx, request, repository)
	if err != nil {
		t.Fatal(err)
	}
	if err := PreflightLocalNamespace(ctx, request, &namespace); err != nil {
		t.Fatalf("exact admitted namespace rejected: %v", err)
	}
	other := request
	other.RequestID = "44444444-4444-4444-4444-444444444444"
	if err := PreflightLocalNamespace(ctx, other, &namespace); err == nil {
		t.Fatal("mismatched recorded identity accepted")
	}
}
