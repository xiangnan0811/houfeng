import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
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

    expect(screen.getAllByText('筛选条件').length).toBeGreaterThanOrEqual(2)
    expect(screen.getAllByText('事件流').length).toBeGreaterThanOrEqual(2)

    expect(screen.getAllByText('事件').length).toBeGreaterThanOrEqual(1)
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

  it('offers binding audit event filters for node onboarding actions', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(<EventsPage />)

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '事件' })).toBeInTheDocument(),
    )

    fireEvent.change(screen.getByLabelText('事件类型'), {
      target: { value: 'node_binding_reset' },
    })
    fireEvent.click(screen.getByRole('button', { name: '应用筛选' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenLastCalledWith('/api/events?event_type=node_binding_reset&limit=50', {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
      }),
    )
  })

  it('offers runtime-control event filters and submits them', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(<EventsPage />)

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '事件' })).toBeInTheDocument(),
    )

    const eventTypeSelect = screen.getByLabelText('事件类型')

    expect(
      within(eventTypeSelect).getByRole('option', { name: '节点进入维护' }),
    ).toBeInTheDocument()
    expect(
      within(eventTypeSelect).getByRole('option', { name: '节点退出维护' }),
    ).toBeInTheDocument()
    expect(
      within(eventTypeSelect).getByRole('option', { name: '节点暂停监控' }),
    ).toBeInTheDocument()
    expect(
      within(eventTypeSelect).getByRole('option', { name: '节点恢复监控' }),
    ).toBeInTheDocument()
    expect(
      within(eventTypeSelect).getByRole('option', { name: '目标进入维护' }),
    ).toBeInTheDocument()
    expect(
      within(eventTypeSelect).getByRole('option', { name: '目标退出维护' }),
    ).toBeInTheDocument()
    expect(
      within(eventTypeSelect).getByRole('option', { name: '目标已暂停' }),
    ).toBeInTheDocument()
    expect(
      within(eventTypeSelect).getByRole('option', { name: '目标已恢复' }),
    ).toBeInTheDocument()
    expect(
      within(eventTypeSelect).getByRole('option', { name: '目标已归档' }),
    ).toBeInTheDocument()
    expect(
      within(eventTypeSelect).getByRole('option', { name: '目标已恢复为暂停' }),
    ).toBeInTheDocument()

    fireEvent.change(eventTypeSelect, {
      target: { value: 'target_restored_to_paused' },
    })
    fireEvent.click(screen.getByRole('button', { name: '应用筛选' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenLastCalledWith(
        '/api/events?event_type=target_restored_to_paused&limit=50',
        {
          headers: { Accept: 'application/json' },
          cache: 'no-store',
        },
      ),
    )
  })

  it('offers node lifecycle event filters and submits them', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(<EventsPage />)

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '事件' })).toBeInTheDocument(),
    )

    const eventTypeSelect = screen.getByLabelText('事件类型')

    expect(
      within(eventTypeSelect).getByRole('option', { name: '节点已退役' }),
    ).toBeInTheDocument()
    expect(
      within(eventTypeSelect).getByRole('option', { name: '节点恢复到观察中' }),
    ).toBeInTheDocument()

    fireEvent.change(eventTypeSelect, {
      target: { value: 'node_restored_to_observing' },
    })
    fireEvent.click(screen.getByRole('button', { name: '应用筛选' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenLastCalledWith(
        '/api/events?event_type=node_restored_to_observing&limit=50',
        {
          headers: { Accept: 'application/json' },
          cache: 'no-store',
        },
      ),
    )
  })

  it('submits advanced context filters and can reset to the default event stream', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    render(<EventsPage />)

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '事件' })).toBeInTheDocument(),
    )

    fireEvent.change(screen.getByLabelText('对象类型'), { target: { value: 'node' } })
    fireEvent.change(screen.getByLabelText('数量'), { target: { value: '25' } })
    fireEvent.change(screen.getByLabelText('开始时间'), {
      target: { value: '2026-04-25T00:00:00Z' },
    })
    fireEvent.change(screen.getByLabelText('结束时间'), {
      target: { value: '2026-04-26T00:00:00Z' },
    })
    fireEvent.change(screen.getByLabelText('标签'), { target: { value: 'edge' } })
    fireEvent.click(screen.getByLabelText('仅看通知事件'))
    fireEvent.click(screen.getByLabelText('仅看恢复事件'))
    fireEvent.click(screen.getByLabelText('仅看维护事件'))
    fireEvent.click(screen.getByRole('button', { name: '应用筛选' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenLastCalledWith(
        '/api/events?object_type=node&limit=25&created_from=2026-04-25T00%3A00%3A00Z&created_to=2026-04-26T00%3A00%3A00Z&label=edge&notification_only=true&recovery_only=true&maintenance_only=true',
        {
          headers: { Accept: 'application/json' },
          cache: 'no-store',
        },
      ),
    )

    fireEvent.click(screen.getByRole('button', { name: '重置筛选' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenLastCalledWith('/api/events?limit=50', {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
      }),
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
