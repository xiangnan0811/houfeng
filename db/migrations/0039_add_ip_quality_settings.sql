alter table if exists center_settings
  add column if not exists ip_quality_settings jsonb not null default '{
    "enabled":false,
    "frequency_seconds":86400,
    "timeout_seconds":15,
    "raw_retention_days":90,
    "history_retention_days":365,
    "services":["netflix","chatgpt","youtube-premium","amazon-prime-video","disney-plus","tiktok","reddit"]
  }'::jsonb;

update center_settings
set ip_quality_settings = '{
    "enabled":false,
    "frequency_seconds":86400,
    "timeout_seconds":15,
    "raw_retention_days":90,
    "history_retention_days":365,
    "services":["netflix","chatgpt","youtube-premium","amazon-prime-video","disney-plus","tiktok","reddit"]
  }'::jsonb
where ip_quality_settings is null
   or jsonb_typeof(ip_quality_settings) <> 'object';
