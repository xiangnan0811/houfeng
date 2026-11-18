//go:build linux && amd64 && !cgo

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

func taskCapabilityRows(pid int) ([]map[string]string, error) {
	entries, err := os.ReadDir(fmt.Sprintf("/proc/%d/task", pid))
	if err != nil || len(entries) == 0 || len(entries) > 4096 {
		return nil, errGuardProtocol
	}
	rows := make([]map[string]string, 0, len(entries))
	for _, entry := range entries {
		tid, err := strconv.ParseUint(entry.Name(), 10, 32)
		if err != nil || tid == 0 {
			return nil, errGuardProtocol
		}
		raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%d/status", pid, tid))
		if err != nil || len(raw) == 0 || len(raw) > 65536 {
			return nil, errGuardProtocol
		}
		row := map[string]string{}
		for _, line := range strings.Split(string(raw), "\n") {
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			switch key {
			case "Uid", "Gid", "CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb", "NoNewPrivs", "Seccomp":
				if _, exists := row[key]; exists {
					return nil, errGuardProtocol
				}
				row[key] = strings.TrimSpace(value)
			}
		}
		if len(row) != 9 {
			return nil, errGuardProtocol
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// dropBrokerAuditControl uses Go's all-M barrier before the first BPF syscall.
// It preserves the broker's other bootstrap capabilities and makes both the
// present threads and their future runtime threads unable to query or configure
// audit directly.
func dropBrokerAuditControl() error {
	before, err := taskCapabilityRows(os.Getpid())
	if err != nil {
		return err
	}
	for _, row := range before {
		for _, name := range []string{"CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb"} {
			if row[name] != before[0][name] {
				return errGuardProtocol
			}
		}
	}
	all := func(trap, a, b, c uintptr) error {
		_, _, errno := syscall.AllThreadsSyscall(trap, a, b, c)
		if errno != 0 {
			return errno
		}
		return nil
	}
	if err = all(unix.SYS_PRCTL, unix.PR_CAPBSET_DROP, unix.CAP_AUDIT_CONTROL, 0); err != nil {
		return err
	}
	if err = all(unix.SYS_PRCTL, unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_LOWER, unix.CAP_AUDIT_CONTROL); err != nil {
		return err
	}
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	caps := [2]unix.CapUserData{}
	if err = unix.Capget(&header, &caps[0]); err != nil {
		return err
	}
	caps[0].Effective &^= 1 << unix.CAP_AUDIT_CONTROL
	caps[0].Permitted &^= 1 << unix.CAP_AUDIT_CONTROL
	caps[0].Inheritable &^= 1 << unix.CAP_AUDIT_CONTROL
	if err = all(unix.SYS_CAPSET, uintptr(unsafe.Pointer(&header)), uintptr(unsafe.Pointer(&caps[0])), 0); err != nil {
		return err
	}
	rows, err := taskCapabilityRows(os.Getpid())
	if err != nil {
		return err
	}
	for _, row := range rows {
		for _, name := range []string{"CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb"} {
			value, err := strconv.ParseUint(row[name], 16, 64)
			if err != nil || value&(uint64(1)<<unix.CAP_AUDIT_CONTROL) != 0 {
				return errGuardProtocol
			}
		}
	}
	return nil
}

func verifyAuditReaderConfinement(pid int) error {
	rows, err := taskCapabilityRows(pid)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row["NoNewPrivs"] != "1" || row["Seccomp"] != "2" || strings.Join(strings.Fields(row["Uid"]), " ") != "0 0 0 0" || strings.Join(strings.Fields(row["Gid"]), " ") != "0 0 0 0" {
			return errGuardProtocol
		}
		for _, name := range []string{"CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb"} {
			value, err := strconv.ParseUint(row[name], 16, 64)
			want := uint64(0)
			if name == "CapPrm" || name == "CapEff" || name == "CapBnd" {
				want = uint64(1) << unix.CAP_AUDIT_CONTROL
			}
			if err != nil || value != want {
				return errGuardProtocol
			}
		}
	}
	return nil
}
