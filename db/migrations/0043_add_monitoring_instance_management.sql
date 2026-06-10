alter table monitoring_instances
  add column if not exists archived_at timestamptz;

alter table monitoring_instances
  add column if not exists archived_reason text not null default '';

create index if not exists idx_monitoring_instances_archived_at
  on monitoring_instances (archived_at desc)
  where archived_at is not null;
