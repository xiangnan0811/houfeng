import { fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import { UserChip } from './UserChip'

const user = { user_id: 'u1', username: 'admin', role: 'admin', display_name: '' }

function renderUserChip(onLogout = vi.fn(), onChangePassword = vi.fn()) {
  render(
    <MemoryRouter>
      <UserChip user={user} onLogout={onLogout} onChangePassword={onChangePassword} />
    </MemoryRouter>,
  )
  return { onLogout, onChangePassword }
}

describe('UserChip', () => {
  it('renders a named menu button with stable ownership state', () => {
    renderUserChip()

    const trigger = screen.getByRole('button', { name: 'admin 用户菜单' })
    expect(screen.getByText('admin')).toBeInTheDocument()
    expect(screen.getByText('管理员')).toBeInTheDocument()
    expect(trigger).toHaveAttribute('aria-haspopup', 'menu')
    expect(trigger).toHaveAttribute('aria-expanded', 'false')
    expect(trigger).toHaveAttribute('aria-controls')
    expect(screen.queryByText(/单用户|全权限|个人系统/)).not.toBeInTheDocument()
  })

  it('opens a two-command menu and focuses the first item', () => {
    renderUserChip()
    const trigger = screen.getByRole('button', { name: 'admin 用户菜单' })

    fireEvent.click(trigger)

    const menu = screen.getByRole('menu')
    expect(trigger).toHaveAttribute('aria-expanded', 'true')
    expect(menu).toHaveAttribute('id', trigger.getAttribute('aria-controls'))
    const items = within(menu).getAllByRole('menuitem')
    expect(items.map((item) => item.textContent)).toEqual(['修改密码', '退出登录'])
    const firstItem = items[0]
    if (!firstItem) throw new Error('user menu must expose a first command')
    expect(firstItem).toHaveFocus()
    expect(within(menu).queryByText('主题设置')).not.toBeInTheDocument()
  })

  it('opens at the requested boundary from ArrowDown and ArrowUp on the trigger', () => {
    renderUserChip()
    const trigger = screen.getByRole('button', { name: 'admin 用户菜单' })

    fireEvent.keyDown(trigger, { key: 'ArrowUp' })
    const upItems = screen.getAllByRole('menuitem')
    const upLastItem = upItems[1]
    if (!upLastItem) throw new Error('user menu must expose two commands')
    expect(upLastItem).toHaveFocus()

    fireEvent.keyDown(upLastItem, { key: 'Escape' })
    fireEvent.keyDown(trigger, { key: 'ArrowDown' })
    const downFirstItem = screen.getAllByRole('menuitem')[0]
    if (!downFirstItem) throw new Error('user menu must expose a first command')
    expect(downFirstItem).toHaveFocus()
  })

  it('supports Arrow, Home, and End navigation with wraparound', () => {
    renderUserChip()
    fireEvent.click(screen.getByRole('button', { name: 'admin 用户菜单' }))
    const items = screen.getAllByRole('menuitem')
    const [firstItem, secondItem] = items
    if (!firstItem || !secondItem) throw new Error('user menu must expose two commands')

    fireEvent.keyDown(firstItem, { key: 'ArrowDown' })
    expect(secondItem).toHaveFocus()
    fireEvent.keyDown(secondItem, { key: 'ArrowDown' })
    expect(firstItem).toHaveFocus()
    fireEvent.keyDown(firstItem, { key: 'ArrowUp' })
    expect(secondItem).toHaveFocus()
    fireEvent.keyDown(secondItem, { key: 'Home' })
    expect(firstItem).toHaveFocus()
    fireEvent.keyDown(firstItem, { key: 'End' })
    expect(secondItem).toHaveFocus()
  })

  it('closes on Escape and restores focus to the trigger', () => {
    renderUserChip()
    const trigger = screen.getByRole('button', { name: 'admin 用户菜单' })
    fireEvent.click(trigger)

    const escapeItem = screen.getAllByRole('menuitem')[0]
    if (!escapeItem) throw new Error('user menu must expose a command')
    fireEvent.keyDown(escapeItem, { key: 'Escape' })

    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    expect(trigger).toHaveAttribute('aria-expanded', 'false')
    expect(trigger).toHaveFocus()
  })

  it('closes on Tab without trapping focus', () => {
    renderUserChip()
    fireEvent.click(screen.getByRole('button', { name: 'admin 用户菜单' }))

    const tabItem = screen.getAllByRole('menuitem')[0]
    if (!tabItem) throw new Error('user menu must expose a command')
    fireEvent.keyDown(tabItem, { key: 'Tab' })

    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('closes on an outside pointer press', () => {
    renderUserChip()
    fireEvent.click(screen.getByRole('button', { name: 'admin 用户菜单' }))

    fireEvent.mouseDown(document.body)

    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('invokes each command once after closing the menu', () => {
    const { onLogout, onChangePassword } = renderUserChip()
    const trigger = screen.getByRole('button', { name: 'admin 用户菜单' })

    fireEvent.click(trigger)
    fireEvent.click(screen.getByRole('menuitem', { name: '修改密码' }))
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    expect(onChangePassword).toHaveBeenCalledTimes(1)

    fireEvent.click(trigger)
    fireEvent.click(screen.getByRole('menuitem', { name: '退出登录' }))
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
    expect(onLogout).toHaveBeenCalledTimes(1)
  })
})
