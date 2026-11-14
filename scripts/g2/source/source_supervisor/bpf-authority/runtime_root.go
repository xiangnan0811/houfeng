//go:build linux && amd64 && !cgo

package main

import (
	"fmt"
	"os"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	runtimeRootUninitialized = iota
	runtimeRootReady
	runtimeRootFailed
)

type procMountIdentity struct {
	dev, ino, mount uint64
}

// procDirectoryIdentity uses fdinfo from the original proc mount so a bind
// mount of the same inode is not mistaken for the original mount.
func procDirectoryIdentity(fd int) (procMountIdentity, error) {
	var result procMountIdentity
	var st unix.Stat_t
	if fd < 0 {
		return result, errGuardProtocol
	}
	if err := unix.Fstat(fd, &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR {
		if g2d1.scope != "" {
			g2d1Fail(g2d1.scope, err)
		}
		return result, errGuardProtocol
	}
	raw, err := readHeldProc(fmt.Sprintf("self/fdinfo/%d", fd), 65536)
	if err != nil {
		return result, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok && key == "mnt_id" {
			if result.mount != 0 {
				return procMountIdentity{}, errGuardProtocol
			}
			result.mount, err = strconv.ParseUint(strings.TrimSpace(value), 10, 64)
			if err != nil || result.mount == 0 {
				return procMountIdentity{}, errGuardProtocol
			}
		}
	}
	if result.mount == 0 || st.Ino == 0 {
		return procMountIdentity{}, errGuardProtocol
	}
	result.dev, result.ino = uint64(st.Dev), st.Ino
	return result, nil
}

func heldProcDirectoryIdentity(name string) (_ procMountIdentity, err error) {
	fd, err := unix.Openat(heldProcDirectory, name, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return procMountIdentity{}, err
	}
	defer func() {
		if err != nil && g2d1.scope != "" {
			g2d1Fail(g2d1.scope, err)
		}
		_ = unix.Close(fd)
	}()
	return procDirectoryIdentity(fd)
}

func sameProcNamespace(a, b unix.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino != 0 && a.Ino == b.Ino
}

func checkRuntimeRootNamespaces(prefix string, expected [3]unix.Stat_t) error {
	for i, name := range []string{"mnt", "user", "pid"} {
		var actual unix.Stat_t
		err := unix.Fstatat(heldProcDirectory, prefix+"ns/"+name, &actual, 0)
		if err != nil || !sameProcNamespace(actual, expected[i]) {
			g2d1Fail("THREAD_NS", err)
			return errGuardProtocol
		}
	}
	return nil
}

func runtimeRootThreads(expectedRoot, expectedCwd procMountIdentity) (_ []string, err error) {
	stage := "THREAD_ENUM"
	defer func() {
		if err != nil {
			g2d1Fail(stage, err)
		}
	}()
	fd, err := unix.Openat(heldProcDirectory, "self/task", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "held-proc-task")
	if file == nil {
		g2d1Fail(stage, nil)
		unix.Close(fd)
		return nil, errGuardProtocol
	}
	entries, err := file.ReadDir(4097)
	if err != nil {
		g2d1Fail(stage, err)
	}
	file.Close()
	if err != nil || len(entries) == 0 || len(entries) > 4096 {
		return nil, errGuardProtocol
	}
	var namespaces [3]unix.Stat_t
	for i, name := range []string{"mnt", "user", "pid"} {
		if e := unix.Fstatat(heldProcDirectory, "1/ns/"+name, &namespaces[i], 0); e != nil {
			g2d1Fail("PID1_NS", e)
			return nil, errGuardProtocol
		}
	}
	tids := make([]string, 0, len(entries))
	leaderSeen := false
	for _, entry := range entries {
		tid, e := strconv.ParseUint(entry.Name(), 10, 31)
		if e != nil || tid == 0 || strconv.FormatUint(tid, 10) != entry.Name() {
			return nil, errGuardProtocol
		}
		if int(tid) == os.Getpid() {
			leaderSeen = true
		}
		stage = "THREAD_ROOT"
		g2d1.scope = stage
		prefix := fmt.Sprintf("%d/task/%d/", os.Getpid(), tid)
		root, e := heldProcDirectoryIdentity(prefix + "root")
		if e != nil || root != expectedRoot {
			g2d1Fail(stage, e)
			return nil, errGuardProtocol
		}
		stage = "THREAD_CWD"
		g2d1.scope = stage
		cwd, e := heldProcDirectoryIdentity(prefix + "cwd")
		if e != nil || cwd != expectedCwd {
			g2d1Fail(stage, e)
			return nil, errGuardProtocol
		}
		stage = "THREAD_NS"
		if e := checkRuntimeRootNamespaces(prefix, namespaces); e != nil {
			return nil, e
		}
		stage = "THREAD_CAP"
		raw, e := readHeldProc(prefix+"status", 65536)
		if e != nil {
			return nil, e
		}
		found := false
		for _, line := range strings.Split(string(raw), "\n") {
			key, value, ok := strings.Cut(line, ":")
			if ok && key == "CapEff" {
				caps, e := strconv.ParseUint(strings.TrimSpace(value), 16, 64)
				if found || e != nil || caps&(uint64(1)<<unix.CAP_SYS_CHROOT) == 0 {
					return nil, fmt.Errorf("thread %d lacks effective CAP_SYS_CHROOT: %w", tid, errGuardProtocol)
				}
				found = true
			}
		}
		if !found {
			return nil, errGuardProtocol
		}
		stage = "THREAD_KCMP"
		// KCMP_FS proves the actual fs_struct is shared, not merely
		// that two independently mutable roots currently look alike.
		result, _, errno := unix.Syscall6(unix.SYS_KCMP, uintptr(os.Getpid()), uintptr(tid), 3 /* KCMP_FS */, 0, 0, 0)
		if errno != 0 {
			return nil, fmt.Errorf("KCMP_FS thread %d: %w", tid, errno)
		}
		if result != 0 {
			return nil, fmt.Errorf("thread %d has a different fs_struct: %w", tid, errGuardProtocol)
		}
		tids = append(tids, entry.Name())
		stage = "THREAD_ENUM"
	}
	if !leaderSeen {
		return nil, errGuardProtocol
	}
	sort.Slice(tids, func(i, j int) bool {
		a, _ := strconv.ParseUint(tids[i], 10, 32)
		b, _ := strconv.ParseUint(tids[j], 10, 32)
		return a < b
	})
	return tids, nil
}

func (b *authorityBroker) revalidateRuntimeRoot() (err error) {
	stage := "RECHECK_PIDFD"
	defer func() {
		if err != nil {
			g2d1Fail(stage, err)
		}
	}()
	if b == nil || b.runtimeRootState != runtimeRootReady || !pidfdSelects(b.runtimeRootPIDFD, 1) {
		return errGuardProtocol
	}
	stage = "RECHECK_HELD_ROOT"
	g2d1.scope = stage
	heldRoot, err := procDirectoryIdentity(b.runtimeRootFD)
	if err != nil || heldRoot != b.runtimeRootIdentity {
		g2d1Fail(stage, err)
		return errGuardProtocol
	}
	stage = "RECHECK_PID1_ROOT"
	g2d1.scope = stage
	root, err := heldProcDirectoryIdentity("1/root")
	if err != nil || root != b.runtimeRootIdentity {
		g2d1Fail(stage, err)
		return errGuardProtocol
	}
	stage = "RECHECK_SELF_ROOT"
	g2d1.scope = stage
	self, err := heldProcDirectoryIdentity("self/root")
	if err != nil || self != b.runtimeRootIdentity {
		g2d1Fail(stage, err)
		return errGuardProtocol
	}
	stage = "RECHECK_EXE"
	fd, err := unix.Openat(heldProcDirectory, "1/exe", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			g2d1Fail(stage, err)
		}
		_ = unix.Close(fd)
	}()
	var st, held unix.Stat_t
	if e := unix.Fstat(fd, &st); e != nil || !sameTrustedStat(st, b.runtimeRootExeStat) {
		g2d1Fail(stage, e)
		return errGuardProtocol
	}
	if e := unix.Fstat(b.runtimeRootExeFD, &held); e != nil || !sameTrustedStat(held, st) {
		g2d1Fail(stage, e)
		return errGuardProtocol
	}
	stage = "RECHECK_NATIVE"
	task, err := b.authority.guard.pinTask(1)
	if err != nil {
		return err
	}
	dropErr := b.authority.guard.dropTask(task.Cookie)
	if dropErr != nil || task.TID != 1 || task.TGID != 1 || task.Cookie != b.parentTask.Cookie || task.StartBoottimeNS != b.parentTask.StartBoottimeNS || !pidfdSelects(b.runtimeRootPIDFD, 1) {
		g2d1Fail(stage, dropErr)
		return errGuardProtocol
	}
	return nil
}

// initializeRuntimeRoot runs only on the locked broker main thread. The
// !cgo build and pinned Go runtime's newosproc CLONE_FS paths establish the
// origin of new threads; KCMP_FS proves every existing thread shares it.
func (b *authorityBroker) initializeRuntimeRoot() (err error) {
	stage := "ROOT_STATE"
	defer func() {
		if err != nil {
			g2d1Fail(stage, err)
		}
	}()
	if b == nil || b.runtimeRootState == runtimeRootFailed || unix.Gettid() != os.Getpid() {
		return errGuardProtocol
	}
	if b.runtimeRootState == runtimeRootReady {
		if err = b.revalidateRuntimeRoot(); err != nil {
			b.runtimeRootState = runtimeRootFailed
		}
		return err
	}
	stage = "ROOT_OPEN"
	b.runtimeRootState = runtimeRootFailed
	root, err := unix.Openat(heldProcDirectory, "1/root", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			g2d1Fail(stage, err)
			_ = unix.Close(root)
		}
	}()
	stage = "ROOT_ID"
	g2d1.scope = stage
	rootID, err := procDirectoryIdentity(root)
	if err != nil {
		return err
	}
	stage = "ROOT_REOPEN"
	g2d1.scope = stage
	second, err := heldProcDirectoryIdentity("1/root")
	if err != nil || second != rootID {
		g2d1Fail(stage, err)
		return errGuardProtocol
	}
	stage = "ROOT_MANIFEST"
	systemd := b.manifest.Executables["systemd"]
	if systemd.Package != "systemd" || systemd.Path != "/usr/lib/systemd/systemd" {
		return errGuardProtocol
	}
	stage = "ROOT_HASH"
	file, digest, err := openTrustedRegularAt(root, systemd.Path, 128<<20)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			g2d1Fail(stage, err)
		}
		_ = file.Close()
	}()
	if digest != systemd.SHA256 {
		return errGuardProtocol
	}
	stage = "EXE_OPEN"
	exe, err := unix.Openat(heldProcDirectory, "1/exe", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			g2d1Fail(stage, err)
			_ = unix.Close(exe)
		}
	}()
	stage = "EXE_HASH"
	exeDigest, exeStat, err := heldRegularDigest(exe, 128<<20)
	var named unix.Stat_t
	if err != nil || exeDigest != digest {
		g2d1Fail(stage, err)
		return errGuardProtocol
	}
	if statErr := unix.Fstat(int(file.Fd()), &named); statErr != nil || !sameTrustedStat(named, exeStat) {
		g2d1Fail("EXE_STAT", statErr)
		return errGuardProtocol
	}
	stage = "PIDFD_OPEN"
	pidfd, err := unix.PidfdOpen(1, 0)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			g2d1Fail(stage, err)
			_ = unix.Close(pidfd)
		}
	}()
	b.runtimeRootFD, b.runtimeRootPIDFD, b.runtimeRootExeFD = root, pidfd, exe
	b.runtimeRootIdentity, b.runtimeRootExeStat = rootID, exeStat
	// Validate PID1 before changing root; its root can already differ from ours.
	stage = "PID1_NATIVE"
	task, err := b.authority.guard.pinTask(1)
	if err != nil {
		return err
	}
	dropErr := b.authority.guard.dropTask(task.Cookie)
	if dropErr != nil || task.TID != 1 || task.TGID != 1 || task.Cookie != b.parentTask.Cookie || task.StartBoottimeNS != b.parentTask.StartBoottimeNS || !pidfdSelects(pidfd, 1) {
		g2d1Fail(stage, dropErr)
		return errGuardProtocol
	}
	stage = "SELF_ROOT"
	g2d1.scope = stage
	oldRoot, err := heldProcDirectoryIdentity("self/root")
	if err != nil {
		return err
	}
	stage = "SELF_CWD"
	g2d1.scope = stage
	oldCwd, err := heldProcDirectoryIdentity("self/cwd")
	if err != nil {
		return err
	}
	stage = "PRE_THREADS"
	g2d1Mark("PRE_THREADS")
	before, err := runtimeRootThreads(oldRoot, oldCwd)
	if err != nil {
		return err
	}
	stage = "FCHDIR"
	g2d1Mark("FCHDIR")
	if _, _, errno := syscall.AllThreadsSyscall(unix.SYS_FCHDIR, uintptr(root), 0, 0); errno != 0 {
		return errno
	}
	stage = "CHROOT"
	g2d1Mark("CHROOT")
	const dot = ".\x00"
	if _, _, errno := syscall.AllThreadsSyscall(unix.SYS_CHROOT, uintptr(unsafe.Pointer(unsafe.StringData(dot))), 0, 0); errno != 0 {
		return errno
	}
	stage = "CHDIR"
	g2d1Mark("CHDIR")
	const slash = "/\x00"
	if _, _, errno := syscall.AllThreadsSyscall(unix.SYS_CHDIR, uintptr(unsafe.Pointer(unsafe.StringData(slash))), 0, 0); errno != 0 {
		return errno
	}
	g2d1Mark("ALL_DONE")
	runtime.KeepAlive(dot)
	runtime.KeepAlive(slash)
	stage = "POST_THREADS"
	g2d1Mark("POST_THREADS")
	after, err := runtimeRootThreads(rootID, rootID)
	if err != nil {
		return err
	}
	stage = "THREAD_SET"
	if !slices.Equal(before, after) {
		return errGuardProtocol
	}
	stage = "ROOT_REVALIDATE"
	b.runtimeRootState = runtimeRootReady
	if err = b.revalidateRuntimeRoot(); err != nil {
		b.runtimeRootState = runtimeRootFailed
		return err
	}
	stage = "PID1_REGISTRY"
	if err = b.initializePID1Registry(); err != nil {
		b.runtimeRootState = runtimeRootFailed
		return err
	}
	stage = "ROOT_TRANSCRIPT"
	b.runtimeRootHandoffs++
	threadIDs := make([]any, 0, len(after))
	for _, tid := range after {
		id, parseErr := strconv.ParseUint(tid, 10, 32)
		if parseErr != nil {
			b.runtimeRootState = runtimeRootFailed
			return errGuardProtocol
		}
		threadIDs = append(threadIDs, jsonU64(id))
	}
	_, _, err = b.transcript.append("runtime_root", map[string]any{
		"broker_cookie":   jsonU64(b.authority.brokerCookie),
		"pid1_cookie":     jsonU64(task.Cookie),
		"pid1_start_ns":   jsonU64(task.StartBoottimeNS),
		"pid1_exe_sha256": exeDigest,
		"root_mount_id":   jsonU64(rootID.mount),
		"root_dev":        jsonU64(rootID.dev),
		"root_ino":        jsonU64(rootID.ino),
		"handoff_count":   jsonU64(b.runtimeRootHandoffs),
		"thread_ids":      threadIDs,
	})
	if err != nil {
		b.runtimeRootState = runtimeRootFailed
		return err
	}
	return nil
}
