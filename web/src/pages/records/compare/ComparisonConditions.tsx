import { Input, Select } from '../../../components/atoms'
import type { ComparisonAlignment } from '../../../lib/types'
import type { ComparisonURLState } from './comparisonQueryState'

const ALIGNMENT_LABELS: Record<ComparisonAlignment, string> = {
  actual_coverage: '实际覆盖',
  common_overlap: '共同重叠',
}

type Props = {
  query: ComparisonURLState
  onBaseline: (index: number) => void
  onAlignment: (alignment: ComparisonAlignment) => void
  onWindow: (from: string, to: string) => void
  onTolerance: (seconds: number) => void
  onBucket: (seconds: number | null) => void
}

export function ComparisonConditions({
  query,
  onBaseline,
  onAlignment,
  onWindow,
  onTolerance,
  onBucket,
}: Props) {
  const itemCount = query.mode === 'fixed' ? query.items?.length ?? 0 : 0
  const alignment = query.alignment ?? 'actual_coverage'
  const summary = [
    ALIGNMENT_LABELS[alignment],
    `容差 ${query.tolerance_seconds ?? 60} 秒`,
    query.bucket_seconds == null ? '' : `桶宽 ${query.bucket_seconds} 秒`,
  ].filter(Boolean).join(' · ')
  return (
    <section className="record-section record-compare-conditions" aria-labelledby="comparison-conditions-heading">
      <details open>
        <summary className="record-compare-conditions__summary">
          <h2 className="record-section__title" id="comparison-conditions-heading">比较条件</h2>
          <span className="record-muted">{summary}</span>
        </summary>
        <div className="record-form-grid">
          <div className="record-form-grid__wide">
            <Input
              label="请求开始"
              type="text"
              className="mono"
              value={query.requested_from}
              onChange={(event) => onWindow(event.target.value, query.requested_to)}
            />
          </div>
          <div className="record-form-grid__wide">
            <Input
              label="请求结束"
              type="text"
              className="mono"
              value={query.requested_to}
              onChange={(event) => onWindow(query.requested_from, event.target.value)}
            />
          </div>
          <Select
            label="对齐"
            value={alignment}
            onChange={(event) => onAlignment(event.target.value as ComparisonAlignment)}
          >
            <option value="actual_coverage">{ALIGNMENT_LABELS.actual_coverage}</option>
            <option value="common_overlap">{ALIGNMENT_LABELS.common_overlap}</option>
          </Select>
          {query.mode === 'fixed' ? (
            <Select
              label="基准项"
              value={String(query.baseline ?? 0)}
              onChange={(event) => onBaseline(Number(event.target.value))}
            >
              {Array.from({ length: itemCount }, (_, index) => (
                <option key={index} value={index}>第 {index + 1} 项</option>
              ))}
            </Select>
          ) : null}
          <Input
            label="容差（秒）"
            type="number"
            value={String(query.tolerance_seconds ?? 60)}
            onChange={(event) => onTolerance(Number(event.target.value))}
          />
          <Input
            label="桶宽（秒）"
            type="number"
            placeholder="自动"
            value={query.bucket_seconds == null ? '' : String(query.bucket_seconds)}
            onChange={(event) => {
              const next = event.target.value.trim()
              onBucket(next === '' ? null : Number(next))
            }}
          />
        </div>
      </details>
    </section>
  )
}
