package config_test

import (
	"testing"

	agentconfig "houfeng/agent/config"
)

func TestLoadAgentConfigRequiresServerURLAndTokenFile(t *testing.T) {
	t.Setenv("HOUFENG_AGENT_SERVER_URL", "")
	t.Setenv("HOUFENG_AGENT_TOKEN_FILE", "")

	if _, err := agentconfig.LoadAgentConfig(); err == nil {
		t.Fatal("LoadAgentConfig() error = nil, want non-nil")
	}
}

func TestLoadAgentConfigDoesNotRequireNodeName(t *testing.T) {
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
