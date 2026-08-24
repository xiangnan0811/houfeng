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
	helperPayload, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "lib", "records-runner-lifecycle.sh"))
	if err != nil {
		t.Fatalf("read records runner lifecycle helper: %v", err)
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
		`source "$root/scripts/lib/records-runner-lifecycle.sh"`,
		"records_runner_install_cleanup",
		"records_runner_finish_evidence",
		`containers+=("$name")`,
		`--label "com.houfeng.records.runner=$records_runner_kind"`,
		`--label "com.houfeng.records.run=$records_run_id"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("integration fixture script missing %q", want)
		}
	}
	for _, forbidden := range []string{
		`docker rm -f "$container" >/dev/null 2>&1 || true`,
		"docker system prune",
		"docker volume prune",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("integration fixture script must not contain %q", forbidden)
		}
	}
	helper := string(helperPayload)
	for _, want := range []string{
		`if grep -Fq -- '--- SKIP:' "$stdout_file" "$stderr_file"`,
		`case "$scan_status" in`,
		"records runner evidence scan failed: status %s",
	} {
		if !strings.Contains(helper, want) {
			t.Fatalf("records runner lifecycle helper missing %q", want)
		}
	}
}
