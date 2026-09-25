-- Read-only diagnostic for the VPS state migrations 0065/0066.
-- Run before applying the migrations. This script reports facts only; it never
-- guesses or changes legacy values.
begin transaction read only;

with invalid_state_values(table_name, row_id, field_name, value) as (
  select 'vps_assets', vps_id, 'lifecycle_status', lifecycle_status::text
  from vps_assets
  where lifecycle_status not in ('active', 'idle', 'testing', 'to_migrate', 'to_cancel', 'cancelled', 'archived')
  union all
  select 'vps_assets', vps_id, 'usage_status', usage_status::text
  from vps_assets
  where usage_status not in ('in_use', 'idle', 'standby', 'testing', 'unknown')
  union all
  select 'vps_assets', vps_id, 'renewal_decision', renewal_decision::text
  from vps_assets
  where renewal_decision not in ('unreviewed', 'keep', 'observe', 'migrate', 'cancel', 'auto_renew_cancelled', 'replaced')
  union all
  select 'monitoring_instances', monitoring_instance_id, 'lifecycle_status', lifecycle_status::text
  from monitoring_instances
  where lifecycle_status not in ('待接入', '在用', '观察中', '不续费', '已退役')
  union all
  select 'monitoring_instances', monitoring_instance_id, 'monitoring_status', monitoring_status::text
  from monitoring_instances
  where monitoring_status not in ('启用', '维护中', '暂停')
  union all
  select 'monitoring_instances', monitoring_instance_id, 'binding_status', binding_status::text
  from monitoring_instances
  where binding_status not in ('未绑定', '已绑定', '指纹变更待确认')
  union all
  select 'targets', target_id, 'run_status', run_status::text
  from targets
  where run_status not in ('启用', '维护中', '暂停', '已归档')
  union all
  select 'subscriptions', subscription_id, 'status', status::text
  from subscriptions
  where status not in ('active', 'paused', 'cancelled', 'expired', 'unknown')
  union all
  select 'subscriptions', subscription_id, 'renewal_mode', renewal_mode::text
  from subscriptions
  where renewal_mode not in ('auto', 'manual', 'auto_cancelled', 'lottery', 'gift', 'bonus', 'other')
  union all
  select 'asset_services', service_id, 'status', status::text
  from asset_services
  where status not in ('active', 'paused', 'retired', 'unknown')
  union all
  select 'asset_domains', domain_id, 'status', status::text
  from asset_domains
  where status not in ('active', 'paused', 'retired', 'unknown')
)
select 'invalid_state_value' as report,
       table_name,
       row_id,
       field_name,
       value
from invalid_state_values
order by table_name, row_id, field_name;

-- Migration 0065 preserves these original axes in a migration_observation
-- snapshot, then changes only current usage_status to unknown.
select 'archived_snapshot_backfill' as report,
       vps_id,
       lifecycle_status,
       usage_status as observed_usage_status,
       renewal_decision,
       'migration_observation' as snapshot_source,
       'unknown' as post_migration_usage_status
from vps_assets
where lifecycle_status = 'archived'
order by vps_id;

with retired_instances as (
  select monitoring_instance_id,
         monitoring_status,
         binding_status,
         array_remove(array[
           case when monitoring_status <> '暂停' then 'monitoring_not_paused' end,
           case when enrollment_token_hash is not null then 'enrollment_token_hash' end,
           case when enrollment_token_issued_at is not null then 'enrollment_token_issued_at' end,
           case when enrollment_token_consumed_at is not null then 'enrollment_token_consumed_at' end,
           case when coalesce(sync_token_hash, '') <> '' then 'sync_token_hash' end,
           case when pending_binding_fingerprint is not null then 'pending_binding_fingerprint' end,
           case when pending_binding_first_seen_at is not null then 'pending_binding_first_seen_at' end,
           case when pending_binding_last_seen_at is not null then 'pending_binding_last_seen_at' end,
           case when pending_binding_attempt_count <> 0 then 'pending_binding_attempt_count' end,
           case when pending_action_id is not null then 'pending_action_id' end,
           case when pending_action_command_id is not null then 'pending_action_command_id' end,
           case when last_action->>'status' = 'pending' then 'last_action_pending' end,
           case when coalesce(binding_fingerprint, '') = '' and binding_status <> '未绑定' then 'binding_status_without_fingerprint' end,
           case when coalesce(binding_fingerprint, '') <> '' and binding_status <> '已绑定' then 'binding_status_with_fingerprint' end
         ], null::text) as residue_reasons
  from monitoring_instances
  where lifecycle_status = '已退役'
)
select 'retired_instance_residue' as report,
       monitoring_instance_id,
       monitoring_status,
       binding_status,
       residue_reasons
from retired_instances
where cardinality(residue_reasons) > 0
order by monitoring_instance_id;

-- 0061 stores only a one-way request digest and the result ID, not the original
-- requested VPS owner. This result reports current owners for operator evidence
-- comparison; the original owner cannot be inferred from the digest alone.
select 'subscription_receipt_current_owner' as report,
       receipts.subscription_id,
       subscriptions.vps_id as current_vps_id,
       'original VPS owner is not persisted in the receipt' as owner_evidence
from subscription_create_idempotency receipts
join subscriptions using (subscription_id)
order by receipts.subscription_id;

-- A linked-create receipt contains both result IDs, so a mismatch between the
-- receipt MI and the MI currently referenced by its link is directly detectable.
select 'monitoring_instance_receipt_link_conflict' as report,
       receipts.monitoring_instance_id as receipt_monitoring_instance_id,
       receipts.link_id,
       links.monitoring_instance_id as linked_monitoring_instance_id,
       links.vps_id as current_vps_id
from vps_monitoring_instance_create_idempotency receipts
join vps_monitoring_instance_links links using (link_id)
where receipts.monitoring_instance_id <> links.monitoring_instance_id
order by receipts.monitoring_instance_id, receipts.link_id;

commit;
