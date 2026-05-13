import type { ReactNode } from 'react'

export type ObservabilityEvidenceTone =
  | 'normal'
  | 'notice'
  | 'alert'
  | 'critical'
  | 'maintenance'
  | 'offline'

type ObservabilityEvidenceLeadProps = {
  tone: ObservabilityEvidenceTone
  eyebrow: string
  title: string
  description: string
  filterItems: string[]
  emptyFilterLabel: string
  filterAriaLabel: string
  action: ReactNode
  secondaryAction?: ReactNode
}

export function ObservabilityEvidenceLead({
  tone,
  eyebrow,
  title,
  description,
  filterItems,
  emptyFilterLabel,
  filterAriaLabel,
  action,
  secondaryAction,
}: ObservabilityEvidenceLeadProps) {
  const visibleFilters = filterItems.length > 0 ? filterItems : [emptyFilterLabel]

  return (
    <div className={`observability-evidence-lead observability-evidence-lead--${tone}`}>
      <div className="observability-evidence-lead__main">
        <p className="observability-evidence-lead__eyebrow">{eyebrow}</p>
        <h3>{title}</h3>
        <p>{description}</p>
        <div className="observability-evidence-lead__filters" aria-label={filterAriaLabel}>
          {visibleFilters.map((item) => (
            <span key={item}>{item}</span>
          ))}
        </div>
      </div>
      <div className="observability-evidence-lead__action">
        {action}
        {secondaryAction}
      </div>
    </div>
  )
}
