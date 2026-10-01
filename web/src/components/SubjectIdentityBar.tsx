import type { ReactNode } from 'react'
import { Link, useLocation } from 'react-router-dom'

import { Badge } from './atoms'
import type { RecordSubjectKind, SubjectActivityHeader } from '../lib/types'
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

/** 主体类型图标：与 VPS 概览页头同一位置与尺寸，切换概览 / 活动时页头不跳动。 */
export function SubjectKindMark({ kind }: { kind: RecordSubjectKind }) {
  return (
    <span className="subject-identity-bar__mark" aria-hidden="true">
      <svg viewBox="0 0 24 24" focusable="false">
        {kind === 'vps' ? (
          <>
            <rect x="3.5" y="3.5" width="17" height="7" rx="1.5" />
            <rect x="3.5" y="13.5" width="17" height="7" rx="1.5" />
            <path d="M7 7h.01M7 17h.01M11 7h6M11 17h6" />
          </>
        ) : kind === 'monitoring_instance' ? (
          <path d="M3 12h4l2.5-6 4 12 2.5-6H21" />
        ) : (
          <>
            <circle cx="12" cy="12" r="8.5" />
            <circle cx="12" cy="12" r="4.5" />
            <path d="M12 12h.01" />
          </>
        )}
      </svg>
    </span>
  )
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
  const hostname = subject.identity.hostname?.trim() ?? ''

  return (
    <header className="page__head subject-identity-bar">
      <div className="subject-identity-bar__lead">
        <SubjectKindMark kind={subject.kind} />
        <div className="subject-identity-bar__copy">
          <div className="subject-identity-bar__title-row">
            <h1 className="page__title">{title}</h1>
            {tombstoned ? (
              <Badge variant="state" tone="critical">已删除主体</Badge>
            ) : (
              <Badge variant="info" tone="neutral">在册</Badge>
            )}
          </div>
          <dl className="subject-identity-bar__meta" aria-label="主体身份">
            <div className="subject-identity-bar__meta-item"><dt>类型</dt><dd>{SUBJECT_KIND_LABELS[subject.kind]}</dd></div>
            <div className="subject-identity-bar__meta-item"><dt>ID</dt><dd className="mono">{subject.source_id}</dd></div>
            {hostname && hostname !== title ? (
              <div className="subject-identity-bar__meta-item"><dt>主机名</dt><dd className="mono">{hostname}</dd></div>
            ) : null}
          </dl>
        </div>
      </div>
      {returnHref || actions ? (
        <div className="page__actions subject-identity-bar__actions">
          {returnHref ? (
            <Link className="btn md secondary" to={returnHref} state={subject.kind === 'target' ? undefined : state}>
              {returnLabel}
            </Link>
          ) : null}
          {actions}
        </div>
      ) : null}
    </header>
  )
}
