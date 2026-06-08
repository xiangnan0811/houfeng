package config_test

import (
	"testing"
	"time"

	agentconfig "houfeng/agent/config"
)

func TestLoadAgentConfigRequiresServerURLAndTokenFile(t *testing.T) {
	t.Setenv("HOUFENG_AGENT_SERVER_URL", "")
	t.Setenv("HOUFENG_AGENT_TOKEN_FILE", "")

	if _, err := agentconfig.LoadAgentConfig(); err == nil {
		t.Fatal("LoadAgentConfig() error = nil, want non-nil")
	}
}

func TestLoadAgentConfigDoesNotRequireMonitoringInstanceName(t *testing.T) {
	t.Setenv("HOUFENG_AGENT_SERVER_URL", "http://center")
	t.Setenv("HOUFENG_AGENT_TOKEN_FILE", "/tmp/token")
	t.Setenv("HOUFENG_AGENT_NODE_NAME", "")

	cfg, err := agentconfig.LoadAgentConfig()
	if err != nil {
		t.Fatalf("LoadAgentConfig() error = %v, want nil", err)
	}

	if cfg.ServerURL != "http://center" {
		t.Fatalf("ServerURL = %q, want %q", cfg.ServerURL, "http://center")
	}
	if cfg.TokenFile != "/tmp/token" {
		t.Fatalf("TokenFile = %q, want %q", cfg.TokenFile, "/tmp/token")
	}
}

func TestLoadAgentConfigProvidesDurableBufferDefaults(t *testing.T) {
	t.Setenv("HOUFENG_AGENT_SERVER_URL", "http://center")
	t.Setenv("HOUFENG_AGENT_TOKEN_FILE", "/tmp/token")
	t.Setenv("HOUFENG_AGENT_BUFFER_FILE", "")
	t.Setenv("HOUFENG_AGENT_BUFFER_MAX_ENTRIES", "")
	t.Setenv("HOUFENG_AGENT_BUFFER_MAX_AGE", "")
	t.Setenv("HOUFENG_AGENT_IP_QUALITY_STATE_FILE", "")

	cfg, err := agentconfig.LoadAgentConfig()
	if err != nil {
		t.Fatalf("LoadAgentConfig() error = %v", err)
	}
	if cfg.BufferFile != "/var/lib/houfeng-agent/sync-buffer.json" {
		t.Fatalf("BufferFile = %q, want default", cfg.BufferFile)
	}
	if cfg.BufferMaxEntries != 65536 {
		t.Fatalf("BufferMaxEntries = %d, want 65536", cfg.BufferMaxEntries)
	}
	if cfg.BufferMaxAge != 72*time.Hour {
		t.Fatalf("BufferMaxAge = %s, want 72h", cfg.BufferMaxAge)
	}
	if cfg.IPQualityStateFile != "/var/lib/houfeng-agent/ip-quality-state.json" {
		t.Fatalf("IPQualityStateFile = %q, want default", cfg.IPQualityStateFile)
	}
}

func TestLoadAgentConfigAcceptsDurableBufferOverrides(t *testing.T) {
	t.Setenv("HOUFENG_AGENT_SERVER_URL", "http://center")
	t.Setenv("HOUFENG_AGENT_TOKEN_FILE", "/tmp/token")
	t.Setenv("HOUFENG_AGENT_BUFFER_FILE", "/tmp/houfeng-buffer.json")
	t.Setenv("HOUFENG_AGENT_BUFFER_MAX_ENTRIES", "17")
	t.Setenv("HOUFENG_AGENT_BUFFER_MAX_AGE", "2h")
	t.Setenv("HOUFENG_AGENT_IP_QUALITY_STATE_FILE", "/tmp/ip-quality-state.json")

	cfg, err := agentconfig.LoadAgentConfig()
	if err != nil {
		t.Fatalf("LoadAgentConfig() error = %v", err)
	}
	if cfg.BufferFile != "/tmp/houfeng-buffer.json" ||
		cfg.BufferMaxEntries != 17 ||
		cfg.BufferMaxAge != 2*time.Hour ||
		cfg.IPQualityStateFile != "/tmp/ip-quality-state.json" {
		t.Fatalf("config = %#v, want buffer overrides", cfg)
	}
}

func TestLoadAgentConfigRejectsInvalidDurableBufferMaxEntries(t *testing.T) {
	t.Setenv("HOUFENG_AGENT_SERVER_URL", "http://center")
	t.Setenv("HOUFENG_AGENT_TOKEN_FILE", "/tmp/token")
	t.Setenv("HOUFENG_AGENT_BUFFER_MAX_ENTRIES", "nope")

	_, err := agentconfig.LoadAgentConfig()
	if err == nil {
		t.Fatal("LoadAgentConfig() error = nil, want non-nil")
	}
	if err.Error() != "HOUFENG_AGENT_BUFFER_MAX_ENTRIES must be a positive integer: strconv.Atoi: parsing \"nope\": invalid syntax" {
		t.Fatalf("LoadAgentConfig() error = %q", err)
	}
}

func TestLoadAgentConfigRejectsInvalidDurableBufferMaxAge(t *testing.T) {
	t.Setenv("HOUFENG_AGENT_SERVER_URL", "http://center")
	t.Setenv("HOUFENG_AGENT_TOKEN_FILE", "/tmp/token")
	t.Setenv("HOUFENG_AGENT_BUFFER_MAX_AGE", "soon")

	_, err := agentconfig.LoadAgentConfig()
	if err == nil {
		t.Fatal("LoadAgentConfig() error = nil, want non-nil")
	}
	if err.Error() != "HOUFENG_AGENT_BUFFER_MAX_AGE must be a valid duration: time: invalid duration \"soon\"" {
		t.Fatalf("LoadAgentConfig() error = %q", err)
	}
}
