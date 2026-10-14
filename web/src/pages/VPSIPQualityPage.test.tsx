import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { Link, MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, describe, expect, it, vi, type Mock } from 'vitest'

import { COLLECT_POLL_INTERVAL_MS } from '../components/ip-quality/useIPQualityCollect'
import { VPSIPQualityPage } from './VPSIPQualityPage'

type FetchMock = Mock<(input: RequestInfo | URL, init?: RequestInit) => Promise<Response>>

const IPV4 = '192.0.2.1'
const IPV6 = '2001:db8::10'

function summaryBody(family: 4 | 6, patch: Record<string, unknown> = {}) {
  return {
    report_id: family === 4 ? 'ipq_001' : 'ipq_601',
    vps_id: 'vps_001',
    observed_at: '2026-06-08T12:00:00Z',
    ip_address: family === 4 ? IPV4 : IPV6,
    ip_version: family,
    status: 'success',
    risk_level: 'high',
    use_region_code: 'JP',
    use_region_name: 'Japan',
    asn: family === 4 ? 'AS64500' : 'AS64600',
    organization: family === 4 ? 'Example Transit' : 'Example Six',
    stale: false,
    ambiguous: false,
    assignment_mode: 'link',
    provider_count: 2,
    unlockable_count: 3,
    ...patch,
  }
}

function families(attempts: Partial<Record<4 | 6, Record<string, unknown> | null>> = {}, availability: Partial<Record<4 | 6, string>> = {}) {
  return ([4, 6] as const).map((family) => ({
    ip_version: family,
    availability: availability[family] ?? 'available',
    latest_attempt: attempts[family] ?? null,
    latest_report_summary: null,
  }))
}

type BodyOptions = {
  family?: 4 | 6
  defaultFamily?: 4 | 6
  empty?: boolean
  collectionId?: string
  reportId?: string
  diagnostics?: Record<string, unknown>
  familyStatus?: Array<Record<string, unknown>>
  extra?: Record<string, unknown>
}

function reportBody({ family = 4, defaultFamily = 4, empty = false, collectionId = 'col_old', reportId, diagnostics, familyStatus, extra = {} }: BodyOptions = {}) {
  const base = {
    selected_ip_version: family,
    default_ip_version: defaultFamily,
    families: familyStatus ?? families(),
  }
  if (empty) return { ...base, summary: null, latest_report: null, provider_results: [], service_unlocks: [], history: [], ...extra }
  const summary = summaryBody(family, reportId ? { report_id: reportId } : {})
  return {
    ...base,
    summary,
    latest_report: {
      report_id: summary.report_id,
      monitoring_instance_id: 'mi_001',
      observed_at: summary.observed_at,
      received_at: '2026-06-08T12:00:05Z',
      agent_version: 'dev',
      fingerprint: 'fp-001',
      sync_batch_id: 'sync_001',
      ip_address: summary.ip_address,
      ip_version: family,
      status: 'success',
      risk_level: 'high',
      is_backfilled: false,
      created_at: '2026-06-08T12:00:06Z',
      diagnostics_json: diagnostics ?? { source_version: 'v2', collection_id: collectionId },
    },
    provider_results: [
      { provider: 'ipinfo', status: 'success', source_type: 'default', usage_type: 'hosting', is_vpn: true, is_proxy: false },
    ],
    service_unlocks: [
      { service: 'chatgpt', source: 'openai_status_probe', status: 'unlocked', probe_status: 'success', region: 'JP' },
    ],
    history: [
      summary,
      { ...summary, report_id: family === 4 ? 'ipq_000' : 'ipq_600', observed_at: '2026-06-07T12:00:00Z', risk_level: 'medium' },
    ],
    ...extra,
  }
}

const availableStatus = { enabled: true, available: true, monitoring_instance_id: 'mi_001' }

function collectRequest(status: string, extra: Record<string, unknown> = {}) {
  return {
    request_id: 'ipqc_001',
    monitoring_instance_id: 'mi_001',
    status,
    requested_at: new Date().toISOString(),
    expires_at: new Date(Date.now() + 600_000).toISOString(),
    ...extra,
  }
}

function dualCompleted(results: Array<Record<string, unknown>>, extra: Record<string, unknown> = {}) {
  return collectRequest('completed', {
    report_status: 'success',
    collection_scope: 'dual_stack',
    collection_id: 'col_new',
    family_results: results,
    completed_at: new Date().toISOString(),
    ...extra,
  })
}

function mockJSONResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
  } as Response
}

// body 可为函数：按请求时刻的服务端状态返回，用于模拟“陈旧空闲”的额外状态读取。
type RouteResponse = { body: unknown, status?: number, wait?: Promise<void> }
type FetchRoute = { method?: string, path: string, responses: RouteResponse[] }

// 按 method + 完整 path（含 query）分发的 fetch 替身；每条路由按顺序消费响应，最后一个响应重复使用。
function routeFetch(routes: FetchRoute[]): FetchMock {
  const queues = routes.map((route) => ({ ...route, responses: [...route.responses] }))
  return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = String(input)
    const method = init?.method ?? 'GET'
    const route = queues.find((item) => item.path === path && (item.method ?? 'GET') === method)
    if (!route) throw new Error(`unexpected request ${method} ${path}`)
    const next = route.responses.length > 1 ? route.responses.shift() : route.responses[0]
    if (!next) throw new Error(`no response configured for ${method} ${path}`)
    if (next.wait) await next.wait
    return mockJSONResponse(typeof next.body === 'function' ? (next.body as () => unknown)() : next.body, next.status)
  })
}

function deferred() {
  let resolve!: () => void
  const promise = new Promise<void>((done) => { resolve = done })
  return { promise, resolve }
}

function LocationProbe() {
  const location = useLocation()
  return <output data-testid="location">{`${location.pathname}${location.search}`}</output>
}

function renderPage(initialEntry = '/vps/vps_001/ip-quality') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <LocationProbe />
      <Link to="/vps/vps_002/ip-quality?ip_version=4">切换到 vps_002</Link>
      <Routes>
        <Route path="/vps/:vpsId/ip-quality" element={<VPSIPQualityPage />} />
      </Routes>
    </MemoryRouter>,
  )
}

function callsTo(fetchMock: FetchMock, path: string, method = 'GET') {
  return fetchMock.mock.calls.filter(([input, init]) => String(input) === path && (init?.method ?? 'GET') === method)
}

function reportCalls(fetchMock: FetchMock) {
  return fetchMock.mock.calls.filter(([input]) => String(input).includes('/ip-quality') && !String(input).endsWith('/collect'))
}

const currentLocation = () => screen.getByTestId('location').textContent
const familyGroup = () => screen.getByRole('group', { name: 'IP 地址族' })
const BASE = '/api/vps/vps_001/ip-quality'
const COLLECT = `${BASE}/collect`

describe('VPSIPQualityPage', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  it('loads the server default once and canonicalizes the URL without a second request', async () => {
    const fetchMock = routeFetch([
      { path: BASE, responses: [{ body: reportBody() }] },
      { path: COLLECT, responses: [{ body: availableStatus }] },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await waitFor(() => expect(screen.getByLabelText('报告身份')).toHaveTextContent(IPV4))
    expect(fetchMock).toHaveBeenCalledWith(BASE, {
      headers: { Accept: 'application/json' },
      cache: 'no-store',
      credentials: 'include',
    })
    await waitFor(() => expect(currentLocation()).toBe('/vps/vps_001/ip-quality?ip_version=4'))
    await waitFor(() => expect(callsTo(fetchMock, COLLECT)).toHaveLength(1))
    expect(reportCalls(fetchMock)).toHaveLength(1)
    expect(within(familyGroup()).getByRole('button', { name: 'IPv4' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: '立即采集' })).toBeEnabled()
    expect(screen.getByText('分别检测 IPv4 / IPv6')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: '返回 VPS 详情' })).toHaveAttribute('href', '/vps/vps_001')
    expect(screen.getByRole('heading', { name: '采集诊断' }).closest('details')).not.toHaveAttribute('open')
    expect(screen.queryByRole('button', { name: /保存|编辑|删除/ })).not.toBeInTheDocument()
  })

  it('follows an IPv6-only default, keeps IPv4 selectable and never shows IPv4 facts in the IPv6 view', async () => {
    const fetchMock = routeFetch([
      {
        path: BASE,
        responses: [{ body: reportBody({ family: 6, defaultFamily: 6, familyStatus: families({ 4: { observed_at: '2026-06-08T12:00:00Z', stale: false, report_status: 'failure', error_code: 'address_family_unavailable', collection_scope: 'dual_stack', collection_id: 'col_old' } }, { 4: 'unavailable' }) }) }],
      },
      {
        path: `${BASE}?ip_version=4`,
        responses: [{ body: reportBody({ family: 4, defaultFamily: 6, empty: true, familyStatus: families({ 4: { observed_at: '2026-06-08T12:00:00Z', stale: false, report_status: 'failure', error_code: 'address_family_unavailable', collection_scope: 'dual_stack', collection_id: 'col_old' } }, { 4: 'unavailable' }) }) }],
      },
      { path: COLLECT, responses: [{ body: availableStatus }] },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await waitFor(() => expect(screen.getByLabelText('报告身份')).toHaveTextContent(IPV6))
    await waitFor(() => expect(currentLocation()).toBe('/vps/vps_001/ip-quality?ip_version=6'))
    const page = document.querySelector('.ipq-page') as HTMLElement
    expect(page).not.toHaveTextContent(IPV4)
    expect(page).not.toHaveTextContent('0.0.0.0')

    fireEvent.click(within(familyGroup()).getByRole('button', { name: 'IPv4' }))
    await waitFor(() => expect(screen.getByRole('note', { name: '地址族采集状态' })).toHaveTextContent('本轮未检测到 IPv4 可用本地地址'))
    expect(currentLocation()).toBe('/vps/vps_001/ip-quality?ip_version=4')
    expect(screen.queryByLabelText('报告身份')).not.toBeInTheDocument()
    expect(document.querySelector('.ipq-page')).not.toHaveTextContent(/0\.0\.0\.0|2001:db8::10/)
  })

  it('requests only the explicit family and switches families without posting a collection', async () => {
    const fetchMock = routeFetch([
      { path: `${BASE}?ip_version=4`, responses: [{ body: reportBody() }] },
      { path: `${BASE}?ip_version=6`, responses: [{ body: reportBody({ family: 6 }) }] },
      { path: COLLECT, responses: [{ body: availableStatus }] },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage('/vps/vps_001/ip-quality?ip_version=4')
    await waitFor(() => expect(screen.getByLabelText('报告身份')).toHaveTextContent(IPV4))

    const ipv6Button = within(familyGroup()).getByRole('button', { name: 'IPv6' })
    ipv6Button.focus()
    fireEvent.click(ipv6Button)
    // 切族加载期间页头与选择器保持挂载，焦点留在按钮上。
    expect(screen.getByRole('group', { name: 'IP 地址族' })).toBeInTheDocument()
    await waitFor(() => expect(screen.getByLabelText('报告身份')).toHaveTextContent(IPV6))
    expect(document.activeElement).toBe(ipv6Button)
    expect(currentLocation()).toBe('/vps/vps_001/ip-quality?ip_version=6')
    expect(document.querySelector('.ipq-page')).not.toHaveTextContent(IPV4)
    const historyLink = screen.getAllByRole('link', { name: /^查看 .* 的报告$/ })[0]!
    expect(historyLink).toHaveAttribute('href', '/vps/vps_001/ip-quality?ip_version=6&report_id=ipq_600')
    expect(callsTo(fetchMock, COLLECT, 'POST')).toHaveLength(0)
    expect(callsTo(fetchMock, BASE)).toHaveLength(0)
  })

  it('ignores a late response from a family the user already left', async () => {
    const slowIPv6 = deferred()
    const fetchMock = routeFetch([
      { path: `${BASE}?ip_version=4`, responses: [{ body: reportBody() }] },
      { path: `${BASE}?ip_version=6`, responses: [{ body: reportBody({ family: 6 }), wait: slowIPv6.promise }] },
      { path: COLLECT, responses: [{ body: availableStatus }] },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage('/vps/vps_001/ip-quality?ip_version=4')
    await waitFor(() => expect(screen.getByLabelText('报告身份')).toHaveTextContent(IPV4))
    fireEvent.click(within(familyGroup()).getByRole('button', { name: 'IPv6' }))
    await waitFor(() => expect(callsTo(fetchMock, `${BASE}?ip_version=6`)).toHaveLength(1))
    fireEvent.click(within(familyGroup()).getByRole('button', { name: 'IPv4' }))
    await waitFor(() => expect(screen.getByLabelText('报告身份')).toHaveTextContent(IPV4))

    await act(async () => { slowIPv6.resolve() })
    expect(screen.getByLabelText('报告身份')).toHaveTextContent(IPV4)
    expect(document.querySelector('.ipq-page')).not.toHaveTextContent(IPV6)
    expect(currentLocation()).toBe('/vps/vps_001/ip-quality?ip_version=4')
  })

  it('discards the previous VPS response when the route switches VPS mid-flight', async () => {
    const slowFirst = deferred()
    const fetchMock = routeFetch([
      { path: `${BASE}?ip_version=4`, responses: [{ body: reportBody(), wait: slowFirst.promise }] },
      { path: '/api/vps/vps_002/ip-quality?ip_version=4', responses: [{ body: reportBody({ extra: { summary: summaryBody(4, { vps_id: 'vps_002', ip_address: '198.51.100.20' }) } }) }] },
      { path: COLLECT, responses: [{ body: availableStatus }] },
      { path: '/api/vps/vps_002/ip-quality/collect', responses: [{ body: availableStatus }] },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage('/vps/vps_001/ip-quality?ip_version=4')
    await waitFor(() => expect(callsTo(fetchMock, `${BASE}?ip_version=4`)).toHaveLength(1))
    fireEvent.click(screen.getByRole('link', { name: '切换到 vps_002' }))
    await waitFor(() => expect(screen.getByLabelText('报告身份')).toHaveTextContent('198.51.100.20'))

    await act(async () => { slowFirst.resolve() })
    expect(screen.getByLabelText('报告身份')).toHaveTextContent('198.51.100.20')
    expect(screen.getByLabelText('报告身份')).not.toHaveTextContent(IPV4)
    // 旧 VPS 的报告从未就绪，不读取它的采集状态。
    expect(callsTo(fetchMock, COLLECT)).toHaveLength(0)
  })

  it.each([
    '/vps/vps_001/ip-quality?ip_version=5',
    '/vps/vps_001/ip-quality?ip_version=',
    '/vps/vps_001/ip-quality?ip_version=4&ip_version=6',
  ])('recovers from a malformed family parameter %s without calling the report API', async (entry) => {
    const fetchMock = routeFetch([
      { path: BASE, responses: [{ body: reportBody() }] },
      { path: COLLECT, responses: [{ body: availableStatus }] },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage(entry)

    expect(await screen.findByRole('heading', { name: '地址族参数无效' })).toBeInTheDocument()
    expect(within(familyGroup()).getByRole('button', { name: 'IPv4' })).toHaveAttribute('aria-pressed', 'false')
    expect(fetchMock).not.toHaveBeenCalled()

    fireEvent.click(screen.getByRole('link', { name: '查看默认报告' }))
    await waitFor(() => expect(screen.getByLabelText('报告身份')).toHaveTextContent(IPV4))
    await waitFor(() => expect(currentLocation()).toBe('/vps/vps_001/ip-quality?ip_version=4'))
    expect(reportCalls(fetchMock)).toHaveLength(1)
  })

  it.each([
    ['/vps/vps_001/ip-quality?report_id=ipq_000', 'legacy link without family'],
    ['/vps/vps_001/ip-quality?ip_version=6&report_id=ipq_000', 'conflicting family'],
    ['/vps/vps_001/ip-quality?ip_version=9&report_id=ipq_000', 'malformed family'],
  ])('lets the history report family win for a %s (%s)', async (entry) => {
    const fetchMock = routeFetch([
      { path: `${BASE}/reports/ipq_000`, responses: [{ body: reportBody({ reportId: 'ipq_000' }) }] },
      { path: COLLECT, responses: [{ body: availableStatus }] },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage(entry)

    await waitFor(() => expect(currentLocation()).toBe('/vps/vps_001/ip-quality?ip_version=4&report_id=ipq_000'))
    expect(screen.getByRole('link', { name: '查看最新报告' })).toHaveAttribute('href', '/vps/vps_001/ip-quality?ip_version=4')
    expect(screen.queryByRole('button', { name: '立即采集' })).not.toBeInTheDocument()
    expect(within(screen.getByLabelText('采集诊断')).getByText('ipq_000')).toBeInTheDocument()
    expect(reportCalls(fetchMock)).toHaveLength(1)
  })

  it('collects both families from the empty state and announces an update only when the shown report is from the round', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const fetchMock = routeFetch([
      { path: BASE, responses: [{ body: reportBody({ empty: true }) }] },
      { path: `${BASE}?ip_version=4`, responses: [{ body: reportBody({ collectionId: 'col_new' }) }] },
      {
        path: COLLECT,
        responses: [
          { body: availableStatus },
          { body: { ...availableStatus, request: collectRequest('dispatched', { dispatched_at: new Date().toISOString() }) } },
          { body: { ...availableStatus, request: dualCompleted([{ ip_version: 4, availability: 'available', report_status: 'success' }, { ip_version: 6, availability: 'unavailable', report_status: 'failure', error_code: 'address_family_unavailable' }]) } },
        ],
      },
      { method: 'POST', path: COLLECT, responses: [{ body: { ...availableStatus, request: collectRequest('pending') }, status: 202 }] },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: '暂无 IPv4 质量报告' })).toBeInTheDocument())
    await waitFor(() => expect(currentLocation()).toBe('/vps/vps_001/ip-quality?ip_version=4'))
    expect(screen.getAllByRole('button', { name: '立即采集' })).toHaveLength(1)
    await waitFor(() => expect(screen.getByRole('button', { name: '立即采集' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '立即采集' }))

    await waitFor(() => expect(screen.getByRole('button', { name: /采集中/ })).toBeDisabled())
    expect(callsTo(fetchMock, COLLECT, 'POST')).toHaveLength(1)
    expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).toHaveTextContent('等待 agent 同步')

    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS) })
    await waitFor(() => expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).toHaveTextContent('agent 已接收，正在检测'))

    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS) })
    await waitFor(() => expect(screen.getByLabelText('报告身份')).toHaveTextContent(IPV4))
    await waitFor(() => expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).toHaveTextContent('采集完成，报告已更新'))
    // 刷新使用显式族请求，默认请求只发生一次。
    expect(callsTo(fetchMock, BASE)).toHaveLength(1)
    expect(callsTo(fetchMock, `${BASE}?ip_version=4`)).toHaveLength(1)
    expect(callsTo(fetchMock, COLLECT, 'POST')).toHaveLength(1)

    // 请求结束后停止轮询。
    const pollsBefore = callsTo(fetchMock, COLLECT).length
    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS * 2) })
    expect(callsTo(fetchMock, COLLECT)).toHaveLength(pollsBefore)
  })

  // 回归：切族/进入历史的加载若重新读取采集状态，一个陈旧的空闲响应会丢掉进行中的请求、停止轮询，
  // 之后的完成既不刷新也不提示。服务端在首次读取后只会返回空闲，直到测试宣告本轮完成。
  function staleIdleCollectServer(completedRequest: Record<string, unknown>) {
    const server = { reads: 0, completed: false }
    const body = () => {
      server.reads += 1
      if (server.reads === 1) return { ...availableStatus, request: collectRequest('dispatched') }
      return server.completed ? { ...availableStatus, request: completedRequest } : availableStatus
    }
    return { server, route: { path: COLLECT, responses: [{ body }] } }
  }

  it('keeps an in-flight collection across a family switch and completes it on the newly selected family', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const { server, route } = staleIdleCollectServer(dualCompleted([
      { ip_version: 4, availability: 'available', report_status: 'success' },
      { ip_version: 6, availability: 'available', report_status: 'success' },
    ]))
    const fetchMock = routeFetch([
      { path: `${BASE}?ip_version=4`, responses: [{ body: reportBody() }] },
      { path: `${BASE}?ip_version=6`, responses: [{ body: reportBody({ family: 6 }) }, { body: reportBody({ family: 6, collectionId: 'col_new' }) }] },
      route,
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage('/vps/vps_001/ip-quality?ip_version=4')
    await waitFor(() => expect(screen.getByRole('button', { name: /采集中/ })).toBeDisabled())
    fireEvent.click(within(familyGroup()).getByRole('button', { name: 'IPv6' }))
    await waitFor(() => expect(screen.getByLabelText('报告身份')).toHaveTextContent(IPV6))
    // 切族不重新读取状态、不重复 POST，进行中的请求与进度保持。
    expect(server.reads).toBe(1)
    expect(callsTo(fetchMock, COLLECT, 'POST')).toHaveLength(0)
    expect(screen.getByRole('button', { name: /采集中/ })).toBeDisabled()
    expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).toHaveTextContent('正在采集最新 IP 质量')

    // 轮询仍在进行：本轮完成后刷新当前所选 IPv6，并只在该报告属于本轮时说已更新。
    server.completed = true
    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS) })
    await waitFor(() => expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).toHaveTextContent('采集完成，报告已更新'))
    expect(callsTo(fetchMock, `${BASE}?ip_version=6`)).toHaveLength(2)
    expect(callsTo(fetchMock, `${BASE}?ip_version=4`)).toHaveLength(1)
    expect(currentLocation()).toBe('/vps/vps_001/ip-quality?ip_version=6')
    expect(screen.getByRole('button', { name: '立即采集' })).toBeEnabled()
  })

  it('keeps an in-flight collection when a history report is opened and reports completion as history', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const { server, route } = staleIdleCollectServer(dualCompleted([
      { ip_version: 4, availability: 'available', report_status: 'success' },
      { ip_version: 6, availability: 'available', report_status: 'success' },
    ]))
    const fetchMock = routeFetch([
      { path: `${BASE}?ip_version=4`, responses: [{ body: reportBody() }] },
      { path: `${BASE}/reports/ipq_000`, responses: [{ body: reportBody({ reportId: 'ipq_000' }) }] },
      route,
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage('/vps/vps_001/ip-quality?ip_version=4')
    await waitFor(() => expect(screen.getByRole('button', { name: /采集中/ })).toBeDisabled())
    fireEvent.click(screen.getAllByRole('link', { name: /^查看 .* 的报告$/ })[0]!)
    await waitFor(() => expect(screen.getByRole('link', { name: '查看最新报告' })).toBeInTheDocument())
    expect(server.reads).toBe(1)
    expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).toHaveTextContent('正在采集最新 IP 质量')

    server.completed = true
    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS) })
    await waitFor(() => expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).toHaveTextContent('历史'))
    expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).not.toHaveTextContent('报告已更新')
    expect(currentLocation()).toBe('/vps/vps_001/ip-quality?ip_version=4&report_id=ipq_000')
    // 历史视图不刷新最新报告。
    expect(callsTo(fetchMock, `${BASE}?ip_version=4`)).toHaveLength(1)
    expect(callsTo(fetchMock, COLLECT, 'POST')).toHaveLength(0)
  })

  it('reports the selected family failure and keeps the previous IPv4 result after an IPv6-only success', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const failedAttempt = { observed_at: '2026-06-09T12:00:00Z', stale: false, report_status: 'failure', error_code: 'lookup_failed', collection_scope: 'dual_stack', collection_id: 'col_new' }
    const fetchMock = routeFetch([
      {
        path: `${BASE}?ip_version=4`,
        responses: [
          { body: reportBody() },
          { body: reportBody({ familyStatus: families({ 4: failedAttempt }) }) },
        ],
      },
      {
        path: COLLECT,
        responses: [
          { body: { ...availableStatus, request: collectRequest('dispatched') } },
          { body: { ...availableStatus, request: dualCompleted([{ ip_version: 4, availability: 'available', report_status: 'failure', error_code: 'lookup_failed' }, { ip_version: 6, availability: 'available', report_status: 'success' }], { report_status: 'partial' }) } },
        ],
      },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage('/vps/vps_001/ip-quality?ip_version=4')
    await waitFor(() => expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).toHaveTextContent('正在采集最新 IP 质量'))
    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS) })
    await waitFor(() => expect(screen.getByRole('note', { name: '地址族采集状态' })).toHaveTextContent('IPv4 出口探测失败'))
    const status = screen.getByRole('status', { name: 'IP 质量采集状态' })
    expect(status).toHaveTextContent('本轮 IPv4 采集失败')
    expect(status).not.toHaveTextContent('报告已更新')
    expect(screen.getByRole('note', { name: '地址族采集状态' })).toHaveTextContent('上次有效结果')
    expect(screen.getByLabelText('报告身份')).toHaveTextContent(IPV4)
    expect(currentLocation()).toBe('/vps/vps_001/ip-quality?ip_version=4')
  })

  it('keeps the old report and says the refresh failed when the post-collection reload fails', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const fetchMock = routeFetch([
      {
        path: `${BASE}?ip_version=4`,
        responses: [{ body: reportBody() }, { body: { error: 'temporary failure' }, status: 500 }],
      },
      {
        path: COLLECT,
        responses: [
          { body: { ...availableStatus, request: collectRequest('dispatched') } },
          { body: { ...availableStatus, request: dualCompleted([{ ip_version: 4, availability: 'available', report_status: 'success' }, { ip_version: 6, availability: 'available', report_status: 'success' }]) } },
        ],
      },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage('/vps/vps_001/ip-quality?ip_version=4')
    await waitFor(() => expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).toHaveTextContent('正在采集最新 IP 质量'))
    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS) })
    await waitFor(() => expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).toHaveTextContent('刷新失败'))
    expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).not.toHaveTextContent('报告已更新')
    expect(screen.getByLabelText('报告身份')).toHaveTextContent(IPV4)
    expect(screen.queryByRole('heading', { name: 'IP 质量报告加载失败' })).not.toBeInTheDocument()
  })

  it('stays on the history report when a collection completes', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const fetchMock = routeFetch([
      { path: `${BASE}/reports/ipq_000`, responses: [{ body: reportBody({ reportId: 'ipq_000' }) }] },
      {
        path: COLLECT,
        responses: [
          { body: { ...availableStatus, request: collectRequest('dispatched') } },
          { body: { ...availableStatus, request: dualCompleted([{ ip_version: 4, availability: 'available', report_status: 'success' }]) } },
        ],
      },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage('/vps/vps_001/ip-quality?ip_version=4&report_id=ipq_000')
    await waitFor(() => expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).toHaveTextContent('正在采集最新 IP 质量'))
    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS) })
    await waitFor(() => expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).toHaveTextContent('历史'))
    expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).not.toHaveTextContent('报告已更新')
    expect(currentLocation()).toBe('/vps/vps_001/ip-quality?ip_version=4&report_id=ipq_000')
    expect(reportCalls(fetchMock)).toHaveLength(1)
  })

  it('keeps the previous report and says so when a legacy single-family collection fails', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const fetchMock = routeFetch([
      { path: `${BASE}?ip_version=4`, responses: [{ body: reportBody() }] },
      {
        path: COLLECT,
        responses: [
          { body: { ...availableStatus, request: collectRequest('dispatched') } },
          { body: { ...availableStatus, request: collectRequest('completed', { report_status: 'failure', error_summary: 'lookup timeout', collection_scope: 'legacy_single_family' }) } },
        ],
      },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage('/vps/vps_001/ip-quality?ip_version=4')

    // 页面打开时已有进行中的请求：直接接手并展示进度。
    await waitFor(() => expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).toHaveTextContent('正在采集最新 IP 质量'))
    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS) })
    const status = () => screen.getByRole('status', { name: 'IP 质量采集状态' })
    await waitFor(() => expect(status()).toHaveTextContent('本次采集失败，仍展示上一份有效报告'))
    expect(status()).toHaveTextContent('检测请求超时，未能获取最新数据')
    expect(status()).toHaveTextContent('本次仅收到单地址族报告，未验证另一地址族')
    const details = status().querySelector('details')
    expect(details).not.toBeNull()
    expect(details).not.toHaveAttribute('open')
    expect(details).toHaveTextContent('lookup timeout')
    expect(screen.getByLabelText('报告身份')).toHaveTextContent(IPV4)
  })

  it('shows why collection is unavailable in the empty state and links to workbench with explicit vpsId', async () => {
    const fetchMock = routeFetch([
      { path: BASE, responses: [{ body: reportBody({ empty: true }) }] },
      { path: COLLECT, responses: [{ body: { enabled: true, available: false, unavailable_reason: 'no_monitoring_instance' } }] },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    const status = () => screen.getByRole('status', { name: 'IP 质量采集状态' })
    await waitFor(() => expect(status()).toHaveTextContent('该 VPS 尚未接入监控 agent'))
    expect(within(status()).getByRole('link', { name: '前往监控工作台' })).toHaveAttribute('href', '/vps/vps_001?workbench=monitoring')
    expect(screen.getByRole('button', { name: '立即采集' })).toBeDisabled()
    expect(screen.queryByText('0.0.0.0')).not.toBeInTheDocument()
  })

  it('shows the server reason after a rejected collection request and links to actual MI with valid returnVPS', async () => {
    const fetchMock = routeFetch([
      { path: BASE, responses: [{ body: reportBody() }] },
      {
        path: COLLECT,
        responses: [
          { body: availableStatus },
          { body: { enabled: true, available: false, unavailable_reason: 'monitoring_paused', monitoring_instance_id: 'mi_001' } },
        ],
      },
      { method: 'POST', path: COLLECT, responses: [{ body: { error: 'monitoring instance monitoring is paused', reason: 'monitoring_paused' }, status: 409 }] },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await waitFor(() => expect(screen.getByRole('button', { name: '立即采集' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '立即采集' }))
    const status = () => screen.getByRole('status', { name: 'IP 质量采集状态' })
    await waitFor(() => expect(status()).toHaveTextContent('监控已暂停'))
    expect(within(status()).getByRole('link', { name: '前往监控实例' })).toHaveAttribute('href', '/monitoring/mi_001?return_vps=vps_001')
    expect(screen.getByRole('button', { name: '立即采集' })).toBeDisabled()
    expect(screen.queryByRole('button', { name: /恢复/ })).not.toBeInTheDocument()
  })

  it('renders the selected history report with its own revision, coverage and risk after switching from latest', async () => {
    const coverage = {
      expected_provider_count: 18,
      successful_provider_count: 5,
      failed_provider_count: 0,
      skipped_provider_count: 0,
      not_configured_provider_count: 13,
      expected_service_count: 7,
      successful_service_count: 1,
      failed_service_count: 0,
      skipped_service_count: 0,
      not_configured_service_count: 0,
    }
    const latest = reportBody({ diagnostics: { source_version: 'v2', collection_revision: 4, service_probe_revision: 2 } })
    const latestBody = {
      ...latest,
      summary: { ...latest.summary, coverage },
      latest_report: { ...latest.latest_report, coverage },
      provider_results: [{ provider: 'proxycheck.io', status: 'success', source_type: 'default', risk_level: 'high', risk_score: '80' }],
    }
    const historyCoverage = { ...coverage, successful_provider_count: 4, failed_provider_count: 1, successful_service_count: 0, skipped_service_count: 7 }
    const history = reportBody({ reportId: 'ipq_000', diagnostics: { source_version: 'v2', service_probe_revision: 1 } })
    const historyBody = {
      ...history,
      summary: { ...history.summary, risk_level: 'low', coverage: historyCoverage },
      latest_report: { ...history.latest_report, coverage: historyCoverage },
      provider_results: [{ provider: 'ipapi.is', status: 'success', source_type: 'default', risk_level: 'medium', is_vpn: true }],
      service_unlocks: [
        { service: 'netflix', source: 'netflix_title_probe', status: 'unknown', probe_status: 'skipped', error_code: 'unsupported_default_probe' },
      ],
    }
    const fetchMock = routeFetch([
      { path: BASE, responses: [{ body: latestBody }] },
      { path: `${BASE}/reports/ipq_000`, responses: [{ body: historyBody }] },
      { path: COLLECT, responses: [{ body: availableStatus }] },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: '服务可用性（免登录探测）' })).toBeInTheDocument())
    expect(screen.getByLabelText('数据库风险评级').querySelector('.ipq-verdict__value strong')).toHaveTextContent('高风险')
    expect(screen.getByRole('note', { name: '服务探测可信度' })).toHaveClass('ipq-notice--muted')
    expect(within(screen.getByLabelText('IP 质量摘要指标')).getByText('已启用来源响应成功率').nextElementSibling).toHaveTextContent('响应成功 5/5')

    const historyLink = screen.getAllByRole('link', { name: /^查看 .* 的报告$/ })
      .find((link) => link.getAttribute('href')?.includes('report_id=ipq_000'))
    expect(historyLink).toHaveAttribute('href', '/vps/vps_001/ip-quality?ip_version=4&report_id=ipq_000')
    fireEvent.click(historyLink as HTMLAnchorElement)

    await waitFor(() => expect(callsTo(fetchMock, `${BASE}/reports/ipq_000`)).toHaveLength(1))
    await waitFor(() => expect(screen.getByRole('link', { name: '查看最新报告' })).toBeInTheDocument())
    expect(screen.getByRole('heading', { name: '服务解锁' })).toBeInTheDocument()
    expect(screen.getByRole('note', { name: '服务探测可信度' })).toHaveTextContent('已停用')
    // 历史报告的风险评级来自它自己的成功来源，不沿用最新报告，也不相信 summary low。
    expect(screen.getByLabelText('数据库风险评级').querySelector('.ipq-verdict__value strong')).toHaveTextContent('中风险')
    expect(within(screen.getByLabelText('IP 质量摘要指标')).getByText('已启用来源响应成功率').nextElementSibling).toHaveTextContent('响应成功 4/5')
  })

  it('retries a failed report load without leaving the page', async () => {
    const fetchMock = routeFetch([
      { path: BASE, responses: [{ body: { error: 'temporary failure' }, status: 500 }, { body: reportBody() }] },
      { path: COLLECT, responses: [{ body: availableStatus }] },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage()

    await waitFor(() => expect(screen.getByRole('heading', { name: 'IP 质量报告加载失败' })).toBeInTheDocument())
    expect(screen.getByText('temporary failure')).toBeInTheDocument()
    // 错误页不启动采集状态查询。
    expect(callsTo(fetchMock, COLLECT)).toHaveLength(0)
    fireEvent.click(screen.getByRole('button', { name: '重试' }))

    await waitFor(() => expect(screen.getByLabelText('报告身份')).toHaveTextContent(IPV4))
    expect(callsTo(fetchMock, BASE)).toHaveLength(2)
  })
})
