import {
  MonoDigits,
  Sparkline,
} from '../../components/atoms'
import type { TargetRecord, TargetSparklinesResponse } from '../../lib/types'

type TargetsTrendCellProps = {
  target: TargetRecord
  sparklines: TargetSparklinesResponse | null
}

export function TargetsTrendCell({ target, sparklines }: TargetsTrendCellProps) {
  const series = sparklines?.targets?.[target.target_id]
  if (!series || !series.latency) {
    return <span className="targets-table__trends-empty">—</span>
  }

  const vals = series.latency.filter((value): value is number => value != null)
  const latest = vals.length > 0 ? vals[vals.length - 1] : null
  const tone = !latest
    ? 'default'
    : latest > 1000
      ? 'critical'
      : latest > 200
        ? 'alert'
        : latest > 10
          ? 'notice'
          : 'accent'

  return (
    <span className="targets-table__trend-strip">
      <span className="targets-table__trend-item">
        <span className="targets-table__trend-value">
          {latest != null ? <MonoDigits>{latest.toFixed(1)} ms</MonoDigits> : '—'}
        </span>
        {vals.length > 0 ? (
          <Sparkline values={vals} tone={tone} width={64} height={14} />
        ) : (
          <span className="targets-table__trends-empty">—</span>
        )}
      </span>
    </span>
  )
}
