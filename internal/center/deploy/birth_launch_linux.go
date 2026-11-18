//go:build linux

package deploy

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

	"houfeng/internal/center/recordbirth"
)

const (
	composeLaunchPublisherUID = 10002
	composeLaunchRuntimeGID   = 10001
)

type composeLaunchDirectory struct {
	fd     int
	name   string
	before unix.Stat_t
}

func composeLaunchDirectorySafe(st unix.Stat_t) bool {
	return st.Mode&unix.S_IFMT == unix.S_IFDIR && st.Nlink != 0 && st.Mode&0o011 != 0 &&
		(st.Uid == 0 || st.Uid == composeLaunchPublisherUID) && st.Mode&0o022 == 0
}

func sameComposeLaunchDirectory(a, b unix.Stat_t) bool {
	return composeLaunchDirectorySafe(b) && a.Dev == b.Dev && a.Ino == b.Ino &&
		a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid && a.Nlink == b.Nlink
}

func openComposeLaunchDirectories(path string) ([]composeLaunchDirectory, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" || strings.ContainsRune(path, '\x00') {
		return nil, errComposeBirthLaunch
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	directories := make([]composeLaunchDirectory, 0, len(parts)+1)
	closeOnError := func() {
		for _, directory := range directories {
			_ = unix.Close(directory.fd)
		}
	}
	root, err := unix.Openat2(unix.AT_FDCWD, "/", &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		return nil, errComposeBirthLaunch
	}
	directories = append(directories, composeLaunchDirectory{fd: root})
	if unix.Fstat(root, &directories[0].before) != nil || !composeLaunchDirectorySafe(directories[0].before) {
		closeOnError()
		return nil, errComposeBirthLaunch
	}
	for index, part := range parts {
		flags := uint64(unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW)
		if index == len(parts)-1 {
			flags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW
		}
		fd, err := unix.Openat2(directories[len(directories)-1].fd, part, &unix.OpenHow{
			Flags: flags, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS,
		})
		if err != nil {
			closeOnError()
			return nil, errComposeBirthLaunch
		}
		directories = append(directories, composeLaunchDirectory{fd: fd, name: part})
		last := &directories[len(directories)-1]
		if unix.Fstat(fd, &last.before) != nil || !composeLaunchDirectorySafe(last.before) {
			closeOnError()
			return nil, errComposeBirthLaunch
		}
	}
	return directories, nil
}

func composeLaunchDirectoriesUnchanged(directories []composeLaunchDirectory) bool {
	root, err := unix.Openat2(unix.AT_FDCWD, "/", &unix.OpenHow{
		Flags:   unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Resolve: unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		return false
	}
	defer unix.Close(root)
	var current unix.Stat_t
	if len(directories) == 0 || unix.Fstat(root, &current) != nil || !sameComposeLaunchDirectory(directories[0].before, current) {
		return false
	}
	for index, directory := range directories {
		if unix.Fstat(directory.fd, &current) != nil || !sameComposeLaunchDirectory(directory.before, current) {
			return false
		}
		if index != 0 {
			var named unix.Stat_t
			if unix.Fstatat(directories[index-1].fd, directory.name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil ||
				!sameComposeLaunchDirectory(directory.before, named) {
				return false
			}
		}
	}
	return true
}

func publishComposeBirthLaunchPair(directory string, pair composeBirthLaunchPair) error {
	if os.Getuid() != composeLaunchPublisherUID || os.Geteuid() != composeLaunchPublisherUID {
		return errComposeBirthLaunch
	}
	directories, err := openComposeLaunchDirectories(directory)
	if err != nil {
		return errComposeBirthLaunch
	}
	defer func() {
		for _, dir := range directories {
			_ = unix.Close(dir.fd)
		}
	}()
	if err := publishComposeBirthLaunchFile(directories, directory, composeCenterBirthLaunchFile, "center", pair.center, pair.centerBytes, pair.centerDigest); err != nil {
		return errComposeBirthLaunch
	}
	return publishComposeBirthLaunchFile(directories, directory, composeProcessorBirthLaunchFile, "content-processor", pair.processor, pair.processorBytes, pair.processorDigest)
}

// Publication never replaces a pre-existing target. A lost response can be
// replayed only after the public loader confirms the exact protected file.
func publishComposeBirthLaunchFile(directories []composeLaunchDirectory, directory, name, kind string, binding recordbirth.LaunchBinding, body []byte, digest [32]byte) error {
	parent := directories[len(directories)-1].fd
	validateExisting := func() error {
		value, gotDigest, err := recordbirth.ReadPublishedLaunchBinding(filepath.Join(directory, name), kind)
		if err != nil || value != binding || gotDigest != digest || unix.Fsync(parent) != nil || !composeLaunchDirectoriesUnchanged(directories) {
			return errComposeBirthLaunch
		}
		return nil
	}
	if !composeLaunchDirectoriesUnchanged(directories) {
		return errComposeBirthLaunch
	}
	var target unix.Stat_t
	if err := unix.Fstatat(parent, name, &target, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		return validateExisting()
	} else if !errors.Is(err, unix.ENOENT) {
		return errComposeBirthLaunch
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return errComposeBirthLaunch
	}
	temporaryName := ".birth-launch-" + hex.EncodeToString(nonce[:])
	fd, err := unix.Openat2(parent, temporaryName, &unix.OpenHow{
		Flags: unix.O_WRONLY | unix.O_CREAT | unix.O_EXCL | unix.O_CLOEXEC | unix.O_NOFOLLOW,
		Mode:  0o600, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS,
	})
	if err != nil {
		return errComposeBirthLaunch
	}
	file := os.NewFile(uintptr(fd), "birth launch temporary file")
	published := false
	defer func() {
		_ = file.Close()
		if !published {
			_ = unix.Unlinkat(parent, temporaryName, 0)
		}
	}()
	if unix.Fchown(fd, -1, composeLaunchRuntimeGID) != nil || unix.Fchmod(fd, 0o440) != nil {
		return errComposeBirthLaunch
	}
	if n, err := file.Write(body); err != nil || n != len(body) || file.Sync() != nil {
		return errComposeBirthLaunch
	}
	var before unix.Stat_t
	if unix.Fstat(fd, &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Mode&0o7777 != 0o440 ||
		before.Uid != composeLaunchPublisherUID || before.Gid != composeLaunchRuntimeGID || before.Nlink != 1 ||
		before.Size != int64(len(body)) || !composeLaunchDirectoriesUnchanged(directories) {
		return errComposeBirthLaunch
	}
	if err := unix.Renameat2(parent, temporaryName, parent, name, unix.RENAME_NOREPLACE); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return validateExisting()
		}
		return errComposeBirthLaunch
	}
	published = true
	var named unix.Stat_t
	if unix.Fstatat(parent, name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || named.Dev != before.Dev || named.Ino != before.Ino ||
		unix.Fsync(parent) != nil || !composeLaunchDirectoriesUnchanged(directories) {
		return errComposeBirthLaunch
	}
	return validateExisting()
}
