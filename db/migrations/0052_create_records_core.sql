create table if not exists public.records (
  record_id text primary key check (record_id ~ '^rec_[a-z0-9]{1,64}$'),
  project_id text not null default 'default' check (project_id = 'default'),
  lifecycle text not null default 'active'
    check (lifecycle in ('active', 'archived')),
  current_revision_id text,
  current_title text,
  current_record_type text,
  current_business_status text,
  current_status_group text,
  current_impact_level text,
  current_occurred_at timestamptz,
  current_completed_at timestamptz,
  current_visibility_scope jsonb,
  current_visibility_digest bytea
    check (octet_length(current_visibility_digest) = 32),
  current_owner_id text,
  current_follow_up_at timestamptz,
  lock_version bigint not null default 0 check (lock_version >= 0),
  authorization_epoch bigint not null default 0 check (authorization_epoch >= 0),
  archived_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  check ((lifecycle = 'archived') = (archived_at is not null)),
  check ((current_revision_id is null) = (current_title is null)),
  check ((current_revision_id is null) = (current_record_type is null)),
  check ((current_revision_id is null) = (current_impact_level is null)),
  check ((current_revision_id is null) = (current_visibility_scope is null)),
  check ((current_revision_id is null) = (current_visibility_digest is null)),
  check (current_visibility_scope is null
    or jsonb_typeof(current_visibility_scope) = 'object')
);

create table if not exists public.record_revisions (
  revision_id text primary key check (revision_id ~ '^rrv_[a-z0-9]{1,64}$'),
  record_id text not null,
  project_id text not null default 'default' check (project_id = 'default'),
  base_revision_id text,
  revision_no bigint not null check (revision_no > 0),
  title text not null,
  body_markdown text not null,
  markdown_dialect_version bigint not null
    check (markdown_dialect_version > 0),
  record_type text not null
    check (record_type in ('troubleshooting', 'maintenance', 'migration',
      'provider_communication', 'billing', 'important_finding', 'note')),
  business_status text
    check (business_status is null or business_status ~ '^[a-z0-9_]{1,64}$'),
  status_group text
    check (status_group is null or status_group in ('pending', 'in_progress',
      'waiting', 'verification', 'completed', 'cancelled')),
  impact_level text not null
    check (impact_level ~ '^[a-z0-9_]{1,64}$'),
  occurred_at timestamptz,
  completed_at timestamptz,
  visibility_scope jsonb not null,
  visibility_digest bytea not null
    check (octet_length(visibility_digest) = 32),
  owner_id text,
  follow_up_at timestamptz,
  template_id text,
  template_version bigint,
  author_id text not null,
  save_reason text not null default '',
  canonical_hash bytea not null check (octet_length(canonical_hash) = 32),
  created_at timestamptz not null default now(),
  unique (record_id, revision_id),
  unique (record_id, revision_no),
  check (jsonb_typeof(visibility_scope) = 'object'),
  check ((template_id is null) = (template_version is null)),
  check (template_version is null or template_version > 0),
  check (completed_at is null or occurred_at is null or completed_at >= occurred_at),
  foreign key (record_id) references public.records(record_id)
    on delete restrict,
  foreign key (record_id, base_revision_id)
    references public.record_revisions(record_id, revision_id)
    on delete restrict
);

create index if not exists idx_record_revisions_record_hash
  on public.record_revisions(record_id, canonical_hash);

create table if not exists public.record_revision_subjects (
  revision_id text not null,
  ordinal bigint not null check (ordinal >= 0),
  registry_version bigint not null check (registry_version > 0),
  subject_kind text not null,
  relation_role text not null,
  source_id text not null,
  is_primary boolean not null,
  identity_snapshot jsonb not null,
  capture_authorization jsonb not null,
  capture_authorization_digest bytea not null
    check (octet_length(capture_authorization_digest) = 32),
  primary key (revision_id, ordinal),
  unique (revision_id, subject_kind, source_id, relation_role),
  check (subject_kind in ('vps', 'monitoring_instance', 'target')),
  check (relation_role in ('affected', 'context', 'evidence_source')),
  check (source_id ~ '^[a-z0-9_-]{1,128}$'),
  check (jsonb_typeof(identity_snapshot) = 'object'),
  check (jsonb_typeof(capture_authorization) = 'object'),
  foreign key (revision_id) references public.record_revisions(revision_id)
    on delete restrict
);

create unique index if not exists uq_record_revision_subjects_primary
  on public.record_revision_subjects(revision_id)
  where is_primary;

create table if not exists public.record_revision_tags (
  revision_id text not null,
  ordinal bigint not null check (ordinal >= 0),
  tag_value text not null check (char_length(tag_value) between 1 and 64),
  primary key (revision_id, ordinal),
  unique (revision_id, tag_value),
  foreign key (revision_id) references public.record_revisions(revision_id)
    on delete restrict
);

create table if not exists public.record_revision_participants (
  revision_id text not null,
  ordinal bigint not null check (ordinal >= 0),
  participant_id text not null,
  identity_snapshot jsonb not null,
  primary key (revision_id, ordinal),
  unique (revision_id, participant_id),
  check (participant_id ~ '^usr_[a-z0-9]{1,64}$'),
  check (jsonb_typeof(identity_snapshot) = 'object'),
  foreign key (revision_id) references public.record_revisions(revision_id)
    on delete restrict
);

create table if not exists public.record_drafts (
  draft_id text primary key check (draft_id ~ '^rdf_[a-z0-9]{1,64}$'),
  project_id text not null default 'default' check (project_id = 'default'),
  record_id text,
  base_revision_id text,
  author_id text not null check (author_id ~ '^usr_[a-z0-9]{1,64}$'),
  payload jsonb not null,
  payload_hash bytea not null check (octet_length(payload_hash) = 32),
  draft_version bigint not null check (draft_version > 0),
  etag_digest bytea not null check (octet_length(etag_digest) = 32),
  warning_at timestamptz not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  expires_at timestamptz not null,
  foreign key (record_id) references public.records(record_id)
    on delete restrict,
  foreign key (record_id, base_revision_id)
    references public.record_revisions(record_id, revision_id)
    on delete restrict,
  check (jsonb_typeof(payload) = 'object'),
  check ((record_id is null) = (base_revision_id is null)),
  check (warning_at >= updated_at and warning_at <= expires_at),
  check (expires_at > updated_at)
);

create unique index if not exists uq_record_drafts_record_author
  on public.record_drafts(record_id, author_id)
  where record_id is not null;

create index if not exists idx_record_drafts_author_activity
  on public.record_drafts(author_id, updated_at desc, draft_id);

create index if not exists idx_record_drafts_expiry
  on public.record_drafts(expires_at, draft_id);

create table if not exists public.record_draft_checkpoints (
  checkpoint_id text primary key check (checkpoint_id ~ '^rdc_[a-z0-9]{1,64}$'),
  draft_id text not null,
  checkpoint_bucket timestamptz not null,
  checkpoint_payload jsonb not null,
  checkpoint_payload_hash bytea not null
    check (octet_length(checkpoint_payload_hash) = 32),
  checkpoint_draft_version bigint not null
    check (checkpoint_draft_version > 0),
  created_at timestamptz not null default now(),
  checkpoint_expires_at timestamptz not null,
  unique (draft_id, checkpoint_bucket),
  check (jsonb_typeof(checkpoint_payload) = 'object'),
  check (checkpoint_expires_at > created_at),
  foreign key (draft_id) references public.record_drafts(draft_id)
    on delete restrict
);

create index if not exists idx_record_draft_checkpoints_retention
  on public.record_draft_checkpoints(draft_id, created_at desc, checkpoint_id);

create index if not exists idx_record_draft_checkpoints_expiry
  on public.record_draft_checkpoints(checkpoint_expires_at, checkpoint_id);

create table if not exists public.record_domain_activities (
  activity_id text primary key check (activity_id ~ '^rac_[a-z0-9]{1,64}$'),
  project_id text not null default 'default' check (project_id = 'default'),
  record_id text not null,
  revision_id text,
  event_kind text not null check (event_kind ~ '^[a-z0-9_]{1,64}$'),
  source_event_id text not null,
  source_version bigint not null check (source_version > 0),
  actor_id text not null check (actor_id ~ '^usr_[a-z0-9]{1,64}$'),
  authorization_epoch bigint not null check (authorization_epoch >= 0),
  record_lock_version bigint not null check (record_lock_version > 0),
  event_at timestamptz not null,
  recorded_at timestamptz not null default now(),
  unique (project_id, event_kind, source_event_id),
  foreign key (record_id) references public.records(record_id)
    on delete restrict,
  foreign key (record_id, revision_id)
    references public.record_revisions(record_id, revision_id)
    on delete restrict
);

create index if not exists idx_record_domain_activities_record_time
  on public.record_domain_activities(record_id, event_at desc, activity_id);

create table if not exists public.record_core_purge_receipts (
  operation_id text primary key check (operation_id ~ '^rpo_[a-z0-9]{1,64}$'),
  adapter_name text not null default 'record_core'
    check (adapter_name = 'record_core'),
  project_id text not null default 'default' check (project_id = 'default'),
  record_id text not null check (record_id ~ '^rec_[a-z0-9]{1,64}$'),
  removed_surface_digest bytea not null
    check (octet_length(removed_surface_digest) = 32),
  receipt_digest bytea not null check (octet_length(receipt_digest) = 32),
  removed_row_count bigint not null check (removed_row_count >= 0),
  verified_absent_at timestamptz not null,
  created_at timestamptz not null default now(),
  foreign key (operation_id) references public.record_purge_operations(operation_id)
    on delete restrict
);

do $records_core_current_revision_fk$
begin
  if not exists (
    select 1
    from pg_catalog.pg_constraint
    where conname = 'records_current_revision_same_record_fk'
      and conrelid = 'public.records'::regclass
  ) then
    alter table public.records
      add constraint records_current_revision_same_record_fk
      foreign key (record_id, current_revision_id)
      references public.record_revisions(record_id, revision_id)
      on delete restrict
      deferrable initially deferred;
  end if;
end
$records_core_current_revision_fk$;

create or replace function record_platform_internal.validate_record_revision_primary_subject()
returns trigger
language plpgsql
security invoker
set search_path = pg_catalog
as $$
declare
  checked_revision_id text;
  primary_subject_count bigint;
begin
  if tg_table_schema <> 'public' then
    raise exception using
      errcode = '55000',
      message = 'record revision primary-subject validator attached to unexpected schema';
  end if;

  if tg_table_name = 'record_revisions' and tg_op = 'INSERT' then
    checked_revision_id := new.revision_id;
  elsif tg_table_name = 'record_revision_subjects' and tg_op = 'DELETE' then
    checked_revision_id := old.revision_id;
  elsif tg_table_name = 'record_revision_subjects' and tg_op = 'INSERT' then
    checked_revision_id := new.revision_id;
  else
    raise exception using
      errcode = '55000',
      message = 'record revision primary-subject validator received unexpected operation';
  end if;

  if not exists (
    select 1
    from public.record_revisions
    where revision_id = checked_revision_id
  ) then
    return null;
  end if;

  select count(*)
  into primary_subject_count
  from public.record_revision_subjects
  where revision_id = checked_revision_id
    and is_primary;

  if primary_subject_count <> 1 then
    raise exception using
      errcode = '23514',
      message = 'record revision must have exactly one primary subject';
  end if;
  return null;
end
$$;

revoke all on function record_platform_internal.validate_record_revision_primary_subject()
  from public;

drop trigger if exists record_revisions_require_primary_subject
  on public.record_revisions;
create constraint trigger record_revisions_require_primary_subject
after insert on public.record_revisions
deferrable initially deferred
for each row execute function record_platform_internal.validate_record_revision_primary_subject();

drop trigger if exists record_revision_subjects_require_primary_subject
  on public.record_revision_subjects;
create constraint trigger record_revision_subjects_require_primary_subject
after insert or delete on public.record_revision_subjects
deferrable initially deferred
for each row execute function record_platform_internal.validate_record_revision_primary_subject();

drop trigger if exists record_revisions_reject_update
  on public.record_revisions;
create trigger record_revisions_reject_update
before update on public.record_revisions
for each row execute function record_platform_internal.reject_immutable_mutation();

drop trigger if exists record_revision_subjects_reject_update
  on public.record_revision_subjects;
create trigger record_revision_subjects_reject_update
before update on public.record_revision_subjects
for each row execute function record_platform_internal.reject_immutable_mutation();

drop trigger if exists record_revision_tags_reject_update
  on public.record_revision_tags;
create trigger record_revision_tags_reject_update
before update on public.record_revision_tags
for each row execute function record_platform_internal.reject_immutable_mutation();

drop trigger if exists record_revision_participants_reject_update
  on public.record_revision_participants;
create trigger record_revision_participants_reject_update
before update on public.record_revision_participants
for each row execute function record_platform_internal.reject_immutable_mutation();

drop trigger if exists record_draft_checkpoints_reject_update
  on public.record_draft_checkpoints;
create trigger record_draft_checkpoints_reject_update
before update on public.record_draft_checkpoints
for each row execute function record_platform_internal.reject_immutable_mutation();

drop trigger if exists record_domain_activities_reject_update
  on public.record_domain_activities;
create trigger record_domain_activities_reject_update
before update on public.record_domain_activities
for each row execute function record_platform_internal.reject_immutable_mutation();

drop trigger if exists record_core_purge_receipts_reject_update
  on public.record_core_purge_receipts;
create trigger record_core_purge_receipts_reject_update
before update on public.record_core_purge_receipts
for each row execute function record_platform_internal.reject_immutable_mutation();
