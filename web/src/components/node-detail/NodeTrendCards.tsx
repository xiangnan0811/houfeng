import { Sparkline } from '../atoms'
import { DetailSection } from '../DetailSection'
import { formatDateTime, formatNumber, formatPercent } from '../../lib/format'

export type NodeRecentTrend = {
  count: number
  newestObservedAt: string
  oldestObservedAt: string
  averageLoad5: number | null
  averageIowait: number | null
  averageSteal: number | null
  latestLoad5: number
  latestIowait: number
  latestSteal: number
  load5Series: number[]
  iowaitSeries: number[]
  stealSeries: number[]
}

type NodeTrendCardsProps = {
  recentTrend: NodeRecentTrend | null
}

export function NodeTrendCards({ recentTrend }: NodeTrendCardsProps) {
  return (
    <DetailSection
      eyebrow="近期趋势"
      title="近期趋势"
      aside={recentTrend ? `最新样本：${formatDateTime(recentTrend.newestObservedAt)}` : '近 24h 暂无样本'}
    >
      {recentTrend ? (
        <div className="metric-grid">
          <article className="metric-card">
            <h3>样本概览</h3>
            <dl>
              <div>
                <dt>近 24h 样本</dt>
                <dd>{recentTrend.count}</dd>
              </div>
              <div>
                <dt>最早观测</dt>
                <dd>{formatDateTime(recentTrend.oldestObservedAt)}</dd>
              </div>
              <div>
                <dt>最新观测</dt>
                <dd>{formatDateTime(recentTrend.newestObservedAt)}</dd>
              </div>
            </dl>
          </article>

          <article className="metric-card">
            <h3>Load5 趋势</h3>
            <dl>
              <div>
                <dt>Load5 平均</dt>
                <dd>{formatNumber(recentTrend.averageLoad5)}</dd>
              </div>
              <div>
                <dt>最新 Load5</dt>
                <dd>{formatNumber(recentTrend.latestLoad5)}</dd>
              </div>
            </dl>
            <Sparkline values={recentTrend.load5Series} tone="accent" width={140} height={28} ariaLabel="Load5 近 24h 趋势" />
          </article>

          <article className="metric-card">
            <h3>CPU 等待</h3>
            <dl>
              <div>
                <dt>iowait 平均</dt>
                <dd>{formatPercent(recentTrend.averageIowait)}</dd>
              </div>
              <div>
                <dt>最新 iowait</dt>
                <dd>{formatPercent(recentTrend.latestIowait)}</dd>
              </div>
            </dl>
            <Sparkline values={recentTrend.iowaitSeries} tone="alert" width={140} height={28} ariaLabel="iowait 近 24h 趋势" />
          </article>

          <article className="metric-card">
            <h3>CPU steal 指标</h3>
            <dl>
              <div>
                <dt>steal 平均</dt>
                <dd>{formatPercent(recentTrend.averageSteal)}</dd>
              </div>
              <div>
                <dt>最新 steal</dt>
                <dd>{formatPercent(recentTrend.latestSteal)}</dd>
              </div>
            </dl>
            <Sparkline values={recentTrend.stealSeries} tone="critical" width={140} height={28} ariaLabel="CPU steal 近 24h 趋势" />
          </article>
        </div>
      ) : (
        <div className="empty-state">
          <h3>近 24h 暂无样本</h3>
          <p>近期趋势需要近 24h 主机采样数据，当前还没有可用样本。</p>
        </div>
      )}
    </DetailSection>
  )
}
