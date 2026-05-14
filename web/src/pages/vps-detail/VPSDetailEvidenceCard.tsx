import type { ReactNode } from 'react'

export type VPSDetailEvidenceTone = 'normal' | 'notice' | 'alert' | 'critical' | 'neutral'

type VPSDetailEvidenceCardProps = {
  label: string
  value: ReactNode
  meta?: ReactNode
  tone?: VPSDetailEvidenceTone
  children?: ReactNode
}

export function VPSDetailEvidenceCard({
  label,
  value,
  meta,
  tone = 'neutral',
  children,
}: VPSDetailEvidenceCardProps) {
  return (
    <article className={['vps-detail-evidence-card', `vps-detail-evidence-card--${tone}`].join(' ')}>
      <span className="vps-detail-evidence-card__label">{label}</span>
      <strong className="vps-detail-evidence-card__value">{value}</strong>
      {meta ? <small className="vps-detail-evidence-card__meta">{meta}</small> : null}
      {children ? <div className="vps-detail-evidence-card__body">{children}</div> : null}
    </article>
  )
}
