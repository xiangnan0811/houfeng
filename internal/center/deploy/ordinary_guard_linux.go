//go:build linux

package deploy

import (
	"context"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const OrdinaryComposeGuardRoot = "/var/lib/houfeng-maintenance/ordinary"

const ordinaryGuardFile = "ordinary.lock"

// WithOrdinaryComposeGuard serializes ordinary Compose management. The root is
// provisioned by storage initialization as UID 10002, GID 10001, mode 0750.
// Root storage initialization creates the permanent lock and root marker.
// Empty root selects the production mount path. Host callers
// pass the canonical <working-directory>/maintenance/ordinary path instead.
// The callback must honor ctx itself once entered.
func WithOrdinaryComposeGuard(ctx context.Context, root string, exclusive bool, fn func() error) error {
	if fn == nil {
		return maintenanceGuardError("invalid ordinary guard invocation", unix.EINVAL)
	}
	return withOrdinaryComposeGuardFD(ctx, root, exclusive, func(int) error { return fn() })
}

// The coordinator may transfer a duplicate of this same locked open file
// description to a supervised participant. It must join the participant before
// completing; neither holder may unlock or convert the shared description.
func withOrdinaryComposeGuardFD(ctx context.Context, root string, exclusive bool, fn func(int) error) error {
	if ctx == nil || fn == nil {
		return maintenanceGuardError("invalid ordinary guard invocation", unix.EINVAL)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if root == "" {
		root = OrdinaryComposeGuardRoot
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || root == "/" {
		return maintenanceGuardError("invalid ordinary guard root", unix.EINVAL)
	}
	uid, euid := os.Getuid(), os.Geteuid()
	manager := (uid == 0 && euid == 0) || (uid == composeLaunchPublisherUID && euid == composeLaunchPublisherUID)
	if !manager && !(uid == 10001 && euid == 10001) {
		return maintenanceGuardError("ordinary guard requires manager or namespace identity", unix.EPERM)
	}
	if exclusive && !manager {
		return maintenanceGuardError("ordinary exclusive guard requires manager", unix.EPERM)
	}
	dirs, err := openComposeLaunchDirectories(root)
	if err != nil {
		return maintenanceGuardError("open ordinary guard root", err)
	}
	defer func() {
		for _, dir := range dirs {
			_ = unix.Close(dir.fd)
		}
	}()
	parent := dirs[len(dirs)-1].fd
	stat := dirs[len(dirs)-1].before
	if stat.Uid != composeLaunchPublisherUID || stat.Gid != composeLaunchRuntimeGID || stat.Mode&0o7777 != 0o750 {
		return maintenanceGuardError("unsafe ordinary guard root", unix.EPERM)
	}
	flags := uint64(unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK)
	if exclusive {
		flags = unix.O_RDWR | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
	}
	fd, err := unix.Openat2(parent, ordinaryGuardFile, &unix.OpenHow{Flags: flags, Resolve: maintenanceGuardResolve})
	if err != nil {
		return maintenanceGuardError("open ordinary lock", err)
	}
	defer unix.Close(fd)
	if err := lockMaintenanceGuard(ctx, fd, exclusive); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	identity, err := ordinaryGuardIdentity(fd)
	if err != nil || !ordinaryGuardMatches(dirs, fd, identity) {
		return maintenanceGuardError("ordinary guard identity", unix.ESTALE)
	}
	if len(dirs) < 2 || dirs[len(dirs)-2].before.Uid != 0 || dirs[len(dirs)-2].before.Mode&0o022 != 0 {
		return maintenanceGuardError("unsafe ordinary root marker parent", unix.EPERM)
	}
	marker, marked, err := ordinaryReadRootMarker(dirs[len(dirs)-2].fd)
	if err != nil || !marked || marker.Phase != "COMMITTED" || !ordinaryDispositionMatches(ordinaryMarkerDisposition(marker), dirs, identity) {
		return maintenanceGuardError("ordinary root marker closed", unix.ESTALE)
	}
	disposition, exists, err := ordinaryReadDisposition(parent)
	if err != nil || !exists || disposition.BootstrapName != marker.BootstrapName || !ordinaryDispositionMatches(disposition, dirs, identity) || (!exclusive && disposition.State != "CLEAN" && !ordinaryReadyAllowsShared(root, disposition)) {
		return maintenanceGuardError("ordinary disposition closed", unix.ESTALE)
	}
	// A creator can die after rename and before syncing the directory entry.
	// Make the observed permanent name durable before any guarded effect.
	if unix.Fsync(parent) != nil || !ordinaryGuardMatches(dirs, fd, identity) {
		return maintenanceGuardError("sync ordinary guard root", unix.EIO)
	}
	callbackErr := fn(fd)
	if !ordinaryGuardMatches(dirs, fd, identity) {
		return maintenanceGuardError("ordinary guard identity changed", unix.ESTALE)
	}
	currentMarker, marked, err := ordinaryReadRootMarker(dirs[len(dirs)-2].fd)
	if err != nil || !marked || currentMarker != marker {
		return maintenanceGuardError("ordinary root marker changed", unix.ESTALE)
	}
	if !exclusive {
		after, exists, err := ordinaryReadDisposition(parent)
		if err != nil || !exists || after != disposition || !ordinaryDispositionMatches(after, dirs, identity) || (after.State != "CLEAN" && !ordinaryReadyAllowsShared(root, after)) {
			return maintenanceGuardError("ordinary disposition changed", unix.ESTALE)
		}
	}
	return callbackErr
}

type ordinaryGuardAnchor struct {
	stat  unix.Stat_t
	nonce [32]byte
}

func ordinaryGuardIdentity(fd int) (ordinaryGuardAnchor, error) {
	var anchor ordinaryGuardAnchor
	if unix.Fstat(fd, &anchor.stat) != nil || anchor.stat.Mode&unix.S_IFMT != unix.S_IFREG || anchor.stat.Mode&0o7777 != 0o640 || anchor.stat.Uid != composeLaunchPublisherUID || anchor.stat.Gid != composeLaunchRuntimeGID || anchor.stat.Nlink != 1 || anchor.stat.Size != int64(len(anchor.nonce)) {
		return anchor, maintenanceGuardError("unsafe ordinary lock", unix.EPERM)
	}
	n, err := unix.Pread(fd, anchor.nonce[:], 0)
	if err != nil || n != len(anchor.nonce) || anchor.nonce == [32]byte{} {
		return anchor, maintenanceGuardError("invalid ordinary lock nonce", io.ErrUnexpectedEOF)
	}
	return anchor, nil
}

func ordinaryGuardMatches(dirs []composeLaunchDirectory, fd int, anchor ordinaryGuardAnchor) bool {
	if !composeLaunchDirectoriesUnchanged(dirs) {
		return false
	}
	actual, err := ordinaryGuardIdentity(fd)
	if err != nil || actual.stat.Dev != anchor.stat.Dev || actual.stat.Ino != anchor.stat.Ino || actual.nonce != anchor.nonce {
		return false
	}
	var named unix.Stat_t
	return unix.Fstatat(dirs[len(dirs)-1].fd, ordinaryGuardFile, &named, unix.AT_SYMLINK_NOFOLLOW) == nil && named.Dev == anchor.stat.Dev && named.Ino == anchor.stat.Ino && named.Mode == actual.stat.Mode && named.Uid == actual.stat.Uid && named.Gid == actual.stat.Gid && named.Nlink == 1
}
