import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { Link, MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { afterEach, describe, expect, it, vi, type Mock } from 'vitest'

import { COLLECT_POLL_INTERVAL_MS } from '../components/ip-quality/useIPQualityCollect'
import { ENRICHMENT_POLL_INTERVAL_MS } from '../components/ip-quality/useIPQualityEnrichment'
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

function productSummary(riskFlags: string[] = []) {
  return {
    risk_flags: riskFlags, high_risk_sources: [], restricted_services: [],
    blacklist_listed_count: 0, blacklist_checked_count: 0,
    service_available_count: 1, service_partial_count: 0, service_restricted_count: 0, service_unknown_count: 0,
    geo_status: 'unknown',
  }
}

function productBody(reportId: string, state: 'running' | 'complete') {
  const done = state === 'complete'
  return {
    revision: 1, report_id: reportId, ip_address: IPV4, ip_version: 4, state,
    agent_observed_at: '2026-06-08T12:00:00Z', completed_at: done ? '2026-06-08T12:01:00Z' : null,
    ptr_names: [],
    providers: [{ provider: 'ipinfo', status: 'success', source_type: 'default', is_vpn: true, is_proxy: false }],
    mail: [{ provider: 'gmail', domain: 'gmail.com', status: 'reachable', mx_host: 'mx.example', target_ip: '192.0.2.25', error_code: '', mx_preference: 5, greeting_code: 220, latency_ms: 80, observed_at: '2026-06-08T12:00:00Z' }],
    blacklists: done
      ? [{ source: 'spamcop', category: 'spam', status: 'not_listed', query_method: 'dnsbl', error_code: '', reference_url: '', matched_range: '', codes: [], observed_at: '2026-06-08T12:01:00Z', dataset_at: null }]
      : [],
    registration: { status: 'unknown', rir: '', network_handle: '', start_address: '', end_address: '', network_name: '', network_country_code: '', registrant_country_code: '', registrant_name: '', source_url: '', registered_at: null, updated_at: null },
    asn: { status: 'unknown', number: '', name: '', domain: '', type: '', country_code: '', rir: '', source_url: '', route: '', registered_at: null, updated_at: null, observed_at: null, ipv4_prefixes: null, ipv4_addresses: null, ipv6_prefixes: null, ipv6_48s: null, origins: [] },
    summary: { ...productSummary(['vpn']), blacklist_checked_count: done ? 1 : 0 },
  }
}

function productReportBody(state: 'running' | 'complete') {
  const base = reportBody({ diagnostics: { source_version: 'v2', collection_revision: 4, service_probe_revision: 4 } })
  return {
    ...base,
    summary: { ...base.summary, risk_level: '', product_revision: 1, intelligence_state: state, product_summary: productSummary(['vpn']) },
    product: productBody('ipq_001', state),
  }
}

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
    const trigger = screen.getByRole('button', { name: '立即采集' })
    expect(trigger).toBeEnabled()
    // 采集范围在点击后的菜单里选择，按钮旁不再常驻解释文字。
    expect(trigger).toHaveAttribute('aria-haspopup', 'menu')
    expect(trigger).not.toHaveAttribute('aria-describedby')
    expect(screen.queryByText('分别检测 IPv4 / IPv6')).not.toBeInTheDocument()
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

  it('collects the chosen dual-stack scope from the empty state and announces an update only when the shown report is from the round', async () => {
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
    fireEvent.click(screen.getByRole('menuitem', { name: '采集 IPv4 / IPv6' }))

    await waitFor(() => expect(screen.getByRole('button', { name: /采集中/ })).toBeDisabled())
    expect(callsTo(fetchMock, COLLECT, 'POST')).toHaveLength(1)
    expect(JSON.parse(String(callsTo(fetchMock, COLLECT, 'POST')[0]?.[1]?.body))).toEqual({ ip_versions: [4, 6] })
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
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
    expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).toHaveTextContent('正在采集 IPv4 / IPv6')

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
    expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).toHaveTextContent('正在采集 IPv4 / IPv6')

    server.completed = true
    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS) })
    await waitFor(() => expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).toHaveTextContent('历史'))
    expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).not.toHaveTextContent('报告已更新')
    expect(currentLocation()).toBe('/vps/vps_001/ip-quality?ip_version=4&report_id=ipq_000')
    // 历史视图不刷新最新报告。
    expect(callsTo(fetchMock, `${BASE}?ip_version=4`)).toHaveLength(1)
    expect(callsTo(fetchMock, COLLECT, 'POST')).toHaveLength(0)
  })

  it('collects only IPv6 while viewing IPv4 without refreshing, switching family or claiming the IPv4 report was updated', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const v6Request = (status: string, extra: Record<string, unknown> = {}) => collectRequest(status, { requested_ip_versions: [6], ...extra })
    const fetchMock = routeFetch([
      { path: `${BASE}?ip_version=4`, responses: [{ body: reportBody() }] },
      {
        path: COLLECT,
        responses: [
          { body: availableStatus },
          { body: { ...availableStatus, request: v6Request('completed', { report_status: 'success', collection_scope: 'single_family', collection_id: 'col_v6', family_results: [{ ip_version: 6, availability: 'available', report_status: 'success' }] }) } },
        ],
      },
      { method: 'POST', path: COLLECT, responses: [{ body: { ...availableStatus, request: v6Request('pending') }, status: 202 }] },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage('/vps/vps_001/ip-quality?ip_version=4')
    await waitFor(() => expect(screen.getByRole('button', { name: '立即采集' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '立即采集' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '采集 IPv6' }))

    await waitFor(() => expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).toHaveTextContent('正在采集 IPv6'))
    expect(JSON.parse(String(callsTo(fetchMock, COLLECT, 'POST')[0]?.[1]?.body))).toEqual({ ip_versions: [6] })

    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS) })
    await waitFor(() => expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).toHaveTextContent('IPv6 采集完成'))
    expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).not.toHaveTextContent('报告已更新')
    expect(currentLocation()).toBe('/vps/vps_001/ip-quality?ip_version=4')
    expect(screen.getByLabelText('报告身份')).toHaveTextContent(IPV4)
    // 当前 IPv4 报告不刷新，也不读取 IPv6。
    expect(callsTo(fetchMock, `${BASE}?ip_version=4`)).toHaveLength(1)
    expect(callsTo(fetchMock, `${BASE}?ip_version=6`)).toHaveLength(0)
  })

  it('shows an actionable error and stops when the agent cannot collect a single family', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const fetchMock = routeFetch([
      { path: `${BASE}?ip_version=4`, responses: [{ body: reportBody() }] },
      {
        path: COLLECT,
        responses: [
          { body: availableStatus },
          { body: { ...availableStatus, request: collectRequest('failed', { requested_ip_versions: [4], error_code: 'unsupported_agent' }) } },
        ],
      },
      { method: 'POST', path: COLLECT, responses: [{ body: { ...availableStatus, request: collectRequest('pending', { requested_ip_versions: [4] }) }, status: 202 }] },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage('/vps/vps_001/ip-quality?ip_version=4')
    await waitFor(() => expect(screen.getByRole('button', { name: '立即采集' })).toBeEnabled())
    fireEvent.click(screen.getByRole('button', { name: '立即采集' }))
    fireEvent.click(screen.getByRole('menuitem', { name: '采集 IPv4' }))

    const status = () => screen.getByRole('status', { name: 'IP 质量采集状态' })
    // 先等 POST 落地、页面进入进行中，轮询计时器才被设置；之后再推进时钟。
    await waitFor(() => expect(status()).toHaveTextContent('正在采集 IPv4'))
    expect(JSON.parse(String(callsTo(fetchMock, COLLECT, 'POST')[0]?.[1]?.body))).toEqual({ ip_versions: [4] })
    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS) })

    // failed 终态：显示不支持单族的可操作错误（精确文案由展示层单测覆盖），停止进行中并可重新选择。
    await waitFor(() => expect(status()).toHaveTextContent('agent 版本不支持单独采集 IPv4'))
    expect(status()).not.toHaveTextContent('正在采集')
    expect(screen.getByRole('button', { name: '立即采集' })).toBeEnabled()
    const polls = callsTo(fetchMock, COLLECT).length
    await act(async () => { await vi.advanceTimersByTimeAsync(COLLECT_POLL_INTERVAL_MS * 2) })
    expect(callsTo(fetchMock, COLLECT)).toHaveLength(polls)
    expect(callsTo(fetchMock, `${BASE}?ip_version=4`)).toHaveLength(1)
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
    await waitFor(() => expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).toHaveTextContent('正在采集 IPv4 / IPv6'))
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
    await waitFor(() => expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).toHaveTextContent('正在采集 IPv4 / IPv6'))
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
    await waitFor(() => expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).toHaveTextContent('正在采集 IPv4 / IPv6'))
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
    await waitFor(() => expect(screen.getByRole('status', { name: 'IP 质量采集状态' })).toHaveTextContent('正在采集 IPv4 / IPv6'))
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
    fireEvent.click(screen.getByRole('menuitem', { name: '采集 IPv4 / IPv6' }))
    const status = () => screen.getByRole('status', { name: 'IP 质量采集状态' })
    await waitFor(() => expect(status()).toHaveTextContent('监控已暂停'))
    expect(within(status()).getByRole('link', { name: '前往监控实例' })).toHaveAttribute('href', '/monitoring/mi_001?return_vps=vps_001')
    expect(screen.getByRole('button', { name: '立即采集' })).toBeDisabled()
    expect(screen.queryByRole('button', { name: /恢复/ })).not.toBeInTheDocument()
  })

  it('renders the selected history report with its own facts after switching from a product report', async () => {
    const latest = reportBody({ diagnostics: { source_version: 'v2', collection_revision: 4, service_probe_revision: 4 } })
    const latestBody = {
      ...latest,
      summary: { ...latest.summary, risk_level: '', product_revision: 1, intelligence_state: 'complete', product_summary: productSummary(['vpn']) },
      product: productBody('ipq_001', 'complete'),
    }
    const history = reportBody({ reportId: 'ipq_000', diagnostics: { source_version: 'v2', service_probe_revision: 1 } })
    const historyBody = {
      ...history,
      summary: { ...history.summary, risk_level: 'medium' },
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

    await waitFor(() => expect(screen.getByRole('heading', { name: '邮件连通性' })).toBeInTheDocument())
    expect(screen.queryByText('旧版报告')).not.toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '邮件连通性' }).closest('section')).toHaveTextContent('可连接 1/1')

    const historyLink = screen.getAllByRole('link', { name: /^查看 .* 的报告$/ })
      .find((link) => link.getAttribute('href')?.includes('report_id=ipq_000'))
    expect(historyLink).toHaveAttribute('href', '/vps/vps_001/ip-quality?ip_version=4&report_id=ipq_000')
    fireEvent.click(historyLink as HTMLAnchorElement)

    await waitFor(() => expect(callsTo(fetchMock, `${BASE}/reports/ipq_000`)).toHaveLength(1))
    await waitFor(() => expect(screen.getByRole('link', { name: '查看最新报告' })).toBeInTheDocument())
    // 历史报告用它自己的事实：没有 product 就是旧版报告，不借最新报告的邮件或摘要。
    expect(screen.getByText('旧版报告')).toBeInTheDocument()
    expect(screen.getByText('探测已停用')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: '邮件连通性' }).closest('section')).toHaveTextContent('旧版报告无此项')
    expect(screen.getByRole('group', { name: '风险命中' })).toHaveTextContent('旧版评级 · 中风险')
  })

  it('polls the same selected report while Center enrichment runs and stops at a terminal state', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const running = productReportBody('running')
    const complete = productReportBody('complete')
    const fetchMock = routeFetch([
      { path: `${BASE}?ip_version=4`, responses: [{ body: running }] },
      { path: `${BASE}/reports/ipq_001`, responses: [{ body: running }, { body: complete }] },
      { path: COLLECT, responses: [{ body: availableStatus }] },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage('/vps/vps_001/ip-quality?ip_version=4')
    await waitFor(() => expect(screen.getByRole('status', { name: '补充资料状态' })).toHaveTextContent('查询中'))
    expect(screen.getByRole('heading', { name: '黑名单' }).closest('section')).toHaveTextContent('查询中')

    await act(async () => { await vi.advanceTimersByTimeAsync(ENRICHMENT_POLL_INTERVAL_MS) })
    await waitFor(() => expect(callsTo(fetchMock, `${BASE}/reports/ipq_001`)).toHaveLength(1))
    await act(async () => { await vi.advanceTimersByTimeAsync(ENRICHMENT_POLL_INTERVAL_MS) })
    await waitFor(() => expect(screen.queryByRole('status', { name: '补充资料状态' })).not.toBeInTheDocument())
    expect(screen.getByRole('heading', { name: '黑名单' }).closest('section')).toHaveTextContent('命中 0 / 已查 1')
    // URL 与所选族不变；终态后不再轮询。
    expect(currentLocation()).toBe('/vps/vps_001/ip-quality?ip_version=4')
    await act(async () => { await vi.advanceTimersByTimeAsync(ENRICHMENT_POLL_INTERVAL_MS * 3) })
    expect(callsTo(fetchMock, `${BASE}/reports/ipq_001`)).toHaveLength(2)
  })

  it('stops polling after a failed enrichment read and resumes only on retry', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const running = productReportBody('running')
    const fetchMock = routeFetch([
      { path: `${BASE}?ip_version=4`, responses: [{ body: running }] },
      { path: `${BASE}/reports/ipq_001`, responses: [{ body: { error: 'temporary' }, status: 500 }, { body: productReportBody('complete') }] },
      { path: COLLECT, responses: [{ body: availableStatus }] },
    ])
    vi.stubGlobal('fetch', fetchMock)

    renderPage('/vps/vps_001/ip-quality?ip_version=4')
    await waitFor(() => expect(screen.getByRole('status', { name: '补充资料状态' })).toHaveTextContent('查询中'))
    await act(async () => { await vi.advanceTimersByTimeAsync(ENRICHMENT_POLL_INTERVAL_MS) })
    await waitFor(() => expect(screen.getByRole('status', { name: '补充资料状态' })).toHaveTextContent('资料刷新失败'))
    await act(async () => { await vi.advanceTimersByTimeAsync(ENRICHMENT_POLL_INTERVAL_MS * 3) })
    expect(callsTo(fetchMock, `${BASE}/reports/ipq_001`)).toHaveLength(1)
    // 已显示的 Agent 事实保留。
    expect(screen.getByLabelText('报告身份')).toHaveTextContent(IPV4)

    fireEvent.click(within(screen.getByRole('status', { name: '补充资料状态' })).getByRole('button', { name: '重试' }))
    await waitFor(() => expect(callsTo(fetchMock, `${BASE}/reports/ipq_001`)).toHaveLength(2))
    await waitFor(() => expect(screen.queryByRole('status', { name: '补充资料状态' })).not.toBeInTheDocument())
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
