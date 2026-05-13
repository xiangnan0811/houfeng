import type { ReactNode } from 'react'

type ObservabilityEvidenceFocusProps = {
  glyph: ReactNode
  eyebrow: string
  title: string
  description: string
  meta: ReactNode
  action: ReactNode
  stable?: boolean
}

export function ObservabilityEvidenceFocus({
  glyph,
  eyebrow,
  title,
  description,
  meta,
  action,
  stable = false,
}: ObservabilityEvidenceFocusProps) {
  const className = [
    'observability-evidence-focus',
    stable && 'observability-evidence-focus--stable',
  ]
    .filter(Boolean)
    .join(' ')

  return (
    <article className={className}>
      <div className="observability-evidence-focus__glyph">{glyph}</div>
      <div className="observability-evidence-focus__body">
        <p className="observability-evidence-focus__eyebrow">{eyebrow}</p>
        <h3>{title}</h3>
        <p>{description}</p>
        <span>{meta}</span>
      </div>
      {action}
    </article>
  )
}
