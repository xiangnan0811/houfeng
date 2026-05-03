import { DetailSection } from '../DetailSection'
import { StatusBadge } from '../StatusBadge'
import { formatDateTime, formatLatency } from '../../lib/format'
import type { ProbeItemRecord, ProbeKind } from '../../lib/types'

export type TargetLatencyTrend = {
  probeItemId: string
  probeKind: ProbeKind
  count: number
  distinctNodeCount: number
  averageLatency: number
  maxLatency: number
  latestLatency: number
  newestObservedAt: string
  oldestObservedAt: string
}

type TargetLatencyTrendsProps = {
  trends: TargetLatencyTrend[]
  probeItemsById: Map<string, ProbeItemRecord>
}

export function TargetLatencyTrends({ trends, probeItemsById }: TargetLatencyTrendsProps) {
  return (
    <DetailSection
      eyebrow="近期延迟"
      title="近期延迟趋势"
      aside={
        trends.length > 0
          ? `最近样本更新到：${formatDateTime(trends[0].newestObservedAt)}`
          : '暂无可用延迟样本'
      }
    >
      {trends.length > 0 ? (
        <div className="probe-list">
          {trends.map((trend) => {
            const probeItem = probeItemsById.get(trend.probeItemId)
            return (
              <article key={trend.probeItemId} className="probe-card">
                <header className="probe-card__header">
                  <div>
                    <h3>
                      {probeItem?.probe_kind.toUpperCase() ?? trend.probeKind.toUpperCase()}
                    </h3>
                    <p>{trend.probeItemId}</p>
                  </div>
                  <div className="badge-row">
                    <StatusBadge label={`${trend.count} 次观测`} tone="cyan" />
                    <StatusBadge label={`${trend.distinctNodeCount} 个节点`} />
                  </div>
                </header>

                <dl className="probe-card__meta">
                  <div>
                    <dt>平均延迟</dt>
                    <dd>{formatLatency(Math.round(trend.averageLatency))}</dd>
                  </div>
                  <div>
                    <dt>最新延迟</dt>
                    <dd>{formatLatency(trend.latestLatency)}</dd>
                  </div>
                  <div>
                    <dt>最大延迟</dt>
                    <dd>{formatLatency(trend.maxLatency)}</dd>
                  </div>
                  <div>
                    <dt>样本窗口</dt>
                    <dd>
                      {formatDateTime(trend.oldestObservedAt)} →{' '}
                      {formatDateTime(trend.newestObservedAt)}
                    </dd>
                  </div>
                  <div>
                    <dt>观测次数</dt>
                    <dd>{trend.count}</dd>
                  </div>
                  <div>
                    <dt>覆盖节点</dt>
                    <dd>{trend.distinctNodeCount}</dd>
                  </div>
                  <div>
                    <dt>最近样本</dt>
                    <dd>{formatDateTime(trend.newestObservedAt)}</dd>
                  </div>
                </dl>
              </article>
            )
          })}
        </div>
      ) : (
        <div className="empty-state">
          <h3>暂无可用延迟样本</h3>
          <p>近期延迟趋势仅统计已返回成功且带延迟值的近期探测观测。</p>
        </div>
      )}
    </DetailSection>
  )
}
