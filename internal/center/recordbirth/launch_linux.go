//go:build linux

package recordbirth

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	launchPublisherUID = 10002
	launchRuntimeGID   = 10001
)

type launchDirectory struct {
	fd     int
	name   string
	before unix.Stat_t
}

func openExecutableForDigest(path string) (*os.File, error) {
	// The procfs /proc/self/exe link is intentional: it identifies the image
	// executing this process, including an image whose original name changed.
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}

func executableMetadataUnchanged(a, b os.FileInfo) bool {
	before, beforeOK := a.Sys().(*syscall.Stat_t)
	after, afterOK := b.Sys().(*syscall.Stat_t)
	return beforeOK && afterOK && before.Uid == after.Uid && before.Gid == after.Gid &&
		before.Nlink == after.Nlink && before.Ctim == after.Ctim
}

func launchDirectorySafe(stat unix.Stat_t) bool {
	// Reject the publisher's private 0700 directories. The kernel checks
	// whether UID10001 can actually traverse other group/ACL arrangements.
	return stat.Mode&unix.S_IFMT == unix.S_IFDIR && stat.Nlink != 0 && stat.Mode&0o011 != 0 &&
		(stat.Uid == 0 || stat.Uid == launchPublisherUID) && stat.Mode&0o022 == 0
}

func sameLaunchDirectory(a, b unix.Stat_t) bool {
	return launchDirectorySafe(b) && a.Dev == b.Dev && a.Ino == b.Ino &&
		a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink
}

func sameLaunchFile(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode &&
		a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink &&
		a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}

// Keep the parent descriptors open, so their identities and the entries that
// connect them can be checked again after both the binding and image are read.
func openLaunchDirectories(path string) ([]launchDirectory, string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" || strings.ContainsRune(path, '\x00') {
		return nil, "", fmt.Errorf("%w: invalid launch path", ErrInvalid)
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	directories := make([]launchDirectory, 0, len(parts))
	closeOnError := func() {
		for _, dir := range directories {
			_ = unix.Close(dir.fd)
		}
	}
	root, err := unix.Openat2(unix.AT_FDCWD, "/", &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		return nil, "", fmt.Errorf("%w: open launch root", ErrInvalid)
	}
	directories = append(directories, launchDirectory{fd: root})
	if unix.Fstat(root, &directories[0].before) != nil || !launchDirectorySafe(directories[0].before) {
		closeOnError()
		return nil, "", fmt.Errorf("%w: unsafe launch root", ErrInvalid)
	}
	for _, part := range parts[:len(parts)-1] {
		parent := directories[len(directories)-1].fd
		fd, err := unix.Openat2(parent, part, &unix.OpenHow{
			Flags:   unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
			Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS,
		})
		if err != nil {
			closeOnError()
			return nil, "", fmt.Errorf("%w: open launch directory", ErrInvalid)
		}
		directories = append(directories, launchDirectory{fd: fd, name: part})
		last := &directories[len(directories)-1]
		if unix.Fstat(fd, &last.before) != nil || !launchDirectorySafe(last.before) {
			closeOnError()
			return nil, "", fmt.Errorf("%w: unsafe launch directory", ErrInvalid)
		}
	}
	return directories, parts[len(parts)-1], nil
}

func launchDirectoriesUnchanged(directories []launchDirectory) bool {
	root, err := unix.Openat2(unix.AT_FDCWD, "/", &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		return false
	}
	defer unix.Close(root)
	var current unix.Stat_t
	if unix.Fstat(root, &current) != nil || !sameLaunchDirectory(directories[0].before, current) {
		return false
	}
	for index, dir := range directories {
		if unix.Fstat(dir.fd, &current) != nil || !sameLaunchDirectory(dir.before, current) {
			return false
		}
		if index != 0 {
			var named unix.Stat_t
			if unix.Fstatat(directories[index-1].fd, dir.name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil ||
				!sameLaunchDirectory(dir.before, named) {
				return false
			}
		}
	}
	return true
}

func launchFileUnchanged(fileFD, parentFD int, name string, before unix.Stat_t, directories []launchDirectory) bool {
	var opened, named unix.Stat_t
	return unix.Fstat(fileFD, &opened) == nil && unix.Fstatat(parentFD, name, &named, unix.AT_SYMLINK_NOFOLLOW) == nil &&
		sameLaunchFile(before, opened) && sameLaunchFile(before, named) && launchDirectoriesUnchanged(directories)
}

// LoadRuntimeLaunchBinding reads the public launch file and compares its
// executable digest to the image actually running this process.
func LoadRuntimeLaunchBinding(path, processKind string) (LaunchBinding, [32]byte, error) {
	return readProtectedLaunchBinding(path, processKind, true)
}

// ReadPublishedLaunchBinding reads the public launch file without measuring
// this process. A trusted initializer must independently measure the fixed
// executable that the published binding is intended to authorize.
func ReadPublishedLaunchBinding(path, processKind string) (LaunchBinding, [32]byte, error) {
	return readProtectedLaunchBinding(path, processKind, false)
}

// Both callers use the same protected path and file checks. Keep the
// descriptors open throughout runtime executable measurement, then recheck.
func readProtectedLaunchBinding(path, processKind string, measureRunning bool) (LaunchBinding, [32]byte, error) {
	if processKind != "center" && processKind != "content-processor" {
		return LaunchBinding{}, [32]byte{}, fmt.Errorf("%w: invalid launch process kind", ErrInvalid)
	}
	directories, name, err := openLaunchDirectories(path)
	if err != nil {
		return LaunchBinding{}, [32]byte{}, err
	}
	defer func() {
		for _, dir := range directories {
			_ = unix.Close(dir.fd)
		}
	}()
	parent := directories[len(directories)-1].fd
	fd, err := unix.Openat2(parent, name, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		return LaunchBinding{}, [32]byte{}, fmt.Errorf("%w: open launch file", ErrInvalid)
	}
	file := os.NewFile(uintptr(fd), "public launch binding")
	defer file.Close()
	var before unix.Stat_t
	if unix.Fstat(fd, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG ||
		before.Mode&0o7777 != 0o440 || before.Uid != launchPublisherUID || before.Gid != launchRuntimeGID ||
		before.Nlink != 1 || before.Size <= 0 || before.Size > maxLaunchBindingBytes {
		return LaunchBinding{}, [32]byte{}, fmt.Errorf("%w: unsafe launch file", ErrInvalid)
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxLaunchBindingBytes+1))
	if err != nil || len(raw) != int(before.Size) || !launchFileUnchanged(fd, parent, name, before, directories) {
		return LaunchBinding{}, [32]byte{}, fmt.Errorf("%w: launch file changed during read", ErrInvalid)
	}
	binding, err := DecodeLaunchBinding(raw)
	if err != nil || binding.ProcessKind != processKind {
		return LaunchBinding{}, [32]byte{}, fmt.Errorf("%w: launch process binding mismatch", ErrInvalid)
	}
	if measureRunning {
		binaryDigest, err := DigestExecutable("/proc/self/exe")
		if err != nil || binaryDigest != binding.BinaryDigest {
			return LaunchBinding{}, [32]byte{}, fmt.Errorf("%w: launch executable mismatch", ErrInvalid)
		}
	}
	if !launchFileUnchanged(fd, parent, name, before, directories) {
		return LaunchBinding{}, [32]byte{}, fmt.Errorf("%w: launch file changed during verification", ErrInvalid)
	}
	return binding, sha256.Sum256(raw), nil
}
