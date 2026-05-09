create table if not exists vps_assets (
  vps_id text primary key,
  display_name text not null,
  provider_id text references providers(provider_id) on delete set null,
  provider_name text not null default '',
  product_name text not null default '',
  order_ref text not null default '',
  country text not null default '',
  region text not null default '',
  city text not null default '',
  datacenter text not null default '',
  ipv4 text not null default '',
  ipv6 text not null default '',
  ssh_host text not null default '',
  ssh_port integer not null default 22,
  ssh_user text not null default '',
  os_name text not null default '',
  virtualization text not null default '',
  lifecycle_status text not null,
  usage_status text not null,
  renewal_decision text not null default 'unreviewed',
  importance text not null default 'normal',
  labels text[] not null default '{}',
  note text not null default '',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  archived_at timestamptz,
  constraint vps_assets_display_name_not_blank check (length(btrim(display_name)) > 0),
  constraint vps_assets_lifecycle_status_allowed check (
    lifecycle_status in ('active', 'idle', 'testing', 'to_migrate', 'to_cancel', 'cancelled', 'archived')
  ),
  constraint vps_assets_usage_status_allowed check (
    usage_status in ('in_use', 'idle', 'standby', 'testing', 'unknown')
  ),
  constraint vps_assets_renewal_decision_allowed check (
    renewal_decision in ('unreviewed', 'keep', 'observe', 'migrate', 'cancel', 'auto_renew_cancelled', 'replaced')
  ),
  constraint vps_assets_ssh_port_range check (ssh_port between 1 and 65535)
);

do $$
begin
  if not exists (
    select 1 from pg_constraint
    where conname = 'vps_assets_display_name_not_blank'
      and conrelid = 'vps_assets'::regclass
  ) then
    alter table vps_assets
      add constraint vps_assets_display_name_not_blank check (length(btrim(display_name)) > 0);
  end if;

  if not exists (
    select 1 from pg_constraint
    where conname = 'vps_assets_lifecycle_status_allowed'
      and conrelid = 'vps_assets'::regclass
  ) then
    alter table vps_assets
      add constraint vps_assets_lifecycle_status_allowed check (
        lifecycle_status in ('active', 'idle', 'testing', 'to_migrate', 'to_cancel', 'cancelled', 'archived')
      );
  end if;

  if not exists (
    select 1 from pg_constraint
    where conname = 'vps_assets_usage_status_allowed'
      and conrelid = 'vps_assets'::regclass
  ) then
    alter table vps_assets
      add constraint vps_assets_usage_status_allowed check (
        usage_status in ('in_use', 'idle', 'standby', 'testing', 'unknown')
      );
  end if;

  if not exists (
    select 1 from pg_constraint
    where conname = 'vps_assets_renewal_decision_allowed'
      and conrelid = 'vps_assets'::regclass
  ) then
    alter table vps_assets
      add constraint vps_assets_renewal_decision_allowed check (
        renewal_decision in ('unreviewed', 'keep', 'observe', 'migrate', 'cancel', 'auto_renew_cancelled', 'replaced')
      );
  end if;

  if not exists (
    select 1 from pg_constraint
    where conname = 'vps_assets_ssh_port_range'
      and conrelid = 'vps_assets'::regclass
  ) then
    alter table vps_assets
      add constraint vps_assets_ssh_port_range check (ssh_port between 1 and 65535);
  end if;
end $$;

create index if not exists idx_vps_assets_provider on vps_assets (provider_id);
create index if not exists idx_vps_assets_status on vps_assets (lifecycle_status, usage_status, renewal_decision);
create index if not exists idx_vps_assets_location on vps_assets (country, region, city);
