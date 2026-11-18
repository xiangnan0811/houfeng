//go:build linux

package deploy

import (
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Create only root-controlled parent directories before taking the ordinary
// guard. Their later operation-specific children do not change an ancestor of
// the permanent guard while it is held.
func ordinaryPrepareControllerRoots(request ordinaryUpgradeRequest, create bool) error {
	dirs, err := ordinaryImageDirectories(filepath.Join(request.WorkingDirectory, "maintenance"))
	if err != nil {
		return errOrdinaryUpgrade
	}
	defer func() {
		for _, dir := range dirs {
			_ = unix.Close(dir.fd)
		}
	}()
	parent := dirs[len(dirs)-1].fd
	if create {
		if ordinaryImageMkdirJournalled(parent, "ordinary-targets", request.Project, request.OperationID) != nil || !ordinaryImageRefreshDirectory(dirs) {
			return errOrdinaryUpgrade
		}
		if ordinaryPrivateMkdir(parent, "ordinary-secrets") != nil || !ordinaryImageRefreshDirectory(dirs) {
			return errOrdinaryUpgrade
		}
	}
	for _, item := range []struct {
		name      string
		gid, mode uint32
	}{{"ordinary-targets", composeLaunchRuntimeGID, 0o750}, {"ordinary-secrets", 0, 0o700}} {
		var stat unix.Stat_t
		if unix.Fstatat(parent, item.name, &stat, unix.AT_SYMLINK_NOFOLLOW) != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != 0 || stat.Gid != item.gid || stat.Mode&0o7777 != item.mode || stat.Nlink == 0 {
			return errOrdinaryUpgrade
		}
	}
	if !composeLaunchDirectoriesUnchanged(dirs) {
		return errOrdinaryUpgrade
	}
	return nil
}
