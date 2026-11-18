//go:build linux

package deploy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// HOUFENG_ORDINARY_GUARD_FIXTURE_ROOT must name a protected, disposable
// root-owned directory, outside production state. Run as root for cross-UID tests.
func TestOrdinaryGuardSharedReadersAndExclusiveManager(t *testing.T) {
	base := os.Getenv("HOUFENG_ORDINARY_GUARD_FIXTURE_ROOT")
	if base == "" || os.Geteuid() != 0 {
		t.Skip("requires root and HOUFENG_ORDINARY_GUARD_FIXTURE_ROOT")
	}
	parent, err := os.MkdirTemp(base, "ordinary-guard-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(parent) })
	if err := os.Chmod(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "ordinary")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(root, composeLaunchPublisherUID, composeLaunchRuntimeGID); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o750); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	exe = filepath.Join(parent, "guard-fixture-test")
	copyFile, err := os.OpenFile(exe, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		source.Close()
		t.Fatal(err)
	}
	_, copyErr := io.Copy(copyFile, source)
	closeErr := copyFile.Close()
	_ = source.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatalf("copy fixture executable: %v %v", copyErr, closeErr)
	}
	if err := os.Chmod(exe, 0o755); err != nil {
		t.Fatal(err)
	}
	missing := exec.Command(exe, "-test.run=^TestOrdinaryGuardFixtureChild$")
	missing.Env = append(os.Environ(), "HOUFENG_ORDINARY_GUARD_CHILD_ROOT="+root, "HOUFENG_ORDINARY_GUARD_CHILD_REJECT=1")
	missing.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 10001, Gid: 10001, Groups: []uint32{10001}}}
	if output, err := missing.CombinedOutput(); err != nil {
		t.Fatalf("missing namespace anchor: %v, %s", err, output)
	}
	if _, err := os.Lstat(filepath.Join(root, ordinaryGuardFile)); !os.IsNotExist(err) {
		t.Fatalf("namespace reader created guard: %v", err)
	}
	if err := ProvisionOrdinaryComposeGuard(root); err != nil {
		t.Fatal(err)
	}
	if err := WithOrdinaryComposeGuard(t.Context(), root, false, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(filepath.Join(root, ordinaryGuardFile))
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(exe, "-test.run=^TestOrdinaryGuardFixtureChild$")
	child.Env = append(os.Environ(), "HOUFENG_ORDINARY_GUARD_CHILD_ROOT="+root)
	child.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 10001, Gid: 10001, Groups: []uint32{10001}}}
	out, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	in, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _, _ = child.Process.Wait() })
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || line != "locked\n" {
		t.Fatalf("reader child did not acquire lock: %q %v", line, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if err := WithOrdinaryComposeGuard(ctx, root, true, func() error { t.Error("exclusive callback ran while reader held lock"); return nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("exclusive wait: %v", err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	manager := exec.Command(exe, "-test.run=^TestOrdinaryGuardFixtureChild$")
	manager.Env = append(os.Environ(), "HOUFENG_ORDINARY_GUARD_CHILD_ROOT="+root, "HOUFENG_ORDINARY_GUARD_CHILD_EXCLUSIVE=1")
	manager.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 10002, Gid: 10001, Groups: []uint32{10001}}}
	if output, err := manager.CombinedOutput(); err != nil {
		t.Fatalf("manager exclusive guard: %v, %s", err, output)
	}
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
	outsider := exec.Command(exe, "-test.run=^TestOrdinaryGuardFixtureChild$")
	outsider.Env = append(os.Environ(), "HOUFENG_ORDINARY_GUARD_CHILD_ROOT="+root, "HOUFENG_ORDINARY_GUARD_CHILD_REJECT=1")
	outsider.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 10003, Gid: 10001, Groups: []uint32{10001}}}
	if output, err := outsider.CombinedOutput(); err != nil {
		t.Fatalf("unauthorized group reader: %v, %s", err, output)
	}
	if err := WithOrdinaryComposeGuard(t.Context(), root, true, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(filepath.Join(root, ordinaryGuardFile))
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("guard anchor changed across calls")
	}
	setDisposition := func(stateName string) error {
		return WithOrdinaryComposeGuard(t.Context(), root, true, func() error {
			dirs, err := openComposeLaunchDirectories(root)
			if err != nil {
				return err
			}
			defer func() {
				for _, dir := range dirs {
					_ = unix.Close(dir.fd)
				}
			}()
			state, exists, err := ordinaryReadDisposition(dirs[len(dirs)-1].fd)
			if err != nil || !exists {
				return errOrdinaryUpgrade
			}
			state.State = stateName
			if stateName == "ACTIVE" {
				state.OperationID, state.IntentSHA256 = "test_operation", strings.Repeat("a", 64)
			} else {
				state.OperationID, state.IntentSHA256 = "", ""
			}
			return ordinaryWriteDisposition(dirs, state, false)
		})
	}
	if err := setDisposition("ACTIVE"); err != nil {
		t.Fatal(err)
	}
	if err := WithOrdinaryComposeGuard(t.Context(), root, false, func() error { t.Error("active disposition admitted reader"); return nil }); err == nil {
		t.Fatal("active disposition accepted shared operation")
	}
	if err := setDisposition("CLEAN"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, ordinaryGuardFile), 0o660); err != nil {
		t.Fatal(err)
	}
	called := false
	if err := WithOrdinaryComposeGuard(t.Context(), root, true, func() error { called = true; return nil }); err == nil || called {
		t.Fatal("unsafe guard mode accepted")
	}
	if err := os.Remove(filepath.Join(root, ordinaryGuardFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("elsewhere", filepath.Join(root, ordinaryGuardFile)); err != nil {
		t.Fatal(err)
	}
	if err := WithOrdinaryComposeGuard(t.Context(), root, false, func() error { t.Error("symlink callback ran"); return nil }); err == nil {
		t.Fatal("symlink guard accepted")
	}
	if err := os.Remove(filepath.Join(root, ordinaryGuardFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, ordinaryDispositionFile)); err != nil {
		t.Fatal(err)
	}
	if err := ProvisionOrdinaryComposeGuard(root); err == nil {
		t.Fatal("root marker allowed lost guard and disposition to be recreated")
	}
	if err := WithOrdinaryComposeGuard(t.Context(), root, false, func() error { t.Error("lost guard admitted reader"); return nil }); err == nil {
		t.Fatal("lost guard admitted shared operation")
	}
}

func TestOrdinaryGuardFixtureChild(t *testing.T) {
	root := os.Getenv("HOUFENG_ORDINARY_GUARD_CHILD_ROOT")
	if root == "" {
		return
	}
	if os.Getenv("HOUFENG_ORDINARY_GUARD_CHILD_REJECT") == "1" {
		if err := WithOrdinaryComposeGuard(t.Context(), root, false, func() error { return nil }); err == nil {
			t.Fatal("unrelated group member obtained shared guard")
		}
		return
	}
	if os.Getenv("HOUFENG_ORDINARY_GUARD_CHILD_SHARED") == "1" {
		if err := WithOrdinaryComposeGuard(t.Context(), root, false, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		return
	}
	if os.Getenv("HOUFENG_ORDINARY_GUARD_CHILD_EXCLUSIVE") == "1" {
		if err := WithOrdinaryComposeGuard(t.Context(), root, true, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := WithOrdinaryComposeGuard(t.Context(), root, true, func() error { return nil }); err == nil {
		t.Fatal("namespace UID obtained exclusive guard")
	}
	err := WithOrdinaryComposeGuard(t.Context(), root, false, func() error {
		fmt.Println("locked")
		_, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && !strings.Contains(err.Error(), "EOF") {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
