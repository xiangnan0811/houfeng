import type { ReactNode } from 'react'

export interface SectionTitleProps {
  title: ReactNode
  /** Optional right-aligned action (e.g. a "view all" link). */
  action?: ReactNode
  /** Optional count chip rendered next to the title. */
  count?: ReactNode
  className?: string
}

/**
 * Canonical section heading: a 14/600 title with an optional count chip and an
 * optional right-aligned action slot. Replaces the legacy `.panel-h` markup so
 * every panel/section header shares one visual spec.
 */
export function SectionTitle({ title, action, count, className = '' }: SectionTitleProps) {
  return (
    <div className={`section-title ${className}`.trim()}>
      <span className="section-title__label">{title}</span>
      {count != null && <span className="section-count">{count}</span>}
      {action != null && <span className="section-action">{action}</span>}
    </div>
  )
}
