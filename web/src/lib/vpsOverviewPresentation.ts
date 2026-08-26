import {
  VPS_LIFECYCLE_STATUS_LABELS,
  VPS_RENEWAL_DECISION_LABELS,
  VPS_USAGE_STATUS_LABELS,
  type VPSLifecycleStatus,
  type VPSRenewalDecision,
  type VPSUsageStatus,
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
