create table if not exists asset_services (
  service_id text primary key,
  vps_id text not null,
  target_id text,
  name text not null,
  service_type text not null,
  status text not null default 'active',
  url text not null default '',
  port integer,
  labels text[] not null default '{}',
  note text not null default '',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  constraint asset_services_vps_fk foreign key (vps_id) references vps_assets(vps_id) on delete cascade,
  constraint asset_services_target_fk foreign key (target_id) references targets(target_id) on delete set null,
  constraint asset_services_name_not_blank check (length(btrim(name)) > 0),
  constraint asset_services_type_allowed check (
    service_type in ('web', 'api', 'database', 'worker', 'proxy', 'other')
  ),
  constraint asset_services_status_allowed check (
    status in ('active', 'paused', 'retired', 'unknown')
  ),
  constraint asset_services_port_range check (
    port is null or port between 1 and 65535
  )
);

create index if not exists idx_asset_services_vps on asset_services(vps_id, lower(name), service_id);
create index if not exists idx_asset_services_target on asset_services(target_id) where target_id is not null;
create index if not exists idx_asset_services_status on asset_services(status);
create index if not exists idx_asset_services_type on asset_services(service_type);
