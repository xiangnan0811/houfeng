//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestComposeStoragePreparationRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{
		{"deploy-prepare-storage"},
		{"deploy-prepare-storage", "--scope", "other"},
		{"deploy-prepare-storage", "--scope", "compose", "extra"},
	} {
		if err := runAdminWithDeps(t.Context(), args, appAdminDependencies{}); err != errComposeStoragePreparation {
			t.Fatalf("storage args %q: %v", args, err)
		}
	}
}

func TestComposeStorageSafeAncestor(t *testing.T) {
	for _, tt := range []struct {
		name string
		stat unix.Stat_t
		want bool
	}{
		{"root", unix.Stat_t{Mode: unix.S_IFDIR | 0o755, Uid: 0, Nlink: 2}, true},
		{"manager", unix.Stat_t{Mode: unix.S_IFDIR | 0o750, Uid: 10002, Nlink: 2}, true},
		{"foreign owner", unix.Stat_t{Mode: unix.S_IFDIR | 0o755, Uid: 1000, Nlink: 2}, false},
		{"group writable", unix.Stat_t{Mode: unix.S_IFDIR | 0o775, Uid: 0, Nlink: 2}, false},
		{"world writable", unix.Stat_t{Mode: unix.S_IFDIR | 0o757, Uid: 0, Nlink: 2}, false},
		{"symlink", unix.Stat_t{Mode: unix.S_IFLNK | 0o755, Uid: 0, Nlink: 1}, false},
		{"unlinked", unix.Stat_t{Mode: unix.S_IFDIR | 0o755, Uid: 0}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := composeStorageSafeAncestor(tt.stat); got != tt.want {
				t.Fatalf("safe ancestor = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComposeStorageCreatedParentRefresh(t *testing.T) {
	base := os.Getenv("HOUFENG_COMPOSE_STORAGE_TEST_ROOT")
	if os.Getuid() != 0 || base == "" {
		t.Skip("requires root and HOUFENG_COMPOSE_STORAGE_TEST_ROOT with protected ancestors")
	}
	for _, name := range []string{"zero", "one", "two", "decrease", "replacement"} {
		t.Run(name, func(t *testing.T) {
			root, err := os.MkdirTemp(base, "compose-parent-refresh-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(root) })
			if err := os.Chmod(root, 0o755); err != nil {
				t.Fatal(err)
			}
			parent := filepath.Join(root, "parent")
			if err := os.Mkdir(parent, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(parent, 0o755); err != nil {
				t.Fatal(err)
			}
			if name == "decrease" {
				if err := os.Mkdir(filepath.Join(parent, "seed"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			rootFile, err := os.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			defer rootFile.Close()
			parentFile, err := os.Open(parent)
			if err != nil {
				t.Fatal(err)
			}
			defer parentFile.Close()
			dirs := []composeStorageDirectory{{fd: int(rootFile.Fd())}, {fd: int(parentFile.Fd()), name: "parent"}}
			for i := range dirs {
				if err := unix.Fstat(dirs[i].fd, &dirs[i].stat); err != nil {
					t.Fatal(err)
				}
			}
			before := dirs[1].stat.Nlink
			switch name {
			case "one", "two":
				if err := os.Mkdir(filepath.Join(parent, "first"), 0o700); err != nil {
					t.Fatal(err)
				}
				if name == "two" {
					if err := os.Mkdir(filepath.Join(parent, "second"), 0o700); err != nil {
						t.Fatal(err)
					}
				}
			case "decrease":
				if err := os.Remove(filepath.Join(parent, "seed")); err != nil {
					t.Fatal(err)
				}
			case "replacement":
				if err := os.Rename(parent, filepath.Join(root, "moved")); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(parent, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := unix.Fstat(dirs[0].fd, &dirs[0].stat); err != nil {
					t.Fatal(err)
				}
			}
			var current unix.Stat_t
			if err := unix.Fstat(dirs[1].fd, &current); err != nil {
				t.Fatal(err)
			}
			if (name == "one" && (current.Nlink <= before || current.Nlink-before != 1)) ||
				(name == "two" && (current.Nlink <= before || current.Nlink-before != 2)) ||
				(name == "decrease" && current.Nlink >= before) {
				t.Skip("fixture filesystem does not expose the requested directory link-count delta")
			}
			want := name == "zero" || name == "one"
			if got := composeStorageRefreshCreatedParent(dirs); got != want {
				t.Fatalf("refresh = %v, want %v", got, want)
			}
			if !want && dirs[1].stat.Nlink != before {
				t.Fatal("refused refresh changed parent snapshot")
			}
		})
	}
}

// The fixture must have protected ancestors; ordinary t.TempDir under /tmp is
// deliberately rejected by the production ancestor policy.
func TestComposeStorageGuardPreparation(t *testing.T) {
	base := os.Getenv("HOUFENG_COMPOSE_STORAGE_TEST_ROOT")
	if os.Getuid() != 0 || base == "" {
		t.Skip("requires root and HOUFENG_COMPOSE_STORAGE_TEST_ROOT with protected ancestors")
	}
	root, err := os.MkdirTemp(base, "compose-storage-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := prepareComposeStorageGuard(root); err != nil {
		t.Fatal(err)
	}
	ordinary := filepath.Join(root, "ordinary")
	st, err := os.Lstat(ordinary)
	if err != nil {
		t.Fatal(err)
	}
	var sys unix.Stat_t
	if err := unix.Lstat(ordinary, &sys); err != nil {
		t.Fatal(err)
	}
	if sys.Uid != 10002 || sys.Gid != 10001 || st.Mode().Perm() != 0o750 {
		t.Fatalf("directory metadata = uid %d gid %d mode %v", sys.Uid, sys.Gid, st.Mode())
	}
	if err := prepareComposeStorageGuard(root); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if err := os.Chmod(ordinary, 0o770); err != nil {
		t.Fatal(err)
	}
	if err := prepareComposeStorageGuard(root); err == nil {
		t.Fatal("accepted directory drift")
	}
	st, err = os.Stat(ordinary)
	if err != nil || st.Mode().Perm() != 0o770 {
		t.Fatalf("drift repaired: %v %v", st, err)
	}
	if err := os.Chmod(ordinary, 0o750); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(ordinary, "ordinary.lock")
	original, err := os.ReadFile(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := prepareComposeStorageGuard(root); err == nil {
		t.Fatal("accepted malformed anchor")
	}
	body, err := os.ReadFile(lock)
	if err != nil || string(body) != "invalid" {
		t.Fatalf("anchor changed: %q %v", body, err)
	}
	if err := os.WriteFile(lock, original, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := prepareComposeStorageGuard(root); err != nil {
		t.Fatalf("original anchor replay refused: %v", err)
	}
	if err := os.Rename(lock, lock+".original"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, []byte("0123456789abcdef0123456789abcdef"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(lock, 10002, 10001); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(lock, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := prepareComposeStorageGuard(root); err == nil {
		t.Fatal("accepted replacement anchor")
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(lock+".original", lock); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o775); err != nil {
		t.Fatal(err)
	}
	if err := prepareComposeStorageGuard(root); err == nil {
		t.Fatal("accepted unsafe mount root")
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := prepareComposeStorageGuard(root); err == nil {
		t.Fatal("accepted non-traversable mount root")
	}
	if err := os.Chmod(root, 0o755); err != nil {
		t.Fatal(err)
	}

	other, err := os.MkdirTemp(base, "compose-storage-symlink-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(other) })
	if err := os.Chmod(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(ordinary, filepath.Join(other, "ordinary")); err != nil {
		t.Fatal(err)
	}
	if err := prepareComposeStorageGuard(other); err == nil {
		t.Fatal("accepted symlinked ordinary directory")
	}
}

func TestComposeStorageDockerPrecreatedGuard(t *testing.T) {
	base := os.Getenv("HOUFENG_COMPOSE_STORAGE_TEST_ROOT")
	if os.Getuid() != 0 || base == "" {
		t.Skip("requires root and HOUFENG_COMPOSE_STORAGE_TEST_ROOT with protected ancestors")
	}
	for _, tt := range []struct {
		name        string
		mode        os.FileMode
		entry       string
		wantSuccess bool
	}{
		{name: "pristine", mode: 0o755, wantSuccess: true},
		{name: "hidden entry", mode: 0o755, entry: ".intent"},
		{name: "anchor", mode: 0o755, entry: "ordinary.lock"},
		{name: "other metadata", mode: 0o750},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root, err := os.MkdirTemp(base, "compose-storage-docker-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(root) })
			if err := os.Chmod(root, 0o755); err != nil {
				t.Fatal(err)
			}
			ordinary := filepath.Join(root, "ordinary")
			if err := os.Mkdir(ordinary, tt.mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(ordinary, tt.mode); err != nil {
				t.Fatal(err)
			}
			if tt.entry != "" {
				if err := os.WriteFile(filepath.Join(ordinary, tt.entry), []byte("unchanged"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var before, after unix.Stat_t
			if err := unix.Lstat(ordinary, &before); err != nil {
				t.Fatal(err)
			}
			err = prepareComposeStorageGuard(root)
			if (err == nil) != tt.wantSuccess {
				t.Fatalf("preparation error = %v", err)
			}
			if err := unix.Lstat(ordinary, &after); err != nil {
				t.Fatal(err)
			}
			if before.Dev != after.Dev || before.Ino != after.Ino {
				t.Fatal("ordinary directory was replaced")
			}
			if tt.wantSuccess {
				if after.Uid != 10002 || after.Gid != 10001 || after.Mode&0o7777 != 0o750 {
					t.Fatalf("initialized metadata uid=%d gid=%d mode=%#o", after.Uid, after.Gid, after.Mode&0o7777)
				}
			} else if after.Uid != before.Uid || after.Gid != before.Gid || after.Mode != before.Mode {
				t.Fatal("refused directory metadata was changed")
			}
			if tt.entry != "" {
				body, err := os.ReadFile(filepath.Join(ordinary, tt.entry))
				if err != nil || string(body) != "unchanged" {
					t.Fatalf("entry changed: %q %v", body, err)
				}
			}
		})
	}
}
