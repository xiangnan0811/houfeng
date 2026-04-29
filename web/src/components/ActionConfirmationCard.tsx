import { useEffect, useId, useRef } from 'react'

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
  const titleId = useId()
  const descriptionId = useId()
  const containerRef = useRef<HTMLElement | null>(null)

  useEffect(() => {
    containerRef.current?.focus()
  }, [])

  return (
    <section
      ref={containerRef}
      className="page-panel"
      role="alertdialog"
      aria-labelledby={titleId}
      aria-describedby={descriptionId}
      tabIndex={-1}
    >
      <p className="page-panel__eyebrow">操作确认</p>
      <h3 id={titleId} className="page-panel__title">
        {title}
      </h3>
      <div id={descriptionId} className="page-stack">
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
