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

func TestCommandSensitivityTiers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		commandID string
		want      CommandSensitivity
	}{
		{commandID: "df_h", want: CommandSensitivityStandard},
		{commandID: "free_m", want: CommandSensitivityStandard},
		{commandID: "uptime", want: CommandSensitivityStandard},
		{commandID: "top_head", want: CommandSensitivitySensitive},
		{commandID: "journalctl_u", want: CommandSensitivitySensitive},
		{commandID: "systemctl_status", want: CommandSensitivitySensitive},
		{commandID: "dmesg_err", want: CommandSensitivitySensitive},
		{commandID: "docker_ps", want: CommandSensitivitySensitive},
	}

	if len(KnownCommandDefinitions()) != len(tests) {
		t.Fatalf("len(KnownCommandDefinitions()) = %d, want %d", len(KnownCommandDefinitions()), len(tests))
	}

	for _, tt := range tests {
		t.Run(tt.commandID, func(t *testing.T) {
			got, ok := SensitivityForCommand(tt.commandID)
			if !ok {
				t.Fatalf("SensitivityForCommand(%q) ok = false, want true", tt.commandID)
			}
			if got != tt.want {
				t.Fatalf("SensitivityForCommand(%q) = %q, want %q", tt.commandID, got, tt.want)
			}
			if RequiresSensitiveConfirmation(tt.commandID) != (tt.want == CommandSensitivitySensitive) {
				t.Fatalf("RequiresSensitiveConfirmation(%q) mismatch for sensitivity %q", tt.commandID, tt.want)
			}
		})
	}
}

func TestUnknownCommandHasNoSensitivity(t *testing.T) {
	t.Parallel()

	if got, ok := SensitivityForCommand("sh"); ok || got != "" {
		t.Fatalf("SensitivityForCommand(sh) = %q, %v, want empty false", got, ok)
	}
	if RequiresSensitiveConfirmation("sh") {
		t.Fatal("RequiresSensitiveConfirmation(sh) = true, want false")
	}
}
