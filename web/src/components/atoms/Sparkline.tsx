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

export interface SparklineProps {
  /** Plain values. Ignored when `samples` is provided. */
  values?: number[]
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
}: SparklineProps) {
  const series: number[] = samples ? samples.map((s) => s.value) : (values ?? [])
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
  const [firstValue, ...remainingValues] = series

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

  const min = Math.min(...series)
  const max = Math.max(...series)
  const range = max - min || 1
  const isSingle = series.length === 1
  const stepX = isSingle ? 0 : chartWidth / (series.length - 1)

  const projectXY = (i: number, value: number) => {
    const x = i * stepX
    const y = height - ((value - min) / range) * (height - 2) - 1
    return { x, y }
  }

  const points = series
    .map((value, i) => {
      const p = projectXY(i, value)
      return `${p.x.toFixed(2)},${p.y.toFixed(2)}`
    })
    .join(' ')

  const last = projectXY(series.length - 1, remainingValues.at(-1) ?? firstValue)
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
    if (hoveredValue === undefined) return null
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
      {!isSingle && (
        <polyline
          fill="none"
          stroke={stroke}
          strokeWidth={1.5}
          strokeLinecap="round"
          strokeLinejoin="round"
          points={points}
          vectorEffect={expand ? 'non-scaling-stroke' : undefined}
        />
      )}
      <circle cx={last.x} cy={last.y} r={1.6} fill={stroke} />
      {hoverIndex != null && (() => {
        const hoveredValue = series[hoverIndex]
        if (hoveredValue === undefined) return null
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
