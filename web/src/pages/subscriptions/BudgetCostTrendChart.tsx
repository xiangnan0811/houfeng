import {
  type KeyboardEvent,
  type PointerEvent,
  useEffect,
  useRef,
  useState,
} from 'react'
import type { SubscriptionSeriesPoint } from '../../lib/types'
import {
  buildContiguousPath,
  buildDifferenceAreas,
  buildTrendPoints,
  computeAdaptiveYDomain,
  computeXTickIndices,
  findNearestMonthIndex,
  formatMonthLabel,
  formatTickValue,
  getReadoutFacts,
  type ChartDimensions,
} from './budgetCostTrend'
export type BudgetCostTrendChartProps = {
  buckets: SubscriptionSeriesPoint[]
  baseCurrency: string
}



export function BudgetCostTrendChart({
  buckets,
  baseCurrency,
}: BudgetCostTrendChartProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const [measuredWidth, setMeasuredWidth] = useState<number>(0)
  const [measuredHeight, setMeasuredHeight] = useState<number>(0)
  const [activeIndex, setActiveIndex] = useState<number>(() => Math.max(0, buckets.length - 1))

  useEffect(() => {
    if (!containerRef.current) return

    const applySize = (nextWidth: number, nextHeight: number) => {
      const width = Math.round(nextWidth)
      const height = Math.round(nextHeight)
      if (width > 0) {
        setMeasuredWidth((current) => (current === width ? current : width))
      }
      if (height > 0) {
        setMeasuredHeight((current) => (current === height ? current : height))
      }
    }

    const measure = () => {
      if (!containerRef.current) return
      const rect = containerRef.current.getBoundingClientRect()
      applySize(rect.width, rect.height)
    }

    measure()

    if (typeof globalThis.ResizeObserver === 'undefined') return

    const resizeObserver = new globalThis.ResizeObserver((entries) => {
      const entry = entries[0]
      applySize(entry?.contentRect.width ?? 0, entry?.contentRect.height ?? 0)
    })
    resizeObserver.observe(containerRef.current)
    return () => resizeObserver.disconnect()
  }, [])

  const safeIndex = buckets.length > 0 ? Math.min(Math.max(0, activeIndex), buckets.length - 1) : 0

  const width = Math.max(1, measuredWidth || 760)
  const height = Math.max(160, measuredHeight || 200)
  const domain = computeAdaptiveYDomain(buckets)
  const { yMin, yMax, ticks } = domain
  const domainSpan = Math.max(1e-9, yMax - yMin)

  // Adapt left padding to tick label lengths so long tick labels never clip
  const maxTickLabelLen = Math.max(3, ...ticks.map((t) => formatTickValue(t).length))
  const padLeft = Math.max(48, Math.ceil(maxTickLabelLen * 7.5) + 14)

  const dims: ChartDimensions = {
    width,
    height,
    pad: {
      left: padLeft,
      right: 20,
      top: 16,
      bottom: 24,
    },
  }
  const { pad } = dims
  const chartWidth = Math.max(1, width - pad.left - pad.right)
  const chartHeight = Math.max(1, height - pad.top - pad.bottom)

  const yFor = (value: number) =>
    pad.top + chartHeight - ((value - yMin) / domainSpan) * chartHeight

  const points = buildTrendPoints(buckets, dims, yMin, yMax)
  const costPath = buildContiguousPath(points.map((p) => ({ x: p.x, y: p.yCost })))
  const budgetPath = buildContiguousPath(points.map((p) => ({ x: p.x, y: p.yBudget })))
  const areas = buildDifferenceAreas(points)

  const activePoint = points[safeIndex] ?? points[points.length - 1]
  const activeBucket = buckets[safeIndex] ?? buckets[buckets.length - 1]
  const activeFacts = activeBucket
    ? getReadoutFacts(activeBucket, baseCurrency)
    : null

  const handlePointer = (event: PointerEvent<SVGSVGElement>) => {
    const svg = event.currentTarget
    const ctm = svg.getScreenCTM()
    let svgX: number
    if (ctm) {
      const inverse = ctm.inverse()
      svgX = event.clientX * inverse.a + event.clientY * inverse.c + inverse.e
    } else {
      const rect = svg.getBoundingClientRect()
      if (rect.width <= 0) return
      svgX = ((event.clientX - rect.left) / rect.width) * width
    }
    const nearest = findNearestMonthIndex(points, svgX)
    if (nearest >= 0 && nearest !== safeIndex) {
      setActiveIndex(nearest)
    }
  }

  const handleKeyDown = (event: KeyboardEvent<SVGSVGElement>) => {
    if (event.key === 'ArrowLeft') {
      event.preventDefault()
      setActiveIndex(Math.max(0, safeIndex - 1))
    } else if (event.key === 'ArrowRight') {
      event.preventDefault()
      setActiveIndex(Math.min(Math.max(0, buckets.length - 1), safeIndex + 1))
    } else if (event.key === 'Home') {
      event.preventDefault()
      setActiveIndex(0)
    } else if (event.key === 'End') {
      event.preventDefault()
      setActiveIndex(Math.max(0, buckets.length - 1))
    }
  }

  return (
    <div className="subscription-trend-chart-container">
      <div ref={containerRef} className="subscription-trend-chart-plot">
      <svg
        className="subscription-trend-chart"
        viewBox={`0 0 ${width} ${height}`}
        role="region"
        tabIndex={0}
        aria-label={`月度成本与预算趋势图表，使用左右箭头键切换月份。当前选中：${activeFacts?.bucket ?? ''}，月成本 ${activeFacts?.costText ?? ''}`}
        onKeyDown={handleKeyDown}
        onPointerDown={handlePointer}
        onPointerMove={(event) => {
          if (event.buttons > 0 || event.pointerType === 'mouse') {
            handlePointer(event)
          }
        }}
        style={{ touchAction: 'pan-y' }}
      >
        {/* Y-axis grid and tick labels */}
        {ticks.map((tick) => {
          const y = yFor(tick)
          return (
            <g key={tick}>
              <line
                className="subscription-trend-chart__grid"
                x1={pad.left}
                y1={y}
                x2={width - pad.right}
                y2={y}
              />
              <text
                className="subscription-trend-chart__axis"
                x={pad.left - 8}
                y={y + 3}
                textAnchor="end"
              >
                {formatTickValue(tick)}
              </text>
            </g>
          )
        })}

        {/* Difference areas (red over, green under) with exact linear crossing splits */}
        {areas.map((area, index) => (
          <polygon
            key={`${area.tone}-${index}`}
            className={`subscription-trend-chart__area subscription-trend-chart__area--${area.tone}`}
            points={area.points}
          />
        ))}

        {/* Linear cost line (prominent solid) */}
        {costPath ? (
          <path
            className="subscription-trend-chart__line subscription-trend-chart__line--cost"
            d={costPath}
          />
        ) : null}

        {/* Linear budget line (subordinate dashed, contiguous segments only, no bridging gaps) */}
        {budgetPath ? (
          <path
            className="subscription-trend-chart__line subscription-trend-chart__line--budget"
            d={budgetPath}
          />
        ) : null}

        {/* Active month vertical hairline and points only (no persistent all-month dots) */}
        {activePoint ? (
          <g className="subscription-trend-chart__active-marks">
            <line
              className="subscription-trend-chart__hairline"
              x1={activePoint.x}
              y1={pad.top}
              x2={activePoint.x}
              y2={pad.top + chartHeight}
            />
            {activePoint.yCost != null ? (
              <circle
                className="subscription-trend-chart__point subscription-trend-chart__point--cost"
                cx={activePoint.x}
                cy={activePoint.yCost}
                r="4"
              />
            ) : null}
            {activePoint.yBudget != null ? (
              <circle
                className="subscription-trend-chart__point subscription-trend-chart__point--budget"
                cx={activePoint.x}
                cy={activePoint.yBudget}
                r="4"
              />
            ) : null}
          </g>
        ) : null}

        {/* X-axis month labels */}
        {(() => {
          const tickIndices = new Set(computeXTickIndices(points.length, chartWidth))
          return points.map((point, index) => {
            if (!tickIndices.has(index)) return null
            return (
              <text
                key={point.bucket}
                className="subscription-trend-chart__axis"
                x={point.x}
                y={height - 6}
                textAnchor="middle"
              >
                {formatMonthLabel(point.bucket)}
              </text>
            )
          })
        })()}
        {/* Hit columns for pointer interactions across the entire chart height */}
        {points.map((point, index) => {
          const prevX = index === 0 ? pad.left : (points[index - 1]!.x + point.x) / 2
          const nextX =
            index === points.length - 1
              ? width - pad.right
              : (point.x + points[index + 1]!.x) / 2
          const colWidth = Math.max(1, nextX - prevX)

          return (
            <rect
              key={`hit-${point.bucket}`}
              className="subscription-trend-chart__hit-column"
              x={prevX}
              y={pad.top}
              width={colWidth}
              height={chartHeight}
              fill="transparent"
              pointerEvents="all"
              onPointerEnter={() => setActiveIndex(index)}
              onClick={() => setActiveIndex(index)}
            />
          )
        })}
      </svg>
      </div>

      {/* In-flow stable readout row */}
      {activeFacts ? (
        <div className="subscription-trend-readout" role="status" aria-live="polite">
          <div className="subscription-trend-readout__month">
            <strong>{activeFacts.bucket}</strong>
          </div>
          <div className="subscription-trend-readout__item">
            <span className="subscription-trend-readout__label">月成本</span>
            <span className="subscription-trend-readout__value mono">{activeFacts.costText}</span>
          </div>
          <div className="subscription-trend-readout__item">
            <span className="subscription-trend-readout__label">月预算</span>
            <span className="subscription-trend-readout__value mono">{activeFacts.budgetText}</span>
          </div>
          <div className="subscription-trend-readout__item">
            <span className="subscription-trend-readout__label">差额</span>
            {activeFacts.hasBudget && activeFacts.deltaText ? (
              <span
                className={`subscription-trend-readout__value subscription-trend-readout__delta subscription-trend-readout__delta--${activeFacts.deltaTone} mono`}
              >
                {activeFacts.deltaText}
                {activeFacts.percentText ? ` (${activeFacts.percentText})` : ''}
                <small className="subscription-trend-readout__status">{activeFacts.statusText}</small>
              </span>
            ) : (
              <span className="subscription-trend-readout__value mono">—</span>
            )}
          </div>
          <span className="subscription-trend-readout__hint" aria-hidden="true">
            使用 ← → 切换月份
          </span>
        </div>
      ) : null}

      {/* Legend */}
      <div className="subscription-trend-legend" aria-label="趋势图图例">
        <span>
          <i className="subscription-trend-legend__line subscription-trend-legend__line--cost" />
          月成本
        </span>
        <span>
          <i className="subscription-trend-legend__line subscription-trend-legend__line--budget" />
          月预算
        </span>
        <span>
          <i className="subscription-trend-legend__area subscription-trend-legend__area--over" />
          超预算区间
        </span>
        <span>
          <i className="subscription-trend-legend__area subscription-trend-legend__area--under" />
          低于预算区间
        </span>
      </div>
    </div>
  )
}
