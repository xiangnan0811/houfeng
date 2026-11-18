import { Link } from 'react-router-dom'

import { Badge, Button, Input } from '../../../components/atoms'
import type { ComparisonReason } from '../../../lib/types'
import { presentComparisonReason } from './comparisonLabels'

type Props = {
  blocked: boolean
  blockers?: readonly ComparisonReason[]
  title: string
  conclusion: string
  saving: boolean
  savedRecordId: string | null
  /** A sent save has no confirmed result: inputs keep what was sent and the button replays it. */
  unresolved?: boolean
  feedback?: string | null
  onTitle: (value: string) => void
  onConclusion: (value: string) => void
  onSave: () => void
  /** 放弃确认未决的另存，只清本地重试状态。 */
  onDiscardUnresolved?: () => void
}

export function ComparisonSaveRecord({
  blocked,
  blockers = [],
  title,
  conclusion,
  saving,
  savedRecordId,
  unresolved = false,
  feedback = null,
  onTitle,
  onConclusion,
  onSave,
  onDiscardUnresolved,
}: Props) {
  if (blocked) {
    return (
      <section className="record-section" aria-labelledby="comparison-save-heading">
        <div className="record-section__head">
          <h2 className="record-section__title" id="comparison-save-heading">结论与另存</h2>
          <Badge variant="info" tone="notice">不能另存</Badge>
        </div>
        {feedback ? <p className="record-upload__notice" role="alert">{feedback}</p> : null}
        {blockers.length > 0 ? (
          <ul className="record-compare-findings">
            {blockers.map((blocker) => {
              const reason = presentComparisonReason(blocker)
              return (
                <li key={blocker} className="record-compare-findings__item">
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
        ) : (
          <p className="record-muted">缺少有效的比较意图，请重新比较后再保存。</p>
        )}
      </section>
    )
  }
  return (
    <section className="record-section record-compare-save" aria-labelledby="comparison-save-heading">
      <h2 className="record-section__title" id="comparison-save-heading">结论与另存</h2>
      {feedback ? <p className="record-upload__notice" role="alert">{feedback}</p> : null}
      <Input
        label="记录标题"
        value={title}
        disabled={unresolved}
        onChange={(event) => onTitle(event.target.value)}
      />
      <label className="input-field">
        <span className="input-field__label">人工结论</span>
        <textarea
          className="input record-compare-save__conclusion"
          rows={5}
          value={conclusion}
          disabled={unresolved}
          onChange={(event) => onConclusion(event.target.value)}
        />
      </label>
      {unresolved && !savedRecordId ? (
        <p className="record-muted" role="status">
          上次另存的结果尚未确认。重试会原样重发那次请求，不会另建记录；确认前不能修改标题和结论。
        </p>
      ) : null}
      <div className="record-compare-save__actions">
        {savedRecordId ? (
          <p className="record-compare-save__saved" role="status">
            比较记录已保存
          </p>
        ) : null}
        {savedRecordId ? (
          <Link className="text-link" to={`/records/${encodeURIComponent(savedRecordId)}`}>查看比较记录</Link>
        ) : null}
        {unresolved && !savedRecordId && onDiscardUnresolved ? (
          <Button
            size="lg"
            variant="secondary"
            disabled={saving}
            title="只清除本地的待确认状态，不会撤销已发出的请求"
            onClick={onDiscardUnresolved}
          >
            放弃确认
          </Button>
        ) : null}
        <Button size="lg" disabled={saving} onClick={onSave}>
          {saving ? '正在另存' : unresolved && !savedRecordId ? '重试确认另存' : '另存为记录'}
        </Button>
      </div>
    </section>
  )
}
