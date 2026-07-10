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
})
