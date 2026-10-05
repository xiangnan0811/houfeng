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
  sampleCount: number
  referenceCount: number
  sourceLabel: string
  bucketMixed: boolean
  metricGapCount: number
}

type MetricGapFigure = {
  key: string
  seriesId: string
  metric: string
  count: number
}

type PreviousMetricBucket = {
  bucket: MonitoringBucketReadModel
  ordinal: number
}

function metricValue(metric: MonitoringMetricReadModel): number | null {
  return metric.average ?? metric.max ?? metric.min ?? metric.p95 ?? null
}

function monitoringSourceLabel(layer: string | undefined): string {
  if (layer === 'raw') return '原始'
  if (layer === 'daily_aggregate') return '日聚合'
  if (layer === 'mixed') return '混合来源'
  return layer ?? ''
}

function gapAppliesToMetric(gapMetric: string | undefined, metric: string): boolean {
  return gapMetric == null || gapMetric === metric
}

function hasGapBefore(
  model: MonitoringEvidenceReadModel,
  seriesId: string,
  metric: string,
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
    if (gap.series_id !== seriesId || !gapAppliesToMetric(gap.metric, metric)) return false
    const gapStart = new Date(gap.start).getTime()
    const gapEnd = new Date(gap.end).getTime()
    return !Number.isNaN(gapStart) && !Number.isNaN(gapEnd) &&
      gapStart < currentStart && gapEnd > previousEnd
  })
}

function metricGapCount(model: MonitoringEvidenceReadModel, seriesId: string, metric: string): number {
  return model.gaps.filter((gap) => gap.series_id === seriesId && gap.metric === metric).length
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
      const source = monitoringSourceLabel(metric.source_layer ?? bucket.source_layer)
      const current = series.get(key) ?? {
        key,
        seriesId: bucket.series_id,
        metric: metric.name,
        unit: metric.unit,
        samples: [],
        sampleCount: 0,
        referenceCount: 0,
        sourceLabel: source,
        bucketMixed: false,
        metricGapCount: metricGapCount(model, bucket.series_id, metric.name),
      }
      current.sampleCount += metric.sample_count ?? bucket.sample_count
      current.referenceCount += bucket.sample_count
      if (source && current.sourceLabel !== source) current.sourceLabel = '混合来源'
      if (bucket.source_layer === 'mixed') current.bucketMixed = true
      current.samples.push({
        value,
        observedAt: bucket.end,
        gapBefore: hasGapBefore(model, bucket.series_id, metric.name, previousBuckets.get(key), bucket, bucketOrdinal),
      })
      series.set(key, current)
      previousBuckets.set(key, { bucket, ordinal: bucketOrdinal })
    }
  }
  return Array.from(series.values())
}

function uncoveredMetricGaps(model: MonitoringEvidenceReadModel, series: readonly ChartSeries[]): MetricGapFigure[] {
  const covered = new Set(series.map((item) => item.key))
  const figures = new Map<string, MetricGapFigure>()
  for (const gap of model.gaps) {
    if (!gap.metric) continue
    const key = `${gap.series_id}\u0000${gap.metric}`
    if (covered.has(key)) continue
    const current = figures.get(key) ?? { key, seriesId: gap.series_id, metric: gap.metric, count: 0 }
    current.count += 1
    figures.set(key, current)
  }
  return Array.from(figures.values())
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

function seriesCaption(item: ChartSeries, model: MonitoringEvidenceReadModel, multipleSeries: boolean) {
  const reference = model.calculation_version === 'monitoring-evidence/v2'
  const peak = model.peaks.find((entry) => entry.series_id === item.seriesId && entry.metric === item.metric)
  return (
    <figcaption className="record-evidence__chart-head">
      <strong>{metricLabel(item.metric)}</strong>
      {multipleSeries ? <span className="mono">{item.seriesId}</span> : null}
      <span className="mono">有效样本 {item.sampleCount}{item.sourceLabel ? ` · ${item.sourceLabel}` : ''}</span>
      {item.bucketMixed && item.sourceLabel !== '混合来源' ? <span>混合来源</span> : null}
      {reference ? <span className="mono">覆盖参考样本 {item.referenceCount}</span> : null}
      {item.metricGapCount > 0 ? <span>指标缺口 {item.metricGapCount}</span> : null}
      {peak ? (
        <span className="record-evidence__peak">
          峰值 <span className="mono">{formatMetricValue(peak.value, item.unit)}</span> · <Timestamp value={peak.at} />
        </span>
      ) : null}
    </figcaption>
  )
}

export function MonitoringEvidenceRenderer({ model, title }: Props) {
  const series = monitoringSeries(model)
  const missingMetrics = uncoveredMetricGaps(model, series)
  const multipleSeries = new Set([...series.map((item) => item.seriesId), ...missingMetrics.map((item) => item.seriesId)]).size > 1
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
        {series.map((item) => (
            <figure key={item.key} className="record-evidence__chart">
              {seriesCaption(item, model, multipleSeries)}
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
        ))}
        {missingMetrics.map((item) => (
          <figure key={item.key} className="record-evidence__chart">
            <figcaption className="record-evidence__chart-head">
              <strong>{metricLabel(item.metric)}</strong>
              {multipleSeries ? <span className="mono">{item.seriesId}</span> : null}
              <span>指标缺口 {item.count}</span>
            </figcaption>
          </figure>
        ))}
      </div>
    </section>
  )
}
