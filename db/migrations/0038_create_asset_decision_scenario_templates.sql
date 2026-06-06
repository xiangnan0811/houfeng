create table if not exists asset_decision_scenario_templates (
  template_id text primary key,
  status text not null default 'active',
  scenario text not null default 'general',
  title text not null,
  goal text not null default '',
  note text not null default '',
  source_manual_group_id text not null default '',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  archived_at timestamptz null,
  constraint asset_decision_scenario_templates_status_allowed check (status in ('active', 'archived')),
  constraint asset_decision_scenario_templates_scenario_allowed check (
    scenario in ('general', 'primary_standby', 'budget_reduction', 'provider_review', 'region_review', 'migration_retirement', 'evidence_cleanup')
  ),
  constraint asset_decision_scenario_templates_title_not_blank check (length(btrim(title)) > 0)
);

create table if not exists asset_decision_scenario_template_members (
  member_id text primary key,
  template_id text not null references asset_decision_scenario_templates(template_id) on delete cascade,
  vps_id text not null default '',
  intended_role text not null default '',
  intended_action text not null default '',
  reason text not null default '',
  note text not null default '',
  sort_order integer not null default 0,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  constraint asset_decision_scenario_template_members_role_allowed check (
    intended_role = '' or intended_role in ('primary_candidate', 'standby_candidate', 'observe_candidate', 'retire_candidate', 'evidence_needed')
  ),
  constraint asset_decision_scenario_template_members_action_allowed check (
    intended_action = '' or intended_action in ('review', 'keep', 'observe', 'migrate', 'cancel', 'open_cancellation_workbench', 'complete_evidence')
  )
);

create index if not exists idx_asset_decision_scenario_templates_status_updated
  on asset_decision_scenario_templates(status, updated_at desc, template_id desc);

create index if not exists idx_asset_decision_scenario_templates_scenario_updated
  on asset_decision_scenario_templates(scenario, updated_at desc, template_id desc);

create index if not exists idx_asset_decision_scenario_template_members_template
  on asset_decision_scenario_template_members(template_id, sort_order asc, member_id asc);

create index if not exists idx_asset_decision_scenario_template_members_vps
  on asset_decision_scenario_template_members(vps_id, updated_at desc, template_id desc)
  where vps_id <> '';
