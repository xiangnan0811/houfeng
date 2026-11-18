//go:build linux

package deploy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const OrdinaryComposeInitializationMarker = "ordinary-initialization.json"
const ordinaryInitializationLock = "ordinary-initialization.lock"

type ordinaryRootMarker struct {
	Version         int    `json:"version"`
	Phase           string `json:"phase"`
	DirectoryDevice uint64 `json:"directory_device"`
	DirectoryInode  uint64 `json:"directory_inode"`
	GuardDevice     uint64 `json:"guard_device"`
	GuardInode      uint64 `json:"guard_inode"`
	GuardNonce      string `json:"guard_nonce"`
	BootstrapName   string `json:"bootstrap_name"`
}

func ordinaryMarkerDisposition(marker ordinaryRootMarker) ordinaryDisposition {
	return ordinaryDisposition{Version: marker.Version, State: "CLEAN", DirectoryDevice: marker.DirectoryDevice, DirectoryInode: marker.DirectoryInode, GuardDevice: marker.GuardDevice, GuardInode: marker.GuardInode, GuardNonce: marker.GuardNonce, BootstrapName: marker.BootstrapName}
}

func ordinaryReadRootMarker(parent int) (ordinaryRootMarker, bool, error) {
	var marker ordinaryRootMarker
	fd, err := unix.Openat2(parent, OrdinaryComposeInitializationMarker, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK, Resolve: maintenanceGuardResolve})
	if errors.Is(err, unix.ENOENT) {
		return marker, false, nil
	}
	if err != nil {
		return marker, false, errOrdinaryUpgrade
	}
	file := os.NewFile(uintptr(fd), "ordinary root marker")
	defer file.Close()
	var before, after, named unix.Stat_t
	if unix.Fstat(fd, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Mode&0o7777 != 0o644 || before.Uid != 0 || before.Gid != 0 || before.Nlink != 1 || before.Size <= 0 || before.Size > 2048 {
		return marker, false, errOrdinaryUpgrade
	}
	body, err := io.ReadAll(io.LimitReader(file, 2049))
	if err != nil || len(body) != int(before.Size) || unix.Fstat(fd, &after) != nil || unix.Fstatat(parent, OrdinaryComposeInitializationMarker, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameSourceFile(before, after) || !sameSourceFile(before, named) {
		return marker, false, errOrdinaryUpgrade
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&marker) != nil {
		return marker, false, errOrdinaryUpgrade
	}
	canonical, err := json.Marshal(marker)
	if err != nil || !bytes.Equal(append(canonical, '\n'), body) || (marker.Phase != "PREPARED" && marker.Phase != "COMMITTED") || !ordinaryDispositionValid(ordinaryMarkerDisposition(marker)) {
		return marker, false, errOrdinaryUpgrade
	}
	return marker, true, nil
}

func ordinaryWriteRootMarker(parent int, marker ordinaryRootMarker, create bool) error {
	name, err := ordinaryLaunchTempName()
	if err != nil {
		return errOrdinaryUpgrade
	}
	fd, err := unix.Openat2(parent, name, &unix.OpenHow{Flags: unix.O_WRONLY | unix.O_CREAT | unix.O_EXCL | unix.O_CLOEXEC | unix.O_NOFOLLOW, Mode: 0o600, Resolve: maintenanceGuardResolve})
	if err != nil {
		return errOrdinaryUpgrade
	}
	file := os.NewFile(uintptr(fd), "ordinary marker temporary")
	defer func() { _ = file.Close(); _ = unix.Unlinkat(parent, name, 0) }()
	body, err := json.Marshal(marker)
	if err != nil || unix.Fchown(fd, 0, 0) != nil || unix.Fchmod(fd, 0o644) != nil {
		return errOrdinaryUpgrade
	}
	body = append(body, '\n')
	if n, err := file.Write(body); err != nil || n != len(body) || file.Sync() != nil {
		return errOrdinaryUpgrade
	}
	flags := uint(0)
	if create {
		flags = unix.RENAME_NOREPLACE
	}
	if err := unix.Renameat2(parent, name, parent, OrdinaryComposeInitializationMarker, flags); err != nil {
		return err
	}
	if unix.Fsync(parent) != nil {
		return errOrdinaryUpgrade
	}
	return nil
}

// ProvisionOrdinaryComposeGuard is called by the root storage initializer
// before any ordinary manager can run. It is the only guard bootstrap path.
func ProvisionOrdinaryComposeGuard(root string) error {
	if os.Getuid() != 0 || os.Geteuid() != 0 || !filepath.IsAbs(root) || filepath.Clean(root) != root || filepath.Base(root) != "ordinary" {
		return errOrdinaryUpgrade
	}
	dirs, err := openComposeLaunchDirectories(root)
	if err != nil || len(dirs) < 2 {
		return errOrdinaryUpgrade
	}
	defer func() {
		for _, dir := range dirs {
			_ = unix.Close(dir.fd)
		}
	}()
	parentPath := dirs[len(dirs)-2]
	guardDir := dirs[len(dirs)-1]
	if parentPath.before.Uid != 0 || parentPath.before.Mode&0o022 != 0 || guardDir.before.Uid != composeLaunchPublisherUID || guardDir.before.Gid != composeLaunchRuntimeGID || guardDir.before.Mode&0o7777 != 0o750 {
		return errOrdinaryUpgrade
	}
	parent, err := unix.Openat(parentPath.fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return errOrdinaryUpgrade
	}
	defer unix.Close(parent)
	lock, err := unix.Openat2(parent, ordinaryInitializationLock, &unix.OpenHow{Flags: unix.O_RDWR | unix.O_CREAT | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK, Mode: 0o600, Resolve: maintenanceGuardResolve})
	if err != nil {
		return errOrdinaryUpgrade
	}
	defer unix.Close(lock)
	var lockStat unix.Stat_t
	if unix.Fstat(lock, &lockStat) != nil || lockStat.Mode&unix.S_IFMT != unix.S_IFREG || lockStat.Mode&0o7777 != 0o600 || lockStat.Uid != 0 || lockStat.Gid != 0 || lockStat.Nlink != 1 || lockMaintenanceGuard(context.Background(), lock, true) != nil || unix.Fsync(lock) != nil || unix.Fsync(parent) != nil {
		return errOrdinaryUpgrade
	}
	marker, exists, err := ordinaryReadRootMarker(parent)
	if err != nil {
		return errOrdinaryUpgrade
	}
	if !exists {
		if !ordinaryDirectoryEmpty(guardDir.fd) {
			return errOrdinaryUpgrade
		}
		name, err := ordinaryLaunchTempName()
		if err != nil {
			return errOrdinaryUpgrade
		}
		fd, err := unix.Openat2(parent, name, &unix.OpenHow{Flags: unix.O_RDWR | unix.O_CREAT | unix.O_EXCL | unix.O_CLOEXEC | unix.O_NOFOLLOW, Mode: 0o600, Resolve: maintenanceGuardResolve})
		if err != nil {
			return errOrdinaryUpgrade
		}
		var nonce [32]byte
		_, randomErr := rand.Read(nonce[:])
		if randomErr != nil || unix.Fchown(fd, composeLaunchPublisherUID, composeLaunchRuntimeGID) != nil || unix.Fchmod(fd, 0o640) != nil {
			unix.Close(fd)
			return errOrdinaryUpgrade
		}
		n, writeErr := unix.Pwrite(fd, nonce[:], 0)
		anchor, anchorErr := ordinaryGuardIdentity(fd)
		if writeErr != nil || n != len(nonce) || anchorErr != nil || unix.Fsync(fd) != nil || unix.Fsync(parent) != nil {
			unix.Close(fd)
			return errOrdinaryUpgrade
		}
		unix.Close(fd)
		marker = ordinaryRootMarker{Version: 1, Phase: "PREPARED", DirectoryDevice: uint64(guardDir.before.Dev), DirectoryInode: guardDir.before.Ino, GuardDevice: uint64(anchor.stat.Dev), GuardInode: anchor.stat.Ino, GuardNonce: hex.EncodeToString(anchor.nonce[:]), BootstrapName: name}
		if ordinaryWriteRootMarker(parent, marker, true) != nil {
			return errOrdinaryUpgrade
		}
	}
	if marker.DirectoryDevice != uint64(guardDir.before.Dev) || marker.DirectoryInode != guardDir.before.Ino || !composeLaunchDirectoriesUnchanged(dirs) {
		return errOrdinaryUpgrade
	}
	guard, err := ordinaryOpenRootPreparedGuard(parent, guardDir.fd, dirs, marker)
	if err != nil {
		return errOrdinaryUpgrade
	}
	defer unix.Close(guard)
	if lockMaintenanceGuard(context.Background(), guard, true) != nil {
		return errOrdinaryUpgrade
	}
	anchor, err := ordinaryGuardIdentity(guard)
	if err != nil || !ordinaryDispositionMatches(ordinaryMarkerDisposition(marker), dirs, anchor) || !ordinaryGuardMatches(dirs, guard, anchor) {
		return errOrdinaryUpgrade
	}
	if marker.Phase == "PREPARED" && !ordinaryBootstrapContentsValid(guardDir.fd) {
		return errOrdinaryUpgrade
	}
	state, err := ordinaryBootstrapOrReadDisposition(guardDir.fd, marker)
	if err != nil || state.BootstrapName != marker.BootstrapName || !ordinaryDispositionMatches(state, dirs, anchor) || (marker.Phase == "PREPARED" && state.State != "CLEAN") {
		return errOrdinaryUpgrade
	}
	if marker.Phase == "PREPARED" {
		marker.Phase = "COMMITTED"
		if ordinaryWriteRootMarker(parent, marker, false) != nil {
			return errOrdinaryUpgrade
		}
	}
	if !ordinaryGuardMatches(dirs, guard, anchor) || !composeLaunchDirectoriesUnchanged(dirs) {
		return errOrdinaryUpgrade
	}
	return nil
}

// Keep a durable root-owned name until the exact lock link is synced in the
// ordinary directory. PREPARED recovery recognizes only these two exact names.
func ordinaryOpenRootPreparedGuard(parent, guardDir int, dirs []composeLaunchDirectory, marker ordinaryRootMarker) (int, error) {
	flags := uint64(unix.O_RDWR | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK)
	guard, err := unix.Openat2(guardDir, ordinaryGuardFile, &unix.OpenHow{Flags: flags, Resolve: maintenanceGuardResolve})
	if errors.Is(err, unix.ENOENT) && marker.Phase == "PREPARED" {
		source, sourceErr := unix.Openat2(parent, marker.BootstrapName, &unix.OpenHow{Flags: flags, Resolve: maintenanceGuardResolve})
		if sourceErr != nil {
			return -1, errOrdinaryUpgrade
		}
		anchor, anchorErr := ordinaryGuardIdentity(source)
		if anchorErr != nil || !ordinaryDispositionMatches(ordinaryMarkerDisposition(marker), dirs, anchor) || !ordinaryDirectoryEmpty(guardDir) || unix.Linkat(parent, marker.BootstrapName, guardDir, ordinaryGuardFile, 0) != nil || unix.Fsync(guardDir) != nil {
			unix.Close(source)
			return -1, errOrdinaryUpgrade
		}
		unix.Close(source)
		guard, err = unix.Openat2(guardDir, ordinaryGuardFile, &unix.OpenHow{Flags: flags, Resolve: maintenanceGuardResolve})
	}
	if err != nil {
		return -1, errOrdinaryUpgrade
	}
	var guardStat, sourceStat unix.Stat_t
	if unix.Fstat(guard, &guardStat) != nil {
		unix.Close(guard)
		return -1, errOrdinaryUpgrade
	}
	sourceErr := unix.Fstatat(parent, marker.BootstrapName, &sourceStat, unix.AT_SYMLINK_NOFOLLOW)
	var nonce [32]byte
	read, nonceErr := unix.Pread(guard, nonce[:], 0)
	if guardStat.Mode&unix.S_IFMT != unix.S_IFREG || guardStat.Mode&0o7777 != 0o640 || guardStat.Uid != composeLaunchPublisherUID || guardStat.Gid != composeLaunchRuntimeGID || guardStat.Size != int64(len(nonce)) || nonceErr != nil || read != len(nonce) || hex.EncodeToString(nonce[:]) != marker.GuardNonce {
		unix.Close(guard)
		return -1, errOrdinaryUpgrade
	}
	if sourceErr == nil {
		if marker.Phase != "PREPARED" || guardStat.Dev != sourceStat.Dev || guardStat.Ino != sourceStat.Ino || guardStat.Dev != marker.GuardDevice || guardStat.Ino != marker.GuardInode || guardStat.Nlink != 2 || sourceStat.Nlink != 2 || unix.Fsync(guardDir) != nil || unix.Unlinkat(parent, marker.BootstrapName, 0) != nil || unix.Fsync(parent) != nil {
			unix.Close(guard)
			return -1, errOrdinaryUpgrade
		}
	} else if !errors.Is(sourceErr, unix.ENOENT) {
		unix.Close(guard)
		return -1, errOrdinaryUpgrade
	}
	return guard, nil
}

// Before COMMITTED, no manager can have performed a protected operation.
// Resume only the exact canonical prefix of the root-recorded CLEAN record.
func ordinaryBootstrapOrReadDisposition(parent int, marker ordinaryRootMarker) (ordinaryDisposition, error) {
	state, exists, err := ordinaryReadDisposition(parent)
	if marker.Phase == "COMMITTED" {
		if err != nil || !exists {
			return ordinaryDisposition{}, errOrdinaryUpgrade
		}
		return state, nil
	}
	state = ordinaryMarkerDisposition(marker)
	body, err := json.Marshal(state)
	if err != nil {
		return ordinaryDisposition{}, errOrdinaryUpgrade
	}
	body = append(body, '\n')
	fd, err := unix.Openat2(parent, ordinaryDispositionFile, &unix.OpenHow{Flags: unix.O_RDWR | unix.O_CREAT | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK, Mode: 0o600, Resolve: maintenanceGuardResolve})
	if err != nil {
		return ordinaryDisposition{}, errOrdinaryUpgrade
	}
	defer unix.Close(fd)
	var before, named unix.Stat_t
	if unix.Fstat(fd, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Nlink != 1 || before.Size < 0 || before.Size > int64(len(body)) || !((before.Uid == 0 && before.Gid == 0 && before.Mode&0o7777 == 0o600) || (before.Uid == composeLaunchPublisherUID && before.Gid == composeLaunchRuntimeGID && (before.Mode&0o7777 == 0o600 || before.Mode&0o7777 == 0o640))) {
		return ordinaryDisposition{}, errOrdinaryUpgrade
	}
	prefix := make([]byte, int(before.Size))
	if len(prefix) != 0 {
		n, err := unix.Pread(fd, prefix, 0)
		if err != nil || n != len(prefix) || !bytes.Equal(prefix, body[:len(prefix)]) {
			return ordinaryDisposition{}, errOrdinaryUpgrade
		}
	}
	if unix.Fstatat(parent, ordinaryDispositionFile, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || named.Dev != before.Dev || named.Ino != before.Ino || unix.Fchown(fd, composeLaunchPublisherUID, composeLaunchRuntimeGID) != nil || unix.Fchmod(fd, 0o640) != nil {
		return ordinaryDisposition{}, errOrdinaryUpgrade
	}
	for written := len(prefix); written < len(body); {
		n, err := unix.Pwrite(fd, body[written:], int64(written))
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || n == 0 {
			return ordinaryDisposition{}, errOrdinaryUpgrade
		}
		written += n
	}
	if unix.Fsync(fd) != nil || unix.Fsync(parent) != nil {
		return ordinaryDisposition{}, errOrdinaryUpgrade
	}
	observed, exists, err := ordinaryReadDisposition(parent)
	if err != nil || !exists || observed != state {
		return ordinaryDisposition{}, errOrdinaryUpgrade
	}
	return state, nil
}

func ordinaryDirectoryEmpty(fd int) bool {
	copyFD, err := unix.Openat(fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return false
	}
	file := os.NewFile(uintptr(copyFD), "ordinary directory")
	defer file.Close()
	names, err := file.Readdirnames(1)
	return len(names) == 0 && errors.Is(err, io.EOF)
}

func ordinaryBootstrapContentsValid(fd int) bool {
	copyFD, err := unix.Openat(fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return false
	}
	file := os.NewFile(uintptr(copyFD), "ordinary bootstrap directory")
	defer file.Close()
	names, err := file.Readdirnames(-1)
	if err != nil {
		return false
	}
	for _, name := range names {
		if name != ordinaryGuardFile && name != ordinaryDispositionFile {
			return false
		}
	}
	return true
}
