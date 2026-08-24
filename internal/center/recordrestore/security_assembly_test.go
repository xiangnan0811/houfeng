package recordrestore

import (
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
	"time"

	"houfeng/internal/center/recordbackup"
	"houfeng/internal/center/recorddeletion"
	"houfeng/internal/center/recordreadiness"
)

func TestChild11AssemblyArtifactsRejectLeakCorpus(t *testing.T) {
	t.Parallel()

	registry, err := recordreadiness.NewRegistry(recordreadiness.RegistryInput{})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	matrix, err := registry.Evaluate(context.Background())
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	encodedMatrix, err := matrix.Encode()
	if err != nil {
		t.Fatalf("matrix Encode() error = %v", err)
	}
	if err := recordreadiness.ScanContentSafe(encodedMatrix); err != nil {
		t.Fatalf("readiness matrix leaked: %v", err)
	}

	database, err := recordbackup.NewArtifactRef(
		"postgres_dump", "db.v1",
		sha256.Sum256([]byte("database-artifact")), uint64(len("database-artifact")),
		recordbackup.ClassificationDatabase,
	)
	if err != nil {
		t.Fatalf("NewArtifactRef(database) error = %v", err)
	}
	object, err := recordbackup.NewArtifactRef(
		"record_attachments", "blob.v1",
		sha256.Sum256([]byte("object-artifact")), uint64(len("object-artifact")),
		recordbackup.ClassificationObject,
	)
	if err != nil {
		t.Fatalf("NewArtifactRef(object) error = %v", err)
	}
	deletion, err := recordbackup.NewDeletionWatermark(7, sha256.Sum256([]byte("deletion-watermark")))
	if err != nil {
		t.Fatalf("NewDeletionWatermark() error = %v", err)
	}
	manifest, err := recordbackup.NewManifest(recordbackup.ManifestInput{
		BuildCommit:     "6a37448ddeadbeef",
		BuildVersion:    "0.73.1",
		MigrationDigest: sha256.Sum256([]byte("migration-digest-fixture")),
		AppACLDigest:    sha256.Sum256([]byte("app-acl-digest-fixture")),
		Database:        database,
		Objects:         []recordbackup.ArtifactRef{object},
		Deletion:        deletion,
		CreatedAt:       time.Unix(1_700_000_000, 0).UTC(),
		Profile:         recordbackup.ProfileLocal,
	})
	if err != nil {
		t.Fatalf("NewManifest() error = %v", err)
	}
	encodedManifest, err := manifest.Encode()
	if err != nil {
		t.Fatalf("manifest Encode() error = %v", err)
	}
	if err := recordreadiness.ScanContentSafe(encodedManifest); err != nil {
		t.Fatalf("backup manifest leaked: %v", err)
	}

	report, err := recordbackup.NewProfileReport(recordbackup.ProfileReportInput{
		Profile:         recordbackup.ProfileLocal,
		Commit:          "6a37448ddeadbeef",
		ConfigDigest:    sha256.Sum256([]byte("profile-config")),
		Suites:          []string{"recordbackup.local"},
		PermanentDelete: "disabled",
		Missing:         []string{"deletion.record_markdown_client"},
	})
	if err != nil {
		t.Fatalf("NewProfileReport() error = %v", err)
	}
	encodedReport, err := report.Encode()
	if err != nil {
		t.Fatalf("report Encode() error = %v", err)
	}
	if err := recordreadiness.ScanContentSafe(encodedReport); err != nil {
		t.Fatalf("profile report leaked: %v", err)
	}

	encodedCopies, err := EncodeExternalCopies([]recorddeletion.SurvivingCopySummary{{
		Scope:     recorddeletion.AdapterNameRecordPortability,
		Kind:      recorddeletion.SurvivingCopyKindDeliveredExport,
		CopyCount: 1,
	}})
	if err != nil {
		t.Fatalf("EncodeExternalCopies() error = %v", err)
	}
	if err := recordreadiness.ScanContentSafe(encodedCopies); err != nil {
		t.Fatalf("external copies leaked: %v", err)
	}

	root := filepath.Join("..", "..", "..")
	for _, rel := range []string{
		filepath.Join("scripts", "run-records-integration.sh"),
		filepath.Join("scripts", "run-records-recovery.sh"),
		filepath.Join("scripts", "run-records-security.sh"),
		filepath.Join("scripts", "lib", "records-runner-lifecycle.sh"),
		filepath.Join("scripts", "test-records-s3-lifecycle.sh"),
		filepath.Join("cmd", "houfeng-backup", "main.go"),
		filepath.Join("cmd", "houfeng-restore", "main.go"),
	} {
		payload, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if err := recordreadiness.ScanContentSafe(payload); err != nil {
			t.Fatalf("%s leaked: %v", rel, err)
		}
	}
}
