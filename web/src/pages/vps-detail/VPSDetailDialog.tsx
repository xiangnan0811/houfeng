import type { ReactNode } from 'react'

import { Button } from '../../components/atoms'
import { Modal, type ModalProps } from '../../components/atoms/Modal'

type Template = 'decision' | 'form' | 'objects'

type Props = ModalProps & { template?: Template }

const sizes = { decision: 'sm', form: 'xl', objects: 'lg' } as const

export function VPSDetailDialog({ template, contentClassName, ...props }: Props) {
  return (
    <Modal
      {...props}
      {...(template ? { size: props.size ?? sizes[template] } : {})}
      contentClassName={[
        template && 'vps-dialog',
        template && `vps-dialog--${template}`,
        contentClassName,
      ].filter(Boolean).join(' ')}
    />
  )
}

export function VPSDialogActions({
  formId,
  onCancel,
  submitting,
  disabled = false,
  submitLabel,
  cancelLabel = '取消',
  error,
  notice,
}: {
  formId: string
  onCancel: () => void
  submitting: boolean
  disabled?: boolean
  submitLabel: string
  cancelLabel?: string
  error?: string | null
  notice?: string | null
}) {
  const actions = (
    <>
      <Button type="button" variant="secondary" disabled={submitting} onClick={onCancel}>
        {cancelLabel}
      </Button>
      <Button type="submit" form={formId} disabled={submitting || disabled}>
        {submitting ? '保存中…' : submitLabel}
      </Button>
    </>
  )
  if (!error && !notice) return actions
  return (
    <div className="vps-dialog-feedback">
      {error ? <p className="vps-form-error" role="alert">{error}</p> : null}
      {notice ? <p className="vps-form-notice" role="status">{notice}</p> : null}
      <div className="modal__actions">{actions}</div>
    </div>
  )
}

export function VPSFormSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="vps-form-section">
      <h4>{title}</h4>
      {children}
    </section>
  )
}

export function VPSObject({
  name,
  id,
  status,
  actions,
  children,
}: {
  name: ReactNode
  id: string
  status?: ReactNode
  actions?: ReactNode
  children: ReactNode
}) {
  return (
    <li className="vps-object">
      <div className="vps-object__head">
        <div className="vps-object__identity">
          <h4>{name}</h4>
          <span className="mono">{id}</span>
        </div>
        <div className="vps-object__aside">
          {status}
          {actions}
        </div>
      </div>
      {children}
    </li>
  )
}
