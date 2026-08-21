import { Button, Input } from '../../../components/atoms'

type Props = {
  blocked: boolean
  blockers?: string[]
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
      <section aria-labelledby="comparison-save-heading">
        <div className="section-heading">
          <h2 className="section-heading__title" id="comparison-save-heading">人工结论与另存</h2>
          <p className="section-heading__description">当前选择不能另存为记录。</p>
        </div>
        {blockers.length > 0 ? (
          <ul>
            {blockers.map((blocker) => <li key={blocker}>{blocker}</li>)}
          </ul>
        ) : (
          <p>缺少有效的比较意图，请重新比较后再保存。</p>
        )}
      </section>
    )
  }
  return (
    <section aria-labelledby="comparison-save-heading">
      <div className="section-heading">
        <h2 className="section-heading__title" id="comparison-save-heading">人工结论与另存</h2>
        <p className="section-heading__description">结论只写入新记录修订，不进入系统比较结果。</p>
      </div>
      <Input label="记录标题" value={title} onChange={(event) => onTitle(event.target.value)} />
      <label className="input-field">
        <span className="input-field__label">人工结论</span>
        <textarea
          className="input"
          rows={6}
          value={conclusion}
          onChange={(event) => onConclusion(event.target.value)}
        />
      </label>
      <div className="page-state__actions">
        <Button size="lg" disabled={saving} onClick={onSave}>
          {saving ? '正在另存' : '另存为记录'}
        </Button>
      </div>
      {savedRecordId ? <p role="status">已保存为 {savedRecordId}</p> : null}
    </section>
  )
}
