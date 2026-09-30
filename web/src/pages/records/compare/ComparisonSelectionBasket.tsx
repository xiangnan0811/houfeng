import { Button } from '../../../components/atoms'
import type { ComparisonCandidateItem } from '../../../lib/types'
import type { ComparisonURLFixedItem, ComparisonURLState } from './comparisonQueryState'

const MAX_ITEMS = 6

type Props = {
  query: ComparisonURLState | null
  candidates: ComparisonCandidateItem[] | null
  onConfirm: (items: ComparisonURLFixedItem[]) => void
}

function itemParts(item: ComparisonURLFixedItem): { kind: string; id: string } {
  if ('snapshot_id' in item) return { kind: '快照', id: item.snapshot_id }
  return { kind: '修订', id: `${item.record_id} / ${item.revision_id}` }
}

export function ComparisonSelectionBasket({ query, candidates, onConfirm }: Props) {
  const items = query?.mode === 'fixed' ? query.items ?? [] : []
  const tooFew = query?.mode !== 'candidate' && items.length < 2
  const baseline = query?.baseline ?? 0
  const candidateMode = query?.mode === 'candidate'
  // 一次最多比较 6 项；候选超出时计数、列表与确认都只取前 6 个，并明确说明。
  const confirmable = candidates?.slice(0, MAX_ITEMS) ?? []
  const omitted = (candidates?.length ?? 0) - confirmable.length
  const count = candidateMode ? confirmable.length : items.length
  return (
    <section className="record-section" aria-labelledby="comparison-basket-heading">
      <div className="record-section__head">
        <h2 className="record-section__title" id="comparison-basket-heading">
          比较对象 <span className={count > 0 ? 'record-count record-count--active' : 'record-count'}>{count}</span>
        </h2>
      </div>
      {tooFew ? (
        <p className="record-muted" role="status">至少选择 2 项才能比较。当前 {items.length} 项。</p>
      ) : null}
      {items.length > 0 ? (
        <ol className="record-compare-items">
          {items.map((item, index) => {
            const parts = itemParts(item)
            return (
              <li key={`${index}-${parts.id}`} className="record-compare-items__item">
                <span className="record-compare-items__index" aria-hidden="true">{index + 1}</span>
                <span className="record-compare-items__kind">{parts.kind}</span>
                <code className="record-compare-items__id">{parts.id}</code>
                {index === baseline && items.length > 1 ? <span className="record-compare-baseline">基准</span> : null}
              </li>
            )
          })}
        </ol>
      ) : null}
      {candidateMode && candidates && candidates.length > 0 ? (
        <>
          <ol className="record-compare-items">
            {confirmable.map((candidate, index) => (
              <li key={candidate.snapshot_id} className="record-compare-items__item">
                <span className="record-compare-items__index" aria-hidden="true">{index + 1}</span>
                <span className="record-compare-items__kind">候选</span>
                <code className="record-compare-items__id">{candidate.snapshot_id}</code>
              </li>
            ))}
          </ol>
          {omitted > 0 ? (
            <p className="record-muted" role="status">共 {candidates.length} 个候选，只比较前 {MAX_ITEMS} 个。</p>
          ) : null}
          <Button
            size="lg"
            className="record-compare-items__confirm"
            onClick={() => onConfirm(confirmable.map((candidate) => (
              candidate.revision_ids[0]
                ? {
                    record_id: candidate.record_id,
                    revision_id: candidate.revision_ids[0],
                    snapshot_ids: [candidate.snapshot_id],
                  }
                : { snapshot_id: candidate.snapshot_id }
            )))}
          >
            确认候选并比较
          </Button>
        </>
      ) : null}
      {candidateMode && candidates && candidates.length === 0 ? (
        <p className="record-muted" role="status">当前主体窗口没有可比较候选。</p>
      ) : null}
    </section>
  )
}
