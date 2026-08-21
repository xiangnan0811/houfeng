package migrate

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"houfeng/db/migrations"
)

var recordPortabilityCreateTablePattern = regexp.MustCompile(`(?m)create table if not exists public\.([a-z0-9_]+)\s*\(`)

func recordPortabilityMigrationSQL(t *testing.T) string {
	t.Helper()
	payload, err := migrations.FS.ReadFile("0058_create_record_portability.sql")
	if err != nil {
		t.Fatalf("read 0058 record-portability migration: %v", err)
	}
	return strings.ToLower(string(payload))
}

func recordPortabilityTableDefinition(t *testing.T, table string) string {
	t.Helper()
	sql := recordPortabilityMigrationSQL(t)
	start := strings.Index(sql, "create table if not exists public."+table+" (")
	if start < 0 {
		t.Fatalf("0058 record-portability migration missing %s table", table)
	}
	end := strings.Index(sql[start:], ");")
	if end < 0 {
		t.Fatalf("0058 record-portability %s table is unterminated", table)
	}
	return sql[start : start+end]
}

func TestRecordPortabilityMigrationDefinesExactOwnedTables(t *testing.T) {
	matches := recordPortabilityCreateTablePattern.FindAllStringSubmatch(recordPortabilityMigrationSQL(t), -1)
	got := make([]string, 0, len(matches))
	for _, match := range matches {
		got = append(got, match[1])
	}
	want := []string{
		"record_export_jobs",
		"record_export_artifacts",
		"record_import_jobs",
		"record_import_plans",
		"record_import_artifacts",
		"record_import_entity_mappings",
		"record_origins",
		"record_origin_tombstones",
		"record_portability_purge_receipts",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("0058 record-portability tables = %#v, want %#v", got, want)
	}
}

func TestRecordPortabilityMigrationDoesNotDuplicateMembershipOrContractTables(t *testing.T) {
	sql := recordPortabilityMigrationSQL(t)
	for _, forbidden := range []string{
		"deployment_membership",
		"deployment_contract_state",
		"source_deletion_tombstones",
		"experience_logs",
	} {
		if strings.Contains(sql, "create table if not exists public."+forbidden) {
			t.Fatalf("0058 defines forbidden duplicate table %q", forbidden)
		}
	}
}

func TestRecordPortabilityMigrationExportJobsHaveCASIdempotencyAndTTL(t *testing.T) {
	definition := strings.Join(strings.Fields(recordPortabilityTableDefinition(t, "record_export_jobs")), " ")
	for _, want := range []string{
		"export_job_id text primary key",
		"idempotency_key text not null",
		"lock_version bigint not null default 1",
		"job_state text not null",
		"failure_code text not null default ''",
		"expires_at timestamptz not null",
		"unique (project_id, actor_id, idempotency_key)",
		"check (expires_at > created_at)",
		"check (lock_version > 0)",
	} {
		if !strings.Contains(definition, want) {
			t.Fatalf("record_export_jobs must contain %q\ngot: %s", want, definition)
		}
	}
	if strings.Contains(definition, "error_message") || strings.Contains(definition, "error_text") {
		t.Fatal("export jobs must not store free-text errors")
	}
}

func TestRecordPortabilityMigrationArtifactsUseClosedContentAllowlistAndTTL(t *testing.T) {
	definition := strings.Join(strings.Fields(recordPortabilityTableDefinition(t, "record_export_artifacts")), " ")
	for _, want := range []string{
		"content_type text not null",
		"check (content_type in ('text/markdown', 'application/json', 'application/zip', 'application/pdf'))",
		"artifact_kind text not null",
		"check (artifact_kind in ('markdown', 'comparison_json', 'evidence_json', 'archive', 'pdf'))",
		"expires_at timestamptz not null",
		"sha256 bytea not null check (octet_length(sha256) = 32)",
	} {
		if !strings.Contains(definition, want) {
			t.Fatalf("record_export_artifacts must contain %q\ngot: %s", want, definition)
		}
	}
	if strings.Contains(definition, "filename") || strings.Contains(definition, "original_name") {
		t.Fatal("export artifacts must not persist a display filename")
	}
}

func TestRecordPortabilityMigrationOriginsAndTombstonesHoldNoContent(t *testing.T) {
	for _, table := range []string{"record_origins", "record_origin_tombstones"} {
		definition := recordPortabilityTableDefinition(t, table)
		for _, forbidden := range []string{
			"title", "markdown", "filename", "evidence_summary", "error_message", "body", "display_name",
		} {
			if strings.Contains(definition, forbidden) {
				t.Fatalf("%s carries forbidden content column %q", table, forbidden)
			}
		}
		if !strings.Contains(definition, "origin_digest") {
			t.Fatalf("%s must identify the origin by digest", table)
		}
	}
}

func TestRecordPortabilityMigrationReceiptsAreOperationScopedAppendOnlyProofs(t *testing.T) {
	definition := strings.Join(strings.Fields(recordPortabilityTableDefinition(t, "record_portability_purge_receipts")), " ")
	for _, want := range []string{
		"operation_id text primary key",
		"adapter_name text not null default 'record_portability'",
		"removed_surface_digest bytea not null",
		"receipt_digest bytea not null",
		"removed_row_count bigint not null",
		"verified_absent_at timestamptz not null",
		"references public.record_purge_operations(operation_id)",
	} {
		if !strings.Contains(definition, want) {
			t.Fatalf("record_portability_purge_receipts must contain %q\ngot: %s", want, definition)
		}
	}
	if strings.Contains(definition, "receipt_id") || strings.Contains(definition, "surface_name") {
		t.Fatal("portability purge receipts must be operation-scoped, not per-surface display rows")
	}
}

func TestRecordPortabilityMigrationPurgesThroughOneControlledSurface(t *testing.T) {
	sql := strings.Join(strings.Fields(recordPortabilityMigrationSQL(t)), " ")
	for _, want := range []string{
		"create or replace function record_platform_internal.purge_record_portability( text, text, text, text, bigint, bigint, bytea )",
		"security definer set search_path = pg_catalog",
		"delete from public.record_export_artifacts",
		"delete from public.record_export_jobs",
		"insert into public.record_origin_tombstones",
		"delete from public.record_origins",
		"raise exception using errcode = '55000', message = 'portability purge left owned rows'",
		"create or replace function public.record_portability_purge(bytea)",
		"revoke all on function public.record_portability_purge(bytea) from public",
		"revoke all on function record_platform_internal.purge_record_portability(text,text,text,text,bigint,bigint,bytea) from public",
		"create trigger record_portability_purge_receipts_reject_update",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0058 controlled portability purge missing %q", want)
		}
	}
	if strings.Contains(sql, "delete from public.record_origin_tombstones") {
		t.Error("0058 must not delete origin tombstones during a record purge")
	}
}

func TestRecordPortabilityMigrationImportTablesExistForLaterApply(t *testing.T) {
	for _, table := range []string{
		"record_import_jobs", "record_import_plans", "record_import_artifacts", "record_import_entity_mappings",
	} {
		definition := strings.Join(strings.Fields(recordPortabilityTableDefinition(t, table)), " ")
		if !strings.Contains(definition, "expires_at timestamptz not null") && table != "record_import_entity_mappings" {
			t.Fatalf("%s must carry a TTL unless it is a mapping row", table)
		}
		if strings.Contains(definition, "error_message") {
			t.Fatalf("%s must not store free-text errors", table)
		}
	}
	artifact := strings.Join(strings.Fields(recordPortabilityTableDefinition(t, "record_import_artifacts")), " ")
	if !strings.Contains(artifact, "object_version_id text not null") {
		t.Fatal("record_import_artifacts must persist the blob object version for apply after restart")
	}
	mapping := strings.Join(strings.Fields(recordPortabilityTableDefinition(t, "record_import_entity_mappings")), " ")
	if !strings.Contains(mapping, "source_id text not null") {
		t.Fatal("record_import_entity_mappings must persist the archive source id for remaps")
	}
}
