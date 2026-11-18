//go:build linux

package deploy

import (
	"io"
	"os"
	"regexp"
	"strconv"

	"golang.org/x/sys/unix"
)

var ordinaryBackendName = regexp.MustCompile(`^backend-([1-9][0-9]*)-[a-f0-9]{32}\.json$`)

var ordinaryBackendTemporary = regexp.MustCompile(`^\.tmp-[a-f0-9]{32}$`)

// Read every registration after the old participant has been joined. An orphan
// temporary never exposed a connection; a two-link cut must match exactly one
// published registration. Unknown or changed entries fail closed.
func ordinaryReadBackendRecords(directory, operationID string) ([]ordinaryBackendRecord, error) {
	if !fenceRID.MatchString(operationID) {
		return nil, errOrdinaryUpgrade
	}
	dirs, err := ordinaryOpenPrivateDirectory(directory, composeLaunchRuntimeGID)
	if err != nil {
		return nil, errOrdinaryUpgrade
	}
	defer func() {
		for _, dir := range dirs {
			_ = unix.Close(dir.fd)
		}
	}()
	parent := dirs[len(dirs)-1]
	if parent.before.Uid != composeLaunchPublisherUID || parent.before.Mode&0o7777 != 0o700 {
		return nil, errOrdinaryUpgrade
	}
	copyFD, err := unix.Openat(parent.fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errOrdinaryUpgrade
	}
	list := os.NewFile(uintptr(copyFD), "ordinary backend directory")
	names, err := list.Readdirnames(-1)
	_ = list.Close()
	if err != nil {
		return nil, errOrdinaryUpgrade
	}
	for _, name := range names {
		if !ordinaryBackendTemporary.MatchString(name) {
			continue
		}
		var temp unix.Stat_t
		if unix.Fstatat(parent.fd, name, &temp, unix.AT_SYMLINK_NOFOLLOW) != nil || temp.Mode&unix.S_IFMT != unix.S_IFREG || temp.Mode&0o7777 != 0o600 || temp.Uid != composeLaunchPublisherUID || temp.Gid != composeLaunchRuntimeGID || (temp.Nlink != 1 && temp.Nlink != 2) || temp.Size > 8192 {
			return nil, errOrdinaryUpgrade
		}
		matches := 0
		for _, candidate := range names {
			if !ordinaryBackendName.MatchString(candidate) {
				continue
			}
			var named unix.Stat_t
			if unix.Fstatat(parent.fd, candidate, &named, unix.AT_SYMLINK_NOFOLLOW) != nil {
				return nil, errOrdinaryUpgrade
			}
			if temp.Dev == named.Dev && temp.Ino == named.Ino {
				matches++
			}
		}
		if (temp.Nlink == 1 && matches != 0) || (temp.Nlink == 2 && matches != 1) || unix.Unlinkat(parent.fd, name, 0) != nil || unix.Fsync(parent.fd) != nil || !ordinaryPrivateDirectoryUnchanged(dirs) {
			return nil, errOrdinaryUpgrade
		}
	}
	records := make([]ordinaryBackendRecord, 0, len(names))
	for _, name := range names {
		if ordinaryBackendTemporary.MatchString(name) {
			continue
		}
		match := ordinaryBackendName.FindStringSubmatch(name)
		if len(match) != 2 {
			return nil, errOrdinaryUpgrade
		}
		pid, err := strconv.ParseInt(match[1], 10, 32)
		if err != nil {
			return nil, errOrdinaryUpgrade
		}
		fd, err := unix.Openat2(parent.fd, name, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK, Resolve: maintenanceGuardResolve})
		if err != nil {
			return nil, errOrdinaryUpgrade
		}
		file := os.NewFile(uintptr(fd), "ordinary backend registration")
		var before, after, named unix.Stat_t
		if unix.Fstat(fd, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Mode&0o7777 != 0o600 || before.Uid != composeLaunchPublisherUID || before.Gid != composeLaunchRuntimeGID || before.Nlink != 1 || before.Size <= 0 || before.Size > 8192 {
			file.Close()
			return nil, errOrdinaryUpgrade
		}
		body, readErr := io.ReadAll(io.LimitReader(file, 8193))
		valid := readErr == nil && len(body) == int(before.Size) && unix.Fstat(fd, &after) == nil && unix.Fstatat(parent.fd, name, &named, unix.AT_SYMLINK_NOFOLLOW) == nil && sameSourceFile(before, after) && sameSourceFile(before, named)
		file.Close()
		var record ordinaryBackendRecord
		if !valid || fenceDecode(body, &record) != nil || record.Version != 1 || record.OperationID != operationID || record.PID != int32(pid) {
			return nil, errOrdinaryUpgrade
		}
		records = append(records, record)
	}
	if !ordinaryPrivateDirectoryUnchanged(dirs) {
		return nil, errOrdinaryUpgrade
	}
	return records, nil
}
