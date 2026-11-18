//go:build linux && amd64 && !cgo

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// This exercises the actual registration commit/replay and transport lifecycle
// boundaries in an isolated root, without a guard-backed REGISTER_LAUNCH.
func TestOwnerProbeSettlementRuntimeRootRegistration(t *testing.T) {
	mode := os.Getenv("G2_SETTLEMENT_ROOT_CHILD")
	if mode == "other-client" {
		var command [1]byte
		if _, err := os.Stdout.Write([]byte{'R'}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.ReadFull(os.Stdin, command[:]); err != nil {
			t.Fatal(err)
		}
		fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(fd)
		if err = unix.Connect(fd, &unix.SockaddrUnix{Name: "/proc/self/fd/3/run/g2/owner-probe-settlement-evidence/broker.sock"}); err != nil {
			t.Fatal(err)
		}
		hello, err := canonicalControl(map[string]any{"version": jsonU64(1), "purpose": "postgres-uid", "nonce": strings.Repeat("1", 32), "principal_id": jsonU64(1)})
		if err != nil || sendControl(fd, hello, nil) != nil {
			t.Fatal("other client HELLO", err)
		}
		if _, err = os.Stdout.Write([]byte{'S'}); err != nil {
			t.Fatal(err)
		}
		if _, err = io.ReadFull(os.Stdin, command[:]); err != nil {
			t.Fatal(err)
		}
		return
	}
	if mode == "" {
		ownerRootPrerequisites(t)
		for _, name := range []string{"success", "conflict", "substitution"} {
			t.Run(name, func(t *testing.T) {
				fixture := t.TempDir()
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOwnerProbeSettlementRuntimeRootRegistration$", "-test.timeout=25s")
				cmd.Env = append(os.Environ(), "G2_SETTLEMENT_ROOT_CHILD="+name, "G2_SETTLEMENT_ROOT_DIR="+fixture)
				cmd.SysProcAttr = &syscall.SysProcAttr{Cloneflags: unix.CLONE_NEWNS, Pdeathsig: syscall.SIGKILL}
				out, err := cmd.CombinedOutput()
				if err != nil || ctx.Err() != nil {
					t.Fatalf("isolated root registration: %v context=%v\n%s", err, ctx.Err(), out)
				}
			})
		}
		return
	}

	ownerRootPrerequisites(t)
	rootTestPinProc(t)
	parentNS, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/mnt", os.Getppid()))
	if err != nil {
		t.Fatal(err)
	}
	childNS, err := os.Readlink("/proc/self/ns/mnt")
	if err != nil || childNS == parentNS {
		t.Fatalf("mount namespace not private: %q %q %v", parentNS, childNS, err)
	}
	if err = unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); err != nil {
		t.Fatal(err)
	}
	fixture := os.Getenv("G2_SETTLEMENT_ROOT_DIR")
	if err = unix.Mount("tmpfs", fixture, "tmpfs", unix.MS_NOSUID|unix.MS_NODEV, "size=4m,mode=0755,uid=0,gid=0"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"initial", "runtime", "runtime/run"} {
		if err = os.Mkdir(fixture+"/"+name, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err = unix.Mount("tmpfs", fixture+"/runtime/run", "tmpfs", unix.MS_NOSUID|unix.MS_NODEV, "size=4m,mode=0755,uid=0,gid=0"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(fixture+"/runtime/run/g2", 0755); err != nil {
		t.Fatal(err)
	}
	initialFD, err := unix.Open(fixture+"/initial", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(initialFD)
	runtimeFD, err := unix.Open(fixture+"/runtime", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(runtimeFD)

	// The other client shares the mount namespace but has its own live PID.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	other := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOwnerProbeSettlementRuntimeRootRegistration$", "-test.timeout=18s")
	other.Env = append(os.Environ(), "G2_SETTLEMENT_ROOT_CHILD=other-client")
	other.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	other.ExtraFiles = []*os.File{os.NewFile(uintptr(runtimeFD), "runtime-root")}
	in, err := other.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := other.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	other.Stderr = &stderr
	if err = other.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close(); _ = other.Process.Kill(); _ = other.Wait() }()
	var signal [1]byte
	if _, err = io.ReadFull(out, signal[:]); err != nil || signal[0] != 'R' {
		t.Fatalf("other client start: %v %s", err, stderr.String())
	}

	// A real authority listener in this broker supplies the expected peer tuple.
	authority, err := unix.Socket(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(authority)
	address := &unix.SockaddrUnix{Name: fmt.Sprintf("\x00g2-test-root-%d", os.Getpid())}
	if err = unix.Bind(authority, address); err != nil {
		t.Fatal(err)
	}
	if err = unix.Listen(authority, 1); err != nil {
		t.Fatal(err)
	}
	authorityClient, err := unix.Socket(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(authorityClient)
	if err = unix.Connect(authorityClient, address); err != nil {
		t.Fatal(err)
	}
	accepted, _, err := unix.Accept4(authority, unix.SOCK_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(accepted)
	peer, err := unix.GetsockoptUcred(authorityClient, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || peer.Pid != int32(os.Getpid()) || peer.Uid != 0 || peer.Gid != 0 {
		t.Fatalf("authority peer: %+v %v", peer, err)
	}
	pidfd, err := unix.PidfdOpen(os.Getpid(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(pidfd)
	process, err := observeProcess(uint32(os.Getpid()), pidfd)
	if err != nil {
		t.Fatal(err)
	}
	p := &controlPrincipal{ID: 1, Role: "BOOT_OWNER", PID: uint32(os.Getpid()), UID: 0, GID: 0, PIDFD: pidfd, StartNS: process.StartTicks * 10_000_000, NextRequest: 1}
	b := &authorityBroker{bootID: "00000000-0000-0000-0000-000000000001", epoch: strings.Repeat("b", 32), principals: make(map[uint64]*controlPrincipal), launches: make(map[uint64]*launchRegistration)}
	b.diagnostic = &ownerProbeEvidenceState{broker: b, moduleInstance: 9}

	// Use the same all-thread root/cwd syscalls as the production handoff;
	// authentication of PID1/systemd and the native guard is outside this seam.
	changeRoot := func(fd int) {
		t.Helper()
		if _, _, e := syscall.AllThreadsSyscall(unix.SYS_FCHDIR, uintptr(fd), 0, 0); e != 0 {
			t.Fatal(e)
		}
		dot, _ := unix.BytePtrFromString(".")
		if _, _, e := syscall.AllThreadsSyscall(unix.SYS_CHROOT, uintptr(unsafe.Pointer(dot)), 0, 0); e != 0 {
			t.Fatal(e)
		}
		slash, _ := unix.BytePtrFromString("/")
		if _, _, e := syscall.AllThreadsSyscall(unix.SYS_CHDIR, uintptr(unsafe.Pointer(slash)), 0, 0); e != 0 {
			t.Fatal(e)
		}
		runtime.KeepAlive(dot)
		runtime.KeepAlive(slash)
	}
	changeRoot(initialFD)
	if _, err = os.Lstat("/run/g2"); !os.IsNotExist(err) {
		t.Fatalf("initial root unexpectedly has /run/g2: %v", err)
	}
	beforePoll := b.settlementEvidenceTransport()
	if beforePoll != nil {
		t.Fatal("listener opened before registration")
	}
	changeRoot(runtimeFD)
	var st unix.Stat_t
	var fs unix.Statfs_t
	if unix.Stat("/run/g2", &st) != nil || st.Uid != 0 || st.Gid != 0 || st.Mode&07777 != 0755 || unix.Statfs("/run/g2", &fs) != nil || fs.Type != unix.TMPFS_MAGIC {
		t.Fatal("runtime /run/g2 precondition differs")
	}
	if _, err = os.Lstat(settlementEvidenceDirectory); !os.IsNotExist(err) {
		t.Fatalf("listener exists before registration: %v", err)
	}
	if mode == "conflict" {
		if err = os.Mkdir(settlementEvidenceDirectory, 0700); err != nil {
			t.Fatal(err)
		}
		if err = unix.Lstat(settlementEvidenceDirectory, &st); err != nil {
			t.Fatal(err)
		}
	}
	raw, ack := []byte("registration"), []byte("registration-ack")
	returned, retained, err := b.commitLaunchRegistration(-1, "source", 1, p, 7, pidfd, []int{pidfd}, raw, ack)
	if err != nil || !retained || !bytes.Equal(returned, ack) || b.bootOwner != 1 || b.principals[1] != p || b.launches[7] == nil || !bytes.Equal(b.launches[7].ack, ack) {
		t.Fatalf("registration commit changed: %v", err)
	}
	tr := b.settlementEvidenceTransport()
	if mode == "conflict" {
		var after unix.Stat_t
		if tr != nil || unix.Lstat(settlementEvidenceDirectory, &after) != nil || after.Ino != st.Ino || after.Dev != st.Dev {
			t.Fatal("conflict changed registration or inode")
		}
		if err = os.Remove(settlementEvidenceDirectory); err != nil {
			t.Fatal(err)
		}
	} else {
		if tr == nil || beforePoll != nil || len(tr.poll(nil)) != 1 {
			t.Fatal("late listener missing from next poll snapshot")
		}
		var socket unix.Stat_t
		if unix.Lstat(settlementEvidenceDirectory+"/broker.sock", &socket) != nil || socket.Mode&unix.S_IFMT != unix.S_IFSOCK || socket.Mode&07777 != 0600 {
			t.Fatal("listener absent before ACK return")
		}
	}
	// These are the actual replay/rejection checks following native task pinning.
	if replay, found, e := b.lookupLaunchRegistration(7, "source", raw, []int{pidfd}); e != nil || !found || !bytes.Equal(replay, ack) {
		t.Fatalf("replay: %v", e)
	}
	if _, found, e := b.lookupLaunchRegistration(8, "source", raw, []int{pidfd}); e == nil || found {
		t.Fatal("second source accepted")
	}
	if b.settlementEvidenceTransport() != tr {
		t.Fatal("replay or rejected source reopened listener")
	}
	if mode == "conflict" {
		if _, err = os.Lstat(settlementEvidenceDirectory); !os.IsNotExist(err) {
			t.Fatalf("replay retried failed open: %v", err)
		}
		return
	}

	connect := func() int {
		t.Helper()
		fd, e := unix.Socket(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
		if e != nil {
			t.Fatal(e)
		}
		if e = unix.Connect(fd, &unix.SockaddrUnix{Name: settlementEvidenceDirectory + "/broker.sock"}); e != nil {
			t.Fatal(e)
		}
		actual, e := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
		if e != nil || *actual != *peer {
			t.Fatalf("diagnostic peer differs: %+v %v", actual, e)
		}
		return fd
	}
	sendHello := func(fd int, principal uint64) {
		t.Helper()
		hello, e := canonicalControl(map[string]any{"version": jsonU64(1), "purpose": "postgres-uid", "nonce": strings.Repeat("1", 32), "principal_id": jsonU64(principal)})
		if e != nil || sendControl(fd, hello, nil) != nil {
			t.Fatal("HELLO", e)
		}
	}
	bad := connect()
	sendHello(bad, 2)
	tr.service(b, b.diagnostic)
	if tr.connections[0].fd >= 0 || tr.connections[0].hello {
		t.Fatal("wrong principal accepted")
	}
	unix.Close(bad)
	if _, err = in.Write([]byte{'C'}); err != nil {
		t.Fatal(err)
	}
	if _, err = io.ReadFull(out, signal[:]); err != nil || signal[0] != 'S' {
		t.Fatalf("other client send: %v %s", err, stderr.String())
	}
	tr.service(b, b.diagnostic)
	if tr.connections[0].fd >= 0 || tr.connections[0].hello {
		t.Fatal("other live process accepted")
	}
	if _, err = in.Write([]byte{'X'}); err != nil {
		t.Fatal(err)
	}

	client := connect()
	defer unix.Close(client)
	sendHello(client, 1)
	tr.service(b, b.diagnostic)
	var reply [128]byte
	n, err := unix.Read(client, reply[:])
	if err != nil {
		t.Fatal(err)
	}
	expected, err := canonicalWithLimit(map[string]any{"version": jsonU64(1), "status": "ARMED", "nonce": strings.Repeat("1", 32)}, 128)
	if err != nil || !bytes.Equal(reply[:n], expected) {
		t.Fatal("wrong ARMED reply")
	}
	now := evidenceNow()
	deadline, hard := now+60_000_000_000, now+120_000_000_000
	cid := strings.Repeat("a", 64)
	bind, err := canonicalControl(map[string]any{"version": jsonU64(1), "purpose": "postgres-uid", "nonce": strings.Repeat("1", 32), "principal_id": jsonU64(1), "container_id": cid, "operation_start_ns": jsonU64(now), "operation_deadline_ns": jsonU64(deadline), "authority_deadline_ns": jsonU64(hard)})
	if err != nil || sendControl(client, bind, nil) != nil {
		t.Fatal("BIND", err)
	}
	tr.service(b, b.diagnostic)
	c := &tr.connections[0]
	if !c.hello || !c.bound || c.principal != p || c.purpose != 1 || hex.EncodeToString(c.nonce[:]) != strings.Repeat("1", 32) || hex.EncodeToString(c.cid[:]) != cid || c.start != now || c.deadline != deadline || c.authorityDeadline != hard {
		t.Fatal("BIND changed identity or deadlines")
	}
	g := &brokerGrant{id: 2, owner: 1, generation: 1, purpose: "postgres-uid", containerID: cid}
	b.diagnostic.issue(b, g)
	// Production exit marks and flushes the current, late-installed transport.
	b.finalizeSettlementEvidence()
	for _, phase := range []byte{1, 3} {
		var frame [settlementEvidenceBytes]byte
		n, e := unix.Read(client, frame[:])
		if e != nil || n != len(frame) || frame[10] != phase {
			t.Fatalf("exit snapshot phase %d: n=%d err=%v", phase, n, e)
		}
		if phase == 3 && binary.LittleEndian.Uint32(frame[248:]) != 5 {
			t.Fatal("wrong exit terminal")
		}
	}
	if mode == "substitution" {
		if err = os.Rename(settlementEvidenceDirectory+"/broker.sock", settlementEvidenceDirectory+"/original.sock"); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(settlementEvidenceDirectory+"/broker.sock", []byte("replacement"), 0600); err != nil {
			t.Fatal(err)
		}
		if err = unix.Lstat(settlementEvidenceDirectory+"/broker.sock", &st); err != nil {
			t.Fatal(err)
		}
	}
	listener := tr.listener
	b.closeSettlementEvidenceTransport()
	if _, err = unix.FcntlInt(uintptr(listener), unix.F_GETFD, 0); err != unix.EBADF {
		t.Fatalf("listener not closed: %v", err)
	}
	if mode == "substitution" {
		var after unix.Stat_t
		if unix.Lstat(settlementEvidenceDirectory+"/broker.sock", &after) != nil || after.Ino != st.Ino || after.Dev != st.Dev {
			t.Fatal("substituted inode was removed")
		}
	} else if _, err = os.Lstat(settlementEvidenceDirectory); !os.IsNotExist(err) {
		t.Fatalf("owned transport not removed: %v", err)
	}
}
