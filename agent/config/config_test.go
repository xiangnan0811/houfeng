package config_test

import (
	"testing"

	agentconfig "houfeng/agent/config"
)

func TestLoadAgentConfigRequiresServerURLAndTokenFile(t *testing.T) {
	t.Setenv("HOUFENG_AGENT_SERVER_URL", "")
	t.Setenv("HOUFENG_AGENT_TOKEN_FILE", "")
	t.Setenv("HOUFENG_AGENT_NODE_NAME", "nd-local-01")

	if _, err := agentconfig.LoadAgentConfig(); err == nil {
		t.Fatal("LoadAgentConfig() error = nil, want non-nil")
	}
}
