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
  domain_kind text not null check (domain_kind = 'recovery_control'),
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

create table if not exists public.record_platform_s3_witness_identities (
  domain_id text primary key check (domain_id ~ '^rd-[0-9a-f]{64}$'),
  identity_epoch bigint not null default 1 check (identity_epoch > 0),
  https_authority_digest bytea not null check (octet_length(https_authority_digest) = 32),
  tls_spki_digest bytea not null check (octet_length(tls_spki_digest) = 32),
  identity_adapter_kind text not null check (identity_adapter_kind in
    ('aws_s3_v1', 'minio_v1', 's3_compatible_v1')),
  normalization_version smallint not null check (normalization_version = 1),
  provider_digest bytea not null check (octet_length(provider_digest) = 32),
  account_digest bytea not null check (octet_length(account_digest) = 32),
  cluster_digest bytea not null check (octet_length(cluster_digest) = 32),
  physical_storage_digest bytea not null check (octet_length(physical_storage_digest) = 32),
  snapshot_policy_digest bytea not null check (octet_length(snapshot_policy_digest) = 32),
  restore_authority_digest bytea not null check (octet_length(restore_authority_digest) = 32),
  bucket_name text not null check (
    bucket_name ~ '^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$'
    and bucket_name !~ '\\.\\.|\\.-|-\\.'
    and bucket_name !~ '^[0-9]{1,3}(\\.[0-9]{1,3}){3}$'),
  canonical_namespace bytea not null check (octet_length(canonical_namespace) between 4 and 540),
  namespace_digest bytea not null check (
    octet_length(namespace_digest) = 32
    and namespace_digest = record_platform_internal.digest(canonical_namespace, 'sha256')),
  versioning_enabled boolean not null check (versioning_enabled),
  object_lock_mode text not null check (object_lock_mode = 'COMPLIANCE'),
  default_retention_seconds bigint not null check (default_retention_seconds >= 315360000),
  legal_hold_required boolean not null check (legal_hold_required),
  stable_identity_digest bytea not null unique check (octet_length(stable_identity_digest) = 32),
  provisioning_attestation_digest bytea not null
    check (octet_length(provisioning_attestation_digest) = 32),
  provisioned_at timestamptz not null default now()
);

create table if not exists public.record_platform_s3_witness_attestations (
  domain_id text not null,
  attestation_purpose text not null check (attestation_purpose in
    ('provision', 'renew', 'rotation_candidate', 'retirement')),
  attestation_generation bigint not null check (attestation_generation > 0),
  stable_identity_digest bytea not null check (octet_length(stable_identity_digest) = 32),
  canonical_attestation_body bytea not null
    check (octet_length(canonical_attestation_body) between 1 and 65536),
  attestation_body_digest bytea not null check (octet_length(attestation_body_digest) = 32),
  canonical_attestation bytea not null
    check (octet_length(canonical_attestation) between 1 and 131072),
  attestation_digest bytea not null unique check (octet_length(attestation_digest) = 32),
  signature_set_digest bytea not null check (octet_length(signature_set_digest) = 32),
  signature_count smallint not null check (signature_count between 1 and 64),
  attestation_policy_digest bytea not null check (octet_length(attestation_policy_digest) = 32),
  valid_from timestamptz not null,
  expires_at timestamptz not null,
  witnessed_at timestamptz not null default now(),
  primary key (domain_id, attestation_generation),
  foreign key (domain_id) references public.record_platform_s3_witness_identities(domain_id)
    on delete restrict,
  check (attestation_body_digest =
    record_platform_internal.digest(canonical_attestation_body, 'sha256')),
  check (attestation_digest =
    record_platform_internal.digest(canonical_attestation, 'sha256')),
  check (expires_at > valid_from)
);

create trigger rp_s3_witness_identity_immutable
before update or delete or truncate on public.record_platform_s3_witness_identities
for each statement execute function record_platform_internal.reject_immutable_mutation();

create trigger rp_s3_witness_attestations_immutable
before update or delete or truncate on public.record_platform_s3_witness_attestations
for each statement execute function record_platform_internal.reject_immutable_mutation();

create table if not exists public.recovery_trust_entries (
  trust_revision bigint primary key check (trust_revision > 0),
  mutation_id text not null check (mutation_id ~ '^tm-[0-9a-f]{64}$'),
  entry_kind text not null check (entry_kind in
    ('bootstrap', 'add', 'rotate', 'retire', 'compromise', 'remove', 'approval_policy_rotate')),
  canonical_entry bytea not null check (octet_length(canonical_entry) between 1 and 1048576),
  entry_hash bytea not null unique check (octet_length(entry_hash) = 32),
  previous_hash bytea not null check (octet_length(previous_hash) = 32),
  recorded_at timestamptz not null default now(),
  check (entry_hash = record_platform_internal.digest(canonical_entry, 'sha256'))
);

create table if not exists public.recovery_trust_head (
  singleton boolean primary key default true check (singleton),
  trust_revision bigint not null default 0 check (trust_revision >= 0),
  entry_hash bytea not null default decode(repeat('00', 32), 'hex')
    check (octet_length(entry_hash) = 32),
  updated_at timestamptz not null default now()
);

insert into public.recovery_trust_head(singleton)
values (true)
on conflict (singleton) do nothing;

create table if not exists public.recovery_point_manifests (
  manifest_id text primary key check (manifest_id ~ '^rpm_[a-z0-9]{1,64}$'),
  source_kind text not null check (source_kind ~ '^[a-z0-9_]{1,64}$'),
  source_epoch bigint not null check (source_epoch >= 0),
  witnessed_ledger_sequence bigint not null check (witnessed_ledger_sequence >= 0),
  witnessed_ledger_hash bytea not null check (octet_length(witnessed_ledger_hash) = 32),
  canonical_manifest bytea not null check (octet_length(canonical_manifest) between 1 and 8388608),
  manifest_digest bytea not null unique check (octet_length(manifest_digest) = 32),
  signed_at timestamptz not null,
  recoverable_until timestamptz,
  check (manifest_digest = record_platform_internal.digest(canonical_manifest, 'sha256'))
);

create table if not exists public.recovery_inventory (
  inventory_id text primary key check (inventory_id ~ '^rin_[a-z0-9]{1,64}$'),
  manifest_id text not null unique,
  source_kind text not null check (source_kind ~ '^[a-z0-9_]{1,64}$'),
  source_epoch bigint not null check (source_epoch >= 0),
  status text not null check (status in ('available', 'partial', 'expired', 'unavailable')),
  recoverable_until timestamptz,
  observed_at timestamptz not null default now(),
  foreign key (manifest_id) references public.recovery_point_manifests(manifest_id)
    on delete restrict
);

create table if not exists public.backup_attempts (
  attempt_id text primary key check (attempt_id ~ '^rba_[a-z0-9]{1,64}$'),
  source_kind text not null check (source_kind ~ '^[a-z0-9_]{1,64}$'),
  state text not null check (state in ('pending', 'running', 'completed', 'failed', 'cancelled')),
  owner_id text not null check (owner_id ~ '^[a-z0-9_-]{1,128}$'),
  owner_generation bigint not null check (owner_generation > 0),
  lease_expires_at timestamptz not null,
  created_at timestamptz not null default now(),
  completed_at timestamptz,
  details_delete_after timestamptz,
  check (lease_expires_at > created_at),
  check (details_delete_after is null or completed_at is not null)
);

create table if not exists public.backup_workspaces (
  workspace_id text primary key check (workspace_id ~ '^rbw_[a-z0-9]{1,64}$'),
  attempt_id text not null unique,
  state text not null check (state in ('active', 'purge_pending', 'purged', 'failed')),
  expires_at timestamptz not null,
  created_at timestamptz not null default now(),
  foreign key (attempt_id) references public.backup_attempts(attempt_id)
    on delete restrict,
  check (expires_at > created_at)
);

create table if not exists public.restore_attempts (
  attempt_id text primary key check (attempt_id ~ '^rra_[a-z0-9]{1,64}$'),
  inventory_id text not null,
  state text not null check (state in ('pending', 'running', 'completed', 'failed', 'cancelled')),
  owner_id text not null check (owner_id ~ '^[a-z0-9_-]{1,128}$'),
  owner_generation bigint not null check (owner_generation > 0),
  lease_expires_at timestamptz not null,
  created_at timestamptz not null default now(),
  completed_at timestamptz,
  details_delete_after timestamptz,
  foreign key (inventory_id) references public.recovery_inventory(inventory_id)
    on delete restrict,
  check (lease_expires_at > created_at),
  check (details_delete_after is null or completed_at is not null)
);

create table if not exists public.restore_workspaces (
  workspace_id text primary key check (workspace_id ~ '^rrw_[a-z0-9]{1,64}$'),
  attempt_id text not null unique,
  state text not null check (state in ('active', 'purge_pending', 'purged', 'forensic', 'failed')),
  expires_at timestamptz not null,
  created_at timestamptz not null default now(),
  foreign key (attempt_id) references public.restore_attempts(attempt_id)
    on delete restrict,
  check (expires_at > created_at)
);

create table if not exists public.recovery_purge_receipts (
  receipt_id text primary key check (receipt_id ~ '^rpr_[a-z0-9]{1,64}$'),
  workspace_kind text not null check (workspace_kind in ('backup', 'restore')),
  workspace_id text not null check (workspace_id ~ '^(rbw|rrw)_[a-z0-9]{1,64}$'),
  receipt_digest bytea not null unique check (octet_length(receipt_digest) = 32),
  canonical_receipt bytea not null check (octet_length(canonical_receipt) between 1 and 1048576),
  recorded_at timestamptz not null default now(),
  check (receipt_digest = record_platform_internal.digest(canonical_receipt, 'sha256'))
);

create trigger recovery_trust_entries_immutable
before update or delete or truncate on public.recovery_trust_entries
for each statement execute function record_platform_internal.reject_immutable_mutation();

create trigger recovery_point_manifests_immutable
before update or delete or truncate on public.recovery_point_manifests
for each statement execute function record_platform_internal.reject_immutable_mutation();

create trigger recovery_purge_receipts_immutable
before update or delete or truncate on public.recovery_purge_receipts
for each statement execute function record_platform_internal.reject_immutable_mutation();
