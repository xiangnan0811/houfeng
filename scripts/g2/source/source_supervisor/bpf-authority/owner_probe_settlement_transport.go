//go:build linux && amd64 && !cgo

package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"

	"golang.org/x/sys/unix"
)

const settlementEvidenceDirectory = "/run/g2/owner-probe-settlement-evidence"

type settlementEvidenceConnection struct {
	fd                                 int
	principal                          *controlPrincipal
	purpose                            uint8
	nonce                              [16]byte
	cid                                [32]byte
	start, deadline, authorityDeadline uint64
	registeredAt                       uint64
	hello, bound                       bool
}

type settlementEvidenceTransport struct {
	listener, parent, dir, socket int
	dirStat, socketStat           unix.Stat_t
	connections                   [2]settlementEvidenceConnection
}

func openSettlementEvidenceTransport() (*settlementEvidenceTransport, error) {
	t := &settlementEvidenceTransport{listener: -1, parent: -1, dir: -1, socket: -1}
	for i := range t.connections {
		t.connections[i].fd = -1
	}
	ok := false
	defer func() {
		if !ok {
			t.close()
		}
	}()
	var err error
	t.parent, err = runtimeDirectory("/run/g2", false)
	if err != nil {
		return nil, err
	}
	if err = unix.Mkdirat(t.parent, "owner-probe-settlement-evidence", 0700); err != nil {
		return nil, err
	}
	t.dir, err = unix.Openat(t.parent, "owner-probe-settlement-evidence", unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	var named unix.Stat_t
	var fs unix.Statfs_t
	if unix.Fstat(t.dir, &t.dirStat) != nil || unix.Fstatat(t.parent, "owner-probe-settlement-evidence", &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameTrustedStat(t.dirStat, named) || t.dirStat.Uid != 0 || t.dirStat.Gid != 0 || t.dirStat.Mode&07777 != 0700 || unix.Fstatfs(t.dir, &fs) != nil || fs.Type != unix.TMPFS_MAGIC {
		return nil, errGuardProtocol
	}
	t.listener, err = unix.Socket(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	if err = unix.SetsockoptInt(t.listener, unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
		return nil, err
	}
	if unix.Fstat(t.dir, &named) != nil || named.Dev != t.dirStat.Dev || named.Ino != t.dirStat.Ino || unix.Fstatat(t.parent, "owner-probe-settlement-evidence", &named, unix.AT_SYMLINK_NOFOLLOW) != nil || named.Dev != t.dirStat.Dev || named.Ino != t.dirStat.Ino {
		return nil, errGuardProtocol
	}
	if err = unix.Bind(t.listener, &unix.SockaddrUnix{Name: settlementEvidenceDirectory + "/broker.sock"}); err != nil {
		return nil, err
	}
	t.socket, err = unix.Openat(t.dir, "broker.sock", unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	if unix.Fstat(t.socket, &t.socketStat) != nil || t.socketStat.Mode&unix.S_IFMT != unix.S_IFSOCK || t.socketStat.Uid != 0 || t.socketStat.Gid != 0 {
		return nil, errGuardProtocol
	}
	if err = unix.Fchmodat(t.dir, "broker.sock", 0600, 0); err != nil {
		return nil, err
	}
	if unix.Fstat(t.socket, &t.socketStat) != nil || unix.Fstatat(t.dir, "broker.sock", &named, unix.AT_SYMLINK_NOFOLLOW) != nil || !sameTrustedStat(t.socketStat, named) || t.socketStat.Mode&07777 != 0600 {
		return nil, errGuardProtocol
	}
	if err = unix.Listen(t.listener, 2); err != nil {
		return nil, err
	}
	ok = true
	return t, nil
}

func (t *settlementEvidenceTransport) close() {
	if t == nil {
		return
	}
	for i := range t.connections {
		if t.connections[i].fd >= 0 {
			_ = unix.Close(t.connections[i].fd)
			t.connections[i].fd = -1
		}
	}
	if t.listener >= 0 {
		_ = unix.Close(t.listener)
		t.listener = -1
	}
	var held, named unix.Stat_t
	if t.socket >= 0 && t.dir >= 0 && unix.Fstat(t.socket, &held) == nil && unix.Fstatat(t.dir, "broker.sock", &named, unix.AT_SYMLINK_NOFOLLOW) == nil && sameTrustedStat(t.socketStat, held) && sameTrustedStat(held, named) {
		_ = unix.Unlinkat(t.dir, "broker.sock", 0)
	}
	if t.socket >= 0 {
		_ = unix.Close(t.socket)
		t.socket = -1
	}
	if t.dir >= 0 && t.parent >= 0 && unix.Fstat(t.dir, &held) == nil && unix.Fstatat(t.parent, "owner-probe-settlement-evidence", &named, unix.AT_SYMLINK_NOFOLLOW) == nil && held.Dev == t.dirStat.Dev && held.Ino == t.dirStat.Ino && sameTrustedStat(held, named) {
		_ = unix.Unlinkat(t.parent, "owner-probe-settlement-evidence", unix.AT_REMOVEDIR)
	}
	if t.dir >= 0 {
		_ = unix.Close(t.dir)
		t.dir = -1
	}
	if t.parent >= 0 {
		_ = unix.Close(t.parent)
		t.parent = -1
	}
}

func (t *settlementEvidenceTransport) poll(p []unix.PollFd) []unix.PollFd {
	if t == nil {
		return p
	}
	p = append(p, unix.PollFd{Fd: int32(t.listener), Events: unix.POLLIN})
	for i := range t.connections {
		if t.connections[i].fd >= 0 {
			p = append(p, unix.PollFd{Fd: int32(t.connections[i].fd), Events: unix.POLLIN})
		}
	}
	return p
}

func (t *settlementEvidenceTransport) fail(c *settlementEvidenceConnection, s *ownerProbeEvidenceState, code uint64) {
	if c == nil {
		return
	}
	if c.purpose >= 1 && c.purpose <= 2 {
		e := &s.grants[c.purpose-1]
		if !e.final {
			e.gap |= settlementGapExport
			e.exportError = code
		}
	}
	if c.fd >= 0 {
		_ = unix.Close(c.fd)
		c.fd = -1
	}
}

func receiveSettlementEvidenceFrame(fd int) ([]byte, *unix.Ucred, error) {
	var data [513]byte
	var control [256]byte
	n, on, flags, _, err := unix.Recvmsg(fd, data[:], control[:], unix.MSG_CMSG_CLOEXEC|unix.MSG_DONTWAIT)
	if err != nil {
		return nil, nil, err
	}
	if n == 0 && on == 0 {
		return nil, nil, unix.ECONNRESET
	}
	valid := n > 0 && n <= 512 && flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) == 0
	var cred *unix.Ucred
	messages, parseErr := unix.ParseSocketControlMessage(control[:on])
	if parseErr != nil {
		valid = false
	}
	for _, message := range messages {
		switch {
		case message.Header.Level == unix.SOL_SOCKET && message.Header.Type == unix.SCM_RIGHTS:
			rights, e := unix.ParseUnixRights(&message)
			if e != nil {
				valid = false
			}
			for _, fd := range rights {
				_ = unix.Close(fd)
			}
			valid = false
		case message.Header.Level == unix.SOL_SOCKET && message.Header.Type == unix.SCM_CREDENTIALS:
			c, e := unix.ParseUnixCredentials(&message)
			if e != nil || cred != nil {
				valid = false
			} else {
				cred = c
			}
		default:
			valid = false
		}
	}
	if !valid || cred == nil {
		return nil, nil, errControlFrame
	}
	return append([]byte(nil), data[:n]...), cred, nil
}

func (t *settlementEvidenceTransport) service(b *authorityBroker, s *ownerProbeEvidenceState) {
	if t == nil || b == nil || s == nil {
		return
	}
	for range 2 {
		fd, _, err := unix.Accept4(t.listener, unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC)
		if err != nil {
			break
		}
		index := -1
		for i := range t.connections {
			if t.connections[i].fd < 0 && !t.connections[i].hello {
				index = i
				break
			}
		}
		if index < 0 || unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_PASSCRED, 1) != nil {
			_ = unix.Close(fd)
			continue
		}
		t.connections[index].fd = fd
	}
	for i := range t.connections {
		c := &t.connections[i]
		if c.fd < 0 {
			continue
		}
		raw, cred, err := receiveSettlementEvidenceFrame(c.fd)
		if err == nil {
			if !t.input(b, s, c, raw, cred) {
				t.fail(c, s, 4)
				continue
			}
		} else if errors.Is(err, errControlFrame) {
			t.fail(c, s, 4)
			continue
		} else if !errors.Is(err, unix.EAGAIN) && !errors.Is(err, unix.EINTR) {
			t.fail(c, s, 5)
			continue
		}
		if c.purpose < 1 || c.purpose > 2 {
			continue
		}
		e := &s.grants[c.purpose-1]
		if e.grant == nil || e.sent >= e.snapshotCount {
			continue
		}
		n, err := unix.SendmsgN(c.fd, e.snapshots[e.sent][:], nil, nil, unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL)
		if err == nil && n == settlementEvidenceBytes {
			e.sent++
		} else {
			t.fail(c, s, 2)
		}
	}
}

func (t *settlementEvidenceTransport) flush(s *ownerProbeEvidenceState) {
	if t == nil || s == nil {
		return
	}
	for i := range t.connections {
		c := &t.connections[i]
		if c.fd < 0 || c.purpose < 1 || c.purpose > 2 {
			continue
		}
		e := &s.grants[c.purpose-1]
		for e.sent < e.snapshotCount {
			n, err := unix.SendmsgN(c.fd, e.snapshots[e.sent][:], nil, nil, unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL)
			if err != nil || n != settlementEvidenceBytes {
				t.fail(c, s, 2)
				break
			}
			e.sent++
		}
	}
}

func (t *settlementEvidenceTransport) input(b *authorityBroker, s *ownerProbeEvidenceState, c *settlementEvidenceConnection, raw []byte, cred *unix.Ucred) bool {
	if len(raw) > 512 || cred == nil || cred.Pid <= 1 {
		return false
	}
	p := b.principals[b.bootOwner]
	if p == nil || p.Role != "BOOT_OWNER" || p.Terminal || p.Transferred || p.ID == 0 || uint32(cred.Pid) != p.PID || cred.Uid != p.UID || cred.Gid != p.GID || !pidfdSelects(p.PIDFD, p.PID) {
		return false
	}
	peer, err := unix.GetsockoptUcred(c.fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || peer.Pid != cred.Pid || peer.Uid != cred.Uid || peer.Gid != cred.Gid {
		return false
	}
	process, err := observeProcess(p.PID, p.PIDFD)
	if err != nil || p.StartNS/10_000_000 != process.StartTicks {
		return false
	}
	frame, err := parseCanonicalControl(raw)
	if err != nil || frame["version"] != json.Number("1") {
		return false
	}
	principal, valid := exactNumber(frame["principal_id"])
	purpose, purposeOK := frame["purpose"].(string)
	nonce, nonceOK := frame["nonce"].(string)
	if !valid || principal != p.ID || !purposeOK || !nonceOK || !validCheckpointNonce(nonce) {
		return false
	}
	var purposeID uint8
	switch purpose {
	case "postgres-uid":
		purposeID = 1
	case "postgres-gid":
		purposeID = 2
	default:
		return false
	}
	var nonceBytes [16]byte
	if _, err = hex.Decode(nonceBytes[:], []byte(nonce)); err != nil {
		return false
	}
	if !c.hello {
		if len(raw) > 160 || !exactKeys(frame, "version", "purpose", "nonce", "principal_id") {
			return false
		}
		if e := &s.grants[purposeID-1]; e.grant != nil {
			e.gap |= settlementGapUnbound
			return false
		}
		for i := range t.connections {
			other := &t.connections[i]
			if other != c && other.hello && (other.purpose == purposeID || other.nonce == nonceBytes) {
				return false
			}
		}
		c.principal, c.purpose, c.nonce, c.hello = p, purposeID, nonceBytes, true
		reply, err := canonicalWithLimit(map[string]any{"version": jsonU64(1), "status": "ARMED", "nonce": nonce}, 128)
		if err != nil || sendControl(c.fd, reply, nil) != nil {
			return false
		}
		c.registeredAt = evidenceNow()
		return true
	}
	if c.bound || c.principal != p || c.purpose != purposeID || c.nonce != nonceBytes || !exactKeys(frame, "version", "purpose", "container_id", "nonce", "operation_start_ns", "operation_deadline_ns", "authority_deadline_ns", "principal_id") {
		return false
	}
	cid, ok := frame["container_id"].(string)
	start, okStart := exactNumber(frame["operation_start_ns"])
	deadline, okDeadline := exactNumber(frame["operation_deadline_ns"])
	authority, okAuthority := exactNumber(frame["authority_deadline_ns"])
	if !ok || !runtimeContainerID(cid) || !okStart || !okDeadline || !okAuthority || start == 0 || deadline == 0 || deadline < start {
		return false
	}
	if _, err = hex.Decode(c.cid[:], []byte(cid)); err != nil {
		return false
	}
	c.bound, c.start, c.deadline, c.authorityDeadline = true, start, deadline, authority
	if e := &s.grants[purposeID-1]; e.grant != nil {
		if e.cid != c.cid || e.grant.owner != p.ID {
			return false
		}
		if !e.final {
			e.start, e.deadline, e.authorityDeadline = start, deadline, authority
		}
	}
	return true
}
