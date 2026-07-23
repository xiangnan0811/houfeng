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
