//go:build linux

package deploy

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestOrdinaryControllerExecutableRequiresTraversableRootPath(t *testing.T) {
	work := ordinaryBootstrapFixture(t)
	private := filepath.Join(work, "private")
	visible := filepath.Join(work, "visible")
	if err := os.Mkdir(private, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(visible, 0o755); err != nil {
		t.Fatal(err)
	}
	source, err := os.Open("/proc/self/exe")
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	privateBinary := filepath.Join(private, "admin")
	copyFile, err := os.OpenFile(privateBinary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(copyFile, source)
	closeErr := copyFile.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatalf("copy test executable: %v %v", copyErr, closeErr)
	}
	if err := os.Chmod(privateBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open(privateBinary, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if err := ordinaryValidateControllerExecutable(privateBinary, fd); !errors.Is(err, errOrdinaryControllerExecutable) {
		t.Fatalf("0700 ancestor was not refused: %v", err)
	}
	visibleBinary := filepath.Join(visible, "admin")
	if err := os.Rename(privateBinary, visibleBinary); err != nil {
		t.Fatal(err)
	}
	if err := ordinaryValidateControllerExecutable(visibleBinary, fd); err != nil {
		t.Fatalf("root-owned executable rejected: %v", err)
	}
	// When UID10002 is not in the owning group, other-execute is sufficient.
	if err := os.Chmod(visible, 0o701); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(visibleBinary, 0o701); err != nil {
		t.Fatal(err)
	}
	if err := ordinaryValidateControllerExecutable(visibleBinary, fd); err != nil {
		t.Fatalf("other-executable root path rejected: %v", err)
	}
	// Membership in the owning group selects group permissions, not other bits.
	if err := os.Chown(visible, 0, composeLaunchRuntimeGID); err != nil {
		t.Fatal(err)
	}
	if err := ordinaryValidateControllerExecutable(visibleBinary, fd); !errors.Is(err, errOrdinaryControllerExecutable) {
		t.Fatalf("non-executable owning group accepted: %v", err)
	}
}
