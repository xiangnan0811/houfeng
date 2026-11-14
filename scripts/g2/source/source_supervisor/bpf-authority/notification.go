//go:build linux && amd64 && !cgo

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// runtimeIdentity is installed only after BIND_RUNTIME has independently
// verified the daemon ancestry, invocation/grant and actual wrapper task. The
// exec generation must come from the guard's observed transition to runc.
type runtimeIdentity struct {
	InvocationID, GrantID, Generation uint64
	TGID                              uint32
	LeaderCookie                      uint64
	PIDFD                             int
	RuncSHA256                        string
	RuncExecObserved                  bool
}

type heldNotification struct {
	notice seccompNotification
	guard  guardDevice
	task   guardTask
	memory *notificationMemory
}

func hashNotifyingExecutable(tid uint32, expected string) error {
	if _, ok := digestString(expected); !ok {
		return errGuardProtocol
	}
	name := fmt.Sprintf("/proc/%d/exe", tid)
	fd, err := unix.Open(name, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		_ = unix.Close(fd)
		return errGuardProtocol
	}
	defer file.Close()
	var before, after, named unix.Stat_t
	if err = unix.Fstat(fd, &before); err != nil || before.Mode&unix.S_IFMT != unix.S_IFREG || before.Size <= 0 || before.Size > 128<<20 {
		return errGuardProtocol
	}
	h := sha256.New()
	size, err := io.Copy(h, io.LimitReader(file, (128<<20)+1))
	if err != nil || size != before.Size || unix.Fstat(fd, &after) != nil || !sameTrustedStat(before, after) {
		return errGuardProtocol
	}
	check, err := unix.Open(name, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	err = unix.Fstat(check, &named)
	_ = unix.Close(check)
	if err != nil || !sameTrustedStat(before, named) || hex.EncodeToString(h.Sum(nil)) != expected {
		return errGuardProtocol
	}
	return nil
}

// holdRuntimeNotification pins the actual notifying TID, its mm and executable
// before any caller memory or file table access. A wrapper's listener alone is
// never sufficient authority, nor are inherited filters or a matching TGID.
func holdRuntimeNotification(g guardDevice, listener int, n seccompNotification, invocation runtimeIdentity) (_ *heldNotification, err error) {
	if invocation.InvocationID == 0 || invocation.GrantID == 0 || invocation.Generation == 0 || invocation.TGID == 0 || invocation.LeaderCookie == 0 || !invocation.RuncExecObserved || !livePIDFD(invocation.PIDFD) {
		return nil, errGuardProtocol
	}
	if err = notificationValid(listener, n.ID); err != nil {
		return nil, err
	}
	task, err := g.pinTask(n.TID)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = g.dropTask(task.Cookie)
		}
	}()
	if task.TID != n.TID || task.TGID != invocation.TGID || !livePIDFD(invocation.PIDFD) {
		return nil, errGuardProtocol
	}
	if err = notificationValid(listener, n.ID); err != nil {
		return nil, err
	}
	memory, err := openNotificationMemory(listener, n)
	if err != nil {
		return nil, err
	}
	defer func() {
		if !keep {
			_ = memory.close()
		}
	}()
	if err = hashNotifyingExecutable(n.TID, invocation.RuncSHA256); err != nil {
		return nil, err
	}
	if err = notificationValid(listener, n.ID); err != nil || !livePIDFD(invocation.PIDFD) {
		return nil, errGuardProtocol
	}
	keep = true
	return &heldNotification{notice: n, guard: g, task: task, memory: memory}, nil
}

// importFD obtains the notifying thread's current exact file description from
// the guard; subsequent slot replacement cannot change the held object.
func (h *heldNotification) importFD(slot uint32) (int, error) {
	if h == nil || h.memory == nil {
		return -1, errGuardProtocol
	}
	if err := notificationValid(h.memory.listener, h.notice.ID); err != nil {
		return -1, err
	}
	fd, err := h.guard.importFD(h.task.Cookie, slot)
	if err != nil {
		return -1, err
	}
	if err = notificationValid(h.memory.listener, h.notice.ID); err != nil {
		_ = unix.Close(fd)
		return -1, err
	}
	return fd, nil
}

func (h *heldNotification) close() error {
	if h == nil || h.memory == nil {
		return errGuardProtocol
	}
	a := h.memory.close()
	h.memory = nil
	b := h.guard.dropTask(h.task.Cookie)
	if a != nil {
		return a
	}
	return b
}
