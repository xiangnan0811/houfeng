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
  fireEvent.click(screen.getByRole('button', { name: '高级筛选' }))
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
      expect(screen.getByRole('heading', { name: '审计与诊断时间线' })).toBeInTheDocument(),
    )

    expect(screen.getAllByText('筛选条件').length).toBeGreaterThanOrEqual(2)
    expect(screen.getAllByText('事件流').length).toBeGreaterThanOrEqual(2)
    expect(screen.getByRole('heading', { name: '诊断时间线' })).toBeInTheDocument()
    expect(screen.getByText('对象上下文')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '异常节点' })).toHaveAttribute(
      'href',
      '/nodes?abnormal=1',
    )
    expect(screen.getByRole('link', { name: '异常目标' })).toHaveAttribute(
      'href',
      '/targets?abnormal=1',
    )

    expect(screen.getByText('观测 · 事件')).toBeInTheDocument()
    expect(
      screen.getByText('承接工作台、VPS、Node 和 Target 深链，把严重度、对象、时间窗口与维护上下文呈现为可追溯的处理证据。'),
    ).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '先核对严重事件时间线' })).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '优先核对：Target · 异常升级' })).toBeInTheDocument()
    expect(screen.getAllByText('较新的事件').length).toBeGreaterThanOrEqual(2)
    expect(screen.getByText('较早的事件')).toBeInTheDocument()
    const eventStream = screen.getByRole('heading', { name: '更早' }).closest('.event-group')
    expect(eventStream).not.toBeNull()
    expect(
      within(eventStream as HTMLElement).getByText('较新的事件').compareDocumentPosition(
        within(eventStream as HTMLElement).getByText('较早的事件'),
      ) & Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy()

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
    expect(screen.getByText('对象类型: 节点')).toBeInTheDocument()
    expect(screen.getByText('严重程度: 严重')).toBeInTheDocument()
    expect(screen.getByText('事件类型: 异常开始')).toBeInTheDocument()
    expect(screen.getByText('标签: edge')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '移除筛选 包含补传事件' })).toBeInTheDocument()
    expect(screen.getByText('11 项筛选')).toBeInTheDocument()
    expect(screen.getByText('Node 事件优先核对服务器观测证据；Target 事件优先确认服务入口影响，再回到资产侧决策。')).toBeInTheDocument()
    expect(screen.getByText(/当前只看维护上下文事件 · 自定义时间/)).toBeInTheDocument()
    expect(screen.getByText('已应用筛选 · 自定义时间 · 最近 25 条')).toBeInTheDocument()
    expect(screen.getByText('当前事件流只展示 URL 固定的筛选结果；加载更早事件会沿用同一组条件扩大数量上限。')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '没有匹配当前诊断条件' })).toBeInTheDocument()
  })

  it('clears an empty Dashboard events filter from the evidence lead', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockEventsResponse([]))
      .mockResolvedValueOnce(mockEventsResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    const page = renderEventsPage('/events?severity=%E4%B8%A5%E9%87%8D')

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '没有匹配当前诊断条件' })).toBeInTheDocument(),
    )
    expect(screen.getByText('严重度 严重')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '清空事件筛选' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenLastCalledWith('/api/events?limit=50', {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      }),
    )
    await waitFor(() => expect(page.getCurrentLocation()).toBe('/events'))
  })

  it('shows a stable timeline focus when the loaded slice has no priority event', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        mockEventsResponse([
          {
            event_id: 'evt_normal',
            incident_id: '',
            incident_class: '',
            object_type: 'node',
            object_id: 'nd_001',
            event_type: 'node_monitoring_resumed',
            severity: '正常',
            summary: '节点恢复监控',
            created_at: new Date().toISOString(),
          },
        ]),
      ),
    )

    renderEventsPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '事件时间线当前稳定' })).toBeInTheDocument(),
    )
    expect(screen.getByText('默认：未限定时间 · 最近 50 条')).toBeInTheDocument()
    expect(screen.getByText('默认事件流未限定时间范围，按最近事件数量截取；需要精确窗口可使用高级筛选。')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '没有需要优先核对的事件' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '查看 24h 事件' })).toHaveAttribute(
      'href',
      '/events?time_range=24h',
    )
  })

  it('keeps Dashboard time-range and maintenance deep links visible in the evidence lead', async () => {
    const fetchMock = vi.fn().mockResolvedValue(mockEventsResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    renderEventsPage('/events?time_range=24h&maintenance_only=1')

    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))
    const url = new URL(String(fetchMock.mock.calls[0]?.[0]), 'http://localhost')
    expect(url.searchParams.get('created_from')).toMatch(/^\d{4}-\d{2}-\d{2}T/)
    expect(url.searchParams.get('created_to')).toMatch(/^\d{4}-\d{2}-\d{2}T/)
    expect(url.searchParams.get('maintenance_only')).toBe('true')

    expect(screen.getByText('时间 近 24 小时')).toBeInTheDocument()
    expect(screen.getByText('仅维护')).toBeInTheDocument()
    expect(screen.getByText(/当前只看维护上下文事件 · 近 24 小时/)).toBeInTheDocument()
  })

  it('offers binding audit event filters for node onboarding actions', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockEventsResponse([]))
      .mockResolvedValueOnce(mockEventsResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    renderEventsPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '审计与诊断时间线' })).toBeInTheDocument(),
    )

    const drawer = await openFilterDrawer()
    fireEvent.change(within(drawer).getByLabelText('事件类型'), {
      target: { value: 'node_binding_reset' },
    })
    fireEvent.click(within(drawer).getByRole('button', { name: '应用筛选' }))

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
      .mockResolvedValueOnce(mockEventsResponse([]))
      .mockResolvedValueOnce(mockEventsResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    renderEventsPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '审计与诊断时间线' })).toBeInTheDocument(),
    )

    const drawer = await openFilterDrawer()
    const eventTypeSelect = within(drawer).getByLabelText('事件类型')

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
    fireEvent.click(within(drawer).getByRole('button', { name: '应用筛选' }))

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
      .mockResolvedValueOnce(mockEventsResponse([]))
      .mockResolvedValueOnce(mockEventsResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    renderEventsPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '审计与诊断时间线' })).toBeInTheDocument(),
    )

    const drawer = await openFilterDrawer()
    const eventTypeSelect = within(drawer).getByLabelText('事件类型')

    expect(
      within(eventTypeSelect).getByRole('option', { name: '节点已退役' }),
    ).toBeInTheDocument()
    expect(
      within(eventTypeSelect).getByRole('option', { name: '节点恢复到观察中' }),
    ).toBeInTheDocument()

    fireEvent.change(eventTypeSelect, {
      target: { value: 'node_restored_to_observing' },
    })
    fireEvent.click(within(drawer).getByRole('button', { name: '应用筛选' }))

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
      .mockResolvedValueOnce(mockEventsResponse([]))
      .mockResolvedValueOnce(mockEventsResponse([]))
      .mockResolvedValueOnce(mockEventsResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    const page = renderEventsPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '审计与诊断时间线' })).toBeInTheDocument(),
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

  it('applies and removes the backfilled event toggle', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(mockEventsResponse([]))
      .mockResolvedValueOnce(mockEventsResponse([]))
      .mockResolvedValueOnce(mockEventsResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    const page = renderEventsPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '审计与诊断时间线' })).toBeInTheDocument(),
    )

    const drawer = await openFilterDrawer()
    const backfilledToggle = within(drawer).getByLabelText('包含补传事件')
    expect(backfilledToggle).not.toBeDisabled()
    expect(within(drawer).getByText('未包含')).toBeInTheDocument()
    fireEvent.click(backfilledToggle)
    expect(within(drawer).getByText('已包含')).toBeInTheDocument()
    fireEvent.click(within(drawer).getByRole('button', { name: '应用筛选' }))

    await waitFor(() =>
      expect(fetchMock).toHaveBeenLastCalledWith('/api/events?limit=50&include_backfilled=true', {
        headers: { Accept: 'application/json' },
        cache: 'no-store',
        credentials: 'include',
      }),
    )
    await waitFor(() => expect(page.getCurrentLocation()).toBe('/events?include_backfilled=1'))
    expect(screen.getByRole('button', { name: '移除筛选 包含补传事件' })).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: '移除筛选 包含补传事件' }))

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
      expect(screen.getByRole('heading', { name: '审计与诊断时间线' })).toBeInTheDocument(),
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

  it('ignores invalid URL params and invalid backfilled filters', async () => {
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

  it('renders an explicit empty state when no events exist', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(mockEventsResponse([])))

    renderEventsPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '事件时间线当前稳定' })).toBeInTheDocument(),
    )
    expect(screen.getByRole('heading', { name: '最近没有状态变更事件' })).toBeInTheDocument()
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
      .mockResolvedValueOnce(mockEventsResponse([]))
      .mockResolvedValueOnce(mockEventsResponse([]))
    vi.stubGlobal('fetch', fetchMock)

    const page = renderEventsPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '审计与诊断时间线' })).toBeInTheDocument(),
    )

    const drawer = await openFilterDrawer()
    fireEvent.click(within(drawer).getByRole('tab', { name: '近 7 天' }))

    // Date inputs are disabled while a preset range is selected.
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
      .mockResolvedValueOnce(mockEventsResponse(firstBatch))
      .mockResolvedValueOnce(mockEventsResponse([...firstBatch, ...firstBatch.slice(0, 10)]))
    vi.stubGlobal('fetch', fetchMock)

    renderEventsPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '审计与诊断时间线' })).toBeInTheDocument(),
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
