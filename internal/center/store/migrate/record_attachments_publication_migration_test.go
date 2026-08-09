package migrate

import (
	"strings"
	"testing"
)

func TestRecordAttachmentsMigrationDefinesDurableBlobPublicationIntentProtocol(t *testing.T) {
	sql := normalizedRecordAttachmentsMigrationSQL(t)
	start := strings.Index(sql, "create table if not exists public.blob_publication_intents (")
	if start < 0 {
		t.Fatal("0053 record-attachments migration missing Blob publication intent table")
	}
	end := strings.Index(sql[start:], ");")
	if end < 0 {
		t.Fatal("0053 Blob publication intent table is unterminated")
	}
	tableSQL := sql[start : start+end]
	for _, want := range []string{
		"publication_id text primary key check (publication_id ~ '^bpi_[a-z0-9]{1,64}$')",
		"project_id text not null default 'default' check (project_id = 'default')",
		"owner_kind text not null check (owner_kind in ('upload', 'processor_preview'))",
		"owner_id text not null",
		"owner_generation bigint not null check (owner_generation > 0)",
		"check ((owner_kind = 'upload' and owner_id ~ '^aup_[a-z0-9]{1,64}$' and owner_generation = 1) or (owner_kind = 'processor_preview' and owner_id ~ '^apj_[a-z0-9]{1,64}$'))",
		"blob_key text not null check (blob_key ~ '^sha256/[0-9a-f]{64}$')",
		"sha256_digest bytea not null check (octet_length(sha256_digest) = 32)",
		"check (blob_key = 'sha256/' || encode(sha256_digest, 'hex'))",
		"size_bytes bigint not null check (size_bytes > 0)",
		"backend_kind text not null check (backend_kind in ('local', 's3'))",
		"object_version text check (object_version is null or char_length(object_version) between 1 and 1024)",
		"publication_state text not null check (publication_state in ('prepared', 'published', 'cleanup_claimed', 'retry_wait', 'completed'))",
		"publish_expires_at timestamptz not null",
		"cleanup_owner_id text not null default '' check (cleanup_owner_id = '' or cleanup_owner_id ~ '^[a-z0-9_-]{1,128}$')",
		"cleanup_generation bigint not null default 0 check (cleanup_generation >= 0)",
		"attempt bigint not null default 0 check (attempt >= 0)",
		"cleanup_lease_expires_at timestamptz",
		"retry_at timestamptz",
		"completion_outcome text check (completion_outcome is null or completion_outcome in ('consumed', 'deleted', 'already_absent'))",
		"receipt_digest bytea check (receipt_digest is null or octet_length(receipt_digest) = 32)",
		"completed_at timestamptz",
		"check ((cleanup_owner_id = '' and cleanup_generation = 0 and attempt = 0 and cleanup_lease_expires_at is null) or (cleanup_owner_id <> '' and cleanup_generation > 0 and attempt > 0 and cleanup_lease_expires_at is not null))",
		"check ((publication_state in ('prepared', 'published') and cleanup_owner_id = '') or (publication_state in ('cleanup_claimed', 'retry_wait') and cleanup_owner_id <> '') or publication_state = 'completed')",
		"check ((publication_state = 'prepared' and object_version is null) or (publication_state = 'published' and object_version is not null) or publication_state in ('cleanup_claimed', 'retry_wait') or (publication_state = 'completed' and (completion_outcome = 'already_absent' or object_version is not null)))",
		"check ((publication_state = 'retry_wait') = (retry_at is not null))",
		"check ((publication_state = 'completed') = (completion_outcome is not null and receipt_digest is not null and completed_at is not null))",
		"check (publication_state <> 'completed' or (completion_outcome = 'consumed' and cleanup_owner_id = '') or (completion_outcome in ('deleted', 'already_absent') and cleanup_owner_id <> ''))",
		"unique (owner_kind, owner_id, owner_generation, blob_key)",
	} {
		if !strings.Contains(tableSQL, want) {
			t.Errorf("0053 Blob publication intent missing invariant %q", want)
		}
	}
	for _, want := range []string{
		"create unique index if not exists uq_blob_publication_intents_active_key on public.blob_publication_intents(blob_key) where publication_state <> 'completed'",
		"create index if not exists idx_blob_publication_intents_claim on public.blob_publication_intents(publication_state, retry_at, cleanup_lease_expires_at, publish_expires_at, backend_kind, publication_id)",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0053 Blob publication intent missing index %q", want)
		}
	}
	if strings.Contains(tableSQL, "foreign key") {
		t.Fatal("Blob publication intent must survive owner and Blob metadata loss")
	}
}
