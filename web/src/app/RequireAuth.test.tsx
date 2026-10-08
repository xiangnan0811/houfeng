import { fireEvent, render, screen } from '@testing-library/react'
import { describe, it, expect, vi } from 'vitest'
import { MemoryRouter, Routes, Route } from 'react-router-dom'
import { RequireAuth } from './RequireAuth'
import { RuntimeCapabilityGate } from './RuntimeCapabilityGate'
import * as authCtx from '../lib/auth-context'

function Protected() {
  return <div>secret</div>
}
function Login() {
  return <div>login</div>
}

const enabledCapabilities = { records: true, comparison: true, portability: true }

const retainedUser = {
  user_id: 'u1',
  username: 'admin',
  role: 'admin',
  display_name: '',
  runtime_capabilities: enabledCapabilities,
  management_capabilities: { access: false },
}

const baseAuth = {
  login: vi.fn(),
  logout: vi.fn(),
  refresh: vi.fn(),
  retry: vi.fn(),
  status: 'anonymous' as const,
  error: null,
}

describe('RequireAuth', () => {
  it('renders children when authenticated', () => {
    vi.spyOn(authCtx, 'useAuth').mockReturnValue({
      ...baseAuth,
      user: retainedUser,
      loading: false,
      status: 'ready' as const,
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

  it('shows a capability read error with retry instead of the login page', () => {
    const retry = vi.fn().mockResolvedValue(undefined)
    vi.spyOn(authCtx, 'useAuth').mockReturnValue({
      ...baseAuth,
      retry,
      user: null,
      loading: false,
      status: 'error',
      error: '能力读取失败',
    })
    render(
      <MemoryRouter initialEntries={['/records']}>
        <Routes>
          <Route element={<RequireAuth />}>
            <Route path="/records" element={<Protected />} />
          </Route>
          <Route path="/login" element={<Login />} />
        </Routes>
      </MemoryRouter>,
    )
    expect(screen.getByRole('heading', { name: '能力读取错误' })).toBeInTheDocument()
    expect(screen.queryByText('login')).toBeNull()
    expect(screen.queryByText('secret')).toBeNull()
    expect(screen.queryByText('记录平台未启用')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    expect(retry).toHaveBeenCalled()
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

  it('shows retry instead of the protected tree when a retained session cannot be refreshed', () => {
    const retry = vi.fn().mockResolvedValue(undefined)
    vi.spyOn(authCtx, 'useAuth').mockReturnValue({
      ...baseAuth,
      retry,
      user: retainedUser,
      loading: false,
      status: 'error',
      error: '能力读取失败',
    })
    render(
      <MemoryRouter initialEntries={['/records']}>
        <Routes>
          <Route element={<RequireAuth />}>
            <Route path="/records" element={<Protected />} />
          </Route>
          <Route path="/login" element={<Login />} />
        </Routes>
      </MemoryRouter>,
    )
    expect(screen.getByRole('heading', { name: '能力读取错误' })).toBeInTheDocument()
    expect(screen.getByText('能力读取失败')).toBeInTheDocument()
    expect(screen.queryByText('secret')).toBeNull()
    expect(screen.queryByText('login')).toBeNull()
    expect(screen.queryByText('记录平台未启用')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: '重试' }))
    expect(retry).toHaveBeenCalled()
  })
})

describe('RuntimeCapabilityGate', () => {
  it('does not mount a feature subtree while a retained session is in capability error', () => {
    vi.spyOn(authCtx, 'useAuth').mockReturnValue({
      ...baseAuth,
      user: retainedUser,
      loading: false,
      status: 'error',
      error: '能力读取失败',
    })
    render(
      <RuntimeCapabilityGate kind="records">
        <div>records-child</div>
      </RuntimeCapabilityGate>,
    )
    expect(screen.getByRole('heading', { name: '能力读取错误' })).toBeInTheDocument()
    expect(screen.queryByText('records-child')).toBeNull()
    expect(screen.queryByText('记录平台未启用')).toBeNull()
  })
})
