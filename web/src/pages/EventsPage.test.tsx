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

function mockEventsResponse(events: unknown[], status = 200) {
  return mockJSONResponse({ items: events }, status)
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

async function openFilterDrawer() {
  fireEvent.click(screen.getByRole('button', { name: '筛选面板' }))
  return screen.findByRole('dialog', { name: '事件高级筛选' })
}

describe('EventsPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders loading, fetched events, and applies lightweight filters', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        mockEventsResponse([
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
      .mockResolvedValueOnce(mockEventsResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    renderEventsPage()

    expect(screen.getByText('正在加载事件…')).toBeInTheDocument()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '事件流' })).toBeInTheDocument(),
    )

    expect(screen.getByText('状态变更事件时间线')).toBeInTheDocument()
    expect(screen.getByText(/较新的事件/)).toBeInTheDocument()
    expect(screen.getByText(/较早的事件/)).toBeInTheDocument()

    const drawer = await openFilterDrawer()
    fireEvent.change(within(drawer).getByLabelText('对象类型'), { target: { value: 'target' } })
    fireEvent.change(within(drawer).getByLabelText('严重程度'), { target: { value: '严重' } })
    fireEvent.change(within(drawer).getByLabelText('事件类型'), { target: { value: 'incident_started' } })
    fireEvent.change(within(drawer).getByLabelText('数量'), { target: { value: '10' } })
    fireEvent.click(within(drawer).getByRole('button', { name: '应用筛选' }))

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
    const fetchMock = vi.fn().mockResolvedValue(mockEventsResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    renderEventsPage(
      '/events?object_type=node&severity=%E4%B8%A5%E9%87%8D&event_type=incident_started&limit=25&created_from=2026-04-25T00:00:00Z&created_to=2026-04-26T00:00:00Z&label=edge&notification_only=1&recovery_only=1&maintenance_only=1&time_range=custom&include_backfilled=1',
    )

    await waitFor(() =>
      expect(fetchMock).toHaveBeenLastCalledWith(
        '/api/events?object_type=node&severity=%E4%B8%A5%E9%87%8D&event_type=incident_started&limit=25&created_from=2026-04-25T00%3A00%3A00Z&created_to=2026-04-26T00%3A00%3A00Z&label=edge&notification_only=true&recovery_only=true&maintenance_only=true&include_backfilled=true',
        {
          headers: { Accept: 'application/json' },
          cache: 'no-store',
          credentials: 'include',
        },
      ),
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

  it('renders empty state when no events exist', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockEventsResponse([])))

    renderEventsPage()

    await waitFor(() =>
      expect(screen.getByText('最近没有状态变更事件')).toBeInTheDocument(),
    )
  })

  it('selects a 7d time range preset and forwards created_from/created_to', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockEventsResponse([]))
      .mockResolvedValueOnce(mockEventsResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    const page = renderEventsPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '事件流' })).toBeInTheDocument(),
    )

    const drawer = await openFilterDrawer()
    fireEvent.click(within(drawer).getByRole('tab', { name: '近 7 天' }))

    expect(within(drawer).getByLabelText('开始时间')).toBeDisabled()
    expect(within(drawer).getByLabelText('结束时间')).toBeDisabled()

    fireEvent.click(within(drawer).getByRole('button', { name: '应用筛选' }))

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
  })

  it('preserves relative time range in URL while sending dynamic API dates', async () => {
    const fetchMock = vi.fn().mockResolvedValue(mockEventsResponse([]))
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

  it('submits advanced context filters and can reset to the default event stream', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockEventsResponse([]))
      .mockResolvedValueOnce(mockEventsResponse([]))
      .mockResolvedValueOnce(mockEventsResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    const page = renderEventsPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '事件流' })).toBeInTheDocument(),
    )

    const drawer = await openFilterDrawer()
    fireEvent.change(within(drawer).getByLabelText('对象类型'), { target: { value: 'node' } })
    fireEvent.change(within(drawer).getByLabelText('数量'), { target: { value: '25' } })
    fireEvent.change(within(drawer).getByLabelText('开始时间'), {
      target: { value: '2026-04-25T00:00:00Z' },
    })
    fireEvent.change(within(drawer).getByLabelText('结束时间'), {
      target: { value: '2026-04-26T00:00:00Z' },
    })
    fireEvent.change(within(drawer).getByLabelText('标签'), { target: { value: 'edge' } })
    fireEvent.click(within(drawer).getByLabelText('仅看通知事件'))
    fireEvent.click(within(drawer).getByLabelText('仅看恢复事件'))
    fireEvent.click(within(drawer).getByLabelText('仅看维护事件'))
    fireEvent.click(within(drawer).getByLabelText('包含补传事件'))
    fireEvent.click(within(drawer).getByRole('button', { name: '应用筛选' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenLastCalledWith(
        '/api/events?object_type=node&limit=25&created_from=2026-04-25T00%3A00%3A00Z&created_to=2026-04-26T00%3A00%3A00Z&label=edge&notification_only=true&recovery_only=true&maintenance_only=true&include_backfilled=true',
        {
          headers: { Accept: 'application/json' },
          cache: 'no-store',
          credentials: 'include',
        },
      ),
    )
    await waitFor(() =>
      expect(page.getCurrentLocation()).toBe(
        '/events?object_type=node&limit=25&time_range=custom&created_from=2026-04-25T00%3A00%3A00Z&created_to=2026-04-26T00%3A00%3A00Z&label=edge&notification_only=1&recovery_only=1&maintenance_only=1&include_backfilled=1',
      ),
    )

    const resetDrawer = await openFilterDrawer()
    fireEvent.click(within(resetDrawer).getByRole('button', { name: '重置筛选' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenLastCalledWith('/api/events?limit=50', {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      }),
    )
    await waitFor(() => expect(page.getCurrentLocation()).toBe('/events'))
  })

  it('closes the filter drawer without applying draft changes', async () => {
    const fetchMock = vi.fn().mockResolvedValue(mockEventsResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    const page = renderEventsPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '事件流' })).toBeInTheDocument(),
    )

    const drawer = await openFilterDrawer()
    fireEvent.change(within(drawer).getByLabelText('对象类型'), {
      target: { value: 'target' },
    })
    fireEvent.click(within(drawer).getByText('关闭'))

    await waitFor(() =>
      expect(screen.queryByRole('dialog', { name: '事件高级筛选' })).not.toBeInTheDocument(),
    )
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(page.getCurrentLocation()).toBe('/events')

    const reopened = await openFilterDrawer()
    expect(within(reopened).getByLabelText('对象类型')).toHaveValue('')
    fireEvent.keyDown(document, { key: 'Escape' })

    await waitFor(() =>
      expect(screen.queryByRole('dialog', { name: '事件高级筛选' })).not.toBeInTheDocument(),
    )
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(page.getCurrentLocation()).toBe('/events')
  })

  it('removes active chips from URL state and refetches', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockEventsResponse([]))
      .mockResolvedValueOnce(mockEventsResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    const page = renderEventsPage('/events?severity=%E4%B8%A5%E9%87%8D&label=edge')

    await waitFor(() =>
      expect(fetchMock).toHaveBeenLastCalledWith('/api/events?severity=%E4%B8%A5%E9%87%8D&limit=50&label=edge', {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      }),
    )

    fireEvent.click(screen.getByRole('button', { name: '移除筛选 严重度: 严重' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenLastCalledWith('/api/events?limit=50&label=edge', {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      }),
    )
    await waitFor(() => expect(page.getCurrentLocation()).toBe('/events?label=edge'))
  })

  it('ignores invalid URL params', async () => {
    const fetchMock = vi.fn().mockResolvedValue(mockEventsResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    const page = renderEventsPage(
      '/events?object_type=service&severity=%E6%AD%A3%E5%B8%B8&event_type=unknown&limit=999&created_from=not-a-date&created_to=also-bad&notification_only=true&recovery_only=0&maintenance_only=yes&time_range=invalid&include_backfilled=yes',
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

  it('renders time-grouped events and a load-more button', async () => {
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
      .mockResolvedValueOnce(mockEventsResponse(firstBatch))
      .mockResolvedValueOnce(mockEventsResponse([...firstBatch, ...firstBatch.slice(0, 10)]))
    vi.stubGlobal('fetch', fetchMock)

    renderEventsPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '事件流' })).toBeInTheDocument(),
    )

    const loadMore = screen.getByRole('button', { name: '加载更多' })
    expect(loadMore).not.toBeDisabled()
    fireEvent.click(loadMore)

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2))
    const secondCallUrl = new URL(String(fetchMock.mock.calls[1]?.[0]), 'http://localhost')
    expect(secondCallUrl.searchParams.get('limit')).toBe('100')

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '无更多事件' })).toBeInTheDocument(),
    )
  })
})
