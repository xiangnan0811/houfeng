import { MetricChart, type MetricChartSample } from '../atoms/MetricChart'
import { MonoDigits } from '../atoms/Mono'
import {
  formatBytes,
  formatBytesPerSecond,
  formatNumber,
  formatPercent,
} from '../../lib/format'
import type { HostSample } from '../../lib/types'

type Props = {
  sample: HostSample | null
  samples: HostSample[]
  isMaintenance?: boolean
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
  const tone = isMaintenance ? 'maintenance' : 'accent'
  const altTone = isMaintenance ? 'maintenance' : 'accent-2'

  return (
    <div className="watchtower-metrics" role="group" aria-label="主机指标趋势">
      {/* CPU usage% */}
      <article className="watchtower-metric-card">
        <header className="watchtower-metric-card__head">
          <h3>CPU 使用率</h3>
          <span className="watchtower-metric-card__current">
            <MonoDigits>{formatPercent(sample.cpu_usage_pct)}</MonoDigits>
          </span>
        </header>
        <MetricChart
          samples={toSeries(ascending, (s) => s.cpu_usage_pct)}
          tone={tone}
          width={360}
          height={140}
          yMin={0}
          yMax={100}
          thresholds={[
            { value: 80, tone: 'notice', label: '80%' },
            { value: 95, tone: 'critical', label: '95%' },
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

      {/* Load5 */}
      <article className="watchtower-metric-card">
        <header className="watchtower-metric-card__head">
          <h3>Load5</h3>
          <span className="watchtower-metric-card__current">
            <MonoDigits>{formatNumber(sample.load_5)}</MonoDigits>
          </span>
        </header>
        <MetricChart
          samples={toSeries(ascending, (s) => s.load_5)}
          tone={tone}
          width={360}
          height={140}
          yMin={0}
          thresholds={[
            { value: 4, tone: 'notice', label: '4.0' },
            { value: 8, tone: 'critical', label: '8.0' },
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

      {/* Memory used% */}
      <article className="watchtower-metric-card">
        <header className="watchtower-metric-card__head">
          <h3>内存使用率</h3>
          <span className="watchtower-metric-card__current">
            <MonoDigits>{formatPercent(sample.mem_used_pct)}</MonoDigits>
          </span>
        </header>
        <MetricChart
          samples={toSeries(ascending, (s) => s.mem_used_pct)}
          tone={tone}
          width={360}
          height={140}
          yMin={0}
          yMax={100}
          thresholds={[
            { value: 85, tone: 'notice', label: '85%' },
            { value: 95, tone: 'critical', label: '95%' },
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

      {/* Disk used% */}
      <article className="watchtower-metric-card">
        <header className="watchtower-metric-card__head">
          <h3>磁盘使用率</h3>
          <span className="watchtower-metric-card__current">
            <MonoDigits>{formatPercent(sample.disk_used_pct)}</MonoDigits>
          </span>
        </header>
        <MetricChart
          samples={toSeries(ascending, (s) => s.disk_used_pct)}
          tone={tone}
          width={360}
          height={140}
          yMin={0}
          yMax={100}
          thresholds={[
            { value: 80, tone: 'notice', label: '80%' },
            { value: 95, tone: 'critical', label: '95%' },
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

      {/* Inode used% */}
      <article className="watchtower-metric-card">
        <header className="watchtower-metric-card__head">
          <h3>Inode 使用率</h3>
          <span className="watchtower-metric-card__current">
            <MonoDigits>{formatPercent(sample.inode_used_pct)}</MonoDigits>
          </span>
        </header>
        <MetricChart
          samples={toSeries(ascending, (s) => s.inode_used_pct)}
          tone={tone}
          width={360}
          height={140}
          yMin={0}
          yMax={100}
          thresholds={[
            { value: 80, tone: 'notice', label: '80%' },
            { value: 95, tone: 'critical', label: '95%' },
          ]}
          formatValue={(v) => formatPercent(v)}
          ariaLabel="Inode 使用率近 24h 趋势"
        />
      </article>

      {/* Net In B/s */}
      <article className="watchtower-metric-card">
        <header className="watchtower-metric-card__head">
          <h3>网络入</h3>
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

      {/* Net Out B/s */}
      <article className="watchtower-metric-card">
        <header className="watchtower-metric-card__head">
          <h3>网络出</h3>
          <span className="watchtower-metric-card__current">
            <MonoDigits>{formatBytesPerSecond(sample.net_out_bytes_per_sec)}</MonoDigits>
          </span>
        </header>
        <MetricChart
          samples={toSeries(ascending, (s) => s.net_out_bytes_per_sec)}
          tone={tone}
          width={360}
          height={140}
          yMin={0}
          formatValue={(v) => formatBytesPerSecond(v)}
          ariaLabel="网络出站近 24h 趋势"
        />
      </article>

      {/* IOWait% */}
      <article className="watchtower-metric-card">
        <header className="watchtower-metric-card__head">
          <h3>CPU IOWait</h3>
          <span className="watchtower-metric-card__current">
            <MonoDigits>{formatPercent(sample.cpu_iowait_pct)}</MonoDigits>
          </span>
        </header>
        <MetricChart
          samples={toSeries(ascending, (s) => s.cpu_iowait_pct)}
          tone={tone}
          width={360}
          height={140}
          yMin={0}
          thresholds={[
            { value: 20, tone: 'notice', label: '20%' },
            { value: 50, tone: 'critical', label: '50%' },
          ]}
          formatValue={(v) => formatPercent(v)}
          ariaLabel="CPU IOWait 近 24h 趋势"
        />
      </article>
    </div>
  )
}
