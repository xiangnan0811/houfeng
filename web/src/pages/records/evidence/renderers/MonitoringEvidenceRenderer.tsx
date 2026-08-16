import { MetricChart, type MetricChartSample, MonoDigits, Timestamp } from '../../../../components/atoms'
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

export function MonitoringEvidenceRenderer({ model, title }: Props) {
  const series = monitoringSeries(model)
  return (
    <section className="page-panel evidence-renderer evidence-renderer--monitoring" aria-label={title}>
      <header className="evidence-renderer__header">
        <h3>{title}</h3>
        <span>{model.quality.status}</span>
      </header>
      <dl className="metadata-list evidence-renderer__facts">
        <div><dt>覆盖开始</dt><dd><Timestamp value={model.coverage_start} /></dd></div>
        <div><dt>覆盖结束</dt><dd><Timestamp value={model.coverage_end} /></dd></div>
        <div><dt>实际精度</dt><dd><MonoDigits>{model.actual_precision_seconds} 秒</MonoDigits></dd></div>
        <div><dt>缺口</dt><dd><MonoDigits>{model.quality.gap_count}</MonoDigits></dd></div>
        <div><dt>峰值</dt><dd><MonoDigits>{model.quality.peak_count}</MonoDigits></dd></div>
      </dl>
      <div className="page-stack evidence-renderer__charts">
        {series.map((item) => (
          <section key={item.key} className="page-panel evidence-renderer__chart">
            <h4>{item.metric}</h4>
            <p><MonoDigits>{item.seriesId}</MonoDigits> · {item.unit}</p>
            <MetricChart
              samples={item.samples}
              ariaLabel={`${item.seriesId} ${item.metric} 趋势`}
              formatValue={(value) => `${value} ${item.unit}`}
            />
          </section>
        ))}
      </div>
      {model.peaks.length > 0 ? (
        <section className="evidence-renderer__peaks" aria-label="峰值">
          <h4>峰值</h4>
          <ul className="evidence-renderer__list">
            {model.peaks.map((peak) => (
              <li key={`${peak.series_id}-${peak.metric}-${peak.at}`}>
                <span>{peak.metric}: {peak.value}</span>
                <Timestamp value={peak.at} />
              </li>
            ))}
          </ul>
        </section>
      ) : null}
    </section>
  )
}
