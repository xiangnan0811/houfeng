package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"houfeng/db/migrations"
)

var recordAttachmentsCreateTablePattern = regexp.MustCompile(`(?m)create table if not exists public\.([a-z0-9_]+)\s*\(`)

func recordAttachmentsMigrationSQL(t *testing.T) string {
	t.Helper()

	payload, err := migrations.FS.ReadFile("0053_create_record_attachments.sql")
	if err != nil {
		t.Fatalf("read 0053 record-attachments migration: %v", err)
	}
	return strings.ToLower(string(payload))
}

func normalizedRecordAttachmentsMigrationSQL(t *testing.T) string {
	t.Helper()
	return strings.Join(strings.Fields(recordAttachmentsMigrationSQL(t)), " ")
}

func recordAttachmentsCheckExpressions(t *testing.T, sql string) []string {
	t.Helper()

	const prefix = "check ("
	expressions := make([]string, 0, 69)
	for offset := 0; ; {
		relativeStart := strings.Index(sql[offset:], prefix)
		if relativeStart < 0 {
			return expressions
		}
		start := offset + relativeStart
		depth := 0
		quoted := false
		closed := false
		for index := start + len("check "); index < len(sql); index++ {
			character := sql[index]
			if character == '\'' {
				if quoted && index+1 < len(sql) && sql[index+1] == '\'' {
					index++
					continue
				}
				quoted = !quoted
				continue
			}
			if quoted {
				continue
			}
			switch character {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					expressions = append(expressions, sql[start:index+1])
					offset = index + 1
					closed = true
				}
			}
			if closed {
				break
			}
		}
		if !closed {
			t.Fatalf("0053 record-attachments migration has unterminated CHECK expression at byte %d", start)
		}
	}
}

func TestRecordAttachmentsMigrationDefinesExactOwnedTables(t *testing.T) {
	matches := recordAttachmentsCreateTablePattern.FindAllStringSubmatch(recordAttachmentsMigrationSQL(t), -1)
	got := make([]string, 0, len(matches))
	for _, match := range matches {
		got = append(got, match[1])
	}
	want := []string{
		"blob_objects",
		"attachment_quota_accounts",
		"record_attachments",
		"attachment_uploads",
		"attachment_upload_parts",
		"record_revision_attachments",
		"attachment_processor_jobs",
		"content_processor_workspaces",
		"blob_gc_pins",
		"blob_gc_deletions",
		"blob_publication_intents",
		"attachment_purge_receipts",
		"content_workspace_purge_receipts",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("0053 record-attachments tables = %#v, want %#v", got, want)
	}
}

func TestRecordAttachmentsMigrationFreezesExactDDLInventory(t *testing.T) {
	sql := normalizedRecordAttachmentsMigrationSQL(t)
	generatedObjectPattern := regexp.MustCompile(`(?:create (?:or replace )?(?:(?:temporary|temp) )?(?:function|sequence)|\b(?:smallserial|serial|bigserial|serial2|serial4|serial8)\b|generated (?:always|by default) as identity|nextval\s*\()`)
	if generatedObject := generatedObjectPattern.FindString(sql); generatedObject != "" {
		t.Fatalf("0053 record-attachments migration contains forbidden generated object syntax %q", generatedObject)
	}

	indexPattern := regexp.MustCompile(`create (unique )?index if not exists ([a-z0-9_]+) `)
	indexMatches := indexPattern.FindAllStringSubmatch(sql, -1)
	gotIndexes := make([]string, 0, len(indexMatches))
	for _, match := range indexMatches {
		kind := "index:"
		if match[1] != "" {
			kind = "unique-index:"
		}
		gotIndexes = append(gotIndexes, kind+match[2])
	}
	wantIndexes := []string{
		"index:idx_record_attachments_draft_state",
		"index:idx_record_attachments_record_state",
		"index:idx_record_attachments_blob",
		"index:idx_record_attachments_preview_blob",
		"index:idx_record_attachments_copied_from",
		"index:idx_attachment_uploads_expiry",
		"index:idx_attachment_uploads_draft_state",
		"index:idx_attachment_uploads_temporary_cleanup",
		"index:idx_record_revision_attachments_record",
		"index:idx_record_revision_attachments_attachment",
		"index:idx_attachment_processor_jobs_claim",
		"index:idx_content_processor_workspaces_expiry",
		"index:idx_blob_gc_pins_expiry",
		"index:idx_blob_gc_pins_blob",
		"unique-index:uq_blob_gc_deletions_active",
		"index:idx_blob_gc_deletions_claim",
		"unique-index:uq_blob_publication_intents_active_key",
		"index:idx_blob_publication_intents_claim",
	}
	if !reflect.DeepEqual(gotIndexes, wantIndexes) {
		t.Fatalf("0053 record-attachments indexes = %#v, want %#v", gotIndexes, wantIndexes)
	}

	triggerPattern := regexp.MustCompile(`create trigger ([a-z0-9_]+) before update(?: of [a-z0-9_, ]+)? on public\.[a-z0-9_]+ `)
	triggerMatches := triggerPattern.FindAllStringSubmatch(sql, -1)
	gotTriggers := make([]string, 0, len(triggerMatches))
	for _, match := range triggerMatches {
		gotTriggers = append(gotTriggers, match[1])
	}
	wantTriggers := []string{
		"blob_objects_reject_update",
		"record_attachments_origin_draft_reject_update",
		"attachment_upload_parts_reject_update",
		"record_revision_attachments_reject_update",
		"blob_gc_pins_reject_update",
		"attachment_purge_receipts_reject_update",
		"content_workspace_purge_receipts_reject_update",
	}
	if !reflect.DeepEqual(gotTriggers, wantTriggers) {
		t.Fatalf("0053 record-attachments triggers = %#v, want %#v", gotTriggers, wantTriggers)
	}

	checkExpressions := recordAttachmentsCheckExpressions(t, sql)
	if got, want := len(checkExpressions), 125; got != want {
		t.Fatalf("0053 record-attachments check inventory count = %d, want %d", got, want)
	}
	checkDigest := sha256.Sum256([]byte(strings.Join(checkExpressions, "\n")))
	if got, want := hex.EncodeToString(checkDigest[:]), "32d02856a18a16ba1ae399442eea46a638242cd7c552b3eea9ef548ab3def5c0"; got != want {
		t.Fatalf("0053 record-attachments canonical CHECK inventory digest = %q, want %q", got, want)
	}
	if got, want := strings.Count(sql, "foreign key ("), 12; got != want {
		t.Fatalf("0053 record-attachments foreign-key inventory count = %d, want %d", got, want)
	}
	if got, want := strings.Count(sql, "on delete restrict"), 12; got != want {
		t.Fatalf("0053 record-attachments restrict-delete inventory count = %d, want %d", got, want)
	}

	wantForeignKeys := map[string]int{
		"foreign key (record_id) references public.records(record_id) on delete restrict":                                                                                         1,
		"foreign key (draft_id) references public.record_drafts(draft_id) on delete restrict":                                                                                     1,
		"foreign key (blob_key, blob_object_version) references public.blob_objects(blob_key, object_version) on delete restrict":                                                 2,
		"foreign key (preview_blob_key, preview_blob_object_version, preview_size_bytes) references public.blob_objects(blob_key, object_version, size_bytes) on delete restrict": 1,
		"foreign key (attachment_id, origin_draft_id) references public.record_attachments(attachment_id, origin_draft_id) on delete restrict":                                    1,
		"foreign key (upload_id) references public.attachment_uploads(upload_id) on delete restrict":                                                                              1,
		"foreign key (record_id, revision_id) references public.record_revisions(record_id, revision_id) on delete restrict":                                                      1,
		"foreign key (record_id, attachment_id) references public.record_attachments(record_id, attachment_id) on delete restrict deferrable initially deferred":                  1,
		"foreign key (upload_id, attachment_id) references public.attachment_uploads(upload_id, attachment_id) on delete restrict":                                                1,
		"foreign key (processor_job_id) references public.attachment_processor_jobs(processor_job_id) on delete restrict":                                                         1,
		"foreign key (operation_id) references public.record_purge_operations(operation_id) on delete restrict":                                                                   1,
	}
	for foreignKey, wantCount := range wantForeignKeys {
		if got := strings.Count(sql, foreignKey); got != wantCount {
			t.Errorf("0053 record-attachments foreign key %q count = %d, want %d", foreignKey, got, wantCount)
		}
	}
	if strings.Contains(sql, "foreign key (copied_from_attachment_id)") {
		t.Fatal("0053 copied-attachment lineage must not block deletion of the purgeable source attachment")
	}
	if strings.Contains(sql, "foreign key (workspace_id) references public.content_processor_workspaces") {
		t.Fatal("0053 workspace purge receipt must survive terminal workspace deletion")
	}
}

func TestRecordAttachmentsMigrationEnforcesBlobAndLogicalOwnership(t *testing.T) {
	sql := normalizedRecordAttachmentsMigrationSQL(t)
	blobStart := strings.Index(sql, "create table if not exists public.blob_objects (")
	if blobStart < 0 {
		t.Fatal("0053 record-attachments migration missing Blob object table")
	}
	blobEnd := strings.Index(sql[blobStart:], ");")
	if blobEnd < 0 {
		t.Fatal("0053 Blob object table is unterminated")
	}
	blobSQL := sql[blobStart : blobStart+blobEnd]
	if !strings.Contains(blobSQL, "size_bytes bigint not null check (size_bytes > 0)") {
		t.Fatal("0053 Blob object size must be positive")
	}
	for _, want := range []string{
		"blob_key text primary key check (blob_key ~ '^sha256/[0-9a-f]{64}$')",
		"sha256_digest bytea not null unique check (octet_length(sha256_digest) = 32)",
		"check (blob_key = 'sha256/' || encode(sha256_digest, 'hex'))",
		"object_version text not null check (char_length(object_version) between 1 and 1024)",
		"unique (blob_key, object_version)",
		"attachment_id text primary key check (attachment_id ~ '^att_[a-z0-9]{1,64}$')",
		"record_id text",
		"draft_id text",
		"origin_draft_id text check (origin_draft_id is null or origin_draft_id ~ '^rdf_[a-z0-9]{1,64}$')",
		"copied_from_attachment_id text",
		"check ((record_id is null) <> (draft_id is null))",
		"check (draft_id is null or origin_draft_id is null or draft_id = origin_draft_id)",
		"check ((blob_key is null) = (blob_object_version is null))",
		"check ((attachment_state = 'available') = (blob_key is not null))",
		"foreign key (record_id) references public.records(record_id) on delete restrict",
		"foreign key (draft_id) references public.record_drafts(draft_id) on delete restrict",
		"foreign key (blob_key, blob_object_version) references public.blob_objects(blob_key, object_version) on delete restrict",
		"unique (record_id, attachment_id)",
		"unique (attachment_id, origin_draft_id)",
		"create trigger record_attachments_origin_draft_reject_update before update of origin_draft_id on public.record_attachments for each row execute function record_platform_internal.reject_immutable_mutation()",
		"create index if not exists idx_record_attachments_blob on public.record_attachments(blob_key, blob_object_version, attachment_id)",
		"create index if not exists idx_record_attachments_copied_from on public.record_attachments(copied_from_attachment_id, attachment_id)",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0053 record-attachments migration missing ownership invariant %q", want)
		}
	}
	if strings.Contains(sql, "on delete cascade") {
		t.Fatal("0053 record-attachments migration must keep logical and physical cleanup explicit")
	}
}

func TestRecordAttachmentsMigrationEnforcesUploadStateAndQuotaAccounting(t *testing.T) {
	sql := normalizedRecordAttachmentsMigrationSQL(t)
	for _, want := range []string{
		"project_id text primary key default 'default' check (project_id = 'default')",
		"logical_bytes bigint not null default 0 check (logical_bytes >= 0)",
		"reserved_bytes bigint not null default 0 check (reserved_bytes >= 0)",
		"physical_bytes bigint not null default 0 check (physical_bytes >= 0)",
		"quota_version bigint not null default 0 check (quota_version >= 0)",
		"upload_id text primary key check (upload_id ~ '^aup_[a-z0-9]{1,64}$')",
		"origin_draft_id text not null check (origin_draft_id ~ '^rdf_[a-z0-9]{1,64}$')",
		"upload_state text not null default 'created' check (upload_state in ('created', 'uploading', 'quarantined', 'available', 'rejected', 'expired'))",
		"declared_size_bytes bigint not null check (declared_size_bytes > 0)",
		"reserved_size_bytes bigint not null check (reserved_size_bytes > 0)",
		"actual_size_bytes bigint check (actual_size_bytes is null or actual_size_bytes >= 0)",
		"actual_sha256_digest bytea check (actual_sha256_digest is null or octet_length(actual_sha256_digest) = 32)",
		"temporary_object_key text check (temporary_object_key is null or char_length(temporary_object_key) between 1 and 1024)",
		"temporary_object_version text check (temporary_object_version is null or char_length(temporary_object_version) between 1 and 1024)",
		"temporary_object_cleanup_retry_at timestamptz",
		"temporary_object_deleted_at timestamptz",
		"check ((actual_size_bytes is null) = (actual_sha256_digest is null))",
		"check (temporary_object_version is null or temporary_object_key is not null)",
		"check (temporary_object_cleanup_retry_at is null or temporary_object_key is not null)",
		"check (temporary_object_deleted_at is null or (temporary_object_key is not null and temporary_object_version is not null))",
		"check (expires_at > created_at)",
		"unique (attachment_id)",
		"unique (upload_id, attachment_id)",
		"foreign key (attachment_id, origin_draft_id) references public.record_attachments(attachment_id, origin_draft_id) on delete restrict",
		"create index if not exists idx_attachment_uploads_expiry on public.attachment_uploads(upload_state, expires_at, upload_id)",
		"create index if not exists idx_attachment_uploads_draft_state on public.attachment_uploads(origin_draft_id, upload_state, upload_id)",
		"create index if not exists idx_attachment_uploads_temporary_cleanup on public.attachment_uploads(temporary_object_cleanup_retry_at, expires_at, upload_id) where transport_kind = 's3' and temporary_object_key is not null and temporary_object_deleted_at is null",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0053 record-attachments migration missing upload/quota invariant %q", want)
		}
	}
	if strings.Contains(sql, "check ((temporary_object_key is null) = (temporary_object_version is null))") {
		t.Fatal("0053 upload state must allow a persisted temporary key before its S3 version is known")
	}
}

func TestRecordAttachmentsMigrationEnforcesRevisionReferencesAndPins(t *testing.T) {
	sql := normalizedRecordAttachmentsMigrationSQL(t)
	for _, want := range []string{
		"primary key (revision_id, ordinal)",
		"unique (revision_id, attachment_id)",
		"foreign key (record_id, revision_id) references public.record_revisions(record_id, revision_id) on delete restrict",
		"foreign key (record_id, attachment_id) references public.record_attachments(record_id, attachment_id) on delete restrict deferrable initially deferred",
		"pin_id text primary key check (pin_id ~ '^bgp_[a-z0-9]{1,64}$')",
		"pin_owner_kind text not null check (pin_owner_kind in ('backup_manifest', 'restore_attempt', 'import_plan', 'revision_transaction'))",
		"pin_owner_id text not null check (pin_owner_id ~ '^[a-z0-9_-]{1,128}$')",
		"unique (pin_owner_kind, pin_owner_id, blob_key, blob_object_version)",
		"foreign key (blob_key, blob_object_version) references public.blob_objects(blob_key, object_version) on delete restrict",
		"create index if not exists idx_blob_gc_pins_expiry on public.blob_gc_pins(expires_at, pin_id)",
		"create index if not exists idx_record_revision_attachments_attachment on public.record_revision_attachments(attachment_id, record_id, revision_id, ordinal)",
		"create index if not exists idx_blob_gc_pins_blob on public.blob_gc_pins(blob_key, blob_object_version, expires_at, pin_id)",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0053 record-attachments migration missing revision/pin invariant %q", want)
		}
	}
}

func TestRecordAttachmentsMigrationEnforcesProcessorWorkspaceLifecycle(t *testing.T) {
	sql := normalizedRecordAttachmentsMigrationSQL(t)
	for _, want := range []string{
		"processor_job_id text primary key check (processor_job_id ~ '^apj_[a-z0-9]{1,64}$')",
		"processor_state text not null default 'queued' check (processor_state in ('queued', 'claimed', 'retry_wait', 'succeeded', 'rejected', 'expired'))",
		"attempt bigint not null default 0 check (attempt >= 0)",
		"max_attempts bigint not null check (max_attempts > 0)",
		"check ((processor_state = 'retry_wait') = (retry_at is not null))",
		"workspace_id text primary key check (workspace_id ~ '^cpw_[a-z0-9]{1,64}$')",
		"workspace_state text not null default 'registered' check (workspace_state in ('registered', 'materialized', 'purging', 'purged'))",
		"unique (processor_job_id, attempt)",
		"foreign key (processor_job_id) references public.attachment_processor_jobs(processor_job_id) on delete restrict",
		"foreign key (upload_id, attachment_id) references public.attachment_uploads(upload_id, attachment_id) on delete restrict",
		"create index if not exists idx_attachment_processor_jobs_claim on public.attachment_processor_jobs(processor_state, retry_at, lease_expires_at, processor_job_id)",
		"create index if not exists idx_content_processor_workspaces_expiry on public.content_processor_workspaces(workspace_state, expires_at, workspace_id)",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0053 record-attachments migration missing processor/workspace invariant %q", want)
		}
	}
}

func TestRecordAttachmentsMigrationDefinesPreviewBlobContract(t *testing.T) {
	sql := normalizedRecordAttachmentsMigrationSQL(t)
	for _, want := range []string{
		"blob_key text, blob_object_version text, preview_blob_key text, preview_blob_object_version text, preview_media_type text, preview_size_bytes bigint",
		"unique (blob_key, object_version, size_bytes)",
		"check ((preview_blob_key is null and preview_blob_object_version is null and preview_media_type is null and preview_size_bytes is null) or (preview_blob_key is not null and preview_blob_object_version is not null and preview_media_type is not null and preview_size_bytes is not null))",
		"check (preview_media_type is null or (char_length(preview_media_type) between 1 and 255 and preview_media_type in ('image/png', 'text/plain; charset=utf-8')))",
		"check (preview_size_bytes is null or preview_size_bytes > 0)",
		"check (preview_blob_key is null or attachment_state = 'available')",
		"foreign key (preview_blob_key, preview_blob_object_version, preview_size_bytes) references public.blob_objects(blob_key, object_version, size_bytes) on delete restrict",
		"create index if not exists idx_record_attachments_preview_blob on public.record_attachments(preview_blob_key, preview_blob_object_version, attachment_id)",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0053 record-attachments migration missing managed preview contract %q", want)
		}
	}
	if got := strings.Count(sql, "foreign key (blob_key, blob_object_version) references public.blob_objects(blob_key, object_version) on delete restrict"); got != 2 {
		t.Fatalf("0053 original Blob foreign-key count = %d, want unchanged count 2", got)
	}
}

func TestRecordAttachmentsMigrationDefinesClosedProcessorResultContract(t *testing.T) {
	sql := normalizedRecordAttachmentsMigrationSQL(t)
	for _, want := range []string{
		"result_code text check (result_code is null or result_code in ('clean', 'malware', 'unsafe_content', 'scanner_unavailable', 'timeout', 'processing_error'))",
		"result_digest bytea check (result_digest is null or octet_length(result_digest) = 32)",
		"result_owner_id text not null default '' check (result_owner_id = '' or result_owner_id ~ '^[a-z0-9_-]{1,128}$')",
		"result_lease_expires_at timestamptz",
		"check (((processor_state in ('queued', 'claimed')) and result_owner_id = '' and result_lease_expires_at is null) or ((processor_state in ('retry_wait', 'succeeded', 'rejected', 'expired')) and result_owner_id <> '' and result_lease_expires_at is not null))",
		"check (((processor_state in ('queued', 'claimed')) and result_code is null and result_digest is null) or (result_code is not null and result_digest is not null and ((processor_state = 'retry_wait' and result_code in ('scanner_unavailable', 'timeout', 'processing_error')) or (processor_state = 'succeeded' and result_code = 'clean') or (processor_state = 'rejected' and result_code in ('malware', 'unsafe_content')) or (processor_state = 'expired' and result_code in ('scanner_unavailable', 'timeout', 'processing_error')))))",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0053 record-attachments migration missing processor result contract %q", want)
		}
	}

	processorStart := strings.Index(sql, "create table if not exists public.attachment_processor_jobs (")
	if processorStart < 0 {
		t.Fatal("0053 record-attachments migration missing processor job table")
	}
	processorEnd := strings.Index(sql[processorStart:], ");")
	if processorEnd < 0 {
		t.Fatal("0053 processor job table is unterminated")
	}
	processorSQL := sql[processorStart : processorStart+processorEnd]
	for _, forbidden := range []string{
		"result_message", "result_detail", "result_output", "scanner_reply",
		"raw_error", "error_text", "stdout", "stderr",
	} {
		if strings.Contains(processorSQL, forbidden) {
			t.Errorf("0053 processor job table persists forbidden free-text output %q", forbidden)
		}
	}
}

func TestRecordAttachmentsMigrationKeepsPurgeReceiptsContentFreeAndImmutable(t *testing.T) {
	sql := normalizedRecordAttachmentsMigrationSQL(t)
	for _, table := range []string{
		"blob_objects",
		"attachment_upload_parts",
		"record_revision_attachments",
		"blob_gc_pins",
		"attachment_purge_receipts",
		"content_workspace_purge_receipts",
	} {
		want := "create trigger " + table + "_reject_update before update on public." + table + " for each row execute function record_platform_internal.reject_immutable_mutation()"
		if !strings.Contains(sql, want) {
			t.Errorf("0053 record-attachments migration missing immutable update trigger %q", table)
		}
	}

	receiptStart := strings.Index(sql, "create table if not exists public.attachment_purge_receipts (")
	if receiptStart < 0 {
		t.Fatal("0053 record-attachments migration missing attachment purge receipt table")
	}
	receiptEnd := strings.Index(sql[receiptStart:], ");")
	if receiptEnd < 0 {
		t.Fatal("0053 attachment purge receipt table is unterminated")
	}
	receiptSQL := sql[receiptStart : receiptStart+receiptEnd]
	for _, forbidden := range []string{"project_id", "record_id", "attachment_id", "blob_key"} {
		if strings.Contains(receiptSQL, forbidden) {
			t.Errorf("0053 content-free attachment purge receipt persists identity field %q", forbidden)
		}
	}
	for _, want := range []string{
		"surface_kind text not null check (surface_kind in ('logical_attachment', 'upload_part', 'blob_object'))",
		"object_version_digest bytea not null check (octet_length(object_version_digest) = 32)",
		"adapter_name text not null default 'record_attachments' check (adapter_name = 'record_attachments')",
		"removed_surface_digest bytea not null check (octet_length(removed_surface_digest) = 32)",
		"receipt_digest bytea not null check (octet_length(receipt_digest) = 32)",
		"removed_row_count bigint not null check (removed_row_count >= 0)",
		"primary key (operation_id, surface_kind, object_version_digest)",
		"foreign key (operation_id) references public.record_purge_operations(operation_id) on delete restrict",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0053 record-attachments migration missing purge receipt invariant %q", want)
		}
	}
}
