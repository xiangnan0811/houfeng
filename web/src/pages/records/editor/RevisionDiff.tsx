import { diffLines } from 'diff'

import type { RecordDraftPayload, RecordRevision } from '../../../lib/types'
import {
  differingRecordFields,
  recordComparablePayload,
  recordFieldText,
} from '../recordFields'

type RevisionDiffProps = {
  base: RecordDraftPayload | RecordRevision
  local: RecordDraftPayload | RecordRevision
}

export function RevisionDiff({ base, local }: RevisionDiffProps) {
  const previous = recordComparablePayload(base)
  const next = recordComparablePayload(local)
  const changed = differingRecordFields(previous, next)
  return (
    <section className="card" aria-label="修订差异">
      <h2 className="section-heading__title">字段差异</h2>
      {changed.length === 0 ? <p className="text-muted">没有字段差异。</p> : null}
      {changed.map(({ field, label }) => (
        <article key={field}>
          <h3>{label}</h3>
          {field === 'body_markdown' ? (
            <MarkdownHunks previous={recordFieldText(previous, field)} next={recordFieldText(next, field)} />
          ) : (
            <p>
              <span className="text-muted">{recordFieldText(previous, field) || '（空）'}</span>
              {' → '}
              <strong>{recordFieldText(next, field) || '（空）'}</strong>
            </p>
          )}
        </article>
      ))}
    </section>
  )
}

function MarkdownHunks({ previous, next }: { previous: string; next: string }) {
  const parts = diffLines(previous, next)
  return (
    <pre className="card" aria-label="正文差异">
      {parts.map((part, index) => (
        <span
          key={`${part.value}-${index}`}
          className={part.removed ? 'text-warn' : part.added ? undefined : 'text-muted'}
          data-diff={part.added ? 'add' : part.removed ? 'remove' : 'same'}
        >
          {part.value}
        </span>
      ))}
    </pre>
  )
}
