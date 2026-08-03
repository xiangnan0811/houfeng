package migrate

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"houfeng/db/migrations"
)

var recordsCoreCreateTablePattern = regexp.MustCompile(`(?m)create table if not exists public\.([a-z0-9_]+)\s*\(`)

func recordsCoreMigrationSQL(t *testing.T) string {
	t.Helper()

	payload, err := migrations.FS.ReadFile("0052_create_records_core.sql")
	if err != nil {
		t.Fatalf("read 0052 records-core migration: %v", err)
	}
	return strings.ToLower(string(payload))
}

func normalizedRecordsCoreMigrationSQL(t *testing.T) string {
	t.Helper()
	return strings.Join(strings.Fields(recordsCoreMigrationSQL(t)), " ")
}

func TestRecordsCoreMigrationDefinesExactOwnedTables(t *testing.T) {
	matches := recordsCoreCreateTablePattern.FindAllStringSubmatch(recordsCoreMigrationSQL(t), -1)
	got := make([]string, 0, len(matches))
	for _, match := range matches {
		got = append(got, match[1])
	}
	want := []string{
		"records",
		"record_revisions",
		"record_revision_subjects",
		"record_revision_tags",
		"record_revision_participants",
		"record_drafts",
		"record_draft_checkpoints",
		"record_domain_activities",
		"record_core_purge_receipts",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("0052 records-core tables = %#v, want %#v", got, want)
	}
}

func TestRecordsCoreMigrationUsesCanonicalStorageNamesOnly(t *testing.T) {
	sql := recordsCoreMigrationSQL(t)
	for _, forbidden := range []string{
		"participant_ids",
		"record_draft_recovery_points",
		"experience_logs",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("0052 records-core migration contains forbidden legacy storage name %q", forbidden)
		}
	}
}

func TestRecordsCoreMigrationKeepsOwnedCleanupExplicit(t *testing.T) {
	sql := normalizedRecordsCoreMigrationSQL(t)
	if strings.Contains(sql, "on delete cascade") {
		t.Fatal("0052 records-core migration must not cascade source or record-owned deletion")
	}
}

func TestRecordsCoreMigrationEnforcesRevisionIdentityAndOrdering(t *testing.T) {
	sql := normalizedRecordsCoreMigrationSQL(t)
	for _, want := range []string{
		"revision_no bigint not null check (revision_no > 0)",
		"unique (record_id, revision_id)",
		"unique (record_id, revision_no)",
		"create index if not exists idx_record_revisions_record_hash on public.record_revisions(record_id, canonical_hash)",
		"foreign key (record_id, current_revision_id) references public.record_revisions(record_id, revision_id) on delete restrict deferrable initially deferred",
		"foreign key (record_id, base_revision_id) references public.record_revisions(record_id, revision_id) on delete restrict",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0052 records-core migration missing revision invariant %q", want)
		}
	}
}

func TestRecordsCoreMigrationCapturesImmutableRevisionSubjects(t *testing.T) {
	sql := normalizedRecordsCoreMigrationSQL(t)
	for _, want := range []string{
		"registry_version bigint not null check (registry_version > 0)",
		"subject_kind text not null",
		"relation_role text not null",
		"source_id text not null",
		"identity_snapshot jsonb not null",
		"capture_authorization jsonb not null",
		"capture_authorization_digest bytea not null check (octet_length(capture_authorization_digest) = 32)",
		"primary key (revision_id, ordinal)",
		"create unique index if not exists uq_record_revision_subjects_primary on public.record_revision_subjects(revision_id) where is_primary",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0052 records-core migration missing immutable subject invariant %q", want)
		}
	}
	for _, forbidden := range []string{
		"live_route",
		"current_authorization",
		"authorization_floor",
		"tombstone",
	} {
		if strings.Contains(sql, forbidden) {
			t.Errorf("0052 records-core migration stores mutable subject state %q", forbidden)
		}
	}
}

func TestRecordsCoreMigrationDefersExactlyOnePrimarySubjectValidation(t *testing.T) {
	sql := normalizedRecordsCoreMigrationSQL(t)
	for _, want := range []string{
		"create or replace function record_platform_internal.validate_record_revision_primary_subject() returns trigger language plpgsql security invoker set search_path = pg_catalog",
		"revoke all on function record_platform_internal.validate_record_revision_primary_subject() from public",
		"create constraint trigger record_revisions_require_primary_subject after insert on public.record_revisions deferrable initially deferred for each row execute function record_platform_internal.validate_record_revision_primary_subject()",
		"create constraint trigger record_revision_subjects_require_primary_subject after insert or delete on public.record_revision_subjects deferrable initially deferred for each row execute function record_platform_internal.validate_record_revision_primary_subject()",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0052 records-core migration missing deferred primary-subject invariant %q", want)
		}
	}
}

func TestRecordsCoreMigrationIndexesDraftRetentionAndAuthorAccess(t *testing.T) {
	sql := normalizedRecordsCoreMigrationSQL(t)
	for _, want := range []string{
		"create unique index if not exists uq_record_drafts_record_author on public.record_drafts(record_id, author_id) where record_id is not null",
		"create index if not exists idx_record_drafts_author_activity on public.record_drafts(author_id, updated_at desc, draft_id)",
		"create index if not exists idx_record_drafts_expiry on public.record_drafts(expires_at, draft_id)",
		"unique (draft_id, checkpoint_bucket)",
		"create index if not exists idx_record_draft_checkpoints_retention on public.record_draft_checkpoints(draft_id, created_at desc, checkpoint_id)",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0052 records-core migration missing draft invariant %q", want)
		}
	}
}

func TestRecordsCoreMigrationStoresCompleteRevisionAuthorityAndProjection(t *testing.T) {
	sql := normalizedRecordsCoreMigrationSQL(t)
	for _, want := range []string{
		"lifecycle text not null default 'active' check (lifecycle in ('active', 'archived'))",
		"lock_version bigint not null default 0 check (lock_version >= 0)",
		"authorization_epoch bigint not null default 0 check (authorization_epoch >= 0)",
		"current_title text",
		"current_record_type text",
		"current_status_group text",
		"current_visibility_scope jsonb",
		"current_visibility_digest bytea check (octet_length(current_visibility_digest) = 32)",
		"current_owner_id text",
		"current_follow_up_at timestamptz",
		"title text not null",
		"body_markdown text not null",
		"markdown_dialect_version bigint not null check (markdown_dialect_version > 0)",
		"record_type text not null check (record_type in ('troubleshooting', 'maintenance', 'migration', 'provider_communication', 'billing', 'important_finding', 'note'))",
		"business_status text check (business_status is null or business_status ~ '^[a-z0-9_]{1,64}$')",
		"status_group text check (status_group is null or status_group in ('pending', 'in_progress', 'waiting', 'verification', 'completed', 'cancelled'))",
		"impact_level text not null check (impact_level ~ '^[a-z0-9_]{1,64}$')",
		"occurred_at timestamptz",
		"completed_at timestamptz",
		"visibility_scope jsonb not null",
		"visibility_digest bytea not null check (octet_length(visibility_digest) = 32)",
		"owner_id text",
		"follow_up_at timestamptz",
		"template_id text",
		"template_version bigint",
		"author_id text not null",
		"save_reason text not null default ''",
		"check ((template_id is null) = (template_version is null))",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0052 records-core migration missing authority field %q", want)
		}
	}
}

func TestRecordsCoreMigrationStoresPrivateDraftVersionAndBoundedCheckpoints(t *testing.T) {
	sql := normalizedRecordsCoreMigrationSQL(t)
	for _, want := range []string{
		"payload jsonb not null",
		"payload_hash bytea not null check (octet_length(payload_hash) = 32)",
		"draft_version bigint not null check (draft_version > 0)",
		"etag_digest bytea not null check (octet_length(etag_digest) = 32)",
		"warning_at timestamptz not null",
		"checkpoint_payload jsonb not null",
		"checkpoint_payload_hash bytea not null check (octet_length(checkpoint_payload_hash) = 32)",
		"checkpoint_draft_version bigint not null check (checkpoint_draft_version > 0)",
		"checkpoint_expires_at timestamptz not null",
		"create index if not exists idx_record_draft_checkpoints_expiry on public.record_draft_checkpoints(checkpoint_expires_at, checkpoint_id)",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0052 records-core migration missing draft payload/retention field %q", want)
		}
	}
}

func TestRecordsCoreMigrationKeepsDomainActivityAndPurgeReceiptContentFree(t *testing.T) {
	sql := normalizedRecordsCoreMigrationSQL(t)
	for _, want := range []string{
		"event_kind text not null check (event_kind ~ '^[a-z0-9_]{1,64}$')",
		"source_event_id text not null",
		"source_version bigint not null check (source_version > 0)",
		"record_lock_version bigint not null check (record_lock_version > 0)",
		"event_at timestamptz not null",
		"recorded_at timestamptz not null default now()",
		"unique (project_id, event_kind, source_event_id)",
		"adapter_name text not null default 'record_core' check (adapter_name = 'record_core')",
		"removed_surface_digest bytea not null check (octet_length(removed_surface_digest) = 32)",
		"receipt_digest bytea not null check (octet_length(receipt_digest) = 32)",
		"removed_row_count bigint not null check (removed_row_count >= 0)",
		"verified_absent_at timestamptz not null",
		"foreign key (operation_id) references public.record_purge_operations(operation_id) on delete restrict",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0052 records-core migration missing activity/receipt field %q", want)
		}
	}
}

func TestRecordsCoreMigrationRejectsUpdatesToImmutableRows(t *testing.T) {
	sql := normalizedRecordsCoreMigrationSQL(t)
	for _, table := range []string{
		"record_revisions",
		"record_revision_subjects",
		"record_revision_tags",
		"record_revision_participants",
		"record_draft_checkpoints",
		"record_domain_activities",
		"record_core_purge_receipts",
	} {
		want := "create trigger " + table + "_reject_update before update on public." + table + " for each row execute function record_platform_internal.reject_immutable_mutation()"
		if !strings.Contains(sql, want) {
			t.Errorf("0052 records-core migration missing immutable update trigger %q", table)
		}
	}
}
