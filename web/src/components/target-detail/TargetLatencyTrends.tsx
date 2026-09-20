import { useMemo, useState } from 'react'

import { MetricChart } from '../atoms/MetricChart'
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
import { describeProbeLatencyGap } from './probeObservationGap'

export type TargetLatencyTrend = {
  probeItemId: string
  probeKind: ProbeKind
  count: number
  distinctMonitoringInstanceCount: number
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
  samples: Array<{ value: number | null; observedAt: string }>
  latestLatency: number | null
  averageLatency: number | null
  maxLatency: number | null
  sampleCount: number
  distinctMonitoringInstanceCount: number
}

const CHART_HEIGHT = 140
const PLOT_GUTTER = 34

function deriveLatencyTrends(
  probeItems: ProbeItemRecord[],
  observations: ProbeObservation[],
): LatencyTrendCard[] {
  return probeItems
    .filter((item) => item.enabled)
    .map((item) => {
      const obs = observations
        .filter((o) => o.probe_item_id === item.probe_item_id)
        .sort(
          (a, b) =>
            new Date(a.observed_at).getTime() - new Date(b.observed_at).getTime(),
        )
      const latencies = obs
        .map((o) => o.latency_ms)
        .filter((value): value is number => value != null)
      const samples = obs.map((o) => ({
        value: o.latency_ms,
        observedAt: o.observed_at,
      }))
      const distinctMonitoringInstances = new Set(obs.map((o) => o.monitoring_instance_id))
      return {
        probeItemId: item.probe_item_id,
        kindLabel: `${item.probe_kind.toUpperCase()} · ${formatConfigSummary(item.config)}`,
        samples,
        latestLatency: samples.at(-1)?.value ?? null,
        averageLatency:
          latencies.length > 0
            ? latencies.reduce((acc, value) => acc + value, 0) / latencies.length
            : null,
        maxLatency: latencies.length > 0 ? Math.max(...latencies) : null,
        sampleCount: latencies.length,
        distinctMonitoringInstanceCount: distinctMonitoringInstances.size,
      }
    })
}

function formatTimeWindowLabel(timeWindow: string): string {
  return `近 ${timeWindow}`
}

function LatencyGapState({
  probeItems,
  observations,
  timeWindow,
}: {
  probeItems: ProbeItemRecord[]
  observations: ProbeObservation[]
  timeWindow: string
}) {
  const gap = describeProbeLatencyGap(probeItems, observations, timeWindow)
  return (
    <div className="empty-state">
      <h3>{gap.title}</h3>
      <p>{gap.description}</p>
    </div>
  )
}

function describeMeta(observations: ProbeObservation[], timeWindow: string): string {
  const timeWindowLabel = formatTimeWindowLabel(timeWindow)
  if (observations.length === 0) return `${timeWindowLabel} 暂无观测`
  const sorted = [...observations].sort(
    (a, b) =>
      new Date(a.observed_at).getTime() - new Date(b.observed_at).getTime(),
  )
  const oldest = sorted[0]
  const newest = sorted.at(-1)
  if (!oldest || !newest) return `${timeWindowLabel} 暂无观测`
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
}: TargetLatencyTrendsProps) {
  const [hoveredAt, setHoveredAt] = useState<string | null>(null)
  const trends = useMemo(
    () => deriveLatencyTrends(probeItems, recentObservations),
    [probeItems, recentObservations],
  )
  const meta = describeMeta(recentObservations, timeWindow)
  const hasAnySamples = trends.some((trend) => trend.sampleCount > 0)
  const gapState = (
    <LatencyGapState
      probeItems={probeItems}
      observations={recentObservations}
      timeWindow={timeWindow}
    />
  )

  return (
    <section aria-label="近期延迟趋势">
      {hasAnySamples ? (
        <p className="detail-section__aside-meta target-detail-latency__meta">{meta}</p>
      ) : null}
      {!hasAnySamples ? (
        gapState
      ) : (
        <div className="monitoring-detail-charts target-detail-latency__grid" data-layout="medium">
          {trends.map((trend) => {
            const yMax =
              trend.maxLatency == null ? 1 : Math.max(trend.maxLatency * 1.15, 10)
            const tone = isMaintenance ? 'maintenance' : 'accent'
            return (
              <article
                key={trend.probeItemId}
                className="monitoring-detail-chart"
                aria-label={trend.kindLabel}
              >
                <header className="monitoring-detail-chart__head">
                  <h3 className="monitoring-detail-chart__title">{trend.kindLabel}</h3>
                  <span className="monitoring-detail-chart__current">
                    <span className="monitoring-detail-chart__value">
                      <MonoDigits>{formatLatency(trend.latestLatency)}</MonoDigits>
                    </span>
                  </span>
                </header>
                <MetricChart
                  samples={trend.samples}
                  hoveredAt={hoveredAt}
                  onHoverAtChange={setHoveredAt}
                  allowSinglePoint
                  height={CHART_HEIGHT}
                  paddingLeft={PLOT_GUTTER}
                  yMin={0}
                  yMax={yMax}
                  tone={tone}
                  formatValue={(v) => formatLatency(v)}
                  formatAxisValue={(v) => formatLatency(Math.round(v))}
                  ariaLabel={`${trend.kindLabel} 延迟${formatTimeWindowLabel(timeWindow)}趋势`}
                />
                <dl className="monitoring-detail-chart__notes">
                  <div className="monitoring-detail-chart__note">
                    <dt>平均</dt>
                    <dd>
                      <MonoDigits>
                        {trend.averageLatency != null
                          ? formatLatency(Math.round(trend.averageLatency))
                          : '—'}
                      </MonoDigits>
                    </dd>
                  </div>
                  <div className="monitoring-detail-chart__note">
                    <dt>最大</dt>
                    <dd>
                      <MonoDigits>
                        {trend.maxLatency != null ? formatLatency(trend.maxLatency) : '—'}
                      </MonoDigits>
                    </dd>
                  </div>
                  <div className="monitoring-detail-chart__note">
                    <dt>样本数</dt>
                    <dd><MonoDigits>{trend.sampleCount}</MonoDigits></dd>
                  </div>
                  <div className="monitoring-detail-chart__note">
                    <dt>覆盖监控实例</dt>
                    <dd><MonoDigits>{trend.distinctMonitoringInstanceCount}</MonoDigits></dd>
                  </div>
                </dl>
              </article>
            )
          })}
        </div>
      )}
    </section>
  )
}
