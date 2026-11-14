//go:build linux

package deploy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func ledgerPredecessorProgram(path string, mode uint32) (string, error) {
	root, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", ErrRecordLedgerFence
	}
	defer unix.Close(root)
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	current := root
	for _, part := range parts[:len(parts)-1] {
		next, err := unix.Openat(current, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if current != root {
			unix.Close(current)
		}
		if err != nil {
			return "", ErrRecordLedgerFence
		}
		current = next
		var st unix.Stat_t
		if unix.Fstat(current, &st) != nil || st.Uid != 0 || st.Mode&0022 != 0 {
			unix.Close(current)
			return "", ErrRecordLedgerFence
		}
	}
	if current != root {
		defer unix.Close(current)
	}
	fd, err := unix.Openat(current, parts[len(parts)-1], unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", ErrRecordLedgerFence
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	var before, after unix.Stat_t
	if unix.Fstat(fd, &before) != nil || before.Uid != 0 || before.Gid != 10002 || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Mode&07777 != mode || before.Nlink != 1 || before.Size <= 0 || before.Size > 128<<20 {
		return "", ErrRecordLedgerFence
	}
	if _, err := unix.Fgetxattr(fd, "security.capability", nil); err != unix.ENODATA && err != unix.ENOTSUP {
		return "", ErrRecordLedgerFence
	}
	h := sha256.New()
	if n, err := io.Copy(h, file); err != nil || n != before.Size {
		return "", ErrRecordLedgerFence
	}
	if unix.Fstat(fd, &after) != nil {
		return "", ErrRecordLedgerFence
	}
	before.Atim = after.Atim // Hashing may update atime; content and identity must stay fixed.
	if before != after {
		return "", ErrRecordLedgerFence
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func ledgerPredecessorStart(pid int) (uint64, error) {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil || len(raw) > 16384 {
		return 0, ErrRecordLedgerFence
	}
	end := strings.LastIndexByte(string(raw), ')')
	if end < 0 {
		return 0, ErrRecordLedgerFence
	}
	fields := strings.Fields(string(raw[end+1:]))
	if len(fields) < 20 {
		return 0, ErrRecordLedgerFence
	}
	value, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || value == 0 {
		return 0, ErrRecordLedgerFence
	}
	return value, nil
}

func ledgerPredecessorSelf() (ledgerPredecessorProcess, error) {
	pid := os.Getpid()
	started, err := ledgerPredecessorStart(pid)
	if err != nil {
		return ledgerPredecessorProcess{}, err
	}
	path, err := os.Readlink("/proc/self/exe")
	if err != nil || path != "/opt/g2/houfeng-record-platform-admin" {
		return ledgerPredecessorProcess{}, ErrRecordLedgerFence
	}
	digest, err := ledgerPredecessorProgram(path, 0550)
	if err != nil {
		return ledgerPredecessorProcess{}, err
	}
	again, err := ledgerPredecessorStart(pid)
	if err != nil || again != started {
		return ledgerPredecessorProcess{}, ErrRecordLedgerFence
	}
	return ledgerPredecessorProcess{PID: pid, Starttime: started, ExecutableSHA256: digest}, nil
}

func ledgerPredecessorGroupIdentity() (ledgerPredecessorGroup, error) {
	var st unix.Stat_t
	if unix.Lstat(ledgerPredecessorCgroup, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 || st.Gid != 0 || st.Mode&0022 != 0 {
		return ledgerPredecessorGroup{}, ErrRecordLedgerFence
	}
	return ledgerPredecessorGroup{Path: ledgerPredecessorCgroup, Dev: st.Dev, Ino: st.Ino}, nil
}

func ledgerPredecessorGroupEmpty(expected ledgerPredecessorGroup) error {
	current, err := ledgerPredecessorGroupIdentity()
	if err != nil || current != expected {
		return ErrRecordLedgerFence
	}
	body, err := os.ReadFile(ledgerPredecessorCgroup + "/cgroup.procs")
	if err != nil || len(strings.TrimSpace(string(body))) != 0 {
		return ErrRecordLedgerFence
	}
	events, err := os.ReadFile(ledgerPredecessorCgroup + "/cgroup.events")
	if err != nil || !strings.Contains("\n"+string(events), "\npopulated 0\n") {
		return ErrRecordLedgerFence
	}
	return nil
}

func ledgerPredecessorMemfd(body []byte) (*os.File, error) {
	if len(body) == 0 || len(body) > ledgerCallbackFrameLimit {
		return nil, ErrRecordLedgerFence
	}
	fd, err := unix.MemfdCreate("g2-c80-config", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return nil, ErrRecordLedgerFence
	}
	file := os.NewFile(uintptr(fd), "g2-c80-config")
	if n, err := file.Write(body); err != nil || n != len(body) {
		file.Close()
		return nil, ErrRecordLedgerFence
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return nil, ErrRecordLedgerFence
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_ADD_SEALS, unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL); err != nil {
		file.Close()
		return nil, ErrRecordLedgerFence
	}
	return file, nil
}

type ledgerPredecessorIO struct {
	Stream string
	Body   []byte
	Err    error
}
type ledgerPredecessorChild struct {
	cmd       *exec.Cmd
	pidfd     int
	process   ledgerPredecessorProcess
	channel   *ledgerCallbackChannel
	output    chan ledgerPredecessorIO
	waited    chan error
	waitSeen  bool
	waitError error
	cancel    context.CancelFunc
	pipes     []*os.File
	readers   sync.WaitGroup
}

func ledgerLaunchPredecessor(ctx context.Context, guard int, config *os.File) (*ledgerPredecessorChild, error) {
	sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, ErrRecordLedgerFence
	}
	defer unix.Close(sockets[0])
	remote := os.NewFile(uintptr(sockets[1]), "c80-child-channel")
	defer remote.Close()
	// Enable credentials before exec so an immediately queued ready is authenticated.
	if unix.SetsockoptInt(sockets[0], unix.SOL_SOCKET, unix.SO_PASSCRED, 1) != nil {
		return nil, ErrRecordLedgerFence
	}
	null, err := os.OpenFile("/dev/null", os.O_RDONLY, 0)
	if err != nil {
		return nil, ErrRecordLedgerFence
	}
	defer null.Close()
	guardCopy, err := unix.FcntlInt(uintptr(guard), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, ErrRecordLedgerFence
	}
	held := os.NewFile(uintptr(guardCopy), "c80-guard")
	defer held.Close()
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		return nil, ErrRecordLedgerFence
	}
	errRead, errWrite, err := os.Pipe()
	if err != nil {
		outRead.Close()
		outWrite.Close()
		return nil, ErrRecordLedgerFence
	}
	defer outWrite.Close()
	defer errWrite.Close()
	lifetime, cancel := context.WithCancel(ctx)
	child := &ledgerPredecessorChild{pidfd: -1, output: make(chan ledgerPredecessorIO, 8), waited: make(chan error, 1), cancel: cancel, pipes: []*os.File{outRead, errRead}}
	command := exec.Command(ledgerPredecessorLauncher, "service-preview-execute")
	command.Env = []string{}
	command.Stdin = null
	command.Stdout = outWrite
	command.Stderr = errWrite
	command.ExtraFiles = []*os.File{held, config, null, remote}
	command.SysProcAttr = &syscall.SysProcAttr{PidFD: &child.pidfd}
	child.cmd = command
	if command.Start() != nil {
		cancel()
		outRead.Close()
		errRead.Close()
		return nil, ErrRecordLedgerFence
	}
	child.process.PID = command.Process.Pid
	// clone/pidfd pinning precedes the sole direct wait, preventing PID reuse
	// between process creation and the supervisor's first identity observation.
	go func() { child.waited <- command.Wait() }()
	if child.pidfd < 0 {
		cancel()
		outRead.Close()
		errRead.Close()
		return nil, ErrRecordLedgerFence
	}
	child.process.Starttime, err = ledgerPredecessorStart(child.process.PID)
	if err == nil {
		child.channel, err = ledgerCallbackSocket(sockets[0], unix.Ucred{Pid: int32(child.process.PID), Uid: 10002, Gid: 10002})
	}
	if err != nil {
		child.close()
		return nil, ErrRecordLedgerFence
	}
	pump := func(stream string, file *os.File) {
		defer child.readers.Done()
		var total int
		for {
			buffer := make([]byte, 4096)
			n, err := file.Read(buffer)
			total += n
			if total > ledgerCallbackFrameLimit {
				err = ErrRecordLedgerFence
				n = 0
			}
			event := ledgerPredecessorIO{Stream: stream, Body: buffer[:n], Err: err}
			select {
			case child.output <- event:
			case <-lifetime.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}
	child.readers.Add(3)
	go pump("stdout", outRead)
	go pump("stderr", errRead)
	go func() {
		defer child.readers.Done()
		for {
			body, err := child.channel.receive(lifetime)
			select {
			case child.output <- ledgerPredecessorIO{Stream: "ipc", Body: body, Err: err}:
			case <-lifetime.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return child, nil
}

func (child *ledgerPredecessorChild) close() {
	if child == nil {
		return
	}
	if child.pidfd >= 0 && ledgerHeldProcessLive(child.pidfd) {
		_ = unix.PidfdSendSignal(child.pidfd, unix.SIGKILL, nil, 0)
	}
	child.cancel()
	if child.channel != nil {
		child.channel.close()
	}
	for _, pipe := range child.pipes {
		pipe.Close()
	}
	child.readers.Wait()
	if child.pidfd >= 0 {
		unix.Close(child.pidfd)
		child.pidfd = -1
	}
}

func (child *ledgerPredecessorChild) verify(intent ledgerPredecessorIntent) error {
	if child == nil || !ledgerHeldProcessLive(child.pidfd) {
		return ErrRecordLedgerFence
	}
	started, err := ledgerPredecessorStart(child.process.PID)
	if err != nil || started != child.process.Starttime {
		return ErrRecordLedgerFence
	}
	group, err := os.ReadFile(ledgerPredecessorCgroup + "/cgroup.procs")
	if err != nil || strings.TrimSpace(string(group)) != strconv.Itoa(child.process.PID) {
		return ErrRecordLedgerFence
	}
	actual, err := ledgerPredecessorGroupIdentity()
	if err != nil || actual != intent.ChildCgroup {
		return ErrRecordLedgerFence
	}
	// Root independently proves the protected namespace and all-thread credentials
	// before granting the ready observation. The parent retains direct wait/pidfd.
	child.process.ExecutableSHA256 = intent.PredecessorSHA256
	return nil
}

func ledgerPredecessorJoin(ctx context.Context, child *ledgerPredecessorChild) error {
	if child == nil {
		return nil
	}
	if child.pidfd >= 0 && ledgerHeldProcessLive(child.pidfd) {
		if unix.PidfdSendSignal(child.pidfd, unix.SIGKILL, nil, 0) != nil {
			return ErrRecordLedgerFence
		}
	}
	if !child.waitSeen {
		select {
		case err := <-child.waited:
			child.waitSeen = true
			child.waitError = err
		case <-ctx.Done():
			return ErrRecordLedgerFence
		}
	}
	for ledgerHeldProcessLive(child.pidfd) {
		deadline, ok := ctx.Deadline()
		if !ok {
			return ErrRecordLedgerFence
		}
		remain := time.Until(deadline)
		if remain <= 0 {
			return ErrRecordLedgerFence
		}
		_, err := unix.Poll([]unix.PollFd{{Fd: int32(child.pidfd), Events: unix.POLLIN}}, int(min(remain.Milliseconds(), 100)))
		if err != nil && err != unix.EINTR {
			return ErrRecordLedgerFence
		}
	}
	return nil
}
