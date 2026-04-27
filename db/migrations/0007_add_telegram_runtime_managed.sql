alter table center_settings
  add column if not exists telegram_runtime_managed boolean not null default false;
