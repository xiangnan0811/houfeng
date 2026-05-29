import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation, useNavigationType } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { LoginPage } from './LoginPage'
import * as authCtx from '../lib/auth-context'

const baseAuth = { logout: vi.fn(), refresh: vi.fn() }

function mockAuth(login = vi.fn().mockResolvedValue(undefined)) {
  vi.spyOn(authCtx, 'useAuth').mockReturnValue({
    ...baseAuth,
    user: null,
    loading: false,
    login,
  })
  return login
}

function renderLogin(initialEntry = '/login') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/nodes" element={<NavigationProbe />} />
        <Route path="/" element={<NavigationProbe />} />
      </Routes>
    </MemoryRouter>,
  )
}

function NavigationProbe() {
  const location = useLocation()
  const navigationType = useNavigationType()

  return (
    <div>
      <span>当前位置 {location.pathname}</span>
      <span>导航方式 {navigationType}</span>
    </div>
  )
}

describe('LoginPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders the v2 Houfeng login IA without stale or misleading claims', () => {
    mockAuth()

    renderLogin()

    expect(screen.getByText('候风控制面板')).toBeInTheDocument()
    expect(screen.getByText('Fleet Control Plane')).toBeInTheDocument()

    const submitButton = screen.getByRole('button', { name: '登录' })
    expect(submitButton).toHaveClass('lc-btn')

    expect(screen.queryByText(/v1\.0|单用户|全权限|个人系统|SaaS|企业级|生产就绪|Docker|Kubernetes|真实库存验证完成/)).toBeNull()
  })

  it('submits credentials and replaces navigation with next target', async () => {
    const login = mockAuth()

    renderLogin('/login?next=/nodes')

    fireEvent.change(screen.getByLabelText('用户名'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'pw1234567' } })
    fireEvent.click(screen.getByRole('button', { name: '登录' }))

    await waitFor(() => expect(login).toHaveBeenCalledWith('admin', 'pw1234567'))
    await waitFor(() => expect(screen.getByText('当前位置 /nodes')).toBeInTheDocument())
    expect(screen.getByText('导航方式 REPLACE')).toBeInTheDocument()
  })

  it('falls back to replacing navigation with / after login', async () => {
    const login = mockAuth()

    renderLogin()

    fireEvent.change(screen.getByLabelText('用户名'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'pw1234567' } })
    fireEvent.click(screen.getByRole('button', { name: '登录' }))

    await waitFor(() => expect(login).toHaveBeenCalledWith('admin', 'pw1234567'))
    await waitFor(() => expect(screen.getByText('当前位置 /')).toBeInTheDocument())
    expect(screen.getByText('导航方式 REPLACE')).toBeInTheDocument()
  })

  it('shows a generic in-place alert on bad credentials', async () => {
    const login = mockAuth(vi.fn().mockRejectedValue(new Error('request failed (401): backend detail')))

    renderLogin()

    fireEvent.change(screen.getByLabelText('用户名'), { target: { value: 'admin' } })
    fireEvent.change(screen.getByLabelText('密码'), { target: { value: 'wrongpwd' } })
    fireEvent.click(screen.getByRole('button', { name: '登录' }))

    await waitFor(() => expect(login).toHaveBeenCalledWith('admin', 'wrongpwd'))
    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('用户名或密码不正确')
    expect(alert).not.toHaveTextContent('request failed')
    expect(alert).not.toHaveTextContent('backend detail')
  })
})
