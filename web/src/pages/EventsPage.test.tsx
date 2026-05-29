import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
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

function mockNodesResponse(nodes: unknown[] = []) {
  return mockJSONResponse(nodes)
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
    object_type: 'node',
    object_id: 'nd_001',
    event_type: 'incident_started',
    severity: '告警',
    summary: '节点连接超时',
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

const SAMPLE_NODES = [{ node_id: 'nd_001', display_name: '生产节点-01' }]
const SAMPLE_TARGETS = [{ target_id: 'tg_001', name: 'api.example.com' }]

function setupFetchMock(options: {
  events?: unknown[]
  eventsStatus?: number
  nodes?: unknown[]
  targets?: unknown[]
  dashboard?: object | null
}) {
  const {
    events = SAMPLE_EVENTS,
    eventsStatus = 200,
    nodes = SAMPLE_NODES,
    targets = SAMPLE_TARGETS,
    dashboard = {},
  } = options

  return vi.fn((url: string) => {
    if (url.startsWith('/api/events')) return Promise.resolve(mockEventsResponse(events, eventsStatus))
    if (url === '/api/nodes') return Promise.resolve(mockNodesResponse(nodes))
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

  it('shows loading state then renders table with events', async () => {
    vi.stubGlobal('fetch', setupFetchMock({}))
    renderEventsPage()

    expect(screen.getByText('正在加载事件…')).toBeInTheDocument()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '事件流' })).toBeInTheDocument(),
    )
    expect(screen.getByText('节点连接超时')).toBeInTheDocument()
    expect(screen.getByText('证书即将过期')).toBeInTheDocument()
  })

  it('renders error state when API fails', async () => {
    vi.stubGlobal('fetch', setupFetchMock({ eventsStatus: 500 }))
    renderEventsPage()

    await waitFor(() =>
      expect(screen.getByRole('heading', { name: '事件不可用' })).toBeInTheDocument(),
    )
  })

  it('renders empty state when no events', async () => {
    vi.stubGlobal('fetch', setupFetchMock({ events: [] }))
    renderEventsPage()

    await waitFor(() =>
      expect(screen.getByText('最近没有状态变更事件')).toBeInTheDocument(),
    )
  })

  it('displays hero stats from dashboard API', async () => {
    vi.stubGlobal('fetch', setupFetchMock({}))
    renderEventsPage()

    await waitFor(() =>
      expect(screen.getByText('新增异常 (24h)')).toBeInTheDocument(),
    )
    expect(screen.getByText('3')).toBeInTheDocument()
    expect(screen.getByText('已恢复 (24h)')).toBeInTheDocument()
    expect(screen.getByText('1')).toBeInTheDocument()
  })

  it('resolves object names from nodes and targets', async () => {
    vi.stubGlobal('fetch', setupFetchMock({}))
    renderEventsPage()

    await waitFor(() =>
      expect(screen.getByText(/生产节点-01/)).toBeInTheDocument(),
    )
    expect(screen.getByText(/api\.example\.com/)).toBeInTheDocument()
  })

  it('renders table columns', async () => {
    vi.stubGlobal('fetch', setupFetchMock({}))
    renderEventsPage()

    await waitFor(() =>
      expect(screen.getByRole('columnheader', { name: '时间' })).toBeInTheDocument(),
    )
    expect(screen.getByRole('columnheader', { name: '严重度' })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: '事件类型' })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: '异常类别' })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: '摘要' })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: '对象' })).toBeInTheDocument()
  })

  it('filters locally by incident_class', async () => {
    vi.stubGlobal('fetch', setupFetchMock({}))
    renderEventsPage()

    await waitFor(() =>
      expect(screen.getByText('节点连接超时')).toBeInTheDocument(),
    )
    expect(screen.getByText('证书即将过期')).toBeInTheDocument()

    fireEvent.change(screen.getByLabelText('异常类别'), { target: { value: 'connectivity' } })

    await waitFor(() =>
      expect(screen.queryByText('证书即将过期')).not.toBeInTheDocument(),
    )
    expect(screen.getByText('节点连接超时')).toBeInTheDocument()
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

    await waitFor(() =>
      expect(screen.getByRole('dialog', { name: '事件高级筛选' })).toBeInTheDocument(),
    )
  })
})
