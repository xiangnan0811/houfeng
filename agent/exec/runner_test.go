package exec

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunNormalCommandReturnsZeroExitCodeAndStdout(t *testing.T) {
	result := Run(context.Background(), "echo", []string{"hello"})
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "hello") {
		t.Fatalf("Stdout = %q, want substring %q", result.Stdout, "hello")
	}
	if result.Stderr != "" {
		t.Fatalf("Stderr = %q, want empty", result.Stderr)
	}
}

func TestRunCommandReturnsNonZeroExitCode(t *testing.T) {
	// "false" exits with code 1 on all Unix systems (macOS and Linux).
	result := Run(context.Background(), "false", nil)
	if result.ExitCode != 1 {
		t.Fatalf("ExitCode = %d, want 1", result.ExitCode)
	}
}

func TestRunCommandTimesOut(t *testing.T) {
	// Pass a parent context with a very short deadline so the command is
	// killed before it completes. The sleep command would block for 10s
	// but the 50ms deadline triggers exit code -1.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	result := Run(ctx, "sleep", []string{"10"})
	if result.ExitCode != -1 {
		t.Fatalf("ExitCode = %d, want -1 for timeout", result.ExitCode)
	}
}

func TestRunCommandNotFoundReturnsMinusOne(t *testing.T) {
	result := Run(context.Background(), "no_such_binary_xyz_12345", nil)
	if result.ExitCode != -1 {
		t.Fatalf("ExitCode = %d, want -1 for command not found", result.ExitCode)
	}
}

func TestRunStdoutTruncationAt64KB(t *testing.T) {
	// dd writes to stdout by default; 66000 bytes > 64KB triggers truncation.
	result := Run(context.Background(), "dd", []string{"if=/dev/zero", "bs=66000", "count=1"})
	if !strings.Contains(result.Stdout, "[output truncated at 64KB]") {
		t.Fatalf("Stdout missing truncation marker; len=%d tail=%q", len(result.Stdout), result.Stdout[len(result.Stdout)-100:])
	}
	// Final length includes the marker; verify the data portion was capped.
	markerLen := len("[output truncated at 64KB]") + 1 // +1 for preceding newline
	if len(result.Stdout) > MaxOutputBytes+markerLen {
		t.Fatalf("Stdout length = %d, want <= %d", len(result.Stdout), MaxOutputBytes+markerLen)
	}
}

func TestRunStderrTruncationAt64KB(t *testing.T) {
	// sh -c with >&2 redirects dd's stdout to stderr, producing >64KB there.
	result := Run(context.Background(), "sh", []string{"-c", "dd if=/dev/zero bs=66000 count=1 >&2"})
	if !strings.Contains(result.Stderr, "[stderr truncated at 64KB]") {
		t.Fatalf("Stderr missing truncation marker; len=%d tail=%q", len(result.Stderr), result.Stderr[len(result.Stderr)-100:])
	}
	// Final length includes the marker.
	markerLen := len("[stderr truncated at 64KB]") + 1
	if len(result.Stderr) > MaxOutputBytes+markerLen {
		t.Fatalf("Stderr length = %d, want <= %d", len(result.Stderr), MaxOutputBytes+markerLen)
	}
}
