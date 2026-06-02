import { useEffect, type ReactNode } from 'react'
import { createPortal } from 'react-dom'
import { useModalFocus } from '../../lib/useModalFocus'

export interface ModalProps {
  open: boolean
  onClose: () => void
  title: string
  children: ReactNode
  footer?: ReactNode
  persistent?: boolean
  size?: 'sm' | 'md' | 'lg' | 'xl'
  contentClassName?: string
  ariaLabel?: string
}

export function Modal({ open, onClose, title, children, footer, persistent, size, contentClassName, ariaLabel }: ModalProps) {
  const modalRef = useModalFocus<HTMLDivElement>(open, onClose)

  useEffect(() => {
    if (open) {
      document.body.style.overflow = 'hidden'
    } else {
      document.body.style.overflow = ''
    }
    return () => {
      document.body.style.overflow = ''
    }
  }, [open])

  if (!open) return null

  const handleBackdropClick = (e: React.MouseEvent) => {
    if (persistent || e.target !== e.currentTarget) return
    onClose()
  }

  return createPortal(
    <div
      className="modal-overlay"
      role="presentation"
      onClick={handleBackdropClick}
      onKeyDown={(e) => {
        if (e.key === 'Escape') onClose()
      }}
    >
      <div
        ref={modalRef}
        className={['modal-content', size && `modal-content--${size}`, contentClassName].filter(Boolean).join(' ')}
        role="dialog"
        aria-modal="true"
        {...(ariaLabel ? { 'aria-label': ariaLabel } : { 'aria-labelledby': 'modal-title' })}
        tabIndex={-1}
      >
        <div className="modal-header">
          <h3 id="modal-title">{title}</h3>
          <button type="button" className="modal-close" onClick={onClose} aria-label="关闭">
            ✕
          </button>
        </div>
        <div className="modal-body">{children}</div>
        {footer && <div className="modal-footer">{footer}</div>}
      </div>
    </div>,
    document.body,
  )
}
