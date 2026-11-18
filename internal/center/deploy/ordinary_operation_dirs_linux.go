//go:build linux

package deploy

import (
	"errors"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type ordinaryOperationDirectories struct {
	path            string
	backendPath     string
	fenceConfigPath string
	dirs            []composeLaunchDirectory
}

func (d *ordinaryOperationDirectories) Close() {
	for _, dir := range d.dirs {
		_ = unix.Close(dir.fd)
	}
}

// New directories are made only while the controller lock is held. Recovery
// passes create=false and never creates a second operation for an ACTIVE intent.
func ordinaryOpenOperationDirectories(work, operationID string, create bool) (*ordinaryOperationDirectories, error) {
	if !fenceRID.MatchString(operationID) || !filepath.IsAbs(work) || filepath.Clean(work) != work {
		return nil, errOrdinaryUpgrade
	}
	fencePath, err := ordinaryFenceControlPath(work, operationID, create)
	if err != nil {
		return nil, errOrdinaryUpgrade
	}
	path := filepath.Join(work, "maintenance")
	for _, name := range []string{"ordinary-operations", operationID} {
		dirs, err := ordinaryImageDirectories(path)
		if err != nil {
			return nil, errOrdinaryUpgrade
		}
		parent := dirs[len(dirs)-1].fd
		if create {
			err = ordinaryImageMkdir(parent, name)
			if err == nil && !ordinaryImageRefreshDirectory(dirs) {
				err = errOrdinaryUpgrade
			}
		}
		for _, dir := range dirs {
			_ = unix.Close(dir.fd)
		}
		if err != nil {
			return nil, errOrdinaryUpgrade
		}
		path = filepath.Join(path, name)
	}
	dirs, err := ordinaryImageDirectories(path)
	if err != nil {
		return nil, errOrdinaryUpgrade
	}
	closeDirs := func() {
		for _, dir := range dirs {
			_ = unix.Close(dir.fd)
		}
	}
	parent := dirs[len(dirs)-1].fd
	for _, child := range []struct {
		name     string
		uid, gid uint32
	}{{"backends", composeLaunchPublisherUID, composeLaunchRuntimeGID}} {
		made := false
		if create {
			if err := unix.Mkdirat(parent, child.name, 0o700); err == nil {
				made = true
			} else if !errors.Is(err, unix.EEXIST) {
				closeDirs()
				return nil, errOrdinaryUpgrade
			}
		}
		fd, err := unix.Openat2(parent, child.name, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: maintenanceGuardResolve})
		if err != nil {
			closeDirs()
			return nil, errOrdinaryUpgrade
		}
		if made && (unix.Fchown(fd, int(child.uid), int(child.gid)) != nil || unix.Fchmod(fd, 0o700) != nil || unix.Fsync(fd) != nil || unix.Fsync(parent) != nil || !ordinaryImageRefreshDirectory(dirs)) {
			unix.Close(fd)
			closeDirs()
			return nil, errOrdinaryUpgrade
		}
		var stat unix.Stat_t
		valid := unix.Fstat(fd, &stat) == nil && stat.Mode&unix.S_IFMT == unix.S_IFDIR && stat.Mode&0o7777 == 0o700 && stat.Uid == child.uid && stat.Gid == child.gid && stat.Nlink != 0
		unix.Close(fd)
		if !valid {
			closeDirs()
			return nil, errOrdinaryUpgrade
		}
	}
	return &ordinaryOperationDirectories{path: path, backendPath: filepath.Join(path, "backends"), fenceConfigPath: fencePath, dirs: dirs}, nil
}

// Fence control must not overlap any Compose bind mount. This private root
// sibling is never mounted into the project and does not relax source fencing.
func ordinaryFenceControlPath(work, operationID string, create bool) (string, error) {
	dirs, err := ordinaryOpenPrivateDirectories(work, ".ordinary-fences", operationID, create)
	if err != nil {
		return "", errOrdinaryUpgrade
	}
	defer dirs.Close()
	if !dirs.unchanged() {
		return "", errOrdinaryUpgrade
	}
	return filepath.Join(work, ".ordinary-fences", operationID, "request.json"), nil
}
