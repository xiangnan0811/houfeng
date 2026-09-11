import type { ReactNode } from 'react'

import type { VPSOverview } from '../../lib/types'
import {
  overviewMonitoringSupportingDetail,
  overviewOverallPresentation,
  overviewSummaryCellLabel,
  overviewSummaryDetailLabel,
  type OverviewObservationTone,
} from '../../lib/vpsOverviewPresentation'
import { VPSObservationRows, type VPSObservationRowModel } from './VPSObservationRows'
import { overviewSourceHasRetainedEvidence, overviewSourceIsKnownReadFailure, overviewSourceReadKind } from './vpsOverviewFreshness'

type SummaryKey = keyof VPSOverview['summary']

type Props = {
  summary: VPSOverview['summary']
  onRefresh: () => void
  retrying: boolean
  keys?: SummaryKey[]
  heading?: string
  variant?: 'stack' | 'metrics'
  after?: Partial<Record<SummaryKey, ReactNode>>
  monitoringInstanceCount?: number
  lifecycleStatus?: string
}

const CELLS: Array<{ key: SummaryKey; label: string; retryable: boolean }> = [
  { key: 'overall', label: '总体', retryable: false },
  { key: 'monitoring', label: '监控关联', retryable: true },
  { key: 'ip_quality', label: 'IP 质量', retryable: true },
  { key: 'renewal', label: '续费', retryable: true },
]

const UNKNOWN_STATUSES: Record<string, true> = {
  unlinked: true,
  unknown: true,
  未知: true,
  未关联: true,
  missing: true,
  not_configured: true,
  failure: true,
  缺少证据: true,
  未启用: true,
  采集失败: true,
  unavailable: true,
  暂不可用: true,
}

const EMPTY_IP_LABELS: Record<string, true> = {
  未知: true,
  未启用: true,
  未配置: true,
  暂未配置: true,
  unknown: true,
  not_configured: true,
  missing: true,
  缺少证据: true,
  暂不可用: true,
  查询不可用: true,
  '—': true,
}

const EMPTY_CONFIG_LABELS: Record<string, true> = {
  未启用: true,
  未配置: true,
  暂未配置: true,
  缺少证据: true,
  not_configured: true,
  missing: true,
}


function observationTone(key: SummaryKey, status: string): OverviewObservationTone | '' {
  const normalized = status.trim()
  if (!normalized) return 'unknown'
  if (UNKNOWN_STATUSES[normalized]) return 'unknown'

  if (key === 'monitoring') {
    if (normalized === '严重' || normalized === '告警') return 'alert'
    if (normalized === '关注') return 'notice'
    if (normalized === '正常') return 'ok'
    return 'unknown'
  }

  if (key === 'ip_quality') {
    if (normalized === 'high' || normalized === 'critical' || normalized === '高风险' || normalized === '严重风险') {
      return 'alert'
    }
    if (
      normalized === 'medium'
      || normalized === 'moderate'
      || normalized === 'partial'
      || normalized === '中风险'
      || normalized === '采集不完整'
    ) {
      return 'notice'
    }
    if (normalized === 'low' || normalized === 'success' || normalized === '低风险' || normalized === '采集成功') {
      return 'ok'
    }
    return 'unknown'
  }

  if (key === 'overall') {
    if (normalized === 'healthy' || normalized === '总体正常') return 'ok'
    if (normalized === 'critical' || normalized === '严重') return 'alert'
    if (normalized === 'attention' || normalized === 'notice' || normalized === '需要关注' || normalized === '留意') {
      return 'notice'
    }
    return 'unknown'
  }

  return ''
}

function duplicateEmptyIP(statusLabel: string, detailLabel: string): boolean {
  return Boolean(EMPTY_IP_LABELS[statusLabel] && EMPTY_IP_LABELS[detailLabel])
}
export function VPSOverviewSummaryGrid({
  summary,
  onRefresh,
  retrying,
  keys,
  heading = '决策摘要',
  after,
  monitoringInstanceCount,
  lifecycleStatus,
}: Props) {
  const overall = overviewOverallPresentation(summary, lifecycleStatus)
  const cells = keys ? CELLS.filter((cell) => keys.includes(cell.key)) : CELLS
  const rows: VPSObservationRowModel[] = cells.map(({ key, label, retryable }) => {
    const cell = summary[key]
    const extra = after?.[key]
    const presented = key === 'overall'
      ? { label: overall.label, tone: overall.tone, explanation: overall.explanation }
      : {
          label: overviewSummaryCellLabel(key, cell.status) || '—',
          tone: observationTone(key, cell.status) || undefined,
          explanation: null as string | null,
        }
    const mappedDetail = cell.detail ? overviewSummaryDetailLabel(key, cell.detail) : ''
    let statusLabel = presented.label
    let statusTone = presented.tone
    let detailText = presented.explanation || mappedDetail
    const sourceLabel = label === '监控关联' ? '监控' : label
    let hideDetail = !presented.explanation
      && (
        !detailText
        || detailText === statusLabel
        || (key === 'ip_quality' && duplicateEmptyIP(statusLabel, detailText))
      )
    if (key === 'monitoring') {
      const supporting = overviewMonitoringSupportingDetail(
        mappedDetail,
        statusLabel,
        monitoringInstanceCount,
      )
      detailText = supporting
      hideDetail = !supporting || supporting === statusLabel
    }

    let retained = false
    let compactEmpty = false
    const knownFailure = overviewSourceIsKnownReadFailure(cell.section)
    if (key === 'ip_quality') {
      const emptyStatus = Boolean(
        EMPTY_IP_LABELS[statusLabel]
        || EMPTY_IP_LABELS[cell.status]
        || EMPTY_IP_LABELS[mappedDetail],
      )
      const emptyConfig = Boolean(
        EMPTY_CONFIG_LABELS[statusLabel]
        || EMPTY_CONFIG_LABELS[cell.status]
        || EMPTY_CONFIG_LABELS[mappedDetail]
        || EMPTY_CONFIG_LABELS[cell.detail ?? ''],
      )
      const retainedConclusion = overviewSourceHasRetainedEvidence(statusLabel)
        && !EMPTY_CONFIG_LABELS[statusLabel]
        && !EMPTY_IP_LABELS[statusLabel]
      const hasHistory = overviewSourceReadKind(cell.section) === 'disabled_history'
        || Boolean(cell.section.observed_at?.trim())
        || Boolean(cell.section.last_success_at?.trim())
        || retainedConclusion
      if (cell.section.state === 'unavailable') {
        if (emptyConfig && !knownFailure && !hasHistory) {
          if (EMPTY_CONFIG_LABELS[mappedDetail]) statusLabel = mappedDetail
          else if (EMPTY_CONFIG_LABELS[cell.detail ?? '']) statusLabel = overviewSummaryDetailLabel('ip_quality', cell.detail ?? '')
          statusTone = 'unknown'
          detailText = ''
          hideDetail = true
          compactEmpty = true
        } else if (hasHistory) {
          retained = true
          if (emptyConfig) hideDetail = true
        } else if (!emptyStatus && overviewSourceHasRetainedEvidence(statusLabel)) {
          retained = true
          detailText = ''
          hideDetail = true
        } else if (knownFailure) {
          statusLabel = '暂不可用'
          statusTone = 'unknown'
          detailText = ''
          hideDetail = true
        }
      } else if (cell.section.state === 'stale' && emptyStatus) {
        statusLabel = ''
        detailText = ''
        hideDetail = true
      } else if (emptyStatus && mappedDetail && (mappedDetail === statusLabel || duplicateEmptyIP(statusLabel, mappedDetail))) {
        hideDetail = true
      }
    } else if (cell.section.state === 'unavailable' && overviewSourceHasRetainedEvidence(statusLabel) && !UNKNOWN_STATUSES[statusLabel]) {
      retained = true
    }

    const allowRetry = retryable && (cell.section.state === 'stale' || knownFailure)
    return {
      key,
      project: label,
      ariaLabel: label,
      conclusion: statusLabel,
      conclusionTone: statusTone || '',
      description: hideDetail ? '' : detailText,
      ...(compactEmpty ? {} : { section: cell.section }),
      sourceLabel,
      retained,
      action: extra,
      ...(allowRetry ? {
        onRetry: onRefresh,
        retrying,
        retryScope: '概览',
        retryMode: cell.section.state === 'stale' ? 'refresh' as const : 'retry' as const,
      } : {}),
    }
  })

  return (
    <section
      className="vps-overview-summary vps-overview-summary--metrics"
      aria-label={heading || '决策摘要'}
    >
      {heading ? <h2 className={heading === '决策摘要' ? 'visually-hidden' : undefined}>{heading}</h2> : null}
      <VPSObservationRows rows={rows} labelledBy="vps-section-monitoring-title" ariaLabel="运行观测" />
    </section>
  )
}
