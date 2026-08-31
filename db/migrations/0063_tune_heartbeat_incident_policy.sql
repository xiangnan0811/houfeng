alter table center_settings
  alter column incident_defaults set default '{"heartbeat_interval_seconds":5,"stale_threshold_intervals":12,"sweep_interval_seconds":5,"notify_on_started":true,"notify_on_escalated":true,"notify_on_recovered":true}'::jsonb;

update center_settings
set incident_defaults = incident_defaults || '{"stale_threshold_intervals":12}'::jsonb,
    updated_at = now()
where incident_defaults->>'stale_threshold_intervals' = '3';

create index if not exists idx_monitoring_instance_heartbeats_live_received
  on monitoring_instance_heartbeats (monitoring_instance_id, received_at desc, id desc)
  include (sync_batch_id)
  where is_backfilled = false;
