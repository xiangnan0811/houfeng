//go:build linux && amd64 && !cgo

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const authorityRunToken = "ddac5da9673f4c9ba7d2ad68570fd82d"

func procStartTicks(pid int) (uint64, error) {
	data, err := readHeldProc(fmt.Sprintf("%d/stat", pid), 65536)
	if err != nil || len(data) == 0 || len(data) > 65536 {
		if g2d1.role == 'L' && g2d1.scope != "" {
			g2d1Fail(g2d1.scope, err)
		}
		return 0, errGuardProtocol
	}
	end := strings.LastIndex(string(data), ") ")
	if end < 0 {
		return 0, errGuardProtocol
	}
	fields := strings.Fields(string(data[end+2:]))
	if len(fields) <= 19 {
		return 0, errGuardProtocol
	}
	value, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || value == 0 {
		return 0, errGuardProtocol
	}
	return value, nil
}

func procExecutableHash(pid int) (string, error) {
	name := fmt.Sprintf("%d/exe", pid)
	fd, err := unix.Openat(heldProcDirectory, name, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", err
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		if g2d1.role == 'L' {
			g2d1Fail("PEER_SELF_HASH", nil)
		}
		_ = unix.Close(fd)
		return "", errGuardProtocol
	}
	defer file.Close()
	var before, after unix.Stat_t
	if e := unix.Fstat(fd, &before); e != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Size <= 0 || before.Size > 128<<20 {
		if g2d1.role == 'L' {
			g2d1Fail("PEER_SELF_HASH", e)
		}
		return "", errGuardProtocol
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(file, (128<<20)+1))
	if err != nil || n != before.Size {
		if g2d1.role == 'L' {
			g2d1Fail("PEER_SELF_HASH", err)
		}
		return "", errGuardProtocol
	}
	if e := unix.Fstat(fd, &after); e != nil || !sameTrustedStat(before, after) {
		if g2d1.role == 'L' {
			g2d1Fail("PEER_SELF_HASH", e)
		}
		return "", errGuardProtocol
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fixedBootID() (string, error) {
	raw, err := readHeldProc("sys/kernel/random/boot_id", 64)
	if err != nil {
		if g2d1.role == 'L' {
			g2d1Fail("BOOT_ID", err)
		}
		return "", errGuardProtocol
	}
	s := strings.TrimSpace(string(raw))
	if len(s) != 36 {
		return "", errGuardProtocol
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return "", errGuardProtocol
			}
			continue
		}
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return "", errGuardProtocol
		}
	}
	return s, nil
}

func waitFD(fd int, events int16, deadline time.Time) error {
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return unix.ETIMEDOUT
		}
		ms := int((remaining + time.Millisecond - 1) / time.Millisecond)
		p := []unix.PollFd{{Fd: int32(fd), Events: events}}
		n, err := unix.Poll(p, ms)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return unix.ETIMEDOUT
		}
		if p[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			return errGuardProtocol
		}
		if p[0].Revents&events != 0 {
			return nil
		}
	}
}

func openSeedCarrier() (int, int, error) {
	dir, err := unix.Open("/mnt/g2-seed", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, -1, err
	}
	var st unix.Stat_t
	var fs unix.Statfs_t
	statErr := unix.Fstat(dir, &st)
	if statErr != nil || st.Uid != 0 || st.Gid != 0 || st.Mode&0022 != 0 {
		g2d1Fail("CARRIER_DIR", statErr)
		_ = unix.Close(dir)
		return -1, -1, errGuardProtocol
	}
	statErr = unix.Fstatfs(dir, &fs)
	if statErr != nil || fs.Flags&unix.ST_RDONLY == 0 {
		g2d1Fail("CARRIER_FS", statErr)
		_ = unix.Close(dir)
		return -1, -1, errGuardProtocol
	}
	caseFD, err := unix.Openat(dir, "case.json", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		g2d1Fail("CARRIER_CASE_OPEN", err)
		_ = unix.Close(dir)
		return -1, -1, err
	}
	if statErr = unix.Fstat(caseFD, &st); statErr != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != 0 || st.Gid != 0 || st.Mode&0022 != 0 || st.Size <= 0 || st.Size > maxControlFrame {
		g2d1Fail("CARRIER_CASE_STAT", statErr)
		_ = unix.Close(caseFD)
		_ = unix.Close(dir)
		return -1, -1, errGuardProtocol
	}
	return dir, caseFD, nil
}

func connectBroker(boot string, deadline time.Time) (_ int, _ int, err error) {
	stage := "PEER_SOCKET"
	defer func() {
		if err != nil {
			g2d1Fail(stage, err)
		}
	}()
	fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
	if err != nil {
		return -1, -1, err
	}
	ok := false
	defer func() {
		if !ok {
			if err != nil {
				g2d1Fail(stage, err)
			}
			_ = unix.Close(fd)
		}
	}()
	stage = "PEER_PASSCRED"
	if err = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
		return -1, -1, err
	}
	stage = "PEER_CONNECT"
	err = unix.Connect(fd, &unix.SockaddrUnix{Name: "\x00g2-bpf-control-" + boot})
	if err == unix.EINPROGRESS || err == unix.EAGAIN {
		if err = waitFD(fd, unix.POLLOUT, deadline); err != nil {
			return -1, -1, err
		}
		value, e := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_ERROR)
		if e != nil || value != 0 {
			if e != nil {
				g2d1Fail("PEER_CONNECT_RESULT", e)
			} else {
				g2d1Fail("PEER_CONNECT_RESULT", syscall.Errno(value))
			}
			return -1, -1, errGuardProtocol
		}
	} else if err != nil {
		return -1, -1, err
	}
	stage = "PEER_CRED"
	peer, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || peer.Pid <= 1 || peer.Uid != 0 || peer.Gid != 0 {
		g2d1Fail(stage, err)
		return -1, -1, errGuardProtocol
	}
	stage = "PEER_PIDFD"
	pidfd, err := unix.PidfdOpen(int(peer.Pid), 0)
	if err != nil {
		return -1, -1, err
	}
	keepPID := false
	defer func() {
		if !keepPID {
			if err != nil {
				g2d1Fail(stage, err)
			}
			_ = unix.Close(pidfd)
		}
	}()
	stage = "PEER_START"
	g2d1.scope = stage
	start, err := procStartTicks(int(peer.Pid))
	if err != nil {
		return -1, -1, err
	}
	stage = "PEER_SELF_HASH"
	selfHash, err := procExecutableHash(os.Getpid())
	if err != nil {
		return -1, -1, err
	}
	stage = "PEER_EXE"
	if err = hashNotifyingExecutable(uint32(peer.Pid), selfHash); err != nil {
		return -1, -1, err
	}
	stage = "PEER_RECHECK"
	g2d1.scope = stage
	after, err := procStartTicks(int(peer.Pid))
	if err != nil || after != start || !livePIDFD(pidfd) {
		g2d1Fail(stage, err)
		return -1, -1, errGuardProtocol
	}
	ok, keepPID = true, true
	return fd, pidfd, nil
}

func launcherRegister(fd, brokerPIDFD int, boot, kind string, child int, start uint64, rights []int, deadline time.Time) (_ []byte, err error) {
	stage := "REGISTER_ENCODE"
	defer func() {
		if err != nil {
			g2d1Fail(stage, err)
		}
	}()
	request := map[string]any{"version": json.Number("1"), "boot_id": boot, "request_id": json.Number("1"), "operation": "REGISTER_LAUNCH", "body": map[string]any{"kind": kind, "child_pid": jsonU64(uint64(child)), "child_starttime": jsonU64(start)}}
	raw, err := canonicalControl(request)
	if err != nil {
		return nil, err
	}
	stage = "REGISTER_SEND_WAIT"
	if err = waitFD(fd, unix.POLLOUT, deadline); err != nil {
		return nil, err
	}
	stage = "REGISTER_SEND"
	if err = sendControl(fd, raw, rights); err != nil {
		return nil, err
	}
	if kind == "source" {
		g2d1Mark("REGISTER_SEND_OK")
	}
	stage = "ACK_WAIT"
	if err = waitFD(fd, unix.POLLIN, deadline); err != nil {
		return nil, err
	}
	stage = "ACK_RECV"
	response, cred, received, err := receiveControl(fd, 0)
	if err != nil {
		return nil, err
	}
	for _, extra := range received {
		_ = unix.Close(extra)
	}
	stage = "ACK_PEER"
	peer, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || cred == nil || peer.Pid != cred.Pid || peer.Uid != cred.Uid || peer.Gid != cred.Gid || cred.Uid != 0 || cred.Gid != 0 || !livePIDFD(brokerPIDFD) {
		g2d1Fail(stage, err)
		return nil, errGuardProtocol
	}
	stage = "ACK_PARSE"
	ack, err := parseCanonicalControl(response)
	if err != nil {
		return nil, err
	}
	stage = "ACK_CHECK"
	if !exactKeys(ack, "version", "boot_id", "epoch", "run_token", "principal_id", "request_id", "request_sha256", "status", "body") || ack["version"] != json.Number("1") || ack["boot_id"] != boot || ack["run_token"] != authorityRunToken || ack["request_id"] != json.Number("1") || ack["status"] != "OK" {
		return nil, errControlFrame
	}
	digest := sha256.Sum256(raw)
	if ack["request_sha256"] != hex.EncodeToString(digest[:]) {
		return nil, errControlFrame
	}
	principal, ok := exactNumber(ack["principal_id"])
	if !ok || principal == 0 {
		return nil, errControlFrame
	}
	epoch, ok := ack["epoch"].(string)
	if !ok || len(epoch) != 32 || epoch == strings.Repeat("0", 32) {
		return nil, errControlFrame
	}
	for _, c := range epoch {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return nil, errControlFrame
		}
	}
	body, ok := ack["body"].(map[string]any)
	if !ok || !exactKeys(body, "child_cookie", "role") {
		return nil, errControlFrame
	}
	cookie, ok := exactNumber(body["child_cookie"])
	if !ok || cookie == 0 {
		return nil, errControlFrame
	}
	role := "BOOT_OWNER"
	if kind == "reboot" {
		role = "RECOVERY_PENDING"
	}
	if body["role"] != role {
		return nil, errControlFrame
	}
	if kind == "source" {
		g2d1Mark("ACK_OK")
	}
	return response, nil
}

func runSourceLauncher(kind string) (_ int, err error) {
	stage := "DEADLINE"
	defer func() {
		if err != nil {
			g2d1Fail(stage, err)
		}
	}()
	deadline, err := guestDeadline(60 * time.Second)
	if err != nil {
		return 1, err
	}
	stage = "ROLE"
	runtime.LockOSThread()
	if unix.Gettid() != os.Getpid() || os.Geteuid() != 0 || len(os.Args) != 2 || (kind != "source" && kind != "reboot") {
		return 1, errGuardProtocol
	}
	expectedPath, script := "/opt/g2-launch/g2-bpf-authority", "/run/g2/source_guest.py"
	if kind == "reboot" {
		expectedPath, script = "/opt/g2/g2-bpf-authority", "/opt/g2/realm-supervisor/reboot_observer.py"
	}
	if os.Args[0] != expectedPath || os.Args[1] != "launch-"+kind {
		return 1, errGuardProtocol
	}
	stage = "BOOT_ID"
	boot, err := fixedBootID()
	if err != nil {
		return 1, err
	}
	stage = "SCRIPT"
	scriptFile, _, err := openTrustedRegular(script, 16<<20)
	if err != nil {
		return 1, err
	}
	defer func() {
		if err != nil {
			g2d1Fail(stage, err)
		}
		_ = scriptFile.Close()
	}()
	var seed, caseFD int = -1, -1
	if kind == "source" {
		stage = "CARRIER"
		seed, caseFD, err = openSeedCarrier()
		if err != nil {
			return 1, err
		}
		defer unix.Close(seed)
		defer unix.Close(caseFD)
		defer func() {
			if err != nil {
				g2d1Fail(stage, err)
			}
		}()
	}
	stage = "PIPE_OPEN"
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		return 1, err
	}
	defer readPipe.Close()
	defer writePipe.Close()
	defer func() {
		if err != nil {
			g2d1Fail(stage, err)
		}
	}()
	// The child stays ptrace-stopped until the full frame has been written;
	// require enough kernel pipe capacity before starting it to avoid a
	// writer/reader deadlock at the maximum permitted ACK size.
	stage = "PIPE_GET_SIZE"
	capacity, err := unix.FcntlInt(writePipe.Fd(), unix.F_GETPIPE_SZ, 0)
	if err != nil {
		return 1, err
	}
	if capacity < maxControlFrame+4 {
		stage = "PIPE_SET_SIZE"
		capacity, err = unix.FcntlInt(writePipe.Fd(), unix.F_SETPIPE_SZ, maxControlFrame+4)
		if err != nil || capacity < maxControlFrame+4 {
			g2d1Fail(stage, err)
			return 1, errGuardProtocol
		}
	}
	stage = "CHILD_START"
	if time.Until(deadline) <= 0 {
		return 1, unix.ETIMEDOUT
	}
	child, err := os.StartProcess("/usr/bin/python3", []string{"/usr/bin/python3", "-B", script}, &os.ProcAttr{Env: os.Environ(), Files: []*os.File{os.Stdin, os.Stdout, os.Stderr, readPipe}, Sys: &syscall.SysProcAttr{Ptrace: true, Pdeathsig: syscall.SIGKILL}})
	if err != nil {
		return 1, err
	}
	_ = readPipe.Close()
	waited := false
	defer func() {
		if err != nil {
			g2d1Fail(stage, err)
		}
		if !waited {
			_ = unix.Kill(child.Pid, unix.SIGKILL)
			var status unix.WaitStatus
			_, _ = unix.Wait4(child.Pid, &status, 0, nil)
		}
		_ = child.Release()
	}()
	stage = "CHILD_PIDFD"
	pidfd, err := unix.PidfdOpen(child.Pid, 0)
	if err != nil {
		return 1, err
	}
	defer unix.Close(pidfd)
	defer func() {
		if err != nil {
			g2d1Fail(stage, err)
		}
	}()
	stage = "CHILD_STOP"
	type stopResult struct {
		pid    int
		status unix.WaitStatus
		err    error
	}
	stops := make(chan stopResult, 1)
	go func() {
		var status unix.WaitStatus
		pid, err := unix.Wait4(child.Pid, &status, unix.WUNTRACED, nil)
		stops <- stopResult{pid, status, err}
	}()
	remaining := time.Until(deadline)
	if remaining <= 0 {
		g2d1Fail(stage, unix.ETIMEDOUT)
		_ = unix.Kill(child.Pid, unix.SIGKILL)
		result := <-stops
		waited = result.pid == child.Pid && (result.status.Exited() || result.status.Signaled())
		return 1, unix.ETIMEDOUT
	}
	timer := time.NewTimer(remaining)
	var result stopResult
	select {
	case result = <-stops:
		timer.Stop()
	case <-timer.C:
		g2d1Fail(stage, unix.ETIMEDOUT)
		_ = unix.Kill(child.Pid, unix.SIGKILL)
		result = <-stops
		waited = result.pid == child.Pid && (result.status.Exited() || result.status.Signaled())
		return 1, unix.ETIMEDOUT
	}
	pid, stopped := result.pid, result.status
	if result.err != nil || pid != child.Pid || !stopped.Stopped() || stopped.StopSignal() != unix.SIGTRAP || !livePIDFD(pidfd) {
		g2d1Fail(stage, result.err)
		waited = pid == child.Pid && (stopped.Exited() || stopped.Signaled())
		return 1, errGuardProtocol
	}
	stage = "CHILD_STARTTIME"
	g2d1.scope = stage
	start, err := procStartTicks(child.Pid)
	if err != nil {
		return 1, err
	}
	stage = "PEER"
	control, brokerPIDFD, err := connectBroker(boot, deadline)
	if err != nil {
		return 1, err
	}
	defer unix.Close(control)
	defer unix.Close(brokerPIDFD)
	defer func() {
		if err != nil {
			g2d1Fail(stage, err)
		}
	}()
	rights := []int{pidfd}
	if kind == "source" {
		rights = append(rights, seed, caseFD)
	}
	rights = append(rights, int(scriptFile.Fd()))
	stage = "REGISTER"
	ack, err := launcherRegister(control, brokerPIDFD, boot, kind, child.Pid, start, rights, deadline)
	if err != nil {
		return 1, err
	}
	stage = "HANDOFF_FRAME"
	frame, err := handoffFrame(ack)
	if err != nil {
		return 1, err
	}
	stage = "PIPE_WRITE"
	for len(frame) > 0 {
		if err = waitFD(int(writePipe.Fd()), unix.POLLOUT, deadline); err != nil {
			return 1, err
		}
		n, e := unix.Write(int(writePipe.Fd()), frame)
		if e == unix.EINTR {
			continue
		}
		if e != nil || n <= 0 {
			g2d1Fail(stage, e)
			return 1, errGuardProtocol
		}
		frame = frame[n:]
	}
	stage = "PIPE_CLOSE"
	if err = writePipe.Close(); err != nil {
		return 1, err
	}
	stage = "DETACH"
	if err = unix.PtraceDetach(child.Pid); err != nil {
		return 1, err
	}
	if kind == "source" {
		g2d1Mark("DETACH_OK")
		g2d1Close()
	}
	var status unix.WaitStatus
	pid, err = unix.Wait4(child.Pid, &status, 0, nil)
	if err != nil || pid != child.Pid {
		g2d1Fail("CHILD_WAIT", err)
		return 1, errGuardProtocol
	}
	waited = true
	if status.Exited() {
		return status.ExitStatus(), nil
	}
	if status.Signaled() {
		return 128 + int(status.Signal()), nil
	}
	return 1, errGuardProtocol
}
