import type { ComparisonEvaluateResponse, ComparisonReason } from '../../../lib/types'
import { isHostOrProbeKind, seriesForKindAndMetric } from './comparisonQueryState'

const REASON_LABELS: Partial<Record<ComparisonReason, string>> = {
  metadata_only: '仅元数据',
  kind_missing: '缺少类型',
  metric_missing: '缺少指标',
  coverage_partial: '覆盖不完整',
  coverage_truncated: '覆盖被截断',
  common_overlap_unsupported: '不支持共同重叠',
  common_overlap_empty: '重叠为空',
  schema_incompatible: 'schema 不兼容',
  snapshot_unreadable: '不可读',
}

type Props = {
  kind?: string
  metric?: string
  comparison: ComparisonEvaluateResponse
}

export function ComparisonMatrix({ kind, metric, comparison }: Props) {
  if (!isHostOrProbeKind(kind)) return null
  const headingId = 'comparison-matrix-heading'
  const hintId = 'comparison-matrix-hint'
  const series = seriesForKindAndMetric(comparison.series, kind, metric)
  return (
    <section>
      <div className="section-heading">
        <h2 className="section-heading__title" id={headingId}>对齐矩阵</h2>
        <p className="section-heading__description" id={hintId}>
          一次只看 {kind}{metric ? ` / ${metric}` : ''}。不可计算的格子用文字说明原因，不只靠颜色。
        </p>
      </div>
      <div
        className="page-panel--scroll-x"
        role="region"
        tabIndex={0}
        aria-labelledby={headingId}
        aria-describedby={hintId}
      >
        <table className="table">
          <caption className="visually-hidden">比较项矩阵</caption>
          <thead>
            <tr>
              <th scope="col">项</th>
              <th scope="col">类型</th>
              <th scope="col">覆盖</th>
              <th scope="col">桶数</th>
              <th scope="col">质量</th>
              <th scope="col">规范哈希</th>
              <th scope="col">修订上下文</th>
              <th scope="col">说明</th>
            </tr>
          </thead>
          <tbody>
            {comparison.items.map((item, index) => {
              const finding = comparison.review.find((entry) => entry.item_index === index)
              const pair = comparison.pairwise.find((entry) => entry.item_index === index)
              const itemSeries = series.find((entry) => entry.item_index === index)
              return (
                <tr key={`${item.snapshot_id}-${index}`}>
                  <th scope="row">第 {index + 1} 项</th>
                  <td>{item.kind}/v{item.schema_version}</td>
                  <td>{coverageLabel(pair?.values, finding?.reason)}</td>
                  <td className="mono-digits">{bucketCount(itemSeries)}</td>
                  <td>{qualityLabel(pair?.values, finding?.reason)}</td>
                  <td className="mono-digits">{item.canonical_hash}</td>
                  <td>{item.revision_context === 'not_applicable' ? '不适用' : '绑定修订'}</td>
                  <td>{finding ? (REASON_LABELS[finding.reason] ?? finding.reason) : '可比较'}</td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </section>
  )
}

function numberValue(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}

function bucketCount(series: { segments: unknown[][] } | undefined): number {
  if (!series) return 0
  return series.segments.reduce((total, segment) => total + segment.length, 0)
}

function coverageLabel(values: Record<string, unknown> | undefined, reason?: ComparisonReason): string {
  const matched = numberValue(values?.matched)
  const unmatchedBaseline = numberValue(values?.unmatched_baseline) ?? 0
  const unmatchedItem = numberValue(values?.unmatched_item) ?? 0
  if (matched != null) {
    const total = matched + unmatchedBaseline + unmatchedItem
    if (unmatchedBaseline + unmatchedItem === 0) return `完整 ${matched}/${total || matched}`
    return `部分 ${matched}/${total}`
  }
  if (reason === 'coverage_partial' || reason === 'coverage_truncated') {
    return REASON_LABELS[reason] ?? reason
  }
  return '无对齐'
}

function qualityLabel(values: Record<string, unknown> | undefined, reason?: ComparisonReason): string {
  if (values?.equal === true) return '相等'
  if (values?.equal === false) return '有差值'
  if (reason) return REASON_LABELS[reason] ?? reason
  return '未评估'
}
