create table if not exists asset_decision_manual_groups (
  manual_group_id text primary key,
  status text not null default 'active',
  scenario text not null default 'general',
  title text not null,
  goal text not null default '',
  note text not null default '',
  source_type text not null default 'manual',
  source_group_id text not null default '',
  source_group_type text not null default '',
  source_view text not null default '',
  scope_key text not null default '',
  scope_label text not null default '',
  renew_within_days integer not null default 30,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  archived_at timestamptz null,
  constraint asset_decision_manual_groups_status_allowed check (status in ('active', 'archived')),
  constraint asset_decision_manual_groups_scenario_allowed check (
    scenario in ('general', 'primary_standby', 'budget_reduction', 'provider_review', 'region_review', 'migration_retirement', 'evidence_cleanup')
  ),
  constraint asset_decision_manual_groups_source_type_allowed check (source_type in ('manual', 'auto_group')),
  constraint asset_decision_manual_groups_group_type_allowed check (
    source_group_type = '' or source_group_type in ('renewal_attention', 'cancellation_attention', 'region_portfolio', 'provider_portfolio', 'cost_pressure', 'evidence_gap')
  ),
  constraint asset_decision_manual_groups_view_allowed check (
    source_view = '' or source_view in ('needs_decision', 'renewal', 'region', 'provider', 'cost', 'evidence')
  ),
  constraint asset_decision_manual_groups_renew_window_allowed check (renew_within_days in (30, 60, 90)),
  constraint asset_decision_manual_groups_title_not_blank check (length(btrim(title)) > 0)
);

create table if not exists asset_decision_manual_group_members (
  manual_group_id text not null references asset_decision_manual_groups(manual_group_id) on delete cascade,
  vps_id text not null references vps_assets(vps_id) on delete cascade,
  intended_role text not null,
  intended_action text not null,
  reason text not null default '',
  note text not null default '',
  sort_order integer not null default 0,
  evidence_snapshot jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  primary key (manual_group_id, vps_id),
  constraint asset_decision_manual_group_members_role_allowed check (
    intended_role in ('primary_candidate', 'standby_candidate', 'observe_candidate', 'retire_candidate', 'evidence_needed')
  ),
  constraint asset_decision_manual_group_members_action_allowed check (
    intended_action in ('review', 'keep', 'observe', 'migrate', 'cancel', 'open_cancellation_workbench', 'complete_evidence')
  )
);

create index if not exists idx_asset_decision_manual_groups_status_updated
  on asset_decision_manual_groups(status, updated_at desc, manual_group_id desc);

create index if not exists idx_asset_decision_manual_groups_source
  on asset_decision_manual_groups(source_group_id, updated_at desc, manual_group_id desc);

create index if not exists idx_asset_decision_manual_group_members_vps
  on asset_decision_manual_group_members(vps_id, updated_at desc, manual_group_id desc);

alter table asset_decision_records
  drop constraint if exists asset_decision_records_source_type_allowed;

alter table asset_decision_records
  add constraint asset_decision_records_source_type_allowed
  check (source_type in ('auto_group', 'manual_group'));
