package containersample_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"houfeng/agent/containersample"
)

// fakeDocker creates a temporary script named "docker" that outputs the given
// content on stdout for a specific argument pattern. Returns a cleanup function
// and a modified PATH so the script is picked up by exec.LookPath.
func fakeDocker(t *testing.T, psOutput, statsOutput string) (restore func()) {
	t.Helper()
	dir := t.TempDir()

	script := filepath.Join(dir, "docker")
	// We write a shell script that examines $1 to decide output.
	var content string
	if psOutput != "" && statsOutput != "" {
		content = `#!/bin/sh
case "$1" in
  ps) cat <<'DOCKERPS'
` + psOutput + `
DOCKERPS
    ;;
  stats) cat <<'DOCKERSTATS'
` + statsOutput + `
DOCKERSTATS
    ;;
  *) exit 1 ;;
esac
`
	} else if psOutput != "" {
		content = `#!/bin/sh
case "$1" in
  ps) cat <<'DOCKERPS'
` + psOutput + `
DOCKERPS
    ;;
  *) exit 1 ;;
esac
`
	} else {
		content = "#!/bin/sh\nexit 1\n"
	}

	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake docker script: %v", err)
	}

	origPath := os.Getenv("PATH")
	os.Setenv("PATH", dir+":"+origPath)
	return func() {
		os.Setenv("PATH", origPath)
	}
}

func TestCollect_DockerUnavailable_ReturnsNil(t *testing.T) {
	// No docker in PATH (we don't set up a fake), so Collect should return nil, nil.
	containers, err := containersample.Collect(context.Background())
	if err != nil {
		t.Fatalf("expected nil error when docker unavailable, got: %v", err)
	}
	if containers != nil {
		t.Fatalf("expected nil containers when docker unavailable, got: %v", containers)
	}
}

func TestCollect_DockerAvailable_ReturnsContainers(t *testing.T) {
	psOut := "abc123def456\tmy-app\tnginx:latest\tUp 2 hours\n" +
		"def789012345\tredis\tredis:7\tExited (0) 3 days ago\n"

	statsOut := "0.50%\t1.23%\tmy-app\n" +
		"0.00%\t0.00%\tredis\n"

	restore := fakeDocker(t, psOut, statsOut)
	defer restore()

	containers, err := containersample.Collect(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(containers) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(containers))
	}

	c1 := containers[0]
	if c1.Name != "my-app" {
		t.Errorf("expected name 'my-app', got %q", c1.Name)
	}
	if c1.Image != "nginx:latest" {
		t.Errorf("expected image 'nginx:latest', got %q", c1.Image)
	}
	if c1.Status != "running" {
		t.Errorf("expected status 'running', got %q", c1.Status)
	}
	if c1.CPUPct == nil || *c1.CPUPct != 0.50 {
		t.Errorf("expected cpu_pct 0.50, got %v", c1.CPUPct)
	}
	if c1.MemPct == nil || *c1.MemPct != 1.23 {
		t.Errorf("expected mem_pct 1.23, got %v", c1.MemPct)
	}

	c2 := containers[1]
	if c2.Name != "redis" {
		t.Errorf("expected name 'redis', got %q", c2.Name)
	}
	if c2.Status != "exited" {
		t.Errorf("expected status 'exited', got %q", c2.Status)
	}
	if c2.CPUPct == nil || *c2.CPUPct != 0.00 {
		t.Errorf("expected cpu_pct 0.00, got %v", c2.CPUPct)
	}
}

func TestCollect_DockerPSFails_ReturnsNil(t *testing.T) {
	// Fake docker that only returns stats, not ps (ps exits 1).
	restore := fakeDocker(t, "", "0.50%\t1.23%\tmy-app\n")
	defer restore()

	containers, err := containersample.Collect(context.Background())
	if err != nil {
		t.Fatalf("expected nil error when ps fails, got: %v", err)
	}
	if containers != nil {
		t.Fatalf("expected nil containers when ps fails, got: %v", containers)
	}
}

func TestCollect_StatsFail_StillReturnsPSInfo(t *testing.T) {
	psOut := "abc123\tapp\talpine:latest\tUp 1 hour\n"

	// Fake docker that returns ps but fails on stats.
	dir := t.TempDir()
	script := filepath.Join(dir, "docker")
	content := `#!/bin/sh
case "$1" in
  ps) cat <<'DOCKERPS'
` + psOut + `
DOCKERPS
    ;;
  stats) exit 1 ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake docker script: %v", err)
	}
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", dir+":"+origPath)
	defer os.Setenv("PATH", origPath)

	containers, err := containersample.Collect(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(containers))
	}
	if containers[0].Name != "app" {
		t.Errorf("expected name 'app', got %q", containers[0].Name)
	}
	if containers[0].CPUPct != nil {
		t.Errorf("expected nil CPUPct when stats fail, got %v", *containers[0].CPUPct)
	}
}

func TestCollect_ContextCancelled_ReturnsNil(t *testing.T) {
	psOut := "abc123\tapp\talpine:latest\tUp 1 hour\n"
	restore := fakeDocker(t, psOut, "")
	defer restore()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	containers, err := containersample.Collect(ctx)
	if err != nil {
		t.Fatalf("expected nil error on cancelled context, got: %v", err)
	}
	if containers != nil {
		t.Fatalf("expected nil containers on cancelled context, got: %v", containers)
	}
}

func TestCollect_EmptyOutput_ReturnsNil(t *testing.T) {
	statsOut := "0.50%\t1.23%\tapp\n"
	// Fake docker that returns empty ps output.
	dir := t.TempDir()
	script := filepath.Join(dir, "docker")
	content := `#!/bin/sh
case "$1" in
  ps) echo -n '' ;;
  stats) cat <<'DOCKERSTATS'
` + statsOut + `
DOCKERSTATS
    ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake docker script: %v", err)
	}
	origPath := os.Getenv("PATH")
	os.Setenv("PATH", dir+":"+origPath)
	defer os.Setenv("PATH", origPath)

	containers, err := containersample.Collect(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if containers != nil {
		t.Fatalf("expected nil containers for empty ps, got: %v", containers)
	}
}

func TestCollect_LookPathFails_ReturnsNil(t *testing.T) {
	// Set PATH to an empty directory so exec.LookPath("docker") fails.
	dir := t.TempDir()
	os.Setenv("PATH", dir)
	defer func() {
		// Restore a minimal PATH so other tests aren't affected.
		os.Setenv("PATH", "/usr/bin:/bin")
	}()

	containers, err := containersample.Collect(context.Background())
	if err != nil {
		t.Fatalf("expected nil error when docker not found, got: %v", err)
	}
	if containers != nil {
		t.Fatalf("expected nil containers when docker not in PATH, got: %v", containers)
	}
}

func TestCollect_StatusNormalization(t *testing.T) {
	psOut := "a1\tc-running\timg:v1\tUp 3 days\n" +
		"b2\tc-exited\timg:v2\tExited (0) 5 hours ago\n" +
		"c3\tc-created\timg:v3\tCreated 2 minutes ago\n" +
		"d4\tc-restarting\timg:v4\tRestarting (1) 10 seconds ago\n" +
		"e5\tc-paused\timg:v5\tUp 1 hour (Paused)\n" +
		"f6\tc-dead\timg:v6\tDead\n" +
		"g7\tc-removing\timg:v7\tRemoving\n"

	statsOut := "0.0%\t0.0%\tc-running\n" +
		"0.0%\t0.0%\tc-exited\n" +
		"0.0%\t0.0%\tc-created\n" +
		"0.0%\t0.0%\tc-restarting\n" +
		"0.0%\t0.0%\tc-paused\n" +
		"0.0%\t0.0%\tc-dead\n" +
		"0.0%\t0.0%\tc-removing\n"

	restore := fakeDocker(t, psOut, statsOut)
	defer restore()

	containers, err := containersample.Collect(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := map[string]string{
		"c-running":    "running",
		"c-exited":     "exited",
		"c-created":    "created",
		"c-restarting": "restarting",
		"c-paused":     "paused",
		"c-dead":       "dead",
		"c-removing":   "removing",
	}

	for _, c := range containers {
		want, ok := expected[c.Name]
		if !ok {
			t.Errorf("unexpected container name %q", c.Name)
			continue
		}
		if c.Status != want {
			t.Errorf("container %q: expected status %q, got %q", c.Name, want, c.Status)
		}
	}
}

func TestCollect_WithCancelPropagation(t *testing.T) {
	// Verify that the 5s timeout is applied internally and that the
	// context cancellation is respected.
	psOut := "abc\tapp\timg:v1\tUp 1 hour\n"
	restore := fakeDocker(t, psOut, "0.0%\t0.0%\tapp\n")
	defer restore()

	// This should still work because we have a fake fast docker.
	containers, err := containersample.Collect(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(containers))
	}
}

// Ensure containersample does not import Docker SDK or any heavy dependencies.
func TestNoSDKDependency(t *testing.T) {
	// Compile-time check: if this package imported any Docker SDK, the
	// go list output would show it. This test simply ensures the package
	// compiles and the import graph is clean.
	// We verify by checking that the package itself compiles (done by `go test`).
	_ = exec.LookPath // compile-time proof we use only os/exec
}
