alter table asset_lifecycle_actions
  drop constraint if exists asset_lifecycle_actions_type_allowed;

alter table asset_lifecycle_actions
  add constraint asset_lifecycle_actions_type_allowed check (
    action_type in ('cancel_vps', 'extend_validity', 'archive_vps', 'restore_vps', 'start_migration', 'correct_dependency_status')
  );

alter table asset_lifecycle_action_steps
  drop constraint if exists asset_lifecycle_action_steps_object_type_allowed;

alter table asset_lifecycle_action_steps
  add constraint asset_lifecycle_action_steps_object_type_allowed check (
    object_type in ('vps', 'subscription', 'monitoring_instance', 'target', 'service', 'domain')
  );

alter table asset_lifecycle_action_steps
  drop constraint if exists asset_lifecycle_action_steps_step_type_allowed;

alter table asset_lifecycle_action_steps
  add constraint asset_lifecycle_action_steps_step_type_allowed check (
    step_type in ('vps_lifecycle', 'subscription_status', 'subscription_renew_at', 'monitoring_instance_lifecycle', 'monitoring_instance_monitoring', 'target_run_status', 'dependency_status')
  );

alter table vps_assets
  add column archived_state_snapshot jsonb;

update vps_assets
set archived_state_snapshot = jsonb_build_object(
      'lifecycle_status', lifecycle_status,
      'usage_status', usage_status,
      'renewal_decision', renewal_decision,
      'captured_at', now(),
      'source', 'migration_observation'
    ),
    usage_status = 'unknown'
where lifecycle_status = 'archived';

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
    ) and
    (
      lifecycle_status <> 'archived' or
      usage_status <> 'in_use'
    )
  );
