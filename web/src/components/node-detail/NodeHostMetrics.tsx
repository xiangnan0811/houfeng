import { Sparkline, type SparklineSample } from '../atoms/Sparkline'
import { MonoDigits } from '../atoms/Mono'
import { DetailSection } from '../DetailSection'
import {
  formatBytes,
  formatBytesPerSecond,
  formatDateTime,
  formatNumber,
  formatPercent,
  formatUptime,
} from '../../lib/format'
import type { HostSample } from '../../lib/types'

type NodeHostMetricsProps = {
  sample: HostSample | null
  samples: HostSample[]
  isMaintenance?: boolean
}

function toAscending(samples: HostSample[]): HostSample[] {
  return [...samples].sort(
    (a, b) => new Date(a.observed_at).getTime() - new Date(b.observed_at).getTime(),
  )
}

function toSeries(samples: HostSample[], pick: (s: HostSample) => number): SparklineSample[] {
  return samples.map((s) => ({ value: pick(s), observedAt: s.observed_at }))
}

function describeMeta(samples: HostSample[]): string {
  if (samples.length === 0) return '近 24h 暂无样本'
  const oldest = samples[0]
  const newest = samples[samples.length - 1]
  const backfillCount = samples.filter((s) => s.is_backfilled).length
  const parts = [
    `24h ${samples.length} 样本`,
    `最早 ${formatDateTime(oldest.observed_at)}`,
    `最新 ${formatDateTime(newest.observed_at)}`,
  ]
  if (backfillCount > 0) parts.push(`backfill ${backfillCount}`)
  return parts.join(' · ')
}

export function NodeHostMetrics({ sample, samples, isMaintenance = false }: NodeHostMetricsProps) {
  const ascending = toAscending(samples)
  const meta = describeMeta(ascending)

  const cpuSeries = toSeries(ascending, (s) => s.cpu_usage_pct)
  const memSeries = toSeries(ascending, (s) => s.mem_used_pct)
  const diskSeries = toSeries(ascending, (s) => s.disk_used_pct)
  const netInSeries = toSeries(ascending, (s) => s.net_in_bytes_per_sec)
  const netOutSeries = toSeries(ascending, (s) => s.net_out_bytes_per_sec)

  const primaryTone = isMaintenance ? 'maintenance' : 'accent'
  const secondaryTone = isMaintenance ? 'maintenance' : 'accent-2'

  const ribbon = isMaintenance ? 'maintenance' : undefined

  return (
    <DetailSection
      eyebrow="当前运行事实"
      title="当前主机指标"
      ribbon={ribbon}
      aside={<span className="detail-section__aside-meta">{meta}</span>}
    >
      {sample ? (
        <div className="metric-grid metric-grid--quad">
          <article className="metric-card">
            <header className="metric-card__head">
              <h3>CPU / Load</h3>
              <span className="metric-card__current">
                <MonoDigits>{formatPercent(sample.cpu_usage_pct)}</MonoDigits>
              </span>
            </header>
            <Sparkline
              samples={cpuSeries}
              tone={primaryTone}
              height={60}
              expand
              interactive
              ariaLabel="CPU 使用率近 24h 趋势"
              formatValue={(v) => formatPercent(v)}
            />
            <dl>
              <div>
                <dt>Load 1 / 5 / 15</dt>
                <dd>
                  <MonoDigits>
                    {formatNumber(sample.load_1)} / {formatNumber(sample.load_5)} /{' '}
                    {formatNumber(sample.load_15)}
                  </MonoDigits>
                </dd>
              </div>
              <div>
                <dt>iowait / steal</dt>
                <dd>
                  <MonoDigits>
                    {formatPercent(sample.cpu_iowait_pct)} /{' '}
                    {formatPercent(sample.cpu_steal_pct)}
                  </MonoDigits>
                </dd>
              </div>
            </dl>
          </article>

          <article className="metric-card">
            <header className="metric-card__head">
              <h3>内存 / Swap</h3>
              <span className="metric-card__current">
                <MonoDigits>{formatPercent(sample.mem_used_pct)}</MonoDigits>
              </span>
            </header>
            <Sparkline
              samples={memSeries}
              tone={primaryTone}
              height={60}
              expand
              interactive
              ariaLabel="内存使用率近 24h 趋势"
              formatValue={(v) => formatPercent(v)}
            />
            <dl>
              <div>
                <dt>可用内存</dt>
                <dd><MonoDigits>{formatBytes(sample.mem_available_bytes)}</MonoDigits></dd>
              </div>
              <div>
                <dt>Swap 使用率</dt>
                <dd><MonoDigits>{formatPercent(sample.swap_used_pct)}</MonoDigits></dd>
              </div>
            </dl>
          </article>

          <article className="metric-card">
            <header className="metric-card__head">
              <h3>磁盘 / Inode</h3>
              <span className="metric-card__current">
                <MonoDigits>{formatPercent(sample.disk_used_pct)}</MonoDigits>
              </span>
            </header>
            <Sparkline
              samples={diskSeries}
              tone={primaryTone}
              height={60}
              expand
              interactive
              ariaLabel="磁盘使用率近 24h 趋势"
              formatValue={(v) => formatPercent(v)}
            />
            <dl>
              <div>
                <dt>Inode 使用率</dt>
                <dd><MonoDigits>{formatPercent(sample.inode_used_pct)}</MonoDigits></dd>
              </div>
              <div>
                <dt>磁盘繁忙度</dt>
                <dd><MonoDigits>{formatPercent(sample.disk_busy_pct)}</MonoDigits></dd>
              </div>
              <div>
                <dt>磁盘读 / 写</dt>
                <dd>
                  <MonoDigits>
                    {formatBytesPerSecond(sample.disk_read_bytes_per_sec)} /{' '}
                    {formatBytesPerSecond(sample.disk_write_bytes_per_sec)}
                  </MonoDigits>
                </dd>
              </div>
            </dl>
          </article>

          <article className="metric-card">
            <header className="metric-card__head">
              <h3>网络 / 吞吐</h3>
              <span className="metric-card__current">
                <MonoDigits>{formatBytesPerSecond(sample.net_in_bytes_per_sec)}</MonoDigits>
                <span className="metric-card__current-sub">in</span>
              </span>
            </header>
            <div className="metric-card__sparkline-stack">
              <div className="metric-card__sparkline-row">
                <span className="metric-card__sparkline-tag">in</span>
                <Sparkline
                  samples={netInSeries}
                  tone={secondaryTone}
                  height={28}
                  expand
                  interactive
                  ariaLabel="入站流量近 24h 趋势"
                  formatValue={(v) => formatBytesPerSecond(v)}
                />
              </div>
              <div className="metric-card__sparkline-row">
                <span className="metric-card__sparkline-tag">out</span>
                <Sparkline
                  samples={netOutSeries}
                  tone={primaryTone}
                  height={28}
                  expand
                  interactive
                  ariaLabel="出站流量近 24h 趋势"
                  formatValue={(v) => formatBytesPerSecond(v)}
                />
              </div>
            </div>
            <dl>
              <div>
                <dt>流出</dt>
                <dd><MonoDigits>{formatBytesPerSecond(sample.net_out_bytes_per_sec)}</MonoDigits></dd>
              </div>
              <div>
                <dt>运行时长</dt>
                <dd><MonoDigits>{formatUptime(sample.uptime_seconds)}</MonoDigits></dd>
              </div>
            </dl>
          </article>
        </div>
      ) : (
        <div className="empty-state">
          <h3>尚未收到主机样本</h3>
          <p>该节点已存在，但首批主机采样（HostSample）还未到达。请等待下一次 agent 同步。</p>
        </div>
      )}
    </DetailSection>
  )
}
