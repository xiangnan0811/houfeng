-- The Compose Records authority owns only a short membership lease. The
-- durable deployment contract remains projected through the existing signed
-- activation command and the application runtime remains unable to edit
-- membership rows directly.
create or replace function public.record_platform_compose_membership_heartbeat(bytea)
returns timestamptz
language plpgsql
security definer
set search_path = pg_catalog
as $$
declare
  p_command alias for $1;
  v_deployment_id text;
  v_deployment_epoch bigint;
  v_fence_contract_version bigint;
  v_issued_at_epoch bigint;
  v_expires_at_epoch bigint;
  v_now_epoch bigint;
  v_heartbeat_expires_at timestamptz;
  v_contract public.deployment_contract_state%rowtype;
  v_membership public.deployment_membership%rowtype;
begin
  perform record_platform_internal.record_platform_projection_validate_header_v1(
    p_command,
    3,
    10,
    177
  );

  v_deployment_id := record_platform_internal.record_platform_projection_read_token_v1(
    p_command,
    37,
    'dp-'
  );
  if record_platform_internal.record_platform_projection_read_bytes_v1(p_command, 104, 7)
      <> pg_catalog.convert_to('default', 'UTF8')
    or record_platform_internal.record_platform_projection_read_bytes_v1(p_command, 111, 14)
      <> pg_catalog.convert_to('compose-center', 'UTF8')
    or record_platform_internal.record_platform_projection_read_bytes_v1(p_command, 125, 3)
      <> pg_catalog.convert_to('api', 'UTF8')
    or record_platform_internal.record_platform_projection_read_bytes_v1(p_command, 128, 15)
      <> pg_catalog.convert_to('records.runtime', 'UTF8')
    or pg_catalog.get_byte(p_command, 159) <> 1
    or pg_catalog.get_byte(p_command, 160) <> 0 then
    raise exception using
      errcode = '22023',
      message = 'invalid Compose membership heartbeat identity';
  end if;

  v_deployment_epoch := record_platform_internal.record_platform_projection_read_uint64_v1(p_command, 143);
  v_fence_contract_version := record_platform_internal.record_platform_projection_read_uint64_v1(p_command, 151);
  v_issued_at_epoch := record_platform_internal.record_platform_projection_read_uint64_v1(p_command, 161);
  v_expires_at_epoch := record_platform_internal.record_platform_projection_read_uint64_v1(p_command, 169);
  if v_deployment_epoch <= 0 or v_fence_contract_version <= 0 then
    raise exception using
      errcode = '22023',
      message = 'invalid Compose membership heartbeat contract version';
  end if;

  v_now_epoch := pg_catalog.floor(
    pg_catalog.date_part('epoch', pg_catalog.transaction_timestamp())
  )::bigint;
  if v_issued_at_epoch < v_now_epoch - 30
    or v_issued_at_epoch > v_now_epoch + 30
    or v_expires_at_epoch <= v_now_epoch
    or v_expires_at_epoch > v_now_epoch + 120 then
    raise exception using
      errcode = '22023',
      message = 'invalid Compose membership heartbeat time window';
  end if;
  if v_expires_at_epoch <> v_issued_at_epoch + 90 then
    raise exception using
      errcode = '22023',
      message = 'invalid Compose membership heartbeat lease duration';
  end if;
  v_heartbeat_expires_at := pg_catalog.to_timestamp(v_expires_at_epoch);

  select *
  into v_contract
  from public.deployment_contract_state
  where project_id = 'default'
  for update;
  if not found
    or v_contract.deployment_id is distinct from v_deployment_id
    or v_contract.active_profile is distinct from 'postgres_sync'
    or v_contract.active_domain_identity_epoch is distinct from v_deployment_epoch
    or v_contract.minimum_fence_contract_version is distinct from v_fence_contract_version then
    raise exception using
      errcode = '55000',
      message = 'Compose membership heartbeat does not match the active deployment contract';
  end if;

  select *
  into v_membership
  from public.deployment_membership
  where instance_id = 'compose-center'
  for update;
  if found then
    if v_membership.deployment_id is distinct from v_deployment_id
      or v_membership.project_id is distinct from 'default'
      or v_membership.instance_kind is distinct from 'api'
      or v_membership.deployment_epoch is distinct from v_deployment_epoch
      or v_membership.fence_contract_version is distinct from v_fence_contract_version
      or v_membership.capability is distinct from 'records.runtime'
      or v_membership.load_balancer_admitted is distinct from true
      or v_membership.queue_admitted is distinct from false then
      raise exception using
        errcode = '55000',
        message = 'Compose membership heartbeat conflicts with existing membership identity';
    end if;

    update public.deployment_membership
    set heartbeat_expires_at = v_heartbeat_expires_at,
        updated_at = pg_catalog.transaction_timestamp()
    where instance_id = 'compose-center';
  else
    insert into public.deployment_membership(
      instance_id,
      deployment_id,
      project_id,
      instance_kind,
      deployment_epoch,
      fence_contract_version,
      capability,
      load_balancer_admitted,
      queue_admitted,
      heartbeat_expires_at,
      created_at,
      updated_at
    ) values (
      'compose-center',
      v_deployment_id,
      'default',
      'api',
      v_deployment_epoch,
      v_fence_contract_version,
      'records.runtime',
      true,
      false,
      v_heartbeat_expires_at,
      pg_catalog.transaction_timestamp(),
      pg_catalog.transaction_timestamp()
    );
  end if;

  return v_heartbeat_expires_at;
end
$$;

revoke all on function public.record_platform_compose_membership_heartbeat(bytea) from public;
