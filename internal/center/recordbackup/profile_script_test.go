package recordbackup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecordsIntegrationScriptOwnsLocalAndS3Profiles(t *testing.T) {
	t.Parallel()

	payload, err := os.ReadFile(filepath.Join("..", "..", "..", "scripts", "run-records-integration.sh"))
	if err != nil {
		t.Fatalf("read run-records-integration.sh: %v", err)
	}
	script := string(payload)
	for _, want := range []string{
		"--profile",
		"local",
		"s3",
		"scripts/test-record-platform-integration.sh",
		"HOUFENG_POSTGRES_INTEGRATION=1",
		"HOUFENG_MINIO_INTEGRATION=1",
		"--- SKIP:",
		"mktemp -d",
		"WitnessedRecordSubject",
		"RecordPortabilityDeletion",
		"MinIO",
		"houfeng-record-profile-report/v1",
		"permanent_delete",
		`source "$root/scripts/lib/records-runner-lifecycle.sh"`,
		"records_runner_install_cleanup",
		"docker volume create",
		`--label "com.houfeng.records.runner=$records_runner_kind"`,
		`--label "com.houfeng.records.run=$records_run_id"`,
		`--mount "type=volume,source=$minio_volume,target=/data"`,
		`volumes+=("$minio_volume")`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("run-records-integration.sh missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"AdmissionGateFunc(",
		"houfeng-record-platform-admin",
		"postgres://houfeng",
		`$workspace/minio`,
		`-v "$data:/data"`,
		`rm -rf "$workspace" || true`,
		`docker rm -f "$container" >/dev/null 2>&1 || true`,
		"docker system prune",
		"docker volume prune",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("run-records-integration.sh must not contain %q", forbidden)
		}
	}
}
