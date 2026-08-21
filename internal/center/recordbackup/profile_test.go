package recordbackup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalProfileCreatePublishesReadableArtifacts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := NewLocalStore(root)
	if err != nil {
		t.Fatalf("NewLocalStore() error = %v", err)
	}
	options := fixtureOptions(t, store, ProfileLocal)
	backup, err := NewService(options)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	if _, err := backup.Plan(context.Background(), Request{Profile: ProfileLocal}); err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	manifest, receipt, err := backup.Create(context.Background(), Request{Profile: ProfileLocal})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if manifest.Profile() != ProfileLocal || manifest.CompletionDigest() == ([sha256.Size]byte{}) {
		t.Fatalf("Create() manifest profile/digest = %q %#x", manifest.Profile(), manifest.CompletionDigest())
	}
	if len(receipt.AbortedArtifacts()) != 0 {
		t.Fatalf("Create() cleanup = %#v", receipt.AbortedArtifacts())
	}
	if err := backup.Verify(context.Background(), manifest); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	reader, err := store.Open(context.Background(), manifest.Database())
	if err != nil {
		t.Fatalf("Open(database) error = %v", err)
	}
	payload, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || string(payload) != "database-artifact" {
		t.Fatalf("Open(database) = %q %v", payload, err)
	}
}

func TestLocalStoreCleanupRemovesPartialArtifacts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := NewLocalStore(root)
	if err != nil {
		t.Fatalf("NewLocalStore() error = %v", err)
	}
	artifact, err := NewArtifactRef("postgres_dump", "db.v1", sha256.Sum256([]byte("partial")), 7, ClassificationDatabase)
	if err != nil {
		t.Fatalf("NewArtifactRef() error = %v", err)
	}
	if err := store.Stage(context.Background(), artifact, bytes.NewReader([]byte("partial"))); err != nil {
		t.Fatalf("Stage() error = %v", err)
	}
	if err := store.Abort(context.Background(), artifact); err != nil {
		t.Fatalf("Abort() error = %v", err)
	}
	if err := store.AbortMultipart(context.Background(), artifact); err != nil {
		t.Fatalf("AbortMultipart() error = %v", err)
	}
	if err := store.ReleasePin(context.Background(), artifact); err != nil {
		t.Fatalf("ReleasePin() error = %v", err)
	}
	if err := store.ReleaseWorkspace(context.Background()); err != nil {
		t.Fatalf("ReleaseWorkspace() error = %v", err)
	}
	if leftover := leftoverProfileFiles(t, root); leftover != 0 {
		t.Fatalf("cleanup left %d files under %s", leftover, root)
	}
}

func leftoverProfileFiles(t *testing.T, root string) int {
	t.Helper()
	count := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.IsDir() {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return count
}

func TestProfileReportIsDeterministicAndContentSafe(t *testing.T) {
	t.Parallel()

	report, err := NewProfileReport(ProfileReportInput{
		Profile:         ProfileLocal,
		Commit:          "6a37448ddeadbeef",
		ConfigDigest:    sha256.Sum256([]byte("profile-config")),
		Suites:          []string{"recordbackup.local", "store.witness"},
		PermanentDelete: "disabled",
		Missing:         []string{"deletion.record_markdown_client", "deletion.record_comparison"},
	})
	if err != nil {
		t.Fatalf("NewProfileReport() error = %v", err)
	}
	left, err := report.Encode()
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	right, err := report.Encode()
	if err != nil {
		t.Fatalf("Encode() second error = %v", err)
	}
	if !bytes.Equal(left, right) {
		t.Fatalf("Encode() is not deterministic:\n%s\n%s", left, right)
	}
	if !json.Valid(left) {
		t.Fatalf("Encode() is not JSON: %s", left)
	}
	text := string(left)
	for _, leaked := range []string{
		"postgres://", "password=secret", "# title", "filename.md", `"note"`, "DATABASE_URL",
	} {
		if strings.Contains(text, leaked) {
			t.Fatalf("Encode() leaked %q: %s", leaked, text)
		}
	}
	for _, required := range []string{
		`"format":"houfeng-record-profile-report/v1"`,
		`"profile":"local"`,
		`"permanent_delete":"disabled"`,
		`"config_digest"`,
		`"suites"`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("Encode() missing %s: %s", required, text)
		}
	}
}

func TestNewS3StoreRejectsNilClient(t *testing.T) {
	t.Parallel()

	_, err := NewS3Store(nil, "bucket")
	if !errors.Is(err, ErrInvalidBackupRequest) {
		t.Fatalf("NewS3Store(nil) error = %v, want ErrInvalidBackupRequest", err)
	}
}
