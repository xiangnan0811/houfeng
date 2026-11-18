//go:build linux

package deploy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// The copied static test executable is also the real fixed private participant.
// No production CLI result or protected effect is simulated by the parent.
func init() {
	if len(os.Args) == 4 && os.Args[1] == "_ordinary-launch-target" {
		// Keep the thread-group leader occupied so final exec uses a worker thread.
		runtime.LockOSThread()
		if os.Args[2] == "participant" {
			guardPath, err := os.Readlink("/proc/self/fd/3")
			if err != nil {
				os.Exit(53)
			}
			var identities []string
			for range 16 {
				var pipe [2]int
				if unix.Pipe2(pipe[:], 0) != nil {
					os.Exit(54)
				}
				pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
				if err != nil {
					os.Exit(55)
				}
				for _, fd := range []int{pipe[0], pipe[1], pair[0], pair[1]} {
					var stat unix.Stat_t
					if unix.Fstat(fd, &stat) != nil {
						os.Exit(56)
					}
					identities = append(identities, fmt.Sprintf("%d:%d", stat.Dev, stat.Ino))
				}
			}
			if os.WriteFile(filepath.Join(filepath.Dir(guardPath), "prep-canaries"), []byte(strings.Join(identities, "\n")), 0o600) != nil {
				os.Exit(57)
			}
			for _, denial := range []struct {
				name      string
				trap      uintptr
				flagsOnly bool
			}{{"deny-execveat", unix.SYS_EXECVEAT, false}, {"deny-close-range", unix.SYS_CLOSE_RANGE, false}, {"deny-close-range-flags", unix.SYS_CLOSE_RANGE, true}} {
				if _, err := os.Stat(filepath.Join(filepath.Dir(guardPath), denial.name)); err == nil {
					if ordinaryTestDenySyscall(denial.trap, denial.flagsOnly) != nil {
						os.Exit(58)
					}
					if os.WriteFile(filepath.Join(filepath.Dir(guardPath), "filter-ready"), []byte(denial.name), 0o600) != nil {
						os.Exit(59)
					}
				}
			}
			go func() {
				for {
					var pipe [2]int
					if unix.Pipe2(pipe[:], 0) == nil {
						_ = unix.Close(pipe[0])
						_ = unix.Close(pipe[1])
					}
					pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
					if err == nil {
						_ = unix.Close(pair[0])
						_ = unix.Close(pair[1])
					}
					runtime.Gosched()
				}
			}()
		}
		go func() {
			if RunOrdinaryTargetLauncher(os.Args[2], os.Args[3]) != nil {
				os.Exit(50)
			}
			os.Exit(51)
		}()
		select {}
	}
	if len(os.Args) == 2 && os.Args[1] == "_ordinary-target-contract" {
		if PrepareOrdinaryTargetProcess() != nil {
			os.Exit(52)
		}
		if executable, err := os.Readlink("/proc/self/exe"); err == nil {
			dir := filepath.Join(filepath.Dir(executable), "guard")
			if _, err := os.Stat(filepath.Join(dir, "probe-gate")); err == nil {
				if os.WriteFile(filepath.Join(dir, "probe-ready"), []byte(strconv.Itoa(os.Getpid())), 0o600) != nil {
					os.Exit(64)
				}
				for {
					if _, err := os.Stat(filepath.Join(dir, "probe-release")); err == nil {
						break
					}
					time.Sleep(5 * time.Millisecond)
				}
			}
		}
		if WriteOrdinaryTargetContract(os.Stdout) != nil {
			os.Exit(52)
		}
		os.Exit(0)
	}
	if len(os.Args) == 2 && os.Args[1] == "_ordinary-native-participant" {
		_, payload, err := ordinaryReceiveNativeExecute()
		if err != nil {
			os.Exit(41)
		}
		timer := time.NewTimer(time.Millisecond)
		runtime.GC()
		select {
		case <-timer.C:
		case <-time.After(5 * time.Second):
			os.Exit(63)
		}
		if string(payload) == "wait" {
			var deathSignal int32
			_, _, errno := syscall.RawSyscall6(syscall.SYS_PRCTL, unix.PR_GET_PDEATHSIG, uintptr(unsafe.Pointer(&deathSignal)), 0, 0, 0, 0)
			if errno != 0 || deathSignal != int32(unix.SIGKILL) {
				os.Exit(81)
			}
			guardPath, err := os.Readlink("/proc/self/fd/3")
			if err != nil || os.WriteFile(filepath.Join(filepath.Dir(guardPath), "wait-ready"), []byte("ready"), 0o600) != nil {
				os.Exit(66)
			}
			for {
				time.Sleep(time.Hour)
			}
		}
		switch string(payload) {
		case "status/launch-publish":
			if ordinaryWriteNativeStatus(ordinaryNativeLaunchPublish) != nil {
				os.Exit(43)
			}
			os.Exit(0)
		case "status/unknown":
			if ordinaryNativeFrameWrite(ordinaryNativeFDWriter(5), []byte{ordinaryNativeProtocolVersion, 255}) != nil {
				os.Exit(43)
			}
			_ = unix.Close(5)
			os.Exit(0)
		case "status/version":
			if ordinaryNativeFrameWrite(ordinaryNativeFDWriter(5), []byte{ordinaryNativeProtocolVersion + 1, 0}) != nil {
				os.Exit(43)
			}
			_ = unix.Close(5)
			os.Exit(0)
		}
		if err := os.WriteFile(string(payload), []byte("executed"), 0o600); err != nil {
			os.Exit(42)
		}
		if ordinaryWriteNativeStatus(ordinaryNativeSucceeded) != nil {
			os.Exit(43)
		}
		os.Exit(0)
	}
	if len(os.Args) == 5 && os.Args[1] == "_ordinary-test-driver" {
		fd, err := unix.Open(os.Args[3], unix.O_RDWR|unix.O_CLOEXEC, 0)
		if err != nil || unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB) != nil {
			os.Exit(44)
		}
		file, err := os.Open(os.Args[2])
		if err != nil {
			os.Exit(45)
		}
		digest, err := ordinaryTargetExecutable(file)
		_ = file.Close()
		if err != nil {
			os.Exit(46)
		}
		child, identity, err := ordinarySpawnNative(context.Background(), ordinaryNativeBinding{OperationID: "fixture", IntentSHA256: sha256.Sum256([]byte("intent")), AdminPath: os.Args[2], AdminSHA256: digest, GuardFD: fd})
		if err != nil {
			os.Exit(47)
		}
		if ordinaryNativeFrameWrite(child.control, []byte(filepath.Join(filepath.Dir(os.Args[3]), "effect"))) != nil {
			os.Exit(49)
		}
		if os.WriteFile(os.Args[4], []byte(strconv.Itoa(identity.PID)), 0o600) != nil {
			_, _ = child.Join(context.Background())
			os.Exit(48)
		}
		for {
			time.Sleep(time.Hour)
			runtime.KeepAlive(child)
		}
	}
	if len(os.Args) == 3 && os.Args[1] == "_ordinary-test-deny-kill" {
		control := os.NewFile(4, "fixture-control")
		status := os.NewFile(5, "fixture-status")
		var step [1]byte
		if _, err := io.ReadFull(control, step[:]); err != nil {
			os.Exit(67)
		}
		guardPath, err := os.Readlink("/proc/self/fd/3")
		if err != nil {
			os.Exit(68)
		}
		file, err := os.Open(os.Args[2])
		if err != nil {
			os.Exit(69)
		}
		digest, err := ordinaryTargetExecutable(file)
		_ = file.Close()
		if err != nil {
			os.Exit(70)
		}
		child, identity, err := ordinarySpawnNative(context.Background(), ordinaryNativeBinding{OperationID: "fixture", IntentSHA256: sha256.Sum256([]byte("intent")), AdminPath: os.Args[2], AdminSHA256: digest, GuardFD: 3})
		if err != nil {
			os.Exit(71)
		}
		_ = unix.Close(3)
		if child.Execute([]byte("wait")) != nil {
			os.Exit(72)
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(filepath.Join(filepath.Dir(guardPath), "wait-ready")); err == nil {
				break
			}
			if time.Now().After(deadline) {
				os.Exit(73)
			}
			time.Sleep(5 * time.Millisecond)
		}
		if ordinaryNativeFrameWrite(status, []byte(strconv.Itoa(identity.PID))) != nil {
			os.Exit(74)
		}
		if _, err := io.ReadFull(control, step[:]); err != nil {
			os.Exit(75)
		}
		if ordinaryTestDropKillCapability() != nil {
			os.Exit(76)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		start := time.Now()
		_, err = child.Join(ctx)
		if !errors.Is(err, unix.EPERM) || time.Since(start) > time.Second {
			os.Exit(77)
		}
		if live, err := ordinaryNativeLive(child.pidfd); err != nil || !live {
			os.Exit(78)
		}
		other, err := unix.Open(guardPath, unix.O_RDWR|unix.O_CLOEXEC, 0)
		if err != nil {
			os.Exit(79)
		}
		if err := unix.Flock(other, unix.LOCK_EX|unix.LOCK_NB); err != unix.EWOULDBLOCK {
			os.Exit(80)
		}
		_ = unix.Close(other)
		os.Exit(0)
	}
	if len(os.Args) == 4 && os.Args[1] == "_ordinary-test-missing-cap" {
		capability := uint32(unix.CAP_KILL)
		if os.Args[2] == "ptrace" {
			capability = unix.CAP_SYS_PTRACE
		} else if os.Args[2] != "kill" {
			os.Exit(82)
		}
		file, err := os.Open(os.Args[3])
		if err != nil {
			os.Exit(83)
		}
		digest, err := ordinaryTargetExecutable(file)
		_ = file.Close()
		if err != nil || ordinaryTestDropCapability(capability) != nil {
			os.Exit(84)
		}
		child, _, err := ordinarySpawnNative(context.Background(), ordinaryNativeBinding{OperationID: "fixture", IntentSHA256: sha256.Sum256([]byte("intent")), AdminPath: os.Args[3], AdminSHA256: digest, GuardFD: 3})
		if child != nil || !errors.Is(err, unix.EPERM) {
			os.Exit(85)
		}
		if _, err := ordinaryProbeTargetContract(context.Background(), os.Args[3], digest); !errors.Is(err, unix.EPERM) {
			os.Exit(89)
		}
		threads, err := os.ReadDir("/proc/self/task")
		if err != nil {
			os.Exit(86)
		}
		for _, thread := range threads {
			body, err := os.ReadFile("/proc/self/task/" + thread.Name() + "/children")
			if err != nil && !os.IsNotExist(err) {
				os.Exit(87)
			}
			if strings.TrimSpace(string(body)) != "" {
				os.Exit(88)
			}
		}
		os.Exit(0)
	}
	if len(os.Args) == 3 && os.Args[1] == "_ordinary-test-deny-pidfd" {
		file, err := os.Open(os.Args[2])
		if err != nil {
			os.Exit(60)
		}
		digest, err := ordinaryTargetExecutable(file)
		_ = file.Close()
		if err != nil || ordinaryTestDenyPidfd() != nil {
			os.Exit(61)
		}
		child, _, err := ordinarySpawnNative(context.Background(), ordinaryNativeBinding{OperationID: "fixture", IntentSHA256: sha256.Sum256([]byte("intent")), AdminPath: os.Args[2], AdminSHA256: digest, GuardFD: 3})
		if err == nil {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, _ = child.Join(ctx)
			os.Exit(62)
		}
		os.Exit(0)
	}
}

func ordinaryTestDropKillCapability() error { return ordinaryTestDropCapability(unix.CAP_KILL) }

func ordinaryTestDropCapability(capability uint32) error {
	header := unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}
	var data [2]unix.CapUserData
	if err := unix.Capget(&header, &data[0]); err != nil {
		return err
	}
	bit := uint32(1) << capability
	if data[0].Effective&bit == 0 || data[0].Permitted&bit == 0 {
		return unix.EPERM
	}
	data[0].Effective &^= bit
	data[0].Permitted &^= bit
	data[0].Inheritable &^= bit
	_, _, errno := syscall.AllThreadsSyscall6(syscall.SYS_CAPSET, uintptr(unsafe.Pointer(&header)), uintptr(unsafe.Pointer(&data[0])), 0, 0, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

func ordinaryTestDenySyscall(trap uintptr, flagsOnly bool) error {
	_, _, errno := syscall.AllThreadsSyscall6(syscall.SYS_PRCTL, unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0, 0)
	if errno != 0 {
		return errno
	}
	filter := []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jf: 1, K: uint32(trap)},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
	}
	if flagsOnly {
		filter = []unix.SockFilter{
			{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
			{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jf: 3, K: uint32(trap)},
			{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 32},
			{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jf: 1, K: unix.CLOSE_RANGE_UNSHARE | unix.CLOSE_RANGE_CLOEXEC},
			{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EINVAL)},
			{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
		}
	}
	program := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
	_, _, errno = syscall.RawSyscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, unix.SECCOMP_FILTER_FLAG_TSYNC, uintptr(unsafe.Pointer(&program)))
	if errno != 0 {
		return errno
	}
	return nil
}

func ordinaryTestDenyPidfd() error {
	_, _, errno := syscall.AllThreadsSyscall6(syscall.SYS_PRCTL, unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0, 0)
	if errno != 0 {
		return errno
	}
	filter := []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 0},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jf: 1, K: unix.SYS_CLONE3},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.ENOSYS)},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jf: 3, K: unix.SYS_CLONE},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: 16},
		{Code: unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K, Jf: 1, K: unix.CLONE_PIDFD},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)},
		{Code: unix.BPF_RET | unix.BPF_K, K: unix.SECCOMP_RET_ALLOW},
	}
	program := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
	_, _, errno = syscall.RawSyscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, unix.SECCOMP_FILTER_FLAG_TSYNC, uintptr(unsafe.Pointer(&program)))
	if errno != 0 {
		return errno
	}
	return nil
}

type ordinaryNativeFixture struct {
	dir      string
	guardDir string
	guard    *os.File
	admin    string
	digest   [32]byte
}

func ordinaryNativeTestFixture(t *testing.T) *ordinaryNativeFixture {
	t.Helper()
	if os.Getuid() != 0 || os.Geteuid() != 0 {
		t.Skip("requires root and proc identity access")
	}
	dir, err := os.MkdirTemp("/run", "ordinary-native-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chown(dir, 0, 10001); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	guardDir := filepath.Join(dir, "guard")
	if err := os.Mkdir(guardDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(guardDir, 10002, 10001); err != nil {
		t.Fatal(err)
	}
	guard, err := os.OpenFile(filepath.Join(guardDir, ordinaryGuardFile), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o640)
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.Chown(10002, 10001); err != nil {
		t.Fatal(err)
	}
	if err := guard.Chmod(0o640); err != nil {
		t.Fatal(err)
	}
	nonce := sha256.Sum256([]byte("ordinary-native-owned-test-guard"))
	if _, err := guard.Write(nonce[:]); err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(guard.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	fixture := &ordinaryNativeFixture{dir: dir, guardDir: guardDir, guard: guard, admin: filepath.Join(dir, "admin")}
	t.Cleanup(func() {
		if fixture.guard != nil {
			_ = fixture.guard.Close()
		}
	})
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.OpenFile(fixture.admin, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o550)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(output, input); err != nil {
		t.Fatal(err)
	}
	if err := output.Chown(0, 10001); err != nil {
		t.Fatal(err)
	}
	if err := output.Chmod(0o550); err != nil {
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	admin, err := os.Open(fixture.admin)
	if err != nil {
		t.Fatal(err)
	}
	fixture.digest, err = ordinaryTargetExecutable(admin)
	_ = admin.Close()
	if err != nil {
		t.Skip("requires a static test executable (build with CGO_ENABLED=0)")
	}
	return fixture
}

func (fixture *ordinaryNativeFixture) spawn(t *testing.T) (*ordinaryNativeChild, ordinaryNativeIdentity, **ordinaryNativeChild) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	child, identity, err := ordinarySpawnNative(ctx, ordinaryNativeBinding{OperationID: "fixture", IntentSHA256: sha256.Sum256([]byte("intent")), AdminPath: fixture.admin, AdminSHA256: fixture.digest, GuardFD: int(fixture.guard.Fd())})
	if err != nil {
		t.Fatalf("spawn native participant (requires CAP_SYS_PTRACE when proc restricts inspection): %v", err)
	}
	owned := child
	t.Cleanup(func() {
		if owned != nil {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, _ = owned.Join(ctx)
		}
	})
	return child, identity, &owned
}

func TestOrdinaryNativeDescriptorCanaries(t *testing.T) {
	fixture := ordinaryNativeTestFixture(t)
	var pipe [2]int
	if err := unix.Pipe2(pipe[:], unix.O_NONBLOCK); err != nil {
		t.Fatal(err)
	}
	defer unix.Close(pipe[0])
	unix.CloseOnExec(pipe[0])
	high, err := unix.FcntlInt(uintptr(pipe[1]), unix.F_DUPFD, 256)
	if err != nil {
		t.Fatal(err)
	}
	sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(sockets[0])
	unix.CloseOnExec(sockets[0])
	highSocket, err := unix.FcntlInt(uintptr(sockets[1]), unix.F_DUPFD, 300)
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	started := make(chan struct{})
	collected := make(chan [][2]int, 1)
	go func() {
		var opened [][2]int
		for len(opened) < 128 {
			var pair [2]int
			if unix.Pipe2(pair[:], unix.O_NONBLOCK) != nil {
				break
			}
			unix.CloseOnExec(pair[0])
			opened = append(opened, pair)
			if len(opened) == 1 {
				close(started)
			}
			select {
			case <-stop:
				collected <- opened
				return
			default:
			}
			runtime.Gosched()
		}
		if len(opened) == 0 {
			close(started)
		}
		<-stop
		collected <- opened
	}()
	<-started
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			close(stop)
			for _, pair := range <-collected {
				_ = unix.Close(pair[0])
				_ = unix.Close(pair[1])
			}
		}
	})
	child, identity, owned := fixture.spawn(t)
	close(stop)
	batch := <-collected
	stopped = true
	_ = unix.Close(pipe[1])
	_ = unix.Close(high)
	_ = unix.Close(sockets[1])
	_ = unix.Close(highSocket)
	for _, pair := range batch {
		_ = unix.Close(pair[1])
	}
	defer func() {
		for _, pair := range batch {
			_ = unix.Close(pair[0])
		}
	}()
	var one [1]byte
	if n, err := unix.Read(pipe[0], one[:]); n != 0 || err != nil {
		t.Fatalf("inherited pipe retained: n=%d err=%v", n, err)
	}
	if n, err := unix.Read(sockets[0], one[:]); n != 0 || err != nil {
		t.Fatalf("inherited socket retained: n=%d err=%v", n, err)
	}
	for _, pair := range batch {
		if n, err := unix.Read(pair[0], one[:]); n != 0 || err != nil {
			t.Fatalf("concurrent pipe retained: n=%d err=%v", n, err)
		}
	}
	canaries, err := os.ReadFile(filepath.Join(fixture.guardDir, "prep-canaries"))
	if err != nil {
		t.Fatal(err)
	}
	identities := make(map[string]bool)
	for _, line := range strings.Split(string(canaries), "\n") {
		identities[line] = true
	}
	entries, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", identity.PID))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		var stat unix.Stat_t
		err := unix.Stat(fmt.Sprintf("/proc/%d/fd/%s", identity.PID, entry.Name()), &stat)
		if err == unix.ENOENT {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if identities[fmt.Sprintf("%d:%d", stat.Dev, stat.Ino)] {
			t.Fatalf("preparation canary retained on fd %s", entry.Name())
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = child.Join(ctx)
	*owned = nil
}

func TestOrdinaryNativeTargetContractProbe(t *testing.T) {
	fixture := ordinaryNativeTestFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	body, err := ordinaryProbeTargetContract(ctx, fixture.admin, fixture.digest)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := ordinaryDecodeTargetContract(body)
	if err != nil || contract.SchemaVersion == 0 {
		t.Fatalf("target contract: %v", err)
	}
	wrongDigest := fixture.digest
	wrongDigest[0] ^= 1
	if _, err := ordinaryProbeTargetContract(ctx, fixture.admin, wrongDigest); err == nil {
		t.Fatal("wrong target digest accepted")
	}
}

func TestOrdinaryNativeProbeDescriptorCanaries(t *testing.T) {
	fixture := ordinaryNativeTestFixture(t)
	if err := os.WriteFile(filepath.Join(fixture.guardDir, "probe-gate"), []byte("gate"), 0o600); err != nil {
		t.Fatal(err)
	}
	var pipe [2]int
	if err := unix.Pipe2(pipe[:], unix.O_NONBLOCK); err != nil {
		t.Fatal(err)
	}
	defer unix.Close(pipe[0])
	unix.CloseOnExec(pipe[0])
	sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(sockets[0])
	unix.CloseOnExec(sockets[0])
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := ordinaryProbeTargetContract(ctx, fixture.admin, fixture.digest); result <- err }()
	deadline := time.Now().Add(10 * time.Second)
	var pid int
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(filepath.Join(fixture.guardDir, "probe-ready"))
		if err == nil {
			pid, err = strconv.Atoi(string(body))
			if err != nil {
				t.Fatal(err)
			}
			break
		}
		select {
		case err := <-result:
			t.Fatalf("probe exited before readiness: %v", err)
		default:
		}
		time.Sleep(5 * time.Millisecond)
	}
	if pid <= 0 {
		t.Fatal("probe target did not reach gate")
	}
	probeFD, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(probeFD)
	if live, err := ordinaryNativeLive(probeFD); err != nil || !live {
		t.Fatal("probe target not live")
	}
	_ = unix.Close(pipe[1])
	_ = unix.Close(sockets[1])
	var one [1]byte
	if n, err := unix.Read(pipe[0], one[:]); n != 0 || err != nil {
		t.Fatalf("probe retained pipe: n=%d err=%v", n, err)
	}
	if n, err := unix.Read(sockets[0], one[:]); n != 0 || err != nil {
		t.Fatalf("probe retained socket: n=%d err=%v", n, err)
	}
	if err := os.WriteFile(filepath.Join(fixture.guardDir, "probe-release"), []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestOrdinaryNativeTerminalSyscallRefusal(t *testing.T) {
	for _, denial := range []string{"deny-close-range", "deny-close-range-flags", "deny-execveat"} {
		t.Run(denial, func(t *testing.T) {
			fixture := ordinaryNativeTestFixture(t)
			if err := os.WriteFile(filepath.Join(fixture.guardDir, denial), []byte("deny"), 0o600); err != nil {
				t.Fatal(err)
			}
			controlRead, controlWrite, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer controlRead.Close()
			defer controlWrite.Close()
			statusRead, statusWrite, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer statusRead.Close()
			defer statusWrite.Close()
			admin, err := os.Open(fixture.admin)
			if err != nil {
				t.Fatal(err)
			}
			defer admin.Close()
			pidfd := -1
			command := exec.Command("/proc/self/exe", "_ordinary-launch-target", "participant", hex.EncodeToString(fixture.digest[:]))
			command.Env = []string{}
			command.ExtraFiles = []*os.File{fixture.guard, controlRead, statusWrite, admin}
			command.SysProcAttr = &syscall.SysProcAttr{PidFD: &pidfd, Pdeathsig: syscall.SIGKILL, Credential: &syscall.Credential{Uid: 10002, Gid: 10001, Groups: []uint32{}}}
			waited, err := ordinaryStartLocked(command)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(pidfd)
			defer unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0)
			_ = controlRead.Close()
			_ = statusWrite.Close()
			select {
			case err := <-waited:
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 127 {
					t.Fatalf("terminal syscall refusal exit: %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("preparation process survived terminal syscall failure")
			}
			body, err := os.ReadFile(filepath.Join(fixture.guardDir, "filter-ready"))
			if err != nil || string(body) != denial {
				t.Fatalf("filter was not installed: %q %v", body, err)
			}
			if _, err := ordinaryNativeFrameRead(statusRead, 32); err == nil {
				t.Fatal("preparation sent target readiness")
			}
		})
	}
}

func TestOrdinaryNativeClonePidfdRefusal(t *testing.T) {
	fixture := ordinaryNativeTestFixture(t)
	pidfd := -1
	command := exec.Command("/proc/self/exe", "_ordinary-test-deny-pidfd", fixture.admin)
	command.ExtraFiles = []*os.File{fixture.guard}
	command.SysProcAttr = &syscall.SysProcAttr{PidFD: &pidfd}
	waited, err := ordinaryStartLocked(command)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(pidfd)
	defer unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0)
	select {
	case err := <-waited:
		if err != nil {
			t.Fatalf("pidfd denial fixture: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("pidfd denial fixture did not finish")
	}
}

func TestOrdinaryNativeMissingControllerCapability(t *testing.T) {
	for _, capability := range []string{"kill", "ptrace"} {
		t.Run(capability, func(t *testing.T) {
			fixture := ordinaryNativeTestFixture(t)
			pidfd := -1
			command := exec.Command("/proc/self/exe", "_ordinary-test-missing-cap", capability, fixture.admin)
			command.ExtraFiles = []*os.File{fixture.guard}
			command.SysProcAttr = &syscall.SysProcAttr{PidFD: &pidfd}
			waited, err := ordinaryStartLocked(command)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(pidfd)
			defer unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0)
			select {
			case err := <-waited:
				if err != nil {
					t.Fatalf("capability preflight fixture: %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("capability preflight did not finish")
			}
		})
	}
}

func TestOrdinaryNativeJoinWithoutKillCapability(t *testing.T) {
	fixture := ordinaryNativeTestFixture(t)
	controlRead, controlWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer controlRead.Close()
	defer controlWrite.Close()
	statusRead, statusWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer statusRead.Close()
	defer statusWrite.Close()
	pidfd := -1
	command := exec.Command("/proc/self/exe", "_ordinary-test-deny-kill", fixture.admin)
	command.ExtraFiles = []*os.File{fixture.guard, controlRead, statusWrite}
	command.SysProcAttr = &syscall.SysProcAttr{PidFD: &pidfd}
	waited, err := ordinaryStartLocked(command)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(pidfd)
	defer unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0)
	_ = controlRead.Close()
	_ = statusWrite.Close()
	if err := fixture.guard.Close(); err != nil {
		t.Fatal(err)
	}
	fixture.guard = nil
	if _, err := controlWrite.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	_ = statusRead.SetReadDeadline(time.Now().Add(15 * time.Second))
	body, err := ordinaryNativeFrameRead(statusRead, 32)
	if err != nil {
		t.Fatalf("live participant identity: %v", err)
	}
	participantPID, err := strconv.Atoi(string(body))
	if err != nil || participantPID <= 0 {
		t.Fatalf("participant PID: %q %v", body, err)
	}
	participantFD, err := unix.PidfdOpen(participantPID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(participantFD)
	defer unix.PidfdSendSignal(participantFD, unix.SIGKILL, nil, 0)
	if _, err := controlWrite.Write([]byte{2}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-waited:
		if err != nil {
			t.Fatalf("capability-drop driver: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Join hung after SIGKILL was denied")
	}
	if err := unix.PidfdSendSignal(participantFD, unix.SIGKILL, nil, 0); err != nil {
		t.Fatalf("privileged participant kill: %v", err)
	}
	exitDeadline := time.Now().Add(10 * time.Second)
	for {
		poll := []unix.PollFd{{Fd: int32(participantFD), Events: unix.POLLIN}}
		remaining := time.Until(exitDeadline)
		if remaining <= 0 {
			t.Fatal("participant did not exit after privileged kill")
		}
		count, err := unix.Poll(poll, int(remaining.Milliseconds())+1)
		if err == unix.EINTR {
			continue
		}
		if err != nil || count != 1 {
			t.Fatalf("participant did not exit after privileged kill: %d %v", count, err)
		}
		break
	}
}

func TestOrdinaryNativeExecuteAndJoin(t *testing.T) {
	fixture := ordinaryNativeTestFixture(t)
	child, _, owned := fixture.spawn(t)
	marker := filepath.Join(fixture.guardDir, "effect")
	if err := child.Execute([]byte(marker)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	status, err := child.Join(ctx)
	*owned = nil
	if err != nil || status != ordinaryNativeSucceeded {
		t.Fatalf("join: %q %v", status, err)
	}
	body, err := os.ReadFile(marker)
	if err != nil || string(body) != "executed" {
		t.Fatalf("effect: %q %v", body, err)
	}
	if _, err := child.Join(ctx); err == nil {
		t.Fatal("second join accepted")
	}
}

func TestOrdinaryNativeParticipantStatusCodes(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		status        ordinaryNativeStatus
		wantError     bool
	}{
		{"label-roundtrip", "status/launch-publish", ordinaryNativeLaunchPublish, false},
		{"unknown-refused", "status/unknown", "", true},
		{"version-refused", "status/version", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := ordinaryNativeTestFixture(t)
			child, _, owned := fixture.spawn(t)
			if err := child.Execute([]byte(tc.payload)); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			status, err := child.Join(ctx)
			*owned = nil
			if tc.wantError {
				if !errors.Is(err, errOrdinaryUpgrade) {
					t.Fatalf("unknown status was not refused: %q %v", status, err)
				}
				return
			}
			if err != nil || status != tc.status {
				t.Fatalf("status did not round trip: %q %v", status, err)
			}
			rendered := ordinaryUpgradePhase("participant-execute/"+string(status), errOrdinaryUpgrade)
			if !errors.Is(rendered, errOrdinaryUpgrade) || !strings.Contains(rendered.Error(), "participant-execute/launch-publish") {
				t.Fatalf("participant label was not rendered: %v", rendered)
			}
		})
	}
}

func TestOrdinaryNativeDockerSocketRefusal(t *testing.T) {
	var before unix.Stat_t
	err := unix.Stat("/var/run/docker.sock", &before)
	if err == unix.ENOENT {
		t.Skip("requires a real Docker socket in the fixture namespace")
	}
	if err != nil || before.Mode&unix.S_IFMT != unix.S_IFSOCK {
		t.Fatalf("Docker socket: %v", err)
	}
	fixture := ordinaryNativeTestFixture(t)
	child, _, owned := fixture.spawn(t)
	var after unix.Stat_t
	if unix.Stat("/var/run/docker.sock", &after) != nil || before.Dev != after.Dev || before.Ino != after.Ino {
		t.Fatal("Docker socket changed during refusal check")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = child.Join(ctx)
	*owned = nil
}

func TestOrdinaryNativeEOFAndInheritedExclusiveLock(t *testing.T) {
	fixture := ordinaryNativeTestFixture(t)
	child, _, owned := fixture.spawn(t)
	if err := fixture.guard.Close(); err != nil {
		t.Fatal(err)
	}
	fixture.guard = nil
	other, err := os.OpenFile(filepath.Join(fixture.guardDir, ordinaryGuardFile), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := unix.Flock(int(other.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != unix.EWOULDBLOCK {
		t.Fatalf("child did not retain exclusive OFD: %v", err)
	}
	marker := filepath.Join(fixture.guardDir, "effect")
	if err := ordinaryNativeFrameWrite(child.control, []byte(marker)); err != nil {
		t.Fatal(err)
	}
	if err := child.control.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = child.Join(ctx)
	*owned = nil
	if err == nil {
		t.Fatal("missing ACK accepted")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("effect before ACK: %v", err)
	}
}

func TestOrdinaryNativeRejectsMalformedACK(t *testing.T) {
	for _, ack := range [][]byte{[]byte("NACK"), []byte("ACKACK")} {
		t.Run(string(ack), func(t *testing.T) {
			fixture := ordinaryNativeTestFixture(t)
			child, _, owned := fixture.spawn(t)
			marker := filepath.Join(fixture.guardDir, "effect")
			if err := ordinaryNativeFrameWrite(child.control, []byte(marker)); err != nil {
				t.Fatal(err)
			}
			if err := ordinaryNativeFrameWrite(child.control, ack); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, err := child.Join(ctx)
			*owned = nil
			if err == nil {
				t.Fatal("malformed ACK accepted")
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("effect before valid ACK: %v", err)
			}
		})
	}
}

func TestOrdinaryNativeRecoveryIdentityAndCancellation(t *testing.T) {
	fixture := ordinaryNativeTestFixture(t)
	child, identity, owned := fixture.spawn(t)
	wrongStart := identity
	wrongStart.StartTime++
	if err := ordinaryJoinRecordedNative(context.Background(), wrongStart, fixture.guardDir); err != nil {
		t.Fatalf("reused PID: %v", err)
	}
	wrongGuard := identity
	wrongGuard.GuardInode++
	if err := ordinaryJoinRecordedNative(context.Background(), wrongGuard, fixture.guardDir); err == nil {
		t.Fatal("wrong guard accepted")
	}
	wrongExecutable := identity
	wrongExecutable.ExecutableSHA256[0] ^= 1
	if err := ordinaryJoinRecordedNative(context.Background(), wrongExecutable, fixture.guardDir); err == nil {
		t.Fatal("wrong executable accepted")
	}
	if live, err := ordinaryNativeLive(child.pidfd); err != nil || !live {
		t.Fatalf("unrelated participant signalled: %v", err)
	}
	if err := child.Execute([]byte("wait")); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := child.Join(ctx)
	*owned = nil
	if err == nil {
		t.Fatal("canceled join reported success")
	}
}

func TestOrdinaryNativeRecordedRecovery(t *testing.T) {
	fixture := ordinaryNativeTestFixture(t)
	child, identity, owned := fixture.spawn(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := ordinaryJoinRecordedNative(ctx, identity, fixture.guardDir); err != nil {
		t.Fatalf("recorded join: %v", err)
	}
	_, err := child.Join(ctx)
	*owned = nil
	if err == nil {
		t.Fatal("killed participant reported success")
	}
}

func TestOrdinaryNativeCreatingThreadDeath(t *testing.T) {
	fixture := ordinaryNativeTestFixture(t)
	controlRead, controlWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer controlRead.Close()
	defer controlWrite.Close()
	statusRead, statusWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer statusRead.Close()
	defer statusWrite.Close()
	admin, err := os.Open(fixture.admin)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	pidfd := -1
	command := exec.Command("/proc/self/exe", "_ordinary-launch-target", "participant", hex.EncodeToString(fixture.digest[:]))
	command.Env = []string{}
	command.ExtraFiles = []*os.File{fixture.guard, controlRead, statusWrite, admin}
	command.SysProcAttr = &syscall.SysProcAttr{PidFD: &pidfd, Pdeathsig: syscall.SIGKILL, Credential: &syscall.Credential{Uid: 10002, Gid: 10001, Groups: []uint32{}}}
	started := make(chan error, 1)
	release := make(chan struct{})
	var create func()
	create = func() {
		runtime.LockOSThread()
		if unix.Gettid() == unix.Getpid() {
			// Go never destroys the main OS thread when a locked goroutine
			// exits; it only wedges it, so Pdeathsig would not fire. Keep this
			// thread occupied and create from a different, destroyable thread.
			go create()
			<-release
			return
		}
		started <- command.Start()
		<-release
		// Returning while locked destroys this non-main creating OS thread.
	}
	go create()
	if err := <-started; err != nil {
		close(release)
		t.Fatal(err)
	}
	defer unix.Close(pidfd)
	defer unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0)
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	_ = controlRead.Close()
	_ = statusWrite.Close()
	anchor, err := ordinaryGuardIdentity(int(fixture.guard.Fd()))
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	wire := ordinaryNativeWireBinding{OperationID: "fixture", IntentSHA256: sha256.Sum256([]byte("intent")), AdminPath: fixture.admin, AdminSHA256: fixture.digest, GuardDev: uint64(anchor.stat.Dev), GuardInode: anchor.stat.Ino, GuardNonceSHA256: sha256.Sum256(anchor.nonce[:])}
	body, err := json.Marshal(wire)
	if err != nil || ordinaryNativeFrameWrite(controlWrite, body) != nil {
		close(release)
		t.Fatal("write binding")
	}
	_ = statusRead.SetReadDeadline(time.Now().Add(10 * time.Second))
	ready, err := ordinaryNativeFrameRead(statusRead, 32)
	if err != nil || string(ready) != "READY" {
		close(release)
		t.Fatalf("participant not ready: %q %v", ready, err)
	}
	close(release)
	select {
	case err := <-waited:
		if err == nil {
			t.Fatal("participant survived creator thread death")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("participant did not exit after creator thread death")
	}
}

func TestOrdinaryNativeParentDeathBeforeACK(t *testing.T) {
	fixture := ordinaryNativeTestFixture(t)
	if err := fixture.guard.Close(); err != nil {
		t.Fatal(err)
	}
	fixture.guard = nil
	ready := filepath.Join(fixture.dir, "ready")
	pidfd := -1
	cmd := exec.Command(fixture.admin, "_ordinary-test-driver", fixture.admin, filepath.Join(fixture.guardDir, ordinaryGuardFile), ready)
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{PidFD: &pidfd}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	defer func() {
		if pidfd >= 0 {
			_ = unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0)
			_ = unix.Close(pidfd)
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	var participantPID int
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(ready)
		if err == nil {
			participantPID, err = strconv.Atoi(strings.TrimSpace(string(body)))
			if err != nil {
				t.Fatal(err)
			}
			break
		}
		select {
		case err := <-waited:
			t.Fatalf("driver exited before ready: %v", err)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	if participantPID <= 0 {
		t.Fatal("driver did not publish child identity")
	}
	participantFD, err := unix.PidfdOpen(participantPID, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(participantFD)
	if err := unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0); err != nil {
		t.Fatal(err)
	}
	<-waited
	_ = unix.Close(pidfd)
	pidfd = -1
	// Go runtime preemption signals can interrupt poll; retry until the
	// participant's pidfd is readable or the deadline elapses.
	exitDeadline := time.Now().Add(10 * time.Second)
	for {
		poll := []unix.PollFd{{Fd: int32(participantFD), Events: unix.POLLIN}}
		remaining := time.Until(exitDeadline)
		if remaining <= 0 {
			t.Fatal("participant survived parent death")
		}
		count, err := unix.Poll(poll, int(remaining.Milliseconds())+1)
		if err == unix.EINTR {
			continue
		}
		if err != nil || count != 1 {
			t.Fatalf("participant survived parent death: %d %v", count, err)
		}
		break
	}
	if _, err := os.Stat(filepath.Join(fixture.guardDir, "effect")); !os.IsNotExist(err) {
		t.Fatal(fmt.Errorf("effect before ACK: %w", err))
	}
}
