import { type CSSProperties, type MouseEvent, useState } from 'react'

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

  const toneClass = `sparkline--${tone}`

  if (series.length === 0) {
    return (
      <span
        className={['sparkline', toneClass, 'sparkline--empty', className].filter(Boolean).join(' ')}
        aria-label={ariaLabel ?? '暂无趋势数据'}
        role="img"
        style={{
          width: expand ? '100%' : width,
          height,
          display: 'inline-flex',
          alignItems: 'center',
          justifyContent: 'center',
          verticalAlign: 'middle',
        }}
      >
        <span className="sparkline__placeholder">暂无数据</span>
      </span>
    )
  }

  const min = Math.min(...series)
  const max = Math.max(...series)
  const range = max - min || 1
  const isSingle = series.length === 1
  const stepX = isSingle ? 0 : width / (series.length - 1)

  const projectXY = (i: number) => {
    const x = i * stepX
    const y = height - ((series[i] - min) / range) * (height - 2) - 1
    return { x, y }
  }

  const points = series
    .map((_, i) => {
      const p = projectXY(i)
      return `${p.x.toFixed(2)},${p.y.toFixed(2)}`
    })
    .join(' ')

  const last = projectXY(series.length - 1)
  const stroke = TONE_VAR[tone]

  const handleMove = (e: MouseEvent<SVGSVGElement>) => {
    if (!interactive || isSingle) return
    const rect = e.currentTarget.getBoundingClientRect()
    if (rect.width === 0) return
    const relX = e.clientX - rect.left
    const vbX = (relX / rect.width) * width
    const idx = Math.round(vbX / stepX)
    setHoverIndex(Math.max(0, Math.min(series.length - 1, idx)))
  }

  const handleLeave = () => {
    if (interactive) setHoverIndex(null)
  }

  const svgStyle: CSSProperties = {
    display: 'block',
    cursor: interactive && !isSingle ? 'crosshair' : 'default',
  }

  const svg = (
    <svg
      className={['sparkline', toneClass, className].filter(Boolean).join(' ')}
      width={expand ? '100%' : width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      preserveAspectRatio={expand ? 'none' : undefined}
      role="img"
      aria-label={ariaLabel ?? `趋势 ${series.length} 个采样`}
      style={svgStyle}
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
        const p = projectXY(hoverIndex)
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
    </svg>
  )

  // Backward-compat: non-interactive multi-sample without expand returns bare SVG
  if (!interactive && !isSingle && !expand) {
    return svg
  }

  const containerStyle: CSSProperties = {
    position: 'relative',
    display: expand ? 'block' : 'inline-block',
    width: expand ? '100%' : width,
    height,
    verticalAlign: 'middle',
  }

  const tooltipNode = (() => {
    if (hoverIndex == null || !interactive) return null
    const p = projectXY(hoverIndex)
    const xPercent = (p.x / width) * 100
    const time = samples?.[hoverIndex]?.observedAt
    return (
      <span className="sparkline__tooltip" style={{ left: `${xPercent}%` }}>
        <span className="sparkline__tooltip-value">{formatValue(series[hoverIndex])}</span>
        {time ? <span className="sparkline__tooltip-time">{formatTooltipTime(time)}</span> : null}
      </span>
    )
  })()

  return (
    <span
      className={[
        'sparkline-shell',
        interactive && 'sparkline-shell--interactive',
        isSingle && 'sparkline-shell--single',
      ]
        .filter(Boolean)
        .join(' ')}
      style={containerStyle}
    >
      {svg}
      {isSingle ? <span className="sparkline__hint">样本不足</span> : null}
      {tooltipNode}
    </span>
  )
}
