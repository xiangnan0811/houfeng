import { Link } from 'react-router-dom'

import { Timestamp } from '../../components/atoms'
import type { ContextItem } from './types'

type DashboardContextStripProps = {
  items: ContextItem[]
}

export function DashboardContextStrip({ items }: DashboardContextStripProps) {
  return (
    <div className="dashboard-context-strip" aria-label="运行上下文">
      {items.map((item) => (
        <Link
          className={`dashboard-context-item${item.tone ? ` dashboard-context-item--${item.tone}` : ''}`}
          to={item.to}
          key={item.label}
          aria-label={`${item.label}：${item.detail}`}
        >
          <span className="dashboard-context-item__label">{item.label}</span>
          <strong className="dashboard-context-item__title">{item.title}</strong>
          <span className="dashboard-context-item__detail">
            {item.detail}
            {item.timestampAt ? (
              <>
                {' · '}
                <Timestamp value={item.timestampAt} mode="relative" />
              </>
            ) : null}
          </span>
        </Link>
      ))}
    </div>
  )
}
