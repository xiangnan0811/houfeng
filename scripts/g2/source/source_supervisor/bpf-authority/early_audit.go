//go:build linux && amd64 && !cgo

package main

import (
	"encoding/binary"
	"math"
	"os"

	"golang.org/x/sys/unix"
)

// earlyAuditControl exists only in PID1. Its CLOEXEC socket is never passed to
// broker or reader; after the final immutable configuration it is closed.
type earlyAuditControl struct {
	fd             int
	port, sequence uint32
	deadline       uint64
}

func (a *earlyAuditControl) close() {
	if a != nil && a.fd >= 0 {
		_ = unix.Close(a.fd)
		a.fd = -1
	}
}
func (a *earlyAuditControl) next() (uint32, error) {
	if a == nil || a.fd < 0 || a.sequence == math.MaxUint32 {
		return 0, errGuardProtocol
	}
	a.sequence++
	return a.sequence, nil
}

func (a *earlyAuditControl) configuration(initial, final bool) (observedAuditStatus, error) {
	queries := make([]any, 0, 3)
	for _, kind := range []uint16{auditGetType, auditListRulesType, auditGetType} {
		sequence, err := a.next()
		if err != nil {
			return observedAuditStatus{}, err
		}
		value, err := readerQuery(a.fd, -1, a.port, sequence, kind, a.deadline)
		if err != nil {
			return observedAuditStatus{}, err
		}
		queries = append(queries, value)
	}
	if initial {
		return validateInitialAuditQueries(queries)
	}
	return validateAuditConfiguration(map[string]any{"check": "configuration", "queries": queries}, final, 0)
}

func (a *earlyAuditControl) change(kind uint16, payload []byte) error {
	sequence, err := a.next()
	if err != nil {
		return err
	}
	request := make([]byte, 16+len(payload))
	binary.LittleEndian.PutUint32(request, uint32(len(request)))
	binary.LittleEndian.PutUint16(request[4:], kind)
	binary.LittleEndian.PutUint16(request[6:], unix.NLM_F_REQUEST|unix.NLM_F_ACK)
	binary.LittleEndian.PutUint32(request[8:], sequence)
	binary.LittleEndian.PutUint32(request[12:], a.port)
	copy(request[16:], payload)
	for {
		err = unix.Sendto(a.fd, request, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK})
		if err == unix.EINTR {
			continue
		}
		if err == unix.EAGAIN {
			if err = readerWait(a.fd, unix.POLLOUT, -1, a.deadline); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		break
	}
	if err = readerWait(a.fd, unix.POLLIN, -1, a.deadline); err != nil {
		return err
	}
	var data [65536]byte
	var ancillary [256]byte
	n, on, flags, from, err := unix.Recvmsg(a.fd, data[:], ancillary[:], unix.MSG_CMSG_CLOEXEC)
	if on != 0 {
		messages, _ := unix.ParseSocketControlMessage(ancillary[:on])
		for _, message := range messages {
			if message.Header.Level == unix.SOL_SOCKET && message.Header.Type == unix.SCM_RIGHTS {
				rights, _ := unix.ParseUnixRights(&message)
				for _, fd := range rights {
					_ = unix.Close(fd)
				}
			}
		}
	}
	source, ok := from.(*unix.SockaddrNetlink)
	if err != nil || !ok || source.Pid != 0 || source.Groups != 0 || on != 0 || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || n < 36 {
		return errGuardProtocol
	}
	length := binary.LittleEndian.Uint32(data[:])
	if length < 36 || int((length+3)&^3) != n || binary.LittleEndian.Uint16(data[4:]) != unix.NLMSG_ERROR || binary.LittleEndian.Uint32(data[8:]) != sequence || int32(binary.LittleEndian.Uint32(data[16:])) != 0 {
		return errGuardProtocol
	}
	// CAPPED ACKs contain the complete original header, not the full attr.
	for i := range 16 {
		if data[20+i] != request[i] {
			return errGuardProtocol
		}
	}
	pending := []unix.PollFd{{Fd: int32(a.fd), Events: unix.POLLIN}}
	if _, err = unix.Poll(pending, 0); err != nil || pending[0].Revents != 0 {
		return errGuardProtocol
	}
	return nil
}

func (a *earlyAuditControl) set(mask, enabled, failure, rate, backlog uint32) error {
	payload := make([]byte, 44)
	for index, value := range []uint32{mask, enabled, failure, 0, rate, backlog, 0, 0, 0, 0, 0} {
		binary.LittleEndian.PutUint32(payload[index*4:], value)
	}
	return a.change(1001, payload)
}

func setupEarlyAudit() (_ *earlyAuditControl, err error) {
	if os.Getpid() != 1 || unix.Gettid() != 1 || os.Geteuid() != 0 {
		return nil, errGuardProtocol
	}
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, unix.NETLINK_AUDIT)
	if err != nil {
		return nil, err
	}
	now, err := monoNowNS()
	if err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	deadline, err := clampGuestDeadlineNS(now + 5_000_000_000)
	if err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	a := &earlyAuditControl{fd: fd, deadline: deadline}
	defer func() {
		if err != nil {
			a.close()
		}
	}()
	if err = unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		return nil, err
	}
	address, err := unix.Getsockname(fd)
	if err != nil {
		return nil, err
	}
	source, ok := address.(*unix.SockaddrNetlink)
	if !ok || source.Pid == 0 || source.Groups != 0 {
		return nil, errGuardProtocol
	}
	a.port = source.Pid
	initial, err := a.configuration(true, false)
	if err != nil || initial.pid != 0 || initial.lost != 0 || initial.enabled != 0 {
		return nil, errGuardProtocol
	}
	if err = a.set(1|2|8|16, 1, 1, 0, 16384); err != nil {
		return nil, err
	}
	for _, rule := range [][]byte{fixedAuditRule(auditArchX8664, []uint32{321}), fixedAuditRule(auditArchI386, []uint32{357})} {
		if err = a.change(1011, rule); err != nil {
			return nil, err
		}
	}
	if _, err = a.configuration(false, false); err != nil {
		return nil, err
	}
	return a, nil
}

// finalize is called once, after the broker acknowledges three-link sealing.
// The broker already holds the fixed final-control expectations and drains
// multicast while PID1 performs these synchronous configuration writes.
func (a *earlyAuditControl) finalize() error {
	if os.Getpid() != 1 || unix.Gettid() != 1 {
		return errGuardProtocol
	}
	now, err := monoNowNS()
	if err != nil {
		return err
	}
	a.deadline, err = clampGuestDeadlineNS(now + 5_000_000_000)
	if err != nil {
		return err
	}
	for _, rule := range [][]byte{fixedAuditRule(auditArchX8664, []uint32{49, 59, 322}), fixedAuditRule(auditArchI386, []uint32{11, 102, 358, 361})} {
		if err := a.change(1011, rule); err != nil {
			return err
		}
	}
	if err := a.set(1, 2, 0, 0, 0); err != nil {
		return err
	}
	_, err = a.configuration(false, true)
	return err
}
