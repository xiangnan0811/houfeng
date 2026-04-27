export type ActionConfirmationCardProps = {
  title: string
  current: string
  result: string
  impact: string
  unchanged: string
  confirmLabel: string
  cancelLabel?: string
  disabled?: boolean
  onConfirm: () => void
  onCancel: () => void
}

export function ActionConfirmationCard({
  title,
  current,
  result,
  impact,
  unchanged,
  confirmLabel,
  cancelLabel,
  disabled = false,
  onConfirm,
  onCancel,
}: ActionConfirmationCardProps) {
  return (
    <section className="page-panel" aria-label={title}>
      <p className="page-panel__eyebrow">Confirmation</p>
      <h3 className="page-panel__title">{title}</h3>
      <div className="page-stack">
        <p>{current}</p>
        <p>{result}</p>
        <p>{impact}</p>
        <p>{unchanged}</p>
        <div className="badge-row badge-row--wrap">
          <button type="button" disabled={disabled} onClick={onConfirm}>
            {confirmLabel}
          </button>
          <button type="button" disabled={disabled} onClick={onCancel}>
            {cancelLabel ?? '取消'}
          </button>
        </div>
      </div>
    </section>
  )
}
