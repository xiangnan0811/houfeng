create table if not exists public.record_export_jobs (
  export_job_id text primary key check (export_job_id ~ '^rej_[a-z0-9]{1,64}$'),
  project_id text not null default 'default' check (project_id = 'default'),
  actor_id text not null check (actor_id ~ '^usr_[a-z0-9]{1,64}$'),
  idempotency_key text not null check (idempotency_key ~ '^[a-z0-9._:-]{1,128}$'),
  export_kind text not null
    check (export_kind in ('markdown', 'comparison_json', 'evidence_json', 'archive', 'pdf')),
  export_mode text not null
    check (export_mode in ('safe', 'sensitive_topology')),
  job_state text not null
    check (job_state in ('previewed', 'staging', 'published', 'expired', 'revoked', 'failed')),
  failure_code text not null default '' check (failure_code ~ '^[a-z0-9_]{0,64}$'),
  lock_version bigint not null default 1 check (lock_version > 0),
  request_fingerprint bytea not null check (octet_length(request_fingerprint) = 32),
  inventory_digest bytea not null check (octet_length(inventory_digest) = 32),
  authorization_epoch bigint not null check (authorization_epoch > 0),
  record_id text check (record_id is null or record_id ~ '^rec_[a-z0-9]{1,64}$'),
  revision_id text check (revision_id is null or revision_id ~ '^rrv_[a-z0-9]{1,64}$'),
  owner_id text not null default '' check (owner_id ~ '^[a-z0-9_-]{0,128}$'),
  owner_generation bigint not null default 0 check (owner_generation >= 0),
  lease_expires_at timestamptz,
  expires_at timestamptz not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (project_id, actor_id, idempotency_key),
  check (expires_at > created_at),
  check (updated_at >= created_at),
  check (revision_id is null or record_id is not null),
  check (
    (owner_id = '' and owner_generation = 0 and lease_expires_at is null)
    or (owner_id <> '' and owner_generation > 0 and lease_expires_at is not null)
  )
);

create index if not exists idx_record_export_jobs_expiry
  on public.record_export_jobs(job_state, expires_at, export_job_id);

create table if not exists public.record_export_artifacts (
  export_artifact_id text primary key check (export_artifact_id ~ '^rxa_[a-z0-9]{1,64}$'),
  export_job_id text not null references public.record_export_jobs(export_job_id) on delete restrict,
  artifact_kind text not null
    check (artifact_kind in ('markdown', 'comparison_json', 'evidence_json', 'archive', 'pdf')),
  content_type text not null
    check (content_type in ('text/markdown', 'application/json', 'application/zip', 'application/pdf')),
  backend_kind text not null check (backend_kind in ('local', 's3')),
  blob_key text not null check (blob_key ~ '^[a-z0-9/._-]{1,512}$' and blob_key not like '%..%'),
  sha256 bytea not null check (octet_length(sha256) = 32),
  byte_size bigint not null check (byte_size > 0 and byte_size <= 1073741824),
  expires_at timestamptz not null,
  revoked_at timestamptz,
  created_at timestamptz not null default now(),
  unique (export_job_id, artifact_kind),
  check (expires_at > created_at),
  check (revoked_at is null or revoked_at >= created_at),
  check (
    (artifact_kind = 'markdown' and content_type = 'text/markdown')
    or (artifact_kind in ('comparison_json', 'evidence_json') and content_type = 'application/json')
    or (artifact_kind = 'archive' and content_type = 'application/zip')
    or (artifact_kind = 'pdf' and content_type = 'application/pdf')
  )
);

create index if not exists idx_record_export_artifacts_expiry
  on public.record_export_artifacts(expires_at, export_artifact_id);

create table if not exists public.record_import_jobs (
  import_job_id text primary key check (import_job_id ~ '^rij_[a-z0-9]{1,64}$'),
  project_id text not null default 'default' check (project_id = 'default'),
  actor_id text not null check (actor_id ~ '^usr_[a-z0-9]{1,64}$'),
  idempotency_key text not null check (idempotency_key ~ '^[a-z0-9._:-]{1,128}$'),
  job_state text not null
    check (job_state in ('uploaded', 'quarantined', 'dry_run', 'planned', 'applying', 'applied', 'failed', 'expired')),
  failure_code text not null default '' check (failure_code ~ '^[a-z0-9_]{0,64}$'),
  identity_classification text not null
    check (identity_classification in ('unknown', 'complete')),
  archive_digest bytea not null check (octet_length(archive_digest) = 32),
  lock_version bigint not null default 1 check (lock_version > 0),
  owner_id text not null default '' check (owner_id ~ '^[a-z0-9_-]{0,128}$'),
  owner_generation bigint not null default 0 check (owner_generation >= 0),
  lease_expires_at timestamptz,
  expires_at timestamptz not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (project_id, actor_id, idempotency_key),
  check (expires_at > created_at),
  check (updated_at >= created_at),
  check (
    (owner_id = '' and owner_generation = 0 and lease_expires_at is null)
    or (owner_id <> '' and owner_generation > 0 and lease_expires_at is not null)
  )
);

create index if not exists idx_record_import_jobs_expiry
  on public.record_import_jobs(job_state, expires_at, import_job_id);

create table if not exists public.record_import_plans (
  import_plan_id text primary key check (import_plan_id ~ '^rip_[a-z0-9]{1,64}$'),
  import_job_id text not null unique references public.record_import_jobs(import_job_id) on delete restrict,
  plan_digest bytea not null check (octet_length(plan_digest) = 32),
  object_count bigint not null check (object_count >= 0),
  remap_count bigint not null check (remap_count >= 0),
  expires_at timestamptz not null,
  created_at timestamptz not null default now(),
  check (expires_at > created_at),
  check (remap_count <= object_count)
);

create table if not exists public.record_import_artifacts (
  import_artifact_id text primary key check (import_artifact_id ~ '^ria_[a-z0-9]{1,64}$'),
  import_job_id text not null references public.record_import_jobs(import_job_id) on delete restrict,
  artifact_role text not null check (artifact_role in ('archive', 'workspace', 'scan')),
  backend_kind text not null check (backend_kind in ('local', 's3')),
  blob_key text not null check (blob_key ~ '^[a-z0-9/._-]{1,512}$' and blob_key not like '%..%'),
  object_version_id text not null check (char_length(object_version_id) between 1 and 1024),
  sha256 bytea not null check (octet_length(sha256) = 32),
  byte_size bigint not null check (byte_size > 0 and byte_size <= 1073741824),
  expires_at timestamptz not null,
  created_at timestamptz not null default now(),
  unique (import_job_id, artifact_role),
  check (expires_at > created_at)
);

create index if not exists idx_record_import_artifacts_expiry
  on public.record_import_artifacts(expires_at, import_artifact_id);

create table if not exists public.record_import_entity_mappings (
  import_plan_id text not null references public.record_import_plans(import_plan_id) on delete restrict,
  entity_kind text not null
    check (entity_kind in ('record', 'revision', 'evidence', 'attachment', 'origin')),
  source_id text not null check (char_length(source_id) between 1 and 128),
  source_identity_digest bytea not null check (octet_length(source_identity_digest) = 32),
  target_id text not null check (target_id ~ '^[a-z0-9_-]{1,128}$'),
  created_at timestamptz not null default now(),
  primary key (import_plan_id, entity_kind, source_identity_digest)
);

create table if not exists public.record_origins (
  origin_id text primary key check (origin_id ~ '^ror_[a-z0-9]{1,64}$'),
  project_id text not null default 'default' check (project_id = 'default'),
  origin_kind text not null check (origin_kind in ('import', 'restore', 'copy')),
  origin_digest bytea not null check (octet_length(origin_digest) = 32),
  source_deployment_id text check (
    source_deployment_id is null or source_deployment_id ~ '^dp-[0-9a-f]{64}$'
  ),
  source_record_id text check (
    source_record_id is null or source_record_id ~ '^rec_[a-z0-9]{1,64}$'
  ),
  created_at timestamptz not null default now(),
  unique (project_id, origin_digest)
);

create table if not exists public.record_origin_tombstones (
  project_id text not null default 'default' check (project_id = 'default'),
  origin_digest bytea not null check (octet_length(origin_digest) = 32),
  ledger_sequence bigint not null check (ledger_sequence > 0),
  tombstoned_at timestamptz not null default now(),
  primary key (project_id, origin_digest)
);

create table if not exists public.record_portability_purge_receipts (
  operation_id text primary key check (operation_id ~ '^rpo_[a-z0-9]{1,64}$'),
  adapter_name text not null default 'record_portability'
    check (adapter_name = 'record_portability'),
  removed_surface_digest bytea not null
    check (octet_length(removed_surface_digest) = 32),
  receipt_digest bytea not null check (octet_length(receipt_digest) = 32),
  removed_row_count bigint not null check (removed_row_count >= 0),
  verified_absent_at timestamptz not null,
  created_at timestamptz not null default now(),
  foreign key (operation_id) references public.record_purge_operations(operation_id)
    on delete restrict
);

create index if not exists idx_record_export_jobs_record
  on public.record_export_jobs(record_id)
  where record_id is not null;

create index if not exists idx_record_origins_source_record
  on public.record_origins(source_record_id)
  where source_record_id is not null;

create index if not exists idx_record_import_entity_mappings_record_target
  on public.record_import_entity_mappings(entity_kind, target_id);

create or replace function record_platform_internal.purge_record_portability(
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
  v_plans text[];
  v_jobs text[];
begin
  if p_operation_id is null or p_reservation_id is null or p_project_id <> 'default'
    or p_record_id is null or p_fence_epoch < 0 or p_ledger_sequence <= 0
    or octet_length(p_ledger_entry_hash) <> 32 then
    raise exception using errcode = '55000', message = 'invalid portability purge authority';
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
    raise exception using errcode = '55000', message = 'portability purge authority unavailable';
  end if;

  insert into public.record_origin_tombstones (
    project_id, origin_digest, ledger_sequence, tombstoned_at
  )
  select distinct job.project_id, job.archive_digest, p_ledger_sequence, now()
  from public.record_import_entity_mappings as mapping
  join public.record_import_plans as plan on plan.import_plan_id = mapping.import_plan_id
  join public.record_import_jobs as job on job.import_job_id = plan.import_job_id
  where mapping.entity_kind = 'record' and mapping.target_id = p_record_id
  on conflict do nothing;

  insert into public.record_origin_tombstones (
    project_id, origin_digest, ledger_sequence, tombstoned_at
  )
  select project_id, origin_digest, p_ledger_sequence, now()
  from public.record_origins
  where project_id = p_project_id and source_record_id = p_record_id
  on conflict do nothing;

  delete from public.record_export_artifacts
  where export_job_id in (
    select export_job_id from public.record_export_jobs
    where project_id = p_project_id and record_id = p_record_id
  );
  get diagnostics v_rows = row_count; v_removed := v_removed + v_rows;

  delete from public.record_export_jobs
  where project_id = p_project_id and record_id = p_record_id;
  get diagnostics v_rows = row_count; v_removed := v_removed + v_rows;

  with deleted as (
    delete from public.record_import_entity_mappings
    where entity_kind = 'record' and target_id = p_record_id
    returning import_plan_id
  )
  select count(*), coalesce(array_agg(distinct import_plan_id), array[]::text[])
  into v_rows, v_plans
  from deleted;
  v_removed := v_removed + v_rows;

  select coalesce(array_agg(distinct plan.import_job_id), array[]::text[])
  into v_jobs
  from public.record_import_plans as plan
  where plan.import_plan_id = any(v_plans)
    and not exists (
      select 1 from public.record_import_entity_mappings as mapping
      where mapping.import_plan_id = plan.import_plan_id
    );

  delete from public.record_import_artifacts
  where import_job_id = any(v_jobs);
  get diagnostics v_rows = row_count; v_removed := v_removed + v_rows;

  delete from public.record_import_plans
  where import_plan_id = any(v_plans)
    and not exists (
      select 1 from public.record_import_entity_mappings as mapping
      where mapping.import_plan_id = record_import_plans.import_plan_id
    );
  get diagnostics v_rows = row_count; v_removed := v_removed + v_rows;

  delete from public.record_import_jobs
  where import_job_id = any(v_jobs);
  get diagnostics v_rows = row_count; v_removed := v_removed + v_rows;

  delete from public.record_origins
  where project_id = p_project_id and source_record_id = p_record_id;
  get diagnostics v_rows = row_count; v_removed := v_removed + v_rows;

  select
    (select count(*) from public.record_export_jobs
      where project_id = p_project_id and record_id = p_record_id) +
    (select count(*) from public.record_export_artifacts as artifact
      join public.record_export_jobs as job on job.export_job_id = artifact.export_job_id
      where job.project_id = p_project_id and job.record_id = p_record_id) +
    (select count(*) from public.record_origins
      where project_id = p_project_id and source_record_id = p_record_id) +
    (select count(*) from public.record_import_entity_mappings
      where entity_kind = 'record' and target_id = p_record_id)
  into v_remaining;
  if v_remaining <> 0 then
    raise exception using errcode = '55000', message = 'portability purge left owned rows';
  end if;
  return v_removed;
end
$$;

revoke all on function record_platform_internal.purge_record_portability(text,text,text,text,bigint,bigint,bytea) from public;

create or replace function public.record_portability_purge(bytea)
returns bigint
language plpgsql
security definer
set search_path = pg_catalog
as $$
declare p_command alias for $1; v jsonb;
begin
  if p_command is null or octet_length(p_command) not between 1 and 4096 then
    raise exception using errcode = '55000', message = 'invalid portability purge command';
  end if;
  v := convert_from(p_command, 'UTF8')::jsonb;
  if jsonb_typeof(v) <> 'object' or
    array(select key from jsonb_object_keys(v) as key order by key) <>
    array['fence_epoch','ledger_entry_hash','ledger_sequence','operation_id','project_id','record_id','reservation_id']::text[] then
    raise exception using errcode = '55000', message = 'invalid portability purge command';
  end if;
  return record_platform_internal.purge_record_portability(
    v->>'operation_id', v->>'reservation_id', v->>'project_id', v->>'record_id',
    (v->>'fence_epoch')::bigint, (v->>'ledger_sequence')::bigint,
    decode(v->>'ledger_entry_hash', 'hex')
  );
end
$$;
revoke all on function public.record_portability_purge(bytea) from public;

drop trigger if exists record_portability_purge_receipts_reject_update on public.record_portability_purge_receipts;
create trigger record_portability_purge_receipts_reject_update before update on public.record_portability_purge_receipts
for each row execute function record_platform_internal.reject_immutable_mutation();
