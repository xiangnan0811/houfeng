package migrate

import (
	"regexp"
	"strings"
	"testing"

	"houfeng/db/migrations"
)

var forbiddenRecordPlatformInternalBlanketFunctionPublicRevoke = regexp.MustCompile(`(?i)\brevoke\s+(?:execute|all(?:\s+privileges)?)\s+on\s+all\s+(?:functions|routines)\s+in\s+schema\s+record_platform_internal\s+from\s+public\b`)

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
	if forbiddenRecordPlatformInternalBlanketFunctionPublicRevoke.MatchString(sql) {
		t.Fatal("0051 migration must not blanket-revoke PG16 pgcrypto extension members")
	}
	for _, identity := range []string{
		"record_platform_internal.reject_immutable_mutation()",
		"record_platform_internal.record_platform_projection_read_bytes_v1(bytea, integer, integer)",
		"record_platform_internal.record_platform_projection_read_uint64_v1(bytea, integer)",
		"record_platform_internal.record_platform_projection_read_token_v1(bytea, integer, text)",
		"record_platform_internal.record_platform_projection_read_profile_v1(bytea, integer)",
		"record_platform_internal.record_platform_projection_validate_header_v1(bytea, integer, integer, integer)",
		"record_platform_internal.record_platform_projection_cas_receipt_v1(bytea)",
		"public.record_platform_cas_contract_activation_projection(bytea)",
		"public.record_platform_cas_domain_rotation_projection(bytea)",
		"record_platform_internal.reject_acl_manifest_revision_mutation()",
	} {
		if !strings.Contains(sql, "revoke all on function "+identity+" from public") {
			t.Fatalf("0051 migration must explicitly revoke PUBLIC from migrator-owned function %q", identity)
		}
	}
}

func TestRecordPlatformFoundationMigrationRejectsEquivalentBlanketPGCryptoFunctionRevokes(t *testing.T) {
	tests := []struct {
		name      string
		statement string
	}{
		{
			name:      "execute all functions",
			statement: "revoke execute on all functions in schema record_platform_internal from public;",
		},
		{
			name:      "execute all routines with whitespace",
			statement: "REVOKE\n  EXECUTE ON ALL ROUTINES IN SCHEMA\n  record_platform_internal FROM PUBLIC;",
		},
		{
			name:      "all all functions",
			statement: "revoke all on all functions in schema record_platform_internal from public;",
		},
		{
			name:      "all all routines",
			statement: "revoke all on all routines in schema record_platform_internal from public;",
		},
		{
			name:      "all privileges all functions",
			statement: "revoke all privileges on all functions in schema record_platform_internal from public;",
		},
		{
			name:      "all privileges all routines",
			statement: "revoke all privileges on all routines in schema record_platform_internal from public;",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !forbiddenRecordPlatformInternalBlanketFunctionPublicRevoke.MatchString(tt.statement) {
				t.Fatalf("forbidden blanket revoke detector does not match %q", tt.statement)
			}
		})
	}
	for _, statement := range []string{
		"revoke all on function record_platform_internal.reject_immutable_mutation() from public;",
		"revoke all on schema record_platform_internal from public;",
		"revoke all on all functions in schema public from public;",
	} {
		if forbiddenRecordPlatformInternalBlanketFunctionPublicRevoke.MatchString(statement) {
			t.Fatalf("forbidden blanket revoke detector unexpectedly matches %q", statement)
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
		"migrator_catalog_role text not null",
		"constraint app_acl_manifest_digest_matches check",
		"convert_to('houfeng-app-acl-manifest-v1', 'utf8')",
		"int8send(manifest_revision)",
		"int4send(octet_length(migrator_catalog_role))",
		"convert_to(migrator_catalog_role, 'utf8')",
		"int4send(octet_length(canonical_migration_set))",
		"int4send(octet_length(canonical_privilege_set))",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("0051 migration missing app ACL/domain contract %q", want)
		}
	}
	digestConstraint := sql[strings.Index(sql, "constraint app_acl_manifest_digest_matches check"):]
	for _, ordered := range [][2]string{
		{"int8send(manifest_revision)", "int4send(octet_length(migrator_catalog_role))"},
		{"convert_to(migrator_catalog_role, 'utf8')", "previous_manifest_digest"},
		{"previous_manifest_digest", "int4send(octet_length(canonical_migration_set))"},
	} {
		if strings.Index(digestConstraint, ordered[0]) >= strings.Index(digestConstraint, ordered[1]) {
			t.Fatalf("0051 manifest digest field order must keep %q before %q", ordered[0], ordered[1])
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

func TestRecordPlatformFoundationMigrationDefinesProjectorCASFunctions(t *testing.T) {
	payload, err := migrations.FS.ReadFile("0051_create_record_platform_foundation.sql")
	if err != nil {
		t.Fatalf("read 0051 record-platform migration: %v", err)
	}

	sql := strings.ToLower(string(payload))
	for _, functionName := range []string{
		"record_platform_cas_contract_activation_projection",
		"record_platform_cas_domain_rotation_projection",
	} {
		createPrefix := "create or replace function public." + functionName + "(bytea)"
		if strings.Count(sql, createPrefix) != 1 {
			t.Fatalf("0051 migration must define exactly one %q function", createPrefix)
		}
		revoke := "revoke all on function public." + functionName + "(bytea) from public"
		if !strings.Contains(sql, revoke) {
			t.Fatalf("0051 migration missing public EXECUTE revoke %q", revoke)
		}

		start := strings.Index(sql, createPrefix)
		end := strings.Index(sql[start:], revoke)
		if end < 0 {
			t.Fatalf("0051 migration could not isolate %q definition", functionName)
		}
		definition := sql[start : start+end]
		for _, want := range []string{
			"security definer",
			"set search_path = pg_catalog",
			"public.deployment_contract_state",
			"for update",
		} {
			if !strings.Contains(definition, want) {
				t.Fatalf("%q definition missing %q", functionName, want)
			}
		}
	}

	for _, want := range []string{
		"houfeng-app-projection-command-v1",
		"houfeng-app-projection-cas-receipt-v1",
		"record_platform_internal",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("0051 migration missing projector CAS contract fragment %q", want)
		}
	}
}
