import { Button } from '../../../components/atoms'
import type { ComparisonCandidateItem } from '../../../lib/types'
import type { ComparisonURLFixedItem, ComparisonURLState } from './comparisonQueryState'

type Props = {
  query: ComparisonURLState | null
  candidates: ComparisonCandidateItem[] | null
  onConfirm: (items: ComparisonURLFixedItem[]) => void
}

function itemLabel(item: ComparisonURLFixedItem): string {
  if ('snapshot_id' in item) return `快照 ${item.snapshot_id}`
  return `修订 ${item.record_id} / ${item.revision_id}`
}

export function ComparisonSelectionBasket({ query, candidates, onConfirm }: Props) {
  const items = query?.mode === 'fixed' ? query.items ?? [] : []
  const tooFew = items.length < 2
  return (
    <section aria-labelledby="comparison-basket-heading">
      <div className="section-heading">
        <p className="section-heading__eyebrow">选择</p>
        <h2 className="section-heading__title" id="comparison-basket-heading">选择篮</h2>
        <p className="section-heading__description">2–6 项不可变修订或快照。确认精确 ID 后才开始比较。</p>
      </div>
      {tooFew ? (
        <p role="status">至少选择 2 项才能比较。当前 {items.length} 项。</p>
      ) : null}
      {items.length > 0 ? (
        <ol>
          {items.map((item, index) => (
            <li key={`${index}-${itemLabel(item)}`}>{itemLabel(item)}</li>
          ))}
        </ol>
      ) : null}
      {query?.mode === 'candidate' && candidates && candidates.length > 0 ? (
        <div className="page-state__actions">
          <Button
            size="lg"
            onClick={() => onConfirm(candidates.slice(0, 6).map((candidate) => (
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
        </div>
      ) : null}
      {query?.mode === 'candidate' && candidates && candidates.length === 0 ? (
        <p role="status">当前主体窗口没有可比较候选。</p>
      ) : null}
    </section>
  )
}
