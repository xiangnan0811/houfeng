//go:build linux

package deploy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Explicitly opt in with a locally present image ID containing sleep. This test
// creates and removes only its own uniquely named containers and network.
func TestOrdinaryFenceDockerRestoresDatabaseNetworks(t *testing.T) {
	if os.Getuid() != 0 || os.Geteuid() != 0 {
		t.Skip("requires root on the Docker daemon host to inspect project and foreign mount paths")
	}
	image := os.Getenv("HOUFENG_ORDINARY_FENCE_TEST_IMAGE_ID")
	if !strings.HasPrefix(image, "sha256:") || !fenceID.MatchString(strings.TrimPrefix(image, "sha256:")) {
		t.Skip("requires HOUFENG_ORDINARY_FENCE_TEST_IMAGE_ID=sha256:<local image id>")
	}
	ctx := context.Background()
	if err := fenceDockerBinary(); err != nil {
		t.Fatal(err)
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	suffix := hex.EncodeToString(random[:])
	project := "ordinarytest" + suffix
	rid := "ordinarytest" + suffix
	networkName := "ordinarytest-" + suffix
	work, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	control := filepath.Join(work, ".ordinary-fences", rid)
	maintenance := filepath.Join(work, "maintenance")
	if err := os.MkdirAll(control, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(maintenance, 0o700); err != nil {
		t.Fatal(err)
	}
	var resources []string
	var originalNetworkID string
	t.Cleanup(func() {
		for _, id := range resources {
			_, _ = fenceDocker(ctx, "rm", "-f", id)
		}
		request := ComposeOfflineFenceRequest{RecoveryID: rid, Project: project}
		if network, exists, err := fenceFindNetwork(ctx, request); err == nil && exists {
			_, _ = fenceDocker(ctx, "network", "rm", network.ID)
		}
		if originalNetworkID != "" {
			_, _ = fenceDocker(ctx, "network", "rm", originalNetworkID)
		}
	})
	networkOutput, err := fenceDocker(ctx, "network", "create", networkName)
	if err != nil {
		t.Fatal(err)
	}
	originalNetworkID = strings.TrimSpace(string(networkOutput))
	if !fenceID.MatchString(originalNetworkID) {
		t.Fatalf("unexpected network ID %q", originalNetworkID)
	}
	create := func(service string) string {
		t.Helper()
		out, err := fenceDocker(ctx, "run", "--detach", "--pull=never", "--restart=unless-stopped", "--network", networkName,
			"--network-alias", service, "--label", "com.docker.compose.project="+project,
			"--label", "com.docker.compose.service="+service, "--label", "com.docker.compose.project.working_dir="+work,
			"--mount", "type=bind,src="+maintenance+",dst=/fixture-maintenance,readonly",
			image, "sleep", "300")
		if err != nil {
			t.Fatalf("create owned %s container: %v", service, err)
		}
		id := strings.TrimSpace(string(out))
		if !fenceID.MatchString(id) {
			t.Fatalf("unexpected container ID %q", id)
		}
		resources = append(resources, id)
		return id
	}
	db := create("db")
	business := create("business")
	request := ComposeOfflineFenceRequest{RecoveryID: rid, Project: project, WorkingDirectory: work, DatabaseContainerID: db, ControlConfigPath: filepath.Join(control, "request.json")}
	overlapping := request
	overlapping.ControlConfigPath = filepath.Join(maintenance, "request.json")
	if _, err := ordinaryCaptureFenceIntent(ctx, overlapping); err == nil {
		t.Fatal("fence accepted control directory inside a project bind mount")
	}
	if _, err := ordinaryCaptureFenceIntent(ctx, request); err != nil {
		t.Fatalf("fence rejected separate root-private control directory: %v", err)
	}
	original, err := fenceInspect(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	var fenced ComposeOfflineFence
	if err := fenceDockerPerform(ctx, request, &fenced); err != nil {
		t.Fatalf("fence: %v", err)
	}
	stopped, err := fenceInspect(ctx, business)
	if err != nil || stopped.state != "exited" || stopped.anchor.Restart.Name != "no" {
		t.Fatalf("business was not stopped and disabled: %+v, %v", stopped.anchor, err)
	}
	if err := ordinaryRestoreFenceNetworks(ctx, request); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if err := ordinaryRestoreFenceNetworks(ctx, request); err != nil {
		t.Fatalf("restore replay: %v", err)
	}
	after, err := fenceInspect(ctx, db)
	if err != nil || !ordinaryExactNetworks(original.anchor.Networks, after.anchor.Networks) {
		t.Fatalf("original database networks not restored: %+v %v", after.anchor.Networks, err)
	}
	stopped, err = fenceInspect(ctx, business)
	if err != nil || stopped.state != "exited" || stopped.anchor.Restart.Name != "no" {
		t.Fatalf("business restarted: %+v, %v", stopped.anchor, err)
	}
}

// Exercise the actual Docker wait and inspect evidence used during target
// recreation; the image must provide sleep and both containers are fixture-owned.
func TestOrdinaryOneShotDockerRequiresSuccessfulExit(t *testing.T) {
	image := os.Getenv("HOUFENG_ORDINARY_FENCE_TEST_IMAGE_ID")
	if !strings.HasPrefix(image, "sha256:") || !fenceID.MatchString(strings.TrimPrefix(image, "sha256:")) {
		t.Skip("requires HOUFENG_ORDINARY_FENCE_TEST_IMAGE_ID=sha256:<local image id>")
	}
	ctx := context.Background()
	if err := fenceDockerBinary(); err != nil {
		t.Fatal(err)
	}
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	project := "ordinaryoneshot" + hex.EncodeToString(random[:])
	work, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	request := ordinaryUpgradeRequest{Project: project, WorkingDirectory: work, TargetImageID: image}
	var resources []string
	t.Cleanup(func() {
		for _, id := range resources {
			_, _ = fenceDocker(ctx, "rm", "-f", id)
		}
	})
	create := func(service, duration string) {
		t.Helper()
		out, err := fenceDocker(ctx, "run", "--detach", "--pull=never", "--restart=no", "--network", "bridge", "--label", "com.docker.compose.project="+project, "--label", "com.docker.compose.service="+service, "--label", "com.docker.compose.project.working_dir="+work, image, "sleep", duration)
		if err != nil {
			t.Fatalf("start fixture %s: %v", service, err)
		}
		id := strings.TrimSpace(string(out))
		if !fenceID.MatchString(id) {
			t.Fatalf("unexpected container ID %q", id)
		}
		resources = append(resources, id)
	}
	create("fixture-ok", "1")
	if err := ordinaryWaitOneShot(ctx, ordinaryUpgradeIntent{Request: request}, "fixture-ok"); err != nil {
		t.Fatalf("successful target one-shot rejected: %v", err)
	}
	create("fixture-fail", "invalid")
	if err := ordinaryWaitOneShot(ctx, ordinaryUpgradeIntent{Request: request}, "fixture-fail"); err == nil {
		t.Fatal("nonzero target one-shot accepted")
	}
}
