//go:build linux

package deploy

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Private authority and backend directories are intentionally inaccessible to
// the runtime group. Their ancestors retain the launch walker's traversal rules.
func ordinaryOpenPrivateDirectory(path string, gid uint32) ([]composeLaunchDirectory, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return nil, errOrdinaryUpgrade
	}
	dirs, err := openComposeLaunchDirectories(filepath.Dir(path))
	if err != nil {
		return nil, errOrdinaryUpgrade
	}
	closeDirs := func() {
		for _, dir := range dirs {
			_ = unix.Close(dir.fd)
		}
	}
	name := filepath.Base(path)
	fd, err := unix.Openat2(dirs[len(dirs)-1].fd, name, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		closeDirs()
		return nil, errOrdinaryUpgrade
	}
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Mode&0o7777 != 0o700 || stat.Uid != composeLaunchPublisherUID || stat.Gid != gid || stat.Nlink == 0 {
		_ = unix.Close(fd)
		closeDirs()
		return nil, errOrdinaryUpgrade
	}
	dirs = append(dirs, composeLaunchDirectory{fd: fd, name: name, before: stat})
	if !ordinaryPrivateDirectoryUnchanged(dirs) {
		closeDirs()
		return nil, errOrdinaryUpgrade
	}
	return dirs, nil
}

func ordinaryPrivateDirectoryUnchanged(dirs []composeLaunchDirectory) bool {
	if len(dirs) < 2 || !composeLaunchDirectoriesUnchanged(dirs[:len(dirs)-1]) {
		return false
	}
	parent, private := dirs[len(dirs)-2], dirs[len(dirs)-1]
	var opened, named unix.Stat_t
	if unix.Fstat(private.fd, &opened) != nil || unix.Fstatat(parent.fd, private.name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil {
		return false
	}
	same := func(st unix.Stat_t) bool {
		return st.Mode&unix.S_IFMT == unix.S_IFDIR && st.Mode&0o7777 == 0o700 && st.Nlink != 0 &&
			st.Dev == private.before.Dev && st.Ino == private.before.Ino && st.Mode == private.before.Mode &&
			st.Uid == private.before.Uid && st.Gid == private.before.Gid && st.Nlink == private.before.Nlink
	}
	return same(opened) && same(named)
}

// Root-only fence and secret directories use the root-owned input walker for
// their ancestors and remain inaccessible to the management UID.
type ordinaryPrivateDirectories struct {
	ancestors     []composeLaunchDirectory
	rootFD        int
	operationFD   int
	rootName      string
	operationName string
	rootStat      unix.Stat_t
	operationStat unix.Stat_t
}

func (d *ordinaryPrivateDirectories) Close() {
	_ = unix.Close(d.operationFD)
	_ = unix.Close(d.rootFD)
	for _, dir := range d.ancestors {
		_ = unix.Close(dir.fd)
	}
}

func ordinarySamePrivateDirectory(a, b unix.Stat_t) bool {
	return ordinaryPrivateDirectoryMatches(a) && ordinaryPrivateDirectoryMatches(b) && a.Dev == b.Dev && a.Ino == b.Ino && a.Nlink == b.Nlink
}

func (d *ordinaryPrivateDirectories) unchanged() bool {
	if len(d.ancestors) == 0 || !ordinaryRootDirectoriesUnchanged(d.ancestors) {
		return false
	}
	var root, rootNamed, operation, operationNamed unix.Stat_t
	parent := d.ancestors[len(d.ancestors)-1].fd
	return unix.Fstat(d.rootFD, &root) == nil && unix.Fstatat(parent, d.rootName, &rootNamed, unix.AT_SYMLINK_NOFOLLOW) == nil && ordinarySamePrivateDirectory(d.rootStat, root) && ordinarySamePrivateDirectory(root, rootNamed) && unix.Fstat(d.operationFD, &operation) == nil && unix.Fstatat(d.rootFD, d.operationName, &operationNamed, unix.AT_SYMLINK_NOFOLLOW) == nil && ordinarySamePrivateDirectory(d.operationStat, operation) && ordinarySamePrivateDirectory(operation, operationNamed)
}

func ordinaryOpenPrivateDirectories(parentPath, rootName, operationID string, create bool) (*ordinaryPrivateDirectories, error) {
	if os.Getuid() != 0 || os.Geteuid() != 0 || !filepath.IsAbs(parentPath) || filepath.Clean(parentPath) != parentPath || (rootName != ".ordinary-fences" && rootName != "ordinary-secrets") || !fenceRID.MatchString(operationID) {
		return nil, errOrdinaryUpgrade
	}
	ancestors, err := ordinaryOpenRootDirectories(parentPath)
	if err != nil {
		return nil, errOrdinaryUpgrade
	}
	closeAncestors := func() {
		for _, dir := range ancestors {
			_ = unix.Close(dir.fd)
		}
	}
	parent := ancestors[len(ancestors)-1].fd
	if create && (ordinaryPrivateMkdir(parent, rootName) != nil || !ordinaryRootRefreshDirectory(ancestors)) {
		closeAncestors()
		return nil, errOrdinaryUpgrade
	}
	rootFD, err := unix.Openat2(parent, rootName, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: maintenanceGuardResolve})
	if err != nil {
		closeAncestors()
		return nil, errOrdinaryUpgrade
	}
	closeRoot := func() { _ = unix.Close(rootFD); closeAncestors() }
	var originalRoot, namedRoot unix.Stat_t
	if unix.Fstat(rootFD, &originalRoot) != nil || unix.Fstatat(parent, rootName, &namedRoot, unix.AT_SYMLINK_NOFOLLOW) != nil || !ordinarySamePrivateDirectory(originalRoot, namedRoot) {
		closeRoot()
		return nil, errOrdinaryUpgrade
	}
	if create && ordinaryPrivateMkdir(rootFD, operationID) != nil {
		closeRoot()
		return nil, errOrdinaryUpgrade
	}
	var root unix.Stat_t
	if unix.Fstat(rootFD, &root) != nil || unix.Fstatat(parent, rootName, &namedRoot, unix.AT_SYMLINK_NOFOLLOW) != nil || !ordinarySamePrivateDirectory(root, namedRoot) || originalRoot.Dev != root.Dev || originalRoot.Ino != root.Ino {
		closeRoot()
		return nil, errOrdinaryUpgrade
	}
	operationFD, err := unix.Openat2(rootFD, operationID, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: maintenanceGuardResolve})
	if err != nil {
		closeRoot()
		return nil, errOrdinaryUpgrade
	}
	result := &ordinaryPrivateDirectories{ancestors: ancestors, rootFD: rootFD, operationFD: operationFD, rootName: rootName, operationName: operationID, rootStat: root}
	if unix.Fstat(operationFD, &result.operationStat) != nil || !result.unchanged() {
		result.Close()
		return nil, errOrdinaryUpgrade
	}
	return result, nil
}
