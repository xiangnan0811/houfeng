//go:build linux

package deploy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const ledgerCallbackFrameLimit = 16 << 10

var errLedgerInventoryUnavailable = errors.New("C80 copy inventory unavailable")

// Each endpoint has one request owner. Kernel credentials are checked on every
// packet; SO_PEERCRED alone cannot authenticate a child inheriting a socketpair.
type ledgerCallbackChannel struct {
	mu   sync.Mutex
	conn *net.UnixConn
	peer unix.Ucred
}

func ledgerCallbackSocket(fd int, peer unix.Ucred) (*ledgerCallbackChannel, error) {
	kind, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TYPE)
	if err != nil || kind != unix.SOCK_SEQPACKET || peer.Pid <= 0 {
		return nil, ErrRecordLedgerFence
	}
	copyFD, err := unix.FcntlInt(uintptr(fd), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, ErrRecordLedgerFence
	}
	file := os.NewFile(uintptr(copyFD), "ledger-callback")
	if err := unix.SetsockoptInt(copyFD, unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
		file.Close()
		return nil, ErrRecordLedgerFence
	}
	conn, err := net.FileConn(file)
	file.Close()
	if err != nil {
		return nil, ErrRecordLedgerFence
	}
	packet, ok := conn.(*net.UnixConn)
	if !ok {
		conn.Close()
		return nil, ErrRecordLedgerFence
	}
	return &ledgerCallbackChannel{conn: packet, peer: peer}, nil
}

func (channel *ledgerCallbackChannel) close() {
	if channel != nil && channel.conn != nil {
		channel.conn.Close()
	}
}

func ledgerCallbackFields(body []byte, names ...string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	first, err := decoder.Token()
	if err != nil || first != json.Delim('{') {
		return nil, ErrRecordLedgerFence
	}
	allowed := make(map[string]bool, len(names))
	for _, name := range names {
		allowed[name] = true
	}
	fields := make(map[string]json.RawMessage, len(names))
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		if err != nil || !ok || !allowed[name] || fields[name] != nil {
			return nil, ErrRecordLedgerFence
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil, ErrRecordLedgerFence
		}
		fields[name] = value
	}
	last, err := decoder.Token()
	if err != nil || last != json.Delim('}') || len(fields) != len(names) {
		return nil, ErrRecordLedgerFence
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, ErrRecordLedgerFence
	}
	return fields, nil
}

func ledgerCallbackDecode(fields map[string]json.RawMessage, key string, value any) error {
	body, ok := fields[key]
	if !ok || bytes.Equal(bytes.TrimSpace(body), []byte("null")) || json.Unmarshal(body, value) != nil {
		return ErrRecordLedgerFence
	}
	return nil
}

func ledgerCallbackDigest(fields map[string]json.RawMessage, key string) ([32]byte, error) {
	var result [32]byte
	var value string
	if ledgerCallbackDecode(fields, key, &value) != nil || len(value) != 64 {
		return result, ErrRecordLedgerFence
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || hex.EncodeToString(decoded) != value {
		return result, ErrRecordLedgerFence
	}
	copy(result[:], decoded)
	if result == ([32]byte{}) {
		return result, ErrRecordLedgerFence
	}
	return result, nil
}

func (channel *ledgerCallbackChannel) deadline(ctx context.Context) (func(), error) {
	if ctx == nil || ctx.Err() != nil {
		return nil, ErrRecordLedgerFence
	}
	deadline, ok := ctx.Deadline()
	if !ok || channel == nil || channel.conn == nil {
		return nil, ErrRecordLedgerFence
	}
	if channel.conn.SetDeadline(deadline) != nil {
		return nil, ErrRecordLedgerFence
	}
	stop := context.AfterFunc(ctx, func() { _ = channel.conn.SetDeadline(time.Now()) })
	return func() { stop() }, nil
}

func (channel *ledgerCallbackChannel) send(ctx context.Context, value any) error {
	stop, err := channel.deadline(ctx)
	if err != nil {
		return err
	}
	defer stop()
	body, err := json.Marshal(value)
	if err != nil || len(body) == 0 || len(body) > ledgerCallbackFrameLimit {
		return ErrRecordLedgerFence
	}
	n, control, err := channel.conn.WriteMsgUnix(body, nil, nil)
	if err != nil || n != len(body) || control != 0 {
		return ErrRecordLedgerFence
	}
	return nil
}

func (channel *ledgerCallbackChannel) receive(ctx context.Context) ([]byte, error) {
	stop, err := channel.deadline(ctx)
	if err != nil {
		return nil, err
	}
	defer stop()
	var body [ledgerCallbackFrameLimit + 1]byte
	control := make([]byte, unix.CmsgSpace(unix.SizeofUcred)+unix.CmsgSpace(16*4))
	n, oob, flags, _, readErr := channel.conn.ReadMsgUnix(body[:], control)
	messages, parseErr := unix.ParseSocketControlMessage(control[:oob])
	valid, credentials := parseErr == nil, 0
	for _, message := range messages {
		if message.Header.Level == unix.SOL_SOCKET && message.Header.Type == unix.SCM_RIGHTS {
			fds, _ := unix.ParseUnixRights(&message)
			for _, fd := range fds {
				_ = unix.Close(fd)
			}
			valid = false
			continue
		}
		if message.Header.Level != unix.SOL_SOCKET || message.Header.Type != unix.SCM_CREDENTIALS {
			valid = false
			continue
		}
		actual, err := unix.ParseUnixCredentials(&message)
		if err != nil || actual == nil || *actual != channel.peer {
			valid = false
		}
		credentials++
	}
	if errors.Is(readErr, io.EOF) && n == 0 && oob == 0 && flags == 0 {
		return nil, io.EOF
	}
	if readErr != nil || !valid || credentials != 1 || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || n == 0 || n > ledgerCallbackFrameLimit {
		return nil, ErrRecordLedgerFence
	}
	return body[:n], nil
}

type ledgerRealmObservation struct{ Realm, Roots [32]byte }

type ledgerRealmChannel struct {
	channel  *ledgerCallbackChannel
	pidfd    int
	sequence uint64
	invalid  bool
}

func ledgerOpenRealm(fd int) (*ledgerRealmChannel, error) {
	peer, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || peer == nil || peer.Pid <= 0 || peer.Uid != 0 || peer.Gid != 0 {
		return nil, ErrRecordLedgerFence
	}
	pidfd, err := unix.PidfdOpen(int(peer.Pid), 0)
	if err != nil {
		return nil, ErrRecordLedgerFence
	}
	channel, err := ledgerCallbackSocket(fd, *peer)
	if err != nil {
		unix.Close(pidfd)
		return nil, err
	}
	return &ledgerRealmChannel{channel: channel, pidfd: pidfd}, nil
}

func (realm *ledgerRealmChannel) close() {
	if realm != nil {
		realm.channel.close()
		_ = unix.Close(realm.pidfd)
	}
}

func ledgerHeldProcessLive(pidfd int) bool {
	fds := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
	n, err := unix.Poll(fds, 0)
	return err == nil && n == 0 && fds[0].Revents == 0
}

func (realm *ledgerRealmChannel) observe(ctx context.Context, generation uint64) (result ledgerRealmObservation, resultErr error) {
	defer func() {
		if resultErr != nil {
			if realm != nil {
				realm.invalid = true
			}
			resultErr = errors.Join(resultErr, errLedgerPredecessorRealm)
		}
	}()
	if realm == nil || generation == 0 {
		return result, ErrRecordLedgerFence
	}
	realm.channel.mu.Lock()
	defer realm.channel.mu.Unlock()
	if realm.invalid || !ledgerHeldProcessLive(realm.pidfd) {
		return result, errLedgerInventoryUnavailable
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return result, ErrRecordLedgerFence
	}
	nonceText := hex.EncodeToString(nonce[:])
	if err := realm.channel.send(ctx, map[string]any{"version": 1, "op": "realm_observe", "phase_generation": generation, "nonce": nonceText}); err != nil {
		return result, err
	}
	body, err := realm.channel.receive(ctx)
	if err != nil {
		return result, err
	}
	fields, err := ledgerCallbackFields(body, "version", "op", "phase_generation", "nonce", "observation_sequence", "realm_digest", "roots_digest", "valid")
	if err != nil {
		return result, err
	}
	var version, phase, sequence uint64
	var op, responseNonce string
	var valid bool
	if ledgerCallbackDecode(fields, "version", &version) != nil || version != 1 || ledgerCallbackDecode(fields, "op", &op) != nil || op != "realm_observe" || ledgerCallbackDecode(fields, "phase_generation", &phase) != nil || phase != generation || ledgerCallbackDecode(fields, "nonce", &responseNonce) != nil || responseNonce != nonceText || ledgerCallbackDecode(fields, "observation_sequence", &sequence) != nil || sequence <= realm.sequence || ledgerCallbackDecode(fields, "valid", &valid) != nil || !ledgerHeldProcessLive(realm.pidfd) {
		return result, ErrRecordLedgerFence
	}
	realm.sequence = sequence
	if !valid {
		realm.invalid = true
		if !bytes.Equal(bytes.TrimSpace(fields["realm_digest"]), []byte("null")) || !bytes.Equal(bytes.TrimSpace(fields["roots_digest"]), []byte("null")) {
			return result, ErrRecordLedgerFence
		}
		return result, errLedgerInventoryUnavailable
	}
	result.Realm, err = ledgerCallbackDigest(fields, "realm_digest")
	if err != nil {
		return ledgerRealmObservation{}, err
	}
	result.Roots, err = ledgerCallbackDigest(fields, "roots_digest")
	if err != nil {
		return ledgerRealmObservation{}, err
	}
	return result, nil
}
