import { useId } from 'react'

import { MetricChart, type MetricChartSample, Timestamp } from '../../../../components/atoms'
import { formatDuration, formatMetricAxisValue, formatMetricValue, METRIC_AXIS_GUTTER, metricLabel } from '../evidencePresentation'
import type {
  MonitoringBucketReadModel,
  MonitoringEvidenceReadModel,
  MonitoringMetricReadModel,
} from '../evidenceReadModels'

type Props = {
  model: MonitoringEvidenceReadModel
  title: string
}

type ChartSeries = {
  key: string
  seriesId: string
  metric: string
  unit: string
  samples: MetricChartSample[]
}

type PreviousMetricBucket = {
  bucket: MonitoringBucketReadModel
  ordinal: number
}

function metricValue(metric: MonitoringMetricReadModel): number | null {
  return metric.average ?? metric.max ?? metric.min ?? metric.p95 ?? null
}

function hasGapBefore(
  model: MonitoringEvidenceReadModel,
  seriesId: string,
  previous: PreviousMetricBucket | undefined,
  current: MonitoringBucketReadModel,
  currentOrdinal: number,
): boolean {
  if (!previous) return false
  if (currentOrdinal !== previous.ordinal + 1) return true
  const previousEnd = new Date(previous.bucket.end).getTime()
  const currentStart = new Date(current.start).getTime()
  if (Number.isNaN(previousEnd) || Number.isNaN(currentStart)) return false
  if (currentStart > previousEnd) return true
  return model.gaps.some((gap) => {
    if (gap.series_id !== seriesId) return false
    const gapStart = new Date(gap.start).getTime()
    const gapEnd = new Date(gap.end).getTime()
    return !Number.isNaN(gapStart) && !Number.isNaN(gapEnd) &&
      gapStart < currentStart && gapEnd > previousEnd
  })
}

function monitoringSeries(model: MonitoringEvidenceReadModel): ChartSeries[] {
  const series = new Map<string, ChartSeries>()
  const previousBuckets = new Map<string, PreviousMetricBucket>()
  const bucketOrdinals = new Map<string, number>()
  for (const bucket of model.buckets) {
    const bucketOrdinal = bucketOrdinals.get(bucket.series_id) ?? 0
    bucketOrdinals.set(bucket.series_id, bucketOrdinal + 1)
    for (const metric of bucket.metrics) {
      const value = metricValue(metric)
      if (value === null) continue
      const key = `${bucket.series_id}\u0000${metric.name}`
      const current = series.get(key) ?? {
        key,
        seriesId: bucket.series_id,
        metric: metric.name,
        unit: metric.unit,
        samples: [],
      }
      current.samples.push({
        value,
        observedAt: bucket.end,
        gapBefore: hasGapBefore(model, bucket.series_id, previousBuckets.get(key), bucket, bucketOrdinal),
      })
      series.set(key, current)
      previousBuckets.set(key, { bucket, ordinal: bucketOrdinal })
    }
  }
  return Array.from(series.values())
}

function pad2(value: number): string {
  return String(value).padStart(2, '0')
}

// 覆盖窗口按本地日历日判断：同一天只重复时分，跨日写全两端。
function windowText(start: string, end: string): string {
  const from = new Date(start)
  const to = new Date(end)
  if (Number.isNaN(from.getTime()) || Number.isNaN(to.getTime())) return `${start} – ${end}`
  const full = (date: Date) => `${date.getFullYear()}/${pad2(date.getMonth() + 1)}/${pad2(date.getDate())} ${pad2(date.getHours())}:${pad2(date.getMinutes())}`
  return from.toDateString() === to.toDateString()
    ? `${full(from)} – ${pad2(to.getHours())}:${pad2(to.getMinutes())}`
    : `${full(from)} – ${full(to)}`
}

export function MonitoringEvidenceRenderer({ model, title }: Props) {
  const series = monitoringSeries(model)
  const multipleSeries = new Set(series.map((item) => item.seriesId)).size > 1
  const titleId = useId()
  return (
    <section className="record-section record-evidence__body" aria-labelledby={titleId}>
      <div className="record-section__head">
        <h2 className="record-section__title" id={titleId}>{title}</h2>
        <dl className="record-evidence__chips">
          <div><dt>覆盖</dt><dd className="mono">{windowText(model.coverage_start, model.coverage_end)}</dd></div>
          <div><dt>精度</dt><dd>{formatDuration(model.actual_precision_seconds)}</dd></div>
          <div className={model.quality.gap_count > 0 ? 'record-evidence__chip--notice' : undefined}>
            <dt>缺口</dt><dd className="mono">{model.quality.gap_count}</dd>
          </div>
          <div><dt>峰值</dt><dd className="mono">{model.quality.peak_count}</dd></div>
        </dl>
      </div>
      <div className="record-evidence__charts">
        {series.map((item) => {
          const peak = model.peaks.find((entry) => entry.series_id === item.seriesId && entry.metric === item.metric)
          return (
            <figure key={item.key} className="record-evidence__chart">
              <figcaption className="record-evidence__chart-head">
                <strong>{metricLabel(item.metric)}</strong>
                {multipleSeries ? <span className="mono">{item.seriesId}</span> : null}
                {peak ? (
                  <span className="record-evidence__peak">
                    峰值 <span className="mono">{formatMetricValue(peak.value, item.unit)}</span> · <Timestamp value={peak.at} />
                  </span>
                ) : null}
              </figcaption>
              <MetricChart
                samples={item.samples}
                height={140}
                allowSinglePoint
                ariaLabel={`${multipleSeries ? `${item.seriesId} ` : ''}${metricLabel(item.metric)}趋势`}
                formatValue={(value) => formatMetricValue(value, item.unit)}
                formatAxisValue={(value) => formatMetricAxisValue(value, item.unit)}
                paddingLeft={METRIC_AXIS_GUTTER}
              />
            </figure>
          )
        })}
      </div>
    </section>
  )
}
