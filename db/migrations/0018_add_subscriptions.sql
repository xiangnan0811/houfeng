create table if not exists subscriptions (
  subscription_id text primary key,
  vps_id text not null references vps_assets(vps_id) on delete cascade,
  price numeric(12, 2) not null,
  currency text not null,
  billing_cycle text not null default '',
  billing_months integer not null,
  monthly_price numeric(12, 4) not null,
  started_at date,
  renew_at date,
  auto_renew boolean not null default false,
  auto_renew_cancelled boolean not null default false,
  status text not null default 'active',
  payment_method text not null default '',
  note text not null default '',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  constraint subscriptions_price_non_negative check (price >= 0),
  constraint subscriptions_billing_months_positive check (billing_months > 0),
  constraint subscriptions_currency_code check (currency = upper(currency) and currency ~ '^[A-Z]{3}$'),
  constraint subscriptions_status_allowed check (status in ('active', 'paused', 'cancelled', 'expired', 'unknown'))
);

do $$
begin
  if not exists (
    select 1
    from pg_constraint
    where conname = 'subscriptions_price_non_negative'
      and conrelid = 'subscriptions'::regclass
  ) then
    alter table subscriptions
      add constraint subscriptions_price_non_negative check (price >= 0);
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'subscriptions_billing_months_positive'
      and conrelid = 'subscriptions'::regclass
  ) then
    alter table subscriptions
      add constraint subscriptions_billing_months_positive check (billing_months > 0);
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'subscriptions_currency_code'
      and conrelid = 'subscriptions'::regclass
  ) then
    alter table subscriptions
      add constraint subscriptions_currency_code check (currency = upper(currency) and currency ~ '^[A-Z]{3}$');
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'subscriptions_status_allowed'
      and conrelid = 'subscriptions'::regclass
  ) then
    alter table subscriptions
      add constraint subscriptions_status_allowed check (status in ('active', 'paused', 'cancelled', 'expired', 'unknown'));
  end if;
end $$;

create index if not exists idx_subscriptions_vps on subscriptions (vps_id);
create index if not exists idx_subscriptions_renew_at on subscriptions (renew_at);
create index if not exists idx_subscriptions_status on subscriptions (status);
