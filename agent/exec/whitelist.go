// Package exec provides whitelist-gated command execution for the agent.
// The whitelist is compiled into the agent binary. Adding a new command
// requires recompiling and redeploying the agent.
package exec

// CommandDef maps a command ID to the binary and fixed arguments that the
// agent is allowed to execute. Args is nil when the command takes no args.
type CommandDef struct {
	Bin  string
	Args []string
}

// whitelist is the hardcoded set of allowed commands. Each key is a
// command_id sent from the center; the value defines what the agent
// actually runs. The center never supplies arguments — all arguments
// are fixed at compile time.
var whitelist = map[string]CommandDef{
	"df_h":             {"df", []string{"-h"}},
	"free_m":           {"free", []string{"-m"}},
	"uptime":           {"uptime", nil},
	"top_head":         {"top", []string{"-bn1"}},
	"journalctl_u":     {"journalctl", []string{"--lines=50"}},
	"systemctl_status": {"systemctl", []string{"status"}},
	"dmesg_err":        {"dmesg", []string{"--level=err"}},
	"docker_ps":        {"docker", []string{"ps"}},
}

// Lookup returns the binary and arguments for the given command ID.
// ok is false when the command ID is not in the whitelist.
func Lookup(id string) (bin string, args []string, ok bool) {
	cmd, ok := whitelist[id]
	if !ok {
		return "", nil, false
	}
	return cmd.Bin, cmd.Args, true
}
