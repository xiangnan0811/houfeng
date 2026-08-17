create table if not exists public.record_actions (
  action_id text primary key check (action_id ~ '^ract_[a-z0-9]{1,64}$'),
  project_id text not null default 'default' check (project_id = 'default'),
  record_id text not null,
  subject_revision_id text,
  action_version bigint not null check (action_version > 0),
  title text not null check (char_length(title) between 1 and 512),
  details text not null default '' check (char_length(details) <= 4096),
  status text not null default 'open'
    check (status in ('open', 'completed', 'cancelled')),
  assignee_id text check (assignee_id is null or assignee_id ~ '^usr_[a-z0-9]{1,64}$'),
  due_at timestamptz,
  completed_at timestamptz,
  created_by text not null check (created_by ~ '^usr_[a-z0-9]{1,64}$'),
  updated_by text not null check (updated_by ~ '^usr_[a-z0-9]{1,64}$'),
  record_fence_epoch bigint not null check (record_fence_epoch >= 0),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (record_id, action_id),
  check ((status = 'completed') = (completed_at is not null)),
  check (completed_at is null or completed_at >= created_at),
  check (updated_at >= created_at),
  foreign key (record_id) references public.records(record_id)
    on delete restrict,
  foreign key (record_id, subject_revision_id)
    references public.record_revisions(record_id, revision_id)
    on delete restrict
);

create index if not exists idx_record_actions_record_state
  on public.record_actions(record_id, status, due_at, action_id);
create index if not exists idx_record_actions_assignee
  on public.record_actions(assignee_id, status, due_at, action_id)
  where assignee_id is not null;

create table if not exists public.record_action_events (
  action_event_id text primary key
    check (action_event_id ~ '^raev_[a-z0-9]{1,64}$'),
  project_id text not null default 'default' check (project_id = 'default'),
  record_id text not null,
  action_id text not null,
  action_version bigint not null check (action_version > 0),
  event_kind text not null
    check (event_kind in ('created', 'updated', 'completed', 'cancelled', 'reopened')),
  previous_status text
    check (previous_status is null or previous_status in ('open', 'completed', 'cancelled')),
  current_status text not null
    check (current_status in ('open', 'completed', 'cancelled')),
  actor_id text not null check (actor_id ~ '^usr_[a-z0-9]{1,64}$'),
  record_fence_epoch bigint not null check (record_fence_epoch >= 0),
  occurred_at timestamptz not null,
  created_at timestamptz not null default now(),
  unique (action_id, action_version),
  constraint record_action_events_transition_check check (((event_kind = 'created' and action_version = 1 and previous_status is null and current_status = 'open') or (event_kind = 'updated' and previous_status = current_status) or (event_kind = 'completed' and previous_status = 'open' and current_status = 'completed') or (event_kind = 'cancelled' and previous_status = 'open' and current_status = 'cancelled') or (event_kind = 'reopened' and previous_status in ('completed', 'cancelled') and current_status = 'open')) is true),
  foreign key (record_id) references public.records(record_id)
    on delete restrict,
  foreign key (record_id, action_id)
    references public.record_actions(record_id, action_id)
    on delete restrict
);

create index if not exists idx_record_action_events_action
  on public.record_action_events(action_id, action_version, action_event_id);
create index if not exists idx_record_action_events_record_time
  on public.record_action_events(record_id, occurred_at desc, action_event_id);

create table if not exists public.record_comments (
  comment_id text primary key check (comment_id ~ '^rcm_[a-z0-9]{1,64}$'),
  project_id text not null default 'default' check (project_id = 'default'),
  record_id text not null,
  author_id text not null check (author_id ~ '^usr_[a-z0-9]{1,64}$'),
  comment_state text not null default 'active'
    check (comment_state in ('active', 'redacted')),
  comment_version bigint not null check (comment_version > 0),
  body_markdown text
    check (body_markdown is null or octet_length(body_markdown) between 1 and 16384),
  render_contract_version text,
  render_model jsonb,
  body_digest bytea check (body_digest is null or octet_length(body_digest) = 32),
  tombstone_id text
    check (tombstone_id is null or tombstone_id ~ '^rct_[a-z0-9]{1,64}$'),
  redacted_at timestamptz,
  record_fence_epoch bigint not null check (record_fence_epoch >= 0),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (record_id, comment_id),
  constraint record_comments_redaction_shape_check check (((comment_state = 'active' and body_markdown is not null and render_contract_version = 'comment_markdown/v1' and render_model is not null and jsonb_typeof(render_model) = 'object' and body_digest is not null and tombstone_id is null and redacted_at is null) or (comment_state = 'redacted' and body_markdown is null and render_contract_version is null and render_model is null and body_digest is null and tombstone_id is not null and redacted_at is not null)) is true),
  check (updated_at >= created_at),
  check (redacted_at is null or redacted_at >= created_at),
  foreign key (record_id) references public.records(record_id)
    on delete restrict
);

create index if not exists idx_record_comments_record_time
  on public.record_comments(record_id, created_at, comment_id);

create table if not exists public.record_comment_revisions (
  comment_revision_id text primary key
    check (comment_revision_id ~ '^rcr_[a-z0-9]{1,64}$'),
  project_id text not null default 'default' check (project_id = 'default'),
  record_id text not null,
  comment_id text not null,
  comment_version bigint not null check (comment_version > 0),
  edited_by text not null check (edited_by ~ '^usr_[a-z0-9]{1,64}$'),
  body_markdown text
    check (body_markdown is null or octet_length(body_markdown) between 1 and 16384),
  render_contract_version text,
  render_model jsonb,
  body_digest bytea check (body_digest is null or octet_length(body_digest) = 32),
  tombstone_id text
    check (tombstone_id is null or tombstone_id ~ '^rct_[a-z0-9]{1,64}$'),
  redacted_at timestamptz,
  record_fence_epoch bigint not null check (record_fence_epoch >= 0),
  created_at timestamptz not null default now(),
  unique (comment_id, comment_version),
  unique (record_id, comment_id, comment_version, record_fence_epoch),
  constraint record_comment_revisions_redaction_shape_check check (((redacted_at is null and body_markdown is not null and render_contract_version = 'comment_markdown/v1' and render_model is not null and jsonb_typeof(render_model) = 'object' and body_digest is not null and tombstone_id is null) or (redacted_at is not null and body_markdown is null and render_contract_version is null and render_model is null and body_digest is null and tombstone_id is not null)) is true),
  check (redacted_at is null or redacted_at >= created_at),
  foreign key (record_id) references public.records(record_id)
    on delete restrict,
  foreign key (record_id, comment_id)
    references public.record_comments(record_id, comment_id)
    on delete restrict
);

create index if not exists idx_record_comment_revisions_comment
  on public.record_comment_revisions(comment_id, comment_version, comment_revision_id);

create table if not exists public.record_comment_tombstones (
  tombstone_id text primary key check (tombstone_id ~ '^rct_[a-z0-9]{1,64}$'),
  record_id text not null,
  comment_id text not null,
  tombstone_version bigint not null check (tombstone_version > 0),
  deleted_by text not null check (deleted_by ~ '^usr_[a-z0-9]{1,64}$'),
  reason_code text not null
    check (reason_code in ('author_deleted', 'moderator_deleted', 'record_deleted')),
  deleted_at timestamptz not null,
  record_fence_epoch bigint not null check (record_fence_epoch >= 0),
  created_at timestamptz not null default now(),
  unique (comment_id, tombstone_version),
  foreign key (record_id) references public.records(record_id)
    on delete restrict,
  foreign key (record_id, comment_id)
    references public.record_comments(record_id, comment_id)
    on delete restrict
);

create table if not exists public.record_comment_replies (
  record_id text not null,
  child_comment_id text primary key,
  parent_comment_id text not null,
  record_fence_epoch bigint not null check (record_fence_epoch >= 0),
  created_at timestamptz not null default now(),
  check (child_comment_id <> parent_comment_id),
  foreign key (record_id) references public.records(record_id)
    on delete restrict,
  foreign key (record_id, child_comment_id)
    references public.record_comments(record_id, comment_id)
    on delete restrict,
  foreign key (record_id, parent_comment_id)
    references public.record_comments(record_id, comment_id)
    on delete restrict
);

create index if not exists idx_record_comment_replies_parent
  on public.record_comment_replies(parent_comment_id, child_comment_id);

create table if not exists public.record_comment_mentions (
  record_id text not null,
  comment_id text not null,
  comment_version bigint not null check (comment_version > 0),
  mentioned_user_id text not null check (mentioned_user_id ~ '^usr_[a-z0-9]{1,64}$'),
  record_fence_epoch bigint not null check (record_fence_epoch >= 0),
  created_at timestamptz not null default now(),
  primary key (comment_id, comment_version, mentioned_user_id),
  foreign key (record_id) references public.records(record_id)
    on delete restrict,
  foreign key (record_id, comment_id, comment_version, record_fence_epoch)
    references public.record_comment_revisions(record_id, comment_id, comment_version, record_fence_epoch)
    on delete restrict
);

create index if not exists idx_record_comment_mentions_user
  on public.record_comment_mentions(mentioned_user_id, created_at desc, comment_id);

create table if not exists public.record_followers (
  project_id text not null default 'default' check (project_id = 'default'),
  record_id text not null,
  user_id text not null check (user_id ~ '^usr_[a-z0-9]{1,64}$'),
  follower_version bigint not null check (follower_version > 0),
  manual_preference text not null default 'default'
    check (manual_preference in ('default', 'watching', 'muted')),
  follows_author boolean not null default false,
  follows_owner boolean not null default false,
  follows_participant boolean not null default false,
  follows_comment boolean not null default false,
  follows_mention boolean not null default false,
  follows_action boolean not null default false,
  record_fence_epoch bigint not null check (record_fence_epoch >= 0),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  primary key (record_id, user_id),
  constraint record_followers_source_check check (manual_preference <> 'default' or follows_author or follows_owner or follows_participant or follows_comment or follows_mention or follows_action),
  check (updated_at >= created_at),
  foreign key (record_id) references public.records(record_id)
    on delete restrict
);

create index if not exists idx_record_followers_user
  on public.record_followers(user_id, record_id);

create table if not exists public.record_notifications (
  notification_id text primary key check (notification_id ~ '^rnt_[a-z0-9]{1,64}$'),
  project_id text not null default 'default' check (project_id = 'default'),
  record_id text not null,
  event_kind text not null
    check (event_kind in ('record_owner_changed', 'record_participant_changed',
      'record_follow_up_due', 'action_assigned', 'action_completed',
      'action_cancelled', 'comment_replied', 'comment_mentioned',
      'security_access_revoked')),
  subject_kind text not null check (subject_kind in ('record', 'action', 'comment')),
  subject_id text not null check (subject_id ~ '^[a-z0-9_]{1,128}$'),
  source_version bigint not null check (source_version > 0),
  actor_id text not null check (actor_id ~ '^usr_[a-z0-9]{1,64}$'),
  authorization_epoch bigint not null check (authorization_epoch >= 0),
  record_fence_epoch bigint not null check (record_fence_epoch >= 0),
  event_at timestamptz not null,
  created_at timestamptz not null default now(),
  details_delete_after timestamptz not null,
  unique (record_id, notification_id),
  unique (project_id, event_kind, subject_kind, subject_id, source_version),
  check (details_delete_after > created_at),
  foreign key (record_id) references public.records(record_id)
    on delete restrict
);

create index if not exists idx_record_notifications_record_time
  on public.record_notifications(record_id, event_at desc, notification_id);
create index if not exists idx_record_notifications_retention
  on public.record_notifications(details_delete_after, notification_id);

create table if not exists public.record_notification_recipients (
  notification_id text not null,
  record_id text not null,
  recipient_user_id text not null check (recipient_user_id ~ '^usr_[a-z0-9]{1,64}$'),
  reason_kind text not null
    check (reason_kind in ('owner', 'participant', 'assignee', 'mention', 'reply', 'follower', 'security')),
  mandatory boolean not null,
  authorization_epoch bigint not null check (authorization_epoch >= 0),
  record_fence_epoch bigint not null check (record_fence_epoch >= 0),
  created_at timestamptz not null default now(),
  read_at timestamptz,
  dismissed_at timestamptz,
  primary key (notification_id, recipient_user_id),
  unique (record_id, notification_id, recipient_user_id, record_fence_epoch),
  constraint record_notification_recipients_mandatory_check check (mandatory = (reason_kind in ('assignee', 'mention', 'security'))),
  check (read_at is null or read_at >= created_at),
  check (dismissed_at is null or (read_at is not null and dismissed_at >= read_at)),
  foreign key (record_id) references public.records(record_id)
    on delete restrict,
  foreign key (record_id, notification_id)
    references public.record_notifications(record_id, notification_id)
    on delete restrict
);

create index if not exists idx_record_notification_recipients_inbox
  on public.record_notification_recipients(recipient_user_id, read_at, created_at desc, notification_id);

create table if not exists public.record_notification_deliveries (
  delivery_id text primary key check (delivery_id ~ '^rnd_[a-z0-9]{1,64}$'),
  record_id text not null,
  notification_id text not null,
  recipient_user_id text not null check (recipient_user_id ~ '^usr_[a-z0-9]{1,64}$'),
  channel text not null check (channel in ('telegram', 'feishu')),
  binding_id text not null check (binding_id ~ '^[a-z0-9_-]{1,128}$'),
  delivery_state text not null default 'pending'
    check (delivery_state in ('pending', 'processing', 'retry_wait', 'sent', 'cancelled', 'permanent_failure')),
  attempt_count integer not null default 0 check (attempt_count between 0 and 8),
  next_attempt_at timestamptz,
  reason_code text not null default ''
    check (reason_code = '' or reason_code ~ '^[a-z0-9_]{1,64}$'),
  authorization_epoch bigint not null check (authorization_epoch >= 0),
  record_fence_epoch bigint not null check (record_fence_epoch >= 0),
  sent_at timestamptz,
  cancelled_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (record_id, delivery_id),
  unique (notification_id, recipient_user_id, channel),
  unique (record_id, delivery_id, notification_id, recipient_user_id, record_fence_epoch),
  constraint record_notification_deliveries_retry_check check ((delivery_state = 'retry_wait') = (next_attempt_at is not null)),
  constraint record_notification_deliveries_sent_check check ((delivery_state = 'sent') = (sent_at is not null)),
  check ((delivery_state = 'cancelled') = (cancelled_at is not null)),
  check (sent_at is null or sent_at >= created_at),
  check (cancelled_at is null or cancelled_at >= created_at),
  check (updated_at >= created_at),
  foreign key (record_id) references public.records(record_id)
    on delete restrict,
  foreign key (record_id, notification_id, recipient_user_id, record_fence_epoch)
    references public.record_notification_recipients(record_id, notification_id, recipient_user_id, record_fence_epoch)
    on delete restrict
);

create index if not exists idx_record_notification_deliveries_retry
  on public.record_notification_deliveries(delivery_state, next_attempt_at, delivery_id);

create table if not exists public.record_notification_delivery_attempts (
  attempt_id text primary key check (attempt_id ~ '^rna_[a-z0-9]{1,64}$'),
  record_id text not null,
  delivery_id text not null,
  notification_id text not null,
  recipient_user_id text not null check (recipient_user_id ~ '^usr_[a-z0-9]{1,64}$'),
  attempt_no integer not null check (attempt_no between 1 and 8),
  outcome text not null
    check (outcome in ('sent', 'temporary_failure', 'permanent_failure', 'cancelled')),
  reason_code text not null default ''
    check (reason_code = '' or reason_code ~ '^[a-z0-9_]{1,64}$'),
  authorization_epoch bigint not null check (authorization_epoch >= 0),
  record_fence_epoch bigint not null check (record_fence_epoch >= 0),
  started_at timestamptz not null,
  completed_at timestamptz not null,
  created_at timestamptz not null default now(),
  unique (delivery_id, attempt_no),
  check (completed_at >= started_at),
  foreign key (record_id) references public.records(record_id)
    on delete restrict,
  foreign key (record_id, delivery_id, notification_id, recipient_user_id, record_fence_epoch)
    references public.record_notification_deliveries(record_id, delivery_id, notification_id, recipient_user_id, record_fence_epoch)
    on delete restrict
);

create index if not exists idx_record_notification_delivery_attempts_delivery
  on public.record_notification_delivery_attempts(delivery_id, attempt_no, attempt_id);

create table if not exists public.record_collaboration_purge_receipts (
  operation_id text primary key check (operation_id ~ '^rpo_[a-z0-9]{1,64}$'),
  adapter_name text not null default 'record_collaboration'
    check (adapter_name = 'record_collaboration'),
  removed_surface_digest bytea not null
    check (octet_length(removed_surface_digest) = 32),
  receipt_digest bytea not null check (octet_length(receipt_digest) = 32),
  removed_row_count bigint not null check (removed_row_count >= 0),
  verified_absent_at timestamptz not null,
  created_at timestamptz not null default now(),
  foreign key (operation_id) references public.record_purge_operations(operation_id)
    on delete restrict
);

create or replace function record_platform_internal.enforce_record_comment_mutation()
returns trigger
language plpgsql
security invoker
set search_path = pg_catalog
as $$
begin
  if tg_table_schema <> 'public' or tg_table_name <> 'record_comments' then
    raise exception using
      errcode = '55000',
      message = 'record comment mutation guard attached to unexpected relation';
  end if;

  if tg_op = 'INSERT' then
    if new.comment_state <> 'active' then
      raise exception using
        errcode = '55000',
        message = 'record comment must be inserted active';
    end if;
    return new;
  end if;

  if tg_op <> 'UPDATE'
    or new.comment_id is distinct from old.comment_id
    or new.project_id is distinct from old.project_id
    or new.record_id is distinct from old.record_id
    or new.author_id is distinct from old.author_id
    or new.created_at is distinct from old.created_at
    or new.comment_version <> old.comment_version + 1
    or new.updated_at < old.updated_at
    or old.comment_state = 'redacted' then
    raise exception using
      errcode = '55000',
      message = 'invalid record comment mutation';
  end if;

  if new.comment_state = 'active' then
    if old.comment_state <> 'active'
      or new.tombstone_id is not null
      or new.redacted_at is not null then
      raise exception using
        errcode = '55000',
        message = 'invalid active record comment mutation';
    end if;
    return new;
  end if;

  if new.comment_state <> 'redacted'
    or exists (select 1 from public.record_comment_revisions as revision where revision.record_id = new.record_id and revision.comment_id = new.comment_id and revision.redacted_at is null)
    or not exists (
      select 1
      from public.record_comment_tombstones as tombstone
      where tombstone.tombstone_id = new.tombstone_id
        and tombstone.record_id = new.record_id
        and tombstone.comment_id = new.comment_id
        and tombstone.tombstone_version = new.comment_version
        and tombstone.deleted_at = new.redacted_at
        and tombstone.record_fence_epoch = new.record_fence_epoch
    ) then
    raise exception using
      errcode = '55000',
      message = 'record comment redaction tombstone is missing';
  end if;
  return new;
end
$$;

revoke all on function record_platform_internal.enforce_record_comment_mutation() from public;

create or replace function record_platform_internal.enforce_record_comment_revision_mutation()
returns trigger
language plpgsql
security invoker
set search_path = pg_catalog
as $$
begin
  if tg_table_schema <> 'public' or tg_table_name <> 'record_comment_revisions' then
    raise exception using
      errcode = '55000',
      message = 'record comment revision mutation guard attached to unexpected relation';
  end if;

  if tg_op = 'INSERT' then
    perform 1 from public.record_comments as comment where comment.record_id = new.record_id and comment.comment_id = new.comment_id and comment.project_id = new.project_id and comment.comment_state = 'active' for update;
    if not found or new.redacted_at is not null then
      raise exception using
        errcode = '55000',
        message = 'record comment revision requires an active parent';
    end if;
    return new;
  end if;

  if tg_op <> 'UPDATE'
    or new.comment_revision_id is distinct from old.comment_revision_id
    or new.project_id is distinct from old.project_id
    or new.record_id is distinct from old.record_id
    or new.comment_id is distinct from old.comment_id
    or new.comment_version is distinct from old.comment_version
    or new.edited_by is distinct from old.edited_by
    or new.record_fence_epoch is distinct from old.record_fence_epoch
    or new.created_at is distinct from old.created_at
    or old.redacted_at is not null
    or new.redacted_at is null
    or not exists (
      select 1
      from public.record_comment_tombstones as tombstone
      where tombstone.tombstone_id = new.tombstone_id
        and tombstone.record_id = new.record_id
        and tombstone.comment_id = new.comment_id
        and tombstone.tombstone_version >= new.comment_version
        and tombstone.deleted_at = new.redacted_at
        and tombstone.record_fence_epoch = new.record_fence_epoch
    ) then
    raise exception using
      errcode = '55000',
      message = 'invalid record comment revision redaction';
  end if;
  return new;
end
$$;

revoke all on function record_platform_internal.enforce_record_comment_revision_mutation() from public;

drop trigger if exists record_comments_enforce_insert on public.record_comments;
create trigger record_comments_enforce_insert before insert on public.record_comments
for each row execute function record_platform_internal.enforce_record_comment_mutation();
drop trigger if exists record_comments_enforce_mutation on public.record_comments;
create trigger record_comments_enforce_mutation before update on public.record_comments
for each row execute function record_platform_internal.enforce_record_comment_mutation();
drop trigger if exists record_comment_revisions_enforce_insert on public.record_comment_revisions;
create trigger record_comment_revisions_enforce_insert before insert on public.record_comment_revisions
for each row execute function record_platform_internal.enforce_record_comment_revision_mutation();
drop trigger if exists record_comment_revisions_enforce_mutation on public.record_comment_revisions;
create trigger record_comment_revisions_enforce_mutation before update on public.record_comment_revisions
for each row execute function record_platform_internal.enforce_record_comment_revision_mutation();

drop trigger if exists record_action_events_reject_update on public.record_action_events;
create trigger record_action_events_reject_update before update on public.record_action_events
for each row execute function record_platform_internal.reject_immutable_mutation();
drop trigger if exists record_comment_tombstones_reject_update on public.record_comment_tombstones;
create trigger record_comment_tombstones_reject_update before update on public.record_comment_tombstones
for each row execute function record_platform_internal.reject_immutable_mutation();
drop trigger if exists record_comment_replies_reject_update on public.record_comment_replies;
create trigger record_comment_replies_reject_update before update on public.record_comment_replies
for each row execute function record_platform_internal.reject_immutable_mutation();
drop trigger if exists record_comment_mentions_reject_update on public.record_comment_mentions;
create trigger record_comment_mentions_reject_update before update on public.record_comment_mentions
for each row execute function record_platform_internal.reject_immutable_mutation();
drop trigger if exists record_notifications_reject_update on public.record_notifications;
create trigger record_notifications_reject_update before update on public.record_notifications
for each row execute function record_platform_internal.reject_immutable_mutation();
drop trigger if exists record_notification_delivery_attempts_reject_update on public.record_notification_delivery_attempts;
create trigger record_notification_delivery_attempts_reject_update before update on public.record_notification_delivery_attempts
for each row execute function record_platform_internal.reject_immutable_mutation();
drop trigger if exists record_collaboration_purge_receipts_reject_update on public.record_collaboration_purge_receipts;
create trigger record_collaboration_purge_receipts_reject_update before update on public.record_collaboration_purge_receipts
for each row execute function record_platform_internal.reject_immutable_mutation();
