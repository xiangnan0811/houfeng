//go:build linux

package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"houfeng/internal/center/recordbirth"
)

// Run with the same protected UID/GID fixture as TestComposeBirthLaunchProtectedPublication.
func TestOrdinaryLaunchReplacement(t *testing.T) {
	root := os.Getenv("HOUFENG_BIRTH_LAUNCH_FIXTURE_ROOT")
	if root == "" {
		t.Skip("requires isolated protected fixture root and UID10002 with GID10001")
	}
	if os.Getuid() != composeLaunchPublisherUID || os.Geteuid() != composeLaunchPublisherUID {
		t.Fatal("launch fixture must run as UID10002")
	}
	center := recordbirth.LaunchBinding{ProtocolVersion: 1, ProcessKind: "center", DeploymentID: "dp-" + strings.Repeat("a", 64), ActivationBundleDigest: [32]byte{1}, BinaryDigest: [32]byte{2}}
	processor := center
	processor.ProcessKind = "content-processor"
	processor.BinaryDigest = [32]byte{3}
	old, err := encodeComposeBirthLaunchPair(center, processor)
	if err != nil {
		t.Fatal(err)
	}
	center.BinaryDigest = [32]byte{4}
	processor.BinaryDigest = [32]byte{5}
	next, err := encodeComposeBirthLaunchPair(center, processor)
	if err != nil {
		t.Fatal(err)
	}
	fixture := func(t *testing.T) string {
		t.Helper()
		dir, err := os.MkdirTemp(root, "ordinary-launch-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := publishComposeBirthLaunchPair(dir, old); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	check := func(t *testing.T, dir, name, kind string, want [32]byte) {
		t.Helper()
		_, got, err := recordbirth.ReadPublishedLaunchBinding(filepath.Join(dir, name), kind)
		if err != nil || got != want {
			t.Fatalf("binding %s: digest %x, error %v", name, got, err)
		}
	}
	t.Run("partial publication resumes and replay retains inodes", func(t *testing.T) {
		dir := fixture(t)
		dirs, err := openComposeLaunchDirectories(dir)
		if err != nil {
			t.Fatal(err)
		}
		err = replaceOrdinaryLaunchFile(dirs, dir, composeCenterBirthLaunchFile, "center", old.center, next.center, next.centerBytes, old.centerDigest, next.centerDigest)
		for _, d := range dirs {
			_ = unix.Close(d.fd)
		}
		if err != nil {
			t.Fatal(err)
		}
		check(t, dir, composeProcessorBirthLaunchFile, "content-processor", old.processorDigest)
		if err := replaceOrdinaryComposeBirthLaunchPair(dir, old, next); err != nil {
			t.Fatal(err)
		}
		check(t, dir, composeCenterBirthLaunchFile, "center", next.centerDigest)
		check(t, dir, composeProcessorBirthLaunchFile, "content-processor", next.processorDigest)
		before, err := os.Stat(filepath.Join(dir, composeCenterBirthLaunchFile))
		if err != nil {
			t.Fatal(err)
		}
		if err := replaceOrdinaryComposeBirthLaunchPair(dir, old, next); err != nil {
			t.Fatal(err)
		}
		after, err := os.Stat(filepath.Join(dir, composeCenterBirthLaunchFile))
		if err != nil || !os.SameFile(before, after) {
			t.Fatal("exact replay changed published inode")
		}
	})
	t.Run("unknown second member refuses before first replacement", func(t *testing.T) {
		dir := fixture(t)
		if err := os.Link(filepath.Join(dir, composeProcessorBirthLaunchFile), filepath.Join(dir, "extra-link")); err != nil {
			t.Fatal(err)
		}
		if err := replaceOrdinaryComposeBirthLaunchPair(dir, old, next); err == nil {
			t.Fatal("unsafe second member accepted")
		}
		check(t, dir, composeCenterBirthLaunchFile, "center", old.centerDigest)
	})
	t.Run("superseded target refused", func(t *testing.T) {
		dir := fixture(t)
		if err := replaceOrdinaryComposeBirthLaunchPair(dir, old, next); err != nil {
			t.Fatal(err)
		}
		center.BinaryDigest = [32]byte{6}
		other, err := encodeComposeBirthLaunchPair(center, processor)
		if err != nil {
			t.Fatal(err)
		}
		if err := replaceOrdinaryComposeBirthLaunchPair(dir, old, other); err == nil {
			t.Fatal("superseded target accepted")
		}
		check(t, dir, composeCenterBirthLaunchFile, "center", next.centerDigest)
	})
}
