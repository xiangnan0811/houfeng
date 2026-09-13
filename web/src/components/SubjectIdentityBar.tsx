import type { ReactNode } from 'react'
import { Link, useLocation } from 'react-router-dom'

import { Badge } from './atoms'
import type { SubjectActivityHeader } from '../lib/types'
import { SUBJECT_KIND_LABELS } from './timelineChannel'

type Props = {
  subject: SubjectActivityHeader
  actions?: ReactNode
  returnHref?: string
  returnLabel?: string
}

function displayName(subject: SubjectActivityHeader): string {
  const name = subject.identity.display_name?.trim()
    || subject.identity.name?.trim()
    || subject.identity.hostname?.trim()
  return name || subject.source_id
}

export function SubjectIdentityBar({
  subject,
  actions,
  returnHref,
  returnLabel = '返回主体',
}: Props) {
  const { state } = useLocation()
  const tombstoned = subject.status === 'tombstoned'
  const title = displayName(subject)

  return (
    <header className="page__head subject-identity-bar">
      <div className="subject-identity-bar__main">
        <p className="subject-identity-bar__kind">{SUBJECT_KIND_LABELS[subject.kind]}</p>
        <h1 className="page__title">{title}</h1>
        <p className="page-sub subject-identity-bar__meta">
          <span className="mono">{subject.source_id}</span>
          {' · '}
          {tombstoned ? (
            <Badge variant="state" tone="critical">已删除主体</Badge>
          ) : (
            <Badge variant="info" tone="neutral">在册</Badge>
          )}
        </p>
        {returnHref ? (
          <p className="page-sub">
            <Link className="text-link" to={returnHref} state={subject.kind === 'vps' ? state : undefined}>{returnLabel}</Link>
          </p>
        ) : null}
      </div>
      {actions ? <div className="page__actions subject-identity-bar__actions">{actions}</div> : null}
    </header>
  )
}
