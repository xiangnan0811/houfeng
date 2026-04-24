import type { PropsWithChildren, ReactNode } from 'react'

type DetailSectionProps = PropsWithChildren<{
  eyebrow?: string
  title: string
  aside?: ReactNode
}>

export function DetailSection({
  eyebrow,
  title,
  aside,
  children,
}: DetailSectionProps) {
  return (
    <section className="detail-section">
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
