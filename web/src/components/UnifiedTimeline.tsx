import { type ReactNode, useId } from 'react'
import { Link, useLocation } from 'react-router-dom'

import type { SubjectActivityItem, SubjectActivitySourceStatus } from '../lib/types'
import {
  SOURCE_KIND_LABELS,
  SOURCE_STATE_LABELS,
  timelineChannel,
  TIMELINE_CHANNEL_LABELS,
  type TimelineChannel,
} from './timelineChannel'

type Props = {
  items: SubjectActivityItem[]
  sourceStatuses?: SubjectActivitySourceStatus[]
  emptyTitle?: string
  emptyDescription?: string
  itemActions?: (item: SubjectActivityItem) => ReactNode
  /** 入口探测页的链接不携带导航 state，与页头链接保持一致。 */
  omitLinkState?: boolean
}

function pad2(value: number): string {
  return String(value).padStart(2, '0')
}

// 按本地日历日分组，行内只写时分，和分组标题、全站时间戳的本地时区保持一致。
function dayKey(iso: string): string {
  const parsed = Date.parse(iso)
  if (Number.isNaN(parsed)) return '未知日期'
  const date = new Date(parsed)
  return `${date.getFullYear()}-${pad2(date.getMonth() + 1)}-${pad2(date.getDate())}`
}

// 记入时间与事件同一天时只写时分，跨日写完整日期时间。
function recordedText(eventAt: string, recordedAt: string): string {
  const day = dayKey(recordedAt)
  return day === dayKey(eventAt) ? clockText(recordedAt) : `${day.replaceAll('-', '/')} ${clockText(recordedAt)}`
}

function clockText(iso: string): string {
  const parsed = Date.parse(iso)
  if (Number.isNaN(parsed)) return iso
  const date = new Date(parsed)
  return `${pad2(date.getHours())}:${pad2(date.getMinutes())}`
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

function itemActionLabel(item: SubjectActivityItem, channel: TimelineChannel): string {
  if (channel === 'evidence') return '查看证据'
  if (item.revision_id?.trim()) return '查看修订'
  return '查看记录'
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

function sourceStatusLabel(status: SubjectActivitySourceStatus): string {
  const source = SOURCE_KIND_LABELS[status.source_kind] ?? status.source_kind
  const state = SOURCE_STATE_LABELS[status.state] ?? status.state
  return status.reason_code ? `${source}：${state}（${status.reason_code}）` : `${source}：${state}`
}

const ITEM_CLASSES: Record<TimelineChannel, string> = {
  human: 'unified-timeline__item unified-timeline__item--human',
  system: 'unified-timeline__item unified-timeline__item--system',
  evidence: 'unified-timeline__item unified-timeline__item--evidence',
}

const MARK_CLASSES: Record<TimelineChannel, string> = {
  human: 'unified-timeline__mark unified-timeline__mark--human',
  system: 'unified-timeline__mark unified-timeline__mark--system',
  evidence: 'unified-timeline__mark unified-timeline__mark--evidence',
}

export function UnifiedTimeline({
  items,
  sourceStatuses = [],
  emptyTitle = '暂无活动',
  emptyDescription = '当前筛选条件下没有可见活动。',
  itemActions,
  omitLinkState = false,
}: Props) {
  const location = useLocation()
  const state = omitLinkState ? undefined : location.state
  const idPrefix = useId()
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
            <li key={status.source_kind}>{sourceStatusLabel(status)}</li>
          ))}
        </ul>
      ) : null}
      {groupByDay(items).map((group) => (
        <section key={group.day} className="unified-timeline__day" aria-labelledby={`${idPrefix}-${group.day}`}>
          <h2 className="unified-timeline__day-title" id={`${idPrefix}-${group.day}`}>{group.day}</h2>
          <ol className="unified-timeline__list">
            {group.items.map((item) => {
              const channel = timelineChannel(item)
              const href = itemHref(item, channel)
              const meta = evidenceMeta(item)
              const summary = item.presentation.summary?.trim() ?? ''
              const detail = [summary, meta && meta !== summary ? meta : ''].filter(Boolean).join(' · ')
              const recordedDistinct = item.recorded_at.trim() && item.recorded_at !== item.event_at
              const extraActions = itemActions?.(item)
              return (
                <li key={item.activity_id} className={ITEM_CLASSES[channel]}>
                  <span className={MARK_CLASSES[channel]} aria-hidden />
                  <time className="unified-timeline__clock mono tnum" dateTime={item.event_at}>{clockText(item.event_at)}</time>
                  <div className="unified-timeline__main">
                    <div className="unified-timeline__line">
                      <h3 className="unified-timeline__title">{item.presentation.title}</h3>
                      <span className="unified-timeline__channel">{TIMELINE_CHANNEL_LABELS[channel]}</span>
                      {item.backfilled ? <span className="unified-timeline__tag">回填</span> : null}
                      {recordedDistinct ? (
                        <span className="unified-timeline__recorded">
                          记入 <time className="mono tnum" dateTime={item.recorded_at}>{recordedText(item.event_at, item.recorded_at)}</time>
                        </span>
                      ) : null}
                      {href || extraActions ? (
                        <span className="unified-timeline__actions">
                          {href ? (
                            <Link className="text-link" to={href} state={state}>
                              {itemActionLabel(item, channel)}
                            </Link>
                          ) : null}
                          {extraActions}
                        </span>
                      ) : null}
                    </div>
                    {detail ? <p className="unified-timeline__summary">{detail}</p> : null}
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
