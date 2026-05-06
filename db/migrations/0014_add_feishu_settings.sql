alter table center_settings
  add column if not exists feishu_enabled boolean not null default false;

alter table center_settings
  add column if not exists feishu_webhook_url text not null default '';
