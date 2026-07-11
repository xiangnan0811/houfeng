import { type KeyboardEvent, useEffect, useId, useRef, useState } from 'react'
import { Link, useLocation } from 'react-router-dom'
import { GlobalSearch } from './GlobalSearch'
import { useThemeOptional } from '../../lib/theme-context'
import { SyncStatus, type SyncStatusProps } from './SyncStatus'
import type { User } from '../../lib/auth-client'

const PAGE_TITLES: Record<string, string> = {
  '/': '工作台',
  '/monitoring': '监控',
  '/targets': '入口探测',
  '/events': '事件流',
  '/vps': 'VPS 资产',
  '/providers': '服务商',
  '/subscriptions': '订阅',
  '/asset-decisions': '资产决策',
  '/settings': '设置',
}

interface TopBarProps {
  sync: SyncStatusProps
  user: User | null
}

export function TopBar({ sync, user }: TopBarProps) {
  const location = useLocation()
  const pageTitle = derivePageTitle(location.pathname)
  return (
    <header className="topbar">
      <span className="tp-page">{pageTitle}</span>
      <div className="tp-spacer" />
      <GlobalSearch />
      <div className="tp-divider" />
      <ThemeSwitcher />
      <NotificationBell />
      <SyncStatus {...sync} />
      {user && <UserAvatar user={user} />}
    </header>
  )
}

/* --- Theme Switcher --- */

const THEME_OPTIONS = [
  { preset: 'houfeng' as const, mode: 'dark' as const, icon: '☾', label: '氛围暗色' },
  { preset: 'classic' as const, mode: 'dark' as const, icon: '◐', label: '克制工程' },
  { preset: 'houfeng' as const, mode: 'light' as const, icon: '☀', label: '精致亮色' },
  { preset: 'houfeng' as const, mode: 'system' as const, icon: '⚙', label: '跟随系统' },
] as const

function ThemeSwitcher() {
  const theme = useThemeOptional()
  const [open, setOpen] = useState(false)
  const menuId = `${useId()}-menu`
  const ref = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const itemRefs = useRef<Array<HTMLButtonElement | null>>([])
  const pendingFocusIndex = useRef(0)

  useEffect(() => {
    if (!open) return
    itemRefs.current[pendingFocusIndex.current]?.focus()
  }, [open])

  useEffect(() => {
    if (!open) return
    const close = (e: MouseEvent) => {
      if (!(e.target instanceof Node) || !ref.current?.contains(e.target)) setOpen(false)
    }
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [open])

  if (!theme) return null

  const { preset, mode, setPreset, setMode } = theme
  const current = THEME_OPTIONS.find(
    (o) => o.preset === preset && o.mode === mode
  ) ?? THEME_OPTIONS[0]

  function openMenu(index: number) {
    pendingFocusIndex.current = index
    setOpen(true)
  }

  function closeAndRestoreFocus() {
    setOpen(false)
    triggerRef.current?.focus()
  }

  function selectTheme(option: (typeof THEME_OPTIONS)[number]) {
    setPreset(option.preset)
    setMode(option.mode)
    closeAndRestoreFocus()
  }

  function handleTriggerKeyDown(event: KeyboardEvent<HTMLButtonElement>) {
    if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return
    event.preventDefault()
    openMenu(event.key === 'ArrowDown' ? 0 : THEME_OPTIONS.length - 1)
  }

  function handleItemKeyDown(
    event: KeyboardEvent<HTMLButtonElement>,
    index: number,
    option: (typeof THEME_OPTIONS)[number],
  ) {
    let nextIndex: number
    switch (event.key) {
      case 'ArrowDown':
        nextIndex = (index + 1) % THEME_OPTIONS.length
        break
      case 'ArrowUp':
        nextIndex = (index - 1 + THEME_OPTIONS.length) % THEME_OPTIONS.length
        break
      case 'Home':
        nextIndex = 0
        break
      case 'End':
        nextIndex = THEME_OPTIONS.length - 1
        break
      case 'Enter':
      case ' ':
        event.preventDefault()
        selectTheme(option)
        return
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

  return (
    <div className="tp-theme" ref={ref}>
      <button
        ref={triggerRef}
        type="button"
        className="tp-icon-btn"
        aria-label="切换主题"
        aria-haspopup="menu"
        aria-controls={menuId}
        aria-expanded={open}
        onClick={() => {
          if (open) setOpen(false)
          else openMenu(0)
        }}
        onKeyDown={handleTriggerKeyDown}
      >
        {current.icon}
      </button>
      {open && (
        <div id={menuId} className="theme-menu open" role="menu" aria-label="主题选项">
          {THEME_OPTIONS.map((o, index) => (
            <button
              key={`${o.preset}-${o.mode}`}
              ref={(node) => {
                itemRefs.current[index] = node
              }}
              type="button"
              role="menuitemradio"
              aria-label={o.label}
              aria-checked={o === current}
              className={`tm-item${o === current ? ' active' : ''}`}
              onClick={() => selectTheme(o)}
              onKeyDown={(event) => handleItemKeyDown(event, index, o)}
            >
              <span className="tm-icon">{o.icon}</span>
              {o.label}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

function NotificationBell() {
  return (
    <Link
      className="tp-icon-btn"
      to="/events?notification_only=1"
      aria-label="查看通知事件"
      title="通知事件"
    >
      <svg aria-hidden="true" width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4">
        <path d="M4 6a4 4 0 018 0c0 4 2 5 2 5H2s2-1 2-5" />
        <path d="M6.5 13a1.5 1.5 0 003 0" />
      </svg>
    </Link>
  )
}

function UserAvatar({ user }: { user: User }) {
  const display = user.display_name || user.username || ''
  const initial = display.slice(0, 1).toUpperCase() || '·'
  return (
    <div className="tp-avatar" title={user.username}>
      {initial}
    </div>
  )
}

function derivePageTitle(pathname: string): string {
  if (PAGE_TITLES[pathname]) return PAGE_TITLES[pathname]
  if (pathname.startsWith('/monitoring/compare')) return '监控实例对比'
  if (pathname.startsWith('/monitoring/')) return '监控实例详情'
  if (pathname.startsWith('/targets/')) return '目标详情'
  if (pathname.startsWith('/vps/')) return 'VPS 详情'
  return '候风'
}
