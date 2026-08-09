//go:build !linux

package attachments

import "errors"

var (
	errWorkspaceTestProcessNotFound         = errors.New("workspace test process not found")
	errWorkspaceTestProcessPermissionDenied = errors.New("workspace test process permission denied")
)

func workspaceTestKillProcess(int) error {
	return errors.New("linux-only workspace process helper")
}

func workspaceTestMakeFIFO(string, uint32) error {
	return errors.New("linux-only workspace FIFO helper")
}

func workspaceTestProbeProcess(int) error {
	return errors.New("linux-only workspace process helper")
}
