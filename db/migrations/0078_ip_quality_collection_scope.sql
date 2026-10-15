-- Collection revision 5 accepts an explicit canonical family set.
-- Preserve the view shape, grants and historical facts; revision 4 stays dual-stack.
create or replace view ip_quality_vps_family_state as
with stale_settings as (
  select case
    when coalesce(ip_quality_settings->>'stale_after_seconds', '') ~ '^[0-9]+$'
    then greatest((ip_quality_settings->>'stale_after_seconds')::integer, 60)
    else 604800
  end as seconds
  from center_settings where settings_id = 'center'
  union all
  select 604800 where not exists (select 1 from center_settings where settings_id = 'center')
), family_rows as (
  select v.vps_id, f.ip_version, valid.report_id as latest_report_id,
    attempt.report_id as latest_attempt_id,
    attempt.observed_at < now() - make_interval(secs => (select seconds from stale_settings limit 1)) as attempt_stale,
    case when
      jsonb_typeof(attempt.diagnostics_json->'collection_revision') = 'number'
      and attempt.diagnostics_json->>'collection_revision' in ('4','5')
      and jsonb_typeof(attempt.diagnostics_json->'collection_id') = 'string'
      and attempt.diagnostics_json->>'collection_id' ~ '^[A-Za-z0-9_-]{1,64}$'
      and (
        (attempt.diagnostics_json->>'collection_revision' = '4'
          and attempt.diagnostics_json->>'expected_ip_versions' = '[4, 6]')
        or (attempt.diagnostics_json->>'collection_revision' = '5'
          and attempt.diagnostics_json->>'expected_ip_versions' in ('[4]', '[6]', '[4, 6]'))
      )
      and attempt.diagnostics_json->'expected_ip_versions' @> jsonb_build_array(f.ip_version)
      and jsonb_typeof(attempt.diagnostics_json->'ip_address_family') = 'number'
      and attempt.diagnostics_json->>'ip_address_family' = f.ip_version::text
      and attempt.diagnostics_json->>'family_availability' in ('available','unavailable','unknown')
      and (
        (attempt.diagnostics_json->>'family_availability' = 'unavailable'
          and attempt.status = 'failure' and attempt.error_code = 'address_family_unavailable'
          and attempt.ip_address = case f.ip_version when 4 then '0.0.0.0' else '::' end)
        or (attempt.diagnostics_json->>'family_availability' in ('available','unknown')
          and (
            (attempt.ip_address = case f.ip_version when 4 then '0.0.0.0' else '::' end
              and attempt.status = 'failure' and attempt.error_code = 'lookup_failed')
            -- Match the coordinator's global-unicast terminal-address rule.
            -- Private unicast remains legal; loopback/link-local/multicast do not.
            or (attempt.address not in ('0.0.0.0'::inet, '::'::inet, '::1'::inet, '255.255.255.255'::inet)
              and not (attempt.address <<= any(array[
                '127.0.0.0/8'::inet, '169.254.0.0/16'::inet, '224.0.0.0/4'::inet,
                'fe80::/10'::inet, 'ff00::/8'::inet, '::ffff:0:0/96'::inet
              ]))
              and coalesce(attempt.error_code, '') not in ('lookup_failed','address_family_unavailable')
              and (attempt.status = 'failure' or attempt.diagnostics_json->>'family_availability' = 'available'))
          ))
      )
    then case
      when attempt.diagnostics_json->>'family_availability' = 'unavailable'
        and attempt.status = 'failure' and attempt.error_code = 'address_family_unavailable'
        and attempt.ip_address = case f.ip_version when 4 then '0.0.0.0' else '::' end
      then 'unavailable'
      when attempt.diagnostics_json->>'family_availability' = 'available' then 'available'
      when attempt.status in ('success','partial')
        and public.houfeng_parse_host_address(attempt.ip_address) not in ('0.0.0.0'::inet, '::'::inet)
      then 'available'
      else 'unknown'
    end else 'unknown' end as availability,
    family(public.houfeng_parse_host_address(case f.ip_version when 4 then v.ipv4 else v.ipv6 end)) = f.ip_version
      and public.houfeng_parse_host_address(case f.ip_version when 4 then v.ipv4 else v.ipv6 end)
        not in ('0.0.0.0'::inet, '::'::inet) as has_configured_address
  from vps_assets v
  cross join (values (4), (6)) f(ip_version)
  left join lateral (
    select assigned.report_id
    from ip_quality_assigned_vps_reports assigned
    join ip_quality_reports r on r.report_id = assigned.report_id
    where assigned.vps_id = v.vps_id and assigned.ip_version = f.ip_version
      and public.houfeng_parse_host_address(assigned.ip_address) not in ('0.0.0.0'::inet, '::'::inet)
      and family(public.houfeng_parse_host_address(assigned.ip_address)) = f.ip_version
    order by assigned.observed_at desc, r.is_backfilled asc, r.received_at desc, assigned.report_id desc
    limit 1
  ) valid on true
  left join lateral (
    select r.*, public.houfeng_parse_host_address(r.ip_address) as address
    from ip_quality_reports r
    where r.ip_version = f.ip_version
      and r.status in ('success','partial','failure')
      and family(public.houfeng_parse_host_address(r.ip_address)) = f.ip_version
      and (
        public.houfeng_parse_host_address(r.ip_address) not in ('0.0.0.0'::inet, '::'::inet)
        or (r.status = 'failure' and r.error_code in ('lookup_failed','address_family_unavailable'))
      )
      and (
        exists (select 1 from vps_monitoring_instance_links l
          where l.vps_id = v.vps_id and l.unlinked_at is null
            and l.monitoring_instance_id = r.monitoring_instance_id)
        or (r.report_id = valid.report_id and not exists (
          select 1 from vps_monitoring_instance_links l where l.vps_id = v.vps_id and l.unlinked_at is null
        ))
      )
    order by r.observed_at desc, r.is_backfilled asc, r.received_at desc, r.report_id desc
    limit 1
  ) attempt on true
), eligible as (
  select *,
    not (availability = 'unavailable' and not coalesce(attempt_stale, true))
      and (latest_report_id is not null or latest_attempt_id is not null or coalesce(has_configured_address, false)) as has_evidence
  from family_rows
), defaults as (
  select vps_id, coalesce(min(ip_version) filter (where has_evidence), 4) as ip_version
  from eligible group by vps_id
)
select e.vps_id, e.ip_version, e.latest_report_id, e.latest_attempt_id, e.availability,
  coalesce(e.attempt_stale, false) as attempt_stale, e.ip_version = d.ip_version as is_default
from eligible e join defaults d using (vps_id);
