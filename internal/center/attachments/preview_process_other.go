//go:build !linux

package attachments

import (
	"context"
	"io"
	"os"
	"os/exec"
	"time"
)

const previewCommandWaitDelay = 2 * time.Second

func runPreviewCommandSecure(
	context.Context,
	string,
	[]string,
	secureProcessorWorkspace,
	io.Writer,
	io.Writer,
) error {
	return ErrUnsafeProcessorWorkspace
}

func preparePreviewCommandDescriptor(
	workspace string,
	arguments []string,
) (string, string, []string, []*os.File, func(), error) {
	return workspace, workspace, arguments, nil, func() {}, nil
}

func configurePreviewCommand(command *exec.Cmd) {
	command.WaitDelay = previewCommandWaitDelay
}

func runPreviewCommandProcess(_ context.Context, command *exec.Cmd) error {
	return command.Run()
}
