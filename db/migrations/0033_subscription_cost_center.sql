alter table if exists center_settings
  add column if not exists subscription_cost_settings jsonb not null default '{
    "base_currency":"CNY",
    "exchange_rate_provider":"frankfurter",
    "fixer_api_key":"",
    "default_reminder_offsets_days":[14,7,1],
    "max_reminder_lead_days":30,
    "exchange_rate_stale_after_hours":36
  }'::jsonb;

update center_settings
set subscription_cost_settings = '{
    "base_currency":"CNY",
    "exchange_rate_provider":"frankfurter",
    "fixer_api_key":"",
    "default_reminder_offsets_days":[14,7,1],
    "max_reminder_lead_days":30,
    "exchange_rate_stale_after_hours":36
  }'::jsonb
where subscription_cost_settings is null
   or jsonb_typeof(subscription_cost_settings) <> 'object';

alter table if exists subscriptions
  add column if not exists display_name text not null default '';

alter table if exists subscriptions
  add column if not exists cost_category text not null default '';

alter table if exists subscriptions
  add column if not exists labels text[] not null default '{}';

alter table if exists subscriptions
  add column if not exists trial_ends_at date;

alter table if exists subscriptions
  add column if not exists ends_at date;

create table if not exists subscription_exchange_rates (
  rate_id text primary key,
  provider text not null,
  base_currency text not null,
  quote_currency text not null,
  rate numeric(18, 8) not null,
  rate_date date not null,
  fetched_at timestamptz not null default now(),
  error_summary text not null default '',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  constraint subscription_exchange_rates_provider_allowed check (provider in ('frankfurter', 'fixer')),
  constraint subscription_exchange_rates_base_currency_code check (base_currency = upper(base_currency) and base_currency ~ '^[A-Z]{3}$'),
  constraint subscription_exchange_rates_quote_currency_code check (quote_currency = upper(quote_currency) and quote_currency ~ '^[A-Z]{3}$'),
  constraint subscription_exchange_rates_positive check (rate > 0),
  constraint subscription_exchange_rates_distinct_currency check (base_currency <> quote_currency)
);

do $$
begin
  if not exists (
    select 1
    from pg_constraint
    where conname = 'subscription_exchange_rates_provider_allowed'
      and conrelid = 'subscription_exchange_rates'::regclass
  ) then
    alter table subscription_exchange_rates
      add constraint subscription_exchange_rates_provider_allowed check (provider in ('frankfurter', 'fixer'));
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'subscription_exchange_rates_base_currency_code'
      and conrelid = 'subscription_exchange_rates'::regclass
  ) then
    alter table subscription_exchange_rates
      add constraint subscription_exchange_rates_base_currency_code check (base_currency = upper(base_currency) and base_currency ~ '^[A-Z]{3}$');
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'subscription_exchange_rates_quote_currency_code'
      and conrelid = 'subscription_exchange_rates'::regclass
  ) then
    alter table subscription_exchange_rates
      add constraint subscription_exchange_rates_quote_currency_code check (quote_currency = upper(quote_currency) and quote_currency ~ '^[A-Z]{3}$');
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'subscription_exchange_rates_positive'
      and conrelid = 'subscription_exchange_rates'::regclass
  ) then
    alter table subscription_exchange_rates
      add constraint subscription_exchange_rates_positive check (rate > 0);
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'subscription_exchange_rates_distinct_currency'
      and conrelid = 'subscription_exchange_rates'::regclass
  ) then
    alter table subscription_exchange_rates
      add constraint subscription_exchange_rates_distinct_currency check (base_currency <> quote_currency);
  end if;
end $$;

create unique index if not exists idx_subscription_exchange_rates_lookup
  on subscription_exchange_rates (provider, base_currency, quote_currency, rate_date);

create index if not exists idx_subscription_exchange_rates_latest
  on subscription_exchange_rates (provider, base_currency, quote_currency, fetched_at desc, rate_date desc);

create table if not exists subscription_budgets (
  budget_id text primary key,
  scope_type text not null,
  scope_id text not null default '',
  name text not null default '',
  base_currency text not null default 'CNY',
  monthly_limit numeric(12, 2),
  yearly_limit numeric(12, 2),
  warning_pct integer not null default 80,
  enabled boolean not null default true,
  note text not null default '',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  constraint subscription_budgets_scope_allowed check (scope_type in ('global', 'provider', 'label', 'category', 'vps')),
  constraint subscription_budgets_base_currency_code check (base_currency = upper(base_currency) and base_currency ~ '^[A-Z]{3}$'),
  constraint subscription_budgets_limit_present check (monthly_limit is not null or yearly_limit is not null),
  constraint subscription_budgets_monthly_limit_non_negative check (monthly_limit is null or monthly_limit >= 0),
  constraint subscription_budgets_yearly_limit_non_negative check (yearly_limit is null or yearly_limit >= 0),
  constraint subscription_budgets_warning_pct_range check (warning_pct between 1 and 100)
);

do $$
begin
  if not exists (
    select 1
    from pg_constraint
    where conname = 'subscription_budgets_scope_allowed'
      and conrelid = 'subscription_budgets'::regclass
  ) then
    alter table subscription_budgets
      add constraint subscription_budgets_scope_allowed check (scope_type in ('global', 'provider', 'label', 'category', 'vps'));
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'subscription_budgets_base_currency_code'
      and conrelid = 'subscription_budgets'::regclass
  ) then
    alter table subscription_budgets
      add constraint subscription_budgets_base_currency_code check (base_currency = upper(base_currency) and base_currency ~ '^[A-Z]{3}$');
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'subscription_budgets_limit_present'
      and conrelid = 'subscription_budgets'::regclass
  ) then
    alter table subscription_budgets
      add constraint subscription_budgets_limit_present check (monthly_limit is not null or yearly_limit is not null);
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'subscription_budgets_monthly_limit_non_negative'
      and conrelid = 'subscription_budgets'::regclass
  ) then
    alter table subscription_budgets
      add constraint subscription_budgets_monthly_limit_non_negative check (monthly_limit is null or monthly_limit >= 0);
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'subscription_budgets_yearly_limit_non_negative'
      and conrelid = 'subscription_budgets'::regclass
  ) then
    alter table subscription_budgets
      add constraint subscription_budgets_yearly_limit_non_negative check (yearly_limit is null or yearly_limit >= 0);
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'subscription_budgets_warning_pct_range'
      and conrelid = 'subscription_budgets'::regclass
  ) then
    alter table subscription_budgets
      add constraint subscription_budgets_warning_pct_range check (warning_pct between 1 and 100);
  end if;
end $$;

create unique index if not exists idx_subscription_budgets_scope_name
  on subscription_budgets (scope_type, scope_id, base_currency, name);

create index if not exists idx_subscription_budgets_scope_enabled
  on subscription_budgets (scope_type, scope_id, enabled);

create table if not exists subscription_reminder_deliveries (
  delivery_id text primary key,
  subscription_id text not null references subscriptions(subscription_id) on delete cascade,
  vps_id text not null references vps_assets(vps_id) on delete cascade,
  renew_at date not null,
  offset_days integer not null,
  reminder_kind text not null,
  channel text not null,
  delivery_status text not null,
  summary text not null default '',
  notification_id text,
  sent_at timestamptz,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  constraint subscription_reminder_deliveries_offset_non_negative check (offset_days >= 0),
  constraint subscription_reminder_deliveries_kind_allowed check (reminder_kind in ('renewal', 'decision_attention')),
  constraint subscription_reminder_deliveries_channel_allowed check (channel in ('telegram', 'feishu', 'dispatch')),
  constraint subscription_reminder_deliveries_status_allowed check (delivery_status in ('sent', 'suppressed', 'failed'))
);

do $$
begin
  if not exists (
    select 1
    from pg_constraint
    where conname = 'subscription_reminder_deliveries_offset_non_negative'
      and conrelid = 'subscription_reminder_deliveries'::regclass
  ) then
    alter table subscription_reminder_deliveries
      add constraint subscription_reminder_deliveries_offset_non_negative check (offset_days >= 0);
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'subscription_reminder_deliveries_kind_allowed'
      and conrelid = 'subscription_reminder_deliveries'::regclass
  ) then
    alter table subscription_reminder_deliveries
      add constraint subscription_reminder_deliveries_kind_allowed check (reminder_kind in ('renewal', 'decision_attention'));
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'subscription_reminder_deliveries_channel_allowed'
      and conrelid = 'subscription_reminder_deliveries'::regclass
  ) then
    alter table subscription_reminder_deliveries
      add constraint subscription_reminder_deliveries_channel_allowed check (channel in ('telegram', 'feishu', 'dispatch'));
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'subscription_reminder_deliveries_status_allowed'
      and conrelid = 'subscription_reminder_deliveries'::regclass
  ) then
    alter table subscription_reminder_deliveries
      add constraint subscription_reminder_deliveries_status_allowed check (delivery_status in ('sent', 'suppressed', 'failed'));
  end if;
end $$;

drop index if exists idx_subscription_reminder_deliveries_dedupe;
create unique index idx_subscription_reminder_deliveries_dedupe
  on subscription_reminder_deliveries (subscription_id, renew_at, offset_days);

create index if not exists idx_subscription_reminder_deliveries_subscription
  on subscription_reminder_deliveries (subscription_id, created_at desc);

create index if not exists idx_subscription_reminder_deliveries_vps
  on subscription_reminder_deliveries (vps_id, created_at desc);
