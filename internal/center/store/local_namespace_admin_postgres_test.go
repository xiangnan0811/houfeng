//go:build linux

package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
	"houfeng/internal/center/attachments"
)

func TestPostgresIntegrationLocalNamespaceAdminLifecycle(t *testing.T) {
	ctx := context.Background()
	fixture := newRecordsPostgresFixture(t, ctx)
	repositoryRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "houfeng-record-platform-admin")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/houfeng-record-platform-admin")
	build.Dir = repositoryRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build admin: %v\n%s", err, output)
	}
	root := filepath.Join(t.TempDir(), "blob-root")
	request := map[string]any{"root": root, "namespace_id": localBlobFixtureUUID(t), "incarnation_id": localBlobFixtureUUID(t),
		"request_id": localBlobFixtureUUID(t), "owner_uid": os.Geteuid(), "owner_gid": os.Getegid(),
		"database_url": contentProcessorCrashDatabaseURL(fixture.directRoleConfig(t, fixture.admin, "local-admin-cli"))}
	invoke := func(request map[string]any) ([]byte, error) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "request.json")
		body, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		clear(body)
		output, err := exec.CommandContext(ctx, binary, "local-namespace", "provision", "--request-file", path).CombinedOutput()
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("credential request not consumed: %v", statErr)
		}
		return output, err
	}
	first, err := invoke(request)
	if err != nil {
		t.Fatalf("fresh admin provision: %v\n%s", err, first)
	}
	replay, err := invoke(request)
	if err != nil || string(replay) != string(first) {
		t.Fatalf("admin exact replay: %v\n%s", err, replay)
	}
	var result struct {
		NamespaceID string `json:"namespace_id"`
		Admission   string `json:"admission_state"`
	}
	if err := json.Unmarshal(first, &result); err != nil || result.NamespaceID != request["namespace_id"] || result.Admission != "active" {
		t.Fatalf("invalid admin admission: %v", err)
	}
	var fs unix.Statfs_t
	if err := unix.Statfs(root, &fs); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("HOUFENG_RECORDS_REQUIRE_BTRFS") == "1" && fs.Type != unix.BTRFS_SUPER_MAGIC {
		t.Fatalf("BlobRoot filesystem=%x, want btrfs", fs.Type)
	}
	t.Logf("actual BlobRoot filesystem type=%x", fs.Type)
	runtime := NewPostgresLocalBlobRepository(fixture.openDirectRuntimePool(t, ctx, "local-admin-reopen", 2), nil, nil)
	for range 2 {
		blob, err := attachments.NewLocalBlobStore(ctx, root, runtime)
		if err != nil {
			t.Fatal(err)
		}
		if blob.Namespace().NamespaceID != result.NamespaceID {
			t.Fatal("runtime changed admitted namespace")
		}
		if err := blob.Close(); err != nil {
			t.Fatal(err)
		}
	}
	wrongRoot := filepath.Join(t.TempDir(), "denied-root")
	request["root"] = wrongRoot
	request["namespace_id"], request["incarnation_id"], request["request_id"] = localBlobFixtureUUID(t), localBlobFixtureUUID(t), localBlobFixtureUUID(t)
	request["database_url"] = contentProcessorCrashDatabaseURL(fixture.directRoleConfig(t, fixture.runtime, "local-admin-denied"))
	if _, err := invoke(request); err == nil {
		t.Fatal("runtime login provisioned namespace")
	}
	if _, err := os.Stat(wrongRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unauthorized command created root: %v", err)
	}
}
