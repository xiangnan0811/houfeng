import { Link } from 'react-router-dom'

import { Badge, Button, Input } from '../../../components/atoms'
import type { ComparisonReason } from '../../../lib/types'
import { COMPARISON_REASON_LABELS } from './comparisonLabels'

type Props = {
  blocked: boolean
  blockers?: readonly ComparisonReason[]
  title: string
  conclusion: string
  saving: boolean
  savedRecordId: string | null
  onTitle: (value: string) => void
  onConclusion: (value: string) => void
  onSave: () => void
}

export function ComparisonSaveRecord({
  blocked,
  blockers = [],
  title,
  conclusion,
  saving,
  savedRecordId,
  onTitle,
  onConclusion,
  onSave,
}: Props) {
  if (blocked) {
    return (
      <section className="record-section" aria-labelledby="comparison-save-heading">
        <div className="record-section__head">
          <h2 className="record-section__title" id="comparison-save-heading">结论与另存</h2>
          <Badge variant="info" tone="notice">不能另存</Badge>
        </div>
        {blockers.length > 0 ? (
          <ul className="record-compare-findings">
            {blockers.map((blocker) => <li key={blocker} className="record-compare-findings__item"><strong>{COMPARISON_REASON_LABELS[blocker] ?? blocker}</strong></li>)}
          </ul>
        ) : (
          <p className="record-muted">缺少有效的比较意图，请重新比较后再保存。</p>
        )}
      </section>
    )
  }
  return (
    <section className="record-section record-compare-save" aria-labelledby="comparison-save-heading">
      <h2 className="record-section__title" id="comparison-save-heading">结论与另存</h2>
      <Input label="记录标题" value={title} onChange={(event) => onTitle(event.target.value)} />
      <label className="input-field">
        <span className="input-field__label">人工结论</span>
        <textarea
          className="input record-compare-save__conclusion"
          rows={5}
          value={conclusion}
          onChange={(event) => onConclusion(event.target.value)}
        />
      </label>
      <div className="record-compare-save__actions">
        {savedRecordId ? (
          <p className="record-compare-save__saved" role="status">
            已保存为 {savedRecordId}
          </p>
        ) : null}
        {savedRecordId ? (
          <Link className="text-link" to={`/records/${encodeURIComponent(savedRecordId)}`}>打开记录</Link>
        ) : null}
        <Button size="lg" disabled={saving} onClick={onSave}>
          {saving ? '正在另存' : '另存为记录'}
        </Button>
      </div>
    </section>
  )
}
