/**
 * Shared host-metric series helpers.
 *
 * Extracted from `MonitoringInstanceWatchtowerMetrics` so the monitoring detail
 * observation charts can reuse them instead of copying. This module holds no
 * layout, no JSX and no time-window controls.
 */
import { formatBytes } from '../../lib/format'
import type { HostMetricPoint } from '../../lib/types'
import type { MetricThreshold } from '../../config/thresholds'
import type { MetricChartSample } from '../atoms/MetricChart'
import type { TimeWindow } from '../../pages/monitoring-detail/types'

export type MetricTimeWindow = TimeWindow

export type HostMetricSeriesPoint = Pick<
  HostMetricPoint,
  | 'observed_at'
  | 'cpu_usage_pct'
  | 'mem_used_pct'
  | 'disk_used_pct'
  | 'inode_used_pct'
  | 'load_5'
  | 'cpu_iowait_pct'
  | 'net_in_bytes_per_sec'
  | 'net_out_bytes_per_sec'
  | 'load_1'
  | 'load_15'
  | 'swap_used_pct'
  | 'disk_busy_pct'
  | 'disk_read_bytes_per_sec'
  | 'disk_write_bytes_per_sec'
>

export function toAscending(samples: HostMetricSeriesPoint[]): HostMetricSeriesPoint[] {
  return [...samples].sort(
    (a, b) => new Date(a.observed_at).getTime() - new Date(b.observed_at).getTime(),
  )
}

export function isPresentValue(value: number | null | undefined): value is number {
  return value != null && Number.isFinite(value)
}

export function toSeries(
  samples: HostMetricSeriesPoint[],
  pick: (s: HostMetricSeriesPoint) => number | null,
): MetricChartSample[] {
  return samples.map((s, index) => {
    const value = pick(s)
    const previous = index > 0 ? pick(samples[index - 1]!) : value
    return {
      value,
      observedAt: s.observed_at,
      ...(index > 0 && (value == null || previous == null) ? { gapBefore: true } : {}),
    }
  })
}

export function seriesMax(series: MetricChartSample[]): number | undefined {
  let max = Number.NEGATIVE_INFINITY
  for (const point of series) {
    if (isPresentValue(point.value)) max = Math.max(max, point.value)
  }
  return Number.isFinite(max) ? max : undefined
}

export function seriesValueAt(series: MetricChartSample[], observedAt: string | null): number | null {
  if (!observedAt) {
    const last = series.at(-1)
    return last && isPresentValue(last.value) ? last.value : null
  }
  const target = new Date(observedAt).getTime()
  if (Number.isNaN(target) || series.length === 0) return null
  let nearest = series[0]!
  let nearestDiff = Number.POSITIVE_INFINITY
  for (const point of series) {
    const ms = new Date(point.observedAt).getTime()
    if (Number.isNaN(ms)) continue
    const diff = Math.abs(ms - target)
    if (diff < nearestDiff) {
      nearestDiff = diff
      nearest = point
    }
  }
  return isPresentValue(nearest.value) ? nearest.value : null
}

export function formatNetworkAxis(value: number): string {
  const abs = Math.abs(value)
  if (abs >= 1073741824) return `${(value / 1073741824).toFixed(1)} GB/s`
  // Use MB before 4-digit KB labels ("1010 KB/s") that shove the plot gutter.
  if (abs >= 1024 * 1000) return `${(value / 1048576).toFixed(1)} MB/s`
  if (abs >= 1048576) return `${(value / 1048576).toFixed(1)} MB/s`
  if (abs >= 1024) return `${(value / 1024).toFixed(0)} KB/s`
  return `${Math.round(value)} B/s`
}

export function thresholdLines(thresholds: MetricThreshold, suffix = '') {
  return [
    { value: thresholds.warning, tone: 'notice' as const, label: `${thresholds.warning}${suffix}` },
    { value: thresholds.alert, tone: 'alert' as const, label: `${thresholds.alert}${suffix}` },
    { value: thresholds.critical, tone: 'critical' as const, label: `${thresholds.critical}${suffix}` },
  ]
}

export function thresholdTitle(metric: string, thresholds: MetricThreshold | undefined, suffix: string, extra: string) {
  if (!thresholds) return `${metric}。阈值策略不可用。${extra}`
  return `${metric}。正常：< ${thresholds.warning}${suffix}，关注：≥ ${thresholds.warning}${suffix}，告警：≥ ${thresholds.alert}${suffix}，严重：≥ ${thresholds.critical}${suffix}。${extra}`
}

/** Three resolved levels as one sentence, e.g. `正常 < 80%，关注 ≥ 80%，告警 ≥ 90%，严重 ≥ 95%`. */
export function thresholdLevelsText(thresholds: MetricThreshold, suffix = ''): string {
  return `正常 < ${thresholds.warning}${suffix}，关注 ≥ ${thresholds.warning}${suffix}，告警 ≥ ${thresholds.alert}${suffix}，严重 ≥ ${thresholds.critical}${suffix}`
}

export function formatCapacityBytes(value?: number | null): string {
  if (value == null || !Number.isFinite(value) || value <= 0) return '—'
  return formatBytes(value)
}

export function timeWindowLabel(timeWindow: MetricTimeWindow): string {
  if (timeWindow === 'realtime') return '实时'
  if (timeWindow === '24h') return '近 24h'
  if (timeWindow === '7d') return '近 7d'
  return '近 30d'
}