import { useState, type ReactNode } from 'react'

interface Props {
  title: string
  defaultOpen?: boolean
  children: ReactNode
  className?: string
}

/**
 * Animated collapsible section — replaces native <details> for consistent
 * animation and visual hierarchy across the app.
 */
export function CollapsibleSection({ title, defaultOpen = false, children, className = '' }: Props) {
  const [open, setOpen] = useState(defaultOpen)

  return (
    <section className={`collapsible-section${open ? ' is-open' : ''} ${className}`.trim()}>
      <button
        type="button"
        className="collapsible-section__trigger"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
      >
        <span className="collapsible-section__caret" aria-hidden />
        <span className="collapsible-section__title">{title}</span>
      </button>
      <div
        className="collapsible-section__body-wrapper"
        aria-hidden={!open}
      >
        <div className="collapsible-section__body">
          {children}
        </div>
      </div>
    </section>
  )
}
