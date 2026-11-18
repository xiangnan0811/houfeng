//go:build linux && amd64 && !cgo

package main

import (
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// Each early helper holds the original procfs mount before run-init reclaims
// initramfs names. A retained proc directory remains usable after /proc moves;
// it neither reopens a replacement executable nor grants a new process role.
var heldProcDirectory = -1

func pinProcDirectory() error {
	if heldProcDirectory >= 0 {
		return errGuardProtocol
	}
	fd, err := unix.Open("/proc", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	var st unix.Stat_t
	var fs unix.Statfs_t
	if unix.Fstat(fd, &st) != nil || unix.Fstatfs(fd, &fs) != nil || int64(fs.Type) != unix.PROC_SUPER_MAGIC || st.Uid != 0 || st.Gid != 0 || st.Mode&0022 != 0 {
		_ = unix.Close(fd)
		return errGuardProtocol
	}
	heldProcDirectory = fd
	return nil
}

func readHeldProc(name string, maximum int64) ([]byte, error) {
	if heldProcDirectory < 0 || name == "" || strings.HasPrefix(name, "/") || maximum <= 0 {
		return nil, errGuardProtocol
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return nil, errGuardProtocol
		}
	}
	fd, err := unix.Openat(heldProcDirectory, name, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil || len(data) == 0 || int64(len(data)) > maximum {
		return nil, errGuardProtocol
	}
	return data, nil
}
