//go:build linux && amd64 && !cgo

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// The startup witness requires exactly two descriptors. Rejected packets must
// not leave any received writer alive, including canonical decoding failures.
func TestReaderObserverWitnessRejectedRightsClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		count  int
		packet []byte
	}{
		{"one-right", 1, []byte(`{"version":1}`)},
		{"three-rights", 3, []byte(`{"version":1}`)},
		{"malformed-two-rights", 2, []byte(`{`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer unix.Close(pair[0])
			defer unix.Close(pair[1])
			if err := unix.SetsockoptInt(pair[1], unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
				t.Fatal(err)
			}
			readers := make([]int, 0, tc.count)
			writers := make([]int, 0, tc.count)
			defer func() {
				for _, fd := range readers {
					unix.Close(fd)
				}
				for _, fd := range writers {
					unix.Close(fd)
				}
			}()
			for range tc.count {
				var pipe [2]int
				if err := unix.Pipe2(pipe[:], unix.O_CLOEXEC|unix.O_NONBLOCK); err != nil {
					t.Fatal(err)
				}
				readers = append(readers, pipe[0])
				writers = append(writers, pipe[1])
			}
			if err := sendControl(pair[0], tc.packet, writers); err != nil {
				t.Fatal(err)
			}
			for _, fd := range writers {
				unix.Close(fd)
			}
			writers = nil
			frame, cred, rights, err := earlyProbeReceive(pair[1], 2, time.Now().Add(time.Second))
			if err == nil || frame != nil || cred != nil || rights != nil {
				t.Fatalf("rejected witness returned frame=%v cred=%v rights=%v err=%v", frame, cred, rights, err)
			}
			for _, fd := range readers {
				var b [1]byte
				if n, err := unix.Read(fd, b[:]); err != nil || n != 0 {
					t.Fatalf("received writer remained open: n=%d err=%v", n, err)
				}
			}
		})
	}
}

// /etc/passwd is also the existing trusted regular-file fixture in
// TestRuntimeRootHandoff. SCM_RIGHTS shares its open file description.
func TestReaderObserverWitnessHeldDigestPreservesSharedOffset(t *testing.T) {
	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		t.Fatal(err)
	}
	fd, err := unix.Open("/etc/passwd", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if _, err := unix.Seek(fd, 3, 0); err != nil {
		t.Fatal(err)
	}
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(pair[0])
	defer unix.Close(pair[1])
	if err := unix.SetsockoptInt(pair[1], unix.SOL_SOCKET, unix.SO_PASSCRED, 1); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	if err := earlyProbeSend(pair[0], map[string]any{"version": json.Number("1")}, []int{fd, fd}, deadline); err != nil {
		t.Fatal(err)
	}
	_, _, rights, err := earlyProbeReceive(pair[1], 2, deadline)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(rights[0])
	defer unix.Close(rights[1])
	digest, _, err := heldRegularDigest(rights[1], 128<<20)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(data)
	if digest != hex.EncodeToString(want[:]) {
		t.Fatalf("held digest = %s, want %x", digest, want)
	}
	for _, held := range []int{fd, rights[0], rights[1]} {
		if offset, err := unix.Seek(held, 0, 1); err != nil || offset != 3 {
			t.Fatalf("shared offset = %d, want 3: %v", offset, err)
		}
	}
}
