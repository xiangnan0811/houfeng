alter table center_settings
  alter column retention_policy set default '{"raw_layer_days":30,"aggregate_layer_days":30,"event_layer_days":90,"notification_layer_days":180}'::jsonb;

update center_settings
set retention_policy = jsonb_set(
  coalesce(retention_policy, '{}'::jsonb),
  '{raw_layer_days}',
  to_jsonb(greatest(coalesce((retention_policy->>'raw_layer_days')::integer, 30), 30)),
  true
)
where coalesce((retention_policy->>'raw_layer_days')::integer, 0) < 30;
