//go:build linux && amd64 && !cgo

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// The confinement filter is irreversible, so exercise it only in a bounded
// child. The parent checks that its own thread has not been confined.
func TestAuditReaderConfinementCloneABI(t *testing.T) {
	if os.Getenv("G2_AUDIT_READER_FILTER_TEST_CHILD") == "1" {
		testAuditReaderConfinementCloneABIChild(t)
		return
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	before, err := auditReaderTestThreadStatus()
	if err != nil {
		t.Fatal(err)
	}
	if got := auditReaderTestInvalidExec(); got != unix.EFAULT {
		t.Fatalf("parent execve(NULL) before child = %v, want EFAULT", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAuditReaderConfinementCloneABI$", "-test.v", "-test.timeout=25s")
	cmd.Env = append(os.Environ(), "G2_AUDIT_READER_FILTER_TEST_CHILD=1")
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("confined child timed out: %v\n%s", ctx.Err(), output)
	}
	if err != nil {
		t.Fatalf("confined child failed: %v\n%s", err, output)
	}

	after, err := auditReaderTestThreadStatus()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range auditReaderTestStatusKeys {
		if after[key] != before[key] {
			t.Errorf("parent %s changed after child: %q -> %q", key, before[key], after[key])
		}
	}
	if got := auditReaderTestInvalidExec(); got != unix.EFAULT {
		t.Fatalf("parent execve(NULL) after child = %v, want EFAULT", got)
	}
}

func testAuditReaderConfinementCloneABIChild(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Keep a known pre-existing M occupied across both TSYNC installations.
	// Its subsequent executable probe must see the actual production filter.
	existingReady := make(chan int, 1)
	checkExisting := make(chan struct{})
	existingResult := make(chan auditReaderTestThreadResult, 1)
	releaseExisting := make(chan struct{})
	existingDone := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(existingDone)
		existingReady <- unix.Gettid()
		<-checkExisting
		existingResult <- auditReaderTestProbeThread()
		<-releaseExisting
	}()

	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	var existingTID int
	select {
	case existingTID = <-existingReady:
	case <-deadline.C:
		t.Fatal("pre-existing locked thread did not start")
	}

	if err := auditReaderTestInstallSafetyFilter(); err != nil {
		t.Fatalf("install child safety filter: %v", err)
	}
	if err := auditReaderFilter(); err != nil {
		t.Fatalf("install production confinement filter: %v", err)
	}

	status, err := auditReaderTestThreadStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status["NoNewPrivs"] != "1" || status["Seccomp"] != "2" {
		t.Fatalf("filtered thread status: NoNewPrivs=%q Seccomp=%q", status["NoNewPrivs"], status["Seccomp"])
	}

	// Both exact permitted values are probed with a null stack. The older
	// safety filter returns ENOSYS for these probes, so no raw clone can
	// create a process or a thread; a production denial would return EPERM.
	for _, flags := range []uint64{0x50f00, 0xd0f00} {
		if got := auditReaderTestClone(flags); got != unix.ENOSYS {
			t.Errorf("clone flags %#x = %v, want safety-filter ENOSYS", flags, got)
		}
		// Exercise every single-bit deviation, including all upper 32 bits.
		for bit := range 64 {
			candidate := flags ^ (uint64(1) << bit)
			want := unix.EPERM
			if candidate == 0x50f00 || candidate == 0xd0f00 {
				want = unix.ENOSYS
			}
			if got := auditReaderTestClone(candidate); got != want {
				t.Errorf("clone flags %#x = %v, want %v", candidate, got, want)
			}
		}
	}
	for _, flags := range []uint64{
		0, uint64(unix.SIGCHLD), 0x50f00 &^ uint64(unix.CLONE_THREAD),
		0xd0f00 | uint64(unix.CLONE_NEWUSER), 0xd0f00 | uint64(unix.CLONE_NEWNS),
		0xd0f00 | uint64(unix.CLONE_NEWPID), 0xffffffff000d0f00,
	} {
		if got := auditReaderTestClone(flags); got != unix.EPERM {
			t.Errorf("forbidden clone flags %#x = %v, want EPERM", flags, got)
		}
	}

	// The safety filter catches these calls first if the production filter
	// ever stops rejecting one, so even fork or exec cannot escape the child.
	for _, call := range []struct {
		name string
		nr   uintptr
	}{
		{"execveat", unix.SYS_EXECVEAT}, {"bpf", unix.SYS_BPF},
		{"init_module", unix.SYS_INIT_MODULE}, {"finit_module", unix.SYS_FINIT_MODULE}, {"delete_module", unix.SYS_DELETE_MODULE},
		{"unshare", unix.SYS_UNSHARE}, {"setns", unix.SYS_SETNS},
		{"fork", unix.SYS_FORK}, {"vfork", unix.SYS_VFORK}, {"clone3", unix.SYS_CLONE3},
		{"setuid", unix.SYS_SETUID}, {"setgid", unix.SYS_SETGID}, {"setreuid", unix.SYS_SETREUID}, {"setregid", unix.SYS_SETREGID},
		{"setresuid", unix.SYS_SETRESUID}, {"setresgid", unix.SYS_SETRESGID}, {"setfsuid", unix.SYS_SETFSUID}, {"setfsgid", unix.SYS_SETFSGID},
		{"setgroups", unix.SYS_SETGROUPS}, {"capset", unix.SYS_CAPSET}, {"prctl", unix.SYS_PRCTL}, {"ptrace", unix.SYS_PTRACE},
	} {
		_, _, got := unix.RawSyscall(call.nr, 0, 0, 0)
		if got != unix.EPERM {
			t.Errorf("%s = %v, want EPERM", call.name, got)
		}
	}
	if got := auditReaderTestExecutableExec(); got != unix.EPERM {
		t.Errorf("execve(/proc/self/exe) = %v, want EPERM", got)
	}
	if pid, _, errno := unix.RawSyscall(unix.SYS_GETPID, 0, 0, 0); errno != 0 || int(pid) != os.Getpid() {
		t.Errorf("getpid after confinement = %d, %v", pid, errno)
	}

	close(checkExisting)
	select {
	case result := <-existingResult:
		if result.tid != existingTID {
			t.Errorf("pre-existing locked thread changed TID: %d -> %d", existingTID, result.tid)
		}
		auditReaderTestCheckThread(t, "pre-existing thread", result, status)
	case <-deadline.C:
		t.Fatal("pre-existing locked thread did not return its confinement probe")
	}

	// Snapshot after TSYNC, then occupy more distinct locked threads than
	// existed in that snapshot. At least one reported TID must be genuinely
	// new after installation, rather than a recycled idle Go M.
	entries, err := os.ReadDir("/proc/self/task")
	if err != nil {
		t.Fatal(err)
	}
	prior := make(map[int]bool, len(entries))
	for _, entry := range entries {
		tid, err := strconv.Atoi(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		prior[tid] = true
	}
	threads := len(prior) + 1
	if threads < 16 {
		threads = 16
	}
	if threads > 256 {
		t.Fatalf("unexpected pre-existing thread count %d", len(prior))
	}
	ready := make(chan auditReaderTestThreadResult, threads)
	release := make(chan struct{})
	done := make(chan struct{}, threads)
	for range threads {
		go func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			ready <- auditReaderTestProbeThread()
			<-release
			done <- struct{}{}
		}()
	}
	seen := make(map[int]bool, threads)
	newThreads := 0
	for range threads {
		select {
		case result := <-ready:
			if seen[result.tid] {
				t.Errorf("locked workers shared TID %d", result.tid)
			}
			seen[result.tid] = true
			if !prior[result.tid] {
				newThreads++
			}
			auditReaderTestCheckThread(t, "post-filter thread", result, status)
		case <-deadline.C:
			t.Fatal("filtered runtime could not start all locked threads")
		}
	}
	if newThreads == 0 {
		t.Error("no new OS thread was observed after TSYNC")
	}
	close(release)
	close(releaseExisting)
	for range threads {
		select {
		case <-done:
		case <-deadline.C:
			t.Fatal("filtered workers did not exit")
		}
	}
	select {
	case <-existingDone:
	case <-deadline.C:
		t.Fatal("pre-existing worker did not exit")
	}
}

var auditReaderTestStatusKeys = []string{"Uid", "Gid", "CapInh", "CapPrm", "CapEff", "CapBnd", "CapAmb", "NoNewPrivs", "Seccomp"}

func auditReaderTestThreadStatus() (map[string]string, error) {
	raw, err := os.ReadFile("/proc/thread-self/status")
	if err != nil {
		return nil, err
	}
	values := make(map[string]string, len(auditReaderTestStatusKeys))
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		for _, wanted := range auditReaderTestStatusKeys {
			if key == wanted {
				if _, exists := values[key]; exists {
					return nil, fmt.Errorf("duplicate thread status field %s", key)
				}
				values[key] = strings.TrimSpace(value)
				break
			}
		}
	}
	if len(values) != len(auditReaderTestStatusKeys) {
		return nil, fmt.Errorf("missing thread status fields: %v", values)
	}
	return values, nil
}

type auditReaderTestThreadResult struct {
	tid    int
	exec   unix.Errno
	status map[string]string
	err    error
}

func auditReaderTestProbeThread() auditReaderTestThreadResult {
	result := auditReaderTestThreadResult{tid: unix.Gettid(), exec: auditReaderTestExecutableExec()}
	result.status, result.err = auditReaderTestThreadStatus()
	return result
}

func auditReaderTestCheckThread(t *testing.T, name string, result auditReaderTestThreadResult, expected map[string]string) {
	t.Helper()
	if result.err != nil {
		t.Errorf("%s status: %v", name, result.err)
		return
	}
	if result.exec != unix.EPERM {
		t.Errorf("%s TID %d execve(/proc/self/exe) = %v, want EPERM", name, result.tid, result.exec)
	}
	for _, key := range auditReaderTestStatusKeys {
		if result.status[key] != expected[key] {
			t.Errorf("%s TID %d %s = %q, want %q", name, result.tid, key, result.status[key], expected[key])
		}
	}
}

func auditReaderTestInvalidExec() unix.Errno {
	_, _, errno := unix.RawSyscall(unix.SYS_EXECVE, 0, 0, 0)
	return errno
}

func auditReaderTestExecutableExec() unix.Errno {
	path := []byte("/proc/self/exe\x00")
	argv := [2]*byte{&path[0], nil}
	_, _, errno := unix.RawSyscall(unix.SYS_EXECVE, uintptr(unsafe.Pointer(&path[0])), uintptr(unsafe.Pointer(&argv[0])), 0)
	runtime.KeepAlive(path)
	runtime.KeepAlive(argv)
	return errno
}

func auditReaderTestClone(flags uint64) unix.Errno {
	_, _, errno := unix.RawSyscall6(unix.SYS_CLONE, uintptr(flags), 0, 0, 0, 0, 0)
	return errno
}

// Installed before the filter under test. For equal-precedence ERRNO actions,
// Linux selects the data from the most recently installed matching filter.
// Thus production EPERM remains distinguishable from this ENOSYS backstop.
// The sole exception permits the pinned Go runtime's non-null-stack clone so
// the real runtime can create new Ms after production TSYNC.
func auditReaderTestInstallSafetyFilter() error {
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return err
	}
	const load = unix.BPF_LD | unix.BPF_W | unix.BPF_ABS
	const eq = unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K
	const ret = unix.BPF_RET | unix.BPF_K
	const safe = unix.SECCOMP_RET_ERRNO | uint32(unix.ENOSYS)
	ins := []unix.SockFilter{{Code: load, K: 0}}
	for _, nr := range []uint32{
		unix.SYS_EXECVE, unix.SYS_EXECVEAT, unix.SYS_BPF,
		unix.SYS_INIT_MODULE, unix.SYS_FINIT_MODULE, unix.SYS_DELETE_MODULE,
		unix.SYS_UNSHARE, unix.SYS_SETNS, unix.SYS_FORK, unix.SYS_VFORK, unix.SYS_CLONE3,
		unix.SYS_SETUID, unix.SYS_SETGID, unix.SYS_SETREUID, unix.SYS_SETREGID,
		unix.SYS_SETRESUID, unix.SYS_SETRESGID, unix.SYS_SETFSUID, unix.SYS_SETFSGID,
		unix.SYS_SETGROUPS, unix.SYS_CAPSET, unix.SYS_PRCTL, unix.SYS_PTRACE,
	} {
		ins = append(ins, unix.SockFilter{Code: eq, K: nr, Jf: 1}, unix.SockFilter{Code: ret, K: safe})
	}
	ins = append(ins,
		unix.SockFilter{Code: eq, K: unix.SYS_CLONE, Jf: 9},
		unix.SockFilter{Code: load, K: 20},
		unix.SockFilter{Code: eq, K: 0, Jf: 6},
		unix.SockFilter{Code: load, K: 16},
		unix.SockFilter{Code: eq, K: 0xd0f00, Jf: 4},
		unix.SockFilter{Code: load, K: 28},
		unix.SockFilter{Code: eq, K: 0, Jf: 3},
		unix.SockFilter{Code: load, K: 24},
		unix.SockFilter{Code: eq, K: 0, Jf: 1},
		unix.SockFilter{Code: ret, K: safe},
		unix.SockFilter{Code: ret, K: unix.SECCOMP_RET_ALLOW},
	)
	prog := unix.SockFprog{Len: uint16(len(ins)), Filter: &ins[0]}
	result, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, unix.SECCOMP_FILTER_FLAG_TSYNC, uintptr(unsafe.Pointer(&prog)))
	runtime.KeepAlive(ins)
	if errno != 0 {
		return errno
	}
	if result != 0 {
		return fmt.Errorf("seccomp synchronization failed on thread %d", result)
	}
	return nil
}
