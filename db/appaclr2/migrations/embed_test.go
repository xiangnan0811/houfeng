package migrations_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"strings"
	"testing"

	appaclr2migrations "houfeng/db/appaclr2/migrations"
	rootmigrations "houfeng/db/migrations"
)

const appACLR2IsolatedMigrationName = "0052_app_acl_r2_privileged_transition.sql"

const appACLR2IsolatedMigrationSQL = `-- HOUFENG-APP-ACL-R2-PRIVILEGED-TRANSITION-V1
--
-- This source is intentionally isolated from the generic application migration
-- inventory. Its complete bytes and marker-inclusive section bytes are bound
-- by the R2 receipt without exposing 0052 to the root migration filesystem.
--
-- HOUFENG-APP-ACL-R2-BOOTSTRAP-BEGIN
create function record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(bytea, bytea)
returns boolean
language sql
immutable
parallel safe
set search_path = pg_catalog
as $app_acl_r2_assert_bootstrap_receipt_insert$
  select pg_catalog.octet_length($1) between 1 and 4194304
    and pg_catalog.octet_length($2) = 32
    and record_platform_internal.digest($1, 'sha256') = $2
$app_acl_r2_assert_bootstrap_receipt_insert$;

create function record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation()
returns trigger
language plpgsql
volatile
parallel unsafe
set search_path = pg_catalog
as $app_acl_r2_reject_bootstrap_receipt_mutation$
begin
  raise sqlstate '55000'
    using message = 'app_acl_r2_bootstrap_receipt is immutable';
end;
$app_acl_r2_reject_bootstrap_receipt_mutation$;

create table public.app_acl_r2_bootstrap_receipt (
  singleton boolean primary key default true check (singleton),
  receipt_body bytea not null
    check (pg_catalog.octet_length(receipt_body) between 1 and 4194304),
  receipt_digest bytea not null
    check (pg_catalog.octet_length(receipt_digest) = 32),
  check (record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(
    receipt_body,
    receipt_digest
  ))
);

create trigger app_acl_r2_bootstrap_receipt_immutable
before update or delete or truncate on public.app_acl_r2_bootstrap_receipt
for each statement
execute function record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation();

revoke all privileges on table public.app_acl_r2_bootstrap_receipt from public;
revoke all privileges on function record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(bytea, bytea) from public;
revoke all privileges on function record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation() from public;
-- HOUFENG-APP-ACL-R2-BOOTSTRAP-END
--
-- HOUFENG-APP-ACL-R2-FINALIZE-BEGIN
-- Finalize implementation is owned by Slice 5.
-- HOUFENG-APP-ACL-R2-FINALIZE-END
`

const appACLR2IsolatedMigrationSHA256 = "7e15c579cd2055d61d1768c35556032f3ec4c17950c2a15ef7e5e22f4350fc01"

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

func TestAppACLR2ReceiptSQLCreatesOnlyBootstrapOwnedL2Surface(t *testing.T) {
	payload, err := fs.ReadFile(appaclr2migrations.FS, appACLR2IsolatedMigrationName)
	if err != nil {
		t.Fatalf("ReadFile(isolated, %q) error = %v", appACLR2IsolatedMigrationName, err)
	}
	sql := strings.ToLower(string(payload))
	for _, required := range []string{
		"create table public.app_acl_r2_bootstrap_receipt",
		"create function record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(bytea, bytea)",
		"create function record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation()",
		"create trigger app_acl_r2_bootstrap_receipt_immutable",
		"revoke all privileges on table public.app_acl_r2_bootstrap_receipt from public",
		"revoke all privileges on function record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(bytea, bytea) from public",
		"revoke all privileges on function record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation() from public",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("isolated receipt SQL is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"app_acl_r2_manifest_revisions",
		"app_acl_r2_manifest_head",
		"create role",
		"alter role",
		"grant membership",
		"alter extension",
		"drop extension",
		"create extension",
		"owner to",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("isolated Slice 3 SQL contains forbidden surface %q", forbidden)
		}
	}
}
