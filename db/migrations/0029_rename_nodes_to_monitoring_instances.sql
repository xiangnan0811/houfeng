do $$
begin
  if to_regclass('public.nodes') is not null and to_regclass('public.monitoring_instances') is null then
    alter table nodes rename to monitoring_instances;
  end if;
  if to_regclass('public.node_heartbeats') is not null and to_regclass('public.monitoring_instance_heartbeats') is null then
    alter table node_heartbeats rename to monitoring_instance_heartbeats;
  end if;
  if to_regclass('public.node_host_sample_daily_aggregates') is not null and to_regclass('public.monitoring_instance_host_sample_daily_aggregates') is null then
    alter table node_host_sample_daily_aggregates rename to monitoring_instance_host_sample_daily_aggregates;
  end if;
  if to_regclass('public.vps_node_links') is not null and to_regclass('public.vps_monitoring_instance_links') is null then
    alter table vps_node_links rename to vps_monitoring_instance_links;
  end if;
end $$;

do $$
begin
  if exists (select 1 from information_schema.columns where table_name = 'monitoring_instances' and column_name = 'node_id') then
    alter table monitoring_instances rename column node_id to monitoring_instance_id;
  end if;
  if exists (select 1 from information_schema.columns where table_name = 'monitoring_instance_heartbeats' and column_name = 'node_id') then
    alter table monitoring_instance_heartbeats rename column node_id to monitoring_instance_id;
  end if;
  if exists (select 1 from information_schema.columns where table_name = 'host_samples' and column_name = 'node_id') then
    alter table host_samples rename column node_id to monitoring_instance_id;
  end if;
  if exists (select 1 from information_schema.columns where table_name = 'probe_observations' and column_name = 'node_id') then
    alter table probe_observations rename column node_id to monitoring_instance_id;
  end if;
  if exists (select 1 from information_schema.columns where table_name = 'monitoring_instance_host_sample_daily_aggregates' and column_name = 'node_id') then
    alter table monitoring_instance_host_sample_daily_aggregates rename column node_id to monitoring_instance_id;
  end if;
  if exists (select 1 from information_schema.columns where table_name = 'vps_monitoring_instance_links' and column_name = 'node_id') then
    alter table vps_monitoring_instance_links rename column node_id to monitoring_instance_id;
  end if;
  if exists (select 1 from information_schema.columns where table_name = 'targets' and column_name = 'execution_node_labels') then
    alter table targets rename column execution_node_labels to execution_monitoring_instance_labels;
  end if;
end $$;

alter index if exists nodes_pkey rename to monitoring_instances_pkey;
alter index if exists node_heartbeats_pkey rename to monitoring_instance_heartbeats_pkey;
alter index if exists node_host_sample_daily_aggregates_pkey rename to monitoring_instance_host_sample_daily_aggregates_pkey;
alter index if exists vps_node_links_pkey rename to vps_monitoring_instance_links_pkey;
alter index if exists idx_nodes_labels_gin rename to idx_monitoring_instances_labels_gin;
alter index if exists idx_nodes_enrollment_token_active rename to idx_monitoring_instances_enrollment_token_active;
alter index if exists idx_node_heartbeats_node_time rename to idx_monitoring_instance_heartbeats_instance_time;
alter index if exists idx_host_samples_node_time rename to idx_host_samples_monitoring_instance_time;
alter index if exists idx_probe_observations_node_time rename to idx_probe_observations_monitoring_instance_time;
alter index if exists idx_node_host_sample_daily_aggregates_bucket rename to idx_monitoring_instance_host_sample_daily_aggregates_bucket;
alter index if exists idx_vps_node_links_pair_active rename to idx_vps_monitoring_instance_links_pair_active;
alter index if exists idx_vps_node_links_vps_active rename to idx_vps_monitoring_instance_links_vps_active;
alter index if exists idx_vps_node_links_node_active rename to idx_vps_monitoring_instance_links_monitoring_instance_active;

do $$
begin
  if exists (select 1 from pg_constraint where conname = 'node_heartbeats_node_id_fkey') then
    alter table monitoring_instance_heartbeats rename constraint node_heartbeats_node_id_fkey to monitoring_instance_heartbeats_monitoring_instance_id_fkey;
  end if;
  if exists (select 1 from pg_constraint where conname = 'host_samples_node_id_fkey') then
    alter table host_samples rename constraint host_samples_node_id_fkey to host_samples_monitoring_instance_id_fkey;
  end if;
  if exists (select 1 from pg_constraint where conname = 'probe_observations_node_id_fkey') then
    alter table probe_observations rename constraint probe_observations_node_id_fkey to probe_observations_monitoring_instance_id_fkey;
  end if;
  if exists (select 1 from pg_constraint where conname = 'node_host_sample_daily_aggregates_node_id_fkey') then
    alter table monitoring_instance_host_sample_daily_aggregates rename constraint node_host_sample_daily_aggregates_node_id_fkey to monitoring_instance_host_sample_daily_aggregates_monitoring_instance_id_fkey;
  end if;
  if exists (select 1 from pg_constraint where conname = 'vps_node_links_node_id_fkey') then
    alter table vps_monitoring_instance_links rename constraint vps_node_links_node_id_fkey to vps_monitoring_instance_links_monitoring_instance_id_fkey;
  end if;
  if exists (select 1 from pg_constraint where conname = 'vps_node_links_note_not_null') then
    alter table vps_monitoring_instance_links rename constraint vps_node_links_note_not_null to vps_monitoring_instance_links_note_not_null;
  end if;
end $$;

update active_incidents
set object_type = 'monitoring_instance'
where object_type = 'node';

update active_incidents
set incident_class = regexp_replace(incident_class, '^node_', 'monitoring_instance_')
where incident_class like 'node\_%';

update state_change_events
set object_type = 'monitoring_instance'
where object_type = 'node';

update state_change_events
set event_type = regexp_replace(event_type, '^node_', 'monitoring_instance_')
where event_type like 'node\_%';

update notification_records
set object_type = 'monitoring_instance'
where object_type = 'node';

alter table if exists asset_lifecycle_action_steps
  drop constraint if exists asset_lifecycle_action_steps_object_type_allowed;

alter table if exists asset_lifecycle_action_steps
  drop constraint if exists asset_lifecycle_action_steps_step_type_allowed;

update asset_lifecycle_action_steps
set object_type = 'monitoring_instance'
where object_type = 'node';

update asset_lifecycle_action_steps
set step_type = case step_type
  when 'node_lifecycle' then 'monitoring_instance_lifecycle'
  when 'node_monitoring' then 'monitoring_instance_monitoring'
  else step_type
end
where step_type in ('node_lifecycle', 'node_monitoring');

alter table if exists asset_lifecycle_action_steps
  add constraint asset_lifecycle_action_steps_object_type_allowed check (
    object_type in ('vps', 'subscription', 'monitoring_instance', 'target')
  );

alter table if exists asset_lifecycle_action_steps
  add constraint asset_lifecycle_action_steps_step_type_allowed check (
    step_type in ('vps_lifecycle', 'subscription_status', 'monitoring_instance_lifecycle', 'monitoring_instance_monitoring', 'target_run_status')
  );

update center_settings
set override_rules =
  (override_rules - 'node_labels') ||
  jsonb_build_object(
    'monitoring_instance_labels',
    coalesce(override_rules->'monitoring_instance_labels', override_rules->'node_labels', '[]'::jsonb)
  )
where override_rules ? 'node_labels' or not override_rules ? 'monitoring_instance_labels';
