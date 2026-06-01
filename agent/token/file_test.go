package token_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"houfeng/agent/token"
)

func TestFileSourceReadsLegacyEnrollmentToken(t *testing.T) {
	path := t.TempDir() + "/token"
	if err := os.WriteFile(path, []byte("  enroll_001\n"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	source := token.FileSource{Path: path}
	got, err := source.Token(context.Background())
	if err != nil {
		t.Fatalf("Token() error = %v", err)
	}
	if got != "enroll_001" {
		t.Fatalf("Token() = %q, want %q", got, "enroll_001")
	}

	_, _, ok, err := source.SyncCredentials(context.Background())
	if err != nil {
		t.Fatalf("SyncCredentials() error = %v", err)
	}
	if ok {
		t.Fatal("SyncCredentials() ok = true, want false for legacy enrollment token")
	}
}

func TestFileSourceSavesAndReadsSyncCredentials(t *testing.T) {
	path := t.TempDir() + "/token"
	if err := os.WriteFile(path, []byte("enroll_001"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	source := token.FileSource{Path: path}
	if err := source.SaveSyncCredentials(context.Background(), "mi_001", "sync_001"); err != nil {
		t.Fatalf("SaveSyncCredentials() error = %v", err)
	}

	monitoringInstanceID, syncToken, ok, err := source.SyncCredentials(context.Background())
	if err != nil {
		t.Fatalf("SyncCredentials() error = %v", err)
	}
	if !ok {
		t.Fatal("SyncCredentials() ok = false, want true")
	}
	if monitoringInstanceID != "mi_001" {
		t.Fatalf("monitoringInstanceID = %q, want %q", monitoringInstanceID, "mi_001")
	}
	if syncToken != "sync_001" {
		t.Fatalf("syncToken = %q, want %q", syncToken, "sync_001")
	}

	if _, err := source.Token(context.Background()); err == nil {
		t.Fatal("Token() error = nil, want error after sync credentials replace enrollment token")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("token file mode = %o, want 600", got)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if strings.Contains(string(payload), "enroll_001") {
		t.Fatalf("token file still contains enrollment token: %s", payload)
	}
}

func TestFileSourceReadsLegacyNodeSyncCredentials(t *testing.T) {
	path := t.TempDir() + "/token"
	if err := os.WriteFile(path, []byte(`{"node_id":" node-legacy-001 ","sync_token":" sync_legacy "}`), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	source := token.FileSource{Path: path}
	monitoringInstanceID, syncToken, ok, err := source.SyncCredentials(context.Background())
	if err != nil {
		t.Fatalf("SyncCredentials() error = %v", err)
	}
	if !ok {
		t.Fatal("SyncCredentials() ok = false, want true")
	}
	if monitoringInstanceID != "node-legacy-001" {
		t.Fatalf("monitoringInstanceID = %q, want legacy node id", monitoringInstanceID)
	}
	if syncToken != "sync_legacy" {
		t.Fatalf("syncToken = %q, want sync_legacy", syncToken)
	}
}

func TestFileSourcePrefersMonitoringInstanceIDOverLegacyNodeID(t *testing.T) {
	path := t.TempDir() + "/token"
	if err := os.WriteFile(path, []byte(`{"monitoring_instance_id":"mi_current","node_id":"node_legacy","sync_token":"sync_001"}`), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	source := token.FileSource{Path: path}
	monitoringInstanceID, _, ok, err := source.SyncCredentials(context.Background())
	if err != nil {
		t.Fatalf("SyncCredentials() error = %v", err)
	}
	if !ok {
		t.Fatal("SyncCredentials() ok = false, want true")
	}
	if monitoringInstanceID != "mi_current" {
		t.Fatalf("monitoringInstanceID = %q, want current monitoring instance id", monitoringInstanceID)
	}
}

func TestFileSourceRejectsIncompleteSyncCredentials(t *testing.T) {
	path := t.TempDir() + "/token"
	if err := os.WriteFile(path, []byte(`{"monitoring_instance_id":"mi_001"}`), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	source := token.FileSource{Path: path}
	_, _, ok, err := source.SyncCredentials(context.Background())
	if err == nil {
		t.Fatal("SyncCredentials() error = nil, want error")
	}
	if ok {
		t.Fatal("SyncCredentials() ok = true, want false")
	}
}

func TestFileSourceRejectsBlankSaveInput(t *testing.T) {
	path := t.TempDir() + "/token"
	if err := os.WriteFile(path, []byte("enroll_001"), 0o600); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	source := token.FileSource{Path: path}
	err := source.SaveSyncCredentials(context.Background(), "", "sync_001")
	if err == nil {
		t.Fatal("SaveSyncCredentials() error = nil, want error")
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("SaveSyncCredentials() error = %v, want validation error", err)
	}
}
