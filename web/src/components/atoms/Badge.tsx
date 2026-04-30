import type { ReactNode } from 'react'

export type BadgeVariant = 'state' | 'info' | 'count'
export type BadgeTone =
  | 'normal'
  | 'notice'
  | 'alert'
  | 'critical'
  | 'maintenance'
  | 'offline'
  | 'neutral'

export interface BadgeProps {
  children: ReactNode
  variant?: BadgeVariant
  tone?: BadgeTone
  className?: string
  withDot?: boolean
}

export function Badge({
  children,
  variant = 'info',
  tone = 'neutral',
  className = '',
  withDot = false,
}: BadgeProps) {
  const cls = [
    'badge',
    `badge--${variant}`,
    tone !== 'neutral' && `tone--${tone}`,
    className,
  ]
    .filter(Boolean)
    .join(' ')
  return (
    <span className={cls}>
      {withDot && <span className="badge__dot" aria-hidden />}
      {children}
    </span>
  )
}
