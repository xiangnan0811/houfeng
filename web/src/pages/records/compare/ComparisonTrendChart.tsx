import type { ComparisonSeries } from '../../../lib/types'
import { seriesForKindAndMetric } from './comparisonQueryState'

type Props = {
  kind?: string
  metric?: string
  series: ComparisonSeries[]
}

export function ComparisonTrendChart({ kind, metric, series }: Props) {
  const visible = seriesForKindAndMetric(series, kind, metric)
  if (visible.length === 0) return null
  return (
    <section aria-labelledby="comparison-trend-heading">
      <div className="section-heading">
        <h2 className="section-heading__title" id="comparison-trend-heading">趋势</h2>
        <p className="section-heading__description">一次只画当前类型与指标；每个缺口一条折线，不跨缺口连线。</p>
      </div>
      <div>
        {visible.map((item) => (
          <figure key={`${item.item_index}-${item.metric_id}`}>
            <figcaption>第 {item.item_index + 1} 项 · {item.metric_id}{item.unit ? `（${item.unit}）` : ''}</figcaption>
            <svg
              className="metric-chart"
              viewBox="0 0 320 120"
              role="img"
              aria-label={`第 ${item.item_index + 1} 项 ${item.metric_id}，${item.segments.length} 段`}
            >
              {item.segments.map((segment, segmentIndex) => {
                if (segment.length === 0) return null
                const values = segment.map((point) => point.value)
                const min = Math.min(...values)
                const max = Math.max(...values)
                const span = max - min || 1
                const points = segment.map((point, index) => {
                  const x = segment.length === 1 ? 160 : (index / (segment.length - 1)) * 300 + 10
                  const y = 110 - ((point.value - min) / span) * 100
                  return `${x},${y}`
                }).join(' ')
                return (
                  <polyline
                    key={segmentIndex}
                    points={points}
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2"
                  />
                )
              })}
            </svg>
          </figure>
        ))}
      </div>
    </section>
  )
}
