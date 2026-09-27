-- This contract intentionally supports fresh installations only. Never guess a
-- mapping for legacy business state and never delete existing business data.
do $$
begin
  lock table vps_assets, monitoring_instances, asset_services, asset_domains, targets, subscriptions in access exclusive mode;
  if not exists (select 1 from information_schema.columns where table_schema = current_schema() and table_name = 'vps_assets' and column_name = 'usage_tags') then
    if exists (select 1 from vps_assets) or exists (select 1 from monitoring_instances)
       or exists (select 1 from asset_services) or exists (select 1 from asset_domains)
       or exists (select 1 from targets) or exists (select 1 from subscriptions) then
      raise exception '0067 requires a fresh installation: existing business data needs an explicitly authorized rebuild; automatic conversion and deletion are unsupported' using errcode = '23514';
    end if;
  end if;
end $$;

alter table vps_assets drop constraint if exists vps_assets_state_combination_valid;
alter table vps_assets drop constraint if exists vps_assets_lifecycle_status_allowed;
alter table vps_assets add constraint vps_assets_lifecycle_status_allowed check (lifecycle_status in ('active', 'archived'));
alter table vps_assets drop constraint if exists vps_assets_renewal_decision_allowed;
alter table vps_assets add constraint vps_assets_renewal_decision_allowed check (renewal_decision in ('unreviewed', 'keep', 'cancel'));
-- Retained solely for internal SQL readers while callers move to usage_tags.
alter table vps_assets alter column usage_status set default 'unknown';
alter table vps_assets add column if not exists usage_tags text[] not null default '{}';
alter table vps_assets add column if not exists acquisition_source text not null default '';
alter table vps_assets add column if not exists validity_mode text not null default 'unknown';
alter table vps_assets add column if not exists expires_at date;
alter table vps_assets add column if not exists auto_renew_check text not null default 'unchecked';
alter table vps_assets add column if not exists auto_renew_checked_at timestamptz;
alter table vps_assets add column if not exists renewal_reason text not null default '';
alter table vps_assets add column if not exists renewal_review_at timestamptz;
alter table vps_assets drop constraint if exists vps_assets_validity_allowed;
alter table vps_assets add constraint vps_assets_validity_allowed check ((validity_mode = 'fixed' and expires_at is not null) or (validity_mode in ('unlimited', 'unknown') and expires_at is null));
alter table vps_assets drop constraint if exists vps_assets_auto_renew_check_allowed;
alter table vps_assets add constraint vps_assets_auto_renew_check_allowed check (auto_renew_check in ('unchecked', 'enabled', 'disabled', 'never_enabled', 'unsupported'));
alter table vps_assets drop constraint if exists vps_assets_auto_renew_check_time_valid;
alter table vps_assets add constraint vps_assets_auto_renew_check_time_valid check ((auto_renew_check = 'unchecked') = (auto_renew_checked_at is null));
create index if not exists idx_vps_assets_usage_tags_gin on vps_assets using gin (usage_tags);
alter table renewal_decisions drop constraint if exists renewal_decisions_from_allowed;
alter table renewal_decisions add constraint renewal_decisions_from_allowed check (from_decision is null or from_decision in ('unreviewed', 'keep', 'cancel'));
alter table renewal_decisions drop constraint if exists renewal_decisions_to_allowed;
alter table renewal_decisions add constraint renewal_decisions_to_allowed check (to_decision in ('unreviewed', 'keep', 'cancel'));
alter table subscriptions drop constraint if exists subscriptions_renewal_mode_allowed;
alter table subscriptions add constraint subscriptions_renewal_mode_allowed check (renewal_mode in ('auto', 'manual', 'auto_cancelled'));
alter table price_histories drop constraint if exists price_histories_renewal_mode_allowed;
alter table price_histories add constraint price_histories_renewal_mode_allowed check (from_renewal_mode in ('auto', 'manual', 'auto_cancelled') and to_renewal_mode in ('auto', 'manual', 'auto_cancelled'));
alter table asset_decision_record_members drop constraint if exists asset_decision_record_members_suggested_action_allowed;
alter table asset_decision_record_members add constraint asset_decision_record_members_suggested_action_allowed check (suggested_action in ('review', 'keep', 'observe', 'migrate', 'cancel', 'open_archive_preview', 'complete_evidence'));
alter table asset_decision_record_members drop constraint if exists asset_decision_record_members_decided_action_allowed;
alter table asset_decision_record_members add constraint asset_decision_record_members_decided_action_allowed check (decided_action in ('review', 'keep', 'observe', 'migrate', 'cancel', 'open_archive_preview', 'complete_evidence'));
alter table asset_decision_manual_group_members drop constraint if exists asset_decision_manual_group_members_action_allowed;
alter table asset_decision_manual_group_members add constraint asset_decision_manual_group_members_action_allowed check (intended_action in ('review', 'keep', 'observe', 'migrate', 'cancel', 'open_archive_preview', 'complete_evidence'));
alter table asset_decision_scenario_template_members drop constraint if exists asset_decision_scenario_template_members_action_allowed;
alter table asset_decision_scenario_template_members add constraint asset_decision_scenario_template_members_action_allowed check (intended_action = '' or intended_action in ('review', 'keep', 'observe', 'migrate', 'cancel', 'open_archive_preview', 'complete_evidence'));

alter table monitoring_instances add column if not exists vps_id text not null references vps_assets(vps_id);
alter table monitoring_instances add column if not exists ever_connected boolean not null default false;
alter table monitoring_instances add column if not exists last_trusted_online_at timestamptz;
alter table monitoring_instances add column if not exists control_revision bigint not null default 0;
alter table monitoring_instances drop constraint if exists monitoring_instances_lifecycle_status_allowed;
alter table monitoring_instances add constraint monitoring_instances_lifecycle_status_allowed check (lifecycle_status in ('待接入', '已接入', '已退役'));
create unique index if not exists idx_monitoring_instances_current_vps on monitoring_instances(vps_id) where lifecycle_status <> '已退役';
create unique index if not exists idx_monitoring_instances_owner_identity on monitoring_instances(vps_id,monitoring_instance_id);
alter table vps_monitoring_instance_links drop constraint if exists vps_monitoring_instance_links_owner_fk;
alter table vps_monitoring_instance_links add constraint vps_monitoring_instance_links_owner_fk foreign key (vps_id,monitoring_instance_id) references monitoring_instances(vps_id,monitoring_instance_id);

create or replace function public.enforce_monitoring_vps_ownership() returns trigger
language plpgsql set search_path = pg_catalog as $$
begin
  if new.vps_id is distinct from old.vps_id then
    raise exception 'monitoring instance VPS ownership is immutable' using errcode = '23514';
  end if;
  return new;
end $$;
drop trigger if exists monitoring_vps_ownership_immutable on monitoring_instances;
create trigger monitoring_vps_ownership_immutable before update of vps_id on monitoring_instances for each row execute function public.enforce_monitoring_vps_ownership();
revoke all on function public.enforce_monitoring_vps_ownership() from public;

create table if not exists monitoring_agent_sessions (
  session_id text primary key,
  monitoring_instance_id text not null references monitoring_instances(monitoring_instance_id),
  token_hash text not null unique,
  capability text not null default 'full' check (capability in ('full', 'evidence_only')),
  fingerprint_hash text not null default '',
  started_at timestamptz not null default clock_timestamp(),
  ended_at timestamptz,
  last_trusted_online_at timestamptz,
  ever_connected boolean not null default false,
  check (ended_at is null or ended_at >= started_at)
);
create index if not exists idx_monitoring_agent_sessions_instance on monitoring_agent_sessions(monitoring_instance_id, started_at);
create unique index if not exists idx_monitoring_agent_sessions_full on monitoring_agent_sessions(monitoring_instance_id) where capability = 'full';
create or replace function public.enforce_monitoring_session_identity() returns trigger
language plpgsql set search_path = pg_catalog as $$
begin
  if new.monitoring_instance_id is distinct from old.monitoring_instance_id
     or new.token_hash is distinct from old.token_hash
     or (old.capability = 'evidence_only' and new.capability <> 'evidence_only')
     or (old.ever_connected and not new.ever_connected) then
    raise exception 'monitoring session identity and retired capability are immutable' using errcode = '23514';
  end if;
  return new;
end $$;
drop trigger if exists monitoring_session_identity_immutable on monitoring_agent_sessions;
create trigger monitoring_session_identity_immutable before update on monitoring_agent_sessions for each row execute function public.enforce_monitoring_session_identity();
revoke all on function public.enforce_monitoring_session_identity() from public;
create table if not exists agent_live_signals (
  session_id text not null references monitoring_agent_sessions(session_id),
  signal_id text not null,
  received_at timestamptz not null default now(),
  primary key (session_id, signal_id)
);
create index if not exists idx_agent_live_signals_received on agent_live_signals(received_at);
create table if not exists receiver_health (
  id boolean primary key check (id),
  boot_id text not null,
  healthy_since timestamptz,
  checked_at timestamptz not null,
  healthy boolean not null,
  failure_reason text not null default ''
);
create table if not exists vps_archive_requests (
  vps_id text not null references vps_assets(vps_id),
  idempotency_key text not null,
  request_digest text not null,
  response jsonb not null,
  created_at timestamptz not null default now(),
  primary key (vps_id, idempotency_key)
);
create table if not exists monitoring_instance_lifecycle_receipts (
  idempotency_key text primary key,
  monitoring_instance_id text not null references monitoring_instances(monitoring_instance_id),
  request_digest text not null,
  response jsonb not null,
  created_at timestamptz not null default now()
);
create table if not exists vps_followups (
  followup_id text primary key,
  vps_id text not null references vps_assets(vps_id),
  kind text not null,
  dedupe_key text not null,
  status text not null default 'pending' check (status in ('pending', 'resolved', 'ignored')),
  summary text not null,
  details jsonb not null default '{}',
  resolution_reason text not null default '',
  resolved_by text not null default '',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now(),
  resolved_at timestamptz,
  check (status = 'pending' or (resolved_at is not null and length(btrim(resolution_reason)) > 0 and length(btrim(resolved_by)) > 0))
);
create unique index if not exists idx_vps_followups_pending_dedupe on vps_followups(vps_id, kind, dedupe_key) where status = 'pending';
create index if not exists idx_vps_followups_vps_status on vps_followups(vps_id, status, created_at);

alter table targets add column if not exists lifecycle_status text not null default 'active';
alter table targets add column if not exists control_revision bigint not null default 0;
alter table targets drop constraint if exists targets_lifecycle_status_allowed;
alter table targets add constraint targets_lifecycle_status_allowed check (lifecycle_status in ('active', 'retired'));
alter table targets drop constraint if exists targets_run_status_allowed;
alter table targets add constraint targets_run_status_allowed check (run_status in ('启用', '维护中', '暂停'));

alter table monitoring_instance_command_action_audit drop constraint if exists command_action_audit_event_type_allowed;
alter table monitoring_instance_command_action_audit add constraint command_action_audit_event_type_allowed check (event_type in ('queued', 'dispatched', 'completed', 'rejected', 'cancelled'));
alter table asset_lifecycle_actions drop constraint if exists asset_lifecycle_actions_type_allowed;
alter table asset_lifecycle_actions add constraint asset_lifecycle_actions_type_allowed check (action_type in ('extend_validity', 'archive_vps', 'restore_vps', 'start_migration', 'correct_dependency_status'));
alter table asset_lifecycle_action_steps drop constraint if exists asset_lifecycle_action_steps_step_type_allowed;
alter table asset_lifecycle_action_steps add constraint asset_lifecycle_action_steps_step_type_allowed check (step_type in ('vps_lifecycle', 'vps_validity', 'subscription_status', 'subscription_renew_at', 'monitoring_instance_lifecycle', 'monitoring_instance_monitoring', 'target_run_status', 'dependency_status'));

alter table asset_services alter column vps_id drop not null;
alter table asset_domains alter column vps_id drop not null;
create table if not exists asset_service_associations (
  id text primary key,
  service_id text not null references asset_services(service_id),
  vps_id text not null references vps_assets(vps_id),
  target_id text references targets(target_id),
  address text not null default '',
  port integer check (port is null or port between 1 and 65535),
  started_at timestamptz not null default now(),
  ended_at timestamptz,
  end_reason text not null default '',
  ended_by text not null default '',
  snapshot jsonb not null default '{}',
  check (ended_at is null or ended_at >= started_at)
);
create unique index if not exists idx_asset_service_associations_current on asset_service_associations(service_id,vps_id) where ended_at is null;
create index if not exists idx_asset_service_associations_vps on asset_service_associations(vps_id,started_at);
create table if not exists asset_domain_associations (
  id text primary key,
  domain_id text not null references asset_domains(domain_id),
  vps_id text not null references vps_assets(vps_id),
  service_id text references asset_services(service_id),
  target_id text references targets(target_id),
  address text not null default '',
  started_at timestamptz not null default now(),
  ended_at timestamptz,
  end_reason text not null default '',
  ended_by text not null default '',
  snapshot jsonb not null default '{}',
  check (ended_at is null or ended_at >= started_at)
);
create unique index if not exists idx_asset_domain_associations_current on asset_domain_associations(domain_id,vps_id) where ended_at is null;
create index if not exists idx_asset_domain_associations_vps on asset_domain_associations(vps_id,started_at);

create table if not exists vps_maintenance_actions (
  action_id text primary key,
  vps_id text not null references vps_assets(vps_id),
  reason text not null,
  actor text not null,
  started_at timestamptz not null default now(),
  ended_at timestamptz,
  ended_reason text not null default '',
  ended_by text not null default '',
  check (ended_at is null or ended_at >= started_at)
);
create unique index if not exists idx_vps_maintenance_actions_active on vps_maintenance_actions(vps_id) where ended_at is null;
create table if not exists vps_maintenance_effects (
  action_id text not null references vps_maintenance_actions(action_id),
  resource_kind text not null check (resource_kind in ('monitoring_instance', 'target')),
  resource_id text not null,
  previous_control text not null,
  applied_revision bigint not null,
  primary key (action_id,resource_kind,resource_id)
);

alter table monitoring_instance_host_sample_daily_aggregates add column if not exists finalized boolean not null default false;
alter table target_probe_daily_aggregates add column if not exists finalized boolean not null default false;
alter table center_settings alter column retention_policy set default '{"raw_layer_days":30,"aggregate_layer_days":365}'::jsonb;
update center_settings set retention_policy = (retention_policy - 'event_layer_days' - 'notification_layer_days') || '{"raw_layer_days":30,"aggregate_layer_days":365}'::jsonb,
  ip_quality_settings = ip_quality_settings - 'raw_retention_days' - 'history_retention_days';
