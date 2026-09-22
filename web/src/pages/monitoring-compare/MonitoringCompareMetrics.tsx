import { useMemo, useState, type ReactNode } from 'react'

import { MetricChart, type MetricChartThreshold } from '../../components/atoms/MetricChart'
import { MonoDigits } from '../../components/atoms/Mono'
import {
  seriesMax,
  seriesValueAt,
  toAscending,
  toSeries,
  type HostMetricSeriesPoint,
} from '../../components/monitoring-detail/metricSeries'
import { DEFAULT_THRESHOLDS, type MetricThreshold } from '../../config/thresholds'
import { formatNumber, formatPercent } from '../../lib/format'
import type { MonitoringRuntimeWindow } from '../../lib/types'
import { niceMax } from '../monitoring-detail/monitoringDetailScale'

type TileTone = 'primary' | 'notice' | 'alert' | 'critical'

function tileTone(value: number | null, thresholds: MetricThreshold): TileTone {
  if (value == null || !Number.isFinite(value)) return 'primary'
  if (value >= thresholds.critical) return 'critical'
  if (value >= thresholds.alert) return 'alert'
  if (value >= thresholds.warning) return 'notice'
  return 'primary'
}

function scaledThresholdLines(thresholds: MetricThreshold, yMax: number): MetricChartThreshold[] {
  const candidates = ([
    { value: thresholds.warning, tone: 'notice' as const },
    { value: thresholds.alert, tone: 'alert' as const },
    { value: thresholds.critical, tone: 'critical' as const },
  ]).filter((candidate) => candidate.value < yMax * 0.98)
  return candidates.map((candidate, index) => ({
    ...candidate,
    label: index === candidates.length - 1 ? `告警 ${candidate.value}` : '',
  }))
}

function seriesLegend(...items: Array<{ label: string; swatch: 'down' | 'up' | 'tertiary' }>): ReactNode {
  return items.map((item) => (
    <span key={item.label} className="monitoring-detail-chart__legend-item">
      <i
        className={`monitoring-detail-chart__legend-swatch monitoring-detail-chart__legend-swatch--${item.swatch}`}
        aria-hidden
      />
      {item.label}
    </span>
  ))
}

function valueClass(tone: TileTone): string {
  return `monitoring-detail-chart__value monitoring-detail-chart__value--${tone}`
}

const CHART_HEIGHT = 120
const PLOT_GUTTER = 34

type Props = {
  metricPoints: HostMetricSeriesPoint[]
  window?: MonitoringRuntimeWindow
}

export function MonitoringCompareMetrics({ metricPoints, window: runtimeWindow }: Props) {
  const [hoveredAt, setHoveredAt] = useState<string | null>(null)
  const ascending = useMemo(() => toAscending(metricPoints), [metricPoints])
  const cpuSeries = useMemo(() => toSeries(ascending, (s) => s.cpu_usage_pct), [ascending])
  const memSeries = useMemo(() => toSeries(ascending, (s) => s.mem_used_pct), [ascending])
  const swapSeries = useMemo(() => toSeries(ascending, (s) => s.swap_used_pct ?? null), [ascending])
  const diskSeries = useMemo(() => toSeries(ascending, (s) => s.disk_used_pct), [ascending])
  const loadSeries = useMemo(() => toSeries(ascending, (s) => s.load_5), [ascending])
  const load1Series = useMemo(() => toSeries(ascending, (s) => s.load_1 ?? null), [ascending])
  const load15Series = useMemo(() => toSeries(ascending, (s) => s.load_15 ?? null), [ascending])

  const empty =
    ascending.length === 0 ||
    (runtimeWindow !== undefined ? runtimeWindow.sample_count === 0 : false)

  const cpuReadout = seriesValueAt(cpuSeries, hoveredAt)
  const memReadout = seriesValueAt(memSeries, hoveredAt)
  const swapReadout = seriesValueAt(swapSeries, hoveredAt)
  const diskReadout = seriesValueAt(diskSeries, hoveredAt)
  const loadReadout = seriesValueAt(loadSeries, hoveredAt)
  const cpuTone = tileTone(cpuReadout, DEFAULT_THRESHOLDS.cpu)
  const memTone = tileTone(memReadout, DEFAULT_THRESHOLDS.mem)
  const diskTone = tileTone(diskReadout, DEFAULT_THRESHOLDS.disk)
  const loadTone = tileTone(loadReadout, DEFAULT_THRESHOLDS.load5)
  const loadDataMax = seriesMax([...load1Series, ...loadSeries, ...load15Series])
  const loadYMax = loadDataMax === undefined ? undefined : niceMax(loadDataMax * 1.15)

  const shared = {
    hoveredAt,
    onHoverAtChange: setHoveredAt,
    allowSinglePoint: true,
    height: CHART_HEIGHT,
    paddingLeft: PLOT_GUTTER,
    yMin: 0,
    tone: 'accent' as const,
  }

  return (
    <div className="monitoring-compare-metrics" role="group" aria-label="近 24h 资源趋势">
      {empty ? (
        <p className="monitoring-compare-metrics__empty" role="status">该窗口没有样本</p>
      ) : null}
      <div className="monitoring-detail-charts monitoring-compare-metrics__grid" data-layout="medium">
        <article className={`monitoring-detail-chart monitoring-detail-chart--${cpuTone}`} aria-label="CPU 使用率">
          <header className="monitoring-detail-chart__head">
            <h3 className="monitoring-detail-chart__title">CPU 使用率</h3>
            <span className="monitoring-detail-chart__current">
              <span className={valueClass(cpuTone)}>
                <MonoDigits>{formatPercent(cpuReadout)}</MonoDigits>
              </span>
            </span>
          </header>
          <MetricChart
            samples={cpuSeries}
            {...shared}
            yMax={100}
            alertBandFrom={DEFAULT_THRESHOLDS.cpu.warning}
            formatValue={(v) => formatPercent(v)}
            formatAxisValue={(v) => formatPercent(v, 0)}
            ariaLabel="CPU 使用率近 24h趋势"
          />
        </article>
        <article className={`monitoring-detail-chart monitoring-detail-chart--${memTone}`} aria-label="内存使用率">
          <header className="monitoring-detail-chart__head">
            <h3 className="monitoring-detail-chart__title">内存使用率</h3>
            <span className="monitoring-detail-chart__legend">
              {seriesLegend({ label: '使用率', swatch: 'down' }, { label: '交换', swatch: 'up' })}
            </span>
            <span className="monitoring-detail-chart__current">
              <span className={valueClass(memTone)}>
                <MonoDigits>
                  {formatPercent(memReadout)}
                  {swapReadout != null ? ` · ${formatPercent(swapReadout)}` : ''}
                </MonoDigits>
              </span>
            </span>
          </header>
          <MetricChart
            samples={memSeries}
            secondarySamples={swapSeries}
            secondaryTone="accent-2"
            {...shared}
            yMax={100}
            alertBandFrom={DEFAULT_THRESHOLDS.mem.warning}
            formatValue={(v) => formatPercent(v)}
            formatAxisValue={(v) => formatPercent(v, 0)}
            ariaLabel="内存使用率近 24h趋势"
          />
        </article>
        <article className={`monitoring-detail-chart monitoring-detail-chart--${diskTone}`} aria-label="磁盘使用率">
          <header className="monitoring-detail-chart__head">
            <h3 className="monitoring-detail-chart__title">磁盘使用率</h3>
            <span className="monitoring-detail-chart__current">
              <span className={valueClass(diskTone)}>
                <MonoDigits>{formatPercent(diskReadout)}</MonoDigits>
              </span>
            </span>
          </header>
          <MetricChart
            samples={diskSeries}
            {...shared}
            yMax={100}
            alertBandFrom={DEFAULT_THRESHOLDS.disk.warning}
            formatValue={(v) => formatPercent(v)}
            formatAxisValue={(v) => formatPercent(v, 0)}
            ariaLabel="磁盘使用率近 24h趋势"
          />
        </article>
        <article className={`monitoring-detail-chart monitoring-detail-chart--${loadTone}`} aria-label="负载">
          <header className="monitoring-detail-chart__head">
            <h3 className="monitoring-detail-chart__title">负载</h3>
            <span className="monitoring-detail-chart__legend">
              {seriesLegend(
                { label: '5 分钟', swatch: 'down' },
                { label: '1 分钟', swatch: 'up' },
                { label: '15 分钟', swatch: 'tertiary' },
              )}
            </span>
            <span className="monitoring-detail-chart__current">
              <span className={valueClass(loadTone)}>
                <MonoDigits>{formatNumber(loadReadout)}</MonoDigits>
              </span>
            </span>
          </header>
          <MetricChart
            samples={loadSeries}
            secondarySamples={load1Series}
            secondaryTone="accent-2"
            tertiarySamples={load15Series}
            tertiaryTone="muted"
            {...shared}
            {...(loadYMax === undefined ? {} : { yMax: loadYMax })}
            {...(loadYMax === undefined ? {} : { thresholds: scaledThresholdLines(DEFAULT_THRESHOLDS.load5, loadYMax) })}
            formatValue={(v) => formatNumber(v)}
            formatAxisValue={(v) => formatNumber(v)}
            ariaLabel="1 / 5 / 15 分钟负载近 24h趋势"
          />
        </article>
      </div>
    </div>
  )
}
