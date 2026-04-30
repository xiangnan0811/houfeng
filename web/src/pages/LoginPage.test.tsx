import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { LoginPage } from './LoginPage'
import * as authCtx from '../lib/auth-context'

const baseAuth = { logout: vi.fn(), refresh: vi.fn() }

describe('LoginPage', () => {
  it('does not display single-user phrasing', () => {
    vi.spyOn(authCtx, 'useAuth').mockReturnValue({
      ...baseAuth,
      user: null,
      loading: false,
      login: vi.fn(),
    })
    render(
      <MemoryRouter>
        <LoginPage />
      </MemoryRouter>,
    )
    expect(screen.queryByText(/单用户|全权限|个人系统/)).toBeNull()
  })

  it('submits credentials', async () => {
    const login = vi.fn().mockResolvedValue(undefined)
    vi.spyOn(authCtx, 'useAuth').mockReturnValue({
      ...baseAuth,
      user: null,
      loading: false,
      login,
    })
    render(
      <MemoryRouter>
        <LoginPage />
      </MemoryRouter>,
    )
    fireEvent.change(screen.getByLabelText('用户名'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'pw1234567' } })
    fireEvent.click(screen.getByRole('button', { name: /登/ }))
    await waitFor(() => expect(login).toHaveBeenCalledWith('admin', 'pw1234567'))
  })

  it('shows error on bad credentials', async () => {
    const login = vi.fn().mockRejectedValue(new Error('request failed (401):'))
    vi.spyOn(authCtx, 'useAuth').mockReturnValue({
      ...baseAuth,
      user: null,
      loading: false,
      login,
    })
    render(
      <MemoryRouter>
        <LoginPage />
      </MemoryRouter>,
    )
    fireEvent.change(screen.getByLabelText('用户名'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'wrongpwd' } })
    fireEvent.click(screen.getByRole('button', { name: /登/ }))
    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument())
  })
})
