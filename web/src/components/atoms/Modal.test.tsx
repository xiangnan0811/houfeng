import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { getModalDepth, isTopModal } from '../../lib/modalStack'
import { Modal } from './Modal'

function NestedModalHarness({ persistentChild = false }: { persistentChild?: boolean }) {
  const [parentOpen, setParentOpen] = useState(false)
  const [childOpen, setChildOpen] = useState(false)
  const [grandchildOpen, setGrandchildOpen] = useState(false)

  return (
    <>
      <button type="button" onClick={() => setParentOpen(true)}>
        打开父弹窗
      </button>
      <Modal open={parentOpen} onClose={() => setParentOpen(false)} title="父弹窗">
        <button type="button" onClick={() => setChildOpen(true)}>
          打开子确认
        </button>
        <button type="button">父层末尾</button>
        <Modal
          open={childOpen}
          onClose={() => setChildOpen(false)}
          title="子确认"
          dialogRole="alertdialog"
          persistent={persistentChild}
        >
          <button type="button" onClick={() => setGrandchildOpen(true)}>
            打开第三层
          </button>
          <button type="button">子层确认</button>
          <Modal
            open={grandchildOpen}
            onClose={() => setGrandchildOpen(false)}
            title="第三层确认"
            dialogRole="alertdialog"
          >
            <button type="button">最终确认</button>
          </Modal>
        </Modal>
      </Modal>
    </>
  )
}

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

    const first = screen.getByText('第一个弹窗').closest('[role="dialog"]')
    const second = screen.getByText('第二个弹窗').closest('[role="dialog"]')
    expect(first).not.toBeNull()
    expect(second).not.toBeNull()
    expect(first).toHaveAttribute('aria-labelledby')
    expect(second).toHaveAttribute('aria-labelledby')
    expect(first?.getAttribute('aria-labelledby')).not.toBe(second?.getAttribute('aria-labelledby'))
    expect(first).toHaveAttribute('aria-hidden', 'true')
    expect(second).toHaveAttribute('aria-modal', 'true')
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

  it('keeps focus, Escape, accessibility and scroll lock owned by the top modal', async () => {
    render(<NestedModalHarness />)
    const parentTrigger = screen.getByRole('button', { name: '打开父弹窗' })
    parentTrigger.focus()
    fireEvent.click(parentTrigger)

    const parent = screen.getByRole('dialog', { name: '父弹窗' })
    const childTrigger = screen.getByRole('button', { name: '打开子确认' })
    const focusChildTrigger = childTrigger.focus.bind(childTrigger)
    vi.spyOn(childTrigger, 'focus').mockImplementation(() => {
      if (!childTrigger.closest('[inert]')) focusChildTrigger()
    })
    childTrigger.focus()
    fireEvent.click(childTrigger)

    const child = screen.getByRole('alertdialog', { name: '子确认' })
    expect(child).toContainElement(document.activeElement as HTMLElement)
    expect(parent).toHaveAttribute('aria-hidden', 'true')
    expect(parent).toHaveAttribute('inert')
    expect(document.body).toHaveStyle({ overflow: 'hidden' })

    screen.getByRole('button', { name: '父层末尾', hidden: true }).focus()
    fireEvent.keyDown(document, { key: 'Tab' })
    expect(child).toContainElement(document.activeElement as HTMLElement)

    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('alertdialog', { name: '子确认' })).not.toBeInTheDocument()
    expect(parent).toBeInTheDocument()
    expect(parent).not.toHaveAttribute('aria-hidden')
    expect(document.body).toHaveStyle({ overflow: 'hidden' })
    await waitFor(() => expect(childTrigger).toHaveFocus())

    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('dialog', { name: '父弹窗' })).not.toBeInTheDocument()
    expect(parentTrigger).toHaveFocus()
    expect(document.body).not.toHaveStyle({ overflow: 'hidden' })
  })

  it('closes only one layer per Escape across three nested modals', () => {
    render(<NestedModalHarness />)
    fireEvent.click(screen.getByRole('button', { name: '打开父弹窗' }))
    fireEvent.click(screen.getByRole('button', { name: '打开子确认' }))
    fireEvent.click(screen.getByRole('button', { name: '打开第三层' }))

    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('alertdialog', { name: '第三层确认' })).not.toBeInTheDocument()
    expect(screen.getByRole('alertdialog', { name: '子确认' })).toBeInTheDocument()
    expect(document.body).toHaveStyle({ overflow: 'hidden' })

    fireEvent.keyDown(document, { key: 'Escape' })
    expect(screen.queryByRole('alertdialog', { name: '子确认' })).not.toBeInTheDocument()
    expect(screen.getByRole('dialog', { name: '父弹窗' })).toBeInTheDocument()
  })

  it('treats the deepest dialog as top when nested layers mount together', () => {
    render(
      <Modal open onClose={vi.fn()} title="同时挂载父层">
        <button type="button">父层动作</button>
        <Modal open onClose={vi.fn()} title="同时挂载子层" dialogRole="alertdialog">
          <button type="button">子层动作</button>
        </Modal>
      </Modal>,
    )

    const parent = screen.getByText('同时挂载父层').closest('[role="dialog"]')
    const child = screen.getByText('同时挂载子层').closest('[role="alertdialog"]')
    if (!parent || !child) throw new Error('Expected both nested modal elements')
    const parentId = parent.getAttribute('data-modal-stack-id') ?? ''
    const childId = child.getAttribute('data-modal-stack-id') ?? ''
    expect(getModalDepth(parentId)).toBe(1)
    expect(getModalDepth(childId)).toBe(2)
    expect(isTopModal(childId)).toBe(true)
    expect(parent).toHaveAttribute('aria-hidden', 'true')
    expect(child).toHaveAttribute('aria-modal', 'true')
    expect(child).toContainElement(document.activeElement as HTMLElement)
    expect(parent.closest('.modal-overlay')).not.toHaveClass('modal-stack-layer--top')
    expect(child.closest('.modal-overlay')).toHaveClass('modal-stack-layer--top')
  })

  it('keeps a persistent top modal open for Escape and backdrop clicks', () => {
    render(<NestedModalHarness persistentChild />)
    fireEvent.click(screen.getByRole('button', { name: '打开父弹窗' }))
    fireEvent.click(screen.getByRole('button', { name: '打开子确认' }))

    const child = screen.getByRole('alertdialog', { name: '子确认' })
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(child).toBeInTheDocument()

    const backdrop = child.closest('.modal-overlay')
    expect(backdrop).not.toBeNull()
    fireEvent.click(backdrop!)
    expect(child).toBeInTheDocument()

    fireEvent.click(within(child).getByRole('button', { name: '关闭' }))
    expect(screen.queryByRole('alertdialog', { name: '子确认' })).not.toBeInTheDocument()
  })
})
