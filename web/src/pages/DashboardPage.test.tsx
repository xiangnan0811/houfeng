import { render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { DashboardPage } from './DashboardPage'

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
    text: async () => JSON.stringify(body),
  } as Response
}

describe('DashboardPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders loading then overview counts and recent events from /api/dashboard', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockJSONResponse({
          abnormal_node_count: 2,
          abnormal_target_count: 3,
          severe_node_count: 1,
          severe_target_count: 2,
          maintenance_node_count: 1,
          maintenance_target_count: 0,
          recent_new_incident_count: 4,
          recent_recovery_count: 1,
          recent_events: [
            {
              event_id: 'evt_001',
              incident_id: 'inc_001',
              incident_class: 'target_probe_failure',
              object_type: 'target',
              object_id: 'tg_001',
              event_type: 'incident_started',
              severity: '严重',
              summary: 'HTTPS 探测连续失败',
              created_at: '2026-04-25T08:10:00Z',
            },
          ],
        }),
      ),
    )

    render(<DashboardPage />)

    expect(screen.getByText('正在加载集群概览…')).toBeInTheDocument()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '集群概览' })).toBeInTheDocument(),
    )

    expect(screen.getByText('异常节点')).toBeInTheDocument()
    expect(screen.getByText('异常目标')).toBeInTheDocument()
    expect(screen.getByText('新增异常')).toBeInTheDocument()
    expect(screen.getByText('恢复事件')).toBeInTheDocument()
    expect(screen.getByText('异常节点概览')).toBeInTheDocument()
    expect(screen.getByText('异常目标概览')).toBeInTheDocument()
    expect(screen.getByText('最近事件')).toBeInTheDocument()
    expect(screen.getByText('HTTPS 探测连续失败')).toBeInTheDocument()
    expect(screen.getByText('tg_001')).toBeInTheDocument()
  })

  it('renders an explicit error state when the dashboard request fails', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(mockJSONResponse({ error: 'dashboard unavailable' }, 503)),
    )

    render(<DashboardPage />)

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '集群概览不可用' })).toBeInTheDocument(),
    )
    expect(screen.getByText('dashboard unavailable')).toBeInTheDocument()
  })
})
