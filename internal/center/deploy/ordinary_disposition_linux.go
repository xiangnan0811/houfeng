//go:build linux

package deploy

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

const ordinaryDispositionFile = "disposition.json"

type ordinaryDisposition struct {
	Version         int    `json:"version"`
	State           string `json:"state"`
	DirectoryDevice uint64 `json:"directory_device"`
	DirectoryInode  uint64 `json:"directory_inode"`
	GuardDevice     uint64 `json:"guard_device"`
	GuardInode      uint64 `json:"guard_inode"`
	GuardNonce      string `json:"guard_nonce"`
	BootstrapName   string `json:"bootstrap_name"`
	OperationID     string `json:"operation_id"`
	IntentSHA256    string `json:"intent_sha256"`
}

func ordinaryDispositionValid(state ordinaryDisposition) bool {
	if state.Version != 1 || state.DirectoryInode == 0 || state.GuardInode == 0 || !fenceID.MatchString(state.GuardNonce) || !strings.HasPrefix(state.BootstrapName, ".ordinary-launch-") || len(state.BootstrapName) != len(".ordinary-launch-")+32 || !fenceID.MatchString(state.BootstrapName[len(".ordinary-launch-"):]+strings.Repeat("0", 32)) {
		return false
	}
	switch state.State {
	case "CLEAN":
		return state.OperationID == "" && state.IntentSHA256 == ""
	case "ACTIVE", "COMMITTED_READY_FOR_RESTART":
		return fenceRID.MatchString(state.OperationID) && fenceID.MatchString(state.IntentSHA256)
	default:
		return false
	}
}

func ordinaryReadDisposition(parent int) (ordinaryDisposition, bool, error) {
	var state ordinaryDisposition
	fd, err := unix.Openat2(parent, ordinaryDispositionFile, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK, Resolve: maintenanceGuardResolve})
	if errors.Is(err, unix.ENOENT) {
		return state, false, nil
	}
	if err != nil {
		return state, false, errOrdinaryUpgrade
	}
	file := os.NewFile(uintptr(fd), "ordinary disposition")
	defer file.Close()
	var before, after, named unix.Stat_t
	if unix.Fstat(fd, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Mode&0o7777 != 0o640 || before.Uid != composeLaunchPublisherUID || before.Gid != composeLaunchRuntimeGID || before.Nlink != 1 || before.Size <= 0 || before.Size > 2048 {
		return state, false, errOrdinaryUpgrade
	}
	body, err := io.ReadAll(io.LimitReader(file, 2049))
	if err != nil || len(body) != int(before.Size) || unix.Fstat(fd, &after) != nil || unix.Fstatat(parent, ordinaryDispositionFile, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameSourceFile(before, after) || !sameSourceFile(before, named) {
		return state, false, errOrdinaryUpgrade
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&state) != nil {
		return state, false, errOrdinaryUpgrade
	}
	canonical, err := json.Marshal(state)
	if err != nil || !bytes.Equal(append(canonical, '\n'), body) || !ordinaryDispositionValid(state) {
		return state, false, errOrdinaryUpgrade
	}
	return state, true, nil
}

func ordinaryDispositionMatches(state ordinaryDisposition, dirs []composeLaunchDirectory, anchor ordinaryGuardAnchor) bool {
	parent := dirs[len(dirs)-1].before
	return state.DirectoryDevice == uint64(parent.Dev) && state.DirectoryInode == parent.Ino && state.GuardDevice == uint64(anchor.stat.Dev) && state.GuardInode == anchor.stat.Ino && state.GuardNonce == hex.EncodeToString(anchor.nonce[:])
}

// Called while holding exclusive access to the same permanent guard (or while
// publishing its first bootstrap record before the permanent name exists).
func ordinaryWriteDisposition(dirs []composeLaunchDirectory, next ordinaryDisposition, create bool) error {
	if !ordinaryDispositionValid(next) || !composeLaunchDirectoriesUnchanged(dirs) {
		return errOrdinaryUpgrade
	}
	parent := dirs[len(dirs)-1].fd
	name, err := ordinaryLaunchTempName()
	if err != nil {
		return errOrdinaryUpgrade
	}
	fd, err := unix.Openat2(parent, name, &unix.OpenHow{Flags: unix.O_WRONLY | unix.O_CREAT | unix.O_EXCL | unix.O_CLOEXEC | unix.O_NOFOLLOW, Mode: 0o600, Resolve: maintenanceGuardResolve})
	if err != nil {
		return errOrdinaryUpgrade
	}
	file := os.NewFile(uintptr(fd), "ordinary disposition temporary")
	defer func() { _ = file.Close(); _ = unix.Unlinkat(parent, name, 0) }()
	body, err := json.Marshal(next)
	if err != nil || unix.Fchown(fd, composeLaunchPublisherUID, composeLaunchRuntimeGID) != nil || unix.Fchmod(fd, 0o640) != nil {
		return errOrdinaryUpgrade
	}
	body = append(body, '\n')
	if n, err := file.Write(body); err != nil || n != len(body) || file.Sync() != nil || !composeLaunchDirectoriesUnchanged(dirs) {
		return errOrdinaryUpgrade
	}
	flags := uint(0)
	if create {
		flags = unix.RENAME_NOREPLACE
	}
	if err := unix.Renameat2(parent, name, parent, ordinaryDispositionFile, flags); err != nil {
		return err
	}
	if unix.Fsync(parent) != nil || !composeLaunchDirectoriesUnchanged(dirs) {
		return errOrdinaryUpgrade
	}
	return nil
}
