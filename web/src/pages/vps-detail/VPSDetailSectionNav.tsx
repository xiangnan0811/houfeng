import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Link, useLocation } from 'react-router-dom'

const SECTIONS = [
  { id: 'vps-section-identity', label: '资产信息' },
  { id: 'vps-section-ops', label: '订阅与续费' },
  { id: 'vps-section-monitoring', label: '运行观测' },
  { id: 'vps-section-relations', label: '服务与域名' },
  { id: 'vps-section-activity', label: '最近活动' },
] as const

export function VPSDetailSectionNav() {
  const location = useLocation()
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)

  useLayoutEffect(() => {
    const section = SECTIONS.find((item) => `#${item.id}` === location.hash)
    if (section) document.getElementById(section.id)?.scrollIntoView?.({ block: 'start', inline: 'nearest' })
  }, [location.hash, location.key])

  useEffect(() => {
    if (!open) return

    const closeAndRestoreFocus = () => {
      setOpen(false)
      queueMicrotask(() => triggerRef.current?.focus())
    }
    const onPointer = (event: MouseEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) closeAndRestoreFocus()
    }
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
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
  }, [open])

  return (
    <nav ref={rootRef} className="vps-detail-workspace__toc" aria-label="当前页分区">
      <div className={open ? 'vps-detail-workspace__jump vps-detail-workspace__jump--open' : 'vps-detail-workspace__jump'}>
        <button
          ref={triggerRef}
          type="button"
          className="vps-detail-workspace__jump-trigger"
          aria-expanded={open}
          aria-controls="vps-detail-section-directory"
          onClick={() => setOpen((value) => !value)}
        >
          页面目录
        </button>
        {open ? (
          <div id="vps-detail-section-directory" className="vps-detail-workspace__jump-panel">
            {SECTIONS.map((section) => (
              <Link
                key={section.id}
                to={{ pathname: location.pathname, search: location.search, hash: `#${section.id}` }}
                state={location.state}
                onClick={() => setOpen(false)}
              >
                {section.label}
              </Link>
            ))}
          </div>
        ) : null}
      </div>
    </nav>
  )
}
