import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'

import { PRIMARY_NAV_ITEMS, PRODUCT_FULL_NAME_ZH, PRODUCT_NAME_ZH } from '../metadata'
import { AppShell } from './AppShell'
import * as authCtx from '../../lib/auth-context'

const baseAuth = {
  login: vi.fn(),
  logout: vi.fn(),
  refresh: vi.fn(),
}
const user = { user_id: 'u1', username: 'admin', role: 'admin', display_name: '' }

describe('AppShell', () => {
  it('renders sidebar chrome and sets document title when authenticated', () => {
    vi.spyOn(authCtx, 'useAuth').mockReturnValue({
      ...baseAuth,
      user,
      loading: false,
    })

    render(
      <MemoryRouter>
        <AppShell />
      </MemoryRouter>,
    )

    expect(screen.getByText(PRODUCT_NAME_ZH)).toBeInTheDocument()
    PRIMARY_NAV_ITEMS.forEach((item) => {
      expect(screen.getByRole('link', { name: item.label })).toBeInTheDocument()
    })
    expect(screen.getByText('admin')).toBeInTheDocument()
    expect(document.title).toBe(PRODUCT_FULL_NAME_ZH)
  })

  it('does not surface single-user phrasing', () => {
    vi.spyOn(authCtx, 'useAuth').mockReturnValue({
      ...baseAuth,
      user,
      loading: false,
    })

    const { container } = render(
      <MemoryRouter>
        <AppShell />
      </MemoryRouter>,
    )
    expect(container.textContent).not.toMatch(/单用户|全权限|个人系统|V1 冻结基线/)
  })

  it('renders nothing when no authenticated user', () => {
    vi.spyOn(authCtx, 'useAuth').mockReturnValue({
      ...baseAuth,
      user: null,
      loading: false,
    })
    const { container } = render(
      <MemoryRouter>
        <AppShell />
      </MemoryRouter>,
    )
    expect(container.querySelector('.app-shell')).toBeNull()
  })
})
