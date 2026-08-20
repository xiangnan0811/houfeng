import { Link } from 'react-router-dom'

import { Timestamp } from '../../components/atoms'
import type { SubjectActivityItem } from '../../lib/types'

type Props = {
  items: SubjectActivityItem[]
  activityHref: string
  /** Overview shows at most three recent rows on the decision surface. */
  limit?: number
}

export function VPSOverviewRecentActivity({
  items,
  activityHref,
  limit = 3,
}: Props) {
  const visible = items.slice(0, limit)

  return (
    <section className="vps-overview-recent" aria-labelledby="vps-overview-recent-title">
      <div className="vps-overview-recent__header">
        <h2 id="vps-overview-recent-title">最近活动</h2>
        <Link className="text-link" to={activityHref}>查看全部</Link>
      </div>
      {visible.length === 0 ? (
        <p className="vps-overview-recent__empty">暂无最近活动</p>
      ) : (
        <ol className="vps-overview-recent__list">
          {visible.map((item) => (
            <li key={item.activity_id} className="vps-overview-recent__item">
              <div className="vps-overview-recent__item-main">
                <p className="vps-overview-recent__item-title">{item.presentation.title}</p>
                <Timestamp value={item.event_at} mode="absolute" />
              </div>
            </li>
          ))}
        </ol>
      )}
    </section>
  )
}
