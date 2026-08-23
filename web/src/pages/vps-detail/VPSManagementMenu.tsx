import { useEffect, useRef, type RefObject } from 'react'

import type { VPSManagementController } from './hooks/useVPSManagementController'

type Props = {
  controller: VPSManagementController
  returnFocusRef?: RefObject<HTMLButtonElement | null> | undefined
}

const ITEMS: Array<{
  panel: 'facts' | 'decision' | 'subscription' | 'cancellation' | 'archive'
  label: string
}> = [
  { panel: 'facts', label: '编辑事实' },
  { panel: 'decision', label: '续费决策' },
  { panel: 'subscription', label: '订阅事实' },
  { panel: 'cancellation', label: '取消 / 退役' },
  { panel: 'archive', label: '归档' },
]

export function VPSManagementMenu({ controller, returnFocusRef }: Props) {
  const rootRef = useRef<HTMLDivElement>(null)
  const { closeMenu, menuOpen } = controller

  useEffect(() => {
    if (!menuOpen) return
    rootRef.current?.querySelector<HTMLButtonElement>('[role="menuitem"]')?.focus()

    const closeAndRestoreFocus = () => {
      closeMenu()
      queueMicrotask(() => returnFocusRef?.current?.focus())
    }
    const onPointer = (event: MouseEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) {
        closeAndRestoreFocus()
      }
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      event.preventDefault()
      closeAndRestoreFocus()
    }
    document.addEventListener('mousedown', onPointer)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('mousedown', onPointer)
      document.removeEventListener('keydown', onKeyDown)
    }
  }, [closeMenu, menuOpen, returnFocusRef])

  if (!menuOpen) return null

  return (
    <div className="vps-overview-management" ref={rootRef}>
      <ul className="vps-overview-management__menu" role="menu" aria-label="管理">
        {ITEMS.map((item) => (
          <li key={item.panel} role="none">
            <button
              type="button"
              role="menuitem"
              className="btn lg ghost vps-overview-management__item"
              onClick={() => {
                controller.openPanel(item.panel)
              }}
            >
              {item.label}
            </button>
          </li>
        ))}
      </ul>
    </div>
  )
}
