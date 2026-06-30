alter table subscriptions
  drop constraint if exists subscriptions_renewal_mode_allowed;

alter table subscriptions
  add constraint subscriptions_renewal_mode_allowed check (
    renewal_mode in ('auto', 'manual', 'auto_cancelled', 'lottery', 'gift', 'bonus', 'other')
  );

alter table price_histories
  drop constraint if exists price_histories_renewal_mode_allowed;

alter table price_histories
  add constraint price_histories_renewal_mode_allowed check (
    from_renewal_mode in ('auto', 'manual', 'auto_cancelled', 'lottery', 'gift', 'bonus', 'other') and
    to_renewal_mode in ('auto', 'manual', 'auto_cancelled', 'lottery', 'gift', 'bonus', 'other')
  );
