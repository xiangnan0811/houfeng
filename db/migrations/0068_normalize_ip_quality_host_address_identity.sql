-- Keep the input argument unnamed so PostgreSQL's catalog identity is exactly
-- public.houfeng_parse_host_address(text); p_raw remains a readable local alias.
create or replace function public.houfeng_parse_host_address(text)
returns pg_catalog.inet
language plpgsql
immutable
strict
parallel safe
security invoker
set search_path = pg_catalog
as $function$
declare
  p_raw ALIAS FOR $1;
  value text;
begin
  value := btrim(
    p_raw,
    U&'\0009\000A\000B\000C\000D\0020\0085\00A0\1680\2000\2001\2002\2003\2004\2005\2006\2007\2008\2009\200A\2028\2029\202F\205F\3000'
  );
  if value = '' or value !~ '^[0-9A-Fa-f:.]+$' then
    return null;
  end if;
  if position(':' in value) = 0 then
    if value !~ '^(0|[1-9][0-9]{0,2})([.](0|[1-9][0-9]{0,2})){3}$' then
      return null;
    end if;
  elsif position('.' in value) > 0 then
    if value !~ ':(0|[1-9][0-9]{0,2})([.](0|[1-9][0-9]{0,2})){3}$' then
      return null;
    end if;
  end if;

  begin
    return value::pg_catalog.inet;
  exception
    when invalid_text_representation then
      return null;
  end;
end
$function$;

create or replace view ip_quality_assigned_vps_reports as
with valid_reports as materialized (
  select r.*, public.houfeng_parse_host_address(r.ip_address) as ip_identity
  from ip_quality_reports r
  where r.status in ('success', 'partial')
    and r.ip_address <> '0.0.0.0'
    and r.ip_version in (4, 6)
),
ip_quality_stale_settings as (
  select
    case
      when coalesce(ip_quality_settings, '{}'::jsonb) ? 'stale_after_seconds'
        and coalesce(ip_quality_settings->>'stale_after_seconds', '') ~ '^[0-9]+$'
      then greatest((ip_quality_settings->>'stale_after_seconds')::integer, 60)
      else 604800
    end as stale_after_seconds
  from center_settings
  where settings_id = 'center'
  union all
  select 604800
  where not exists (
    select 1 from center_settings where settings_id = 'center'
  )
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
    r.coverage_json,
    false as ambiguous,
    'link'::text as assignment_mode
  from vps_monitoring_instance_links l
  join valid_reports r on r.monitoring_instance_id = l.monitoring_instance_id
  where l.unlinked_at is null
),
fallback_asset_identities as materialized (
  select
    v.vps_id,
    public.houfeng_parse_host_address(nullif(v.ipv4, '')) as ipv4_identity,
    public.houfeng_parse_host_address(nullif(v.ipv6, '')) as ipv6_identity
  from vps_assets v
  where not exists (
    select 1
    from vps_monitoring_instance_links l
    where l.vps_id = v.vps_id
      and l.unlinked_at is null
  )
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
    r.coverage_json,
    count(*) over (partition by r.report_id) > 1 as ambiguous,
    'ip_match'::text as assignment_mode
  from fallback_asset_identities v
  join valid_reports r
    on r.ip_identity is not null
   and (
     r.ip_identity = v.ipv4_identity
     or r.ip_identity = v.ipv6_identity
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
  assigned_reports.observed_at < now() - make_interval(secs => (
    select stale_after_seconds
    from ip_quality_stale_settings
    limit 1
  )) as stale,
  assigned_reports.ambiguous,
  assigned_reports.assignment_mode,
  assigned_reports.error_code,
  assigned_reports.error_summary,
  coalesce(pr.provider_count, 0)::int as provider_count,
  coalesce(su.unlockable_count, 0)::int as unlockable_count,
  assigned_reports.coverage_json
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
