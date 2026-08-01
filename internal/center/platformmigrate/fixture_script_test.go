package platformmigrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegrationFixtureScriptBuildsFourIsolatedPostgresDomains(t *testing.T) {
	payload, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "test-record-platform-integration.sh"))
	if err != nil {
		t.Fatalf("read integration fixture script: %v", err)
	}

	script := string(payload)
	for _, want := range []string{
		"mktemp -d",
		"--network=host",
		"pg_control_system()",
		"HOUFENG_DATABASE_URL",
		"HOUFENG_DELETION_LEDGER_DATABASE_URL",
		"HOUFENG_DELETION_WITNESS_DATABASE_URL",
		"HOUFENG_RECOVERY_CONTROL_DATABASE_URL",
		"HOUFENG_POSTGRES_INTEGRATION=1",
		"--- SKIP:",
		"docker rm -f",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("integration fixture script missing %q", want)
		}
	}
}
