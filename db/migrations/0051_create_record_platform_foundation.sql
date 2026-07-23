create schema if not exists record_platform_internal;
revoke all on schema record_platform_internal from public;
create extension if not exists pgcrypto with schema record_platform_internal;
do $$
declare
  pgcrypto_schema text;
begin
  select n.nspname
  into pgcrypto_schema
  from pg_extension e
  join pg_namespace n on n.oid = e.extnamespace
  where e.extname = 'pgcrypto';

  if pgcrypto_schema is distinct from 'record_platform_internal' then
    raise exception using
      errcode = '55000',
      message = 'pgcrypto must be installed in record_platform_internal';
  end if;
end
$$;
revoke execute on all functions in schema record_platform_internal from public;

create or replace function record_platform_internal.reject_immutable_mutation()
returns trigger
language plpgsql
security definer
set search_path = pg_catalog
as $$
begin
  raise exception using
    errcode = '55000',
    message = 'record-platform immutable artifact cannot be mutated';
  return null;
end
$$;

revoke all on function record_platform_internal.reject_immutable_mutation() from public;

create table if not exists public.record_access_groups (
  group_id text primary key check (group_id ~ '^rag_[a-z0-9]{1,64}$'),
  project_id text not null default 'default' check (project_id = 'default'),
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (project_id, group_id)
);

create table if not exists public.record_access_group_members (
  group_id text not null,
  user_id text not null,
  created_at timestamptz not null default now(),
  primary key (group_id, user_id),
  foreign key (group_id) references public.record_access_groups(group_id)
    on delete restrict
);

create table if not exists public.record_outbox (
  outbox_row_id bigserial primary key,
  project_id text not null default 'default' check (project_id = 'default'),
  event_kind text not null check (event_kind ~ '^[a-z0-9_]{1,64}$'),
  subject_kind text not null check (subject_kind ~ '^[a-z0-9_]{1,64}$'),
  subject_id text not null check (subject_id ~ '^[a-z0-9_]{1,128}$'),
  authorization_epoch bigint not null check (authorization_epoch >= 0),
  status text not null default 'pending'
    check (status in ('pending', 'processing', 'sent', 'cancelled')),
  owner_id text not null default '' check (owner_id = '' or owner_id ~ '^[a-z0-9_-]{1,128}$'),
  owner_generation bigint not null default 0 check (owner_generation >= 0),
  owner_expires_at timestamptz,
  attempt_count bigint not null default 0 check (attempt_count >= 0),
  next_attempt_at timestamptz not null default now(),
  created_at timestamptz not null default now(),
  sent_at timestamptz,
  expires_at timestamptz not null,
  check ((status = 'processing') = (owner_id <> '' and owner_expires_at is not null)),
  check (expires_at > created_at)
);

create index if not exists idx_record_outbox_pending
  on public.record_outbox(status, next_attempt_at, outbox_row_id);

create table if not exists public.record_idempotency_keys (
  project_id text not null default 'default' check (project_id = 'default'),
  operation_kind text not null check (operation_kind ~ '^[a-z0-9_]{1,64}$'),
  idempotency_key text not null check (idempotency_key ~ '^[A-Za-z0-9._~-]{1,200}$'),
  request_fingerprint bytea not null check (octet_length(request_fingerprint) = 32),
  result_fingerprint bytea check (octet_length(result_fingerprint) = 32),
  status text not null check (status in ('in_progress', 'completed', 'conflict')),
  owner_id text not null default '' check (owner_id = '' or owner_id ~ '^[a-z0-9_-]{1,128}$'),
  owner_generation bigint not null default 0 check (owner_generation >= 0),
  owner_expires_at timestamptz,
  created_at timestamptz not null default now(),
  expires_at timestamptz not null,
  primary key (project_id, operation_kind, idempotency_key),
  check ((status = 'in_progress') = (owner_id <> '' and owner_expires_at is not null)),
  check (expires_at > created_at)
);

create table if not exists public.identity_mutation_guards (
  project_id text not null default 'default' check (project_id = 'default'),
  object_kind text not null check (object_kind ~ '^[a-z0-9_]{1,64}$'),
  object_id text not null check (object_id ~ '^[a-z0-9_]{1,128}$'),
  mutation_kind text not null check (mutation_kind ~ '^[a-z0-9_]{1,64}$'),
  owner_id text not null check (owner_id ~ '^[a-z0-9_-]{1,128}$'),
  owner_generation bigint not null check (owner_generation > 0),
  expires_at timestamptz not null,
  created_at timestamptz not null default now(),
  primary key (project_id, object_kind, object_id, mutation_kind),
  check (expires_at > created_at)
);

create table if not exists public.deletion_reservations (
  reservation_id text primary key check (reservation_id ~ '^drs_[a-z0-9]{1,64}$'),
  project_id text not null default 'default' check (project_id = 'default'),
  object_kind text not null check (object_kind ~ '^[a-z0-9_]{1,64}$'),
  object_id text not null check (object_id ~ '^[a-z0-9_]{1,128}$'),
  deletion_token_commitment bytea not null unique
    check (octet_length(deletion_token_commitment) = 32),
  request_fingerprint bytea not null check (octet_length(request_fingerprint) = 32),
  state text not null check (state in ('previewed', 'fenced', 'committed', 'not_committed', 'released')),
  fence_epoch bigint not null default 0 check (fence_epoch >= 0),
  owner_id text not null default '' check (owner_id = '' or owner_id ~ '^[a-z0-9_-]{1,128}$'),
  owner_generation bigint not null default 0 check (owner_generation >= 0),
  owner_expires_at timestamptz,
  created_at timestamptz not null default now(),
  expires_at timestamptz not null,
  completed_at timestamptz,
  unique (project_id, object_kind, object_id, deletion_token_commitment),
  check (expires_at > created_at),
  check ((state = 'fenced') = (owner_id <> '' and owner_expires_at is not null))
);

create index if not exists idx_deletion_reservations_expiry
  on public.deletion_reservations(expires_at);

create table if not exists public.record_purge_operations (
  operation_id text primary key check (operation_id ~ '^rpo_[a-z0-9]{1,64}$'),
  reservation_id text not null unique,
  project_id text not null default 'default' check (project_id = 'default'),
  operation_state text not null check (operation_state in ('pending', 'unknown', 'committed', 'not_committed', 'failed')),
  ledger_sequence bigint check (ledger_sequence > 0),
  ledger_entry_hash bytea check (octet_length(ledger_entry_hash) = 32),
  started_at timestamptz not null default now(),
  completed_at timestamptz,
  details_delete_after timestamptz,
  foreign key (reservation_id) references public.deletion_reservations(reservation_id)
    on delete restrict,
  check ((operation_state in ('committed', 'not_committed')) = (completed_at is not null)),
  check (details_delete_after is null or completed_at is not null)
);

create table if not exists public.record_deletion_audits (
  audit_id text primary key check (audit_id ~ '^rda_[a-z0-9]{1,64}$'),
  operation_id text not null,
  project_id text not null default 'default' check (project_id = 'default'),
  event_kind text not null check (event_kind in ('previewed', 'fenced', 'committed', 'not_committed', 'released')),
  reason_code text not null default '' check (reason_code = '' or reason_code ~ '^[a-z0-9_]{1,64}$'),
  occurred_at timestamptz not null default now(),
  foreign key (operation_id) references public.record_purge_operations(operation_id)
    on delete restrict,
  unique (operation_id, event_kind)
);

create table if not exists public.deletion_fence_leases (
  project_id text not null default 'default' check (project_id = 'default'),
  object_kind text not null check (object_kind ~ '^[a-z0-9_]{1,64}$'),
  object_id text not null check (object_id ~ '^[a-z0-9_]{1,128}$'),
  owner_id text not null check (owner_id ~ '^[a-z0-9_-]{1,128}$'),
  owner_generation bigint not null check (owner_generation > 0),
  expires_at timestamptz not null,
  created_at timestamptz not null default now(),
  primary key (project_id, object_kind, object_id),
  check (expires_at > created_at)
);

create table if not exists public.object_content_leases (
  project_id text not null default 'default' check (project_id = 'default'),
  object_kind text not null check (object_kind ~ '^[a-z0-9_]{1,64}$'),
  object_id text not null check (object_id ~ '^[a-z0-9_]{1,128}$'),
  owner_id text not null check (owner_id ~ '^[a-z0-9_-]{1,128}$'),
  owner_generation bigint not null check (owner_generation > 0),
  expires_at timestamptz not null,
  created_at timestamptz not null default now(),
  primary key (project_id, object_kind, object_id),
  check (expires_at > created_at)
);

create table if not exists public.client_content_leases (
  project_id text not null default 'default' check (project_id = 'default'),
  client_id text not null check (client_id ~ '^[a-z0-9_-]{1,128}$'),
  owner_id text not null check (owner_id ~ '^[a-z0-9_-]{1,128}$'),
  owner_generation bigint not null check (owner_generation > 0),
  expires_at timestamptz not null,
  created_at timestamptz not null default now(),
  primary key (project_id, client_id),
  check (expires_at > created_at)
);

create table if not exists public.content_delivery_epochs (
  project_id text not null default 'default' check (project_id = 'default'),
  object_kind text not null check (object_kind ~ '^[a-z0-9_]{1,64}$'),
  object_id text not null check (object_id ~ '^[a-z0-9_]{1,128}$'),
  delivery_epoch bigint not null check (delivery_epoch >= 0),
  updated_at timestamptz not null default now(),
  primary key (project_id, object_kind, object_id)
);

create table if not exists public.backup_epochs (
  project_id text not null default 'default' check (project_id = 'default'),
  backup_epoch bigint not null check (backup_epoch >= 0),
  witnessed_ledger_sequence bigint not null check (witnessed_ledger_sequence >= 0),
  witnessed_ledger_hash bytea not null check (octet_length(witnessed_ledger_hash) = 32),
  recoverable_until timestamptz,
  created_at timestamptz not null default now(),
  primary key (project_id, backup_epoch)
);

create table if not exists public.recovery_inventory_projection (
  inventory_id text primary key check (inventory_id ~ '^rin_[a-z0-9]{1,64}$'),
  project_id text not null default 'default' check (project_id = 'default'),
  source_kind text not null check (source_kind ~ '^[a-z0-9_]{1,64}$'),
  source_epoch bigint not null check (source_epoch >= 0),
  manifest_digest bytea not null check (octet_length(manifest_digest) = 32),
  recoverable_until timestamptz,
  status text not null check (status in ('available', 'partial', 'expired', 'unavailable')),
  observed_at timestamptz not null default now(),
  unique (project_id, source_kind, source_epoch, manifest_digest)
);

create table if not exists public.deletion_replay_state (
  project_id text primary key default 'default' check (project_id = 'default'),
  applied_ledger_sequence bigint not null default 0 check (applied_ledger_sequence >= 0),
  applied_ledger_hash bytea not null default decode(repeat('00', 32), 'hex')
    check (octet_length(applied_ledger_hash) = 32),
  updated_at timestamptz not null default now()
);

create table if not exists public.deployment_membership (
  instance_id text primary key check (instance_id ~ '^[a-z0-9_-]{1,128}$'),
  deployment_id text not null check (deployment_id ~ '^dp-[0-9a-f]{64}$'),
  project_id text not null default 'default' check (project_id = 'default'),
  instance_kind text not null check (instance_kind in ('api', 'worker', 'recovery')),
  deployment_epoch bigint not null check (deployment_epoch > 0),
  fence_contract_version bigint not null check (fence_contract_version > 0),
  capability text not null check (capability ~ '^[a-z0-9_.]{1,128}$'),
  load_balancer_admitted boolean not null default false,
  queue_admitted boolean not null default false,
  heartbeat_expires_at timestamptz not null,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  unique (deployment_id, project_id, instance_id),
  check (heartbeat_expires_at > created_at)
);

create index if not exists idx_deployment_membership_expiry
  on public.deployment_membership(heartbeat_expires_at);

create table if not exists public.source_deletion_tombstones (
  project_id text not null default 'default' check (project_id = 'default'),
  source_kind text not null check (source_kind ~ '^[a-z0-9_]{1,64}$'),
  source_id text not null check (source_id ~ '^[a-z0-9_]{1,128}$'),
  authorization_floor_digest bytea not null check (octet_length(authorization_floor_digest) = 32),
  deleted_at timestamptz not null default now(),
  primary key (project_id, source_kind, source_id)
);

create table if not exists public.deployment_contract_state (
  project_id text primary key default 'default' check (project_id = 'default'),
  deployment_id text check (deployment_id ~ '^dp-[0-9a-f]{64}$'),
  active_profile text check (active_profile in ('postgres_sync', 's3_worm')),
  activation_sequence bigint check (activation_sequence = 1),
  activation_mutation_id text check (activation_mutation_id ~ '^tm-[0-9a-f]{64}$'),
  activation_plan_digest bytea check (octet_length(activation_plan_digest) = 32),
  activation_authorization_artifact_digest bytea
    check (octet_length(activation_authorization_artifact_digest) = 32),
  activation_bundle_digest bytea check (octet_length(activation_bundle_digest) = 32),
  trust_revision bigint check (trust_revision > 0),
  trust_head_hash bytea check (octet_length(trust_head_hash) = 32),
  inventory_digest bytea check (octet_length(inventory_digest) = 32),
  approval_policy_digest bytea check (octet_length(approval_policy_digest) = 32),
  activation_adapter_policy_digest bytea
    check (octet_length(activation_adapter_policy_digest) = 32),
  activation_adapter_policy_generation bigint
    check (activation_adapter_policy_generation = 1),
  active_adapter_policy_digest bytea
    check (octet_length(active_adapter_policy_digest) = 32),
  active_adapter_policy_generation bigint
    check (active_adapter_policy_generation >= 1),
  drain_receipt_digest bytea check (octet_length(drain_receipt_digest) = 32),
  activation_domain_identity_set_digest bytea
    check (octet_length(activation_domain_identity_set_digest) = 32),
  activation_domain_identity_epoch bigint
    check (activation_domain_identity_epoch = 1),
  active_domain_identity_epoch bigint check (active_domain_identity_epoch > 0),
  active_domain_identity_set_digest bytea
    check (octet_length(active_domain_identity_set_digest) = 32),
  last_domain_identity_sequence bigint check (last_domain_identity_sequence >= 1),
  last_domain_identity_entry_hash bytea
    check (octet_length(last_domain_identity_entry_hash) = 32),
  minimum_fence_contract_version bigint not null default 0 check (minimum_fence_contract_version >= 0),
  witnessed_ledger_sequence bigint not null default 0 check (witnessed_ledger_sequence >= 0),
  witnessed_ledger_hash bytea not null default decode(repeat('00', 32), 'hex')
    check (octet_length(witnessed_ledger_hash) = 32),
  updated_at timestamptz not null default now(),
  check (
    (deployment_id is null
      and active_profile is null
      and activation_sequence is null
      and activation_mutation_id is null
      and activation_plan_digest is null
      and activation_authorization_artifact_digest is null
      and activation_bundle_digest is null
      and trust_revision is null
      and trust_head_hash is null
      and inventory_digest is null
      and approval_policy_digest is null
      and activation_adapter_policy_digest is null
      and activation_adapter_policy_generation is null
      and active_adapter_policy_digest is null
      and active_adapter_policy_generation is null
      and drain_receipt_digest is null
      and activation_domain_identity_set_digest is null
      and activation_domain_identity_epoch is null
      and active_domain_identity_epoch is null
      and active_domain_identity_set_digest is null
      and last_domain_identity_sequence is null
      and last_domain_identity_entry_hash is null
      and minimum_fence_contract_version = 0
      and witnessed_ledger_sequence = 0
      and witnessed_ledger_hash = decode(repeat('00', 32), 'hex'))
    or
    (deployment_id is not null
      and active_profile is not null
      and activation_sequence = 1
      and activation_mutation_id is not null
      and activation_plan_digest is not null
      and activation_authorization_artifact_digest is not null
      and activation_bundle_digest is not null
      and trust_revision is not null
      and trust_head_hash is not null
      and inventory_digest is not null
      and approval_policy_digest is not null
      and activation_adapter_policy_digest is not null
      and activation_adapter_policy_generation = 1
      and active_adapter_policy_digest is not null
      and active_adapter_policy_generation >= activation_adapter_policy_generation
      and drain_receipt_digest is not null
      and activation_domain_identity_set_digest is not null
      and activation_domain_identity_epoch = 1
      and active_domain_identity_epoch >= activation_domain_identity_epoch
      and active_domain_identity_set_digest is not null
      and last_domain_identity_sequence >= activation_sequence
      and last_domain_identity_entry_hash is not null
      and minimum_fence_contract_version > 0
      and witnessed_ledger_sequence >= activation_sequence
      and (active_domain_identity_epoch > activation_domain_identity_epoch
        or active_domain_identity_set_digest = activation_domain_identity_set_digest))
  )
);

insert into public.deployment_contract_state(project_id)
values ('default')
on conflict (project_id) do nothing;

create or replace function record_platform_internal.record_platform_projection_read_bytes_v1(
  p_command bytea,
  p_offset integer,
  p_length integer
)
returns bytea
language plpgsql
security definer
set search_path = pg_catalog
as $$
begin
  if p_command is null
    or p_offset < 0
    or p_length < 0
    or p_offset > pg_catalog.octet_length(p_command)
    or p_length > pg_catalog.octet_length(p_command) - p_offset then
    raise exception using
      errcode = '22023',
      message = 'invalid record-platform projection command field bounds';
  end if;

  return pg_catalog.substr(p_command, p_offset + 1, p_length);
end
$$;

revoke all on function record_platform_internal.record_platform_projection_read_bytes_v1(bytea, integer, integer) from public;

create or replace function record_platform_internal.record_platform_projection_read_uint64_v1(
  p_command bytea,
  p_offset integer
)
returns bigint
language plpgsql
security definer
set search_path = pg_catalog
as $$
declare
  v_value bigint;
begin
  if pg_catalog.get_byte(
      record_platform_internal.record_platform_projection_read_bytes_v1(p_command, p_offset, 8),
      0
    ) >= 128 then
    raise exception using
      errcode = '22023',
      message = 'record-platform projection integer exceeds PostgreSQL bigint';
  end if;

  v_value :=
    pg_catalog.get_byte(p_command, p_offset)::bigint * 72057594037927936::bigint
    + pg_catalog.get_byte(p_command, p_offset + 1)::bigint * 281474976710656::bigint
    + pg_catalog.get_byte(p_command, p_offset + 2)::bigint * 1099511627776::bigint
    + pg_catalog.get_byte(p_command, p_offset + 3)::bigint * 4294967296::bigint
    + pg_catalog.get_byte(p_command, p_offset + 4)::bigint * 16777216::bigint
    + pg_catalog.get_byte(p_command, p_offset + 5)::bigint * 65536::bigint
    + pg_catalog.get_byte(p_command, p_offset + 6)::bigint * 256::bigint
    + pg_catalog.get_byte(p_command, p_offset + 7)::bigint;
  return v_value;
end
$$;

revoke all on function record_platform_internal.record_platform_projection_read_uint64_v1(bytea, integer) from public;

create or replace function record_platform_internal.record_platform_projection_read_token_v1(
  p_command bytea,
  p_offset integer,
  p_prefix text
)
returns text
language plpgsql
security definer
set search_path = pg_catalog
as $$
declare
  v_token bytea;
  v_index integer;
  v_byte integer;
begin
  if p_prefix is null or p_prefix not in ('dp-', 'tm-') then
    raise exception using
      errcode = '22023',
      message = 'invalid record-platform projection token prefix';
  end if;

  v_token := record_platform_internal.record_platform_projection_read_bytes_v1(
    p_command,
    p_offset,
    67
  );
  if pg_catalog.substr(v_token, 1, 3) <> pg_catalog.convert_to(p_prefix, 'UTF8') then
    raise exception using
      errcode = '22023',
      message = 'invalid record-platform projection token prefix bytes';
  end if;

  for v_index in 3..66 loop
    v_byte := pg_catalog.get_byte(v_token, v_index);
    if (v_byte < 48 or v_byte > 57) and (v_byte < 97 or v_byte > 102) then
      raise exception using
        errcode = '22023',
        message = 'invalid record-platform projection token bytes';
    end if;
  end loop;

  return pg_catalog.convert_from(v_token, 'UTF8');
end
$$;

revoke all on function record_platform_internal.record_platform_projection_read_token_v1(bytea, integer, text) from public;

create or replace function record_platform_internal.record_platform_projection_read_profile_v1(
  p_command bytea,
  p_offset integer
)
returns text
language plpgsql
security definer
set search_path = pg_catalog
as $$
declare
  v_profile integer;
begin
  v_profile := pg_catalog.get_byte(
    record_platform_internal.record_platform_projection_read_bytes_v1(p_command, p_offset, 1),
    0
  );
  case v_profile
    when 1 then return 'postgres_sync';
    when 2 then return 's3_worm';
    else
      raise exception using
        errcode = '22023',
        message = 'invalid record-platform projection profile';
  end case;
end
$$;

revoke all on function record_platform_internal.record_platform_projection_read_profile_v1(bytea, integer) from public;

create or replace function record_platform_internal.record_platform_projection_validate_header_v1(
  p_command bytea,
  p_operation integer,
  p_field_count integer,
  p_exact_length integer
)
returns void
language plpgsql
security definer
set search_path = pg_catalog
as $$
begin
  if p_command is null
    or pg_catalog.octet_length(p_command) <> p_exact_length
    or pg_catalog.substr(p_command, 1, 33) <> pg_catalog.convert_to('HOUFENG-APP-PROJECTION-COMMAND-V1', 'UTF8')
    or pg_catalog.get_byte(p_command, 33) <> 0
    or pg_catalog.get_byte(p_command, 34) <> 1
    or pg_catalog.get_byte(p_command, 35) <> p_operation
    or pg_catalog.get_byte(p_command, 36) <> p_field_count then
    raise exception using
      errcode = '22023',
      message = 'invalid record-platform projection command header';
  end if;
end
$$;

revoke all on function record_platform_internal.record_platform_projection_validate_header_v1(bytea, integer, integer, integer) from public;

create or replace function record_platform_internal.record_platform_projection_cas_receipt_v1(
  p_command bytea
)
returns bytea
language plpgsql
security definer
set search_path = pg_catalog
as $$
begin
  if p_command is null then
    raise exception using
      errcode = '22023',
      message = 'record-platform projection command is null';
  end if;
  return record_platform_internal.digest(
    pg_catalog.convert_to('HOUFENG-APP-PROJECTION-CAS-RECEIPT-V1', 'UTF8')
    || pg_catalog.int4send(pg_catalog.octet_length(p_command))
    || p_command,
    'sha256'
  );
end
$$;

revoke all on function record_platform_internal.record_platform_projection_cas_receipt_v1(bytea) from public;

-- External ledger and full-witness proof is verified by the runtime before this
-- local APP projection CAS function is invoked.
create or replace function public.record_platform_cas_contract_activation_projection(bytea)
returns bytea
language plpgsql
security definer
set search_path = pg_catalog
as $$
declare
  p_command alias for $1;
  v_deployment_id text;
  v_active_profile text;
  v_activation_mutation_id text;
  v_witnessed_ledger_sequence bigint;
  v_witnessed_ledger_hash bytea;
  v_plan_digest bytea;
  v_authorization_artifact_digest bytea;
  v_activation_bundle_digest bytea;
  v_trust_revision bigint;
  v_trust_head_hash bytea;
  v_inventory_digest bytea;
  v_approval_policy_digest bytea;
  v_adapter_policy_generation bigint;
  v_adapter_policy_digest bytea;
  v_drain_receipt_digest bytea;
  v_identity_set_epoch bigint;
  v_identity_set_digest bytea;
  v_minimum_fence_contract_version bigint;
  v_receipt bytea;
  v_state public.deployment_contract_state%rowtype;
begin
  perform record_platform_internal.record_platform_projection_validate_header_v1(
    p_command,
    1,
    18,
    532
  );
  v_deployment_id := record_platform_internal.record_platform_projection_read_token_v1(p_command, 37, 'dp-');
  v_active_profile := record_platform_internal.record_platform_projection_read_profile_v1(p_command, 104);
  v_activation_mutation_id := record_platform_internal.record_platform_projection_read_token_v1(p_command, 105, 'tm-');
  v_witnessed_ledger_sequence := record_platform_internal.record_platform_projection_read_uint64_v1(p_command, 172);
  v_witnessed_ledger_hash := record_platform_internal.record_platform_projection_read_bytes_v1(p_command, 180, 32);
  v_plan_digest := record_platform_internal.record_platform_projection_read_bytes_v1(p_command, 212, 32);
  v_authorization_artifact_digest := record_platform_internal.record_platform_projection_read_bytes_v1(p_command, 244, 32);
  v_activation_bundle_digest := record_platform_internal.record_platform_projection_read_bytes_v1(p_command, 276, 32);
  v_trust_revision := record_platform_internal.record_platform_projection_read_uint64_v1(p_command, 308);
  v_trust_head_hash := record_platform_internal.record_platform_projection_read_bytes_v1(p_command, 316, 32);
  v_inventory_digest := record_platform_internal.record_platform_projection_read_bytes_v1(p_command, 348, 32);
  v_approval_policy_digest := record_platform_internal.record_platform_projection_read_bytes_v1(p_command, 380, 32);
  v_adapter_policy_generation := record_platform_internal.record_platform_projection_read_uint64_v1(p_command, 412);
  v_adapter_policy_digest := record_platform_internal.record_platform_projection_read_bytes_v1(p_command, 420, 32);
  v_drain_receipt_digest := record_platform_internal.record_platform_projection_read_bytes_v1(p_command, 452, 32);
  v_identity_set_epoch := record_platform_internal.record_platform_projection_read_uint64_v1(p_command, 484);
  v_identity_set_digest := record_platform_internal.record_platform_projection_read_bytes_v1(p_command, 492, 32);
  v_minimum_fence_contract_version := record_platform_internal.record_platform_projection_read_uint64_v1(p_command, 524);

  if v_witnessed_ledger_sequence <> 1
    or v_trust_revision <= 0
    or v_adapter_policy_generation <> 1
    or v_identity_set_epoch <> 1
    or v_minimum_fence_contract_version <= 0 then
    raise exception using
      errcode = '22023',
      message = 'invalid contract activation projection command invariants';
  end if;
  v_receipt := record_platform_internal.record_platform_projection_cas_receipt_v1(p_command);

  select *
  into v_state
  from public.deployment_contract_state
  where project_id = 'default'
  for update;
  if not found then
    raise exception using
      errcode = '55000',
      message = 'deployment contract singleton is missing';
  end if;

  if v_state.deployment_id is null then
    update public.deployment_contract_state
    set deployment_id = v_deployment_id,
        active_profile = v_active_profile,
        activation_sequence = v_witnessed_ledger_sequence,
        activation_mutation_id = v_activation_mutation_id,
        activation_plan_digest = v_plan_digest,
        activation_authorization_artifact_digest = v_authorization_artifact_digest,
        activation_bundle_digest = v_activation_bundle_digest,
        trust_revision = v_trust_revision,
        trust_head_hash = v_trust_head_hash,
        inventory_digest = v_inventory_digest,
        approval_policy_digest = v_approval_policy_digest,
        activation_adapter_policy_digest = v_adapter_policy_digest,
        activation_adapter_policy_generation = v_adapter_policy_generation,
        active_adapter_policy_digest = v_adapter_policy_digest,
        active_adapter_policy_generation = v_adapter_policy_generation,
        drain_receipt_digest = v_drain_receipt_digest,
        activation_domain_identity_set_digest = v_identity_set_digest,
        activation_domain_identity_epoch = v_identity_set_epoch,
        active_domain_identity_epoch = v_identity_set_epoch,
        active_domain_identity_set_digest = v_identity_set_digest,
        last_domain_identity_sequence = v_witnessed_ledger_sequence,
        last_domain_identity_entry_hash = v_witnessed_ledger_hash,
        minimum_fence_contract_version = v_minimum_fence_contract_version,
        witnessed_ledger_sequence = v_witnessed_ledger_sequence,
        witnessed_ledger_hash = v_witnessed_ledger_hash,
        updated_at = pg_catalog.transaction_timestamp()
    where project_id = 'default';
    return v_receipt;
  end if;

  if v_state.deployment_id is not distinct from v_deployment_id
    and v_state.active_profile is not distinct from v_active_profile
    and v_state.activation_sequence is not distinct from v_witnessed_ledger_sequence
    and v_state.activation_mutation_id is not distinct from v_activation_mutation_id
    and v_state.activation_plan_digest is not distinct from v_plan_digest
    and v_state.activation_authorization_artifact_digest is not distinct from v_authorization_artifact_digest
    and v_state.activation_bundle_digest is not distinct from v_activation_bundle_digest
    and v_state.trust_revision is not distinct from v_trust_revision
    and v_state.trust_head_hash is not distinct from v_trust_head_hash
    and v_state.inventory_digest is not distinct from v_inventory_digest
    and v_state.approval_policy_digest is not distinct from v_approval_policy_digest
    and v_state.activation_adapter_policy_digest is not distinct from v_adapter_policy_digest
    and v_state.activation_adapter_policy_generation is not distinct from v_adapter_policy_generation
    and v_state.active_adapter_policy_digest is not distinct from v_adapter_policy_digest
    and v_state.active_adapter_policy_generation is not distinct from v_adapter_policy_generation
    and v_state.drain_receipt_digest is not distinct from v_drain_receipt_digest
    and v_state.activation_domain_identity_set_digest is not distinct from v_identity_set_digest
    and v_state.activation_domain_identity_epoch is not distinct from v_identity_set_epoch
    and v_state.active_domain_identity_epoch is not distinct from v_identity_set_epoch
    and v_state.active_domain_identity_set_digest is not distinct from v_identity_set_digest
    and v_state.last_domain_identity_sequence is not distinct from v_witnessed_ledger_sequence
    and v_state.last_domain_identity_entry_hash is not distinct from v_witnessed_ledger_hash
    and v_state.minimum_fence_contract_version is not distinct from v_minimum_fence_contract_version
    and v_state.witnessed_ledger_sequence is not distinct from v_witnessed_ledger_sequence
    and v_state.witnessed_ledger_hash is not distinct from v_witnessed_ledger_hash then
    return v_receipt;
  end if;

  raise exception using
    errcode = '55000',
    message = 'contract activation projection compare-and-swap conflict';
end
$$;

revoke all on function public.record_platform_cas_contract_activation_projection(bytea) from public;

create or replace function public.record_platform_cas_domain_rotation_projection(bytea)
returns bytea
language plpgsql
security definer
set search_path = pg_catalog
as $$
declare
  p_command alias for $1;
  v_deployment_id text;
  v_active_profile text;
  v_rotation_mutation_id text;
  v_expected_witnessed_ledger_sequence bigint;
  v_expected_witnessed_ledger_hash bytea;
  v_expected_identity_set_epoch bigint;
  v_expected_identity_set_digest bytea;
  v_expected_adapter_policy_generation bigint;
  v_expected_adapter_policy_digest bytea;
  v_expected_minimum_fence_contract_version bigint;
  v_expected_trust_revision bigint;
  v_expected_trust_head_hash bytea;
  v_next_witnessed_ledger_sequence bigint;
  v_next_witnessed_ledger_hash bytea;
  v_next_identity_set_epoch bigint;
  v_next_identity_set_digest bytea;
  v_next_adapter_policy_generation bigint;
  v_next_adapter_policy_digest bytea;
  v_next_minimum_fence_contract_version bigint;
  v_next_trust_revision bigint;
  v_next_trust_head_hash bytea;
  v_receipt bytea;
  v_state public.deployment_contract_state%rowtype;
begin
  perform record_platform_internal.record_platform_projection_validate_header_v1(
    p_command,
    2,
    21,
    508
  );
  v_deployment_id := record_platform_internal.record_platform_projection_read_token_v1(p_command, 37, 'dp-');
  v_active_profile := record_platform_internal.record_platform_projection_read_profile_v1(p_command, 104);
  v_rotation_mutation_id := record_platform_internal.record_platform_projection_read_token_v1(p_command, 105, 'tm-');
  v_expected_witnessed_ledger_sequence := record_platform_internal.record_platform_projection_read_uint64_v1(p_command, 172);
  v_expected_witnessed_ledger_hash := record_platform_internal.record_platform_projection_read_bytes_v1(p_command, 180, 32);
  v_expected_identity_set_epoch := record_platform_internal.record_platform_projection_read_uint64_v1(p_command, 212);
  v_expected_identity_set_digest := record_platform_internal.record_platform_projection_read_bytes_v1(p_command, 220, 32);
  v_expected_adapter_policy_generation := record_platform_internal.record_platform_projection_read_uint64_v1(p_command, 252);
  v_expected_adapter_policy_digest := record_platform_internal.record_platform_projection_read_bytes_v1(p_command, 260, 32);
  v_expected_minimum_fence_contract_version := record_platform_internal.record_platform_projection_read_uint64_v1(p_command, 292);
  v_expected_trust_revision := record_platform_internal.record_platform_projection_read_uint64_v1(p_command, 300);
  v_expected_trust_head_hash := record_platform_internal.record_platform_projection_read_bytes_v1(p_command, 308, 32);
  v_next_witnessed_ledger_sequence := record_platform_internal.record_platform_projection_read_uint64_v1(p_command, 340);
  v_next_witnessed_ledger_hash := record_platform_internal.record_platform_projection_read_bytes_v1(p_command, 348, 32);
  v_next_identity_set_epoch := record_platform_internal.record_platform_projection_read_uint64_v1(p_command, 380);
  v_next_identity_set_digest := record_platform_internal.record_platform_projection_read_bytes_v1(p_command, 388, 32);
  v_next_adapter_policy_generation := record_platform_internal.record_platform_projection_read_uint64_v1(p_command, 420);
  v_next_adapter_policy_digest := record_platform_internal.record_platform_projection_read_bytes_v1(p_command, 428, 32);
  v_next_minimum_fence_contract_version := record_platform_internal.record_platform_projection_read_uint64_v1(p_command, 460);
  v_next_trust_revision := record_platform_internal.record_platform_projection_read_uint64_v1(p_command, 468);
  v_next_trust_head_hash := record_platform_internal.record_platform_projection_read_bytes_v1(p_command, 476, 32);

  if v_expected_witnessed_ledger_sequence <= 0
    or v_expected_identity_set_epoch <= 0
    or v_expected_adapter_policy_generation <= 0
    or v_expected_minimum_fence_contract_version <= 0
    or v_expected_trust_revision <= 0
    or v_next_witnessed_ledger_sequence <= 0
    or v_next_identity_set_epoch <= 0
    or v_next_adapter_policy_generation <= 0
    or v_next_minimum_fence_contract_version <= 0
    or v_next_trust_revision <= 0 then
    raise exception using
      errcode = '22023',
      message = 'invalid domain rotation projection positive integer';
  end if;
  if v_next_witnessed_ledger_sequence <= v_expected_witnessed_ledger_sequence then
    raise exception using
      errcode = '22023',
      message = 'domain rotation witnessed ledger sequence must advance';
  end if;
  if v_expected_identity_set_epoch >= 9223372036854775807 then
    raise exception using
      errcode = '22023',
      message = 'domain rotation identity epoch cannot advance';
  end if;
  if v_next_identity_set_epoch <> v_expected_identity_set_epoch + 1
    or v_next_identity_set_digest = v_expected_identity_set_digest then
    raise exception using
      errcode = '22023',
      message = 'invalid domain rotation identity transition';
  end if;
  if v_next_adapter_policy_generation = v_expected_adapter_policy_generation then
    if v_next_adapter_policy_digest <> v_expected_adapter_policy_digest then
      raise exception using
        errcode = '22023',
        message = 'unchanged domain rotation policy generation has a changed digest';
    end if;
  elsif v_expected_adapter_policy_generation < 9223372036854775807
    and v_next_adapter_policy_generation = v_expected_adapter_policy_generation + 1 then
    if v_next_adapter_policy_digest = v_expected_adapter_policy_digest then
      raise exception using
        errcode = '22023',
        message = 'advanced domain rotation policy generation has an unchanged digest';
    end if;
  else
    raise exception using
      errcode = '22023',
      message = 'invalid domain rotation policy generation transition';
  end if;
  if v_next_minimum_fence_contract_version < v_expected_minimum_fence_contract_version then
    raise exception using
      errcode = '22023',
      message = 'domain rotation minimum fence contract version decreased';
  end if;
  if v_next_trust_revision < v_expected_trust_revision
    or (v_next_trust_revision = v_expected_trust_revision and v_next_trust_head_hash <> v_expected_trust_head_hash) then
    raise exception using
      errcode = '22023',
      message = 'invalid domain rotation trust transition';
  end if;
  v_receipt := record_platform_internal.record_platform_projection_cas_receipt_v1(p_command);

  select *
  into v_state
  from public.deployment_contract_state
  where project_id = 'default'
  for update;
  if not found then
    raise exception using
      errcode = '55000',
      message = 'deployment contract singleton is missing';
  end if;

  if v_state.deployment_id is not distinct from v_deployment_id
    and v_state.active_profile is not distinct from v_active_profile
    and v_state.witnessed_ledger_sequence is not distinct from v_next_witnessed_ledger_sequence
    and v_state.witnessed_ledger_hash is not distinct from v_next_witnessed_ledger_hash
    and v_state.active_domain_identity_epoch is not distinct from v_next_identity_set_epoch
    and v_state.active_domain_identity_set_digest is not distinct from v_next_identity_set_digest
    and v_state.active_adapter_policy_generation is not distinct from v_next_adapter_policy_generation
    and v_state.active_adapter_policy_digest is not distinct from v_next_adapter_policy_digest
    and v_state.minimum_fence_contract_version is not distinct from v_next_minimum_fence_contract_version
    and v_state.trust_revision is not distinct from v_next_trust_revision
    and v_state.trust_head_hash is not distinct from v_next_trust_head_hash
    and v_state.last_domain_identity_sequence is not distinct from v_next_witnessed_ledger_sequence
    and v_state.last_domain_identity_entry_hash is not distinct from v_next_witnessed_ledger_hash then
    return v_receipt;
  end if;

  if v_state.deployment_id is distinct from v_deployment_id
    or v_state.active_profile is distinct from v_active_profile
    or v_state.witnessed_ledger_sequence is distinct from v_expected_witnessed_ledger_sequence
    or v_state.witnessed_ledger_hash is distinct from v_expected_witnessed_ledger_hash
    or v_state.active_domain_identity_epoch is distinct from v_expected_identity_set_epoch
    or v_state.active_domain_identity_set_digest is distinct from v_expected_identity_set_digest
    or v_state.active_adapter_policy_generation is distinct from v_expected_adapter_policy_generation
    or v_state.active_adapter_policy_digest is distinct from v_expected_adapter_policy_digest
    or v_state.minimum_fence_contract_version is distinct from v_expected_minimum_fence_contract_version
    or v_state.trust_revision is distinct from v_expected_trust_revision
    or v_state.trust_head_hash is distinct from v_expected_trust_head_hash then
    raise exception using
      errcode = '55000',
      message = 'domain rotation projection compare-and-swap conflict';
  end if;

  update public.deployment_contract_state
  set trust_revision = v_next_trust_revision,
      trust_head_hash = v_next_trust_head_hash,
      active_adapter_policy_digest = v_next_adapter_policy_digest,
      active_adapter_policy_generation = v_next_adapter_policy_generation,
      active_domain_identity_epoch = v_next_identity_set_epoch,
      active_domain_identity_set_digest = v_next_identity_set_digest,
      last_domain_identity_sequence = v_next_witnessed_ledger_sequence,
      last_domain_identity_entry_hash = v_next_witnessed_ledger_hash,
      minimum_fence_contract_version = v_next_minimum_fence_contract_version,
      witnessed_ledger_sequence = v_next_witnessed_ledger_sequence,
      witnessed_ledger_hash = v_next_witnessed_ledger_hash,
      updated_at = pg_catalog.transaction_timestamp()
  where project_id = 'default';
  return v_receipt;
end
$$;

revoke all on function public.record_platform_cas_domain_rotation_projection(bytea) from public;

create table if not exists public.record_platform_domain_identity (
  domain_id text primary key check (domain_id ~ '^rd-[0-9a-f]{64}$'),
  domain_kind text not null check (domain_kind = 'application'),
  identity_epoch bigint not null default 1 check (identity_epoch > 0),
  identity_mode text not null check (identity_mode in ('postgres_system', 'external_attestation')),
  postgres_system_identifier text check (postgres_system_identifier ~ '^[1-9][0-9]{0,19}$'),
  external_stable_identity_digest bytea check (octet_length(external_stable_identity_digest) = 32),
  provisioning_attestation_digest bytea check (octet_length(provisioning_attestation_digest) = 32),
  database_oid oid not null,
  database_name text not null check (database_name ~ '^[a-z][a-z0-9_]{0,62}$'),
  provisioned_at timestamptz not null default now(),
  check (
    (identity_mode = 'postgres_system' and postgres_system_identifier is not null
      and external_stable_identity_digest is null and provisioning_attestation_digest is null)
    or
    (identity_mode = 'external_attestation' and postgres_system_identifier is null
      and external_stable_identity_digest is not null and provisioning_attestation_digest is not null)
  )
);

create table if not exists public.record_platform_domain_attestations (
  domain_id text not null,
  attestation_purpose text not null check (attestation_purpose in ('provision', 'renew', 'rotation_candidate', 'retirement')),
  attestation_generation bigint not null check (attestation_generation > 0),
  stable_identity_digest bytea not null check (octet_length(stable_identity_digest) = 32),
  canonical_attestation_body bytea not null check (octet_length(canonical_attestation_body) between 1 and 65536),
  attestation_body_digest bytea not null check (octet_length(attestation_body_digest) = 32),
  canonical_attestation bytea not null check (octet_length(canonical_attestation) between 1 and 131072),
  attestation_digest bytea not null unique check (octet_length(attestation_digest) = 32),
  signature_set_digest bytea not null check (octet_length(signature_set_digest) = 32),
  signature_count smallint not null check (signature_count between 1 and 64),
  attestation_policy_digest bytea not null check (octet_length(attestation_policy_digest) = 32),
  valid_from timestamptz not null,
  expires_at timestamptz not null,
  witnessed_at timestamptz not null default now(),
  primary key (domain_id, attestation_generation),
  foreign key (domain_id) references public.record_platform_domain_identity(domain_id)
    on delete restrict,
  check (attestation_body_digest = record_platform_internal.digest(canonical_attestation_body, 'sha256')),
  check (attestation_digest = record_platform_internal.digest(canonical_attestation, 'sha256')),
  check (expires_at > valid_from)
);

create trigger rp_domain_identity_immutable
before update or delete or truncate on public.record_platform_domain_identity
for each statement execute function record_platform_internal.reject_immutable_mutation();

create trigger rp_domain_attestations_immutable
before update or delete or truncate on public.record_platform_domain_attestations
for each statement execute function record_platform_internal.reject_immutable_mutation();

create table if not exists public.app_acl_manifest_revisions (
  manifest_revision bigint primary key check (manifest_revision between 1 and 999999),
  predecessor_revision bigint generated always as
    (case when manifest_revision = 1 then null else manifest_revision - 1 end) stored,
  previous_manifest_digest bytea not null check (octet_length(previous_manifest_digest) = 32),
  canonical_migration_set bytea not null check (octet_length(canonical_migration_set) between 1 and 4194304),
  sorted_migration_set_digest bytea not null check (octet_length(sorted_migration_set_digest) = 32),
  canonical_privilege_set bytea not null check (octet_length(canonical_privilege_set) between 1 and 4194304),
  privilege_set_digest bytea not null check (octet_length(privilege_set_digest) = 32),
  manifest_digest bytea not null unique check (octet_length(manifest_digest) = 32),
  recorded_at timestamptz not null default transaction_timestamp(),
  unique (manifest_revision, manifest_digest),
  check (manifest_revision <> 1 or previous_manifest_digest = decode(repeat('00', 32), 'hex')),
  check (sorted_migration_set_digest = record_platform_internal.digest(canonical_migration_set, 'sha256')),
  check (privilege_set_digest = record_platform_internal.digest(canonical_privilege_set, 'sha256')),
  constraint app_acl_manifest_digest_matches check (
    manifest_digest = record_platform_internal.digest(
      convert_to('HOUFENG-APP-ACL-MANIFEST-V1', 'UTF8')
      || int8send(manifest_revision)
      || previous_manifest_digest
      || int4send(octet_length(canonical_migration_set))
      || canonical_migration_set
      || sorted_migration_set_digest
      || int4send(octet_length(canonical_privilege_set))
      || canonical_privilege_set
      || privilege_set_digest,
      'sha256')
  ),
  foreign key (predecessor_revision, previous_manifest_digest)
    references public.app_acl_manifest_revisions(manifest_revision, manifest_digest)
    on delete restrict
);

create table if not exists public.app_acl_manifest_head (
  singleton boolean primary key default true check (singleton),
  manifest_revision bigint check (manifest_revision between 1 and 999999),
  manifest_digest bytea check (octet_length(manifest_digest) = 32),
  updated_at timestamptz not null default transaction_timestamp(),
  check (
    (manifest_revision is null and manifest_digest is null)
    or (manifest_revision is not null and manifest_digest is not null)
  ),
  foreign key (manifest_revision, manifest_digest)
    references public.app_acl_manifest_revisions(manifest_revision, manifest_digest)
    on delete restrict
);

insert into public.app_acl_manifest_head(singleton)
values (true)
on conflict (singleton) do nothing;

create or replace function record_platform_internal.reject_acl_manifest_revision_mutation()
returns trigger
language plpgsql
security definer
set search_path = pg_catalog
as $$
begin
  raise exception using
    errcode = '55000',
    message = 'app ACL manifest revisions are immutable';
  return null;
end
$$;

revoke all on function record_platform_internal.reject_acl_manifest_revision_mutation() from public;

create trigger app_acl_manifest_revisions_immutable
before update or delete or truncate on public.app_acl_manifest_revisions
for each statement execute function record_platform_internal.reject_acl_manifest_revision_mutation();
