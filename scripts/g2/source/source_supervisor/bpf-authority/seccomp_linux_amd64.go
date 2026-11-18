//go:build linux && amd64 && !cgo

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	auditArchX8664 = 0xc000003e
	auditArchI386  = 0x40000003
	x32SyscallBit  = 0x40000000
)

// installRuntimeListener installs the filter on the locked calling thread.
// The ensuing exec discards other Go threads and preserves this filter for
// runc and its descendants. A listener is never installed on the daemon.
func installRuntimeListener() (int, error) {
	const (
		ldWordAbsolute = unix.BPF_LD | unix.BPF_W | unix.BPF_ABS
		jumpEqual      = unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K
		returnConstant = unix.BPF_RET | unix.BPF_K
	)
	ins := []unix.SockFilter{
		{Code: ldWordAbsolute, K: 4}, // seccomp_data.arch
		{Code: jumpEqual, Jt: 0, Jf: 5, K: auditArchX8664},
		{Code: ldWordAbsolute, K: 0}, // seccomp_data.nr
		{Code: jumpEqual, Jt: 0, Jf: 1, K: uint32(unix.SYS_BPF)},
		{Code: returnConstant, K: unix.SECCOMP_RET_USER_NOTIF},
		{Code: jumpEqual, Jt: 0, Jf: 1, K: x32SyscallBit | uint32(unix.SYS_BPF)},
		{Code: returnConstant, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
		{Code: jumpEqual, Jt: 0, Jf: 3, K: auditArchI386},
		{Code: ldWordAbsolute, K: 0},
		{Code: jumpEqual, Jt: 0, Jf: 1, K: 357},
		{Code: returnConstant, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
		{Code: returnConstant, K: unix.SECCOMP_RET_ALLOW},
	}
	prog := unix.SockFprog{Len: uint16(len(ins)), Filter: &ins[0]}
	r, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, unix.SECCOMP_FILTER_FLAG_NEW_LISTENER, uintptr(unsafe.Pointer(&prog)))
	if errno != 0 {
		return -1, errno
	}
	if r > math.MaxInt32 {
		return -1, errGuardProtocol
	}
	fd := int(r)
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_SETFD, unix.FD_CLOEXEC); err != nil {
		_ = unix.Close(fd)
		return -1, err
	}
	return fd, nil
}

type seccompNotification struct {
	ID                 uint64
	TID                uint32
	Flags              uint32
	Syscall            int32
	Arch               uint32
	InstructionPointer uint64
	Args               [6]uint64
}

func notificationIOCTL(fd int, request uintptr, raw []byte) (uintptr, error) {
	if fd < 0 || len(raw) == 0 {
		return 0, errGuardProtocol
	}
	r, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), request, uintptr(unsafe.Pointer(&raw[0])))
	if errno != 0 {
		return 0, errno
	}
	return r, nil
}

func receiveNotification(fd int) (seccompNotification, error) {
	var out seccompNotification
	raw := make([]byte, 80)
	if _, err := notificationIOCTL(fd, unix.SECCOMP_IOCTL_NOTIF_RECV, raw); err != nil {
		return out, err
	}
	out.ID, out.TID, out.Flags = guardU64(raw, 0), guardU32(raw, 8), guardU32(raw, 12)
	out.Syscall, out.Arch = int32(guardU32(raw, 16)), guardU32(raw, 20)
	out.InstructionPointer = guardU64(raw, 24)
	for i := range out.Args {
		out.Args[i] = guardU64(raw, 32+i*8)
	}
	if out.ID == 0 || out.TID == 0 || out.Flags != 0 || out.Syscall != unix.SYS_BPF || out.Arch != auditArchX8664 {
		return seccompNotification{}, errGuardProtocol
	}
	return out, nil
}

func notificationValid(fd int, id uint64) error {
	if id == 0 {
		return errGuardProtocol
	}
	b := make([]byte, 8)
	guardPut64(b, 0, id)
	_, err := notificationIOCTL(fd, unix.SECCOMP_IOCTL_NOTIF_ID_VALID, b)
	return err
}

// replyNotification always emulates a result; the kernel CONTINUE flag is not
// exposed. Negative errno is in the response error field, as required by UAPI.
func replyNotification(fd int, id uint64, value int64, errno unix.Errno) error {
	if id == 0 || errno > 4095 || (errno != 0 && value != 0) {
		return errGuardProtocol
	}
	b := make([]byte, 24)
	guardPut64(b, 0, id)
	guardPut64(b, 8, uint64(value))
	if errno != 0 {
		guardPut32(b, 16, uint32(-int32(errno)))
	}
	_, err := notificationIOCTL(fd, unix.SECCOMP_IOCTL_NOTIF_SEND, b)
	return err
}

// injectNotificationFD atomically installs the broker-held file and sends the
// successful syscall response. An error, including an interrupted or failed
// ioctl, must remain an unresolved settlement until independently classified.
func injectNotificationFD(fd int, id uint64, sourceFD int) (int, error) {
	if id == 0 || sourceFD < 0 || int64(sourceFD) > math.MaxInt32 {
		return -1, errGuardProtocol
	}
	b := make([]byte, 24)
	guardPut64(b, 0, id)
	guardPut32(b, 8, unix.SECCOMP_ADDFD_FLAG_SEND)
	guardPut32(b, 12, uint32(sourceFD))
	guardPut32(b, 20, unix.O_CLOEXEC)
	r, err := notificationIOCTL(fd, unix.SECCOMP_IOCTL_NOTIF_ADDFD, b)
	if err != nil {
		return -1, err
	}
	if r > math.MaxInt32 {
		return -1, errGuardProtocol
	}
	return int(r), nil
}

// notificationMemory pins the notifying thread's mm by opening /proc/tid/mem
// before any copy. It must be paired with a held guard task and with repeated
// ID_VALID checks at every commit and import boundary.
type notificationMemory struct {
	fd, listener int
	id           uint64
}

func openNotificationMemory(listener int, n seccompNotification) (*notificationMemory, error) {
	if err := notificationValid(listener, n.ID); err != nil {
		return nil, err
	}
	fd, err := unix.Open(fmt.Sprintf("/proc/%d/mem", n.TID), unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	if err := notificationValid(listener, n.ID); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	return &notificationMemory{fd: fd, listener: listener, id: n.ID}, nil
}

func (m *notificationMemory) close() error {
	if m.fd < 0 {
		return errGuardProtocol
	}
	fd := m.fd
	m.fd = -1
	return unix.Close(fd)
}

func (m *notificationMemory) transfer(address uint64, b []byte, write bool) error {
	if m.fd < 0 || len(b) == 0 || len(b) > 131072 || address == 0 || address > math.MaxInt64 || uint64(len(b)-1) > math.MaxInt64-address {
		return errGuardProtocol
	}
	if err := notificationValid(m.listener, m.id); err != nil {
		return err
	}
	for done := 0; done < len(b); {
		var n int
		var err error
		if write {
			n, err = unix.Pwrite(m.fd, b[done:], int64(address)+int64(done))
		} else {
			n, err = unix.Pread(m.fd, b[done:], int64(address)+int64(done))
		}
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrUnexpectedEOF
		}
		done += n
	}
	return notificationValid(m.listener, m.id)
}

func (m *notificationMemory) copyIn(address uint64, size int) ([]byte, error) {
	if size <= 0 || size > 65536 {
		return nil, errGuardProtocol
	}
	b := make([]byte, size)
	if err := m.transfer(address, b, false); err != nil {
		return nil, err
	}
	return b, nil
}

func (m *notificationMemory) copyOut(address uint64, b []byte) error {
	return m.transfer(address, b, true)
}

func littleU64(b []byte) uint64 { return binary.LittleEndian.Uint64(b) }
