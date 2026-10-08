import {
  VPS_LIFECYCLE_STATUS_LABELS,
  VPS_RENEWAL_DECISION_LABELS,
  type VPSOverview,
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

const UNKNOWN_STATUS_LABEL = '状态未知'

function mappedLabel(map: object, value: string): string | undefined {
  if (!Object.hasOwn(map, value)) return undefined
  const label = Object.getOwnPropertyDescriptor(map, value)?.value
  return typeof label === 'string' && label ? label : undefined
}

/** Known map entries stay visible. Anything else, including Chinese prose or a URL, is not a label. */
function mappedOrUnknown(map: object, value: string): string {
  const trimmed = value.trim()
  if (!trimmed) return ''
  return mappedLabel(map, trimmed) ?? UNKNOWN_STATUS_LABEL
}

export function overviewLifecycleLabel(value: string): string {
  return mappedOrUnknown(VPS_LIFECYCLE_STATUS_LABELS, value)
}

export function overviewUsageLabel(value: string): string {
  return value
}

export function overviewRenewalLabel(value: string): string {
  return mappedOrUnknown(VPS_RENEWAL_DECISION_LABELS, value)
}

export function overviewOverallLabel(value: string): string {
  return mappedOrUnknown(OVERALL_STATUS_LABELS, value)
}

export function overviewMonitoringLabel(value: string): string {
  return mappedOrUnknown(MONITORING_STATUS_LABELS, value)
}

export function overviewIPLabel(value: string): string {
  return mappedOrUnknown(IP_STATUS_LABELS, value)
}

export function overviewUnmatchedStatus(
  key: 'overall' | 'monitoring' | 'ip_quality' | 'renewal',
  value: string,
): string | null {
  const trimmed = value.trim()
  if (!trimmed) return null
  const map = key === 'overall'
    ? OVERALL_STATUS_LABELS
    : key === 'monitoring'
      ? MONITORING_STATUS_LABELS
      : key === 'ip_quality'
        ? IP_STATUS_LABELS
        : VPS_RENEWAL_DECISION_LABELS
  return mappedLabel(map, trimmed) ? null : trimmed
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
  待接入: true,
  已退役: true,
  维护中: true,
  暂停: true,
  未启用: true,
  unavailable: true,
  暂不可用: true,
  pending: true,
  maintenance: true,
  paused: true,
  retired: true,
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
  if (summary.overall.section.state === 'unavailable') gaps.push('总体观测暂不可用')
  else if (summary.overall.section.state === 'stale') gaps.push('总体观测数据陈旧')
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
  if (lifecycleStatus === 'archived') {
    return { label: '已归档', tone: 'unknown', explanation: '当前运行观测已结束；历史事实保留。', rawLabel }
  }
  const gaps = runningEvidenceGaps(summary)
  const adverse = ADVERSE_OVERALL[summary.overall.status]
  if (adverse) {
    const parts: string[] = []
    if (gaps.length > 0) parts.push(`${gaps.join('，')}。`)
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
  return mappedOrUnknown(RELATION_STATUS_LABELS, value)
}

export type DiagnosticNote = { label: string; detail: string }

function knownLabel(map: object, value: string): string | undefined {
  return mappedLabel(map, value)
}

/** Primary copy is a known label only. Any other text, including Chinese or a URL, stays diagnostic. */
function knownOrDiagnostic(
  map: object,
  value: string,
  diagnosticLabel: string,
): { summary: string; diagnostics: DiagnosticNote[] } {
  const label = knownLabel(map, value)
  if (label) return { summary: label, diagnostics: [] }
  return { summary: '', diagnostics: [{ label: diagnosticLabel, detail: value }] }
}

export function overviewAnomalySourceLabel(value: string): string {
  return mappedLabel(ANOMALY_SOURCE_LABELS, value) ?? ''
}

export function overviewAnomalySourcePresentation(value: string): { summary: string | null; diagnostics: DiagnosticNote[] } {
  const trimmed = value.trim()
  if (!trimmed) return { summary: null, diagnostics: [] }
  const known = mappedLabel(ANOMALY_SOURCE_LABELS, trimmed)
  if (known) return { summary: known, diagnostics: [] }
  return { summary: null, diagnostics: [{ label: '来源', detail: trimmed }] }
}

export function overviewImportanceLabel(value: string): string {
  return mappedOrUnknown(IMPORTANCE_LABELS, value)
}

export function overviewLocationLabel(parts: Array<string | undefined>): string {
  return parts.map((part) => part?.trim()).filter(Boolean).join(' · ')
}

const SUMMARY_DETAIL_FALLBACKS: Record<string, string> = {
  historical_disabled: '存在历史报告（当前未启用）',
  ip_quality_disabled_has_history: '存在历史报告（当前未启用）',
}

export function overviewSummaryDetailPresentation(
  key: 'overall' | 'monitoring' | 'ip_quality' | 'renewal',
  value: string,
): { summary: string; diagnostics: DiagnosticNote[] } {
  const trimmed = value.trim()
  if (!trimmed) return { summary: '', diagnostics: [] }
  const fallback = SUMMARY_DETAIL_FALLBACKS[trimmed]
  if (fallback) return { summary: fallback, diagnostics: [] }
  if (key === 'monitoring') return knownOrDiagnostic(MONITORING_STATUS_LABELS, trimmed, '监控详情')
  if (key === 'ip_quality') return knownOrDiagnostic(IP_STATUS_LABELS, trimmed, '原始详情')
  if (key === 'renewal') return knownOrDiagnostic(VPS_RENEWAL_DECISION_LABELS, trimmed, '原始详情')
  if (key === 'overall') return knownOrDiagnostic(OVERALL_STATUS_LABELS, trimmed, '原始详情')
  return knownOrDiagnostic({}, trimmed, '原始详情')
}

export function overviewSummaryDetailLabel(
  key: 'overall' | 'monitoring' | 'ip_quality' | 'renewal',
  value: string,
): string {
  return overviewSummaryDetailPresentation(key, value).summary
}

export function overviewAnomalyDetailPresentation(ruleId: string, value: string): { summary: string | null; diagnostics: DiagnosticNote[] } {
  const trimmed = value.trim()
  if (!trimmed) return { summary: null, diagnostics: [] }
  if (ruleId === 'source.unavailable.v1') {
    const labels: string[] = []
    const diagnostics: DiagnosticNote[] = []
    for (const part of trimmed.split(',').map((item) => item.trim()).filter(Boolean)) {
      const presented = overviewAnomalySourcePresentation(part)
      if (presented.summary) labels.push(presented.summary)
      diagnostics.push(...presented.diagnostics)
    }
    return { summary: labels.length > 0 ? labels.join('、') : null, diagnostics }
  }
  const map = ruleId.startsWith('ip_quality.')
    ? IP_STATUS_LABELS
    : ruleId.startsWith('lifecycle.')
      ? VPS_LIFECYCLE_STATUS_LABELS
      : ruleId.startsWith('renewal.')
        ? VPS_RENEWAL_DECISION_LABELS
        : ruleId.startsWith('monitoring.')
          ? MONITORING_STATUS_LABELS
          : null
  if (!map) return { summary: null, diagnostics: [{ label: '原始详情', detail: trimmed }] }
  const presented = knownOrDiagnostic(map, trimmed, '原始详情')
  return { summary: presented.summary || null, diagnostics: presented.diagnostics }
}

export function overviewAnomalyDetailLabel(ruleId: string, value: string): string {
  return overviewAnomalyDetailPresentation(ruleId, value).summary ?? ''
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

export function overviewMonitoringSupportingPresentation(
  detail: string,
  statusLabel: string,
  relationCount?: number,
): { text: string; diagnostics: DiagnosticNote[] } {
  const trimmed = detail.trim()
  const generated = GENERATED_MONITORING_COUNT.exec(trimmed)
  const extraSource = generated ? (generated[2] ?? '').trim() : trimmed
  const knownExtra = knownLabel(MONITORING_STATUS_LABELS, extraSource)
  const duplicatesStatus = !extraSource || extraSource === statusLabel || knownExtra === statusLabel
  const folded = !extraSource || duplicatesStatus
    ? { summary: '', diagnostics: [] as DiagnosticNote[] }
    : knownExtra
      ? { summary: knownExtra, diagnostics: [] as DiagnosticNote[] }
      : { summary: '', diagnostics: [{ label: '监控详情', detail: extraSource }] }

  const count = relationCount != null && relationCount > 0
    ? relationCount
    : generated
      ? Number(generated[1])
      : undefined
  const countLabel = count != null && count > 0 ? overviewMonitoringInstanceCountLabel(count) : ''
  const visibleExtra = folded.summary
  const text = countLabel
    ? (visibleExtra && visibleExtra !== countLabel ? `${countLabel} · ${visibleExtra}` : countLabel)
    : visibleExtra
  return { text, diagnostics: folded.diagnostics }
}

export function overviewMonitoringSupportingDetail(
  detail: string,
  statusLabel: string,
  relationCount?: number,
): string {
  return overviewMonitoringSupportingPresentation(detail, statusLabel, relationCount).text
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
  diagnostics: DiagnosticNote[]
} {
  const detail = anomaly.detail?.trim() ?? ''
  return {
    reason: '当前 VPS 没有关联监控实例。',
    impact: '运行观测缺少心跳、健康与异常证据。',
    diagnostics: detail && detail !== anomaly.title ? [{ label: '原始详情', detail }] : [],
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
