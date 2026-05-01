import type { HTMLAttributes, ReactNode } from 'react'

export type CardRole = 'default' | 'state' | 'accent' | 'warning' | 'dim'
export type CardTone = 'normal' | 'notice' | 'alert' | 'critical' | 'maintenance' | 'offline'
export type RibbonPlacement = 'left' | 'top'

export interface CardProps extends Omit<HTMLAttributes<HTMLDivElement>, 'role'> {
  cardRole?: CardRole
  tone?: CardTone
  /** Only meaningful when cardRole='state'. Defaults to 'left' for backward compat. */
  ribbonPlacement?: RibbonPlacement
  children: ReactNode
}

export function Card({
  cardRole = 'default',
  tone,
  ribbonPlacement,
  className = '',
  children,
  ...rest
}: CardProps) {
  const ribbonClass =
    cardRole === 'state' ? `card--ribbon-${ribbonPlacement ?? 'left'}` : ''
  const classes = [
    'card',
    `card--${cardRole}`,
    ribbonClass,
    tone && `tone--${tone}`,
    className,
  ]
    .filter(Boolean)
    .join(' ')
  return (
    <div className={classes} {...rest}>
      {children}
    </div>
  )
}
