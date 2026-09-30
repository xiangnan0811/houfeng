import { useId } from 'react'
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
  title?: string
}

export function RevisionDiff({ base, local, title = '与当前版本的差异' }: RevisionDiffProps) {
  const titleId = useId()
  const previous = recordComparablePayload(base)
  const next = recordComparablePayload(local)
  const changed = differingRecordFields(previous, next)
  return (
    <section className="record-section record-diff" aria-labelledby={titleId}>
      <div className="record-section__head">
        <h2 className="record-section__title" id={titleId}>{title}</h2>
        <span className="record-muted">{changed.length === 0 ? '无差异' : `${changed.length} 个字段`}</span>
      </div>
      {changed.length === 0 ? null : (
        <dl className="record-diff__fields">
          {changed.map(({ field, label }) => (
            <div key={field} className="record-diff__field">
              <dt>{label}</dt>
              <dd>
                {field === 'body_markdown' ? (
                  <MarkdownHunks previous={recordFieldText(previous, field)} next={recordFieldText(next, field)} />
                ) : (
                  <>
                    <span className="record-diff__before">{recordFieldText(previous, field) || '（空）'}</span>
                    <span className="record-diff__arrow" aria-hidden="true">→</span>
                    <span className="record-diff__after">{recordFieldText(next, field) || '（空）'}</span>
                  </>
                )}
              </dd>
            </div>
          ))}
        </dl>
      )}
    </section>
  )
}

function MarkdownHunks({ previous, next }: { previous: string; next: string }) {
  const parts = diffLines(previous, next)
  const added = parts.reduce((total, part) => total + (part.added ? part.count ?? 0 : 0), 0)
  const removed = parts.reduce((total, part) => total + (part.removed ? part.count ?? 0 : 0), 0)
  return (
    <details className="record-disclosure">
      <summary>
        <span className="record-diff__stat record-diff__stat--add">+{added}</span>
        <span className="record-diff__stat record-diff__stat--remove">−{removed}</span>
        <span>行</span>
      </summary>
      <pre className="record-diff__hunks" role="region" aria-label="正文差异" tabIndex={0}>
        {parts.map((part, index) => (
          <span
            key={`${part.value}-${index}`}
            className={part.removed ? 'record-diff__line--remove' : part.added ? 'record-diff__line--add' : 'record-diff__line--same'}
            data-diff={part.added ? 'add' : part.removed ? 'remove' : 'same'}
          >
            {part.value}
          </span>
        ))}
      </pre>
    </details>
  )
}
