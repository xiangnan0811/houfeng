import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
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

function mockMonitoringInstancesResponse(monitoring: unknown[] = []) {
  return mockJSONResponse(monitoring)
}

function mockTargetsResponse(targets: unknown[] = []) {
  return mockJSONResponse(targets)
}

function mockDashboardResponse(overrides = {}) {
  return mockJSONResponse({
    recent_new_incident_count: 3,
    recent_recovery_count: 1,
    new_incident_trend_24h: [0, 1, 0, 2, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
    recovery_trend_24h: [0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
    ...overrides,
  })
}

const SAMPLE_EVENTS = [
  {
    event_id: 'evt_001',
    incident_id: 'inc_001',
    incident_class: 'connectivity',
    object_type: 'monitoring_instance',
    object_id: 'mi_001',
    event_type: 'incident_started',
    severity: '告警',
    summary: '监控实例连接超时',
    created_at: '2026-05-28T10:00:00Z',
  },
  {
    event_id: 'evt_002',
    incident_id: 'inc_002',
    incident_class: 'certificate',
    object_type: 'target',
    object_id: 'tg_001',
    event_type: 'incident_escalated',
    severity: '严重',
    summary: '证书即将过期',
    created_at: '2026-05-28T09:30:00Z',
  },
]

const SAMPLE_MONITORING_INSTANCES = [{ monitoring_instance_id: 'mi_001', display_name: '生产监控实例-01' }]
const SAMPLE_TARGETS = [{ target_id: 'tg_001', name: 'api.example.com' }]

function setupFetchMock(options: {
  events?: unknown[]
  eventsStatus?: number
  monitoring?: unknown[]
  targets?: unknown[]
  dashboard?: object | null
}) {
  const {
    events = SAMPLE_EVENTS,
    eventsStatus = 200,
    monitoring = SAMPLE_MONITORING_INSTANCES,
    targets = SAMPLE_TARGETS,
    dashboard = {},
  } = options

  return vi.fn((url: string) => {
    if (url.startsWith('/api/events')) return Promise.resolve(mockEventsResponse(events, eventsStatus))
    if (url === '/api/monitoring-instances') return Promise.resolve(mockMonitoringInstancesResponse(monitoring))
    if (url === '/api/targets') return Promise.resolve(mockTargetsResponse(targets))
    if (url === '/api/dashboard') return Promise.resolve(mockDashboardResponse(dashboard ?? {}))
    return Promise.resolve(mockJSONResponse({}, 404))
  })
}

function renderEventsPage(initialEntry = '/events') {
  render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <EventsPage />
    </MemoryRouter>,
  )
}

describe('EventsPage', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('shows loading state then renders notice rows with events', async () => {
    vi.stubGlobal('fetch', setupFetchMock({}))
    renderEventsPage()

    expect(screen.getByRole('heading', { name: '事件流' })).toBeInTheDocument()
    expect(screen.getByText('正在加载事件…')).toBeInTheDocument()

    await waitFor(() =>
      expect(screen.getByText('监控实例连接超时')).toBeInTheDocument(),
    )
    expect(screen.getByText('证书即将过期')).toBeInTheDocument()
    expect(screen.getAllByText('异常开始').length).toBeGreaterThan(0)
    expect(screen.getAllByText('异常升级').length).toBeGreaterThan(0)
    expect(screen.queryByText('新增异常 (24h)')).not.toBeInTheDocument()
    expect(screen.getByLabelText('时间范围')).toHaveDisplayValue('全部时间')
  })

  it('forwards object_id from the URL into the events query', async () => {
    const fetchMock = setupFetchMock({})
    vi.stubGlobal('fetch', fetchMock)
    renderEventsPage('/events?object_type=monitoring_instance&object_id=mi_001')

    await waitFor(() =>
      expect(fetchMock.mock.calls.some((call) =>
        String(call[0]).includes('/api/events?')
        && String(call[0]).includes('object_type=monitoring_instance')
        && String(call[0]).includes('object_id=mi_001'),
      )).toBe(true),
    )
  })

  it('shows object_id as a clearable chip and drops it when object type changes', async () => {
    const fetchMock = setupFetchMock({})
    vi.stubGlobal('fetch', fetchMock)
    renderEventsPage('/events?object_type=monitoring_instance&object_id=mi_001')

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '移除筛选 对象 ID: mi_001' })).toBeInTheDocument(),
    )

    fireEvent.change(screen.getByLabelText('对象类型'), { target: { value: 'target' } })

    await waitFor(() =>
      expect(fetchMock.mock.calls.some((call) =>
        String(call[0]).includes('/api/events?')
        && String(call[0]).includes('object_type=target')
        && !String(call[0]).includes('object_id='),
      )).toBe(true),
    )
    expect(screen.queryByRole('button', { name: '移除筛选 对象 ID: mi_001' })).not.toBeInTheDocument()
  })

  it('renders error state when API fails', async () => {
    vi.stubGlobal('fetch', setupFetchMock({ eventsStatus: 500 }))
    renderEventsPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '事件不可用' })).toBeInTheDocument(),
    )
    expect(screen.getByRole('heading', { name: '事件流' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重试' })).toBeInTheDocument()
  })

  it('renders empty state when no events', async () => {
    vi.stubGlobal('fetch', setupFetchMock({ events: [] }))
    renderEventsPage()

    await waitFor(() =>
      expect(screen.getByText('没有状态变更事件')).toBeInTheDocument(),
    )
  })

  it('does not stack dashboard stats above the event stream', async () => {
    vi.stubGlobal('fetch', setupFetchMock({}))
    renderEventsPage()

    await waitFor(() =>
      expect(screen.getByText('监控实例连接超时')).toBeInTheDocument(),
    )
    expect(screen.queryByText('新增异常 (24h)')).not.toBeInTheDocument()
    expect(screen.queryByText('已恢复 (24h)')).not.toBeInTheDocument()
  })

  it('resolves object names from monitoring and targets', async () => {
    vi.stubGlobal('fetch', setupFetchMock({}))
    renderEventsPage()

    await waitFor(() =>
      expect(screen.getByText(/生产监控实例-01/)).toBeInTheDocument(),
    )
    expect(screen.getByText(/api\.example\.com/)).toBeInTheDocument()
  })

  it('renders Chinese incident classes instead of snake_case', async () => {
    vi.stubGlobal('fetch', setupFetchMock({}))
    renderEventsPage()

    await waitFor(() =>
      expect(screen.getByText('监控实例连接超时')).toBeInTheDocument(),
    )
    expect(screen.getAllByText('连通性').length).toBeGreaterThan(0)
    expect(screen.getAllByText('证书').length).toBeGreaterThan(0)
    expect(screen.queryByText('connectivity')).not.toBeInTheDocument()
    expect(screen.queryByText('certificate')).not.toBeInTheDocument()
  })

  it('filters locally by incident_class', async () => {
    vi.stubGlobal('fetch', setupFetchMock({}))
    renderEventsPage()

    await waitFor(() =>
      expect(screen.getByText('监控实例连接超时')).toBeInTheDocument(),
    )
    expect(screen.getByText('证书即将过期')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('异常类别'), { target: { value: 'connectivity' } })

    await waitFor(() =>
      expect(screen.queryByText('证书即将过期')).not.toBeInTheDocument(),
    )
    expect(screen.getByText('监控实例连接超时')).toBeInTheDocument()
  })

  it('has CSV export button', async () => {
    vi.stubGlobal('fetch', setupFetchMock({}))
    renderEventsPage()

    await waitFor(() =>
      expect(screen.getByRole('button', { name: '导出 CSV' })).toBeInTheDocument(),
    )
  })

  it('opens advanced filter drawer', async () => {
    vi.stubGlobal('fetch', setupFetchMock({}))
    renderEventsPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '事件流' })).toBeInTheDocument(),
    )

    fireEvent.click(screen.getByRole('button', { name: '高级筛选' }))

    const drawer = await screen.findByRole('dialog', { name: '事件高级筛选' })
    const timeRange = within(drawer).getByRole('group', { name: '事件时间范围' })
    expect(timeRange).toBeInTheDocument()
    expect(within(timeRange).getByRole('button', { name: '全部时间' })).toHaveAttribute('aria-pressed', 'true')
    expect(within(timeRange).queryByRole('tab')).not.toBeInTheDocument()
  })

  it('folds a dateless custom range back to all time and omits time_range from the URL', async () => {
    const fetchMock = setupFetchMock({})
    vi.stubGlobal('fetch', fetchMock)
    function SearchProbe() {
      const location = useLocation()
      return <output aria-label="当前查询参数">{location.search}</output>
    }
    render(
      <MemoryRouter initialEntries={['/events']}>
        <Routes>
          <Route
            path="/events"
            element={(
              <>
                <EventsPage />
                <SearchProbe />
              </>
            )}
          />
        </Routes>
      </MemoryRouter>,
    )

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '事件流' })).toBeInTheDocument(),
    )
    fireEvent.click(screen.getByRole('button', { name: '高级筛选' }))
    const drawer = await screen.findByRole('dialog', { name: '事件高级筛选' })
    fireEvent.click(within(drawer).getByRole('button', { name: '自定义' }))
    fireEvent.click(within(drawer).getByRole('button', { name: '应用筛选' }))

    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: '事件高级筛选' })).not.toBeInTheDocument()
    })
    expect(screen.getByLabelText('时间范围')).toHaveDisplayValue('全部时间')
    expect(screen.getByLabelText('当前查询参数')).toHaveTextContent('')
    expect(fetchMock.mock.calls.some((call) => {
      const url = String(call[0])
      return url.startsWith('/api/events') && !url.includes('created_from') && !url.includes('created_to') && !url.includes('time_range')
    })).toBe(true)
  })

  it('returns the events time filter to all time when the placeholder option is chosen', async () => {
    vi.stubGlobal('fetch', setupFetchMock({}))
    renderEventsPage()
    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '事件流' })).toBeInTheDocument(),
    )
    fireEvent.change(screen.getByLabelText('时间范围'), { target: { value: '24h' } })
    expect(screen.getByLabelText('时间范围')).toHaveDisplayValue('近 24 小时')
    fireEvent.change(screen.getByLabelText('时间范围'), { target: { value: '' } })
    expect(screen.getByLabelText('时间范围')).toHaveDisplayValue('全部时间')
  })
})
