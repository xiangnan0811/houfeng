import { MonoDigits, Sparkline, type SparklineTone } from '../../components/atoms'
import { type MetricThreshold, type MetricThresholds } from '../../config/thresholds'
import { formatPercent } from '../../lib/format'
import type { MonitoringInstanceRecord, MonitoringInstanceSparklinesResponse } from '../../lib/types'

type MonitoringInstancesTrendCellProps = {
  monitoringInstance: MonitoringInstanceRecord
  sparklines: MonitoringInstanceSparklinesResponse | null
  thresholds: MetricThresholds | null
}

type TrendSegment = {
  start: number
  values: number[]
}

const METRICS = [
  { key: 'cpu_usage_pct', label: 'CPU' },
  { key: 'mem_used_pct', label: '内存' },
  { key: 'disk_used_pct', label: '磁盘' },
] as const

function lastNonNull(values: Array<number | null | undefined> | undefined): number | null {
  if (!values) return null
  for (let index = values.length - 1; index >= 0; index -= 1) {
    const value = values[index]
    if (value != null && !Number.isNaN(value)) return value
  }
  return null
}

function trendSegments(values: Array<number | null | undefined>): TrendSegment[] {
  const segments: TrendSegment[] = []
  let current: TrendSegment | null = null
  values.forEach((value, index) => {
    if (value == null || Number.isNaN(value)) {
      current = null
      return
    }
    if (!current) {
      current = { start: index, values: [value] }
      segments.push(current)
      return
    }
    current.values.push(value)
  })
  return segments
}

function seriesDomain(values: Array<number | null | undefined>): { min: number; max: number } | undefined {
  let min = Number.POSITIVE_INFINITY
  let max = Number.NEGATIVE_INFINITY
  for (const value of values) {
    if (value == null || Number.isNaN(value)) continue
    if (value < min) min = value
    if (value > max) max = value
  }
  if (!Number.isFinite(min) || !Number.isFinite(max)) return undefined
  return { min, max }
}


function toneFromThreshold(value: number | null, threshold: MetricThreshold | undefined): SparklineTone {
  if (value == null || !threshold) return 'default'
  if (value >= threshold.critical) return 'critical'
  if (value >= threshold.alert) return 'alert'
  if (value >= threshold.warning) return 'notice'
  return 'accent'
}

function thresholdForKey(thresholds: MetricThresholds | null, key: (typeof METRICS)[number]['key']): MetricThreshold | undefined {
  if (!thresholds) return undefined
  if (key === 'cpu_usage_pct') return thresholds.cpu
  if (key === 'mem_used_pct') return thresholds.mem
  return thresholds.disk
}

export function MonitoringInstancesTrendCell({
  monitoringInstance,
  sparklines,
  thresholds,
}: MonitoringInstancesTrendCellProps) {
  const series = sparklines?.monitoring_instances?.[monitoringInstance.monitoring_instance_id]
  if (!series) {
    return <span className="monitoring-table__trends-empty">—</span>
  }

  return (
    <span className="monitoring-table__trend-strip" aria-label="24小时历史趋势，降采样桶值，不含精确采样时间">
      {METRICS.map((metric) => {
        const values = series[metric.key] ?? []
        const latest = lastNonNull(values)
        const threshold = thresholdForKey(thresholds, metric.key)
        const tone = toneFromThreshold(latest, threshold)
        const segments = trendSegments(values)
        const bucketCount = Math.max(values.length, 1)
        const domain = seriesDomain(values)
        return (
          <span
            key={metric.key}
            className="monitoring-table__trend-item"
            aria-label={`${metric.label} 24小时历史 ${formatPercent(latest)}`}
          >
            <span className="monitoring-table__trend-label">{metric.label}</span>
            <span className="monitoring-table__trend-value">
              {latest != null ? <MonoDigits>{formatPercent(latest)}</MonoDigits> : '—'}
            </span>
            {segments.length === 0 ? (
              <span className="monitoring-table__trends-empty">—</span>
            ) : (
              <span className="monitoring-table__trend-track">
                {segments.map((segment) => (
                  <span
                    key={`${metric.key}-${segment.start}`}
                    className="monitoring-table__trend-segment"
                    style={{
                      left: `${(segment.start / bucketCount) * 100}%`,
                      width: `${(segment.values.length / bucketCount) * 100}%`,
                    }}
                  >
                    <Sparkline values={segment.values} tone={tone} width={120} height={16} expand {...(domain ? { domain } : {})} />
                  </span>
                ))}
              </span>
            )}
          </span>
        )
      })}
    </span>
  )
}
