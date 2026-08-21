import type { ComparisonEvaluateResponse, ComparisonReason } from '../../../lib/types'

const REASON_LABELS: Record<ComparisonReason, string> = {
  metadata_only: '仅元数据，无数值比较',
  kind_missing: '缺少该证据类型',
  metric_missing: '缺少该指标',
  coverage_partial: '覆盖不完整',
  coverage_truncated: '序列被截断',
  common_overlap_unsupported: '当前类型不支持共同重叠',
  common_overlap_empty: '共同重叠为空',
  schema_incompatible: 'schema 不兼容',
  unit_incompatible: '单位不兼容',
  precision_incompatible: '精度不兼容',
  source_tombstoned: '来源已墓碑化',
  source_unavailable: '来源当前不可用',
  snapshot_unreadable: '快照不可读',
}

type Props = {
  comparison: ComparisonEvaluateResponse | null
}

export function ComparabilityReview({ comparison }: Props) {
  if (!comparison) return null
  const findings = comparison.review
  return (
    <section aria-labelledby="comparison-review-heading">
      <div className="section-heading">
        <p className="section-heading__eyebrow">审查</p>
        <h2 className="section-heading__title" id="comparison-review-heading">可比性审查</h2>
        <p className="section-heading__description">先看兼容与缺口，再看任何指标或图表。</p>
      </div>
      {findings.length === 0 ? (
        <p>当前选择没有额外可比性告警。</p>
      ) : (
        <ul>
          {findings.map((finding, index) => (
            <li key={`${finding.item_index}-${finding.reason}-${index}`}>
              第 {finding.item_index + 1} 项
              {finding.kind ? ` · ${finding.kind}/v${finding.schema_version}` : ''}
              ：{REASON_LABELS[finding.reason]}
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}
