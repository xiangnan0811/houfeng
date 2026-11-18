//go:build linux && amd64 && !cgo

package main

import (
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// confineAuditReader is entered before READY and before opening the audit
// query socket. AllThreadsSyscall changes every Go M, including idle threads;
// newly created Ms inherit the resulting credentials and TSYNC filter.
func confineAuditReader() error {
	lastRaw, err := os.ReadFile("/proc/sys/kernel/cap_last_cap")
	if err != nil || len(lastRaw) > 32 {
		return errGuardProtocol
	}
	last, err := strconv.ParseUint(strings.TrimSpace(string(lastRaw)), 10, 32)
	if err != nil || last < unix.CAP_AUDIT_CONTROL || last > 63 {
		return errGuardProtocol
	}
	all := func(trap, a, b, c uintptr) error {
		_, _, e := syscall.AllThreadsSyscall(trap, a, b, c)
		if e != 0 {
			return e
		}
		return nil
	}
	for cap := uint64(0); cap <= last; cap++ {
		if cap != unix.CAP_AUDIT_CONTROL {
			if err = all(unix.SYS_PRCTL, unix.PR_CAPBSET_DROP, uintptr(cap), 0); err != nil {
				return err
			}
		}
	}
	if err = all(unix.SYS_PRCTL, unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0); err != nil {
		return err
	}
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	caps := [2]unix.CapUserData{}
	caps[0].Effective = 1 << unix.CAP_AUDIT_CONTROL
	caps[0].Permitted = 1 << unix.CAP_AUDIT_CONTROL
	if err = all(unix.SYS_CAPSET, uintptr(unsafe.Pointer(&header)), uintptr(unsafe.Pointer(&caps[0])), 0); err != nil {
		return err
	}
	if err = all(unix.SYS_PRCTL, unix.PR_SET_NO_NEW_PRIVS, 1, 0); err != nil {
		return err
	}
	if err = auditReaderThreads(false); err != nil {
		return err
	}
	if err = auditReaderFilter(); err != nil {
		return err
	}
	return auditReaderThreads(true)
}

func auditReaderThreads(filtered bool) error {
	entries, err := os.ReadDir("/proc/self/task")
	if err != nil || len(entries) == 0 || len(entries) > 4096 {
		return errGuardProtocol
	}
	for _, entry := range entries {
		if _, err = strconv.ParseUint(entry.Name(), 10, 32); err != nil {
			return errGuardProtocol
		}
		f, e := os.Open("/proc/self/task/" + entry.Name() + "/status")
		if e != nil {
			return e
		}
		raw, e := io.ReadAll(io.LimitReader(f, 65537))
		_ = f.Close()
		if e != nil || len(raw) > 65536 {
			return errGuardProtocol
		}
		values := map[string]string{}
		for _, line := range strings.Split(string(raw), "\n") {
			key, val, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			switch key {
			case "Uid", "Gid", "CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb", "NoNewPrivs", "Seccomp":
				if _, exists := values[key]; exists {
					return errGuardProtocol
				}
				values[key] = strings.TrimSpace(val)
			}
		}
		if len(values) != 9 || values["NoNewPrivs"] != "1" || strings.Join(strings.Fields(values["Uid"]), " ") != "0 0 0 0" || strings.Join(strings.Fields(values["Gid"]), " ") != "0 0 0 0" {
			return errGuardProtocol
		}
		for _, key := range []string{"CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb"} {
			value, e := strconv.ParseUint(values[key], 16, 64)
			if e != nil {
				return errGuardProtocol
			}
			expected := uint64(0)
			if key == "CapPrm" || key == "CapEff" || key == "CapBnd" {
				expected = uint64(1) << unix.CAP_AUDIT_CONTROL
			}
			if value != expected {
				return errGuardProtocol
			}
		}
		if filtered && values["Seccomp"] != "2" {
			return errGuardProtocol
		}
	}
	return nil
}

func auditReaderFilter() error {
	const load = unix.BPF_LD | unix.BPF_W | unix.BPF_ABS
	const eq = unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K
	const ret = unix.BPF_RET | unix.BPF_K
	const deny = unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)
	ins := []unix.SockFilter{
		{Code: load, K: 4}, {Code: eq, K: auditArchX8664, Jt: 1}, {Code: ret, K: unix.SECCOMP_RET_KILL_PROCESS},
		{Code: load, K: 0}, {Code: unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K, K: x32SyscallBit, Jf: 1}, {Code: ret, K: unix.SECCOMP_RET_KILL_PROCESS},
	}
	for _, nr := range []uint32{unix.SYS_EXECVE, unix.SYS_EXECVEAT, unix.SYS_BPF, unix.SYS_INIT_MODULE, unix.SYS_FINIT_MODULE, unix.SYS_DELETE_MODULE, unix.SYS_UNSHARE, unix.SYS_SETNS, unix.SYS_FORK, unix.SYS_VFORK, unix.SYS_CLONE3, unix.SYS_SETUID, unix.SYS_SETGID, unix.SYS_SETREUID, unix.SYS_SETREGID, unix.SYS_SETRESUID, unix.SYS_SETRESGID, unix.SYS_SETFSUID, unix.SYS_SETFSGID, unix.SYS_SETGROUPS, unix.SYS_CAPSET, unix.SYS_PRCTL, unix.SYS_PTRACE} {
		ins = append(ins, unix.SockFilter{Code: eq, K: nr, Jf: 1}, unix.SockFilter{Code: ret, K: deny})
	}
	// Go 1.26.2's linux/amd64 runtime uses cloneFlags=0x50f00 and adds
	// CLONE_SETTLS (0x80000) for normal newosproc calls. Check both halves
	// and accept only those exact thread flags, never process or namespace flags.
	ins = append(ins, unix.SockFilter{Code: eq, K: unix.SYS_CLONE, Jf: 6}, unix.SockFilter{Code: load, K: 20}, unix.SockFilter{Code: eq, K: 0, Jf: 3}, unix.SockFilter{Code: load, K: 16}, unix.SockFilter{Code: eq, K: 0x50f00, Jt: 2}, unix.SockFilter{Code: eq, K: 0xd0f00, Jt: 1}, unix.SockFilter{Code: ret, K: deny}, unix.SockFilter{Code: ret, K: unix.SECCOMP_RET_ALLOW})
	prog := unix.SockFprog{Len: uint16(len(ins)), Filter: &ins[0]}
	result, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, unix.SECCOMP_FILTER_FLAG_TSYNC, uintptr(unsafe.Pointer(&prog)))
	if errno != 0 {
		return errno
	}
	if result != 0 {
		return errGuardProtocol
	}
	return nil
}
