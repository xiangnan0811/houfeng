package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordplatform"
)

// This is the C80 source revision that predates the unshipped C81 successor.
// The driver runs that actual revision's direct-login migration and V1 methods.
const recordDeletionPredecessorRevision = "ec38a48d3538735ee343698d7ed5b4ecfeb4ad12"

func TestPostgresIntegrationRecordDeletionPredecessorConvergesBeforeC81(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	fixture := newRecordPlatformPostgresBaseFixture(t, ctx)
	base, err := url.Parse(os.Getenv("HOUFENG_DATABASE_URL"))
	if err != nil || (base.Scheme != "postgres" && base.Scheme != "postgresql") || base.Host == "" {
		t.Fatal("predecessor fixture requires the runner PostgreSQL URL")
	}
	base.User = url.UserPassword(fixture.migrator, fixture.passwords[fixture.migrator])
	base.Path = "/" + fixture.databaseName
	driver := newRecordDeletionPredecessorDriver(t, ctx)
	driver.run(t, ctx, recordDeletionPredecessorRequest{
		Action: "converge", DatabaseURL: base.String(), RuntimeRole: fixture.runtime, AdminRole: fixture.admin,
	})
	var predecessor, successor, gate bool
	if err := fixture.db.QueryRow(ctx, `select
		exists(select 1 from public.schema_migrations where name='0080_bound_agent_sync_replay.sql'),
		exists(select 1 from public.schema_migrations where name='0081_record_deletion_successor.sql'),
		pg_catalog.to_regclass('public.record_deletion_gates') is not null`).Scan(&predecessor, &successor, &gate); err != nil {
		t.Fatalf("read predecessor migration state: %v", err)
	}
	if !predecessor || successor || gate {
		t.Fatalf("predecessor, successor, gate = %t, %t, %t", predecessor, successor, gate)
	}
}

type recordDeletionPredecessorRequest struct {
	Action             string                                   `json:"action"`
	DatabaseURL        string                                   `json:"database_url"`
	RuntimeRole        string                                   `json:"runtime_role"`
	AdminRole          string                                   `json:"admin_role"`
	DeploymentID       recordplatform.DeploymentID              `json:"deployment_id"`
	Preview            recorddeletion.CreatePreviewCommand      `json:"preview"`
	Fingerprint        recordplatform.RequestFingerprintInputV1 `json:"fingerprint"`
	ReservationID      string                                   `json:"reservation_id"`
	TokenTransport     string                                   `json:"token_transport"`
	ObservedWitness    recorddeletion.WitnessHead               `json:"observed_witness"`
	ActorID            string                                   `json:"actor_id"`
	OwnerID            string                                   `json:"owner_id"`
	OwnerLeaseDuration time.Duration                            `json:"owner_lease_duration"`
}

type recordDeletionPredecessorResult struct {
	ReservationID string                            `json:"reservation_id"`
	ExpiresAt     time.Time                         `json:"expires_at"`
	Operation     *recorddeletion.DeletionOperation `json:"operation"`
}

type recordDeletionPredecessorDriver struct {
	binary string
	dir    string
}

func newRecordDeletionPredecessorDriver(t *testing.T, ctx context.Context) recordDeletionPredecessorDriver {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate predecessor driver source")
	}
	repository := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	dir := t.TempDir()
	snapshot := filepath.Join(dir, "predecessor")
	if err := os.Mkdir(snapshot, 0o700); err != nil {
		t.Fatal(err)
	}
	archive := exec.CommandContext(ctx, "git", "archive", recordDeletionPredecessorRevision)
	archive.Dir = repository
	stream, err := archive.StdoutPipe()
	if err != nil {
		t.Fatalf("archive predecessor: %v", err)
	}
	extract := exec.CommandContext(ctx, "tar", "-x", "-C", snapshot)
	extract.Stdin = stream
	var archiveError, extractError bytes.Buffer
	archive.Stderr, extract.Stderr = &archiveError, &extractError
	if err := archive.Start(); err != nil {
		t.Fatalf("start predecessor archive: %v", err)
	}
	extractErr := extract.Run()
	if extractErr != nil {
		_ = stream.Close()
	}
	archiveErr := archive.Wait()
	if extractErr != nil || archiveErr != nil {
		t.Fatalf("extract pinned predecessor: archive=%v %s extract=%v %s", archiveErr, archiveError.String(), extractErr, extractError.String())
	}
	commandDir := filepath.Join(snapshot, "cmd", "record-deletion-predecessor-fixture")
	if err := os.MkdirAll(commandDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"main.go", "inventory.go"} {
		source, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "testdata", "legacydeletion", name))
		if err != nil {
			t.Fatalf("read predecessor driver %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(commandDir, name), source, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Logf("pinned predecessor source %s sha256=%x", name, sha256.Sum256(source))
	}
	binary := filepath.Join(dir, "predecessor-driver")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/record-deletion-predecessor-fixture")
	build.Dir = snapshot
	build.Env = append(os.Environ(), "GOWORK=off")
	output, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("build pinned predecessor driver: %v: %s", err, output)
	}
	return recordDeletionPredecessorDriver{binary: binary, dir: dir}
}

func (driver recordDeletionPredecessorDriver) run(t *testing.T, ctx context.Context, input recordDeletionPredecessorRequest) recordDeletionPredecessorResult {
	t.Helper()
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("encode predecessor request: %v", err)
	}
	file, err := os.CreateTemp(driver.dir, "predecessor-request-*.json")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.Write(body); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, driver.binary, path)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("predecessor %s failed: %v: %s", input.Action, err, stderr.String())
	}
	var result recordDeletionPredecessorResult
	decoder := json.NewDecoder(&stdout)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		t.Fatalf("decode predecessor result: %v", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		t.Fatal("predecessor returned trailing data")
	}
	if result.Operation != nil {
		result.Operation.ContractVersion = recorddeletion.DeletionContractV1
		if result.Operation.Validate() != nil {
			t.Fatal("predecessor returned invalid operation")
		}
	}
	return result
}
