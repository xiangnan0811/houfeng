//go:build linux

package deploy

import (
	"archive/tar"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// ordinaryCopyImageBinary copies only one of the three fixed executable paths
// from a controller-verified, stopped image container. The surrounding intent
// must pin the image, container provenance, installed digest and directory.
func ordinaryCopyImageBinary(ctx context.Context, containerID, name, directory string) ([32]byte, error) {
	temp, err := ordinaryLaunchTempName()
	if err != nil {
		return [32]byte{}, errOrdinaryUpgrade
	}
	return ordinaryCopyImageBinaryWithTemp(ctx, containerID, name, directory, temp)
}

// The caller records temp durably before invoking this variant and may remove
// that exact owned temporary after a crash; the permanent target is never reset.
func ordinaryCopyImageBinaryWithTemp(ctx context.Context, containerID, name, directory, temp string) ([32]byte, error) {
	var zero [32]byte
	if ctx == nil || !fenceID.MatchString(containerID) {
		return zero, errOrdinaryUpgrade
	}
	const prefix = ".ordinary-launch-"
	if !strings.HasPrefix(temp, prefix) || len(temp) != len(prefix)+32 || !fenceID.MatchString(temp[len(prefix):]+strings.Repeat("0", 32)) {
		return zero, errOrdinaryUpgrade
	}
	switch name {
	case "houfeng-record-platform-admin", "houfeng-center", "houfeng-content-processor":
	default:
		return zero, errOrdinaryUpgrade
	}
	dirs, err := openComposeLaunchDirectories(directory)
	if err != nil {
		return zero, errOrdinaryUpgrade
	}
	defer func() {
		for _, dir := range dirs {
			_ = unix.Close(dir.fd)
		}
	}()
	parent := dirs[len(dirs)-1].fd
	stat := dirs[len(dirs)-1].before
	if stat.Uid != 0 || stat.Gid != composeLaunchRuntimeGID || stat.Mode&0o7777 != 0o750 {
		return zero, errOrdinaryUpgrade
	}
	fd, err := unix.Openat2(parent, temp, &unix.OpenHow{Flags: unix.O_RDWR | unix.O_CREAT | unix.O_EXCL | unix.O_CLOEXEC | unix.O_NOFOLLOW, Mode: 0o600, Resolve: maintenanceGuardResolve})
	if err != nil {
		return zero, errOrdinaryUpgrade
	}
	file := os.NewFile(uintptr(fd), "ordinary extracted executable")
	defer func() { _ = file.Close(); _ = unix.Unlinkat(parent, temp, 0) }()
	if unix.Fchown(fd, 0, composeLaunchRuntimeGID) != nil || unix.Fchmod(fd, 0o550) != nil {
		return zero, errOrdinaryUpgrade
	}
	command := exec.CommandContext(ctx, "/usr/bin/docker", "--host", "unix:///var/run/docker.sock", "cp", containerID+":/usr/local/bin/"+name, "-")
	command.Env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent", "DOCKER_CONFIG=/nonexistent"}
	var stderr fenceLimitedBuffer
	command.Stderr = &stderr
	output, err := command.StdoutPipe()
	if err != nil || command.Start() != nil {
		return zero, errOrdinaryUpgrade
	}
	archive := tar.NewReader(output)
	header, readErr := archive.Next()
	if readErr == nil && (header.Name != name || header.Typeflag != tar.TypeReg || header.Linkname != "" || header.Size <= 0 || header.Size > 256<<20 || header.Mode&0o6000 != 0 || len(header.PAXRecords) != 0) {
		readErr = errOrdinaryUpgrade
	}
	if readErr == nil {
		var copied int64
		copied, readErr = io.CopyN(file, archive, header.Size)
		if readErr == nil && copied != header.Size {
			readErr = errOrdinaryUpgrade
		}
	}
	if readErr == nil {
		_, readErr = archive.Next()
		if readErr == io.EOF {
			readErr = nil
		} else if readErr == nil {
			readErr = errOrdinaryUpgrade
		}
	}
	_ = output.Close()
	waitErr := command.Wait()
	if readErr != nil || waitErr != nil || stderr.overflow || file.Sync() != nil {
		return zero, errOrdinaryUpgrade
	}
	digest, err := ordinaryTargetExecutable(file)
	if err != nil || !composeLaunchDirectoriesUnchanged(dirs) {
		return zero, errOrdinaryUpgrade
	}
	var before, named unix.Stat_t
	if unix.Fstat(fd, &before) != nil || unix.Renameat2(parent, temp, parent, name, unix.RENAME_NOREPLACE) != nil || unix.Fstatat(parent, name, &named, unix.AT_SYMLINK_NOFOLLOW) != nil || named.Dev != before.Dev || named.Ino != before.Ino || named.Mode != before.Mode || named.Uid != before.Uid || named.Gid != before.Gid || named.Nlink != 1 || named.Size != before.Size || unix.Fsync(parent) != nil || !composeLaunchDirectoriesUnchanged(dirs) {
		return zero, errOrdinaryUpgrade
	}
	return digest, nil
}

func ordinaryTargetPaths(directory string) composeExecutablePaths {
	return composeExecutablePaths{center: filepath.Join(directory, "houfeng-center"), processor: filepath.Join(directory, "houfeng-content-processor")}
}
