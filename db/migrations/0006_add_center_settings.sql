create table if not exists center_settings (
  settings_id text primary key check (settings_id = 'center'),
  telegram_bot_token text not null default '',
  telegram_chat_id text not null default '',
  host_sample_frequency_tier text not null default '5m',
  probe_frequency_defaults jsonb not null default '{"tcp":"5m","http":"5m","tls":"5m"}'::jsonb,
  incident_defaults jsonb not null default '{"heartbeat_interval_seconds":30,"stale_threshold_intervals":3,"sweep_interval_seconds":60,"notify_on_started":true,"notify_on_escalated":true,"notify_on_recovered":true}'::jsonb,
  override_rules jsonb not null default '{"node_labels":[],"target_types":[],"target_labels":[]}'::jsonb,
  retention_policy jsonb not null default '{"raw_layer_days":7,"aggregate_layer_days":30,"event_layer_days":90,"notification_layer_days":180}'::jsonb,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);
