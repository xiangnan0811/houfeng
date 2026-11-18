//go:build linux && !cgo

package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

type parentChannel struct {
	fd    int
	pidfd int
	pid   int
	peer  unix.Ucred
}

func openParent(ctx context.Context) (*parentChannel, error) {
	pid := os.Getppid()
	if pid <= 1 || ctx.Err() != nil {
		return nil, denied
	}
	var st unix.Stat_t
	kind, kindErr := unix.GetsockoptInt(6, unix.SOL_SOCKET, unix.SO_TYPE)
	family, familyErr := unix.GetsockoptInt(6, unix.SOL_SOCKET, unix.SO_DOMAIN)
	peer, peerErr := unix.GetsockoptUcred(6, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if unix.Fstat(6, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFSOCK || kindErr != nil ||
		kind != unix.SOCK_SEQPACKET || familyErr != nil || family != unix.AF_UNIX || peerErr != nil || peer == nil ||
		int(peer.Pid) != pid || peer.Uid != serviceUID || peer.Gid != serviceGID ||
		unix.SetsockoptInt(6, unix.SOL_SOCKET, unix.SO_PASSCRED, 1) != nil || unix.SetNonblock(6, true) != nil {
		return nil, denied
	}
	pidfd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		return nil, denied
	}
	unix.CloseOnExec(3)
	unix.CloseOnExec(4)
	unix.CloseOnExec(6)
	parent := &parentChannel{fd: 6, pidfd: pidfd, pid: pid, peer: *peer}
	if parent.alive(ctx) != nil {
		unix.Close(pidfd)
		return nil, denied
	}
	return parent, nil
}

func (parent *parentChannel) close() {
	_ = unix.Close(parent.fd)
	_ = unix.Close(parent.pidfd)
}

func (parent *parentChannel) alive(ctx context.Context) error {
	if ctx.Err() != nil || os.Getppid() != parent.pid {
		return denied
	}
	peer, err := unix.GetsockoptUcred(parent.fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || peer == nil || *peer != parent.peer {
		return denied
	}
	fds := []unix.PollFd{{Fd: int32(parent.pidfd), Events: unix.POLLIN}}
	count, err := unix.Poll(fds, 0)
	if err != nil || count != 0 || fds[0].Revents != 0 || os.Getppid() != parent.pid {
		return denied
	}
	return nil
}

// Reject a queued duplicate, a packet sent out of order, or a closed channel
// before sending the next request or accepting the completed exchange.
func (parent *parentChannel) quiet(ctx context.Context) error {
	if parent.alive(ctx) != nil {
		return denied
	}
	fds := []unix.PollFd{{Fd: int32(parent.fd), Events: unix.POLLIN | unix.POLLRDHUP}}
	count, err := unix.Poll(fds, 0)
	if err != nil || count != 0 || fds[0].Revents != 0 {
		return denied
	}
	return nil
}

func deadlineMillis(ctx context.Context) (int, error) {
	deadline, ok := ctx.Deadline()
	if !ok || ctx.Err() != nil {
		return 0, denied
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 0, denied
	}
	if remaining > 60*time.Second {
		remaining = 60 * time.Second
	}
	return int((remaining + time.Millisecond - 1) / time.Millisecond), nil
}

func (parent *parentChannel) wait(ctx context.Context, events int16) error {
	for {
		if parent.alive(ctx) != nil {
			return denied
		}
		timeout, err := deadlineMillis(ctx)
		if err != nil {
			return denied
		}
		fds := []unix.PollFd{
			{Fd: int32(parent.fd), Events: events | unix.POLLRDHUP},
			{Fd: int32(parent.pidfd), Events: unix.POLLIN},
		}
		_, err = unix.Poll(fds, timeout)
		if err == unix.EINTR {
			continue
		}
		if err != nil || ctx.Err() != nil || fds[1].Revents != 0 ||
			fds[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL|unix.POLLRDHUP) != 0 {
			return denied
		}
		if fds[0].Revents&events != 0 {
			return parent.alive(ctx)
		}
		if fds[0].Revents != 0 {
			return denied
		}
	}
}

func (parent *parentChannel) send(ctx context.Context, value any) error {
	if parent.quiet(ctx) != nil {
		return denied
	}
	body, err := json.Marshal(value)
	if err != nil || len(body) == 0 || len(body) > privateLimit {
		return denied
	}
	for {
		n, err := unix.SendmsgN(parent.fd, body, nil, nil, unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL)
		if err == unix.EINTR {
			continue
		}
		if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
			if parent.wait(ctx, unix.POLLOUT) != nil || parent.quiet(ctx) != nil {
				return denied
			}
			continue
		}
		if err != nil || n != len(body) || parent.alive(ctx) != nil {
			return denied
		}
		return nil
	}
}

func (parent *parentChannel) receive(ctx context.Context) ([]byte, error) {
	var body [privateLimit + 1]byte
	control := make([]byte, unix.CmsgSpace(unix.SizeofUcred)+unix.CmsgSpace(256*4))
	for {
		if parent.wait(ctx, unix.POLLIN) != nil {
			return nil, denied
		}
		n, oobn, flags, _, err := unix.Recvmsg(parent.fd, body[:], control, unix.MSG_DONTWAIT|unix.MSG_CMSG_CLOEXEC)
		if err == unix.EINTR || err == unix.EAGAIN || err == unix.EWOULDBLOCK {
			continue
		}
		if err != nil || oobn < 0 || oobn > len(control) {
			return nil, denied
		}
		messages, parseErr := unix.ParseSocketControlMessage(control[:oobn])
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
			actual, credentialErr := unix.ParseUnixCredentials(&message)
			if credentialErr != nil || actual == nil || *actual != parent.peer {
				valid = false
			}
			credentials++
		}
		if !valid || credentials != 1 || n <= 0 || n > privateLimit ||
			flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 || parent.alive(ctx) != nil {
			return nil, denied
		}
		return body[:n], nil
	}
}

func (parent *parentChannel) exchangeReady(ctx context.Context, phase uint64, backend backendIdentity) error {
	started := backend.started.UTC().Format(time.RFC3339Nano)
	request := struct {
		Op             string `json:"op"`
		Phase          uint64 `json:"phase_generation"`
		BackendPID     int32  `json:"backend_pid"`
		BackendStarted string `json:"backend_started"`
		Role           string `json:"role"`
	}{"ready", phase, backend.pid, started, backend.role}
	if parent.send(ctx, request) != nil {
		return denied
	}
	body, err := parent.receive(ctx)
	if err != nil {
		return denied
	}
	fields, err := strictObject(body, "op", "phase_generation", "backend_pid", "backend_started", "role")
	var op, actualStarted, role string
	var actualPhase uint64
	var pid int32
	if err != nil || json.Unmarshal(fields["op"], &op) != nil || op != "ready_ack" ||
		json.Unmarshal(fields["phase_generation"], &actualPhase) != nil || actualPhase != phase ||
		json.Unmarshal(fields["backend_pid"], &pid) != nil || pid != backend.pid ||
		json.Unmarshal(fields["backend_started"], &actualStarted) != nil || actualStarted != started ||
		json.Unmarshal(fields["role"], &role) != nil || role != backend.role || parent.quiet(ctx) != nil {
		return denied
	}
	return nil
}

func (parent *parentChannel) exchangeResult(ctx context.Context, phase uint64, manifest [32]byte) error {
	if manifest == ([32]byte{}) {
		return denied
	}
	digest := hex.EncodeToString(manifest[:])
	request := struct {
		Op     string `json:"op"`
		Phase  uint64 `json:"phase_generation"`
		Digest string `json:"manifest_digest"`
	}{"migration_result", phase, digest}
	if parent.send(ctx, request) != nil {
		return denied
	}
	body, err := parent.receive(ctx)
	if err != nil {
		return denied
	}
	fields, err := strictObject(body, "op", "phase_generation", "manifest_digest")
	var op, actualDigest string
	var actualPhase uint64
	if err != nil || json.Unmarshal(fields["op"], &op) != nil || op != "migration_result_ack" ||
		json.Unmarshal(fields["phase_generation"], &actualPhase) != nil || actualPhase != phase ||
		json.Unmarshal(fields["manifest_digest"], &actualDigest) != nil || actualDigest != digest || parent.quiet(ctx) != nil {
		return denied
	}
	return nil
}

// A private eventfd stops this bounded watcher without waiting for a sampling
// interval. Any parent/socket event during the transaction fails the call.
func (parent *parentChannel) watchDuringMigration(ctx context.Context, cancel context.CancelFunc) (func() error, error) {
	if parent.quiet(ctx) != nil {
		return nil, denied
	}
	stopfd, err := unix.Eventfd(0, unix.EFD_CLOEXEC|unix.EFD_NONBLOCK)
	if err != nil {
		return nil, denied
	}
	var stopping atomic.Bool
	done := make(chan error, 1)
	callbackDone := make(chan struct{})
	signal := func() {
		var body [8]byte
		binary.NativeEndian.PutUint64(body[:], 1)
		for {
			_, err := unix.Write(stopfd, body[:])
			if err != unix.EINTR {
				return
			}
		}
	}
	stopCancel := context.AfterFunc(ctx, func() {
		signal()
		close(callbackDone)
	})
	go func() {
		for {
			timeout, err := deadlineMillis(ctx)
			if err != nil {
				cancel()
				done <- denied
				return
			}
			fds := []unix.PollFd{
				{Fd: int32(parent.fd), Events: unix.POLLIN | unix.POLLRDHUP},
				{Fd: int32(parent.pidfd), Events: unix.POLLIN},
				{Fd: int32(stopfd), Events: unix.POLLIN},
			}
			_, err = unix.Poll(fds, timeout)
			if err == unix.EINTR {
				continue
			}
			if err != nil || ctx.Err() != nil || fds[0].Revents != 0 || fds[1].Revents != 0 ||
				fds[2].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
				cancel()
				done <- denied
				return
			}
			if fds[2].Revents&unix.POLLIN != 0 {
				if stopping.Load() {
					done <- nil
				} else {
					cancel()
					done <- denied
				}
				return
			}
		}
	}()
	return func() error {
		stopping.Store(true)
		signal()
		watchErr := <-done
		if !stopCancel() {
			<-callbackDone
		}
		_ = unix.Close(stopfd)
		if watchErr != nil || parent.quiet(ctx) != nil {
			return denied
		}
		return nil
	}, nil
}
