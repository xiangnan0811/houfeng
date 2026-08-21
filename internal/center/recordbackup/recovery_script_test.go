package recordbackup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecordsRecoveryScriptOwnsLocalAndS3Profiles(t *testing.T) {
	t.Parallel()

	payload, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "run-records-recovery.sh"))
	if err != nil {
		t.Fatalf("read run-records-recovery.sh: %v", err)
	}
	script := string(payload)
	for _, want := range []string{
		"--profile",
		"--all",
		"local",
		"s3",
		"scripts/test-record-platform-integration.sh",
		"HOUFENG_POSTGRES_INTEGRATION=1",
		"HOUFENG_MINIO_INTEGRATION=1",
		"--- SKIP:",
		"mktemp -d",
		"Resurrection",
		"PermanentDeleteDisabled",
		"houfeng-record-profile-report/v1",
		"permanent_delete",
		"docker rm -f",
		`rm -rf "$workspace" || true`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("run-records-recovery.sh missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"AdmissionGateFunc(",
		"houfeng-record-platform-admin",
		"postgres://houfeng",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("run-records-recovery.sh must not contain %q", forbidden)
		}
	}
}
