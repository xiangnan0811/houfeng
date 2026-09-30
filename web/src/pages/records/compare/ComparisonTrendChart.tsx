import type { ComparisonSeries } from '../../../lib/types'
import { seriesForKindAndMetric } from './comparisonQueryState'

type Props = {
  kind?: string
  metric?: string
  series: ComparisonSeries[]
}

const WIDTH = 1000
const HEIGHT = 200

// 最多 6 项比较；按项序固定配色与线型，颜色相近的主题里仍能靠虚实区分。
// 图例与折线共用同一个类。
const SERIES_CLASSES = [
  'record-compare-series--1',
  'record-compare-series--2',
  'record-compare-series--3',
  'record-compare-series--4',
  'record-compare-series--5',
  'record-compare-series--6',
] as const

type PlotPoint = { offset: number; value: number }

function seriesClass(itemIndex: number): string {
  return SERIES_CLASSES[itemIndex % SERIES_CLASSES.length] ?? SERIES_CLASSES[0]
}

function isTimed(item: ComparisonSeries): boolean {
  return item.segments.flat().every((point) => Number.isFinite(Date.parse(point.start)))
}

/**
 * 比较对象通常来自不同时间段，所以横轴是相对各自首个桶的时间偏移，而不是绝对时间。
 * 只要有一条序列的时间戳不可解析，全部序列统一按桶序排列，避免同一横轴混用两种单位。
 */
function plotSegments(item: ComparisonSeries, timed: boolean): PlotPoint[][] {
  const origin = Date.parse(item.segments.flat()[0]?.start ?? '')
  let ordinal = 0
  return item.segments.map((segment) => segment.map((point) => {
    const offset = timed ? (Date.parse(point.start) - origin) / 1000 : ordinal
    ordinal += 1
    return { offset, value: point.value }
  }))
}

function formatValue(value: number): string {
  const digits = Math.abs(value) >= 100 ? 0 : Math.abs(value) >= 10 ? 1 : 2
  return String(Number(value.toFixed(digits)))
}

function formatOffset(seconds: number, timed: boolean): string {
  if (!timed) return `#${Math.round(seconds) + 1}`
  if (seconds <= 0) return '起点'
  if (seconds < 3600) return `+${Math.round(seconds / 60)} 分`
  if (seconds < 86_400) return `+${(seconds / 3600).toFixed(seconds % 3600 === 0 ? 0 : 1)} 时`
  return `+${(seconds / 86_400).toFixed(1)} 天`
}

/** 纵轴刻度：数据范围外扩 8%（常量序列外扩 ±10% 或 ±1），标签、网格与数据共用同一映射。 */
function comparisonValueDomain(values: readonly number[]): { min: number; max: number } {
  const rawMin = values.length > 0 ? Math.min(...values) : 0
  const rawMax = values.length > 0 ? Math.max(...values) : 1
  const spread = rawMax - rawMin
  const pad = spread === 0 ? Math.max(Math.abs(rawMax) * 0.1, 1) : spread * 0.08
  return { min: rawMin - pad, max: rawMax + pad }
}

export function ComparisonTrendChart({ kind, metric, series }: Props) {
  const visible = seriesForKindAndMetric(series, kind, metric)
  if (visible.length === 0) return null

  const timed = visible.every(isTimed)
  const plotted = visible.map((item) => ({ item, segments: plotSegments(item, timed) }))
  const allPoints = plotted.flatMap((entry) => entry.segments.flat())
  const maxOffset = Math.max(0, ...allPoints.map((point) => point.offset))
  const { min, max } = comparisonValueDomain(allPoints.map((point) => point.value))
  const mid = (min + max) / 2
  const x = (offset: number) => (maxOffset === 0 ? WIDTH / 2 : (offset / maxOffset) * WIDTH)
  const y = (value: number) => (1 - (value - min) / (max - min)) * HEIGHT
  const metricLabel = visible[0]?.metric_id ?? metric ?? ''
  const unit = visible[0]?.unit ?? ''
  const summary = plotted
    .map(({ item, segments }) => `第 ${item.item_index + 1} 项 ${segments.length} 段`)
    .join('，')

  return (
    <div className="record-compare__block">
      <div className="record-compare__block-head">
        <h3 className="record-compare__block-title" id="comparison-trend-heading">趋势</h3>
        <ul className="record-compare-legend" aria-label="图例">
          {plotted.map(({ item }) => (
            <li key={`${item.item_index}-${item.metric_id}`} className={seriesClass(item.item_index)}>
              <svg className="record-compare-legend__swatch" viewBox="0 0 24 8" aria-hidden="true" focusable="false">
                <line className="record-compare-chart__line" x1={0} x2={24} y1={4} y2={4} />
                <polyline className="record-compare-chart__dot" points="12,4 12,4" />
              </svg>
              第 {item.item_index + 1} 项
            </li>
          ))}
        </ul>
      </div>
      <figure className="record-compare-chart">
        <figcaption className="record-compare-chart__caption">
          {metricLabel}{unit ? `（${unit}）` : ''}
        </figcaption>
        <div className="record-compare-chart__plot">
          <div className="record-compare-chart__y" aria-hidden="true">
            <span>{formatValue(max)}</span>
            <span>{formatValue(mid)}</span>
            <span>{formatValue(min)}</span>
          </div>
          <svg
            className="record-compare-chart__svg"
            viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
            preserveAspectRatio="none"
            role="img"
            aria-label={`${metricLabel} 趋势：${summary}`}
          >
            {[max, mid, min].map((value) => y(value)).map((lineY) => (
              <line key={lineY} className="record-compare-chart__grid" x1={0} x2={WIDTH} y1={lineY} y2={lineY} vectorEffect="non-scaling-stroke" />
            ))}
            {plotted.map(({ item, segments }) => (
              <g key={`${item.item_index}-${item.metric_id}`} className={seriesClass(item.item_index)}>
                {segments.map((segment, segmentIndex) => {
                  if (segment.length === 0) return null
                  // 单桶段画成标记点：零长度折线配合按项区分的圆 / 方线帽，不随横向拉伸变形。
                  const coords = segment.map((point) => `${x(point.offset)},${y(point.value)}`)
                  const points = segment.length === 1 ? `${coords[0]} ${coords[0]}` : coords.join(' ')
                  return (
                    <polyline
                      key={segmentIndex}
                      data-segment={segmentIndex}
                      className={segment.length === 1 ? 'record-compare-chart__dot' : 'record-compare-chart__line'}
                      points={points}
                      vectorEffect="non-scaling-stroke"
                    />
                  )
                })}
              </g>
            ))}
          </svg>
        </div>
        <div className="record-compare-chart__x" aria-hidden="true">
          <span>{formatOffset(0, timed)}</span>
          <span>{formatOffset(maxOffset / 2, timed)}</span>
          <span>{formatOffset(maxOffset, timed)}</span>
        </div>
      </figure>
    </div>
  )
}
