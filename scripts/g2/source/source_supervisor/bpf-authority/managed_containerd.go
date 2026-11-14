//go:build linux && amd64 && !cgo

package main

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	containerdLauncherDir   = "/opt/g2/containerd-bin"
	containerdLauncherPath  = containerdLauncherDir + "/containerd"
	managedContainerdConfig = "/run/g2-docker/containerd/containerd.toml"
	baseGuestPath           = "/usr/sbin:/usr/bin:/sbin:/bin"
	managedDockerPath       = containerdLauncherDir + ":" + baseGuestPath
)

func replaceExactPath(environment []string, expected, replacement string) error {
	index := -1
	for i, value := range environment {
		if strings.HasPrefix(value, "PATH=") {
			if index >= 0 || value != "PATH="+expected {
				return errGuardProtocol
			}
			index = i
		}
	}
	if index < 0 {
		return errGuardProtocol
	}
	environment[index] = "PATH=" + replacement
	return nil
}

func managedContainerdPreflight() error {
	fd, err := runtimeDirectory(containerdLauncherDir, false)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 || st.Gid != 0 || st.Mode&07777 != 0500 {
		return errGuardProtocol
	}
	streamFD, err := unix.Openat2(fd, ".", &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return err
	}
	dir := os.NewFile(uintptr(streamFD), containerdLauncherDir)
	if dir == nil {
		_ = unix.Close(streamFD)
		return errGuardProtocol
	}
	defer dir.Close()
	var streamStat unix.Stat_t
	if err := unix.Fstat(streamFD, &streamStat); err != nil {
		return err
	}
	if !sameTrustedStat(st, streamStat) {
		return errGuardProtocol
	}
	entries, err := dir.ReadDir(2)
	if err != nil || len(entries) != 1 || entries[0].Name() != "containerd" || entries[0].Type()&os.ModeSymlink != 0 || entries[0].IsDir() {
		return errGuardProtocol
	}
	file, _, err := openTrustedRegular(containerdLauncherPath, 128<<20)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := unix.Fstat(int(file.Fd()), &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != 0 || st.Gid != 0 || st.Mode&07777 != 0500 || st.Nlink != 1 {
		return errGuardProtocol
	}
	for _, name := range []string{"/run/g2-docker", "/run/containerd/containerd.sock"} {
		var item unix.Stat_t
		err := unix.Lstat(name, &item)
		if err == nil {
			return errGuardProtocol
		}
		if err != unix.ENOENT {
			return err
		}
	}
	return nil
}

func managedContainerdArgsValid(args []string) bool {
	return len(args) == 3 && args[0] == containerdLauncherPath && args[1] == "--config" && args[2] == managedContainerdConfig
}

func runManagedContainerdLauncher() error {
	runtime.LockOSThread()
	if !managedContainerdArgsValid(os.Args) || os.Geteuid() != 0 || os.Getegid() != 0 || unix.Gettid() != os.Getpid() {
		return errGuardProtocol
	}
	environment := os.Environ()
	if err := replaceExactPath(environment, managedDockerPath, baseGuestPath); err != nil {
		return err
	}
	program, _, err := openTrustedRegular("/usr/bin/containerd", 128<<20)
	if err != nil {
		return err
	}
	defer program.Close()
	unix.Umask(0022)
	if unix.Umask(0022) != 0022 {
		return errGuardProtocol
	}
	execPath := "/proc/self/fd/" + strconv.Itoa(int(program.Fd()))
	return syscall.Exec(execPath, []string{"/usr/bin/containerd", "--config", managedContainerdConfig}, environment)
}
