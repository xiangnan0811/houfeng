package installer

import (
	"strings"
	"testing"
)

func TestScriptMatchesReleaseChecksumManifestLines(t *testing.T) {
	t.Parallel()

	if !strings.Contains(Script, `awk -v asset="$ASSET" '$2 == asset`) {
		t.Fatalf("installer script should select checksum manifest rows by asset field, not whitespace-sensitive grep")
	}
	if strings.Contains(Script, `grep "  ${ASSET}$"`) {
		t.Fatalf("installer script should not require a GNU sha256sum two-space manifest separator")
	}
}

func TestScriptRestartsActiveServiceAfterInstall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want string
	}{
		{name: "daemon reload", want: "systemctl daemon-reload"},
		{name: "enable only", want: "systemctl enable houfeng-agent"},
		{name: "active check", want: "if systemctl is-active --quiet houfeng-agent; then"},
		{name: "restart active", want: "systemctl restart houfeng-agent"},
		{name: "start inactive", want: "systemctl start houfeng-agent"},
	}

	previousIndex := -1
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			index := strings.Index(Script, tt.want)
			if index == -1 {
				t.Fatalf("installer script missing %q", tt.want)
			}
			if index <= previousIndex {
				t.Fatalf("installer script should run %q after the previous systemd step", tt.want)
			}
			previousIndex = index
		})
	}
	if strings.Contains(Script, "systemctl enable --now houfeng-agent") {
		t.Fatal("installer script should not use enable --now because it does not restart active services")
	}
}
