package exec

import (
	"bytes"
	"context"
	osexec "os/exec"
	"time"
)

const (
	// DefaultTimeout is the maximum wall-clock time a command may run before
	// the agent kills the process.
	DefaultTimeout = 30 * time.Second

	// MaxOutputBytes caps stdout and stderr independently.
	MaxOutputBytes = 64 * 1024 // 64KB
)

// Result holds the outcome of a whitelisted command execution.
type Result struct {
	ActionID  string
	CommandID string
	Stdout    string
	Stderr    string
	ExitCode  int
}

// Run executes bin with args using exec.CommandContext (no shell) and
// returns a Result. The effective timeout is min(parent context deadline,
// DefaultTimeout). Stdout and stderr are each capped at MaxOutputBytes.
//
// ExitCode semantics:
//   - 0 on successful exit.
//   - The process exit code when the command exits non-zero.
//   - -1 for all other errors (timeout, command not found, etc.).
func Run(ctx context.Context, bin string, args []string) Result {
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	cmd := osexec.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*osexec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	out := stdout.String()
	if len(out) > MaxOutputBytes {
		out = out[:MaxOutputBytes] + "\n[output truncated at 64KB]"
	}

	errOut := stderr.String()
	if len(errOut) > MaxOutputBytes {
		errOut = errOut[:MaxOutputBytes] + "\n[stderr truncated at 64KB]"
	}

	return Result{Stdout: out, Stderr: errOut, ExitCode: exitCode}
}
