import type { CSSProperties } from 'react'

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

export interface SparklineProps {
  values: number[]
  tone?: SparklineTone
  width?: number
  height?: number
  className?: string
  ariaLabel?: string
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

export function Sparkline({
  values,
  tone = 'default',
  width = 64,
  height = 16,
  className = '',
  ariaLabel,
}: SparklineProps) {
  if (values.length === 0) {
    return (
      <span
        className={['sparkline', 'sparkline--empty', className].filter(Boolean).join(' ')}
        aria-label={ariaLabel ?? '无趋势数据'}
        role="img"
        style={{ width, height, display: 'inline-block' }}
      />
    )
  }

  const min = Math.min(...values)
  const max = Math.max(...values)
  const range = max - min || 1
  const stepX = values.length === 1 ? 0 : width / (values.length - 1)
  const points = values
    .map((v, i) => {
      const x = i * stepX
      const y = height - ((v - min) / range) * (height - 2) - 1
      return `${x.toFixed(2)},${y.toFixed(2)}`
    })
    .join(' ')

  const lastX = (values.length - 1) * stepX
  const lastY = height - ((values[values.length - 1] - min) / range) * (height - 2) - 1

  const stroke = TONE_VAR[tone]

  const style: CSSProperties = { display: 'inline-block', verticalAlign: 'middle' }

  return (
    <svg
      className={['sparkline', `sparkline--${tone}`, className].filter(Boolean).join(' ')}
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      role="img"
      aria-label={ariaLabel ?? `趋势 ${values.length} 个采样`}
      style={style}
    >
      <polyline
        fill="none"
        stroke={stroke}
        strokeWidth={1.5}
        strokeLinecap="round"
        strokeLinejoin="round"
        points={points}
      />
      <circle cx={lastX} cy={lastY} r={1.6} fill={stroke} />
    </svg>
  )
}
