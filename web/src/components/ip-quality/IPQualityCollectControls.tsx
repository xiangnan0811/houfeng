import { useEffect, useId, useRef, useState, type KeyboardEvent } from 'react'
import { Link } from 'react-router-dom'

import { Button } from '../atoms'
import { READ_ONLY_PREVIEW } from '../../lib/readOnlyPreview'
import type { IPQualityCollectIPVersions } from '../../lib/types'
import { COLLECT_SCOPE_OPTIONS, collectUnavailableLabel, type CollectNotice } from './ipQualityCollectPresentation'
import type { IPQualityCollectController } from './useIPQualityCollect'

type CollectButtonProps = {
  collect: IPQualityCollectController
  variant?: 'primary' | 'secondary'
}

const OPTION_COUNT = COLLECT_SCOPE_OPTIONS.length

// 立即采集：点击后在按钮正下方展开 IPv4 / IPv6 / 双栈三个动作，与当前查看的地址族无关。
export function IPQualityCollectButton({ collect, variant = 'primary' }: CollectButtonProps) {
  const baseId = useId()
  const menuId = `${baseId}-menu`
  const triggerId = `${baseId}-trigger`
  const [open, setOpen] = useState(false)
  const containerRef = useRef<HTMLSpanElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const itemRefs = useRef<Array<HTMLButtonElement | null>>([])
  const pendingFocusIndex = useRef(0)
  const unavailable = collect.status != null && !collect.status.available
  const busy = collect.submitting || collect.active
  // 首次状态读取中采集条件未知：禁用且不展开菜单，避免在不可用/已在途时提交。
  const disabled = busy || unavailable || collect.checking
  // 进行中或不可用时菜单关闭且不再自动重现，避免重复提交（渲染期重置，不在 effect 里同步 setState）。
  if (open && disabled) setOpen(false)
  const menuOpen = open && !disabled && !READ_ONLY_PREVIEW

  useEffect(() => {
    if (!menuOpen) return
    itemRefs.current[pendingFocusIndex.current]?.focus()
  }, [menuOpen])

  useEffect(() => {
    if (!menuOpen) return
    const close = (event: MouseEvent) => {
      if (!(event.target instanceof Node) || !containerRef.current?.contains(event.target)) setOpen(false)
    }
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [menuOpen])

  // 只读预览不提供写操作入口。
  if (READ_ONLY_PREVIEW) return null

  function openMenu(index: number) {
    pendingFocusIndex.current = index
    setOpen(true)
  }

  function closeAndRestoreFocus() {
    setOpen(false)
    triggerRef.current?.focus()
  }

  function handleTriggerKeyDown(event: KeyboardEvent<HTMLButtonElement>) {
    if (event.key === 'Escape' && menuOpen) {
      event.preventDefault()
      setOpen(false)
      return
    }
    if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return
    event.preventDefault()
    openMenu(event.key === 'ArrowDown' ? 0 : OPTION_COUNT - 1)
  }

  function handleItemKeyDown(event: KeyboardEvent<HTMLButtonElement>, index: number) {
    let nextIndex: number
    switch (event.key) {
      case 'ArrowDown':
        nextIndex = (index + 1) % OPTION_COUNT
        break
      case 'ArrowUp':
        nextIndex = (index - 1 + OPTION_COUNT) % OPTION_COUNT
        break
      case 'Home':
        nextIndex = 0
        break
      case 'End':
        nextIndex = OPTION_COUNT - 1
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

  function choose(ipVersions: IPQualityCollectIPVersions) {
    closeAndRestoreFocus()
    collect.start(ipVersions)
  }

  const label = collect.submitting ? '正在发起…' : collect.active ? '采集中…' : '立即采集'
  return (
    <span className="ipq-collect" ref={containerRef}>
      <Button
        ref={triggerRef}
        id={triggerId}
        size="md"
        variant={variant}
        className={busy ? 'ipq-collect-button ipq-collect-button--busy' : 'ipq-collect-button'}
        onClick={() => {
          if (menuOpen) setOpen(false)
          else openMenu(0)
        }}
        onKeyDown={handleTriggerKeyDown}
        disabled={disabled}
        aria-busy={busy || undefined}
        aria-haspopup="menu"
        aria-controls={menuOpen ? menuId : undefined}
        aria-expanded={menuOpen}
        title={unavailable ? collectUnavailableLabel(collect.status?.unavailable_reason) : undefined}
      >
        {busy ? <span className="ipq-pulse" aria-hidden="true" /> : null}
        {label}
        {busy ? null : <span className="ipq-collect__caret" aria-hidden="true">{menuOpen ? '▴' : '▾'}</span>}
      </Button>
      {menuOpen ? (
        <span id={menuId} className="ipq-collect__menu" role="menu" aria-labelledby={triggerId} aria-orientation="vertical">
          {COLLECT_SCOPE_OPTIONS.map((option, index) => (
            <button
              key={option.label}
              ref={(node) => {
                itemRefs.current[index] = node
              }}
              type="button"
              role="menuitem"
              className="ipq-collect__item"
              onClick={() => choose(option.ipVersions)}
              onKeyDown={(event) => handleItemKeyDown(event, index)}
            >
              {option.label}
            </button>
          ))}
        </span>
      ) : null}
    </span>
  )
}

// 采集进度与结果条：只在有事可说时出现，不常驻解释文字；通知由页面外壳一次派生。
export function IPQualityCollectNotice({ notice }: { notice: CollectNotice | null }) {
  if (!notice) return <div className="ipq-notice-slot" role="status" aria-live="polite" aria-label="IP 质量采集状态" />

  return (
    <div className="ipq-notice-slot" role="status" aria-live="polite" aria-label="IP 质量采集状态">
      <div className={`ipq-notice ipq-notice--${notice.tone}`}>
        {notice.tone === 'progress' ? <span className="ipq-pulse" aria-hidden="true" /> : <span className="ipq-notice__dot" aria-hidden="true" />}
        <span className="ipq-notice__title">{notice.title}</span>
        {notice.detail ? <span className="ipq-notice__detail">{notice.detail}</span> : null}
        {notice.action ? (
          <Link className="text-link ipq-notice__action" to={notice.action.to}>
            {notice.action.label}
          </Link>
        ) : null}
        {notice.rawError ? (
          <details className="ipq-notice__raw-details" aria-label="原始错误信息">
            <summary className="ipq-notice__raw-summary">原始错误信息</summary>
            <pre className="ipq-notice__raw-text mono">{notice.rawError}</pre>
          </details>
        ) : null}
      </div>
    </div>
  )
}
