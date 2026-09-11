import type { VPSOverviewSectionState } from '../../lib/types'

export const OVERVIEW_SOURCE_STATE_LABELS = {
  stale: '数据陈旧',
  unavailable: '暂不可用',
} as const

const TIMEOUT_CODES: Record<string, true> = {
  monitoring_timeout: true,
  ip_quality_timeout: true,
  subscription_timeout: true,
  relation_timeout: true,
  activity_timeout: true,
}

const UNAVAILABLE_CODES: Record<string, true> = {
  monitoring_unavailable: true,
  ip_quality_unavailable: true,
  subscription_unavailable: true,
  relation_unavailable: true,
  activity_unavailable: true,
}

export type OverviewSourceReadKind =
  | 'ready'
  | 'stale'
  | 'timeout'
  | 'unavailable'
  | 'disabled_history'
  | 'invalid_time'

export function overviewSourceReadKind(section: VPSOverviewSectionState): OverviewSourceReadKind {
  if (section.reason_code === 'ip_quality_disabled_has_history') return 'disabled_history'
  if (TIMEOUT_CODES[section.reason_code]) return 'timeout'
  if (UNAVAILABLE_CODES[section.reason_code] || section.reason_code === 'activity_projection_unavailable') {
    return 'unavailable'
  }
  if (section.reason_code === 'source_timestamp_invalid' || section.reason_code === 'ip_quality_stale') {
    return section.reason_code === 'source_timestamp_invalid' ? 'invalid_time' : 'stale'
  }
  if (section.state === 'stale') return 'stale'
  return 'ready'
}

export function overviewSourceIsKnownReadFailure(section: VPSOverviewSectionState): boolean {
  const kind = overviewSourceReadKind(section)
  return kind === 'timeout' || kind === 'unavailable'
}

export function overviewSourceStateLabel(section: VPSOverviewSectionState): string | null {
  return section.state === 'stale' || section.state === 'unavailable'
    ? OVERVIEW_SOURCE_STATE_LABELS[section.state]
    : null
}

export function overviewSourceTimes(section: VPSOverviewSectionState): {
  observedAt: string | null
  lastSuccessAt: string | null
  sameSuccess: boolean
  displayAt: string | null
} {
  const observedAt = section.observed_at
  const lastSuccessAt = section.last_success_at
  const sameSuccess = Boolean(observedAt && lastSuccessAt && observedAt === lastSuccessAt)
  return {
    observedAt,
    lastSuccessAt,
    sameSuccess,
    displayAt: observedAt || lastSuccessAt,
  }
}

export function overviewSourceHasRetainedEvidence(value: string | null | undefined): boolean {
  const trimmed = value?.trim() ?? ''
  if (!trimmed || trimmed === '—') return false
  return true
}

export function overviewSourceReason(
  section: VPSOverviewSectionState,
  sourceLabel: string,
  options?: { retained?: boolean },
): string | null {
  const retained = options?.retained === true
  const kind = overviewSourceReadKind(section)

  if (kind === 'disabled_history') return '存在历史报告（当前未启用）。'

  if (retained && (kind === 'timeout' || kind === 'unavailable')) {
    const outcome = kind === 'timeout' ? `${sourceLabel}读取超时。` : `${sourceLabel}当前读取失败。`
    return `仍展示上次可用结果。${outcome}`
  }
  switch (section.reason_code) {
    case 'ip_quality_stale':
      return `${sourceLabel}数据超过新鲜度阈值。`
    case 'source_timestamp_invalid':
      return `${sourceLabel}来源时间异常，已标记为陈旧。`
    case 'monitoring_timeout':
    case 'ip_quality_timeout':
    case 'subscription_timeout':
    case 'relation_timeout':
    case 'activity_timeout':
      return `${sourceLabel}读取超时，请重试。`
    case 'monitoring_unavailable':
    case 'ip_quality_unavailable':
    case 'subscription_unavailable':
    case 'relation_unavailable':
    case 'activity_unavailable':
      return `${sourceLabel}数据暂不可用，请稍后重试。`
    case 'activity_projection_unavailable':
      return '活动投影暂不可用，请稍后重试。'
    case '':
      if (overviewSourceIsKnownReadFailure(section)) {
        return section.reason_code.includes('timeout')
          ? `${sourceLabel}读取超时，请重试。`
          : `${sourceLabel}数据暂不可用，请稍后重试。`
      }
      return null
    default:
      // Ready sections may carry a non-judging note code. Never treat that as
      // source failure, and never echo an unknown reason_code into the DOM.
      // Unknown stale/unavailable is not a failed read.
      if (overviewSourceIsKnownReadFailure(section)) {
        return `${sourceLabel}数据暂不可用，请稍后重试。`
      }
      if (retained) return '仍展示上次可用结果。'
      return null
  }
}
