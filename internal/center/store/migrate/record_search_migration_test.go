package migrate

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"houfeng/db/migrations"
)

var recordSearchCreateTablePattern = regexp.MustCompile(`(?m)create table if not exists public\.([a-z0-9_]+)\s*\(`)
var recordSearchColumnPattern = regexp.MustCompile(`(?m)^  ([a-z][a-z0-9_]*)\s+(?:bigint|boolean|bytea|integer|jsonb|text|text\[\]|timestamptz)\b`)
var recordSearchIndexPattern = regexp.MustCompile(`create (?:unique )?index if not exists ([a-z0-9_]+)`)

func recordSearchMigrationSQL(t *testing.T) string {
	t.Helper()
	payload, err := migrations.FS.ReadFile("0056_create_record_search.sql")
	if err != nil {
		t.Fatalf("read 0056 record-search migration: %v", err)
	}
	return strings.ToLower(string(payload))
}

func normalizedRecordSearchMigrationSQL(t *testing.T) string {
	t.Helper()
	return strings.Join(strings.Fields(recordSearchMigrationSQL(t)), " ")
}

func recordSearchTableDefinition(t *testing.T, table string) string {
	t.Helper()
	sql := recordSearchMigrationSQL(t)
	start := strings.Index(sql, "create table if not exists public."+table+" (")
	if start < 0 {
		t.Fatalf("0056 record-search migration missing %s table", table)
	}
	end := strings.Index(sql[start:], ");")
	if end < 0 {
		t.Fatalf("0056 record-search %s table is unterminated", table)
	}
	return sql[start : start+end]
}

func normalizedRecordSearchTableDefinition(t *testing.T, table string) string {
	t.Helper()
	return strings.Join(strings.Fields(recordSearchTableDefinition(t, table)), " ")
}

func recordSearchTableColumns(t *testing.T, table string) []string {
	t.Helper()
	matches := recordSearchColumnPattern.FindAllStringSubmatch(recordSearchTableDefinition(t, table), -1)
	columns := make([]string, 0, len(matches))
	for _, match := range matches {
		columns = append(columns, match[1])
	}
	return columns
}

func TestRecordSearchMigrationDefinesExactOwnedTables(t *testing.T) {
	matches := recordSearchCreateTablePattern.FindAllStringSubmatch(recordSearchMigrationSQL(t), -1)
	got := make([]string, 0, len(matches))
	for _, match := range matches {
		got = append(got, match[1])
	}
	want := []string{
		"record_search_generations",
		"record_search_documents",
		"record_search_subjects",
		"record_search_rebuild_jobs",
		"record_search_purge_receipts",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("0056 record-search tables = %#v, want %#v", got, want)
	}
}

// The project ships one binary and one PostgreSQL, so a search index may only
// depend on an extension the database owner can install without a superuser and
// without a package outside core contrib.
func TestRecordSearchMigrationInstallsOnlyTheTrigramExtensionInTheInternalSchema(t *testing.T) {
	sql := normalizedRecordSearchMigrationSQL(t)
	if want := "create extension if not exists pg_trgm with schema record_platform_internal"; !strings.Contains(sql, want) {
		t.Fatalf("0056 must install the trigram extension in the internal schema with %q", want)
	}
	for _, forbidden := range []string{"zhparser", "pg_bigm", "pgroonga", "with schema public"} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("0056 installs forbidden extension surface %q", forbidden)
		}
	}
}

func TestRecordSearchMigrationReusesPlatformPrimitives(t *testing.T) {
	for _, match := range recordSearchCreateTablePattern.FindAllStringSubmatch(recordSearchMigrationSQL(t), -1) {
		for _, forbidden := range []string{"idempotency", "outbox", "_lease", "authorization"} {
			if strings.Contains(match[1], forbidden) {
				t.Fatalf("0056 duplicates a record-platform primitive with table %q", match[1])
			}
		}
	}
	for _, forbidden := range []string{
		"record_search_activities",
		"record_search_comments",
		"record_search_drafts",
		"record_activity_projection",
		"record_portability",
	} {
		if strings.Contains(recordSearchMigrationSQL(t), "create table if not exists public."+forbidden) {
			t.Fatalf("0056 contains forbidden duplicate or downstream table %q", forbidden)
		}
	}
}

// A search document is derived evidence. It may hold normalized text it can
// rebuild from authoritative rows, and nothing that would make it a second
// source of record content.
func TestRecordSearchMigrationStoresDerivedTextWithoutCopyingAuthoritativeContent(t *testing.T) {
	definition := normalizedRecordSearchTableDefinition(t, "record_search_documents")
	for _, want := range []string{
		"plain_text text not null default '' check (octet_length(plain_text) <= 65536)",
		"search_text text generated always as (lower(title || ' ' || plain_text)) stored",
		"document_digest bytea not null check (octet_length(document_digest) = 32)",
	} {
		if !strings.Contains(definition, want) {
			t.Errorf("0056 record_search_documents missing derived-text invariant %q", want)
		}
	}
	for _, forbidden := range []string{
		"body_markdown", "render_model", "render_contract_version", "draft_", "comment_",
		"attachment_blob", "evidence_payload", "payload_bytes",
	} {
		if strings.Contains(definition, forbidden) {
			t.Errorf("0056 record_search_documents copies authoritative content through %q", forbidden)
		}
	}
}

// Every row a query can return has to carry the evidence the query path needs
// to re-authorize it, so a stale generation can never widen visibility.
func TestRecordSearchMigrationBindsDocumentsToGenerationAndAuthorizationEvidence(t *testing.T) {
	definition := normalizedRecordSearchTableDefinition(t, "record_search_documents")
	for _, want := range []string{
		"primary key (generation, record_id)",
		"visibility_kind text not null check (visibility_kind in ('project', 'restricted'))",
		"visibility_digest bytea not null check (octet_length(visibility_digest) = 32)",
		"authorization_epoch bigint not null check (authorization_epoch >= 0)",
		"record_fence_epoch bigint not null check (record_fence_epoch >= 0)",
		"foreign key (generation) references public.record_search_generations(generation) on delete restrict",
		"foreign key (record_id) references public.records(record_id) on delete restrict",
	} {
		if !strings.Contains(definition, want) {
			t.Errorf("0056 record_search_documents missing authorization binding %q", want)
		}
	}
}

// A derived row may be narrower than its source but never stricter, or a record
// the platform accepts becomes one the index cannot hold. records.lock_version
// is bounded by >= 0 in 0052, so the projection has to accept the same range.
func TestRecordSearchMigrationMirrorsSourceBoundsInsteadOfTighteningThem(t *testing.T) {
	definition := normalizedRecordSearchTableDefinition(t, "record_search_documents")
	if !strings.Contains(definition, "record_lock_version bigint not null check (record_lock_version >= 0)") {
		t.Error("0056 record_lock_version must accept the same range as records.lock_version")
	}
	// 0052 couples every current_* column to current_revision_id, so requiring
	// them here is what keeps revision-less records out of the index.
	for _, want := range []string{
		"current_revision_id text not null",
		"title text not null",
		"impact_level text not null",
		"visibility_digest bytea not null",
	} {
		if !strings.Contains(definition, want) {
			t.Errorf("0056 must index only records that have a current revision, missing %q", want)
		}
	}
}

// Publishing a rebuilt index is a swap, not a window during which two indexes
// are both current or none is.
func TestRecordSearchMigrationAllowsOnePublishedAndOneBuildingGeneration(t *testing.T) {
	sql := normalizedRecordSearchMigrationSQL(t)
	for _, want := range []string{
		"create unique index if not exists uq_record_search_generations_published on public.record_search_generations ((true)) where generation_state = 'published'",
		"create unique index if not exists uq_record_search_generations_building on public.record_search_generations ((true)) where generation_state = 'building'",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0056 generation state must be exclusive through %q", want)
		}
	}
	definition := normalizedRecordSearchTableDefinition(t, "record_search_generations")
	for _, want := range []string{
		"generation_state text not null default 'building' check (generation_state in ('building', 'published', 'superseded', 'failed'))",
		"check (generation_state <> 'published' or published_at is not null)",
		"check ((generation_state = 'superseded') = (superseded_at is not null))",
		"check ((generation_state = 'failed') = (failure_reason <> ''))",
		"coverage_digest bytea check (coverage_digest is null or octet_length(coverage_digest) = 32)",
	} {
		if !strings.Contains(definition, want) {
			t.Errorf("0056 record_search_generations missing state invariant %q", want)
		}
	}
}

// Supersession is a demotion, not an erasure: the generation that served reads
// keeps the timestamp proving when it did, while a generation that never
// published must not claim one.
func TestRecordSearchMigrationKeepsPublishTimestampAcrossSupersession(t *testing.T) {
	definition := normalizedRecordSearchTableDefinition(t, "record_search_generations")
	for _, want := range []string{
		"check (published_at is null or generation_state in ('published', 'superseded'))",
		"check (superseded_at is null or published_at is null or superseded_at >= published_at)",
	} {
		if !strings.Contains(definition, want) {
			t.Errorf("0056 record_search_generations missing publish-history invariant %q", want)
		}
	}
}

func TestRecordSearchMigrationIndexesTheReviewedFilterAndSortShapes(t *testing.T) {
	matches := recordSearchIndexPattern.FindAllStringSubmatch(recordSearchMigrationSQL(t), -1)
	got := make([]string, 0, len(matches))
	for _, match := range matches {
		got = append(got, match[1])
	}
	want := []string{
		"uq_record_search_generations_published",
		"uq_record_search_generations_building",
		"idx_record_search_documents_updated",
		"idx_record_search_documents_type_updated",
		"idx_record_search_documents_status_group_updated",
		"idx_record_search_documents_owner_updated",
		"idx_record_search_documents_follow_up",
		"idx_record_search_documents_search_text",
		"idx_record_search_documents_tags",
		"idx_record_search_documents_participants",
		"idx_record_search_subjects_source",
		"idx_record_search_subjects_primary",
		"idx_record_search_rebuild_jobs_generation",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("0056 record-search indexes = %#v, want %#v", got, want)
	}
	sql := normalizedRecordSearchMigrationSQL(t)
	for _, want := range []string{
		"idx_record_search_documents_updated on public.record_search_documents(generation, lifecycle, record_updated_at desc, record_id)",
		"idx_record_search_documents_search_text on public.record_search_documents using gin (search_text record_platform_internal.gin_trgm_ops)",
		"idx_record_search_documents_tags on public.record_search_documents using gin (tags)",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0056 missing reviewed index shape %q", want)
		}
	}
}

func TestRecordSearchMigrationKeepsSubjectRegistryExactlyAsCoreDefines(t *testing.T) {
	definition := normalizedRecordSearchTableDefinition(t, "record_search_subjects")
	for _, want := range []string{
		"subject_kind text not null check (subject_kind in ('vps', 'monitoring_instance', 'target'))",
		"relation_role text not null check (relation_role in ('affected', 'context', 'evidence_source'))",
		"is_primary boolean not null",
		"primary key (generation, record_id, subject_kind, source_id, relation_role)",
		"foreign key (generation, record_id) references public.record_search_documents(generation, record_id) on delete cascade",
	} {
		if !strings.Contains(definition, want) {
			t.Errorf("0056 record_search_subjects missing registry invariant %q", want)
		}
	}
}

func TestRecordSearchMigrationResumesRebuildsWithoutContentOrUnboundedProgress(t *testing.T) {
	definition := normalizedRecordSearchTableDefinition(t, "record_search_rebuild_jobs")
	for _, want := range []string{
		"job_id text primary key check (job_id ~ '^rsj_[a-z0-9]{1,64}$')",
		"job_state text not null default 'running' check (job_state in ('running', 'completed', 'failed', 'cancelled'))",
		"resume_after_record_id text check (resume_after_record_id is null or resume_after_record_id ~ '^rec_[a-z0-9]{1,64}$')",
		"processed_count bigint not null default 0 check (processed_count >= 0)",
		"foreign key (generation) references public.record_search_generations(generation) on delete restrict",
	} {
		if !strings.Contains(definition, want) {
			t.Errorf("0056 record_search_rebuild_jobs missing resume invariant %q", want)
		}
	}
	for _, forbidden := range []string{"plain_text", "search_text", "title", "query"} {
		if strings.Contains(definition, forbidden) {
			t.Errorf("0056 record_search_rebuild_jobs retains forbidden content field %q", forbidden)
		}
	}
}

func TestRecordSearchMigrationKeepsPurgeReceiptMinimalAndImmutable(t *testing.T) {
	wantColumns := []string{
		"operation_id", "adapter_name", "removed_surface_digest", "receipt_digest",
		"removed_row_count", "verified_absent_at", "created_at",
	}
	if got := recordSearchTableColumns(t, "record_search_purge_receipts"); !reflect.DeepEqual(got, wantColumns) {
		t.Fatalf("0056 content-free search purge receipt columns = %#v, want %#v", got, wantColumns)
	}
	sql := normalizedRecordSearchMigrationSQL(t)
	for _, want := range []string{
		"adapter_name text not null default 'record_search' check (adapter_name = 'record_search')",
		"foreign key (operation_id) references public.record_purge_operations(operation_id) on delete restrict",
		"create trigger record_search_purge_receipts_reject_update before update on public.record_search_purge_receipts for each row execute function record_platform_internal.reject_immutable_mutation()",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0056 search purge receipt missing invariant %q", want)
		}
	}
}

// Permanent delete must reach the derived index through the same controlled
// SECURITY DEFINER surface every other adapter uses, and must prove the index
// holds no remaining row for the purged record.
func TestRecordSearchMigrationPurgesThroughOneControlledSurface(t *testing.T) {
	sql := normalizedRecordSearchMigrationSQL(t)
	for _, want := range []string{
		"create or replace function record_platform_internal.purge_record_search( text, text, text, text, bigint, bigint, bytea )",
		"security definer set search_path = pg_catalog",
		"delete from public.record_search_subjects where record_id = p_record_id",
		"delete from public.record_search_documents where record_id = p_record_id",
		"raise exception using errcode = '55000', message = 'search purge left owned rows'",
		"create or replace function public.record_search_purge(bytea)",
		"revoke all on function public.record_search_purge(bytea) from public",
		"revoke all on function record_platform_internal.purge_record_search(text,text,text,text,bigint,bigint,bytea) from public",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0056 controlled search purge missing %q", want)
		}
	}
	if strings.Contains(sql, "on delete cascade") &&
		!strings.Contains(normalizedRecordSearchTableDefinition(t, "record_search_subjects"), "on delete cascade") {
		t.Error("0056 may only cascade from a search document to its own subjects")
	}
}

// Retiring a rebuilt-away generation removes derived rows in bulk, so it must
// refuse to touch the generation a live query is reading.
func TestRecordSearchMigrationRetiresOnlySupersededOrFailedGenerations(t *testing.T) {
	sql := normalizedRecordSearchMigrationSQL(t)
	for _, want := range []string{
		"create or replace function record_platform_internal.retire_record_search_generation(bigint)",
		"and generation.generation_state in ('superseded', 'failed') for update",
		"raise exception using errcode = '55000', message = 'search generation is not retirable'",
		"where job.generation = p_generation and job.job_state = 'running'",
		"raise exception using errcode = '55000', message = 'search generation still has a running rebuild job'",
		"create or replace function public.record_search_retire_generation(bytea)",
		"revoke all on function public.record_search_retire_generation(bytea) from public",
		"revoke all on function record_platform_internal.retire_record_search_generation(bigint) from public",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0056 controlled generation retirement missing %q", want)
		}
	}
}

// The grantable function grammar is exactly public.name(bytea), and no app role
// has USAGE on the extension schema, so 0056 must not create a search helper the
// runtime could never be granted and must not widen the schema instead.
func TestRecordSearchMigrationAddsNoUngrantableRuntimeSurface(t *testing.T) {
	sql := normalizedRecordSearchMigrationSQL(t)
	if strings.Contains(sql, "grant usage on schema record_platform_internal") {
		t.Error("0056 must not grant app roles USAGE on the extension schema")
	}
	for _, match := range regexp.MustCompile(`create or replace function public\.([a-z0-9_]+)\(([^)]*)\)`).FindAllStringSubmatch(sql, -1) {
		if match[2] != "bytea" {
			t.Errorf("0056 public function %s(%s) cannot be granted under the public.name(bytea) ACL grammar", match[1], match[2])
		}
	}
	if strings.Contains(sql, "word_similarity") || strings.Contains(sql, "similarity(") {
		t.Error("0056 must not depend on a trigram scalar the runtime cannot reach")
	}
}
