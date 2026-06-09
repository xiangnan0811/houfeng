drop view if exists ip_quality_latest_vps_summaries;
drop view if exists ip_quality_assigned_vps_reports;

create or replace view ip_quality_assigned_vps_reports as
with valid_reports as (
  select *
  from ip_quality_reports r
  where r.status in ('success', 'partial')
    and r.ip_address <> '0.0.0.0'
    and r.ip_version in (4, 6)
),
active_link_reports as (
  select
    l.vps_id,
    r.report_id,
    r.observed_at,
    r.ip_address,
    r.ip_version,
    r.status,
    r.risk_level,
    r.use_region_code,
    r.use_region_name,
    r.asn,
    r.organization,
    r.error_code,
    r.error_summary,
    false as ambiguous,
    'link'::text as assignment_mode
  from vps_monitoring_instance_links l
  join valid_reports r on r.monitoring_instance_id = l.monitoring_instance_id
  where l.unlinked_at is null
),
ip_match_candidates as (
  select
    v.vps_id,
    r.report_id,
    r.observed_at,
    r.ip_address,
    r.ip_version,
    r.status,
    r.risk_level,
    r.use_region_code,
    r.use_region_name,
    r.asn,
    r.organization,
    r.error_code,
    r.error_summary,
    count(*) over (partition by r.report_id) > 1 as ambiguous,
    'ip_match'::text as assignment_mode
  from vps_assets v
  join valid_reports r on r.ip_address in (nullif(v.ipv4, ''), nullif(v.ipv6, ''))
  where not exists (
    select 1
    from vps_monitoring_instance_links l
    where l.vps_id = v.vps_id
      and l.unlinked_at is null
  )
),
assigned_reports as (
  select * from active_link_reports
  union all
  select * from ip_match_candidates
)
select
  assigned_reports.vps_id,
  assigned_reports.report_id,
  assigned_reports.observed_at,
  assigned_reports.ip_address,
  assigned_reports.ip_version,
  assigned_reports.status,
  assigned_reports.risk_level,
  assigned_reports.use_region_code,
  assigned_reports.use_region_name,
  assigned_reports.asn,
  assigned_reports.organization,
  assigned_reports.observed_at < now() - interval '7 days' as stale,
  assigned_reports.ambiguous,
  assigned_reports.assignment_mode,
  assigned_reports.error_code,
  assigned_reports.error_summary,
  coalesce(pr.provider_count, 0)::int as provider_count,
  coalesce(su.unlockable_count, 0)::int as unlockable_count
from assigned_reports
left join (
  select report_id, count(*)::int as provider_count
  from ip_quality_provider_results
  group by report_id
) pr on pr.report_id = assigned_reports.report_id
left join (
  select report_id, count(*)::int as unlockable_count
  from ip_quality_service_unlocks
  group by report_id
) su on su.report_id = assigned_reports.report_id;

create or replace view ip_quality_latest_vps_summaries as
select
  ranked.vps_id,
  ranked.report_id,
  ranked.observed_at,
  ranked.ip_address,
  ranked.ip_version,
  ranked.status,
  ranked.risk_level,
  ranked.use_region_code,
  ranked.use_region_name,
  ranked.asn,
  ranked.organization,
  ranked.stale,
  ranked.ambiguous,
  ranked.assignment_mode,
  ranked.error_code,
  ranked.error_summary,
  ranked.provider_count,
  ranked.unlockable_count
from (
  select
    assigned.*,
    row_number() over (
      partition by assigned.vps_id
      order by assigned.observed_at desc, assigned.report_id desc
    ) as rn
  from ip_quality_assigned_vps_reports assigned
) ranked
where ranked.rn = 1;
