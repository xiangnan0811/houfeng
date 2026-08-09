//go:build linux

package attachments

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const previewCommandWaitDelay = 2 * time.Second

func runPreviewCommandSecure(
	ctx context.Context,
	binary string,
	arguments []string,
	workspace secureProcessorWorkspace,
	stdout io.Writer,
	stderr io.Writer,
) error {
	secure, ok := workspace.(*linuxSecureProcessorWorkspace)
	if !ok || secure == nil || secure.file == nil || secure.sourceFile == nil {
		return ErrInvalidPreviewContent
	}
	workspaceFD, err := unix.Dup(int(secure.file.Fd()))
	if err != nil {
		return ErrInvalidPreviewContent
	}
	sourceFD, err := unix.Dup(int(secure.sourceFile.Fd()))
	if err != nil {
		_ = unix.Close(workspaceFD)
		return ErrInvalidPreviewContent
	}
	if _, err := unix.Seek(sourceFD, 0, 0); err != nil {
		_ = unix.Close(workspaceFD)
		_ = unix.Close(sourceFD)
		return ErrInvalidPreviewContent
	}
	workspaceFile := os.NewFile(uintptr(workspaceFD), "preview-command-workspace")
	sourceFile := os.NewFile(uintptr(sourceFD), "preview-command-source")
	if workspaceFile == nil || sourceFile == nil {
		if workspaceFile != nil {
			_ = workspaceFile.Close()
		} else {
			_ = unix.Close(workspaceFD)
		}
		if sourceFile != nil {
			_ = sourceFile.Close()
		} else {
			_ = unix.Close(sourceFD)
		}
		return ErrInvalidPreviewContent
	}
	defer workspaceFile.Close()
	defer sourceFile.Close()
	commandArguments := make([]string, len(arguments))
	copy(commandArguments, arguments)
	for index, argument := range commandArguments {
		if filepath.Base(argument) == processorWorkspaceSourceName {
			commandArguments[index] = secureWorkspaceProcFDPath + "/4"
		}
	}
	command := exec.CommandContext(ctx, binary, commandArguments...)
	command.ExtraFiles = []*os.File{workspaceFile, sourceFile}
	command.Env = previewCommandEnvironment(secureWorkspaceProcFDPath + "/3")
	// The original held workspace FD remains inherited long enough for the child
	// to chdir; the ExtraFiles copies remain available to the command as fd 3/4.
	command.Dir = previewCommandWorkingDirectory(secure.path)
	configurePreviewCommand(command)
	command.Stdin = nil
	command.Stdout = stdout
	command.Stderr = stderr
	return runPreviewCommandProcess(ctx, command)
}

func preparePreviewCommandDescriptor(
	workspace string,
	arguments []string,
) (string, string, []string, []*os.File, func(), error) {
	cleaned := filepath.Clean(workspace)
	if filepath.Dir(cleaned) != secureWorkspaceProcFDPath {
		return workspace, workspace, arguments, nil, func() {}, nil
	}
	fd, err := strconv.Atoi(filepath.Base(cleaned))
	if err != nil || fd < 0 {
		return "", "", nil, nil, func() {}, ErrInvalidPreviewContent
	}
	duplicate, err := unix.Dup(fd)
	if err != nil {
		return "", "", nil, nil, func() {}, ErrInvalidPreviewContent
	}
	file := os.NewFile(uintptr(duplicate), "preview-workspace")
	if file == nil {
		_ = unix.Close(duplicate)
		return "", "", nil, nil, func() {}, ErrInvalidPreviewContent
	}
	childWorkspace := secureWorkspaceProcFDPath + "/3"
	rewritten := make([]string, len(arguments))
	for index, argument := range arguments {
		rewritten[index] = argument
		if argument == cleaned {
			rewritten[index] = childWorkspace
		} else if strings.HasPrefix(argument, cleaned+string(filepath.Separator)) {
			rewritten[index] = childWorkspace + strings.TrimPrefix(argument, cleaned)
		}
	}
	return childWorkspace, cleaned, rewritten, []*os.File{file}, func() { _ = file.Close() }, nil
}

func configurePreviewCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = previewCommandWaitDelay
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}

func runPreviewCommandProcess(ctx context.Context, command *exec.Cmd) error {
	if err := command.Start(); err != nil {
		return err
	}
	processGroupID := command.Process.Pid
	cancelDone := make(chan struct{})
	var watcherMu sync.Mutex
	commandDone := false
	go func() {
		select {
		case <-ctx.Done():
			watcherMu.Lock()
			if !commandDone {
				_ = syscall.Kill(-processGroupID, syscall.SIGKILL)
			}
			watcherMu.Unlock()
		case <-cancelDone:
		}
	}()
	err := command.Wait()
	watcherMu.Lock()
	commandDone = true
	close(cancelDone)
	watcherMu.Unlock()
	if contextError := ctx.Err(); contextError != nil {
		return contextError
	}
	return err
}
