create extension if not exists pg_trgm with schema record_platform_internal;

create table if not exists public.record_search_generations (
  generation bigint primary key check (generation > 0),
  project_id text not null default 'default' check (project_id = 'default'),
  generation_state text not null default 'building'
    check (generation_state in ('building', 'published', 'superseded', 'failed')),
  document_count bigint not null default 0 check (document_count >= 0),
  coverage_digest bytea
    check (coverage_digest is null or octet_length(coverage_digest) = 32),
  failure_reason text not null default ''
    check (failure_reason = '' or failure_reason ~ '^[a-z0-9_]{1,64}$'),
  started_at timestamptz not null default now(),
  published_at timestamptz,
  superseded_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  check (generation_state <> 'published' or published_at is not null),
  check (published_at is null or generation_state in ('published', 'superseded')),
  check ((generation_state = 'superseded') = (superseded_at is not null)),
  check ((generation_state = 'failed') = (failure_reason <> '')),
  check (published_at is null or published_at >= started_at),
  check (superseded_at is null or superseded_at >= started_at),
  check (superseded_at is null or published_at is null or superseded_at >= published_at),
  check (updated_at >= created_at)
);

create unique index if not exists uq_record_search_generations_published
  on public.record_search_generations ((true))
  where generation_state = 'published';
create unique index if not exists uq_record_search_generations_building
  on public.record_search_generations ((true))
  where generation_state = 'building';

create table if not exists public.record_search_documents (
  generation bigint not null,
  record_id text not null check (record_id ~ '^rec_[a-z0-9]{1,64}$'),
  project_id text not null default 'default' check (project_id = 'default'),
  current_revision_id text not null check (current_revision_id ~ '^rrv_[a-z0-9]{1,64}$'),
  record_lock_version bigint not null check (record_lock_version >= 0),
  authorization_epoch bigint not null check (authorization_epoch >= 0),
  record_fence_epoch bigint not null check (record_fence_epoch >= 0),
  lifecycle text not null check (lifecycle in ('active', 'archived')),
  record_type text not null
    check (record_type in ('troubleshooting', 'maintenance', 'migration',
      'provider_communication', 'billing', 'important_finding', 'note')),
  business_status text
    check (business_status is null or business_status ~ '^[a-z0-9_]{1,64}$'),
  status_group text
    check (status_group is null or status_group in ('pending', 'in_progress',
      'waiting', 'verification', 'completed', 'cancelled')),
  impact_level text not null check (impact_level ~ '^[a-z0-9_]{1,64}$'),
  owner_id text check (owner_id is null or owner_id ~ '^usr_[a-z0-9]{1,64}$'),
  title text not null check (char_length(title) between 1 and 512),
  plain_text text not null default '' check (octet_length(plain_text) <= 65536),
  search_text text generated always as (lower(title || ' ' || plain_text)) stored,
  tags text[] not null default '{}'::text[]
    check (array_position(tags, null) is null and cardinality(tags) <= 64),
  participant_ids text[] not null default '{}'::text[]
    check (array_position(participant_ids, null) is null and cardinality(participant_ids) <= 512),
  visibility_kind text not null check (visibility_kind in ('project', 'restricted')),
  visibility_digest bytea not null check (octet_length(visibility_digest) = 32),
  open_action_count bigint not null default 0 check (open_action_count >= 0),
  next_action_due_at timestamptz,
  occurred_at timestamptz,
  completed_at timestamptz,
  follow_up_at timestamptz,
  record_created_at timestamptz not null,
  record_updated_at timestamptz not null,
  document_digest bytea not null check (octet_length(document_digest) = 32),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  primary key (generation, record_id),
  check (record_updated_at >= record_created_at),
  check (updated_at >= created_at),
  check ((next_action_due_at is null) or open_action_count > 0),
  foreign key (generation) references public.record_search_generations(generation)
    on delete restrict,
  foreign key (record_id) references public.records(record_id)
    on delete restrict
);

create index if not exists idx_record_search_documents_updated
  on public.record_search_documents(generation, lifecycle, record_updated_at desc, record_id);
create index if not exists idx_record_search_documents_type_updated
  on public.record_search_documents(generation, record_type, record_updated_at desc, record_id);
create index if not exists idx_record_search_documents_status_group_updated
  on public.record_search_documents(generation, status_group, record_updated_at desc, record_id)
  where status_group is not null;
create index if not exists idx_record_search_documents_owner_updated
  on public.record_search_documents(generation, owner_id, record_updated_at desc, record_id)
  where owner_id is not null;
create index if not exists idx_record_search_documents_follow_up
  on public.record_search_documents(generation, follow_up_at, record_id)
  where follow_up_at is not null;
create index if not exists idx_record_search_documents_search_text
  on public.record_search_documents using gin (search_text record_platform_internal.gin_trgm_ops);
create index if not exists idx_record_search_documents_tags
  on public.record_search_documents using gin (tags);
create index if not exists idx_record_search_documents_participants
  on public.record_search_documents using gin (participant_ids);

create table if not exists public.record_search_subjects (
  generation bigint not null,
  record_id text not null,
  subject_kind text not null
    check (subject_kind in ('vps', 'monitoring_instance', 'target')),
  relation_role text not null
    check (relation_role in ('affected', 'context', 'evidence_source')),
  source_id text not null check (source_id ~ '^[a-z0-9_-]{1,128}$'),
  is_primary boolean not null,
  created_at timestamptz not null default now(),
  primary key (generation, record_id, subject_kind, source_id, relation_role),
  foreign key (generation, record_id)
    references public.record_search_documents(generation, record_id)
    on delete cascade
);

create index if not exists idx_record_search_subjects_source
  on public.record_search_subjects(generation, subject_kind, source_id, relation_role, record_id);
create index if not exists idx_record_search_subjects_primary
  on public.record_search_subjects(generation, subject_kind, source_id, record_id)
  where is_primary;

create table if not exists public.record_search_rebuild_jobs (
  job_id text primary key check (job_id ~ '^rsj_[a-z0-9]{1,64}$'),
  generation bigint not null,
  project_id text not null default 'default' check (project_id = 'default'),
  job_state text not null default 'running'
    check (job_state in ('running', 'completed', 'failed', 'cancelled')),
  owner_id text not null check (owner_id ~ '^[a-z0-9_-]{1,128}$'),
  lease_expires_at timestamptz not null,
  resume_after_record_id text
    check (resume_after_record_id is null or resume_after_record_id ~ '^rec_[a-z0-9]{1,64}$'),
  processed_count bigint not null default 0 check (processed_count >= 0),
  failure_reason text not null default ''
    check (failure_reason = '' or failure_reason ~ '^[a-z0-9_]{1,64}$'),
  started_at timestamptz not null default now(),
  finished_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  check ((job_state = 'running') = (finished_at is null)),
  check ((job_state = 'failed') = (failure_reason <> '')),
  check (finished_at is null or finished_at >= started_at),
  check (updated_at >= created_at),
  foreign key (generation) references public.record_search_generations(generation)
    on delete restrict
);

create index if not exists idx_record_search_rebuild_jobs_generation
  on public.record_search_rebuild_jobs(generation, job_state, job_id);

create table if not exists public.record_search_purge_receipts (
  operation_id text primary key check (operation_id ~ '^rpo_[a-z0-9]{1,64}$'),
  adapter_name text not null default 'record_search'
    check (adapter_name = 'record_search'),
  removed_surface_digest bytea not null
    check (octet_length(removed_surface_digest) = 32),
  receipt_digest bytea not null check (octet_length(receipt_digest) = 32),
  removed_row_count bigint not null check (removed_row_count >= 0),
  verified_absent_at timestamptz not null,
  created_at timestamptz not null default now(),
  foreign key (operation_id) references public.record_purge_operations(operation_id)
    on delete restrict
);

create or replace function record_platform_internal.purge_record_search(
  text, text, text, text, bigint, bigint, bytea
)
returns bigint
language plpgsql
security definer
set search_path = pg_catalog
as $$
declare
  p_operation_id alias for $1;
  p_reservation_id alias for $2;
  p_project_id alias for $3;
  p_record_id alias for $4;
  p_fence_epoch alias for $5;
  p_ledger_sequence alias for $6;
  p_ledger_entry_hash alias for $7;
  v_removed bigint := 0;
  v_rows bigint;
  v_remaining bigint;
begin
  if p_operation_id is null or p_reservation_id is null or p_project_id <> 'default'
    or p_record_id is null or p_fence_epoch < 0 or p_ledger_sequence <= 0
    or octet_length(p_ledger_entry_hash) <> 32 then
    raise exception using errcode = '55000', message = 'invalid search purge authority';
  end if;

  perform 1
  from public.record_purge_operations as operation
  join public.deletion_reservations as reservation
    on reservation.reservation_id = operation.reservation_id
  where operation.operation_id = p_operation_id
    and operation.reservation_id = p_reservation_id
    and operation.project_id = p_project_id
    and operation.operation_state = 'online_purging'
    and operation.ledger_sequence = p_ledger_sequence
    and operation.ledger_entry_hash = p_ledger_entry_hash
    and reservation.project_id = p_project_id
    and reservation.object_kind = 'record'
    and reservation.object_id = p_record_id
    and reservation.state = 'committed'
    and reservation.fence_epoch = p_fence_epoch
  for update of operation, reservation;
  if not found then
    raise exception using errcode = '55000', message = 'search purge authority unavailable';
  end if;

  delete from public.record_search_subjects where record_id = p_record_id;
  get diagnostics v_rows = row_count; v_removed := v_removed + v_rows;
  delete from public.record_search_documents where record_id = p_record_id;
  get diagnostics v_rows = row_count; v_removed := v_removed + v_rows;

  select
    (select count(*) from public.record_search_subjects where record_id = p_record_id) +
    (select count(*) from public.record_search_documents where record_id = p_record_id)
  into v_remaining;
  if v_remaining <> 0 then
    raise exception using errcode = '55000', message = 'search purge left owned rows';
  end if;
  return v_removed;
end
$$;

revoke all on function record_platform_internal.purge_record_search(text,text,text,text,bigint,bigint,bytea) from public;

create or replace function public.record_search_purge(bytea)
returns bigint
language plpgsql
security definer
set search_path = pg_catalog
as $$
declare p_command alias for $1; v jsonb;
begin
  if p_command is null or octet_length(p_command) not between 1 and 4096 then
    raise exception using errcode = '55000', message = 'invalid search purge command';
  end if;
  v := convert_from(p_command, 'UTF8')::jsonb;
  if jsonb_typeof(v) <> 'object' or
    array(select key from jsonb_object_keys(v) as key order by key) <>
    array['fence_epoch','ledger_entry_hash','ledger_sequence','operation_id','project_id','record_id','reservation_id']::text[] then
    raise exception using errcode = '55000', message = 'invalid search purge command';
  end if;
  return record_platform_internal.purge_record_search(
    v->>'operation_id', v->>'reservation_id', v->>'project_id', v->>'record_id',
    (v->>'fence_epoch')::bigint, (v->>'ledger_sequence')::bigint,
    decode(v->>'ledger_entry_hash', 'hex')
  );
end
$$;
revoke all on function public.record_search_purge(bytea) from public;

create or replace function record_platform_internal.retire_record_search_generation(bigint)
returns bigint
language plpgsql
security definer
set search_path = pg_catalog
as $$
declare
  p_generation alias for $1;
  v_removed bigint := 0;
  v_rows bigint;
begin
  if p_generation is null or p_generation <= 0 then
    raise exception using errcode = '55000', message = 'invalid search generation retirement';
  end if;

  perform 1
  from public.record_search_generations as generation
  where generation.generation = p_generation
    and generation.project_id = 'default'
    and generation.generation_state in ('superseded', 'failed')
  for update;
  if not found then
    raise exception using errcode = '55000', message = 'search generation is not retirable';
  end if;

  if exists (
    select 1 from public.record_search_rebuild_jobs as job
    where job.generation = p_generation and job.job_state = 'running'
  ) then
    raise exception using errcode = '55000', message = 'search generation still has a running rebuild job';
  end if;

  delete from public.record_search_subjects where generation = p_generation;
  get diagnostics v_rows = row_count; v_removed := v_removed + v_rows;
  delete from public.record_search_documents where generation = p_generation;
  get diagnostics v_rows = row_count; v_removed := v_removed + v_rows;
  delete from public.record_search_rebuild_jobs where generation = p_generation;
  get diagnostics v_rows = row_count; v_removed := v_removed + v_rows;
  delete from public.record_search_generations where generation = p_generation;
  get diagnostics v_rows = row_count; v_removed := v_removed + v_rows;
  return v_removed;
end
$$;

revoke all on function record_platform_internal.retire_record_search_generation(bigint) from public;

create or replace function public.record_search_retire_generation(bytea)
returns bigint
language plpgsql
security definer
set search_path = pg_catalog
as $$
declare p_command alias for $1; v jsonb;
begin
  if p_command is null or octet_length(p_command) not between 1 and 4096 then
    raise exception using errcode = '55000', message = 'invalid search generation retirement command';
  end if;
  v := convert_from(p_command, 'UTF8')::jsonb;
  if jsonb_typeof(v) <> 'object' or
    array(select key from jsonb_object_keys(v) as key order by key) <>
    array['generation']::text[] then
    raise exception using errcode = '55000', message = 'invalid search generation retirement command';
  end if;
  return record_platform_internal.retire_record_search_generation((v->>'generation')::bigint);
end
$$;
revoke all on function public.record_search_retire_generation(bytea) from public;

drop trigger if exists record_search_purge_receipts_reject_update on public.record_search_purge_receipts;
create trigger record_search_purge_receipts_reject_update before update on public.record_search_purge_receipts
for each row execute function record_platform_internal.reject_immutable_mutation();
