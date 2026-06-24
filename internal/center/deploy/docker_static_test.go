package deploy_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDockerRuntimeRunsAsHoufengUser(t *testing.T) {
	root := repoRoot(t)
	dockerfile := readText(t, filepath.Join(root, "Dockerfile"))

	if !strings.Contains(dockerfile, "\nUSER houfeng:houfeng\n") {
		t.Fatal("Dockerfile runtime stage must set USER houfeng:houfeng")
	}
	if strings.Contains(dockerfile, "gosu") {
		t.Fatal("Dockerfile runtime stage must not depend on gosu privilege dropping")
	}
}

func TestDockerEntrypointDoesNotDropPrivilegesAtRuntime(t *testing.T) {
	root := repoRoot(t)
	entrypoint := readText(t, filepath.Join(root, "scripts", "docker-entrypoint.sh"))

	for _, forbidden := range []string{"gosu", "id -u", "install -d -o"} {
		if strings.Contains(entrypoint, forbidden) {
			t.Fatalf("docker entrypoint must not contain %q", forbidden)
		}
	}
}

func TestDockerEntrypointAcceptsSecretFileInputs(t *testing.T) {
	root := repoRoot(t)
	entrypoint := readText(t, filepath.Join(root, "scripts", "docker-entrypoint.sh"))

	for _, required := range []string{
		"${HOUFENG_INITIAL_PASSWORD_FILE:-}",
		"${HOUFENG_SESSION_HMAC_KEY_FILE:-}",
		"HOUFENG_INITIAL_PASSWORD or HOUFENG_INITIAL_PASSWORD_FILE",
		"HOUFENG_SESSION_HMAC_KEY or HOUFENG_SESSION_HMAC_KEY_FILE",
	} {
		if !strings.Contains(entrypoint, required) {
			t.Fatalf("docker entrypoint must contain %q", required)
		}
	}
}

func TestComposeUsesNamedLogVolumeForNonRootContainer(t *testing.T) {
	root := repoRoot(t)
	compose := readText(t, filepath.Join(root, "compose.yaml"))

	if strings.Contains(compose, "./data/logs:/var/log/houfeng") {
		t.Fatal("compose.yaml must not bind-mount ./data/logs over the non-root image log directory")
	}
	if !strings.Contains(compose, "houfeng_logs:/var/log/houfeng") {
		t.Fatal("compose.yaml must mount the named houfeng_logs volume at /var/log/houfeng")
	}
	if !strings.Contains(compose, "\nvolumes:\n  houfeng_logs:\n") {
		t.Fatal("compose.yaml must declare the houfeng_logs named volume")
	}
}

func TestCIDockerImageJobBuildsImageInGitHubActions(t *testing.T) {
	root := repoRoot(t)
	workflow := readText(t, filepath.Join(root, ".github", "workflows", "ci.yml"))

	for _, required := range []string{
		"\n  docker-image:\n",
		"docker/setup-buildx-action@v4",
		"docker/build-push-action@v7",
		"context: .",
		"file: Dockerfile",
		"push: false",
		"VERSION=dev",
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("ci docker-image job must contain %q", required)
		}
	}
}

func TestPublishImagesWorkflowBuildsAndInspectsDockerImage(t *testing.T) {
	root := repoRoot(t)
	workflow := readText(t, filepath.Join(root, ".github", "workflows", "publish-images.yml"))

	for _, required := range []string{
		"\n  build:\n",
		"\n  publish:\n",
		"docker/build-push-action@v7",
		"file: Dockerfile",
		"outputs: type=image,name=${{ env.REGISTRY_IMAGE }},push-by-digest=true,name-canonical=true,push=true",
		"docker buildx imagetools create",
		"docker buildx imagetools inspect",
	} {
		if !strings.Contains(workflow, required) {
			t.Fatalf("publish-images workflow must contain %q", required)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
}

func readText(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}
