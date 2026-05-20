import { DetailSection } from '../DetailSection'
import { Sparkline, type SparklineSample } from '../atoms/Sparkline'
import { MonoDigits } from '../atoms/Mono'
import {
  formatConfigSummary,
  formatDateTime,
  formatLatency,
} from '../../lib/format'
import type {
  ProbeItemRecord,
  ProbeKind,
  ProbeObservation,
} from '../../lib/types'

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
  probeItems: ProbeItemRecord[]
  recentObservations: ProbeObservation[]
  timeWindow?: '24h' | '7d' | '30d'
  isMaintenance?: boolean
  watchtower?: boolean
}

type LatencyTrendCard = {
  probeItemId: string
  kindLabel: string
  samples: SparklineSample[]
  latestLatency: number | null
  averageLatency: number | null
  maxLatency: number | null
  sampleCount: number
  distinctNodeCount: number
}

function deriveLatencyTrends(
  probeItems: ProbeItemRecord[],
  observations: ProbeObservation[],
): LatencyTrendCard[] {
  return probeItems
    .filter((item) => item.enabled)
    .map((item) => {
      const obs = observations
        .filter(
          (o) => o.probe_item_id === item.probe_item_id && o.latency_ms != null,
        )
        .sort(
          (a, b) =>
            new Date(a.observed_at).getTime() - new Date(b.observed_at).getTime(),
        )

      const latencies = obs.map((o) => o.latency_ms as number)
      const samples: SparklineSample[] = obs.map((o) => ({
        value: o.latency_ms as number,
        observedAt: o.observed_at,
      }))
      const distinctNodes = new Set(obs.map((o) => o.node_id))

      const latestLatency =
        latencies.length > 0 ? latencies[latencies.length - 1] : null
      const averageLatency =
        latencies.length > 0
          ? latencies.reduce((acc, value) => acc + value, 0) / latencies.length
          : null
      const maxLatency = latencies.length > 0 ? Math.max(...latencies) : null

      return {
        probeItemId: item.probe_item_id,
        kindLabel: `${item.probe_kind.toUpperCase()} · ${formatConfigSummary(item.config)}`,
        samples,
        latestLatency,
        averageLatency,
        maxLatency,
        sampleCount: latencies.length,
        distinctNodeCount: distinctNodes.size,
      }
    })
}

function formatTimeWindowLabel(timeWindow: string): string {
  return `近 ${timeWindow}`
}

function describeMeta(observations: ProbeObservation[], timeWindow: string): string {
  const timeWindowLabel = formatTimeWindowLabel(timeWindow)
  if (observations.length === 0) return `${timeWindowLabel} 暂无观测`
  const sorted = [...observations].sort(
    (a, b) =>
      new Date(a.observed_at).getTime() - new Date(b.observed_at).getTime(),
  )
  const oldest = sorted[0]
  const newest = sorted[sorted.length - 1]
  const backfillCount = observations.filter((o) => o.is_backfilled).length
  const parts = [
    `${timeWindowLabel} ${observations.length} 样本`,
    `最早 ${formatDateTime(oldest.observed_at)}`,
    `最新 ${formatDateTime(newest.observed_at)}`,
  ]
  if (backfillCount > 0) parts.push(`backfill ${backfillCount}`)
  return parts.join(' · ')
}

export function TargetLatencyTrends({
  probeItems,
  recentObservations,
  timeWindow = '24h',
  isMaintenance = false,
  watchtower = false,
}: TargetLatencyTrendsProps) {
  const trends = deriveLatencyTrends(probeItems, recentObservations)
  const meta = describeMeta(recentObservations, timeWindow)
  const tone = isMaintenance ? 'maintenance' : 'accent'

  if (watchtower) {
    return (
      <section aria-label="近期延迟趋势">
        {trends.length === 0 ? (
          <div className="empty-state">
            <h3>{formatTimeWindowLabel(timeWindow)} 暂无可用延迟样本</h3>
            <p>
              该目标尚未收到带有 latency_ms 的成功观测，或所有 ProbeItem 当前均处于停用状态。
            </p>
          </div>
        ) : (
          <>
            <p className="watchtower-metrics-meta">{meta}</p>
            <div className="watchtower-metrics">
              {trends.map((trend) => (
                <article key={trend.probeItemId} className="watchtower-metric-card">
                  <header className="watchtower-metric-card__head">
                    <h3>{trend.kindLabel}</h3>
                    <span className="watchtower-metric-card__current">
                      {trend.latestLatency != null ? (
                        <MonoDigits>{formatLatency(trend.latestLatency)}</MonoDigits>
                      ) : (
                        '—'
                      )}
                    </span>
                  </header>
                  <Sparkline
                    samples={trend.samples}
                    tone={tone}
                    height={60}
                    expand
                    interactive
                    ariaLabel={`${trend.kindLabel} 延迟${formatTimeWindowLabel(timeWindow)} 趋势`}
                    formatValue={(v) => formatLatency(v)}
                  />
                  <dl className="watchtower-metric-card__sub">
                    <div>
                      <dt>平均</dt>
                      <dd>
                        {trend.averageLatency != null ? (
                          <MonoDigits>
                            {formatLatency(Math.round(trend.averageLatency))}
                          </MonoDigits>
                        ) : (
                          '—'
                        )}
                      </dd>
                    </div>
                    <div>
                      <dt>最大</dt>
                      <dd>
                        {trend.maxLatency != null ? (
                          <MonoDigits>{formatLatency(trend.maxLatency)}</MonoDigits>
                        ) : (
                          '—'
                        )}
                      </dd>
                    </div>
                    <div>
                      <dt>样本数</dt>
                      <dd>
                        <MonoDigits>{trend.sampleCount}</MonoDigits>
                      </dd>
                    </div>
                    <div>
                      <dt>覆盖节点</dt>
                      <dd>
                        <MonoDigits>{trend.distinctNodeCount}</MonoDigits>
                      </dd>
                    </div>
                  </dl>
                </article>
              ))}
            </div>
          </>
        )}
      </section>
    )
  }

  const ribbon = isMaintenance ? 'maintenance' : undefined

  return (
    <DetailSection
      eyebrow="近期延迟"
      title="近期延迟趋势"
      ribbon={ribbon}
      aside={<span className="detail-section__aside-meta">{meta}</span>}
    >
      {trends.length === 0 ? (
        <div className="empty-state">
          <h3>{formatTimeWindowLabel(timeWindow)} 暂无可用延迟样本</h3>
          <p>
            该目标尚未收到带有 latency_ms 的成功观测，或所有 ProbeItem 当前均处于停用状态。
          </p>
        </div>
      ) : (
        <div className="metric-grid">
          {trends.map((trend) => (
            <article key={trend.probeItemId} className="metric-card">
              <header className="metric-card__head">
                <h3>{trend.kindLabel}</h3>
                <span className="metric-card__current">
                  {trend.latestLatency != null ? (
                    <MonoDigits>{formatLatency(trend.latestLatency)}</MonoDigits>
                  ) : (
                    <span className="metric-card__current-empty">—</span>
                  )}
                </span>
              </header>
              <Sparkline
                samples={trend.samples}
                tone={tone}
                height={60}
                expand
                interactive
                ariaLabel={`${trend.kindLabel} 延迟${formatTimeWindowLabel(timeWindow)} 趋势`}
                formatValue={(v) => formatLatency(v)}
              />
              <dl>
                <div>
                  <dt>平均</dt>
                  <dd>
                    {trend.averageLatency != null ? (
                      <MonoDigits>
                        {formatLatency(Math.round(trend.averageLatency))}
                      </MonoDigits>
                    ) : (
                      '—'
                    )}
                  </dd>
                </div>
                <div>
                  <dt>最大</dt>
                  <dd>
                    {trend.maxLatency != null ? (
                      <MonoDigits>{formatLatency(trend.maxLatency)}</MonoDigits>
                    ) : (
                      '—'
                    )}
                  </dd>
                </div>
                <div>
                  <dt>样本数</dt>
                  <dd>
                    <MonoDigits>{trend.sampleCount}</MonoDigits>
                  </dd>
                </div>
                <div>
                  <dt>覆盖节点</dt>
                  <dd>
                    <MonoDigits>{trend.distinctNodeCount}</MonoDigits>
                  </dd>
                </div>
              </dl>
            </article>
          ))}
        </div>
      )}
    </DetailSection>
  )
}
