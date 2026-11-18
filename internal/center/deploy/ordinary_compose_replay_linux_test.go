//go:build linux

package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestOrdinaryComposeOverrideExactReplayAndForeignRefusal(t *testing.T) {
	work := ordinaryBootstrapFixture(t)
	op := filepath.Join(work, "operation")
	if err := os.Mkdir(op, 0o700); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(op, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	request := ordinaryUpgradeRequest{OperationID: "override-replay", WorkingDirectory: work, TargetImageID: "sha256:" + strings.Repeat("a", 64)}
	body, err := ordinaryComposeOverride(request)
	if err != nil {
		t.Fatal(err)
	}
	digest := ordinaryDigestHex(body)
	temp := filepath.Join(op, ".ordinary-launch-"+digest[:32])
	if err := os.WriteFile(temp, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ordinaryStageComposeOverride(fd, request, digest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(temp); !os.IsNotExist(err) {
		t.Fatal("owned interrupted temp remained")
	}
	if err := ordinaryStageComposeOverride(fd, request, digest); err != nil {
		t.Fatal("exact override replay refused")
	}
	path := filepath.Join(op, "compose-override.yaml")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("foreign"), 0o440); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o440); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(temp, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ordinaryStageComposeOverride(fd, request, digest); err == nil {
		t.Fatal("foreign override accepted")
	}
	if _, err := os.Lstat(temp); err != nil {
		t.Fatal("foreign override refusal changed temporary state")
	}
}
