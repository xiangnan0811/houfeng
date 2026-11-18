//go:build linux

package deploy

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type ordinaryBootstrapDirectoryIntent struct {
	Version      int    `json:"version"`
	ParentDevice uint64 `json:"parent_device"`
	ParentInode  uint64 `json:"parent_inode"`
	Name         string `json:"name"`
	UID          uint32 `json:"uid"`
	GID          uint32 `json:"gid"`
	Nonce        string `json:"nonce"`
}

type ordinaryBootstrapDirectoryCommit struct {
	Version int                              `json:"version"`
	Intent  ordinaryBootstrapDirectoryIntent `json:"intent"`
	Device  uint64                           `json:"device"`
	Inode   uint64                           `json:"inode"`
}

type ordinaryBootstrapCommit struct {
	Version               int    `json:"version"`
	ControllerDevice      uint64 `json:"controller_device"`
	ControllerInode       uint64 `json:"controller_inode"`
	ControllerNonceSHA256 string `json:"controller_nonce_sha256"`
	MaintenanceDevice     uint64 `json:"maintenance_device"`
	MaintenanceInode      uint64 `json:"maintenance_inode"`
	GuardDirectoryDevice  uint64 `json:"guard_directory_device"`
	GuardDirectoryInode   uint64 `json:"guard_directory_inode"`
	GuardDevice           uint64 `json:"guard_device"`
	GuardInode            uint64 `json:"guard_inode"`
	GuardNonce            string `json:"guard_nonce"`
	BootstrapName         string `json:"bootstrap_name"`
}

// Directory creation has its own root-owned durable intent. An uncommitted
// replay may finish only the exact empty intermediate authorized by that intent.
func ordinaryBootstrapDirectory(parent int, name string, uid, gid uint32) (bool, error) {
	if name != "maintenance" && name != "ordinary" {
		return false, errOrdinaryUpgrade
	}
	var parentStat unix.Stat_t
	if unix.Fstat(parent, &parentStat) != nil || parentStat.Mode&unix.S_IFMT != unix.S_IFDIR || parentStat.Uid != 0 || parentStat.Mode&0o022 != 0 {
		return false, errOrdinaryUpgrade
	}
	intentName := ".ordinary-bootstrap-" + name + ".intent.json"
	commitName := ".ordinary-bootstrap-" + name + ".commit.json"
	body, exists, err := ordinaryReadImageRecord(parent, intentName)
	if err != nil {
		return false, errOrdinaryUpgrade
	}
	var intent ordinaryBootstrapDirectoryIntent
	if exists {
		if ordinaryReadCanonicalRecord(body, &intent) != nil || intent.Version != 1 || intent.ParentDevice != uint64(parentStat.Dev) || intent.ParentInode != parentStat.Ino || intent.Name != name || intent.UID != uid || intent.GID != gid || !ordinaryImageNonceValid(intent.Nonce) {
			return false, errOrdinaryUpgrade
		}
	} else {
		var existing unix.Stat_t
		err := unix.Fstatat(parent, name, &existing, unix.AT_SYMLINK_NOFOLLOW)
		if err == nil {
			if existing.Mode&unix.S_IFMT != unix.S_IFDIR || existing.Uid != uid || existing.Nlink == 0 || (name == "ordinary" && (existing.Gid != gid || existing.Mode&0o7777 != 0o750)) || (name == "maintenance" && existing.Mode&0o7777 != 0o755 && (existing.Mode&0o7777 != 0o750 || existing.Gid != gid)) {
				return false, errOrdinaryUpgrade
			}
			if _, committed, err := ordinaryReadImageRecord(parent, commitName); err != nil || committed {
				return false, errOrdinaryUpgrade
			}
			return false, nil
		}
		if !errors.Is(err, unix.ENOENT) {
			return false, errOrdinaryUpgrade
		}
		if _, committed, err := ordinaryReadImageRecord(parent, commitName); err != nil || committed {
			return false, errOrdinaryUpgrade
		}
		var nonce [16]byte
		if _, err := rand.Read(nonce[:]); err != nil {
			return false, errOrdinaryUpgrade
		}
		intent = ordinaryBootstrapDirectoryIntent{Version: 1, ParentDevice: uint64(parentStat.Dev), ParentInode: parentStat.Ino, Name: name, UID: uid, GID: gid, Nonce: hex.EncodeToString(nonce[:])}
		if ordinaryImageRecord(parent, intentName, intent) != nil {
			return false, errOrdinaryUpgrade
		}
	}
	commitBody, committed, err := ordinaryReadImageRecord(parent, commitName)
	if err != nil {
		return false, errOrdinaryUpgrade
	}
	var commit ordinaryBootstrapDirectoryCommit
	if committed {
		if ordinaryReadCanonicalRecord(commitBody, &commit) != nil || commit.Version != 1 || commit.Intent != intent || commit.Inode == 0 {
			return false, errOrdinaryUpgrade
		}
	} else if err := unix.Mkdirat(parent, name, 0o750); err != nil && !errors.Is(err, unix.EEXIST) {
		return false, errOrdinaryUpgrade
	}
	fd, err := unix.Openat2(parent, name, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW, Resolve: maintenanceGuardResolve})
	if err != nil {
		return false, errOrdinaryUpgrade
	}
	defer unix.Close(fd)
	var before, named unix.Stat_t
	if unix.Fstat(fd, &before) != nil || unix.Fstatat(parent, name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || before.Mode&unix.S_IFMT != unix.S_IFDIR || before.Nlink == 0 || before.Dev != named.Dev || before.Ino != named.Ino || before.Mode != named.Mode || before.Uid != named.Uid || before.Gid != named.Gid || before.Nlink != named.Nlink {
		return false, errOrdinaryUpgrade
	}
	if committed {
		if uint64(before.Dev) != commit.Device || before.Ino != commit.Inode || before.Uid != uid || before.Gid != gid || before.Mode&0o7777 != 0o750 || unix.Fsync(parent) != nil {
			return false, errOrdinaryUpgrade
		}
		return true, nil
	}
	if (before.Uid != 0 && before.Uid != uid) || (before.Gid != 0 && before.Gid != gid) || (before.Mode&0o7777 != 0o700 && before.Mode&0o7777 != 0o750) || ordinaryImageDirectoryEmpty(fd) != nil {
		return false, errOrdinaryUpgrade
	}
	if unix.Fchown(fd, int(uid), int(gid)) != nil || unix.Fchmod(fd, 0o750) != nil || unix.Fsync(fd) != nil || unix.Fsync(parent) != nil {
		return false, errOrdinaryUpgrade
	}
	var after, afterNamed unix.Stat_t
	if unix.Fstat(fd, &after) != nil || unix.Fstatat(parent, name, &afterNamed, unix.AT_SYMLINK_NOFOLLOW) != nil || after.Dev != before.Dev || after.Ino != before.Ino || after.Uid != uid || after.Gid != gid || after.Mode&0o7777 != 0o750 || !sameComposeLaunchDirectory(after, afterNamed) || ordinaryImageDirectoryEmpty(fd) != nil {
		return false, errOrdinaryUpgrade
	}
	commit = ordinaryBootstrapDirectoryCommit{Version: 1, Intent: intent, Device: uint64(after.Dev), Inode: after.Ino}
	if ordinaryImageRecord(parent, commitName, commit) != nil {
		return false, errOrdinaryUpgrade
	}
	return true, nil
}

// Called under the working-directory controller lock, before ordinary EX.
func ordinaryBootstrapGuard(work string) error {
	if os.Getuid() != 0 || os.Geteuid() != 0 {
		return errOrdinaryUpgrade
	}
	workDirs, err := ordinaryImageDirectories(work)
	if err != nil {
		return errOrdinaryUpgrade
	}
	defer func() {
		for _, dir := range workDirs {
			_ = unix.Close(dir.fd)
		}
	}()
	workFD := workDirs[len(workDirs)-1].fd
	controllerFD, err := unix.Openat2(workFD, ordinaryControllerLockName, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK, Resolve: maintenanceGuardResolve})
	if err != nil {
		return errOrdinaryUpgrade
	}
	defer unix.Close(controllerFD)
	var controller, namedController unix.Stat_t
	var nonce [32]byte
	if unix.Fstat(controllerFD, &controller) != nil || unix.Fstatat(workFD, ordinaryControllerLockName, &namedController, unix.AT_SYMLINK_NOFOLLOW) != nil || controller.Mode&unix.S_IFMT != unix.S_IFREG || controller.Mode&0o7777 != 0o600 || controller.Uid != 0 || controller.Gid != 0 || controller.Nlink != 1 || controller.Size != int64(len(nonce)) || !sameSourceFile(controller, namedController) {
		return errOrdinaryUpgrade
	}
	if n, err := unix.Pread(controllerFD, nonce[:], 0); err != nil || n != len(nonce) || nonce == [32]byte{} {
		return errOrdinaryUpgrade
	}
	commitBody, committed, err := ordinaryReadImageRecord(workFD, ".ordinary-bootstrap.json")
	if err != nil {
		return errOrdinaryUpgrade
	}
	var prior ordinaryBootstrapCommit
	if committed && (ordinaryReadCanonicalRecord(commitBody, &prior) != nil || prior.Version != 1) {
		return errOrdinaryUpgrade
	}
	if committed {
		var maintenance unix.Stat_t
		if unix.Fstatat(workFD, "maintenance", &maintenance, unix.AT_SYMLINK_NOFOLLOW) != nil || uint64(maintenance.Dev) != prior.MaintenanceDevice || maintenance.Ino != prior.MaintenanceInode {
			return errOrdinaryUpgrade
		}
	}
	if _, err := ordinaryBootstrapDirectory(workFD, "maintenance", 0, composeLaunchRuntimeGID); err != nil || !ordinaryImageRefreshDirectory(workDirs) {
		return errOrdinaryUpgrade
	}
	maintenancePath := filepath.Join(work, "maintenance")
	maintenanceDirs, err := ordinaryImageDirectories(maintenancePath)
	if err != nil {
		return errOrdinaryUpgrade
	}
	defer func() {
		for _, dir := range maintenanceDirs {
			_ = unix.Close(dir.fd)
		}
	}()
	parent := maintenanceDirs[len(maintenanceDirs)-1]
	marker, markerExists, err := ordinaryReadRootMarker(parent.fd)
	if err != nil || (committed && (!markerExists || marker.Phase != "COMMITTED")) {
		return errOrdinaryUpgrade
	}
	var existing unix.Stat_t
	guardErr := unix.Fstatat(parent.fd, "ordinary", &existing, unix.AT_SYMLINK_NOFOLLOW)
	if (committed || markerExists) && guardErr != nil {
		return errOrdinaryUpgrade
	}
	owned, err := ordinaryBootstrapDirectory(parent.fd, "ordinary", composeLaunchPublisherUID, composeLaunchRuntimeGID)
	if err != nil || !ordinaryImageRefreshDirectory(maintenanceDirs) || (!markerExists && !owned) {
		return errOrdinaryUpgrade
	}
	guardRoot := filepath.Join(maintenancePath, "ordinary")
	if (!markerExists || marker.Phase == "PREPARED") && ProvisionOrdinaryComposeGuard(guardRoot) != nil {
		return errOrdinaryUpgrade
	}
	state, err := ordinaryObserveDisposition(guardRoot)
	if err != nil {
		return errOrdinaryUpgrade
	}
	record := ordinaryBootstrapCommit{Version: 1, ControllerDevice: uint64(controller.Dev), ControllerInode: controller.Ino, ControllerNonceSHA256: ordinaryDigestHex(nonce[:]), MaintenanceDevice: uint64(parent.before.Dev), MaintenanceInode: parent.before.Ino, GuardDirectoryDevice: state.DirectoryDevice, GuardDirectoryInode: state.DirectoryInode, GuardDevice: state.GuardDevice, GuardInode: state.GuardInode, GuardNonce: state.GuardNonce, BootstrapName: state.BootstrapName}
	if committed && record != prior {
		return errOrdinaryUpgrade
	}
	if ordinaryImageRecord(workFD, ".ordinary-bootstrap.json", record) != nil || !ordinaryImageRefreshDirectory(workDirs) {
		return errOrdinaryUpgrade
	}
	return nil
}
