//go:build linux

package deploy

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

var errOrdinaryControllerExecutable = errors.New("ordinary controller executable must be root-owned, not group/other-writable, and executable by UID 10002/GID 10001 through root-owned traversable ancestors")

func ordinaryControllerIdentityCanExecute(stat unix.Stat_t) bool {
	// All checked owners are root and the private launcher has no supplementary groups.
	if stat.Gid == composeLaunchRuntimeGID {
		return stat.Mode&0o010 != 0
	}
	return stat.Mode&0o001 != 0
}

// Pin the running executable to its root-controlled named path and reject
// common mode-bit access mistakes. This is not a named-path ACL evaluation;
// the real two-exec probe checks access through /proc/self/exe before ACTIVE.
func ordinaryValidateControllerExecutable(path string, runningFD int) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" || strings.ContainsRune(path, '\x00') || runningFD < 0 {
		return errOrdinaryControllerExecutable
	}
	parentPath := filepath.Dir(path)
	var dirs []composeLaunchDirectory
	var err error
	if parentPath == "/" {
		fd, openErr := unix.Openat2(unix.AT_FDCWD, "/", &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: unix.RESOLVE_NO_SYMLINKS})
		if openErr != nil {
			return errOrdinaryControllerExecutable
		}
		dirs = []composeLaunchDirectory{{fd: fd}}
		if unix.Fstat(fd, &dirs[0].before) != nil {
			_ = unix.Close(fd)
			return errOrdinaryControllerExecutable
		}
	} else {
		dirs, err = openComposeLaunchDirectories(parentPath)
		if err != nil {
			return errOrdinaryControllerExecutable
		}
	}
	defer func() {
		for _, dir := range dirs {
			_ = unix.Close(dir.fd)
		}
	}()
	for _, dir := range dirs {
		if dir.before.Uid != 0 || dir.before.Mode&0o022 != 0 || !ordinaryControllerIdentityCanExecute(dir.before) {
			return errOrdinaryControllerExecutable
		}
	}
	parent := dirs[len(dirs)-1].fd
	fd, err := unix.Openat2(parent, filepath.Base(path), &unix.OpenHow{Flags: unix.O_PATH | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: maintenanceGuardResolve})
	if err != nil {
		return errOrdinaryControllerExecutable
	}
	defer unix.Close(fd)
	var running, selected, named, after unix.Stat_t
	if unix.Fstat(runningFD, &running) != nil || unix.Fstat(fd, &selected) != nil || unix.Fstatat(parent, filepath.Base(path), &named, unix.AT_SYMLINK_NOFOLLOW) != nil || selected.Mode&unix.S_IFMT != unix.S_IFREG || selected.Uid != 0 || selected.Mode&0o6022 != 0 || selected.Nlink == 0 || !ordinaryControllerIdentityCanExecute(selected) || !sameSourceFile(running, selected) || !sameSourceFile(selected, named) || unix.Fstat(runningFD, &after) != nil || !sameSourceFile(running, after) || !composeLaunchDirectoriesUnchanged(dirs) {
		return errOrdinaryControllerExecutable
	}
	return nil
}

// The later target-image probe exercises the real two-exec route before ACTIVE.
func ordinaryControllerExecutablePreflight() error {
	if os.Getuid() != 0 || os.Geteuid() != 0 {
		return errOrdinaryControllerExecutable
	}
	path, err := os.Readlink("/proc/self/exe")
	if err != nil {
		return errOrdinaryControllerExecutable
	}
	fd, err := unix.Open("/proc/self/exe", unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return errOrdinaryControllerExecutable
	}
	defer unix.Close(fd)
	if ordinaryValidateControllerExecutable(path, fd) != nil {
		return errOrdinaryControllerExecutable
	}
	return nil
}
