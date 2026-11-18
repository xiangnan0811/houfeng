//go:build linux

package deploy

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestOrdinaryPrivateDirectoriesUseRealAuthorityAndBackendModes(t *testing.T) {
	work := ordinaryBootstrapFixture(t)
	makeDir := func(path string, uid, gid int, mode os.FileMode) {
		t.Helper()
		if err := os.Mkdir(path, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(path, uid, gid); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	data := filepath.Join(work, "data")
	makeDir(data, 10002, 10002, 0o750)
	root := filepath.Join(data, "records-authority")
	makeDir(root, 10002, 10002, 0o755)
	private := filepath.Join(root, "private")
	makeDir(private, 10002, 10002, 0o700)
	file := filepath.Join(private, "activation-receipt.json")
	if err := os.WriteFile(file, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(file, 10002, 10002); err != nil {
		t.Fatal(err)
	}
	body, err := ordinaryReadAuthorityComponent(root, "activation-receipt.json", 1024)
	if err != nil || string(body) != "secret\n" {
		t.Fatalf("authority read: %q %v", body, err)
	}
	if err := os.Chmod(private, 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := ordinaryReadAuthorityComponent(root, "activation-receipt.json", 1024); err == nil {
		t.Fatal("accepted group-accessible private authority")
	}
	if err := os.Chmod(private, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(private, 0, 10002); err != nil {
		t.Fatal(err)
	}
	if _, err := ordinaryReadAuthorityComponent(root, "activation-receipt.json", 1024); err == nil {
		t.Fatal("accepted wrong authority owner")
	}

	operation := filepath.Join(work, "operation")
	makeDir(operation, 0, 10001, 0o750)
	backend := filepath.Join(operation, "backends")
	makeDir(backend, 10002, 10001, 0o700)
	dirs, err := ordinaryOpenPrivateDirectory(backend, 10001)
	if err != nil {
		t.Fatalf("backend walker: %v", err)
	}
	if !ordinaryPrivateDirectoryUnchanged(dirs) {
		t.Fatal("valid backend directory changed")
	}
	if err := os.Rename(backend, backend+"-old"); err != nil {
		t.Fatal(err)
	}
	makeDir(backend, 10002, 10001, 0o700)
	if ordinaryPrivateDirectoryUnchanged(dirs) {
		t.Fatal("accepted replaced backend directory")
	}
	for _, dir := range dirs {
		_ = unix.Close(dir.fd)
	}
	if records, err := ordinaryReadBackendRecords(backend, "coldoperation"); err != nil || len(records) != 0 {
		t.Fatalf("empty backend records: %v %v", records, err)
	}
	if err := os.Chown(backend, 10002, 10002); err != nil {
		t.Fatal(err)
	}
	if _, err := ordinaryReadBackendRecords(backend, "coldoperation"); err == nil {
		t.Fatal("accepted wrong backend group")
	}
}
