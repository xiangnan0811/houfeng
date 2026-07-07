import type { ReactNode } from 'react'

import { Card } from './Card'

export type StatTone = 'normal' | 'warn' | 'err'

export interface StatCardProps {
  value: ReactNode
  label: ReactNode
  /** Optional supporting content rendered below the label (e.g. a sparkline or hint). */
  sub?: ReactNode
  tone?: StatTone
  onClick?: () => void
  className?: string
}

/**
 * Canonical metric card. Renders a single bold value (26/700), a label and an
 * optional sub-slot, all inside a `Card`. Replaces the legacy `.hero-stats` /
 * `.hs-value` and Dashboard `.metric` markup so every stat card shares one spec.
 */
export function StatCard({ value, label, sub, tone = 'normal', onClick, className = '' }: StatCardProps) {
  const classes = ['stat', onClick ? 'stat--clickable' : '', className].filter(Boolean).join(' ')
  const content = (
    <>
      <span className={`stat-value${tone !== 'normal' ? ` is-${tone}` : ''}`}>{value}</span>
      <span className="stat-label">{label}</span>
      {sub != null && <span className="stat-sub">{sub}</span>}
    </>
  )
  // When interactive, render a real <button type="button"> so keyboard users and
  // screen readers get native Enter/Space activation and focus — never a bare
  // clickable <div>. The card classes are kept so visuals stay identical.
  if (onClick) {
    return (
      <button type="button" className={`card card--default ${classes}`} onClick={onClick}>
        {content}
      </button>
    )
  }
  return <Card className={classes}>{content}</Card>
}
