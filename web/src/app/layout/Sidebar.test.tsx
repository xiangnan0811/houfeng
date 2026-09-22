import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { Sidebar } from './Sidebar'

const user = { user_id: 'u1', username: 'admin', role: 'admin', display_name: '' }

describe('Sidebar', () => {
  it('renders brand and a quiet primary rail with overflow links', () => {
    render(
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
    expect(screen.queryByText('运营')).not.toBeInTheDocument()
    expect(screen.queryByText('资产')).not.toBeInTheDocument()
    expect(screen.queryByText('观测')).not.toBeInTheDocument()
    expect(screen.queryByText('系统')).not.toBeInTheDocument()
    for (const label of ['工作台', 'VPS', '监控', '入口探测', '设置']) {
      expect(screen.getByRole('link', { name: label })).toBeInTheDocument()
    }
    for (const label of ['运维记录', '归档', '服务商', '订阅', '资产决策', '事件', '命令审计']) {
      expect(screen.getByRole('link', { name: label })).toBeInTheDocument()
    }
    expect(screen.getByText('更多')).toBeInTheDocument()
    expect(screen.getByText('更多').closest('summary')).toHaveAttribute('aria-label', '更多')
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
    expect(linkText).toEqual(['工作台', 'VPS', '监控3', '入口探测1', '资产决策', '订阅', '服务商', '归档', '运维记录', '事件', '命令审计', '设置'])
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

  it('provides an accessible name on overflow summary for screen readers and preserves disclosure', () => {
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
    const summary = container.querySelector('summary.sidebar-more__summary')
    expect(summary).toBeInTheDocument()
    expect(summary).toHaveAccessibleName('更多')
    const details = container.querySelector('details.sidebar-more') as HTMLDetailsElement
    summary!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    expect(details.open).toBe(true)
    summary!.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    expect(details.open).toBe(false)
  })

  it('auto-expands overflow details when on an overflow route', () => {
    const { container } = render(
      <MemoryRouter initialEntries={['/records']}>
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
    const details = container.querySelector('details.sidebar-more') as HTMLDetailsElement
    expect(details).toBeInTheDocument()
    expect(details.open).toBe(true)
  })
})
