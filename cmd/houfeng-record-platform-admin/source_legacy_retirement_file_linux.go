//go:build linux

package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// A token file is only accessed through a private owned directory. The
// directory and its name are checked again around publication and reading.
type legacySourcePrivateOutput struct {
	parent     int
	parentPath string
	parentStat unix.Stat_t
	name       string
	file       *os.File
	fileStat   unix.Stat_t
}

func legacySourcePrivatePath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && path != "/" && !strings.ContainsRune(path, '\x00')
}

func legacySourcePrivateParent(path string) (int, unix.Stat_t, error) {
	var stat unix.Stat_t
	if !legacySourcePrivatePath(path) {
		return -1, stat, errLegacySourceInput
	}
	fd, err := unix.Openat2(unix.AT_FDCWD, filepath.Dir(path), &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		return -1, stat, errLegacySourceInput
	}
	if unix.Fstat(fd, &stat) != nil || !legacySourcePrivateDirectory(stat) {
		_ = unix.Close(fd)
		return -1, unix.Stat_t{}, errLegacySourceInput
	}
	return fd, stat, nil
}

func legacySourcePrivateDirectory(stat unix.Stat_t) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFDIR && stat.Mode&0o7077 == 0 &&
		stat.Uid == uint32(os.Geteuid()) && stat.Nlink > 0
}

func legacySourceSameDirectory(left, right unix.Stat_t) bool {
	return legacySourcePrivateDirectory(right) && left.Dev == right.Dev && left.Ino == right.Ino &&
		left.Mode == right.Mode && left.Uid == right.Uid && left.Gid == right.Gid && left.Nlink == right.Nlink
}

func legacySourceParentUnchanged(parent int, path string, original unix.Stat_t) bool {
	var held unix.Stat_t
	if unix.Fstat(parent, &held) != nil || !legacySourceSameDirectory(original, held) {
		return false
	}
	current, err := unix.Openat2(unix.AT_FDCWD, path, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		return false
	}
	defer unix.Close(current)
	var named unix.Stat_t
	return unix.Fstat(current, &named) == nil && legacySourceSameDirectory(original, named)
}

func legacySourcePrivateFile(stat unix.Stat_t) bool {
	return stat.Mode&unix.S_IFMT == unix.S_IFREG && stat.Mode&0o7777 == 0o600 &&
		stat.Uid == uint32(os.Geteuid()) && stat.Nlink == 1 && stat.Size >= 0 && stat.Size <= sourceRetirementFileLimit
}

func legacySourceSameFile(left, right unix.Stat_t) bool {
	return legacySourcePrivateFile(right) && left.Dev == right.Dev && left.Ino == right.Ino &&
		left.Mode == right.Mode && left.Uid == right.Uid && left.Gid == right.Gid && left.Nlink == right.Nlink
}

// Reserve the operator's name before minting. The empty reservation is made
// durable; no failure path unlinks or overwrites it or any existing request.
func reserveLegacySourceOutput(path string) (*legacySourcePrivateOutput, error) {
	parent, parentStat, err := legacySourcePrivateParent(path)
	if err != nil {
		return nil, errLegacySourceInput
	}
	fd, err := unix.Openat2(parent, filepath.Base(path), &unix.OpenHow{
		Flags: unix.O_WRONLY | unix.O_CREAT | unix.O_EXCL | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Mode:  0o600, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		_ = unix.Close(parent)
		return nil, errLegacySourceInput
	}
	output := &legacySourcePrivateOutput{parent: parent, parentPath: filepath.Dir(path), parentStat: parentStat,
		name: filepath.Base(path), file: os.NewFile(uintptr(fd), "private-source-request")}
	if unix.Fchmod(fd, 0o600) != nil || unix.Fstat(fd, &output.fileStat) != nil || !legacySourcePrivateFile(output.fileStat) || output.fileStat.Size != 0 ||
		!legacySourceParentUnchanged(parent, output.parentPath, parentStat) || output.file.Sync() != nil || unix.Fsync(parent) != nil {
		output.close()
		return nil, errLegacySourceOutput
	}
	return output, nil
}

func (output *legacySourcePrivateOutput) close() {
	if output == nil {
		return
	}
	if output.file != nil {
		_ = output.file.Close()
		output.file = nil
	}
	if output.parent >= 0 {
		_ = unix.Close(output.parent)
		output.parent = -1
	}
}

func (output *legacySourcePrivateOutput) publish(wire legacySourceTokenFile) error {
	body, err := json.Marshal(wire)
	if err != nil || len(body)+1 > sourceRetirementFileLimit {
		return errLegacySourceOutput
	}
	body = append(body, '\n')
	defer clear(body)
	if output == nil || output.file == nil || output.parent < 0 ||
		!legacySourceParentUnchanged(output.parent, output.parentPath, output.parentStat) {
		return errLegacySourceOutput
	}
	var held, named unix.Stat_t
	if unix.Fstat(int(output.file.Fd()), &held) != nil || !legacySourceSameFile(output.fileStat, held) || held.Size != 0 ||
		unix.Fstatat(output.parent, output.name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !legacySourceSameFile(held, named) {
		return errLegacySourceOutput
	}
	if n, writeErr := output.file.Write(body); writeErr != nil || n != len(body) || output.file.Sync() != nil {
		return errLegacySourceOutput
	}
	if unix.Fstat(int(output.file.Fd()), &held) != nil || !legacySourceSameFile(output.fileStat, held) || held.Size != int64(len(body)) ||
		unix.Fstatat(output.parent, output.name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !legacySourceSameFile(held, named) ||
		unix.Fsync(output.parent) != nil || !legacySourceParentUnchanged(output.parent, output.parentPath, output.parentStat) {
		return errLegacySourceOutput
	}
	return nil
}

func readLegacySourceTokenFile(path string) ([]byte, error) {
	parent, parentStat, err := legacySourcePrivateParent(path)
	if err != nil {
		return nil, errLegacySourceInput
	}
	defer unix.Close(parent)
	fd, err := unix.Openat2(parent, filepath.Base(path), &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		return nil, errLegacySourceInput
	}
	file := os.NewFile(uintptr(fd), "private-source-request")
	defer file.Close()
	var before, named, after unix.Stat_t
	if unix.Fstat(fd, &before) != nil || !legacySourcePrivateFile(before) ||
		unix.Fstatat(parent, filepath.Base(path), &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !legacySourceSameFile(before, named) ||
		!legacySourceParentUnchanged(parent, filepath.Dir(path), parentStat) {
		return nil, errLegacySourceInput
	}
	body, err := io.ReadAll(io.LimitReader(file, sourceRetirementFileLimit+1))
	if err != nil || len(body) > sourceRetirementFileLimit || unix.Fstat(fd, &after) != nil ||
		!legacySourceSameFile(before, after) || before.Size != after.Size || before.Mtim != after.Mtim || before.Ctim != after.Ctim ||
		unix.Fstatat(parent, filepath.Base(path), &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !legacySourceSameFile(after, named) ||
		!legacySourceParentUnchanged(parent, filepath.Dir(path), parentStat) {
		clear(body)
		return nil, errLegacySourceInput
	}
	return body, nil
}
