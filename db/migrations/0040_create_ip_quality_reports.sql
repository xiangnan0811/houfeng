create table if not exists ip_quality_reports (
  report_id text primary key,
  monitoring_instance_id text not null references monitoring_instances(monitoring_instance_id) on delete cascade,
  observed_at timestamptz not null,
  received_at timestamptz not null default now(),
  agent_version text not null,
  fingerprint text not null,
  sync_batch_id text not null,
  ip_address text not null,
  ip_version integer not null,
  status text not null,
  asn text not null default '',
  organization text not null default '',
  latitude double precision,
  longitude double precision,
  use_region_code text not null default '',
  use_region_name text not null default '',
  registered_region_code text not null default '',
  registered_region_name text not null default '',
  risk_level text not null default '',
  error_code text not null default '',
  error_summary text not null default '',
  is_backfilled boolean not null default false,
  raw_json jsonb,
  created_at timestamptz not null default now(),
  constraint ip_quality_reports_ip_version_allowed check (ip_version in (4, 6)),
  constraint ip_quality_reports_status_allowed check (status in ('success', 'partial', 'failure'))
);

create index if not exists idx_ip_quality_reports_instance_time
  on ip_quality_reports (monitoring_instance_id, observed_at desc, report_id desc);

create index if not exists idx_ip_quality_reports_ip_time
  on ip_quality_reports (ip_address, observed_at desc, report_id desc);

create table if not exists ip_quality_provider_results (
  result_id text primary key,
  report_id text not null references ip_quality_reports(report_id) on delete cascade,
  provider text not null,
  usage_type text not null default '',
  company_type text not null default '',
  risk_level text not null default '',
  risk_score text not null default '',
  region_code text not null default '',
  region_name text not null default '',
  is_proxy boolean,
  is_tor boolean,
  is_vpn boolean,
  is_server boolean,
  is_abuser boolean,
  is_robot boolean,
  error_code text not null default '',
  error_summary text not null default '',
  created_at timestamptz not null default now(),
  constraint ip_quality_provider_results_provider_not_blank check (length(trim(provider)) > 0)
);

create unique index if not exists idx_ip_quality_provider_results_report_provider
  on ip_quality_provider_results (report_id, provider);

create table if not exists ip_quality_service_unlocks (
  unlock_id text primary key,
  report_id text not null references ip_quality_reports(report_id) on delete cascade,
  service text not null,
  status text not null,
  region text not null default '',
  unlock_type text not null default '',
  error_code text not null default '',
  error_summary text not null default '',
  created_at timestamptz not null default now(),
  constraint ip_quality_service_unlocks_service_not_blank check (length(trim(service)) > 0),
  constraint ip_quality_service_unlocks_status_not_blank check (length(trim(status)) > 0)
);

create unique index if not exists idx_ip_quality_service_unlocks_report_service
  on ip_quality_service_unlocks (report_id, service);

drop view if exists ip_quality_latest_vps_summaries;
drop view if exists ip_quality_assigned_vps_reports;

create view ip_quality_assigned_vps_reports as
with active_link_reports as (
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
  join ip_quality_reports r on r.monitoring_instance_id = l.monitoring_instance_id
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
  join ip_quality_reports r on r.ip_address in (nullif(v.ipv4, ''), nullif(v.ipv6, ''))
  where not exists (
    select 1
    from active_link_reports linked
    where linked.vps_id = v.vps_id
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

create view ip_quality_latest_vps_summaries as
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
