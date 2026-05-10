create table if not exists experience_logs (
  experience_log_id text primary key,
  vps_id text not null references vps_assets(vps_id) on delete cascade,
  category text not null,
  severity text not null,
  summary text not null,
  details text not null default '',
  occurred_at timestamptz not null default now(),
  created_at timestamptz not null default now(),
  constraint experience_logs_category_allowed check (
    category in ('note', 'stability', 'network', 'support', 'billing', 'migration', 'cancellation')
  ),
  constraint experience_logs_severity_allowed check (
    severity in ('info', 'warning', 'critical')
  ),
  constraint experience_logs_summary_not_blank check (length(btrim(summary)) > 0),
  constraint experience_logs_details_not_null check (details is not null)
);

do $$
begin
  if not exists (
    select 1
    from pg_constraint
    where conname = 'experience_logs_category_allowed'
      and conrelid = 'experience_logs'::regclass
  ) then
    alter table experience_logs
      add constraint experience_logs_category_allowed check (
        category in ('note', 'stability', 'network', 'support', 'billing', 'migration', 'cancellation')
      );
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'experience_logs_severity_allowed'
      and conrelid = 'experience_logs'::regclass
  ) then
    alter table experience_logs
      add constraint experience_logs_severity_allowed check (
        severity in ('info', 'warning', 'critical')
      );
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'experience_logs_summary_not_blank'
      and conrelid = 'experience_logs'::regclass
  ) then
    alter table experience_logs
      add constraint experience_logs_summary_not_blank check (length(btrim(summary)) > 0);
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'experience_logs_details_not_null'
      and conrelid = 'experience_logs'::regclass
  ) then
    alter table experience_logs
      add constraint experience_logs_details_not_null check (details is not null);
  end if;
end $$;

create index if not exists idx_experience_logs_vps_time
  on experience_logs (vps_id, occurred_at desc, created_at desc);

create index if not exists idx_experience_logs_category
  on experience_logs (category);

create index if not exists idx_experience_logs_severity
  on experience_logs (severity);
