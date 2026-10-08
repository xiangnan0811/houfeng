import { ScrollRegion } from '../../../components/atoms'
import type { ComparisonEvaluateResponse, ComparisonReason } from '../../../lib/types'
import { comparisonEvidenceKindLabel, comparisonEvidenceTechnicalKind } from './comparisonLabels'
import { isHostOrProbeKind, seriesForKindAndMetric } from './comparisonQueryState'

const REASON_LABELS: Partial<Record<ComparisonReason, string>> = {
  metadata_only: '仅元数据',
  kind_missing: '缺少类型',
  metric_missing: '缺少指标',
  coverage_partial: '覆盖不完整',
  coverage_truncated: '覆盖被截断',
  common_overlap_unsupported: '不支持共同重叠',
  common_overlap_empty: '重叠为空',
  schema_incompatible: '结构不兼容',
  unit_incompatible: '单位不兼容',
  precision_incompatible: '精度不兼容',
  source_tombstoned: '来源已墓碑化',
  source_unavailable: '来源当前不可用',
  snapshot_unreadable: '不可读',
}

function matrixReasonCopy(reason: string): { label: string; diagnostic: string | null } {
  const label = Object.hasOwn(REASON_LABELS, reason) ? REASON_LABELS[reason as ComparisonReason] : undefined
  if (label) return { label, diagnostic: null }
  return { label: '不兼容', diagnostic: reason }
}

type Props = {
  kind?: string
  metric?: string
  baseline?: number
  comparison: ComparisonEvaluateResponse
}

export function ComparisonMatrix({ kind, metric, baseline = 0, comparison }: Props) {
  if (!isHostOrProbeKind(kind)) return null
  const headingId = 'comparison-matrix-heading'
  const series = seriesForKindAndMetric(comparison.series, kind, metric)
  return (
    <div className="record-compare__block">
      <div className="record-compare__block-head">
        <h3 className="record-compare__block-title" id={headingId}>对齐矩阵</h3>
      </div>
      <ScrollRegion
        labelledBy={headingId}
        hintId="comparison-matrix-hint"
        hint="左右滑动查看全部列"
        className="record-compare-matrix"
        hintClassName="record-compare-matrix__hint"
      >
        <table className="table record-compare-matrix__table">
          <caption className="visually-hidden">比较项矩阵</caption>
          <thead>
            <tr>
              <th scope="col">项</th>
              <th scope="col">类型</th>
              <th scope="col">覆盖</th>
              <th scope="col">桶数</th>
              <th scope="col">质量</th>
              <th scope="col">修订</th>
              <th scope="col">说明</th>
            </tr>
          </thead>
          <tbody>
            {comparison.items.map((item, index) => {
              const finding = comparison.review.find((entry) => entry.item_index === index)
              const pair = comparison.pairwise.find((entry) => entry.item_index === index)
              const itemSeries = series.find((entry) => entry.item_index === index)
              const reasonCopy = finding ? matrixReasonCopy(finding.reason) : null
              return (
                <tr key={`${item.snapshot_id}-${index}`}>
                  <th scope="row">
                    第 {index + 1} 项{index === baseline ? <span className="record-compare-baseline">基准</span> : null}
                  </th>
                  <td>
                    <span>{comparisonEvidenceKindLabel(item.kind)}</span>
                    <details>
                      <summary>技术标识</summary>
                      <code>{comparisonEvidenceTechnicalKind(item.kind, item.schema_version)}</code>
                    </details>
                  </td>
                  <td>{coverageLabel(pair?.values, finding?.reason)}</td>
                  <td className="mono">{bucketCount(itemSeries)}</td>
                  <td>{qualityLabel(pair?.values, finding?.reason)}</td>
                  <td>{item.revision_context === 'not_applicable' ? '不适用' : '绑定修订'}</td>
                  <td className={finding ? 'record-compare-matrix__note--warn' : undefined}>
                    {reasonCopy ? (
                      <>
                        {reasonCopy.label}
                        {reasonCopy.diagnostic ? (
                          <details>
                            <summary>诊断信息</summary>
                            <p>{reasonCopy.diagnostic}</p>
                          </details>
                        ) : null}
                      </>
                    ) : '可比较'}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </ScrollRegion>
    </div>
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
    return matrixReasonCopy(reason).label
  }
  return '无对齐'
}

function qualityLabel(values: Record<string, unknown> | undefined, reason?: ComparisonReason): string {
  if (values?.equal === true) return '相等'
  if (values?.equal === false) return '有差值'
  if (reason) return matrixReasonCopy(reason).label
  return '未评估'
}
