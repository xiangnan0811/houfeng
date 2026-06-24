package agentapi

import "testing"

func TestKnownCommandIDsIncludeAgentWhitelistCommands(t *testing.T) {
	t.Parallel()

	for _, commandID := range []string{
		"df_h",
		"free_m",
		"uptime",
		"top_head",
		"journalctl_u",
		"systemctl_status",
		"dmesg_err",
		"docker_ps",
	} {
		if !IsKnownCommandID(commandID) {
			t.Fatalf("IsKnownCommandID(%q) = false, want true", commandID)
		}
	}
}

func TestKnownCommandIDsRejectUnknownValues(t *testing.T) {
	t.Parallel()

	for _, commandID := range []string{"", "systemd_status", "sh", "sh -c uptime"} {
		if IsKnownCommandID(commandID) {
			t.Fatalf("IsKnownCommandID(%q) = true, want false", commandID)
		}
	}
}
