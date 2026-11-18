package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"houfeng/internal/center/recordbirth"
)

// The container supplies protected launch ancestors without changing host /run
// or ownership of the caller's checkout. Blob/workspace binds retain the real
// host filesystem and owner; this fixture exercises host-mode runtime identity.
func newContentProcessorLaunchFixture(t *testing.T, parent context.Context, fixture recordPlatformPostgresFixture, binary, blobRoot, workspaceRoot string) (string, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	var deploymentID string
	var activation []byte
	var enrolled bool
	if err := fixture.db.QueryRow(ctx, `select deployment_id, activation_bundle_digest,
		exists(select 1 from public.source_authority_admission where singleton)
		from public.deployment_contract_state where project_id='default'`).Scan(&deploymentID, &activation, &enrolled); err != nil {
		t.Fatal(err)
	}
	if enrolled || len(activation) != sha256.Size {
		t.Fatal("processor crash fixture requires an activated, never-enrolled APP")
	}
	digest, err := recordbirth.DigestExecutable(binary)
	if err != nil {
		t.Fatal(err)
	}
	binding := recordbirth.LaunchBinding{ProtocolVersion: 1, ProcessKind: "content-processor", DeploymentID: deploymentID, BinaryDigest: digest}
	copy(binding.ActivationBundleDigest[:], activation)
	processor, err := recordbirth.EncodeLaunchBinding(binding)
	if err != nil {
		t.Fatal(err)
	}
	// Only the processor runs here. The paired center registration binds the
	// same measured fixture executable; it is not center-bootstrap evidence.
	binding.ProcessKind = "center"
	center, err := recordbirth.EncodeLaunchBinding(binding)
	if err != nil {
		t.Fatal(err)
	}
	owner := fixture.openDirectRolePool(t, ctx, fixture.migrator, "processor-launch-owner", 1)
	var revision int64
	if err := owner.QueryRow(ctx, `select record_platform_internal.record_birth_launch_stage($1::bytea,$2::bytea,$3::name,$4::name,0::bigint)`,
		center, processor, fixture.runtime, fixture.deletionWorker).Scan(&revision); err != nil || revision != 1 {
		t.Fatalf("stage processor launch: revision=%d error=%v", revision, err)
	}
	if err := os.Mkdir(workspaceRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	identity := strconv.Itoa(os.Geteuid()) + ":" + strconv.Itoa(os.Getegid())
	create := exec.CommandContext(ctx, "docker", "create", "--network=host", "--read-only", "--user", identity,
		"--group-add", "10001", "--tmpfs", "/run:rw,nosuid,nodev,noexec,size=1m",
		"--mount", "type=bind,source="+binary+",target=/processor-helper,readonly",
		"--mount", "type=bind,source="+blobRoot+",target="+blobRoot,
		"--mount", "type=bind,source="+workspaceRoot+",target="+workspaceRoot,
		"--entrypoint", "/bin/sh", "postgres:16-alpine", "-c", "exec sleep 600")
	output, err := create.CombinedOutput()
	if err != nil {
		t.Fatalf("create owned processor fixture: %v: %s", err, output)
	}
	container := strings.TrimSpace(string(output))
	if len(container) != 64 || strings.ContainsAny(container, " \n\r\t") {
		t.Fatal("invalid processor fixture container identity")
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if output, err := exec.CommandContext(cleanupCtx, "docker", "rm", "--force", "--volumes", container).CombinedOutput(); err != nil {
			t.Errorf("remove owned processor fixture: %v: %s", err, output)
		}
	})
	if output, err := exec.CommandContext(ctx, "docker", "start", container).CombinedOutput(); err != nil {
		t.Fatalf("start processor fixture: %v: %s", err, output)
	}
	prepare := exec.CommandContext(ctx, "docker", "exec", "--user", "0:0", container, "/bin/sh", "-c",
		"mkdir -p /run/houfeng/records-authority && chmod 0755 /run/houfeng /run/houfeng/records-authority && chown 10002:10001 /run/houfeng/records-authority")
	if output, err := prepare.CombinedOutput(); err != nil {
		t.Fatalf("prepare protected launch directory: %v: %s", err, output)
	}
	publish := exec.CommandContext(ctx, "docker", "exec", "--interactive", "--user", "10002:10001", container, "/bin/sh", "-c",
		"set -Ce; umask 027; cat > /run/houfeng/records-authority/birth-launch-processor.json; chmod 0440 /run/houfeng/records-authority/birth-launch-processor.json; sync /run/houfeng/records-authority/birth-launch-processor.json /run/houfeng/records-authority")
	publish.Stdin = bytes.NewReader(processor)
	if output, err := publish.CombinedOutput(); err != nil {
		t.Fatalf("publish protected launch file: %v: %s", err, output)
	}
	readback := exec.CommandContext(ctx, "docker", "exec", "--user", fmt.Sprintf("%d:10001", os.Geteuid()), container,
		"cat", "/run/houfeng/records-authority/birth-launch-processor.json")
	if output, err := readback.CombinedOutput(); err != nil || !bytes.Equal(output, processor) {
		t.Fatalf("read back protected processor launch: %v", err)
	}
	if _, err := owner.Exec(ctx, `select record_platform_internal.record_birth_launch_open($1::bigint)`, revision); err != nil {
		t.Fatalf("open exact processor launch registration: %v", err)
	}
	return container, deploymentID
}
