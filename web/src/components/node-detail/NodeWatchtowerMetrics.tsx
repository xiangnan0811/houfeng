import type { ReactNode } from 'react'
import { Fragment } from 'react'
import { MetricChart, type MetricChartSample } from '../atoms/MetricChart'
import { MonoDigits } from '../atoms/Mono'
import {
  formatBytes,
  formatBytesPerSecond,
  formatNumber,
  formatPercent,
} from '../../lib/format'
import type { HostSample } from '../../lib/types'
import { DEFAULT_THRESHOLDS } from '../../config/thresholds'

type Props = {
  sample: HostSample | null
  samples: HostSample[]
  isMaintenance?: boolean
}

type MetricPriority = 0 | 2 | 3 // 0=normal, 2=notice, 3=critical
type MetricTone = 'normal' | 'notice' | 'critical'

interface MetricCardDef {
  id: string
  priority: MetricPriority
  tone: MetricTone
  render: () => ReactNode
}

function priorityFromThresholds(value: number, thresholds: { value: number; tone: string }[]): MetricPriority {
  let p: MetricPriority = 0
  for (const t of thresholds) {
    if (value >= t.value) {
      if (t.tone === 'critical') p = 3
      else if (t.tone === 'notice' && p < 2) p = 2
    }
  }
  return p
}

function priorityTone(p: MetricPriority): MetricTone {
  if (p === 3) return 'critical'
  if (p === 2) return 'notice'
  return 'normal'
}

function toAscending(samples: HostSample[]): HostSample[] {
  return [...samples].sort(
    (a, b) => new Date(a.observed_at).getTime() - new Date(b.observed_at).getTime(),
  )
}

function toSeries(
  samples: HostSample[],
  pick: (s: HostSample) => number,
): MetricChartSample[] {
  return samples.map((s) => ({ value: pick(s), observedAt: s.observed_at }))
}

function cardRibbonClass(priority: MetricPriority): string {
  if (priority === 3) return 'watchtower-metric-card--critical'
  if (priority === 2) return 'watchtower-metric-card--notice'
  return ''
}

export function NodeWatchtowerMetrics({ sample, samples, isMaintenance = false }: Props) {
  if (!sample) {
    return (
      <div className="empty-state">
        <h3>尚未收到主机样本</h3>
        <p>该节点已存在，但首批主机采样（HostSample）还未到达。请等待下一次 agent 同步。</p>
      </div>
    )
  }

  const ascending = toAscending(samples)
  const baseTone = isMaintenance ? 'maintenance' : 'accent'
  const altTone = isMaintenance ? 'maintenance' : 'accent-2'

  // Compute priorities for each metric using default thresholds
  const t = DEFAULT_THRESHOLDS
  const cpuPriority = priorityFromThresholds(sample.cpu_usage_pct, [
    { value: t.cpu.notice, tone: 'notice' }, { value: t.cpu.critical, tone: 'critical' },
  ])
  const memPriority = priorityFromThresholds(sample.mem_used_pct, [
    { value: t.mem.notice, tone: 'notice' }, { value: t.mem.critical, tone: 'critical' },
  ])
  const diskPriority = priorityFromThresholds(sample.disk_used_pct, [
    { value: t.disk.notice, tone: 'notice' }, { value: t.disk.critical, tone: 'critical' },
  ])
  const inodePriority = priorityFromThresholds(sample.inode_used_pct, [
    { value: t.inode.notice, tone: 'notice' }, { value: t.inode.critical, tone: 'critical' },
  ])
  const iowaitPriority = priorityFromThresholds(sample.cpu_iowait_pct, [
    { value: t.iowait.notice, tone: 'notice' }, { value: t.iowait.critical, tone: 'critical' },
  ])
  const load5Priority = priorityFromThresholds(sample.load_5, [
    { value: t.load5.notice, tone: 'notice' }, { value: t.load5.critical, tone: 'critical' },
  ])
  // Network metrics have no thresholds — always normal priority
  const netInPriority: MetricPriority = 0
  const netOutPriority: MetricPriority = 0

  const cards: MetricCardDef[] = [
    {
      id: 'cpu',
      priority: cpuPriority,
      tone: priorityTone(cpuPriority),
      render: () => (
        <article className={`watchtower-metric-card ${cardRibbonClass(cpuPriority)}`.trim()}>
          <header className="watchtower-metric-card__head">
            <h3 title="CPU 总体使用率。正常：< 80%，关注：≥ 80%，严重：≥ 95%。含 steal 时间占比。">CPU 使用率</h3>
            <span className="watchtower-metric-card__current">
              <MonoDigits>{formatPercent(sample.cpu_usage_pct)}</MonoDigits>
            </span>
          </header>
          <MetricChart
            samples={toSeries(ascending, (s) => s.cpu_usage_pct)}
            tone={cpuPriority > 0 ? priorityTone(cpuPriority) : baseTone}
            width={360}
            height={140}
            yMin={0}
            yMax={100}
            thresholds={[
              { value: t.cpu.notice, tone: 'notice', label: `${t.cpu.notice}%` },
              { value: t.cpu.critical, tone: 'critical', label: `${t.cpu.critical}%` },
            ]}
            formatValue={(v) => formatPercent(v)}
            ariaLabel="CPU 使用率近 24h 趋势"
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
      priority: memPriority,
      tone: priorityTone(memPriority),
      render: () => (
        <article className={`watchtower-metric-card ${cardRibbonClass(memPriority)}`.trim()}>
          <header className="watchtower-metric-card__head">
            <h3 title="内存总体使用率。正常：< 85%，关注：≥ 85%，严重：≥ 95%。含 swap 和可用内存。">内存使用率</h3>
            <span className="watchtower-metric-card__current">
              <MonoDigits>{formatPercent(sample.mem_used_pct)}</MonoDigits>
            </span>
          </header>
          <MetricChart
            samples={toSeries(ascending, (s) => s.mem_used_pct)}
            tone={memPriority > 0 ? priorityTone(memPriority) : baseTone}
            width={360}
            height={140}
            yMin={0}
            yMax={100}
            thresholds={[
              { value: t.mem.notice, tone: 'notice', label: `${t.mem.notice}%` },
              { value: t.mem.critical, tone: 'critical', label: `${t.mem.critical}%` },
            ]}
            formatValue={(v) => formatPercent(v)}
            ariaLabel="内存使用率近 24h 趋势"
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
          </dl>
        </article>
      ),
    },
    {
      id: 'disk',
      priority: diskPriority,
      tone: priorityTone(diskPriority),
      render: () => (
        <article className={`watchtower-metric-card ${cardRibbonClass(diskPriority)}`.trim()}>
          <header className="watchtower-metric-card__head">
            <h3 title="磁盘空间使用率。正常：< 80%，关注：≥ 80%，严重：≥ 95%。含 IO busy 和读写速率。">磁盘使用率</h3>
            <span className="watchtower-metric-card__current">
              <MonoDigits>{formatPercent(sample.disk_used_pct)}</MonoDigits>
            </span>
          </header>
          <MetricChart
            samples={toSeries(ascending, (s) => s.disk_used_pct)}
            tone={diskPriority > 0 ? priorityTone(diskPriority) : baseTone}
            width={360}
            height={140}
            yMin={0}
            yMax={100}
            thresholds={[
              { value: t.disk.notice, tone: 'notice', label: `${t.disk.notice}%` },
              { value: t.disk.critical, tone: 'critical', label: `${t.disk.critical}%` },
            ]}
            formatValue={(v) => formatPercent(v)}
            ariaLabel="磁盘使用率近 24h 趋势"
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
          </dl>
        </article>
      ),
    },
    {
      id: 'inode',
      priority: inodePriority,
      tone: priorityTone(inodePriority),
      render: () => (
        <article className={`watchtower-metric-card ${cardRibbonClass(inodePriority)}`.trim()}>
          <header className="watchtower-metric-card__head">
            <h3 title="Inode 使用率。正常：< 80%，关注：≥ 80%，严重：≥ 95%。Inode 耗尽会导致无法创建新文件。">Inode 使用率</h3>
            <span className="watchtower-metric-card__current">
              <MonoDigits>{formatPercent(sample.inode_used_pct)}</MonoDigits>
            </span>
          </header>
          <MetricChart
            samples={toSeries(ascending, (s) => s.inode_used_pct)}
            tone={inodePriority > 0 ? priorityTone(inodePriority) : baseTone}
            width={360}
            height={140}
            yMin={0}
            yMax={100}
            thresholds={[
              { value: t.inode.notice, tone: 'notice', label: `${t.inode.notice}%` },
              { value: t.inode.critical, tone: 'critical', label: `${t.inode.critical}%` },
            ]}
            formatValue={(v) => formatPercent(v)}
            ariaLabel="Inode 使用率近 24h 趋势"
          />
        </article>
      ),
    },
    {
      id: 'load5',
      priority: load5Priority,
      tone: priorityTone(load5Priority),
      render: () => (
        <article className={`watchtower-metric-card ${cardRibbonClass(load5Priority)}`.trim()}>
          <header className="watchtower-metric-card__head">
            <h3 title="系统 5 分钟负载均值。正常：< 4.0，关注：≥ 4.0，严重：≥ 8.0。需结合 CPU 核数判断。">Load5</h3>
            <span className="watchtower-metric-card__current">
              <MonoDigits>{formatNumber(sample.load_5)}</MonoDigits>
            </span>
          </header>
          <MetricChart
            samples={toSeries(ascending, (s) => s.load_5)}
            tone={load5Priority > 0 ? priorityTone(load5Priority) : baseTone}
            width={360}
            height={140}
            yMin={0}
            thresholds={[
              { value: t.load5.notice, tone: 'notice', label: String(t.load5.notice) },
              { value: t.load5.critical, tone: 'critical', label: String(t.load5.critical) },
            ]}
            formatValue={(v) => formatNumber(v)}
            ariaLabel="Load5 近 24h 趋势"
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
      priority: iowaitPriority,
      tone: priorityTone(iowaitPriority),
      render: () => (
        <article className={`watchtower-metric-card ${cardRibbonClass(iowaitPriority)}`.trim()}>
          <header className="watchtower-metric-card__head">
            <h3 title="CPU 等待 I/O 的时间占比。正常：< 20%，关注：≥ 20%，严重：≥ 50%。偏高通常意味着磁盘瓶颈。">CPU IOWait</h3>
            <span className="watchtower-metric-card__current">
              <MonoDigits>{formatPercent(sample.cpu_iowait_pct)}</MonoDigits>
            </span>
          </header>
          <MetricChart
            samples={toSeries(ascending, (s) => s.cpu_iowait_pct)}
            tone={iowaitPriority > 0 ? priorityTone(iowaitPriority) : baseTone}
            width={360}
            height={140}
            yMin={0}
            thresholds={[
              { value: t.iowait.notice, tone: 'notice', label: `${t.iowait.notice}%` },
              { value: t.iowait.critical, tone: 'critical', label: `${t.iowait.critical}%` },
            ]}
            formatValue={(v) => formatPercent(v)}
            ariaLabel="CPU IOWait 近 24h 趋势"
          />
        </article>
      ),
    },
    {
      id: 'net-in',
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
            tone={altTone}
            width={360}
            height={140}
            yMin={0}
            formatValue={(v) => formatBytesPerSecond(v)}
            ariaLabel="网络入站近 24h 趋势"
          />
        </article>
      ),
    },
    {
      id: 'net-out',
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
            tone={baseTone}
            width={360}
            height={140}
            yMin={0}
            formatValue={(v) => formatBytesPerSecond(v)}
            ariaLabel="网络出站近 24h 趋势"
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

  return (
    <div className="watchtower-metrics" role="group" aria-label="主机指标趋势">
      {sorted.map((card) => (
        <Fragment key={card.id}>{card.render()}</Fragment>
      ))}
    </div>
  )
}
