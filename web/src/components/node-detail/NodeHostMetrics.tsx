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
}

export function NodeHostMetrics({ sample }: NodeHostMetricsProps) {
  return (
    <DetailSection
      eyebrow="当前运行事实"
      title="当前主机指标"
      aside={sample ? `样本时间：${formatDateTime(sample.observed_at)}` : '等待首批样本'}
    >
      {sample ? (
        <div className="metric-grid">
          <article className="metric-card">
            <h3>CPU / Load</h3>
            <dl>
              <div>
                <dt>CPU 使用率</dt>
                <dd>{formatPercent(sample.cpu_usage_pct)}</dd>
              </div>
              <div>
                <dt>Load 1 / 5 / 15</dt>
                <dd>
                  {formatNumber(sample.load_1)} / {formatNumber(sample.load_5)} /{' '}
                  {formatNumber(sample.load_15)}
                </dd>
              </div>
              <div>
                <dt>iowait / steal</dt>
                <dd>
                  {formatPercent(sample.cpu_iowait_pct)} /{' '}
                  {formatPercent(sample.cpu_steal_pct)}
                </dd>
              </div>
            </dl>
          </article>

          <article className="metric-card">
            <h3>内存 / Swap</h3>
            <dl>
              <div>
                <dt>内存使用率</dt>
                <dd>{formatPercent(sample.mem_used_pct)}</dd>
              </div>
              <div>
                <dt>可用内存</dt>
                <dd>{formatBytes(sample.mem_available_bytes)}</dd>
              </div>
              <div>
                <dt>Swap 使用率</dt>
                <dd>{formatPercent(sample.swap_used_pct)}</dd>
              </div>
            </dl>
          </article>

          <article className="metric-card">
            <h3>磁盘 / Inode</h3>
            <dl>
              <div>
                <dt>磁盘使用率</dt>
                <dd>{formatPercent(sample.disk_used_pct)}</dd>
              </div>
              <div>
                <dt>Inode 使用率</dt>
                <dd>{formatPercent(sample.inode_used_pct)}</dd>
              </div>
              <div>
                <dt>磁盘繁忙度</dt>
                <dd>{formatPercent(sample.disk_busy_pct)}</dd>
              </div>
            </dl>
          </article>

          <article className="metric-card">
            <h3>网络 / 吞吐</h3>
            <dl>
              <div>
                <dt>流入 / 流出</dt>
                <dd>
                  {formatBytesPerSecond(sample.net_in_bytes_per_sec)} /{' '}
                  {formatBytesPerSecond(sample.net_out_bytes_per_sec)}
                </dd>
              </div>
              <div>
                <dt>磁盘读 / 写</dt>
                <dd>
                  {formatBytesPerSecond(sample.disk_read_bytes_per_sec)} /{' '}
                  {formatBytesPerSecond(sample.disk_write_bytes_per_sec)}
                </dd>
              </div>
              <div>
                <dt>运行时长</dt>
                <dd>{formatUptime(sample.uptime_seconds)}</dd>
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
