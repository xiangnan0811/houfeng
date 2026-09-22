import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { TargetDetailPage } from './TargetDetailPage'

vi.mock('../lib/readOnlyPreview', () => ({
  READ_ONLY_PREVIEW: true,
}))

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
    text: async () => JSON.stringify(body),
  } as Response
}

describe('TargetDetailPage read-only preview', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('hides write controls and shows the read-only mark', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn()
        .mockResolvedValueOnce(
          mockJSONResponse({
            target_id: 'tg_001',
            name: 'Blog',
            target_type: 'service',
            host: 'blog.example.com',
            base_port: 443,
            execution_monitoring_instance_labels: ['edge'],
            run_status: '启用',
            labels: [],
            note: '',
            current_health_status: '正常',
            current_active_incident_count: 0,
            current_primary_issue_summary: '',
            created_at: '2026-04-20T00:00:00Z',
            updated_at: '2026-04-24T09:05:00Z',
          }),
        )
        .mockResolvedValueOnce(
          mockJSONResponse([
            {
              probe_item_id: 'pb_001',
              target_id: 'tg_001',
              probe_kind: 'http',
              enabled: true,
              frequency_tier: '1m',
              timeout_seconds: 5,
              config: { path: '/healthz' },
              created_at: '2026-04-20T00:00:00Z',
              updated_at: '2026-04-24T09:05:00Z',
            },
          ]),
        )
        .mockResolvedValueOnce(mockJSONResponse({ target_id: 'tg_001', latest_probe_observations: [] }))
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(mockJSONResponse([])),
    )

    render(
      <MemoryRouter initialEntries={['/targets/tg_001']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Blog' })).toBeInTheDocument(),
    )

    expect(screen.getByText('只读预览')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: '查看历史' }).length).toBeGreaterThan(0)
    expect(screen.queryByRole('button', { name: '资料维护' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '进入维护' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '添加 ProbeItem' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /编辑 ProbeItem/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /删除 ProbeItem/ })).not.toBeInTheDocument()
  })
})
