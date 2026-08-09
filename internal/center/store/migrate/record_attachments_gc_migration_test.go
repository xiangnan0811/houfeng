package migrate

import (
	"strings"
	"testing"
)

func TestRecordAttachmentsMigrationDefinesDurableBlobGCDeletionProtocol(t *testing.T) {
	sql := normalizedRecordAttachmentsMigrationSQL(t)
	start := strings.Index(sql, "create table if not exists public.blob_gc_deletions (")
	if start < 0 {
		t.Fatal("0053 record-attachments migration missing durable Blob GC deletion table")
	}
	end := strings.Index(sql[start:], ");")
	if end < 0 {
		t.Fatal("0053 durable Blob GC deletion table is unterminated")
	}
	tableSQL := sql[start : start+end]
	for _, want := range []string{
		"deletion_id text primary key check (deletion_id ~ '^bgd_[a-z0-9]{1,64}$')",
		"project_id text not null default 'default' check (project_id = 'default')",
		"purge_mode text not null check (purge_mode in ('ordinary', 'permanent'))",
		"blob_key text not null check (blob_key ~ '^sha256/[0-9a-f]{64}$')",
		"sha256_digest bytea not null check (octet_length(sha256_digest) = 32)",
		"check (blob_key = 'sha256/' || encode(sha256_digest, 'hex'))",
		"object_version text not null check (char_length(object_version) between 1 and 1024)",
		"size_bytes bigint not null check (size_bytes > 0)",
		"backend_kind text not null check (backend_kind in ('local', 's3'))",
		"blob_created_at timestamptz not null",
		"deletion_state text not null check (deletion_state in ('claimed', 'retry_wait', 'completed'))",
		"owner_id text not null check (owner_id ~ '^[a-z0-9_-]{1,128}$')",
		"owner_generation bigint not null check (owner_generation > 0)",
		"attempt bigint not null check (attempt > 0)",
		"lease_expires_at timestamptz not null",
		"retry_at timestamptz",
		"physical_delete_result text check (physical_delete_result is null or physical_delete_result in ('deleted', 'already_absent'))",
		"receipt_digest bytea check (receipt_digest is null or octet_length(receipt_digest) = 32)",
		"completed_at timestamptz",
		"check ((deletion_state = 'claimed' and retry_at is null and physical_delete_result is null and receipt_digest is null and completed_at is null)",
		"or (deletion_state = 'retry_wait' and retry_at is not null and physical_delete_result is null and receipt_digest is null and completed_at is null)",
		"or (deletion_state = 'completed' and retry_at is null and physical_delete_result is not null and receipt_digest is not null and completed_at is not null))",
	} {
		if !strings.Contains(tableSQL, want) {
			t.Errorf("0053 record-attachments migration missing durable Blob GC invariant %q", want)
		}
	}
	for _, want := range []string{
		"create unique index if not exists uq_blob_gc_deletions_active on public.blob_gc_deletions(blob_key, object_version) where deletion_state <> 'completed'",
		"create index if not exists idx_blob_gc_deletions_claim on public.blob_gc_deletions(deletion_state, retry_at, lease_expires_at, backend_kind, blob_created_at, deletion_id)",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0053 record-attachments migration missing durable Blob GC index %q", want)
		}
	}
	if strings.Contains(tableSQL, "foreign key (blob_key, object_version) references public.blob_objects") {
		t.Fatal("durable Blob GC deletion must survive removal of blob_objects metadata")
	}
}
