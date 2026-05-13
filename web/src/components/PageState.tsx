import type { ReactNode } from 'react'

import { Timestamp } from './atoms'

export type PageStateKind = 'loading' | 'error' | 'empty'
export type PageStateSurface = 'panel' | 'empty'

type PageStateProps = {
  kind: PageStateKind
  title: string
  eyebrow?: string
  description?: ReactNode
  action?: ReactNode
  technicalSummary?: string | null
  compact?: boolean
  surface?: PageStateSurface
  timestamp?: string | Date
  className?: string
}

function truncateTechnicalSummary(value: string): string {
  const normalized = value.trim()
  if (normalized.length <= 120) return normalized
  return `${normalized.slice(0, 119)}…`
}

function stateLabel(kind: PageStateKind): string {
  if (kind === 'loading') return '正在加载'
  if (kind === 'error') return '状态异常'
  return '当前为空'
}

function StateMark() {
  return (
    <svg className="page-state__mark" viewBox="0 0 48 48" aria-hidden="true">
      <path d="M10 19h18c5.5 0 9-2.8 9-7 0-2.6-1.5-4.8-4.3-5.7" />
      <path d="M8 28h27c4.2 0 7 2.2 7 5.6 0 3-2.1 5.1-5.4 5.4" />
      <path d="M15 37h13" />
    </svg>
  )
}

export function PageState({
  kind,
  title,
  eyebrow,
  description,
  action,
  technicalSummary,
  compact = false,
  surface = 'panel',
  timestamp,
  className = '',
}: PageStateProps) {
  const descriptionText = typeof description === 'string' ? description.trim() : null
  const rawSummary = technicalSummary?.trim() ?? null
  const summary =
    rawSummary && rawSummary !== descriptionText ? truncateTechnicalSummary(rawSummary) : null
  const classes = [
    surface === 'panel' ? 'page-panel' : 'empty-state',
    'page-state',
    `page-state--${kind}`,
    compact && 'page-state--compact',
    className,
  ].filter(Boolean).join(' ')
  const role = kind === 'error' ? 'alert' : kind === 'loading' ? 'status' : undefined
  const ariaLive = kind === 'error' ? 'assertive' : 'polite'

  return (
    <section className={classes} role={role} aria-live={ariaLive}>
      <StateMark />
      <div className="page-state__content">
        <p className="page-state__eyebrow">{eyebrow ?? stateLabel(kind)}</p>
        <h2 className="page-state__title">{title}</h2>
        {description ? <p className="page-state__description">{description}</p> : null}
        {kind === 'loading' ? (
          <p className="page-state__meta">
            请求发起 <Timestamp value={timestamp ?? new Date()} mode="absolute" />
          </p>
        ) : null}
        {summary ? (
          <p className="page-state__summary" title={rawSummary ?? undefined}>
            <span>错误摘要</span>
            {summary}
          </p>
        ) : null}
        {action ? <div className="page-state__actions">{action}</div> : null}
      </div>
    </section>
  )
}
