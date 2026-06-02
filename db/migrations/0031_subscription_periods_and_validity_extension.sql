alter table if exists subscriptions
  add column if not exists billing_period_unit text not null default 'month';

alter table if exists subscriptions
  add column if not exists billing_period_length integer not null default 1;

alter table if exists subscriptions
  add column if not exists renewal_mode text not null default 'manual';

update subscriptions
set billing_period_unit = 'month'
where billing_period_unit is null or billing_period_unit = '';

update subscriptions
set billing_period_length = greatest(coalesce(billing_months, 1), 1)
where billing_period_length is null or billing_period_length <= 0;

update subscriptions
set renewal_mode = case
  when auto_renew_cancelled then 'auto_cancelled'
  when auto_renew then 'auto'
  else 'manual'
end
where renewal_mode is null or renewal_mode = '';

do $$
begin
  if not exists (
    select 1
    from pg_constraint
    where conname = 'subscriptions_billing_period_unit_allowed'
      and conrelid = 'subscriptions'::regclass
  ) then
    alter table subscriptions
      add constraint subscriptions_billing_period_unit_allowed check (
        billing_period_unit in ('day', 'week', 'month', 'year')
      );
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'subscriptions_billing_period_length_positive'
      and conrelid = 'subscriptions'::regclass
  ) then
    alter table subscriptions
      add constraint subscriptions_billing_period_length_positive check (billing_period_length > 0);
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'subscriptions_renewal_mode_allowed'
      and conrelid = 'subscriptions'::regclass
  ) then
    alter table subscriptions
      add constraint subscriptions_renewal_mode_allowed check (
        renewal_mode in ('auto', 'manual', 'auto_cancelled', 'lottery', 'bonus', 'other')
      );
  end if;
end $$;

alter table if exists price_histories
  add column if not exists from_billing_period_unit text not null default 'month';

alter table if exists price_histories
  add column if not exists to_billing_period_unit text not null default 'month';

alter table if exists price_histories
  add column if not exists from_billing_period_length integer not null default 1;

alter table if exists price_histories
  add column if not exists to_billing_period_length integer not null default 1;

alter table if exists price_histories
  add column if not exists from_renewal_mode text not null default 'manual';

alter table if exists price_histories
  add column if not exists to_renewal_mode text not null default 'manual';

update price_histories
set from_billing_period_unit = 'month'
where from_billing_period_unit is null or from_billing_period_unit = '';

update price_histories
set to_billing_period_unit = 'month'
where to_billing_period_unit is null or to_billing_period_unit = '';

update price_histories
set from_billing_period_length = greatest(coalesce(from_billing_months, 1), 1)
where from_billing_period_length is null or from_billing_period_length <= 0;

update price_histories
set to_billing_period_length = greatest(coalesce(to_billing_months, 1), 1)
where to_billing_period_length is null or to_billing_period_length <= 0;

update price_histories
set from_renewal_mode = case
  when from_auto_renew_cancelled then 'auto_cancelled'
  when from_auto_renew then 'auto'
  else 'manual'
end
where from_renewal_mode is null or from_renewal_mode = '';

update price_histories
set to_renewal_mode = case
  when to_auto_renew_cancelled then 'auto_cancelled'
  when to_auto_renew then 'auto'
  else 'manual'
end
where to_renewal_mode is null or to_renewal_mode = '';

do $$
begin
  if not exists (
    select 1
    from pg_constraint
    where conname = 'price_histories_billing_period_unit_allowed'
      and conrelid = 'price_histories'::regclass
  ) then
    alter table price_histories
      add constraint price_histories_billing_period_unit_allowed check (
        from_billing_period_unit in ('day', 'week', 'month', 'year') and
        to_billing_period_unit in ('day', 'week', 'month', 'year')
      );
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'price_histories_billing_period_length_positive'
      and conrelid = 'price_histories'::regclass
  ) then
    alter table price_histories
      add constraint price_histories_billing_period_length_positive check (
        from_billing_period_length > 0 and to_billing_period_length > 0
      );
  end if;

  if not exists (
    select 1
    from pg_constraint
    where conname = 'price_histories_renewal_mode_allowed'
      and conrelid = 'price_histories'::regclass
  ) then
    alter table price_histories
      add constraint price_histories_renewal_mode_allowed check (
        from_renewal_mode in ('auto', 'manual', 'auto_cancelled', 'lottery', 'bonus', 'other') and
        to_renewal_mode in ('auto', 'manual', 'auto_cancelled', 'lottery', 'bonus', 'other')
      );
  end if;
end $$;

alter table if exists asset_lifecycle_actions
  drop constraint if exists asset_lifecycle_actions_type_allowed;

alter table if exists asset_lifecycle_actions
  add constraint asset_lifecycle_actions_type_allowed check (
    action_type in ('cancel_vps', 'extend_validity')
  );

alter table if exists asset_lifecycle_action_steps
  drop constraint if exists asset_lifecycle_action_steps_step_type_allowed;

alter table if exists asset_lifecycle_action_steps
  add constraint asset_lifecycle_action_steps_step_type_allowed check (
    step_type in (
      'vps_lifecycle',
      'subscription_status',
      'subscription_renew_at',
      'monitoring_instance_lifecycle',
      'monitoring_instance_monitoring',
      'target_run_status'
    )
  );
