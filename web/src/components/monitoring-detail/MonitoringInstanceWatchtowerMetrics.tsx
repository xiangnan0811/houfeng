import type { ReactNode } from 'react'
import { Fragment, useState } from 'react'
import { MetricChart, type MetricChartSample } from '../atoms/MetricChart'
import { MonoDigits } from '../atoms/Mono'
import {
  formatBytes,
  formatBytesPerSecond,
  formatNumber,
  formatPercent,
} from '../../lib/format'
import type { HostMetricPoint, HostSample, MonitoringRuntimeWindow } from '../../lib/types'
import { DEFAULT_THRESHOLDS, type MetricThreshold, type MetricThresholds } from '../../config/thresholds'

type Props = {
  sample: HostSample | null
  metricPoints: HostMetricSeriesPoint[]
  timeWindow: MetricTimeWindow
  window?: MonitoringRuntimeWindow
  isMaintenance?: boolean
  thresholds?: MetricThresholds
}

type MetricTimeWindow = 'realtime' | '24h' | '7d' | '30d'

export type HostMetricSeriesPoint = Pick<
  HostMetricPoint,
  | 'observed_at'
  | 'cpu_usage_pct'
  | 'mem_used_pct'
  | 'disk_used_pct'
  | 'inode_used_pct'
  | 'load_5'
  | 'cpu_iowait_pct'
  | 'net_in_bytes_per_sec'
  | 'net_out_bytes_per_sec'
>

type MetricPriority = 0 | 1 | 2 | 3 // 0=normal, 1=warning, 2=alert, 3=critical
type MetricTone = 'normal' | 'notice' | 'alert' | 'critical'

interface MetricCardDef {
  id: string
  label: string
  priority: MetricPriority
  tone: MetricTone
  render: () => ReactNode
}

function priorityFromThresholds(value: number, thresholds: MetricThreshold): MetricPriority {
  if (value >= thresholds.critical) return 3
  if (value >= thresholds.alert) return 2
  if (value >= thresholds.warning) return 1
  return 0
}

function priorityTone(p: MetricPriority): MetricTone {
  if (p === 3) return 'critical'
  if (p === 2) return 'alert'
  if (p === 1) return 'notice'
  return 'normal'
}

function toAscending(samples: HostMetricSeriesPoint[]): HostMetricSeriesPoint[] {
  return [...samples].sort(
    (a, b) => new Date(a.observed_at).getTime() - new Date(b.observed_at).getTime(),
  )
}

function toSeries(
  samples: HostMetricSeriesPoint[],
  pick: (s: HostMetricSeriesPoint) => number,
): MetricChartSample[] {
  return samples.map((s) => ({ value: pick(s), observedAt: s.observed_at }))
}

function cardRibbonClass(priority: MetricPriority): string {
  if (priority === 3) return 'watchtower-metric-card--critical'
  if (priority === 2) return 'watchtower-metric-card--alert'
  if (priority === 1) return 'watchtower-metric-card--notice'
  return ''
}

function thresholdLines(thresholds: MetricThreshold, suffix = '') {
  return [
    { value: thresholds.warning, tone: 'notice' as const, label: `${thresholds.warning}${suffix}` },
    { value: thresholds.alert, tone: 'alert' as const, label: `${thresholds.alert}${suffix}` },
    { value: thresholds.critical, tone: 'critical' as const, label: `${thresholds.critical}${suffix}` },
  ]
}

function thresholdTitle(metric: string, thresholds: MetricThreshold, suffix: string, extra: string) {
  return `${metric}。正常：< ${thresholds.warning}${suffix}，关注：≥ ${thresholds.warning}${suffix}，告警：≥ ${thresholds.alert}${suffix}，严重：≥ ${thresholds.critical}${suffix}。${extra}`
}

function formatCapacityBytes(value?: number | null): string {
  if (value == null || !Number.isFinite(value) || value <= 0) return '—'
  return formatBytes(value)
}

function timeWindowLabel(timeWindow: MetricTimeWindow): string {
  if (timeWindow === 'realtime') return '实时'
  if (timeWindow === '24h') return '近 24h'
  if (timeWindow === '7d') return '近 7d'
  return '近 30d'
}

function availableWindowLabel(window?: MonitoringRuntimeWindow): string {
  if (!window?.available_started_at || !window.available_ended_at) return '暂无可用历史跨度'
  return `${new Date(window.available_started_at).toLocaleString()} - ${new Date(window.available_ended_at).toLocaleString()}`
}

export function MonitoringInstanceWatchtowerMetrics({
  sample,
  metricPoints,
  timeWindow,
  window,
  isMaintenance = false,
  thresholds = DEFAULT_THRESHOLDS,
}: Props) {
  const [hoveredAt, setHoveredAt] = useState<string | null>(null)

  if (!sample) {
    return (
      <div className="empty-state">
        <h3>尚未收到主机样本</h3>
        <p>该监控实例已存在，但首批主机采样（HostSample）还未到达。请等待下一次 agent 同步。</p>
      </div>
    )
  }

  const ascending = toAscending(metricPoints)
  const labelPrefix = timeWindowLabel(timeWindow)
  const baseTone = isMaintenance ? 'maintenance' : 'accent'
  const altTone = isMaintenance ? 'maintenance' : 'accent-2'
  const sharedChartProps = {
    hoveredAt,
    onHoverAtChange: setHoveredAt,
  }

  const t = thresholds
  const cpuPriority = priorityFromThresholds(sample.cpu_usage_pct, t.cpu)
  const memPriority = priorityFromThresholds(sample.mem_used_pct, t.mem)
  const diskPriority = priorityFromThresholds(sample.disk_used_pct, t.disk)
  const inodePriority = priorityFromThresholds(sample.inode_used_pct, t.inode)
  const iowaitPriority = priorityFromThresholds(sample.cpu_iowait_pct, t.iowait)
  const load5Priority = priorityFromThresholds(sample.load_5, t.load5)
  // Network metrics have no thresholds — always normal priority
  const netInPriority: MetricPriority = 0
  const netOutPriority: MetricPriority = 0

  const cards: MetricCardDef[] = [
    {
      id: 'cpu',
      label: 'CPU',
      priority: cpuPriority,
      tone: priorityTone(cpuPriority),
      render: () => (
        <article className={`watchtower-metric-card ${cardRibbonClass(cpuPriority)}`.trim()}>
          <header className="watchtower-metric-card__head">
            <h3 title={thresholdTitle('CPU 总体使用率', t.cpu, '%', '含 steal 时间占比。')}>CPU 使用率</h3>
            <span className="watchtower-metric-card__current">
              <MonoDigits>{formatPercent(sample.cpu_usage_pct)}</MonoDigits>
            </span>
          </header>
          <MetricChart
            samples={toSeries(ascending, (s) => s.cpu_usage_pct)}
            {...sharedChartProps}
            tone={cpuPriority > 0 ? priorityTone(cpuPriority) : baseTone}
            height={160}
            yMin={0}
            yMax={100}
            thresholds={thresholdLines(t.cpu, '%')}
            formatValue={(v) => formatPercent(v)}
            ariaLabel={`CPU 使用率${labelPrefix}趋势`}
          />
          <dl className="watchtower-metric-card__sub">
            <div>
              <dt>steal</dt>
              <dd>
                <MonoDigits>{formatPercent(sample.cpu_steal_pct)}</MonoDigits>
              </dd>
            </div>
          </dl>
        </article>
      ),
    },
    {
      id: 'mem',
      label: '内存',
      priority: memPriority,
      tone: priorityTone(memPriority),
      render: () => (
        <article className={`watchtower-metric-card ${cardRibbonClass(memPriority)}`.trim()}>
          <header className="watchtower-metric-card__head">
            <h3 title={thresholdTitle('内存总体使用率', t.mem, '%', '含 swap 和可用内存。')}>内存使用率</h3>
            <span className="watchtower-metric-card__current">
              <MonoDigits>{formatPercent(sample.mem_used_pct)}</MonoDigits>
            </span>
          </header>
          <MetricChart
            samples={toSeries(ascending, (s) => s.mem_used_pct)}
            {...sharedChartProps}
            tone={memPriority > 0 ? priorityTone(memPriority) : baseTone}
            height={160}
            yMin={0}
            yMax={100}
            thresholds={thresholdLines(t.mem, '%')}
            formatValue={(v) => formatPercent(v)}
            ariaLabel={`内存使用率${labelPrefix}趋势`}
          />
          <dl className="watchtower-metric-card__sub">
            <div>
              <dt>swap</dt>
              <dd>
                <MonoDigits>{formatPercent(sample.swap_used_pct)}</MonoDigits>
              </dd>
            </div>
            <div>
              <dt>可用</dt>
              <dd>
                <MonoDigits>{formatBytes(sample.mem_available_bytes)}</MonoDigits>
              </dd>
            </div>
            <div className="watchtower-metric-card__sub-item--end">
              <dt>总内存</dt>
              <dd>
                <MonoDigits>{formatCapacityBytes(sample.mem_total_bytes)}</MonoDigits>
              </dd>
            </div>
          </dl>
        </article>
      ),
    },
    {
      id: 'disk',
      label: '磁盘',
      priority: diskPriority,
      tone: priorityTone(diskPriority),
      render: () => (
        <article className={`watchtower-metric-card ${cardRibbonClass(diskPriority)}`.trim()}>
          <header className="watchtower-metric-card__head">
            <h3 title={thresholdTitle('磁盘空间使用率', t.disk, '%', '含 IO busy 和读写速率。')}>磁盘使用率</h3>
            <span className="watchtower-metric-card__current">
              <MonoDigits>{formatPercent(sample.disk_used_pct)}</MonoDigits>
            </span>
          </header>
          <MetricChart
            samples={toSeries(ascending, (s) => s.disk_used_pct)}
            {...sharedChartProps}
            tone={diskPriority > 0 ? priorityTone(diskPriority) : baseTone}
            height={160}
            yMin={0}
            yMax={100}
            thresholds={thresholdLines(t.disk, '%')}
            formatValue={(v) => formatPercent(v)}
            ariaLabel={`磁盘使用率${labelPrefix}趋势`}
          />
          <dl className="watchtower-metric-card__sub">
            <div>
              <dt>busy</dt>
              <dd>
                <MonoDigits>{formatPercent(sample.disk_busy_pct)}</MonoDigits>
              </dd>
            </div>
            <div>
              <dt>读 / 写</dt>
              <dd>
                <MonoDigits>
                  {formatBytesPerSecond(sample.disk_read_bytes_per_sec)} /{' '}
                  {formatBytesPerSecond(sample.disk_write_bytes_per_sec)}
                </MonoDigits>
              </dd>
            </div>
            <div className="watchtower-metric-card__sub-item--end">
              <dt>总磁盘</dt>
              <dd>
                <MonoDigits>{formatCapacityBytes(sample.disk_total_bytes)}</MonoDigits>
              </dd>
            </div>
          </dl>
        </article>
      ),
    },
    {
      id: 'inode',
      label: 'Inode',
      priority: inodePriority,
      tone: priorityTone(inodePriority),
      render: () => (
        <article className={`watchtower-metric-card ${cardRibbonClass(inodePriority)}`.trim()}>
          <header className="watchtower-metric-card__head">
            <h3 title={thresholdTitle('Inode 使用率', t.inode, '%', 'Inode 耗尽会导致无法创建新文件。')}>Inode 使用率</h3>
            <span className="watchtower-metric-card__current">
              <MonoDigits>{formatPercent(sample.inode_used_pct)}</MonoDigits>
            </span>
          </header>
          <MetricChart
            samples={toSeries(ascending, (s) => s.inode_used_pct)}
            {...sharedChartProps}
            tone={inodePriority > 0 ? priorityTone(inodePriority) : baseTone}
            height={160}
            yMin={0}
            yMax={100}
            thresholds={thresholdLines(t.inode, '%')}
            formatValue={(v) => formatPercent(v)}
            ariaLabel={`Inode 使用率${labelPrefix}趋势`}
          />
        </article>
      ),
    },
    {
      id: 'load5',
      label: 'Load5',
      priority: load5Priority,
      tone: priorityTone(load5Priority),
      render: () => (
        <article className={`watchtower-metric-card ${cardRibbonClass(load5Priority)}`.trim()}>
          <header className="watchtower-metric-card__head">
            <h3 title={thresholdTitle('系统 5 分钟负载均值', t.load5, '', '需结合 CPU 核数判断。')}>Load5</h3>
            <span className="watchtower-metric-card__current">
              <MonoDigits>{formatNumber(sample.load_5)}</MonoDigits>
            </span>
          </header>
          <MetricChart
            samples={toSeries(ascending, (s) => s.load_5)}
            {...sharedChartProps}
            tone={load5Priority > 0 ? priorityTone(load5Priority) : baseTone}
            height={160}
            yMin={0}
            thresholds={thresholdLines(t.load5)}
            formatValue={(v) => formatNumber(v)}
            ariaLabel={`Load5 ${labelPrefix}趋势`}
          />
          <dl className="watchtower-metric-card__sub">
            <div>
              <dt>load 1 / 15</dt>
              <dd>
                <MonoDigits>
                  {formatNumber(sample.load_1)} / {formatNumber(sample.load_15)}
                </MonoDigits>
              </dd>
            </div>
          </dl>
        </article>
      ),
    },
    {
      id: 'iowait',
      label: 'IOWait',
      priority: iowaitPriority,
      tone: priorityTone(iowaitPriority),
      render: () => (
        <article className={`watchtower-metric-card ${cardRibbonClass(iowaitPriority)}`.trim()}>
          <header className="watchtower-metric-card__head">
            <h3 title={thresholdTitle('CPU 等待 I/O 的时间占比', t.iowait, '%', '偏高通常意味着磁盘瓶颈。')}>CPU IOWait</h3>
            <span className="watchtower-metric-card__current">
              <MonoDigits>{formatPercent(sample.cpu_iowait_pct)}</MonoDigits>
            </span>
          </header>
          <MetricChart
            samples={toSeries(ascending, (s) => s.cpu_iowait_pct)}
            {...sharedChartProps}
            tone={iowaitPriority > 0 ? priorityTone(iowaitPriority) : baseTone}
            height={160}
            yMin={0}
            thresholds={thresholdLines(t.iowait, '%')}
            formatValue={(v) => formatPercent(v)}
            ariaLabel={`CPU IOWait ${labelPrefix}趋势`}
          />
        </article>
      ),
    },
    {
      id: 'net-in',
      label: '网络入',
      priority: netInPriority,
      tone: 'normal',
      render: () => (
        <article className="watchtower-metric-card">
          <header className="watchtower-metric-card__head">
            <h3 title="网络入站速率（B/s）。无固定阈值，关注异常波动。">网络入</h3>
            <span className="watchtower-metric-card__current">
              <MonoDigits>{formatBytesPerSecond(sample.net_in_bytes_per_sec)}</MonoDigits>
            </span>
          </header>
          <MetricChart
            samples={toSeries(ascending, (s) => s.net_in_bytes_per_sec)}
            {...sharedChartProps}
            tone={altTone}
            height={160}
            yMin={0}
            formatValue={(v) => formatBytesPerSecond(v)}
            ariaLabel={`网络入站${labelPrefix}趋势`}
          />
        </article>
      ),
    },
    {
      id: 'net-out',
      label: '网络出',
      priority: netOutPriority,
      tone: 'normal',
      render: () => (
        <article className="watchtower-metric-card">
          <header className="watchtower-metric-card__head">
            <h3 title="网络出站速率（B/s）。无固定阈值，关注异常波动。">网络出</h3>
            <span className="watchtower-metric-card__current">
              <MonoDigits>{formatBytesPerSecond(sample.net_out_bytes_per_sec)}</MonoDigits>
            </span>
          </header>
          <MetricChart
            samples={toSeries(ascending, (s) => s.net_out_bytes_per_sec)}
            {...sharedChartProps}
            tone={baseTone}
            height={160}
            yMin={0}
            formatValue={(v) => formatBytesPerSecond(v)}
            ariaLabel={`网络出站${labelPrefix}趋势`}
          />
        </article>
      ),
    },
  ]

  // Sort: highest priority first, then by id for stable order within same priority
  const sorted = [...cards].sort((a, b) => {
    if (b.priority !== a.priority) return b.priority - a.priority
    return a.id.localeCompare(b.id)
  })
  const topCard = sorted[0]

  return (
    <section className="watchtower-metrics-panel" aria-label="主机指标趋势">
      <div className="watchtower-metrics-panel__header">
        <div>
          <p className="watchtower-metrics-panel__eyebrow">Host Metrics</p>
          <h2>关键资源趋势</h2>
        </div>
        <p>
          {timeWindow === 'realtime'
            ? `实时滚动 ${ascending.length} 点`
            : `${labelPrefix} · ${window?.sample_count ?? 0} 个原始样本 · ${availableWindowLabel(window)}`}
          {' · 已按阈值优先级排序'}
          {topCard && topCard.priority > 0 ? <> · 首要关注 {topCard.label}</> : null}
        </p>
      </div>
      <div className="watchtower-metrics" role="group" aria-label="主机指标趋势">
        {sorted.map((card) => (
          <Fragment key={card.id}>{card.render()}</Fragment>
        ))}
      </div>
    </section>
  )
}
