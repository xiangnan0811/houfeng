import { useEffect, useId, useRef, type RefObject } from 'react'

import type { VPSManagementController } from './hooks/useVPSManagementController'

type Props = {
  lifecycleStatus: string
  controller: VPSManagementController
  returnFocusRef?: RefObject<HTMLButtonElement | null> | undefined
  menuId?: string
}

type ManagementPanel = 'facts' | 'decision' | 'subscription' | 'cancellation' | 'archive'

const ITEMS: Array<{
  panel: ManagementPanel
  label: string
}> = [
  { panel: 'facts', label: '编辑事实' },
  { panel: 'decision', label: '续费决策' },
  { panel: 'subscription', label: '订阅事实' },
  { panel: 'cancellation', label: '取消 / 退役' },
  { panel: 'archive', label: '归档' },
]

function visibleManagementPanels(lifecycleStatus: string): ManagementPanel[] {
  return ITEMS.filter((item) => {
    if (item.panel === 'cancellation') {
      return false
    }
    if (item.panel === 'archive') {
      return lifecycleStatus === 'to_cancel'
    }
    if (lifecycleStatus === 'archived' || lifecycleStatus === 'cancelled') {
      return false
    }
    return true
  }).map((item) => item.panel)
}

function menuItems(root: HTMLElement | null): HTMLButtonElement[] {
  return Array.from(root?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]') ?? [])
}

export function VPSManagementMenu({ lifecycleStatus, controller, returnFocusRef, menuId }: Props) {
  const generatedId = useId()
  const resolvedMenuId = menuId ?? generatedId
  const rootRef = useRef<HTMLDivElement>(null)
  const { closeMenu, menuOpen } = controller
  const items = ITEMS.filter((item) => visibleManagementPanels(lifecycleStatus).includes(item.panel))

  useEffect(() => {
    if (!menuOpen) return
    menuItems(rootRef.current)[0]?.focus()

    const closeAndRestoreFocus = () => {
      closeMenu()
      queueMicrotask(() => returnFocusRef?.current?.focus())
    }
    const onPointer = (event: MouseEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) {
        closeAndRestoreFocus()
      }
    }
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      const buttons = menuItems(rootRef.current)
      if (event.key === 'Tab') {
        closeMenu()
        return
      }
      if (event.key === 'Escape') {
        event.preventDefault()
        closeAndRestoreFocus()
        return
      }
      if (buttons.length === 0) return
      const currentIndex = Math.max(0, buttons.findIndex((button) => button === document.activeElement))
      if (event.key === 'ArrowDown') {
        event.preventDefault()
        buttons[(currentIndex + 1) % buttons.length]?.focus()
        return
      }
      if (event.key === 'ArrowUp') {
        event.preventDefault()
        buttons[(currentIndex - 1 + buttons.length) % buttons.length]?.focus()
        return
      }
      if (event.key === 'Home') {
        event.preventDefault()
        buttons[0]?.focus()
        return
      }
      if (event.key === 'End') {
        event.preventDefault()
        buttons[buttons.length - 1]?.focus()
      }
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
      <ul
        id={resolvedMenuId}
        className="vps-overview-management__menu"
        role="menu"
        aria-label="管理"
        aria-orientation="vertical"
      >
        {items.map((item) => (
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
