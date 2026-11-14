//go:build linux

package deploy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestMaintenanceGuardProvisionAndReplay(t *testing.T) {
	root := filepath.Join(t.TempDir(), "guard")
	identity, err := ProvisionMaintenanceGuard(root)
	if err != nil {
		t.Fatal(err)
	}
	if identity.DirectoryInode == 0 || identity.FileInode == 0 || identity.Nonce == [32]byte{} {
		t.Fatalf("incomplete guard identity: %+v", identity)
	}
	again, err := ProvisionMaintenanceGuard(root)
	if err != nil || again != identity {
		t.Fatalf("provision replay = %+v, %v; want original identity", again, err)
	}
	data, err := os.ReadFile(filepath.Join(root, "maintenance.lock"))
	if err != nil || !bytes.Equal(data, identity.Nonce[:]) {
		t.Fatalf("persisted nonce = %x, %v", data, err)
	}
	for _, exclusive := range []bool{false, true} {
		called := 0
		want := errors.New("callback failure")
		err := WithMaintenanceGuard(t.Context(), root, identity, exclusive, func() error {
			called++
			return want
		})
		if !errors.Is(err, want) || called != 1 {
			t.Fatalf("callback = %d, %v", called, err)
		}
	}
	// The callback error above must have released the exclusive lock.
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := WithMaintenanceGuard(ctx, root, identity, true, func() error { return nil }); err != nil {
		t.Fatalf("reacquire after callback error: %v", err)
	}
	if again, err := ProvisionMaintenanceGuard(root); err != nil || again != identity {
		t.Fatalf("runtime changed permanent identity: %+v, %v", again, err)
	}
}

func TestMaintenanceGuardProvisionEmptyAndRejectMalformed(t *testing.T) {
	for _, size := range []int{0, 1, 31, 33, 64} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			root := t.TempDir()
			lock := filepath.Join(root, "maintenance.lock")
			before := bytes.Repeat([]byte{0x7a}, size)
			if err := os.WriteFile(lock, before, 0o600); err != nil {
				t.Fatal(err)
			}
			identity, err := ProvisionMaintenanceGuard(root)
			if size == 0 {
				if err != nil || identity.Nonce == [32]byte{} {
					t.Fatalf("initialize empty file = %+v, %v", identity, err)
				}
				return
			}
			if !errors.Is(err, ErrMaintenanceGuardMismatch) {
				t.Fatalf("malformed nonce error = %v", err)
			}
			after, readErr := os.ReadFile(lock)
			if readErr != nil || !bytes.Equal(before, after) {
				t.Fatalf("provision repaired malformed nonce: %x, %v", after, readErr)
			}
		})
	}
}

func TestMaintenanceGuardRejectsUnsafePathsWithoutCallback(t *testing.T) {
	cases := map[string]func(*testing.T, string){
		"missing-file": func(t *testing.T, root string) {
			mustMaintenanceGuard(t, os.Remove(filepath.Join(root, "maintenance.lock")))
		},
		"empty-file": func(t *testing.T, root string) {
			mustMaintenanceGuard(t, os.Truncate(filepath.Join(root, "maintenance.lock"), 0))
		},
		"short-nonce": func(t *testing.T, root string) {
			mustMaintenanceGuard(t, os.Truncate(filepath.Join(root, "maintenance.lock"), 31))
		},
		"long-nonce": func(t *testing.T, root string) {
			mustMaintenanceGuard(t, os.Truncate(filepath.Join(root, "maintenance.lock"), 33))
		},
		"corrupt-nonce": func(t *testing.T, root string) {
			lock := filepath.Join(root, "maintenance.lock")
			data, err := os.ReadFile(lock)
			mustMaintenanceGuard(t, err)
			data[0] ^= 0xff
			mustMaintenanceGuard(t, os.WriteFile(lock, data, 0o600))
		},
		"replaced-file-same-nonce": func(t *testing.T, root string) {
			lock := filepath.Join(root, "maintenance.lock")
			data, err := os.ReadFile(lock)
			mustMaintenanceGuard(t, err)
			// Keep the original inode alive to avoid depending on inode reuse.
			mustMaintenanceGuard(t, os.Rename(lock, lock+".original"))
			mustMaintenanceGuard(t, os.WriteFile(lock, data, 0o600))
		},
		"file-symlink": func(t *testing.T, root string) {
			lock := filepath.Join(root, "maintenance.lock")
			mustMaintenanceGuard(t, os.Rename(lock, lock+".original"))
			mustMaintenanceGuard(t, os.Symlink(lock+".original", lock))
		},
		"file-hardlink": func(t *testing.T, root string) {
			mustMaintenanceGuard(t, os.Link(filepath.Join(root, "maintenance.lock"), filepath.Join(root, "alias")))
		},
		"unsafe-file-mode": func(t *testing.T, root string) {
			mustMaintenanceGuard(t, os.Chmod(filepath.Join(root, "maintenance.lock"), 0o640))
		},
		"unsafe-directory-mode": func(t *testing.T, root string) {
			mustMaintenanceGuard(t, os.Chmod(root, 0o720))
		},
		"directory-symlink": func(t *testing.T, root string) {
			mustMaintenanceGuard(t, os.Rename(root, root+".original"))
			mustMaintenanceGuard(t, os.Symlink(root+".original", root))
		},
		"replaced-directory": func(t *testing.T, root string) {
			mustMaintenanceGuard(t, os.Rename(root, root+".original"))
			_, err := ProvisionMaintenanceGuard(root)
			mustMaintenanceGuard(t, err)
		},
		"fifo-lock": func(t *testing.T, root string) {
			lock := filepath.Join(root, "maintenance.lock")
			mustMaintenanceGuard(t, os.Remove(lock))
			mustMaintenanceGuard(t, unix.Mkfifo(lock, 0o600))
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "guard")
			identity, err := ProvisionMaintenanceGuard(root)
			mustMaintenanceGuard(t, err)
			mutate(t, root)
			for _, exclusive := range []bool{false, true} {
				called := false
				err := WithMaintenanceGuard(t.Context(), root, identity, exclusive, func() error {
					called = true
					return nil
				})
				if called || !errors.Is(err, ErrMaintenanceGuardMismatch) {
					t.Fatalf("unsafe guard callback = %t, error = %v", called, err)
				}
			}
		})
	}
}

func TestMaintenanceGuardRuntimeDoesNotProvision(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent")
	called := false
	err := WithMaintenanceGuard(t.Context(), root, MaintenanceGuardIdentity{}, true, func() error {
		called = true
		return nil
	})
	if called || !errors.Is(err, ErrMaintenanceGuardMismatch) {
		t.Fatalf("missing guard callback = %t, error = %v", called, err)
	}
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtime created missing directory: %v", err)
	}
	mustMaintenanceGuard(t, os.Mkdir(root, 0o700))
	err = WithMaintenanceGuard(t.Context(), root, MaintenanceGuardIdentity{}, false, func() error {
		called = true
		return nil
	})
	if called || !errors.Is(err, ErrMaintenanceGuardMismatch) {
		t.Fatalf("missing lock callback = %t, error = %v", called, err)
	}
	if _, err := os.Lstat(filepath.Join(root, "maintenance.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtime created missing lock: %v", err)
	}
}

func TestMaintenanceGuardProvisionRejectsUnsafeExistingState(t *testing.T) {
	for _, kind := range []string{"writable-directory", "symlink-directory", "symlink-file", "permissive-file"} {
		t.Run(kind, func(t *testing.T) {
			parent := t.TempDir()
			root := filepath.Join(parent, "guard")
			mustMaintenanceGuard(t, os.Mkdir(root, 0o700))
			switch kind {
			case "writable-directory":
				mustMaintenanceGuard(t, os.Chmod(root, 0o722))
			case "symlink-directory":
				mustMaintenanceGuard(t, os.Rename(root, root+".original"))
				mustMaintenanceGuard(t, os.Symlink(root+".original", root))
			case "symlink-file":
				sentinel := filepath.Join(parent, "sentinel")
				mustMaintenanceGuard(t, os.WriteFile(sentinel, []byte("untouched"), 0o600))
				mustMaintenanceGuard(t, os.Symlink(sentinel, filepath.Join(root, "maintenance.lock")))
			case "permissive-file":
				mustMaintenanceGuard(t, os.WriteFile(filepath.Join(root, "maintenance.lock"), nil, 0o644))
			}
			if _, err := ProvisionMaintenanceGuard(root); !errors.Is(err, ErrMaintenanceGuardMismatch) {
				t.Fatalf("unsafe provision error = %v", err)
			}
			if kind == "symlink-file" {
				data, err := os.ReadFile(filepath.Join(parent, "sentinel"))
				if err != nil || string(data) != "untouched" {
					t.Fatalf("provision modified symlink target: %q, %v", data, err)
				}
			}
		})
	}
}

func TestMaintenanceGuardConcurrentCallsKeepIndependentLocks(t *testing.T) {
	root := filepath.Join(t.TempDir(), "guard")
	identity, err := ProvisionMaintenanceGuard(root)
	mustMaintenanceGuard(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	entered := make(chan struct{}, 2)
	releaseFirst := make(chan struct{})
	releaseSecond := make(chan struct{})
	firstDone := make(chan error, 1)
	secondDone := make(chan error, 1)
	hold := func(release <-chan struct{}, done chan<- error) {
		done <- WithMaintenanceGuard(ctx, root, identity, false, func() error {
			entered <- struct{}{}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}
	go hold(releaseFirst, firstDone)
	go hold(releaseSecond, secondDone)
	for range 2 {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("concurrent shared callbacks did not enter")
		}
	}
	close(releaseFirst)
	mustMaintenanceGuard(t, <-firstDone)
	// If the two shared calls reused one open file description, releasing
	// the first call could unlock the second call's still-active callback.
	wait, stop := context.WithTimeout(ctx, 75*time.Millisecond)
	err = WithMaintenanceGuard(wait, root, identity, true, func() error {
		t.Error("exclusive callback entered while second shared call was active")
		return nil
	})
	stop()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("independent shared lock was released: %v", err)
	}
	close(releaseSecond)
	mustMaintenanceGuard(t, <-secondDone)
	mustMaintenanceGuard(t, WithMaintenanceGuard(ctx, root, identity, true, func() error { return nil }))
}

func TestMaintenanceGuardIndependentProcesses(t *testing.T) {
	root := filepath.Join(t.TempDir(), "guard")
	identity, err := ProvisionMaintenanceGuard(root)
	mustMaintenanceGuard(t, err)
	first := startMaintenanceGuardProcess(t, root, identity, false)
	first.expect(t, "waiting")
	first.expect(t, "entered")
	second := startMaintenanceGuardProcess(t, root, identity, false)
	second.expect(t, "waiting")
	second.expect(t, "entered")
	writer := startMaintenanceGuardProcess(t, root, identity, true)
	writer.expect(t, "waiting")
	writer.expectBlocked(t)
	first.release(t)
	writer.expectBlocked(t) // Releasing one shared descriptor must not release the other.
	second.release(t)
	writer.expect(t, "entered")
	reader := startMaintenanceGuardProcess(t, root, identity, false)
	reader.expect(t, "waiting")
	reader.expectBlocked(t)
	otherWriter := startMaintenanceGuardProcess(t, root, identity, true)
	otherWriter.expect(t, "waiting")
	otherWriter.expectBlocked(t)
	writer.release(t)
	// Release the reader as soon as it acquires so either kernel scheduling
	// order is valid; both independent contenders must eventually complete.
	mustMaintenanceGuard(t, reader.input.Close())
	mustMaintenanceGuard(t, otherWriter.input.Close())
	reader.expect(t, "entered")
	reader.expect(t, "released")
	otherWriter.expect(t, "entered")
	otherWriter.expect(t, "released")
	reader.expectExit(t)
	otherWriter.expectExit(t)
}

func TestMaintenanceGuardCancellationWhileProcessHoldsLock(t *testing.T) {
	root := filepath.Join(t.TempDir(), "guard")
	identity, err := ProvisionMaintenanceGuard(root)
	mustMaintenanceGuard(t, err)
	holder := startMaintenanceGuardProcess(t, root, identity, true)
	holder.expect(t, "waiting")
	holder.expect(t, "entered")
	for _, exclusive := range []bool{false, true} {
		ctx, cancel := context.WithTimeout(t.Context(), 75*time.Millisecond)
		called := false
		start := time.Now()
		err := WithMaintenanceGuard(ctx, root, identity, exclusive, func() error {
			called = true
			return nil
		})
		cancel()
		if called || !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
			t.Fatalf("cancelled wait callback=%t, duration=%s, error=%v", called, time.Since(start), err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := WithMaintenanceGuard(ctx, root, identity, true, func() error {
		t.Error("pre-cancelled callback executed")
		return nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled error = %v", err)
	}
	holder.release(t)
	ctx, cancel = context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	mustMaintenanceGuard(t, WithMaintenanceGuard(ctx, root, identity, true, func() error { return nil }))
}

func TestMaintenanceGuardRechecksAfterWaiting(t *testing.T) {
	for _, replaceDirectory := range []bool{false, true} {
		t.Run(strconv.FormatBool(replaceDirectory), func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "guard")
			identity, err := ProvisionMaintenanceGuard(root)
			mustMaintenanceGuard(t, err)
			holder := startMaintenanceGuardProcess(t, root, identity, true)
			holder.expect(t, "waiting")
			holder.expect(t, "entered")
			// The separate waiter has already opened the original descriptor if
			// it reaches flock first; either scheduling order must fail closed.
			result := make(chan error, 1)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			go func() {
				result <- WithMaintenanceGuard(ctx, root, identity, true, func() error {
					return errors.New("unexpected callback")
				})
			}()
			if replaceDirectory {
				mustMaintenanceGuard(t, os.Rename(root, root+".old"))
				_, err = ProvisionMaintenanceGuard(root)
				mustMaintenanceGuard(t, err)
			} else {
				lock := filepath.Join(root, "maintenance.lock")
				mustMaintenanceGuard(t, os.Rename(lock, lock+".old"))
				mustMaintenanceGuard(t, os.WriteFile(lock, identity.Nonce[:], 0o600))
			}
			holder.release(t)
			select {
			case err := <-result:
				if !errors.Is(err, ErrMaintenanceGuardMismatch) {
					t.Fatalf("replacement while waiting error = %v", err)
				}
			case <-ctx.Done():
				t.Fatal("replacement waiter did not finish")
			}
		})
	}
}

func TestMaintenanceGuardReleasesAfterPanic(t *testing.T) {
	root := filepath.Join(t.TempDir(), "guard")
	identity, err := ProvisionMaintenanceGuard(root)
	mustMaintenanceGuard(t, err)
	func() {
		defer func() {
			if recover() != "callback panic" {
				t.Error("callback panic was suppressed or changed")
			}
		}()
		_ = WithMaintenanceGuard(t.Context(), root, identity, true, func() error {
			panic("callback panic")
		})
	}()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	mustMaintenanceGuard(t, WithMaintenanceGuard(ctx, root, identity, true, func() error { return nil }))
}

// This helper is a genuinely separate process, not a goroutine sharing an open
// file description. The dedicated event FD keeps the Go test harness output out
// of the lock protocol, and stdin EOF explicitly releases the callback.
func TestMaintenanceGuardProcessHelper(t *testing.T) {
	if os.Getenv("HOUFENG_MAINTENANCE_GUARD_HELPER") != "1" {
		return
	}
	var identity MaintenanceGuardIdentity
	mustMaintenanceGuard(t, json.Unmarshal([]byte(os.Getenv("HOUFENG_MAINTENANCE_GUARD_IDENTITY")), &identity))
	exclusive, err := strconv.ParseBool(os.Getenv("HOUFENG_MAINTENANCE_GUARD_EXCLUSIVE"))
	mustMaintenanceGuard(t, err)
	events := os.NewFile(3, "guard-events")
	defer events.Close()
	_, err = fmt.Fprintln(events, "waiting")
	mustMaintenanceGuard(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	err = WithMaintenanceGuard(ctx, os.Getenv("HOUFENG_MAINTENANCE_GUARD_DIRECTORY"), identity, exclusive, func() error {
		if _, err := fmt.Fprintln(events, "entered"); err != nil {
			return err
		}
		_, err := io.Copy(io.Discard, os.Stdin)
		return err
	})
	mustMaintenanceGuard(t, err)
	_, err = fmt.Fprintln(events, "released")
	mustMaintenanceGuard(t, err)
}

type maintenanceGuardProcess struct {
	input  io.WriteCloser
	events <-chan string
	done   <-chan error
	output *bytes.Buffer
}

func startMaintenanceGuardProcess(t *testing.T, root string, identity MaintenanceGuardIdentity, exclusive bool) *maintenanceGuardProcess {
	t.Helper()
	encoded, err := json.Marshal(identity)
	mustMaintenanceGuard(t, err)
	readEvents, writeEvents, err := os.Pipe()
	mustMaintenanceGuard(t, err)
	t.Cleanup(func() { _ = readEvents.Close() })
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestMaintenanceGuardProcessHelper$")
	cmd.Env = append(os.Environ(),
		"HOUFENG_MAINTENANCE_GUARD_HELPER=1",
		"HOUFENG_MAINTENANCE_GUARD_DIRECTORY="+root,
		"HOUFENG_MAINTENANCE_GUARD_IDENTITY="+string(encoded),
		"HOUFENG_MAINTENANCE_GUARD_EXCLUSIVE="+strconv.FormatBool(exclusive),
	)
	cmd.ExtraFiles = []*os.File{writeEvents}
	output := &bytes.Buffer{}
	cmd.Stdout = output
	cmd.Stderr = output
	input, err := cmd.StdinPipe()
	if err != nil {
		_ = writeEvents.Close()
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		_ = input.Close()
		_ = writeEvents.Close()
		t.Fatal(err)
	}
	_ = writeEvents.Close()
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		close(done)
	}()
	events := make(chan string, 3)
	go func() {
		defer close(events)
		scanner := bufio.NewScanner(readEvents)
		for scanner.Scan() {
			events <- scanner.Text()
		}
	}()
	process := &maintenanceGuardProcess{input: input, events: events, done: done, output: output}
	t.Cleanup(func() {
		_ = input.Close()
		_ = cmd.Process.Kill()
		<-done
	})
	return process
}

func (process *maintenanceGuardProcess) expect(t *testing.T, expected string) {
	t.Helper()
	select {
	case got, ok := <-process.events:
		if !ok || got != expected {
			t.Fatalf("process event = %q (open=%t), want %q", got, ok, expected)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for process event %q", expected)
	}
}

func (process *maintenanceGuardProcess) expectBlocked(t *testing.T) {
	t.Helper()
	select {
	case got, ok := <-process.events:
		t.Fatalf("blocked process emitted %q (open=%t)", got, ok)
	case <-time.After(100 * time.Millisecond):
	}
}

func (process *maintenanceGuardProcess) release(t *testing.T) {
	t.Helper()
	mustMaintenanceGuard(t, process.input.Close())
	process.expect(t, "released")
	process.expectExit(t)
}

func (process *maintenanceGuardProcess) expectExit(t *testing.T) {
	t.Helper()
	select {
	case err := <-process.done:
		if err != nil {
			t.Fatalf("guard process failed: %v\n%s", err, process.output.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("guard process did not exit after release")
	}
}

func mustMaintenanceGuard(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
