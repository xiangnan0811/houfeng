//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

const maxControlFrame = 16 * 1024

var errControlFrame = errors.New("BPF_AUTH_DENIED: malformed control frame")

// readJSONValue checks duplicate members at every nesting depth and keeps
// numbers as decimal text so neither float rounding nor float acceptance can
// alter a principal, counter, or evidence record.
func readJSONValue(d *json.Decoder, depth int) (any, error) {
	if depth > 64 {
		return nil, errControlFrame
	}
	t, err := d.Token()
	if err != nil {
		return nil, errControlFrame
	}
	switch v := t.(type) {
	case nil, bool, string:
		return v, nil
	case json.Number:
		s := string(v)
		if strings.ContainsAny(s, ".eE+") || s == "-0" {
			return nil, errControlFrame
		}
		if strings.HasPrefix(s, "-") {
			if _, err := strconv.ParseInt(s, 10, 64); err != nil {
				return nil, errControlFrame
			}
		} else if _, err := strconv.ParseUint(s, 10, 64); err != nil {
			return nil, errControlFrame
		}
		return v, nil
	case json.Delim:
		switch v {
		case '{':
			m := make(map[string]any)
			for d.More() {
				k, err := d.Token()
				if err != nil {
					return nil, errControlFrame
				}
				key, ok := k.(string)
				if !ok {
					return nil, errControlFrame
				}
				if _, exists := m[key]; exists {
					return nil, errControlFrame
				}
				value, err := readJSONValue(d, depth+1)
				if err != nil {
					return nil, err
				}
				m[key] = value
			}
			end, err := d.Token()
			if err != nil || end != json.Delim('}') {
				return nil, errControlFrame
			}
			return m, nil
		case '[':
			a := make([]any, 0)
			for d.More() {
				value, err := readJSONValue(d, depth+1)
				if err != nil {
					return nil, err
				}
				a = append(a, value)
			}
			end, err := d.Token()
			if err != nil || end != json.Delim(']') {
				return nil, errControlFrame
			}
			return a, nil
		}
	}
	return nil, errControlFrame
}

func appendJSONString(b *bytes.Buffer, s string, limit int) error {
	if !utf8.ValidString(s) || b.Len() >= limit {
		return errControlFrame
	}
	b.WriteByte('"')
	for _, r := range s {
		if r >= 0x20 && r < 0x7f && r != '"' && r != '\\' {
			if b.Len() >= limit {
				return errControlFrame
			}
			b.WriteByte(byte(r))
			continue
		}
		size := 2
		switch r {
		case '"', '\\', '\b', '\f', '\n', '\r', '\t':
		default:
			size = 6
			if r > 0xffff {
				size = 12
			}
		}
		if size > limit-b.Len() {
			return errControlFrame
		}
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteByte(byte(r))
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r <= 0xffff {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				a, c := utf16.EncodeRune(r)
				fmt.Fprintf(b, `\u%04x\u%04x`, a, c)
			}
		}
	}
	if b.Len() >= limit {
		return errControlFrame
	}
	b.WriteByte('"')
	return nil
}

func appendCanonical(b *bytes.Buffer, value any, depth, limit int) error {
	if depth > 64 || b.Len() > limit {
		return errControlFrame
	}
	switch v := value.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if v {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case string:
		return appendJSONString(b, v, limit)
	case json.Number:
		s := string(v)
		if strings.ContainsAny(s, ".eE+") || s == "-0" {
			return errControlFrame
		}
		if strings.HasPrefix(s, "-") {
			if _, err := strconv.ParseInt(s, 10, 64); err != nil {
				return errControlFrame
			}
		} else if _, err := strconv.ParseUint(s, 10, 64); err != nil {
			return errControlFrame
		}
		b.WriteString(s)
	case []any:
		b.WriteByte('[')
		for i, x := range v {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := appendCanonical(b, x, depth+1, limit); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := appendJSONString(b, k, limit); err != nil {
				return err
			}
			b.WriteByte(':')
			if err := appendCanonical(b, v[k], depth+1, limit); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return errControlFrame
	}
	if b.Len() > limit {
		return errControlFrame
	}
	return nil
}

func canonicalWithLimit(value any, limit int) ([]byte, error) {
	var b bytes.Buffer
	if err := appendCanonical(&b, value, 0, limit); err != nil {
		return nil, err
	}
	if b.Len() == 0 || b.Len() > limit {
		return nil, errControlFrame
	}
	return b.Bytes(), nil
}

func canonicalControl(value any) ([]byte, error) {
	return canonicalWithLimit(value, maxControlFrame)
}

func parseCanonicalControl(raw []byte) (map[string]any, error) {
	if len(raw) == 0 || len(raw) > maxControlFrame || !utf8.Valid(raw) {
		return nil, errControlFrame
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	v, err := readJSONValue(d, 0)
	if err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, errControlFrame
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errControlFrame
	}
	encoded, err := canonicalControl(m)
	if err != nil || !bytes.Equal(raw, encoded) {
		return nil, errControlFrame
	}
	return m, nil
}

func transcriptHash(record map[string]any) (string, error) {
	body, err := canonicalControl(record)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, _ = h.Write([]byte("HOUFENG-G2-BPF-V1\x00"))
	_, _ = h.Write(body)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// controlReceiveObservation records only facts returned by recvmsg and the
// subsequent validation. It never retains a received descriptor.
type controlReceiveObservation struct {
	Bytes            int     `json:"bytes"`
	PacketSHA256     *string `json:"packet_sha256"`
	Truncated        bool    `json:"msg_trunc"`
	ControlTruncated bool    `json:"msg_ctrunc"`
	PID              *int32  `json:"pid"`
	UID              *uint32 `json:"uid"`
	GID              *uint32 `json:"gid"`
	RightsCount      int     `json:"rights_count"`
	ClosedCount      int     `json:"closed_count"`
	Stage            string  `json:"validation_stage"`
	Errno            int     `json:"errno"`
}

// receiveControl consumes a single seqpacket and closes rejected descriptors.
func receiveControl(fd, expectedFDs int) ([]byte, *unix.Ucred, []int, error) {
	data, cred, fds, _, err := receiveControlObserved(fd, expectedFDs)
	return data, cred, fds, err
}

func receiveControlObserved(fd, expectedRights int) ([]byte, *unix.Ucred, []int, controlReceiveObservation, error) {
	observation := controlReceiveObservation{Stage: "arguments"}
	if expectedRights < -1 || expectedRights > 4 {
		return nil, nil, nil, observation, errControlFrame
	}
	data := make([]byte, maxControlFrame+1)
	oob := make([]byte, unix.CmsgSpace(4*16)+unix.CmsgSpace(unix.SizeofUcred))
	observation.Stage = "recvmsg"
	n, on, flags, _, err := unix.Recvmsg(fd, data, oob, unix.MSG_CMSG_CLOEXEC)
	if err != nil {
		var errno syscall.Errno
		if errors.As(err, &errno) {
			observation.Errno = int(errno)
		}
		return nil, nil, nil, observation, err
	}
	observation.Bytes = n
	observation.Truncated = flags&unix.MSG_TRUNC != 0
	observation.ControlTruncated = flags&unix.MSG_CTRUNC != 0
	if n > 0 && !observation.Truncated {
		sum := sha256.Sum256(data[:n])
		digest := hex.EncodeToString(sum[:])
		observation.PacketSHA256 = &digest
	}
	var fds []int
	var cred *unix.Ucred
	valid := n > 0 && n <= maxControlFrame && !observation.Truncated && !observation.ControlTruncated
	observation.Stage = "control_messages"
	messages, parseErr := unix.ParseSocketControlMessage(oob[:on])
	if parseErr != nil {
		valid = false
	}
	for _, msg := range messages {
		switch {
		case msg.Header.Level == unix.SOL_SOCKET && msg.Header.Type == unix.SCM_RIGHTS:
			rights, e := unix.ParseUnixRights(&msg)
			if e != nil {
				valid = false
			} else {
				fds = append(fds, rights...)
			}
		case msg.Header.Level == unix.SOL_SOCKET && msg.Header.Type == unix.SCM_CREDENTIALS:
			c, e := unix.ParseUnixCredentials(&msg)
			if e != nil || cred != nil {
				valid = false
			} else {
				cred = c
			}
		default:
			valid = false
		}
	}
	observation.RightsCount = len(fds)
	if cred != nil && !observation.ControlTruncated {
		pid, uid, gid := cred.Pid, cred.Uid, cred.Gid
		observation.PID, observation.UID, observation.GID = &pid, &uid, &gid
	}
	observation.Stage = "validation"
	if cred == nil || len(fds) > 4 || (expectedRights >= 0 && len(fds) != expectedRights) {
		valid = false
	}
	if !valid {
		for _, received := range fds {
			if unix.Close(received) == nil {
				observation.ClosedCount++
			}
		}
		return nil, nil, nil, observation, errControlFrame
	}
	observation.Stage = "accepted"
	return data[:n], cred, fds, observation, nil
}

func sendControl(fd int, payload []byte, fds []int) error {
	if len(payload) == 0 || len(payload) > maxControlFrame || len(fds) > 4 {
		return errControlFrame
	}
	var oob []byte
	if len(fds) > 0 {
		oob = unix.UnixRights(fds...)
	}
	n, err := unix.SendmsgN(fd, payload, oob, nil, unix.MSG_NOSIGNAL)
	if err != nil {
		return err
	}
	if n != len(payload) {
		return io.ErrShortWrite
	}
	return nil
}

func handoffFrame(payload []byte) ([]byte, error) {
	if len(payload) == 0 || len(payload) > maxControlFrame {
		return nil, errControlFrame
	}
	frame := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(frame, uint32(len(payload)))
	copy(frame[4:], payload)
	return frame, nil
}
