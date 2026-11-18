//go:build linux

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

	"houfeng/internal/center/deploy"
)

var errComposeStoragePreparation = errors.New("Compose storage guard preparation failed")

type composeStorageDirectory struct {
	fd   int
	name string
	stat unix.Stat_t
}

func composeStorageSafeAncestor(st unix.Stat_t) bool {
	return st.Mode&unix.S_IFMT == unix.S_IFDIR && st.Nlink != 0 && st.Mode&0o011 != 0 &&
		(st.Uid == 0 || st.Uid == 10002) && st.Mode&0o022 == 0
}

func composeStorageSameDirectory(a, b unix.Stat_t) bool {
	return composeStorageSafeAncestor(b) && a.Dev == b.Dev && a.Ino == b.Ino &&
		a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink
}

func composeStorageDirectoriesUnchanged(dirs []composeStorageDirectory) bool {
	for i := range dirs {
		var current unix.Stat_t
		if unix.Fstat(dirs[i].fd, &current) != nil || !composeStorageSameDirectory(dirs[i].stat, current) {
			return false
		}
		if i > 0 && (unix.Fstatat(dirs[i-1].fd, dirs[i].name, &current, unix.AT_SYMLINK_NOFOLLOW) != nil || !composeStorageSameDirectory(dirs[i].stat, current)) {
			return false
		}
	}
	return true
}

// Creating one subdirectory may increase its parent's link count by one.
// All other metadata and every held and named ancestor must remain unchanged.
func composeStorageRefreshCreatedParent(dirs []composeStorageDirectory) bool {
	parent := &dirs[len(dirs)-1]
	var current unix.Stat_t
	if unix.Fstat(parent.fd, &current) != nil || current.Nlink < parent.stat.Nlink || current.Nlink-parent.stat.Nlink > 1 {
		return false
	}
	previous := parent.stat.Nlink
	parent.stat.Nlink = current.Nlink
	if !composeStorageDirectoriesUnchanged(dirs) {
		parent.stat.Nlink = previous
		return false
	}
	return true
}

func composeStorageOrdinaryUnchanged(parent, fd int, before unix.Stat_t) bool {
	var current, named unix.Stat_t
	if unix.Fstat(fd, &current) != nil || unix.Fstatat(parent, "ordinary", &named, unix.AT_SYMLINK_NOFOLLOW) != nil {
		return false
	}
	matches := func(st *unix.Stat_t) bool {
		return before.Dev == st.Dev && before.Ino == st.Ino && before.Mode == st.Mode && before.Uid == st.Uid && before.Gid == st.Gid && before.Nlink == st.Nlink
	}
	return matches(&current) && matches(&named)
}

func composeStorageDirectoryEmpty(fd int) bool {
	var buf [4096]byte
	for {
		n, err := unix.ReadDirent(fd, buf[:])
		if err != nil {
			return false
		}
		if n == 0 {
			return true
		}
		_, _, names := unix.ParseDirent(buf[:n], 1, nil)
		if len(names) != 0 {
			return false
		}
	}
}

func runComposeStoragePreparation(args []string) error {
	if len(args) != 3 || args[0] != "deploy-prepare-storage" || args[1] != "--scope" || args[2] != "compose" {
		return errComposeStoragePreparation
	}
	if os.Getuid() != 0 || os.Geteuid() != 0 {
		return errComposeStoragePreparation
	}
	return prepareComposeStorageGuard("/var/lib/houfeng-maintenance")
}

// The mount root is an operator-owned precondition, never repaired here.
// An absent ordinary directory or Docker's pristine empty root-owned bind
// source can be initialized; all other existing metadata must already match.
func prepareComposeStorageGuard(root string) error {
	if root == "" || root[0] != '/' || strings.Contains(root, "//") || strings.HasSuffix(root, "/") {
		return errComposeStoragePreparation
	}
	parts := strings.Split(strings.TrimPrefix(root, "/"), "/")
	dirs := make([]composeStorageDirectory, 0, len(parts)+2)
	defer func() {
		for _, dir := range dirs {
			_ = unix.Close(dir.fd)
		}
	}()
	rootFD, err := unix.Openat2(unix.AT_FDCWD, "/", &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return errComposeStoragePreparation
	}
	dirs = append(dirs, composeStorageDirectory{fd: rootFD})
	if unix.Fstat(rootFD, &dirs[0].stat) != nil || !composeStorageSafeAncestor(dirs[0].stat) {
		return errComposeStoragePreparation
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return errComposeStoragePreparation
		}
		fd, err := unix.Openat2(dirs[len(dirs)-1].fd, part, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS})
		if err != nil {
			return errComposeStoragePreparation
		}
		dirs = append(dirs, composeStorageDirectory{fd: fd, name: part})
		if unix.Fstat(fd, &dirs[len(dirs)-1].stat) != nil || !composeStorageSafeAncestor(dirs[len(dirs)-1].stat) {
			return errComposeStoragePreparation
		}
	}
	parent := dirs[len(dirs)-1].fd
	if st := dirs[len(dirs)-1].stat; st.Uid != 0 || st.Mode&0o022 != 0 || (st.Mode&0o001 == 0 && (st.Gid != 10001 || st.Mode&0o010 == 0)) {
		return errComposeStoragePreparation
	}
	var existingMarker, existingOrdinary unix.Stat_t
	markerErr := unix.Fstatat(parent, deploy.OrdinaryComposeInitializationMarker, &existingMarker, unix.AT_SYMLINK_NOFOLLOW)
	ordinaryErr := unix.Fstatat(parent, "ordinary", &existingOrdinary, unix.AT_SYMLINK_NOFOLLOW)
	if markerErr == nil && errors.Is(ordinaryErr, unix.ENOENT) {
		return errComposeStoragePreparation
	}
	if markerErr != nil && !errors.Is(markerErr, unix.ENOENT) {
		return errComposeStoragePreparation
	}
	created := false
	if err := unix.Mkdirat(parent, "ordinary", 0o700); err == nil {
		created = true
		if !composeStorageRefreshCreatedParent(dirs) {
			return errComposeStoragePreparation
		}
	} else if !errors.Is(err, unix.EEXIST) {
		return errComposeStoragePreparation
	}
	fd, err := unix.Openat2(parent, "ordinary", &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return errComposeStoragePreparation
	}
	defer unix.Close(fd)
	var ordinary unix.Stat_t
	if unix.Fstat(fd, &ordinary) != nil || ordinary.Mode&unix.S_IFMT != unix.S_IFDIR || ordinary.Nlink == 0 {
		return errComposeStoragePreparation
	}
	pristine := ordinary.Uid == 0 && ordinary.Gid == 0 && ordinary.Mode&0o7777 == 0o755
	if created || pristine {
		if (created && (ordinary.Uid != 0 || ordinary.Mode&0o7777 != 0o700)) ||
			!composeStorageDirectoriesUnchanged(dirs) || !composeStorageOrdinaryUnchanged(parent, fd, ordinary) ||
			!composeStorageDirectoryEmpty(fd) || !composeStorageDirectoriesUnchanged(dirs) || !composeStorageOrdinaryUnchanged(parent, fd, ordinary) {
			return errComposeStoragePreparation
		}
		before := ordinary
		if unix.Fchown(fd, 10002, 10001) != nil || unix.Fchmod(fd, 0o750) != nil || unix.Fsync(fd) != nil || unix.Fsync(parent) != nil ||
			unix.Fstat(fd, &ordinary) != nil || before.Dev != ordinary.Dev || before.Ino != ordinary.Ino {
			return errComposeStoragePreparation
		}
	}
	if !composeStorageSafeAncestor(ordinary) || ordinary.Uid != 10002 || ordinary.Gid != 10001 || ordinary.Mode&0o7777 != 0o750 ||
		!composeStorageDirectoriesUnchanged(dirs) || !composeStorageOrdinaryUnchanged(parent, fd, ordinary) {
		return errComposeStoragePreparation
	}
	if err := deploy.ProvisionOrdinaryComposeGuard(filepath.Join(root, "ordinary")); err != nil {
		return errComposeStoragePreparation
	}
	lockFD, err := unix.Openat2(fd, "ordinary.lock", &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS})
	if err == nil {
		defer unix.Close(lockFD)
		var st unix.Stat_t
		var nonce [32]byte
		n, readErr := unix.Pread(lockFD, nonce[:], 0)
		if unix.Fstat(lockFD, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0o7777 != 0o640 || st.Uid != 10002 || st.Gid != 10001 || st.Nlink != 1 || st.Size != 32 || readErr != nil || n != len(nonce) || nonce == [32]byte{} {
			return errComposeStoragePreparation
		}
		var named unix.Stat_t
		if unix.Fstatat(fd, "ordinary.lock", &named, unix.AT_SYMLINK_NOFOLLOW) != nil || named.Dev != st.Dev || named.Ino != st.Ino || named.Mode != st.Mode || named.Uid != st.Uid || named.Gid != st.Gid || named.Nlink != st.Nlink || named.Size != st.Size {
			return errComposeStoragePreparation
		}
	} else {
		return errComposeStoragePreparation
	}
	if !composeStorageDirectoriesUnchanged(dirs) || !composeStorageOrdinaryUnchanged(parent, fd, ordinary) {
		return errComposeStoragePreparation
	}
	return nil
}
