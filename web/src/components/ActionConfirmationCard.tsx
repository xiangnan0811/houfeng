import { useEffect, useId, useRef } from 'react'

import { Button } from './atoms'

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

/**
 * v2 visual: state-migration card.
 *
 *   ┌───── 操作确认 ribbon ─────┐
 *   │  {title}                  │
 *   │  ┌──────┐ → 操作 ┌──────┐ │
 *   │  │ 当前 │       │ 之后 │ │
 *   │  └──────┘       └──────┘ │
 *   │  ✓ 会发生：{impact}       │
 *   │  ◯ 不变：{unchanged}      │
 *   │                  确认 取消 │
 *   └───────────────────────────┘
 *
 * Props are unchanged from v1 — every existing caller works as-is.
 */
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
      className="page-panel action-confirm"
      role="alertdialog"
      aria-labelledby={titleId}
      aria-describedby={descriptionId}
      tabIndex={-1}
    >
      <p className="page-panel__eyebrow">操作确认</p>
      <h3 id={titleId} className="page-panel__title">
        {title}
      </h3>
      {/* page-stack class preserved for imperative DOM contracts.
       * TargetDetailPage's probe-delete confirmation effect appends a
       * <p data-probe-confirmation-summary> into this body via
       * querySelector('.page-stack'). Removing the class silently breaks
       * that flow. The class also gives the inserted <p> sane spacing. */}
      <div id={descriptionId} className="action-confirm__body page-stack">
        <div className="action-confirm__migration">
          <div className="action-confirm__pane action-confirm__pane--current">
            <span className="action-confirm__pane-label">当前</span>
            <span className="action-confirm__pane-value">{current}</span>
          </div>
          <span className="action-confirm__arrow" aria-hidden>
            →
          </span>
          <div className="action-confirm__pane action-confirm__pane--result">
            <span className="action-confirm__pane-label">之后</span>
            <span className="action-confirm__pane-value">{result}</span>
          </div>
        </div>
        <div className="action-confirm__callouts">
          <p className="action-confirm__callout action-confirm__callout--impact">
            <span className="action-confirm__callout-mark" aria-hidden>
              ✓
            </span>
            {impact}
          </p>
          <p className="action-confirm__callout action-confirm__callout--unchanged">
            <span className="action-confirm__callout-mark" aria-hidden>
              ◯
            </span>
            {unchanged}
          </p>
        </div>
        <div className="action-confirm__actions">
          <Button variant="secondary" disabled={disabled} onClick={onCancel}>
            {cancelLabel ?? '取消'}
          </Button>
          <Button variant="primary" disabled={disabled} onClick={onConfirm}>
            {confirmLabel}
          </Button>
        </div>
      </div>
    </section>
  )
}
