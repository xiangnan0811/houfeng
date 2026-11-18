//go:build linux

package deploy

import "golang.org/x/sys/unix"

// Used only while holding the controller-only lock to identify a previously
// recorded child before ordinary EX. CLEAN is rechecked after acquiring EX;
// this observation alone never authorizes a protected transition.
func ordinaryObserveDisposition(root string) (ordinaryDisposition, error) {
	var state ordinaryDisposition
	dirs, err := openComposeLaunchDirectories(root)
	if err != nil || len(dirs) < 2 {
		return state, errOrdinaryUpgrade
	}
	defer func() {
		for _, dir := range dirs {
			_ = unix.Close(dir.fd)
		}
	}()
	parent, rootStat := dirs[len(dirs)-1].fd, dirs[len(dirs)-1].before
	if rootStat.Uid != composeLaunchPublisherUID || rootStat.Gid != composeLaunchRuntimeGID || rootStat.Mode&0o7777 != 0o750 || dirs[len(dirs)-2].before.Uid != 0 || dirs[len(dirs)-2].before.Mode&0o022 != 0 {
		return state, errOrdinaryUpgrade
	}
	fd, err := unix.Openat2(parent, ordinaryGuardFile, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK, Resolve: maintenanceGuardResolve})
	if err != nil {
		return state, errOrdinaryUpgrade
	}
	defer unix.Close(fd)
	anchor, err := ordinaryGuardIdentity(fd)
	if err != nil || !ordinaryGuardMatches(dirs, fd, anchor) {
		return state, errOrdinaryUpgrade
	}
	marker, exists, err := ordinaryReadRootMarker(dirs[len(dirs)-2].fd)
	if err != nil || !exists || marker.Phase != "COMMITTED" || !ordinaryDispositionMatches(ordinaryMarkerDisposition(marker), dirs, anchor) {
		return state, errOrdinaryUpgrade
	}
	state, exists, err = ordinaryReadDisposition(parent)
	if err != nil || !exists || state.BootstrapName != marker.BootstrapName || !ordinaryDispositionMatches(state, dirs, anchor) || !ordinaryGuardMatches(dirs, fd, anchor) {
		return ordinaryDisposition{}, errOrdinaryUpgrade
	}
	return state, nil
}
