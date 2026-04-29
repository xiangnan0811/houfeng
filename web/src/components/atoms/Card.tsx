import type { HTMLAttributes, ReactNode } from 'react'

export type CardRole = 'default' | 'state' | 'accent' | 'warning'
export type CardTone = 'normal' | 'notice' | 'alert' | 'critical' | 'maintenance' | 'offline'

export interface CardProps extends Omit<HTMLAttributes<HTMLDivElement>, 'role'> {
  cardRole?: CardRole
  tone?: CardTone
  children: ReactNode
}

export function Card({
  cardRole = 'default',
  tone,
  className = '',
  children,
  ...rest
}: CardProps) {
  const classes = ['card', `card--${cardRole}`, tone && `tone--${tone}`, className]
    .filter(Boolean)
    .join(' ')
  return (
    <div className={classes} {...rest}>
      {children}
    </div>
  )
}
