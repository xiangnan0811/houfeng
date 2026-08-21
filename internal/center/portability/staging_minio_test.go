package portability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"houfeng/internal/center/attachments"
	"houfeng/internal/center/records"
)

func TestPortabilityImportStagingMinIOConformance(t *testing.T) {
	if os.Getenv("HOUFENG_MINIO_INTEGRATION") != "1" {
		t.Skip("set HOUFENG_MINIO_INTEGRATION=1 to run the real MinIO suite")
	}

	client, bucket := newPortabilityMinIOFixture(t)
	blob, err := attachments.NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	base, _ := mustPortabilityService(t, portabilityHarness{enabled: true, document: records.ExportDocument{
		RecordID: "rec_export1", RevisionID: "rrv_export1", Title: "x", BodyMarkdown: "x\n",
		AuthorizationEpoch: 1, LockVersion: 1,
	}})
	writer := &importWriterStub{}
	imports := newMemoryImportRepository()
	base.imports = imports
	base.importer = writer
	base.rebuilder = &importRebuildStub{}
	base.backendKind = "s3"
	base.staging = NewLeasedBlobStore(blob)

	archive := mustImportArchive(t, []ArchiveEntry{{
		Path: "records/rec_source01/document.md", Classification: ArchiveClassMarkdown,
		Payload: []byte("# Disk notes\n"),
	}})
	preview, err := base.DryRun(context.Background(), DryRunRequest{
		Actor: portabilityTestActor(t), IdempotencyKey: "import-minio", Archive: archive,
	})
	if err != nil {
		t.Fatalf("DryRun() error = %v", err)
	}
	artifact, ok := imports.artifacts["rij_memory1"]
	if !ok || artifact.BackendKind != "s3" || artifact.ObjectVersionID == "" {
		t.Fatalf("MinIO import artifact = %#v", artifact)
	}

	base.mu.Lock()
	base.importPlans = map[string]cachedImportPlan{}
	base.mu.Unlock()
	base.staging.dropLease("rij_memory1")

	applied, err := base.Apply(context.Background(), ApplyRequest{
		Actor: portabilityTestActor(t), PlanID: preview.PlanID, LockVersion: preview.LockVersion,
	})
	if err != nil {
		t.Fatalf("Apply after MinIO lease drop error = %v", err)
	}
	if writer.writes != 1 || len(applied.RecordIDs) != 1 {
		t.Fatalf("writes=%d applied=%#v", writer.writes, applied)
	}
}

func TestPortabilityLeasedBlobStoreStageImportMinIORoundTrip(t *testing.T) {
	if os.Getenv("HOUFENG_MINIO_INTEGRATION") != "1" {
		t.Skip("set HOUFENG_MINIO_INTEGRATION=1 to run the real MinIO suite")
	}

	client, bucket := newPortabilityMinIOFixture(t)
	inner, err := attachments.NewS3BlobStore(client, bucket)
	if err != nil {
		t.Fatalf("NewS3BlobStore() error = %v", err)
	}
	store := NewLeasedBlobStore(inner)
	payload := []byte("minio-import-bytes")
	version, err := store.StageImport(context.Background(), "rij_minio1", payload)
	if err != nil {
		t.Fatalf("StageImport() error = %v", err)
	}
	if version.VersionID == "" {
		t.Fatal("MinIO StageImport returned empty object version")
	}
	store.dropLease("rij_minio1")
	reader, _, err := store.OpenPublished(context.Background(), "rij_minio1", version)
	if err != nil {
		t.Fatalf("OpenPublished() error = %v", err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil || string(got) != string(payload) {
		t.Fatalf("OpenPublished() = %q %v", got, err)
	}
}

func newPortabilityMinIOFixture(t *testing.T) (*minio.Client, string) {
	t.Helper()
	endpoint := requiredPortabilityMinIOEnv(t, "HOUFENG_MINIO_ENDPOINT")
	accessKey := requiredPortabilityMinIOEnv(t, "HOUFENG_MINIO_ACCESS_KEY")
	secretKey := requiredPortabilityMinIOEnv(t, "HOUFENG_MINIO_SECRET_KEY")
	bucketPrefix := requiredPortabilityMinIOEnv(t, "HOUFENG_MINIO_BUCKET")
	secure := false
	if value := os.Getenv("HOUFENG_MINIO_SECURE"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			t.Fatalf("parse HOUFENG_MINIO_SECURE: %v", err)
		}
		secure = parsed
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: secure,
	})
	if err != nil {
		t.Fatalf("minio.New() error = %v", err)
	}
	prefix := strings.Trim(strings.ToLower(bucketPrefix), "-.")
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		t.Fatalf("bucket suffix: %v", err)
	}
	bucket := prefix + "-imp-" + hex.EncodeToString(random)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatalf("MakeBucket() error = %v", err)
	}
	if err := client.EnableVersioning(ctx, bucket); err != nil {
		t.Fatalf("EnableVersioning() error = %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		for object := range client.ListObjects(cleanupCtx, bucket, minio.ListObjectsOptions{
			Recursive: true, WithVersions: true,
		}) {
			if object.Err != nil {
				t.Errorf("ListObjects(cleanup) error = %v", object.Err)
				continue
			}
			if err := client.RemoveObject(cleanupCtx, bucket, object.Key, minio.RemoveObjectOptions{
				VersionID: object.VersionID, GovernanceBypass: true,
			}); err != nil {
				t.Errorf("RemoveObject(cleanup) error = %v", err)
			}
		}
		if err := client.RemoveBucket(cleanupCtx, bucket); err != nil {
			t.Errorf("RemoveBucket(cleanup) error = %v", err)
		}
	})
	versioning, err := client.GetBucketVersioning(ctx, bucket)
	if err != nil || !versioning.Enabled() {
		t.Fatalf("MinIO versioning: %v enabled=%v", err, versioning.Enabled())
	}
	return client, bucket
}

func requiredPortabilityMinIOEnv(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Fatalf("%s must be set when HOUFENG_MINIO_INTEGRATION=1", name)
	}
	return value
}
