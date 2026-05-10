import { useState } from 'react'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { Drawer } from './Drawer'

describe('Drawer', () => {
  it('renders title and children when open', () => {
    render(
      <Drawer open onClose={() => {}} title="节点 nd_001 · 历史">
        <p>抽屉内容</p>
      </Drawer>,
    )
    expect(screen.getByRole('dialog')).toHaveAttribute('aria-modal', 'true')
    expect(screen.getByText('节点 nd_001 · 历史')).toBeInTheDocument()
    expect(screen.getByText('抽屉内容')).toBeInTheDocument()
  })

  it('renders the dialog through a document body portal', () => {
    const { container } = render(
      <Drawer open onClose={() => {}} title="历史">
        <p>抽屉内容</p>
      </Drawer>,
    )

    const dialog = screen.getByRole('dialog')
    expect(container.querySelector('.drawer')).toBeNull()
    expect(document.body.querySelector('.drawer')).toBe(dialog)
  })

  it('renders nothing when open=false and mounts content when reopened', () => {
    const { rerender } = render(
      <Drawer open={false} onClose={() => {}} title="历史">
        <p>抽屉内容</p>
      </Drawer>,
    )
    expect(document.body.querySelector('.drawer')).toBeNull()
    expect(screen.queryByText('抽屉内容')).not.toBeInTheDocument()

    rerender(
      <Drawer open onClose={() => {}} title="历史">
        <p>抽屉内容</p>
      </Drawer>,
    )
    const dialogOpen = screen.getByRole('dialog')
    expect(dialogOpen).toHaveClass('drawer--open')
    expect(screen.getByText('抽屉内容')).toBeInTheDocument()
  })

  it('triggers onClose when overlay is clicked', () => {
    const onClose = vi.fn()
    render(
      <Drawer open onClose={onClose} title="历史">
        <p>抽屉内容</p>
      </Drawer>,
    )
    const overlay = document.body.querySelector('.drawer-overlay')
    expect(overlay).not.toBeNull()
    fireEvent.mouseDown(overlay!)
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('triggers onClose when close button is clicked', () => {
    const onClose = vi.fn()
    render(
      <Drawer open onClose={onClose} title="历史">
        <p>抽屉内容</p>
      </Drawer>,
    )
    fireEvent.click(screen.getByRole('button', { name: '关闭' }))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('triggers onClose when Esc key is pressed', () => {
    const onClose = vi.fn()
    render(
      <Drawer open onClose={onClose} title="历史">
        <p>抽屉内容</p>
      </Drawer>,
    )
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('does not trigger onClose on Esc when closed', () => {
    const onClose = vi.fn()
    render(
      <Drawer open={false} onClose={onClose} title="历史">
        <p>抽屉内容</p>
      </Drawer>,
    )
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).not.toHaveBeenCalled()
  })

  it('focuses the close button on open and restores focus to the opener on close', async () => {
    function DrawerHarness() {
      const [open, setOpen] = useState(false)

      return (
        <>
          <button type="button" onClick={() => setOpen(true)}>
            打开抽屉
          </button>
          <Drawer open={open} onClose={() => setOpen(false)} title="历史">
            <button type="button">内容动作</button>
          </Drawer>
        </>
      )
    }

    render(<DrawerHarness />)
    const opener = screen.getByRole('button', { name: '打开抽屉' })
    opener.focus()
    fireEvent.click(opener)

    const closeButton = screen.getByRole('button', { name: '关闭' })
    await waitFor(() => expect(closeButton).toHaveFocus())

    fireEvent.click(closeButton)
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(opener).toHaveFocus()
  })

  it('contains Tab navigation within the drawer', async () => {
    render(
      <Drawer open onClose={() => {}} title="历史">
        <button type="button">第一项</button>
        <button type="button">第二项</button>
      </Drawer>,
    )

    const closeButton = screen.getByRole('button', { name: '关闭' })
    const lastAction = screen.getByRole('button', { name: '第二项' })
    await waitFor(() => expect(closeButton).toHaveFocus())

    lastAction.focus()
    fireEvent.keyDown(document, { key: 'Tab' })
    expect(closeButton).toHaveFocus()

    closeButton.focus()
    fireEvent.keyDown(document, { key: 'Tab', shiftKey: true })
    expect(lastAction).toHaveFocus()
  })

  it('uses left side positioning when side="left"', () => {
    render(
      <Drawer open onClose={() => {}} title="历史" side="left">
        <p>抽屉内容</p>
      </Drawer>,
    )
    expect(screen.getByRole('dialog')).toHaveClass('drawer--left')
  })
})
