package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func readRecordsForTest(path string) ([]importing.InputRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return importing.DecodeRecords(file)
}
