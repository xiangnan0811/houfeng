import type { ReactNode } from 'react'

type Props = {
  title: string
  meta: string
  snapshotId: string
  children?: ReactNode
}

export function ComparisonEvidenceChoice({ title, meta, snapshotId, children }: Props) {
  return (
    <div className="record-compare-picker__row">
      <div className="record-compare-items__copy">
        <span className="record-compare-items__title">{title}</span>
        {meta ? <span className="record-compare-items__meta">{meta}</span> : null}
      </div>
      {children}
      <details className="record-disclosure record-compare-items__tech">
        <summary>技术标识</summary>
        <code>{snapshotId}</code>
      </details>
    </div>
  )
}
