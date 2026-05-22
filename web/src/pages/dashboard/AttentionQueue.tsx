import { Link } from 'react-router-dom'

import { Badge, Hostname, MonoDigits, StatusGlyph, Timestamp } from '../../components/atoms'
import { statusGlyph, statusTone } from './dashboardHelpers'
import type { AttentionItem } from './types'

const MAX_ATTENTION_ITEMS = 6

type AttentionQueueProps = {
  items: AttentionItem[]
}

export function AttentionQueue({ items }: AttentionQueueProps) {
  const visibleItems = items.slice(0, MAX_ATTENTION_ITEMS)

  return (
    <div className="dashboard-attention" aria-label="异常处理队列">
      <div className="dashboard-attention-list">
        {visibleItems.map((item, index) => (
          <article
            className={`dashboard-attention-item dashboard-attention-item--${statusTone(item.health)}`}
            key={`${item.kind}-${item.id}`}
          >
            <Link
              className="dashboard-attention-item__main"
              to={item.route}
              aria-label={`进入${item.kind === 'node' ? '节点' : '目标'} ${item.name}`}
            >
              <div className="dashboard-attention-item__status">
                <span className="dashboard-attention-item__rank">
                  P<MonoDigits>{index + 1}</MonoDigits>
                </span>
                <StatusGlyph
                  state={statusGlyph(item.health)}
                  size="md"
                  ariaLabel={`${item.name} 健康 ${item.health}`}
                />
              </div>
              <div className="dashboard-attention-item__identity">
                <h3 className="dashboard-attention-item__name">{item.name}</h3>
                <p className="dashboard-attention-item__meta">
                  {item.meta} · {item.location} · {item.freshnessLabel}{' '}
                  <Timestamp value={item.freshnessAt ?? null} mode="relative" />
                </p>
                <Hostname truncate maxChars={32} className="dashboard-attention-item__technical-id">
                  {item.technicalId}
                </Hostname>
              </div>
              <div className="dashboard-attention-item__issue">
                <Badge variant="state" tone={statusTone(item.health)} withDot>
                  {item.health}
                </Badge>
                <p>
                  <span className="dashboard-attention-item__issue-label">当前问题</span>
                  <span>
                    活跃问题 <MonoDigits>{item.incidentCount}</MonoDigits>
                  </span>
                  <strong>{item.issueSummary}</strong>
                </p>
              </div>
            </Link>
            <Link
              className="text-link dashboard-attention-item__link"
              to={item.route}
              aria-label={`查看${item.kind === 'node' ? '节点' : '目标'} ${item.name}`}
              onClick={(event) => event.stopPropagation()}
              onKeyDown={(event) => event.stopPropagation()}
            >
              处理
            </Link>
          </article>
        ))}
      </div>
      {items.length > visibleItems.length ? (
        <p className="dashboard-attention__limit">
          仅显示 P<MonoDigits>{visibleItems.length}</MonoDigits> 以内；完整队列进节点 / 目标 / 事件。
        </p>
      ) : null}
    </div>
  )
}
