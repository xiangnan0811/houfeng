import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, useLocation } from 'react-router-dom'
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

function renderEventsPage(initialEntry = '/events') {
  const locationSnapshots: string[] = []

  function LocationCapture() {
    const location = useLocation()
    locationSnapshots.push(`${location.pathname}${location.search}`)
    return null
  }

  render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <LocationCapture />
      <EventsPage />
    </MemoryRouter>,
  )

  return {
    getCurrentLocation: () => locationSnapshots.at(-1) ?? initialEntry,
  }
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

    renderEventsPage()

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
        credentials: 'include',
        },
      ),
    )
  })

  it('uses valid URL filters for the initial events request', async () => {
    const fetchMock = vi.fn().mockResolvedValue(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    renderEventsPage(
      '/events?object_type=node&severity=%E4%B8%A5%E9%87%8D&event_type=incident_started&limit=25&created_from=2026-04-25T00:00:00Z&created_to=2026-04-26T00:00:00Z&label=edge&notification_only=1&recovery_only=1&maintenance_only=1&time_range=custom&include_backfilled=1',
    )

    await waitFor(() =>
      expect(fetchMock).toHaveBeenLastCalledWith(
        '/api/events?object_type=node&severity=%E4%B8%A5%E9%87%8D&event_type=incident_started&limit=25&created_from=2026-04-25T00%3A00%3A00Z&created_to=2026-04-26T00%3A00%3A00Z&label=edge&notification_only=true&recovery_only=true&maintenance_only=true',
        {
          headers: { Accept: 'application/json' },
          cache: 'no-store',
          credentials: 'include',
        },
      ),
    )
    expect(screen.getByText('对象类型: 节点')).toBeInTheDocument()
    expect(screen.getByText('严重程度: 严重')).toBeInTheDocument()
    expect(screen.getByText('事件类型: 异常开始')).toBeInTheDocument()
    expect(screen.getByText('标签: edge')).toBeInTheDocument()
  })

  it('offers binding audit event filters for node onboarding actions', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    renderEventsPage()

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
        credentials: 'include',
      }),
    )
  })

  it('offers runtime-control event filters and submits them', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    renderEventsPage()

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
        credentials: 'include',
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

    renderEventsPage()

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
        credentials: 'include',
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

    const page = renderEventsPage()

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
        credentials: 'include',
        },
      ),
    )
    await waitFor(() =>
      expect(page.getCurrentLocation()).toBe(
        '/events?object_type=node&limit=25&time_range=custom&created_from=2026-04-25T00%3A00%3A00Z&created_to=2026-04-26T00%3A00%3A00Z&label=edge&notification_only=1&recovery_only=1&maintenance_only=1',
      ),
    )

    fireEvent.click(screen.getByRole('button', { name: '重置筛选' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenLastCalledWith('/api/events?limit=50', {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      }),
    )
    await waitFor(() => expect(page.getCurrentLocation()).toBe('/events'))
  })

  it('marks the backfilled event toggle as pending backend support', async () => {
    const fetchMock = vi.fn().mockResolvedValue(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    renderEventsPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '事件' })).toBeInTheDocument(),
    )

    const backfilledToggle = screen.getByLabelText('包含补传事件')
    expect(backfilledToggle).toBeDisabled()
    expect(screen.getByText('待后端支持')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: '应用筛选' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenLastCalledWith('/api/events?limit=50', {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      }),
    )
  })

  it('removes active chips from URL state and refetches', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    const page = renderEventsPage('/events?severity=%E4%B8%A5%E9%87%8D&label=edge')

    await waitFor(() =>
      expect(fetchMock).toHaveBeenLastCalledWith('/api/events?severity=%E4%B8%A5%E9%87%8D&limit=50&label=edge', {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      }),
    )

    fireEvent.click(screen.getByRole('button', { name: '移除筛选 严重程度: 严重' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenLastCalledWith('/api/events?limit=50&label=edge', {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      }),
    )
    await waitFor(() => expect(page.getCurrentLocation()).toBe('/events?label=edge'))
  })

  it('ignores invalid URL params and excludes unsupported backfilled filters', async () => {
    const fetchMock = vi.fn().mockResolvedValue(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    const page = renderEventsPage(
      '/events?object_type=service&severity=%E6%AD%A3%E5%B8%B8&event_type=unknown&limit=999&created_from=not-a-date&created_to=also-bad&notification_only=true&recovery_only=0&maintenance_only=yes&time_range=invalid&include_backfilled=1',
    )

    await waitFor(() =>
      expect(fetchMock).toHaveBeenLastCalledWith('/api/events?limit=50', {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      }),
    )
    await waitFor(() => expect(page.getCurrentLocation()).toBe('/events'))
  })

  it('renders an explicit empty state when no events exist', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockJSONResponse([])))

    renderEventsPage()

    await waitFor(() =>
      expect(screen.getByText('最近没有状态变更事件')).toBeInTheDocument(),
    )
  })

  it('renders an explicit error state when the events request fails', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(mockJSONResponse({ error: 'events unavailable' }, 500)),
    )

    renderEventsPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '事件不可用' })).toBeInTheDocument(),
    )
    expect(screen.getByText('events unavailable')).toBeInTheDocument()
  })

  it('selects a 7d time range preset and forwards created_from/created_to', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse([]))
      .mockResolvedValueOnce(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    const page = renderEventsPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '事件' })).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('tab', { name: '近 7 天' }))
    fireEvent.click(screen.getByRole('button', { name: '应用筛选' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    const lastCall = fetchMock.mock.calls.at(-1)
    expect(lastCall).toBeDefined()
    const url = new URL(String(lastCall?.[0]), 'http://localhost')
    expect(url.pathname).toBe('/api/events')
    expect(url.searchParams.get('limit')).toBe('50')
    expect(url.searchParams.get('created_from')).toMatch(/^\d{4}-\d{2}-\d{2}T/)
    expect(url.searchParams.get('created_to')).toMatch(/^\d{4}-\d{2}-\d{2}T/)
    const fromMs = Date.parse(url.searchParams.get('created_from') ?? '')
    const toMs = Date.parse(url.searchParams.get('created_to') ?? '')
    expect(toMs - fromMs).toBeGreaterThan(6.5 * 24 * 60 * 60 * 1000)
    expect(toMs - fromMs).toBeLessThan(7.5 * 24 * 60 * 60 * 1000)
    await waitFor(() => expect(page.getCurrentLocation()).toBe('/events?time_range=7d'))

    // Date inputs are disabled while a preset range is selected.
    expect(screen.getByLabelText('开始时间')).toBeDisabled()
    expect(screen.getByLabelText('结束时间')).toBeDisabled()
  })

  it('preserves relative time range in URL while sending dynamic API dates', async () => {
    const fetchMock = vi.fn().mockResolvedValue(mockJSONResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    const page = renderEventsPage('/events?time_range=24h')

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    const url = new URL(String(fetchMock.mock.calls[0]?.[0]), 'http://localhost')
    expect(url.pathname).toBe('/api/events')
    expect(url.searchParams.get('limit')).toBe('50')
    expect(url.searchParams.get('created_from')).toMatch(/^\d{4}-\d{2}-\d{2}T/)
    expect(url.searchParams.get('created_to')).toMatch(/^\d{4}-\d{2}-\d{2}T/)
    expect(url.searchParams.has('time_range')).toBe(false)

    const fromMs = Date.parse(url.searchParams.get('created_from') ?? '')
    const toMs = Date.parse(url.searchParams.get('created_to') ?? '')
    expect(toMs - fromMs).toBeGreaterThan(23.5 * 60 * 60 * 1000)
    expect(toMs - fromMs).toBeLessThan(24.5 * 60 * 60 * 1000)
    expect(page.getCurrentLocation()).toBe('/events?time_range=24h')
  })

  it('renders time-grouped events and a load-more button that refetches with a larger limit', async () => {
    const firstBatch = Array.from({ length: 50 }, (_, i) => ({
      event_id: `evt_${String(i).padStart(3, '0')}`,
      incident_id: `inc_${i}`,
      incident_class: 'target_probe_failure',
      object_type: 'target',
      object_id: 'tg_001',
      event_type: 'incident_started',
      severity: '告警',
      summary: `事件 ${i}`,
      created_at: new Date().toISOString(),
    }))
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockJSONResponse(firstBatch))
      .mockResolvedValueOnce(mockJSONResponse([...firstBatch, ...firstBatch.slice(0, 10)]))
    vi.stubGlobal('fetch', fetchMock)

    renderEventsPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '事件' })).toBeInTheDocument(),
    )

    // Time-group header for "今天" should appear (50 events created now).
    expect(screen.getByRole('heading', { name: '今天' })).toBeInTheDocument()

    // Load-more button should be enabled because returned count == effectiveLimit
    // (backend may have more rows). Click it to refetch with a bigger limit.
    const loadMore = screen.getByRole('button', { name: '加载更早事件 ↓' })
    expect(loadMore).not.toBeDisabled()
    fireEvent.click(loadMore)

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    const secondCallUrl = new URL(String(fetchMock.mock.calls[1]?.[0]), 'http://localhost')
    expect(secondCallUrl.searchParams.get('limit')).toBe('100')

    // Second batch returned 60 items < limit 100 → exhausted.
    await waitFor(() =>
      expect(
        screen.getByRole('button', { name: '无更多事件' }),
      ).toBeInTheDocument(),
    )
  })
})
