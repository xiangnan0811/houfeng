//go:build linux && amd64 && !cgo

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// The broker is admitted by the guard in the initial user namespace, as a
// direct child of early PID1. Recheck that its executing thread remains in
// PID1's namespace and still has the effective privilege used by bpf_capable
// before treating the program info's creator and translated length as visible.
func requireProgramInfoVisibility() error {
	if unix.Gettid() != os.Getpid() || os.Getppid() != 1 {
		return errGuardProtocol
	}
	var broker, init unix.Stat_t
	if unix.Stat("/proc/thread-self/ns/user", &broker) != nil || unix.Stat("/proc/1/ns/user", &init) != nil || broker.Dev != init.Dev || broker.Ino == 0 || broker.Ino != init.Ino {
		return errGuardProtocol
	}
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var caps [2]unix.CapUserData
	if err := unix.Capget(&header, &caps[0]); err != nil {
		return err
	}
	effective := uint64(caps[0].Effective) | uint64(caps[1].Effective)<<32
	if effective&(uint64(1)<<unix.CAP_BPF|uint64(1)<<unix.CAP_SYS_ADMIN) == 0 {
		return errGuardProtocol
	}
	return nil
}
