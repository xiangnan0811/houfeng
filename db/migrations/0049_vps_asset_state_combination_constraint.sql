alter table vps_assets
  drop constraint if exists vps_assets_state_combination_valid;

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
