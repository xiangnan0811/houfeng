//go:build linux

package attachments

import (
	"errors"
	"syscall"
)

var (
	errWorkspaceTestProcessNotFound         = errors.New("workspace test process not found")
	errWorkspaceTestProcessPermissionDenied = errors.New("workspace test process permission denied")
)

func workspaceTestKillProcess(pid int) error {
	return syscall.Kill(pid, syscall.SIGKILL)
}

func workspaceTestMakeFIFO(path string, mode uint32) error {
	return syscall.Mkfifo(path, mode)
}

func workspaceTestProbeProcess(pid int) error {
	err := syscall.Kill(pid, 0)
	if errors.Is(err, syscall.ESRCH) {
		return errWorkspaceTestProcessNotFound
	}
	if errors.Is(err, syscall.EPERM) {
		return errWorkspaceTestProcessPermissionDenied
	}
	return err
}
