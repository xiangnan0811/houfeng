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
-- Finalize implementation is owned by Slice 5.
-- HOUFENG-APP-ACL-R2-FINALIZE-END
