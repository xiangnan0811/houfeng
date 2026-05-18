alter table center_settings
  alter column host_sample_frequency_tier set default '5s';

alter table center_settings
  alter column probe_frequency_defaults set default '{"tcp":"5s","http":"5s","tls":"6h"}'::jsonb;

alter table center_settings
  alter column incident_defaults set default '{"heartbeat_interval_seconds":5,"stale_threshold_intervals":3,"sweep_interval_seconds":5,"notify_on_started":true,"notify_on_escalated":true,"notify_on_recovered":true}'::jsonb;

update center_settings
set
  host_sample_frequency_tier = case
    when host_sample_frequency_tier = '5m' then '5s'
    else host_sample_frequency_tier
  end,
  probe_frequency_defaults =
    coalesce(probe_frequency_defaults, '{}'::jsonb)
    || case when probe_frequency_defaults->>'tcp' = '5m' then '{"tcp":"5s"}'::jsonb else '{}'::jsonb end
    || case when probe_frequency_defaults->>'http' = '5m' then '{"http":"5s"}'::jsonb else '{}'::jsonb end
    || case when probe_frequency_defaults->>'tls' = '5m' then '{"tls":"6h"}'::jsonb else '{}'::jsonb end,
  incident_defaults =
    coalesce(incident_defaults, '{}'::jsonb)
    || case when incident_defaults->>'heartbeat_interval_seconds' = '30' then '{"heartbeat_interval_seconds":5}'::jsonb else '{}'::jsonb end
    || case when incident_defaults->>'sweep_interval_seconds' = '60' then '{"sweep_interval_seconds":5}'::jsonb else '{}'::jsonb end,
  updated_at = now()
where host_sample_frequency_tier = '5m'
  or probe_frequency_defaults->>'tcp' = '5m'
  or probe_frequency_defaults->>'http' = '5m'
  or probe_frequency_defaults->>'tls' = '5m'
  or incident_defaults->>'heartbeat_interval_seconds' = '30'
  or incident_defaults->>'sweep_interval_seconds' = '60';

update probe_items
set frequency_tier = '5s',
    updated_at = now()
where probe_kind in ('tcp', 'http')
  and frequency_tier = '5m';
