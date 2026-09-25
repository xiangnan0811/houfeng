import { useEffect, useId, useRef, type RefObject } from 'react'

import type { VPSManagementController, VPSManagementPanel } from './hooks/useVPSManagementController'

type Props = {
  lifecycleStatus: string
  renewalDecision?: string
  controller: VPSManagementController
  returnFocusRef?: RefObject<HTMLButtonElement | null> | undefined
  menuId?: string
}

type MenuGroup = 'business' | 'runtime' | 'relations' | 'lifecycle'
type MenuPanel = Exclude<VPSManagementPanel, null | 'menu' | 'monitoring-instance-create'>

const GROUP_ORDER: MenuGroup[] = ['business', 'runtime', 'relations', 'lifecycle']
const GROUP_LABELS: Record<MenuGroup, string> = {
  business: '业务 / 账单',
  runtime: '运行',
  relations: '关联',
  lifecycle: '生命周期',
}

const ITEMS: Array<{
  panel: MenuPanel
  label: string
  group: MenuGroup
}> = [
  { panel: 'facts', label: '编辑事实', group: 'business' },
  { panel: 'decision', label: '续费决策', group: 'business' },
  { panel: 'subscription', label: '订阅事实', group: 'business' },
  { panel: 'validity-extension', label: '延长有效期', group: 'business' },
  { panel: 'monitoring-instance-evidence', label: '监控实例', group: 'runtime' },
  { panel: 'monitoring-instance-link', label: '关联监控', group: 'runtime' },
  { panel: 'services-detail', label: '服务', group: 'relations' },
  { panel: 'service', label: '新增服务', group: 'relations' },
  { panel: 'domains-detail', label: '域名', group: 'relations' },
  { panel: 'domain', label: '新增域名', group: 'relations' },
  { panel: 'cancellation', label: '取消 / 退役', group: 'lifecycle' },
  { panel: 'archive', label: '归档', group: 'lifecycle' },
  { panel: 'start-migration', label: '开始迁移', group: 'lifecycle' },
]

const WRITEABLE_LIFECYCLES = new Set(['active', 'idle', 'testing'])
const CANCELLATION_RENEWALS = new Set(['cancel', 'auto_renew_cancelled', 'migrate'])

function visibleManagementPanels(lifecycleStatus: string, renewalDecision = ''): MenuPanel[] {
  return ITEMS.filter((item) => {
    if (lifecycleStatus === 'archived' || lifecycleStatus === 'cancelled') {
      return false
    }
    if (item.panel === 'cancellation') {
      if (lifecycleStatus === 'to_cancel' || lifecycleStatus === 'to_migrate') return true
      return WRITEABLE_LIFECYCLES.has(lifecycleStatus) && CANCELLATION_RENEWALS.has(renewalDecision)
    }
    if (item.panel === 'archive') {
      return lifecycleStatus === 'to_cancel'
    }
    if (item.panel === 'start-migration') {
      return lifecycleStatus === 'active' || lifecycleStatus === 'idle' || lifecycleStatus === 'testing'
    }
    return true
  }).map((item) => item.panel)
}

function menuItems(root: HTMLElement | null): HTMLButtonElement[] {
  return Array.from(root?.querySelectorAll<HTMLButtonElement>('[role="menuitem"]') ?? [])
}

export function VPSManagementMenu({ lifecycleStatus, renewalDecision, controller, returnFocusRef, menuId }: Props) {
  const generatedId = useId()
  const resolvedMenuId = menuId ?? generatedId
  const rootRef = useRef<HTMLDivElement>(null)
  const { closeMenu, menuOpen } = controller
  const items = ITEMS.filter((item) => visibleManagementPanels(lifecycleStatus, renewalDecision).includes(item.panel))

  useEffect(() => {
    if (!menuOpen) return
    menuItems(rootRef.current)[0]?.focus()

    const closeAndRestoreFocus = () => {
      closeMenu()
      queueMicrotask(() => returnFocusRef?.current?.focus())
    }
    const onPointer = (event: MouseEvent) => {
      if (!rootRef.current?.contains(event.target as Node) && !returnFocusRef?.current?.contains(event.target as Node)) {
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
        {GROUP_ORDER.map((group) => {
          const groupItems = items.filter((item) => item.group === group)
          if (groupItems.length === 0) return null
          return (
            <li key={group} role="none" className="vps-overview-management__group">
              <p className="vps-overview-management__group-label">{GROUP_LABELS[group]}</p>
              {groupItems.map((item) => (
                <button
                  key={item.panel}
                  type="button"
                  role="menuitem"
                  className="btn lg ghost vps-overview-management__item"
                  onClick={() => {
                    controller.openPanel(item.panel)
                  }}
                >
                  {item.label}
                </button>
              ))}
            </li>
          )
        })}
      </ul>
    </div>
  )
}
