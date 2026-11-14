//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

func mountEarly(source, target, filesystem string, flags uintptr, magic int64, stage string) error {
	if err := unix.Mkdir(target, 0755); err != nil && err != unix.EEXIST {
		return err
	}
	err := unix.Mount(source, target, filesystem, flags, "")
	if err != nil && err != unix.EBUSY {
		return err
	}
	var st unix.Statfs_t
	if err = unix.Statfs(target, &st); err != nil || int64(st.Type) != magic {
		g2d1EarlyCause(stage, err)
		return errGuardProtocol
	}
	return nil
}

// prepareEarlyKernel runs only in the new guest's initramfs as PID 1. It
// verifies the held input bytes, copies the module ELF into a sealed 0400
// memfd, and loads it via finit_module(fd, "", 0).
const moduleSealsMask = unix.F_SEAL_WRITE | unix.F_SEAL_GROW | unix.F_SEAL_SHRINK | unix.F_SEAL_SEAL

func verifySealedModuleMemfd(fd int, expectedSHA string, expectedDev uint64, expectedIno uint64, expectedSize uint64) (uint64, uint64, uint64, error) {
	if fd < 0 || expectedSHA == "" {
		return 0, 0, 0, errGuardProtocol
	}
	seals, err := unix.FcntlInt(uintptr(fd), unix.F_GET_SEALS, 0)
	if err != nil || seals&moduleSealsMask != moduleSealsMask {
		g2d1EarlyCause(g2d1.scope, err)
		return 0, 0, 0, errGuardProtocol
	}
	digest, st, err := heldRegularDigest(fd, 128<<20)
	if err != nil || digest != expectedSHA || st.Uid != 0 || st.Gid != 0 || st.Mode&07777 != 0400 || st.Size <= 0 {
		g2d1EarlyCause(g2d1.scope, err)
		return 0, 0, 0, errGuardProtocol
	}
	var fs unix.Statfs_t
	if err := unix.Fstatfs(fd, &fs); err != nil || int64(fs.Type) != unix.TMPFS_MAGIC {
		g2d1EarlyCause(g2d1.scope, err)
		return 0, 0, 0, errGuardProtocol
	}
	dev, err := kernelDevice(uint64(st.Dev))
	if err != nil || st.Ino == 0 {
		return 0, 0, 0, errGuardProtocol
	}
	size := uint64(st.Size)
	if expectedIno != 0 && (dev != expectedDev || st.Ino != expectedIno || size != expectedSize) {
		return 0, 0, 0, errGuardProtocol
	}
	sealsAfter, err := unix.FcntlInt(uintptr(fd), unix.F_GET_SEALS, 0)
	if err != nil || sealsAfter&moduleSealsMask != moduleSealsMask {
		g2d1EarlyCause(g2d1.scope, err)
		return 0, 0, 0, errGuardProtocol
	}
	return dev, st.Ino, size, nil
}

func prepareEarlyKernel(audit **earlyAuditControl) (_ bootstrapManifest, _ string, _ *os.File, err error) {
	var empty bootstrapManifest
	stage := "EARLY_IDENTITY"
	defer func() {
		if err != nil && g2d1.role == 'E' {
			g2d1Fail(stage, err)
		}
	}()
	runtime.LockOSThread()
	if audit == nil || os.Getpid() != 1 || unix.Gettid() != 1 || os.Geteuid() != 0 {
		return empty, "", nil, errGuardProtocol
	}
	for _, m := range []struct {
		source, target, fs, stage string
		flags                     uintptr
		magic                     int64
	}{
		{"proc", "/proc", "proc", "MOUNT_PROC", unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, unix.PROC_SUPER_MAGIC},
		{"sysfs", "/sys", "sysfs", "MOUNT_SYSFS", unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, unix.SYSFS_MAGIC},
		{"devtmpfs", "/dev", "devtmpfs", "MOUNT_DEV", unix.MS_NOSUID, unix.TMPFS_MAGIC},
		{"securityfs", "/sys/kernel/security", "securityfs", "MOUNT_SECURITYFS", unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, unix.SECURITYFS_MAGIC},
		{"cgroup2", "/sys/fs/cgroup", "cgroup2", "MOUNT_CGROUP2", unix.MS_NOSUID | unix.MS_NODEV | unix.MS_NOEXEC, unix.CGROUP2_SUPER_MAGIC},
	} {
		stage = m.stage
		if err := mountEarly(m.source, m.target, m.fs, m.flags, m.magic, stage); err != nil {
			return empty, "", nil, err
		}
	}
	stage = "PROC_CUSTODY"
	if err := pinProcDirectory(); err != nil {
		return empty, "", nil, err
	}
	stage = "KERNEL_RELEASE"
	var uts unix.Utsname
	if err := unix.Uname(&uts); err != nil || unix.ByteSliceToString(uts.Release[:]) != "6.8.0-147-generic" {
		g2d1EarlyCause(stage, err)
		return empty, "", nil, errGuardProtocol
	}
	stage = "LSM_READ"
	lsmBytes, err := os.ReadFile("/sys/kernel/security/lsm")
	if err != nil || len(lsmBytes) == 0 || len(lsmBytes) > 1024 {
		g2d1EarlyCause(stage, err)
		return empty, "", nil, errGuardProtocol
	}
	stage = "LSM_PARSE"
	seen := make(map[string]bool)
	for _, name := range strings.Split(strings.TrimSpace(string(lsmBytes)), ",") {
		if name == "" || seen[name] {
			return empty, "", nil, errGuardProtocol
		}
		seen[name] = true
	}
	stage = "LSM_REQUIRED"
	for _, name := range []string{"capability", "landlock", "lockdown", "yama", "apparmor", "bpf"} {
		if !seen[name] {
			return empty, "", nil, fmt.Errorf("BPF_AUTH_UNAVAILABLE: required LSM missing")
		}
	}
	stage = "LSM_EXTRA"
	if len(seen) != 6 {
		return empty, "", nil, errGuardProtocol
	}
	stage = "BTF"
	btf, err := unix.Open("/sys/kernel/btf/vmlinux", unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return empty, "", nil, err
	}
	var magic [2]byte
	n, err := unix.Read(btf, magic[:])
	_ = unix.Close(btf)
	if err != nil || n != 2 || magic != [2]byte{0x9f, 0xeb} {
		g2d1EarlyCause(stage, err)
		return empty, "", nil, errGuardProtocol
	}
	stage = "SECCOMP_SIZES"
	var sizes [6]byte
	_, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_GET_NOTIF_SIZES, 0, uintptr(unsafe.Pointer(&sizes[0])))
	if errno != 0 || int(sizes[0])|int(sizes[1])<<8 < 80 || int(sizes[2])|int(sizes[3])<<8 < 24 || int(sizes[4])|int(sizes[5])<<8 < 64 {
		if errno != 0 {
			g2d1EarlyCause(stage, errno)
		}
		return empty, "", nil, fmt.Errorf("BPF_AUTH_UNAVAILABLE: seccomp notification ABI")
	}
	stage = "SECCOMP_ACTION"
	action := uint32(unix.SECCOMP_RET_USER_NOTIF)
	_, _, errno = unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_GET_ACTION_AVAIL, 0, uintptr(unsafe.Pointer(&action)))
	if errno != 0 {
		return empty, "", nil, errno
	}
	stage = "MANIFEST_OPEN"
	manifestFile, manifestSHA, err := openTrustedRegular("/g2-bootstrap.json", maxControlFrame)
	if err != nil {
		return empty, "", nil, err
	}
	stage = "MANIFEST_READ"
	raw, err := io.ReadAll(io.LimitReader(manifestFile, maxControlFrame+1))
	_ = manifestFile.Close()
	if err != nil || len(raw) == 0 || len(raw) > maxControlFrame {
		g2d1EarlyCause(stage, err)
		return empty, "", nil, errGuardProtocol
	}
	stage = "MANIFEST_PARSE"
	manifest, err := parseBootstrapManifest(raw)
	if err != nil {
		return empty, "", nil, err
	}
	stage = "PINNED_ARTIFACTS"
	for _, input := range []struct{ path, digest string }{
		{"/g2-early-init", manifest.BrokerSHA256}, {"/g2-bpf-authority", manifest.BrokerSHA256},
		{"/g2-policy.o", manifest.PolicySHA256},
	} {
		f, digest, e := openTrustedRegular(input.path, 128<<20)
		if e != nil {
			return empty, "", nil, e
		}
		_ = f.Close()
		if digest != input.digest {
			return empty, "", nil, errGuardProtocol
		}
	}
	stage = "MODULE_BYTES"
	module, hash, err := openTrustedRegular("/g2_bpf_guard.ko", 128<<20)
	if err != nil {
		return empty, "", nil, err
	}
	defer module.Close()
	if hash != manifest.GuardSHA256 {
		return empty, "", nil, errGuardProtocol
	}
	moduleBytes, err := readHeldRegular(int(module.Fd()), 128<<20)
	if err != nil || hashBytes(moduleBytes) != manifest.GuardSHA256 {
		g2d1EarlyCause(stage, err)
		return empty, "", nil, errGuardProtocol
	}
	stage = "MODULE_MEMFD"
	memfd, err := unix.MemfdCreate("g2_bpf_guard.ko", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return empty, "", nil, err
	}
	memFile := os.NewFile(uintptr(memfd), "g2_bpf_guard.ko")
	if memFile == nil {
		_ = unix.Close(memfd)
		return empty, "", nil, errGuardProtocol
	}
	keepMemfd := false
	defer func() {
		if !keepMemfd {
			_ = memFile.Close()
		}
	}()
	written := 0
	for written < len(moduleBytes) {
		nw, e := unix.Write(memfd, moduleBytes[written:])
		if e != nil {
			if e == unix.EINTR {
				continue
			}
			return empty, "", nil, e
		}
		if nw <= 0 {
			return empty, "", nil, errGuardProtocol
		}
		written += nw
	}
	if _, err = unix.Seek(memfd, 0, io.SeekStart); err != nil {
		return empty, "", nil, err
	}
	if err = unix.Fchmod(memfd, 0400); err != nil {
		return empty, "", nil, err
	}
	stage = "MODULE_SEALS"
	if _, err = unix.FcntlInt(uintptr(memfd), unix.F_ADD_SEALS, moduleSealsMask); err != nil {
		return empty, "", nil, err
	}
	g2d1.scope = stage
	devBefore, inoBefore, sizeBefore, err := verifySealedModuleMemfd(memfd, manifest.GuardSHA256, 0, 0, 0)
	if err != nil || sizeBefore != uint64(len(moduleBytes)) {
		return empty, "", nil, errGuardProtocol
	}
	stage = "OBSERVER_BINDING"
	if err = validateObserverBinding(memFile, manifest); err != nil {
		return empty, "", nil, err
	}
	stage = "SECCOMP_PROOF"
	if err = proveEarlySeccomp(); err != nil {
		return empty, "", nil, err
	}
	stage = "AUDIT_SETUP"
	*audit, err = setupEarlyAudit()
	if err != nil {
		return empty, "", nil, err
	}
	stage = "FINIT_MODULE"
	emptyArgs := []byte{0}
	_, _, errno = unix.Syscall(unix.SYS_FINIT_MODULE, uintptr(memfd), uintptr(unsafe.Pointer(&emptyArgs[0])), 0)
	if errno != 0 {
		(*audit).close()
		return empty, "", nil, errno
	}
	stage = "MODULE_POSTCHECK"
	g2d1.scope = stage
	if _, _, _, err = verifySealedModuleMemfd(memfd, manifest.GuardSHA256, devBefore, inoBefore, sizeBefore); err != nil {
		(*audit).close()
		return empty, "", nil, err
	}
	keepMemfd = true
	return manifest, manifestSHA, memFile, nil
}

// exactArgumentVector compares raw NUL-terminated procfs argv without ever
// persisting it into a transcript.
func exactArgumentVector(raw []byte, args []string) bool {
	if len(raw) == 0 || raw[len(raw)-1] != 0 {
		return false
	}
	parts := bytes.Split(raw[:len(raw)-1], []byte{0})
	if len(parts) != len(args) {
		return false
	}
	for i := range parts {
		if string(parts[i]) != args[i] {
			return false
		}
	}
	return true
}
