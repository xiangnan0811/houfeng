import { Input, Select } from '../../../components/atoms'
import type { ComparisonAlignment } from '../../../lib/types'
import type { ComparisonURLState } from './comparisonQueryState'

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
  return (
    <section aria-labelledby="comparison-conditions-heading">
      <details open>
        <summary className="section-heading">
          <p className="section-heading__eyebrow">条件</p>
          <h2 className="section-heading__title" id="comparison-conditions-heading">比较条件</h2>
        </summary>
      <div className="metric-grid">
        <Input
          label="请求开始"
          type="text"
          value={query.requested_from}
          onChange={(event) => onWindow(event.target.value, query.requested_to)}
        />
        <Input
          label="请求结束"
          type="text"
          value={query.requested_to}
          onChange={(event) => onWindow(query.requested_from, event.target.value)}
        />
        <Select
          label="对齐"
          value={query.alignment ?? 'actual_coverage'}
          onChange={(event) => onAlignment(event.target.value as ComparisonAlignment)}
        >
          <option value="actual_coverage">实际覆盖</option>
          <option value="common_overlap">共同重叠</option>
        </Select>
        <Input
          label="容差（秒）"
          type="number"
          value={String(query.tolerance_seconds ?? 60)}
          onChange={(event) => onTolerance(Number(event.target.value))}
        />
        <Input
          label="桶宽（秒）"
          type="number"
          value={query.bucket_seconds == null ? '' : String(query.bucket_seconds)}
          onChange={(event) => {
            const next = event.target.value.trim()
            onBucket(next === '' ? null : Number(next))
          }}
        />
        {query.mode === 'fixed' ? (
          <Select
            label="基准项"
            value={String(query.baseline ?? 0)}
            onChange={(event) => onBaseline(Number(event.target.value))}
          >
            {Array.from({ length: itemCount }, (_, index) => (
              <option key={index} value={index}>第 {index + 1} 项{index === 0 ? '（默认建议）' : ''}</option>
            ))}
          </Select>
        ) : null}
      </div>
      </details>
    </section>
  )
}
