import { render, screen, within } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { Sidebar } from './Sidebar'

const user = {
  user_id: 'u1',
  username: 'admin',
  role: 'admin',
  display_name: '',
  runtime_capabilities: { records: true, comparison: true, portability: true },
  management_capabilities: { access: false },
}
const recordsOffUser = {
  ...user,
  runtime_capabilities: { records: false, comparison: false, portability: false },
  management_capabilities: { access: false },
}

describe('Sidebar', () => {
  it('renders brand and every destination in always-open named groups', () => {
    const { container } = render(
      <MemoryRouter>
        <Sidebar
          user={user}
          anomalyCounts={{ monitoring: 0, targets: 0, unobservedTargets: 0 }}
          collapsed={false}
          onToggle={() => {}}
          onLogout={() => {}}
          onChangePassword={() => {}}
        />
      </MemoryRouter>,
    )
    expect(screen.getByText('候风')).toBeInTheDocument()
    expect(screen.getByRole('navigation', { name: '主导航' })).toBeInTheDocument()
    const groups = {
      资产: ['VPS', '订阅', '服务商', '资产决策', '归档'],
      观测: ['监控', '入口探测', '事件'],
      记录: ['运维记录', '命令审计'],
    }
    for (const [groupName, labels] of Object.entries(groups)) {
      const group = screen.getByRole('group', { name: groupName })
      expect(within(group).getAllByRole('link').map((link) => link.getAttribute('aria-label'))).toEqual(labels)
    }
    for (const label of ['工作台', '设置']) {
      expect(screen.getByRole('link', { name: label })).toBeInTheDocument()
    }
    expect(container.querySelector('details, summary')).toBeNull()
    expect(screen.queryByText('更多')).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'VPS' })).toHaveAttribute('aria-label', 'VPS')
    expect(screen.queryByRole('link', { name: '首页' })).not.toBeInTheDocument()
  })

  it('renders anomaly counts only on monitoring/targets nav', () => {
    render(
      <MemoryRouter>
        <Sidebar
          user={user}
          anomalyCounts={{ monitoring: 3, targets: 1, unobservedTargets: 0 }}
          collapsed={false}
          onToggle={() => {}}
          onLogout={() => {}}
          onChangePassword={() => {}}
        />
      </MemoryRouter>,
    )
    const links = screen.getAllByRole('link')
    const linkText = links.map((link) => link.textContent)
    expect(linkText).toEqual(['工作台', 'VPS', '订阅', '服务商', '资产决策', '归档', '监控3', '入口探测1', '事件', '运维记录', '命令审计', '设置'])
    expect(screen.getByRole('link', { name: '运维记录' })).toHaveAttribute('href', '/records')
    expect(screen.getByRole('link', { name: '监控，3 个异常' })).toHaveAttribute('href', '/monitoring')
    expect(screen.getByRole('link', { name: '入口探测，1 个异常' })).toHaveAttribute('href', '/targets')
    expect(screen.getByRole('link', { name: '命令审计' })).toHaveAttribute('href', '/command-audit')
    expect(screen.getByText('3')).toHaveClass('nav-badge')
    expect(screen.getByText('1')).toHaveClass('nav-badge')
  })

  it('links unobserved targets separately from the abnormal target badge', () => {
    render(
      <MemoryRouter>
        <Sidebar
          user={user}
          anomalyCounts={{ monitoring: 0, targets: 2, unobservedTargets: 4 }}
          collapsed={false}
          onToggle={() => {}}
          onLogout={() => {}}
          onChangePassword={() => {}}
        />
      </MemoryRouter>,
    )
    expect(screen.getByRole('link', { name: '入口探测，2 个异常' })).toHaveAttribute('href', '/targets')
    expect(screen.getByRole('link', { name: '尚无观测，4 个尚无观测' })).toHaveAttribute('href', '/targets?view=unobserved')
    expect(screen.getByRole('link', { name: '入口探测，2 个异常' })).not.toHaveTextContent('4')
  })

  it('omits count badges when zero', () => {
    const { container } = render(
      <MemoryRouter>
        <Sidebar
          user={user}
          anomalyCounts={{ monitoring: 0, targets: 0, unobservedTargets: 0 }}
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
          anomalyCounts={{ monitoring: 0, targets: 0, unobservedTargets: 0 }}
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

  it('marks the current destination active inside its group without extra disclosure', () => {
    render(
      <MemoryRouter initialEntries={['/records']}>
        <Sidebar
          user={user}
          anomalyCounts={{ monitoring: 0, targets: 0, unobservedTargets: 0 }}
          collapsed={false}
          onToggle={() => {}}
          onLogout={() => {}}
          onChangePassword={() => {}}
        />
      </MemoryRouter>,
    )
    const records = within(screen.getByRole('group', { name: '记录' })).getByRole('link', { name: '运维记录' })
    expect(records).toHaveClass('active')
    expect(records).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('link', { name: '工作台' })).not.toHaveClass('active')
  })

  it('hides only the records destination when the records platform is off', () => {
    render(
      <MemoryRouter>
        <Sidebar
          user={recordsOffUser}
          anomalyCounts={{ monitoring: 0, targets: 0, unobservedTargets: 0 }}
          collapsed={false}
          onToggle={() => {}}
          onLogout={() => {}}
          onChangePassword={() => {}}
        />
      </MemoryRouter>,
    )
    const group = screen.getByRole('group', { name: '记录' })
    expect(within(group).queryByRole('link', { name: '运维记录' })).not.toBeInTheDocument()
    expect(within(group).getByRole('link', { name: '命令审计' })).toHaveAttribute('href', '/command-audit')
    expect(screen.getByRole('link', { name: '资产决策' })).toHaveAttribute('href', '/asset-decisions')
    expect(screen.getByRole('link', { name: 'VPS' })).toBeInTheDocument()
  })
})
