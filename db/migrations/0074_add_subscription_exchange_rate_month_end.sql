-- 补录历史月份的月末汇率。month_end_for 非空的行是按日期回补的“某月月末汇率”，只供成本趋势使用；
-- 它的 fetched_at 是回补时间，不能参与“最新汇率”或“某时刻已知汇率”的选择，读取方必须过滤
-- month_end_for is null。实时汇率与月末汇率各自唯一，互不覆盖。
alter table public.subscription_exchange_rates
  add column if not exists month_end_for date;

alter table public.subscription_exchange_rates
  drop constraint if exists subscription_exchange_rates_month_end_shape;

alter table public.subscription_exchange_rates
  add constraint subscription_exchange_rates_month_end_shape check (
    month_end_for is null
    or (
      month_end_for = (date_trunc('month', month_end_for) + interval '1 month - 1 day')::date
      and rate_date <= month_end_for
      and rate_date >= date_trunc('month', month_end_for)::date
    )
  );

drop index if exists public.idx_subscription_exchange_rates_lookup;

create unique index if not exists idx_subscription_exchange_rates_lookup
  on public.subscription_exchange_rates (provider, base_currency, quote_currency, rate_date)
  where month_end_for is null;

create unique index if not exists idx_subscription_exchange_rates_month_end
  on public.subscription_exchange_rates (provider, base_currency, quote_currency, month_end_for)
  where month_end_for is not null;
