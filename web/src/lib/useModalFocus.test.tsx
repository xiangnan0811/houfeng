import { fireEvent, render, screen } from '@testing-library/react'
import { StrictMode, useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { getModalDepth } from './modalStack'
import { useModalFocus } from './useModalFocus'

afterEach(() => {
  document.body.style.overflow = ''
})

function FocusSurface({ onClose }: { onClose: () => void }) {
  const ref = useModalFocus<HTMLDivElement>(true, onClose, 'focus-surface')
  return (
    <div ref={ref} role="dialog" aria-label="焦点层" tabIndex={-1}>
      <button type="button">第一个动作</button>
      <button type="button" onClick={onClose}>关闭焦点层</button>
    </div>
  )
}

function EmptyFocusSurface({ onClose }: { onClose: () => void }) {
  const ref = useModalFocus<HTMLDivElement>(true, onClose, 'empty-focus-surface')
  return <div ref={ref} role="dialog" aria-label="空焦点层" tabIndex={-1} />
}

function DetachedFocusSurface({ onClose }: { onClose: () => void }) {
  useModalFocus<HTMLDivElement>(true, onClose, 'detached-focus-surface')
  return <div>没有连接焦点引用</div>
}

function DetachableFocusSurface({ onClose }: { onClose: () => void }) {
  const [attached, setAttached] = useState(true)
  const ref = useModalFocus<HTMLDivElement>(true, onClose, 'detachable-focus-surface')
  return attached ? (
    <div ref={ref} role="dialog" aria-label="可分离焦点层" tabIndex={-1}>
      <button type="button" onClick={() => setAttached(false)}>分离容器</button>
    </div>
  ) : <div>焦点容器已分离</div>
}

function FilteredFocusSurface({ onClose }: { onClose: () => void }) {
  const ref = useModalFocus<HTMLDivElement>(true, onClose, 'filtered-focus-surface')
  return (
    <div ref={ref} role="dialog" aria-label="过滤焦点层" tabIndex={-1}>
      <div hidden><button type="button">隐藏祖先动作</button></div>
      <div aria-hidden="true"><button type="button">ARIA 隐藏动作</button></div>
      <button type="button" style={{ display: 'none' }}>display 隐藏动作</button>
      <button type="button" style={{ visibility: 'hidden' }}>visibility 隐藏动作</button>
      <button type="button">可见动作</button>
    </div>
  )
}

function CyclicParentFocusSurface({ onClose }: { onClose: () => void }) {
  const [open, setOpen] = useState(false)
  const ref = useModalFocus<HTMLDivElement>(open, onClose, 'cycle-child')
  return (
    <>
      <div data-modal-stack-id="cycle-candidate" data-modal-stack-parent-id="cycle-child">
        <button type="button" onClick={() => setOpen(true)}>打开循环候选层</button>
      </div>
      {open ? (
        <div ref={ref} role="dialog" aria-label="循环候选层" tabIndex={-1}>
          <button type="button">循环层动作</button>
        </div>
      ) : null}
    </>
  )
}

describe('useModalFocus', () => {
  it('keeps one registration in StrictMode and releases focus and scroll lock on unmount', () => {
    document.body.style.overflow = 'clip'

    function Harness() {
      const [open, setOpen] = useState(false)
      return (
        <>
          <button type="button" onClick={() => setOpen(true)}>打开焦点层</button>
          {open ? <FocusSurface onClose={() => setOpen(false)} /> : null}
        </>
      )
    }

    render(<StrictMode><Harness /></StrictMode>)
    const opener = screen.getByRole('button', { name: '打开焦点层' })
    opener.focus()
    fireEvent.click(opener)

    expect(screen.getByRole('button', { name: '第一个动作' })).toHaveFocus()
    expect(getModalDepth('focus-surface')).toBe(1)
    expect(document.body).toHaveStyle({ overflow: 'hidden' })

    fireEvent.click(screen.getByRole('button', { name: '关闭焦点层' }))
    expect(screen.queryByRole('dialog', { name: '焦点层' })).not.toBeInTheDocument()
    expect(getModalDepth('focus-surface')).toBe(0)
    expect(document.body).toHaveStyle({ overflow: 'clip' })
    expect(opener).toHaveFocus()
  })

  it('uses the latest close callback without re-registering the modal', () => {
    const firstClose = vi.fn()
    const latestClose = vi.fn()
    const { rerender } = render(<FocusSurface onClose={firstClose} />)
    rerender(<FocusSurface onClose={latestClose} />)

    fireEvent.keyDown(document, { key: 'Escape' })
    expect(firstClose).not.toHaveBeenCalled()
    expect(latestClose).toHaveBeenCalledTimes(1)
    expect(getModalDepth('focus-surface')).toBe(1)
  })

  it('does not register or lock scrolling until its container is connected', () => {
    document.body.style.overflow = 'clip'

    render(<DetachedFocusSurface onClose={vi.fn()} />)

    expect(getModalDepth('detached-focus-surface')).toBe(0)
    expect(document.body).toHaveStyle({ overflow: 'clip' })
  })

  it('focuses an empty container and keeps Tab inside it', () => {
    render(<EmptyFocusSurface onClose={vi.fn()} />)
    const dialog = screen.getByRole('dialog', { name: '空焦点层' })

    expect(dialog).toHaveFocus()

    fireEvent.keyDown(document, { key: 'Tab' })

    expect(dialog).toHaveFocus()
  })

  it('cycles focus from the first and last actions with real Tab directions', () => {
    render(<FocusSurface onClose={vi.fn()} />)
    const first = screen.getByRole('button', { name: '第一个动作' })
    const last = screen.getByRole('button', { name: '关闭焦点层' })

    expect(first).toHaveFocus()

    last.focus()
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true })
    expect(last).toHaveFocus()

    first.focus()
    fireEvent.keyDown(document, { key: 'Tab' })
    expect(first).toHaveFocus()

    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true })
    expect(last).toHaveFocus()

    fireEvent.keyDown(document, { key: 'Tab' })
    expect(first).toHaveFocus()
  })

  it('ignores Tab after the registered focus container is detached', () => {
    const onClose = vi.fn()
    render(<DetachableFocusSurface onClose={onClose} />)

    fireEvent.click(screen.getByRole('button', { name: '分离容器' }))
    fireEvent.keyDown(document, { key: 'Tab' })

    expect(screen.getByText('焦点容器已分离')).toBeInTheDocument()
    expect(onClose).not.toHaveBeenCalled()
  })

  it('skips controls hidden by ancestors, ARIA, display, or visibility', () => {
    render(<FilteredFocusSurface onClose={vi.fn()} />)

    expect(screen.getByRole('button', { name: '可见动作' })).toHaveFocus()
  })

  it('does not infer a parent that would make the new modal its own ancestor', () => {
    render(<CyclicParentFocusSurface onClose={vi.fn()} />)
    const opener = screen.getByRole('button', { name: '打开循环候选层' })
    opener.focus()

    fireEvent.click(opener)

    expect(screen.getByRole('button', { name: '循环层动作' })).toHaveFocus()
    expect(getModalDepth('cycle-child')).toBe(1)
  })

  it('opens safely from an SVG focus target', () => {
    const { container } = render(
      <svg><circle aria-label="SVG 触发点" tabIndex={0} /></svg>,
    )
    const trigger = container.querySelector('circle')
    trigger?.focus()
    expect(document.activeElement).toBe(trigger)

    render(<FocusSurface onClose={vi.fn()} />)

    expect(screen.getByRole('button', { name: '第一个动作' })).toHaveFocus()
  })
})
