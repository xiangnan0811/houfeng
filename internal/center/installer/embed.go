package installer

import _ "embed"

// Script is the Linux systemd installer served by each center instance.
//
//go:embed houfeng-agent-install.sh
var Script string
