import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { Sidebar } from './Sidebar'

const user = { user_id: 'u1', username: 'admin', role: 'admin', display_name: '' }
const sync = { state: 'ok' as const, label: '摘要已加载', meta: 'v1.0 · dashboard 14:32:01' }

describe('Sidebar', () => {
  it('renders brand and grouped asset-aware nav items', () => {
    const { container } = render(
      <MemoryRouter>
        <Sidebar
          user={user}
          sync={sync}
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
    for (const label of ['VPS', '服务商', '订阅', '资产决策', '监控', '入口探测', '事件', '设置']) {
      expect(screen.getByRole('link', { name: label })).toBeInTheDocument()
    }
    expect(screen.queryByRole('link', { name: '首页' })).not.toBeInTheDocument()
  })

  it('renders anomaly counts only on monitoring/targets nav', () => {
    render(
      <MemoryRouter>
        <Sidebar
          user={user}
          sync={sync}
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
    expect(linkText).toEqual(['工作台', 'VPS', '服务商', '订阅', '资产决策', '监控3', '入口探测1', '事件', '设置'])
    expect(screen.getByText('3')).toHaveClass('nav-badge')
    expect(screen.getByText('1')).toHaveClass('nav-badge')
  })

  it('omits count badges when zero', () => {
    const { container } = render(
      <MemoryRouter>
        <Sidebar
          user={user}
          sync={sync}
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
})
