import { useState } from 'react'

import { Button } from '../../../components/atoms'
import { formatDateTime } from '../../../lib/format'
import type { ComparisonCandidateItem } from '../../../lib/types'
import { evidenceKindLabel, identityTypeLabel, QUALITY_BADGE_LABELS } from '../evidence/evidencePresentation'
import { ComparisonRevisionEvidenceDialog } from './ComparisonRevisionEvidenceDialog'
import { evidenceChoiceMeta } from './comparisonEvidenceCopy'
import {
  COMPARISON_FIXED_ITEM_LIMIT,
  COMPARISON_SELECTION_LIMIT_ERROR,
  comparisonFixedItemKey,
  type ComparisonURLFixedItem,
  type ComparisonURLState,
} from './comparisonQueryState'
import { useComparisonBasketIdentity } from './useComparisonBasketIdentity'

type RevisionItem = Extract<ComparisonURLFixedItem, { record_id: string }>

type Props = {
  query: ComparisonURLState | null
  linkProblem: 'missing' | 'invalid' | 'unknown_version' | null
  candidates: ComparisonCandidateItem[] | null
  selectionError: string | null
  onConfirm: (items: ComparisonURLFixedItem[]) => void
  onRemove: (key: string) => void
  onClear: () => void
  onAdd?: () => void
  onReviseSnapshots: (item: RevisionItem, snapshotIds: string[]) => void
}

function dedupeCandidates(candidates: readonly ComparisonCandidateItem[]): ComparisonCandidateItem[] {
  const seen = new Set<string>()
  const unique: ComparisonCandidateItem[] = []
  for (const candidate of candidates) {
    if (seen.has(candidate.snapshot_id)) continue
    seen.add(candidate.snapshot_id)
    unique.push(candidate)
  }
  return unique
}

function candidateCopy(candidate: ComparisonCandidateItem, index: number): { title: string; meta: string } {
  return {
    title: `第 ${index + 1} 项 · ${evidenceKindLabel(candidate.kind)}`,
    meta: evidenceChoiceMeta({
      timeLabel: formatDateTime(candidate.captured_at),
      kindLabel: identityTypeLabel(candidate.subject.kind),
      qualityLabel: QUALITY_BADGE_LABELS[candidate.quality_status] ?? null,
    }),
  }
}

function basketResetKey(
  query: ComparisonURLState | null,
  candidates: ComparisonCandidateItem[] | null,
): string {
  if (query?.mode !== 'candidate') return 'fixed'
  if (!candidates) return 'candidate'
  return `candidate:${dedupeCandidates(candidates).map((candidate) => candidate.snapshot_id).join('\n')}`
}

export function ComparisonSelectionBasket(props: Props) {
  return <ComparisonSelectionBasketBody key={basketResetKey(props.query, props.candidates)} {...props} />
}

function ComparisonSelectionBasketBody({
  query,
  linkProblem,
  candidates,
  selectionError,
  onConfirm,
  onRemove,
  onClear,
  onAdd,
  onReviseSnapshots,
}: Props) {
  const items = query?.mode === 'fixed' ? query.items ?? [] : []
  const identities = useComparisonBasketIdentity(items)
  const candidateMode = query?.mode === 'candidate'
  const deduped = candidateMode && candidates ? dedupeCandidates(candidates) : []
  const merged = candidateMode && candidates ? candidates.length - deduped.length : 0
  const [checked, setChecked] = useState<string[]>([])
  const [limitMessage, setLimitMessage] = useState<string | null>(null)
  const [revisionItem, setRevisionItem] = useState<RevisionItem | null>(null)
  const baseline = query?.baseline ?? 0
  const showTooFew = !candidateMode
    && linkProblem !== 'invalid'
    && linkProblem !== 'unknown_version'
    && items.length < 2
  const count = candidateMode ? checked.length : items.length

  function toggleCandidate(snapshotId: string) {
    setChecked((current) => {
      if (current.includes(snapshotId)) {
        setLimitMessage(null)
        return current.filter((id) => id !== snapshotId)
      }
      if (current.length >= COMPARISON_FIXED_ITEM_LIMIT) {
        setLimitMessage(COMPARISON_SELECTION_LIMIT_ERROR)
        return current
      }
      setLimitMessage(null)
      return [...current, snapshotId]
    })
  }

  const selectedCandidates = checked.flatMap((id) => {
    const candidate = deduped.find((item) => item.snapshot_id === id)
    return candidate ? [candidate] : []
  })

  return (
    <section className="record-section" aria-labelledby="comparison-basket-heading">
      <div className="record-section__head">
        <h2 className="record-section__title" id="comparison-basket-heading">
          比较对象 <span className={count > 0 ? 'record-count record-count--active' : 'record-count'}>{count}</span>
        </h2>
      </div>
      {selectionError ? <p className="record-compare-items__alert" role="alert">{selectionError}</p> : null}
      {limitMessage ? <p className="record-compare-items__alert" role="alert">{limitMessage}</p> : null}
      {showTooFew ? (
        <p className="record-muted" role="status">至少选择 2 项才能比较。当前 {items.length} 项。</p>
      ) : null}
      <div className="record-compare-items__actions">
        {onAdd ? <Button size="sm" variant="secondary" onClick={onAdd}>添加对象</Button> : null}
        {items.length > 0 ? <Button size="sm" variant="ghost" onClick={onClear}>清空比较篮</Button> : null}
      </div>
      {items.length > 0 ? (
        <ol className="record-compare-items">
          {items.map((item, index) => {
            const key = comparisonFixedItemKey(item)
            const identity = identities.get(key)
            const title = identity?.status === 'ready'
              ? identity.title
              : identity?.status === 'unreadable'
                ? '不可读'
                : '正在读取身份'
            const meta = identity?.status === 'ready'
              ? identity.meta
              : identity?.status === 'unreadable'
                ? '无法读取这份比较对象的身份'
                : ''
            const technical = identity?.status === 'loading' || !identity
              ? ('snapshot_id' in item ? [item.snapshot_id] : [item.record_id, item.revision_id])
              : identity.technical
            return (
              <li key={key} className="record-compare-items__item">
                <span className="record-compare-items__index" aria-hidden="true">{index + 1}</span>
                <div className="record-compare-items__copy">
                  <span className="record-compare-items__title">{title}</span>
                  {meta ? <span className="record-compare-items__meta">{meta}</span> : null}
                </div>
                {index === baseline && items.length > 1 ? <span className="record-compare-baseline">基准</span> : null}
                {'record_id' in item ? (
                  <Button size="sm" variant="secondary" onClick={() => setRevisionItem(item)}>选择证据</Button>
                ) : null}
                <Button size="sm" variant="ghost" aria-label={`移出第 ${index + 1} 项`} onClick={() => onRemove(key)}>
                  移出
                </Button>
                <details className="record-disclosure record-compare-items__tech">
                  <summary>技术标识</summary>
                  {technical.map((line) => <code key={line}>{line}</code>)}
                </details>
              </li>
            )
          })}
        </ol>
      ) : null}
      {candidateMode && deduped.length > 0 ? (
        <>
          <p className="record-muted" role="status">
            共 {deduped.length} 个候选，请勾选最多 {COMPARISON_FIXED_ITEM_LIMIT} 个。不会自动比较前 {COMPARISON_FIXED_ITEM_LIMIT} 项。
          </p>
          {merged > 0 ? <p className="record-muted" role="status">已按快照合并重复候选。</p> : null}
          <ul className="record-compare-items">
            {deduped.map((candidate, index) => {
              const copy = candidateCopy(candidate, index)
              const selected = checked.includes(candidate.snapshot_id)
              return (
                <li key={candidate.snapshot_id} className="record-compare-items__item">
                  <label className="record-compare-items__check">
                    <input
                      type="checkbox"
                      aria-label={copy.title}
                      checked={selected}
                      onChange={() => toggleCandidate(candidate.snapshot_id)}
                    />
                    <span className="record-compare-items__copy">
                      <span className="record-compare-items__title">{copy.title}</span>
                      <span className="record-compare-items__meta">{copy.meta}</span>
                    </span>
                  </label>
                  <details className="record-disclosure record-compare-items__tech">
                    <summary>技术标识</summary>
                    <code>{candidate.snapshot_id}</code>
                  </details>
                </li>
              )
            })}
          </ul>
          <Button
            size="lg"
            className="record-compare-items__confirm"
            disabled={selectedCandidates.length === 0}
            onClick={() => onConfirm(selectedCandidates.map((candidate) => ({ snapshot_id: candidate.snapshot_id })))}
          >
            确认候选并比较
          </Button>
        </>
      ) : null}
      {candidateMode && candidates && candidates.length === 0 ? (
        <p className="record-muted" role="status">当前主体窗口没有可比较候选。</p>
      ) : null}
      <ComparisonRevisionEvidenceDialog
        item={revisionItem}
        onClose={() => setRevisionItem(null)}
        onApply={onReviseSnapshots}
      />
    </section>
  )
}
