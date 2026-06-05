alter table asset_decision_record_members
  add column if not exists followup_status text not null default 'todo',
  add column if not exists followup_note text not null default '',
  add column if not exists followup_updated_at timestamptz null;

do $$
begin
  if not exists (
    select 1
    from pg_constraint
    where conname = 'asset_decision_record_members_followup_status_allowed'
      and conrelid = 'asset_decision_record_members'::regclass
  ) then
    alter table asset_decision_record_members
      add constraint asset_decision_record_members_followup_status_allowed
      check (followup_status in ('todo', 'in_progress', 'blocked', 'done', 'skipped'));
  end if;
end $$;

create index if not exists idx_asset_decision_record_members_followup
  on asset_decision_record_members(record_id, followup_status, updated_at desc);

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
  (count(m.vps_id) filter (where m.followup_status = 'todo'))::int as followup_todo_count,
  (count(m.vps_id) filter (where m.followup_status = 'in_progress'))::int as followup_in_progress_count,
  (count(m.vps_id) filter (where m.followup_status = 'blocked'))::int as followup_blocked_count,
  (count(m.vps_id) filter (where m.followup_status = 'done'))::int as followup_done_count,
  (count(m.vps_id) filter (where m.followup_status = 'skipped'))::int as followup_skipped_count,
  r.evidence_snapshot,
  r.created_at,
  r.updated_at,
  r.decided_at,
  r.completed_at
from asset_decision_records r
left join asset_decision_record_members m on m.record_id = r.record_id
group by r.record_id;
