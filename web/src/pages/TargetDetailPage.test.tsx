import { render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { TargetDetailPage } from './TargetDetailPage'

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
    text: async () => JSON.stringify(body),
  } as Response
}

describe('TargetDetailPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders probe items with latest runtime observations', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse({
            target_id: 'tg_001',
            name: 'Blog',
            target_type: 'service',
            host: 'blog.example.com',
            base_port: 443,
            execution_node_labels: ['edge'],
            run_status: '启用',
            labels: ['公开'],
            note: '',
            current_health_status: '正常',
            current_active_incident_count: 0,
            last_success_at: '2026-04-24T09:00:00Z',
            last_failure_at: '2026-04-24T08:30:00Z',
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
              config: { path: '/healthz', method: 'GET' },
              created_at: '2026-04-20T00:00:00Z',
              updated_at: '2026-04-24T09:05:00Z',
            },
          ]),
        )
        .mockResolvedValueOnce(
          mockJSONResponse({
            target_id: 'tg_001',
            latest_probe_observations: [
              {
                node_id: 'nd_001',
                target_id: 'tg_001',
                probe_item_id: 'pb_001',
                probe_kind: 'http',
                observed_at: '2026-04-24T09:05:00Z',
                received_at: '2026-04-24T09:05:01Z',
                agent_version: 'dev',
                fingerprint: 'fp-001',
                result_kind: 'success',
                latency_ms: 83,
                http_status: 200,
                tls_expiry_days: null,
                maintenance_context: false,
                is_backfilled: false,
                sync_batch_id: 'sync-001',
              },
            ],
          }),
        ),
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
    expect(screen.getByText('ProbeItem 列表')).toBeInTheDocument()
    expect(screen.getByText('HTTP')).toBeInTheDocument()
    expect(screen.getByText('83 ms')).toBeInTheDocument()
    expect(screen.getByText('200')).toBeInTheDocument()
    expect(screen.getByText('nd_001')).toBeInTheDocument()
  })

  it('renders the frozen empty state when the target has no probe items', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse({
            target_id: 'tg_002',
            name: 'Cache',
            target_type: 'service',
            host: 'cache.example.com',
            execution_node_labels: ['edge'],
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
        .mockResolvedValueOnce(mockJSONResponse([]))
        .mockResolvedValueOnce(
          mockJSONResponse({
            target_id: 'tg_002',
            latest_probe_observations: [],
          }),
        ),
    )

    render(
      <MemoryRouter initialEntries={['/targets/tg_002']}>
        <Routes>
          <Route path="/targets/:targetId" element={<TargetDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByText('当前还没有 ProbeItem')).toBeInTheDocument(),
    )
    expect(
      screen.getByText('当前还没有 ProbeItem，请为该入口添加至少一种观测方式。'),
    ).toBeInTheDocument()
  })
})
