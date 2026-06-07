import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { Modal } from './Modal'

describe('Modal', () => {
  it('can render confirmation flows as alertdialog', () => {
    render(
      <Modal open onClose={vi.fn()} title="确认暂停" dialogRole="alertdialog">
        <p>暂停后会停止采集。</p>
      </Modal>,
    )

    expect(screen.getByRole('alertdialog', { name: '确认暂停' })).toBeInTheDocument()
  })

  it('generates a unique labelledby id for each dialog title', () => {
    render(
      <>
        <Modal open onClose={vi.fn()} title="第一个弹窗">
          <p>One</p>
        </Modal>
        <Modal open onClose={vi.fn()} title="第二个弹窗">
          <p>Two</p>
        </Modal>
      </>,
    )

    const first = screen.getByRole('dialog', { name: '第一个弹窗' })
    const second = screen.getByRole('dialog', { name: '第二个弹窗' })
    expect(first).toHaveAttribute('aria-labelledby')
    expect(second).toHaveAttribute('aria-labelledby')
    expect(first.getAttribute('aria-labelledby')).not.toBe(second.getAttribute('aria-labelledby'))
  })

  it('moves focus into the dialog and restores focus after close', () => {
    function Harness() {
      const [open, setOpen] = useState(false)
      return (
        <>
          <button type="button" onClick={() => setOpen(true)}>
            打开弹窗
          </button>
          <Modal open={open} onClose={() => setOpen(false)} title="焦点弹窗">
            <button type="button">弹窗动作</button>
          </Modal>
        </>
      )
    }

    render(<Harness />)
    const trigger = screen.getByRole('button', { name: '打开弹窗' })
    trigger.focus()
    fireEvent.click(trigger)

    expect(screen.getByRole('dialog', { name: '焦点弹窗' })).toContainElement(document.activeElement as HTMLElement)
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(trigger).toHaveFocus()
  })
})
