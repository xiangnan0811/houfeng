import { SegmentedControl } from '../../../components/atoms'
import type { ComparisonEvaluateResponse } from '../../../lib/types'
import {
  comparisonKindKey,
  comparisonSeriesMetrics,
  defaultComparisonMetric,
  isHostOrProbeKind,
} from './comparisonQueryState'

type Props = {
  comparison: ComparisonEvaluateResponse
  activeKind?: string
  metric?: string
  onSelect: (kind: string, metric?: string) => void
}

export function ComparisonKindPanel({ comparison, activeKind, metric, onSelect }: Props) {
  const items = comparison.available_kinds.map((key) => {
    const label = comparisonKindKey(key)
    return { value: label, label }
  })
  if (items.length === 0) {
    return <p role="status">没有兼容的证据类型可比较。</p>
  }
  const fallback = items[0]
  if (!fallback) {
    return <p role="status">没有兼容的证据类型可比较。</p>
  }
  const value = items.some((item) => item.value === activeKind) && activeKind
    ? activeKind
    : fallback.value
  const metrics = isHostOrProbeKind(value) ? comparisonSeriesMetrics(comparison.series) : []
  const fallbackMetric = metrics[0]
  const selectedMetric = metric && metrics.includes(metric)
    ? metric
    : (fallbackMetric ?? defaultComparisonMetric(value))
  return (
    <section aria-labelledby="comparison-kinds-heading">
      <div className="section-heading">
        <h2 className="section-heading__title" id="comparison-kinds-heading">证据类型</h2>
      </div>
      <SegmentedControl
        label="比较证据类型"
        items={items}
        value={value}
        onChange={(kind) => onSelect(kind, defaultComparisonMetric(kind) ?? selectedMetric)}
      />
      {isHostOrProbeKind(value) && fallbackMetric ? (
        <SegmentedControl
          label="比较指标"
          items={metrics.map((item) => ({ value: item, label: item }))}
          value={selectedMetric ?? fallbackMetric}
          onChange={(next) => onSelect(value, next)}
        />
      ) : isHostOrProbeKind(value) ? (
        <p>当前指标 {selectedMetric || '未选择'}，窄屏一次只比较这一个类型与指标。</p>
      ) : (
        <p>当前类型没有时序图，只展示精确比较结果。</p>
      )}
    </section>
  )
}
