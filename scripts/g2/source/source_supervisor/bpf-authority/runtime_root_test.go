//go:build linux && amd64 && !cgo

package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func rootTestPinProc(t *testing.T) {
	t.Helper()
	if heldProcDirectory < 0 {
		if err := pinProcDirectory(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRuntimeRootHandoff(t *testing.T) {
	rootTestPinProc(t)
	t.Run("wrong-root", func(t *testing.T) {
		root, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(root)
		file, digest, err := openTrustedRegularAt(root, "/etc/passwd", 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		ordinary, originalDigest, err := openTrustedRegular("/etc/passwd", 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		defer ordinary.Close()
		if digest != originalDigest {
			t.Fatal("held-root digest differs")
		}
		if f, _, e := openTrustedRegularAt(heldProcDirectory, "/etc/passwd", 1<<20); e == nil {
			f.Close()
			t.Fatal("accepted a file outside the supplied root")
		}
		wrong, err := procDirectoryIdentity(heldProcDirectory)
		if err != nil {
			t.Fatal(err)
		}
		cwd, err := heldProcDirectoryIdentity("self/cwd")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = runtimeRootThreads(wrong, cwd); err == nil {
			t.Fatal("accepted a thread with a different root")
		}
	})
	t.Run("wrong-thread", func(t *testing.T) {
		ready := make(chan bool, 2)
		release := make(chan struct{})
		for range 2 {
			go func() {
				runtime.LockOSThread()
				defer runtime.UnlockOSThread()
				if unix.Gettid() == os.Getpid() {
					ready <- false
				} else {
					b := &authorityBroker{}
					ready <- b.initializeRuntimeRoot() != nil && b.runtimeRootState == runtimeRootUninitialized
				}
				<-release
			}()
		}
		first, second := <-ready, <-ready
		close(release)
		if !first && !second {
			t.Fatal("no nonleader thread was rejected")
		}
	})
	t.Run("wrong-namespace", func(t *testing.T) {
		var expected [3]unix.Stat_t
		for i, name := range []string{"mnt", "user", "pid"} {
			if unix.Fstatat(heldProcDirectory, "self/ns/"+name, &expected[i], 0) != nil {
				t.Fatal("cannot inspect actual namespace identities")
			}
		}
		if err := checkRuntimeRootNamespaces("self/", expected); err != nil {
			t.Fatalf("matching namespaces rejected: %v", err)
		}
		expected[0] = expected[1]
		if err := checkRuntimeRootNamespaces("self/", expected); err == nil {
			t.Fatal("accepted a different mount namespace")
		}
	})
}

func testTerminalObserverSeqpacket(t *testing.T) {
	rootTestPinProc(t)
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(pair[0])
	defer unix.Close(pair[1])
	if err = unix.SetsockoptInt(pair[1], unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
		t.Fatal(err)
	}
	frame := func(kind string) []byte {
		raw, e := canonicalControl(map[string]any{"version": jsonU64(1), "boot_id": "test", "kind": kind, "variant": "audit-loss"})
		if e != nil {
			t.Fatal(e)
		}
		return raw
	}
	for _, count := range []int{0, 1} {
		var rights []int
		var pipe [2]int
		if count == 1 {
			if err := unix.Pipe2(pipe[:], unix.O_CLOEXEC); err != nil {
				t.Fatal(err)
			}
			rights = []int{pipe[1]}
		}
		if err := sendControl(pair[0], frame("broker_observation_fault"), rights); err != nil {
			t.Fatal(err)
		}
		_, cred, received, err := receiveControl(pair[1], -1)
		if err != nil || cred == nil || int(cred.Pid) != os.Getpid() || cred.Uid != uint32(os.Getuid()) || len(received) != count {
			t.Fatalf("actual seqpacket with %d rights: cred=%v rights=%d err=%v", count, cred, len(received), err)
		}
		for _, fd := range received {
			unix.Close(fd)
		}
		if count == 1 {
			unix.Close(pipe[0])
			unix.Close(pipe[1])
		}
	}
	var pipe [2]int
	if err = unix.Pipe2(pipe[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
		t.Fatal(err)
	}
	defer unix.Close(pipe[0])
	if err = sendControl(pair[0], frame("broker_observation_fault"), []int{pipe[1]}); err != nil {
		t.Fatal(err)
	}
	unix.Close(pipe[1])
	observer := &terminalObserver{brokerFD: pair[1], brokerPID: uint32(os.Getpid()), bootID: "test"}
	if err = observer.handleBrokerMessage(); err == nil {
		t.Fatal("accepted observation with an unexpected descriptor")
	}
	var one [1]byte
	n, err := unix.Read(pipe[0], one[:])
	if err != nil || n != 0 {
		t.Fatalf("rejected received descriptor remained open: n=%d err=%v", n, err)
	}
	if os.Getuid() == 0 && os.Getgid() == 0 {
		if err = sendControl(pair[0], frame("broker_observation_fault"), nil); err != nil {
			t.Fatal(err)
		}
		if err = observer.handleBrokerMessage(); err == nil || observer.auditLost || observer.nativeLost {
			t.Fatalf("label-only observation was accepted or changed stream loss: %v", err)
		}
		pidfd, e := unix.PidfdOpen(os.Getpid(), 0)
		if e != nil {
			t.Fatal(e)
		}
		defer unix.Close(pidfd)
		digest := strings.Repeat("a", 64)
		binding, e := canonicalControl(map[string]any{"version": jsonU64(1), "boot_id": "test", "kind": "bind_exporter", "pid": jsonU64(uint64(os.Getpid())), "task_cookie": jsonU64(1), "start_ns": jsonU64(1), "exe_sha256": digest, "case_sha256": digest, "epoch": strings.Repeat("b", 32)})
		if e != nil {
			t.Fatal(e)
		}
		observer.transcript = newObserverTranscript("test", "")
		if e = sendControl(pair[0], binding, []int{pidfd}); e != nil {
			t.Fatal(e)
		}
		if e = observer.handleBrokerMessage(); e != nil || observer.exporterPeer == nil {
			t.Fatalf("one-right binding: %v", e)
		}
		defer observer.exporterPeer.close()
		realm, e := canonicalControl(map[string]any{"version": jsonU64(1), "boot_id": "test", "kind": "bind_realm_root", "pid": jsonU64(uint64(os.Getpid())), "task_cookie": jsonU64(1), "start_ns": jsonU64(1), "exe_sha256": digest, "script_sha256": digest, "case_sha256": digest, "epoch": strings.Repeat("b", 32)})
		if e != nil {
			t.Fatal(e)
		}
		if e = sendControl(pair[0], realm, []int{pidfd}); e != nil {
			t.Fatal(e)
		}
		if e = observer.handleBrokerMessage(); e != nil || observer.rootPeer == nil {
			t.Fatalf("one-right realm binding: %v", e)
		}
		defer observer.rootPeer.close()
	} else {
		// The real observer accepts only root broker credentials.
		if err = sendControl(pair[0], frame("broker_observation_fault"), nil); err != nil {
			t.Fatal(err)
		}
		if err = observer.handleBrokerMessage(); err == nil {
			t.Fatal(fmt.Errorf("accepted non-root broker credentials"))
		}
	}
}
