-- HOUFENG-APP-ACL-R2-PRIVILEGED-TRANSITION-V1
--
-- This source is intentionally isolated from the generic application migration
-- inventory. Its complete bytes and marker-inclusive section bytes are bound
-- by the R2 receipt without exposing 0052 to the root migration filesystem.
--
-- HOUFENG-APP-ACL-R2-BOOTSTRAP-BEGIN
create function record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(bytea, bytea)
returns boolean
language sql
immutable
parallel safe
set search_path = pg_catalog
as $app_acl_r2_assert_bootstrap_receipt_insert$
  select pg_catalog.octet_length($1) between 1 and 4194304
    and pg_catalog.octet_length($2) = 32
    and record_platform_internal.digest($1, 'sha256') = $2
$app_acl_r2_assert_bootstrap_receipt_insert$;

create function record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation()
returns trigger
language plpgsql
volatile
parallel unsafe
set search_path = pg_catalog
as $app_acl_r2_reject_bootstrap_receipt_mutation$
begin
  raise sqlstate '55000'
    using message = 'app_acl_r2_bootstrap_receipt is immutable';
end;
$app_acl_r2_reject_bootstrap_receipt_mutation$;

create table public.app_acl_r2_bootstrap_receipt (
  singleton boolean primary key default true check (singleton),
  receipt_body bytea not null
    check (pg_catalog.octet_length(receipt_body) between 1 and 4194304),
  receipt_digest bytea not null
    check (pg_catalog.octet_length(receipt_digest) = 32),
  check (record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(
    receipt_body,
    receipt_digest
  ))
);

create trigger app_acl_r2_bootstrap_receipt_immutable
before update or delete or truncate on public.app_acl_r2_bootstrap_receipt
for each statement
execute function record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation();

revoke all privileges on table public.app_acl_r2_bootstrap_receipt from public;
revoke all privileges on function record_platform_internal.app_acl_r2_assert_bootstrap_receipt_insert(bytea, bytea) from public;
revoke all privileges on function record_platform_internal.app_acl_r2_reject_bootstrap_receipt_mutation() from public;
-- HOUFENG-APP-ACL-R2-BOOTSTRAP-END
--
-- HOUFENG-APP-ACL-R2-FINALIZE-BEGIN
create function record_platform_internal.app_acl_r2_reject_manifest_mutation()
returns trigger
language plpgsql
volatile
parallel unsafe
set search_path = pg_catalog
as $app_acl_r2_reject_manifest_mutation$
begin
  raise sqlstate '55000'
    using message = 'app_acl_r2_manifest is immutable';
end;
$app_acl_r2_reject_manifest_mutation$;

create table public.app_acl_r2_manifest_revisions (
  protocol_version smallint not null check (protocol_version = 2),
  manifest_revision bigint not null check (manifest_revision = 2),
  m1_revision bigint not null check (m1_revision = 1),
  m1_manifest_digest bytea not null check (octet_length(m1_manifest_digest) = 32),
  m1_source_set_digest bytea not null check (octet_length(m1_source_set_digest) = 32),
  m1_privilege_set_digest bytea not null check (octet_length(m1_privilege_set_digest) = 32),
  m1_migrator_catalog_role text not null,
  direct_migrator_name text not null,
  direct_migrator_oid oid not null,
  r2_source_set_body bytea not null,
  r2_source_set_digest bytea not null check (octet_length(r2_source_set_digest) = 32),
  r2_privilege_set_body bytea not null,
  r2_privilege_set_digest bytea not null check (octet_length(r2_privilege_set_digest) = 32),
  domain_body bytea not null,
  domain_digest bytea not null check (octet_length(domain_digest) = 32),
  receipt_digest bytea not null check (octet_length(receipt_digest) = 32),
  control_acl_body bytea not null,
  control_acl_digest bytea not null check (octet_length(control_acl_digest) = 32),
  manifest_digest bytea not null check (octet_length(manifest_digest) = 32),
  recorded_at timestamptz not null default transaction_timestamp(),
  primary key (protocol_version, manifest_revision),
  unique (protocol_version, manifest_revision, manifest_digest),
  check (r2_source_set_digest = record_platform_internal.digest(r2_source_set_body, 'sha256')),
  check (r2_privilege_set_digest = record_platform_internal.digest(r2_privilege_set_body, 'sha256')),
  check (domain_digest = record_platform_internal.digest(domain_body, 'sha256')),
  check (control_acl_digest = record_platform_internal.digest(control_acl_body, 'sha256')),
  check (
    manifest_digest = record_platform_internal.digest(
      convert_to('HOUFENG-APP-ACL-MANIFEST-R2-V1', 'UTF8')
      || int2send(1::smallint)
      || int2send(protocol_version)
      || int8send(manifest_revision)
      || int8send(m1_revision)
      || m1_manifest_digest
      || m1_source_set_digest
      || m1_privilege_set_digest
      || int2send(octet_length(m1_migrator_catalog_role)::smallint)
      || convert_to(m1_migrator_catalog_role, 'UTF8')
      || int2send(octet_length(direct_migrator_name)::smallint)
      || convert_to(direct_migrator_name, 'UTF8')
      || int4send(direct_migrator_oid::integer)
      || int4send(octet_length(r2_source_set_body))
      || r2_source_set_body
      || r2_source_set_digest
      || int4send(octet_length(r2_privilege_set_body))
      || r2_privilege_set_body
      || r2_privilege_set_digest
      || int4send(octet_length(domain_body))
      || domain_body
      || domain_digest
      || receipt_digest
      || int4send(octet_length(control_acl_body))
      || control_acl_body
      || control_acl_digest
      || int8send((extract(epoch from recorded_at) * 1000000)::bigint),
      'sha256'
    )
  ),
  foreign key (m1_revision, m1_manifest_digest)
    references public.app_acl_manifest_revisions(manifest_revision, manifest_digest)
    on delete restrict
);

create table public.app_acl_r2_manifest_head (
  singleton boolean primary key default true check (singleton),
  protocol_version smallint not null check (protocol_version = 2),
  manifest_revision bigint not null check (manifest_revision = 2),
  manifest_digest bytea not null check (octet_length(manifest_digest) = 32),
  foreign key (protocol_version, manifest_revision, manifest_digest)
    references public.app_acl_r2_manifest_revisions(protocol_version, manifest_revision, manifest_digest)
    on delete restrict
);

create trigger app_acl_r2_manifest_revisions_immutable
before update or delete or truncate on public.app_acl_r2_manifest_revisions
for each statement
execute function record_platform_internal.app_acl_r2_reject_manifest_mutation();

create trigger app_acl_r2_manifest_head_immutable
before update or delete or truncate on public.app_acl_r2_manifest_head
for each statement
execute function record_platform_internal.app_acl_r2_reject_manifest_mutation();
-- HOUFENG-APP-ACL-R2-FINALIZE-END
