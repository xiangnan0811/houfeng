/** Operator-facing labels for incident_class. Unknown values become 异常, never snake_case. */
export const INCIDENT_CLASS_LABELS: Record<string, string> = {
  monitoring_instance_heartbeat_missing: '监控实例心跳缺失',
  monitoring_instance_disk_pressure: '监控实例磁盘压力',
  monitoring_instance_inode_pressure: '监控实例 inode 压力',
  monitoring_instance_resource_pressure: '监控实例资源压力',
  resource_threshold: '资源阈值',
  heartbeat_stale: '心跳超时',
  target_probe_failure: '目标探测失败',
  target_tls_expiry: '目标 TLS 即将过期',
  connectivity: '连通性',
  certificate: '证书',
  performance: '性能',
  availability: '可用性',
}

export function incidentClassLabel(value: string): string {
  const trimmed = value.trim()
  if (!trimmed) return ''
  return INCIDENT_CLASS_LABELS[trimmed] ?? '异常'
}

export type ObservabilityTone = 'critical' | 'alert' | 'notice' | 'offline'

export function severityTone(severity: string): ObservabilityTone {
  if (severity === '严重') return 'critical'
  if (severity === '告警') return 'alert'
  if (severity === '关注') return 'notice'
  return 'offline'
}
