import { type MouseEvent, useState, useEffect, useRef } from 'react'
export type MetricChartTone =
  | 'normal'
  | 'notice'
  | 'alert'
  | 'critical'
  | 'maintenance'
  | 'accent'
  | 'accent-2'
  | 'muted'

export type MetricChartSample = {
  value: number | null
  observedAt: string
  /** Start a new line segment before this sample; values remain authoritative. */
  gapBefore?: boolean
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
  /**
   * Optional second series drawn in the same plot. Must be the same length as
   * `samples` and share every `observedAt`; otherwise it is ignored.
   */
  secondarySamples?: MetricChartSample[]
  /** Stroke tone for `secondarySamples`. Default `accent-2`. */
  secondaryTone?: MetricChartTone
  /**
   * Optional third series, same alignment rules as `secondarySamples`.
   * Ignored when omitted. Used by the load 1/5/15 overlay on detail and compare.
   */
  tertiarySamples?: MetricChartSample[]
  /** Stroke tone for `tertiarySamples`. Default `muted`. */
  tertiaryTone?: MetricChartTone
  /** Draw a 1px warning line at this Y value. No filled band — light-theme card fills read as a dirty header. */
  alertBandFrom?: number
  /** Left gutter width in viewBox units. Default keeps the existing internal rule. */
  paddingLeft?: number
  /** Draw a single finite sample as a dot instead of the "样本不足" hint. */
  allowSinglePoint?: boolean
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
  /** Expand unlocked Y bounds to include threshold values. Default keeps data domain. */
  includeThresholdsInScale?: boolean
  /** `gutter` places threshold labels in the right margin with collision spacing. */
  thresholdLabelPlacement?: 'overlay' | 'gutter'
  /** Number of Y-axis ticks including domain ends. Default 4. */
  yTickCount?: number
}
const TONE_VAR: Record<MetricChartTone, string> = {
  normal: 'var(--color-state-normal)',
  notice: 'var(--color-state-notice)',
  alert: 'var(--color-state-alert)',
  critical: 'var(--color-state-critical)',
  maintenance: 'var(--color-state-maintenance)',
  accent: 'var(--accent)',
  'accent-2': 'var(--accent-2)',
  muted: 'var(--text-secondary)',
}

const THRESHOLD_VAR: Record<MetricChartThreshold['tone'], string> = {
  notice: 'var(--color-state-notice)',
  alert: 'var(--color-state-alert)',
  critical: 'var(--color-state-critical)',
}

const PADDING_BASE = { top: 8, right: 8, bottom: 24, left: 32 }

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

function isPresentValue(value: number | null | undefined): value is number {
  return value != null && Number.isFinite(value)
}

function alignedSeries(primary: MetricChartSample[], extra?: MetricChartSample[]): MetricChartSample[] | null {
  if (!extra || extra.length !== primary.length) return null
  for (let i = 0; i < primary.length; i += 1) {
    if (primary[i]!.observedAt !== extra[i]!.observedAt) return null
  }
  return extra
}

/**
 * Compute "nice" Y-axis tick values between min/max.
 * Returns approximately `targetCount` ticks; always includes domain endpoints
 * (caller may render them as-is or snap to round numbers later).
 */
function computeYTicks(min: number, max: number, tickCount = 4): number[] {
  if (max <= min) return [min]
  if (tickCount <= 2) return [min, max]
  const step = (max - min) / (tickCount - 1)
  return Array.from({ length: tickCount }, (_, index) => min + step * index)
}

/**
 * Pick `targetCount` evenly-spaced sample indices for X-axis tick labels.
 * Always includes 0 and last index.
 */
function computeXTickIndices(sampleCount: number, tickCount = 5): number[] {
  if (sampleCount <= 1) return [0]
  if (sampleCount <= tickCount) {
    return Array.from({ length: sampleCount }, (_, i) => i)
  }
  return Array.from(
    { length: tickCount },
    (_, index) => Math.round((index / (tickCount - 1)) * (sampleCount - 1)),
  )
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
  secondarySamples,
  secondaryTone = 'accent-2',
  tertiarySamples,
  tertiaryTone = 'muted',
  alertBandFrom,
  paddingLeft,
  allowSinglePoint = false,
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
  includeThresholdsInScale = false,
  thresholdLabelPlacement = 'overlay',
  yTickCount = 4,
}: MetricChartProps) {
  const [hoverIndex, setHoverIndex] = useState<number | null>(null)
  const containerRef = useRef<HTMLSpanElement>(null)
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
  const secondaryStroke = TONE_VAR[secondaryTone]
  const tertiaryStroke = TONE_VAR[tertiaryTone]
  const width = propsWidth || measuredWidth || 360
  const firstSample = samples.at(0)

  // Extra series only participate when index-aligned with the primary series:
  // X is projected by index, so a mismatched series would lie.
  const secondary = alignedSeries(samples, secondarySamples)
  const tertiary = alignedSeries(samples, tertiarySamples)

  // Empty state looks at the primary series plus validated extra series, so a
  // chart with data on one side only still renders instead of claiming no data.
  const numericValues = [
    ...samples.map((sample) => sample.value),
    ...(secondary?.map((sample) => sample.value) ?? []),
    ...(tertiary?.map((sample) => sample.value) ?? []),
  ].filter(isPresentValue)

  // Empty state
  if (!firstSample || numericValues.length === 0) {
    return (
      <span
        ref={containerRef}
        className={[
          'metric-chart-shell',
          propsWidth && 'metric-chart-shell--fixed',
        ].filter(Boolean).join(' ')}
      >
        <svg
          className={['metric-chart', 'metric-chart--empty', className].filter(Boolean).join(' ')}
          width={propsWidth ? width : '100%'}
          height={height}
          viewBox={`0 0 ${width} ${height}`}
          role="img"
          aria-label={ariaLabel ?? '暂无观测数据'}
        >
          <text
            className="metric-chart__placeholder"
            x={width / 2}
            y={height / 2}
            textAnchor="middle"
            dominantBaseline="middle"
          >
            暂无观测数据
          </text>
        </svg>
      </span>
    )
  }

  const PADDING = {
    ...PADDING_BASE,
    right: thresholdLabelPlacement === 'gutter' ? 40 : PADDING_BASE.right,
    left: paddingLeft ?? (yTickCount <= 2 ? 96 : PADDING_BASE.left),
  }
  const innerW = width - PADDING.left - PADDING.right
  const innerH = height - PADDING.top - PADDING.bottom
  const isSingle = samples.length === 1
  // A lone finite primary sample can be drawn as a dot instead of the "样本不足" hint.
  const singlePointDot = isSingle && allowSinglePoint && isPresentValue(firstSample?.value)
  const firstNumeric = numericValues[0] ?? 0

  // Derive Y range from present values so missing buckets stay honest gaps.
  let dataMin = firstNumeric
  let dataMax = firstNumeric
  for (const value of numericValues) {
    dataMin = Math.min(dataMin, value)
    dataMax = Math.max(dataMax, value)
  }
  let effectiveYMin = yMin ?? dataMin
  let effectiveYMax = yMax ?? dataMax
  if (includeThresholdsInScale && thresholds?.length) {
    for (const threshold of thresholds) {
      if (!Number.isFinite(threshold.value)) continue
      if (yMin == null) effectiveYMin = Math.min(effectiveYMin, threshold.value)
      if (yMax == null) effectiveYMax = Math.max(effectiveYMax, threshold.value)
    }
  }
  if (effectiveYMax <= effectiveYMin) {
    const pad = effectiveYMax === 0 ? 1 : Math.abs(effectiveYMax) * 0.1
    effectiveYMin = effectiveYMin - pad
    effectiveYMax = effectiveYMax + pad
  } else if (yMin == null && yMax == null) {
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

  const buildSegments = (series: MetricChartSample[]): string[] => {
    const segments: string[] = []
    series.forEach((sample, index) => {
      if (!isPresentValue(sample.value)) return
      const point = `${projectX(index).toFixed(2)},${projectY(sample.value).toFixed(2)}`
      const previous = series[index - 1]
      const startSegment = index === 0 || sample.gapBefore || !isPresentValue(previous?.value)
      if (startSegment) segments.push(point)
      else segments[segments.length - 1] += ` ${point}`
    })
    return segments
  }
  const lineSegments = buildSegments(samples)
  const secondaryLineSegments = secondary ? buildSegments(secondary) : []
  const tertiaryLineSegments = tertiary ? buildSegments(tertiary) : []

  const lastIdx = samples.length - 1
  const sampleAt = (index: number) => samples[index] ?? firstSample
  const lastNumericIndex = samples.reduce((found, sample, index) => (
    isPresentValue(sample.value) ? index : found
  ), -1)
  const lastSample = lastNumericIndex >= 0 ? sampleAt(lastNumericIndex) : firstSample
  const lastX = projectX(lastNumericIndex >= 0 ? lastNumericIndex : lastIdx)
  const lastY = isPresentValue(lastSample.value) ? projectY(lastSample.value) : projectY(effectiveYMin)
  const secondaryLastNumericIndex = secondary
    ? secondary.reduce((found, sample, index) => (isPresentValue(sample.value) ? index : found), -1)
    : -1
  const secondaryLastX = projectX(secondaryLastNumericIndex >= 0 ? secondaryLastNumericIndex : lastIdx)
  const secondaryLastY = secondary && secondaryLastNumericIndex >= 0
    ? projectY(secondary[secondaryLastNumericIndex]!.value as number)
    : projectY(effectiveYMin)
  const tertiaryLastNumericIndex = tertiary
    ? tertiary.reduce((found, sample, index) => (isPresentValue(sample.value) ? index : found), -1)
    : -1
  const tertiaryLastX = projectX(tertiaryLastNumericIndex >= 0 ? tertiaryLastNumericIndex : lastIdx)
  const tertiaryLastY = tertiary && tertiaryLastNumericIndex >= 0
    ? projectY(tertiary[tertiaryLastNumericIndex]!.value as number)
    : projectY(effectiveYMin)
  const isControlledHover = hoveredAt !== undefined || onHoverAtChange !== undefined
  const effectiveHoverIndex = isControlledHover ? indexForObservedAt(samples, hoveredAt) : hoverIndex

  // Y ticks (deduplicated by formatted string)
  const rawYTicks = computeYTicks(effectiveYMin, effectiveYMax, yTickCount)
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
  const xTickIndices = computeXTickIndices(samples.length, innerW < 260 ? 3 : 5)

  // Handle hover
  const selectIndex = (index: number) => {
    if (isControlledHover) onHoverAtChange?.(sampleAt(index).observedAt)
    else setHoverIndex(index)
  }
  const handleMove = (e: MouseEvent<SVGSVGElement>) => {
    if (isSingle) return
    const rect = e.currentTarget.getBoundingClientRect()
    if (rect.width === 0) return
    const relX = e.clientX - rect.left
    // Map relative pixel X to viewBox X
    const vbX = (relX / rect.width) * width
    // Convert vbX into sample index, accounting for left padding
    if (vbX < PADDING.left) {
      selectIndex(0)
      return
    }
    if (vbX > PADDING.left + innerW) {
      selectIndex(lastIdx)
      return
    }
    const dataX = vbX - PADDING.left
    const idx = Math.round((dataX / innerW) * (samples.length - 1))
    selectIndex(Math.max(0, Math.min(lastIdx, idx)))
  }

  const handleLeave = () => {
    if (isControlledHover) onHoverAtChange?.(null)
    else setHoverIndex(null)
  }

  // Tooltip (anchored near the hovered sample point inside the chart)
  const tooltipNode = (() => {
    if (!showTooltip || effectiveHoverIndex == null || isSingle) return null
    const x = projectX(effectiveHoverIndex)
    const sample = sampleAt(effectiveHoverIndex)
    const y = isPresentValue(sample.value) ? projectY(sample.value) : PADDING.top + innerH / 2
    const extraValues = [
      secondary && isPresentValue(secondary[effectiveHoverIndex]?.value)
        ? formatValue(secondary[effectiveHoverIndex]!.value as number)
        : null,
      tertiary && isPresentValue(tertiary[effectiveHoverIndex]?.value)
        ? formatValue(tertiary[effectiveHoverIndex]!.value as number)
        : null,
    ].filter((value): value is string => value != null)
    const tooltipInset = 6
    const tooltipWidth = Math.min(196, width - tooltipInset * 2)
    const tooltipHeight = extraValues.length > 1 ? 48 : 38
    const tooltipX = Math.max(
      tooltipInset,
      Math.min(width - tooltipInset - tooltipWidth, x - tooltipWidth / 2),
    )
    const preferredY = y < PADDING.top + tooltipHeight + tooltipInset
      ? y + tooltipInset
      : y - tooltipHeight - tooltipInset
    const tooltipY = Math.max(0, Math.min(height - tooltipHeight, preferredY))
    return (
      <foreignObject
        className="metric-chart__tooltip-frame"
        x={tooltipX}
        y={tooltipY}
        width={tooltipWidth}
        height={tooltipHeight}
      >
        <div className="metric-chart__tooltip">
          <span className="metric-chart__tooltip-value">
            {[
              isPresentValue(sample.value) ? formatValue(sample.value) : '—',
              ...extraValues,
            ].join(' · ')}
          </span>
          <span className="metric-chart__tooltip-time">{formatTooltipLabel(sample.observedAt)}</span>
        </div>
      </foreignObject>
    )
  })()

  // Maintenance window rectangles (rendered first → behind grid + line)
  const maintenanceRects = (() => {
    if (!maintenanceWindows || maintenanceWindows.length === 0 || isSingle) return null
    const firstTime = new Date(firstSample.observedAt).getTime()
    const lastTime = new Date(lastSample.observedAt).getTime()
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

  // Warning is a 1px line at the threshold. A filled 80–100 band still reads as a
  // dirty header in the light theme even at 8% opacity, because it paints 20% of
  // the plot. Three-level copy stays in the chart title / aria-label.
  const alertBand = (() => {
    if (alertBandFrom == null || !Number.isFinite(alertBandFrom)) return null
    const y = projectY(alertBandFrom)
    if (y <= PADDING.top) return null
    return (
      <line
        className="metric-chart__alert-band-line"
        x1={PADDING.left}
        y1={y}
        x2={PADDING.left + innerW}
        y2={y}
        stroke="var(--color-state-notice)"
        strokeWidth={1}
        strokeDasharray="3 3"
        opacity={0.7}
      />
    )
  })()

  const thresholdMarks = (thresholds ?? []).map((threshold, index) => ({
    index,
    threshold,
    lineY: projectY(threshold.value),
    color: THRESHOLD_VAR[threshold.tone],
    // `label: ''` means "draw the line only" — no text node, no leader.
    showLabel: threshold.label !== '',
    labelText: threshold.label ?? formatValue(threshold.value),
    labelY: projectY(threshold.value) - 2,
    labelBelow: false,
  }))
  if (thresholdLabelPlacement !== 'gutter') {
    for (const mark of thresholdMarks) {
      if (!mark.showLabel) continue
      // Overlay labels sit 2px above the line. At the axis cap that clips into
      // the top padding and reads as a second header on the plot.
      if (mark.labelY < PADDING.top + 8) {
        mark.labelY = mark.lineY + 11
        mark.labelBelow = true
      }
    }
  }
  if (thresholdLabelPlacement === 'gutter') {
    const minGap = 14
    const ordered = thresholdMarks.filter((mark) => mark.showLabel).sort((a, b) => a.lineY - b.lineY)
    const top = PADDING.top + 8
    const bottom = PADDING.top + innerH - 4
    for (let i = 0; ordered.length > 1 && i < ordered.length; i += 1) {
      const mark = ordered[i]!
      const previous = ordered[i - 1]
      let labelY = Math.max(top, mark.lineY)
      if (previous) labelY = Math.max(labelY, previous.labelY + minGap)
      mark.labelY = Math.min(bottom, labelY)
    }
    for (let i = ordered.length - 2; i >= 0; i -= 1) {
      const mark = ordered[i]!
      const next = ordered[i + 1]!
      mark.labelY = Math.max(top, Math.min(mark.labelY, next.labelY - minGap))
    }
  }

  const svg = (
    <svg
      className={[
        'metric-chart',
        isSingle ? 'metric-chart--single' : 'metric-chart--interactive',
        className,
      ].filter(Boolean).join(' ')}
      width={propsWidth ? width : '100%'}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      role="img"
      aria-label={ariaLabel ?? `时序图 ${samples.length} 个采样`}
      onMouseMove={isSingle ? undefined : handleMove}
      onMouseLeave={isSingle ? undefined : handleLeave}
    >
      {/* Maintenance windows (background layer) */}
      {maintenanceRects}

      {/* Warning threshold: 1px dashed line, no filled band */}
      {alertBand}

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
        const sample = sampleAt(idx)
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
            opacity={0.8}
          >
            {formatTime(sample.observedAt)}
          </text>
        )
      })}

      {/* Gaps split the line. Isolated samples duplicate one coordinate for a visible rounded zero-length stroke. */}
      {!isSingle && tertiaryLineSegments.map((points) => (
        <polyline
          key={`tertiary-${points}`}
          className="metric-chart__line metric-chart__line--tertiary"
          fill="none"
          stroke={tertiaryStroke}
          strokeWidth={1.5}
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeDasharray="4 3"
          points={points.includes(' ') ? points : `${points} ${points}`}
        />
      ))}
      {!isSingle && secondaryLineSegments.map((points) => (
        <polyline
          key={`secondary-${points}`}
          className="metric-chart__line metric-chart__line--secondary"
          fill="none"
          stroke={secondaryStroke}
          strokeWidth={1.5}
          strokeLinecap="round"
          strokeLinejoin="round"
          points={points.includes(' ') ? points : `${points} ${points}`}
        />
      ))}
      {!isSingle && lineSegments.map((points) => (
        <polyline
          key={points}
          fill="none"
          stroke={stroke}
          strokeWidth={1.5}
          strokeLinecap="round"
          strokeLinejoin="round"
          points={points.includes(' ') ? points : `${points} ${points}`}
        />
      ))}

      {/* Threshold lines */}
      {thresholdMarks.map((mark) => {
        const labelX = thresholdLabelPlacement === 'gutter' ? width - 2 : PADDING.left + innerW - 2
        const lineEndX = PADDING.left + innerW
        const gutter = thresholdLabelPlacement === 'gutter'
        return (
          <g key={`th-${mark.index}`} className="metric-chart__threshold">
            <line
              x1={PADDING.left}
              y1={mark.lineY}
              x2={lineEndX}
              y2={mark.lineY}
              stroke={mark.color}
              strokeWidth={1}
              strokeDasharray="4 3"
              opacity={0.7}
            />
            {mark.showLabel && gutter ? (
              <polyline
                className="metric-chart__threshold-leader"
                fill="none"
                stroke={mark.color}
                strokeWidth={1}
                points={`${lineEndX},${mark.lineY} ${labelX - 10},${mark.lineY} ${labelX - 10},${mark.labelY} ${labelX - 4},${mark.labelY}`}
              />
            ) : null}
            {mark.showLabel ? (
              <text
                className="metric-chart__threshold-label"
                x={labelX}
                y={mark.labelY}
                textAnchor="end"
                dominantBaseline={gutter ? 'middle' : mark.labelBelow ? 'hanging' : undefined}
                fill={mark.color}
              >
                {mark.labelText}
              </text>
            ) : null}
          </g>
        )
      })}

      {/* End point dot */}
      {isPresentValue(lastSample.value) ? (
        <circle cx={lastX} cy={lastY} r={singlePointDot ? 3 : 2.5} fill={stroke} />
      ) : null}
      {tertiary && tertiaryLastNumericIndex >= 0 ? (
        <circle
          className="metric-chart__end-dot--tertiary"
          cx={tertiaryLastX}
          cy={tertiaryLastY}
          r={isSingle ? 3 : 2.5}
          fill={tertiaryStroke}
        />
      ) : null}
      {secondary && secondaryLastNumericIndex >= 0 ? (
        <circle
          className="metric-chart__end-dot--secondary"
          cx={secondaryLastX}
          cy={secondaryLastY}
          r={isSingle ? 3 : 2.5}
          fill={secondaryStroke}
        />
      ) : null}

      {/* Hover crosshair */}
      {effectiveHoverIndex != null && !isSingle && (() => {
        const hovered = sampleAt(effectiveHoverIndex)
        const x = projectX(effectiveHoverIndex)
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
            {isPresentValue(hovered.value) ? (
              <circle
                cx={x}
                cy={projectY(hovered.value)}
                r={3}
                fill={stroke}
                stroke="var(--bg)"
                strokeWidth={1}
              />
            ) : null}
            {tertiary && isPresentValue(tertiary[effectiveHoverIndex]?.value) ? (
              <circle
                cx={x}
                cy={projectY(tertiary[effectiveHoverIndex]!.value as number)}
                r={3}
                fill={tertiaryStroke}
                stroke="var(--bg)"
                strokeWidth={1}
              />
            ) : null}
            {secondary && isPresentValue(secondary[effectiveHoverIndex]?.value) ? (
              <circle
                cx={x}
                cy={projectY(secondary[effectiveHoverIndex]!.value as number)}
                r={3}
                fill={secondaryStroke}
                stroke="var(--bg)"
                strokeWidth={1}
              />
            ) : null}
          </g>
        )
      })()}
      {tooltipNode}
    </svg>
  )

  return (
    <span
      ref={containerRef}
      className={[
        'metric-chart-shell',
        isSingle && 'metric-chart-shell--single',
        propsWidth && 'metric-chart-shell--fixed',
      ]
        .filter(Boolean)
        .join(' ')}
    >
      {svg}
      {isSingle && !singlePointDot ? <span className="metric-chart__hint">样本不足</span> : null}
    </span>
  )
}
