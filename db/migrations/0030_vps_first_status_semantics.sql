-- VPS-first control-plane semantics.
--
-- Business lifecycle, usage, renewal, migration, and cancellation decisions
-- belong to vps_assets. Subscription and MonitoringInstance rows keep legacy
-- columns only where the current schema still needs them for derived facts and
-- runtime state.

update subscriptions
set status = 'unknown',
    updated_at = now()
where status is null
   or status = ''
   or status not in ('active', 'paused', 'cancelled', 'expired', 'unknown');

update monitoring_instances
set lifecycle_status = '待接入',
    updated_at = now()
where lifecycle_status is null
   or lifecycle_status = ''
   or lifecycle_status not in ('待接入', '在用', '观察中', '不续费', '已退役');

with subscription_evidence as (
  select
    s.vps_id,
    count(*) filter (where s.status = 'active') as active_count,
    count(*) filter (where s.auto_renew_cancelled) as cancelled_auto_renew_count,
    count(*) filter (where s.status in ('cancelled', 'expired')) as final_inactive_count,
    count(*) filter (where s.status in ('paused', 'unknown')) as review_inactive_count,
    jsonb_agg(
      jsonb_build_object(
        'subscription_id', s.subscription_id,
        'status', s.status,
        'renew_at', s.renew_at,
        'auto_renew', s.auto_renew,
        'auto_renew_cancelled', s.auto_renew_cancelled
      )
      order by s.renew_at nulls last, s.subscription_id
    ) as evidence
  from subscriptions s
  group by s.vps_id
),
monitoring_evidence as (
  select
    l.vps_id,
    count(*) filter (where mi.lifecycle_status in ('在用', '观察中')) as live_count,
    count(*) filter (where mi.lifecycle_status in ('不续费', '已退役')) as retiring_count,
    jsonb_agg(
      jsonb_build_object(
        'monitoring_instance_id', mi.monitoring_instance_id,
        'lifecycle_status', mi.lifecycle_status,
        'monitoring_status', mi.monitoring_status,
        'binding_status', mi.binding_status
      )
      order by mi.monitoring_instance_id
    ) as evidence
  from vps_monitoring_instance_links l
  join monitoring_instances mi
    on mi.monitoring_instance_id = l.monitoring_instance_id
  where l.unlinked_at is null
  group by l.vps_id
),
classified as (
  select
    v.vps_id,
    v.lifecycle_status as old_lifecycle_status,
    v.usage_status as old_usage_status,
    v.renewal_decision as old_renewal_decision,
    case
      when v.lifecycle_status = 'archived' then v.lifecycle_status
      when coalesce(se.cancelled_auto_renew_count, 0) > 0 then 'to_cancel'
      when coalesce(se.active_count, 0) = 0
        and coalesce(se.final_inactive_count, 0) > 0
        and coalesce(me.live_count, 0) = 0 then 'cancelled'
      when coalesce(se.active_count, 0) = 0
        and coalesce(se.final_inactive_count, 0) > 0 then 'to_cancel'
      when coalesce(me.retiring_count, 0) > 0
        and coalesce(me.live_count, 0) = 0 then 'to_cancel'
      else v.lifecycle_status
    end as new_lifecycle_status,
    case
      when v.usage_status <> '' then v.usage_status
      else 'unknown'
    end as new_usage_status,
    case
      when v.renewal_decision in ('cancel', 'auto_renew_cancelled', 'migrate', 'keep', 'observe', 'replaced') then v.renewal_decision
      when coalesce(se.cancelled_auto_renew_count, 0) > 0 then 'auto_renew_cancelled'
      when coalesce(se.active_count, 0) = 0
        and coalesce(se.final_inactive_count, 0) > 0 then 'cancel'
      when coalesce(me.retiring_count, 0) > 0
        and coalesce(me.live_count, 0) = 0 then 'cancel'
      when coalesce(se.review_inactive_count, 0) > 0 then 'observe'
      else v.renewal_decision
    end as new_renewal_decision,
    jsonb_build_object(
      'source', '0030_vps_first_status_semantics',
      'subscription_evidence', coalesce(se.evidence, '[]'::jsonb),
      'monitoring_evidence', coalesce(me.evidence, '[]'::jsonb)
    ) as evidence
  from vps_assets v
  left join subscription_evidence se on se.vps_id = v.vps_id
  left join monitoring_evidence me on me.vps_id = v.vps_id
),
changed as (
  select *
  from classified
  where old_lifecycle_status is distinct from new_lifecycle_status
     or old_usage_status is distinct from new_usage_status
     or old_renewal_decision is distinct from new_renewal_decision
),
updated_vps as (
  update vps_assets v
  set lifecycle_status = c.new_lifecycle_status,
      usage_status = c.new_usage_status,
      renewal_decision = c.new_renewal_decision,
      updated_at = now(),
      archived_at = case
        when c.new_lifecycle_status = 'archived' then coalesce(v.archived_at, now())
        else null
      end
  from changed c
  where v.vps_id = c.vps_id
  returning
    v.vps_id,
    c.old_lifecycle_status,
    c.new_lifecycle_status,
    c.old_usage_status,
    c.new_usage_status,
    c.old_renewal_decision,
    c.new_renewal_decision,
    c.evidence
),
inserted_actions as (
  insert into asset_lifecycle_actions (
    action_id,
    vps_id,
    action_type,
    status,
    reason,
    summary,
    created_at,
    confirmed_at,
    completed_at
  )
  select
    'ala_mig0030_' || substr(md5(vps_id), 1, 20),
    vps_id,
    'cancel_vps',
    'completed',
    'VPS-first upgrade normalized legacy subscription and monitoring state into the VPS aggregate.',
    jsonb_build_object(
      'source', '0030_vps_first_status_semantics',
      'old_lifecycle_status', old_lifecycle_status,
      'new_lifecycle_status', new_lifecycle_status,
      'old_usage_status', old_usage_status,
      'new_usage_status', new_usage_status,
      'old_renewal_decision', old_renewal_decision,
      'new_renewal_decision', new_renewal_decision,
      'legacy_evidence', evidence
    ),
    now(),
    now(),
    now()
  from updated_vps
  where not exists (
    select 1
    from asset_lifecycle_actions existing
    where existing.action_id = 'ala_mig0030_' || substr(md5(updated_vps.vps_id), 1, 20)
  )
  returning action_id, vps_id
)
insert into asset_lifecycle_action_steps (
  step_id,
  action_id,
  object_type,
  object_id,
  step_type,
  status,
  before_state,
  after_state,
  message,
  executed_at,
  created_at
)
select
  'als_mig0030_' || substr(md5(u.vps_id || ':vps'), 1, 20),
  a.action_id,
  'vps',
  u.vps_id,
  'vps_lifecycle',
  'completed',
  jsonb_build_object(
    'lifecycle_status', u.old_lifecycle_status,
    'usage_status', u.old_usage_status,
    'renewal_decision', u.old_renewal_decision
  ),
  jsonb_build_object(
    'lifecycle_status', u.new_lifecycle_status,
    'usage_status', u.new_usage_status,
    'renewal_decision', u.new_renewal_decision
  ),
  '升级到 VPS-first control plane：旧订阅和监控实例状态已对齐到 VPS 主体状态。',
  now(),
  now()
from updated_vps u
join inserted_actions a on a.vps_id = u.vps_id
where not exists (
  select 1
  from asset_lifecycle_action_steps existing
  where existing.step_id = 'als_mig0030_' || substr(md5(u.vps_id || ':vps'), 1, 20)
);

insert into renewal_decisions (
  decision_id,
  vps_id,
  from_decision,
  to_decision,
  reason,
  decided_at,
  created_at
)
select
  'rdec_mig0030_' || substr(md5(v.vps_id), 1, 19),
  v.vps_id,
  nullif(action.summary->>'old_renewal_decision', ''),
  action.summary->>'new_renewal_decision',
  'VPS-first upgrade recorded the VPS-owned renewal decision after legacy status normalization.',
  now(),
  now()
from vps_assets v
join asset_lifecycle_actions action
  on action.action_id = 'ala_mig0030_' || substr(md5(v.vps_id), 1, 20)
where action.summary->>'old_renewal_decision' is distinct from action.summary->>'new_renewal_decision'
  and action.summary->>'new_renewal_decision' in ('cancel', 'auto_renew_cancelled', 'migrate', 'observe', 'keep', 'replaced')
  and not exists (
    select 1
    from renewal_decisions existing
    where existing.decision_id = 'rdec_mig0030_' || substr(md5(v.vps_id), 1, 19)
  );

comment on column vps_assets.lifecycle_status is 'VPS-owned business lifecycle. User-facing cancellation, migration, archive, and retirement decisions start from the VPS aggregate.';
comment on column vps_assets.usage_status is 'VPS-owned business usage status. Subscription and MonitoringInstance rows must not ask users to duplicate this state.';
comment on column vps_assets.renewal_decision is 'VPS-owned business renewal decision. Billing facts explain evidence; they do not own the decision state.';
comment on column subscriptions.status is 'Legacy derived/internal subscription state retained for schema compatibility. New VPS-first UI must not ask users to enter this as an independent business status.';
comment on column monitoring_instances.lifecycle_status is 'Monitoring onboarding/runtime lifecycle retained for observability compatibility. VPS owns the business lifecycle and retirement/cancellation workflow.';
