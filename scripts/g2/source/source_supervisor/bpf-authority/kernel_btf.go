//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"io"
	"os"

	"github.com/cilium/ebpf/btf"
	"golang.org/x/sys/unix"
)

const pinnedKernelBTFBytes = 6103569
const pinnedKernelBTFSHA256 = "8c10e7e174e89c92f1d2708f12bb5268dc381e57e867397534c4e145348622e0"

// readPinnedKernelBTF binds relocations to the actual running kernel's vmlinux
// BTF and the bytes independently extracted from the pinned kernel image.
// No module BTF, host headers, or alternate kernel source is consulted.
func readPinnedKernelBTF() (*btf.Spec, error) {
	fd, err := unix.Open("/sys/kernel/btf/vmlinux", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "/sys/kernel/btf/vmlinux")
	if file == nil {
		_ = unix.Close(fd)
		return nil, errGuardProtocol
	}
	defer file.Close()
	var before, after, named unix.Stat_t
	var fs unix.Statfs_t
	if unix.Fstat(fd, &before) != nil || unix.Fstatfs(fd, &fs) != nil || int64(fs.Type) != unix.SYSFS_MAGIC || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Uid != 0 || before.Gid != 0 || before.Mode&0022 != 0 {
		return nil, errGuardProtocol
	}
	raw, err := io.ReadAll(io.LimitReader(file, pinnedKernelBTFBytes+1))
	if err != nil || len(raw) != pinnedKernelBTFBytes || hashBytes(raw) != pinnedKernelBTFSHA256 {
		return nil, errGuardProtocol
	}
	if unix.Fstat(fd, &after) != nil || unix.Lstat("/sys/kernel/btf/vmlinux", &named) != nil || !sameTrustedStat(before, after) || !sameTrustedStat(before, named) {
		return nil, errGuardProtocol
	}
	return btf.LoadSpecFromReader(bytes.NewReader(raw))
}
