import type { ReactNode } from 'react'

import { Button, Modal } from './atoms'

export type ActionConfirmationModalProps = {
  open: boolean
  title: string
  current: string
  result: string
  impact: string
  unchanged: string
  confirmLabel: string
  cancelLabel?: string
  error?: string | null
  disabled?: boolean
  cancelDisabled?: boolean
  onConfirm: () => void
  onCancel: () => void
  children?: ReactNode
}

export function ActionConfirmationModal({
  open,
  title,
  current,
  result,
  impact,
  unchanged,
  confirmLabel,
  cancelLabel,
  error = null,
  disabled = false,
  cancelDisabled,
  onConfirm,
  onCancel,
  children,
}: ActionConfirmationModalProps) {
  const resolvedCancelDisabled = cancelDisabled ?? disabled

  return (
    <Modal open={open} onClose={onCancel} title={title} dialogRole="alertdialog" size="md">
      <div className="action-confirm__body page-stack">
        <p className="page-panel__eyebrow">操作确认</p>
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
        {children}
        {error ? (
          <p className="watchtower-runtime-error" role="alert">
            {error}
          </p>
        ) : null}
        <div className="action-confirm__actions">
          <Button variant="secondary" disabled={resolvedCancelDisabled} onClick={onCancel}>
            {cancelLabel ?? '取消'}
          </Button>
          <Button variant="primary" disabled={disabled} onClick={onConfirm}>
            {confirmLabel}
          </Button>
        </div>
      </div>
    </Modal>
  )
}
