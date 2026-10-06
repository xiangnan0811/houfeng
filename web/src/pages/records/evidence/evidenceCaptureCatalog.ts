import type { EvidenceKindName, RecordSubjectKind } from '../../../lib/types'

// 可采集证据的目录。后端没有提供目录接口，这里与各 adapter 的 ValidateSelection 保持一致：
// internal/center/evidence/adapters/{monitoring,events,command_audits,ip_quality,subscription_costs}.go。
// comparison.result 只能由横向比较另存产生，不在此列。

export type EvidenceCaptureWindowMode = 'range' | 'month'

export type EvidenceCaptureMetric = Readonly<{ value: string; label: string; common?: boolean }>

export type EvidenceCaptureKind = Readonly<{
  kind: EvidenceKindName
  schema_version: number
  label: string
  /** 来源取自记录主体：source_type 与主体 kind 同名。 */
  sourceKinds: readonly RecordSubjectKind[]
  window: EvidenceCaptureWindowMode
  /** 为空表示该类型不选指标；非空时至少选一个，提交前按字典序排序。 */
  metrics: readonly EvidenceCaptureMetric[]
  /** 只有监控时间序列可调精度，0 表示由服务端按窗口长度取默认精度。 */
  adjustablePrecision: boolean
  sensitiveFields: readonly EvidenceCaptureMetric[]
}>

const HOST_METRICS: readonly EvidenceCaptureMetric[] = [
  { value: 'cpu_usage_pct', label: 'CPU 使用率', common: true },
  { value: 'mem_used_pct', label: '内存使用率', common: true },
  { value: 'disk_used_pct', label: '磁盘使用率', common: true },
  { value: 'net_in_bytes_per_sec', label: '入站流量', common: true },
  { value: 'net_out_bytes_per_sec', label: '出站流量', common: true },
  { value: 'load_1', label: '1 分钟负载', common: true },
  { value: 'cpu_iowait_pct', label: 'CPU IO 等待' },
  { value: 'cpu_steal_pct', label: 'CPU 窃取' },
  { value: 'disk_busy_pct', label: '磁盘繁忙度' },
  { value: 'disk_read_bytes_per_sec', label: '磁盘读取' },
  { value: 'disk_write_bytes_per_sec', label: '磁盘写入' },
  { value: 'disk_total_bytes', label: '磁盘总量' },
  { value: 'inode_used_pct', label: 'inode 使用率' },
  { value: 'load_5', label: '5 分钟负载' },
  { value: 'load_15', label: '15 分钟负载' },
  { value: 'mem_available_bytes', label: '可用内存' },
  { value: 'mem_total_bytes', label: '内存总量' },
  { value: 'swap_used_pct', label: 'Swap 使用率' },
  { value: 'uptime_seconds', label: '运行时长' },
]

const PROBE_METRICS: readonly EvidenceCaptureMetric[] = [
  { value: 'success_ratio', label: '成功率', common: true },
  { value: 'latency_ms', label: '延迟', common: true },
  { value: 'http_status', label: 'HTTP 状态', common: true },
  { value: 'tls_expiry_days', label: '证书剩余天数', common: true },
]

const IP_QUALITY_SENSITIVE_FIELDS: readonly EvidenceCaptureMetric[] = [
  { value: 'ip_address', label: 'IP 地址' },
  { value: 'asn', label: 'ASN' },
  { value: 'organization', label: '所属组织' },
  { value: 'assignment_mode', label: '分配方式' },
  { value: 'latitude', label: '纬度' },
  { value: 'longitude', label: '经度' },
  { value: 'registered_region_code', label: '注册地区代码' },
  { value: 'registered_region_name', label: '注册地区' },
  { value: 'use_region_code', label: '使用地区代码' },
  { value: 'use_region_name', label: '使用地区' },
  { value: 'providers.region_code', label: '各数据库地区代码' },
  { value: 'providers.region_name', label: '各数据库地区' },
  { value: 'services.region', label: '服务解锁地区' },
]

export const EVIDENCE_CAPTURE_KINDS: readonly EvidenceCaptureKind[] = [
  { kind: 'monitoring.host', schema_version: 1, label: '主机监控', sourceKinds: ['monitoring_instance'], window: 'range', metrics: HOST_METRICS, adjustablePrecision: true, sensitiveFields: [] },
  { kind: 'monitoring.probe', schema_version: 2, label: '入口探测', sourceKinds: ['target'], window: 'range', metrics: PROBE_METRICS, adjustablePrecision: true, sensitiveFields: [] },
  { kind: 'monitoring.event', schema_version: 2, label: '监控事件', sourceKinds: ['monitoring_instance', 'target'], window: 'range', metrics: [], adjustablePrecision: false, sensitiveFields: [] },
  { kind: 'command.audit', schema_version: 1, label: '命令审计', sourceKinds: ['monitoring_instance'], window: 'range', metrics: [], adjustablePrecision: false, sensitiveFields: [] },
  { kind: 'ip_quality.report', schema_version: 1, label: 'IP 质量', sourceKinds: ['vps'], window: 'range', metrics: [], adjustablePrecision: false, sensitiveFields: IP_QUALITY_SENSITIVE_FIELDS },
  { kind: 'subscription.cost', schema_version: 1, label: '订阅成本', sourceKinds: ['vps'], window: 'month', metrics: [], adjustablePrecision: false, sensitiveFields: [] },
]

/** 监控精度可选值：必须是 60 的倍数且不低于服务端按窗口长度给出的默认精度。 */
export const MONITORING_PRECISION_OPTIONS: readonly { seconds: number; label: string }[] = [
  { seconds: 0, label: '自动' },
  { seconds: 60, label: '1 分钟' },
  { seconds: 300, label: '5 分钟' },
  { seconds: 3600, label: '1 小时' },
  { seconds: 86_400, label: '1 天' },
]

/** 服务端按窗口长度选择的默认精度（monitoring.go defaultPrecision）。 */
export function defaultMonitoringPrecision(windowSeconds: number): number {
  if (windowSeconds <= 6 * 3600) return 60
  if (windowSeconds <= 48 * 3600) return 300
  if (windowSeconds <= 30 * 86_400) return 3600
  return 86_400
}

export function precisionOptionsForWindow(windowSeconds: number): readonly { seconds: number; label: string }[] {
  const minimum = defaultMonitoringPrecision(windowSeconds)
  return MONITORING_PRECISION_OPTIONS.filter((option) => option.seconds === 0 || option.seconds >= minimum)
}

export function sortedSelection(values: readonly string[]): string[] {
  return [...new Set(values)].sort()
}

// 证据窗口按浏览器本地时间显示（分钟精度）。
export function formatLocalTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  const pad = (part: number) => String(part).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}

export function formatEvidenceWindow(range: { start: string; end: string }): string {
  return `${formatLocalTime(range.start)} – ${formatLocalTime(range.end)}`
}
