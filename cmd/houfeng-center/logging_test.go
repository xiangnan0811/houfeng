package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"houfeng/internal/center/config"
)

func TestSafeLogVersion(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "release", value: "v1.2.3", want: "v1.2.3"},
		{name: "dev", value: "dev", want: "dev"},
		{name: "empty", value: "", want: "unknown"},
		{name: "spaces", value: "  ", want: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := safeLogVersion(tt.value); got != tt.want {
				t.Fatalf("safeLogVersion(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}

func TestSetupLoggingWritesToConfiguredFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "center.log")

	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })

	cleanup, err := setupLogging(config.CenterConfig{LogFile: path})
	if err != nil {
		t.Fatalf("setupLogging() error = %v", err)
	}

	slog.Info("file logging smoke", "component", "center")
	cleanup()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	logs := string(content)
	if !strings.Contains(logs, "file logging smoke") || !strings.Contains(logs, "component=center") {
		t.Fatalf("log file content = %q, want emitted slog record", logs)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat log file: %v", err)
	}
	if gotMode := info.Mode().Perm(); gotMode != 0o644 {
		t.Fatalf("log file mode = %o, want 0644", gotMode)
	}
}

func TestSetupLoggingFailsForMissingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "center.log")

	if _, err := setupLogging(config.CenterConfig{LogFile: path}); err == nil {
		t.Fatal("setupLogging() error = nil, want non-nil")
	}
}

func TestSetupLoggingAllowsUnsetLogFile(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })

	cleanup, err := setupLogging(config.CenterConfig{})
	if err != nil {
		t.Fatalf("setupLogging() error = %v", err)
	}
	slog.Info("stdout logging smoke", "component", "center")
	cleanup()
}

func TestLockedWriterSerializesWrites(t *testing.T) {
	var sink strings.Builder
	writer := &lockedWriter{w: &sink}

	const goroutines = 16
	const writesPerGoroutine = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < writesPerGoroutine; j++ {
				if _, err := writer.Write([]byte("record\n")); err != nil {
					t.Errorf("Write() error = %v", err)
				}
			}
		}()
	}
	wg.Wait()

	lines := strings.Split(strings.TrimSuffix(sink.String(), "\n"), "\n")
	if len(lines) != goroutines*writesPerGoroutine {
		t.Fatalf("logged lines = %d, want %d", len(lines), goroutines*writesPerGoroutine)
	}
	for _, line := range lines {
		if line != "record" {
			t.Fatalf("log line = %q, want complete serialized record", line)
		}
	}
}
