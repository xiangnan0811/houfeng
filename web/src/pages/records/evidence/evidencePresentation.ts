import type { BadgeTone } from '../../../components/atoms'
import { formatBytes, formatBytesPerSecond, formatUptime } from '../../../lib/format'
import { STATE_CHANGE_EVENT_TYPE_LABELS, type EvidenceKindName, type EvidenceQuality } from '../../../lib/types'

/** 证据阅读页的中文标签与状态色：未知取值原样显示，不猜测含义。 */

export const EVIDENCE_KIND_LABELS: Record<EvidenceKindName, string> = {
  'ip_quality.report': 'IP 质量',
  'monitoring.host': '主机监控',
  'monitoring.probe': '入口探测',
  'monitoring.event': '监控事件',
  'subscription.cost': '订阅成本',
  'command.audit': '命令审计',
  'comparison.result': '比较结果',
}

const IDENTITY_TYPE_LABELS: Record<string, string> = {
  vps: 'VPS',
  monitoring_instance: '监控实例',
  target: '探测目标',
  subscription: '订阅',
  ip_quality_report: 'IP 质量报告',
  command_audit: '命令审计',
}

// 取值来自服务端字符串：只认自有键，避免 "__proto__" / "constructor" 命中原型属性。
function ownValue<T>(map: Readonly<Record<string, T>>, key: string): T | undefined {
  return Object.hasOwn(map, key) ? map[key] : undefined
}

export function identityTypeLabel(type: string): string {
  return ownValue(IDENTITY_TYPE_LABELS, type) ?? type
}

export function evidenceKindLabel(kind: string): string {
  return ownValue<string>(EVIDENCE_KIND_LABELS, kind) ?? kind
}

const GENERATED_EVIDENCE_TITLES: Readonly<Record<string, string>> = {
  'Monitoring events': '监控事件',
  'Monitoring host evidence': '主机监控',
  'Monitoring probe evidence': '入口探测',
  'Monitoring evidence': '监控证据',
  'Command audit': '命令审计',
  'IP quality report': 'IP 质量报告',
  'Subscription cost': '订阅成本',
  'Comparison result': '比较结果',
}

const GENERATED_KIND_TITLE = /^([a-z][a-z0-9_.]*)(?:\/v\d+)?$/

/** Maps frozen generated evidence titles. Arbitrary user titles stay unchanged. */
export function presentGeneratedEvidenceTitle(title: string): string {
  const trimmed = title.trim()
  if (!trimmed) return trimmed
  const generated = ownValue(GENERATED_EVIDENCE_TITLES, trimmed)
  if (generated) return generated
  const match = GENERATED_KIND_TITLE.exec(trimmed)
  if (!match) return trimmed
  const label = evidenceKindLabel(match[1] ?? '')
  return label === match[1] ? trimmed : label
}

const QUALITY_TONES: Record<EvidenceQuality['status'], BadgeTone> = {
  complete: 'normal',
  partial: 'notice',
  degraded: 'alert',
  unknown: 'neutral',
}

/** 页头徽标：完整说明质量含义，不写成"质量部分"。 */
export const QUALITY_BADGE_LABELS: Record<EvidenceQuality['status'], string> = {
  complete: '数据完整',
  partial: '部分覆盖',
  degraded: '质量降级',
  unknown: '质量未知',
}

export function qualityTone(status: EvidenceQuality['status']): BadgeTone {
  return QUALITY_TONES[status]
}

export function lookup(labels: Readonly<Record<string, string>>, value: string): string {
  return ownValue(labels, value) ?? value
}

export function toneOf(tones: Readonly<Record<string, BadgeTone>>, value: string): BadgeTone {
  return ownValue(tones, value) ?? 'neutral'
}

/** 监控事件类型沿用全站事件中心的中文用词，人工更正为证据读模型独有。 */
const EVENT_TYPE_LABELS: Readonly<Record<string, string>> = {
  ...STATE_CHANGE_EVENT_TYPE_LABELS,
  event_corrected: '人工更正',
}

export function eventTypeLabel(type: string): string {
  return lookup(EVENT_TYPE_LABELS, type)
}

// 监控指标：名称、单位与数值格式。
const METRIC_LABELS: Record<string, string> = {
  cpu_iowait_pct: 'CPU iowait',
  cpu_steal_pct: 'CPU steal',
  cpu_usage_pct: 'CPU 使用率',
  disk_busy_pct: '磁盘繁忙',
  disk_read_bytes_per_sec: '磁盘读',
  disk_total_bytes: '磁盘总量',
  disk_used_pct: '磁盘使用率',
  disk_write_bytes_per_sec: '磁盘写',
  inode_used_pct: 'inode 使用率',
  load_1: '负载 1m',
  load_5: '负载 5m',
  load_15: '负载 15m',
  mem_available_bytes: '可用内存',
  mem_total_bytes: '内存总量',
  mem_used_pct: '内存使用率',
  net_in_bytes_per_sec: '入站流量',
  net_out_bytes_per_sec: '出站流量',
  swap_used_pct: 'Swap 使用率',
  uptime_seconds: '运行时长',
  http_status: 'HTTP 状态码',
  latency_ms: '延迟',
  success_ratio: '成功率',
  tls_expiry_days: '证书剩余',
}

export function metricLabel(name: string): string {
  return ownValue(METRIC_LABELS, name) ?? name
}

function trimNumber(value: number, digits: number): string {
  return String(Number(value.toFixed(digits)))
}

export function formatMetricValue(value: number, unit: string): string {
  switch (unit) {
    case 'percent': return `${trimNumber(value, 1)}%`
    case 'ms': return `${trimNumber(value, 0)} ms`
    case 'bytes': return formatBytes(value)
    case 'bytes_per_second': return formatBytesPerSecond(value)
    case 'seconds': return formatUptime(value)
    case 'ratio': return `${trimNumber(value * 100, 1)}%`
    case 'days': return `${trimNumber(value, 0)} 天`
    case 'load': return trimNumber(value, 2)
    case 'status_code': return String(Math.round(value))
    default: return unit ? `${trimNumber(value, 2)} ${unit}` : trimNumber(value, 2)
  }
}

/** 纵轴刻度空间有限：与峰值 / 提示同一量纲（速率保留 /s），用紧凑写法放进刻度槽。 */
export function formatMetricAxisValue(value: number, unit: string): string {
  switch (unit) {
    case 'percent': return `${trimNumber(value, 0)}%`
    case 'ratio': return `${trimNumber(value * 100, 0)}%`
    case 'ms': return `${trimNumber(value, 0)}ms`
    case 'bytes': return compactBytes(value)
    case 'bytes_per_second': return `${compactBytes(value)}/s`
    case 'seconds': return compactDuration(value)
    case 'days': return `${trimNumber(value, 0)}天`
    case 'status_code': return String(Math.round(value))
    default: return trimNumber(value, 1)
  }
}

// 一位小数只在个位数时保留，其余取整，刻度最长不超过 "999KB/s" / "-9.5时"。
function compact(value: number): string {
  return Math.abs(value) < 10 ? trimNumber(value, 1) : trimNumber(value, 0)
}

const BYTE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']

function compactBytes(value: number): string {
  let scaled = value
  let index = 0
  // 按舍入后的系数进位（999.5 会显示成 1000），保证刻度不出现四位整数。
  while (Math.abs(Number(compact(scaled))) >= 1000 && index < BYTE_UNITS.length - 1) {
    scaled /= 1024
    index += 1
  }
  return `${compact(scaled)}${BYTE_UNITS[index]}`
}

const DURATION_STEPS: ReadonlyArray<[seconds: number, limit: number, unit: string]> = [
  [1, 60, '秒'],
  [60, 60, '分'],
  [3600, 24, '时'],
  [86_400, 365, '天'],
  [365 * 86_400, Number.POSITIVE_INFINITY, '年'],
]

// 图表会把值域向下扩展到负数，按绝对值选单位再补回符号，避免 "-3153600秒" 这类超长刻度。
function compactDuration(seconds: number): string {
  const sign = seconds < 0 ? '-' : ''
  const magnitude = Math.abs(seconds)
  for (const [size, limit, unit] of DURATION_STEPS) {
    const text = compact(magnitude / size)
    if (Number(text) < limit) return `${sign}${text}${unit}`
  }
  return `${sign}${compact(magnitude / (365 * 86_400))}年`
}

/** 纵轴刻度槽宽度：容纳最长的紧凑刻度（如 "999KB/s"，10px 等宽约 42px）。 */
export const METRIC_AXIS_GUTTER = 56

export function formatDuration(seconds: number): string {
  if (seconds % 86_400 === 0) return `${seconds / 86_400} 天`
  if (seconds % 3600 === 0) return `${seconds / 3600} 小时`
  if (seconds % 60 === 0) return `${seconds / 60} 分钟`
  return `${seconds} 秒`
}
