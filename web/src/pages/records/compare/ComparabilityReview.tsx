import { Badge } from '../../../components/atoms'
import type { ComparisonEvaluateResponse } from '../../../lib/types'
import { comparisonEvidenceKindLabel, comparisonEvidenceTechnicalKind, presentComparisonReason } from './comparisonLabels'

type Props = {
  comparison: ComparisonEvaluateResponse | null
}

export function ComparabilityReview({ comparison }: Props) {
  if (!comparison) return null
  const findings = comparison.review
  return (
    <section className="record-section" aria-labelledby="comparison-review-heading">
      <div className="record-section__head">
        <h2 className="record-section__title" id="comparison-review-heading">可比性审查</h2>
        {findings.length === 0
          ? <Badge variant="info" tone="normal">可比较</Badge>
          : <Badge variant="info" tone="notice">{findings.length} 项需注意</Badge>}
      </div>
      {findings.length > 0 ? (
        <ul className="record-compare-findings">
          {findings.map((finding, index) => {
            const reason = presentComparisonReason(finding.reason)
            return (
            <li key={`${finding.item_index}-${finding.reason}-${index}`} className="record-compare-findings__item">
              <span className="record-compare-findings__item-ref">
                第 {finding.item_index + 1} 项{finding.kind ? ` · ${comparisonEvidenceKindLabel(finding.kind)}` : ''}
                {finding.kind && finding.schema_version != null ? (
                  <details>
                    <summary>技术标识</summary>
                    <code>{comparisonEvidenceTechnicalKind(finding.kind, finding.schema_version)}</code>
                  </details>
                ) : null}
              </span>
              <strong>{reason.label}</strong>
              {reason.diagnostic ? (
                <details>
                  <summary>诊断信息</summary>
                  <p>{reason.diagnostic}</p>
                </details>
              ) : null}
            </li>
            )
          })}
        </ul>
      ) : null}
    </section>
  )
}
