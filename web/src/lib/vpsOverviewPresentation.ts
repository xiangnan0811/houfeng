import {
  VPS_LIFECYCLE_STATUS_LABELS,
  VPS_RENEWAL_DECISION_LABELS,
  VPS_USAGE_STATUS_LABELS,
  type VPSLifecycleStatus,
  type VPSOverview,
  type VPSRenewalDecision,
  type VPSUsageStatus,
  type SubjectActivitySubjectSnapshot,
} from './types'


const OVERALL_STATUS_LABELS: Record<string, string> = {
  healthy: '总体正常',
  attention: '需要关注',
  notice: '留意',
  critical: '严重',
}

const MONITORING_STATUS_LABELS: Record<string, string> = {
  unlinked: '未关联',
  unknown: '未知',
  正常: '正常',
  关注: '关注',
  告警: '告警',
  严重: '严重',
}

const IP_STATUS_LABELS: Record<string, string> = {
  low: '低风险',
  medium: '中风险',
  moderate: '中风险',
  high: '高风险',
  critical: '严重风险',
  missing: '缺少证据',
  not_configured: '未启用',
  success: '采集成功',
  partial: '采集不完整',
  failure: '采集失败',
  unknown: '未知',
}

const RELATION_STATUS_LABELS: Record<string, string> = {
  ...MONITORING_STATUS_LABELS,
  ...IP_STATUS_LABELS,
  ...VPS_RENEWAL_DECISION_LABELS,
  unavailable: '暂不可用',
  active: '生效中',
}

const ANOMALY_SOURCE_LABELS: Record<string, string> = {
  monitoring: '监控',
  ip_quality: 'IP 质量',
  renewal: '续费',
  lifecycle: '生命周期',
  overview: '概览',
}

const IMPORTANCE_LABELS: Record<string, string> = {
  high: '高',
  normal: '普通',
  critical: '关键',
}

export function overviewLifecycleLabel(value: string): string {
  return VPS_LIFECYCLE_STATUS_LABELS[value as VPSLifecycleStatus] ?? value
}

export function overviewUsageLabel(value: string): string {
  return VPS_USAGE_STATUS_LABELS[value as VPSUsageStatus] ?? value
}

export function overviewRenewalLabel(value: string): string {
  return VPS_RENEWAL_DECISION_LABELS[value as VPSRenewalDecision] ?? value
}

export function overviewOverallLabel(value: string): string {
  return OVERALL_STATUS_LABELS[value] ?? value
}

export function overviewMonitoringLabel(value: string): string {
  return MONITORING_STATUS_LABELS[value] ?? value
}

export function overviewIPLabel(value: string): string {
  return IP_STATUS_LABELS[value] ?? value
}

const HEALTHY_OVERALL: Record<string, true> = { healthy: true, 总体正常: true }
const ADVERSE_OVERALL: Record<string, 'notice' | 'alert'> = {
  attention: 'notice',
  需要关注: 'notice',
  notice: 'notice',
  留意: 'notice',
  critical: 'alert',
  严重: 'alert',
}
const INCOMPLETE_MONITORING: Record<string, true> = {
  unlinked: true,
  unknown: true,
  未知: true,
  未关联: true,
}
const INCOMPLETE_IP: Record<string, true> = {
  unknown: true,
  未知: true,
  missing: true,
  failure: true,
  缺少证据: true,
  采集失败: true,
  unavailable: true,
  暂不可用: true,
}

export type OverviewObservationTone = 'ok' | 'notice' | 'alert' | 'unknown'

export type OverallObservationPresentation = {
  label: string
  tone: OverviewObservationTone
  explanation: string | null
  rawLabel: string
}

function runningEvidenceGaps(summary: VPSOverview['summary']): string[] {
  const gaps: string[] = []
  const monitoring = summary.monitoring
  if (monitoring.section.state === 'stale') gaps.push('监控数据陈旧')
  else if (monitoring.section.state === 'unavailable') gaps.push('监控暂不可用')
  else if (INCOMPLETE_MONITORING[monitoring.status]) {
    gaps.push(monitoring.status === 'unlinked' || monitoring.status === '未关联' ? '监控未关联' : '监控证据不完整')
  }
  const ipQuality = summary.ip_quality
  if (ipQuality.section.state === 'stale') gaps.push('IP 质量数据陈旧')
  else if (ipQuality.section.state === 'unavailable') gaps.push('IP 质量暂不可用')
  else if (INCOMPLETE_IP[ipQuality.status]) gaps.push(`IP 质量${overviewIPLabel(ipQuality.status)}`)
  return gaps
}

export function overviewOverallPresentation(
  summary: VPSOverview['summary'],
  lifecycleStatus?: string,
): OverallObservationPresentation {
  const rawLabel = overviewOverallLabel(summary.overall.status) || '—'
  const gaps = runningEvidenceGaps(summary)
  const adverse = ADVERSE_OVERALL[summary.overall.status]
  if (adverse) {
    const parts: string[] = []
    if (gaps.length > 0) parts.push(`${gaps.join('，')}。`)
    if (
      (summary.overall.status === 'attention' || summary.overall.status === '需要关注')
      && lifecycleStatus === 'to_cancel'
    ) {
      parts.push('取消计划待处理。')
    }
    return {
      label: rawLabel,
      tone: adverse,
      explanation: parts.length > 0 ? parts.join('') : null,
      rawLabel,
    }
  }
  if (gaps.length > 0) {
    return {
      label: '观测不完整',
      tone: 'unknown',
      explanation: `${gaps.join('，')}；以下分项保留各自的观测结果。`,
      rawLabel,
    }
  }

  if (HEALTHY_OVERALL[summary.overall.status]) {
    const ipDisabled = summary.ip_quality.status === 'not_configured' || summary.ip_quality.status === '未启用'
    return {
      label: rawLabel,
      tone: 'ok',
      explanation: ipDisabled ? '仅基于已启用的观测；IP 质量未启用。' : null,
      rawLabel,
    }
  }
  return { label: rawLabel, tone: 'unknown', explanation: null, rawLabel }
}


export function overviewSummaryCellLabel(key: 'overall' | 'monitoring' | 'ip_quality' | 'renewal', value: string): string {
  switch (key) {
    case 'overall':
      return overviewOverallLabel(value)
    case 'monitoring':
      return overviewMonitoringLabel(value)
    case 'ip_quality':
      return overviewIPLabel(value)
    case 'renewal':
      return overviewRenewalLabel(value)
  }
}

export function overviewRelationStatusLabel(value: string): string {
  return RELATION_STATUS_LABELS[value] ?? overviewRenewalLabel(value)
}

export function overviewAnomalySourceLabel(value: string): string {
  return ANOMALY_SOURCE_LABELS[value] ?? value
}

export function overviewImportanceLabel(value: string): string {
  return IMPORTANCE_LABELS[value] ?? value
}

export function overviewLocationLabel(parts: Array<string | undefined>): string {
  return parts.map((part) => part?.trim()).filter(Boolean).join(' · ')
}

const SUMMARY_DETAIL_FALLBACKS: Record<string, string> = {
  historical_disabled: '存在历史报告（当前未启用）',
  ip_quality_disabled_has_history: '存在历史报告（当前未启用）',
}

export function overviewSummaryDetailLabel(
  key: 'overall' | 'monitoring' | 'ip_quality' | 'renewal',
  value: string,
): string {
  const trimmed = value.trim()
  if (!trimmed) return ''
  if (key === 'monitoring') return trimmed
  if (SUMMARY_DETAIL_FALLBACKS[trimmed]) return SUMMARY_DETAIL_FALLBACKS[trimmed]
  if (key === 'ip_quality') return overviewIPLabel(trimmed)
  if (key === 'renewal') return overviewRenewalLabel(trimmed)
  if (key === 'overall') return overviewOverallLabel(trimmed)
  return trimmed
}

export function overviewAnomalyDetailLabel(ruleId: string, value: string): string {
  const trimmed = value.trim()
  if (!trimmed) return ''
  if (ruleId.startsWith('monitoring.')) return trimmed
  if (ruleId === 'source.unavailable.v1') {
    return trimmed.split(',').map((part) => overviewAnomalySourceLabel(part.trim())).join('、')
  }
  if (ruleId.startsWith('ip_quality.')) return overviewIPLabel(trimmed)
  if (ruleId.startsWith('lifecycle.')) return overviewLifecycleLabel(trimmed)
  if (ruleId.startsWith('renewal.')) return overviewRenewalLabel(trimmed)
  return trimmed
}

export function overviewAnomalySeverityClass(severity: string): string {
  switch (severity) {
    case 'critical':
    case 'warning':
    case 'notice':
    case 'info':
      return `vps-overview-anomalies__item--${severity}`
    default:
      return ''
  }
}

const ACTIVITY_TITLE_SEPARATOR = /^(?:[\s·•・—–|:：-]+)+/

export function overviewActivityForeignNames(
  subjects: SubjectActivitySubjectSnapshot[],
  asset: { vpsId: string; displayName: string },
): string[] {
  const assetId = asset.vpsId.trim()
  const assetName = asset.displayName.trim()
  const names: string[] = []
  for (const subject of subjects) {
    if (subject.kind === 'vps' && subject.source_id === assetId) continue
    for (const value of Object.values(subject.identity)) {
      const trimmed = value.trim()
      if (trimmed && trimmed !== assetName) names.push(trimmed)
    }
  }
  return names
}

export function overviewSameAssetActionTitle(
  title: string,
  assetName: string,
  otherObjectNames: string[] = [],
): string {
  const full = title.trim()
  const asset = assetName.trim()
  if (!full || !asset || !full.startsWith(asset)) return full
  const after = full.slice(asset.length)
  if (!ACTIVITY_TITLE_SEPARATOR.test(after)) return full
  const remainder = after.replace(ACTIVITY_TITLE_SEPARATOR, '').trim()
  if (!remainder) return full
  const others = otherObjectNames.map((name) => name.trim()).filter((name) => name && name !== asset)
  if (others.some((name) => remainder.includes(name))) return full
  return remainder
}

const GENERATED_MONITORING_COUNT = /^(\d+)\s*个实例(?:\s*[·•]\s*(.*))?$/

export function overviewMonitoringInstanceCountLabel(count: number): string {
  return `${count}个实例`
}

export function overviewMonitoringSupportingDetail(
  detail: string,
  statusLabel: string,
  relationCount?: number,
): string {
  const trimmed = detail.trim()
  const generated = GENERATED_MONITORING_COUNT.exec(trimmed)
  let extra = ''
  if (generated) {
    const rest = (generated[2] ?? '').trim()
    const restLabel = rest ? overviewMonitoringLabel(rest) : ''
    const duplicatesStatus = !rest || rest === statusLabel || restLabel === statusLabel
    extra = duplicatesStatus ? '' : rest
  } else if (trimmed && trimmed !== statusLabel) {
    extra = trimmed
  }

  const count = relationCount != null && relationCount > 0
    ? relationCount
    : generated
      ? Number(generated[1])
      : undefined
  if (count != null && count > 0) {
    const countLabel = overviewMonitoringInstanceCountLabel(count)
    return extra && extra !== countLabel ? `${countLabel} · ${extra}` : countLabel
  }
  return extra
}

const EMPTY_IP_ACTION_STATUSES: Record<string, true> = {
  unknown: true,
  未知: true,
  missing: true,
  not_configured: true,
  未启用: true,
  未配置: true,
  暂未配置: true,
  缺少证据: true,
  暂不可用: true,
  查询不可用: true,
  unavailable: true,
  '—': true,
}

export function overviewUnlinkedAnomalyCopy(anomaly: { title: string; detail?: string }): {
  reason: string
  impact: string | null
} {
  const detail = anomaly.detail?.trim() ?? ''
  if (detail && detail !== anomaly.title) {
    return { reason: detail, impact: null }
  }
  return {
    reason: '当前 VPS 没有关联监控实例。',
    impact: '运行观测缺少心跳、健康与异常证据。',
  }
}

export function overviewIPQualityActionLabel(cell: {
  status: string
  detail?: string
  section: { state: string; observed_at: string | null; last_success_at: string | null }
}): string {
  const status = cell.status.trim()
  const label = overviewIPLabel(status)
  const detail = cell.detail?.trim() ?? ''
  const empty = Boolean(
    !status
    || EMPTY_IP_ACTION_STATUSES[status]
    || EMPTY_IP_ACTION_STATUSES[label]
    || EMPTY_IP_ACTION_STATUSES[detail],
  )
  const unavailable = cell.section.state === 'unavailable'
  const hasHistory = detail === 'ip_quality_disabled_has_history'
    || detail === 'historical_disabled'
    || Boolean(cell.section.observed_at?.trim())
    || Boolean(cell.section.last_success_at?.trim())
  if ((empty || unavailable) && hasHistory) return '查看历史报告'
  if (empty || unavailable) return '查看 IP 质量结果'
  return '查看 IP 质量报告'
}


