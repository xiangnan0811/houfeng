package migrate

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"houfeng/db/migrations"
)

var recordCollaborationCreateTablePattern = regexp.MustCompile(`(?m)create table if not exists public\.([a-z0-9_]+)\s*\(`)
var recordCollaborationColumnPattern = regexp.MustCompile(`(?m)^  ([a-z][a-z0-9_]*)\s+(?:bigint|boolean|bytea|integer|jsonb|text|timestamptz)\b`)

func recordCollaborationMigrationSQL(t *testing.T) string {
	t.Helper()
	payload, err := migrations.FS.ReadFile("0055_create_record_collaboration.sql")
	if err != nil {
		t.Fatalf("read 0055 record-collaboration migration: %v", err)
	}
	return strings.ToLower(string(payload))
}

func normalizedRecordCollaborationMigrationSQL(t *testing.T) string {
	t.Helper()
	return strings.Join(strings.Fields(recordCollaborationMigrationSQL(t)), " ")
}

func TestRecordCollaborationMigrationDefinesExactOwnedTables(t *testing.T) {
	matches := recordCollaborationCreateTablePattern.FindAllStringSubmatch(recordCollaborationMigrationSQL(t), -1)
	got := make([]string, 0, len(matches))
	for _, match := range matches {
		got = append(got, match[1])
	}
	want := []string{
		"record_actions",
		"record_action_events",
		"record_comments",
		"record_comment_revisions",
		"record_comment_tombstones",
		"record_comment_replies",
		"record_comment_mentions",
		"record_followers",
		"record_notifications",
		"record_notification_recipients",
		"record_notification_deliveries",
		"record_notification_delivery_attempts",
		"record_collaboration_purge_receipts",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("0055 record-collaboration tables = %#v, want %#v", got, want)
	}
}

func TestRecordCollaborationMigrationReusesFoundationPrimitives(t *testing.T) {
	tables := recordCollaborationCreateTablePattern.FindAllStringSubmatch(recordCollaborationMigrationSQL(t), -1)
	for _, match := range tables {
		name := match[1]
		for _, forbidden := range []string{"idempotency", "outbox", "_lease", "authorization"} {
			if strings.Contains(name, forbidden) {
				t.Fatalf("0055 duplicates record-platform primitive with table %q", name)
			}
		}
	}
	for _, forbidden := range []string{
		"record_collaboration_idempotency_keys",
		"record_collaboration_outbox",
		"record_collaboration_leases",
		"record_collaboration_authorizations",
		"record_search",
		"record_activity_projection",
		"record_portability",
	} {
		if strings.Contains(recordCollaborationMigrationSQL(t), "create table if not exists public."+forbidden) {
			t.Fatalf("0055 contains forbidden duplicate/downstream root table %q", forbidden)
		}
	}
}

func TestRecordCollaborationMigrationExtendsExistingOutboxWithIdentityOnlySourceVersion(t *testing.T) {
	normalized := normalizedRecordCollaborationMigrationSQL(t)
	for _, want := range []string{
		"alter table public.record_outbox add column if not exists source_version bigint not null default 0",
		"check (source_version >= 0)",
	} {
		if !strings.Contains(normalized, want) {
			t.Fatalf("0055 existing-outbox source-version extension missing %q", want)
		}
	}
}

func TestRecordCollaborationMigrationBindsEveryRecordSurfaceToFenceEpoch(t *testing.T) {
	sql := recordCollaborationMigrationSQL(t)
	for _, table := range []string{
		"record_actions",
		"record_action_events",
		"record_comments",
		"record_comment_revisions",
		"record_comment_tombstones",
		"record_comment_replies",
		"record_comment_mentions",
		"record_followers",
		"record_notifications",
		"record_notification_recipients",
		"record_notification_deliveries",
		"record_notification_delivery_attempts",
	} {
		tableSQL := normalizedRecordCollaborationTableDefinition(t, sql, table)
		if !strings.Contains(tableSQL, "record_fence_epoch bigint not null check (record_fence_epoch >= 0)") {
			t.Errorf("0055 %s does not bind a nonnegative record_fence_epoch", table)
		}
	}
	if strings.Contains(recordCollaborationTableDefinition(t, sql, "record_collaboration_purge_receipts"), "record_fence_epoch") {
		t.Fatal("0055 content-free purge receipt must not retain a record fence identity")
	}
}

func TestRecordCollaborationMigrationKeepsCleanupExplicitAndDetachedFromSources(t *testing.T) {
	sql := normalizedRecordCollaborationMigrationSQL(t)
	if strings.Contains(sql, "on delete cascade") || strings.Contains(sql, "on delete set null") {
		t.Fatal("0055 collaboration cleanup must be explicit and must not cascade from records or sources")
	}
	for _, forbidden := range []string{
		"references public.vps_assets",
		"references public.monitoring_instances",
		"references public.targets",
		"foreign key (source_id)",
		"foreign key (actor_id)",
		"foreign key (user_id)",
	} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("0055 collaboration migration unexpectedly binds live source identity with %q", forbidden)
		}
	}
	if got := strings.Count(sql, "on delete restrict"); got < 18 {
		t.Fatalf("0055 explicit restrict-delete bindings = %d, want at least 18", got)
	}
}

func TestRecordCollaborationMigrationEnforcesActionStateAndAppendOnlyHistory(t *testing.T) {
	sql := recordCollaborationMigrationSQL(t)
	actionSQL := normalizedRecordCollaborationTableDefinition(t, sql, "record_actions")
	for _, want := range []string{
		"action_id text primary key check (action_id ~ '^ract_[a-z0-9]{1,64}$')",
		"action_version bigint not null check (action_version > 0)",
		"status text not null default 'open' check (status in ('open', 'completed', 'cancelled'))",
		"check ((status = 'completed') = (completed_at is not null))",
		"unique (record_id, action_id)",
		"foreign key (record_id) references public.records(record_id) on delete restrict",
		"foreign key (record_id, subject_revision_id) references public.record_revisions(record_id, revision_id) on delete restrict",
	} {
		if !strings.Contains(actionSQL, want) {
			t.Errorf("0055 record_actions missing invariant %q", want)
		}
	}

	eventSQL := normalizedRecordCollaborationTableDefinition(t, sql, "record_action_events")
	for _, want := range []string{
		"assignee_id text check (assignee_id is null or assignee_id ~ '^usr_[a-z0-9]{1,64}$')",
		"event_kind text not null check (event_kind in ('created', 'updated', 'completed', 'cancelled', 'reopened'))",
		"previous_status text check (previous_status is null or previous_status in ('open', 'completed', 'cancelled'))",
		"current_status text not null check (current_status in ('open', 'completed', 'cancelled'))",
		"unique (action_id, action_version)",
		"constraint record_action_events_transition_check check (((event_kind = 'created' and action_version = 1 and previous_status is null and current_status = 'open') or (event_kind = 'updated' and previous_status = current_status) or (event_kind = 'completed' and previous_status = 'open' and current_status = 'completed') or (event_kind = 'cancelled' and previous_status = 'open' and current_status = 'cancelled') or (event_kind = 'reopened' and previous_status in ('completed', 'cancelled') and current_status = 'open')) is true)",
	} {
		if !strings.Contains(eventSQL, want) {
			t.Errorf("0055 record_action_events missing invariant %q", want)
		}
	}
	wantTrigger := "create trigger record_action_events_reject_update before update on public.record_action_events for each row execute function record_platform_internal.reject_immutable_mutation()"
	if !strings.Contains(normalizedRecordCollaborationMigrationSQL(t), wantTrigger) {
		t.Errorf("0055 action history missing immutable trigger %q", wantTrigger)
	}
}

func TestRecordCollaborationMigrationEnforcesOneWayCommentRedaction(t *testing.T) {
	sql := normalizedRecordCollaborationMigrationSQL(t)
	commentSQL := normalizedRecordCollaborationTableDefinition(t, recordCollaborationMigrationSQL(t), "record_comments")
	for _, want := range []string{
		"comment_state text not null default 'active' check (comment_state in ('active', 'redacted'))",
		"comment_version bigint not null check (comment_version > 0)",
		"body_markdown text",
		"render_contract_version text",
		"render_model jsonb",
		"body_digest bytea check (body_digest is null or octet_length(body_digest) = 32)",
		"constraint record_comments_redaction_shape_check check (((comment_state = 'active' and body_markdown is not null and render_contract_version = 'comment_markdown/v1' and render_model is not null and jsonb_typeof(render_model) = 'object' and body_digest is not null and tombstone_id is null and redacted_at is null) or (comment_state = 'redacted' and body_markdown is null and render_contract_version is null and render_model is null and body_digest is null and tombstone_id is not null and redacted_at is not null)) is true)",
	} {
		if !strings.Contains(commentSQL, want) {
			t.Errorf("0055 record_comments missing redaction invariant %q", want)
		}
	}
	revisionSQL := normalizedRecordCollaborationTableDefinition(t, recordCollaborationMigrationSQL(t), "record_comment_revisions")
	for _, want := range []string{
		"unique (comment_id, comment_version)",
		"constraint record_comment_revisions_redaction_shape_check check (((redacted_at is null and body_markdown is not null and render_contract_version = 'comment_markdown/v1' and render_model is not null and jsonb_typeof(render_model) = 'object' and body_digest is not null and tombstone_id is null) or (redacted_at is not null and body_markdown is null and render_contract_version is null and render_model is null and body_digest is null and tombstone_id is not null)) is true)",
	} {
		if !strings.Contains(revisionSQL, want) {
			t.Errorf("0055 record_comment_revisions missing redaction invariant %q", want)
		}
	}
	for _, want := range []string{
		"create or replace function record_platform_internal.enforce_record_comment_mutation() returns trigger language plpgsql security invoker set search_path = pg_catalog",
		"create or replace function record_platform_internal.enforce_record_comment_revision_mutation() returns trigger language plpgsql security invoker set search_path = pg_catalog",
		"revoke all on function record_platform_internal.enforce_record_comment_mutation() from public",
		"revoke all on function record_platform_internal.enforce_record_comment_revision_mutation() from public",
		"create trigger record_comments_enforce_mutation before update on public.record_comments for each row execute function record_platform_internal.enforce_record_comment_mutation()",
		"create trigger record_comment_revisions_enforce_mutation before update on public.record_comment_revisions for each row execute function record_platform_internal.enforce_record_comment_revision_mutation()",
		"create trigger record_comment_tombstones_reject_update before update on public.record_comment_tombstones for each row execute function record_platform_internal.reject_immutable_mutation()",
		"or exists (select 1 from public.record_comment_revisions as revision where revision.record_id = new.record_id and revision.comment_id = new.comment_id and revision.redacted_at is null)",
		"perform 1 from public.record_comments as comment where comment.record_id = new.record_id and comment.comment_id = new.comment_id and comment.project_id = new.project_id and comment.comment_state = 'active' for update",
		"if not found or new.redacted_at is not null then",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0055 comment redaction missing database enforcement %q", want)
		}
	}
	tombstoneColumns := recordCollaborationTableColumns(t, "record_comment_tombstones")
	wantTombstoneColumns := []string{"tombstone_id", "record_id", "comment_id", "tombstone_version", "deleted_by", "reason_code", "deleted_at", "record_fence_epoch", "created_at"}
	if !reflect.DeepEqual(tombstoneColumns, wantTombstoneColumns) {
		t.Errorf("0055 minimal comment tombstone columns = %#v, want %#v", tombstoneColumns, wantTombstoneColumns)
	}
}

func TestRecordCollaborationMigrationBindsChildIdentityAndFenceToItsParent(t *testing.T) {
	sql := normalizedRecordCollaborationMigrationSQL(t)
	for _, want := range []string{
		"unique (record_id, comment_id, comment_version, record_fence_epoch)",
		"foreign key (record_id, comment_id, comment_version, record_fence_epoch) references public.record_comment_revisions(record_id, comment_id, comment_version, record_fence_epoch) on delete restrict",
		"unique (record_id, notification_id, recipient_user_id, record_fence_epoch)",
		"foreign key (record_id, notification_id, recipient_user_id, record_fence_epoch) references public.record_notification_recipients(record_id, notification_id, recipient_user_id, record_fence_epoch) on delete restrict",
		"unique (record_id, delivery_id, notification_id, recipient_user_id, record_fence_epoch)",
		"foreign key (record_id, delivery_id, notification_id, recipient_user_id, record_fence_epoch) references public.record_notification_deliveries(record_id, delivery_id, notification_id, recipient_user_id, record_fence_epoch) on delete restrict",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0055 collaboration parent identity/fence binding missing %q", want)
		}
	}
}

func TestRecordCollaborationMigrationEnforcesReplyMentionFollowerAndInboxIdentity(t *testing.T) {
	sql := normalizedRecordCollaborationMigrationSQL(t)
	for _, want := range []string{
		"check (child_comment_id <> parent_comment_id)",
		"primary key (comment_id, comment_version, mentioned_user_id)",
		"manual_preference text not null default 'default' check (manual_preference in ('default', 'watching', 'muted'))",
		"constraint record_followers_source_check check (manual_preference <> 'default' or follows_author or follows_owner or follows_participant or follows_comment or follows_mention or follows_action)",
		"reason_kind text not null check (reason_kind in ('owner', 'participant', 'assignee', 'mention', 'reply', 'follower', 'security'))",
		"constraint record_notification_recipients_mandatory_check check (mandatory = (reason_kind in ('assignee', 'mention', 'security')))",
		"check (read_at is null or read_at >= created_at)",
		"check (dismissed_at is null or (read_at is not null and dismissed_at >= read_at))",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0055 collaboration relation/inbox missing invariant %q", want)
		}
	}
}

func TestRecordCollaborationMigrationBoundsNotificationRetentionAndRetries(t *testing.T) {
	sql := normalizedRecordCollaborationMigrationSQL(t)
	for _, want := range []string{
		"check (details_delete_after > created_at)",
		"delivery_state text not null default 'pending' check (delivery_state in ('pending', 'processing', 'retry_wait', 'sent', 'cancelled', 'permanent_failure'))",
		"attempt_count integer not null default 0 check (attempt_count between 0 and 8)",
		"constraint record_notification_deliveries_retry_check check ((delivery_state = 'retry_wait') = (next_attempt_at is not null))",
		"constraint record_notification_deliveries_sent_check check ((delivery_state = 'sent') = (sent_at is not null))",
		"attempt_no integer not null check (attempt_no between 1 and 8)",
		"outcome text not null check (outcome in ('sent', 'temporary_failure', 'permanent_failure', 'cancelled'))",
		"unique (delivery_id, attempt_no)",
		"create index if not exists idx_record_notifications_retention on public.record_notifications(details_delete_after, notification_id)",
		"create index if not exists idx_record_notification_deliveries_retry on public.record_notification_deliveries(delivery_state, next_attempt_at, delivery_id)",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0055 notification retention/retry missing invariant %q", want)
		}
	}
	attemptSQL := recordCollaborationTableDefinition(t, recordCollaborationMigrationSQL(t), "record_notification_delivery_attempts")
	for _, forbidden := range []string{"payload", "body", "comment_markdown", "render_model", "provider_error", "response"} {
		if strings.Contains(attemptSQL, forbidden) {
			t.Errorf("0055 delivery-attempt audit retains forbidden content field %q", forbidden)
		}
	}
}

func TestRecordCollaborationMigrationKeepsPurgeReceiptMinimalAndImmutable(t *testing.T) {
	wantColumns := []string{
		"operation_id", "adapter_name", "removed_surface_digest", "receipt_digest",
		"removed_row_count", "verified_absent_at", "created_at",
	}
	if got := recordCollaborationTableColumns(t, "record_collaboration_purge_receipts"); !reflect.DeepEqual(got, wantColumns) {
		t.Fatalf("0055 content-free collaboration purge receipt columns = %#v, want %#v", got, wantColumns)
	}
	sql := normalizedRecordCollaborationMigrationSQL(t)
	for _, want := range []string{
		"adapter_name text not null default 'record_collaboration' check (adapter_name = 'record_collaboration')",
		"foreign key (operation_id) references public.record_purge_operations(operation_id) on delete restrict",
		"create trigger record_collaboration_purge_receipts_reject_update before update on public.record_collaboration_purge_receipts for each row execute function record_platform_internal.reject_immutable_mutation()",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("0055 collaboration purge receipt missing invariant %q", want)
		}
	}
	for _, forbidden := range []string{"project_id", "record_id", "revision_id", "comment_id", "action_id", "user_id"} {
		if strings.Contains(recordCollaborationTableDefinition(t, recordCollaborationMigrationSQL(t), "record_collaboration_purge_receipts"), forbidden) {
			t.Errorf("0055 collaboration purge receipt retains forbidden identity field %q", forbidden)
		}
	}
}

func recordCollaborationTableColumns(t *testing.T, table string) []string {
	t.Helper()
	tableSQL := recordCollaborationTableDefinition(t, recordCollaborationMigrationSQL(t), table)
	matches := recordCollaborationColumnPattern.FindAllStringSubmatch(tableSQL, -1)
	columns := make([]string, 0, len(matches))
	for _, match := range matches {
		columns = append(columns, match[1])
	}
	return columns
}

func recordCollaborationTableDefinition(t *testing.T, sql, table string) string {
	t.Helper()
	start := strings.Index(sql, "create table if not exists public."+table+" (")
	if start < 0 {
		t.Fatalf("0055 record-collaboration migration missing %s table", table)
	}
	end := strings.Index(sql[start:], ");")
	if end < 0 {
		t.Fatalf("0055 record-collaboration %s table is unterminated", table)
	}
	return sql[start : start+end]
}

func normalizedRecordCollaborationTableDefinition(t *testing.T, sql, table string) string {
	t.Helper()
	return strings.Join(strings.Fields(recordCollaborationTableDefinition(t, sql, table)), " ")
}
