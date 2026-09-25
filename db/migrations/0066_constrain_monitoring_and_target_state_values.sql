do $$
declare
  invalid_id text;
  invalid_value text;
begin
  select monitoring_instance_id, lifecycle_status
  into invalid_id, invalid_value
  from monitoring_instances
  where lifecycle_status not in ('待接入', '在用', '观察中', '不续费', '已退役')
  order by monitoring_instance_id
  limit 1;
  if found then
    raise exception '0066 enum validation failed: monitoring_instances.lifecycle_status id=% value=%', invalid_id, invalid_value
      using errcode = '23514';
  end if;

  select monitoring_instance_id, monitoring_status
  into invalid_id, invalid_value
  from monitoring_instances
  where monitoring_status not in ('启用', '维护中', '暂停')
  order by monitoring_instance_id
  limit 1;
  if found then
    raise exception '0066 enum validation failed: monitoring_instances.monitoring_status id=% value=%', invalid_id, invalid_value
      using errcode = '23514';
  end if;

  select monitoring_instance_id, binding_status
  into invalid_id, invalid_value
  from monitoring_instances
  where binding_status not in ('未绑定', '已绑定', '指纹变更待确认')
  order by monitoring_instance_id
  limit 1;
  if found then
    raise exception '0066 enum validation failed: monitoring_instances.binding_status id=% value=%', invalid_id, invalid_value
      using errcode = '23514';
  end if;

  select target_id, run_status
  into invalid_id, invalid_value
  from targets
  where run_status not in ('启用', '维护中', '暂停', '已归档')
  order by target_id
  limit 1;
  if found then
    raise exception '0066 enum validation failed: targets.run_status id=% value=%', invalid_id, invalid_value
      using errcode = '23514';
  end if;
end
$$;

alter table monitoring_instances
  add constraint monitoring_instances_lifecycle_status_allowed check (
    lifecycle_status in ('待接入', '在用', '观察中', '不续费', '已退役')
  );

alter table monitoring_instances
  add constraint monitoring_instances_monitoring_status_allowed check (
    monitoring_status in ('启用', '维护中', '暂停')
  );

alter table monitoring_instances
  add constraint monitoring_instances_binding_status_allowed check (
    binding_status in ('未绑定', '已绑定', '指纹变更待确认')
  );

alter table targets
  add constraint targets_run_status_allowed check (
    run_status in ('启用', '维护中', '暂停', '已归档')
  );
