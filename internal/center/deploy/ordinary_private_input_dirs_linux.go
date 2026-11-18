//go:build linux

package deploy

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// The root controller pins every private-path ancestor. Root-owned directories
// may deliberately be mode 0700; group or other write access is never allowed.
func ordinaryRootDirectorySafe(st unix.Stat_t) bool {
	return st.Mode&unix.S_IFMT == unix.S_IFDIR && st.Nlink != 0 && st.Uid == 0 && st.Mode&0o100 != 0 && st.Mode&0o022 == 0
}

func ordinarySameRootDirectory(a, b unix.Stat_t) bool {
	return ordinaryRootDirectorySafe(a) && ordinaryRootDirectorySafe(b) && a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink
}

func ordinaryOpenRootDirectories(path string) ([]composeLaunchDirectory, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsRune(path, '\x00') {
		return nil, errOrdinaryUpgrade
	}
	parts := []string{}
	if path != "/" {
		parts = strings.Split(strings.TrimPrefix(path, "/"), "/")
	}
	dirs := make([]composeLaunchDirectory, 0, len(parts)+1)
	closeDirs := func() {
		for _, dir := range dirs {
			_ = unix.Close(dir.fd)
		}
	}
	flags := uint64(unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW)
	if len(parts) == 0 {
		flags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW
	}
	root, err := unix.Openat2(unix.AT_FDCWD, "/", &unix.OpenHow{Flags: flags, Resolve: unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return nil, errOrdinaryUpgrade
	}
	dirs = append(dirs, composeLaunchDirectory{fd: root})
	if unix.Fstat(root, &dirs[0].before) != nil || !ordinaryRootDirectorySafe(dirs[0].before) {
		closeDirs()
		return nil, errOrdinaryUpgrade
	}
	for index, part := range parts {
		flags := uint64(unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW)
		if index == len(parts)-1 {
			flags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW
		}
		fd, err := unix.Openat2(dirs[len(dirs)-1].fd, part, &unix.OpenHow{Flags: flags, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS})
		if err != nil {
			closeDirs()
			return nil, errOrdinaryUpgrade
		}
		dirs = append(dirs, composeLaunchDirectory{fd: fd, name: part})
		if unix.Fstat(fd, &dirs[len(dirs)-1].before) != nil || !ordinaryRootDirectorySafe(dirs[len(dirs)-1].before) {
			closeDirs()
			return nil, errOrdinaryUpgrade
		}
	}
	return dirs, nil
}

func ordinaryRootDirectoriesUnchanged(dirs []composeLaunchDirectory) bool {
	if len(dirs) == 0 {
		return false
	}
	root, err := unix.Openat2(unix.AT_FDCWD, "/", &unix.OpenHow{Flags: unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return false
	}
	defer unix.Close(root)
	var current unix.Stat_t
	if unix.Fstat(root, &current) != nil || !ordinarySameRootDirectory(dirs[0].before, current) {
		return false
	}
	for i, dir := range dirs {
		if unix.Fstat(dir.fd, &current) != nil || !ordinarySameRootDirectory(dir.before, current) {
			return false
		}
		if i != 0 {
			var named unix.Stat_t
			if unix.Fstatat(dirs[i-1].fd, dir.name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !ordinarySameRootDirectory(dir.before, named) {
				return false
			}
		}
	}
	return true
}

// The caller just created a child under the last pinned directory. Preserve
// every identity and mode check while accepting only its changed link count.
func ordinaryRootRefreshDirectory(dirs []composeLaunchDirectory) bool {
	if len(dirs) == 0 {
		return false
	}
	last := len(dirs) - 1
	var current unix.Stat_t
	if unix.Fstat(dirs[last].fd, &current) != nil || !ordinaryRootDirectorySafe(current) {
		return false
	}
	old := dirs[last].before
	if old.Dev != current.Dev || old.Ino != current.Ino || old.Mode != current.Mode || old.Uid != current.Uid || old.Gid != current.Gid {
		return false
	}
	if last != 0 {
		var named unix.Stat_t
		if unix.Fstatat(dirs[last-1].fd, dirs[last].name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !ordinarySameRootDirectory(current, named) {
			return false
		}
	}
	dirs[last].before.Nlink = current.Nlink
	return ordinaryRootDirectoriesUnchanged(dirs)
}
