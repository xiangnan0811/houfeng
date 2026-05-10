import { type ReactNode } from 'react'
import { createPortal } from 'react-dom'

import { useModalFocus } from '../../lib/useModalFocus'

export interface DrawerProps {
  open: boolean
  onClose: () => void
  title: string
  side?: 'right' | 'left'
  children: ReactNode
  ariaLabel?: string
}

/**
 * Right-side slide-in drawer for secondary content (e.g. node history timeline).
 * Renders through a portal, closes on overlay click and Esc key, and restores
 * focus to the opener when unmounted.
 */
export function Drawer({
  open,
  onClose,
  title,
  side = 'right',
  children,
  ariaLabel,
}: DrawerProps) {
  const drawerRef = useModalFocus<HTMLElement>(open, onClose)

  // We only mount the drawer body when open. This keeps the page DOM small,
  // avoids accessibility name collisions (drawer EventList vs in-page EventList),
  // and ensures click handlers in the closed drawer never fire.
  if (!open) return null

  return createPortal(
    <>
      <div
        className="drawer-overlay drawer-overlay--open"
        onMouseDown={onClose}
      />
      <aside
        ref={drawerRef}
        className={['drawer', `drawer--${side}`, 'drawer--open'].join(' ')}
        role="dialog"
        aria-modal="true"
        aria-label={ariaLabel ?? title}
        tabIndex={-1}
      >
        <header className="drawer__header">
          <h2 className="drawer__title">{title}</h2>
          <button
            type="button"
            className="drawer__close"
            onClick={onClose}
            aria-label="关闭"
          >
            ×
          </button>
        </header>
        <div className="drawer__body">{children}</div>
      </aside>
    </>,
    document.body,
  )
}
