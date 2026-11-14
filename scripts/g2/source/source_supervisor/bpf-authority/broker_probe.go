//go:build linux && amd64 && !cgo

package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// ownerProbeObservation holds independent Docker attach and next-exit streams.
// Both are established against the registered daemon before the caller can
// start the container. Neither stream is allowed to outlive its grant.
type ownerProbeObservation struct {
	attachConn, waitConn net.Conn
	attachReader         *bufio.Reader
	waitResponse         *http.Response
	deadline             time.Time
}

func connectDocker(daemon *daemonRegistration, deadline time.Time) (net.Conn, error) {
	if daemon == nil || daemon.state != "executing" || daemon.pid <= 1 || !livePIDFD(daemon.pidfd) || time.Until(deadline) <= 0 {
		return nil, errGuardProtocol
	}
	var before, after unix.Stat_t
	if unix.Lstat("/run/docker.sock", &before) != nil || before.Mode&unix.S_IFMT != unix.S_IFSOCK || before.Uid != 0 || before.Gid != 10002 || before.Mode&0007 != 0 {
		return nil, errGuardProtocol
	}
	var parent unix.Stat_t
	if unix.Lstat("/run", &parent) != nil || parent.Mode&unix.S_IFMT != unix.S_IFDIR || parent.Uid != 0 || parent.Gid != 0 || parent.Mode&0022 != 0 {
		return nil, errGuardProtocol
	}
	dialer := net.Dialer{Deadline: deadline}
	conn, err := dialer.Dial("unix", "/run/docker.sock")
	if err != nil {
		return nil, err
	}
	valid := false
	defer func() {
		if !valid {
			_ = conn.Close()
		}
	}()
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return nil, errGuardProtocol
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return nil, err
	}
	var peer *unix.Ucred
	var peerErr error
	if err = raw.Control(func(fd uintptr) { peer, peerErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) }); err != nil || peerErr != nil || peer == nil || peer.Pid != int32(daemon.pid) || peer.Uid != 0 || peer.Gid != 0 {
		return nil, errGuardProtocol
	}
	if unix.Lstat("/run/docker.sock", &after) != nil || !sameTrustedStat(before, after) || !livePIDFD(daemon.pidfd) {
		return nil, errGuardProtocol
	}
	if err = conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	valid = true
	return conn, nil
}

func writeDockerRequest(conn net.Conn, path string, upgrade bool) error {
	if conn == nil || !strings.HasPrefix(path, "/v1.41/containers/") || strings.ContainsAny(path, "\r\n ") {
		return errGuardProtocol
	}
	connection := "close"
	extra := ""
	if upgrade {
		connection = "Upgrade"
		extra = "Upgrade: tcp\r\n"
	}
	message := []byte("POST " + path + " HTTP/1.1\r\nHost: localhost\r\nConnection: " + connection + "\r\n" + extra + "Content-Length: 0\r\n\r\n")
	for len(message) > 0 {
		n, err := conn.Write(message)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrUnexpectedEOF
		}
		message = message[n:]
	}
	return nil
}

func boundedDockerHeaders(reader *bufio.Reader) ([]byte, error) {
	raw, err, _ := boundedDockerHeadersDiagnostic(reader)
	return raw, err
}

// Keep the production rejection unchanged while retaining an actual read error
// for the caller's deferred diagnostic. Invalid framing has no read error.
func boundedDockerHeadersDiagnostic(reader *bufio.Reader) ([]byte, error, error) {
	var raw []byte
	for {
		line, err := reader.ReadSlice('\n')
		if err != nil {
			return nil, errGuardProtocol, err
		}
		if len(line) < 2 || line[len(line)-2] != '\r' || len(raw)+len(line) > 8192 {
			return nil, errGuardProtocol, nil
		}
		raw = append(raw, line...)
		if bytes.Equal(line, []byte("\r\n")) {
			return raw, nil, nil
		}
	}
}

func openOwnerProbeObservation(daemon *daemonRegistration, containerID string, deadline time.Time, diagnostic *admitDiagnostic) (_ *ownerProbeObservation, err error) {
	diagnostic.stage = "ADMIT_PROBE_OBSERVATION_INPUT"
	if !runtimeContainerID(containerID) {
		return nil, errGuardProtocol
	}
	o := &ownerProbeObservation{deadline: deadline}
	defer func() {
		if err != nil {
			o.close()
		}
	}()
	diagnostic.stage = "ADMIT_PROBE_ATTACH_CONNECT"
	o.attachConn, err = connectDocker(daemon, deadline)
	if err != nil {
		return nil, err
	}
	diagnostic.stage = "ADMIT_PROBE_ATTACH_SEND"
	if err = writeDockerRequest(o.attachConn, "/v1.41/containers/"+containerID+"/attach?stream=1&stdout=1&stderr=1", true); err != nil {
		return nil, err
	}
	o.attachReader = bufio.NewReaderSize(o.attachConn, 8192)
	diagnostic.stage = "ADMIT_PROBE_ATTACH_HEADERS"
	headers, err, observed := boundedDockerHeadersDiagnostic(o.attachReader)
	diagnostic.observed = observed
	if err != nil {
		return nil, err
	}
	diagnostic.stage = "ADMIT_PROBE_ATTACH_RESPONSE"
	response, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(headers)), &http.Request{Method: "POST"})
	if err != nil {
		return nil, err
	}
	if response.StatusCode != 101 && response.StatusCode != 200 {
		_ = response.Body.Close()
		return nil, errGuardProtocol
	}
	// This is the raw upgraded Docker multiplexed stream, not an HTTP entity.
	if response.StatusCode == 200 && (response.ContentLength > 0 || len(response.TransferEncoding) != 0) {
		_ = response.Body.Close()
		return nil, errGuardProtocol
	}
	diagnostic.stage = "ADMIT_PROBE_WAIT_CONNECT"
	o.waitConn, err = connectDocker(daemon, deadline)
	if err != nil {
		return nil, err
	}
	diagnostic.stage = "ADMIT_PROBE_WAIT_SEND"
	if err = writeDockerRequest(o.waitConn, "/v1.41/containers/"+containerID+"/wait?condition=next-exit", false); err != nil {
		return nil, err
	}
	reader := bufio.NewReaderSize(o.waitConn, 8192)
	diagnostic.stage = "ADMIT_PROBE_WAIT_HEADERS"
	headers, err, observed = boundedDockerHeadersDiagnostic(reader)
	diagnostic.observed = observed
	if err != nil {
		return nil, err
	}
	diagnostic.stage = "ADMIT_PROBE_WAIT_RESPONSE"
	o.waitResponse, err = http.ReadResponse(bufio.NewReader(io.MultiReader(bytes.NewReader(headers), reader)), &http.Request{Method: "POST"})
	if err != nil {
		return nil, err
	}
	diagnostic.stage = "ADMIT_PROBE_WAIT_STATUS"
	if o.waitResponse.StatusCode != 200 {
		return nil, errGuardProtocol
	}
	// Moby v29.8.2 installs the next-exit waiter before flushing this response
	// header. A created container is not considered to have exited yet.
	return o, nil
}

func (o *ownerProbeObservation) close() {
	if o == nil {
		return
	}
	if o.waitResponse != nil && o.waitResponse.Body != nil {
		_ = o.waitResponse.Body.Close()
		o.waitResponse = nil
	}
	if o.attachConn != nil {
		_ = o.attachConn.Close()
		o.attachConn = nil
	}
	if o.waitConn != nil {
		_ = o.waitConn.Close()
		o.waitConn = nil
	}
}

func (o *ownerProbeObservation) finish(actualStartObserved bool) (uint32, error) {
	if o == nil || !actualStartObserved || o.attachReader == nil || o.waitResponse == nil || time.Until(o.deadline) <= 0 {
		return 0, errGuardProtocol
	}
	stream, err := io.ReadAll(io.LimitReader(o.attachReader, 8193))
	if err != nil || len(stream) > 8192 {
		return 0, errGuardProtocol
	}
	var stdout []byte
	for len(stream) != 0 {
		if len(stream) < 8 || stream[0] != 1 || !bytes.Equal(stream[1:4], []byte{0, 0, 0}) {
			return 0, errGuardProtocol
		}
		n := binary.BigEndian.Uint32(stream[4:8])
		if uint64(n) > uint64(len(stream)-8) {
			return 0, errGuardProtocol
		}
		stdout = append(stdout, stream[8:8+int(n)]...)
		stream = stream[8+int(n):]
	}
	text := strings.TrimSpace(string(stdout))
	if text == "" || len(text) > 10 {
		return 0, errGuardProtocol
	}
	for _, c := range text {
		if c < '0' || c > '9' {
			return 0, errGuardProtocol
		}
	}
	value, err := strconv.ParseUint(text, 10, 31)
	if err != nil || value == 0 || value == 10002 {
		return 0, errGuardProtocol
	}
	waitBody, err := io.ReadAll(io.LimitReader(o.waitResponse.Body, 8193))
	if err != nil || len(waitBody) > 8192 {
		return 0, errGuardProtocol
	}
	decoder := json.NewDecoder(bytes.NewReader(waitBody))
	decoder.UseNumber()
	decoded, err := readJSONValue(decoder, 0)
	if err != nil {
		return 0, err
	}
	if _, err = decoder.Token(); err != io.EOF {
		return 0, errGuardProtocol
	}
	result, ok := decoded.(map[string]any)
	if !ok || !exactKeys(result, "StatusCode") || result["StatusCode"] != json.Number("0") {
		return 0, fmt.Errorf("BPF_AUTH_UNAVAILABLE: probe wait did not settle successfully")
	}
	return uint32(value), nil
}
