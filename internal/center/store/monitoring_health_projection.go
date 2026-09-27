package store

import "fmt"

// monitoringHealthProjectionSQL mirrors projectMonitoringHealth for aggregate
// readers. The stored incident health is only authoritative after lifecycle,
// control, binding and trusted-online evidence have been considered. Arguments
// are internal SQL identifiers/expressions, never request input.
func monitoringHealthProjectionSQL(alias, ownerLifecycle string) string {
	return fmt.Sprintf(`case
		when %s = 'archived' then '已归档'
		when %[2]s.lifecycle_status = '已退役' then '已退役'
		when %[2]s.lifecycle_status = '待接入' then '未接入'
		when %[2]s.monitoring_status = '暂停' then '暂停'
		when %[2]s.monitoring_status = '维护中' then '维护中'
		when %[2]s.binding_status <> '已绑定' then '绑定待确认'
		when %[2]s.last_trusted_online_at is null or %[2]s.current_health_status in ('unknown', '未接入', '') then '数据不可用'
		else %[2]s.current_health_status
	end`, ownerLifecycle, alias)
}
