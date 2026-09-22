import type { HostMetricPoint, HostSample, MonitoringInstanceRuntimeFacts } from '../../lib/types'
import type { TimeWindow } from './types'

export const DEFAULT_TIME_WINDOW: TimeWindow = '24h'
export const TIME_WINDOW_VALUES: readonly TimeWindow[] = ['realtime', '24h', '7d', '30d']

export function parseTimeWindow(value: string | null | undefined): TimeWindow {
  if (value === 'realtime' || value === '24h' || value === '7d' || value === '30d') return value
  return DEFAULT_TIME_WINDOW
}

export function parseReadAt(value: string | null | undefined): Date | null {
  if (!value) return null
  const parsed = new Date(value)
  return Number.isNaN(parsed.getTime()) ? null : parsed
}

export function parseSampleTimestamp(value: string | null | undefined): number | null {
  if (!value) return null
  const parsed = Date.parse(value)
  return Number.isNaN(parsed) ? null : parsed
}

export function hostSampleHasValidTimes(sample: HostSample): boolean {
  return parseSampleTimestamp(sample.observed_at) != null && parseSampleTimestamp(sample.received_at) != null
}

export function hostNetworkRate(sample: Pick<HostSample, 'network_rates_valid' | 'net_in_bytes_per_sec' | 'net_out_bytes_per_sec'>, direction: 'in' | 'out'): number | null {
  if (sample.network_rates_valid !== true) return null
  const value = direction === 'in' ? sample.net_in_bytes_per_sec : sample.net_out_bytes_per_sec
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

/**
 * Newer sample is greater. Order: observed desc, non-backfilled first, received desc, sync_batch_id desc.
 * No public observation id or fingerprint is used.
 */
export function compareHostSampleRecency(left: HostSample, right: HostSample): number {
  const leftObserved = parseSampleTimestamp(left.observed_at)
  const rightObserved = parseSampleTimestamp(right.observed_at)
  if (leftObserved == null || rightObserved == null) return 0
  if (leftObserved !== rightObserved) return leftObserved - rightObserved
  if (left.is_backfilled !== right.is_backfilled) return left.is_backfilled ? -1 : 1
  const leftReceived = parseSampleTimestamp(left.received_at)
  const rightReceived = parseSampleTimestamp(right.received_at)
  if (leftReceived == null || rightReceived == null) return 0
  if (leftReceived !== rightReceived) return leftReceived - rightReceived
  return left.sync_batch_id.localeCompare(right.sync_batch_id)
}

export function isEligibleHostSample(
  sample: HostSample | null | undefined,
  monitoringInstanceId: string,
  options: { allowBackfilled: boolean },
): sample is HostSample {
  if (!sample) return false
  if (sample.monitoring_instance_id !== monitoringInstanceId) return false
  if (!hostSampleHasValidTimes(sample)) return false
  if (!options.allowBackfilled && sample.is_backfilled) return false
  return true
}

export function mergeLatestHostSample(
  current: HostSample | null,
  incoming: HostSample | null | undefined,
  monitoringInstanceId: string,
  options: { allowBackfilled: boolean },
): HostSample | null {
  if (!isEligibleHostSample(incoming, monitoringInstanceId, options)) return current
  if (!current) return incoming
  if (!isEligibleHostSample(current, monitoringInstanceId, { allowBackfilled: true })) return incoming
  return compareHostSampleRecency(incoming, current) > 0 ? incoming : current
}

/**
 * Current-binding HTTP snapshot is authoritative, including latest_host_sample: null after rebind/reset.
 * A live sample received after this snapshot's read_at survives a late older HTTP body.
 */
export function applyHttpLatestSample(
  current: HostSample | null,
  incoming: HostSample | null | undefined,
  monitoringInstanceId: string,
  snapshotReadAt: string | null | undefined,
): HostSample | null {
  if (incoming != null && !isEligibleHostSample(incoming, monitoringInstanceId, { allowBackfilled: true })) {
    return current
  }
  if (current && isEligibleHostSample(current, monitoringInstanceId, { allowBackfilled: true })) {
    const readAtMs = parseSampleTimestamp(snapshotReadAt)
    const liveMs = parseSampleTimestamp(current.received_at) ?? parseSampleTimestamp(current.observed_at)
    if (readAtMs != null && liveMs != null && liveMs > readAtMs) return current
  }
  return incoming ?? null
}

export function runtimeFactsMatchWindow(facts: MonitoringInstanceRuntimeFacts, timeWindow: TimeWindow): boolean {
  if (facts.monitoring_instance_id == null) return false
  if (!facts.window) return true
  return facts.window.key === timeWindow
}

export function hostSampleToMetricPoint(sample: HostSample): HostMetricPoint {
  return {
    observed_at: sample.observed_at,
    sample_count: 1,
    cpu_usage_pct: sample.cpu_usage_pct,
    mem_used_pct: sample.mem_used_pct,
    disk_used_pct: sample.disk_used_pct,
    inode_used_pct: sample.inode_used_pct,
    load_5: sample.load_5,
    cpu_iowait_pct: sample.cpu_iowait_pct,
    net_in_bytes_per_sec: hostNetworkRate(sample, 'in'),
    net_out_bytes_per_sec: hostNetworkRate(sample, 'out'),
    load_1: sample.load_1,
    load_15: sample.load_15,
    swap_used_pct: sample.swap_used_pct,
    disk_busy_pct: sample.disk_busy_pct,
    disk_read_bytes_per_sec: sample.disk_read_bytes_per_sec,
    disk_write_bytes_per_sec: sample.disk_write_bytes_per_sec,
  }
}
