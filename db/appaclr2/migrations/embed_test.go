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

const (
	appACLR2FrozenRootMigrationCount = 52
	appACLR2FrozenRootMigrationTail  = "0051_create_record_platform_foundation.sql"
)

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
create function record_platform_internal.app_acl_r2_reject_manifest_mutation()
returns trigger
language plpgsql
volatile
parallel unsafe
set search_path = pg_catalog
as $app_acl_r2_reject_manifest_mutation$
begin
  raise sqlstate '55000'
    using message = 'app_acl_r2_manifest is immutable';
end;
$app_acl_r2_reject_manifest_mutation$;

create table public.app_acl_r2_manifest_revisions (
  protocol_version smallint not null check (protocol_version = 2),
  manifest_revision bigint not null check (manifest_revision = 2),
  m1_revision bigint not null check (m1_revision = 1),
  m1_manifest_digest bytea not null check (octet_length(m1_manifest_digest) = 32),
  m1_source_set_digest bytea not null check (octet_length(m1_source_set_digest) = 32),
  m1_privilege_set_digest bytea not null check (octet_length(m1_privilege_set_digest) = 32),
  m1_migrator_catalog_role text not null,
  direct_migrator_name text not null,
  direct_migrator_oid oid not null,
  r2_source_set_body bytea not null,
  r2_source_set_digest bytea not null check (octet_length(r2_source_set_digest) = 32),
  r2_privilege_set_body bytea not null,
  r2_privilege_set_digest bytea not null check (octet_length(r2_privilege_set_digest) = 32),
  domain_body bytea not null,
  domain_digest bytea not null check (octet_length(domain_digest) = 32),
  receipt_digest bytea not null check (octet_length(receipt_digest) = 32),
  control_acl_body bytea not null,
  control_acl_digest bytea not null check (octet_length(control_acl_digest) = 32),
  manifest_digest bytea not null check (octet_length(manifest_digest) = 32),
  recorded_at timestamptz not null default transaction_timestamp(),
  primary key (protocol_version, manifest_revision),
  unique (protocol_version, manifest_revision, manifest_digest),
  check (r2_source_set_digest = record_platform_internal.digest(r2_source_set_body, 'sha256')),
  check (r2_privilege_set_digest = record_platform_internal.digest(r2_privilege_set_body, 'sha256')),
  check (domain_digest = record_platform_internal.digest(domain_body, 'sha256')),
  check (control_acl_digest = record_platform_internal.digest(control_acl_body, 'sha256')),
  check (
    manifest_digest = record_platform_internal.digest(
      convert_to('HOUFENG-APP-ACL-MANIFEST-R2-V1', 'UTF8')
      || int2send(1::smallint)
      || int2send(protocol_version)
      || int8send(manifest_revision)
      || int8send(m1_revision)
      || m1_manifest_digest
      || m1_source_set_digest
      || m1_privilege_set_digest
      || int2send(octet_length(m1_migrator_catalog_role)::smallint)
      || convert_to(m1_migrator_catalog_role, 'UTF8')
      || int2send(octet_length(direct_migrator_name)::smallint)
      || convert_to(direct_migrator_name, 'UTF8')
      || int4send(direct_migrator_oid::integer)
      || int4send(octet_length(r2_source_set_body))
      || r2_source_set_body
      || r2_source_set_digest
      || int4send(octet_length(r2_privilege_set_body))
      || r2_privilege_set_body
      || r2_privilege_set_digest
      || int4send(octet_length(domain_body))
      || domain_body
      || domain_digest
      || receipt_digest
      || int4send(octet_length(control_acl_body))
      || control_acl_body
      || control_acl_digest
      || int8send((extract(epoch from recorded_at) * 1000000)::bigint),
      'sha256'
    )
  ),
  foreign key (m1_revision, m1_manifest_digest)
    references public.app_acl_manifest_revisions(manifest_revision, manifest_digest)
    on delete restrict
);

create table public.app_acl_r2_manifest_head (
  singleton boolean primary key default true check (singleton),
  protocol_version smallint not null check (protocol_version = 2),
  manifest_revision bigint not null check (manifest_revision = 2),
  manifest_digest bytea not null check (octet_length(manifest_digest) = 32),
  foreign key (protocol_version, manifest_revision, manifest_digest)
    references public.app_acl_r2_manifest_revisions(protocol_version, manifest_revision, manifest_digest)
    on delete restrict
);

create trigger app_acl_r2_manifest_revisions_immutable
before update or delete or truncate on public.app_acl_r2_manifest_revisions
for each statement
execute function record_platform_internal.app_acl_r2_reject_manifest_mutation();

create trigger app_acl_r2_manifest_head_immutable
before update or delete or truncate on public.app_acl_r2_manifest_head
for each statement
execute function record_platform_internal.app_acl_r2_reject_manifest_mutation();
-- HOUFENG-APP-ACL-R2-FINALIZE-END
`

const appACLR2IsolatedMigrationSHA256 = "23f79c60dcede45a42aae82da5a9de0d3d650d7eef64dbfd7ce96c6dd5d95fff"

func TestAppACLR2SourceEmbeddedInventoryIsIsolated(t *testing.T) {
	rootEntries, err := fs.ReadDir(rootmigrations.FS, ".")
	if err != nil {
		t.Fatalf("ReadDir(root migrations) error = %v", err)
	}
	rootSQLNames := make([]string, 0, len(rootEntries))
	for _, entry := range rootEntries {
		if entry.IsDir() || len(entry.Name()) < 4 || entry.Name()[len(entry.Name())-4:] != ".sql" {
			continue
		}
		rootSQLNames = append(rootSQLNames, entry.Name())
		if entry.Name() == appACLR2IsolatedMigrationName {
			t.Fatalf("root migration filesystem exposes isolated %q", entry.Name())
		}
	}
	if len(rootSQLNames) < appACLR2FrozenRootMigrationCount {
		t.Fatalf("root SQL inventory count = %d, want at least frozen prefix %d", len(rootSQLNames), appACLR2FrozenRootMigrationCount)
	}
	if got := rootSQLNames[appACLR2FrozenRootMigrationCount-1]; got != appACLR2FrozenRootMigrationTail {
		t.Fatalf("frozen root SQL tail = %q, want %q", got, appACLR2FrozenRootMigrationTail)
	}
	for _, name := range rootSQLNames[appACLR2FrozenRootMigrationCount:] {
		if name <= appACLR2FrozenRootMigrationTail {
			t.Fatalf("current root migration %q is not appended after frozen tail %q", name, appACLR2FrozenRootMigrationTail)
		}
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

func TestAppACLR2TransitionSQLCreatesOnlyAuthorizedL2AndM2Surfaces(t *testing.T) {
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
		"create table public.app_acl_r2_manifest_revisions",
		"create table public.app_acl_r2_manifest_head",
		"create function record_platform_internal.app_acl_r2_reject_manifest_mutation()",
		"create trigger app_acl_r2_manifest_revisions_immutable",
		"create trigger app_acl_r2_manifest_head_immutable",
	} {
		if !strings.Contains(sql, required) {
			t.Fatalf("isolated receipt SQL is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"create table if not exists",
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
