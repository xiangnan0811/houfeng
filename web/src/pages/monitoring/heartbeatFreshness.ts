import type { IncidentDefaults } from '../../lib/types'
import type { HeartbeatFreshness, HeartbeatFreshnessPolicy } from './types'

function isPositiveInteger(value: unknown): value is number {
  return typeof value === 'number' && Number.isInteger(value) && value > 0
}

/** Global incident_defaults only. No invented fallbacks, no label overrides. */
export function readHeartbeatFreshnessPolicy(
  incidentDefaults: IncidentDefaults | null | undefined,
): HeartbeatFreshnessPolicy | null {
  if (!incidentDefaults) return null
  if (!isPositiveInteger(incidentDefaults.heartbeat_interval_seconds)) return null
  if (!isPositiveInteger(incidentDefaults.stale_threshold_intervals)) return null
  return {
    heartbeatIntervalMs: incidentDefaults.heartbeat_interval_seconds * 1000,
    missingThreshold: incidentDefaults.stale_threshold_intervals,
  }
}

/**
 * Frontend equivalent of incidents.HeartbeatIsStale: missed intervals >= threshold.
 * Invalid or future observations are not stale; callers surface invalid timestamps.
 */
export function heartbeatIsStale(
  now: Date,
  lastHeartbeatAt: Date,
  policy: HeartbeatFreshnessPolicy,
): boolean {
  return heartbeatMissedIntervals(now, lastHeartbeatAt, policy.heartbeatIntervalMs) >= policy.missingThreshold
}

export function heartbeatMissedIntervals(now: Date, lastHeartbeatAt: Date, heartbeatIntervalMs: number): number {
  if (heartbeatIntervalMs <= 0 || now.getTime() <= lastHeartbeatAt.getTime()) return 0
  const missed = Math.floor((now.getTime() - lastHeartbeatAt.getTime()) / heartbeatIntervalMs)
  if (missed <= 0) return 0
  return missed
}

export function parseHeartbeatTimestamp(value: string | undefined): Date | null {
  if (!value) return null
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? null : parsed
}

export function classifyHeartbeatFreshness(
  lastHeartbeatAt: string | undefined,
  policy: HeartbeatFreshnessPolicy | null,
  now: Date,
  settingsResolved = true,
): HeartbeatFreshness {
  if (!lastHeartbeatAt) return { kind: 'missing' }
  const parsed = parseHeartbeatTimestamp(lastHeartbeatAt)
  if (!parsed) return { kind: 'invalid', raw: lastHeartbeatAt }
  if (!settingsResolved) return { kind: 'pending', at: lastHeartbeatAt }
  if (!policy) return { kind: 'policy-unavailable', at: lastHeartbeatAt }
  if (heartbeatIsStale(now, parsed, policy)) return { kind: 'stale', at: lastHeartbeatAt }
  return { kind: 'fresh', at: lastHeartbeatAt }
}

export function heartbeatFreshnessLabel(freshness: HeartbeatFreshness): string {
  switch (freshness.kind) {
    case 'missing':
      return '未收到心跳'
    case 'invalid':
      return '心跳时间无效'
    case 'pending':
      return ''
    case 'policy-unavailable':
      return '新鲜度策略不可用'
    case 'fresh':
      return ''
    case 'stale':
      return '数据陈旧'
  }
}
