create table if not exists asset_decision_records (
  record_id text primary key,
  source_type text not null default 'auto_group',
  source_group_id text not null,
  source_group_type text not null,
  source_view text not null,
  scope_key text not null default '',
  scope_label text not null default '',
  renew_within_days integer not null default 30,
  title text not null,
  goal text not null default '',
  status text not null default 'draft',
  evidence_snapshot jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  decided_at timestamptz null,
  completed_at timestamptz null,
  constraint asset_decision_records_source_type_allowed check (source_type in ('auto_group')),
  constraint asset_decision_records_group_type_allowed check (
    source_group_type in ('renewal_attention', 'cancellation_attention', 'region_portfolio', 'provider_portfolio', 'cost_pressure', 'evidence_gap')
  ),
  constraint asset_decision_records_view_allowed check (
    source_view in ('needs_decision', 'renewal', 'region', 'provider', 'cost', 'evidence')
  ),
  constraint asset_decision_records_renew_window_allowed check (renew_within_days in (30, 60, 90)),
  constraint asset_decision_records_title_not_blank check (length(btrim(title)) > 0),
  constraint asset_decision_records_status_allowed check (status in ('draft', 'decided', 'in_progress', 'completed', 'abandoned'))
);

create table if not exists asset_decision_record_members (
  record_id text not null references asset_decision_records(record_id) on delete cascade,
  vps_id text not null references vps_assets(vps_id) on delete cascade,
  display_name text not null,
  suggested_role text not null,
  decided_role text not null,
  suggested_action text not null,
  decided_action text not null,
  reason text not null default '',
  evidence_snapshot jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  primary key (record_id, vps_id),
  constraint asset_decision_record_members_suggested_role_allowed check (
    suggested_role in ('primary_candidate', 'standby_candidate', 'observe_candidate', 'retire_candidate', 'evidence_needed')
  ),
  constraint asset_decision_record_members_decided_role_allowed check (
    decided_role in ('primary_candidate', 'standby_candidate', 'observe_candidate', 'retire_candidate', 'evidence_needed')
  ),
  constraint asset_decision_record_members_suggested_action_allowed check (
    suggested_action in ('review', 'keep', 'observe', 'migrate', 'cancel', 'open_cancellation_workbench', 'complete_evidence')
  ),
  constraint asset_decision_record_members_decided_action_allowed check (
    decided_action in ('review', 'keep', 'observe', 'migrate', 'cancel', 'open_cancellation_workbench', 'complete_evidence')
  ),
  constraint asset_decision_record_members_display_name_not_blank check (length(btrim(display_name)) > 0)
);

create index if not exists idx_asset_decision_records_status_updated
  on asset_decision_records(status, updated_at desc, record_id desc);

create index if not exists idx_asset_decision_records_source_group
  on asset_decision_records(source_group_id, updated_at desc, record_id desc);

create index if not exists idx_asset_decision_record_members_vps
  on asset_decision_record_members(vps_id, updated_at desc, record_id desc);

create or replace view asset_decision_records_with_counts as
select
  r.record_id,
  r.title,
  r.goal,
  r.status,
  r.source_type,
  r.source_group_id,
  r.source_group_type,
  r.source_view,
  r.scope_key,
  r.scope_label,
  r.renew_within_days,
  count(m.vps_id)::int as member_count,
  r.evidence_snapshot,
  r.created_at,
  r.updated_at,
  r.decided_at,
  r.completed_at
from asset_decision_records r
left join asset_decision_record_members m on m.record_id = r.record_id
group by r.record_id;
