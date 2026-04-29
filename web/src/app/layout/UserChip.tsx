import { useState, useRef, useEffect } from 'react'
import { Link } from 'react-router-dom'
import type { User } from '../../lib/auth-client'

const ROLE_LABEL: Record<string, string> = { admin: '管理员' }

export interface UserChipProps {
  user: User
  onLogout: () => void
  onChangePassword: () => void
}

export function UserChip({ user, onLogout, onChangePassword }: UserChipProps) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const close = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [open])

  const initial = (user.display_name || user.username).slice(0, 1)
  const roleLabel = ROLE_LABEL[user.role] ?? user.role

  return (
    <div className="user-chip" ref={ref}>
      <button
        type="button"
        className="user-chip__trigger"
        onClick={() => setOpen((v) => !v)}
        aria-label={`${user.username} menu`}
      >
        <span className="user-chip__avatar">{initial}</span>
        <span className="user-chip__body">
          <span className="user-chip__name">{user.display_name || user.username}</span>
          <span className="user-chip__role">{roleLabel}</span>
        </span>
        <span className="user-chip__caret">{open ? '▴' : '▾'}</span>
      </button>
      {open && (
        <div className="user-chip__menu" role="menu">
          <Link
            to="/settings"
            className="user-chip__menu-item"
            onClick={() => setOpen(false)}
            role="menuitem"
          >
            主题设置
          </Link>
          <button
            type="button"
            className="user-chip__menu-item"
            onClick={() => {
              setOpen(false)
              onChangePassword()
            }}
            role="menuitem"
          >
            修改密码
          </button>
          <div className="user-chip__divider" />
          <button
            type="button"
            className="user-chip__menu-item user-chip__menu-item--danger"
            onClick={() => {
              setOpen(false)
              onLogout()
            }}
            role="menuitem"
          >
            退出登录
          </button>
        </div>
      )}
    </div>
  )
}
