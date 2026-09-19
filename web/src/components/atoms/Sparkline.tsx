import { type MouseEvent, useEffect, useRef, useState } from 'react'

export type SparklineTone =
  | 'default'
  | 'normal'
  | 'notice'
  | 'alert'
  | 'critical'
  | 'maintenance'
  | 'offline'
  | 'accent'
  | 'accent-2'

export type SparklineSample = {
  value: number
  observedAt: string
}

export type SparklineDomain = {
  min: number
  max: number
}


export interface SparklineProps {
  /**
   * Plain values. Ignored when `samples` is provided. A `null` keeps its bucket
   * position and breaks the line instead of joining across the gap.
   */
  values?: Array<number | null>
  /** Richer form with timestamps. Enables tooltip time display. Takes precedence over `values`. */
  samples?: SparklineSample[]
  tone?: SparklineTone
  width?: number
  height?: number
  className?: string
  ariaLabel?: string
  /** Enable hover tooltip showing point value (and time when samples are provided). */
  interactive?: boolean
  /** Stretch SVG horizontally to fill its parent. viewBox stays at the numeric width for layout math. */
  expand?: boolean
  /** Custom value formatter for the tooltip body. Defaults to `value.toFixed(2)`. */
  formatValue?: (value: number) => string
  /** Shared Y domain. When omitted, domain is this series min/max. */
  domain?: SparklineDomain
}

const TONE_VAR: Record<SparklineTone, string> = {
  default: 'var(--text-secondary)',
  normal: 'var(--color-state-normal)',
  notice: 'var(--color-state-notice)',
  alert: 'var(--color-state-alert)',
  critical: 'var(--color-state-critical)',
  maintenance: 'var(--color-state-maintenance)',
  offline: 'var(--color-state-offline)',
  accent: 'var(--accent)',
  'accent-2': 'var(--accent-2)',
}

function pad2(n: number): string {
  return n < 10 ? `0${n}` : `${n}`
}

function formatTooltipTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return `${d.getFullYear()}/${pad2(d.getMonth() + 1)}/${pad2(d.getDate())} ${pad2(d.getHours())}:${pad2(d.getMinutes())}`
}

export function Sparkline({
  values,
  samples,
  tone = 'default',
  width = 64,
  height = 16,
  className = '',
  ariaLabel,
  interactive = false,
  expand = false,
  formatValue = (v) => v.toFixed(2),
  domain,
}: SparklineProps) {
  const series: Array<number | null> = samples ? samples.map((s) => s.value) : (values ?? [])
  const finiteValues = series.filter((value): value is number => value != null && !Number.isNaN(value))
  const [hoverIndex, setHoverIndex] = useState<number | null>(null)
  const svgRef = useRef<SVGSVGElement>(null)
  const [measuredWidth, setMeasuredWidth] = useState(0)

  useEffect(() => {
    if (!expand || !svgRef.current) return

    const measureCurrentWidth = () => {
      const nextWidth = svgRef.current?.getBoundingClientRect().width ?? 0
      if (nextWidth > 0) setMeasuredWidth(nextWidth)
    }
    measureCurrentWidth()

    if (typeof globalThis.ResizeObserver === 'undefined') return
    const resizeObserver = new globalThis.ResizeObserver((entries) => {
      const nextWidth = entries[0]?.contentRect.width ?? 0
      if (nextWidth > 0) setMeasuredWidth(nextWidth)
    })
    resizeObserver.observe(svgRef.current)
    return () => resizeObserver.disconnect()
  }, [expand])

  const toneClass = `sparkline--${tone}`
  const chartWidth = expand ? measuredWidth || width : width
  const [firstValue] = finiteValues

  if (firstValue === undefined) {
    return (
      <svg
        ref={svgRef}
        className={['sparkline', toneClass, 'sparkline--empty', className].filter(Boolean).join(' ')}
        width={expand ? '100%' : chartWidth}
        height={height}
        viewBox={`0 0 ${chartWidth} ${height}`}
        aria-label={ariaLabel ?? '暂无趋势数据'}
        role="img"
      >
        <text
          className="sparkline__placeholder"
          x={chartWidth / 2}
          y={height / 2}
          textAnchor="middle"
          dominantBaseline="middle"
        >
          暂无数据
        </text>
      </svg>
    )
  }

  const min = domain?.min ?? Math.min(...finiteValues)
  const max = domain?.max ?? Math.max(...finiteValues)
  const range = max > min ? max - min : 1
  const isSingle = series.length === 1
  // One finite bucket cannot form a line, but it still sits at its own bucket.
  const canDrawLine = finiteValues.length >= 2
  const stepX = isSingle ? 0 : chartWidth / (series.length - 1)

  const projectXY = (i: number, value: number) => {
    const x = i * stepX
    const y = height - ((value - min) / range) * (height - 2) - 1
    return { x, y }
  }

  // A gap starts a new polyline. An isolated bucket repeats its own coordinate so
  // the point stays visible without joining it to its neighbours.
  const lineSegments: string[] = []
  series.forEach((value, i) => {
    if (value == null || Number.isNaN(value)) return
    const p = projectXY(i, value)
    const point = `${p.x.toFixed(2)},${p.y.toFixed(2)}`
    const previous = series[i - 1]
    const startsSegment = i === 0 || previous == null || Number.isNaN(previous)
    if (startsSegment) lineSegments.push(point)
    else lineSegments[lineSegments.length - 1] += ` ${point}`
  })

  const lastFiniteIndex = series.reduce<number>(
    (found, value, i) => (value != null && !Number.isNaN(value) ? i : found),
    -1,
  )
  const last = projectXY(lastFiniteIndex, series[lastFiniteIndex] as number)
  const stroke = TONE_VAR[tone]

  const handleMove = (e: MouseEvent<SVGSVGElement>) => {
    if (!interactive || isSingle) return
    const rect = e.currentTarget.getBoundingClientRect()
    if (rect.width === 0) return
    const relX = e.clientX - rect.left
    const vbX = (relX / rect.width) * chartWidth
    const idx = Math.round(vbX / stepX)
    setHoverIndex(Math.max(0, Math.min(series.length - 1, idx)))
  }

  const handleLeave = () => {
    if (interactive) setHoverIndex(null)
  }

  const tooltipNode = (() => {
    if (hoverIndex == null || !interactive) return null
    const hoveredValue = series[hoverIndex]
    if (hoveredValue == null || Number.isNaN(hoveredValue)) return null
    const p = projectXY(hoverIndex, hoveredValue)
    const tooltipWidth = Math.min(128, chartWidth)
    const tooltipHeight = 36
    const tooltipX = Math.max(0, Math.min(chartWidth - tooltipWidth, p.x - tooltipWidth / 2))
    const time = samples?.[hoverIndex]?.observedAt
    return (
      <foreignObject
        className="sparkline__tooltip-frame"
        x={tooltipX}
        y={-tooltipHeight - 6}
        width={tooltipWidth}
        height={tooltipHeight}
      >
        <div className="sparkline__tooltip">
          <span className="sparkline__tooltip-value">{formatValue(hoveredValue)}</span>
          {time ? <span className="sparkline__tooltip-time">{formatTooltipTime(time)}</span> : null}
        </div>
      </foreignObject>
    )
  })()

  const svg = (
    <svg
      ref={svgRef}
      className={[
        'sparkline',
        toneClass,
        interactive && !isSingle && 'sparkline--interactive',
        className,
      ].filter(Boolean).join(' ')}
      width={expand ? '100%' : chartWidth}
      height={height}
      viewBox={`0 0 ${chartWidth} ${height}`}
      preserveAspectRatio={expand ? 'none' : undefined}
      role="img"
      aria-label={ariaLabel ?? `趋势 ${series.length} 个采样`}
      onMouseMove={interactive ? handleMove : undefined}
      onMouseLeave={interactive ? handleLeave : undefined}
    >
      {!isSingle && canDrawLine && lineSegments.map((points) => (
        <polyline
          key={points}
          fill="none"
          stroke={stroke}
          strokeWidth={1.5}
          strokeLinecap="round"
          strokeLinejoin="round"
          points={points.includes(' ') ? points : `${points} ${points}`}
          vectorEffect={expand ? 'non-scaling-stroke' : undefined}
        />
      ))}
      <circle cx={last.x} cy={last.y} r={1.6} fill={stroke} />
      {hoverIndex != null && (() => {
        const hoveredValue = series[hoverIndex]
        if (hoveredValue == null || Number.isNaN(hoveredValue)) return null
        const p = projectXY(hoverIndex, hoveredValue)
        return (
          <g className="sparkline__cursor">
            <line
              x1={p.x}
              y1={0}
              x2={p.x}
              y2={height}
              stroke="var(--text-muted)"
              strokeWidth={0.6}
              strokeDasharray="2 2"
              vectorEffect={expand ? 'non-scaling-stroke' : undefined}
            />
            <circle
              cx={p.x}
              cy={p.y}
              r={2.4}
              fill={stroke}
              stroke="var(--bg)"
              strokeWidth={0.8}
            />
          </g>
        )
      })()}
      {tooltipNode}
    </svg>
  )

  // Backward-compat: non-interactive multi-sample without expand returns bare SVG
  if (!interactive && !isSingle && !expand) {
    return svg
  }

  return (
    <span
      className={[
        'sparkline-shell',
        interactive && 'sparkline-shell--interactive',
        isSingle && 'sparkline-shell--single',
        expand && 'sparkline-shell--expand',
      ]
        .filter(Boolean)
        .join(' ')}
    >
      {svg}
      {isSingle ? <span className="sparkline__hint">样本不足</span> : null}
    </span>
  )
}
