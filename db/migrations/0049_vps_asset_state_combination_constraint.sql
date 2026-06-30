alter table vps_assets
  drop constraint if exists vps_assets_state_combination_valid;

update vps_assets
set renewal_decision = case
      when renewal_decision not in ('cancel', 'auto_renew_cancelled') then 'cancel'
      else renewal_decision
    end,
    usage_status = case
      when usage_status = 'in_use' then 'idle'
      else usage_status
    end,
    updated_at = now()
where lifecycle_status = 'cancelled'
  and (
    renewal_decision not in ('cancel', 'auto_renew_cancelled')
    or usage_status = 'in_use'
  );

update vps_assets
set renewal_decision = 'cancel',
    updated_at = now()
where lifecycle_status = 'to_cancel'
  and renewal_decision not in ('cancel', 'auto_renew_cancelled');

update vps_assets
set renewal_decision = 'migrate',
    updated_at = now()
where lifecycle_status = 'to_migrate'
  and renewal_decision <> 'migrate';

update vps_assets
set lifecycle_status = case
      when lifecycle_status = 'active' then 'idle'
      else lifecycle_status
    end,
    usage_status = case
      when usage_status = 'in_use' then 'idle'
      else usage_status
    end,
    updated_at = now()
where renewal_decision = 'replaced'
  and (
    lifecycle_status = 'active'
    or usage_status = 'in_use'
  );

alter table vps_assets
  add constraint vps_assets_state_combination_valid check (
    (
      lifecycle_status <> 'cancelled' or
      (
        renewal_decision in ('cancel', 'auto_renew_cancelled') and
        usage_status <> 'in_use'
      )
    ) and
    (
      lifecycle_status <> 'to_cancel' or
      renewal_decision in ('cancel', 'auto_renew_cancelled')
    ) and
    (
      lifecycle_status <> 'to_migrate' or
      renewal_decision = 'migrate'
    ) and
    (
      renewal_decision <> 'replaced' or
      (
        lifecycle_status <> 'active' and
        usage_status <> 'in_use'
      )
    )
  );
