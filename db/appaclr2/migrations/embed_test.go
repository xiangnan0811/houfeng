package migrations_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"testing"

	appaclr2migrations "houfeng/db/appaclr2/migrations"
	rootmigrations "houfeng/db/migrations"
)

const appACLR2IsolatedMigrationName = "0052_app_acl_r2_privileged_transition.sql"

const appACLR2IsolatedMigrationSQL = `-- HOUFENG-APP-ACL-R2-PRIVILEGED-TRANSITION-V1
--
-- This source is intentionally isolated from the generic application migration
-- inventory. Later slices replace the marker bodies without exposing 0052 to
-- the root migration filesystem.
--
-- HOUFENG-APP-ACL-R2-BOOTSTRAP-BEGIN
-- Bootstrap implementation is owned by Slice 3.
-- HOUFENG-APP-ACL-R2-BOOTSTRAP-END
--
-- HOUFENG-APP-ACL-R2-FINALIZE-BEGIN
-- Finalize implementation is owned by Slice 5.
-- HOUFENG-APP-ACL-R2-FINALIZE-END
`

const appACLR2IsolatedMigrationSHA256 = "8c80d64adfbe80d6bb28c4bbf82bfc0a64560be81615721b8f18f7283d8188ab"

func TestAppACLR2SourceEmbeddedInventoryIsIsolated(t *testing.T) {
	rootEntries, err := fs.ReadDir(rootmigrations.FS, ".")
	if err != nil {
		t.Fatalf("ReadDir(root migrations) error = %v", err)
	}
	rootSQLCount := 0
	for _, entry := range rootEntries {
		if entry.IsDir() || len(entry.Name()) < 4 || entry.Name()[len(entry.Name())-4:] != ".sql" {
			continue
		}
		rootSQLCount++
		if entry.Name() == appACLR2IsolatedMigrationName {
			t.Fatalf("root migration filesystem exposes isolated %q", entry.Name())
		}
	}
	if rootSQLCount != 52 {
		t.Fatalf("root SQL inventory count = %d, want frozen 52", rootSQLCount)
	}
	if _, err := fs.ReadFile(rootmigrations.FS, appACLR2IsolatedMigrationName); err == nil {
		t.Fatalf("ReadFile(root, %q) error = nil, want invisibility", appACLR2IsolatedMigrationName)
	}

	isolatedEntries, err := fs.ReadDir(appaclr2migrations.FS, ".")
	if err != nil {
		t.Fatalf("ReadDir(isolated migrations) error = %v", err)
	}
	if len(isolatedEntries) != 1 || isolatedEntries[0].IsDir() || isolatedEntries[0].Name() != appACLR2IsolatedMigrationName {
		t.Fatalf("isolated inventory = %#v, want exactly %q", isolatedEntries, appACLR2IsolatedMigrationName)
	}
	payload, err := fs.ReadFile(appaclr2migrations.FS, appACLR2IsolatedMigrationName)
	if err != nil {
		t.Fatalf("ReadFile(isolated, %q) error = %v", appACLR2IsolatedMigrationName, err)
	}
	if string(payload) != appACLR2IsolatedMigrationSQL {
		t.Fatalf("isolated SQL bytes = %q, want fixed scaffold %q", payload, appACLR2IsolatedMigrationSQL)
	}
	digest := sha256.Sum256(payload)
	if got := hex.EncodeToString(digest[:]); got != appACLR2IsolatedMigrationSHA256 {
		t.Fatalf("isolated SQL SHA-256 = %s, want %s", got, appACLR2IsolatedMigrationSHA256)
	}
}
