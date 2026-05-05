import { useEffect, type ReactNode } from 'react'

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
 * Closes on overlay click and Esc key. Renders inline with position: fixed.
 *
 * Width is controlled via CSS (`min(440px, 40vw)` by default; override via
 * .drawer custom class if needed).
 */
export function Drawer({
  open,
  onClose,
  title,
  side = 'right',
  children,
  ariaLabel,
}: DrawerProps) {
  useEffect(() => {
    if (!open) return
    function onKey(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        onClose()
      }
    }
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('keydown', onKey)
    }
  }, [open, onClose])

  // We only mount the drawer body when open. This keeps the page DOM small,
  // avoids accessibility name collisions (drawer EventList vs in-page EventList),
  // and ensures click handlers in the closed drawer never fire.
  if (!open) return null

  return (
    <>
      <div
        className="drawer-overlay drawer-overlay--open"
        onMouseDown={onClose}
      />
      <aside
        className={['drawer', `drawer--${side}`, 'drawer--open'].join(' ')}
        role="dialog"
        aria-modal="true"
        aria-label={ariaLabel ?? title}
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
    </>
  )
}
