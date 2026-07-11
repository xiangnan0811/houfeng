import { type KeyboardEvent, useEffect, useId, useRef, useState } from 'react'
import type { User } from '../../lib/auth-client'

const ROLE_LABEL: Record<string, string> = { admin: '管理员' }
const MENU_ITEM_COUNT = 2

export interface UserChipProps {
  user: User
  onLogout: () => void
  onChangePassword: () => void
}

export function UserChip({ user, onLogout, onChangePassword }: UserChipProps) {
  const [open, setOpen] = useState(false)
  const baseId = useId()
  const menuId = `${baseId}-menu`
  const triggerId = `${baseId}-trigger`
  const containerRef = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const itemRefs = useRef<Array<HTMLButtonElement | null>>([])
  const pendingFocusIndex = useRef(0)

  useEffect(() => {
    if (!open) return
    itemRefs.current[pendingFocusIndex.current]?.focus()
  }, [open])

  useEffect(() => {
    if (!open) return
    const close = (event: MouseEvent) => {
      if (!(event.target instanceof Node) || !containerRef.current?.contains(event.target)) setOpen(false)
    }
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [open])

  const display = user.display_name || user.username || ''
  const initial = display.slice(0, 1).toUpperCase() || '·'
  const roleLabel = ROLE_LABEL[user.role] ?? user.role ?? ''

  function openMenu(index: number) {
    pendingFocusIndex.current = index
    setOpen(true)
  }

  function closeAndRestoreFocus() {
    setOpen(false)
    triggerRef.current?.focus()
  }

  function handleTriggerKeyDown(event: KeyboardEvent<HTMLButtonElement>) {
    if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return
    event.preventDefault()
    openMenu(event.key === 'ArrowDown' ? 0 : MENU_ITEM_COUNT - 1)
  }

  function handleItemKeyDown(event: KeyboardEvent<HTMLButtonElement>, index: number) {
    let nextIndex: number
    switch (event.key) {
      case 'ArrowDown':
        nextIndex = (index + 1) % MENU_ITEM_COUNT
        break
      case 'ArrowUp':
        nextIndex = (index - 1 + MENU_ITEM_COUNT) % MENU_ITEM_COUNT
        break
      case 'Home':
        nextIndex = 0
        break
      case 'End':
        nextIndex = MENU_ITEM_COUNT - 1
        break
      case 'Escape':
        event.preventDefault()
        closeAndRestoreFocus()
        return
      case 'Tab':
        setOpen(false)
        return
      default:
        return
    }

    event.preventDefault()
    itemRefs.current[nextIndex]?.focus()
  }

  function runCommand(command: () => void) {
    setOpen(false)
    command()
  }

  return (
    <div className="user-chip" ref={containerRef}>
      <button
        ref={triggerRef}
        id={triggerId}
        type="button"
        className="user-chip__trigger"
        aria-label={`${user.username} 用户菜单`}
        aria-haspopup="menu"
        aria-controls={menuId}
        aria-expanded={open}
        onClick={() => {
          if (open) setOpen(false)
          else openMenu(0)
        }}
        onKeyDown={handleTriggerKeyDown}
      >
        <span className="user-avatar">{initial}</span>
        <span className="user-info">
          <span className="user-name">{user.display_name || user.username}</span>
          <span className="user-role">{roleLabel}</span>
        </span>
        <span className="user-chip__caret" aria-hidden="true">{open ? '▴' : '▾'}</span>
      </button>
      {open && (
        <div id={menuId} className="user-chip__menu" role="menu" aria-labelledby={triggerId}>
          <button
            ref={(node) => {
              itemRefs.current[0] = node
            }}
            type="button"
            className="user-chip__menu-item"
            role="menuitem"
            onClick={() => runCommand(onChangePassword)}
            onKeyDown={(event) => handleItemKeyDown(event, 0)}
          >
            修改密码
          </button>
          <div className="user-chip__divider" role="separator" />
          <button
            ref={(node) => {
              itemRefs.current[1] = node
            }}
            type="button"
            className="user-chip__menu-item user-chip__menu-item--danger"
            role="menuitem"
            onClick={() => runCommand(onLogout)}
            onKeyDown={(event) => handleItemKeyDown(event, 1)}
          >
            退出登录
          </button>
        </div>
      )}
    </div>
  )
}
