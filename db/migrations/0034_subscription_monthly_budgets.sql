create table if not exists subscription_monthly_budgets (
  budget_month date primary key,
  base_currency text not null default 'CNY',
  monthly_limit numeric(12, 2) not null,
  warning_pct integer not null default 80,
  note text not null default '',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  constraint subscription_monthly_budgets_month_start check (date_trunc('month', budget_month::timestamp)::date = budget_month),
  constraint subscription_monthly_budgets_base_currency_code check (base_currency = upper(base_currency) and base_currency ~ '^[A-Z]{3}$'),
  constraint subscription_monthly_budgets_monthly_limit_non_negative check (monthly_limit >= 0),
  constraint subscription_monthly_budgets_warning_pct_range check (warning_pct between 1 and 100)
);

do $$
begin
  if not exists (
    select 1
    from pg_constraint
    where conname = 'subscription_monthly_budgets_month_start'
      and conrelid = 'subscription_monthly_budgets'::regclass
  ) then
    alter table subscription_monthly_budgets
      add constraint subscription_monthly_budgets_month_start check (date_trunc('month', budget_month::timestamp)::date = budget_month);
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'subscription_monthly_budgets_base_currency_code'
      and conrelid = 'subscription_monthly_budgets'::regclass
  ) then
    alter table subscription_monthly_budgets
      add constraint subscription_monthly_budgets_base_currency_code check (base_currency = upper(base_currency) and base_currency ~ '^[A-Z]{3}$');
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'subscription_monthly_budgets_monthly_limit_non_negative'
      and conrelid = 'subscription_monthly_budgets'::regclass
  ) then
    alter table subscription_monthly_budgets
      add constraint subscription_monthly_budgets_monthly_limit_non_negative check (monthly_limit >= 0);
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'subscription_monthly_budgets_warning_pct_range'
      and conrelid = 'subscription_monthly_budgets'::regclass
  ) then
    alter table subscription_monthly_budgets
      add constraint subscription_monthly_budgets_warning_pct_range check (warning_pct between 1 and 100);
  end if;
end $$;

create index if not exists idx_subscription_monthly_budgets_currency_month
  on subscription_monthly_budgets (base_currency, budget_month desc);
