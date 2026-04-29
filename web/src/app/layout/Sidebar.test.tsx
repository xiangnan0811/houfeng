import { render, screen } from '@testing-library/react'
import { describe, it, expect } from 'vitest'
import { MemoryRouter } from 'react-router-dom'
import { Sidebar } from './Sidebar'

const user = { user_id: 'u1', username: 'admin', role: 'admin', display_name: '' }
const sync = { state: 'ok' as const, version: 'v1.0', lastSync: '2026-04-29T14:32:01Z' }

describe('Sidebar', () => {
  it('renders brand and 5 nav items', () => {
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
    expect(screen.getByText('候风')).toBeInTheDocument()
    for (const label of ['首页', '节点', '目标', '事件', '设置']) {
      expect(screen.getByText(label)).toBeInTheDocument()
    }
  })

  it('renders anomaly counts on nodes/targets nav', () => {
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
    expect(screen.getByText('3')).toBeInTheDocument()
    expect(screen.getByText('1')).toBeInTheDocument()
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
