import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'

import { MetricChart, type MetricChartThreshold } from '../../components/atoms/MetricChart'
import { MonoDigits, Timestamp } from '../../components/atoms/Mono'
import { Button } from '../../components/atoms/Button'
import type { MetricThreshold, MetricThresholds } from '../../config/thresholds'
import {
  formatBytes,
  formatBytesPerSecond,
  formatNumber,
  formatPercent,
} from '../../lib/format'
import type { HostSample, MonitoringRuntimeWindow } from '../../lib/types'
import {
  formatCapacityBytes,
  formatCompactRateAxis,
  seriesMax,
  seriesValueAt,
  thresholdLevelsText,
  toAscending,
  toSeries,
  type HostMetricSeriesPoint,
} from '../../components/monitoring-detail/metricSeries'
import {
  niceMax,
  OBSERVATION_CHART_HEIGHT,
  type ObservationLayout,
} from './monitoringDetailScale'
import type { TimeWindow } from './types'

type Layout = ObservationLayout
type TileTone = 'primary' | 'notice' | 'alert' | 'critical'

const NARROW_CONTAINER_PX = 640 // 40rem
/** Four columns need ~270px cells after padding; below this, drop to 2×4. */
const WIDE_CONTAINER_PX = 1100
const NARROW_VIEWPORT_QUERY = '(max-width: 760px)'
/** Same left gutter on every tile so plot origins line up. Keep it tight:
 *  axis ticks use compact labels (100%, 2.0, 3.2M), not "3.2 MB/s". */
const PLOT_GUTTER = 34

type ObservationsProps = {
  /** Page-level window control, rendered in this section's head so it never owns an empty row. */
  timeRangeControl?: ReactNode
  sample: HostSample | null
  metricPoints: HostMetricSeriesPoint[]
  timeWindow: TimeWindow
  window?: MonitoringRuntimeWindow
  isMaintenance?: boolean
  thresholds: MetricThresholds | null
  loading: boolean
  error: string | null
  onRetryThresholds: () => void
}

function Plot({
  variant,
  title,
  hint,
  tone = 'primary',
  legend,
  current,
  children,
  notes,
  notesTitle,
}: {
  variant: string
  title: string
  hint: string
  tone?: TileTone
  legend?: ReactNode
  current: ReactNode
  children: ReactNode
  notes?: ReactNode
  notesTitle?: string
}) {
  return (
    <section
      className={`monitoring-detail-chart monitoring-detail-chart--${variant} monitoring-detail-chart--${tone}`}
      aria-label={title}
    >
      <header className="monitoring-detail-chart__head">
        <h3 className="monitoring-detail-chart__title" title={hint} aria-label={`${title}。${hint}`}>
          {title}
        </h3>
        {legend ? <span className="monitoring-detail-chart__legend">{legend}</span> : null}
        <span className="monitoring-detail-chart__current">{current}</span>
      </header>
      {children}
      <dl className="monitoring-detail-chart__notes" title={notesTitle}>{notes}</dl>
    </section>
  )
}

function Note({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="monitoring-detail-chart__note">
      <dt>{label}</dt>
      <dd><MonoDigits>{children}</MonoDigits></dd>
    </div>
  )
}

function toneFor(value: number | null, thresholds: MetricThreshold | undefined): TileTone {
  if (value == null || !Number.isFinite(value) || !thresholds) return 'primary'
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
    // Only the highest visible line carries text; the rest are bare 1px lines.
    // A line on the axis cap is already the top tick — don't also stamp 告警.
    label: index === candidates.length - 1 ? `告警 ${candidate.value}` : '',
  }))
}

function availabilityLine(timeWindow: TimeWindow, window?: MonitoringRuntimeWindow): string {
  if (timeWindow === 'realtime') return '实时'
  const prefix = timeWindow === '24h' ? '近 24h' : timeWindow === '7d' ? '近 7d' : '近 30d'
  if (!window) return `${prefix} · 窗口样本数未知`
  return `${prefix} · ${window.sample_count} 个原始样本`
}

function pointHasFiniteMetric(point: HostMetricSeriesPoint): boolean {
  return [
    point.cpu_usage_pct,
    point.mem_used_pct,
    point.disk_used_pct,
    point.inode_used_pct,
    point.load_5,
    point.load_1,
    point.load_15,
    point.swap_used_pct,
    point.cpu_iowait_pct,
    point.disk_busy_pct,
    point.net_in_bytes_per_sec,
    point.net_out_bytes_per_sec,
    point.disk_read_bytes_per_sec,
    point.disk_write_bytes_per_sec,
  ].some((value) => value != null && Number.isFinite(value))
}

function windowRange(runtimeWindow: MonitoringRuntimeWindow | undefined, points: HostMetricSeriesPoint[]): {
  start: string | null
  end: string | null
} {
  const availableStart = runtimeWindow?.available_started_at ?? null
  const availableEnd = runtimeWindow?.available_ended_at ?? null
  if (availableStart && availableEnd) return { start: availableStart, end: availableEnd }
  const present = points.filter(pointHasFiniteMetric)
  return {
    start: present.at(0)?.observed_at ?? null,
    end: present.at(-1)?.observed_at ?? null,
  }
}

function windowKindLabel(timeWindow: TimeWindow): string {
  if (timeWindow === 'realtime') return '实时'
  if (timeWindow === '24h') return '近 24h'
  if (timeWindow === '7d') return '近 7d'
  return '近 30d'
}

export function MonitoringDetailObservations({
  timeRangeControl,
  sample,
  metricPoints,
  timeWindow,
  window: runtimeWindow,
  isMaintenance = false,
  thresholds,
  loading,
  error,
  onRetryThresholds,
}: ObservationsProps) {
  const gridRef = useRef<HTMLDivElement>(null)
  const [layout, setLayout] = useState<Layout>('wide')
  const [hoveredAt, setHoveredAt] = useState<string | null>(null)

  useEffect(() => {
    const element = gridRef.current
    if (!element) return

    const measure = () => {
      const width = element.getBoundingClientRect().width
      const viewportNarrow =
        typeof globalThis.matchMedia === 'function' &&
        globalThis.matchMedia(NARROW_VIEWPORT_QUERY).matches
      if (viewportNarrow || (width > 0 && width < NARROW_CONTAINER_PX)) setLayout('narrow')
      else if (width > 0 && width < WIDE_CONTAINER_PX) setLayout('medium')
      else setLayout('wide')
    }

    measure()

    let observer: ResizeObserver | undefined
    if (typeof globalThis.ResizeObserver !== 'undefined') {
      observer = new globalThis.ResizeObserver(measure)
      observer.observe(element)
    }
    const mediaQuery =
      typeof globalThis.matchMedia === 'function'
        ? globalThis.matchMedia(NARROW_VIEWPORT_QUERY)
        : null
    mediaQuery?.addEventListener?.('change', measure)

    return () => {
      observer?.disconnect()
      mediaQuery?.removeEventListener?.('change', measure)
    }
  }, [])

  const ascending = useMemo(() => toAscending(metricPoints), [metricPoints])
  const cpuSeries = useMemo(() => toSeries(ascending, (s) => s.cpu_usage_pct), [ascending])
  const memSeries = useMemo(() => toSeries(ascending, (s) => s.mem_used_pct), [ascending])
  const swapSeries = useMemo(() => toSeries(ascending, (s) => s.swap_used_pct ?? null), [ascending])
  const diskSeries = useMemo(() => toSeries(ascending, (s) => s.disk_used_pct), [ascending])
  const inodeSeries = useMemo(() => toSeries(ascending, (s) => s.inode_used_pct), [ascending])
  const load1Series = useMemo(() => toSeries(ascending, (s) => s.load_1 ?? null), [ascending])
  const loadSeries = useMemo(() => toSeries(ascending, (s) => s.load_5), [ascending])
  const load15Series = useMemo(() => toSeries(ascending, (s) => s.load_15 ?? null), [ascending])
  const iowaitSeries = useMemo(() => toSeries(ascending, (s) => s.cpu_iowait_pct), [ascending])
  const diskBusySeries = useMemo(() => toSeries(ascending, (s) => s.disk_busy_pct ?? null), [ascending])
  const netInSeries = useMemo(() => toSeries(ascending, (s) => s.net_in_bytes_per_sec), [ascending])
  const netOutSeries = useMemo(() => toSeries(ascending, (s) => s.net_out_bytes_per_sec), [ascending])
  const diskReadSeries = useMemo(() => toSeries(ascending, (s) => s.disk_read_bytes_per_sec ?? null), [ascending])
  const diskWriteSeries = useMemo(() => toSeries(ascending, (s) => s.disk_write_bytes_per_sec ?? null), [ascending])

  const pointCount = ascending.length
  const lastPoint = ascending.at(-1) ?? null
  const lastFinitePoint = [...ascending].reverse().find(pointHasFiniteMetric) ?? null
  const range = windowRange(runtimeWindow, ascending)
  const hoverAt = hoveredAt
  const liveAt = timeWindow === 'realtime' ? lastPoint?.observed_at ?? lastFinitePoint?.observed_at ?? null : null

  const netYMaxRaw = seriesMax([...netInSeries, ...netOutSeries])
  const netYMax = netYMaxRaw === undefined ? 1 : Math.max(netYMaxRaw * 1.1, 1)
  const diskIOYMaxRaw = seriesMax([...diskReadSeries, ...diskWriteSeries])
  const diskIOYMax = diskIOYMaxRaw === undefined ? 1 : Math.max(diskIOYMaxRaw * 1.1, 1)

  const loadDataMax = seriesMax([...load1Series, ...loadSeries, ...load15Series])
  const loadYMax = loadDataMax === undefined ? undefined : niceMax(loadDataMax * 1.15)
  const iowaitDataMax = seriesMax([...iowaitSeries, ...diskBusySeries])
  const iowaitYMax = iowaitDataMax === undefined ? undefined : niceMax(iowaitDataMax * 1.15)

  const primaryTone = isMaintenance ? 'maintenance' : 'accent'
  const outboundTone = isMaintenance ? 'maintenance' : 'accent-2'

  const shared = {
    hoveredAt,
    onHoverAtChange: setHoveredAt,
    allowSinglePoint: true,
  }

  const chartHint = (levels: string, extra = '') =>
    thresholds ? `${levels}。${extra}`.trim() : `阈值策略不可用。${extra}`.trim()

  const cpuHint = chartHint(
    thresholds ? thresholdLevelsText(thresholds.cpu, '%') : '',
    'CPU 使用率可能包含也可能不包含 I/O 等待时间，两者分开显示。',
  )
  const memHint = chartHint(
    thresholds ? thresholdLevelsText(thresholds.mem, '%') : '',
    '内存占用和交换都是 0–100%，共用一张图。警告线用内存的阈值。',
  )
  const diskHint = chartHint(
    thresholds ? thresholdLevelsText(thresholds.disk, '%') : '',
    '磁盘占用，不是 Inode，也不是磁盘繁忙。',
  )
  const inodeHint = chartHint(
    thresholds ? thresholdLevelsText(thresholds.inode, '%') : '',
    'Inode 耗尽和磁盘容量不是同一件事。',
  )
  const loadHint = thresholds
    ? `三条线是 1 / 5 / 15 分钟负载。阈值看 5 分钟：关注 ≥ ${thresholds.load5.warning}，告警 ≥ ${thresholds.load5.alert}，严重 ≥ ${thresholds.load5.critical}。无核数，不能按核归一。`
    : '三条线是 1 / 5 / 15 分钟负载。阈值策略不可用。无核数，不能按核归一。'
  const iowaitHint = chartHint(
    thresholds ? thresholdLevelsText(thresholds.iowait, '%') : '',
    'I/O 等待是 CPU 在等 I/O；磁盘繁忙是盘本身的利用率。共用按数据缩放的纵轴：繁忙远大于等待时，等待线会贴近底轴，读右上角和图例。',
  )
  const netHint = '下行与上行速率（B/s），两条都为正、共用纵轴。无效速率显示为缺口，不画成 0。'
  const diskIOHint = '磁盘读与写速率（B/s），两条都为正、共用纵轴。'

  const cpuReadout = seriesValueAt(cpuSeries, hoveredAt)
  const memReadout = seriesValueAt(memSeries, hoveredAt)
  const swapReadout = seriesValueAt(swapSeries, hoveredAt)
  const diskReadout = seriesValueAt(diskSeries, hoveredAt)
  const inodeReadout = seriesValueAt(inodeSeries, hoveredAt)
  const loadReadout = seriesValueAt(loadSeries, hoveredAt)
  const iowaitReadout = seriesValueAt(iowaitSeries, hoveredAt)
  const diskBusyReadout = seriesValueAt(diskBusySeries, hoveredAt)
  const netInReadout = seriesValueAt(netInSeries, hoveredAt)
  const netOutReadout = seriesValueAt(netOutSeries, hoveredAt)
  const diskReadReadout = seriesValueAt(diskReadSeries, hoveredAt)
  const diskWriteReadout = seriesValueAt(diskWriteSeries, hoveredAt)

  const realtimeWaiting = timeWindow === 'realtime' && pointCount === 0
  // A window can hold buckets that are all gaps: no samples is still "no samples".
  const historyHasNoSamples =
    timeWindow !== 'realtime' &&
    (runtimeWindow !== undefined ? runtimeWindow.sample_count === 0 : pointCount === 0)
  const showEmptyLine = !realtimeWaiting && (pointCount === 0 || (!loading && historyHasNoSamples))

  const placeholder = realtimeWaiting ? (
    <p className="monitoring-detail-chart__waiting" role="status">等待实时样本</p>
  ) : null

  const seriesLegend = (...items: Array<{ label: string; swatch: 'down' | 'up' | 'tertiary' }>) => (
    <>
      {items.map((item) => (
        <span key={item.label} className="monitoring-detail-chart__legend-item">
          <i
            className={[
              'monitoring-detail-chart__legend-swatch',
              `monitoring-detail-chart__legend-swatch--${item.swatch}`,
              isMaintenance && item.swatch !== 'tertiary' && 'monitoring-detail-chart__legend-swatch--maintenance',
            ].filter(Boolean).join(' ')}
            aria-hidden
          />
          {item.label}
        </span>
      ))}
    </>
  )

  const percentChart = (
    samples: typeof cpuSeries,
    alertFrom: number | undefined,
    ariaLabel: string,
  ) =>
    placeholder ?? (
      <MetricChart
        samples={samples}
        {...shared}
        tone={primaryTone}
        height={OBSERVATION_CHART_HEIGHT}
        paddingLeft={PLOT_GUTTER}
        yMin={0}
        yMax={100}
        {...(alertFrom !== undefined ? { alertBandFrom: alertFrom } : {})}
        formatValue={(v) => formatPercent(v)}
        formatAxisValue={(v) => formatPercent(v, 0)}
        ariaLabel={ariaLabel}
      />
    )

  return (
    <section className="monitoring-detail-section monitoring-detail-observations" aria-label="资源趋势">
      <header className="monitoring-detail-section__head monitoring-detail-observations__head">
        <h2>资源趋势</h2>
        <div className="monitoring-detail-observations__toolbar">
          {thresholds ? null : (
            <span className="monitoring-detail-observations__readout">
              阈值策略不可用{' '}
              <Button variant="ghost" size="sm" onClick={onRetryThresholds}>重试策略</Button>
            </span>
          )}
          {timeRangeControl}
        </div>
      </header>
      {showEmptyLine ? (
        <p className="monitoring-detail-observations__empty" role="status">
          {loading ? '正在加载运行指标…' : error ? '运行指标不可用。' : '该窗口没有样本'}
        </p>
      ) : null}
      <div
        className="monitoring-detail-charts"
        data-layout={layout}
        ref={gridRef}
        role="group"
        aria-label="资源趋势"
      >
        <Plot
          variant="cpu"
          title="CPU 使用率"
          hint={cpuHint}
          tone={toneFor(cpuReadout, thresholds?.cpu)}
          current={
            <span className={`monitoring-detail-chart__value monitoring-detail-chart__value--${toneFor(cpuReadout, thresholds?.cpu)}`}>
              <MonoDigits>{formatPercent(cpuReadout)}</MonoDigits>
            </span>
          }
          notes={<Note label="被窃取">{formatPercent(sample?.cpu_steal_pct)}</Note>}
        >
          {percentChart(cpuSeries, thresholds?.cpu.warning, `CPU 使用率${availabilityLine(timeWindow, runtimeWindow)}趋势`)}
        </Plot>

        <Plot
          variant="mem"
          title="内存使用率"
          hint={memHint}
          tone={toneFor(memReadout, thresholds?.mem)}
          legend={seriesLegend({ label: '使用率', swatch: 'down' }, { label: '交换', swatch: 'up' })}
          current={
            <span className={`monitoring-detail-chart__value monitoring-detail-chart__value--${toneFor(memReadout, thresholds?.mem)}`}>
              <MonoDigits>{formatPercent(memReadout)} · {formatPercent(swapReadout)}</MonoDigits>
            </span>
          }
          notes={
            <>
              <Note label="可用">{formatBytes(sample?.mem_available_bytes)}</Note>
              <Note label="容量">{formatCapacityBytes(sample?.mem_total_bytes)}</Note>
            </>
          }
        >
          {placeholder ?? (
            <MetricChart
              samples={memSeries}
              secondarySamples={swapSeries}
              secondaryTone={outboundTone}
              {...shared}
              tone={primaryTone}
              height={OBSERVATION_CHART_HEIGHT}
              paddingLeft={PLOT_GUTTER}
              yMin={0}
              yMax={100}
              {...(thresholds ? { alertBandFrom: thresholds.mem.warning } : {})}
              formatValue={(v) => formatPercent(v)}
              formatAxisValue={(v) => formatPercent(v, 0)}
              ariaLabel={`内存使用率与交换${availabilityLine(timeWindow, runtimeWindow)}趋势`}
            />
          )}
        </Plot>

        <Plot
          variant="disk"
          title="磁盘使用率"
          hint={diskHint}
          tone={toneFor(diskReadout, thresholds?.disk)}
          current={
            <span className={`monitoring-detail-chart__value monitoring-detail-chart__value--${toneFor(diskReadout, thresholds?.disk)}`}>
              <MonoDigits>{formatPercent(diskReadout)}</MonoDigits>
            </span>
          }
          notes={<Note label="容量">{formatCapacityBytes(sample?.disk_total_bytes)}</Note>}
        >
          {percentChart(diskSeries, thresholds?.disk.warning, `磁盘使用率${availabilityLine(timeWindow, runtimeWindow)}趋势`)}
        </Plot>

        <Plot
          variant="inode"
          title="Inode 使用率"
          hint={inodeHint}
          tone={toneFor(inodeReadout, thresholds?.inode)}
          current={
            <span className={`monitoring-detail-chart__value monitoring-detail-chart__value--${toneFor(inodeReadout, thresholds?.inode)}`}>
              <MonoDigits>{formatPercent(inodeReadout)}</MonoDigits>
            </span>
          }
        >
          {percentChart(inodeSeries, thresholds?.inode.warning, `Inode 使用率${availabilityLine(timeWindow, runtimeWindow)}趋势`)}
        </Plot>

        <Plot
          variant="load"
          title="负载"
          hint={loadHint}
          tone={toneFor(loadReadout, thresholds?.load5)}
          legend={seriesLegend(
            { label: '5 分钟', swatch: 'down' },
            { label: '1 分钟', swatch: 'up' },
            { label: '15 分钟', swatch: 'tertiary' },
          )}
          current={
            <span className={`monitoring-detail-chart__value monitoring-detail-chart__value--${toneFor(loadReadout, thresholds?.load5)}`}>
              <MonoDigits>{formatNumber(loadReadout)}</MonoDigits>
            </span>
          }
        >
          {placeholder ?? (
            <MetricChart
              samples={loadSeries}
              secondarySamples={load1Series}
              secondaryTone={outboundTone}
              tertiarySamples={load15Series}
              tertiaryTone="muted"
              {...shared}
              tone={primaryTone}
              height={OBSERVATION_CHART_HEIGHT}
              paddingLeft={PLOT_GUTTER}
              yMin={0}
              {...(loadYMax === undefined ? {} : { yMax: loadYMax })}
              {...(thresholds && loadYMax !== undefined
                ? { thresholds: scaledThresholdLines(thresholds.load5, loadYMax) }
                : {})}
              formatValue={(v) => formatNumber(v)}
              formatAxisValue={(v) => formatNumber(v)}
              ariaLabel={`1 / 5 / 15 分钟负载${availabilityLine(timeWindow, runtimeWindow)}趋势`}
            />
          )}
        </Plot>

        <Plot
          variant="iowait"
          title="I/O 等待"
          hint={iowaitHint}
          tone={toneFor(iowaitReadout, thresholds?.iowait)}
          legend={seriesLegend({ label: '等待', swatch: 'down' }, { label: '繁忙', swatch: 'up' })}
          current={
            <span className={`monitoring-detail-chart__value monitoring-detail-chart__value--${toneFor(iowaitReadout, thresholds?.iowait)}`}>
              <MonoDigits>{formatPercent(iowaitReadout)} · {formatPercent(diskBusyReadout)}</MonoDigits>
            </span>
          }
        >
          {placeholder ?? (
            <MetricChart
              samples={iowaitSeries}
              secondarySamples={diskBusySeries}
              secondaryTone={outboundTone}
              {...shared}
              tone={primaryTone}
              height={OBSERVATION_CHART_HEIGHT}
              paddingLeft={PLOT_GUTTER}
              yMin={0}
              {...(iowaitYMax === undefined ? {} : { yMax: iowaitYMax })}
              {...(thresholds && iowaitYMax !== undefined
                ? { thresholds: scaledThresholdLines(thresholds.iowait, iowaitYMax) }
                : {})}
              formatValue={(v) => formatPercent(v)}
              formatAxisValue={(v) => formatPercent(v, iowaitYMax !== undefined && iowaitYMax < 5 ? 1 : 0)}
              ariaLabel={`CPU I/O 等待与磁盘繁忙${availabilityLine(timeWindow, runtimeWindow)}趋势`}
            />
          )}
        </Plot>

        <Plot
          variant="network"
          title="网络"
          hint={netHint}
          legend={seriesLegend({ label: '下行', swatch: 'down' }, { label: '上行', swatch: 'up' })}
          current={
            <span className="monitoring-detail-chart__value monitoring-detail-chart__value--primary">
              <MonoDigits>↓ {formatBytesPerSecond(netInReadout)} ↑ {formatBytesPerSecond(netOutReadout)}</MonoDigits>
            </span>
          }
        >
          {placeholder ?? (
            <MetricChart
              samples={netInSeries}
              secondarySamples={netOutSeries}
              secondaryTone={outboundTone}
              {...shared}
              tone={primaryTone}
              height={OBSERVATION_CHART_HEIGHT}
              paddingLeft={PLOT_GUTTER}
              yMin={0}
              yMax={netYMax}
              formatValue={(v) => formatBytesPerSecond(v)}
              formatAxisValue={formatCompactRateAxis}
              ariaLabel={`网络下行与上行${availabilityLine(timeWindow, runtimeWindow)}趋势`}
            />
          )}
        </Plot>

        <Plot
          variant="disk-io"
          title="磁盘读写"
          hint={diskIOHint}
          legend={seriesLegend({ label: '读', swatch: 'down' }, { label: '写', swatch: 'up' })}
          current={
            <span className="monitoring-detail-chart__value monitoring-detail-chart__value--primary">
              <MonoDigits>↓ {formatBytesPerSecond(diskReadReadout)} ↑ {formatBytesPerSecond(diskWriteReadout)}</MonoDigits>
            </span>
          }
        >
          {placeholder ?? (
            <MetricChart
              samples={diskReadSeries}
              secondarySamples={diskWriteSeries}
              secondaryTone={outboundTone}
              {...shared}
              tone={primaryTone}
              height={OBSERVATION_CHART_HEIGHT}
              paddingLeft={PLOT_GUTTER}
              yMin={0}
              yMax={diskIOYMax}
              formatValue={(v) => formatBytesPerSecond(v)}
              formatAxisValue={formatCompactRateAxis}
              ariaLabel={`磁盘读与写${availabilityLine(timeWindow, runtimeWindow)}趋势`}
            />
          )}
        </Plot>
      </div>
      <p className="monitoring-detail-observations__footer">
        {windowKindLabel(timeWindow)}
        {timeWindow !== 'realtime' && range.start && range.end ? (
          <>
            {' · '}
            <Timestamp value={range.start} mode="absolute" />
            {' – '}
            <Timestamp value={range.end} mode="absolute" />
          </>
        ) : timeWindow !== 'realtime' && !showEmptyLine ? (
          ' · 窗口区间未知'
        ) : null}
        {hoverAt ? (
          <>
            {' · 选中 '}
            <Timestamp value={hoverAt} mode="absolute" />
          </>
        ) : liveAt ? (
          <>
            {' · 实时点 '}
            <Timestamp value={liveAt} mode="absolute" />
          </>
        ) : null}
      </p>
    </section>
  )
}
