import { Link } from 'react-router-dom'

import { Badge, Timestamp } from '../../components/atoms'
import { TIMELINE_CHANNEL_LABELS, timelineChannel } from '../../components/timelineChannel'
import type { SubjectActivityItem, VPSOverviewSectionState } from '../../lib/types'
import { VPSOverviewFreshness } from './VPSOverviewFreshness'

type Props = {
  items: SubjectActivityItem[]
  activityHref: string
  section: VPSOverviewSectionState
  onRefresh: () => void
  retrying: boolean
  /** Overview shows at most three recent rows on the decision surface. */
  limit?: number
}

export function VPSOverviewRecentActivity({
  items,
  activityHref,
  section,
  onRefresh,
  retrying,
  limit = 3,
}: Props) {
  const visible = items.slice(0, limit)

  return (
    <section className="vps-overview-recent" aria-labelledby="vps-overview-recent-title">
      <div className="vps-overview-recent__header">
        <h2 id="vps-overview-recent-title">最近活动</h2>
        <Link className="text-link" to={activityHref}>查看全部</Link>
      </div>
      <VPSOverviewFreshness
        section={section}
        sourceLabel="最近活动"
        onRetry={onRefresh}
        retrying={retrying}
      />
      {visible.length === 0 ? (
        <p className="vps-overview-recent__empty">
          {section.state === 'unavailable' ? '最近活动暂不可用，无法确认是否为空。' : '暂无最近活动'}
        </p>
      ) : (
        <ol className="vps-overview-recent__list">
          {visible.map((item) => (
            <li key={item.activity_id} className="vps-overview-recent__item">
              <div className="vps-overview-recent__item-main">
                <p className="vps-overview-recent__item-title">
                  <Badge variant="info">{TIMELINE_CHANNEL_LABELS[timelineChannel(item)]}</Badge>
                  {item.presentation.title}
                </p>
                <Timestamp value={item.event_at} mode="absolute" />
              </div>
            </li>
          ))}
        </ol>
      )}
    </section>
  )
}
