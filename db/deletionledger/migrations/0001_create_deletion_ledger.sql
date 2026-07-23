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

create table public.record_platform_domain_identity (
  domain_id text primary key check (domain_id ~ '^rd-[0-9a-f]{64}$'),
  domain_kind text not null check (domain_kind = 'deletion_ledger'),
  identity_epoch bigint not null default 1 check (identity_epoch > 0),
  identity_mode text not null check (identity_mode in ('postgres_system', 'external_attestation')),
  postgres_system_identifier text
    check (postgres_system_identifier ~ '^[1-9][0-9]{0,19}$'),
  external_stable_identity_digest bytea
    check (octet_length(external_stable_identity_digest) = 32),
  provisioning_attestation_digest bytea
    check (octet_length(provisioning_attestation_digest) = 32),
  database_oid oid not null,
  database_name text not null check (database_name ~ '^[a-z][a-z0-9_]{0,62}$'),
  provisioned_at timestamptz not null default now(),
  check (
    (identity_mode = 'postgres_system'
      and postgres_system_identifier is not null
      and external_stable_identity_digest is null
      and provisioning_attestation_digest is null)
    or
    (identity_mode = 'external_attestation'
      and postgres_system_identifier is null
      and external_stable_identity_digest is not null
      and provisioning_attestation_digest is not null)
  )
);

create table public.record_platform_domain_attestations (
  domain_id text not null,
  attestation_purpose text not null check (attestation_purpose in
    ('provision', 'renew', 'rotation_candidate', 'retirement')),
  attestation_generation bigint not null check (attestation_generation > 0),
  stable_identity_digest bytea not null
    check (octet_length(stable_identity_digest) = 32),
  canonical_attestation_body bytea not null
    check (octet_length(canonical_attestation_body) between 1 and 65536),
  attestation_body_digest bytea not null
    check (octet_length(attestation_body_digest) = 32),
  canonical_attestation bytea not null
    check (octet_length(canonical_attestation) between 1 and 131072),
  attestation_digest bytea not null unique
    check (octet_length(attestation_digest) = 32),
  signature_set_digest bytea not null
    check (octet_length(signature_set_digest) = 32),
  signature_count smallint not null check (signature_count between 1 and 64),
  attestation_policy_digest bytea not null
    check (octet_length(attestation_policy_digest) = 32),
  valid_from timestamptz not null,
  expires_at timestamptz not null,
  witnessed_at timestamptz not null default now(),
  primary key (domain_id, attestation_generation),
  foreign key (domain_id) references public.record_platform_domain_identity(domain_id)
    on delete restrict,
  check (attestation_body_digest =
    record_platform_internal.digest(canonical_attestation_body, 'sha256')),
  check (attestation_digest =
    record_platform_internal.digest(canonical_attestation, 'sha256')),
  check (expires_at > valid_from)
);

create trigger rp_domain_identity_immutable
before update or delete or truncate on public.record_platform_domain_identity
for each statement execute function record_platform_internal.reject_immutable_mutation();

create trigger rp_domain_attestations_immutable
before update or delete or truncate on public.record_platform_domain_attestations
for each statement execute function record_platform_internal.reject_immutable_mutation();

create table if not exists public.deletion_ledger_entries (
  sequence bigint primary key check (sequence > 0),
  entry_version smallint not null check (entry_version = 1),
  entry_type text not null check (entry_type in ('delete_commit', 'attempt_not_committed')),
  deployment_id text not null check (deployment_id ~ '^dp-[0-9a-f]{64}$'),
  project_id text not null check (project_id = 'default'),
  operation_id text not null check (operation_id ~ '^[a-z0-9_-]{1,128}$'),
  actor_id text not null check (actor_id ~ '^[a-z0-9_-]{1,128}$'),
  route text not null check (route in ('record_permanent_delete', 'source_permanent_delete')),
  object_kind text not null check (object_kind in ('record', 'vps', 'monitoring_instance', 'target')),
  object_id text not null check (object_id ~ '^[a-z0-9_-]{1,128}$'),
  origin_identity bytea check (octet_length(origin_identity) between 1 and 4096),
  authorization_floor bytea check (octet_length(authorization_floor) between 1 and 4096),
  authorization_floor_hash bytea check (octet_length(authorization_floor_hash) = 32),
  deletion_request_token_commitment bytea not null
    check (octet_length(deletion_request_token_commitment) = 32),
  request_fingerprint bytea not null check (octet_length(request_fingerprint) = 32),
  deletion_contract_version bigint check (deletion_contract_version > 0),
  reason_code text not null check (reason_code in
    ('user_confirmed', 'source_removed', 'retention_replay')),
  release_epoch bigint check (release_epoch > 0),
  confirmed_at timestamptz not null,
  previous_hash bytea not null check (octet_length(previous_hash) = 32),
  canonical_entry bytea not null check (octet_length(canonical_entry) between 1 and 1048576),
  entry_hash bytea not null unique check (octet_length(entry_hash) = 32),
  unique (deployment_id, project_id, operation_id),
  constraint deletion_ledger_entries_contract_fields_valid check (
    (entry_type = 'delete_commit'
      and deletion_contract_version is not null
      and release_epoch is null
      and ((object_kind in ('vps', 'monitoring_instance', 'target')
          and origin_identity is not null
          and authorization_floor is not null
          and authorization_floor_hash is not null)
        or (object_kind = 'record'
          and origin_identity is null
          and authorization_floor is null
          and authorization_floor_hash is null)))
    or
    (entry_type = 'attempt_not_committed'
      and deletion_contract_version is null
      and release_epoch is not null
      and origin_identity is null
      and authorization_floor is null
      and authorization_floor_hash is null)
  ),
  check (entry_hash = record_platform_internal.digest(canonical_entry, 'sha256')),
  check (authorization_floor_hash is null or authorization_floor_hash =
    record_platform_internal.digest(authorization_floor, 'sha256'))
);

create table if not exists public.deletion_ledger_head (
  singleton boolean primary key default true check (singleton),
  sequence bigint not null default 0 check (sequence >= 0),
  entry_hash bytea not null default decode(repeat('00', 32), 'hex')
    check (octet_length(entry_hash) = 32),
  updated_at timestamptz not null default now()
);

insert into public.deletion_ledger_head(singleton)
values (true)
on conflict (singleton) do nothing;

create table if not exists public.deletion_ledger_checkpoints (
  sequence bigint primary key check (sequence >= 0),
  entry_hash bytea not null check (octet_length(entry_hash) = 32),
  checkpointed_at timestamptz not null default now()
);

create trigger deletion_ledger_entries_immutable
before update or delete or truncate on public.deletion_ledger_entries
for each statement execute function record_platform_internal.reject_immutable_mutation();

create trigger deletion_ledger_checkpoints_immutable
before update or delete or truncate on public.deletion_ledger_checkpoints
for each statement execute function record_platform_internal.reject_immutable_mutation();
