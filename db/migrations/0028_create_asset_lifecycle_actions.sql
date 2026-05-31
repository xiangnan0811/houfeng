create table if not exists asset_lifecycle_actions (
  action_id text primary key,
  vps_id text not null references vps_assets(vps_id) on delete cascade,
  action_type text not null,
  status text not null default 'completed',
  reason text not null default '',
  effective_date date null,
  summary jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now(),
  confirmed_at timestamptz null,
  completed_at timestamptz null,
  constraint asset_lifecycle_actions_type_allowed check (
    action_type in ('cancel_vps')
  ),
  constraint asset_lifecycle_actions_status_allowed check (
    status in ('pending', 'completed', 'failed')
  ),
  constraint asset_lifecycle_actions_reason_not_null check (reason is not null)
);

create table if not exists asset_lifecycle_action_steps (
  step_id text primary key,
  action_id text not null references asset_lifecycle_actions(action_id) on delete cascade,
  object_type text not null,
  object_id text not null,
  step_type text not null,
  status text not null,
  before_state jsonb not null default '{}'::jsonb,
  after_state jsonb not null default '{}'::jsonb,
  message text not null default '',
  executed_at timestamptz null,
  created_at timestamptz not null default now(),
  constraint asset_lifecycle_action_steps_object_type_allowed check (
    object_type in ('vps', 'subscription', 'node', 'target')
  ),
  constraint asset_lifecycle_action_steps_step_type_allowed check (
    step_type in ('vps_lifecycle', 'subscription_status', 'node_lifecycle', 'node_monitoring', 'target_run_status')
  ),
  constraint asset_lifecycle_action_steps_status_allowed check (
    status in ('completed', 'skipped', 'failed')
  ),
  constraint asset_lifecycle_action_steps_message_not_null check (message is not null)
);

create index if not exists idx_asset_lifecycle_actions_vps_time
  on asset_lifecycle_actions(vps_id, created_at desc, action_id desc);

create index if not exists idx_asset_lifecycle_actions_status
  on asset_lifecycle_actions(status);

create index if not exists idx_asset_lifecycle_action_steps_action
  on asset_lifecycle_action_steps(action_id, created_at asc, step_id asc);

create index if not exists idx_asset_lifecycle_action_steps_object
  on asset_lifecycle_action_steps(object_type, object_id, created_at desc);
