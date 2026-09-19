import { useState, type ReactNode } from 'react'
import { MetricChart } from '../atoms/MetricChart'
import { MonoDigits, Timestamp } from '../atoms/Mono'
import {
  formatBytes,
  formatBytesPerSecond,
  formatNumber,
  formatPercent,
} from '../../lib/format'
import type { HostSample, MonitoringRuntimeWindow } from '../../lib/types'
import type { MetricThresholds } from '../../config/thresholds'
import {
  formatCapacityBytes,
  formatNetworkAxis,
  seriesMax,
  seriesValueAt,
  thresholdLines,
  thresholdTitle,
  timeWindowLabel,
  toAscending,
  toSeries,
  type HostMetricSeriesPoint,
  type MetricTimeWindow,
} from './metricSeries'

type Props = {
  sample: HostSample | null
  metricPoints: HostMetricSeriesPoint[]
  timeWindow: MetricTimeWindow
  window?: MonitoringRuntimeWindow
  isMaintenance?: boolean
  thresholds?: MetricThresholds | null
  detailPresentation?: boolean
}

export type { HostMetricSeriesPoint }

function availableWindowLabel(window?: MonitoringRuntimeWindow): string {
  if (!window?.available_started_at || !window.available_ended_at) return '暂无可用历史跨度'
  return `${new Date(window.available_started_at).toLocaleString()} - ${new Date(window.available_ended_at).toLocaleString()}`
}

function readoutKind(timeWindow: MetricTimeWindow, hovering: boolean): string {
  if (timeWindow === 'realtime') return hovering ? '选中点' : '实时点'
  return hovering ? '选中桶' : '窗口末值'
}

function Plot({
  title,
  titleHint,
  current,
  currentLabel,
  children,
}: {
  title: string
  titleHint: string
  current?: ReactNode
  currentLabel?: string
  children: ReactNode
}) {
  return (
    <div className="watchtower-metric-plot">
      <header className="watchtower-metric-plot__head">
        <h3 title={titleHint}>{title}</h3>
        {current ? (
          <span className="watchtower-metric-plot__current">
            {currentLabel ? <span className="watchtower-metric-plot__current-label">{currentLabel}</span> : null}
            {current}
          </span>
        ) : null}
      </header>
      {children}
    </div>
  )
}

export function MonitoringInstanceWatchtowerMetrics({
  sample,
  metricPoints,
  timeWindow,
  window,
  isMaintenance = false,
  thresholds = null,
  detailPresentation = false,
}: Props) {
  const [hoveredAt, setHoveredAt] = useState<string | null>(null)
  const empty = !sample && metricPoints.length === 0
  const ascending = empty ? [] : toAscending(metricPoints)
  const labelPrefix = timeWindowLabel(timeWindow)
  const baseTone = isMaintenance ? 'maintenance' : 'accent'
  const altTone = isMaintenance ? 'maintenance' : 'accent-2'
  const hovering = hoveredAt != null
  const kind = readoutKind(timeWindow, hovering)
  const sharedChartProps = {
    hoveredAt,
    onHoverAtChange: setHoveredAt,
    ...(detailPresentation ? { showTooltip: false as const } : {}),
  }
  const thresholdPresentation = detailPresentation
    ? { includeThresholdsInScale: true as const, thresholdLabelPlacement: 'gutter' as const }
    : {}

  const cpuSeries = toSeries(ascending, (s) => s.cpu_usage_pct)
  const memSeries = toSeries(ascending, (s) => s.mem_used_pct)
  const diskSeries = toSeries(ascending, (s) => s.disk_used_pct)
  const loadSeries = toSeries(ascending, (s) => s.load_5)
  const iowaitSeries = toSeries(ascending, (s) => s.cpu_iowait_pct)
  const inodeSeries = toSeries(ascending, (s) => s.inode_used_pct)
  const netInSeries = toSeries(ascending, (s) => s.net_in_bytes_per_sec)
  const netOutSeries = toSeries(ascending, (s) => s.net_out_bytes_per_sec)
  const netYMax = seriesMax([...netInSeries, ...netOutSeries])
  const netAxisMax = netYMax === undefined ? undefined : Math.max(netYMax, 1)

  const pointCount = ascending.length
  const cpuReadout = seriesValueAt(cpuSeries, hoveredAt)
  const memReadout = seriesValueAt(memSeries, hoveredAt)
  const diskReadout = seriesValueAt(diskSeries, hoveredAt)
  const netInReadout = seriesValueAt(netInSeries, hoveredAt)
  const netOutReadout = seriesValueAt(netOutSeries, hoveredAt)

  return (
    <section className="watchtower-metrics-panel" aria-label="主机指标趋势">
      <p className="watchtower-metrics-meta">
          {timeWindow === 'realtime'
            ? `实时滚动 ${pointCount} 点`
            : window
              ? `${labelPrefix} · ${window.sample_count} 个原始样本 · ${availableWindowLabel(window)}`
              : `${labelPrefix} · 窗口样本数未知`}
          {thresholds ? null : ' · 阈值策略不可用'}
          {hoveredAt ? (
            <>
              {' · '}
              {kind}{' '}
              <Timestamp value={hoveredAt} mode="absolute" />
            </>
          ) : null}
      </p>

      {empty ? (
        <div className="empty-state">
          <h3>尚未收到主机样本</h3>
          <p>该监控实例已存在，但首批主机采样（HostSample）还未到达。请等待下一次 agent 同步。</p>
        </div>
      ) : (
      <div className="watchtower-metrics" role="group" aria-label="主机指标趋势">
        <section className="watchtower-metric-group" aria-label="CPU 与负载">
          <header className="watchtower-metric-group__head">
            <h3 className="watchtower-metric-group__title">CPU 与负载</h3>
            <span className="watchtower-metric-group__current">
              <span className="watchtower-metric-plot__current-label">{kind}</span>
              <MonoDigits>{formatPercent(cpuReadout)}</MonoDigits>
            </span>
          </header>
          <Plot
            title="CPU 使用率"
            titleHint={thresholdTitle('CPU 总体使用率', thresholds?.cpu, '%', '含 CPU 被窃取时间占比。')}
          >
            <MetricChart
              samples={cpuSeries}
              {...sharedChartProps}
              tone={baseTone}
              height={160}
              yMin={0}
              yMax={100}
              {...(thresholds ? { thresholds: thresholdLines(thresholds.cpu, '%'), ...thresholdPresentation } : {})}
              formatValue={(v) => formatPercent(v)}
              formatAxisValue={(v) => formatPercent(v, 0)}
              ariaLabel={`CPU 使用率${labelPrefix}趋势`}
            />
          </Plot>
          <dl className="watchtower-metric-facts" aria-label="最近样本">
            <div className="watchtower-metric-facts__source">
              <dt>最近样本</dt>
            </div>
            <div>
              <dt>CPU 被窃取</dt>
              <dd><MonoDigits>{formatPercent(sample?.cpu_steal_pct)}</MonoDigits></dd>
            </div>
            <div>
              <dt>5 分钟负载</dt>
              <dd><MonoDigits>{formatNumber(sample?.load_5)}</MonoDigits></dd>
            </div>
            <div>
              <dt>1 分钟</dt>
              <dd><MonoDigits>{formatNumber(sample?.load_1)}</MonoDigits></dd>
            </div>
            <div>
              <dt>15 分钟</dt>
              <dd><MonoDigits>{formatNumber(sample?.load_15)}</MonoDigits></dd>
            </div>
            <div>
              <dt>CPU I/O 等待</dt>
              <dd><MonoDigits>{formatPercent(sample?.cpu_iowait_pct)}</MonoDigits></dd>
            </div>
          </dl>
        </section>

        <section className="watchtower-metric-group" aria-label="内存">
          <header className="watchtower-metric-group__head">
            <h3 className="watchtower-metric-group__title">内存</h3>
            <span className="watchtower-metric-group__current">
              <span className="watchtower-metric-plot__current-label">{kind}</span>
              <MonoDigits>{formatPercent(memReadout)}</MonoDigits>
            </span>
          </header>
          <Plot
            title="内存使用率"
            titleHint={thresholdTitle('内存总体使用率', thresholds?.mem, '%', '含交换空间和可用内存。')}
          >
            <MetricChart
              samples={memSeries}
              {...sharedChartProps}
              tone={baseTone}
              height={160}
              yMin={0}
              yMax={100}
              {...(thresholds ? { thresholds: thresholdLines(thresholds.mem, '%'), ...thresholdPresentation } : {})}
              formatValue={(v) => formatPercent(v)}
              formatAxisValue={(v) => formatPercent(v, 0)}
              ariaLabel={`内存使用率${labelPrefix}趋势`}
            />
          </Plot>
          <dl className="watchtower-metric-facts" aria-label="最近样本">
            <div className="watchtower-metric-facts__source">
              <dt>最近样本</dt>
            </div>
            <div>
              <dt>交换使用率</dt>
              <dd><MonoDigits>{formatPercent(sample?.swap_used_pct)}</MonoDigits></dd>
            </div>
            <div>
              <dt>可用</dt>
              <dd><MonoDigits>{formatBytes(sample?.mem_available_bytes)}</MonoDigits></dd>
            </div>
            <div>
              <dt>容量</dt>
              <dd><MonoDigits>{formatCapacityBytes(sample?.mem_total_bytes)}</MonoDigits></dd>
            </div>
          </dl>
        </section>

        <section className="watchtower-metric-group" aria-label="磁盘与 I/O">
          <header className="watchtower-metric-group__head">
            <h3 className="watchtower-metric-group__title">磁盘与 I/O</h3>
            <span className="watchtower-metric-group__current">
              <span className="watchtower-metric-plot__current-label">{kind}</span>
              <MonoDigits>{formatPercent(diskReadout)}</MonoDigits>
            </span>
          </header>
          <Plot
            title="磁盘使用率"
            titleHint={thresholdTitle('磁盘空间使用率', thresholds?.disk, '%', '磁盘繁忙是磁盘自身忙碌占比，不是 CPU I/O 等待。')}
          >
            <MetricChart
              samples={diskSeries}
              {...sharedChartProps}
              tone={baseTone}
              height={160}
              yMin={0}
              yMax={100}
              {...(thresholds ? { thresholds: thresholdLines(thresholds.disk, '%'), ...thresholdPresentation } : {})}
              formatValue={(v) => formatPercent(v)}
              formatAxisValue={(v) => formatPercent(v, 0)}
              ariaLabel={`磁盘使用率${labelPrefix}趋势`}
            />
          </Plot>
          <dl className="watchtower-metric-facts" aria-label="最近样本">
            <div className="watchtower-metric-facts__source">
              <dt>最近样本</dt>
            </div>
            <div>
              <dt>磁盘繁忙</dt>
              <dd><MonoDigits>{formatPercent(sample?.disk_busy_pct)}</MonoDigits></dd>
            </div>
            <div>
              <dt>读 / 写</dt>
              <dd>
                <MonoDigits>
                  {formatBytesPerSecond(sample?.disk_read_bytes_per_sec)} /{' '}
                  {formatBytesPerSecond(sample?.disk_write_bytes_per_sec)}
                </MonoDigits>
              </dd>
            </div>
            <div>
              <dt>容量</dt>
              <dd><MonoDigits>{formatCapacityBytes(sample?.disk_total_bytes)}</MonoDigits></dd>
            </div>
            <div>
              <dt>Inode</dt>
              <dd><MonoDigits>{formatPercent(sample?.inode_used_pct)}</MonoDigits></dd>
            </div>
          </dl>
        </section>

        <section className="watchtower-metric-group" aria-label="网络">
          <header className="watchtower-metric-group__head">
            <h3 className="watchtower-metric-group__title">网络</h3>
            <span className="watchtower-metric-group__current">
              <span className="watchtower-metric-plot__current-label">{kind}</span>
              <MonoDigits>↓ {formatBytesPerSecond(netInReadout)}</MonoDigits>
              <span className="watchtower-metric-plot__current-label">·</span>
              <MonoDigits>↑ {formatBytesPerSecond(netOutReadout)}</MonoDigits>
            </span>
          </header>
          <p className="watchtower-metric-facts">下行 / 上行共用纵轴 · B/s</p>
          <div className="watchtower-metric-group__network">
            <Plot
              title="下行"
              titleHint="网络下行速率（B/s）。与上行共用纵轴以便比较；无固定阈值。无效或历史未知速率显示为 —。"
            >
              <MetricChart
                samples={netInSeries}
                {...sharedChartProps}
                tone={altTone}
                height={90}
                yMin={0}
                yTickCount={2}
                {...(netAxisMax === undefined ? {} : { yMax: netAxisMax })}
                formatValue={(v) => formatBytesPerSecond(v)}
                formatAxisValue={formatNetworkAxis}
                ariaLabel={`下行${labelPrefix}趋势`}
              />
            </Plot>
            <Plot
              title="上行"
              titleHint="网络上行速率（B/s）。与下行共用纵轴以便比较；无固定阈值。无效或历史未知速率显示为 —。"
            >
              <MetricChart
                samples={netOutSeries}
                {...sharedChartProps}
                tone={baseTone}
                height={90}
                yMin={0}
                yTickCount={2}
                {...(netAxisMax === undefined ? {} : { yMax: netAxisMax })}
                formatValue={(v) => formatBytesPerSecond(v)}
                formatAxisValue={formatNetworkAxis}
                ariaLabel={`上行${labelPrefix}趋势`}
              />
            </Plot>
          </div>
        </section>

        <details className="watchtower-metric-aux">
          <summary>5 分钟负载</summary>
          <Plot
            title="5 分钟负载"
            titleHint={thresholdTitle('系统 5 分钟负载均值', thresholds?.load5, '', '需结合 CPU 核数判断。')}
            current={<MonoDigits>{formatNumber(seriesValueAt(loadSeries, hoveredAt))}</MonoDigits>}
            currentLabel={kind}
          >
            <MetricChart
              samples={loadSeries}
              {...sharedChartProps}
              tone={baseTone}
              height={120}
              yMin={0}
              {...(thresholds ? { thresholds: thresholdLines(thresholds.load5), ...thresholdPresentation } : {})}
              formatValue={(v) => formatNumber(v)}
              formatAxisValue={(v) => formatNumber(v)}
              ariaLabel={`5 分钟负载${labelPrefix}趋势`}
            />
          </Plot>
        </details>

        <details className="watchtower-metric-aux">
          <summary>CPU I/O 等待</summary>
          <Plot
            title="CPU I/O 等待"
            titleHint={thresholdTitle('CPU 等待 I/O 的时间占比', thresholds?.iowait, '%', '这是 CPU 时间，不是磁盘空间使用率或磁盘繁忙。')}
            current={<MonoDigits>{formatPercent(seriesValueAt(iowaitSeries, hoveredAt))}</MonoDigits>}
            currentLabel={kind}
          >
            <MetricChart
              samples={iowaitSeries}
              {...sharedChartProps}
              tone={baseTone}
              height={120}
              yMin={0}
              {...(thresholds ? { thresholds: thresholdLines(thresholds.iowait, '%'), ...thresholdPresentation } : {})}
              formatValue={(v) => formatPercent(v)}
              formatAxisValue={(v) => formatPercent(v, 0)}
              ariaLabel={`CPU I/O 等待${labelPrefix}趋势`}
            />
          </Plot>
        </details>

        <details className="watchtower-metric-aux">
          <summary>Inode 使用率</summary>
          <Plot
            title="Inode 使用率"
            titleHint={thresholdTitle('Inode 使用率', thresholds?.inode, '%', 'Inode 耗尽会导致无法创建新文件。')}
            current={<MonoDigits>{formatPercent(seriesValueAt(inodeSeries, hoveredAt))}</MonoDigits>}
            currentLabel={kind}
          >
            <MetricChart
              samples={inodeSeries}
              {...sharedChartProps}
              tone={baseTone}
              height={120}
              yMin={0}
              yMax={100}
              {...(thresholds ? { thresholds: thresholdLines(thresholds.inode, '%'), ...thresholdPresentation } : {})}
              formatValue={(v) => formatPercent(v)}
              formatAxisValue={(v) => formatPercent(v, 0)}
              ariaLabel={`Inode 使用率${labelPrefix}趋势`}
            />
          </Plot>
        </details>
      </div>
      )}
    </section>
  )
}
