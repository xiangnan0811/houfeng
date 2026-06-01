import { MonoDigits, Sparkline } from '../../components/atoms'
import { DEFAULT_THRESHOLDS } from '../../config/thresholds'
import { formatPercent } from '../../lib/format'
import type { MonitoringInstanceRecord, MonitoringInstanceSparklinesResponse } from '../../lib/types'

type MonitoringInstancesTrendCellProps = {
  monitoringInstance: MonitoringInstanceRecord
  sparklines: MonitoringInstanceSparklinesResponse | null
}

export function MonitoringInstancesTrendCell({ monitoringInstance, sparklines }: MonitoringInstancesTrendCellProps) {
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

  const thresholds = DEFAULT_THRESHOLDS
  const cpuTone = !latestCpu ? 'default' : latestCpu >= thresholds.cpu.critical ? 'critical' : latestCpu >= thresholds.cpu.notice ? 'alert' : 'accent'
  const memTone = !latestMem ? 'default' : latestMem >= thresholds.mem.critical ? 'critical' : latestMem >= thresholds.mem.notice ? 'alert' : 'accent'
  const diskTone = !latestDisk ? 'default' : latestDisk >= thresholds.disk.critical ? 'critical' : latestDisk >= thresholds.disk.notice ? 'alert' : 'accent'

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
