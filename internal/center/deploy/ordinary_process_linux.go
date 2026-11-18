//go:build linux

package deploy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const ordinaryNativePayloadLimit = 1 << 20

type ordinaryNativeBinding struct {
	OperationID  string
	IntentSHA256 [32]byte
	AdminPath    string
	AdminSHA256  [32]byte
	GuardFD      int
}

type ordinaryNativeIdentity struct {
	PID              int      `json:"pid"`
	BootID           string   `json:"boot_id"`
	StartTime        uint64   `json:"start_time"`
	ExecutableSHA256 [32]byte `json:"executable_sha256"`
	GuardDev         uint64   `json:"guard_dev"`
	GuardInode       uint64   `json:"guard_inode"`
	GuardNonceSHA256 [32]byte `json:"guard_nonce_sha256"`
}

type ordinaryNativeStatus string

// Bump whenever BIND/EXECUTE/ACK, participant payload, or status framing changes.
const ordinaryNativeProtocolVersion = 3

const (
	ordinaryNativeSucceeded     ordinaryNativeStatus = "succeeded"
	ordinaryNativeFailed        ordinaryNativeStatus = "validation"
	ordinaryNativePreview       ordinaryNativeStatus = "preview"
	ordinaryNativeConnect       ordinaryNativeStatus = "connect"
	ordinaryNativeRegistry      ordinaryNativeStatus = "registry"
	ordinaryNativeConverge      ordinaryNativeStatus = "converge"
	ordinaryNativeLaunchClose   ordinaryNativeStatus = "launch-close"
	ordinaryNativePrepare       ordinaryNativeStatus = "prepare"
	ordinaryNativeProject       ordinaryNativeStatus = "project"
	ordinaryNativeReceipt       ordinaryNativeStatus = "receipt"
	ordinaryNativeLaunchStage   ordinaryNativeStatus = "launch-stage"
	ordinaryNativeLaunchPublish ordinaryNativeStatus = "launch-publish"
	ordinaryNativeLaunchVerify  ordinaryNativeStatus = "launch-verify"
	ordinaryNativeLaunchOpen    ordinaryNativeStatus = "launch-open"
	ordinaryNativeHeartbeat     ordinaryNativeStatus = "heartbeat"
	ordinaryNativeAdmit         ordinaryNativeStatus = "admit"
	ordinaryNativeProbe         ordinaryNativeStatus = "probe"
	ordinaryNativeBootstrap     ordinaryNativeStatus = "bootstrap"
)

// Version and code are fixed-size; labels never come from a participant input.
var ordinaryNativeStatusCodes = [...]ordinaryNativeStatus{
	ordinaryNativeSucceeded, ordinaryNativeFailed, ordinaryNativePreview, ordinaryNativeConnect,
	ordinaryNativeRegistry, ordinaryNativeConverge, ordinaryNativeLaunchClose, ordinaryNativePrepare,
	ordinaryNativeProject, ordinaryNativeReceipt, ordinaryNativeLaunchStage, ordinaryNativeLaunchPublish,
	ordinaryNativeLaunchVerify, ordinaryNativeLaunchOpen, ordinaryNativeHeartbeat, ordinaryNativeAdmit, ordinaryNativeProbe, ordinaryNativeBootstrap,
}

func ordinaryEncodeNativeStatus(status ordinaryNativeStatus) ([2]byte, error) {
	for code, candidate := range ordinaryNativeStatusCodes {
		if candidate == status {
			return [2]byte{ordinaryNativeProtocolVersion, byte(code)}, nil
		}
	}
	return [2]byte{}, errOrdinaryUpgrade
}

func ordinaryDecodeNativeStatus(body []byte) (ordinaryNativeStatus, error) {
	if len(body) != 2 || body[0] != ordinaryNativeProtocolVersion || int(body[1]) >= len(ordinaryNativeStatusCodes) {
		return "", errOrdinaryUpgrade
	}
	return ordinaryNativeStatusCodes[body[1]], nil
}

type ordinaryNativeWireBinding struct {
	OperationID      string   `json:"operation_id"`
	IntentSHA256     [32]byte `json:"intent_sha256"`
	AdminPath        string   `json:"admin_path"`
	AdminSHA256      [32]byte `json:"admin_sha256"`
	GuardDev         uint64   `json:"guard_dev"`
	GuardInode       uint64   `json:"guard_inode"`
	GuardNonceSHA256 [32]byte `json:"guard_nonce_sha256"`
}

type ordinaryNativeChild struct {
	pidfd    int
	control  *os.File
	status   *os.File
	waited   <-chan error
	mu       sync.Mutex
	executed bool
	joined   bool
}

func ordinaryNativeFrameWrite(w io.Writer, body []byte) error {
	if len(body) == 0 || len(body) > ordinaryNativePayloadLimit {
		return errOrdinaryUpgrade
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(body)))
	for _, part := range [][]byte{header[:], body} {
		for len(part) != 0 {
			n, err := w.Write(part)
			if err != nil {
				return err
			}
			if n <= 0 {
				return io.ErrShortWrite
			}
			part = part[n:]
		}
	}
	return nil
}

func ordinaryNativeFrameRead(r io.Reader, limit uint32) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > limit {
		return nil, errOrdinaryUpgrade
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(r, body); err != nil {
		clear(body)
		return nil, err
	}
	return body, nil
}

// The path itself, not just the final inode, must be protected against rename.
func ordinaryNativeOpenAdmin(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return nil, errOrdinaryUpgrade
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	fd, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errOrdinaryUpgrade
	}
	defer func() { _ = unix.Close(fd) }()
	for _, part := range parts[:len(parts)-1] {
		var stat unix.Stat_t
		if unix.Fstat(fd, &stat) != nil || stat.Uid != 0 || stat.Mode&0o022 != 0 {
			return nil, errOrdinaryUpgrade
		}
		next, e := unix.Openat(fd, part, unix.O_PATH|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if e != nil {
			return nil, errOrdinaryUpgrade
		}
		_ = unix.Close(fd)
		fd = next
	}
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Uid != 0 || stat.Mode&0o022 != 0 {
		return nil, errOrdinaryUpgrade
	}
	fileFD, err := unix.Openat(fd, parts[len(parts)-1], unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errOrdinaryUpgrade
	}
	return os.NewFile(uintptr(fileFD), "ordinary-admin"), nil
}

func ordinaryNativeBootID() (string, error) {
	body, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	id := strings.TrimSpace(string(body))
	if err != nil || !ordinaryNativeValidBootID(id) {
		return "", errOrdinaryUpgrade
	}
	return id, nil
}

func ordinaryNativeValidBootID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, c := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func ordinaryNativeStart(pid int) (uint64, error) {
	body, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil || len(body) > 16384 {
		return 0, errOrdinaryUpgrade
	}
	end := bytes.LastIndexByte(body, ')')
	if end < 0 {
		return 0, errOrdinaryUpgrade
	}
	fields := strings.Fields(string(body[end+1:]))
	if len(fields) < 20 {
		return 0, errOrdinaryUpgrade
	}
	value, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || value == 0 {
		return 0, errOrdinaryUpgrade
	}
	return value, nil
}

func ordinaryNativeLive(pidfd int) (bool, error) {
	fds := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
	n, err := unix.Poll(fds, 0)
	if err != nil || fds[0].Revents&(unix.POLLERR|unix.POLLNVAL) != 0 {
		return false, errOrdinaryUpgrade
	}
	return n == 0, nil
}

func ordinaryNativeGuard(fd int, fdinfoPath string) (ordinaryGuardAnchor, error) {
	anchor, err := ordinaryGuardIdentity(fd)
	if err != nil {
		return anchor, errOrdinaryUpgrade
	}
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_ACCMODE != unix.O_RDWR {
		return anchor, errOrdinaryUpgrade
	}
	if !ordinaryNativeExclusiveLock(fdinfoPath) {
		return anchor, errOrdinaryUpgrade
	}
	return anchor, nil
}

func ordinaryNativeExclusiveLock(path string) bool {
	body, err := os.ReadFile(path)
	if err != nil || len(body) > 16384 {
		return false
	}
	locks := 0
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "lock:") {
			fields := strings.Fields(line)
			if len(fields) < 6 || fields[2] != "FLOCK" || fields[3] != "ADVISORY" || fields[4] != "WRITE" {
				return false
			}
			locks++
		}
	}
	return locks == 1
}

func ordinaryNativeCredentials(path string) bool {
	body, err := os.ReadFile(path)
	if err != nil || len(body) > 65536 {
		return false
	}
	required := map[string]string{
		"Uid:": "10002 10002 10002 10002", "Gid:": "10001 10001 10001 10001",
		"Groups:": "", "CapInh:": "0000000000000000", "CapPrm:": "0000000000000000",
		"CapEff:": "0000000000000000", "CapAmb:": "0000000000000000", "NoNewPrivs:": "1",
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if expected, ok := required[fields[0]]; ok {
			if strings.Join(fields[1:], " ") != expected {
				return false
			}
			delete(required, fields[0])
		}
	}
	return len(required) == 0
}

func ordinaryNativePipe(fd int, access int) error {
	var stat unix.Stat_t
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_ACCMODE != access || unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFIFO || stat.Mode&0o7777 != 0o600 || stat.Uid != 0 || stat.Nlink != 1 {
		return errOrdinaryUpgrade
	}
	link, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
	if err != nil || link != fmt.Sprintf("pipe:[%d]", stat.Ino) {
		return errOrdinaryUpgrade
	}
	return nil
}

func ordinaryNativeInspect(pidfd int, identity ordinaryNativeIdentity) error {
	live, err := ordinaryNativeLive(pidfd)
	if err != nil || !live {
		return errOrdinaryUpgrade
	}
	start, err := ordinaryNativeStart(identity.PID)
	if err != nil || start != identity.StartTime {
		return errOrdinaryUpgrade
	}
	base := fmt.Sprintf("/proc/%d", identity.PID)
	exe, err := os.Open(base + "/exe")
	if err != nil {
		return errOrdinaryUpgrade
	}
	digest, digestErr := ordinaryTargetExecutable(exe)
	_ = exe.Close()
	if digestErr != nil || digest != identity.ExecutableSHA256 {
		return errOrdinaryUpgrade
	}
	guardFD, err := unix.Open(base+"/fd/3", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return errOrdinaryUpgrade
	}
	anchor, guardErr := ordinaryGuardIdentity(guardFD)
	_ = unix.Close(guardFD)
	if guardErr != nil || uint64(anchor.stat.Dev) != identity.GuardDev || anchor.stat.Ino != identity.GuardInode || sha256.Sum256(anchor.nonce[:]) != identity.GuardNonceSHA256 {
		return errOrdinaryUpgrade
	}
	// Read the actual inherited descriptor's fdinfo, not the newly opened file.
	if !ordinaryNativeExclusiveLock(base+"/fdinfo/3") || !ordinaryNativeCredentials(base+"/status") {
		return errOrdinaryUpgrade
	}
	again, err := ordinaryNativeStart(identity.PID)
	live, liveErr := ordinaryNativeLive(pidfd)
	if err != nil || again != start || liveErr != nil || !live {
		return errOrdinaryUpgrade
	}
	return nil
}

// An unsuccessful signal is not settlement. Keep the sole owning Wait and
// pidfd until the participant actually exits, even after returning an error.
func ordinaryNativeFinishLater(waited <-chan error, pidfd int, status *os.File) {
	go func() {
		<-waited
		if status != nil {
			_ = status.Close()
		}
		if pidfd >= 0 {
			_ = unix.Close(pidfd)
		}
	}()
}

func ordinaryNativeKill(pidfd int) error {
	if pidfd < 0 {
		return errOrdinaryUpgrade
	}
	err := unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0)
	if errors.Is(err, unix.ESRCH) {
		return nil
	}
	return err
}

func ordinarySpawnNative(ctx context.Context, binding ordinaryNativeBinding) (*ordinaryNativeChild, ordinaryNativeIdentity, error) {
	var identity ordinaryNativeIdentity
	if ctx == nil || ctx.Err() != nil || os.Getuid() != 0 || os.Geteuid() != 0 || !fenceRID.MatchString(binding.OperationID) || binding.IntentSHA256 == [32]byte{} || binding.AdminSHA256 == [32]byte{} {
		return nil, identity, errOrdinaryUpgrade
	}
	admin, err := ordinaryNativeOpenAdmin(binding.AdminPath)
	if err != nil {
		return nil, identity, err
	}
	defer admin.Close()
	digest, err := ordinaryTargetExecutable(admin)
	if err != nil || digest != binding.AdminSHA256 {
		return nil, identity, errOrdinaryUpgrade
	}
	anchor, err := ordinaryNativeGuard(binding.GuardFD, fmt.Sprintf("/proc/self/fdinfo/%d", binding.GuardFD))
	if err != nil {
		return nil, identity, err
	}
	copyFD, err := unix.FcntlInt(uintptr(binding.GuardFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, identity, err
	}
	guard := os.NewFile(uintptr(copyFD), "ordinary-guard")
	defer guard.Close()
	controlRead, controlWrite, err := os.Pipe()
	if err != nil {
		return nil, identity, err
	}
	defer controlRead.Close()
	statusRead, statusWrite, err := os.Pipe()
	if err != nil {
		controlWrite.Close()
		return nil, identity, err
	}
	defer statusWrite.Close()
	child := &ordinaryNativeChild{pidfd: -1, control: controlWrite, status: statusRead}
	cmd := exec.Command("/proc/self/exe", "_ordinary-launch-target", "participant", hex.EncodeToString(digest[:]))
	cmd.Env = []string{}
	cmd.ExtraFiles = []*os.File{guard, controlRead, statusWrite, admin}
	cmd.SysProcAttr = &syscall.SysProcAttr{PidFD: &child.pidfd, Pdeathsig: syscall.SIGKILL, Credential: &syscall.Credential{Uid: 10002, Gid: 10001, Groups: []uint32{}}}
	child.waited, err = ordinaryStartLocked(cmd)
	if err != nil {
		controlWrite.Close()
		statusRead.Close()
		return nil, identity, err
	}
	_ = controlRead.Close()
	_ = statusWrite.Close()
	fail := func(err error) (*ordinaryNativeChild, ordinaryNativeIdentity, error) {
		_ = child.control.Close()
		_ = child.status.Close()
		if signalErr := ordinaryNativeKill(child.pidfd); signalErr != nil {
			ordinaryNativeFinishLater(child.waited, child.pidfd, nil)
			return nil, ordinaryNativeIdentity{}, errors.Join(err, signalErr)
		}
		<-child.waited
		_ = unix.Close(child.pidfd)
		return nil, ordinaryNativeIdentity{}, err
	}
	if child.pidfd < 0 {
		return fail(errOrdinaryUpgrade)
	}
	if signalErr := unix.PidfdSendSignal(child.pidfd, 0, nil, 0); signalErr != nil {
		return fail(errors.Join(errOrdinaryUpgrade, signalErr))
	}
	identity = ordinaryNativeIdentity{PID: cmd.Process.Pid, ExecutableSHA256: digest, GuardDev: uint64(anchor.stat.Dev), GuardInode: anchor.stat.Ino, GuardNonceSHA256: sha256.Sum256(anchor.nonce[:])}
	identity.BootID, err = ordinaryNativeBootID()
	if err == nil {
		identity.StartTime, err = ordinaryNativeStart(identity.PID)
	}
	if err != nil {
		return fail(errOrdinaryUpgrade)
	}
	wire := ordinaryNativeWireBinding{binding.OperationID, binding.IntentSHA256, binding.AdminPath, binding.AdminSHA256, identity.GuardDev, identity.GuardInode, identity.GuardNonceSHA256}
	body, err := json.Marshal(wire)
	if err != nil || ordinaryNativeFrameWrite(child.control, body) != nil {
		return fail(errOrdinaryUpgrade)
	}
	ready := make(chan error, 1)
	go func() {
		msg, e := ordinaryNativeFrameRead(child.status, 32)
		if e == nil && string(msg) != "READY" {
			e = errOrdinaryUpgrade
		}
		ready <- e
	}()
	select {
	case err = <-ready:
	case <-ctx.Done():
		_ = child.status.Close()
		err = ctx.Err()
	}
	if err != nil || ordinaryNativeInspect(child.pidfd, identity) != nil {
		return fail(errOrdinaryUpgrade)
	}
	return child, identity, nil
}

// Execute must only follow the caller's durable intent and identity fsync.
func (child *ordinaryNativeChild) Execute(payload []byte) error {
	child.mu.Lock()
	if child.joined || child.executed || len(payload) == 0 || len(payload) > ordinaryNativePayloadLimit {
		child.mu.Unlock()
		return errOrdinaryUpgrade
	}
	child.executed = true
	child.mu.Unlock()
	if ordinaryNativeFrameWrite(child.control, payload) != nil || ordinaryNativeFrameWrite(child.control, []byte("ACK")) != nil {
		_ = child.control.Close()
		return errOrdinaryUpgrade
	}
	return nil
}

func (child *ordinaryNativeChild) Join(ctx context.Context) (ordinaryNativeStatus, error) {
	if ctx == nil {
		return "", errOrdinaryUpgrade
	}
	child.mu.Lock()
	defer child.mu.Unlock()
	if child.joined {
		return "", errOrdinaryUpgrade
	}
	child.joined = true
	_ = child.control.Close()
	var waitErr error
	select {
	case waitErr = <-child.waited:
	case <-ctx.Done():
		if signalErr := ordinaryNativeKill(child.pidfd); signalErr != nil {
			ordinaryNativeFinishLater(child.waited, child.pidfd, child.status)
			return "", errors.Join(ctx.Err(), signalErr)
		}
		waitErr = <-child.waited
		_ = child.status.Close()
		_ = unix.Close(child.pidfd)
		return "", errors.Join(ctx.Err(), waitErr)
	}
	body, err := ordinaryNativeFrameRead(child.status, 2)
	_ = child.status.Close()
	_ = unix.Close(child.pidfd)
	status, decodeErr := ordinaryDecodeNativeStatus(body)
	if err != nil || waitErr != nil || decodeErr != nil {
		return "", errors.Join(errOrdinaryUpgrade, err, waitErr, decodeErr)
	}
	return status, nil
}

func ordinaryNativeNoDockerAccess() error {
	for _, path := range []string{"/run/docker.sock", "/var/run/docker.sock"} {
		fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			return errOrdinaryUpgrade
		}
		err = unix.Connect(fd, &unix.SockaddrUnix{Name: path})
		_ = unix.Close(fd)
		if err != unix.EACCES && err != unix.ENOENT && err != unix.ENOTDIR {
			return errOrdinaryUpgrade
		}
	}
	return nil
}

// This is the only private-child entry point. No payload is returned before ACK.
func ordinaryReceiveNativeExecute() (ordinaryNativeBinding, []byte, error) {
	var binding ordinaryNativeBinding
	if PrepareOrdinaryTargetProcess() != nil {
		return binding, nil, errOrdinaryUpgrade
	}
	unix.CloseOnExec(3)
	unix.CloseOnExec(4)
	unix.CloseOnExec(5)
	if os.Getuid() != 10002 || os.Geteuid() != 10002 || os.Getgid() != 10001 || os.Getegid() != 10001 {
		return binding, nil, errOrdinaryUpgrade
	}
	groups, err := os.Getgroups()
	if err != nil || len(groups) != 0 || ordinaryNativePipe(4, unix.O_RDONLY) != nil || ordinaryNativePipe(5, unix.O_WRONLY) != nil || ordinaryNativeNoDockerAccess() != nil {
		return binding, nil, errOrdinaryUpgrade
	}
	var controlStat, statusStat unix.Stat_t
	if unix.Fstat(4, &controlStat) != nil || unix.Fstat(5, &statusStat) != nil || (controlStat.Dev == statusStat.Dev && controlStat.Ino == statusStat.Ino) {
		return binding, nil, errOrdinaryUpgrade
	}
	parentStatus, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", os.Getppid()))
	if err != nil || !strings.Contains(string(parentStatus), "Uid:\t0\t0\t0\t0\n") {
		return binding, nil, errOrdinaryUpgrade
	}
	anchor, err := ordinaryNativeGuard(3, "/proc/self/fdinfo/3")
	if err != nil {
		return binding, nil, errOrdinaryUpgrade
	}
	control := os.NewFile(4, "ordinary-control")
	defer control.Close()
	body, err := ordinaryNativeFrameRead(control, 8192)
	if err != nil {
		return binding, nil, errOrdinaryUpgrade
	}
	var wire ordinaryNativeWireBinding
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&wire) != nil {
		return binding, nil, errOrdinaryUpgrade
	}
	canonical, err := json.Marshal(wire)
	if err != nil || !bytes.Equal(body, canonical) || !fenceRID.MatchString(wire.OperationID) || wire.IntentSHA256 == [32]byte{} || wire.AdminSHA256 == [32]byte{} || uint64(anchor.stat.Dev) != wire.GuardDev || anchor.stat.Ino != wire.GuardInode || sha256.Sum256(anchor.nonce[:]) != wire.GuardNonceSHA256 {
		return binding, nil, errOrdinaryUpgrade
	}
	if !filepath.IsAbs(wire.AdminPath) || filepath.Clean(wire.AdminPath) != wire.AdminPath || wire.AdminPath == "/" {
		return binding, nil, errOrdinaryUpgrade
	}
	exe, err := os.Open("/proc/self/exe")
	if err != nil {
		return binding, nil, errOrdinaryUpgrade
	}
	digest, err := ordinaryTargetExecutable(exe)
	_ = exe.Close()
	if err != nil || digest != wire.AdminSHA256 {
		return binding, nil, errOrdinaryUpgrade
	}
	if !ordinaryNativeCredentials("/proc/self/status") || ordinaryNativeFrameWrite(ordinaryNativeFDWriter(5), []byte("READY")) != nil {
		return binding, nil, errOrdinaryUpgrade
	}
	payload, err := ordinaryNativeFrameRead(control, ordinaryNativePayloadLimit)
	if err != nil {
		return binding, nil, errOrdinaryUpgrade
	}
	ack, err := ordinaryNativeFrameRead(control, 16)
	if err != nil || string(ack) != "ACK" {
		clear(payload)
		return binding, nil, errOrdinaryUpgrade
	}
	binding = ordinaryNativeBinding{wire.OperationID, wire.IntentSHA256, wire.AdminPath, wire.AdminSHA256, 3}
	return binding, payload, nil
}

type ordinaryNativeFDWriter int

func (fd ordinaryNativeFDWriter) Write(body []byte) (int, error) {
	return unix.Write(int(fd), body)
}

func ordinaryWriteNativeStatus(status ordinaryNativeStatus) error {
	body, err := ordinaryEncodeNativeStatus(status)
	if err != nil {
		return errOrdinaryUpgrade
	}
	defer unix.Close(5)
	return ordinaryNativeFrameWrite(ordinaryNativeFDWriter(5), body[:])
}

// Recovery never locks or unlocks the guard and signals only the pinned process.
func ordinaryJoinRecordedNative(ctx context.Context, identity ordinaryNativeIdentity, guardRoot string) error {
	if ctx == nil || identity.PID <= 0 || identity.StartTime == 0 || !ordinaryNativeValidBootID(identity.BootID) || identity.ExecutableSHA256 == [32]byte{} || identity.GuardDev == 0 || identity.GuardInode == 0 || identity.GuardNonceSHA256 == [32]byte{} || !filepath.IsAbs(guardRoot) || filepath.Clean(guardRoot) != guardRoot {
		return errOrdinaryUpgrade
	}
	boot, err := ordinaryNativeBootID()
	if err != nil {
		return errOrdinaryUpgrade
	}
	if boot != identity.BootID {
		return nil
	}
	pidfd, err := unix.PidfdOpen(identity.PID, 0)
	if errors.Is(err, unix.ESRCH) {
		return nil
	}
	if err != nil {
		return errOrdinaryUpgrade
	}
	defer unix.Close(pidfd)
	live, err := ordinaryNativeLive(pidfd)
	if err != nil {
		return errOrdinaryUpgrade
	}
	if !live {
		return nil
	}
	start, err := ordinaryNativeStart(identity.PID)
	if err != nil {
		return errOrdinaryUpgrade
	}
	if start != identity.StartTime {
		return nil
	}
	guard, err := unix.Open(filepath.Join(guardRoot, ordinaryGuardFile), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return errOrdinaryUpgrade
	}
	anchor, guardErr := ordinaryGuardIdentity(guard)
	_ = unix.Close(guard)
	if guardErr != nil || uint64(anchor.stat.Dev) != identity.GuardDev || anchor.stat.Ino != identity.GuardInode || sha256.Sum256(anchor.nonce[:]) != identity.GuardNonceSHA256 {
		return errOrdinaryUpgrade
	}
	if ordinaryNativeInspect(pidfd, identity) != nil {
		live, err = ordinaryNativeLive(pidfd)
		if err == nil && !live {
			return nil
		}
		return errOrdinaryUpgrade
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
		return errOrdinaryUpgrade
	}
	for {
		live, err = ordinaryNativeLive(pidfd)
		if err != nil {
			return errOrdinaryUpgrade
		}
		if !live {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}
