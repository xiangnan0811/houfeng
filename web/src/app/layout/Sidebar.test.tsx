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
          anomalyCounts={{ nodes: 0, targets: 0 }}
          onLogout={() => {}}
          onChangePassword={() => {}}
        />
      </MemoryRouter>,
    )
    expect(screen.getByText('候风')).toBeInTheDocument()
    for (const label of ['总览', '资产', '观测', '系统']) {
      expect(
        Array.from(container.querySelectorAll('.sidebar__nav-group-title')).some(
          (title) => title.textContent === label,
        ),
      ).toBe(true)
    }
    for (const label of ['VPS', '服务商', '订阅', '资产决策', '节点', '目标', '事件', '设置']) {
      expect(screen.getByRole('link', { name: label })).toBeInTheDocument()
    }
    expect(screen.queryByRole('link', { name: '首页' })).not.toBeInTheDocument()
  })

  it('renders anomaly counts only on nodes/targets nav', () => {
    render(
      <MemoryRouter>
        <Sidebar
          user={user}
          sync={sync}
          anomalyCounts={{ nodes: 3, targets: 1 }}
          onLogout={() => {}}
          onChangePassword={() => {}}
        />
      </MemoryRouter>,
    )
    const links = screen.getAllByRole('link')
    const linkText = links.map((link) => link.textContent)
    expect(linkText).toEqual(['工作台', '资产决策', 'VPS', '服务商', '订阅', '节点3', '目标1', '事件', '设置'])
    expect(screen.getByText('3')).toHaveClass('badge--count')
    expect(screen.getByText('3')).not.toHaveClass('tone--alert', 'tone--critical')
    expect(screen.getByText('1')).toHaveClass('badge--count')
    expect(screen.getByText('1')).not.toHaveClass('tone--alert', 'tone--critical')
  })

  it('omits count badges when zero', () => {
    const { container } = render(
      <MemoryRouter>
        <Sidebar
          user={user}
          sync={sync}
          anomalyCounts={{ nodes: 0, targets: 0 }}
          onLogout={() => {}}
          onChangePassword={() => {}}
        />
      </MemoryRouter>,
    )
    expect(container.querySelectorAll('.badge--count')).toHaveLength(0)
  })
})
