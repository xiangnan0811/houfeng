import type {
  ProbeObservationFreshness,
  ProbeObservationFreshnessState,
  TargetObservationFreshness,
  TargetRecord,
} from '../../lib/types'
import type { StatusBadgeTone } from '../StatusBadge'

const LATEST_KNOWN_HEALTH = new Set(['关注', '告警', '严重', '数据不可用'])

export function targetFreshnessLabel(freshness: TargetObservationFreshness): string | null {
  switch (freshness.state) {
    case 'inactive':
      return null
    case 'uncovered':
      return '未配置启用探测项'
    case 'unobserved':
      return '尚无观测'
    case 'pending':
      return '等待新观测'
    case 'fresh':
      return '观测新鲜'
    case 'stale':
      return '观测已过期'
    case 'partial':
      return freshness.stale_probe_count > 0 ? '部分观测过期' : '部分探测项等待观测'
    default:
      return freshness.state
  }
}

export function targetFreshnessTone(freshness: TargetObservationFreshness): StatusBadgeTone {
  switch (freshness.state) {
    case 'fresh':
      return 'green'
    case 'pending':
      return 'yellow'
    case 'partial':
      return freshness.stale_probe_count > 0 ? 'red' : 'yellow'
    case 'stale':
      return 'red'
    case 'uncovered':
      return 'yellow'
    default:
      return 'slate'
  }
}

export function probeFreshnessStateLabel(state: ProbeObservationFreshnessState): string {
  switch (state) {
    case 'fresh':
      return '观测新鲜'
    case 'pending':
      return '等待新观测'
    case 'stale':
      return '观测已过期'
    default:
      return state
  }
}

export function probeFreshnessTone(state: ProbeObservationFreshnessState): StatusBadgeTone {
  switch (state) {
    case 'fresh':
      return 'green'
    case 'pending':
      return 'yellow'
    case 'stale':
      return 'red'
    default:
      return 'slate'
  }
}

export function probeDeadlinePrefix(state: ProbeObservationFreshnessState): string {
  switch (state) {
    case 'fresh':
      return '有效至'
    case 'pending':
      return '等待至'
    case 'stale':
      return '过期于'
    default:
      return '期限'
  }
}

export function freshnessByProbeId(
  probes: ProbeObservationFreshness[],
): Map<string, ProbeObservationFreshness> {
  const map = new Map<string, ProbeObservationFreshness>()
  for (const probe of probes) map.set(probe.probe_item_id, probe)
  return map
}

/** Historical 正常 is not current evidence once the projection leaves fresh. */
export function historicalNormalEvidenceNotice(target: TargetRecord): string | null {
  if (target.lifecycle_status === 'retired') return null
  if (target.current_health_status !== '正常') return null
  const freshness = target.observation_freshness
  if (freshness.state === 'inactive' || freshness.state === 'fresh') return null
  return '最近一次正常，当前证据不足'
}

export function showsLatestKnownHealth(target: TargetRecord): boolean {
  if (target.lifecycle_status === 'retired') return false
  return LATEST_KNOWN_HEALTH.has(target.current_health_status)
}
