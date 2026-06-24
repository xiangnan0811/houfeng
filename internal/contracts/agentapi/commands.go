package agentapi

var knownCommandIDs = map[string]struct{}{
	"df_h":             {},
	"free_m":           {},
	"uptime":           {},
	"top_head":         {},
	"journalctl_u":     {},
	"systemctl_status": {},
	"dmesg_err":        {},
	"docker_ps":        {},
}

func IsKnownCommandID(commandID string) bool {
	_, ok := knownCommandIDs[commandID]
	return ok
}
