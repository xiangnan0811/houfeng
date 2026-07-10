export type HealthState =
  | 'normal'
  | 'notice'
  | 'alert'
  | 'critical'
  | 'maintenance'
  | 'offline'

export type StatusGlyphSize = 'sm' | 'md'

export interface StatusGlyphProps {
  state: HealthState
  size?: StatusGlyphSize
  className?: string
  ariaLabel?: string
}

const STATE_COLOR: Record<HealthState, string> = {
  normal: 'var(--color-state-normal)',
  notice: 'var(--color-state-notice)',
  alert: 'var(--color-state-alert)',
  critical: 'var(--color-state-critical)',
  maintenance: 'var(--color-state-maintenance)',
  offline: 'var(--color-state-offline)',
}

const STATE_LABEL: Record<HealthState, string> = {
  normal: '正常',
  notice: '关注',
  alert: '告警',
  critical: '严重',
  maintenance: '维护',
  offline: '离线',
}

const SIZE_PX: Record<StatusGlyphSize, number> = { sm: 10, md: 14 }

export function StatusGlyph({
  state,
  size = 'md',
  className = '',
  ariaLabel,
}: StatusGlyphProps) {
  const px = SIZE_PX[size]
  const color = STATE_COLOR[state]
  const stroke = 1.4

  const cls = [
    'status-glyph',
    `status-glyph--${state}`,
    `status-glyph--${size}`,
    className,
  ]
    .filter(Boolean)
    .join(' ')

  return (
    <svg
      className={cls}
      width={px}
      height={px}
      viewBox="0 0 16 16"
      role="img"
      aria-label={ariaLabel ?? STATE_LABEL[state]}
    >
      {state === 'normal' && <circle cx={8} cy={8} r={5} fill={color} />}
      {state === 'notice' && (
        <>
          <circle cx={8} cy={8} r={5} fill="none" stroke={color} strokeWidth={stroke} />
          <path d="M8 3 A5 5 0 0 0 8 13 Z" fill={color} />
        </>
      )}
      {state === 'alert' && (
        <>
          <circle cx={8} cy={8} r={5} fill="none" stroke={color} strokeWidth={stroke} />
          <path d="M3 8 A5 5 0 0 0 13 8 Z" fill={color} />
        </>
      )}
      {state === 'critical' && (
        <>
          <rect x={3} y={3} width={10} height={10} fill={color} />
          <path d="M3 13 L13 3" stroke="var(--bg)" strokeWidth={1.5} />
        </>
      )}
      {state === 'maintenance' && (
        <rect
          x={3.5}
          y={3.5}
          width={9}
          height={9}
          fill="none"
          stroke={color}
          strokeWidth={stroke}
        />
      )}
      {state === 'offline' && (
        <circle
          cx={8}
          cy={8}
          r={5}
          fill="none"
          stroke={color}
          strokeWidth={stroke}
          strokeDasharray="2 2"
        />
      )}
    </svg>
  )
}
