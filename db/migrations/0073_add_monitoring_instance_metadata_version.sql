-- 监控实例资料（group / labels / note）的并发令牌。updated_at 会被同步、在线信号与事件摘要持续推进，
-- 不能再作为资料编辑的 If-Match；只有资料写入才推进 metadata_updated_at。
alter table public.monitoring_instances
  add column if not exists metadata_updated_at timestamptz;

update public.monitoring_instances
set metadata_updated_at = updated_at
where metadata_updated_at is null;

alter table public.monitoring_instances
  alter column metadata_updated_at set default now(),
  alter column metadata_updated_at set not null;
