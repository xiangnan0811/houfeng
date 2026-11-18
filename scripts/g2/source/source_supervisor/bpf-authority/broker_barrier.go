//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// heldSelectReadSet captures the actual x86-64 select/pselect read set while
// the child is asleep in the inherited handoff. It does not infer an fd from a
// syscall number or rely on the child's supplied PID as an authority.
func heldSelectReadSet(pid uint32) ([]byte, int, error) {
	raw, err := boundedProc(pid, "syscall", 4096)
	if err != nil {
		return nil, -1, err
	}
	parts := strings.Fields(string(raw))
	if len(parts) < 7 {
		return nil, -1, errGuardProtocol
	}
	parse := func(s string) (uint64, error) { return strconv.ParseUint(s, 0, 64) }
	nr, err := parse(parts[0])
	if err != nil || nr != unix.SYS_SELECT && nr != unix.SYS_PSELECT6 {
		return nil, -1, errGuardProtocol
	}
	nfds, err := parse(parts[1])
	if err != nil || nfds == 0 || nfds > 1024 {
		return nil, -1, errGuardProtocol
	}
	address, err := parse(parts[2])
	if err != nil || address == 0 || address > math.MaxInt64 {
		return nil, -1, errGuardProtocol
	}
	writeSet, err := parse(parts[3])
	if err != nil {
		return nil, -1, errGuardProtocol
	}
	exceptSet, err := parse(parts[4])
	if err != nil {
		return nil, -1, errGuardProtocol
	}
	length := int((nfds+63)/64) * 8
	if uint64(length-1) > math.MaxInt64-address {
		return nil, -1, errGuardProtocol
	}
	fd, err := unix.Open(fmt.Sprintf("/proc/%d/mem", pid), unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, -1, err
	}
	defer unix.Close(fd)
	set := make([]byte, length)
	for _, optional := range []uint64{writeSet, exceptSet} {
		if optional == 0 {
			continue
		}
		if optional > math.MaxInt64 || uint64(length-1) > math.MaxInt64-optional {
			return nil, -1, errGuardProtocol
		}
		empty := make([]byte, length)
		for offset := 0; offset < len(empty); {
			n, e := unix.Pread(fd, empty[offset:], int64(optional)+int64(offset))
			if e == unix.EINTR {
				continue
			}
			if e != nil || n <= 0 {
				return nil, -1, errGuardProtocol
			}
			offset += n
		}
		if !guardZero(empty) {
			return nil, -1, errGuardProtocol
		}
	}
	for offset := 0; offset < len(set); {
		n, e := unix.Pread(fd, set[offset:], int64(address)+int64(offset))
		if e == unix.EINTR {
			continue
		}
		if e != nil || n <= 0 {
			return nil, -1, errGuardProtocol
		}
		offset += n
	}
	chosen := -1
	for bit := 0; bit < len(set)*8; bit++ {
		if set[bit/8]&(1<<uint(bit%8)) == 0 {
			continue
		}
		if uint64(bit) >= nfds || chosen != -1 {
			return nil, -1, errGuardProtocol
		}
		chosen = bit
	}
	if chosen < 0 {
		return nil, -1, errGuardProtocol
	}
	second := make([]byte, len(set))
	for offset := 0; offset < len(second); {
		n, e := unix.Pread(fd, second[offset:], int64(address)+int64(offset))
		if e == unix.EINTR {
			continue
		}
		if e != nil || n <= 0 {
			return nil, -1, errGuardProtocol
		}
		offset += n
	}
	after, err := boundedProc(pid, "syscall", 4096)
	if err != nil || !bytes.Equal(raw, after) || !bytes.Equal(set, second) {
		return nil, -1, errGuardProtocol
	}
	return raw, chosen, nil
}

func pipeDescriptors(g guardDevice, taskCookie uint64, pid uint32, wanted unix.Stat_t, access int) (int, error) {
	entries, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
	if err != nil || len(entries) > 4096 {
		return 0, errGuardProtocol
	}
	matches := 0
	for _, entry := range entries {
		slot, e := strconv.ParseUint(entry.Name(), 10, 32)
		if e != nil {
			return 0, errGuardProtocol
		}
		link, e := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", pid, entry.Name()))
		if e != nil {
			return 0, errGuardProtocol
		}
		if link != fmt.Sprintf("pipe:[%d]", wanted.Ino) {
			continue
		}
		fd, e := g.importFD(taskCookie, uint32(slot))
		if e != nil {
			return 0, e
		}
		var actual unix.Stat_t
		flags, flagErr := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
		statErr := unix.Fstat(fd, &actual)
		_ = unix.Close(fd)
		if statErr != nil || flagErr != nil || actual.Mode&unix.S_IFMT != unix.S_IFIFO || actual.Dev != wanted.Dev || actual.Ino != wanted.Ino || flags&unix.O_ACCMODE != access {
			return 0, errGuardProtocol
		}
		matches++
	}
	return matches, nil
}

func verifyRealmChildBarrier(g guardDevice, parentCookie, childCookie uint64, parentPID, childPID uint32) error {
	if parentCookie == 0 || childCookie == 0 || parentPID <= 1 || childPID <= 1 || parentPID == childPID {
		return errGuardProtocol
	}
	before, slot, err := heldSelectReadSet(childPID)
	if err != nil {
		return err
	}
	fd, err := g.importFD(childCookie, uint32(slot))
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var pipe unix.Stat_t
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil || unix.Fstat(fd, &pipe) != nil || pipe.Mode&unix.S_IFMT != unix.S_IFIFO || pipe.Ino == 0 || flags&unix.O_ACCMODE != unix.O_RDONLY {
		return errGuardProtocol
	}
	children, err := pipeDescriptors(g, childCookie, childPID, pipe, unix.O_RDONLY)
	if err != nil || children != 1 {
		return errGuardProtocol
	}
	parents, err := pipeDescriptors(g, parentCookie, parentPID, pipe, unix.O_WRONLY)
	if err != nil || parents != 1 {
		return errGuardProtocol
	}
	after, sameSlot, err := heldSelectReadSet(childPID)
	if err != nil || sameSlot != slot || !bytes.Equal(before, after) {
		return errGuardProtocol
	}
	return nil
}
