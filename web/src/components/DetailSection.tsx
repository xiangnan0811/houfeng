import type { PropsWithChildren, ReactNode } from 'react'

export type DetailSectionRibbon =
  | 'normal'
  | 'notice'
  | 'alert'
  | 'critical'
  | 'maintenance'
  | 'offline'
  | 'accent'
  | 'accent-2'

type DetailSectionProps = PropsWithChildren<{
  eyebrow?: string
  title: string
  aside?: ReactNode
  /** Optional 1px hairline ribbon at the top of the section. */
  ribbon?: DetailSectionRibbon
}>

export function DetailSection({
  eyebrow,
  title,
  aside,
  ribbon,
  children,
}: DetailSectionProps) {
  const cls = ['detail-section', ribbon && `detail-section--ribbon-${ribbon}`]
    .filter(Boolean)
    .join(' ')
  return (
    <section className={cls}>
      <header className="detail-section__header">
        <div>
          {eyebrow ? <p className="detail-section__eyebrow">{eyebrow}</p> : null}
          <h2 className="detail-section__title">{title}</h2>
        </div>
        {aside ? <div className="detail-section__aside">{aside}</div> : null}
      </header>
      <div className="detail-section__body">{children}</div>
    </section>
  )
}
