import { render, screen } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { RequireAuth } from './RequireAuth'
import * as authCtx from '../lib/auth-context'

function Protected() {
  return <div>secret</div>
}
function Login() {
  return <div>login</div>
}

const baseAuth = {
  login: vi.fn(),
  logout: vi.fn(),
  refresh: vi.fn(),
}

describe('RequireAuth', () => {
  it('renders children when authenticated', () => {
    vi.spyOn(authCtx, 'useAuth').mockReturnValue({
      ...baseAuth,
      user: { user_id: 'u1', username: 'admin', role: 'admin', display_name: '' },
      loading: false,
    })
    render(
      <MemoryRouter initialEntries={['/x']}>
        <Routes>
          <Route element={<RequireAuth />}>
            <Route path="/x" element={<Protected />} />
          </Route>
          <Route path="/login" element={<Login />} />
        </Routes>
      </MemoryRouter>,
    )
    expect(screen.getByText('secret')).toBeInTheDocument()
  })

  it('redirects to /login when unauthenticated', () => {
    vi.spyOn(authCtx, 'useAuth').mockReturnValue({
      ...baseAuth,
      user: null,
      loading: false,
    })
    render(
      <MemoryRouter initialEntries={['/x']}>
        <Routes>
          <Route element={<RequireAuth />}>
            <Route path="/x" element={<Protected />} />
          </Route>
          <Route path="/login" element={<Login />} />
        </Routes>
      </MemoryRouter>,
    )
    expect(screen.getByText('login')).toBeInTheDocument()
  })

  it('shows nothing while loading', () => {
    vi.spyOn(authCtx, 'useAuth').mockReturnValue({
      ...baseAuth,
      user: null,
      loading: true,
    })
    render(
      <MemoryRouter initialEntries={['/x']}>
        <Routes>
          <Route element={<RequireAuth />}>
            <Route path="/x" element={<Protected />} />
          </Route>
          <Route path="/login" element={<Login />} />
        </Routes>
      </MemoryRouter>,
    )
    expect(screen.queryByText('secret')).toBeNull()
    expect(screen.queryByText('login')).toBeNull()
  })
})
