import { useEffect, useRef } from 'react'

import type { VPSManagementController } from './hooks/useVPSManagementController'

type Props = {
  controller: VPSManagementController
  onSelect: (panel: 'facts' | 'decision' | 'subscription' | 'cancellation' | 'archive') => void
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

export function VPSManagementMenu({ controller, onSelect }: Props) {
  const rootRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!controller.menuOpen) return
    const onPointer = (event: MouseEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) {
        controller.closeMenu()
      }
    }
    document.addEventListener('mousedown', onPointer)
    return () => document.removeEventListener('mousedown', onPointer)
  }, [controller])

  if (!controller.menuOpen) return null

  return (
    <div className="vps-overview-management" ref={rootRef}>
      <ul className="vps-overview-management__menu" role="menu" aria-label="管理">
        {ITEMS.map((item) => (
          <li key={item.panel} role="none">
            <button
              type="button"
              role="menuitem"
              className="vps-overview-management__item"
              onClick={() => {
                onSelect(item.panel)
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
