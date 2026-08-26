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
