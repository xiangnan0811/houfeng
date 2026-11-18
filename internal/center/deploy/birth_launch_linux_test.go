//go:build linux

package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"houfeng/internal/center/recordbirth"
)

// The fixture root must be an isolated protected directory traversable by the
// runtime UID. Run this test as UID10002 with supplementary GID10001.
func TestComposeBirthLaunchProtectedPublication(t *testing.T) {
	root := os.Getenv("HOUFENG_BIRTH_LAUNCH_FIXTURE_ROOT")
	if root == "" {
		t.Skip("requires isolated protected fixture root and UID10002 with GID10001")
	}
	if os.Getuid() != composeLaunchPublisherUID || os.Geteuid() != composeLaunchPublisherUID {
		t.Fatal("launch fixture must run as UID10002")
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		t.Fatal("fixture root must be an absolute clean path")
	}
	newDirectory := func(t *testing.T) string {
		t.Helper()
		directory, err := os.MkdirTemp(root, "birth-launch-")
		if err != nil {
			t.Fatalf("create isolated fixture: %v", err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(directory) })
		if err := os.Chmod(directory, 0o755); err != nil {
			t.Fatalf("secure isolated fixture: %v", err)
		}
		return directory
	}
	binding := recordbirth.LaunchBinding{
		ProtocolVersion: 1, ProcessKind: "center", DeploymentID: "dp-" + strings.Repeat("a", 64),
		ActivationBundleDigest: [32]byte{1}, BinaryDigest: [32]byte{2},
	}
	processor := binding
	processor.ProcessKind = "content-processor"
	processor.BinaryDigest = [32]byte{3}
	pair, err := encodeComposeBirthLaunchPair(binding, processor)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("publish and exact replay", func(t *testing.T) {
		directory := newDirectory(t)
		if err := publishComposeBirthLaunchPair(directory, pair); err != nil {
			t.Fatalf("publish: %v", err)
		}
		path := filepath.Join(directory, composeCenterBirthLaunchFile)
		before, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, digest, err := recordbirth.ReadPublishedLaunchBinding(path, "center"); err != nil || digest != pair.centerDigest {
			t.Fatalf("strict center read: %v", err)
		}
		if _, digest, err := recordbirth.ReadPublishedLaunchBinding(filepath.Join(directory, composeProcessorBirthLaunchFile), "content-processor"); err != nil || digest != pair.processorDigest {
			t.Fatalf("strict processor read: %v", err)
		}
		if err := publishComposeBirthLaunchPair(directory, pair); err != nil {
			t.Fatalf("exact replay: %v", err)
		}
		after, err := os.Stat(path)
		if err != nil || !os.SameFile(before, after) {
			t.Fatal("exact replay replaced the published inode")
		}
		changed := binding
		changed.BinaryDigest = [32]byte{4}
		different, err := encodeComposeBirthLaunchPair(changed, processor)
		if err != nil {
			t.Fatal(err)
		}
		if err := publishComposeBirthLaunchPair(directory, different); err == nil {
			t.Fatal("publication replaced a different existing binding")
		}
		if _, digest, err := recordbirth.ReadPublishedLaunchBinding(path, "center"); err != nil || digest != pair.centerDigest {
			t.Fatal("rejected replacement changed the original binding")
		}
	})

	t.Run("symlink target is rejected", func(t *testing.T) {
		directory := newDirectory(t)
		path := filepath.Join(directory, composeCenterBirthLaunchFile)
		if err := os.Symlink("unrelated", path); err != nil {
			t.Fatal(err)
		}
		if err := publishComposeBirthLaunchPair(directory, pair); err == nil {
			t.Fatal("publication accepted a symlink target")
		}
		if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatal("rejected publication changed the target")
		}
	})

	t.Run("unsafe file and hard link are rejected", func(t *testing.T) {
		directory := newDirectory(t)
		if err := publishComposeBirthLaunchPair(directory, pair); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, composeCenterBirthLaunchFile)
		if err := os.Chmod(path, 0o640); err != nil {
			t.Fatal(err)
		}
		if err := publishComposeBirthLaunchPair(directory, pair); err == nil {
			t.Fatal("publication accepted unsafe file permissions")
		}
		if err := os.Chmod(path, 0o440); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(path, filepath.Join(directory, "extra-link")); err != nil {
			t.Fatal(err)
		}
		if err := publishComposeBirthLaunchPair(directory, pair); err == nil {
			t.Fatal("publication accepted a multiply linked file")
		}
	})

	t.Run("writable ancestor is rejected", func(t *testing.T) {
		directory := newDirectory(t)
		if err := os.Chmod(directory, 0o775); err != nil {
			t.Fatal(err)
		}
		if err := publishComposeBirthLaunchPair(directory, pair); err == nil {
			t.Fatal("publication accepted a group-writable directory")
		}
		if _, err := os.Lstat(filepath.Join(directory, composeCenterBirthLaunchFile)); !os.IsNotExist(err) {
			t.Fatal("unsafe directory received a launch file")
		}
	})

	t.Run("symlinked ancestor is rejected", func(t *testing.T) {
		directory := newDirectory(t)
		actual := filepath.Join(directory, "actual")
		if err := os.Mkdir(actual, 0o755); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(directory, "link")
		if err := os.Symlink(actual, link); err != nil {
			t.Fatal(err)
		}
		if err := publishComposeBirthLaunchPair(link, pair); err == nil {
			t.Fatal("publication followed a symlinked ancestor")
		}
	})
}
