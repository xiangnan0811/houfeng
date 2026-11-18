//go:build linux && (amd64 || arm64)

package deploy

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// The terminal bridge never returns: after blocking signals it either executes
// the pinned ELF from its private descriptor table or calls exit_group.
func ordinaryExecBridge(targetFD uintptr, first uintptr, path *byte, argv **byte, env **byte, mask *uint64)

type ordinaryExecStorage struct {
	path   [1]byte
	arg0   []byte
	arg1   []byte
	argv   [3]*byte
	env    [1]*byte
	mask   uint64
	pinner runtime.Pinner
	file   *os.File
}

// Strong roots persist until exec replaces the whole preparation process.
var ordinaryExecRoot *ordinaryExecStorage

// PrepareOrdinaryTargetProcess is the first operation in either target route.
// Private targets rely on control EOF, deadlines, pidfds and SIGKILL; they do
// not rely on inherited asynchronous signal handlers.
func PrepareOrdinaryTargetProcess() error {
	_, _, errno := syscall.AllThreadsSyscall6(syscall.SYS_PRCTL, unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0, 0)
	if errno != 0 || unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0) != nil || !ordinaryNativeCredentials("/proc/self/status") || ordinaryNativeNoDockerAccess() != nil {
		return errOrdinaryUpgrade
	}
	return nil
}

func ordinaryLauncherStdio(mode string) error {
	for _, fd := range []int{0, 1, 2} {
		if mode == "probe" && fd == 1 {
			if ordinaryNativePipe(fd, unix.O_WRONLY) != nil {
				return errOrdinaryUpgrade
			}
			continue
		}
		var stat unix.Stat_t
		if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFCHR || stat.Rdev != unix.Mkdev(1, 3) {
			return errOrdinaryUpgrade
		}
	}
	return nil
}

// RunOrdinaryTargetLauncher is called only by the fixed private preparation
// route. It cannot select a path or an arbitrary command or environment.
func RunOrdinaryTargetLauncher(mode string, expectedSHA256 string) error {
	if mode != "participant" && mode != "probe" {
		return errOrdinaryUpgrade
	}
	if len(expectedSHA256) != 64 || strings.ToLower(expectedSHA256) != expectedSHA256 {
		return errOrdinaryUpgrade
	}
	decoded, err := hex.DecodeString(expectedSHA256)
	if err != nil || len(decoded) != 32 {
		return errOrdinaryUpgrade
	}
	parent := os.Getppid()
	parentStatus, err := os.ReadFile("/proc/" + strconv.Itoa(parent) + "/status")
	if parent <= 0 || err != nil || !strings.Contains(string(parentStatus), "Uid:\t0\t0\t0\t0\n") {
		return errOrdinaryUpgrade
	}
	if PrepareOrdinaryTargetProcess() != nil || ordinaryLauncherStdio(mode) != nil {
		return errOrdinaryUpgrade
	}
	targetFD, first, argument := 6, uintptr(6), "_ordinary-native-participant"
	if mode == "participant" {
		if _, err := ordinaryNativeGuard(3, "/proc/self/fdinfo/3"); err != nil || ordinaryNativePipe(4, unix.O_RDONLY) != nil || ordinaryNativePipe(5, unix.O_WRONLY) != nil {
			return errOrdinaryUpgrade
		}
		for _, fd := range []int{3, 4, 5} {
			flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
			if err != nil || flags&unix.FD_CLOEXEC != 0 {
				return errOrdinaryUpgrade
			}
		}
	} else {
		targetFD, first, argument = 3, 3, "_ordinary-target-contract"
	}
	file := os.NewFile(uintptr(targetFD), "ordinary-target")
	defer file.Close()
	digest, err := ordinaryTargetExecutable(file)
	if err != nil || !strings.EqualFold(hex.EncodeToString(digest[:]), expectedSHA256) {
		return errOrdinaryUpgrade
	}
	storage := &ordinaryExecStorage{arg0: append([]byte("ordinary-target"), 0), arg1: append([]byte(argument), 0), mask: ^uint64(0), file: file}
	storage.argv = [3]*byte{&storage.arg0[0], &storage.arg1[0], nil}
	storage.pinner.Pin(&storage.path[0])
	storage.pinner.Pin(&storage.arg0[0])
	storage.pinner.Pin(&storage.arg1[0])
	storage.pinner.Pin(&storage.argv[0])
	storage.pinner.Pin(&storage.env[0])
	storage.pinner.Pin(&storage.mask)
	ordinaryExecRoot = storage
	runtime.LockOSThread()
	defer func() {
		runtime.UnlockOSThread()
		storage.pinner.Unpin()
		ordinaryExecRoot = nil
	}()
	if unix.Prctl(unix.PR_SET_PDEATHSIG, uintptr(unix.SIGKILL), 0, 0, 0) != nil {
		return errOrdinaryUpgrade
	}
	var deathSignal int32
	_, _, errno := syscall.RawSyscall6(syscall.SYS_PRCTL, unix.PR_GET_PDEATHSIG, uintptr(unsafe.Pointer(&deathSignal)), 0, 0, 0, 0)
	if errno != 0 || deathSignal != int32(unix.SIGKILL) || os.Getppid() != parent {
		return errOrdinaryUpgrade
	}
	ordinaryExecBridge(uintptr(targetFD), first, &storage.path[0], &storage.argv[0], &storage.env[0], &storage.mask)
	panic("ordinary terminal exec returned")
}

func ordinaryNativeControllerCapabilities() error {
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var data [2]unix.CapUserData
	if err := unix.Capget(&header, &data[0]); err != nil {
		return err
	}
	const required = (uint32(1) << unix.CAP_KILL) | (uint32(1) << unix.CAP_SYS_PTRACE)
	if data[0].Effective&required != required {
		return fmt.Errorf("ordinary controller requires effective CAP_KILL and CAP_SYS_PTRACE: %w", unix.EPERM)
	}
	return nil
}

// The creating OS thread remains alive until its sole cmd.Wait has finished.
func ordinaryStartLocked(command *exec.Cmd) (<-chan error, error) {
	started := make(chan error, 1)
	waited := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		err := ordinaryNativeControllerCapabilities()
		if err == nil {
			err = command.Start()
		}
		started <- err
		if err == nil {
			waited <- command.Wait()
		}
	}()
	return waited, <-started
}

// ordinaryProbeTargetContract executes only the fixed target contract route.
func ordinaryProbeTargetContract(ctx context.Context, path string, digest [32]byte) ([]byte, error) {
	if ctx == nil || ctx.Err() != nil || os.Getuid() != 0 || os.Geteuid() != 0 || digest == [32]byte{} {
		return nil, errOrdinaryUpgrade
	}
	file, err := ordinaryNativeOpenAdmin(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	actual, err := ordinaryTargetExecutable(file)
	if err != nil || actual != digest {
		return nil, errOrdinaryUpgrade
	}
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer readEnd.Close()
	defer writeEnd.Close()
	pidfd := -1
	command := exec.Command("/proc/self/exe", "_ordinary-launch-target", "probe", hex.EncodeToString(digest[:]))
	command.Env = []string{}
	command.Stdout = writeEnd
	command.ExtraFiles = []*os.File{file}
	command.SysProcAttr = &syscall.SysProcAttr{PidFD: &pidfd, Pdeathsig: syscall.SIGKILL, Credential: &syscall.Credential{Uid: 10002, Gid: 10001, Groups: []uint32{}}}
	waited, err := ordinaryStartLocked(command)
	if err != nil {
		var pathErr *os.PathError
		if errors.As(err, &pathErr) && pathErr.Op == "fork/exec" && pathErr.Path == "/proc/self/exe" && (errors.Is(err, unix.EACCES) || errors.Is(err, unix.EPERM)) {
			return nil, fmt.Errorf("%w: %w", errOrdinaryControllerExecutable, err)
		}
		return nil, err
	}
	_ = writeEnd.Close()
	defer func() {
		if pidfd >= 0 {
			_ = unix.Close(pidfd)
		}
	}()
	cancelProbe := func(cause error) ([]byte, error) {
		_ = readEnd.Close()
		if signalErr := ordinaryNativeKill(pidfd); signalErr != nil {
			ordinaryNativeFinishLater(waited, pidfd, nil)
			pidfd = -1
			return nil, errors.Join(cause, signalErr)
		}
		<-waited
		return nil, cause
	}
	if pidfd < 0 {
		return cancelProbe(errOrdinaryUpgrade)
	}
	if signalErr := unix.PidfdSendSignal(pidfd, 0, nil, 0); signalErr != nil && !errors.Is(signalErr, unix.ESRCH) {
		return cancelProbe(errors.Join(errOrdinaryUpgrade, signalErr))
	}
	type result struct {
		body []byte
		err  error
	}
	output := make(chan result, 1)
	go func() {
		body, e := io.ReadAll(io.LimitReader(readEnd, ordinaryContractLimit+1))
		if len(body) > ordinaryContractLimit {
			e = errOrdinaryUpgrade
		}
		output <- result{body, e}
	}()
	var got result
	select {
	case got = <-output:
	case <-ctx.Done():
		return cancelProbe(ctx.Err())
	}
	if got.err != nil {
		return cancelProbe(errors.Join(errOrdinaryUpgrade, got.err))
	}
	select {
	case err = <-waited:
	case <-ctx.Done():
		return cancelProbe(ctx.Err())
	}
	if got.err != nil || err != nil || len(got.body) == 0 {
		return nil, errOrdinaryUpgrade
	}
	return got.body, nil
}
