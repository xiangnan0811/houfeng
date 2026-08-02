package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"houfeng/internal/center/importing"
)

func TestValidateOptions(t *testing.T) {
	tests := []struct {
		name    string
		opts    cliOptions
		wantErr bool
	}{
		{name: "default dry run", opts: cliOptions{filePath: "vps.json", format: "text"}},
		{name: "json import", opts: cliOptions{filePath: "vps.json", doImport: true, format: "json"}},
		{name: "missing file", opts: cliOptions{format: "text"}, wantErr: true},
		{name: "mutually exclusive modes", opts: cliOptions{filePath: "vps.json", dryRun: true, doImport: true, format: "text"}, wantErr: true},
		{name: "invalid format", opts: cliOptions{filePath: "vps.json", format: "yaml"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOptions(tt.opts)
			if tt.wantErr && err == nil {
				t.Fatal("validateOptions() error = nil, want non-nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validateOptions() error = %v, want nil", err)
			}
		})
	}
}

func TestRunDryRunContinuesWhenDatabaseCheckFails(t *testing.T) {
	t.Setenv("HOUFENG_DATABASE_URL", "not a postgres url")
	inputPath := filepath.Join(t.TempDir(), "vps.json")
	body := `[
		{
			"display_name":"tokyo",
			"provider_name":"example",
			"lifecycle_status":"active",
			"usage_status":"in_use"
		}
	]`
	if err := os.WriteFile(inputPath, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	records, err := readRecordsForTest(inputPath)
	if err != nil {
		t.Fatalf("readRecordsForTest: %v", err)
	}
	var output bytes.Buffer
	if err := runDryRun(t.Context(), records, "json", &output); err != nil {
		t.Fatalf("runDryRun() error = %v, want nil with database warning fallback", err)
	}
	if !strings.Contains(output.String(), "database check skipped") {
		t.Fatalf("runDryRun output = %s, want database warning", output.String())
	}
}

func TestRunImportRequiresDatabaseURL(t *testing.T) {
	t.Setenv("HOUFENG_DATABASE_URL", "")
	err := runImport(t.Context(), nil, "json", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "HOUFENG_DATABASE_URL") {
		t.Fatalf("runImport() error = %v, want missing database URL", err)
	}
}

func TestRunWithDepsRejectsUnsupportedRecordPlatformModeBeforeOpeningInputFile(t *testing.T) {
	tests := []struct {
		name                   string
		recordsEnabled         string
		permanentDeleteEnabled string
		wantErr                string
	}{
		{
			name:                   "delete without records",
			recordsEnabled:         "false",
			permanentDeleteEnabled: "true",
			wantErr:                "HOUFENG_RECORD_PERMANENT_DELETE_ENABLED requires HOUFENG_RECORDS_ENABLED=true",
		},
		{
			name:                   "delete with records",
			recordsEnabled:         "true",
			permanentDeleteEnabled: "true",
			wantErr:                "HOUFENG_RECORD_PERMANENT_DELETE_ENABLED=true is not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOUFENG_RECORDS_ENABLED", tt.recordsEnabled)
			t.Setenv("HOUFENG_RECORD_PERMANENT_DELETE_ENABLED", tt.permanentDeleteEnabled)
			opened := false

			err := runWithDeps(context.Background(), cliOptions{filePath: "unreadable-vps.json", format: "json"}, importerDeps{
				openFile: func(string) (io.ReadCloser, error) {
					opened = true
					return nil, errors.New("input file must remain unread")
				},
			})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("runWithDeps() error = %v, want record-platform mode rejection", err)
			}
			if opened {
				t.Fatal("runWithDeps() opened input file before rejecting unsupported record-platform mode")
			}
		})
	}
}

func TestRunWithDepsRecordsOnImportRejectsRuntimeAdmissionFailureWithoutMigration(t *testing.T) {
	t.Setenv("HOUFENG_RECORDS_ENABLED", "true")
	t.Setenv("HOUFENG_RECORD_PERMANENT_DELETE_ENABLED", "false")
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://runtime-only")
	wantErr := errors.New("runtime admission boom")
	applyCalls := 0
	admitCalls := 0
	closeCalls := 0

	err := runWithDeps(context.Background(), cliOptions{filePath: "vps.json", doImport: true, format: "json"}, importerDeps{
		openFile: func(string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("[]")), nil
		},
		openPostgres: func(context.Context, string) (*pgxpool.Pool, error) {
			return nil, nil
		},
		closePostgres: func(*pgxpool.Pool) {
			closeCalls++
		},
		applyMigrations: func(context.Context, *pgxpool.Pool) error {
			applyCalls++
			return nil
		},
		admitRuntime: func(context.Context, *pgxpool.Pool) error {
			admitCalls++
			return wantErr
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("runWithDeps() error = %v, want wrapped runtime admission error", err)
	}
	if applyCalls != 0 {
		t.Fatalf("applyMigrations calls = %d, want 0", applyCalls)
	}
	if admitCalls != 1 {
		t.Fatalf("admitRuntime calls = %d, want 1", admitCalls)
	}
	if closeCalls != 1 {
		t.Fatalf("closePostgres calls = %d, want 1 after runtime admission failure", closeCalls)
	}
}

func TestRunWithDepsRecordsOnImportUsesDefaultRuntimeAdmission(t *testing.T) {
	t.Setenv("HOUFENG_RECORDS_ENABLED", "true")
	t.Setenv("HOUFENG_RECORD_PERMANENT_DELETE_ENABLED", "false")
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://runtime-only")
	applyCalls := 0
	closeCalls := 0

	err := runWithDeps(context.Background(), cliOptions{filePath: "vps.json", doImport: true, format: "json"}, importerDeps{
		openFile: func(string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("[]")), nil
		},
		openPostgres: func(context.Context, string) (*pgxpool.Pool, error) {
			return nil, nil
		},
		closePostgres: func(*pgxpool.Pool) {
			closeCalls++
		},
		applyMigrations: func(context.Context, *pgxpool.Pool) error {
			applyCalls++
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "current app ACL runtime admission has no PostgreSQL pool") {
		t.Fatalf("runWithDeps() error = %v, want default current app ACL runtime admission error", err)
	}
	if applyCalls != 0 {
		t.Fatalf("applyMigrations calls = %d, want 0", applyCalls)
	}
	if closeCalls != 1 {
		t.Fatalf("closePostgres calls = %d, want 1 after default runtime admission failure", closeCalls)
	}
}

func TestRunWithDepsRecordsOnImportReturnsRuntimeOpenFailureWithoutMigration(t *testing.T) {
	t.Setenv("HOUFENG_RECORDS_ENABLED", "true")
	t.Setenv("HOUFENG_RECORD_PERMANENT_DELETE_ENABLED", "false")
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://runtime-only")
	wantErr := errors.New("runtime open boom")
	applyCalls := 0
	admitCalls := 0

	err := runWithDeps(context.Background(), cliOptions{filePath: "vps.json", doImport: true, format: "json"}, importerDeps{
		openFile: func(string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("[]")), nil
		},
		openPostgres: func(context.Context, string) (*pgxpool.Pool, error) {
			return nil, wantErr
		},
		applyMigrations: func(context.Context, *pgxpool.Pool) error {
			applyCalls++
			return nil
		},
		admitRuntime: func(context.Context, *pgxpool.Pool) error {
			admitCalls++
			return nil
		},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("runWithDeps() error = %v, want wrapped runtime-open error", err)
	}
	if applyCalls != 0 || admitCalls != 0 {
		t.Fatalf("writer/admission calls = %d/%d, want 0/0 after runtime open failure", applyCalls, admitCalls)
	}
}

func TestRunWithDepsRecordsOnDryRunReturnsRuntimeOpenFailureWithoutWarningFallback(t *testing.T) {
	t.Setenv("HOUFENG_RECORDS_ENABLED", "true")
	t.Setenv("HOUFENG_RECORD_PERMANENT_DELETE_ENABLED", "false")
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://runtime-only")
	wantErr := errors.New("runtime open boom")
	applyCalls := 0
	admitCalls := 0
	var output bytes.Buffer

	err := runWithDeps(context.Background(), cliOptions{filePath: "vps.json", dryRun: true, format: "json"}, importerDeps{
		openFile: func(string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("[]")), nil
		},
		openPostgres: func(context.Context, string) (*pgxpool.Pool, error) {
			return nil, wantErr
		},
		applyMigrations: func(context.Context, *pgxpool.Pool) error {
			applyCalls++
			return nil
		},
		admitRuntime: func(context.Context, *pgxpool.Pool) error {
			admitCalls++
			return nil
		},
		output: &output,
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("runWithDeps() error = %v, want wrapped runtime-open error", err)
	}
	if strings.Contains(output.String(), "database check skipped") {
		t.Fatalf("runWithDeps() output = %q, must not use legacy warning fallback", output.String())
	}
	if applyCalls != 0 {
		t.Fatalf("applyMigrations calls = %d, want 0", applyCalls)
	}
	if admitCalls != 0 {
		t.Fatalf("admitRuntime calls = %d, want 0 after runtime open failure", admitCalls)
	}
}

func TestRunWithDepsRecordsOnDryRunReturnsRuntimeAdmissionFailureWithoutWarningFallback(t *testing.T) {
	t.Setenv("HOUFENG_RECORDS_ENABLED", "true")
	t.Setenv("HOUFENG_RECORD_PERMANENT_DELETE_ENABLED", "false")
	t.Setenv("HOUFENG_DATABASE_URL", "postgres://runtime-only")
	wantErr := errors.New("runtime admission boom")
	applyCalls := 0
	admitCalls := 0
	closeCalls := 0
	var output bytes.Buffer

	err := runWithDeps(context.Background(), cliOptions{filePath: "vps.json", dryRun: true, format: "json"}, importerDeps{
		openFile: func(string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("[]")), nil
		},
		openPostgres: func(context.Context, string) (*pgxpool.Pool, error) {
			return nil, nil
		},
		closePostgres: func(*pgxpool.Pool) {
			closeCalls++
		},
		applyMigrations: func(context.Context, *pgxpool.Pool) error {
			applyCalls++
			return nil
		},
		admitRuntime: func(context.Context, *pgxpool.Pool) error {
			admitCalls++
			return wantErr
		},
		output: &output,
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("runWithDeps() error = %v, want wrapped runtime-admission error", err)
	}
	if strings.Contains(output.String(), "database check skipped") {
		t.Fatalf("runWithDeps() output = %q, must not use legacy warning fallback", output.String())
	}
	if applyCalls != 0 || admitCalls != 1 || closeCalls != 1 {
		t.Fatalf("writer/admission/close calls = %d/%d/%d, want 0/1/1", applyCalls, admitCalls, closeCalls)
	}
}

func readRecordsForTest(path string) ([]importing.InputRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return importing.DecodeRecords(file)
}
