import { fireEvent, render, screen } from '@testing-library/react'
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

  it('renders nothing when open=false and mounts content when reopened', () => {
    const { rerender, container } = render(
      <Drawer open={false} onClose={() => {}} title="历史">
        <p>抽屉内容</p>
      </Drawer>,
    )
    expect(container.querySelector('.drawer')).toBeNull()
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
    const { container } = render(
      <Drawer open onClose={onClose} title="历史">
        <p>抽屉内容</p>
      </Drawer>,
    )
    const overlay = container.querySelector('.drawer-overlay')
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

  it('uses left side positioning when side="left"', () => {
    render(
      <Drawer open onClose={() => {}} title="历史" side="left">
        <p>抽屉内容</p>
      </Drawer>,
    )
    expect(screen.getByRole('dialog')).toHaveClass('drawer--left')
  })
})
