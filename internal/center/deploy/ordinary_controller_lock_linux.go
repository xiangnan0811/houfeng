//go:build linux

package deploy

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const ordinaryControllerLockName = ".ordinary-controller.lock"

// The permanent controller-only lock is rooted in the operator-controlled
// working directory, before maintenance bootstrap, and never passed to a child.
func ordinaryWithControllerLock(ctx context.Context, work string, fn func() error) error {
	if ctx == nil || fn == nil || os.Getuid() != 0 || os.Geteuid() != 0 || !filepath.IsAbs(work) || filepath.Clean(work) != work {
		return errOrdinaryUpgrade
	}
	dirs, err := ordinaryImageDirectories(work)
	if err != nil {
		return errOrdinaryUpgrade
	}
	defer func() {
		for _, dir := range dirs {
			_ = unix.Close(dir.fd)
		}
	}()
	for _, dir := range dirs {
		if dir.before.Uid != 0 || dir.before.Mode&0o022 != 0 {
			return errOrdinaryUpgrade
		}
	}
	parent := dirs[len(dirs)-1].fd
	fd, err := ordinaryOpenControllerLock(parent)
	if err != nil {
		return errOrdinaryUpgrade
	}
	defer unix.Close(fd)
	if err := lockMaintenanceGuard(ctx, fd, true); err != nil {
		return err
	}
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Mode&0o7777 != 0o600 || stat.Uid != 0 || stat.Gid != 0 || stat.Nlink != 1 || stat.Size != 32 {
		return errOrdinaryUpgrade
	}
	if unix.Fsync(fd) != nil || unix.Fsync(parent) != nil {
		return errOrdinaryUpgrade
	}
	var observed [32]byte
	if n, err := unix.Pread(fd, observed[:], 0); err != nil || n != len(observed) || observed == [32]byte{} {
		return errOrdinaryUpgrade
	}
	matches := func() bool {
		var opened, named unix.Stat_t
		var current [32]byte
		n, err := unix.Pread(fd, current[:], 0)
		return err == nil && n == len(current) && current == observed && unix.Fstat(fd, &opened) == nil && unix.Fstatat(parent, ordinaryControllerLockName, &named, unix.AT_SYMLINK_NOFOLLOW) == nil && opened.Dev == stat.Dev && opened.Ino == stat.Ino && named.Dev == stat.Dev && named.Ino == stat.Ino && opened.Mode&0o7777 == 0o600 && opened.Uid == 0 && opened.Gid == 0 && opened.Nlink == 1 && opened.Size == 32 && named.Mode == opened.Mode && named.Uid == opened.Uid && named.Gid == opened.Gid && named.Nlink == 1 && composeLaunchDirectoriesUnchanged(dirs)
	}
	if !matches() {
		return errOrdinaryUpgrade
	}
	result := fn()
	if !ordinaryImageRefreshDirectory(dirs) {
		return errOrdinaryUpgrade
	}
	if !matches() {
		return errOrdinaryUpgrade
	}
	return result
}

func ordinaryOpenControllerLock(parent int) (int, error) {
	flags := uint64(unix.O_RDWR | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK)
	open := func() (int, error) {
		return unix.Openat2(parent, ordinaryControllerLockName, &unix.OpenHow{Flags: flags, Resolve: maintenanceGuardResolve})
	}
	fd, err := open()
	if !errors.Is(err, unix.ENOENT) {
		return fd, err
	}
	name, err := ordinaryLaunchTempName()
	if err != nil {
		return -1, errOrdinaryUpgrade
	}
	temp, err := unix.Openat2(parent, name, &unix.OpenHow{Flags: flags | unix.O_CREAT | unix.O_EXCL, Mode: 0o600, Resolve: maintenanceGuardResolve})
	if err != nil {
		return -1, errOrdinaryUpgrade
	}
	defer func() { _ = unix.Close(temp); _ = unix.Unlinkat(parent, name, 0) }()
	if unix.Fchown(temp, 0, 0) != nil || unix.Fchmod(temp, 0o600) != nil {
		return -1, errOrdinaryUpgrade
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return -1, errOrdinaryUpgrade
	}
	for written := 0; written < len(nonce); {
		n, err := unix.Pwrite(temp, nonce[written:], int64(written))
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || n == 0 {
			return -1, errOrdinaryUpgrade
		}
		written += n
	}
	if unix.Fsync(temp) != nil {
		return -1, errOrdinaryUpgrade
	}
	if err := unix.Renameat2(parent, name, parent, ordinaryControllerLockName, unix.RENAME_NOREPLACE); err != nil && !errors.Is(err, unix.EEXIST) {
		return -1, errOrdinaryUpgrade
	}
	if unix.Fsync(parent) != nil {
		return -1, errOrdinaryUpgrade
	}
	return open()
}
