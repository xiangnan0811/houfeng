//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	auditReadlogGroup   = 1
	auditSyscallRecord  = 1300
	auditEOERecord      = 1320
	auditConfigRecord   = 1305
	auditSockaddrRecord = 1306
	auditExecveRecord   = 1309
	auditBPFRecord      = 1334
)

type auditEventKey struct {
	Seconds      uint64
	Milliseconds uint32
	Serial       uint64
}

func (k auditEventKey) stamp() map[string]any {
	return map[string]any{"seconds": jsonU64(k.Seconds), "milliseconds": jsonU64(uint64(k.Milliseconds))}
}

func auditStamp(body []byte) (auditEventKey, error) {
	var key auditEventKey
	if !bytes.HasPrefix(body, []byte("audit(")) {
		return key, errGuardProtocol
	}
	end := bytes.Index(body, []byte("):"))
	if end < 0 {
		return key, errGuardProtocol
	}
	stamp := body[6:end]
	colon := bytes.IndexByte(stamp, ':')
	dot := bytes.IndexByte(stamp, '.')
	if dot < 1 || colon != dot+4 || colon == len(stamp)-1 {
		return key, errGuardProtocol
	}
	for _, part := range [][]byte{stamp[:dot], stamp[dot+1 : colon], stamp[colon+1:]} {
		for _, c := range part {
			if c < '0' || c > '9' {
				return key, errGuardProtocol
			}
		}
	}
	sec, err := strconv.ParseUint(string(stamp[:dot]), 10, 63)
	if err != nil {
		return key, errGuardProtocol
	}
	ms, err := strconv.ParseUint(string(stamp[dot+1:colon]), 10, 32)
	if err != nil || ms > 999 {
		return key, errGuardProtocol
	}
	serial, err := strconv.ParseUint(string(stamp[colon+1:]), 10, 32)
	if err != nil || serial == 0 {
		return key, errGuardProtocol
	}
	return auditEventKey{sec, uint32(ms), serial}, nil
}

type auditFact struct {
	Key                      auditEventKey
	PayloadSHA256            string
	Fields                   map[string]string
	Serial                   uint64
	Kind                     uint16
	Arch, Syscall, PID, PPID uint64
	Argument0                uint64
	Success                  bool
	Result                   int64
	ProgramID                uint64
	Operation                string
}

type auditMulticast struct{ fd int }

// openAuditMulticast is a read-only listener; it neither registers as auditd
// nor installs rules. The independently owned unicast reader keeps its normal
// exec/bind and origin delivery. The fixed kernel requires CAP_AUDIT_READ.
func openAuditMulticast() (*auditMulticast, error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, unix.NETLINK_AUDIT)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = unix.Close(fd)
		}
	}()
	if err = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, 8*1024*1024); err != nil {
		return nil, err
	}
	size, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF)
	if err != nil || size < 256*1024 {
		return nil, errGuardProtocol
	}
	if err = unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Groups: 1 << (auditReadlogGroup - 1)}); err != nil {
		return nil, err
	}
	ok = true
	return &auditMulticast{fd: fd}, nil
}

func (a *auditMulticast) close() error {
	if a == nil || a.fd < 0 {
		return errGuardProtocol
	}
	fd := a.fd
	a.fd = -1
	return unix.Close(fd)
}

// auditTokens understands audit's quoted values without interpreting them as
// shell syntax. Unconsumed fields are ignored only after their token framing is
// validated; no argv, address or unbounded raw payload is retained.
func auditTokens(body []byte) (uint64, map[string]string, error) {
	body = bytes.TrimRight(body, "\x00")
	if len(body) == 0 || len(body) > 65536 || !bytes.HasPrefix(body, []byte("audit(")) {
		return 0, nil, errGuardProtocol
	}
	end := bytes.Index(body, []byte("):"))
	if end < 0 {
		return 0, nil, errGuardProtocol
	}
	key, err := auditStamp(body)
	if err != nil {
		return 0, nil, err
	}
	serial := key.Serial
	fields := make(map[string]string)
	rest := body[end+2:]
	for i := 0; i < len(rest); {
		for i < len(rest) && rest[i] == ' ' {
			i++
		}
		if i == len(rest) {
			break
		}
		start := i
		for i < len(rest) && rest[i] != '=' && rest[i] != ' ' {
			if rest[i] < 0x21 || rest[i] > 0x7e {
				return 0, nil, errGuardProtocol
			}
			i++
		}
		if i == start || i == len(rest) || rest[i] != '=' {
			return 0, nil, errGuardProtocol
		}
		key := string(rest[start:i])
		i++
		start = i
		if i < len(rest) && rest[i] == '"' {
			i++
			closed := false
			for i < len(rest) {
				if rest[i] == '\\' {
					i++
					if i == len(rest) {
						return 0, nil, errGuardProtocol
					}
					i++
					continue
				}
				if rest[i] == '"' {
					i++
					closed = true
					break
				}
				if rest[i] < 0x20 {
					return 0, nil, errGuardProtocol
				}
				i++
			}
			if !closed || (i < len(rest) && rest[i] != ' ') {
				return 0, nil, errGuardProtocol
			}
		} else {
			for i < len(rest) && rest[i] != ' ' {
				if rest[i] < 0x21 || rest[i] > 0x7e {
					return 0, nil, errGuardProtocol
				}
				i++
			}
		}
		if i == start {
			return 0, nil, errGuardProtocol
		}
		if _, exists := fields[key]; exists {
			return 0, nil, errGuardProtocol
		}
		fields[key] = string(rest[start:i])
	}
	return serial, fields, nil
}

func auditNumber(fields map[string]string, key string, base int, bits int) (uint64, error) {
	s, ok := fields[key]
	if !ok || s == "" || strings.HasPrefix(s, "+") || strings.HasPrefix(s, "-") {
		return 0, errGuardProtocol
	}
	v, err := strconv.ParseUint(s, base, bits)
	if err != nil {
		return 0, errGuardProtocol
	}
	return v, nil
}

func parseAuditFact(kind uint16, body []byte) (auditFact, error) {
	var out auditFact
	key, err := auditStamp(body)
	if err != nil {
		return out, err
	}
	serial, fields, err := auditTokens(body)
	if err != nil {
		return out, err
	}
	h := sha256.New()
	_, _ = h.Write([]byte("HOUFENG-G2-AUDIT-WIRE-V1\x00"))
	var typ [2]byte
	binary.LittleEndian.PutUint16(typ[:], kind)
	_, _ = h.Write(typ[:])
	_, _ = h.Write(body)
	out.Serial, out.Key, out.Kind, out.PayloadSHA256 = serial, key, kind, hex.EncodeToString(h.Sum(nil))
	switch kind {
	case auditBPFRecord:
		id, e := auditNumber(fields, "prog-id", 10, 32)
		if e != nil || id == 0 {
			return auditFact{}, errGuardProtocol
		}
		op := fields["op"]
		if len(fields) != 2 || (op != "LOAD" && op != "UNLOAD") {
			return auditFact{}, errGuardProtocol
		}
		out.ProgramID, out.Operation = id, op
	case auditSyscallRecord:
		if out.Arch, err = auditNumber(fields, "arch", 16, 32); err != nil {
			return auditFact{}, err
		}
		if out.Syscall, err = auditNumber(fields, "syscall", 10, 32); err != nil {
			return auditFact{}, err
		}
		if out.PID, err = auditNumber(fields, "pid", 10, 32); err != nil || out.PID == 0 {
			return auditFact{}, errGuardProtocol
		}
		if out.PPID, err = auditNumber(fields, "ppid", 10, 32); err != nil {
			return auditFact{}, err
		}
		if out.Argument0, err = auditNumber(fields, "a0", 16, 64); err != nil {
			return auditFact{}, err
		}
		success := fields["success"]
		if success != "yes" && success != "no" {
			return auditFact{}, errGuardProtocol
		}
		out.Success = success == "yes"
		result, err := strconv.ParseInt(fields["exit"], 10, 64)
		if err != nil || (out.Success && result < 0) || (!out.Success && result >= 0) {
			return auditFact{}, errGuardProtocol
		}
		out.Result = result
	case auditEOERecord:
		if len(fields) != 0 {
			return auditFact{}, errGuardProtocol
		}
	case auditConfigRecord:
		out.Fields = fields
	case auditSockaddrRecord, auditExecveRecord:
	default:
		return auditFact{}, errGuardProtocol
	}
	return out, nil
}

// receive returns the records in a multicast datagram in wire order. Loss,
// truncation and malformed framing fail the observation rather than being
// silently skipped. Multicast delivery alone is not a completeness proof.
func (a *auditMulticast) receive() ([]auditFact, error) {
	if a == nil || a.fd < 0 {
		return nil, errGuardProtocol
	}
	buf := make([]byte, 65536)
	oob := make([]byte, 256)
	n, on, flags, address, err := unix.Recvmsg(a.fd, buf, oob, 0)
	if err != nil {
		return nil, err
	}
	sa, ok := address.(*unix.SockaddrNetlink)
	if !ok || sa.Pid != 0 || on != 0 || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || n == 0 {
		return nil, errGuardProtocol
	}
	var facts []auditFact
	for offset := 0; offset < n; {
		if n-offset < 16 {
			return nil, errGuardProtocol
		}
		length := int(binary.LittleEndian.Uint32(buf[offset:]))
		kind := binary.LittleEndian.Uint16(buf[offset+4:])
		seq := binary.LittleEndian.Uint32(buf[offset+8:])
		if length < 16 || length > n-offset || seq != 0 {
			return nil, errGuardProtocol
		}
		if kind == unix.NLMSG_OVERRUN || kind == unix.NLMSG_ERROR {
			return nil, errGuardProtocol
		}
		if kind == auditBPFRecord || kind == auditSyscallRecord || kind == auditEOERecord || kind == auditConfigRecord || kind == auditSockaddrRecord || kind == auditExecveRecord {
			fact, parseErr := parseAuditFact(kind, buf[offset+16:offset+length])
			if parseErr != nil {
				return nil, parseErr
			}
			facts = append(facts, fact)
		}
		next := (length + 3) &^ 3
		if next > n-offset {
			if length != n-offset {
				return nil, errGuardProtocol
			}
			offset = n
		} else {
			offset += next
		}
	}
	return facts, nil
}

func auditReceiveAgain(err error) bool {
	return errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EWOULDBLOCK)
}
