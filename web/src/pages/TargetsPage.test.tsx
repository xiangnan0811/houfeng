import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { TargetsPage } from './TargetsPage'

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
    text: async () => JSON.stringify(body),
  } as Response
}

describe('TargetsPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders runtime quick actions by target run status and restores archived targets to paused', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse([
          {
            target_id: 'tg_enabled',
            name: 'Blog',
            target_type: 'service',
            host: 'blog.example.com',
            base_port: 443,
            execution_node_labels: ['edge'],
            run_status: '启用',
            labels: ['public'],
            note: '',
            current_health_status: '正常',
            current_active_incident_count: 0,
            last_success_at: '2026-04-26T09:00:00Z',
            last_failure_at: '2026-04-26T08:00:00Z',
            current_primary_issue_summary: '',
            created_at: '2026-04-20T00:00:00Z',
            updated_at: '2026-04-26T09:05:00Z',
          },
          {
            target_id: 'tg_archived',
            name: 'Legacy API',
            target_type: 'service',
            host: 'legacy.example.com',
            execution_node_labels: ['edge'],
            run_status: '已归档',
            labels: [],
            note: '',
            current_health_status: '正常',
            current_active_incident_count: 0,
            last_success_at: '2026-04-26T09:00:00Z',
            last_failure_at: '2026-04-26T08:00:00Z',
            current_primary_issue_summary: '',
            created_at: '2026-04-20T00:00:00Z',
            updated_at: '2026-04-26T09:05:00Z',
          },
        ]),
      )
      .mockResolvedValueOnce(
        mockJSONResponse({
          target_id: 'tg_archived',
          name: 'Legacy API',
          target_type: 'service',
          host: 'legacy.example.com',
          execution_node_labels: ['edge'],
          run_status: '暂停',
          labels: [],
          note: '',
          current_health_status: '正常',
          current_active_incident_count: 0,
          last_success_at: '2026-04-26T09:00:00Z',
          last_failure_at: '2026-04-26T08:00:00Z',
          current_primary_issue_summary: '',
          created_at: '2026-04-20T00:00:00Z',
          updated_at: '2026-04-26T09:10:00Z',
        }),
      )
    vi.stubGlobal('fetch', fetchMock)

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    const enabledRow = screen.getByText('Blog').closest('article')
    const archivedRow = screen.getByText('Legacy API').closest('article')
    expect(enabledRow).not.toBeNull()
    expect(archivedRow).not.toBeNull()

    expect(within(enabledRow!).getByRole('button', { name: '进入维护' })).toBeInTheDocument()
    expect(within(enabledRow!).getByRole('button', { name: '暂停' })).toBeInTheDocument()
    expect(within(enabledRow!).getByRole('button', { name: '归档' })).toBeInTheDocument()
    expect(within(archivedRow!).queryByRole('button', { name: '归档' })).not.toBeInTheDocument()
    expect(within(archivedRow!).getByRole('button', { name: '恢复到暂停' })).toBeInTheDocument()

    fireEvent.click(within(archivedRow!).getByRole('button', { name: '恢复到暂停' }))

    await waitFor(() =>
      expect(within(archivedRow!).getByRole('button', { name: '恢复' })).toBeInTheDocument(),
    )
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/targets/tg_archived/runtime/restore-to-paused', {
      method: 'POST',
      headers: { Accept: 'application/json' },
      cache: 'no-store',
    })
  })

  it('requires strong confirmation before archiving and keeps runtime errors local', async () => {
    const confirmMock = vi.spyOn(window, 'confirm').mockReturnValue(true)
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValueOnce(
          mockJSONResponse([
            {
              target_id: 'tg_001',
              name: 'Blog',
              target_type: 'service',
              host: 'blog.example.com',
              base_port: 443,
              execution_node_labels: ['edge'],
              run_status: '启用',
              labels: ['public'],
              note: '',
              current_health_status: '正常',
              current_active_incident_count: 0,
              last_success_at: '2026-04-26T09:00:00Z',
              last_failure_at: '2026-04-26T08:00:00Z',
              current_primary_issue_summary: '',
              created_at: '2026-04-20T00:00:00Z',
              updated_at: '2026-04-26T09:05:00Z',
            },
          ]),
        )
        .mockResolvedValueOnce(mockJSONResponse({ error: 'invalid runtime transition' }, 409)),
    )

    render(
      <MemoryRouter initialEntries={['/targets']}>
        <Routes>
          <Route path="/targets" element={<TargetsPage />} />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() => expect(screen.getByText('Blog')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: '归档' }))

    expect(confirmMock).toHaveBeenCalledWith('归档会让目标退出当前工作集，但会保留历史记录，确定继续吗？')
    await waitFor(() =>
      expect(screen.getByText('invalid runtime transition')).toBeInTheDocument(),
    )
    expect(screen.getByRole('heading', { name: '目标列表' })).toBeInTheDocument()
  })
})
