import { Link, useLocation } from 'react-router-dom'

import { Timestamp } from '../../components/atoms'
import { TIMELINE_CHANNEL_LABELS, timelineChannel } from '../../components/timelineChannel'
import type { SubjectActivityItem, VPSOverviewSectionState } from '../../lib/types'
import {
  overviewActivityForeignNames,
  overviewSameAssetActionTitle,
} from '../../lib/vpsOverviewPresentation'
import { VPSOverviewFreshness } from './VPSOverviewFreshness'
import { overviewSourceReadKind } from './vpsOverviewFreshness'

type Props = {
  items: SubjectActivityItem[]
  activityHref: string
  section: VPSOverviewSectionState
  onRefresh: () => void
  retrying: boolean
  /** Overview shows at most three recent rows on the decision surface. */
  limit?: number
  heading?: string
  assetName?: string
  vpsId?: string
}

export function VPSOverviewRecentActivity({
  items,
  activityHref,
  section,
  onRefresh,
  retrying,
  limit = 3,
  heading = '最近活动',
  assetName = '',
  vpsId = '',
}: Props) {
  const location = useLocation()
  const visible = items.slice(0, limit)
  const readKind = overviewSourceReadKind(section)
  const failedRead = readKind === 'timeout' || readKind === 'unavailable'
  const needsRefresh = section.state === 'stale' || section.state === 'unavailable'
  const retained = visible.length > 0 && failedRead

  return (
    <section className="vps-overview-recent" aria-label={heading || '最近活动'}>
      <div className="vps-overview-recent__header">
        {heading ? <h2 id="vps-overview-recent-title">{heading}</h2> : null}
        <Link className="text-link" to={activityHref} state={location.state}>查看全部</Link>
      </div>
      <VPSOverviewFreshness
        section={section}
        sourceLabel="最近活动"
        {...(needsRefresh ? { onRetry: onRefresh } : {})}
        retrying={retrying}
        omitReadyTime
        retained={retained}
        timeLabel="活动源最新入库"
      />

      {visible.length === 0 ? (
        <p className="vps-overview-recent__empty">
          {section.state === 'unavailable' ? '最近活动暂不可用，无法确认是否为空。' : '暂无最近活动'}
        </p>
      ) : (
        <ol className="vps-overview-recent__list">
          {visible.map((item) => (
            <li key={item.activity_id} className="vps-overview-recent__item">
              <span className="vps-overview-recent__marker" aria-hidden="true" />
              <div className="vps-overview-recent__item-main">
                <ActivityTitle item={item} assetName={assetName} vpsId={vpsId} />
                <p className="vps-overview-recent__meta">
                  <span className="vps-overview-recent__clock">
                    <span className="vps-overview-recent__clock-label">事件时间</span>
                    {' '}
                    <Timestamp value={item.event_at} mode="absolute" />
                  </span>
                  <span className="vps-overview-recent__source">{TIMELINE_CHANNEL_LABELS[timelineChannel(item)]}</span>
                </p>
              </div>
            </li>
          ))}
        </ol>
      )}
    </section>
  )
}

function ActivityTitle({
  item,
  assetName,
  vpsId,
}: {
  item: SubjectActivityItem
  assetName: string
  vpsId: string
}) {
  const full = item.presentation.title
  const visible = overviewSameAssetActionTitle(
    full,
    assetName,
    vpsId ? overviewActivityForeignNames(item.subjects, { vpsId, displayName: assetName }) : [],
  )
  return (
    <p
      className="vps-overview-recent__item-title"
      {...(visible !== full ? { title: full, 'aria-label': full } : {})}
    >
      {visible}
    </p>
  )
}
