package store

import (
	"fmt"

	"houfeng/internal/center/targets"
)

// projectTargetHealth applies the same control and observation precedence used
// by target list/detail reads and dashboard aggregate reads. It deliberately
// does not mutate the stored incident projection.
func projectTargetHealth(record targets.TargetRecord) string {
	switch {
	case record.LifecycleStatus == targets.LifecycleRetired:
		return "已退役"
	case record.RunStatus == targets.RunStatusPaused:
		return "暂停"
	case record.RunStatus == targets.RunStatusMaintenance:
		return "维护中"
	case record.LastSuccessAt == nil && record.LastFailureAt == nil:
		return "数据不可用"
	default:
		return record.CurrentHealthStatus
	}
}

// targetHealthProjectionSQL mirrors projectTargetHealth for set-based readers.
// The alias is an internal SQL identifier supplied by callers, never request
// input.
func targetHealthProjectionSQL(alias string) string {
	return fmt.Sprintf(`case
		when %[1]s.lifecycle_status = 'retired' then '已退役'
		when %[1]s.run_status = '暂停' then '暂停'
		when %[1]s.run_status = '维护中' then '维护中'
		when %[1]s.last_success_at is null and %[1]s.last_failure_at is null then '数据不可用'
		else %[1]s.current_health_status
	end`, alias)
}

// targetCurrentVisibilitySQL is the single current-target visibility predicate
// shared by target list/detail projections, dashboard aggregates, and event
// visibility. A target is current when active and either unlinked from current
// service/domain associations or linked to at least one active VPS.
func targetCurrentVisibilitySQL(alias string) string {
	return fmt.Sprintf(`(%[1]s.lifecycle_status = 'active' and (
		not exists (
			select 1
			from (
				select vps_id, target_id from asset_service_associations where ended_at is null and target_id is not null
				union all
				select vps_id, target_id from asset_domain_associations where ended_at is null and target_id is not null
			) a
			where a.target_id = %[1]s.target_id
		)
		or exists (
			select 1
			from (
				select vps_id, target_id from asset_service_associations where ended_at is null and target_id is not null
				union all
				select vps_id, target_id from asset_domain_associations where ended_at is null and target_id is not null
			) a
			join vps_assets v on v.vps_id = a.vps_id
			where a.target_id = %[1]s.target_id
			  and v.lifecycle_status = 'active'
		)
	))`, alias)
}

// targetCoverageProjectionSQL keeps list/detail coverage in one SQL read. The
// executor predicate intentionally mirrors selectAgentPlanAssignmentsSQL and
// buildSyncPlan: enabled probes, active/maintenance targets, and monitoring
// instances that are not archived, retired, or paused with overlapping labels.
func targetCoverageProjectionSQL(alias string) string {
	return fmt.Sprintf(`(
		select count(*)::int
		from probe_items p
		where p.target_id = %[1]s.target_id
		  and p.enabled = true
	) as enabled_probe_count,
	case
		when %[1]s.lifecycle_status = 'active'
		 and %[1]s.run_status in ('启用', '维护中')
		then (
			select count(*)::int
			from monitoring_instances mi
			where mi.archived_at is null
			  and mi.lifecycle_status <> '已退役'
			  and mi.monitoring_status <> '暂停'
			  and mi.labels && %[1]s.execution_monitoring_instance_labels
		)
		else 0
	end as matching_executor_count`, alias)
}
