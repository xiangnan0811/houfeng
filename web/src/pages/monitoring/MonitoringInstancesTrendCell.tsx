import { MonoDigits, Sparkline } from '../../components/atoms'
import { DEFAULT_THRESHOLDS, type MetricThreshold, type MetricThresholds } from '../../config/thresholds'
import { formatPercent } from '../../lib/format'
import type { MonitoringInstanceRecord, MonitoringInstanceSparklinesResponse } from '../../lib/types'

type MonitoringInstancesTrendCellProps = {
  monitoringInstance: MonitoringInstanceRecord
  sparklines: MonitoringInstanceSparklinesResponse | null
  thresholds?: MetricThresholds
}

function toneFromThreshold(value: number | null, threshold: MetricThreshold) {
  if (value == null) return 'default'
  if (value >= threshold.critical) return 'critical'
  if (value >= threshold.alert) return 'alert'
  if (value >= threshold.warning) return 'notice'
  return 'accent'
}

export function MonitoringInstancesTrendCell({ monitoringInstance, sparklines, thresholds = DEFAULT_THRESHOLDS }: MonitoringInstancesTrendCellProps) {
  const series = sparklines?.monitoring_instances?.[monitoringInstance.monitoring_instance_id]
  if (!series) {
    return <span className="monitoring-table__trends-empty">—</span>
  }
  const cpu = series.cpu_usage_pct
  const mem = series.mem_used_pct
  const disk = series.disk_used_pct
  const latestCpu = cpu?.[cpu.length - 1] ?? null
  const latestMem = mem?.[mem.length - 1] ?? null
  const latestDisk = disk?.[disk.length - 1] ?? null

  const cpuTone = toneFromThreshold(latestCpu, thresholds.cpu)
  const memTone = toneFromThreshold(latestMem, thresholds.mem)
  const diskTone = toneFromThreshold(latestDisk, thresholds.disk)

  return (
    <span className="monitoring-table__trend-strip">
      <span className="monitoring-table__trend-item">
        <span className="monitoring-table__trend-value">
          {latestCpu != null ? <MonoDigits>{formatPercent(latestCpu)}</MonoDigits> : '—'}
        </span>
        {cpu && cpu.length > 0 ? (
          <Sparkline values={cpu.filter((value): value is number => value != null)} tone={cpuTone} width={64} height={14} />
        ) : (
          <span className="monitoring-table__trends-empty">—</span>
        )}
      </span>
      <span className="monitoring-table__trend-item">
        <span className="monitoring-table__trend-value">
          {latestMem != null ? <MonoDigits>{formatPercent(latestMem)}</MonoDigits> : '—'}
        </span>
        {mem && mem.length > 0 ? (
          <Sparkline values={mem.filter((value): value is number => value != null)} tone={memTone} width={64} height={14} />
        ) : (
          <span className="monitoring-table__trends-empty">—</span>
        )}
      </span>
      <span className="monitoring-table__trend-item">
        <span className="monitoring-table__trend-value">
          {latestDisk != null ? <MonoDigits>{formatPercent(latestDisk)}</MonoDigits> : '—'}
        </span>
        {disk && disk.length > 0 ? (
          <Sparkline values={disk.filter((value): value is number => value != null)} tone={diskTone} width={64} height={14} />
        ) : (
          <span className="monitoring-table__trends-empty">—</span>
        )}
      </span>
    </span>
  )
}
