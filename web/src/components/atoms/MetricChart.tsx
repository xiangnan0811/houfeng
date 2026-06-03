import { type CSSProperties, type MouseEvent, useState, useEffect, useRef } from 'react'
export type MetricChartTone =
  | 'normal'
  | 'notice'
  | 'alert'
  | 'critical'
  | 'maintenance'
  | 'accent'
  | 'accent-2'

export type MetricChartSample = {
  value: number
  observedAt: string
}

export type MetricChartThreshold = {
  value: number
  tone: 'notice' | 'alert' | 'critical'
  label?: string
}

export type MetricChartMaintenanceWindow = {
  startedAt: string
  endedAt: string
}

export interface MetricChartProps {
  samples: MetricChartSample[]
  width?: number
  height?: number
  tone?: MetricChartTone
  thresholds?: MetricChartThreshold[]
  maintenanceWindows?: MetricChartMaintenanceWindow[]
  ariaLabel?: string
  /** Y-axis tick + tooltip value formatter. Defaults to `v.toFixed(1)`. */
  formatValue?: (value: number) => string
  /** Y-axis tick label formatter. If omitted, uses an integer compact format. */
  formatAxisValue?: (value: number) => string
  /** X-axis tick label formatter. Defaults to `HH:mm` for high-frequency samples. */
  formatTime?: (observedAt: string) => string
  /** Tooltip time label formatter. Defaults to a full local timestamp. */
  formatTooltipTime?: (observedAt: string) => string
  /** Lock Y-axis lower bound. When omitted, derived from data. */
  yMin?: number
  /** Lock Y-axis upper bound. When omitted, derived from data. */
  yMax?: number
  className?: string
  hoveredAt?: string | null
  onHoverAtChange?: (observedAt: string | null) => void
  showTooltip?: boolean
}
const TONE_VAR: Record<MetricChartTone, string> = {
  normal: 'var(--color-state-normal)',
  notice: 'var(--color-state-notice)',
  alert: 'var(--color-state-alert)',
  critical: 'var(--color-state-critical)',
  maintenance: 'var(--color-state-maintenance)',
  accent: 'var(--accent)',
  'accent-2': 'var(--accent-2)',
}

const THRESHOLD_VAR: Record<MetricChartThreshold['tone'], string> = {
  notice: 'var(--color-state-notice)',
  alert: 'var(--color-state-alert)',
  critical: 'var(--color-state-critical)',
}

const PADDING = { top: 8, right: 8, bottom: 24, left: 32 }

function pad2(n: number): string {
  return n < 10 ? `0${n}` : `${n}`
}

function formatTooltipTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return `${d.getFullYear()}/${pad2(d.getMonth() + 1)}/${pad2(d.getDate())} ${pad2(d.getHours())}:${pad2(d.getMinutes())}:${pad2(d.getSeconds())}`
}

function formatAxisTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return `${pad2(d.getHours())}:${pad2(d.getMinutes())}`
}

/**
 * Compute "nice" Y-axis tick values between min/max.
 * Returns approximately `targetCount` ticks; always includes domain endpoints
 * (caller may render them as-is or snap to round numbers later).
 */
function computeYTicks(min: number, max: number, targetCount = 4): number[] {
  if (max <= min) {
    return [min]
  }
  const ticks: number[] = []
  const step = (max - min) / (targetCount - 1)
  for (let i = 0; i < targetCount; i += 1) {
    ticks.push(min + step * i)
  }
  return ticks
}

/**
 * Pick `targetCount` evenly-spaced sample indices for X-axis tick labels.
 * Always includes 0 and last index.
 */
function computeXTickIndices(sampleCount: number, targetCount = 5): number[] {
  if (sampleCount <= 1) return [0]
  if (sampleCount <= targetCount) {
    return Array.from({ length: sampleCount }, (_, i) => i)
  }
  const indices: number[] = []
  for (let i = 0; i < targetCount; i += 1) {
    const ratio = i / (targetCount - 1)
    indices.push(Math.round(ratio * (sampleCount - 1)))
  }
  return indices
}

function indexForObservedAt(samples: MetricChartSample[], observedAt: string | null | undefined): number | null {
  if (!observedAt) return null
  const target = new Date(observedAt).getTime()
  if (Number.isNaN(target)) return null
  let nearestIndex = 0
  let nearestDiff = Number.POSITIVE_INFINITY
  samples.forEach((sample, index) => {
    const ms = new Date(sample.observedAt).getTime()
    if (Number.isNaN(ms)) return
    const diff = Math.abs(ms - target)
    if (diff < nearestDiff) {
      nearestDiff = diff
      nearestIndex = index
    }
  })
  return Number.isFinite(nearestDiff) ? nearestIndex : null
}

export function MetricChart({
  samples,
  width: propsWidth,
  height = 160,
  tone = 'accent',
  thresholds,
  maintenanceWindows,
  ariaLabel,
  formatValue = (v) => v.toFixed(1),
  formatAxisValue = (v) => {
    const abs = Math.abs(v)
    if (abs >= 1000000000) return (v / 1000000000).toFixed(1) + 'B'
    if (abs >= 1000000) return (v / 1000000).toFixed(1) + 'M'
    if (abs >= 1000) return (v / 1000).toFixed(1) + 'k'
    return v.toFixed(0)
  },
  formatTime = formatAxisTime,
  formatTooltipTime: formatTooltipLabel = formatTooltipTime,
  yMin,
  yMax,
  className = '',
  hoveredAt,
  onHoverAtChange,
  showTooltip = true,
}: MetricChartProps) {
  const [hoverIndex, setHoverIndex] = useState<number | null>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const [measuredWidth, setMeasuredWidth] = useState<number>(0)

  useEffect(() => {
    if (propsWidth || !containerRef.current) return

    const measureCurrentWidth = () => {
      if (!containerRef.current) return
      const nextWidth = containerRef.current.getBoundingClientRect().width
      if (nextWidth > 0) {
        setMeasuredWidth(nextWidth)
      }
    }

    measureCurrentWidth()

    if (typeof globalThis.ResizeObserver === 'undefined') return

    const resizeObserver = new globalThis.ResizeObserver((entries) => {
      const nextWidth = entries[0]?.contentRect.width ?? 0
      if (nextWidth > 0) {
        setMeasuredWidth(nextWidth)
      }
    })
    resizeObserver.observe(containerRef.current)
    return () => resizeObserver.disconnect()
  }, [propsWidth])

  const stroke = TONE_VAR[tone]
  const width = propsWidth || measuredWidth || 360

  // Empty state
  if (samples.length === 0) {
    return (
      <span
        ref={containerRef}
        className={['metric-chart', 'metric-chart--empty', className].filter(Boolean).join(' ')}
        role="img"
        aria-label={ariaLabel ?? '暂无观测数据'}
        style={{ width: propsWidth ? width : '100%', height }}
      >
        <span className="metric-chart__placeholder">暂无观测数据</span>
      </span>
    )
  }

  const innerW = width - PADDING.left - PADDING.right
  const innerH = height - PADDING.top - PADDING.bottom
  const isSingle = samples.length === 1

  // Derive Y range
  const dataMin = Math.min(...samples.map((s) => s.value))
  const dataMax = Math.max(...samples.map((s) => s.value))
  let effectiveYMin = yMin ?? dataMin
  let effectiveYMax = yMax ?? dataMax
  if (effectiveYMax <= effectiveYMin) {
    // Pad symmetrically when the data is flat or domain is degenerate
    const pad = effectiveYMax === 0 ? 1 : Math.abs(effectiveYMax) * 0.1
    effectiveYMin = effectiveYMin - pad
    effectiveYMax = effectiveYMax + pad
  } else if (yMin == null && yMax == null) {
    // Auto-pad ~10% top/bottom for breathing room
    const pad = (effectiveYMax - effectiveYMin) * 0.1
    effectiveYMin = effectiveYMin - pad
    effectiveYMax = effectiveYMax + pad
  }
  const yRange = effectiveYMax - effectiveYMin || 1

  // Coordinate projection
  const projectX = (i: number): number => {
    if (isSingle) return PADDING.left + innerW
    return PADDING.left + (i / (samples.length - 1)) * innerW
  }
  const projectY = (value: number): number => {
    const clamped = Math.max(effectiveYMin, Math.min(effectiveYMax, value))
    return PADDING.top + (1 - (clamped - effectiveYMin) / yRange) * innerH
  }

  const polylinePoints = samples
    .map((s, i) => `${projectX(i).toFixed(2)},${projectY(s.value).toFixed(2)}`)
    .join(' ')

  const lastIdx = samples.length - 1
  const lastX = projectX(lastIdx)
  const lastY = projectY(samples[lastIdx].value)
  const isControlledHover = hoveredAt !== undefined || onHoverAtChange !== undefined
  const effectiveHoverIndex = isControlledHover ? indexForObservedAt(samples, hoveredAt) : hoverIndex

  // Y ticks (deduplicated by formatted string)
  const rawYTicks = computeYTicks(effectiveYMin, effectiveYMax, 4)
  const uniqueYTicksMap = new Map<string, number>()
  rawYTicks.forEach((t) => {
    const s = formatAxisValue(t)
    // Keep the highest numerical value for a given formatted string
    // This looks better (e.g. if 0.9 rounds to 1, put it closer to 1)
    if (!uniqueYTicksMap.has(s) || uniqueYTicksMap.get(s)! < t) {
      uniqueYTicksMap.set(s, t)
    }
  })
  const yTicks = Array.from(uniqueYTicksMap.values())

  // X ticks (sample indices)
  const xTickIndices = computeXTickIndices(samples.length, 5)

  // Handle hover
  const handleMove = (e: MouseEvent<SVGSVGElement>) => {
    if (isSingle) return
    const rect = e.currentTarget.getBoundingClientRect()
    if (rect.width === 0) return
    const relX = e.clientX - rect.left
    // Map relative pixel X to viewBox X
    const vbX = (relX / rect.width) * width
    // Convert vbX into sample index, accounting for left padding
    if (vbX < PADDING.left) {
      if (isControlledHover) onHoverAtChange?.(samples[0].observedAt)
      else setHoverIndex(0)
      return
    }
    if (vbX > PADDING.left + innerW) {
      if (isControlledHover) onHoverAtChange?.(samples[lastIdx].observedAt)
      else setHoverIndex(lastIdx)
      return
    }
    const dataX = vbX - PADDING.left
    const idx = Math.round((dataX / innerW) * (samples.length - 1))
    const nextIndex = Math.max(0, Math.min(lastIdx, idx))
    if (isControlledHover) onHoverAtChange?.(samples[nextIndex].observedAt)
    else setHoverIndex(nextIndex)
  }

  const handleLeave = () => {
    if (isControlledHover) onHoverAtChange?.(null)
    else setHoverIndex(null)
  }

  // Tooltip (anchored near the hovered sample point inside the chart)
  const tooltipNode = (() => {
    if (!showTooltip || effectiveHoverIndex == null || isSingle) return null
    const x = projectX(effectiveHoverIndex)
    const y = projectY(samples[effectiveHoverIndex].value)
    const xPercent = (x / width) * 100
    const sample = samples[effectiveHoverIndex]
    const placementClass = y < PADDING.top + 34
      ? 'metric-chart__tooltip--below'
      : 'metric-chart__tooltip--above'
    const edgeClass = xPercent < 18
      ? 'metric-chart__tooltip--edge-start'
      : xPercent > 82
        ? 'metric-chart__tooltip--edge-end'
        : ''
    return (
      <span
        className={['metric-chart__tooltip', placementClass, edgeClass].filter(Boolean).join(' ')}
        style={{ left: `${xPercent}%`, top: `${y.toFixed(2)}px` }}
      >
        <span className="metric-chart__tooltip-value">{formatValue(sample.value)}</span>
        <span className="metric-chart__tooltip-time">{formatTooltipLabel(sample.observedAt)}</span>
      </span>
    )
  })()

  // Maintenance window rectangles (rendered first → behind grid + line)
  const maintenanceRects = (() => {
    if (!maintenanceWindows || maintenanceWindows.length === 0 || isSingle) return null
    const firstTime = new Date(samples[0].observedAt).getTime()
    const lastTime = new Date(samples[lastIdx].observedAt).getTime()
    const span = lastTime - firstTime
    if (span <= 0) return null
    return maintenanceWindows.map((win, i) => {
      const startMs = new Date(win.startedAt).getTime()
      const endMs = new Date(win.endedAt).getTime()
      if (Number.isNaN(startMs) || Number.isNaN(endMs)) return null
      // Clamp to data range
      const sx = Math.max(firstTime, Math.min(lastTime, startMs))
      const ex = Math.max(firstTime, Math.min(lastTime, endMs))
      if (ex <= sx) return null
      const startRatio = (sx - firstTime) / span
      const endRatio = (ex - firstTime) / span
      const xStart = PADDING.left + startRatio * innerW
      const xEnd = PADDING.left + endRatio * innerW
      const w = xEnd - xStart
      return (
        <g key={`maint-${i}`} className="metric-chart__maintenance">
          <rect
            x={xStart}
            y={PADDING.top}
            width={w}
            height={innerH}
            fill="var(--color-state-maintenance)"
            opacity={0.08}
          />
          <polygon
            points={`${xStart + w / 2 - 4},${PADDING.top} ${xStart + w / 2 + 4},${PADDING.top} ${xStart + w / 2},${PADDING.top + 4}`}
            fill="var(--color-state-maintenance)"
            opacity={0.6}
          />
        </g>
      )
    })
  })()

  const svgStyle: CSSProperties = {
    display: 'block',
    cursor: isSingle ? 'default' : 'crosshair',
  }

  const svg = (
    <svg
      className={['metric-chart', className].filter(Boolean).join(' ')}
      width={propsWidth ? width : '100%'}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      role="img"
      aria-label={ariaLabel ?? `时序图 ${samples.length} 个采样`}
      style={svgStyle}
      onMouseMove={isSingle ? undefined : handleMove}
      onMouseLeave={isSingle ? undefined : handleLeave}
    >
      {/* Maintenance windows (background layer) */}
      {maintenanceRects}

      {/* Horizontal grid + Y-axis tick labels */}
      {yTicks.map((tickValue, i) => {
        const y = projectY(tickValue)
        return (
          <g key={`y-${i}`} className="metric-chart__y-tick">
            <line
              x1={PADDING.left}
              y1={y}
              x2={PADDING.left + innerW}
              y2={y}
              stroke="var(--border)"
              strokeWidth={0.5}
              strokeDasharray="2 3"
            />
            <text
              className="metric-chart__axis-text"
              x={PADDING.left - 4}
              y={y}
              textAnchor="end"
              dominantBaseline="middle"
              fontSize={10}
              fill="var(--text-muted)"
              opacity={0.8}
            >
              {formatAxisValue(tickValue)}
            </text>
          </g>
        )
      })}

      {/* X-axis tick labels */}
      {xTickIndices.map((idx) => {
        const x = projectX(idx)
        const sample = samples[idx]
        let anchor: 'start' | 'middle' | 'end' = 'middle'
        if (idx === 0) anchor = 'start'
        else if (idx === samples.length - 1) anchor = 'end'

        return (
          <text
            key={`x-${idx}`}
            className="metric-chart__axis-text"
            x={x}
            y={PADDING.top + innerH + 14}
            textAnchor={anchor}
            dominantBaseline="hanging"
            fontSize={10}
            fill="var(--text-muted)"
            opacity={0.8}
          >
            {formatTime(sample.observedAt)}
          </text>
        )
      })}

      {/* Polyline */}
      {!isSingle && (
        <polyline
          fill="none"
          stroke={stroke}
          strokeWidth={1.5}
          strokeLinecap="round"
          strokeLinejoin="round"
          points={polylinePoints}
        />
      )}

      {/* Threshold lines */}
      {thresholds?.map((t, i) => {
        // Clamp threshold visual position even if value is outside Y range
        const clampedY = projectY(t.value)
        const color = THRESHOLD_VAR[t.tone]
        const labelText = t.label ?? formatValue(t.value)
        return (
          <g key={`th-${i}`} className="metric-chart__threshold">
            <line
              x1={PADDING.left}
              y1={clampedY}
              x2={PADDING.left + innerW}
              y2={clampedY}
              stroke={color}
              strokeWidth={1}
              strokeDasharray="4 3"
              opacity={0.7}
            />
            <text
              className="metric-chart__threshold-label"
              x={PADDING.left + innerW - 2}
              y={clampedY - 2}
              textAnchor="end"
              fill={color}
            >
              {labelText}
            </text>
          </g>
        )
      })}

      {/* End point dot */}
      <circle cx={lastX} cy={lastY} r={2.5} fill={stroke} />

      {/* Hover crosshair */}
      {effectiveHoverIndex != null && !isSingle && (() => {
        const x = projectX(effectiveHoverIndex)
        const y = projectY(samples[effectiveHoverIndex].value)
        return (
          <g className="metric-chart__cursor">
            <line
              x1={x}
              y1={PADDING.top}
              x2={x}
              y2={PADDING.top + innerH}
              stroke="var(--text-muted)"
              strokeWidth={0.6}
              strokeDasharray="2 2"
            />
            <circle
              cx={x}
              cy={y}
              r={3}
              fill={stroke}
              stroke="var(--bg)"
              strokeWidth={1}
            />
          </g>
        )
      })()}
    </svg>
  )

  return (
    <span
      ref={containerRef}
      className={[
        'metric-chart-shell',
        isSingle && 'metric-chart-shell--single',
      ]
        .filter(Boolean)
        .join(' ')}
      style={{
        position: 'relative',
        display: 'inline-block',
        width: propsWidth ? width : '100%',
        height,
        verticalAlign: 'middle',
      }}
    >
      {svg}
      {isSingle ? <span className="metric-chart__hint">样本不足</span> : null}
      {tooltipNode}
    </span>
  )
}
