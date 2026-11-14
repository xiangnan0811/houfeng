//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const earlyProbePattern = "HOUFENG-G2-EARLY-SECCOMP-ADDFD-SEND-V1\n"

func earlyProbeWait(fd int, events int16, deadline time.Time) error {
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return unix.ETIMEDOUT
		}
		p := []unix.PollFd{{Fd: int32(fd), Events: events}}
		n, err := unix.Poll(p, int((remaining+time.Millisecond-1)/time.Millisecond))
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return unix.ETIMEDOUT
		}
		if p[0].Revents&events != 0 {
			return nil
		}
		return errGuardProtocol
	}
}

func earlyProbeSend(fd int, message map[string]any, rights []int, deadline time.Time) error {
	raw, err := canonicalControl(message)
	if err != nil {
		return err
	}
	for {
		err = sendControl(fd, raw, rights)
		if err != unix.EAGAIN && err != unix.EWOULDBLOCK {
			return err
		}
		if err = earlyProbeWait(fd, unix.POLLOUT, deadline); err != nil {
			return err
		}
	}
}

func earlyProbeReceive(fd, expectedFDs int, deadline time.Time) (map[string]any, *unix.Ucred, []int, error) {
	if err := earlyProbeWait(fd, unix.POLLIN, deadline); err != nil {
		return nil, nil, nil, err
	}
	raw, cred, rights, err := receiveControl(fd, expectedFDs)
	if err != nil {
		return nil, nil, nil, err
	}
	value, err := parseCanonicalControl(raw)
	if err != nil {
		for _, received := range rights {
			_ = unix.Close(received)
		}
		return nil, nil, nil, err
	}
	return value, cred, rights, nil
}

func earlyProbeSameExecutable(pid int) error {
	self, err := unix.Open("/proc/self/exe", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(self)
	other, err := unix.Open(fmt.Sprintf("/proc/%d/exe", pid), unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(other)
	want, _, err := heldRegularDigest(self, 128<<20)
	if err != nil {
		return err
	}
	got, _, err := heldRegularDigest(other, 128<<20)
	if err != nil || want != got {
		return errGuardProtocol
	}
	return nil
}

// runEarlySeccompProbeChild has one fixed syscall and no caller-selected input.
// Its main thread receives the actual return value from ADDFD|SEND and verifies
// both the transferred file description and its complete read-only contents.
func runEarlySeccompProbeChild() error {
	runtime.LockOSThread()
	if os.Getppid() != 1 || unix.Gettid() != os.Getpid() || os.Geteuid() != 0 {
		return errGuardProtocol
	}
	if err := earlyProbeSameExecutable(1); err != nil {
		return err
	}
	const control = 3
	if err := unix.SetsockoptInt(control, unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
		return err
	}
	if err := unix.SetNonblock(control, true); err != nil {
		return err
	}
	deadline := time.Now().Add(5 * time.Second)
	boot, err := fixedBootID()
	if err != nil {
		return err
	}
	if err = unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return err
	}
	filter := []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 4},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: auditArchX8664, Jf: 3},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, K: uint32(unix.SYS_GETPGID), Jf: 1},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_USER_NOTIF},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
	}
	program := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
	result, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, unix.SECCOMP_FILTER_FLAG_NEW_LISTENER, uintptr(unsafe.Pointer(&program)))
	if errno != 0 {
		return errno
	}
	listener := int(result)
	defer unix.Close(listener)
	if err = earlyProbeSend(control, map[string]any{"version": json.Number("1"), "boot_id": boot, "kind": "early_seccomp_listener", "tid": json.Number(fmt.Sprint(unix.Gettid()))}, []int{listener}, deadline); err != nil {
		return err
	}
	frame, cred, _, err := earlyProbeReceive(control, 0, deadline)
	if err != nil || cred.Pid != 1 || cred.Uid != 0 || cred.Gid != 0 || !exactKeys(frame, "version", "boot_id", "kind", "dev", "ino", "mode", "size") || frame["version"] != json.Number("1") || frame["boot_id"] != boot || frame["kind"] != "early_seccomp_pattern" {
		return errGuardProtocol
	}
	dev, okDev := exactNumber(frame["dev"])
	ino, okIno := exactNumber(frame["ino"])
	mode, okMode := exactNumber(frame["mode"])
	size, okSize := exactNumber(frame["size"])
	if !okDev || !okIno || !okMode || !okSize || size != uint64(len(earlyProbePattern)) {
		return errGuardProtocol
	}
	result, _, errno = unix.RawSyscall(unix.SYS_GETPGID, 0, 0, 0)
	if errno != 0 || result > 0x7fffffff {
		return errGuardProtocol
	}
	transferred := int(result)
	defer unix.Close(transferred)
	var st unix.Stat_t
	if err = unix.Fstat(transferred, &st); err != nil || st.Dev != dev || st.Ino != ino || uint64(st.Mode) != mode || uint64(st.Size) != size {
		return errGuardProtocol
	}
	flags, err := unix.FcntlInt(uintptr(transferred), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_ACCMODE != unix.O_RDONLY {
		return errGuardProtocol
	}
	seals, err := unix.FcntlInt(uintptr(transferred), unix.F_GET_SEALS, 0)
	if err != nil || seals != unix.F_SEAL_SEAL|unix.F_SEAL_SHRINK|unix.F_SEAL_GROW|unix.F_SEAL_WRITE {
		return errGuardProtocol
	}
	var body [128]byte
	n, err := unix.Read(transferred, body[:])
	if err != nil || !bytes.Equal(body[:n], []byte(earlyProbePattern)) {
		return errGuardProtocol
	}
	n, err = unix.Read(transferred, body[:])
	if err != nil || n != 0 {
		return errGuardProtocol
	}
	return earlyProbeSend(control, map[string]any{"version": json.Number("1"), "boot_id": boot, "kind": "early_seccomp_verified", "fd": json.Number(fmt.Sprint(transferred))}, nil, deadline)
}

// proveEarlySeccomp runs before guard initialization, so the capability child is
// genuinely reaped before the no-producer census and first BPF publication.
func proveEarlySeccomp() (err error) {
	if os.Getpid() != 1 || unix.Gettid() != 1 {
		return errGuardProtocol
	}
	deadline := time.Now().Add(5 * time.Second)
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
	if err != nil {
		return err
	}
	defer unix.Close(pair[0])
	childSocket := os.NewFile(uintptr(pair[1]), "early-seccomp-child")
	defer childSocket.Close()
	if err = unix.SetsockoptInt(pair[0], unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
		return err
	}
	command := exec.Command("/g2-bpf-authority", "selftest", "early-seccomp-probe")
	command.ExtraFiles = []*os.File{childSocket}
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	command.Stdin = nil
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: unix.SIGKILL}
	if err = command.Start(); err != nil {
		return err
	}
	_ = childSocket.Close()
	pidfd, err := unix.PidfdOpen(command.Process.Pid, 0)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return err
	}
	defer unix.Close(pidfd)
	waited := false
	defer func() {
		if !waited {
			_ = unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0)
			waitErr := command.Wait()
			waited = true
			if err == nil {
				err = waitErr
			}
			if exitErr := earlyProbeWait(pidfd, unix.POLLIN, deadline); err == nil {
				err = exitErr
			}
		}
	}()
	if err = earlyProbeSameExecutable(command.Process.Pid); err != nil {
		return err
	}
	boot, err := fixedBootID()
	if err != nil {
		return err
	}
	frame, cred, rights, err := earlyProbeReceive(pair[0], 1, deadline)
	if err != nil {
		return err
	}
	listener := rights[0]
	defer unix.Close(listener)
	if cred.Pid != int32(command.Process.Pid) || cred.Uid != 0 || cred.Gid != 0 || !exactKeys(frame, "version", "boot_id", "kind", "tid") || frame["version"] != json.Number("1") || frame["boot_id"] != boot || frame["kind"] != "early_seccomp_listener" || frame["tid"] != json.Number(fmt.Sprint(command.Process.Pid)) {
		return errGuardProtocol
	}
	pattern, err := unix.MemfdCreate("g2-early-seccomp-pattern", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return err
	}
	defer unix.Close(pattern)
	if n, e := unix.Write(pattern, []byte(earlyProbePattern)); e != nil || n != len(earlyProbePattern) {
		return errGuardProtocol
	}
	if _, err = unix.FcntlInt(uintptr(pattern), unix.F_ADD_SEALS, unix.F_SEAL_SEAL|unix.F_SEAL_SHRINK|unix.F_SEAL_GROW|unix.F_SEAL_WRITE); err != nil {
		return err
	}
	readonly, err := unix.Open(fmt.Sprintf("/proc/self/fd/%d", pattern), unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(readonly)
	var st unix.Stat_t
	if err = unix.Fstat(readonly, &st); err != nil {
		return err
	}
	if err = earlyProbeSend(pair[0], map[string]any{"version": json.Number("1"), "boot_id": boot, "kind": "early_seccomp_pattern", "dev": json.Number(fmt.Sprint(st.Dev)), "ino": json.Number(fmt.Sprint(st.Ino)), "mode": json.Number(fmt.Sprint(st.Mode)), "size": json.Number(fmt.Sprint(st.Size))}, nil, deadline); err != nil {
		return err
	}
	if err = earlyProbeWait(listener, unix.POLLIN, deadline); err != nil {
		return err
	}
	var notification [80]byte
	if _, err = notificationIOCTL(listener, unix.SECCOMP_IOCTL_NOTIF_RECV, notification[:]); err != nil {
		return err
	}
	id := guardU64(notification[:], 0)
	if id == 0 || guardU32(notification[:], 8) != uint32(command.Process.Pid) || guardU32(notification[:], 12) != 0 || guardU32(notification[:], 16) != unix.SYS_GETPGID || guardU32(notification[:], 20) != auditArchX8664 || guardU64(notification[:], 32) != 0 {
		return errGuardProtocol
	}
	if err = notificationValid(listener, id); err != nil {
		return err
	}
	installed, err := injectNotificationFD(listener, id, readonly)
	if err != nil {
		return err
	}
	frame, cred, _, err = earlyProbeReceive(pair[0], 0, deadline)
	if err != nil || cred.Pid != int32(command.Process.Pid) || cred.Uid != 0 || cred.Gid != 0 || !exactKeys(frame, "version", "boot_id", "kind", "fd") || frame["version"] != json.Number("1") || frame["boot_id"] != boot || frame["kind"] != "early_seccomp_verified" || frame["fd"] != json.Number(fmt.Sprint(installed)) {
		return errGuardProtocol
	}
	if err = earlyProbeWait(pidfd, unix.POLLIN, deadline); err != nil {
		return err
	}
	err = command.Wait()
	waited = true
	if err != nil {
		return err
	}
	var extra [1]byte
	n, _, _, _, readErr := unix.Recvmsg(pair[0], extra[:], nil, unix.MSG_DONTWAIT)
	if readErr != nil || n != 0 {
		return errGuardProtocol
	}
	return nil
}
