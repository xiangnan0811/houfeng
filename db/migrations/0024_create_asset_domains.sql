create table if not exists asset_domains (
  domain_id text primary key,
  vps_id text not null,
  service_id text,
  target_id text,
  domain_name text not null,
  purpose text not null default '',
  status text not null default 'active',
  registrar text not null default '',
  expires_at date,
  auto_renew boolean not null default false,
  https_enabled boolean not null default false,
  labels text[] not null default '{}',
  note text not null default '',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  constraint asset_domains_vps_fk foreign key (vps_id) references vps_assets(vps_id) on delete cascade,
  constraint asset_domains_service_fk foreign key (service_id) references asset_services(service_id) on delete set null,
  constraint asset_domains_target_fk foreign key (target_id) references targets(target_id) on delete set null,
  constraint asset_domains_name_unique unique (domain_name),
  constraint asset_domains_name_not_blank check (length(btrim(domain_name)) > 0),
  constraint asset_domains_name_normalized check (
    domain_name = lower(btrim(domain_name)) and domain_name !~ '\s'
  ),
  constraint asset_domains_status_allowed check (
    status in ('active', 'paused', 'retired', 'unknown')
  )
);

create index if not exists idx_asset_domains_vps on asset_domains(vps_id, lower(domain_name), domain_id);
create index if not exists idx_asset_domains_service on asset_domains(service_id) where service_id is not null;
create index if not exists idx_asset_domains_target on asset_domains(target_id) where target_id is not null;
create index if not exists idx_asset_domains_status on asset_domains(status);
create index if not exists idx_asset_domains_expires_at on asset_domains(expires_at) where expires_at is not null;
