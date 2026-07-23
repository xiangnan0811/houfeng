package migrate

import (
	"strings"
	"testing"

	"houfeng/db/migrations"
)

func TestRecordPlatformFoundationMigrationDefinesOnlyFoundationTables(t *testing.T) {
	payload, err := migrations.FS.ReadFile("0051_create_record_platform_foundation.sql")
	if err != nil {
		t.Fatalf("read 0051 record-platform migration: %v", err)
	}

	sql := strings.ToLower(string(payload))
	for _, table := range []string{
		"record_access_groups",
		"record_access_group_members",
		"record_outbox",
		"record_idempotency_keys",
		"identity_mutation_guards",
		"deletion_reservations",
		"record_purge_operations",
		"record_deletion_audits",
		"deletion_fence_leases",
		"object_content_leases",
		"client_content_leases",
		"content_delivery_epochs",
		"backup_epochs",
		"recovery_inventory_projection",
		"deletion_replay_state",
		"deployment_membership",
		"source_deletion_tombstones",
		"deployment_contract_state",
		"record_platform_domain_identity",
		"record_platform_domain_attestations",
		"app_acl_manifest_revisions",
		"app_acl_manifest_head",
	} {
		if !strings.Contains(sql, "create table if not exists public."+table) {
			t.Fatalf("0051 migration missing foundation table %q", table)
		}
	}

	for _, forbidden := range []string{
		"create table if not exists public.records",
		"create table if not exists public.record_revisions",
		"on delete cascade",
		"raw_token",
		"markdown_body",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("0051 migration must not contain %q", forbidden)
		}
	}
}

func TestRecordPlatformFoundationMigrationPinsPGCryptoToItsLocalSchema(t *testing.T) {
	payload, err := migrations.FS.ReadFile("0051_create_record_platform_foundation.sql")
	if err != nil {
		t.Fatalf("read 0051 record-platform migration: %v", err)
	}

	sql := strings.ToLower(string(payload))
	for _, want := range []string{
		"create schema if not exists record_platform_internal",
		"create extension if not exists pgcrypto with schema record_platform_internal",
		"from pg_extension",
		"extnamespace",
		"record_platform_internal",
		"revoke all on schema record_platform_internal from public",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("0051 migration missing local pgcrypto contract %q", want)
		}
	}
}

func TestRecordPlatformFoundationMigrationBindsAppACLManifestAndLocalDomainKind(t *testing.T) {
	payload, err := migrations.FS.ReadFile("0051_create_record_platform_foundation.sql")
	if err != nil {
		t.Fatalf("read 0051 record-platform migration: %v", err)
	}

	sql := strings.ToLower(string(payload))
	for _, want := range []string{
		"domain_kind text not null check (domain_kind = 'application')",
		"constraint app_acl_manifest_digest_matches check",
		"convert_to('houfeng-app-acl-manifest-v1', 'utf8')",
		"int8send(manifest_revision)",
		"int4send(octet_length(canonical_migration_set))",
		"int4send(octet_length(canonical_privilege_set))",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("0051 migration missing app ACL/domain contract %q", want)
		}
	}
}

func TestRecordPlatformFoundationMigrationDefinesActivationAndAdmissionProjection(t *testing.T) {
	payload, err := migrations.FS.ReadFile("0051_create_record_platform_foundation.sql")
	if err != nil {
		t.Fatalf("read 0051 record-platform migration: %v", err)
	}

	sql := strings.ToLower(string(payload))
	for _, want := range []string{
		"activation_sequence bigint",
		"activation_mutation_id text",
		"activation_plan_digest bytea",
		"activation_authorization_artifact_digest bytea",
		"activation_bundle_digest bytea",
		"trust_revision bigint",
		"trust_head_hash bytea",
		"inventory_digest bytea",
		"approval_policy_digest bytea",
		"active_adapter_policy_digest bytea",
		"active_domain_identity_set_digest bytea",
		"last_domain_identity_sequence bigint",
		"last_domain_identity_entry_hash bytea",
		"instance_kind text not null check (instance_kind in ('api', 'worker', 'recovery'))",
		"deployment_epoch bigint not null check (deployment_epoch > 0)",
		"fence_contract_version bigint not null check (fence_contract_version > 0)",
		"capability text not null check (capability ~ '^[a-z0-9_.]{1,128}$')",
		"load_balancer_admitted boolean not null default false",
		"queue_admitted boolean not null default false",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("0051 migration missing activation/admission contract %q", want)
		}
	}
}
