import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useRef, useState } from 'react'
import { describe, expect, it, vi } from 'vitest'

import { VPSManagementMenu } from './VPSManagementMenu'
import type { VPSManagementController } from './hooks/useVPSManagementController'

function controller(overrides: Partial<VPSManagementController> = {}): VPSManagementController {
  return {
    panel: 'menu',
    menuOpen: true,
    openMenu: vi.fn(),
    closeMenu: vi.fn(),
    openPanel: vi.fn(),
    closePanel: vi.fn(),
    ...overrides,
  }
}

describe('VPSManagementMenu', () => {
  it('moves focus with arrow, home, and end keys', () => {
    render(<VPSManagementMenu lifecycleStatus="active" controller={controller()} />)
    const items = screen.getAllByRole('menuitem')
    expect(items[0]).toHaveFocus()

    fireEvent.keyDown(document, { key: 'ArrowDown' })
    expect(items[1]).toHaveFocus()
    fireEvent.keyDown(document, { key: 'End' })
    expect(items[items.length - 1]).toHaveFocus()
    fireEvent.keyDown(document, { key: 'Home' })
    expect(items[0]).toHaveFocus()
    fireEvent.keyDown(document, { key: 'ArrowUp' })
    expect(items[items.length - 1]).toHaveFocus()
  })

  it('shows cancellation after a cancel-like renewal and keeps archive on to_cancel only', () => {
    const { rerender } = render(<VPSManagementMenu lifecycleStatus="active" controller={controller()} />)
    expect(screen.queryByRole('menuitem', { name: '取消 / 退役' })).not.toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: '归档' })).not.toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: '编辑事实' })).toBeInTheDocument()

    rerender(<VPSManagementMenu lifecycleStatus="active" renewalDecision="cancel" controller={controller()} />)
    expect(screen.getByRole('menuitem', { name: '取消 / 退役' })).toBeInTheDocument()
    expect(screen.queryByRole('menuitem', { name: '归档' })).not.toBeInTheDocument()

    rerender(<VPSManagementMenu lifecycleStatus="to_cancel" controller={controller()} />)
    expect(screen.getByRole('menuitem', { name: '取消 / 退役' })).toBeInTheDocument()
    expect(screen.getByRole('menuitem', { name: '归档' })).toBeInTheDocument()

    rerender(<VPSManagementMenu lifecycleStatus="cancelled" controller={controller()} />)
    expect(screen.queryByRole('menuitem')).not.toBeInTheDocument()
  })

  it('closes on Tab without preventing the default action', async () => {
    function TabHarness() {
      const trigger = useRef<HTMLButtonElement>(null)
      const [menuOpen, setMenuOpen] = useState(true)
      const management = controller({
        menuOpen,
        closeMenu: () => setMenuOpen(false),
      })
      return (
        <>
          <button type="button" ref={trigger}>管理</button>
          <button type="button">下一个控件</button>
          {menuOpen ? (
            <VPSManagementMenu
              lifecycleStatus="active"
              controller={management}
              returnFocusRef={trigger}
            />
          ) : null}
        </>
      )
    }
    render(<TabHarness />)
    const trigger = screen.getByRole('button', { name: '管理' })
    trigger.focus()
    expect(screen.getAllByRole('menuitem').length).toBeGreaterThan(0)

    const event = new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true })
    await act(async () => {
      document.dispatchEvent(event)
    })

    expect(event.defaultPrevented).toBe(false)
    await waitFor(() => expect(screen.queryAllByRole('menuitem')).toHaveLength(0))
  })
})
