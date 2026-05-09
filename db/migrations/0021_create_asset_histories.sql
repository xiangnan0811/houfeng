create table if not exists price_histories (
  price_history_id text primary key,
  subscription_id text not null references subscriptions(subscription_id) on delete cascade,
  vps_id text not null references vps_assets(vps_id) on delete cascade,
  from_price numeric(12, 2) not null,
  to_price numeric(12, 2) not null,
  from_currency text not null,
  to_currency text not null,
  from_billing_cycle text not null default '',
  to_billing_cycle text not null default '',
  from_billing_months integer not null,
  to_billing_months integer not null,
  from_monthly_price numeric(12, 4) not null,
  to_monthly_price numeric(12, 4) not null,
  from_renew_at date,
  to_renew_at date,
  from_auto_renew boolean not null,
  to_auto_renew boolean not null,
  from_auto_renew_cancelled boolean not null,
  to_auto_renew_cancelled boolean not null,
  from_status text not null,
  to_status text not null,
  changed_at timestamptz not null default now(),
  created_at timestamptz not null default now(),
  constraint price_histories_price_non_negative check (from_price >= 0 and to_price >= 0),
  constraint price_histories_billing_months_positive check (from_billing_months > 0 and to_billing_months > 0),
  constraint price_histories_currency_code check (
    from_currency = upper(from_currency) and from_currency ~ '^[A-Z]{3}$' and
    to_currency = upper(to_currency) and to_currency ~ '^[A-Z]{3}$'
  ),
  constraint price_histories_status_allowed check (
    from_status in ('active', 'paused', 'cancelled', 'expired', 'unknown') and
    to_status in ('active', 'paused', 'cancelled', 'expired', 'unknown')
  )
);

create table if not exists ip_histories (
  ip_history_id text primary key,
  vps_id text not null references vps_assets(vps_id) on delete cascade,
  from_ipv4 text not null default '',
  to_ipv4 text not null default '',
  from_ipv6 text not null default '',
  to_ipv6 text not null default '',
  changed_at timestamptz not null default now(),
  created_at timestamptz not null default now(),
  constraint ip_histories_changed check (from_ipv4 <> to_ipv4 or from_ipv6 <> to_ipv6)
);

create table if not exists vps_spec_snapshots (
  snapshot_id text primary key,
  vps_id text not null references vps_assets(vps_id) on delete cascade,
  product_name text not null default '',
  ssh_host text not null default '',
  ssh_port integer not null,
  ssh_user text not null default '',
  os_name text not null default '',
  virtualization text not null default '',
  captured_at timestamptz not null default now(),
  created_at timestamptz not null default now(),
  constraint vps_spec_snapshots_ssh_port_range check (ssh_port between 1 and 65535)
);

do $$
begin
  if not exists (
    select 1
    from pg_constraint
    where conname = 'price_histories_price_non_negative'
      and conrelid = 'price_histories'::regclass
  ) then
    alter table price_histories
      add constraint price_histories_price_non_negative check (from_price >= 0 and to_price >= 0);
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'price_histories_billing_months_positive'
      and conrelid = 'price_histories'::regclass
  ) then
    alter table price_histories
      add constraint price_histories_billing_months_positive check (from_billing_months > 0 and to_billing_months > 0);
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'price_histories_currency_code'
      and conrelid = 'price_histories'::regclass
  ) then
    alter table price_histories
      add constraint price_histories_currency_code check (
        from_currency = upper(from_currency) and from_currency ~ '^[A-Z]{3}$' and
        to_currency = upper(to_currency) and to_currency ~ '^[A-Z]{3}$'
      );
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'price_histories_status_allowed'
      and conrelid = 'price_histories'::regclass
  ) then
    alter table price_histories
      add constraint price_histories_status_allowed check (
        from_status in ('active', 'paused', 'cancelled', 'expired', 'unknown') and
        to_status in ('active', 'paused', 'cancelled', 'expired', 'unknown')
      );
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'ip_histories_changed'
      and conrelid = 'ip_histories'::regclass
  ) then
    alter table ip_histories
      add constraint ip_histories_changed check (from_ipv4 <> to_ipv4 or from_ipv6 <> to_ipv6);
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'vps_spec_snapshots_ssh_port_range'
      and conrelid = 'vps_spec_snapshots'::regclass
  ) then
    alter table vps_spec_snapshots
      add constraint vps_spec_snapshots_ssh_port_range check (ssh_port between 1 and 65535);
  end if;
end $$;

create index if not exists idx_price_histories_vps_time
  on price_histories (vps_id, changed_at desc, created_at desc);

create index if not exists idx_price_histories_subscription_time
  on price_histories (subscription_id, changed_at desc, created_at desc);

create index if not exists idx_ip_histories_vps_time
  on ip_histories (vps_id, changed_at desc, created_at desc);

create index if not exists idx_vps_spec_snapshots_vps_time
  on vps_spec_snapshots (vps_id, captured_at desc, created_at desc);
