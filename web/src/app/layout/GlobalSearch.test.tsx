import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi, beforeEach } from 'vitest'

import { GlobalSearch } from './GlobalSearch'
import * as api from '../../lib/api'

const mockNodes = [
  {
    node_id: 'nd_001',
    display_name: 'Tokyo Edge',
    region: 'ap-northeast-1',
    city: 'Tokyo',
    provider: 'aws',
    lifecycle_status: '在用',
    monitoring_status: '启用',
    binding_status: '已绑定',
    group: 'edge-group',
    labels: ['edge'],
    note: '',
    current_health_status: '正常',
    last_heartbeat_at: '2026-04-30T08:00:00Z',
    last_sync_at: '2026-04-30T08:00:00Z',
    current_active_incident_count: 0,
    current_primary_issue_summary: '',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-30T08:00:00Z',
  },
] as Awaited<ReturnType<typeof api.listNodes>>

const mockTargets = [
  {
    target_id: 'tg_001',
    name: 'Blog',
    target_type: 'service' as const,
    host: 'blog.example.com',
    base_port: 443,
    execution_node_labels: ['edge'],
    run_status: '启用',
    group: 'prod-group',
    labels: [],
    note: '',
    current_health_status: '正常',
    current_active_incident_count: 0,
    last_success_at: undefined,
    last_failure_at: undefined,
    current_primary_issue_summary: '',
    created_at: '2026-04-20T00:00:00Z',
    updated_at: '2026-04-30T08:00:00Z',
  },
] as Awaited<ReturnType<typeof api.listTargets>>

describe('GlobalSearch', () => {
  beforeEach(() => {
    vi.spyOn(api, 'listNodes').mockResolvedValue(mockNodes)
    vi.spyOn(api, 'listTargets').mockResolvedValue(mockTargets)
  })

  it('renders the search input', () => {
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    expect(screen.getByLabelText('全局搜索')).toBeInTheDocument()
  })

  it('matches a node by display_name and shows it in the dropdown', async () => {
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    const input = screen.getByLabelText('全局搜索')
    fireEvent.change(input, { target: { value: 'tokyo' } })
    fireEvent.submit(input.closest('form')!)

    await waitFor(() => {
      expect(screen.getByText('Tokyo Edge')).toBeInTheDocument()
    })
    const option = screen.getByRole('option', { name: /Tokyo Edge/ })
    expect(option).toBeInTheDocument()
  })

  it('matches a target by host', async () => {
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    const input = screen.getByLabelText('全局搜索')
    fireEvent.change(input, { target: { value: 'blog.example' } })
    fireEvent.submit(input.closest('form')!)

    await waitFor(() => {
      expect(screen.getByText('Blog')).toBeInTheDocument()
    })
  })

  it('shows "没有匹配项" when nothing matches', async () => {
    render(
      <MemoryRouter>
        <GlobalSearch />
      </MemoryRouter>,
    )
    const input = screen.getByLabelText('全局搜索')
    fireEvent.change(input, { target: { value: 'zzznever' } })
    fireEvent.submit(input.closest('form')!)

    await waitFor(() => {
      expect(screen.getByText('没有匹配项')).toBeInTheDocument()
    })
  })
})
