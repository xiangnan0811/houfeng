import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { Sidebar } from './Sidebar'

const user = { user_id: 'u1', username: 'admin', role: 'admin', display_name: '' }

describe('Sidebar', () => {
  it('renders brand and grouped asset-aware nav items', () => {
    const { container } = render(
      <MemoryRouter>
        <Sidebar
          user={user}
          anomalyCounts={{ monitoring: 0, targets: 0 }}
          collapsed={false}
          onToggle={() => {}}
          onLogout={() => {}}
          onChangePassword={() => {}}
        />
      </MemoryRouter>,
    )
    expect(screen.getByText('候风')).toBeInTheDocument()
    for (const label of ['运营', '资产', '观测', '系统']) {
      expect(
        Array.from(container.querySelectorAll('.nav-label')).some(
          (title) => title.textContent === label,
        ),
      ).toBe(true)
    }
    for (const label of ['运维记录', 'VPS', '归档', '服务商', '订阅', '资产决策', '监控', '入口探测', '事件', '命令审计', '设置']) {
      expect(screen.getByRole('link', { name: label })).toBeInTheDocument()
    }
    expect(screen.getByRole('link', { name: 'VPS' })).toHaveAttribute('aria-label', 'VPS')
    expect(screen.queryByRole('link', { name: '首页' })).not.toBeInTheDocument()
  })

  it('renders anomaly counts only on monitoring/targets nav', () => {
    render(
      <MemoryRouter>
        <Sidebar
          user={user}
          anomalyCounts={{ monitoring: 3, targets: 1 }}
          collapsed={false}
          onToggle={() => {}}
          onLogout={() => {}}
          onChangePassword={() => {}}
        />
      </MemoryRouter>,
    )
    const links = screen.getAllByRole('link')
    const linkText = links.map((link) => link.textContent)
    expect(linkText).toEqual(['工作台', '运维记录', 'VPS', '归档', '服务商', '订阅', '资产决策', '监控3', '入口探测1', '事件', '命令审计', '设置'])
    expect(screen.getByRole('link', { name: '运维记录' })).toHaveAttribute('href', '/records')
    expect(screen.getByRole('link', { name: '监控，3 个异常' })).toHaveAttribute('href', '/monitoring')
    expect(screen.getByRole('link', { name: '入口探测，1 个异常' })).toHaveAttribute('href', '/targets')
    expect(screen.getByRole('link', { name: '命令审计' })).toHaveAttribute('href', '/command-audit')
    expect(screen.getByText('3')).toHaveClass('nav-badge')
    expect(screen.getByText('1')).toHaveClass('nav-badge')
  })

  it('omits count badges when zero', () => {
    const { container } = render(
      <MemoryRouter>
        <Sidebar
          user={user}
          anomalyCounts={{ monitoring: 0, targets: 0 }}
          collapsed={false}
          onToggle={() => {}}
          onLogout={() => {}}
          onChangePassword={() => {}}
        />
      </MemoryRouter>,
    )
    expect(container.querySelectorAll('.nav-badge')).toHaveLength(0)
  })

  it('uses one native UserChip menu button instead of an inline clickable container', () => {
    const { container } = render(
      <MemoryRouter>
        <Sidebar
          user={user}
          anomalyCounts={{ monitoring: 0, targets: 0 }}
          collapsed={false}
          onToggle={() => {}}
          onLogout={() => {}}
          onChangePassword={() => {}}
        />
      </MemoryRouter>,
    )

    expect(container.querySelectorAll('.user-chip')).toHaveLength(1)
    expect(screen.getByRole('button', { name: 'admin 用户菜单' })).toBeInTheDocument()
  })
})
