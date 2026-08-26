import { type ReactNode } from 'react'
import { Link } from 'react-router-dom'

import { Timestamp } from './atoms'
import type { SubjectActivityItem, SubjectActivitySourceStatus } from '../lib/types'
import { timelineChannel, TIMELINE_CHANNEL_LABELS, type TimelineChannel } from './timelineChannel'

type Props = {
  items: SubjectActivityItem[]
  sourceStatuses?: SubjectActivitySourceStatus[]
  emptyTitle?: string
  emptyDescription?: string
  itemActions?: (item: SubjectActivityItem) => ReactNode
}

const CHANNEL_LABEL = TIMELINE_CHANNEL_LABELS

function dayKey(iso: string): string {
  const parsed = Date.parse(iso)
  if (Number.isNaN(parsed)) return '未知日期'
  return new Date(parsed).toISOString().slice(0, 10)
}

function itemHref(item: SubjectActivityItem, channel: TimelineChannel): string | null {
  if (channel === 'system') return null
  const recordId = item.record_id?.trim()
  const revisionId = item.revision_id?.trim()
  const evidenceId = item.evidence_snapshot_id?.trim()
    || item.subjects.find((subject) => subject.identity.evidence_snapshot_id)?.identity.evidence_snapshot_id

  if (channel === 'evidence' && evidenceId) {
    return `/evidence/${encodeURIComponent(evidenceId)}`
  }
  if (channel === 'human' && recordId && revisionId) {
    return `/records/${encodeURIComponent(recordId)}/revisions/${encodeURIComponent(revisionId)}`
  }
  if (channel === 'human' && recordId) {
    return `/records/${encodeURIComponent(recordId)}`
  }
  return null
}

function evidenceMeta(item: SubjectActivityItem): string | null {
  if (timelineChannel(item) !== 'evidence') return null
  const identity = item.subjects.find((subject) =>
    subject.identity.coverage || subject.identity.bucket || subject.identity.quality,
  )?.identity
  if (!identity) {
    return item.presentation.summary?.trim() || null
  }
  const parts = [
    identity.coverage ? `覆盖 ${identity.coverage}` : null,
    identity.bucket ? `桶 ${identity.bucket}` : null,
    identity.quality ? `质量 ${identity.quality}` : null,
  ].filter(Boolean)
  return parts.length ? parts.join(' · ') : (item.presentation.summary?.trim() || null)
}

function groupByDay(items: SubjectActivityItem[]): Array<{ day: string; items: SubjectActivityItem[] }> {
  const groups: Array<{ day: string; items: SubjectActivityItem[] }> = []
  for (const item of items) {
    const day = dayKey(item.event_at)
    const last = groups[groups.length - 1]
    if (last && last.day === day) {
      last.items.push(item)
    } else {
      groups.push({ day, items: [item] })
    }
  }
  return groups
}

export function UnifiedTimeline({
  items,
  sourceStatuses = [],
  emptyTitle = '暂无活动',
  emptyDescription = '当前筛选条件下没有可见活动。',
  itemActions,
}: Props) {
  const degraded = sourceStatuses.filter((status) => status.state !== 'ready')

  if (items.length === 0) {
    return (
      <div className="unified-timeline unified-timeline--empty" role="status">
        <h2 className="unified-timeline__empty-title">{emptyTitle}</h2>
        <p className="unified-timeline__empty-description">{emptyDescription}</p>
      </div>
    )
  }

  return (
    <div className="unified-timeline">
      {degraded.length > 0 ? (
        <ul className="unified-timeline__source-status" aria-label="来源状态">
          {degraded.map((status) => (
            <li key={status.source_kind}>
              {status.source_kind}：{status.state}
              {status.reason_code ? `（${status.reason_code}）` : ''}
            </li>
          ))}
        </ul>
      ) : null}
      {groupByDay(items).map((group) => (
        <section key={group.day} className="unified-timeline__day" aria-label={group.day}>
          <h2 className="unified-timeline__day-title">{group.day}</h2>
          <ol className="unified-timeline__list">
            {group.items.map((item) => {
              const channel = timelineChannel(item)
              const href = itemHref(item, channel)
              const meta = evidenceMeta(item)
              return (
                <li
                  key={item.activity_id}
                  className={`unified-timeline__item unified-timeline__item--${channel}`}
                >
                  <span
                    className={`unified-timeline__mark unified-timeline__mark--${channel}`}
                    aria-hidden
                  />
                  <div className="unified-timeline__body">
                    <div className="unified-timeline__header">
                      <p className="unified-timeline__channel">{CHANNEL_LABEL[channel]}</p>
                      <Timestamp value={item.event_at} mode="absolute" />
                    </div>
                    <h3 className="unified-timeline__title">{item.presentation.title}</h3>
                    {item.presentation.summary ? (
                      <p className="unified-timeline__summary">{item.presentation.summary}</p>
                    ) : null}
                    {meta ? <p className="unified-timeline__evidence-meta">{meta}</p> : null}
                    {href || itemActions ? (
                      <p className="unified-timeline__actions">
                        {href ? (
                          <Link className="text-link" to={href}>
                            {channel === 'evidence' ? '查看证据' : '查看修订'}
                          </Link>
                        ) : null}
                        {itemActions?.(item)}
                      </p>
                    ) : null}
                    {channel === 'system' ? (
                      <p className="unified-timeline__actions unified-timeline__actions--none">
                        系统事实不可编辑
                      </p>
                    ) : null}
                  </div>
                </li>
              )
            })}
          </ol>
        </section>
      ))}
    </div>
  )
}
