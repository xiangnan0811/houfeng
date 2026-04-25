import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { EventsPage } from './EventsPage'

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
    text: async () => JSON.stringify(body),
  } as Response
}

describe('EventsPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders loading, fetched events, and applies lightweight filters', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockJSONResponse([
          {
            event_id: 'evt_002',
            incident_id: 'inc_002',
            incident_class: 'target_probe_failure',
            object_type: 'target',
            object_id: 'tg_001',
            event_type: 'incident_escalated',
            severity: '严重',
            summary: '较新的事件',
            created_at: '2026-04-25T08:10:00Z',
          },
          {
            event_id: 'evt_001',
            incident_id: 'inc_001',
            incident_class: 'target_probe_failure',
            object_type: 'target',
            object_id: 'tg_001',
            event_type: 'incident_started',
            severity: '告警',
            summary: '较早的事件',
            created_at: '2026-04-25T08:00:00Z',
          },
        ]),
      )
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(<EventsPage />)

    expect(screen.getByText('正在加载事件…')).toBeInTheDocument()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '事件' })).toBeInTheDocument(),
    )

    expect(screen.getByText('较新的事件')).toBeInTheDocument()
    expect(screen.getByText('较早的事件')).toBeInTheDocument()
    expect(screen.getByText('较新的事件').compareDocumentPosition(screen.getByText('较早的事件')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()

    fireEvent.change(screen.getByLabelText('对象类型'), { target: { value: 'target' } })
    fireEvent.change(screen.getByLabelText('严重程度'), { target: { value: '严重' } })
    fireEvent.change(screen.getByLabelText('事件类型'), { target: { value: 'incident_started' } })
    fireEvent.change(screen.getByLabelText('数量'), { target: { value: '10' } })
    fireEvent.click(screen.getByRole('button', { name: '应用筛选' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenLastCalledWith(
        '/api/events?object_type=target&severity=%E4%B8%A5%E9%87%8D&event_type=incident_started&limit=10',
        {
          headers: { Accept: 'application/json' },
          cache: 'no-store',
        },
      ),
    )
  })

  it('renders an explicit empty state when no events exist', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockJSONResponse([])))

    render(<EventsPage />)

    await waitFor(() =>
      expect(screen.getByText('最近没有状态变更事件')).toBeInTheDocument(),
    )
  })

  it('renders an explicit error state when the events request fails', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(mockJSONResponse({ error: 'events unavailable' }, 500)),
    )

    render(<EventsPage />)

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '事件不可用' })).toBeInTheDocument(),
    )
    expect(screen.getByText('events unavailable')).toBeInTheDocument()
  })
})
