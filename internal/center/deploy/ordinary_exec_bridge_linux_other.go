//go:build linux && !amd64 && !arm64

package deploy

import (
	"context"
	"os/exec"
)

// Unsupported architectures never launch the target through an unisolated path.
func PrepareOrdinaryTargetProcess() error                 { return errOrdinaryUpgrade }
func RunOrdinaryTargetLauncher(string, string) error      { return errOrdinaryUpgrade }
func ordinaryStartLocked(*exec.Cmd) (<-chan error, error) { return nil, errOrdinaryUpgrade }
func ordinaryProbeTargetContract(context.Context, string, [32]byte) ([]byte, error) {
	return nil, errOrdinaryUpgrade
}
