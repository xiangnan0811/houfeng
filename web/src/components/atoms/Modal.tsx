import {
  createContext,
  useContext,
  useId,
  useSyncExternalStore,
  type ReactNode,
} from 'react'
import { createPortal } from 'react-dom'
import {
  getModalDepth,
  isTopModal,
  subscribeModalStack,
} from '../../lib/modalStack'
import { useModalFocus } from '../../lib/useModalFocus'

const ModalParentContext = createContext<string | null>(null)

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
  dialogRole?: 'dialog' | 'alertdialog'
}

export function Modal({
  open,
  onClose,
  title,
  children,
  footer,
  persistent,
  size,
  contentClassName,
  ariaLabel,
  dialogRole = 'dialog',
}: ModalProps) {
  const parentModalId = useContext(ModalParentContext)
  const modalId = useId()
  const titleId = useId()
  const modalRef = useModalFocus<HTMLDivElement>(
    open,
    onClose,
    modalId,
    !persistent,
    parentModalId,
  )
  const isTop = useSyncExternalStore(
    subscribeModalStack,
    () => !open || getModalDepth(modalId) === 0 || isTopModal(modalId),
    () => !open,
  )

  if (!open) return null

  const handleBackdropClick = (e: React.MouseEvent) => {
    if (persistent || !isTopModal(modalId) || e.target !== e.currentTarget) return
    onClose()
  }

  return createPortal(
    <>
      {/* a11y-allow-nonsemantic-click: modal-backdrop */}
      <div
        className={['modal-overlay', isTop && 'modal-stack-layer--top']
          .filter(Boolean)
          .join(' ')}
        role="presentation"
        onClick={handleBackdropClick}
      >
        <div
          ref={modalRef}
          data-modal-stack-id={modalId}
          data-modal-stack-parent-id={parentModalId ?? undefined}
          className={['modal-content', size && `modal-content--${size}`, contentClassName].filter(Boolean).join(' ')}
          role={dialogRole}
          aria-modal={isTop ? 'true' : undefined}
          aria-hidden={isTop ? undefined : 'true'}
          inert={isTop ? undefined : true}
          {...(ariaLabel ? { 'aria-label': ariaLabel } : { 'aria-labelledby': titleId })}
          tabIndex={-1}
        >
          <div className="modal-header">
            <h3 id={titleId}>{title}</h3>
            <button type="button" className="modal-close" onClick={onClose} aria-label="关闭">
              ✕
            </button>
          </div>
          <ModalParentContext.Provider value={modalId}>
            <div className="modal-body">{children}</div>
            {footer && <div className="modal-footer">{footer}</div>}
          </ModalParentContext.Provider>
        </div>
      </div>
    </>,
    document.body,
  )
}
