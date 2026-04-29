import { render, screen, fireEvent } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { UserChip } from './UserChip'

const user = { user_id: 'u1', username: 'admin', role: 'admin', display_name: '' }

describe('UserChip', () => {
  it('shows username and role label', () => {
    render(
      <MemoryRouter>
        <UserChip user={user} onLogout={() => {}} onChangePassword={() => {}} />
      </MemoryRouter>,
    )
    expect(screen.getByText('admin')).toBeInTheDocument()
    expect(screen.getByText('管理员')).toBeInTheDocument()
  })

  it('does not display single-user phrasing', () => {
    render(
      <MemoryRouter>
        <UserChip user={user} onLogout={() => {}} onChangePassword={() => {}} />
      </MemoryRouter>,
    )
    expect(screen.queryByText(/单用户|全权限|个人系统/)).toBeNull()
  })

  it('opens menu on click and exposes 退出登录', () => {
    render(
      <MemoryRouter>
        <UserChip user={user} onLogout={() => {}} onChangePassword={() => {}} />
      </MemoryRouter>,
    )
    fireEvent.click(screen.getByRole('button', { name: /admin/ }))
    expect(screen.getByText('退出登录')).toBeInTheDocument()
  })

  it('logout button calls onLogout', () => {
    const onLogout = vi.fn()
    render(
      <MemoryRouter>
        <UserChip user={user} onLogout={onLogout} onChangePassword={() => {}} />
      </MemoryRouter>,
    )
    fireEvent.click(screen.getByRole('button', { name: /admin/ }))
    fireEvent.click(screen.getByText('退出登录'))
    expect(onLogout).toHaveBeenCalled()
  })

  it('change password button calls onChangePassword', () => {
    const onChangePassword = vi.fn()
    render(
      <MemoryRouter>
        <UserChip user={user} onLogout={() => {}} onChangePassword={onChangePassword} />
      </MemoryRouter>,
    )
    fireEvent.click(screen.getByRole('button', { name: /admin/ }))
    fireEvent.click(screen.getByText('修改密码'))
    expect(onChangePassword).toHaveBeenCalled()
  })
})
