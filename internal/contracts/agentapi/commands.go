package agentapi

type CommandSensitivity string

const (
	CommandSensitivityStandard  CommandSensitivity = "standard"
	CommandSensitivitySensitive CommandSensitivity = "sensitive"
)

type CommandDefinition struct {
	ID          string
	Sensitivity CommandSensitivity
}

var knownCommandDefinitions = []CommandDefinition{
	{ID: "df_h", Sensitivity: CommandSensitivityStandard},
	{ID: "free_m", Sensitivity: CommandSensitivityStandard},
	{ID: "uptime", Sensitivity: CommandSensitivityStandard},
	{ID: "top_head", Sensitivity: CommandSensitivitySensitive},
	{ID: "journalctl_u", Sensitivity: CommandSensitivitySensitive},
	{ID: "systemctl_status", Sensitivity: CommandSensitivitySensitive},
	{ID: "dmesg_err", Sensitivity: CommandSensitivitySensitive},
	{ID: "docker_ps", Sensitivity: CommandSensitivitySensitive},
}

var knownCommandIDs = func() map[string]CommandDefinition {
	out := make(map[string]CommandDefinition, len(knownCommandDefinitions))
	for _, command := range knownCommandDefinitions {
		out[command.ID] = command
	}
	return out
}()

func IsKnownCommandID(commandID string) bool {
	_, ok := knownCommandIDs[commandID]
	return ok
}

func KnownCommandDefinitions() []CommandDefinition {
	return append([]CommandDefinition(nil), knownCommandDefinitions...)
}

func SensitivityForCommand(commandID string) (CommandSensitivity, bool) {
	command, ok := knownCommandIDs[commandID]
	if !ok {
		return "", false
	}
	return command.Sensitivity, true
}

func RequiresSensitiveConfirmation(commandID string) bool {
	sensitivity, ok := SensitivityForCommand(commandID)
	return ok && sensitivity == CommandSensitivitySensitive
}
